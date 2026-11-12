---
title: 第 117 讲 · 实战 11 · Kubernetes Operator 与可观测平台
description: 用 CRD 声明期望状态，写出完整 Reconcile 循环与限速队列，并把指标、日志、告警收成平台
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="75 分钟" goal="用 Go 管理 Go 服务" prereq="第 116 讲 · 秒杀与高并发库存" />

# 第 117 讲 · 实战 11 · Kubernetes Operator 与可观测平台

> 前十讲我们写了 CLI、网络服务、KV 存储、电商与秒杀系统，服务越来越多。靠 `kubectl scale` 和几段脚本去管它们有个致命问题：**脚本执行一次就结束了**，有人手动改了副本数、某个 Pod 挂了、版本被回滚，没有任何东西会把状态拉回来。Operator 的思路是反过来——用户只写「我要 3 个 v1.4.0 的副本」，控制器则不知疲倦地把实际状态往期望状态上收敛。这一讲用标准库写出一个完整可编译的 **Reconcile 循环**，并把指标、日志与告警接成一套可观测闭环。

## 这一讲你会得到什么

<CheckList id="L117-goal" title="本讲目标" :items='["用 CRD 表达「期望状态」，用 Observed 表达「实际状态」","写出可重入、幂等的 Reconcile 循环与纯函数决策 Decide","用限速队列加指数退避保护 apiserver，用全量 resync 兜底事件丢失","把调谐指标、日志与错误预算告警接成一套可观测闭环"]' />

## 项目目标与约束

- **目标**：定义一个 `KVApp` 自定义资源；控制器监听变化并自动完成扩缩容、滚动升级、状态回写；暴露指标与告警规则。
- **约束**：**声明式**而非命令式；`Reconcile` 必须幂等、可重入、单次只推进一个动作；失败靠队列重试而非就地死循环；控制器只依赖一个窄窄的 `Cluster` 接口。
- **诚实边界**：下面用标准库实现控制循环的完整逻辑，不引入 `controller-runtime`；生产环境会由它负责 informer、leader election 与 webhook，本讲聚焦「调谐」这件事本身。

<Term word="Reconcile" text="调谐：反复比较期望状态与实际状态，执行一个让两者靠近的动作，直到收敛；level-triggered 而非 edge-triggered" />

## 架构蓝图

<ConceptDiagram type="k8s" title="Operator 控制回路：CRD → apiserver → informer → workqueue → Reconcile → 状态回写" caption="事件只负责把对象 key 放进队列，真正的逻辑全在 Reconcile 里" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"事件入队","desc":"CR 变更或定时 resync 把 name 放进队列","tone":"blue"},
  {"title":"读取状态","desc":"Cluster.Get 拿期望与实际","tone":"green"},
  {"title":"纯函数决策","desc":"Decide 算出唯一动作与目标相位","tone":"orange"},
  {"title":"执行动作","desc":"扩缩容或滚动升级只做一个","tone":"orange"},
  {"title":"回写状态","desc":"UpdateStatus，失败则退避重入队列","tone":"red"}
]' />

## 关键实现

以下代码同属 `package operator`，分别为 `types.go`、`reconcile.go`、`controller.go`、`metrics.go`、`reconcile_test.go`，同一目录 `go test ./...` 可编译运行。

### 1. CRD 的 Go 类型与状态机

```go
// 文件：types.go
package operator

// KVApp 对应一份 CRD 自定义资源：用户只声明期望，不写操作步骤。
type KVApp struct {
	Name     string
	Replicas int
	Version  string
	Status   Phase
}

type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseUpgrading Phase = "Upgrading"
	PhaseDegraded  Phase = "Degraded"
)

// Observed 是控制器从集群里读到的实际状态。
type Observed struct {
	ReadyReplicas int
	Version       string
}

// Action 是 Reconcile 计算出的「让实际趋近期望」的动作。
type Action string

const (
	ActionNone      Action = "none"
	ActionScaleUp   Action = "scale-up"
	ActionScaleDown Action = "scale-down"
	ActionRollout   Action = "rollout"
)
```

用户提交的 YAML 就是全部输入，控制器不保存任何"上次执行到哪"的记忆：

```yaml
apiVersion: kv.example.com/v1
kind: KVApp
metadata:
  name: kv-prod
spec:
  replicas: 3
  version: v1.4.0
```

### 2. Reconcile：决策与一次调谐

```go
// 文件：reconcile.go
package operator

// Decide 是一个纯函数：只根据「期望 + 实际」算出下一步动作。
// 写成纯函数是 Reconcile 可测试、可重入的关键：没有副作用、没有隐藏状态。
// 版本不一致优先于扩缩容，避免一边升级一边扩容互相打架。
func Decide(want KVApp, got Observed) (Action, Phase) {
	if got.Version != want.Version {
		return ActionRollout, PhaseUpgrading
	}
	switch {
	case got.ReadyReplicas < want.Replicas:
		return ActionScaleUp, PhasePending
	case got.ReadyReplicas > want.Replicas:
		return ActionScaleDown, PhasePending
	case got.ReadyReplicas == want.Replicas && got.ReadyReplicas > 0:
		return ActionNone, PhaseRunning
	default:
		return ActionNone, PhaseDegraded
	}
}

// Cluster 抽象对 K8s 的读写，控制器只依赖这张接口，便于用 fake 做单测。
type Cluster interface {
	Get(name string) (KVApp, Observed, bool, error)
	ApplyReplicas(name string, n int) error
	ApplyVersion(name string, v string) error
	UpdateStatus(name string, p Phase) error
}

// Reconcile 是一次调谐：读期望与实际 → 决策 → 执行一个动作 → 回写状态。
// 它必须幂等：无论被重复调用多少次，结果都收敛到同一个状态。
func Reconcile(c Cluster, name string) error {
	want, got, ok, err := c.Get(name)
	if err != nil {
		return err
	}
	if !ok {
		return nil // 资源已删除，无需动作（清理交给 finalizer）
	}
	action, phase := Decide(want, got)
	switch action {
	case ActionScaleUp, ActionScaleDown:
		err = c.ApplyReplicas(name, want.Replicas)
	case ActionRollout:
		err = c.ApplyVersion(name, want.Version)
	case ActionNone:
		// 已经收敛，什么都不做
	}
	if err != nil {
		return err // 失败即返回，由上层带退避重入队列重试
	}
	return c.UpdateStatus(name, phase)
}
```

### 3. 控制器主循环：限速队列与退避

```go
// 文件：controller.go
package operator

import (
	"context"
	"time"
)

// RateLimitingQueue 用「失败重入 + 退避」避免错误被高频重试打爆 apiserver。
type RateLimitingQueue struct {
	ch chan string
}

func NewRateLimitingQueue(size int) *RateLimitingQueue {
	return &RateLimitingQueue{ch: make(chan string, size)}
}

func (q *RateLimitingQueue) Add(name string) {
	select {
	case q.ch <- name:
	default: // 队列已满：去重丢弃，等下一次事件或全量同步兜底
	}
}

type Controller struct {
	cluster Cluster
	queue   *RateLimitingQueue
}

func NewController(c Cluster) *Controller {
	return &Controller{cluster: c, queue: NewRateLimitingQueue(1024)}
}

// Run 是控制器的主循环：事件只把 key 入队，循环里逐个 Reconcile；
// 失败按退避重新入队，并周期性做一次全量同步兜底，防止事件丢失。
func (ctrl *Controller) Run(ctx context.Context) {
	resync := time.NewTicker(5 * time.Minute)
	defer resync.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case name := <-ctrl.queue.ch:
			if err := Reconcile(ctrl.cluster, name); err != nil {
				// 简化版指数退避；生产用 workqueue 的 rate limiter。
				go func(n string) {
					time.Sleep(2 * time.Second)
					ctrl.queue.Add(n)
				}(name)
			}
		case <-resync.C:
			// 真实控制器在这里 List 全部 CR 并逐个 Add，做一次状态对账。
		}
	}
}

func (ctrl *Controller) Enqueue(name string) { ctrl.queue.Add(name) }
```

### 4. 可观测：指标与告警规则

```go
// 文件：metrics.go
package operator

import "sync/atomic"

// Metrics 用原子计数暴露调谐次数、失败次数与就绪副本数，
// 对应 Prometheus 的 Counter 与 Gauge，供告警规则消费。
type Metrics struct {
	Reconciles atomic.Int64
	Failures   atomic.Int64
	Ready      atomic.Int64
}

func (m *Metrics) Observe(err error) {
	m.Reconciles.Add(1)
	if err != nil {
		m.Failures.Add(1)
	}
}

// ErrorBudgetBurn 计算错误预算燃烧率，超过阈值即触发告警。
func ErrorBudgetBurn(failures, total float64) float64 {
	if total == 0 {
		return 0
	}
	return failures / total
}
```

对应的告警规则（Prometheus rule）关注「失败率」而不是绝对次数：

```yaml
groups:
  - name: kvapp.rules
    rules:
      - alert: KVAppReconcileErrorBudgetBurn
        expr: rate(reconcile_failures_total[5m]) / rate(reconciles_total[5m]) > 0.05
        for: 10m
        annotations:
          summary: 调谐失败率持续超过阈值，需检查 apiserver 与控制器
```

### 5. 纯函数与收敛测试

```go
// 文件：reconcile_test.go
package operator

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		name       string
		want       KVApp
		got        Observed
		wantAction Action
		wantPhase  Phase
	}{
		{"版本不同优先升级", KVApp{Replicas: 3, Version: "v2"}, Observed{ReadyReplicas: 3, Version: "v1"}, ActionRollout, PhaseUpgrading},
		{"副本不足扩容", KVApp{Replicas: 5, Version: "v1"}, Observed{ReadyReplicas: 3, Version: "v1"}, ActionScaleUp, PhasePending},
		{"已收敛无动作", KVApp{Replicas: 3, Version: "v1"}, Observed{ReadyReplicas: 3, Version: "v1"}, ActionNone, PhaseRunning},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, p := Decide(c.want, c.got)
			if a != c.wantAction || p != c.wantPhase {
				t.Fatalf("got %s/%s", a, p)
			}
		})
	}
}

type fakeCluster struct {
	want   KVApp
	got    Observed
	status Phase
	ver    string
}

func (f *fakeCluster) Get(_ string) (KVApp, Observed, bool, error) {
	return f.want, f.got, true, nil
}
func (f *fakeCluster) ApplyReplicas(_ string, _ int) error { return nil }
func (f *fakeCluster) ApplyVersion(_ string, v string) error {
	f.ver = v
	return nil
}
func (f *fakeCluster) UpdateStatus(_ string, p Phase) error {
	f.status = p
	return nil
}

func TestReconcileConverges(t *testing.T) {
	c := &fakeCluster{
		want: KVApp{Name: "kv", Replicas: 3, Version: "v2"},
		got:  Observed{ReadyReplicas: 1, Version: "v1"},
	}
	if err := Reconcile(c, "kv"); err != nil {
		t.Fatal(err)
	}
	if c.ver != "v2" { // 版本不一致优先于扩缩容
		t.Fatalf("version=%s", c.ver)
	}
	if c.status != PhaseUpgrading {
		t.Fatalf("status=%s", c.status)
	}
}
```

## 深入一步：为什么必须是 level-triggered

<Compare title="命令式脚本与声明式控制器" :panels='[
  {"label":"命令式脚本","code":"kubectl scale 后 set image，执行完就结束","note":"有人手动改回去、Pod 挂掉，都不会被自动纠正"},
  {"label":"声明式 Reconcile","code":"Decide(want, got) 得出动作再 UpdateStatus","note":"每次事件都重新对账，偏离期望就持续收敛，天然自愈"}
]' />

控制器不记「上次做了什么」，每次都从**当前实际状态**重新算一遍。这带来三个好处：事件丢了也会有全量 resync 兜底；升级到一半崩溃，重启后仍能接着收敛；重复调谐同一对象结果不变。代价是 `Reconcile` 必须幂等——`ApplyReplicas(3)` 调用两次和一次效果相同，`UpdateStatus` 也要允许覆盖。

<Term word="level-triggered" text="基于「当前状态」而非「状态变化事件」驱动：只要现状偏离期望就持续动作，天然容忍事件丢失与重复" />

单次只做一个动作也是刻意的：同时扩容又升级，会让「实际状态」同时被两个维度改变，很难判断哪一步成功、失败了该补偿谁。一次一步、每步回写 `status`，调谐过程就变成了可观测、可打断、可重入的状态机。

## 失败路径与边界条件

| 失败场景 | 后果 | 处理 |
| --- | --- | --- |
| apiserver 抖动 | `Get` 或 `UpdateStatus` 报错 | 直接返回错误，由队列退避重入 |
| 事件丢失 | 某次变更 never 被处理 | 周期性全量 resync 重新对账 |
| 同一对象并发调谐 | 两个动作互相覆盖 | workqueue 按 key 去重，单 worker 串行 |
| `status` 更新冲突 | 乐观锁失败 | 带 `resourceVersion` 重试，不覆盖别人的更新 |
| finalizer 卡住 | 删除操作永久挂起 | 清理逻辑必须幂等且有超时 |
| 升级中途崩溃 | 部分副本是旧版本 | 重启后 `Decide` 看到版本不一致，继续 rollout |
| 错误被高频重试 | 打爆 apiserver、雪崩 | 限速队列 + 指数退避 + 熔断 |

<Callout type="warn" title="幂等不是加分项，是及格线" text="Reconcile 会被重复调用无数次：事件重复、退避重试、resync 都会触发它。任何一步只要不是幂等的（比如把副本数加 3 而不是设为 3），重复调谐就会让状态越跑越偏。写控制器时先问自己一句：这个函数被连调十次，结果还一样吗？" />

## 测试与压测思路

- **纯函数**：`TestDecide` 用表驱动覆盖版本、副本、已收敛三类；纯函数无需集群，是覆盖率最高的一层。
- **集成**：`TestReconcileConverges` 用 `fakeCluster` 断言「版本优先」与状态回写；真实项目用 `envtest` 起一个轻量 apiserver 做端到端。
- **幂等**：连续 `Reconcile` 三次，断言最终 `status` 一致且 `ApplyReplicas` 收到的是绝对值而非增量。
- **故障注入**：让 `Cluster.Get` 随机失败，验证队列退避重试且不会无限打满；让 `UpdateStatus` 冲突，验证重试收敛。
- **压测**：灌入上千个 CR，观察队列积压、调谐延迟分位与 apiserver QPS；用错误预算燃烧率验证告警规则会按预期触发。

## 工程实践 / 真实场景

生产 Operator 由 `controller-runtime` / kubebuilder 生成骨架：informer 负责 watch 与本地缓存、workqueue 负责去重与限速、leader election 保证多副本只有一个在调谐、webhook 做准入校验。可观测上把 `reconciles_total`、`reconcile_failures_total`、`reconcile_duration_seconds` 三个指标接入 Prometheus，日志里带 CR 的 `name` 与 `generation`，再配上面那条燃烧率告警。发布与回滚则交给 GitOps（第 92 讲）：改的是仓库里的 YAML，控制器负责把集群拉到那个状态。

## 可继续扩展的方向

- 用 `controller-runtime` 替换手写循环，接上 informer、leader election 与 finalizer。
- 支持多版本 CRD 的转换 webhook，让 `v1` 与 `v2` 共存。
- 加 Status Condition（Ready / Progressing / Degraded）与事件记录，提升可诊断性。
- 把指标、日志、链路追踪汇总成统一大盘，并接入分级告警与 On-call（第 93 讲）。

## 动手练一练

<Exercise title="让 Reconcile 支持删除与 finalizer" task="当前资源不存在时 Reconcile 直接返回。请设计带 finalizer 的删除流程：用户删除 CR 后，控制器先清理外部资源再移除 finalizer。写出状态判定与关键步骤。" :hints='["删除时 apiserver 会给对象打 deletionTimestamp，而不是立刻消失","finalizer 是一个字符串列表，非空时对象不会被真正删除","清理动作必须幂等，且成功后才能移除 finalizer"]' solution="在 KVApp 增加 DeletionTimestamp 与 Finalizers 字段；Reconcile 先判断时间戳非空：若 finalizers 含 kv.example.com/cleanup，则调用 Cluster.Cleanup(name)（幂等，如删除对应的 StatefulSet 与 PVC），成功后 RemoveFinalizer 并更新对象，返回；若无 finalizer 则资源已可被删除，直接返回。测试覆盖「清理失败保留 finalizer」「清理成功移除 finalizer」「重复删除不会重复清理」三种。" explain="难点在于「清理」与「移除 finalizer」的顺序：先移除再清理会让外部资源永远没人回收；先清理再移除，即便崩溃重入也会因为清理幂等而安全收敛。" />

## 随堂检测

<Quiz no="117-1" q="为什么 Reconcile 要写成 level-triggered，而不是只在事件到达时执行一次动作？" :options='["为了少写代码","因为事件可能丢失、动作可能失败、控制器可能重启，必须靠反复对账收敛","因为 Kubernetes 不支持事件","因为 Go 的 channel 不可靠"]' answer="1" explain="事件会丢、动作会失败、进程会重启。只有每次都从当前实际状态重新计算，配合全量 resync，才能保证系统最终收敛到期望，而不是依赖某次事件恰好被处理。" />

<Predict no="117-2" q="期望 3 副本 v2，实际 1 副本 v1，一次 Reconcile 会做什么？" :options='["同时扩容到 3 并升级到 v2","先扩容到 3，下一轮再升级","先升级到 v2，下一轮再扩容","什么都不做，等待人工介入"]' answer="2" output="先升级到 v2，下一轮再扩容" explain="Decide 里版本不一致优先于副本数差异，返回 ActionRollout 与 PhaseUpgrading。一次只推进一个维度，等下一轮 Get 读到新版本后再处理扩缩容。" />

<Quiz no="117-3" q="关于 Reconcile 的幂等性，哪种写法是错的？" :options='["ApplyReplicas 传期望的绝对值 3","UpdateStatus 允许重复覆盖","ApplyReplicas 传增量，每次加 1","Cleanup 重复调用结果相同"]' answer="2" explain="传增量会让重复调谐不断叠加，状态越跑越偏。所有动作都必须用「设为期望值」的绝对值语义，且清理、状态回写都要能重复执行。" />

## 一页纸速记

<KnowledgeCard :points='["CRD 只声明期望状态，Controller 负责把实际状态持续拉向期望","Decide 写成纯函数，可单测、可重入；一次 Reconcile 只推进一个动作","level-triggered 加全量 resync，天然容忍事件丢失与进程重启","限速队列加指数退避，保护 apiserver 不被错误重试打爆","用调谐次数、失败率、燃烧率三个指标接告警"]' :pitfalls='["动作传增量而非绝对值，重复调谐状态漂移","把事件当唯一真相，不做 resync 兜底","清理前先移除 finalizer，外部资源永久泄漏","status 更新不带 resourceVersion 重试，覆盖别人的写入"]' summary="Operator 的本质是一个永不停歇的幂等对账循环：声明期望，反复比较，一次一步地收敛。" />

## 小结与下一讲

1. 用 CRD 表达期望状态、`Observed` 表达实际状态，`Decide` 作为纯函数算出唯一动作，`Reconcile` 完成「读、决、做、写状态」一次调谐。
2. 控制器用限速队列、指数退避与全量 resync 兜住失败与事件丢失，并讲清了幂等、level-triggered、状态冲突、finalizer 四类边界。
3. 把调谐指标、结构化日志与错误预算告警接成闭环，让「控制器自己健康不健康」也可被观测。

最后一个项目把 Go 的并发优势用到 AI 工程上：多家大模型供应商的流式协议各不相同，网关要统一鉴权、限流、计费，还要把 token 一个一个实时转发给客户端。下一讲实现多供应商适配与 SSE 流式转发。

[下一讲：实战 12 · LLM 网关与流式推理管道](/projects/ai-gateway)
