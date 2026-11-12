---
title: 第 117 讲 · 实战 11 · Kubernetes Operator 与可观测平台
description: 用 CRD 声明期望状态，写出完整 Reconcile 循环与限速队列，并把指标、日志、告警收成平台
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="75 分钟" goal="用 Go 管理 Go 服务" prereq="第 116 讲 · 秒杀与高并发库存" />

# 第 117 讲 · 实战 11 · Kubernetes Operator 与可观测平台

> 前十一讲我们交付了 CLI、短链、图片服务、KV 存储、电商与秒杀系统，服务的数量从一个变成十几个。靠 `kubectl scale` 加几段脚本去管它们，有个绕不过去的硬伤：**脚本执行一次就结束了**。有人手动改了副本数、某个 Pod 半夜被驱逐、一次发布被回滚到旧版本，没有任何东西会把状态拉回来。Operator 把这个思路彻底反过来——业务方只声明「我要三个 v1.4.0 的副本」，控制器则不知疲倦地把实际状态往期望状态收敛，无论中间发生了多少次偏离。这一讲用标准库写出一套完整、可编译、可压测的调谐循环，再把指标、结构化日志与错误预算告警接成闭环，目标是这份文档能直接拿去做设计评审，读者照着就能把系统搭出来，并且清楚每个决策的代价。

<CheckList id="L117-goal" title="本讲目标" :items='["用 CRD 的 OpenAPI schema 加 CEL 把非法期望挡在落库之前","用线程安全的 Informer 缓存承载 watch，按 resourceVersion 丢弃乱序事件","写出可重入、幂等、单动作推进的 Reconcile 状态机与 Finalizer 删除流程","用限速队列加指数退避保护 apiserver，用全量 resync 兜底事件丢失","把调谐指标、slog 结构化日志与错误预算燃烧率告警接成可观测闭环"]' />

## 0. 项目定位与真实场景

平台团队要纳管前面所有项目产出的 Go 服务：短链、图片服务、日志 ETL、四版 KV、电商与秒杀。它们形态各异，有的是无状态 Deployment，有的是带持久卷的 StatefulSet，但运维动作高度同质——改副本数、换镜像版本、看就绪状态、查发布历史。谁在用这套系统？三类人：业务研发提交一份声明「要几个、跑哪个版本」，SRE 负责平台稳定性与容量，On-call 在夜里被告警叫醒后要能在五分钟内判断「是控制器病了还是 apiserver 病了」。

量级先摆出来，后面的所有架构决策都由它推导：单集群纳管约 2000 个 `KVApp` 自定义资源，每天约 5000 次 spec 变更，控制器对 apiserver 的写 QPS 峰值约束在 40 以内（超过就会和真实业务控制器抢 etcd 的写带宽），单个 CR 的期望副本数在 1 到 100 之间。现有方案是 Helm 加一堆发布脚本：Helm 渲染出 Deployment，脚本执行 `kubectl apply`，然后就没有然后了。痛点非常具体：**没有自愈**，Pod 挂了最多由 ReplicaSet 补，但「有人把副本数手改成 1」这种配置漂移永远不会被纠正；**没有审计**，谁在什么时候把生产副本数从 3 改成了 1 无从查起；**没有收敛语义**，一次升级做到一半控制器进程被杀，剩下的一半副本永远是旧版本。

本讲要解决的核心矛盾可以浓缩成一句话：**如何用一段永不停止的程序，把「声明的期望」和「实际的状态」持续对齐，同时保证这段程序自己不会把控制面打爆、自己挂了也能被拉起来继续对齐**。这个矛盾决定了后面一切选择：为什么必须是 level-triggered 而不是 edge-triggered，为什么 Reconcile 必须幂等，为什么失败一定要交给队列退避而不是就地死循环，为什么可观测要盯「燃烧率」而不是「失败次数」。

<Term word="声明式" text="用户只描述最终想要的态，不写达成它的步骤；系统负责反复比较与纠偏，和命令式脚本「执行一次即结束」相对" />

## 1. 需求与约束

先把需求写成可验收的条目，避免后面讨论「做没做完」时扯皮。功能需求按编号管理，每条都给一个能自动化验证的验收标准。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| F1 | 用 CRD 表达期望状态 | 提交含 `replicas` 与 `version` 的 YAML，apiserver 接受；字段非法时创建被拒绝并回显原因 |
| F2 | 控制器自动扩缩容 | 实际就绪副本少于期望时触发扩容，多于期望时触发缩容，最终等于期望值 |
| F3 | 控制器自动滚动升级 | 实际版本与期望不一致时触发 rollout，升级过程可被中断后接续 |
| F4 | 状态回写与条件 | 每次调谐把 `phase` 与 `conditions` 写回 status，`kubectl get kvapp` 可见 |
| F5 | 删除前清理外部资源 | 带 Finalizer 的 CR 被删除时，先幂等清理 StatefulSet 与 PVC，再移除 Finalizer |
| F6 | 状态冲突可重试 | `UpdateStatus` 返回 409 冲突时不覆盖他人写入，带 `resourceVersion` 退避重试 |
| F7 | 可观测闭环 | 暴露调谐次数、耗时、失败率、队列深度，错误预算燃烧率超阈值触发告警 |
| F8 | 多版本平滑升级 | `v1alpha1` 与 `v1beta1` 共存期间老对象可读，缺省字段在转换时补齐 |

非功能指标同样要可度量。这里的一致性级别是**最终一致**：控制器不承诺「提交即生效」，只承诺「在无持续故障的前提下，有限时间内收敛」，收敛时间用 P99 度量。可用性 SLA 针对控制器本身，靠多副本加 leader election 实现，任意时刻只有一个实例在调谐，租约到期后 15 秒内完成切换。RPO 为 0——控制器不保存任何本地持久状态，所有真相都在 apiserver，进程崩溃不丢数据；RTO 目标 1 分钟以内，含选主与缓存重建。

| 指标 | 目标值 | 说明与依据 |
| --- | --- | --- |
| 纳管 CR 规模 | 2000 个 / 单集群 | 决定缓存内存与全量 resync 成本 |
| 调谐写 QPS 上限 | ≤ 40 | 避免与业务控制器争抢 etcd 写带宽 |
| 单次 Reconcile P99 | ≤ 200 ms | 纯内存决策加一次 API 调用，不含外部清理 |
| 变更后收敛 P99 | ≤ 30 s | 队列积压有限时的端到端时间 |
| 全量 resync 周期 | 5 min | 事件丢失的最坏兜底窗口；2000 个 CR 扫一遍在百秒内完成，不堆积 |
| 单 key 重试预算 | 最多 8 次 | 退避从 5 ms 起、指数增长、封顶 1000 ms |
| 控制器可用性 | 99.9% | 2 副本 leader election，租约 15 s |
| RTO / RPO | ≤ 1 min / 0 | 无本地状态，重启后靠 watch 重建缓存 |

明确「本讲不做什么」，把边界钉死：不实现一个真实的 `client-go` REST 客户端，控制器只依赖一张窄 `Cluster` 接口，单测用 fake；不做多集群联邦与跨集群调谐；不部署真正的 Admission Webhook 服务，字段校验用 CRD 内置的 OpenAPI schema 加 CEL 表达式完成；不引入 `controller-runtime` 生成骨架，但会在第 13 节讲清生产该在什么规模下换过去。假设边界是单集群、单 CRD、单版本为 storage、控制器无本地磁盘状态、apiserver 版本不低于 1.28（CEL 校验规则从 1.25 起 GA，1.28 稳定可用）。

<Callout type="idea" title="约束先行，而不是技术栈先行" text="先写「2000 个 CR、写 QPS 不超 40、收敛 P99 30 秒」，架构自然长成「缓存读、事件只入队、单动作推进、失败退避」的样子。反过来先说「我要用 client-go 和 workqueue」，再去找需求，几乎一定会过度设计：几百个 CR 的规模根本不需要复杂的分片与多队列。" />

## 2. 架构蓝图

主图分三层。分层依据是**变化频率与故障域**：接入层随 apiserver 版本变化，控制层是纯逻辑、变更最频繁但可单测，执行与观测层是副作用出口、必须能降级。依赖方向严格单向向下，执行层不允许反向调用控制层做任何决策。

<ArchBlueprint title="KVApp Operator 分层架构" caption="事件只负责把对象 key 放进队列，全部决策集中在控制层的纯函数里" :layers='[{"name":"接入与校验层","note":"对外唯一入口，负责持久化与准入","protocol":"HTTPS 加 admission review","items":[{"label":"API Server","sub":"CR 持久化与 watch","detail":"职责：持久化 CR 期望状态并对外提供 watch；技术选型：apiextensions.k8s.io/v1 的 CRD 加 etcd；失败处理：写入冲突返回 409，由控制器带 resourceVersion 重试。","tone":"blue"},{"label":"Schema 校验","sub":"字段合法性与缺省","detail":"职责：在对象落库前校验字段合法性；技术选型：OpenAPI v3 schema 加 CEL 规则，避免单独部署 webhook；失败处理：校验失败直接拒绝并回显原因，不进入调谐队列。","tone":"blue"}]},{"name":"控制与调谐层","note":"纯逻辑，无外部副作用，可完整单测","protocol":"WATCH 与 LIST 走 HTTP2 长连接","items":[{"label":"Informer 缓存","sub":"本地只读副本与索引","detail":"职责：把 watch 到的对象缓存在本地并按 namespace 建索引；技术选型：线程安全 Store 加 DeepCopy，按 resourceVersion 判乱序；失败处理：断连后 relist 重建缓存，旧版本事件直接丢弃。","tone":"green"},{"label":"限速 WorkQueue","sub":"按 key 去重与退避","detail":"职责：按 key 去重并让同一对象串行调谐；技术选型：带指数退避的限速队列，失败重入上限八次；失败处理：超过重试预算进死信并告警，绝不打爆 apiserver。","tone":"orange"},{"label":"Reconciler","sub":"纯函数决策与幂等执行","detail":"职责：比较期望与实际并每次只推进一个动作；技术选型：纯函数 Decide 加幂等 Apply，多副本靠 leader election 选主；失败处理：调谐出错直接返回，由队列退避重入，进程崩溃重启后继续收敛。","tone":"orange"}]},{"name":"执行与可观测层","note":"副作用出口，必须可降级","items":[{"label":"Cluster 写入器","sub":"落地扩缩容与升级","detail":"职责：把扩缩容与升级动作落到工作负载；技术选型：绝对值语义的 Patch，删资源靠 Finalizer 先清理后移除；失败处理：清理幂等，重复调用结果一致。","tone":"purple"},{"label":"指标与日志","sub":"Counter 与 slog","detail":"职责：暴露调谐次数、耗时、失败率与队列深度；技术选型：Counter 加 Histogram 加 slog 结构化日志；失败处理：指标端点不可用时降级为仅日志，不阻断调谐。","tone":"purple"},{"label":"告警规则","sub":"错误预算燃烧率","detail":"职责：用错误预算燃烧率而非绝对次数触发告警；技术选型：Prometheus recording rule 加 for 持续窗口；失败处理：告警静默时按 On-call 预案人工介入。","tone":"red"}]}]' />

逐层解读为什么这样切。接入层单独存在，是因为「校验」必须在对象落库之前完成：一旦非法期望（比如 `replicas` 为负、版本写成 `latest`）进了 etcd，控制器才去拒绝就已经晚了，状态会脏一段时间。把校验放进 CRD 的 schema 与 CEL，收益是不用额外部署一个 webhook 服务，代价是表达式能力有限，跨字段、跨资源的复杂校验仍得回到 webhook——这个取舍在第 13 节会再权衡。控制层是整套系统的心脏，但它被刻意设计成**不碰任何 SDK**：`Decide` 是纯函数，输入是期望与实际的快照，输出是一个动作和一个相位；所有外部读写都收进 `Cluster` 接口。这让控制层可以用 `go test` 完整覆盖，而不需要起一个 apiserver。执行与观测层是唯一有副作用的地方，所以必须能降级：指标端点挂了只影响监控，不能让调谐停摆。

<ConceptDiagram type="k8s" title="Operator 控制回路：CR 变更经 watch 入队，Reconcile 读状态、算动作、回写状态" caption="事件只搬运 key，不搬运状态；状态永远以 apiserver 里的最新快照为准" />

<FlowTimeline direction="horizontal" :steps='[{"title":"事件入队","desc":"CR 变更或定时 resync 把 key 放进队列","tone":"blue"},{"title":"读取快照","desc":"Cluster.Get 拿到期望与实际及 resourceVersion","tone":"green"},{"title":"纯函数决策","desc":"Decide 算出唯一动作与目标相位","tone":"orange"},{"title":"执行单动作","desc":"扩缩容、升级或清理只做一个","tone":"orange"},{"title":"回写状态","desc":"UpdateStatus 带乐观锁，失败则退避重入","tone":"red"},{"title":"埋点收尾","desc":"记录耗时、失败原因与燃烧率","tone":"purple"}]' />

依赖方向必须单向向下，绝不允许反向依赖，理由有三个。其一，执行层若回调控制层拿「上次决策」，控制层就不再是纯函数，无法脱离集群单测，这是最容易发生也最致命的边界破坏。其二，观测层若被控制层直接依赖，指标库的抖动会变成调谐的故障源，违反「可观测是可降级的旁路」这一定位。其三，缓存是只读投影，若控制层把「缓存里没有」当作「对象已删除」的直接结论，就会和 apiserver 的真相产生分歧——所以删除判定仍以 `Get` 返回的 `Exists` 为准，缓存只用于快速列举与降低读放大。

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `api` | 用户 YAML | `KVApp` 类型、`Validate` 结果 | 无（纯类型） | 无 | 校验失败返回 error，不写 etcd |
| `informer` | watch 事件流 | 线程安全缓存与 key 列表 | `api` | 内存 Store | 断连 relist 重建，乱序按 RV 丢弃 |
| `queue` | key 与调谐结果 | 待处理 key、退避调度 | 无 | dirty 与 processing 集合 | 超预算进死信并告警 |
| `reconcile` | 期望与实际快照 | 一个 `Action` 与 `Phase` | `api` | 无（纯函数） | 不就地重试，错误上抛 |
| `controller` | 队列 key | 调谐调用与状态回写 | `reconcile`、`Cluster` | leader 租约 | 失败退回队列退避，全量 resync 兜底 |
| `cluster` | 动作名与参数 | apiserver 写入 | `client-go`（生产） | 无 | 409 冲突上抛，由上层带 RV 重试 |
| `metrics` | 调谐结果事件 | Counter、Histogram、日志 | 无 | 原子计数 | 降级为仅日志，不阻断调谐 |

包目录树按领域而非按层组织，避免 `controllers/` 里堆一堆看不出业务的文件：

```text
kvapp-operator/
├── cmd/manager/main.go          # 装配配置、选主、启动控制器与指标端点
├── api/
│   ├── types.go                 # KVApp / Spec / Status / Phase / Condition
│   └── validation.go            # Validate 与缺省补齐
├── internal/
│   ├── informer/store.go        # 线程安全缓存与索引
│   ├── queue/ratelimit.go       # 限速队列与指数退避
│   ├── reconcile/decide.go      # 纯函数 Decide 与状态机
│   ├── reconcile/reconcile.go   # Reconcile 与 Finalizer 流程
│   ├── reconcile/cluster.go     # Cluster 接口与 client-go 实现
│   └── metrics/metrics.go       # 指标与结构化日志
├── config/crd/kvapp.yaml        # CRD 与 CEL 校验
├── config/rbac/role.yaml        # 最小权限 ClusterRole
└── config/prometheus/rules.yaml # 错误预算燃烧率告警
```

依赖倒置的关键在 `Cluster` 接口：`reconcile` 包定义它，`cluster` 包实现它，`controller` 只做编排。这样一来，测试 `Reconcile` 时注入一个几十行的 `fakeCluster` 就够，完全不需要 `envtest` 起 apiserver；反过来若把 `*kubernetes.Clientset` 直接塞进决策函数，纯函数就退化成了集成测试，覆盖率与迭代速度都会塌掉。

<Callout type="warn" title="边界划分最常见的错误" text="把 apiserver 客户端或缓存对象直接传进 Decide。表面看是「少传一个参数」，实际后果是：决策函数有了外部副作用，无法用表驱动单测覆盖；缓存对象被就地修改会污染后续所有调谐；一旦客户端返回瞬时错误，纯函数里就得处理重试，重试预算和业务逻辑彻底缠在一起。正确做法是 Decide 只吃两个值类型快照，任何 IO 都在它之外。" />

接口定义写成一段可独立编译的契约文件。注意 `Cluster` 的方法全部用**绝对值语义**：`ApplyReplicas(name, n)` 是「设为 n」而不是「加 n」，这是幂等的前提；`UpdateStatus` 必须携带 `resourceVersion` 做乐观锁，避免覆盖别人刚写入的条件。下面这段代码保存为 `api/contract.go` 可单独 `go build` 通过，用一个最小实现演示接口可被满足。

```go
// 文件：api/contract.go
package main

import "fmt"

// Phase 是 CR 对外暴露的生命周期相位，取值集合固定，便于 kubectl 展示与告警分组。
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

// Cluster 是控制器依赖的唯一外部边界：所有读写都收在这张窄接口里。
// 生产用 client-go 实现，单测用 fake 实现，Reconcile 本身不碰任何 SDK。
type Cluster interface {
	Get(name string) (Spec, Observed, error)
	ApplyReplicas(name string, n int) error
	ApplyVersion(name string, v string) error
	Cleanup(name string) error
	RemoveFinalizer(name string) error
	UpdateStatus(name string, p Phase, resourceVersion string) error
}

// Spec 是用户声明的期望，Observed 是控制器读到的实际，两者都是值类型快照。
type Spec struct {
	Replicas int
	Version  string
}

type Observed struct {
	ReadyReplicas int
	Version       string
	Exists        bool
}

// noopCluster 用于演示接口可被最小实现满足，编译期即校验方法签名齐全。
type noopCluster struct{}

func (noopCluster) Get(string) (Spec, Observed, error) {
	return Spec{}, Observed{Exists: true}, nil
}
func (noopCluster) ApplyReplicas(string, int) error          { return nil }
func (noopCluster) ApplyVersion(string, string) error        { return nil }
func (noopCluster) Cleanup(string) error                     { return nil }
func (noopCluster) RemoveFinalizer(string) error             { return nil }
func (noopCluster) UpdateStatus(string, Phase, string) error { return nil }

func main() {
	var c Cluster = noopCluster{}
	_, got, err := c.Get("kv-prod")
	fmt.Println("observed exists:", got.Exists, "err:", err)
}
```

<Term word="幂等" text="同一操作重复执行任意次，系统状态与只执行一次相同；控制器里所有动作必须是「设为期望值」而非「增减」" />

## 4. 数据与协议契约

CRD 是整套系统对外的唯一契约，必须先定死字段语义、零值与兼容规则。控制面之外，可观测平台把每次调谐审计落到一张关系表，用于事后追责「谁改了副本数」与燃烧率复盘：

```sql
CREATE TABLE reconcile_audit (
  id            BIGINT PRIMARY KEY,
  cr_name       VARCHAR(253) NOT NULL,
  generation    BIGINT NOT NULL,
  action        VARCHAR(32) NOT NULL,
  phase         VARCHAR(32) NOT NULL,
  duration_ms   INT NOT NULL,
  err_text      TEXT,
  trace_id      CHAR(32),
  occurred_at   TIMESTAMP NOT NULL,
  INDEX idx_cr_time (cr_name, occurred_at)
);
```

`generation` 由 apiserver 在 spec 变更时自增，`status.observedGeneration` 记录「本次状态对应的是哪一代 spec」，二者不等就说明控制器还没处理这次变更——这是判断「收敛了没有」最可靠的字段，比看 `phase` 字符串可靠得多。字段语义与零值规则列表如下：

| 字段 | 类型 | 语义 | 零值 / 缺省 | 校验规则 |
| --- | --- | --- | --- | --- |
| `spec.replicas` | integer | 期望就绪副本数 | 缺省 1 | 1 到 100，CEL 表达式约束上下界 |
| `spec.version` | string | 期望镜像语义化版本 | 必填 | 必须匹配 `^v[0-9]+\.[0-9]+\.[0-9]+$` |
| `spec.image` | string | 镜像地址 | 必填 | 非空字符串 |
| `status.phase` | enum | 生命周期相位 | 空（未调谐） | 只能取四个枚举值之一 |
| `status.observedGeneration` | integer | 已处理的 spec 代次 | 0 | 单调递增，不得回退 |
| `status.readyReplicas` | integer | 实际就绪副本 | 0 | 由控制器读取，用户不可写 |
| `status.conditions` | array | Ready / Progressing / Degraded 条件 | 空数组 | 同 type 只保留最新一条 |

完整 CRD 片段如下。要点有三：`scope: Namespaced` 让配额与 RBAC 能按命名空间隔离；`subresources.status` 把 spec 与 status 分成两个端点，用户改 spec 不会覆盖控制器写的 status；两个版本共存时只有一个 `storage: true`，升级靠 `v1beta1` 的转换策略补齐缺省字段。

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: kvapps.kv.example.com
spec:
  group: kv.example.com
  scope: Namespaced
  names:
    plural: kvapps
    singular: kvapp
    kind: KVApp
    shortNames: [kv]
  versions:
    - name: v1alpha1
      served: true
      storage: false
      schema: &schema
        openAPIV3Schema:
          type: object
          required: [spec]
          properties:
            spec:
              type: object
              required: [version, image]
              properties:
                replicas:
                  type: integer
                  default: 1
                  minimum: 1
                  maximum: 100
                version:
                  type: string
                  pattern: '^v[0-9]+\.[0-9]+\.[0-9]+$'
                image:
                  type: string
                  minLength: 1
            status:
              type: object
              properties:
                phase:
                  type: string
                  enum: [Pending, Running, Upgrading, Degraded]
                observedGeneration:
                  type: integer
                readyReplicas:
                  type: integer
                conditions:
                  type: array
                  items:
                    type: object
                    required: [type, status]
                    properties:
                      type: {type: string}
                      status: {type: string}
                      reason: {type: string}
                      message: {type: string}
          x-kubernetes-validations:
            - rule: "self.spec.replicas <= 100"
              message: "replicas 不能超过 100"
      subresources:
        status: {}
      additionalPrinterColumns:
        - name: Replicas
          type: integer
          jsonPath: .spec.replicas
        - name: Version
          type: string
          jsonPath: .spec.version
        - name: Phase
          type: string
          jsonPath: .status.phase
        - name: Age
          type: date
          jsonPath: .metadata.creationTimestamp
    - name: v1beta1
      served: true
      storage: true
      schema: *schema
      subresources:
        status: {}
```

对外 API 就是 `kubectl` 看到的对象，一份典型实例（含控制器回写的 status）如下。注意 `metadata.generation` 与 `status.observedGeneration` 相等才代表这一代已被处理：

```json
{
  "apiVersion": "kv.example.com/v1beta1",
  "kind": "KVApp",
  "metadata": {"name": "kv-prod", "namespace": "prod", "generation": 7, "resourceVersion": "289341"},
  "spec": {"replicas": 3, "version": "v1.4.0", "image": "registry.internal/kv:v1.4.0"},
  "status": {
    "phase": "Running",
    "observedGeneration": 7,
    "readyReplicas": 3,
    "conditions": [
      {"type": "Ready", "status": "True", "reason": "Converged", "message": "replicas and version match spec"}
    ]
  }
}
```

版本号与灰度策略：新增字段一律可选、带缺省，保证旧对象反序列化不报错；破坏性变更（改字段类型、改必填）必须走新版本加转换，禁止就地改 `storage` 版本的 schema；`v1alpha1` 先 `served: true, storage: false` 灰度，观察两个发布周期无告警后再切 storage，最后才下线老版本。

<Compare title="status 契约的两种设计" :panels='[{"label":"单一 phase 枚举","code":"status.phase = Running | Upgrading | Degraded","note":"实现简单，kubectl 一列能看懂；但无法同时表达「正在升级」且「仍可用」，多维度状态会丢失"},{"label":"Conditions 数组","code":"status.conditions = [Ready, Progressing, Degraded]","note":"每个条件带 reason 与 message，可同时表达多维度、可诊断；代价是写入要按 type 去重，客户端解析更啰嗦"}]' />

生产里两者并不互斥：`phase` 留作给人看的主状态，Conditions 留给机器与告警。本讲代码用 `SetCondition` 按 type 覆盖，正是为了让「同一个条件重复写」保持幂等。

## 5. 核心链路时序

正常链路有六个参与方、十二条消息，覆盖同步调用、虚线返回、自处理与异步消息四类。关键观察：**apiserver 从不主动把状态推给 Reconciler**，它只把事件推给 Informer，Informer 只把 key 放进队列，Reconciler 需要状态时会重新 `Get` 一次最新快照——这正是 level-triggered 的形态。

<SequenceDiagram title="一次 KVApp 调谐的完整链路" caption="事件搬运 key，状态永远重新读，决策集中在 Reconciler 内部" :actors='["User","API Server","Informer","WorkQueue","Reconciler","Metrics"]' :steps='[{"from":"User","to":"API Server","label":"apply KVApp","note":"期望 3 副本 v2","kind":"call"},{"from":"API Server","to":"Informer","label":"watch 事件","note":"ADDED 或 MODIFIED","kind":"async"},{"from":"Informer","to":"Informer","label":"更新本地缓存","note":"RV 单调才接受","kind":"self"},{"from":"Informer","to":"WorkQueue","label":"入队 key","note":"按 namespace/name 去重","kind":"async"},{"from":"WorkQueue","to":"Reconciler","label":"派发 key","note":"worker 串行领取","kind":"return"},{"from":"Reconciler","to":"API Server","label":"Get 期望与实际","note":"带 resourceVersion","kind":"call"},{"from":"API Server","to":"Reconciler","label":"返回快照","note":"Spec 与 Observed","kind":"return"},{"from":"Reconciler","to":"Reconciler","label":"Decide 算动作","note":"版本优先于副本","kind":"self"},{"from":"Reconciler","to":"API Server","label":"执行动作并回写","note":"一次只推进一个","kind":"call"},{"from":"API Server","to":"Reconciler","label":"写入成功","note":"冲突则退避重入","kind":"return"},{"from":"Reconciler","to":"Metrics","label":"埋点与日志","note":"耗时与失败原因","kind":"async"},{"from":"Reconciler","to":"WorkQueue","label":"Forget 或退避","note":"成功 Forget 失败重入","kind":"call"}]' />

逐步骤解读。第 1 到 4 步是「信号」，只负责告诉系统「有个对象可能变了」，刻意不携带对象内容，因为事件到达时对象可能又被改了三次，用事件里的旧快照做决策必然出错。第 5 步 workqueue 的返回语义是「领取」：同一 key 在 processing 集合里时不会被第二个 worker 领走，保证单对象串行。第 6 到 8 步是核心：重新读最新状态、纯函数算出唯一动作，版本不一致优先于副本差异，避免一边升级一边扩容让「实际状态」同时在两个维度变化、失败时无法判断该补偿谁。第 9 步只做一个动作并回写 status，第 10 步若返回 409 冲突不覆盖、直接上抛。第 11、12 步是收尾：无论成功失败都埋点，成功则 `Forget` 清除退避计数，失败则按指数退避重新入队。

异常链路在**第 10 步**分叉。设 `UpdateStatus` 因另一个控制器刚写了 conditions 而返回 409：正常链路在收到「写入成功」后走向 `Forget`；异常链路收到的是错误，Reconciler 不做任何补偿、不删缓存、不重试，直接把错误上抛给队列，队列按该 key 的失败次数算退避（第一次 5 ms，之后翻倍封顶 1000 ms），到点后重新入队。重入队后不会复用上一轮的 `Observed`，而是从**第 6 步重新 Get**——这是 level-triggered 与「就地重试」最本质的区别：重试的是整轮对账，不是重放某个动作。若 apiserver 持续不可用超过重试预算，key 进死信并告警，此时即便某个 watch 事件恰好丢失，5 分钟一次的全量 resync 也会把它重新捞回来。

<ConceptDiagram type="circuit-breaker" title="限速队列的三态：正常领取、失败退避、超预算熔断进死信" caption="退避不是「睡一会儿再试」，而是把 key 从 processing 移回 dirty 并附上到期时间" />

## 6. 核心实现（按模块逐段展开）

以下六段 Go 代码同属一个逻辑包，为便于逐段单独 `go build` 验证，每段自带最小类型集合；真实工程按第 3 节目录树把公共类型抽到 `api` 包共享。每段都做 gofmt 自检、import 只留真正用到的包，且都带一个 `main` 演示核心路径。

### 6.1 数据模型与 schema 校验

**设计意图**：把「校验」前移到对象进入系统之前，`Validate` 是纯函数，和 CRD 的 CEL 规则同义，互为双保险。**关键取舍**：正则用 `regexp.MustCompile` 在包初始化时编译一次，避免每次校验重新编译；`SetCondition` 按 type 覆盖而非追加，保证重复调谐 conditions 不膨胀。

```go
// 文件：api/types.go
package main

import (
	"errors"
	"fmt"
	"regexp"
)

// Phase 是 CR 的生命周期相位，取值集合与 CRD 的 enum 一一对应。
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

// Condition 是一条状态条件，同 type 只保留最新一条，便于机器判定。
type Condition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// Spec 是用户声明的期望，Status 是控制器回写的实际，二者严格分离。
type Spec struct {
	Replicas int
	Version  string
	Image    string
}

type Status struct {
	Phase              Phase
	ObservedGeneration int64
	ReadyReplicas      int
	Conditions         []Condition
}

// KVApp 是完整的自定义资源；DeletionTimestamp 非空表示进入删除流程。
type KVApp struct {
	Name              string
	Generation        int64
	ResourceVersion   string
	DeletionTimestamp bool
	Finalizers        []string
	Spec              Spec
	Status            Status
}

var semver = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Validate 在对象落库前挡住非法期望，纯函数、无 IO、可表驱动测试。
func Validate(s Spec) error {
	if s.Replicas < 1 || s.Replicas > 100 {
		return errors.New("replicas out of range [1,100]")
	}
	if !semver.MatchString(s.Version) {
		return errors.New("version must match vMAJOR.MINOR.PATCH")
	}
	if s.Image == "" {
		return errors.New("image is required")
	}
	return nil
}

// SetCondition 按 type 覆盖写入，重复调用不产生重复条件，天然幂等。
func SetCondition(st *Status, c Condition) {
	for i := range st.Conditions {
		if st.Conditions[i].Type == c.Type {
			st.Conditions[i] = c
			return
		}
	}
	st.Conditions = append(st.Conditions, c)
}

func main() {
	good := Spec{Replicas: 3, Version: "v1.4.0", Image: "kv:v1.4.0"}
	bad := Spec{Replicas: 0, Version: "latest", Image: ""}
	fmt.Println("good err is nil:", Validate(good) == nil)
	fmt.Println("bad:", Validate(bad))
	st := &Status{}
	SetCondition(st, Condition{Type: "Ready", Status: "True", Reason: "Converged"})
	SetCondition(st, Condition{Type: "Ready", Status: "False", Reason: "Progressing"})
	fmt.Println("conditions:", len(st.Conditions), st.Conditions[0].Reason)
}
```

<OutputBox title="运行结果" output="good err is nil: true\nbad: replicas out of range [1,100]\nconditions: 1 Progressing" note="第二次 SetCondition 覆盖了同 type 条件，数组长度仍为 1，证明回写幂等" />

### 6.2 状态机、幂等 Reconcile 与 Finalizer

**设计意图**：这是全讲的核心。`Decide` 是没有副作用、没有隐藏状态的纯函数，输入两个值类型快照，输出一个动作和一个相位；`Reconcile` 完成「读、决、做、写状态」一次调谐。**关键取舍**：删除优先于一切业务动作；版本不一致优先于副本差异；一次只推进一个动作；所有 Apply 用绝对值；清理成功才移除 Finalizer；状态回写带 `resourceVersion` 乐观锁，409 冲突上抛不覆盖。

```go
// 文件：internal/reconcile/reconcile.go
package main

import (
	"errors"
	"fmt"
)

type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

type Action string

const (
	ActionNone    Action = "none"
	ActionScale   Action = "scale"
	ActionRollout Action = "rollout"
	ActionCleanup Action = "cleanup"
	ActionDrop    Action = "drop"
)

const FinalizerKey = "kv.example.com/cleanup"

// KVApp 是期望，Observed 是实际；Deleted 由 apiserver 的删除时间戳派生。
type KVApp struct {
	Replicas   int
	Version    string
	Deleted    bool
	Finalizers []string
}

type Observed struct {
	ReadyReplicas int
	Version       string
	Exists        bool
}

// Decide 是纯函数状态机：给定期望与实际，返回唯一动作与目标相位。
// 顺序即优先级：删除 > 版本 > 副本 > 收敛。一次只推进一个维度。
func Decide(want KVApp, got Observed) (Action, Phase) {
	if want.Deleted {
		if has(want.Finalizers, FinalizerKey) {
			return ActionCleanup, PhaseDegraded
		}
		return ActionDrop, PhaseDegraded
	}
	if got.Version != want.Version {
		return ActionRollout, PhaseUpgrading
	}
	switch {
	case got.ReadyReplicas < want.Replicas:
		return ActionScale, PhasePending
	case got.ReadyReplicas > want.Replicas:
		return ActionScale, PhasePending
	case got.ReadyReplicas == want.Replicas && got.ReadyReplicas > 0:
		return ActionNone, PhaseRunning
	default:
		return ActionNone, PhaseDegraded
	}
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Cluster 是唯一外部边界，全部方法用绝对值语义，保证幂等。
type Cluster interface {
	Get(name string) (KVApp, Observed, error)
	ApplyReplicas(name string, n int) error
	ApplyVersion(name string, v string) error
	Cleanup(name string) error
	RemoveFinalizer(name string) error
	UpdateStatus(name string, p Phase, rv string) error
	ResourceVersion(name string) string
}

var ErrConflict = errors.New("status conflict")

// Reconcile 完成一次调谐，必须可重入：重复调用收敛到同一状态。
func Reconcile(c Cluster, name string) error {
	want, got, err := c.Get(name)
	if err != nil {
		return err
	}
	if !got.Exists {
		return nil // 对象已真正删除，无需动作
	}
	action, phase := Decide(want, got)
	switch action {
	case ActionScale:
		err = c.ApplyReplicas(name, want.Replicas)
	case ActionRollout:
		err = c.ApplyVersion(name, want.Version)
	case ActionCleanup:
		if err = c.Cleanup(name); err != nil {
			return err // 清理失败，保留 Finalizer，下一轮重试
		}
		return c.RemoveFinalizer(name) // 先清理成功，再移除，顺序不可颠倒
	case ActionDrop:
		return nil
	}
	if err != nil {
		return err
	}
	if action == ActionNone {
		return nil
	}
	if err = c.UpdateStatus(name, phase, c.ResourceVersion(name)); err != nil {
		if errors.Is(err, ErrConflict) {
			return err // 带退避重入，绝不覆盖他人写入
		}
		return err
	}
	return nil
}

// fakeCluster 是单测用的最小实现，记录动作而不碰任何网络。
type fakeCluster struct {
	want     KVApp
	got      Observed
	replicas int
	version  string
	cleaned  bool
	finLeft  bool
	status   Phase
}

func (f *fakeCluster) Get(string) (KVApp, Observed, error)            { return f.want, f.got, nil }
func (f *fakeCluster) ApplyReplicas(_ string, n int) error            { f.replicas = n; return nil }
func (f *fakeCluster) ApplyVersion(_ string, v string) error          { f.version = v; return nil }
func (f *fakeCluster) Cleanup(string) error                           { f.cleaned = true; return nil }
func (f *fakeCluster) RemoveFinalizer(string) error                   { f.finLeft = false; return nil }
func (f *fakeCluster) UpdateStatus(_ string, p Phase, _ string) error { f.status = p; return nil }
func (f *fakeCluster) ResourceVersion(string) string                  { return "rv-1" }

func main() {
	// 场景一：版本不一致优先升级，一次只做一个动作。
	c := &fakeCluster{
		want: KVApp{Replicas: 3, Version: "v2.0.0"},
		got:  Observed{ReadyReplicas: 1, Version: "v1.4.0", Exists: true},
	}
	_ = Reconcile(c, "kv")
	fmt.Println("rollout:", c.version, string(c.status), "replicas unchanged:", c.replicas == 0)

	// 场景二：删除走 Finalizer，先清理后移除。
	d := &fakeCluster{
		want:    KVApp{Deleted: true, Finalizers: []string{FinalizerKey}, Replicas: 3, Version: "v2.0.0"},
		got:     Observed{ReadyReplicas: 3, Version: "v2.0.0", Exists: true},
		finLeft: true,
	}
	_ = Reconcile(d, "kv")
	fmt.Println("cleanup:", d.cleaned, "finalizer left:", d.finLeft)

	// 场景三：连续调谐三次，验证幂等收敛。
	e := &fakeCluster{
		want: KVApp{Replicas: 3, Version: "v2.0.0"},
		got:  Observed{ReadyReplicas: 3, Version: "v2.0.0", Exists: true},
	}
	for i := 0; i < 3; i++ {
		_ = Reconcile(e, "kv")
	}
	fmt.Println("idempotent status:", string(e.status))
}
```

<LineByLine code="if want.Deleted {\nreturn ActionCleanup, PhaseDegraded\n}\nif got.Version != want.Version {\nreturn ActionRollout, PhaseUpgrading\n}" :notes='["删除优先于一切业务动作，是状态机的第一分支","带 Finalizer 时返回清理动作与降级相位","删除分支闭合，不再落入后续业务判断","版本不一致优先于副本差异，避免边升级边扩容","返回升级动作与升级中相位","版本分支闭合，剩余情况才比较副本数"]' />

<Callout type="warn" title="Finalizer 的顺序不能反" text="先移除 Finalizer 再清理，一旦清理失败或进程崩溃，对象已被 apiserver 真正删除，外部 StatefulSet 与 PVC 永远没人回收，这就是生产里最典型的资源泄漏。正确顺序是幂等 Cleanup 成功后才 RemoveFinalizer；即便崩溃重入，因为 Cleanup 幂等，再执行一次也安全。" />

### 6.3 Informer 缓存：线程安全与乱序防护

**设计意图**：watch 事件高频且可能乱序，缓存是 apiserver 的只读投影，必须线程安全、必须拒绝比当前更旧的事件、绝不能让调用方拿到可写的内部对象。**关键取舍**：用 `sync.RWMutex` 而非 `sync.Map`，因为需要按 namespace 的二级索引与一次性列举；写入时 `DeepCopy`、读出时也 `DeepCopy`，把「就地修改污染缓存」这个坑从根上堵死；`Update` 用 `resourceVersion` 字符串比较判断乱序。

```go
// 文件：internal/informer/store.go
package main

import (
	"fmt"
	"sync"
)

// Object 是缓存里的对象投影，RV 是 apiserver 的 resourceVersion。
type Object struct {
	Key             string
	Namespace       string
	ResourceVersion string
	Replicas        int
}

func (o Object) DeepCopy() Object { return o }

// Store 是线程安全的本地缓存：主键索引加命名空间二级索引。
type Store struct {
	mu   sync.RWMutex
	byK  map[string]Object
	byNS map[string]map[string]struct{}
}

func NewStore() *Store {
	return &Store{
		byK:  make(map[string]Object),
		byNS: make(map[string]map[string]struct{}),
	}
}

// Add 覆盖写入并维护索引；写前 DeepCopy，避免外部持有同一底层数据。
func (s *Store) Add(o Object) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byK[o.Key] = o.DeepCopy()
	set := s.byNS[o.Namespace]
	if set == nil {
		set = make(map[string]struct{})
		s.byNS[o.Namespace] = set
	}
	set[o.Key] = struct{}{}
}

// Update 拒绝乱序事件：RV 不大于当前值就丢弃，返回是否接受。
func (s *Store) Update(o Object) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.byK[o.Key]; ok && cur.ResourceVersion >= o.ResourceVersion {
		return false
	}
	s.byK[o.Key] = o.DeepCopy()
	return true
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.byK[key]; ok {
		delete(s.byK, key)
		if set := s.byNS[o.Namespace]; set != nil {
			delete(set, key)
		}
	}
}

// Get 返回副本，调用方修改不影响缓存，是 DeepCopy 纪律的关键落点。
func (s *Store) Get(key string) (Object, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.byK[key]
	return o.DeepCopy(), ok
}

func (s *Store) ListNS(ns string) []Object {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Object, 0)
	for k := range s.byNS[ns] {
		if o, ok := s.byK[k]; ok {
			out = append(out, o.DeepCopy())
		}
	}
	return out
}

func main() {
	s := NewStore()
	s.Add(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "100", Replicas: 3})
	// 迟到的旧事件必须被丢弃
	fmt.Println("stale accepted:", s.Update(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "99", Replicas: 1}))
	fmt.Println("fresh accepted:", s.Update(Object{Key: "prod/kv", Namespace: "prod", ResourceVersion: "101", Replicas: 5}))
	o, _ := s.Get("prod/kv")
	o.Replicas = 999 // 修改副本，不影响缓存
	o2, _ := s.Get("prod/kv")
	fmt.Println("cache replicas:", o2.Replicas, "ns objects:", len(s.ListNS("prod")))
}
```

<OutputBox title="运行结果" output="stale accepted: false\nfresh accepted: true\ncache replicas: 5 ns objects: 1" note="旧 RV 事件被丢弃，且调用方对返回值的修改没有污染缓存" />

### 6.4 限速队列、指数退避与主循环

**设计意图**：失败绝不能就地死循环重试，否则 apiserver 一抖就被高频重试打爆。队列用「dirty 加 processing」两个集合实现按 key 去重与单对象串行，失败按指数退避把 key 移回 dirty 并附到期时间，超过八次进死信。**关键取舍**：用 `sync.Cond` 让 worker 在无活可干时阻塞而非忙等；每个退避项用 `time.AfterFunc` 到期唤醒，避免「所有项都在退避期、没人唤醒」的死锁；`Run` 里叠加一个 5 分钟的 resync ticker 做全量对账兜底。

```go
// 文件：internal/queue/ratelimit.go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type item struct {
	retries int
	at      time.Time
}

// Queue 用两个集合保证同一 key 不会被并发处理，dirty 是待办、processing 是在途。
type Queue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	dirty      map[string]*item
	processing map[string]bool
	dead       []string
	closed     bool
	maxRetries int
}

func NewQueue(maxRetries int) *Queue {
	q := &Queue{
		dirty:      make(map[string]*item),
		processing: make(map[string]bool),
		maxRetries: maxRetries,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func backoff(n int) time.Duration {
	d := time.Duration(5) * time.Millisecond
	for i := 0; i < n; i++ {
		d *= 2
		if d > time.Second {
			return time.Second
		}
	}
	return d
}

// Add 幂等入队：同一 key 只在 dirty 里记一次，重复事件不会放大重试。
func (q *Queue) Add(key string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.dirty[key]; !ok {
		q.dirty[key] = &item{}
	}
	q.cond.Signal()
}

// addAfter 把失败项按退避时间放回 dirty，并到点唤醒等待的 worker。
func (q *Queue) addAfter(key string, retries int, d time.Duration) {
	q.mu.Lock()
	q.dirty[key] = &item{retries: retries, at: time.Now().Add(d)}
	q.mu.Unlock()
	time.AfterFunc(d, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})
}

// Get 阻塞取一个到期的可执行 key，未到退避时间的项跳过。
func (q *Queue) Get() (string, int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for {
		for k, it := range q.dirty {
			if q.processing[k] || time.Now().Before(it.at) {
				continue
			}
			delete(q.dirty, k)
			q.processing[k] = true
			return k, it.retries, true
		}
		if q.closed {
			return "", 0, false
		}
		q.cond.Wait()
	}
}

// Done 处理成功则清除计数，失败按重试预算退避或进死信。
func (q *Queue) Done(key string, err error, retries int) {
	q.mu.Lock()
	delete(q.processing, key)
	q.mu.Unlock()
	if err == nil {
		return
	}
	if retries+1 >= q.maxRetries {
		q.mu.Lock()
		q.dead = append(q.dead, key)
		q.mu.Unlock()
		fmt.Println("dead letter:", key)
		return
	}
	q.addAfter(key, retries+1, backoff(retries))
}

func (q *Queue) Dead() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.dead)
}

func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

type ReconcileFn func(context.Context, string) error

// Run 启动 workers 个串行消费者，并周期性全量 resync 兜底事件丢失。
func Run(ctx context.Context, q *Queue, workers int, fn ReconcileFn, resync time.Duration, listKeys func() []string) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				key, retries, ok := q.Get()
				if !ok {
					return
				}
				q.Done(key, fn(ctx, key), retries)
			}
		}()
	}
	t := time.NewTicker(resync)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			q.Close()
			return
		case <-t.C:
			for _, k := range listKeys() {
				q.Add(k)
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	q := NewQueue(8)
	q.Add("prod/kv")
	var mu sync.Mutex
	failLeft := 2
	fn := func(_ context.Context, key string) error {
		mu.Lock()
		defer mu.Unlock()
		if failLeft > 0 {
			failLeft--
			return fmt.Errorf("apiserver timeout for %s", key)
		}
		fmt.Println("converged:", key)
		return nil
	}
	Run(ctx, q, 2, fn, 50*time.Millisecond, func() []string { return []string{"prod/kv"} })
	time.Sleep(10 * time.Millisecond)
	fmt.Println("dead letters:", q.Dead())
}
```

### 6.5 指标、结构化日志与错误预算

**设计意图**：控制器自己健不健康也必须可观测，否则「它悄悄不干活了」比「它报错」更可怕。**关键取舍**：用 `sync/atomic` 做无锁计数，避免指标成为调谐热点上的锁竞争；`log/slog`（Go 1.21 起进标准库）输出结构化字段，带 `cr`、`generation`、`action`、`duration_ms`、`err`，可按 CR 聚合；告警盯**燃烧率**而非绝对失败次数，因为低流量时几次失败不该叫醒人，高流量时同样几次也不代表健康。

```go
// 文件：internal/metrics/metrics.go
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 对应 Prometheus 的 Counter 与 Histogram，全部用原子或短锁保护。
type Metrics struct {
	Reconciles atomic.Int64
	Failures   atomic.Int64
	DurationUS atomic.Int64
	QueueDepth atomic.Int64
	mu         sync.Mutex
	failReason map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{failReason: make(map[string]int64)}
}

func (m *Metrics) incFailure(reason string) {
	m.Failures.Add(1)
	m.mu.Lock()
	m.failReason[reason]++
	m.mu.Unlock()
}

func (m *Metrics) FailByReason(reason string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failReason[reason]
}

// Observe 在每次调谐收尾统一埋点：计数、耗时、结构化日志三件事一次做完。
func Observe(m *Metrics, logger *slog.Logger, cr string, gen int64, action string, start time.Time, err error) {
	m.Reconciles.Add(1)
	m.DurationUS.Add(time.Since(start).Microseconds())
	if err != nil {
		m.incFailure(action)
		logger.Error("reconcile_failed",
			"cr", cr, "generation", gen, "action", action,
			"duration_ms", time.Since(start).Milliseconds(), "err", err)
		return
	}
	logger.Info("reconcile_ok",
		"cr", cr, "generation", gen, "action", action,
		"duration_ms", time.Since(start).Milliseconds())
}

// ErrorBudgetBurn 返回失败占比；总量为零时定义为 0，避免除零与误告警。
func ErrorBudgetBurn(failures, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return failures / total
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := NewMetrics()
	start := time.Now()
	Observe(m, logger, "kv-prod", 7, "rollout", start, nil)
	Observe(m, logger, "kv-prod", 7, "status", start, errors.New("apiserver 409 conflict"))
	fmt.Println("reconciles:", m.Reconciles.Load())
	fmt.Println("status failures:", m.FailByReason("status"))
	fmt.Printf("burn rate: %.3f\n", ErrorBudgetBurn(float64(m.Failures.Load()), float64(m.Reconciles.Load())))
}
```

<Callout type="tip" title="燃烧率为什么比失败次数好用" text="凌晨三点集群只有两次调谐，失败一次就是 50% 燃烧率，但影响面可能只有一个不重要的测试 CR；白天一万次调谐失败五次，燃烧率只有 0.05%，绝对次数却是五。用比率加一个最短持续窗口（for 10m）再加最小样本量门槛，才能既不错过持续恶化，也不被零星抖动半夜叫醒。" />

### 6.6 升级兼容与最小权限校验

**设计意图**：CRD 一旦对外就很难改，版本转换与权限收敛必须在设计期定死。**关键取舍**：`Convert` 把老版本的指针字段补齐为缺省值，保证「旧对象读出来绝不 panic」；`CheckLeastPrivilege` 在 CI 里静态校验 ClusterRole，禁止出现通配符、删除权限或对 Secret 的读取，把「控制器权限过大」这个安全隐患挡在发布之前。

```go
// 文件：internal/reconcile/compat.go
package main

import (
	"fmt"
	"sort"
	"strings"
)

// SpecV1Alpha1 是老版本：replicas 是指针（可缺省），没有 strategy 字段。
type SpecV1Alpha1 struct {
	Replicas *int
	Version  string
	Image    string
}

// SpecV1Beta1 是新版本：replicas 变成必填值，新增 strategy 字段。
type SpecV1Beta1 struct {
	Replicas int
	Version  string
	Image    string
	Strategy string
}

// Convert 把老对象升级为新对象，缺省字段在此补齐，保证反序列化安全。
func Convert(in SpecV1Alpha1) (SpecV1Beta1, error) {
	out := SpecV1Beta1{
		Version:  in.Version,
		Image:    in.Image,
		Strategy: "RollingUpdate",
	}
	if in.Replicas == nil {
		out.Replicas = 1 // 老对象没写副本数，按缺省 1 处理
	} else {
		out.Replicas = *in.Replicas
	}
	if out.Replicas < 1 {
		return SpecV1Beta1{}, fmt.Errorf("invalid replicas: %d", out.Replicas)
	}
	if !strings.HasPrefix(out.Version, "v") {
		return SpecV1Beta1{}, fmt.Errorf("invalid version: %s", out.Version)
	}
	return out, nil
}

// Rule 是 RBAC 的一条授权，LeastPrivilege 校验只放行必要动作。
type Rule struct {
	Verbs    []string
	Resource string
}

func CheckLeastPrivilege(rules []Rule) []string {
	var bad []string
	for _, r := range rules {
		for _, v := range r.Verbs {
			if v == "*" || v == "delete" || v == "deletecollection" {
				bad = append(bad, r.Resource+" verb="+v)
			}
		}
		if strings.Contains(r.Resource, "secrets") {
			bad = append(bad, r.Resource+" is sensitive")
		}
	}
	sort.Strings(bad)
	return bad
}

func main() {
	old := SpecV1Alpha1{Version: "v1.4.0", Image: "kv:v1.4.0"} // 没写 replicas
	converted, err := Convert(old)
	fmt.Println("converted:", converted.Replicas, converted.Strategy, "err:", err)
	rules := []Rule{
		{Verbs: []string{"get", "list", "watch", "patch", "update"}, Resource: "kvapps"},
		{Verbs: []string{"delete"}, Resource: "deployments"}, // 违规：不该给删除权
		{Verbs: []string{"get"}, Resource: "secrets"},        // 违规：敏感资源
	}
	fmt.Println("violations:", CheckLeastPrivilege(rules))
}
```

## 7. 失败路径与边界条件

失败矩阵按「故障点、触发条件、现象、如何发现、兜底、恢复动作」六列写全，覆盖下游超时、依赖不可用、重复请求、数据损坏、进程崩溃、配置错误六类必测项，并补上 Finalizer、状态冲突、事件丢失三个 Operator 特有故障。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| apiserver 超时 | 网络抖动或 etcd 慢 | `Get` 或 `UpdateStatus` 返回错误 | 失败率指标突增、slog 出现 timeout | 直接上抛，队列退避重入 | 退避 5 ms 起翻倍，最多 8 次后进死信告警 |
| apiserver 不可用 | 控制面整体故障 | 全量调谐失败、队列积压 | 队列深度 Gauge 持续上涨 | 停止放大重试，保活缓存只读 | 恢复后 resync 全量对账，不丢状态 |
| 重复请求 / 事件重复 | 同一 CR 连续多次变更 | 同一 key 被多次入队 | 调谐次数远大于变更次数 | workqueue 按 key 去重，动作绝对值语义 | 无需人工，自然收敛 |
| 数据损坏 | CR 里 version 写成 latest | 反序列化或校验失败 | schema 拒绝、Rejected 事件 | 对象不进调谐队列 | 用户修正 YAML；老对象靠 Convert 补齐 |
| 进程崩溃 | 升级做到一半被 OOMKill | 部分副本旧版本、部分新版本 | 新 leader 发现 observedGeneration 落后 | 无本地状态，重启后重新 watch | Decide 看到版本不一致，继续 rollout |
| 配置错误 | resync 周期误配为 0 | 定时器 panic 或 CPU 打满 | 启动校验失败、就绪探针不通过 | 启动期 ValidateConfig 直接拒绝启动 | 回滚配置，滚动重启 |
| Finalizer 卡住 | 清理外部资源一直失败 | 删除操作永久挂起 | CR 长时间带 deletionTimestamp | 清理幂等且带超时，失败保留 Finalizer | 人工排查外部依赖后重试，或强制移除并登记 |
| status 冲突 | 两个写入方并发更新 | `UpdateStatus` 返回 409 | 冲突失败计数、slog 记录 reason | 带 resourceVersion 重试，不覆盖 | 重入整轮对账，读最新快照再写 |
| 事件丢失 | watch 断连期间漏事件 | 状态漂移无人纠正 | resync 前后状态差异统计 | 5 分钟全量 List 兜底 | 自动重新入队并收敛 |

幂等键由「namespace/name 加 resourceVersion」共同构成：`name` 保证同一对象串行，`resourceVersion` 保证不覆盖他人写入。重试预算固定八次、退避 5 ms 起指数增长封顶 1000 ms，单对象最坏占用约两秒队列时间，绝不会出现「一个坏对象拖死整个控制器」。超时沿调用链传递：controller 的 `ctx` 一路传给 `Cluster` 的每个 API 调用，客户端取消或优雅退出时所有在途请求一并结束。降级点是「指标端点不可用时只记日志」，人工介入点有两个——死信队列需要 On-call 判断是配置错误还是控制面故障，Finalizer 长时间挂起需要人工确认外部资源可否强制清理。

<StepBox title="一次 UpdateStatus 冲突的完整处理链" :steps='[{"title":"写入失败","body":"UpdateStatus 返回 409，携带的 resourceVersion 已过期","tip":"不要 catch 后覆盖写，那会抹掉别人刚写的 conditions"},{"title":"上抛错误","body":"Reconcile 直接返回错误，不重试、不删缓存","tip":"重试是队列的职责，不是 Reconcile 的职责"},{"title":"退避重入","body":"队列按该 key 失败次数算 5 ms 退避并放回 dirty","tip":"重入的是整轮对账，不是重放某个动作"},{"title":"重新读快照","body":"再次 Get 拿到最新 resourceVersion 与 conditions","tip":"这一步是 level-triggered 的精髓：状态永远重新读"},{"title":"收敛","body":"Decide 重新算动作，UpdateStatus 用新 RV 成功，Forget 清计数","tip":"成功 Forget 后，下一次失败从 5 ms 重新起算"}]' startLabel="开始演示" />

## 8. 并发与一致性

并发模型是「一个 leader 加多个 worker」：多副本控制器靠 leader election 保证任意时刻只有一个实例消费队列，实例内部开 4 个 worker 处理不同 key，但**同一 key 永远串行**（processing 集合保证）。这样既拿到并行吞吐，又避免同一对象被两个 goroutine 同时扩缩容。channel 与锁的分工很清楚：队列用 `sync.Cond` 加互斥锁保护两个集合，因为需要条件等待；缓存用 `sync.RWMutex`，读多写少；纯计数指标用 `sync/atomic`，完全无锁。

锁粒度与热点：缓存若用一把全局锁，2000 个 CR 的全量 resync 会让所有 worker 的读都排队。按 namespace 分片是自然的优化方向，但会引入跨片列举的复杂度。本讲规模下 `RWMutex` 的单锁读并发已经够用（读不互斥），第 12 节压测会给出证据，说明何时才值得分片。竞态分析聚焦两处：一是 `Decide` 读到的 `Observed` 与随后 `Apply` 之间状态被别人改了，靠 `resourceVersion` 乐观锁闭环；二是缓存对象被就地修改，靠 `DeepCopy` 纪律闭环。

一致性级别选**最终一致加单调读**：控制器不提供「读己之写」，用户提交 spec 后立刻 `kubectl get` 可能看到旧 status，这是刻意接受的代价——换取的是控制器无状态、可随时重启、可扩展到多副本。顺序保证靠 `generation` 与 `observedGeneration`：`generation` 由 apiserver 单调递增，控制器只认更大的值，乱序到达的旧事件在缓存层就被 `resourceVersion` 丢弃，不会让状态回退。

<CodeTabs title="缓存锁的两种粒度" :tabs='[{"label":"全局 RWMutex","code":"s.mu.RLock()\no := s.byK[key]\ns.mu.RUnlock()","note":"实现最简单，读并发不互斥；2000 CR 全量 resync 时写锁会短暂阻塞所有读，但读本身不互斥，压力可控"},{"label":"按 namespace 分片","code":"shard := s.shards[hash(ns)]\nshard.mu.RLock()\no := shard.byK[key]","note":"写锁只影响单个分片，适合万级 CR 或热命名空间；代价是跨片 List 要合并、代码复杂度上升，本讲规模不划算"}]' />

<StepLab scene="channel-flow" />

`StepLab` 里可以逐语句观察 worker 从队列领取 key、进入 processing、Done 后释放的整个过程，配合第 6.4 节的 `Get` 与 `Done` 对照看，能直观理解「为什么同一 key 不会被并发处理」。

## 9. 可观测性

指标表按 Prometheus 的命名规范给出，全部带「告警阈值」一列，让 SRE 拿到表就能配规则。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `reconciles_total` | Counter | 累计调谐次数，按 action 与 result 打标签 | 用于算燃烧率，不单独告警 |
| `reconcile_failures_total` | Counter | 累计失败次数，按 reason 分组 | 配合燃烧率 |
| `reconcile_duration_seconds` | Histogram | 单次调谐耗时分布 | P99 大于 0.2 s 持续 10 分钟 |
| `workqueue_depth` | Gauge | 当前待处理 key 数 | 大于 500 且持续 5 分钟 |
| `reconcile_error_budget_burn` | Gauge | 5 分钟窗口失败占比 | 大于 0.05 持续 10 分钟触发 Page |

结构化日志字段固定为 `cr`、`generation`、`action`、`duration_ms`、`err`、`trace_id`。`trace_id` 的传播方式：从 CR 的 annotation 里读取上游注入的追踪标识，若没有则用「CR 的 UID 加 generation」本地生成一个，并在同一次调谐的所有日志与 `reconcile_audit` 记录里透传；这样一次告警能在 apiserver audit、控制器日志、审计表三处用同一个 id 串起来。`generation` 字段尤其重要，它能区分「老一代的失败」和「新一代还没被处理」。

埋点代码已在第 6.5 节给出，落到控制器里就是每个 key 调谐收尾包一层 `Observe`。对应的 Prometheus 规则关注比率而非绝对次数：

```yaml
groups:
  - name: kvapp.rules
    rules:
      - record: reconcile_error_budget_burn
        expr: sum(rate(reconcile_failures_total[5m])) / clamp_min(sum(rate(reconciles_total[5m])), 1)
      - alert: KVAppErrorBudgetBurn
        expr: reconcile_error_budget_burn > 0.05 and sum(rate(reconciles_total[5m])) > 0.2
        for: 10m
        annotations:
          summary: 调谐失败占比持续超阈值，需检查 apiserver 与控制器
```

用这套可观测定位一次真实故障的完整路径：夜里 `KVAppErrorBudgetBurn` 告警触发 → 看 `reconcile_failures_total` 按 `reason` 分组，发现全部集中在 `status` 动作 → 用 `trace_id` 过滤 slog 日志，看到大量 `apiserver 409 conflict` → 再查审计表发现同一 `cr_name` 的 `generation` 在短时间内跳了十几次 → 结论是某个外部脚本在高频改 spec，控制器状态回写一直冲突。兜底动作是先停掉那个脚本，控制器靠退避重入自动收敛，无需重启。整个过程从告警到定位不依赖登录机器翻文本日志。

<FillBlank no="117-F1" tpl="Reconcile 失败时应该把错误 ___ 给队列，由队列按 ___ 重新入队，而不是在函数内就地死循环重试。" :answers='["上抛~返回","指数退避~退避~backoff"]' hint="想想 level-triggered 下重试的对象是整轮对账还是单个动作" explain="错误上抛、队列指数退避重入，重入后从 Get 重新读最新快照。就地重试既会打爆 apiserver，又复用了可能已经过期的 Observed，是典型的错误写法。" />

## 10. 测试策略

分六层：单测覆盖纯函数 `Decide` 与 `Validate`（表驱动、不需要集群）；集成用 `fakeCluster` 验证 Reconcile 的动作顺序与状态回写，生产再加 `envtest` 起轻量 apiserver；契约测试把第 4 节的 CRD YAML 喂给 `kubectl apply --dry-run` 校验 schema 与 CEL；故障注入让 `Get`、`UpdateStatus` 随机返回错误或冲突，验证退避与冲突收敛且不死循环；基准测试压 `Decide` 与缓存 `Get`；全链路压测灌入上千 CR 观察队列与 apiserver QPS。mock 边界只切在 `Cluster` 接口一处——因为它是唯一的 IO 出口，把它替换成 fake 就能让上层全部逻辑脱离 K8s 运行，这是依赖倒置带来的直接收益；再往里 mock（比如 mock `Decide`）没有意义，纯函数本来就该真跑。

```go
// 文件：internal/reconcile/reconcile_test.go
package main

import "testing"

// 本段自带被测纯函数，保证单独 go test 即可编译运行。
type spec struct {
	replicas int
	version  string
}

type observed struct {
	ready   int
	version string
}

func decide(w spec, g observed) string {
	if g.version != w.version {
		return "rollout"
	}
	if g.ready != w.replicas {
		return "scale"
	}
	return "none"
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		want spec
		got  observed
		act  string
	}{
		{"版本不同优先升级", spec{3, "v2.0.0"}, observed{3, "v1.4.0"}, "rollout"},
		{"副本不足扩容", spec{5, "v1.4.0"}, observed{3, "v1.4.0"}, "scale"},
		{"副本过多缩容", spec{2, "v1.4.0"}, observed{3, "v1.4.0"}, "scale"},
		{"已收敛无动作", spec{3, "v1.4.0"}, observed{3, "v1.4.0"}, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decide(c.want, c.got); got != c.act {
				t.Fatalf("decide=%s want=%s", got, c.act)
			}
		})
	}
}

func TestDecideIdempotent(t *testing.T) {
	w := spec{replicas: 3, version: "v2.0.0"}
	g := observed{ready: 3, version: "v2.0.0"}
	first := decide(w, g)
	for i := 0; i < 3; i++ {
		if got := decide(w, g); got != first {
			t.Fatalf("call %d changed result: %s", i, got)
		}
	}
}

func BenchmarkDecide(b *testing.B) {
	w := spec{replicas: 3, version: "v2.0.0"}
	g := observed{ready: 3, version: "v1.4.0"}
	for i := 0; i < b.N; i++ {
		_ = decide(w, g)
	}
}
```

压测与故障注入的验收线：连调 `Reconcile` 三次断言最终 `status` 一致且 `ApplyReplicas` 收到的是绝对值；让 `UpdateStatus` 交替返回冲突与成功，断言八次预算内收敛、超预算进死信；表驱动覆盖「版本优先、扩容、缩容、收敛、删除带 Finalizer、删除无 Finalizer」六类。

## 11. 部署与容量

部署拓扑：控制器以 `Deployment` 跑 2 个副本，靠 `Lease` 做 leader election，非 leader 待命但同样维护缓存与指标端点；`ServiceAccount` 绑定最小权限 `ClusterRole`；`Service` 暴露 `/metrics` 与 `/healthz`；`ServiceMonitor` 让 Prometheus 抓取；`PrometheusRule` 落第 9 节的告警规则。CRD 与 RBAC 通过 GitOps 渠道先行应用，控制器镜像随后滚动升级。

依赖组件与版本前提：Kubernetes 1.28+（CRD 的 `apiextensions.k8s.io/v1` 与 CEL 校验规则稳定可用），Go 1.21+（`log/slog`、`atomic.Int64` 均为标准库），生产替换 `controller-runtime` v0.16 与 `client-go` v0.28 时二者版本需对齐，Prometheus Operator 用于加载 `PrometheusRule`。本讲代码不依赖任何第三方库，只用标准库即可编译运行。

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `--workers` | 4 | 2 | 4 | 并发调谐的 worker 数，按 CPU 核数调 |
| `--resync-period` | 5m | 30s | 5m | 全量对账周期，环境越小越短 |
| `--qps` / `--burst` | 20 / 40 | 5 / 10 | 20 / 40 | 客户端侧限流，硬约束写 QPS 不超 40 |
| `--leader-lease` | 15s | 5s | 15s | 选主租约，决定 RTO 上界 |
| `--max-retries` | 8 | 3 | 8 | 单 key 重试预算 |
| `--metrics-bind` | :8080 | :8080 | :8080 | 指标与健康端点 |

资源估算：Informer 缓存 2000 个 CR、单对象约 2 KB，含索引放大三倍约 12 MB，加队列与运行时开销，单实例内存控制在 200 MB 以内足够；CPU 峰值 0.6 核；磁盘为零（无本地持久状态，日志走标准输出由节点采集）；带宽主要是 watch 长连接与 resync 的 LIST，稳态每秒几十 KB，全量 resync 瞬间约 4 MB。扩容方式是水平加副本——但受 leader election 限制，加副本只提升**故障切换速度**而非吞吐；要提升吞吐应调 `--workers` 并同步确认 apiserver QPS 预算。优雅退出：收到 SIGTERM 后先 cancel `ctx`，停止接收新事件，等待在途调谐完成或超时，最后主动释放 `Lease` 让备副本立即接管，把切换时间压到租约以内。

## 12. 性能与压测

压测方法要真实可执行：用 `go test -bench=. -benchmem` 跑 `BenchmarkDecide` 拿纯函数基线；用第 6.4 节的 `Run` 配 `fakeCluster` 灌入 2000 个 key，配合 `go test -race` 检查队列与缓存的竞态；用自写的灌数据程序批量构造 CR YAML 并 `kubectl apply -f`；端到端压测用 `vegeta` 打 apiserver 写请求，同时观察 `reconcile_duration_seconds` 与 `workqueue_depth`。数据构造讲究「热点与冷对象混合」：80% 的 CR 长期不变，20% 在压测窗口内被反复改 spec，模拟真实的发布节奏。

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 调谐吞吐 | 12 /s | 20 /s | 4 worker 加缓存读，单动作 40 ms 量级 |
| 单次 Reconcile P50 / P99 | 65 / 310 ms | 40 / 180 ms | 去掉 UpdateStatus 前的重复 Get |
| 失败率 | 0.9% | 0.2% | 冲突带 RV 重试、脏数据 schema 前置拦截 |
| apiserver 写 QPS 峰值 | 62 | 42 | 客户端 `--qps` 限流加事件去重 |
| 内存 / CPU | 260 MB / 1.1 核 | 180 MB / 0.6 核 | 缓存对象不重复深拷贝、指标改原子计数 |
| 全量 resync 耗时 | 140 s | 95 s | 按 namespace 批量 List，减少往返 |

瓶颈定位过程有指标证据：第一版 P99 卡在 310 ms，`go tool pprof` 的 mutex profile 显示 `Store.Update` 的写锁在全量 resync 时排队，原因是列举时也持了写锁；改成 `RLock` 列举后写竞争下降。CPU profile 又显示 `json.Marshal` 在 `UpdateStatus` 里占用偏高，原因是每次都把整个对象序列化后提交；改为只 patch status 子资源的字段后 P50 降到 40 ms。优化前后对比见上表，但**仍然存在的限制**必须写清：单 leader 的写路径是串行的，CR 规模上万后即便加 worker 也要等 apiserver 写带宽；全量 resync 是周期性尖峰，集群规模再涨就得引入按命名空间的增量对账与分片控制器，这正是第 13 节的演进触发条件。

## 13. 演进与替代方案

先给四个替代架构的取舍，再给演进路线。这里敢写代价：手写控制循环在本讲规模是最优解，但不是万能解。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 手写控制循环（本讲） | 千级 CR、单 CRD | 依赖零第三方、逻辑全可读、纯函数可完整单测 | 要自己处理选主、webhook、转换，功能有天花板 | 需要 Admission Webhook、多版本转换或多 CRD 时 |
| controller-runtime / kubebuilder | 万级 CR、多 CRD | 内建 informer、workqueue、leader election、webhook、指标 | 引入庞大依赖与生成器约定，调试要懂框架 | 团队要长期维护多个 Operator 时 |
| Helm 加 GitOps（Argo CD） | 无状态服务、无自愈需求 | 简单、声明式、天然可回滚 | 只管部署不管运行时漂移，Pod 驱逐与手改副本无法纠正 | 需要「运行时自愈」时就不能只靠它 |
| CronJob 定时对账脚本 | 十几个 CR、容忍分钟级延迟 | 几行脚本就能跑，无需常驻进程 | 非事件驱动、延迟高、无状态机、失败难追踪 | 出现「要求 30 秒内收敛」的 SLO 时 |

从当前版本演进到「controller-runtime 版」的触发条件写死：纳管 CR 超过 5000、需要第二个 CRD、或需要真正的 ValidatingWebhook 做跨资源校验，三者任一满足就启动迁移。迁移步骤：第一步先把 `Cluster` 接口的实现从 fake 换成 `client.Client`，`Decide` 与 `Reconcile` 一行不改（这正是依赖倒置的回报）；第二步引入 controller-runtime 的 `Builder`，让它接管 watch 与 workqueue，保留自研的限速参数；第三步接上 webhook 与转换；第四步灰度——新老控制器并行但只让一个 leader 生效，观察燃烧率两个发布周期无异常后下线老版本。回滚路径清晰：CRD 的 schema 只增不减，老控制器镜像能直接读新对象，回切时把镜像 tag 改回即可，无需数据迁移，这也是「status 用独立子资源、新字段全可选」带来的兼容性红利。

## 14. 真实工程里的坑

以下七条都来自生产里真会遇到的场景，每条给出错误写法、正确写法与提前发现手段。

**坑一：动作传增量，重复调谐状态漂移。** 现象是副本数越调越大。原因是 `ApplyReplicas` 写成「当前加 1」，而事件重复、退避重试、resync 都会反复调用它。错误写法 `c.ApplyReplicas(name, got.ReadyReplicas+1)`，正确写法 `c.ApplyReplicas(name, want.Replicas)` 传期望绝对值。提前发现：写一条「连续调谐十次断言副本不变」的幂等测试，这是本讲 `TestDecideIdempotent` 的延伸。

<Compare title="增量与绝对值" :panels='[{"label":"错误：增量","code":"ApplyReplicas(name, got.ReadyReplicas + 1)","note":"每重试一次就多一个副本，事件重复一次又多一个，状态越跑越偏"},{"label":"正确：绝对值","code":"ApplyReplicas(name, want.Replicas)","note":"无论调用多少次都是设为期望值，重复执行结果恒成立"}]' />

**坑二：无条件 UpdateStatus 覆盖他人写入。** 现象是 conditions 里刚写的 Degraded 被莫名其妙抹掉，或 `generation` 回退。原因是拿到旧对象直接 `Update` 整个 status。正确写法是携带 `resourceVersion` 走乐观锁，409 冲突上抛给队列退避重入，绝不强制覆盖。提前发现：故障注入让 `UpdateStatus` 随机返回冲突，断言没有出现状态回退。

**坑三：先移除 Finalizer 再清理。** 现象是删掉 CR 后外部 StatefulSet 与 PVC 永久残留、账单一直在扣。原因是移除 Finalizer 后对象立即被 apiserver 删除，清理动作即便失败也没人再触发。正确顺序是 `Cleanup` 幂等成功后再 `RemoveFinalizer`（见第 6.2 节）。提前发现：集成测试覆盖「清理失败必须仍带 Finalizer」。

**坑四：事件丢失没有 resync 兜底。** 现象是某次 spec 改动永远没生效，且没有任何报错。原因是把 watch 事件当成唯一真相。正确做法是 `Run` 里挂 5 分钟全量 resync，把所有 CR 重新入队对账。提前发现：故障注入主动丢弃一批事件，断言状态仍在两个 resync 周期内收敛。

**坑五：重试无退避，打爆 apiserver 引发雪崩。** 现象是控制面一慢，控制器 QPS 反而飙升，把 etcd 彻底压垮。原因是失败就地 `for` 循环重试。正确做法见第 6.4 节：限速队列加指数退避加最大重试预算。提前发现：压测时注入 30 秒 apiserver 超时，观察写 QPS 是否被 `--qps` 稳稳压在 40 以内。

**坑六：就地修改 Informer 缓存对象。** 现象是控制器读到「幽灵状态」——明明 apiserver 里是 3 副本，缓存里却变成 999。原因是 `Get` 返回了内部对象的引用，调用方改了它。正确做法是读与写都 `DeepCopy`（见第 6.3 节）。提前发现：`go test -race` 加一条「改返回值后缓存不变」的断言。

**坑七：用本地时钟判断条件与 lease，遭遇时钟回拨。** 现象是 NTP 回拨后条件时间倒挂、leader 租约被误判过期引发双主。原因是到处用 `time.Now()`。正确做法是用 apiserver 下发的 `metav1.Time` 做条件时间戳、用租约对象的 `renewTime` 配合单调时钟判断存活，控制器内部只用 `time.Duration` 做相对退避。提前发现：混沌演练里主动回拨节点时钟，断言不出现双主。

<Compare title="结构化日志与文本日志" :panels='[{"label":"错误：文本拼接","code":"log.Printf 把消息拼成一整行文本","note":"无法按 CR、generation、action 聚合，夜里翻日志靠肉眼，故障时定位以小时计"},{"label":"正确：slog 结构化","code":"logger.Error 带 cr 与 generation 与 action 字段","note":"字段可检索、可与 trace_id 关联，一条告警能直接串到审计表"}]' />

## 动手练一练

<Exercise title="给 Reconcile 加上 Conditions 与 observedGeneration" task="当前 Reconcile 只回写 phase 一个字段。请扩展状态回写：每次调谐同时写入 observedGeneration，并按结果维护 Ready、Progressing、Degraded 三条条件，且同 type 条件只保留最新一条。写出关键结构与判断逻辑。" :hints='["状态里加 ObservedGeneration 字段，只有成功处理完这一代才更新它","动作是 none 且实际就绪等于期望时写 Ready 为 True，否则写 Progressing","任何一次动作失败都要写一条 Degraded 条件并带上 reason","SetCondition 按 type 覆盖，别用 append 导致条件数组膨胀"]' solution="在 Status 增加 ObservedGeneration 与 Conditions 字段；Reconcile 成功执行动作后，先 SetCondition 写入对应条件（收敛写 Ready 为 True 且 reason 为 Converged，未收敛写 Progressing，调谐出错写 Degraded 且 reason 取错误分类），再把 ObservedGeneration 设为本次 Get 到的 Generation，最后带 resourceVersion 一次性 UpdateStatus。注意顺序：先按结果决定条件，再更新 observedGeneration，避免「这一代还没处理完就声称已处理」。测试覆盖「连续三次调谐条件数组长度恒为三」「失败时 observedGeneration 不推进」「冲突重试成功后 observedGeneration 等于最新 generation」三种。" explain="难点有两个：一是 observedGeneration 只能在真正收敛或动作成功后推进，写早了会让「还没处理」被误判成「已处理」；二是 Conditions 必须按 type 覆盖，否则每次 resync 追加一条，几千轮之后 status 会把 etcd 对象撑爆并拖慢所有读。" />

## 随堂检测

<Quiz no="117-1" q="为什么本讲的 Reconcile 必须是 level-triggered，而不是只在事件到达时执行一次动作？" :options='["为了少写几行代码","因为事件可能丢失、动作可能失败、控制器可能重启，必须靠反复对账收敛","因为 Kubernetes 不支持 watch 事件","因为 Go 的 channel 不可靠"]' answer="1" explain="事件会丢、动作会失败、进程会重启，这三条决定了只有每次从当前实际状态重新计算、配合 5 分钟全量 resync，才能保证最终收敛。第 6.2 节的 Reconcile 重入后重新 Get 而非复用旧 Observed，正是这个设计。" />

<Predict no="117-2" q="期望 3 副本 v2，实际 1 副本 v1，一次 Reconcile 会做什么？" :options='["同时扩容到 3 并升级到 v2","先扩容到 3，下一轮再升级","先升级到 v2，下一轮再扩容","什么都不做，等待人工介入"]' answer="2" output="先升级到 v2，下一轮再扩容" explain="Decide 里版本不一致优先于副本差异，返回 ActionRollout 与 PhaseUpgrading。一次只推进一个维度，等下一轮 Get 读到新版本后才处理扩缩容，这样失败时不用判断该补偿哪个动作。" />

<Quiz no="117-3" q="关于 Reconcile 的幂等性，下列哪种写法是错的？" :options='["ApplyReplicas 传期望的绝对值 3","UpdateStatus 允许带 resourceVersion 重复覆盖","ApplyReplicas 传增量，每次把当前副本加 1","Cleanup 重复调用结果相同"]' answer="2" explain="传增量会让重复调谐不断叠加，事件重复、退避重试、resync 都会放大它。所有动作必须是「设为期望值」的绝对值语义，删除清理同样要幂等。" />

<Quiz no="117-4" q="UpdateStatus 返回 409 冲突时，Reconcile 正确处理是？" :options='["强制覆盖写入最新状态","直接返回错误，由队列退避重入整轮对账","删除本地缓存后重试","把对象标记为删除"]' answer="1" explain="冲突说明有别的写入方刚更新过，强制覆盖会抹掉对方的条件。上抛错误让队列退避重入，重入后重新 Get 拿最新 resourceVersion，这是 level-triggered 与乐观锁配合的标准动作。" />

## 一页纸速记

<KnowledgeCard :points='["CRD 只声明期望状态，Controller 负责把实际状态持续拉向期望","Decide 写成纯函数，可表驱动单测、可重入；一次 Reconcile 只推进一个动作","level-triggered 加全量 resync，天然容忍事件丢失与进程重启","限速队列加指数退避加最大重试预算，保护 apiserver 不被错误重试打爆","Finalizer 先幂等清理再移除，status 回写带 resourceVersion 乐观锁","用调谐次数、耗时、失败率与错误预算燃烧率四个指标接告警"]' :pitfalls='["动作传增量而非绝对值，重复调谐状态漂移","把事件当唯一真相，不做 resync 兜底","清理前先移除 Finalizer，外部资源永久泄漏","无条件 UpdateStatus 覆盖他人写入或不带乐观锁","就地修改 Informer 缓存对象，读到幽灵状态","失败就地死循环重试，打爆 apiserver 引发雪崩"]' summary="Operator 的本质是一个永不停歇的幂等对账循环：声明期望，反复比较，一次一步地收敛，并把控制器自己的健康也做成可观测。" />

## 小结与下一讲

1. 用 CRD 的 OpenAPI schema 加 CEL 把非法期望挡在落库之前，用 `Spec` 表达期望、`Observed` 表达实际、`Decide` 作为纯函数算出唯一动作，`Reconcile` 完成「读、决、做、写状态」一次调谐，并给出了带 Finalizer 的完整状态机代码。
2. 控制器用线程安全 Informer 缓存、限速队列、指数退避与全量 resync 兜住乱序、失败与事件丢失，讲清了幂等、level-triggered、状态冲突、并发串行、资源释放五类边界，也点明了单 leader 写串行这一仍然存在的限制。
3. 把调谐指标、slog 结构化日志、trace_id 传播与错误预算燃烧率告警接成闭环，让「控制器自己健康不健康」也可被观测，并给出从手写循环演进到 controller-runtime 的触发条件与回滚路径。

到这里，云原生这条线收束了：用 Go 写的控制器去管理同样用 Go 写的服务，正是「用 Go 管理 Go 服务」这个目标最贴切的落点。最后一个项目把 Go 的并发优势用到 AI 工程上——多家大模型供应商的流式协议各不相同，网关要统一鉴权、限流、计费，还要把 token 一个一个实时转发给客户端，并且在客户端断开时立刻停掉上游，别让钱白烧。下一讲实现多供应商适配与 SSE 流式转发。

[下一讲：实战 12 · LLM 网关与流式推理管道](/projects/ai-gateway)
