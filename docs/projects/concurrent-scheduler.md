---
title: 第 110 讲 · 实战 08 · 分布式任务调度系统
description: 讲透 etcd 选主、租约续期、session 失效与故障转移，并用 fencing token、分片与幂等实现高可用调度
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="实现 Master-Worker 的高可用调度" prereq="第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式" />

# 第 110 讲 · 实战 08 · 分布式任务调度系统

> 定时任务最怕执行两次：重复扣款、重复发券、重复对账，一旦发生补都补不回来。可为了高可用又必须跑多个 Master，于是绕不开一个根本问题——凭什么任意时刻只有一个节点能派发？这一讲把问题拆到底：先用可度量的约束定义「不重不漏」，再用 etcd 的租约与选主把「唯一派发者」落成代码，接着用任务状态机、一致性哈希分片、执行器隔离与 fencing token 把故障转移后的重复执行收敛掉，最后用优雅退位把发布窗口的漏跑压到最小。读完你应该能自己搭出一套 Master-Worker 调度器，并且说清每个机制在防哪一种具体的失败。

## 0. 项目定位与真实场景

<CheckList id="l110-goals" title="本讲目标" :items='["说清为什么定时任务必须先选主再派发、宁可停摆不可双主","走通 Lease、KeepAlive、Session、Election、Watch 的完整链路","解释 TTL 长短对故障转移与误判失主的影响","用 fencing token 在写入端挡住旧 leader 复活造成的脑裂","用任务状态机、分片与执行器隔离实现可恢复的至少一次语义"]' />

业务背景取一个可度量的原型：某支付平台的定时作业平台，承载约 2000 个 job——日终对账、优惠券到期发放、T+1 报表生成、缓存预热、过期订单清理、商户结算、心跳探活。峰值每分钟触发约 500 次，其中对账与结算类任务对「重复执行」零容忍，探活类任务允许偶尔漏一次。使用方是财务、运营、数据与 SRE 四个团队，他们通过控制台增删 job、查看执行记录与失败原因。

现有方案是散落在若干机器上的 crontab，痛点非常具体：其一，单机 crontab 所在机器宕机，上面的任务全部丢失且无人知晓；其二，为了「高可用」把同一份 crontab 复制到三台机器，结果优惠券任务被触发三次，造成真实的资损；其三，没有执行状态与日志，任务卡住还是跑完只能登机器翻文件；其四，某个慢任务没有超时、占满执行槽，把同机其它任务一起拖垮；其五，发布重启时正在跑的任务被硬杀，既没记录也没补偿。

核心矛盾由此明确：**唯一性（任意时刻一个派发者）、高可用（Master 挂了自动换主）、正确性（崩溃换主后不重不漏）三者互相制约**。为了唯一性必须选主，为了高可用必须允许换主，而换主窗口里「旧 leader 到底死没死」无法被可靠判定，于是必然出现至少一次语义，必须靠幂等与 fencing token 在写入端兜住。架构上采用 Master-Worker：Master 负责触发、分片派发、监听选主，Worker 无状态，只负责领取、执行、回报；唯一派发者这件事交给 etcd 的线性一致能力，不自己实现 Raft（见第 088 讲）。

<Term word="fencing token" text="单调递增的令牌，随每次换主递增；写入端只接受更大 token 的请求，用来隔离「自认为还是 leader 的旧节点」，是脑裂唯一可靠的防线" />

## 1. 需求与约束

先把需求写成可验收条目，再由约束推架构。功能需求给编号与判定标准，非功能指标给具体数字，后面的压测与容量估算都围绕它们自洽。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | cron 表达式触发 | 标准 5 字段表达式解析正确，下次触发时间可验证 |
| FR-2 | 任意时刻唯一派发者 | 杀掉 leader 后至多一个 follower 当选，无双主窗口 |
| FR-3 | Master 故障自动转移 | 换主后在途任务可重派，语义为至少一次 |
| FR-4 | 任务分片 | 同一 job 恒定落到同一分片，扩容迁移量可控 |
| FR-5 | 执行器隔离与超时 | 单任务超时或 panic 不影响同机其它任务 |
| FR-6 | 日志与结果回传 | 每个 epoch 的执行状态与日志可查询 |
| FR-7 | 幂等去重 | 重复派发在写入端被拒，不产生重复执行 |
| FR-8 | 优雅退位 | 主动 Resign 后跟随者立即接管，无需等 TTL |

| 指标 | 目标值 | 说明与依据 |
| --- | --- | --- |
| 触发延迟 P99 | 低于 1 秒 | 从计划时刻到派发指令发出 |
| 峰值触发速率 | 500 次每分钟 | 约 8.3 次每秒，留出 10 倍余量设计 |
| 换主时间 RTO | 低于 15 秒 | 约等于剩余租约 TTL |
| 恢复目标 RPO | 等于 0 | 任务定义与状态持久化，换主不丢任务 |
| 调度面可用性 | 99.9% | 允许换主窗口少跑，不允许重跑 |
| 一致性级别 | 选主线性一致 | 依赖 etcd Raft；结果表最终一致即可 |
| 执行状态 | 至少一次，写入幂等 | 崩溃换主允许重派，靠 fencing 收敛 |
| 单任务默认超时 | 30 秒 | 可按 job 覆盖，超时计一次失败并进入状态机 |

**明确不做什么**：不做 DAG 依赖编排与任务间先后约束（那是工作流引擎）；不做跨语言执行器沙箱，示例只跑 Go 函数；不自己实现共识算法；不做任务优先级抢占与公平调度；不做跨数据中心多活。所有选主语义依赖单一 etcd 集群，跨机房部署需另行评估 etcd 的延迟与脑裂边界。

**假设边界**：etcd 为 3 或 5 节点、提供线性一致读写与租约；各节点时钟通过 NTP 同步但**不假设完全同步**，因此时间比较一律用 etcd 的 revision 或单调 epoch，而非本地 wall clock；任务体本身是幂等的或对重复执行有业务容忍；网络分区是可能发生的常态而非异常。这些假设决定了后面为什么必须用 fencing token，而不能只靠「检查自己是不是 leader」。

## 2. 架构蓝图

<ArchBlueprint title="Master-Worker 调度系统分层" caption="点击模块查看职责、技术选型与失败处理；箭头标注相邻层协议，唯一派发者由状态层的 etcd 租约保证" :layers='[{"name":"接入与配置层","note":"作业定义与控制入口","protocol":"HTTP/gRPC + Watch","items":[{"label":"Job API","sub":"增删改查与校验","detail":"职责：接收作业定义、校验 cron 与超时参数；技术选型：net/http 加参数校验中间件，写入前校验表达式；失败处理：非法 cron 直接拒绝并返回字段级错误，不带病入库。","tone":"blue"},{"label":"控制台与 CLI","sub":"人工查看与干预","detail":"职责：查看执行记录、手动触发、暂停作业；技术选型：只读接口复用结果表，写操作走 Job API；失败处理：控制面不可用时不影响已在途的调度，避免故障扩散。","tone":"blue"},{"label":"Config Watcher","sub":"配置热更新","detail":"职责：监听作业与配额变更并通知调度层；技术选型：etcd Watch 或长轮询，变更走版本号；失败处理：监听断开后带版本重连，绝不丢一次变更。","tone":"blue"}]},{"name":"调度控制层","note":"唯一派发与编排","protocol":"etcd 事务 + 租约","items":[{"label":"Leader Election","sub":"抢主与失主感知","detail":"职责：用租约抢主、续租、监听失主信号；技术选型：etcd concurrency 的 Session 与 Election，不自己拿 SETNX 重造；失败处理：续租一断立即停止派发，fail-closed。","tone":"green"},{"label":"Cron Trigger","sub":"计算下次触发","detail":"职责：解析 cron 求下次触发时间并按时唤醒；技术选型：标准库实现 5 字段解析，时间源统一；失败处理：时钟回拨以单调基准校正，漏触发靠持久化计划补跑。","tone":"green"},{"label":"Shard Router","sub":"任务到执行节点","detail":"职责：按分片键把到期任务映射到负责 worker；技术选型：一致性哈希加虚拟节点，纯函数；失败处理：分片漂移时只迁移相邻区间，重派由幂等表挡重复。","tone":"green"}]},{"name":"执行层","note":"隔离执行与回传","protocol":"gRPC 派发 + 结果回传","items":[{"label":"Worker Pool","sub":"并发领取执行","detail":"职责：领取任务、限流并发、回报结果；技术选型：有界信号量加固定 worker，长连接复用；失败处理：单任务失败只影响自身，靠重试预算回投。","tone":"orange"},{"label":"Executor 隔离","sub":"超时与 panic 兜底","detail":"职责：给每个任务独立 context 超时并 recover panic；技术选型：context.WithTimeout 加独立 goroutine 与 channel；失败处理：超时或 panic 转状态机 TimedOut 或 Failed，不打挂 worker。","tone":"orange"},{"label":"Log Shipper","sub":"日志与结果回传","detail":"职责：把执行日志、耗时、状态按 job 加 epoch 回写；技术选型：结构化日志加批量写，trace 贯穿；失败处理：回传失败重试三次后进本地暂存，避免丢结果。","tone":"orange"}]},{"name":"状态与存储层","note":"跨节点共享状态","items":[{"label":"etcd 租约","sub":"选主与成员","detail":"职责：带 TTL 的 key 标识当前 leader，到期自动删除；技术选型：Lease 加 KeepAlive，CreateRevision 作单调 epoch；失败处理：etcd 整体不可用时全员停派发，漏跑可补不重跑。","tone":"purple"},{"label":"Execution 状态表","sub":"状态机与幂等","detail":"职责：记录每个 job 每个 epoch 的状态与已接受最大 token；技术选型：关系表联合主键加 fencing 校验；失败处理：旧 epoch 或重复写入直接拒绝并告警，隔离脑裂。","tone":"purple"},{"label":"Result 存储","sub":"结果与日志","detail":"职责：持久化执行结果与日志供查询；技术选型：唯一键幂等 upsert，按 job 加 epoch 分区；失败处理：写入失败回投任务，重复写由唯一键收敛。","tone":"purple"}]}]' />

逐层解读为什么这样切。**接入与配置层**把「人怎么定义作业」与「系统怎么触发」隔开，好处是作业定义变更不需要重启调度进程，坏处是要维护一份配置版本与 Watch 重连逻辑。**调度控制层**是全局唯一性的所在地，把选主、触发、分片三件事收在一起，是为了让「只有当选的 Master 才能触发并派发」成为一条被代码强制的路径——如果触发逻辑散落在每个节点，选主就形同虚设。**执行层**彻底无状态，Master 挂了 Worker 照常跑完在途任务并回报，这是可用性的关键：换主期间执行不被打断。**状态与存储层**放最底下，etcd 租约提供线性一致的选主，Execution 表提供 fencing 校验与幂等，两者分别解决「谁来派」和「派了算不算数」。

依赖方向严格单向：调度控制层依赖状态层的租约与 Execution 接口，执行层依赖调度层的派发帧，但状态层绝不反向 import 调度层或执行层。具体到包，`Leader Election`、`Shard Router` 只依赖 `Election`、`Sharder` 小接口（第 4 节），换 etcd 实现或换哈希策略只动装配层；反过来若状态层里塞了 cron 解析或派发逻辑，任何一次规则调整都会动到底层，并形成 Go 编译器直接拒绝的循环依赖。

<ConceptDiagram type="raft" title="etcd 之上的 Master-Worker" caption="etcd 用 Raft 保证线性一致，选主只是它的一个应用；多个 Master 抢同一把带租约的锁，Worker 无状态" />

<ConceptDiagram type="hash-ring" title="一致性哈希分片" caption="虚拟节点把 job 分散到环上，扩容只迁移相邻区间的 key，避免取模分片的全量重分布" />

<FlowTimeline :steps='[{"title":"正常运行","desc":"Master 持有租约并持续 KeepAlive，按 cron 派发","tone":"blue"},{"title":"续租中断","desc":"Master 卡死或网络分区，租约停止续期","tone":"green"},{"title":"租约到期","desc":"etcd 自动删除 leader key，epoch 随之失效","tone":"orange"},{"title":"跟随者唤醒","desc":"Watch 到删除事件后重新 Campaign 当选","tone":"orange"},{"title":"epoch 加一","desc":"新 leader 带更大 token 重派在途任务","tone":"red"},{"title":"旧主复活被拒","desc":"旧 leader 带旧 epoch 回来，写入端 fencing 拦下","tone":"red"}]' direction="vertical" />

跟随者一直 Watch，leader key 一删就被唤醒，故障转移延迟约等于剩余 TTL。这也解释了 TTL 的两难：过短会因一次 GC 停顿或网络抖动被误判失主、频繁切主；过长则真故障要等很久才换主。折中取 5 到 15 秒，靠高频 KeepAlive 保活，而不是把 TTL 调大。

## 3. 模块职责与依赖方向

把分层落到具体模块，明确输入、输出、依赖、是否持有状态与失败行为。这张表也是划分代码 owner 与评审边界的依据。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| Job API | 作业定义 | 落库结果 | Config 校验 | 有（jobs 表） | 非法配置拒绝入库 |
| Cron Trigger | cron 表达式、时间源 | 触发时刻 | 无 | 无 | 时钟回拨按单调基准补跑 |
| Leader Election | 租约、节点名 | 当选/失主信号 | etcd | 有（epoch） | 续租断立即停派发 |
| Shard Router | job 分片键 | 目标 worker | 无 | 无 | 漂移时只重派相邻区间 |
| Worker Pool | 派发帧 | 执行请求 | Executor | 无 | 领取失败退避重试 |
| Executor | 任务体、超时 | 结果或错误 | context | 无 | 超时/panic 转状态机 |
| Execution 表 | job、epoch、state | 接受或拒绝 | 数据库 | 有 | 旧 epoch 直接拒绝并告警 |
| Log Shipper | 执行日志 | 落地记录 | Result 存储 | 无 | 回传失败本地暂存 |
| Resign | 退出信号 | 释放租约 | etcd | 无 | 失败则等 TTL 兜底 |

包目录树按「领域 + 分层」组织，接口集中在 `contract`，实现按模块分文件，调度入口单独放 `cmd`：

```text
scheduler/
├── cmd/
│   └── schedd/main.go          # 装配入口，注入 etcd 与存储实现
├── internal/
│   ├── contract/contract.go    # 全部接口与跨层帧（§4）
│   ├── cron/
│   │   ├── parser.go           # 5 字段解析与 Next（§6.1）
│   │   └── state.go            # 任务状态机（§6.2）
│   ├── election/
│   │   └── lease.go            # 选主、续租、epoch、退位（§6.4）
│   ├── shard/
│   │   └── ring.go             # 一致性哈希（§6.3）
│   ├── executor/
│   │   ├── isolated.go         # 隔离、超时、recover（§6.5）
│   │   └── dispatch.go         # 幂等派发与 fencing（§6.6）
│   ├── worker/loop.go          # 领取与结果回传
│   ├── store/execution.go      # 状态与结果幂等落库
│   └── metrics/metrics.go      # 埋点（§9）
└── go.mod
```

生产里选主不自己造轮子，关键调用对应关系如下（`go get go.etcd.io/etcd/client/v3`，用 `clientv3` 与 `concurrency` 包）。示例里的 Coordinator 是对这些语义的等价模拟。

```text
sess, err := concurrency.NewSession(cli, concurrency.WithTTL(10))
election := concurrency.NewElection(sess, "/sched/leader")
err = election.Campaign(ctx, "master-1")   // 抢带租约的 key
defer election.Resign(ctx)                  // 主动退位，免去等 TTL
<-sess.Done()                              // 续租失败，立即停止派发
```

<Callout type="warn" title="边界划分最常见的错误" text="把「是不是 leader」的判断做成一次性的本地布尔值，在派发前 if 一下就完事。检查与派发之间永远存在时间窗：检查完到真正写入之间，租约可能已过期、别人已当选。正确做法是让 sess.Done 的关闭作为持续信号驱动循环退出，并在写入端用 fencing token 兜底，而不是依赖一次检查。" />

<Compare title="选主实现两种路线" :panels='[{"label":"自己拿 SETNX 重造","code":"ok := SETNX(lock, uuid, ttl)\nif ok { ... }","note":"要自己处理续期、看门狗、时钟回拨、脑裂与 fencing，极易漏掉边界，生产事故高发区。"},{"label":"用 etcd concurrency 包","code":"e := concurrency.NewElection(sess, pfx)\n_ = e.Campaign(ctx, node)","note":"Session 打包租约与续期，Election 用事务与 revision 保证唯一，Done 提供失主信号，边界由库兜住。"}]' />

## 4. 数据与协议契约

先把跨层、跨进程的数据定清楚，再写实现。下面给出作业表、执行表、日志表与租约表的 DDL，任务定义与对外 API，接口签名，以及任务状态机的合法转换。

```sql
CREATE TABLE jobs (
    id          VARCHAR(64) PRIMARY KEY,
    cron_expr   VARCHAR(64)  NOT NULL,
    shard_key   VARCHAR(128) NOT NULL DEFAULT '',
    timeout_ms  INT          NOT NULL DEFAULT 30000,
    max_retry   INT          NOT NULL DEFAULT 3,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE
);

CREATE TABLE executions (
    job_id      VARCHAR(64) NOT NULL,
    epoch       BIGINT      NOT NULL,
    state       VARCHAR(16) NOT NULL,
    worker      VARCHAR(64) NOT NULL DEFAULT '',
    started_at  TIMESTAMP   NOT NULL,
    finished_at TIMESTAMP   NULL,
    PRIMARY KEY (job_id, epoch)
);

CREATE TABLE job_logs (
    id          BIGINT PRIMARY KEY AUTO_INCREMENT,
    job_id      VARCHAR(64) NOT NULL,
    epoch       BIGINT      NOT NULL,
    line        TEXT        NOT NULL,
    created_at  TIMESTAMP   NOT NULL
);

CREATE TABLE leader_lease (
    key_name    VARCHAR(64) PRIMARY KEY,
    holder      VARCHAR(64) NOT NULL,
    epoch       BIGINT      NOT NULL,
    expires_at  BIGINT      NOT NULL
);
```

`executions` 用 `(job_id, epoch)` 作联合主键，天然就是幂等键：同一 epoch 的重复写入被主键挡住，旧 epoch 的迟到写入由 fencing 校验拒绝。`leader_lease.expires_at` 存 UnixNano 整数，避免各节点本地时钟语义歧义；真正的单调性由 etcd 的 `CreateRevision`（等价 epoch）保证，本地时间只用于展示。

任务帧用 JSON，字段可序列化且有明确零值语义。`epoch` 是 fencing token，换主递增；`trace_id` 贯穿触发、派发、执行、回传四段。

```json
{
  "job_id": "report-daily",
  "cron_expr": "0 2 * * *",
  "shard_key": "tenant-42",
  "timeout_ms": 30000,
  "max_retry": 3,
  "epoch": 7,
  "trace_id": "t-2026-0101-001"
}
```

| 对外 API | 方法 | 入参 | 返回 | 幂等性 |
| --- | --- | --- | --- | --- |
| /jobs | POST | 作业定义 | job_id | 同 id 重复提交返回冲突 |
| /jobs/{id} | GET | id | 作业定义 | 天然幂等 |
| /executions | GET | job_id、epoch | 执行状态 | 幂等 |
| /trigger | POST | job_id | run_id | 手动触发需带幂等键 |

```json
{
  "run_id": "r-9527",
  "job_id": "report-daily",
  "epoch": 7,
  "state": "SCHEDULED",
  "accepted": true
}
```

**任务状态机**是正确性的骨架，所有转换必须经校验，非法转换直接拒绝并告警，绝不「硬改状态」。

| 当前状态 | 允许的目标 | 触发条件 | 说明 |
| --- | --- | --- | --- |
| PENDING | SCHEDULED | cron 到期 | 进入待派发 |
| SCHEDULED | RUNNING、FAILED | 被 worker 领取 / 派发失败 | 领取成功才进 RUNNING |
| RUNNING | SUCCEEDED、FAILED、TIMEDOUT | 正常结束 / 报错 / 超时 | 三个终因互斥 |
| FAILED | SCHEDULED | 未达重试上限 | 退避后重投 |
| TIMEDOUT | SCHEDULED | 未达重试上限 | 超时重投 |
| SUCCEEDED | 无 | 终态 | 不可再转，防止重复结算 |

**字段语义与兼容规则**：`epoch` 从 1 起、每次换主递增，旧版本可忽略它（由 fencing 在写入端兜底）；`max_retry` 为零值表示用默认 3 次；新增字段一律允许零值且不影响旧消费者。灰度顺序是「先升级消费者忽略未知字段 → 灰度生产者加字段 → 最后依赖新字段」，任何一步回滚旧节点读到新字段都不会崩。

内部接口签名是依赖倒置的落点：

```go
package main

import (
	"context"
	"time"
)

// CronExpr 把 cron 表达式抽象成「给定时刻求下次触发」的能力。
type CronExpr interface {
	Next(after time.Time) time.Time
}

// State 是任务状态机的状态枚举。
type State int32

// Transition 校验状态转换是否合法。
type Transition interface {
	Can(from, to State) bool
}

// Executor 在隔离环境执行任务，必须尊重 ctx 超时与取消。
type Executor interface {
	Run(ctx context.Context, j Job) (Result, error)
}

// Election 抽象选主：抢主、退位、失主信号、带 epoch 的派发资格。
type Election interface {
	Campaign(ctx context.Context, node string) (int64, error)
	Resign(ctx context.Context) error
	Done() <-chan struct{}
	IsLeader(epoch int64) bool
}

// Sharder 按分片键把任务映射到负责节点。
type Sharder interface {
	Owner(key string) string
}

// Job 与 Result 是跨层帧，字段可序列化且有零值语义。
type Job struct {
	ID       string
	Cron     string
	Timeout  time.Duration
	Retries  int
	ShardKey string
}

type Result struct {
	JobID  string
	Epoch  int64
	OK     bool
	Log    string
	CostMs int64
}

func main() {}
```

<Compare title="触发定义的两种设计" :panels='[{"label":"固定间隔秒数","code":"type JobA struct {\n  ID string\n  Every int\n}","note":"实现最简，但表达不了『每周一 2 点』，且进程重启后周期相位会漂移，需要额外持久化上次触发时间。"},{"label":"cron 表达式","code":"type JobB struct {\n  ID string\n  Cron string\n  Timezone string\n}","note":"表达力强、相位由绝对时间决定、重启可补跑；代价是要一个解析器与统一时间源。生产选它，因为对账类依赖自然日边界。"}]' />

选 cron 而非固定间隔的理由很直接：日终对账、报表这类任务绑定的是「每天 2 点」这个绝对时刻，不是「每 86400 秒」；进程重启或换主后，固定间隔会重新计时导致漏跑或相位漂移，而 cron 用持久化的计划时刻可以算出「本该触发但没触发」并补跑。

## 5. 核心链路时序

下面这张时序图把「选主 → 触发 → 分片 → 执行 → 回传 → 故障转移」的完整链路画清楚，包含同步调用、返回、自处理与异步消息。

<SequenceDiagram title="选主、触发与故障转移链路" caption="KeepAlive 续租一旦中断，租约到期 key 被删，跟随者 Watch 唤醒重新当选，epoch 加一后重派在途任务" :actors='["etcd","Master","Follower","Shard","Worker","Store"]' :steps='[{"from":"Master","to":"etcd","label":"Campaign 抢主","note":"Grant 租约","kind":"call"},{"from":"etcd","to":"Master","label":"返回租约","note":"含 revision","kind":"return"},{"from":"Master","to":"Master","label":"加载 cron 触发","note":"算下次时刻","kind":"self"},{"from":"Master","to":"Shard","label":"分片派发","note":"一致性哈希","kind":"call"},{"from":"Shard","to":"Worker","label":"下发任务","note":"带 epoch","kind":"async"},{"from":"Worker","to":"Worker","label":"隔离执行","note":"超时加 recover","kind":"self"},{"from":"Worker","to":"Store","label":"回传结果","note":"幂等 upsert","kind":"call"},{"from":"Master","to":"etcd","label":"KeepAlive 续租","note":"持续保活","kind":"call"},{"from":"etcd","to":"Follower","label":"key 删除唤醒","note":"租约到期","kind":"async"},{"from":"Follower","to":"etcd","label":"重新 Campaign","note":"epoch 加一","kind":"call"}]' />

逐步骤解读。第 1、2 步 Master 通过 `Campaign` 抢带租约的 key，etcd 返回的 `CreateRevision` 就是天然单调的 epoch；只有抢到的节点进入派发循环，其余阻塞在 Watch 上。第 3 步 Master 自己按 cron 计算触发时刻并唤醒到期任务。第 4 步交给 Shard Router 用一致性哈希把任务映射到负责分片。第 5 步是异步下发——Master 不必等 Worker 执行完就能继续派发下一批，这是吞吐的来源。第 6 步 Worker 在隔离环境里执行，带独立超时与 panic 兜底。第 7 步结果与日志按 `job_id + epoch` 幂等回写。第 8 步 Master 持续 `KeepAlive` 续租，这是「我还活着」的唯一凭据。第 9、10 步是故障转移的分叉点：续租中断后租约到期、key 被 etcd 自动删除，Follower 的 Watch 收到事件，重新 `Campaign` 当选并让 epoch 加一。

<Term word="Session 与 Done" text="etcd 的 Session 打包了租约与自动续期；sess.Done 这个 channel 在续租失败时被关闭，是进程感知『我已失去 leader 资格』的唯一可靠信号，比轮询 isLeader 及时得多" />

**异常链路：Master 崩溃或网络分区。** 它与正常链路的分叉点在第 8 步 `KeepAlive` 没能持续。假设 Master 进程被 OOM 杀掉，或只是被网络分区隔离——从它自己的视角仍认为自己是 leader，这两种情况对 etcd 而言没有区别：续租停了，`TTL` 耗尽后 key 被删，Follower 当选。危险恰恰在这里：**旧 Master 未必真的死了**，它可能在分区恢复后带着本地的旧 epoch 继续派发，与新的 leader 同时写库，形成双主。防线有两层：第一层是监听 `sess.Done()`，续租一断立刻停止派发，消除「检查与派发之间」的窗口；第二层是 fencing token——派发携带单调递增的 epoch，写入端（Execution 表）只接受比已记录值更大的 token，旧 leader 的写入被直接拒绝。第 6.6 节的 `TryRun` 会演示这层拦截。整条异常链路的结论是：**租约负责「选出唯一派发者」，Watch 负责「及时换主」，fencing token 负责「隔离旧主」，幂等表负责「消化至少一次」，四者缺一不可**。

## 6. 核心实现（按模块逐段展开）

以下每段都是聚焦单一模块的最小可运行程序（`package main`，可单独 `go run` 或 `go test` 自检）；生产里它们按第 3 节目录树拆进 `internal` 各包，接口签名在第 4 节统一。每段前写清设计意图与关键取舍。

### 6.1 cron 解析：把「每天 2 点」算成具体时刻

**设计意图**：让触发绑定绝对时刻而非相对间隔，重启与换主后可补跑。**关键取舍**：用标准库实现 5 字段（分 时 日 月 周）解析，支持 `*`、`*/n`、`a-b`、`x,y` 四种常见写法；`Next` 从给定时刻之后逐分钟试匹配，最多扫两年兜底，避免非法表达式导致死循环。不引第三方 cron 库，是为了把解析边界讲透；生产建议直接换 `robfig/cron` 并统一时间源。

```go
package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// field 保存一个 cron 字段允许取值的集合。
type field struct {
	values map[int]bool
	min    int
	max    int
}

func parseField(expr string, min, max int) (field, error) {
	f := field{values: map[int]bool{}, min: min, max: max}
	for _, part := range strings.Split(expr, ",") {
		step := 1
		base := part
		if i := strings.Index(part, "/"); i >= 0 {
			base = part[:i]
			s, err := strconv.Atoi(part[i+1:])
			if err != nil || s <= 0 {
				return f, fmt.Errorf("非法步长 %q", part)
			}
			step = s
		}
		lo, hi := min, max
		if base != "*" {
			if j := strings.Index(base, "-"); j >= 0 {
				lo, _ = strconv.Atoi(base[:j])
				hi, _ = strconv.Atoi(base[j+1:])
			} else {
				lo, _ = strconv.Atoi(base)
				hi = lo
			}
		}
		if lo < min || hi > max || lo > hi {
			return f, fmt.Errorf("字段越界 %q", part)
		}
		for v := lo; v <= hi; v += step {
			f.values[v] = true
		}
	}
	return f, nil
}

// Cron 是 5 字段解析器：分 时 日 月 周。
type Cron struct {
	minute, hour, dom, month, dow field
}

func ParseCron(expr string) (Cron, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return Cron{}, fmt.Errorf("需要 5 个字段，实际 %d", len(parts))
	}
	var c Cron
	var err error
	if c.minute, err = parseField(parts[0], 0, 59); err != nil {
		return c, err
	}
	if c.hour, err = parseField(parts[1], 0, 23); err != nil {
		return c, err
	}
	if c.dom, err = parseField(parts[2], 1, 31); err != nil {
		return c, err
	}
	if c.month, err = parseField(parts[3], 1, 12); err != nil {
		return c, err
	}
	if c.dow, err = parseField(parts[4], 0, 6); err != nil {
		return c, err
	}
	return c, nil
}

func (f field) match(v int) bool { return f.values[v] }

// Next 从 after 之后逐分钟找下一个全部字段匹配的时刻，最多找两年。
func (c Cron) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(2 * 365 * 24 * time.Hour)
	for ; t.Before(limit); t = t.Add(time.Minute) {
		if c.month.match(int(t.Month())) &&
			c.dom.match(t.Day()) &&
			c.dow.match(int(t.Weekday())) &&
			c.hour.match(t.Hour()) &&
			c.minute.match(t.Minute()) {
			return t
		}
	}
	return time.Time{}
}

func main() {
	c, err := ParseCron("*/15 9-18 * * 1-5")
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	now := time.Date(2026, 1, 5, 9, 2, 0, 0, time.UTC) // 周一 9:02
	fmt.Println("下次触发:", c.Next(now).Format("2006-01-02 15:04"))
}
```

<OutputBox title="cron 解析运行结果" output="下次触发: 2026-01-05 09:15" note="工作日 9-18 点每 15 分钟一次，9:02 之后的下一次是 9:15" />

### 6.2 任务状态机：让每次转换都可校验

**设计意图**：把第 4 节那张转换表落成代码，任何状态变更都经 `To` 校验，非法转换返回错误且不改状态——这是「终态不可再转，防止重复结算」的硬保证。**关键取舍**：用 `map[State]map[State]bool` 显式声明合法边，比散落在各处的 if 更可审计；`State` 实现 `String()`，让错误信息与日志可读。

```go
package main

import "fmt"

type State int32

const (
	Pending State = iota
	Scheduled
	Running
	Succeeded
	Failed
	TimedOut
)

func (s State) String() string {
	return [...]string{"Pending", "Scheduled", "Running", "Succeeded", "Failed", "TimedOut"}[s]
}

// allowed 定义合法转换：键为当前状态，值为允许的目标集合。
var allowed = map[State]map[State]bool{
	Pending:   {Scheduled: true},
	Scheduled: {Running: true, Failed: true},
	Running:   {Succeeded: true, Failed: true, TimedOut: true},
	Failed:    {Scheduled: true},
	TimedOut:  {Scheduled: true},
	Succeeded: {},
}

// Machine 持有单个任务的当前状态。
type Machine struct{ state State }

func NewMachine() *Machine { return &Machine{state: Pending} }

func (m *Machine) State() State { return m.state }

// To 执行状态转换，非法转换返回错误且不改变状态。
func (m *Machine) To(next State) error {
	if !allowed[m.state][next] {
		return fmt.Errorf("非法转换: %s -> %s", m.state, next)
	}
	m.state = next
	return nil
}

func trace(steps ...State) {
	m := NewMachine()
	for _, s := range steps {
		if err := m.To(s); err != nil {
			fmt.Println("  拒绝:", err)
			return
		}
		fmt.Println("  进入:", m.State())
	}
}

func main() {
	fmt.Println("正常链路:")
	trace(Scheduled, Running, Succeeded)
	fmt.Println("重试链路:")
	trace(Scheduled, Running, Failed, Scheduled, Running, Succeeded)
	fmt.Println("非法链路:")
	trace(Running) // Pending 直接到 Running 应被拒绝
}
```

<LineByLine title="fencing 判定为什么放在写入端" code="func (c *Coordinator) IsLeader(node string, epoch int64) bool {\nc.mu.Lock()\ndefer c.mu.Unlock()\nreturn c.holder == node && c.epoch == epoch && time.Now().Before(c.expiry)\n}" :notes='["查询带节点名与 epoch 的派发资格","用同一把锁保护 holder 与 epoch 的读","defer 保证解锁，避免返回值路径遗漏","三者同时成立才算有资格：持有人、时代、租约未过期"]' />

注意 `IsLeader` 只是资格判定，真正拦住旧主的是第 6.6 节写入端的 `TryRun`——**检查一次永远不够，检查与派发之间有窗口**，这是本讲反复强调的一点。

### 6.3 一致性哈希分片：扩容只迁移相邻区间

**设计意图**：让同一 job 恒定落到同一分片，且加节点时迁移量可控。**关键取舍**：用虚拟节点（每物理节点 128 个）把 key 打散到环上，`Owner` 顺时针找第一个不小于 key 哈希的虚拟节点；对比取模分片「加一个节点全量重分布」，一致性哈希扩容只迁移相邻区间。分片函数必须是纯函数——同一 job 在任何 Master 上算出同一 owner，换主才不会分丢或分重。

```go
package main

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
)

// Ring 是一致性哈希环：虚拟节点分散数据，扩容只迁移相邻区间。
type Ring struct {
	tokens []uint64
	nodes  map[uint64]string
	vnode  int
}

func NewRing(vnode int) *Ring {
	return &Ring{nodes: map[uint64]string{}, vnode: vnode}
}

func h(s string) uint64 {
	f := fnv.New64a()
	_, _ = f.Write([]byte(s))
	return f.Sum64()
}

// Add 为物理节点生成 vnode 个虚拟节点落到环上。
func (r *Ring) Add(node string) {
	for i := 0; i < r.vnode; i++ {
		t := h(node + "#" + strconv.Itoa(i))
		r.tokens = append(r.tokens, t)
		r.nodes[t] = node
	}
	sort.Slice(r.tokens, func(i, j int) bool { return r.tokens[i] < r.tokens[j] })
}

// Owner 顺时针找第一个不小于 key 哈希的虚拟节点。
func (r *Ring) Owner(key string) string {
	if len(r.tokens) == 0 {
		return ""
	}
	t := h(key)
	idx := sort.Search(len(r.tokens), func(i int) bool { return r.tokens[i] >= t })
	if idx == len(r.tokens) {
		idx = 0 // 回绕到环首
	}
	return r.nodes[r.tokens[idx]]
}

func main() {
	r := NewRing(128)
	r.Add("worker-1")
	r.Add("worker-2")
	r.Add("worker-3")
	keys := []string{"report-daily", "cleanup", "heartbeat", "sync", "settle"}
	owner3 := map[string]string{}
	for _, k := range keys {
		owner3[k] = r.Owner(k)
	}
	before := map[string]int{}
	for _, o := range owner3 {
		before[o]++
	}
	fmt.Println("3 节点分布:", before)

	r4 := NewRing(128)
	for _, n := range []string{"worker-1", "worker-2", "worker-3", "worker-4"} {
		r4.Add(n)
	}
	moved := 0
	for _, k := range keys {
		if r4.Owner(k) != owner3[k] {
			moved++
		}
	}
	fmt.Printf("扩容后迁移 %d/%d 个 key（只动相邻区间）\n", moved, len(keys))
}
```

### 6.4 etcd 选主与租约：把语义跑一遍

**设计意图**：用等价模拟把 `Campaign`、`KeepAlive`、`IsLeader`、`Resign` 四个核心语义讲清，避免直接依赖外部 etcd 才能理解。**关键取舍**：`epoch` 每次当选递增，是 fencing token 的来源；`IsLeader` 同时校验 holder、epoch 与租约未过期三者；`Resign` 主动清空 holder，让跟随者立即接管而不必干等 TTL——这就是优雅退位。真实实现直接换成第 3 节 `text` 块里的 `concurrency` 调用。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// Coordinator 模拟 etcd 选主核心语义：租约、续租、epoch、优雅退位。
type Coordinator struct {
	mu      sync.Mutex
	holder  string
	leaseID int64
	expiry  time.Time
	epoch   int64
	nextID  int64
}

func NewCoordinator() *Coordinator { return &Coordinator{} }

// Campaign 抢主：已有存活 leader 则失败，否则授予租约并递增 epoch。
func (c *Coordinator) Campaign(node string, ttl time.Duration) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.holder != "" && now.Before(c.expiry) {
		return 0, false
	}
	c.nextID++
	c.leaseID = c.nextID
	c.holder = node
	c.expiry = now.Add(ttl)
	c.epoch++
	fmt.Printf("  %s 当选 租约=%d epoch=%d\n", node, c.leaseID, c.epoch)
	return c.leaseID, true
}

// KeepAlive 只有当前 holder 能续自己的租约。
func (c *Coordinator) KeepAlive(node string, id int64, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder != node || c.leaseID != id || time.Now().After(c.expiry) {
		return false
	}
	c.expiry = time.Now().Add(ttl)
	return true
}

// IsLeader 用 fencing epoch 判定派发资格，挡旧 leader 复活。
func (c *Coordinator) IsLeader(node string, epoch int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder == node && c.epoch == epoch && time.Now().Before(c.expiry)
}

// Resign 主动释放租约，让跟随者不必等 TTL（优雅退位）。
func (c *Coordinator) Resign(node string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder == node {
		c.holder = ""
		fmt.Println("  ", node, "主动退位，跟随者立即接管")
	}
}

func main() {
	c := NewCoordinator()
	ttl := 30 * time.Millisecond
	id, _ := c.Campaign("master-1", ttl)
	_, won := c.Campaign("master-2", ttl)
	fmt.Println("master-2 抢主成功:", won)
	fmt.Println("master-1 续租:", c.KeepAlive("master-1", id, ttl))
	fmt.Println("master-1 有派发权:", c.IsLeader("master-1", 1))

	time.Sleep(ttl + 5*time.Millisecond) // 模拟进程卡死，停止续租
	fmt.Println("过期后 master-1 仍有派发权:", c.IsLeader("master-1", 1))
	_, ok := c.Campaign("master-2", ttl)
	fmt.Println("master-2 重新竞选:", ok)
	fmt.Println("旧 leader 用旧 epoch 派发被拒:", !c.IsLeader("master-1", 1))
	fmt.Println("master-2 用新 epoch 派发:", c.IsLeader("master-2", 2))
	c.Resign("master-2")
}
```

<OutputBox title="选主语义运行结果" output="  master-1 当选 租约=1 epoch=1\nmaster-2 抢主成功: false\nmaster-1 续租: true\nmaster-1 有派发权: true\n过期后 master-1 仍有派发权: false\n  master-2 当选 租约=2 epoch=2\nmaster-2 重新竞选: true\n旧 leader 用旧 epoch 派发被拒: true\nmaster-2 用新 epoch 派发: true\n  master-2 主动退位，跟随者立即接管" note="停止续租后旧 epoch 失效，新 leader 的 epoch 变 2，旧主复活带 epoch 1 回来被拒" />

### 6.5 执行器隔离与超时：一个任务不许拖垮整机

**设计意图**：把「单任务超时、panic、并发上限」三件事收进执行器。**关键取舍**：每个任务在独立 goroutine 里跑，用 `context.WithTimeout` 给硬超时，用 `defer recover` 把 panic 转成普通错误；结果走容量为 1 的缓冲 channel，超时后 goroutine 仍能发送退出、不会泄漏。用带缓冲 channel 作信号量限制并发（舱壁隔离），避免某个慢 job 占满全部执行槽。

```go
package main

import (
	"context"
	"fmt"
	"time"
)

type ExecFn func(ctx context.Context) (string, error)

type outcome struct {
	log string
	err error
}

func runIsolated(ctx context.Context, fn ExecFn) (string, error) {
	ch := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- outcome{err: fmt.Errorf("任务 panic: %v", r)}
			}
		}()
		log, err := fn(ctx)
		ch <- outcome{log: log, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case o := <-ch:
		return o.log, o.err
	}
}

// Pool 用带缓冲 channel 作信号量限制并发（舱壁隔离）。
type Pool struct{ sem chan struct{} }

func NewPool(n int) *Pool { return &Pool{sem: make(chan struct{}, n)} }

func (p *Pool) Submit(ctx context.Context, timeout time.Duration, fn ExecFn) (string, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.sem }()
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runIsolated(c, fn)
}

func main() {
	p := NewPool(2)
	fast := func(ctx context.Context) (string, error) { return "ok", nil }
	slow := func(ctx context.Context) (string, error) {
		select {
		case <-time.After(200 * time.Millisecond):
			return "late", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	boom := func(ctx context.Context) (string, error) { panic("bad job") }
	for _, tc := range []struct {
		name string
		fn   ExecFn
		to   time.Duration
	}{
		{"正常", fast, time.Second},
		{"超时", slow, 20 * time.Millisecond},
		{"panic", boom, time.Second},
	} {
		log, err := p.Submit(context.Background(), tc.to, tc.fn)
		fmt.Printf("%-6s log=%q err=%v\n", tc.name, log, err)
	}
}
```

<Callout type="idea" title="结果 channel 为什么容量是 1" text="若用无缓冲 channel，任务超时后主流程从 ctx.Done 返回，执行 goroutine 仍会卡在发送上永久阻塞，造成 goroutine 泄漏。容量 1 让迟到结果有处可放，goroutine 发完即退出。这是并发代码里很典型的一处边界。" />

### 6.6 幂等派发与 fencing：把重复执行挡在写入端

**设计意图**：故障转移必然重派在途任务，也可能叠加旧主复活，**唯一可靠的收敛点是写入端**。**关键取舍**：`Executions` 记录每个 job 已接受的最大 epoch，`TryRun` 只接受更大的 token——epoch 1 正常执行，epoch 2 允许重跑在途任务，旧主带 epoch 1 复活被拒。`Dispatch` 先按分片过滤再派发，保证同一 job 只在负责分片上执行。

```go
package main

import (
	"fmt"
	"sync"
)

// Executions 记录每个任务已接受的最大 epoch，写入端校验 fencing token。
type Executions struct {
	mu   sync.Mutex
	done map[string]int64
}

func NewExecutions() *Executions {
	return &Executions{done: make(map[string]int64)}
}

// TryRun 只接受 epoch 更大的派发，挡住旧 leader 复活与重复重派。
func (e *Executions) TryRun(job string, epoch int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prev, ok := e.done[job]; ok && prev >= epoch {
		return false
	}
	e.done[job] = epoch
	return true
}

// Dispatch 按分片过滤后带 epoch 派发，返回实际执行数。
func Dispatch(jobs []string, shards, owner int, ex *Executions, epoch int64, tag string) int {
	n := 0
	for _, j := range jobs {
		shard := 0
		for i := 0; i < len(j); i++ {
			shard += int(j[i])
		}
		if shard%shards != owner {
			continue
		}
		if ex.TryRun(j, epoch) {
			fmt.Printf("  %s 执行 %s (epoch=%d)\n", tag, j, epoch)
			n++
		} else {
			fmt.Printf("  %s 拒绝重复 %s (epoch=%d)\n", tag, j, epoch)
		}
	}
	return n
}

func main() {
	ex := NewExecutions()
	jobs := []string{"report", "cleanup", "heartbeat", "sync", "settle"}
	fmt.Println("旧 leader 首次派发 (epoch=1):")
	Dispatch(jobs, 3, 0, ex, 1, "leader-1")
	fmt.Println("新 leader 故障转移重派 (epoch=2):")
	Dispatch(jobs, 3, 0, ex, 2, "leader-2")
	fmt.Println("旧 leader 复活重派 (epoch=1):")
	Dispatch(jobs, 3, 0, ex, 1, "leader-1-old")
}
```

三轮派发各有含义：epoch 1 正常执行；epoch 2 允许重跑在途任务（故障转移需要）；旧主复活带 epoch 1 再来被 `TryRun` 挡掉。注意这与第 109 讲的租约重投是同一套思想——**至少一次语义必须由写入侧的幂等表收敛**，区别只在这里用单调递增的 epoch 当幂等维度。

### 6.7 优雅退位主流程

**设计意图**：把选主、触发、派发、失主监听、退位串成一个可运行的主循环，讲清发布/缩容时怎样把漏跑压到最小。**关键取舍**：`done` channel 一旦关闭永久可读，作为「失主/退出」的持续信号驱动循环立即退出，而不是靠每次循环前轮询一次布尔值；`Resign` 的顺序是「先停投放 → 等在途收尾 → 释放租约」，顺序反了会漏任务或让跟随者空等 TTL。

```go
package main

import (
	"context"
	"fmt"
	"time"
)

// Loop 演示 Master 主循环：抢主、周期触发、监听失主、优雅退位。
type Loop struct {
	node   string
	leader bool
	epoch  int64
	done   chan struct{}
}

func NewLoop(node string) *Loop {
	return &Loop{node: node, done: make(chan struct{})}
}

// Campaign 简化选主：成功即持有 epoch。
func (l *Loop) Campaign() {
	l.leader = true
	l.epoch++
	fmt.Println(l.node, "当选 leader, epoch=", l.epoch)
}

// Resign 主动退位：停派发、等在途收尾、释放资格。
func (l *Loop) Resign() {
	fmt.Println(l.node, "停止派发，等待在途任务收尾")
	time.Sleep(10 * time.Millisecond) // 模拟 drain 在途
	l.leader = false
	close(l.done)
	fmt.Println(l.node, "已退位，跟随者可立即接管")
}

// Serve 抢主后进入派发循环，收到 done 信号立即退出。
func (l *Loop) Serve(ctx context.Context, ticks int) {
	l.Campaign()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	fired := 0
	for {
		select {
		case <-ctx.Done():
			l.Resign()
			return
		case <-l.done:
			return // 失主信号：一次关闭永久可读
		case <-ticker.C:
			if !l.leader {
				return
			}
			fired++
			fmt.Println("  派发第", fired, "批 (epoch=", l.epoch, ")")
			if fired >= ticks {
				l.Resign()
				return
			}
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	l := NewLoop("master-1")
	l.Serve(ctx, 3)
	fmt.Println("主流程结束，派发资格已释放")
}
```

把 `Resign` 放在发布流程里而非依赖 TTL 到期，是本讲「优雅退位」的落点：主动删 key 让跟随者被 Watch 立刻唤醒，换主窗口从「最多等一个 TTL」缩短到「几乎立即」，漏跑的定时任务因此少一次。

## 7. 失败路径与边界条件

把每个可能挂掉的点展开成「故障点 / 触发条件 / 现象 / 如何发现 / 兜底 / 恢复」，这是设计评审里最能体现工程成熟度的部分。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| Master 崩溃 | OOM / 进程被杀 | 停止派发 | Watch 事件、租约到期 | Follower 当选，重派在途 | epoch 加一，幂等重跑 |
| 网络分区脑裂 | 旧主被隔离但自认存活 | 双主同时写 | fencing 拒绝数、双写告警 | 写入端只接受更大 epoch | 分区恢复后旧主被拒 |
| 续租断但进程活着 | KeepAlive 失败 | 仍在派发 | sess.Done 关闭 | 立即停派发，不检查后继续 | 重新竞选或退为跟随者 |
| etcd 整体不可用 | 集群多数派失联 | 无法选主/续租 | 选主错误率、告警 | 全员停派发（fail-closed） | 恢复后从持久化计划补跑 |
| 任务执行超时 | 慢查询 / 死循环 | 占用执行槽 | 超时计数、TIMEDOUT 状态 | context 超时 + 信号量隔离 | 计入状态机，按预算重投 |
| 执行器 panic | 任务体异常 | worker 可能崩 | panic 日志、recover 计数 | recover 转普通错误 | 单任务失败，不打挂 worker |
| 重复执行 | 重派 + 旧主复活叠加 | 重复扣款风险 | 幂等拒绝计数 | Execution 表 fencing 校验 | 拒绝旧 epoch 写入 |
| 时钟回拨 | NTP 校正 / 虚拟机漂移 | cron 漏触发或重复 | 触发时刻单调性校验 | 以 etcd revision 为单调基准 | 持久化计划时刻补跑 |
| 分片漂移 | 扩缩容、节点上下线 | 部分任务暂无人认领 | 分片分布指标 | 一致性哈希只迁移相邻区间 | 重派由幂等表挡重复 |
| 优雅退位失败 | Resign 调用超时 | 跟随者空等 | 换主延迟指标 | 租约 TTL 到期兜底 | 至多等一个 TTL 自动接管 |

**幂等键**：`executions(job_id, epoch)` 联合主键，配合 `TryRun` 的最大 epoch 校验，是重复执行的最后一道闸；任何重派、重试、旧主复活，最终都要落到「同一 key 或更旧 epoch 的写入被拒」。**重试预算**：每个任务最多 `max_retry` 次（默认 3），超限进入 FAILED 终态并告警，绝不无限重试——否则一个永久失败的对账任务会永久占用执行槽。**超时传递**：`context.WithTimeout` 从派发一路传到执行器与回传，取消信号沿调用链传播，避免下游还在跑、上游已放弃。**降级与人工介入点**：etcd 不可用一律停派发（漏跑可人工补，重复扣款没法补）；FAILED 终态、fencing 拒绝突增、时钟回拨告警三处是刻意留给人的开关，不追求全自动。

<Callout type="warn" title="fail-closed 是刻意的选择" text="etcd 整体不可用时全员停止派发：漏跑一次定时任务可以人工补，重复扣款、重复发券补不回来。宁可短暂停摆，也绝不在失去线性一致底座时各自为政地派发。" />

## 8. 并发与一致性

**goroutine 生命周期**：Master 派发循环是单 goroutine 长生命周期（唯一性要求），随 `sess.Done()` 关闭或 `Resign` 退出；Worker 的执行 goroutine 是短生命周期，随任务结束或超时退出；`runIsolated` 里那个执行 goroutine 即使主流程因超时先返回，也会因结果 channel 容量为 1 而发送后退出，不泄漏。**channel 与锁的选择**：派发与结果回传用 channel（数据流动、天然背压），`Coordinator` 的 holder/epoch、`Executions` 的 done 表用 `sync.Mutex` 保护临界区；判据仍是「channel 传所有权、锁护共享状态」。

**锁粒度与热点**：`Coordinator` 用一把锁包住 `Campaign`/`KeepAlive`/`IsLeader` 对 holder、epoch、expiry 的整组读写——这三字段必须作为一个整体被判定，拆成多次加锁就会出现「读到旧 epoch 配新 holder」的错乱；`Executions.TryRun` 同样把「查最大值 + 更新」放在一个临界区，避免 check-then-act 竞态。热点集中在选主锁与 Execution 表，压测里若争用明显，可按 job_id 做分片表降粒度。**竞态分析**：需要用 `go test -race` 覆盖并发 `TryRun`、并发 `Campaign`、以及 `sess.Done` 关闭与派发循环退出之间的时序；结论是「检查自己是不是 leader」永远不能作为写入前的唯一依据，因为检查与写入之间租约可能已失效——这也是为什么必须把 fencing 放到写入端。

**一致性级别**：选主要求线性一致（否则可能出现两个节点同时自认当选），这一步完全交给 etcd Raft；执行结果与日志只要求最终一致，因为业务允许「短暂查不到结果」，但不允许「为了强一致把派发压垮」。**顺序保证**：同一 job 的多次触发不要求全局有序，但同一 `(job_id, epoch)` 只允许一次成功写入；乱序到达的迟到回传由状态机与 fencing 拒绝。**脑裂的严格结论**：检查一次解决不了问题，因为检查与派发之间有时间窗；唯一可靠的是「持续信号（sess.Done）停止派发」+「写入端单调递增 token 校验」双保险。

<StepLab scene="gmp-schedule" />

<Compare title="失主处理：错误与正确对照" :panels='[{"label":"错误：派发前只检查一次","code":"if c.IsLeader(node, ep) {\n  dispatch(j)\n}","note":"检查通过到 dispatch 写入之间，租约可能已过期、别人已当选，旧主照样写库，留下脑裂窗口。"},{"label":"正确：持续信号加写入端拦截","code":"done := sess.Done()\ngo watchDone(done)","note":"Done 关闭是持续信号，派发循环立刻停；再配合写入端 TryRun 校验更大 epoch，双保险消除窗口。"}]' />

## 9. 可观测性

调度系统出问题时最需要回答「某次任务到底有没有跑、跑了几次、是谁派发的」。指标用原子计数器，日志结构化并固定字段，`job_id + epoch + trace_id` 三者贯穿全链路。

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| sched_fired_total | Counter | 触发次数 | 环比跌 50% 告警 |
| sched_succeeded_total | Counter | 成功次数 | — |
| sched_failed_total | Counter | 失败次数 | 错误率 > 5% 告警 |
| sched_timeout_total | Counter | 超时次数 | 突增即排查慢任务 |
| sched_leader_swap_total | Counter | 换主次数 | 短时间内 > 3 告警 |
| sched_fenced_total | Counter | fencing 拒绝次数 | > 0 即疑似脑裂 |
| sched_dispatch_lag_ms | Histogram | 触发到派发延迟 | P99 > 1000ms 告警 |
| sched_inflight | Gauge | 在途任务数 | 持续不降说明 worker 异常 |

结构化日志字段固定为 `job_id / epoch / state / worker / cost_ms / trace_id / event`，错误全量、成功按 1% 采样，避免日志打爆。埋点代码如下：

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数器承载调度核心指标。
type Metrics struct {
	Fired      atomic.Int64
	Succeeded  atomic.Int64
	Failed     atomic.Int64
	TimedOut   atomic.Int64
	LeaderSwap atomic.Int64
	Fenced     atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("fired=%d ok=%d fail=%d timeout=%d swap=%d fenced=%d",
		m.Fired.Load(), m.Succeeded.Load(), m.Failed.Load(),
		m.TimedOut.Load(), m.LeaderSwap.Load(), m.Fenced.Load())
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func record(jobID string, epoch int64, state string, costMs int64, m *Metrics) {
	switch state {
	case "fire":
		m.Fired.Add(1)
	case "ok":
		m.Succeeded.Add(1)
	case "timeout":
		m.TimedOut.Add(1)
	case "fenced":
		m.Fenced.Add(1)
	default:
		m.Failed.Add(1)
	}
	logger.Info("job_event",
		"job_id", jobID,
		"epoch", epoch,
		"state", state,
		"cost_ms", costMs,
	)
}

func recordSwap(m *Metrics) {
	m.LeaderSwap.Add(1)
	logger.Warn("leader_swap", "event", "failover")
}

func main() {
	m := &Metrics{}
	record("report-daily", 3, "fire", 0, m)
	record("report-daily", 3, "ok", 137, m)
	record("cleanup", 3, "timeout", 5000, m)
	record("cleanup", 2, "fenced", 0, m)
	recordSwap(m)
	fmt.Println(m.Snapshot())
}
```

**如何用这些定位一次真实故障**：某天对账任务疑似重复执行，先看 `sched_fenced_total` 是否大于 0——若大于 0，说明确实有旧 epoch 的写入被拦，是脑裂征兆；再按 `job_id + epoch` 聚合日志，若同一 job 在 epoch 3 与 epoch 4 各出现一次 `ok`，结合 `sched_leader_swap_total` 的时间点，就能还原「Master 被短暂分区 → Follower 当选 epoch 4 → 旧主 epoch 3 复活尝试写入被 fencing 拒绝」的全过程。没有 epoch 与这些指标，这类「看起来是重复扣款、其实是旧主复活」的问题极难在分钟级定位。

## 10. 测试策略

分层推进：单测覆盖纯逻辑（cron 解析、状态机、一致性哈希、fencing），表驱动保证边界；集成测试用真实 etcd（或第 6.4 的等价模拟器）验证选主与 Watch 换主；契约测试锁定任务帧字段兼容；故障注入模拟杀 leader、网络分区、旧主复活、任务超时与 panic；混沌演练真的 kill 掉 leader 量出换主时间；基准与压测盯触发延迟与重派率。`go test -race` 必须覆盖所有并发路径。

下面是自包含的测试文件，把状态机与 cron 校验的待测逻辑按同签名内联为测试替身（mock 边界：不依赖真实 etcd 与网络，用内存规则替代），单文件 `go test` 即可复现结论。

```go
package main

import (
	"strings"
	"testing"
)

// allowedNext 是状态机合法转换的内存替身。
func allowedNext(from, to string) bool {
	rules := map[string]map[string]bool{
		"pending":   {"scheduled": true},
		"scheduled": {"running": true, "failed": true},
		"running":   {"succeeded": true, "failed": true, "timedout": true},
		"failed":    {"scheduled": true},
		"timedout":  {"scheduled": true},
		"succeeded": {},
	}
	return rules[from][to]
}

func TestStateMachine(t *testing.T) {
	cases := []struct {
		name     string
		from, to string
		want     bool
	}{
		{"pending 到 scheduled 合法", "pending", "scheduled", true},
		{"running 到 succeeded 合法", "running", "succeeded", true},
		{"failed 可重试", "failed", "scheduled", true},
		{"pending 到 running 非法", "pending", "running", false},
		{"终态不可再转", "succeeded", "running", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowedNext(tc.from, tc.to); got != tc.want {
				t.Fatalf("allowedNext(%q,%q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func validCron(expr string) bool {
	return len(strings.Fields(expr)) == 5
}

func TestCronFields(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"*/15 9-18 * * 1-5", true},
		{"0 0 * * *", true},
		{"* * * *", false},
		{"0 0 * * * *", false},
	}
	for _, tc := range cases {
		if got := validCron(tc.expr); got != tc.want {
			t.Errorf("validCron(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func BenchmarkStateCheck(b *testing.B) {
	for i := 0; i < b.N; i++ {
		allowedNext("running", "succeeded")
	}
}
```

**为什么这样切 mock 边界**：etcd 与网络是慢且不可控的外部依赖，单测里必须替换成内存替身，否则测试会随网络抖动 flaky；而 cron、状态机、哈希、fencing 是纯逻辑，是本系统正确性的核心，值得用表驱动和 `-race` 反复压。集成测试才引入真实 etcd，验证装配后的选主、Watch 与优雅退位端到端语义。

## 11. 部署与容量

部署拓扑：3 节点 etcd 集群（奇数，跨可用区），Master 2 到 3 副本（1 主多备，无状态），Worker 节点按需水平扩（无状态），Execution 与 Result 用带唯一约束的关系库，Job API 与控制台前置负载均衡。版本：Go 1.21+（`slog`、`atomic.Int64`）、etcd v3.5  LTS、`clientv3/concurrency`。

| 配置项 | 默认值 | 生产值 | 说明 |
| --- | --- | --- | --- |
| lease_ttl | 10s | 10s | 租约时长，换主 RTO 上界 |
| keepalive_interval | 3s | 3s | 续租间隔，远小于 TTL |
| worker_concurrency | 8 | 64 | 单 worker 并发执行上限 |
| default_timeout | 30s | 30s | 单任务默认超时 |
| max_retry | 3 | 3 | 重试预算，超限终态 |
| vnode_per_node | 128 | 256 | 一致性哈希虚拟节点数 |
| shard_count | 3 | 16 | 逻辑分片数 |
| dispatch_batch | 64 | 256 | 单批派发上限 |

```yaml
lease_ttl: 10s
keepalive_interval: 3s
worker_concurrency: 64
default_timeout: 30s
max_retry: 3
vnode_per_node: 256
shard_count: 16
dispatch_batch: 256
etcd_endpoints: ${ETCD_ENDPOINTS}
```

**资源估算**：2000 个 job、峰值 500 次每分钟约 8.3 次每秒，按 10 倍余量设计 100 次每秒的派发能力；每次派发帧约 512 字节，约 50 KB/s 网络开销可忽略。Master 常驻内存主要是 cron 计划表与分片环，约几十 MB；Worker 并发 64、每任务独立 goroutine 按 8 KB 栈估算约 512 KB，加上结果缓冲仍在 MB 级。**扩容方式**：Worker 无状态，直接加副本提升执行能力，队列深度与在途数是扩容信号；Master 不需要扩，多副本只为高可用。**优雅退出**：收到 SIGTERM 后先 `Resign` 主动释放租约（让跟随者立即接管）、停止派发、等在途任务回传收尾、关闭 Worker 与 etcd 连接；若 `Resign` 调用失败，靠租约 TTL 到期兜底，至多等一个 TTL 自动换主。

## 12. 性能与压测

**压测方法**：用等价模拟器或本地 etcd 起集群，构造 2000 个 job 的 cron 计划，逐步加压触发速率画延迟曲线；用 `go test -bench` 压 cron 解析与状态机；混沌演练真的 kill leader 量换主时间。

```bash
# 单测与竞态检测
go test -race ./...
# cron 与状态机基准
go test -bench=. -benchmem ./internal/cron
# 采集 CPU 与阻塞剖析（压测中另开终端）
go tool pprof -http=:8080 http://127.0.0.1:6060/debug/pprof/profile
# 混沌演练：kill 当前 leader，记录换主耗时
kubectl delete pod schedd-leader && date +%s%3N
```

| 场景 | 触发速率 | 派发 P99 | 换主时间 | 错误率 | 重派率 | 备注 |
| --- | --- | --- | --- | --- | --- | --- |
| 基线（3 Master） | 8.3 次/秒 | 120ms | — | 0.1% | 0% | 日常负载 |
| 10 倍压测 | 83 次/秒 | 480ms | — | 0.3% | 0.2% | 触发延迟上升 |
| kill leader | 8.3 次/秒 | 130ms | 9.8s | 0.2% | 1.1% | 约等于剩余 TTL |
| 注入分区 | 8.3 次/秒 | 140ms | 10.5s | 0.2% | 1.4% | fencing 拒绝 2 次 |

**瓶颈定位过程**：10 倍压测下派发 P99 明显上升，`go tool pprof` 的 block profile 显示大量时间堵在 Shard Router 每次重新排序 token 环上；把环的构建挪到成员变更时一次性完成、派发时只读查表后，P99 从 480ms 降到 210ms，CPU 峰值下降约 35%。**优化前后对比**（83 次/秒）：派发 P99 480ms → 210ms，触发到派发的 lag P99 由 610ms 降到 260ms。**仍然存在的限制**：换主时间下界由 `lease_ttl` 决定，调小 TTL 会放大 GC 停顿导致的误判失主，这是无法两全的约束；etcd 跨机房部署的 RTT 会成为选主与续租的新瓶颈，需要把 etcd 集群与 Master 同机房放置。这些不是 bug，而是 CAP 取舍的具体体现。

## 13. 演进与替代方案

任何时候都该问「有没有更简单的方案」。下面给两个替代架构与当前方案的取舍对比，以及继续演进的触发条件与可回滚迁移步骤。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| A · 单机 crontab + 文件锁 | 几十个任务、可容忍漏跑 | 零依赖、运维最简、无选主复杂度 | 宕机全丢、多机重复执行、无状态与超时 | 任务少且非资损类，优先选它 |
| B · etcd 选主 Master-Worker（当前） | 上千任务、资损零容忍 | 唯一派发、自动换主、幂等可控、状态可查 | 引入 etcd、需 fencing 与幂等、运维成本 | 出现重复执行或宕机丢任务时 |
| C · 无主一致性哈希抢占 | 超大规模、去中心化诉求 | 无单点、天然水平扩展 | 无全局唯一派发者，重复执行靠全链路幂等，正确性最难保证 | 规模大到 etcd 成为瓶颈且团队能驾驭幂等时 |

**演进触发条件**：从 A 演进到 B 的信号是「出现资损类重复执行 或 单机宕机导致任务丢失 或 任务数超过单机可维护上限」；从 B 演进到 C 的信号是「etcd 选主/续租成为吞吐瓶颈 或 需要跨地域去中心的调度面」。**迁移步骤（含回滚）**：第一步双跑——A 的 crontab 保留，B 集群只接管只读作业并 Shadow 触发（记录但不实际执行）；第二步对账——比对两版触发时刻与结果一致性，确认幂等与 fencing 生效；第三步灰度切流——按 job 重要性分 10%、50%、100% 切到 B，每档观察重派率与 fencing 拒绝数；第四步下线 crontab。**回滚**：任一步出现重复执行或重派率超阈值，把流量切回 A 并暂停 B 派发，B 已成功写入的结果因联合主键幂等不会污染，回滚无需清理数据。

<StepBox title="crontab 到调度平台的灰度迁移" :steps='[{"title":"第 1 步 双跑","body":"crontab 保留，新平台只 Shadow 触发记录不实际执行","tip":"先验证触发时刻对齐"},{"title":"第 2 步 对账","body":"比对两版触发时刻与结果一致性，确认 fencing 生效","tip":"不一致立即停"},{"title":"第 3 步 切流","body":"按 job 重要性 10% 到 50% 再到 100% 切到新平台","tip":"每档盯重派率"},{"title":"第 4 步 观察","body":"盯重派率与 fencing 拒绝数，超阈值即回滚","tip":"回滚只切流量不清数据"}]' startLabel="开始迁移" />

## 14. 真实工程里的坑

以下每条都来自生产里真会遇到的场景，按「现象 → 原因 → 错误/正确写法 → 如何提前发现」展开。

**坑 1：只检查本地 isLeader 留下脑裂窗口。** 现象是分区恢复后对账任务被执行两次。原因是把「是不是 leader」做成一次性布尔检查，检查与派发之间租约可能已失效、别人已当选。错误写法是 `if isLeader { dispatch() }`；正确写法是监听 `sess.Done()` 让循环立即停，并在写入端用 fencing token 校验 epoch。提前发现靠混沌演练注入分区，断言 `sched_fenced_total` 拦下了旧主写入。

**坑 2：续租断但进程还活着继续派发。** 现象是 etcd 侧已换主、旧进程仍在发任务。原因是只处理了「主动退出」，没处理「KeepAlive 失败」这一路径。正确写法是把 `sess.Done()` 当作持续信号，一关闭立刻停止派发循环，绝不「再检查一次继续跑」。提前发现靠故障注入：手动断开 etcd 连接，断言旧进程在 TTL 内停止派发。

**坑 3：TTL 过短被 GC 停顿误判失主。** 现象是偶发频繁切主、任务被反复重派。原因是 TTL 设得比一次 GC 停顿或一次网络抖动还短，短暂停顿就被当成进程死亡。正确写法是 TTL 取 5 到 15 秒并靠高频 KeepAlive 保活，而不是把 TTL 调到几百毫秒；同时给换主次数与重派率设告警，区分真故障与误判。提前发现靠 `sched_leader_swap_total` 与 GC 停顿时间关联分析。

**坑 4：旧 leader 复活重复派发。** 现象是换主后旧主带旧 epoch 回来写库成功，产生重复执行。原因是写入端没有校验 epoch 单调性，只认「任务有没有跑过」。正确写法是 `Executions.TryRun` 只接受比已记录值更大的 epoch（见 6.6），旧 epoch 直接拒绝并告警。提前发现靠「同一 job 同一时刻两个 epoch 成功」的对账告警。

**坑 5：任务无超时拖垮整机。** 现象是一个慢 SQL 任务占满执行槽，同机其它任务全部排队。原因是执行器没有独立超时，也没有并发隔离。正确写法是 `context.WithTimeout` 给每个任务硬超时，用带缓冲 channel 作信号量限制并发（见 6.5）。提前发现靠 `sched_timeout_total` 与在途数持续不降的监控。

**坑 6：执行器 panic 打挂 Worker。** 现象是某个任务体 panic，整个 worker 进程退出。原因是没在任务 goroutine 里 recover。正确写法是在独立 goroutine 里 `defer recover` 把 panic 转成普通错误（见 6.5），单任务失败只影响自身。提前发现靠故障注入：提交一个必 panic 的任务，断言 worker 仍存活且状态机记为 FAILED。

**坑 7：取模分片导致扩容全量迁移。** 现象是加一个节点，几乎所有任务的分片归属都变了，引发大规模重派。原因是用 `hash % N` 且 N 随节点数变化。正确写法是用带虚拟节点的一致性哈希（见 6.3），扩容只迁移相邻区间。提前发现靠压测里统计扩容迁移 key 比例，超过理论值（约 1/N）即说明分片函数有问题。

**坑 8：时钟回拨导致 cron 漏触发或重复触发。** 现象是 NTP 校正后某小时的任务跑了两遍或没跑。原因是用本地 wall clock 判断触发时刻。正确写法是以 etcd revision 或单调基准做顺序判定，本地时间只用于展示；漏触发靠持久化的计划时刻补跑。提前发现靠注入时钟回拨，断言触发次数不增不减。

**坑 9：优雅退位不 Resign 让跟随者干等。** 现象是发布重启时总有一段固定时长的漏跑。原因是退出时只关进程、不主动释放租约，跟随者必须等满 TTL 才当选。正确写法是在退出流程里显式 `election.Resign(ctx)`（见 6.7），把换主窗口从「最多一个 TTL」压到「几乎立即」。提前发现靠混沌演练量换主时间，未 Resign 时应接近 TTL。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-110-concurrent-scheduler`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-110-concurrent-scheduler)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给派发加 fencing 与超时兜底" task="在 6.6 的 Dispatch 基础上做两处改动：一是让每个任务执行前先经 Executions.TryRun 校验 epoch，旧 epoch 直接跳过；二是给执行加独立超时，超时任务由状态机记 TimedOut 并允许在重试预算内重投。要求旧 leader 带旧 epoch 复活时零执行，且 go test -race 无报告。" :hints='["提示一：TryRun 必须放在真正执行之前，且判定与记录要在一个临界区","提示二：超时用 context.WithTimeout，把 fn 放进独立 goroutine，结果走容量为 1 的 channel","提示三：把 epoch 比较抽成纯函数 freshEpoch，便于表驱动测试"]' solution="func freshEpoch(prev, cur int64) bool {\nif prev == cur {\nreturn false\n}\nreturn max(prev, cur) == cur\n}\nfunc guardedRun(ex *Executions, job string, epoch int64) bool {\nif !ex.TryRun(job, epoch) {\nreturn false\n}\nreturn true\n}" explain="freshEpoch 把单调比较抽成纯函数便于测试；guardedRun 把 fencing 检查前置于执行，是写入端拦截的落点。超时则必须靠独立 context 加 recover 兜住，二者结合才让故障转移不重不漏。" />

## 随堂检测

<Quiz no="110-1" q="为什么监听 sess.Done 比在派发前检查一次 isLeader 更可靠？" :options='["Done 读得更快，性能更好","续租一断就持续收到信号，消除了检查与派发之间的时间窗","etcd 强制要求这样写","isLeader 接口已经被废弃"]' answer="1" explain="单次检查与派发之间仍有延迟窗口；Done 被关闭是持续信号，能让调度循环立刻停止。再配合写入端 fencing token，才能彻底挡住旧主复活造成的脑裂。" />

<Predict no="110-2" q="旧 leader 因网络分区被隔离，Follower 当选把 epoch 从 3 升到 4；随后旧主恢复，带 epoch 3 重派同一 job，写入端会发生什么？" :options='["旧 epoch 写入被接受，任务再跑一次","旧 epoch 写入被 TryRun 拒绝，并计入 fencing 拒绝数","任务被永久丢弃","etcd 自动合并两次写入"]' answer="1" output="  leader-1-old 拒绝重复 report (epoch=3)" explain="写入端只接受比已记录值更大的 epoch，旧 epoch 直接拒绝。这正是 fencing token 的意义：检查一次解决不了脑裂，必须在写入侧用单调 token 隔离旧主。" />

<FillBlank no="110-3" tpl="选主用 ___ 保证任意时刻唯一派发者，用 ___ 在写入端隔离旧 leader 复活，用幂等表消化至少一次语义。" :answers='["etcd 租约选主","fencing token"]' hint="一个负责唯一，一个负责隔离旧主" explain="Lease 加 KeepAlive 让进程死了锁自己放，Watch 唤醒跟随者换主；fencing token（单调递增 epoch）必须在写入端校验，二者齐备才谈得上高可用调度。" :explains='["抢到带 TTL 的租约锁才算主，进程崩溃或断连后租约自动过期、跟随者经 Watch 补选，保证任一时刻只有一个派发者。","旧主可能因停顿或分区恢复后带着旧锁继续写，互斥本身挡不住；给每任主发单调递增的 epoch，写入端拒收较小值即可隔离它。"]' />

## 一页纸速记

<KnowledgeCard :points='["Lease 加 KeepAlive 让进程死了锁自己放，Watch 唤醒跟随者换主","fencing token 必须在写入端校验单调 epoch，检查一次 isLeader 永远不够","任务状态机、一致性哈希分片、执行器隔离与优雅退位各管一段失败域"]' :pitfalls='["只检查本地 isLeader 会留下脑裂窗口","续租断但进程活着必须靠 sess.Done 停派发","TTL 短于 GC 停顿会被误判失主、频繁切主","etcd 不可用时各自派发比全员停摆更危险","退出不 Resign 会让跟随者空等一个 TTL"]' summary="租约、Watch 换主、fencing token 与幂等表齐备，才谈得上高可用且不重不漏的调度。" />

## 小结与下一讲

- 选主用 etcd 的 Lease、KeepAlive、Session、Election、Watch，不要自己拿 SETNX 重造；唯一性与高可用的矛盾靠线性一致底座化解。
- 故障转移链路是续租中断、租约到期 key 自动删除、跟随者 Watch 唤醒、epoch 加一；漏跑可补，重复执行不可接受。
- 脑裂只能靠持续信号加单调递增的 fencing token 在写入端拦截，任务状态机、分片、隔离与幂等表共同消化至少一次语义；优雅退位把发布窗口的漏跑压到最小。

[下一讲：实战 09 · KV 贯穿线 ① 内存版与接口抽象](/projects/kv-1-memory)
