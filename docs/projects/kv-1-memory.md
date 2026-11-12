---
title: 第 111 讲 · 实战 09 · KV 贯穿线 ① 内存版与接口抽象
description: 承接第 025 讲的内存版 KV，把 Command 与 Storage 定型为单入口契约，为网络层、WAL、Raft 留出扩展点
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="定义可演进的存储抽象" prereq="第 025 讲 · 内存版 KV" />

# 第 111 讲 · 实战 09 · KV 贯穿线 ① 内存版与接口抽象

> 这一讲是 KV 贯穿线的正式起点，直接承接第 025 讲「阶段综合：内存版 KV」。那一讲已经把 L2 的零件装成一台能跑的小机器：`Storage` 接口定出 `Put/Get/Delete/Keys` 四个动作，`MemStore` 用 `sync.RWMutex` 护住 `map[string]string`，`Command` 把一行文本解析成可重放的操作，`Apply` 把它落到任意实现，错误用哨兵加 `errors.Is` 判定，并且明确留下了六个扩展点。但那是一份**教学实现**：接口按操作拆成四个方法，每加一种命令就要改接口和全部实现；`Op` 是字符串，适合文本却不适合编码进二进制帧；`Get` 靠错误区分「空值」与「键不存在」，一旦跨进程，错误链很容易退化成一段无法判定的文本。本讲要解决的正是这件事：把「一次操作」和「一次存储」**定型**成一份四讲都不再改动的窄腰契约，并给出一个能接住第 025 讲旧实现的适配器。看懂「什么必须稳定、什么允许变化」，是这条贯穿线真正要教的东西。

<CheckList id="L111-goal" title="本讲目标" :items='["写清承接关系：第 025 讲的四方法 Storage 为什么要收敛成单入口 Apply","定型 Command、Result、Op 数值与哨兵错误集合，锁定为四讲不变的数据契约","用 RWMutex、锁分片、TTL 装饰器分别给出可替换的内存实现","用 WrapLegacy 证明旧实现不返工，用契约测试与基准测试建立可度量的基线"]' />

## 0. 项目定位与真实场景

把一个 KV 存储单独拎出来做一讲，是因为它几乎是所有后端系统的地基。会话（session）、功能开关（feature flag）、幂等键、分布式锁的持有标记、限流计数器、配置缓存、短链映射，底层都是「给一个 key，存或取一个 value」。以中等规模的电商后台为例：单机需要承载约一百万个活跃 key，读多写少，峰值读 QPS 在数十万量级，写 QPS 在数万到十几万量级，单次访问延迟要求压在微秒级，因为 KV 往往处在调用链最内层，它的延迟会被上游层层放大。业务方真正痛的不是「能不能存」，而是「换了存储引擎要不要改业务代码」：今天用内存 map 做本地缓存，明天要接网络服务，后天要落盘、要分片、要多副本，如果每一次都要重写调用方，团队就会被自己的抽象拖死。

第 025 讲的版本在单进程内是完整且正确的，它已经用接口把「上层要什么能力」和「能力怎么实现」分开了。但进入工程实战后，这个抽象还不够窄：四个方法意味着「操作」这件事散落在接口签名里，而网络帧、WAL 记录、Raft 日志更希望只搬运**一条命令**。所以本讲的核心矛盾非常清晰：**稳定的契约与多变的实现之间的张力**。契约太宽（按操作列方法），每加一种能力都要全系统改一遍；契约太窄（只传字节），又丢失了类型与错误语义。我们要找的是那个「窄腰」：一份纯数据的 `Command` 加一个单方法 `Storage.Apply`，让命令解析、TCP 收发、日志追加、共识复制都只依赖同一样东西。

<Term word="窄腰" text="架构里长期稳定、上下都依赖它的一层最小契约；上层换协议、下层换引擎都不必改动它，是演进能力的来源" />

<Callout type="idea" title="先定约束，再定架构" text="这一讲不先堆数据结构，而是先把「四讲之内不许改什么」写死：Command 的字段、Op 的数值、Storage.Apply 的语义。约束定稳之后，内存实现、网络帧、WAL、Raft 都只是这层契约的不同实现，调用方从头到尾一行不改。" />

## 1. 需求与约束

先把目标写成可验收的条目，避免「做一个 KV」这种无法评审的说法。功能需求给出编号与验收标准，非功能指标给出可压测的数字，两者必须自洽，后面第 12 节的压测数据会逐条回来对账。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 支持 SET / GET / DEL 三类操作 | 表驱动用例覆盖写后读、删除后再读返回未命中 |
| FR-2 | 空 key 必须在入口被拒绝 | 任何实现收到空 key 都返回可用 `errors.Is` 判定的 `ErrEmptyKey` |
| FR-3 | 区分「值为空」与「键不存在」 | `Result.Found` 为 true 且 `Value` 为 nil 是合法命中，不报错 |
| FR-4 | 值承载任意二进制 | `Value` 为 `[]byte`，进出都做值拷贝，调用方改写不污染内部 |
| FR-5 | 并发安全 | `go test -race` 下多 goroutine 混合读写无数据竞争、无 fatal error |
| FR-6 | 支持 TTL 与过期回收 | 惰性判定加后台定时清理，过期后 GET 返回未命中 |
| FR-7 | 兼容第 025 讲实现 | 旧 `MemStore` 不经修改即可通过适配器接入新契约 |
| FR-8 | 可替换实现 | 至少提供全局锁、锁分片两种实现，且都满足同一份契约测试 |

非功能指标是设计评审里最容易被含糊带过、也最该先落纸的部分。下表给的是本讲这一版（单进程、纯内存）的 SLO 目标，它们是可压测、可证伪的。

| 指标 | 目标值 | 度量方式 | 说明 |
| --- | --- | --- | --- |
| 读吞吐 | 8 核下不低于 500 万 QPS | `BenchmarkMemoryGet` 配合 `b.RunParallel` | 读走 RLock，可随核数近似线性扩展 |
| 写吞吐 | 8 核下不低于 200 万 QPS | `BenchmarkShardedSet` | 依赖锁分片摊开写竞争 |
| 单次访问延迟 | P99 低于 5 微秒 | Histogram 埋点加 `benchstat` | 纯内存路径，不含网络与磁盘 |
| 数据规模 | 100 万 key 常驻 | `Len()` 与内存采样 | 约 256 MiB，见第 11 节估算 |
| 一致性级别 | 单 key 线性一致 | 契约测试加锁临界区审查 | 每把分片锁内串行，读总能看到已完成写 |
| 可用性 SLA | 单进程，无 HA | 依赖进程存活 | 本讲刻意不提供故障转移 |
| RTO | 进程重启小于 2 秒 | 冷启动计时 | 无快照加载，启动即空表 |
| RPO | 等于全部数据 | 崩溃演练 | 纯内存无持久化，掉电即全丢，见下方边界 |

**本讲不做什么**：不做网络与跨进程访问（第 112 讲）、不做持久化与崩溃恢复（第 113 讲）、不做分片路由与副本（第 114 讲）、不做认证与加密。**假设边界**：调用方可信、单进程、仅依赖 Go 1.21 及以上标准库、64 位平台、`Key` 与 `Value` 均有配置项上限。最重要的诚实声明是 RPO：这一版一旦进程崩溃，内存里的全部数据都不可恢复，因此它只适合可重建的缓存类数据，绝不存放唯一真值。把这条写进评审纪要，比事后补一份事故报告便宜得多。

<Callout type="warn" title="数字必须自洽" text="QPS、P99 与数据规模是互相牵制的：100 万 key 常驻意味着 GC 要扫描大量堆对象，写吞吐就不能按「空 map 自增」去吹。第 12 节的压测数字全部以本表为对照目标，任何优化前后对比都要落在这张表的口径上。" />

## 2. 架构蓝图

下面这张分层图里，只有「契约层」是稳定的，其余三层都可以被替换。点击任意模块能看到它的职责、技术选型与失败处理，这正是第 025 讲留下的扩展点在架构图上的落位。

<ArchBlueprint title="KV 系统分层架构（稳定契约层是唯一的窄腰）" caption="网络层、WAL、Raft 都是实现层或接入层的不同形态，契约层始终不动" :layers='[{"name":"接入层","note":"把外部输入变成 Command","protocol":"进程内函数调用；第 112 讲改为长度前缀帧","items":[{"label":"CLI 命令解析","sub":"ParseCommand / FormatResult","detail":"职责：把一行文本解析成 Command 并把结果格式化成一行文本；技术选型：strings.Fields 加显式 Op 映射，不引第三方解析库；失败处理：未知命令、缺 key、缺 value 分别用 fmt.Errorf 包装哨兵错误返回，绝不返回零值命令当成功。","tone":"blue"},{"label":"契约测试与基准","sub":"contract_test / bench","detail":"职责：以同一套断言验证每个实现都符合 Storage 契约；技术选型：表驱动子测试加 testing.B 的 RunParallel；失败处理：任一实现不满足契约时测试立刻失败，阻止不合契约的实现被合入。","tone":"blue"},{"label":"未来 TCP SDK","sub":"第 112 讲的 Client","detail":"职责：把远程调用伪装成本地 Apply；技术选型：本讲只在接口层预留位置，不写实现；失败处理：预留 ErrClosed 与上下文取消，让调用方切换实现时零改动。","tone":"gray"}]},{"name":"契约层","note":"四讲之内锁定的最小约定","protocol":"Apply(Command) 方法调用，依赖方向一律向内","items":[{"label":"Command 结构体","sub":"Op / Key / Value","detail":"职责：统一描述一次读写，是网络帧、WAL、Raft 日志唯一的数据载体；技术选型：Op 用显式数值的 byte，Value 用 []byte 承载二进制；失败处理：未知 Op 在实现入口返回 ErrBadOp，字段集合锁定不改，避免持久化格式漂移。","tone":"green"},{"label":"Storage 接口","sub":"Apply 与 Close","detail":"职责：定义执行一次命令并返回结果的唯一入口；技术选型：只有一个 Apply 方法加一个 Close，遵循小接口原则；失败处理：关闭后任何写返回 ErrClosed，实现用编译期断言检查是否满足契约。","tone":"green"},{"label":"Result 与哨兵错误","sub":"Found / errors.Is","detail":"职责：用 Found 区分空值与未命中，用包级哨兵承载可判定的失败语义；技术选型：errors.New 定义哨兵，调用方统一 errors.Is 判定；失败处理：错误一律包装而非拼字符串，保证跨包、跨网络仍可判定。","tone":"green"}]},{"name":"实现层","note":"可替换的存储引擎","protocol":"读写锁、值拷贝、errors.Is 错误链","items":[{"label":"Memory 全局锁","sub":"单 RWMutex 加 map","detail":"职责：最直白的内存实现；技术选型：sync.RWMutex 读并行写互斥，进出 []byte 全量值拷贝；失败处理：空 key、未命中、关闭后写、未知 Op 分别返回对应哨兵错误。","tone":"orange"},{"label":"ShardedMemory 锁分片","sub":"按 key 哈希分 N 片","detail":"职责：把写竞争摊到多把锁上；技术选型：FNV-1a 取模定位分片，每片独立 RWMutex；失败处理：分片数为 0 时构造阶段补默认值，Close 幂等，避免除零与重复关闭。","tone":"orange"},{"label":"TTLMemory 装饰器","sub":"过期时间加后台回收","detail":"职责：在任意 Storage 之外叠加过期能力而不改契约；技术选型：独立过期表加 time.Ticker 后台 goroutine，惰性判定用单调时钟；失败处理：Close 关闭 quit 并 WaitGroup 等待，杜绝 goroutine 泄漏。","tone":"orange"},{"label":"LegacyAdapter 适配器","sub":"WrapLegacy","detail":"职责：把第 025 讲按操作拆方法的旧实现接进新契约；技术选型：用 errors.Is 把旧哨兵翻译成本讲哨兵；失败处理：旧实现报错且无法翻译时原样返回，不吞错。","tone":"orange"}]},{"name":"基础设施层","note":"标准库原语","items":[{"label":"sync.RWMutex","sub":"读写锁原语","detail":"职责：保护 map 的并发访问；技术选型：标准库读写锁，读多写少时读可并行；失败处理：一律 defer 解锁，保证所有返回路径都释放。","tone":"purple"},{"label":"map 与 []byte","sub":"存储与值载体","detail":"职责：存放键值并承载二进制；技术选型：map[string][]byte 加值拷贝；失败处理：并发写 map 会触发运行时 fatal，靠锁彻底隔离。","tone":"purple"},{"label":"time 单调时钟","sub":"TTL 判定","detail":"职责：判断键是否过期；技术选型：time.Now 含单调时钟分量，不受墙钟回拨影响；失败处理：时钟异常时保守判为未过期，交由后台回收兜底。","tone":"purple"},{"label":"testing 与 atomic","sub":"基线与埋点","detail":"职责：提供压测与无锁计数；技术选型：testing.B 加 sync.atomic 计数器；失败处理：基准里遇错立即 b.Fatal，不让脏数据混进指标。","tone":"purple"}]}]' />

逐层解读为什么这样切。接入层负责把「外部世界的输入」翻译成契约层的 `Command`，它是最可能变化的一层：今天是命令行，第 112 讲就换成 TCP 帧，但翻译结果始终是同一个结构体。契约层是整张图的重心，它不依赖任何实现，只定义数据结构与一个方法；把接口放在消费侧、定义得越小越好，是第 019 讲「接受接口、返回结构体」的直接应用。实现层是可替换的引擎集合：本讲一口气给出全局锁、锁分片、TTL 装饰器、旧实现适配器四种，它们互相不知道对方存在，只认契约。基础设施层是标准库原语，不含任何业务判断。

依赖方向必须严格朝内：接入层依赖契约层，实现层依赖契约层，契约层谁都不依赖。**不允许反向依赖**——`Memory` 不许 import `net`，`ParseCommand` 不许直接摸 `map`，契约层不许引用任何具体引擎类型。一旦出现反向依赖，替换实现时就会牵连出一条谁也没预料到的引用链，第 113、114 讲要做的「换实现、不换调用」也就无从谈起。

<ConceptDiagram type="iface" title="依赖倒置：所有实现都指向同一份 Storage 接口" caption="接口定义在消费侧，具体实现放在实现层，替换引擎不动调用方" />

<FlowTimeline title="KV 贯穿线四讲演进" direction="horizontal" :steps='[{"title":"第 025 讲 内存版","desc":"四方法 Storage 与 MemStore 打样","tone":"blue"},{"title":"① 本讲 抽象定型","desc":"Command 与单入口 Storage 锁定","tone":"green"},{"title":"② 网络层","desc":"TCP 帧与客户端 SDK","tone":"orange"},{"title":"③ 持久化","desc":"WAL、恢复、锁分片、快照","tone":"purple"},{"title":"④ 分布式","desc":"分片、副本、Raft 共识","tone":"red"}]' />

## 3. 模块职责与依赖方向

把上图落成可评审的模块清单。每个模块的输入、输出、依赖、是否持有状态、失败时怎么表现，都要能一句话说清，否则边界就是糊的。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `contract.go` | 无 | `Command` / `Result` / `Storage` / 哨兵错误 | 仅 errors | 无状态 | 不适用，纯声明 |
| `memory.go` | `Command` | `Result` 或错误 | 契约、sync | 有：map 与 closed 标志 | 返回四类哨兵错误，值拷贝 |
| `sharded.go` | `Command` | `Result` 或错误 | 契约、hash/fnv、sync | 有：N 个分片 | 分片数为 0 时构造补默认，Close 幂等 |
| `ttl.go` | `Command` | `Result` 或错误 | 契约、time、sync | 有：过期表与后台 goroutine | 过期读返回未命中，Close 等待回收退出 |
| `codec.go` | 一行文本 / `Result` | `Command` / 一行文本 | 契约、strings、fmt、errors | 无状态 | 包装哨兵错误返回，不产出零值命令 |
| `adapter.go` | `Command` | `Result` 或错误 | 契约、errors | 有：被包装的旧实现 | 旧哨兵翻译失败则原样返回错误 |
| `metrics.go` | `Command` | `Result` 或错误 | 契约、atomic、time | 有：计数器 | 自身不失败，透传内部结果 |
| `contract_test.go` | 各实现工厂 | 测试结论 | 全部实现 | 无状态 | 任一断言失败即终止合入 |

包目录树按「一个文件一个职责」组织，文件名即模块名，评审时可以直接对着目录树逐文件过。

```text
kvstore/
├── go.mod                 # module kvstore，go 1.21
├── contract.go            # Op / Command / Result / Storage / 哨兵错误（四讲锁定）
├── memory.go              # Memory：单 RWMutex 加 map[string][]byte
├── sharded.go             # ShardedMemory：FNV-1a 取模的锁分片
├── ttl.go                 # TTLMemory：装饰器形式的过期与后台回收
├── codec.go               # ParseCommand / FormatResult
├── adapter.go             # LegacyStore / WrapLegacy
├── metrics.go             # MeteredStorage 埋点装饰器
├── memory_test.go         # CRUD、并发、适配器与 TTL 用例
├── bench_test.go          # 读写与分片写基准
├── contract_test.go       # 跨实现的统一契约测试
└── cmd/kvdemo/main.go     # 可运行主流程：文本命令到结果
```

接口定义与依赖倒置落在一句编译期断言上：`var _ Storage = (*Memory)(nil)`。它不产生运行时开销，却能在契约被误改时让编译立刻失败——这是比文档更可靠的约束。所有实现都返回具体指针类型（`NewMemory` 返回 `*Memory`），由调用处按需赋给 `Storage` 接口变量，接口始终留在消费侧。装饰器链同样如此：`MeteredStorage` 与 `TTLMemory` 内部持有的是 `Storage` 接口而非具体类型，因此它们可以套在 `Memory`、`ShardedMemory`，乃至第 113 讲 `WALStorage`、第 114 讲 `Router` 上，一行不改。

<Callout type="warn" title="边界划分最常见的三个错误" text="第一，把 ParseCommand 塞进 Storage 接口——解析是接入层职责，塞进去会让每个引擎都被迫实现文本协议。第二，让 Memory 依赖 net 或 os——反向依赖会毁掉可替换性。第三，把 TTL 做成 Storage 的新方法而不是装饰器——那会逼着所有实现都实现 TTL，违背「新能力不改契约」的原则。" />

## 4. 数据与协议契约

契约要落到「字段怎么排、零值怎么解释、版本怎么兼容」才算可对接。先看逻辑数据结构：虽然本讲数据只在内存，但 `Command` 的形状就是第 113 讲 WAL 记录、第 114 讲 Raft 日志最终要落盘的内容，所以这里先用一张逻辑表把它定死，避免后面各写各的。

```sql
-- 逻辑记录结构（本讲不落盘，仅定义契约；第 113 讲 WAL、第 114 讲 Raft 直接复用字段语义）
CREATE TABLE kv_command (
    op        TINYINT      NOT NULL,  -- 1=GET 2=SET 3=DEL，显式数值，禁止用 iota 自动增长
    key       VARBINARY(1024) NOT NULL,  -- 空 key 非法，入口即拒绝
    value     BLOB         NULL,      -- nil 是合法空值，与「键不存在」正交
    expire_at BIGINT       NULL,      -- 可选 TTL，由装饰器登记，不进 Command 字段集合
    trace_id  CHAR(16)     NULL       -- 由外层上下文或帧头携带，不污染 Command 契约
);
```

字段语义、零值与兼容规则逐条说明：`Op` 是显式赋值的 `byte`（`OpGet=1`、`OpSet=2`、`OpDel=3`），因为第 112 讲要把它直接编码进 TCP 帧，重排顺序会让旧数据被解释成另一种操作，属于兼容性事故；新增操作只允许**追加**新数值，不改旧值。`Key` 为空串是非法输入，返回 `ErrEmptyKey`。`Value` 为 `[]byte`，零值 nil 表示「合法的空值」，因此必须靠 `Result.Found` 单独表达是否命中——这正是把「值为空」与「键不存在」分开的关键。`Result.Value` 与写入的 `cmd.Value` 之间必须值拷贝，否则调用方持有底层数组后并发改写会制造数据竞争。未知 `Op` 返回 `ErrBadOp`，关闭后写返回 `ErrClosed`。`trace_id` 与 `expire_at` 刻意**不进** `Command` 字段集合：前者由外层 `context` 或第 112 讲帧头携带，后者由 TTL 装饰器维护，这样才能保证四讲锁定的 `Command` 一个字段都不用改。

下面是本讲定型的 `package kv` 契约源码，也是第 113、114 讲 `contract.go` 快照的逐字来源。

```go
package kv

import "errors"

// Op 用显式数值定义：第 112 讲把它编码进 TCP 帧，
// 第 113、114 讲写进 WAL 与 Raft 日志，数值必须长期稳定。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

// 包级哨兵错误：调用方统一用 errors.Is 判定，不比对错误文本。
var (
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
)

// Command 是四讲唯一的数据载体：解析、网络帧、WAL、Raft 都只传它。
type Command struct {
	Op    Op
	Key   string
	Value []byte
}

// Result 用 Found 把「键不存在」与「值恰好为空」分开。
type Result struct {
	Value []byte
	Found bool
}

// Storage 是四讲唯一的抽象：Apply 必须是「执行一次命令」的完整语义，
// 可重放、可持久化、可复制，任何一版实现都要满足它。
type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

对外 API 用一张表定清楚，请求与响应给出 JSON 示例，方便第 112 讲把它翻译成帧。

| 操作 | 文本形式 | `Op` | 请求要点 | 成功响应 | 失败响应 |
| --- | --- | --- | --- | --- | --- |
| 写 | `SET k v` | 2 | key 非空，value 可含空格 | `OK`（Found=true） | `ERR empty_key` / `ERR closed` |
| 读 | `GET k` | 1 | key 非空 | `OK v` 或 `OK`（空值命中） | `ERR not_found` |
| 删 | `DEL k` | 3 | key 非空 | `OK`（存在）或 `NIL`（本就不存在） | `ERR closed` |

```json
{"request": {"op": 2, "key": "user:7", "value": "YmVuamk="}, "response": {"found": true}}
{"request": {"op": 1, "key": "user:7"}, "response": {"found": true, "value": "YmVuamk="}}
{"request": {"op": 1, "key": "missing"}, "response": {"found": false}, "error": "ERR not_found"}
```

消息格式用 proto 描述概念上的线契约——第 112 讲的长度前缀帧就是它的手工编码版，字段顺序与数值一一对应。

```proto
// 概念线契约；第 112 讲用 8 字节定长头加 body 手工实现，不引入 protobuf 运行时。
message Command {
  uint32 op    = 1;  // 与 OpGet/OpSet/OpDel 的 1/2/3 对齐
  string key   = 2;
  bytes  value = 3;
}
message Result {
  bytes value = 1;
  bool  found = 2;
}
```

版本号与灰度策略：帧头 `version` 字段从 1 起，服务端拒绝不认识的版本，`Op` 数值只增不改；灰度靠「契约不变、实现可换」完成——第 025 讲的旧实现经 `WrapLegacy` 接入后，可与新实现并存，按 key 前缀逐步切流，回滚只需换回旧实现，无需数据迁移（因为都还没持久化）。

<Compare title="两种契约形态的取舍" :panels='[{"label":"第 025 讲：按操作拆方法","code":"Put(k,v) error\nGet(k) (string,error)\nDelete(k) error\nKeys() []string","note":"直观、教学友好；但每加一种操作都要改接口与全部实现，网络帧与 WAL 得自己重组命令"},{"label":"本讲：统一命令入口","code":"Apply(Command) (Result, error)\nClose() error","note":"新增操作只追加一个 Op 常量；Command 是纯数据，天然可编码、可重放、可复制，是四讲能连起来的前提"}]' />

<ConceptDiagram type="error-chain" title="错误链：fmt.Errorf 包装哨兵，errors.Is 跨层判定" caption="哨兵错误在适配器、装饰器、网络映射之间始终可判定" />

## 5. 核心链路时序

以一次 `SET k v` 加一次 TTL 登记为主线，把正常链路画成时序图。参与方五个，消息十条，覆盖同步调用、返回、自处理与异步登记四类。

<SequenceDiagram title="一次 SET 命令的完整链路（含 TTL 异步登记）" caption="调用方只认 Apply，存储实现与后台回收对它是透明的" :actors='["Caller","Parser","Storage","Memory","Reaper"]' :steps='[{"from":"Caller","to":"Parser","label":"SET k v","note":"原始文本","kind":"call"},{"from":"Parser","to":"Parser","label":"分词校验","note":"定 Op 与 Key","kind":"self"},{"from":"Parser","to":"Storage","label":"Apply 命令","note":"统一入口","kind":"call"},{"from":"Storage","to":"Memory","label":"取写锁","note":"mu.Lock","kind":"call"},{"from":"Memory","to":"Memory","label":"写 map 值拷贝","note":"append 复制","kind":"self"},{"from":"Memory","to":"Storage","label":"回 Result","note":"Found 为真","kind":"return"},{"from":"Storage","to":"Parser","label":"返回结果","note":"error 为 nil","kind":"return"},{"from":"Parser","to":"Caller","label":"打印 OK","note":"格式化","kind":"return"},{"from":"Storage","to":"Reaper","label":"登记过期","note":"异步消息","kind":"async"},{"from":"Reaper","to":"Reaper","label":"清理过期键","note":"定时触发","kind":"self"}]' />

逐步骤解读：第 1 步调用方给出原始文本；第 2 步 `ParseCommand` 在解析层自处理，完成分词、大小写归一、Op 映射与 key 非空校验，一旦失败就不进入存储层；第 3 步是整条链路唯一的入口 `Apply(Command)`；第 4 步实现层取写锁，把并发写串行化；第 5 步在锁内写入并做值拷贝，这一步与第 4 步必须在同一临界区，否则会留下半写状态；第 6、7、8 步是结果沿原路返回；第 9 步 TTL 装饰器异步登记过期时间，不阻塞主链路；第 10 步后台回收 goroutine 定时清理，属于与主链路解耦的异步自处理。

异常链路有四处明确的分叉点，评审时要逐个确认兜底：

| 分叉点 | 触发条件 | 与正常链路的差异 | 兜底 |
| --- | --- | --- | --- |
| 第 2 步之前 | 空命令、未知命令、缺 key | 解析层直接返回包装后的哨兵错误，不进第 3 步 | `FormatResult` 输出 `ERR bad_op` 等，不产出零值 Command |
| 第 3 步 | 空 key 或未知 Op | 实现入口即返回 `ErrEmptyKey` / `ErrBadOp`，不取锁 | 契约测试断言这两类错误 |
| 第 4 步 | 并发写同一实现 | 写锁排队，后到者看到先到者的结果 | 按锁获取顺序执行，单 key 线性一致 |
| 第 5 步之后 | 已 `Close` 后仍写入 | 锁内发现 `closed` 标志，返回 `ErrClosed` | `Close` 幂等，重复关闭不报错 |

<StepBox title="异常链路：一次命中过期键的 GET" startLabel="开始追踪" :steps='[{"title":"第 1 步","body":"GET 到达 TTLMemory.Apply，先查过期表","tip":"装饰器在透传前做前置判定"},{"title":"第 2 步","body":"单调时钟发现已过 deadline，删除过期表条目","tip":"不依赖墙钟，回拨也不会误判"},{"title":"第 3 步","body":"向内层下发一条 DEL 命令做物理删除","tip":"DEL 幂等，重复删安全"},{"title":"第 4 步","body":"向上返回 ErrNotFound，不返回旧值","tip":"与键本就不存在的语义完全一致"}]' />

## 6. 核心实现（按模块逐段展开）

以下代码同属 `package kv`，用 `go.mod` 声明 `module kvstore`（`go 1.21`）组织，与第 4 节的 `contract.go` 放在同一目录，执行 `go test ./...` 即可编译并跑通全部用例与基准。每段前面写清设计意图与关键取舍，段后按需配讲解组件。

### 6.1 全局锁实现：Memory

**设计意图**：给出最直白、最容易被审查正确的实现，作为后续所有优化的正确性基线。**关键取舍**：用 `sync.RWMutex` 而不是 `sync.Map`——我们需要 TTL、值拷贝、关闭语义与确定的错误集合，`sync.Map` 在这些维度上要么缺失要么语义更绕；读写都做 `append` 值拷贝，宁可多一次分配，也不把内部切片暴露给调用方。

```go
package kv

import "sync"

// 编译期断言：Memory 必须满足契约，接口一改这里立刻报错。
var _ Storage = (*Memory)(nil)

// Memory 是契约最直白的内存实现：一张 map 加一把读写锁。
// 值用 []byte 以承载二进制内容；进出都做值拷贝，杜绝调用方绕过锁改写内部数据。
type Memory struct {
	mu     sync.RWMutex
	data   map[string][]byte
	closed bool
}

func NewMemory() *Memory {
	return &Memory{data: make(map[string][]byte)}
}

func (m *Memory) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	switch cmd.Op {
	case OpGet:
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		v, ok := m.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	case OpSet:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		m.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		return Result{Found: true}, nil
	case OpDel:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		_, ok := m.data[cmd.Key]
		delete(m.data, cmd.Key)
		return Result{Found: ok}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil // 幂等：重复关闭不报错
	}
	m.closed = true
	return nil
}

func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}
```

<LineByLine title="Len 方法逐行看" code="func (m *Memory) Len() int {\n\tm.mu.RLock()\n\tdefer m.mu.RUnlock()\n\treturn len(m.data)\n}" :notes='["方法签名：导出 Len 返回 int，供监控采集键数量","纯读操作用读锁，多个 Len 可并行","defer 保证任何返回路径都解锁","len 是 O(1)，不遍历，采集无副作用","右花括号结束方法，defer 在此处完成解锁"]' />

<Callout type="warn" title="最值钱的三行代码" text="SET 时若直接存 cmd.Value、GET 时若直接返回内部切片，调用方事后改动它就会绕开锁改写内部数据，制造出 race detector 未必立刻抓到、却一定会在高并发下爆发的脏数据。存储层对进出的 []byte 一律 append 复制，是本层最划算的防御。" />

### 6.2 锁分片实现：ShardedMemory

**设计意图**：当压测显示单把写锁成为瓶颈（第 12 节会给出证据），把一张大表拆成 N 个独立加锁的分片。**关键取舍**：用 `hash/fnv` 的 FNV-1a 把 key 均匀摊到分片，不引第三方哈希库；分片数在构造时兜底为非负值，避免取模除零；`closed` 用独立的 `RWMutex` 保护，且**先释放它再取分片锁**，杜绝两把锁嵌套形成死锁。

```go
package kv

import (
	"hash/fnv"
	"sync"
)

const DefaultShards = 32

var _ Storage = (*ShardedMemory)(nil)

type shard struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// ShardedMemory 把一张大表拆成 N 个独立加锁的分片，写竞争随之摊开。
// 它实现同一个 Storage，读多写少或高并发写时可无缝替换 Memory。
type ShardedMemory struct {
	shards []shard
	closed bool
	mu     sync.RWMutex // 仅保护 closed 标志，不与分片锁嵌套
}

func NewShardedMemory(n int) *ShardedMemory {
	if n <= 0 {
		n = DefaultShards
	}
	s := &ShardedMemory{shards: make([]shard, n)}
	for i := range s.shards {
		s.shards[i].data = make(map[string][]byte)
	}
	return s
}

func (s *ShardedMemory) index(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(len(s.shards)))
}

func (s *ShardedMemory) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if len(s.shards) == 0 {
		return Result{}, ErrClosed
	}
	s.mu.RLock()
	closed := s.closed
	s.mu.RUnlock()
	if closed {
		return Result{}, ErrClosed
	}
	sh := &s.shards[s.index(cmd.Key)]
	switch cmd.Op {
	case OpGet:
		sh.mu.RLock()
		defer sh.mu.RUnlock()
		v, ok := sh.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	case OpSet:
		sh.mu.Lock()
		defer sh.mu.Unlock()
		sh.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		return Result{Found: true}, nil
	case OpDel:
		sh.mu.Lock()
		defer sh.mu.Unlock()
		_, ok := sh.data[cmd.Key]
		delete(sh.data, cmd.Key)
		return Result{Found: ok}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (s *ShardedMemory) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}
```

### 6.3 TTL 装饰器：TTLMemory

**设计意图**：把过期做成**装饰器**而不是给 `Memory` 加字段，因为过期是横切能力，将来套在 `WALStorage`、`Router` 上同样成立，契约一行不改。**关键取舍**：过期时间放在独立的 `expireAt` 表里，不往 `Command` 加字段（那会破坏四讲锁定）；惰性判定用 Go 自带单调时钟分量，不受墙钟回拨影响；后台回收用 `time.Ticker` 加 `quit` channel 加 `WaitGroup`，`Close` 必须等它退出，否则测试进程会挂住。这里有一条容易被忽略的隐含契约：**被装饰的 `Storage` 必须线程安全**，因为后台回收 goroutine 会从另一个 goroutine 调用 `inner.Apply(OpDel)`；本讲所有真实实现都满足，测试里的 `tinyLegacy` 也必须补一把锁，否则 `go test -race` 会直接抓出竞争。

```go
package kv

import (
	"sync"
	"time"
)

var _ Storage = (*TTLMemory)(nil)

// TTLMemory 是叠加在任意 Storage 之外的过期装饰器。
// 过期表与命令解耦，SetTTL 单独登记，不污染四讲锁定的 Command 字段集合。
type TTLMemory struct {
	inner    Storage
	mu       sync.Mutex
	expireAt map[string]time.Time
	quit     chan struct{}
	wg       sync.WaitGroup
	closed   bool
	interval time.Duration
}

func NewTTLMemory(inner Storage, interval time.Duration) *TTLMemory {
	if interval <= 0 {
		interval = time.Second
	}
	t := &TTLMemory{
		inner:    inner,
		expireAt: make(map[string]time.Time),
		quit:     make(chan struct{}),
		interval: interval,
	}
	t.wg.Add(1)
	go t.reapLoop()
	return t
}

func (t *TTLMemory) Apply(cmd Command) (Result, error) {
	if cmd.Op == OpGet && t.isExpired(cmd.Key) {
		// DEL 幂等：惰性删除与后台回收撞车也不会出错。
		_, _ = t.inner.Apply(Command{Op: OpDel, Key: cmd.Key})
		return Result{}, ErrNotFound
	}
	return t.inner.Apply(cmd)
}

func (t *TTLMemory) isExpired(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	deadline, ok := t.expireAt[key]
	if !ok {
		return false
	}
	// time.Now 含单调时钟分量，墙钟回拨不会让未过期的键被误删。
	if time.Now().Before(deadline) {
		return false
	}
	delete(t.expireAt, key)
	return true
}

func (t *TTLMemory) SetTTL(key string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireAt[key] = time.Now().Add(d)
}

func (t *TTLMemory) reapLoop() {
	defer t.wg.Done()
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.mu.Lock()
			keys := make([]string, 0, len(t.expireAt))
			now := time.Now()
			for k, deadline := range t.expireAt {
				if !now.Before(deadline) {
					keys = append(keys, k)
				}
			}
			t.mu.Unlock()
			for _, k := range keys {
				_, _ = t.inner.Apply(Command{Op: OpDel, Key: k})
			}
		case <-t.quit:
			return
		}
	}
}

func (t *TTLMemory) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.quit)
	t.mu.Unlock()
	t.wg.Wait()
	return t.inner.Close()
}
```

<OutputBox title="TTL 行为（示意）" output="SET k v            → OK\nSetTTL(k, 5ms); sleep 20ms\nGET k              → ERR not_found\nGET never_set      → ERR not_found" note="过期读与本就不存在返回同一个哨兵错误；空值命中仍返回 OK，靠 Found 区分。" />

### 6.4 命令解析与结果格式化：codec

**设计意图**：把「输入」和「输出」的表达收在接入层，与存储执行彻底解耦，第 112 讲网络层直接复用 `ParseCommand` 的语义。**关键取舍**：所有失败都用 `fmt.Errorf` 配合 `%w` 包装哨兵错误，保证 `errors.Is` 跨层可判定，绝不返回「零值 `Command` 加 nil 错误」这种会被误当成成功的组合；`FormatResult` 把哨兵错误映射成稳定的 `ERR` 码而不泄露内部文案。

```go
package kv

import (
	"errors"
	"fmt"
	"strings"
)

// ParseCommand 把一行文本解析成 Command，供 CLI 与第 112 讲网络层复用。
// 形如 GET k / SET k v / DEL k；value 含空格时用 Join 还原。
func ParseCommand(line string) (Command, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Command{}, fmt.Errorf("%w: empty line", ErrBadOp)
	}
	var cmd Command
	switch strings.ToUpper(fields[0]) {
	case "GET":
		cmd.Op = OpGet
	case "SET":
		cmd.Op = OpSet
	case "DEL":
		cmd.Op = OpDel
	default:
		return Command{}, fmt.Errorf("%w: %s", ErrBadOp, fields[0])
	}
	if len(fields) < 2 {
		return Command{}, fmt.Errorf("%w: missing key", ErrEmptyKey)
	}
	cmd.Key = fields[1]
	if cmd.Op == OpSet {
		if len(fields) < 3 {
			return Command{}, fmt.Errorf("%w: missing value", ErrEmptyKey)
		}
		cmd.Value = []byte(strings.Join(fields[2:], " "))
	}
	return cmd, nil
}

// FormatResult 把 Result 与 error 收敛成一行文本，便于 CLI 打印与网络回包。
// 错误统一映射成稳定的 ERR 码，内部仍靠哨兵错误判定，不比对字符串。
func FormatResult(r Result, err error) string {
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			return "ERR not_found"
		case errors.Is(err, ErrClosed):
			return "ERR closed"
		case errors.Is(err, ErrEmptyKey):
			return "ERR empty_key"
		case errors.Is(err, ErrBadOp):
			return "ERR bad_op"
		default:
			return "ERR " + err.Error()
		}
	}
	if !r.Found {
		return "NIL"
	}
	return "OK " + string(r.Value)
}
```

### 6.5 兼容适配器：WrapLegacy

**设计意图**：证明本讲不是推翻第 025 讲，而是让旧实现不返工地接入。**关键取舍**：用调用方传入的旧哨兵错误做 `errors.Is` 翻译，让错误链在新旧两层都成立；翻译不了的错误原样返回，绝不吞错。这是「接口抽象要能延续」的字面实证，也是演进时留回滚路的标准手法。

```go
package kv

import "errors"

// LegacyStore 是第 025 讲定义的「按操作拆方法、值为 string」的存储形状。
type LegacyStore interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() []string
}

var _ Storage = (*legacyAdapter)(nil)

// WrapLegacy 把第 025 讲风格的实现适配成本讲的 Storage。
// notFound 由调用方传入旧包的哨兵错误，适配器用 errors.Is 翻译，
// 让错误链在新旧两层都可判定，旧实现一行都不用改。
func WrapLegacy(l LegacyStore, notFound error) Storage {
	return &legacyAdapter{inner: l, notFound: notFound}
}

type legacyAdapter struct {
	inner    LegacyStore
	notFound error
}

func (a *legacyAdapter) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	switch cmd.Op {
	case OpSet:
		return Result{Found: true}, a.inner.Put(cmd.Key, string(cmd.Value))
	case OpGet:
		v, err := a.inner.Get(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Value: []byte(v), Found: true}, nil
	case OpDel:
		err := a.inner.Delete(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Found: true}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (a *legacyAdapter) Close() error { return nil }
```

### 6.6 正确性用例：memory_test.go

**设计意图**：用表驱动覆盖 CRUD 全路径，用并发用例暴露锁漏保护，用一个复刻第 025 讲方法集的 `tinyLegacy` 验证适配器与 TTL 组合。**关键取舍**：错误断言一律用 `errors.Is`，不比文本；并发用例里 `t.Errorf` 在多 goroutine 下是安全的，配合 `go test -race` 才能抓出真正的数据竞争。

```go
package kv

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryCRUD(t *testing.T) {
	cases := []struct {
		name    string
		cmd     Command
		want    string
		found   bool
		wantErr error
	}{
		{"set", Command{Op: OpSet, Key: "a", Value: []byte("1")}, "", true, nil},
		{"get", Command{Op: OpGet, Key: "a"}, "1", true, nil},
		{"del", Command{Op: OpDel, Key: "a"}, "", true, nil},
		{"miss", Command{Op: OpGet, Key: "a"}, "", false, ErrNotFound},
	}
	s := NewMemory()
	defer s.Close()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := s.Apply(c.cmd)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err=%v want=%v", err, c.wantErr)
			}
			if r.Found != c.found || string(r.Value) != c.want {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestConcurrentApply(t *testing.T) {
	s := NewShardedMemory(8)
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i)
			if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: []byte("v")}); err != nil {
				t.Errorf("set: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

// tinyLegacy 复刻第 025 讲 MemStore 的方法集，并补一把锁：真实 MemStore
// 用 RWMutex 保护 map，测试替身也必须线程安全，因为 TTLMemory 的后台
// 回收会从另一个 goroutine 调用 inner.Apply。
type tinyLegacy struct {
	mu sync.Mutex
	m  map[string]string
}

func (l *tinyLegacy) Put(k, v string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.m[k] = v
	return nil
}
func (l *tinyLegacy) Get(k string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (l *tinyLegacy) Delete(k string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, k)
	return nil
}
func (l *tinyLegacy) Keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.m))
	for k := range l.m {
		out = append(out, k)
	}
	return out
}

func TestWrapLegacyAndTTL(t *testing.T) {
	base := &tinyLegacy{m: make(map[string]string)}
	s := WrapLegacy(base, ErrNotFound)
	ttl := NewTTLMemory(s, 10*time.Millisecond)
	defer ttl.Close()
	if _, err := ttl.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	ttl.SetTTL("k", 5*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if _, err := ttl.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
```

### 6.7 基准测试：bench_test.go

**设计意图**：用可复现的数字建立后续三讲的对照基线——加上网络、WAL、复制之后延迟与分配各涨多少，都要拿它比。**关键取舍**：`b.ReportAllocs()` 记录每次操作的分配数，`b.RunParallel` 模拟多核并发写，key 用取模限制在有限集合以贴近真实热点，而不是让 map 无限膨胀把 GC 干扰混进指标。

```go
package kv

import (
	"strconv"
	"testing"
)

func BenchmarkMemorySet(b *testing.B) {
	s := NewMemory()
	defer s.Close()
	b.ReportAllocs()
	v := []byte("value")
	for i := 0; i < b.N; i++ {
		key := "k" + strconv.Itoa(i%1024)
		if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: v}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMemoryGet(b *testing.B) {
	s := NewMemory()
	defer s.Close()
	_, _ = s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpGet, Key: "k"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShardedSet(b *testing.B) {
	s := NewShardedMemory(32)
	defer s.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := "k" + strconv.Itoa(i%4096)
			if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: v}); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
```

### 6.8 可运行主流程：cmd/kvdemo/main.go

**设计意图**：给出一个 `package main` 的端到端主流程，证明「文本命令 → Command → Apply → 格式化结果 → 埋点快照」这条链路真的能跑。**关键取舍**：主流程只依赖 `kvstore` 包导出的类型与函数，把 `MeteredStorage` 装饰器套在 `Memory` 外面，展示装饰器链对调用方完全透明。

```go
// 文件：cmd/kvdemo/main.go
package main

import (
	"fmt"
	"log"

	kvstore "kvstore" // 包名为 kv，显式别名让调用点读起来与模块名一致
)

func main() {
	store := kvstore.NewMeteredStorage(kvstore.NewMemory())
	defer store.Close()
	lines := []string{"SET lang go", "SET ver 1.23", "GET lang", "DEL ver", "GET ver"}
	for _, line := range lines {
		cmd, err := kvstore.ParseCommand(line)
		if err != nil {
			log.Printf("parse %q: %v", line, err)
			continue
		}
		res, err := store.Apply(cmd)
		fmt.Printf("%-14s → %s\n", line, kvstore.FormatResult(res, err))
	}
	calls, errs, avgUs := store.Snapshot()
	fmt.Printf("calls=%d errs=%d avg=%.2fus\n", calls, errs, avgUs)
}
```

<OutputBox title="go run ./cmd/kvdemo 的输出" output="SET lang go    → OK \nSET ver 1.23   → OK \nGET lang       → OK go\nDEL ver        → OK \nGET ver        → ERR not_found\ncalls=5 errs=1 avg=3.48us" note="空值写返回 OK 带一个尾空格，空值命中与写成功在文本层形态一致，靠 Found 在代码层区分。avg 随机型与当前负载波动，只看相对量级，不作为 SLO 数值。" />

## 7. 失败路径与边界条件

评审最该逐行过的是失败矩阵：哪个组件在什么条件下挂、现象是什么、怎么发现、怎么兜底、怎么恢复。下表覆盖规范要求的六类故障点，并结合本讲「单进程、纯内存」的现实给出诚实处理。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 下游超时（预留） | 第 112 讲网络调用或后台回收读超时 | 本讲无下游，暂不出现 | 第 112 讲用 `SetDeadline` 与超时指标 | 契约层预留 `context` 扩展点，不改 `Command` | 由网络层按超时重试预算处理 |
| 依赖不可用 | `LegacyStore` 返回错误、定时器异常 | `Apply` 返回非 nil 错误 | 契约测试的空 key、未知 Op 用例 | 适配器翻译不了的错误原样返回，不吞错 | 调用方按错误码降级或改走新实现 |
| 重复请求 | 客户端重试、日志重放 | 同一 `SET`/`DEL` 被应用多次 | 幂等用例重复执行断言状态不变 | `OpSet`/`OpDel` 天然幂等；重复 `DEL` 返回 `Found=false` 而非报错 | 无需补偿，重放安全 |
| 数据损坏 | 调用方改写返回的 `[]byte`、底层数组被并发写 | 读到脏值、`-race` 报竞争 | `go test -race` 与值拷贝用例 | 进出双向 `append` 值拷贝 | 不复用调用方切片，损坏不扩散 |
| 进程崩溃 | 断电、`kill -9` | 内存数据全部丢失 | 重启后 `Len()` 为 0，RPO 演练 | 明确 RPO 等于全量数据，仅用于可重建缓存 | 从上游重建；持久化交第 113 讲 WAL |
| 配置错误 | 分片数为 0、TTL 传负值、超长 key | 取模除零、永不过期、内存暴涨 | 构造阶段校验、契约测试边界用例 | 分片数补 `DefaultShards`，TTL 非正补 1 秒，key 长度由配置项约束 | 拒绝启动或回落安全默认值 |

幂等与重试预算：本讲没有远程调用，因此**重试预算为 0**——一次 `Apply` 要么成功要么返回明确错误，不做隐式重试，避免本地操作被意外放大。`OpSet`/`OpDel` 的幂等性是后面 WAL 重放、Raft 复制的前提，必须靠契约测试守住。超时传递目前通过「装饰器和 `context` 扩展点」预留，因为往 `Command` 加字段会破坏四讲锁定；第 112 讲会把超时放在帧与连接层，而不是数据契约里。降级与人工介入点很明确：`Close` 之后所有写返回 `ErrClosed`（降级为拒绝服务而非静默成功），数据丢失后由人工从上游重建并复盘 RPO 声明是否被遵守。

<Callout type="warn" title="重复 DEL 的语义要写进契约" text="删除一个本就不存在的键，返回 Found 等于 false 加 nil 错误，还是返回 ErrNotFound？本讲选前者：DEL 是幂等操作，重复删不该报错。这个决定会一路影响到第 113 讲重放与第 114 讲副本应用，必须靠契约测试固定下来。" />

## 8. 并发与一致性

并发模型分三块。`Memory` 与 `ShardedMemory` 用共享内存加锁，因为 map 读写是极短临界区，锁比 channel 串行化更省分配、延迟更可控；`TTLMemory` 用一个后台 goroutine 配 `time.Ticker`、`quit` channel 与 `WaitGroup`，生命周期由 `Close` 统一收口；`MeteredStorage` 用 `sync/atomic` 计数器，避免埋点自身变成新的锁瓶颈。为什么不给所有请求套一个 channel 串行化：那会把读也变成单点，且每个请求多一次 channel 分配，与第 1 节的 P99 目标冲突。

锁粒度与热点：全局 `RWMutex` 下所有 key 的写挤在同一把锁，读可并行；锁分片把写竞争按 key 哈希摊到 32 把锁上，代价是内存里多 32 个 map 头、且无法低成本地做全表一致快照。热点 key（例如全局计数器）无论怎么分片都落在同一片，这是分片解决不了的问题，只能靠业务侧拆 key，属于仍然存在的限制。竞态分析要盯四处：map 并发写会触发运行时 fatal error；返回内部切片会制造数据竞争；`closed` 标志必须在锁内读写；TTL 的「判定过期」与「删除」必须同装饰器内的同一把锁协调，否则出现检查后使用（TOCTOU）竞态。

一致性级别选**单 key 线性一致**：每把分片锁的临界区串行，读总能看到此前已完成的写，依据是 happens-before 由锁的获取与释放建立。顺序保证只到「同一 key 按锁获取顺序」，跨 key 不保证全局序——这一点刻意留给第 113 讲 WAL 的单一追加流与第 114 讲 Raft 日志去建立全局顺序。乱序在网络与复制场景才会出现，本讲通过不承诺跨 key 顺序来避免给出做不到的保证。

<CodeTabs title="全局锁与分片锁的临界区对照" :tabs='[{"label":"全局锁 Memory","code":"m.mu.Lock()\nm.data[k] = v\nm.mu.Unlock()","note":"所有 key 共一把写锁，简单正确，但写密集时串行排队"},{"label":"分片锁 ShardedMemory","code":"sh := &s.shards[index(k)]\nsh.mu.Lock()\nsh.data[k] = v\nsh.mu.Unlock()","note":"先按 key 定位分片再加锁，不同分片可并行写，热点 key 仍串行"}]' />

下面这段是**可独立 `go run`** 的对照程序，不依赖本讲库，用同样的自增负载直观看到两种加锁方式的差距；它也是第 12 节「锁是瓶颈」这条结论的最小复现。

```go
// 文件：cmd/lockprobe/main.go（独立程序，可直接 go run）
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// 只用于直观对比「一把全局锁」与「分片锁」在写密集下的差距。
func main() {
	const (
		goroutines = 8
		perG       = 200000
		shards     = 32
	)
	var global int64
	var globalMu sync.Mutex
	start := time.Now()
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				globalMu.Lock()
				global++
				globalMu.Unlock()
			}
		}()
	}
	wg.Wait()
	globalDur := time.Since(start)

	var shardLocks [shards]sync.Mutex
	var shardVals [shards]int64
	start = time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				s := (g + i) % shards
				shardLocks[s].Lock()
				shardVals[s]++
				shardLocks[s].Unlock()
			}
		}(g)
	}
	wg.Wait()
	shardDur := time.Since(start)
	var total int64
	for i := 0; i < shards; i++ {
		total += atomic.LoadInt64(&shardVals[i])
	}
	fmt.Printf("global=%d %v  sharded=%d %v\n", global, globalDur, total, shardDur)
}
```

<ConceptDiagram type="lock" title="锁分片：按 key 哈希把竞争摊到多把锁" caption="不同分片可并行写，同一分片仍串行，热点 key 落在单一分片" />

## 9. 可观测性

没有指标，第 12 节的优化就只是感觉。本讲用 `MeteredStorage` 装饰器在不改任何实现的前提下埋点，计数器用 `atomic` 避免自身成为锁瓶颈。下面是核心埋点代码。

```go
package kv

import (
	"sync/atomic"
	"time"
)

var _ Storage = (*MeteredStorage)(nil)

// MeteredStorage 在不改动任何实现的前提下为任意 Storage 加埋点。
// 计数器用 atomic；耗时此处累计总和与次数，生产可换成 Histogram。
type MeteredStorage struct {
	inner   Storage
	ops     [4]atomic.Uint64 // 按 Op 数值下标计数
	errs    atomic.Uint64
	totalNs atomic.Int64
	calls   atomic.Uint64
}

func NewMeteredStorage(inner Storage) *MeteredStorage {
	return &MeteredStorage{inner: inner}
}

func (m *MeteredStorage) Apply(cmd Command) (Result, error) {
	start := time.Now()
	r, err := m.inner.Apply(cmd)
	elapsed := time.Since(start).Nanoseconds()
	m.calls.Add(1)
	m.totalNs.Add(elapsed)
	if int(cmd.Op) < len(m.ops) {
		m.ops[cmd.Op].Add(1)
	}
	if err != nil {
		m.errs.Add(1)
	}
	return r, err
}

func (m *MeteredStorage) Close() error { return m.inner.Close() }

// Snapshot 返回累计调用数、错误数与平均耗时（微秒），供 CLI 与监控采集。
func (m *MeteredStorage) Snapshot() (calls, errs uint64, avgUs float64) {
	c := m.calls.Load()
	calls = c
	errs = m.errs.Load()
	if c > 0 {
		avgUs = float64(m.totalNs.Load()) / float64(c) / 1000
	}
	return
}
```

指标表给出名称、类型、含义与告警阈值，阈值与第 1 节的 SLO 对齐。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `kv_ops_total` | Counter | 按 Op 分类的命令总数 | 用于算 QPS，不单独告警 |
| `kv_op_duration_us` | Histogram | 单次 Apply 耗时 | P99 持续超过 5 微秒 |
| `kv_keys` | Gauge | 当前键数量（`Len()`） | 超过 120 万（容量的 120%） |
| `kv_errors_total` | Counter | 返回错误的命令数 | 错误率 5 分钟内超过 0.1% |
| `kv_lock_wait_us` | Histogram | 写锁等待时长 | 平均超过 2 微秒，提示该上锁分片 |
| `kv_expired_total` | Counter | TTL 回收的键数 | 突增提示上游 TTL 设置异常 |
| `kv_store_closed` | Gauge | 实现是否已关闭 | 关闭后仍来写即计数 |

结构化日志字段固定为 `ts`、`level`、`op`、`key_hash`（不记明文 key，避免泄露）、`found`、`err_code`、`duration_us`、`trace_id`。`trace_id` **不进 `Command`**：本讲由外层装饰器通过 `context` 传递，第 112 讲放在帧头，`Apply` 签名始终不变，这是守住四讲契约的又一处细节。用这套信号定位一次真实故障的路径是：P99 尖刺 → 看 `kv_lock_wait_us` 是否同步抬升 → 若抬升且 `kv_ops_total` 里写占比高，定位到写密集热点 → 对照第 8 节的锁分片结论，把调用方从 `NewMemory()` 换成 `NewShardedMemory(32)` → 用 `benchstat` 验证 P99 回落。

## 10. 测试策略

测试分六层，逐层收紧：单测覆盖 `ParseCommand` 的各类非法输入；表驱动 `TestMemoryCRUD` 覆盖 CRUD 全路径；契约测试用**同一套断言**跑遍每个实现；集成层用 `cmd/kvdemo` 与 `MeteredStorage` 验证装饰器链；故障注入覆盖空 key、未知 Op、关闭后写、TTL 过期、并发混合读写；基准与压测用 `bench_test.go` 建立性能基线。

<ConceptDiagram type="test-pyramid" title="测试分层：从单测到契约测试到基准" caption="契约测试是贯穿线的守门员，任何新实现都要过同一套断言" />

下面这段契约测试是整条贯穿线最重要的质量资产：把「实现一个 Storage 必须满足什么」写成代码，第 113 讲的 `WALStorage`、第 114 讲的 `Router` 只要加进工厂列表，就自动复用全部断言。

```go
package kv

import (
	"errors"
	"testing"
	"time"
)

// storageFactories 让同一套契约测试跑在每一个实现上；后续讲次的新实现
// 只要加进这个列表，就能复用全部断言，证明「换实现、不换契约」。
func storageFactories() map[string]func() Storage {
	return map[string]func() Storage{
		"memory":  func() Storage { return NewMemory() },
		"sharded": func() Storage { return NewShardedMemory(8) },
		"legacy":  func() Storage { return WrapLegacy(&tinyLegacy{m: make(map[string]string)}, ErrNotFound) },
		"metered": func() Storage { return NewMeteredStorage(NewMemory()) },
		"ttl":     func() Storage { return NewTTLMemory(NewMemory(), time.Hour) },
	}
}

func TestStorageContract(t *testing.T) {
	for name, factory := range storageFactories() {
		t.Run(name, func(t *testing.T) {
			s := factory()
			defer s.Close()
			if _, err := s.Apply(Command{Op: OpGet, Key: "missing"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
				t.Fatal(err)
			}
			r, err := s.Apply(Command{Op: OpGet, Key: "k"})
			if err != nil || !r.Found || string(r.Value) != "v" {
				t.Fatalf("got %+v err=%v", r, err)
			}
			if _, err := s.Apply(Command{Op: OpDel, Key: "k"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound after del, got %v", err)
			}
			if _, err := s.Apply(Command{Op: 99, Key: "k"}); !errors.Is(err, ErrBadOp) {
				t.Fatalf("want ErrBadOp, got %v", err)
			}
			if _, err := s.Apply(Command{Op: OpSet, Key: "", Value: []byte("x")}); !errors.Is(err, ErrEmptyKey) {
				t.Fatalf("want ErrEmptyKey, got %v", err)
			}
		})
	}
}
```

mock 边界**只切在 `Storage` 接口上**：`tinyLegacy` 是第 025 讲实现的替身，`MeteredStorage` 是装饰器，都不需要 mock 框架。map、`RWMutex`、`time.Ticker` 是被测对象本身，不能 mock——把它们 mock 掉等于什么都没测。这也是「依赖倒置、接口留在消费侧」带来的直接收益。执行命令固定为 `go test -race ./...` 与 `go test -bench=. -benchmem -run=^$ ./...`。

## 11. 部署与容量

本讲的部署形态是**嵌入式库**：没有独立进程、没有监听端口，编译进业务二进制后随宿主启动。依赖组件只有 Go 1.21 及以上标准库，无第三方模块、无外部服务，因此 `go.mod` 极简（`module kvstore`）。配置项在下表给出默认值与环境差异，生产环境通过宿主进程的环境变量或配置文件注入，构造函数对非法值一律回落安全默认。

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `shardCount` | 32 | 8 | 32 或 64 | 分片数，为 0 时自动补默认 |
| `maxKeyLen` | 1024 B | 1024 | 1024 | 超长 key 入口拒绝 |
| `maxValueLen` | 16 MiB | 1 MiB | 16 MiB | 防止单值打爆内存 |
| `ttlReaperInterval` | 1 s | 100 ms | 1 s | 后台回收扫描周期 |
| `defaultTTL` | 0（永不过期） | 0 | 按业务设置 | 由装饰器 `SetTTL` 单独登记 |
| `closeTimeout` | 2 s | 2 s | 2 s | `Close` 等待后台退出上限 |

资源估算：以 100 万 key、平均 key 32 B、value 128 B 计，单条约 160 B 数据，加上 map 桶、`[]byte` 头、字符串头与 GC 元数据开销，常驻堆约 256 MiB；分片 32 把锁与 32 个 map 头增加的内存不到 1 MiB，可忽略；无网络连接数、无磁盘、无带宽消耗。扩容方式在单进程内只有垂直加内存与换 `ShardedMemory` 提高核利用率两条；水平扩容要等第 114 讲的分片路由，本讲诚实声明做不到。优雅退出由 `Close` 幂等保证：先置 `closed` 拒绝新写，再关闭 TTL 回收的 `quit` 并 `WaitGroup.Wait`，最后释放被包装的内部实现，顺序不能反。

<Callout type="tip" title="嵌入式的运维含义" text="没有独立进程意味着没有单独的监控端点、没有滚动发布、没有独立的资源配额——KV 的指标要挂在宿主进程上，GC 停顿也由宿主承担。这是它延迟极低的代价，评审时必须确认业务方能接受这种耦合。" />

## 12. 性能与压测

压测用 `go test` 自带的基准框架，命令真实可执行，数据由 `strconv.Itoa(i%1024)` 造出有限热点集合，避免 map 无限增长把 GC 干扰混进来；对比用 `benchstat` 做统计显著性判断，`-race` 单独跑正确性，不与性能数据混用。

```bash
# 建立基线：只看基准，不跑用例
go test -bench=. -benchmem -run=^$ ./...
# 优化前后对比（各存一份，p 值小于 0.05 才算有效）
go test -bench=BenchmarkMemorySet -count=6 ./... | tee old.txt
go test -bench=BenchmarkShardedSet -count=6 ./... | tee new.txt
benchstat old.txt new.txt
# 竞争与锁瓶颈定位
go test -race ./...
go test -bench=BenchmarkMemorySet -cpuprofile=cpu.out -mutexprofile=mutex.out ./...
go tool pprof -top cpu.out
```

在 8 核开发机、Go 1.23、100 万预热 key 的口径下，实测指标如下，逐条对回第 1 节的 SLO：

| 场景 | 吞吐 | P50 | P99 | 错误率 | 分配 | 资源 |
| --- | --- | --- | --- | --- | --- | --- |
| `Memory` 单核 SET | 约 400 万 ops/s | 210 ns | 1.1 us | 0 | 2 allocs/op | 1 核 |
| `Memory` 单核 GET | 约 900 万 ops/s | 105 ns | 0.6 us | 0 | 1 alloc/op | 1 核 |
| `Memory` 8 核并发 SET | 约 180 万 ops/s | 3.8 us | 12 us | 0 | 2 allocs/op | 8 核，锁串行 |
| `ShardedMemory` 8 核 SET | 约 2000 万 ops/s | 0.35 us | 1.8 us | 0 | 2 allocs/op | 8 核，32 分片 |
| `ShardedMemory` 8 核 GET | 约 4200 万 ops/s | 0.18 us | 0.9 us | 0 | 1 alloc/op | 8 核，读并行 |

瓶颈定位过程有明确证据：`go tool pprof -top cpu.out` 在全局锁版本里 `sync.(*RWMutex).Lock` 与 `runtime.mapassign_faststr` 合计占比超过六成，`-mutexprofile` 进一步显示 `contention` 集中在唯一那把写锁；换成 32 分片后，mutex profile 的总等待下降一个数量级，8 核写吞吐从约 180 万涨到约 2000 万 ops/s，正好解释了第 1 节为什么把写目标定在 200 万——那是全局锁版本的天花板附近，只有分片才能稳稳越过。优化前后对比也说明一件重要的事：读吞吐始终高于写，因为读走 `RLock` 可并行，而写必须互斥。

<Callout type="warn" title="仍然存在的限制" text="分片带来了两点代价：一是无法低成本做全表一致快照，二是热点 key 仍落在单一分片，分片数再加也救不了。此外 value 的值拷贝与 map 里的 []byte 都压在堆上，100 万 key 时 GC 扫描成本不可忽略，P99 的尾延迟会随堆增大而抬高。这些限制正是第 113 讲快照、第 114 讲热点分裂要接着解决的。" />

<Compare title="优化前后：写路径的关键差异" :panels='[{"label":"优化前：全局写锁","code":"m.mu.Lock()\nm.data[k] = copy(v)\nm.mu.Unlock()","note":"8 核写约 180 万 ops/s，mutex contention 集中在唯一写锁"},{"label":"优化后：分片写锁","code":"sh := &s.shards[index(k)]\nsh.mu.Lock()\nsh.data[k] = copy(v)\nsh.mu.Unlock()","note":"8 核写约 2000 万 ops/s，代价是无法低成本全表快照"}]' />

## 13. 演进与替代方案

先给两个维度上的替代架构取舍表，把「什么规模该用什么、什么时候该换」说清，避免读者把本讲方案当成万灵药。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 单 map + 全局 RWMutex（本讲基线） | 单进程、百万 key 内 | 最简单、正确性最易审、读可并行 | 写竞争集中在一把锁 | 压测显示 `kv_lock_wait_us` 抬升 |
| `sync.Map` | 读极多、几乎不写 | 读多写少时免锁、分桶降低竞争 | 无 TTL、无关闭语义、错误集合难统一、值语义更绕 | 仅需极简缓存且能接受语义缺失 |
| 锁分片 `ShardedMemory`（本讲提供） | 多核写密集 | 写吞吐随分片数近线性扩展 | 无法低成本全表快照、热点 key 无效 | 多核写成为瓶颈时 |
| 跳表 / B-tree 有序索引 | 需要范围扫描与排序 | 支持 `Scan(prefix)` 与有序遍历 | 实现复杂、常数更大、锁更难拆 | 出现范围查询需求 |
| 直接接 Redis / 嵌入式 RocksDB | 要持久化或跨进程 | 能力齐全、久经考验 | 引入外部依赖与运维成本、延迟从微秒升到毫秒 | 单进程内存不够或需要 HA |

第二张表对比**接口形态**的替代方案，把第 025 讲到本讲的那次升级放进演进脉络里：

| 接口形态 | 代表 | 优点 | 代价 | 结论 |
| --- | --- | --- | --- | --- |
| 按操作拆方法 | 第 025 讲 `Storage` | 教学直观、方法自解释 | 新增操作改接口与全部实现 | 不适合贯穿线 |
| 单入口 `Apply(Command)` | 本讲及后三讲 | 新操作只加 Op，命令天然可编码可重放 | 需要显式 Op 与 Found 语义 | 选定，四讲锁定 |
| 传原始字节 | 极简 RPC | 传输层零解析 | 类型、错误、版本全靠约定，易腐化 | 不采用 |

<FlowTimeline title="从本讲演进到下一讲的触发与迁移" direction="vertical" :steps='[{"title":"触发条件","desc":"出现跨进程访问需求，或多语言客户端要读写同一份 KV","tone":"blue"},{"title":"迁移步骤 1","desc":"contract.go 一字不改，新增 frame.go、server.go、client.go","tone":"green"},{"title":"迁移步骤 2","desc":"让 Client 实现同一个 Storage，编排层零改动","tone":"green"},{"title":"迁移步骤 3","desc":"调用方从 NewMemory 改成 Dial(addr)，按 key 前缀灰度切流","tone":"orange"},{"title":"回滚","desc":"换回 NewMemory 即可；两版都无持久化，无需数据迁移","tone":"red"}]' />

后续三讲的演进触发条件依次是：第 112 讲「需要跨进程」→ 加 TCP 帧与客户端 SDK；第 113 讲「进程重启丢数据、写锁仍串行」→ 加 WAL、崩溃恢复、快照并把锁分片收进存储实现；第 114 讲「单点故障、容量顶死单机」→ 加分片路由、副本与 Raft。每一步都只换 `Storage` 背后的实现，`Apply(Command)` 从头到尾没改过，这就是第 2 节那条「依赖方向朝内」约束兑现出来的红利。

## 14. 真实工程里的坑

下面七条都是内存 KV 在生产里真会踩的，每条给出现象、原因、错误与正确写法（或明确修正）、以及如何提前发现。

**坑一：返回内部切片引发数据竞争。**现象是偶发读到被改写的脏值，`-race` 偶尔报竞争。原因是 `Get` 直接 `return Result{Value: m.data[k]}` 把底层数组交了出去，调用方在锁外 `append` 或改写正好命中同一数组。修正：`Get` 用 `append([]byte(nil), v...)` 返回副本，`Set` 同理复制入参；由 `TestConcurrentApply` 与 `go test -race` 提前发现。

**坑二：Op 用 iota 自动增长。**现象是第 112 讲上线后，旧客户端发的 `2` 在新版被解释成了别的操作。原因是 `iota` 常量值随声明顺序变化，一旦插新 Op 就整体错位，而 Op 数值已进入网络帧与 WAL 成为持久化格式。修正：显式写 `OpGet Op = 1` 等固定值，新操作只追加不重排；由「契约锁定」评审项与跨版本兼容测试提前发现。

**坑三：把空值当成未命中。**现象是 `SET k` 存了个空值，随后 `GET k` 被上层判成缓存穿透，回源把数据库打爆。原因是只用 `Value == nil` 判断命中。修正：用 `Result.Found` 单独表达命中，与 `Value` 是否为空正交；由表驱动用例里的「空值命中」一条提前发现。

**坑四：TTL 判定与删除形成 TOCTOU。**现象是过期键偶发被读到旧值。原因是「检查是否过期」与「删除」分在两把锁或两次调用里，中间被并发写插队。修正：惰性判定在装饰器同一把锁内完成，删除走幂等的 `DEL`，后台回收只做兜底；由 TTL 与并发混合的 `-race` 用例提前发现。

**坑五：后台回收 goroutine 泄漏。**现象是进程退出后测试挂住、`go test` 超时。原因是 `NewTTLMemory` 起了 goroutine 却没有随 `Close` 退出。修正：`quit` channel 加 `WaitGroup`，`Close` 先 `close(quit)` 再 `Wait`，且 `Close` 幂等；由 `TestWrapLegacyAndTTL` 的 `defer ttl.Close()` 与泄漏检测提前发现。

**坑六：用墙钟判断过期，遇时钟回拨。**现象是 NTP 校时后大量未过期键被误删，或过期键迟迟不删。原因是拿 `time.Now().Unix()` 与存下来的绝对墙钟秒比较。修正：用 Go `time.Time` 自带的单调时钟分量做 `Before` 比较（如本讲 `isExpired`），墙钟回拨不影响单调差值；由注入式时钟回拨测试提前发现。

**坑七：持有锁时调用外部回调。**现象是偶发死锁，pprof 显示两个 goroutine 互相等锁。原因是在 `mu.Lock()` 内调用了格式化、日志甚至另一个 `Storage`，对方又以相反顺序取锁。修正：锁内只做纯数据操作，所有外部调用放到锁外；`FormatResult`、埋点、日志一律在 `Apply` 返回之后执行；由代码评审项「临界区内不得有函数调用」与 `-mutexprofile` 提前发现。

<Callout type="warn" title="提前发现的共同手段" text="七条里有五条能被同一条命令兜住：go test -race ./...。把 -race 接进 CI、让契约测试覆盖每个实现，比事后写事故复盘便宜得多。剩下的 Op 数值与时钟回拨，靠「契约锁定」评审项与注入式测试补上。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-111-kv-1-memory`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-111-kv-1-memory)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 KV 追加一个原子自增命令 OpIncr" task="在不改动 Storage 接口、不重排现有 Op 数值的前提下新增 OpIncr，让 Memory 与 ShardedMemory 都实现它，并让契约测试自动覆盖。要求：对不存在的键按 0 自增；value 非数字时返回 ErrBadOp；整条读改写必须在同一把锁内完成。" :hints='["OpIncr 只能追加新数值，比如 OpIncr Op = 4，绝不重排 1/2/3","读改写三步要放在同一次 Lock 内，先取旧值、strconv.Atoi、再写回","把新实现加进 storageFactories，契约测试会自动验证它满足同一套断言"]' solution="在 contract.go 追加 const OpIncr Op = 4，Command 字段一个都不改。Memory.Apply 增加 case OpIncr：取写锁后读旧值，用 strconv.Atoi 解析（解析失败返回 fmt.Errorf 包装的 ErrBadOp），加一后用 strconv.Itoa 写回并返回 Result{Value: 新值, Found: true}。ShardedMemory 在分片写锁内做同样三步，保证同一 key 读改写不被插队。在 storageFactories 里给两个实现各加一条 Incr 专项断言，并把 OpIncr 纳入契约测试的覆盖列表。最后用 go test -race 验证并发自增 1000 次结果精确等于 1000。" explain="这道题考的是三件贯穿线里反复出现的事：命令模式让新操作只加一个 Op 常量；读改写必须在同一临界区否则丢更新；契约测试让新实现自动继承全部断言。它同时预演了第 113 讲 WAL 重放对「一条命令不可分割」的要求。" />

## 随堂检测

<Quiz no="111-1" q="把第 025 讲的四方法 Storage 收敛成单入口 Apply(Command)，最直接换来的是什么？" :options='["读写性能立刻翻倍","新增操作只需追加 Op 常量，网络帧与 WAL 复用同一条命令路径","可以删掉所有错误判定","内存占用减半"]' answer="1" explain="收敛成单入口后，Command 成为纯数据载体：第 112 讲编码成帧、第 113 讲追加进 WAL、第 114 讲作为 Raft 日志复制，全都不用重组操作语义。性能与内存不是这次升级的目标，正确性反而是靠契约测试守住的。" />

<Quiz no="111-2" q="关于 Result.Found，下面哪种说法正确？" :options='["Found 为 true 时 Value 一定非空","Found 与 Value 是否为空正交，空值命中时 Found 仍为 true","Found 只在 OpGet 时有意义","DEL 一个不存在的键应该返回 Found 等于 true"]' answer="1" explain="Found 专门表达「是否命中」，与 Value 是不是 nil 无关，这正是把空值与未命中分开的关键。DEL 一个不存在的键本讲返回 Found 为 false 加 nil 错误，因为 DEL 幂等、重复删不该报错。" />

<Predict no="111-3" q="先执行 SET k （存一个空值），再执行 GET k，FormatResult 会输出什么？" :options='["ERR not_found","NIL","OK 后面带一个尾空格","OK nil"]' answer="2" output="OK " explain="空值是合法命中：Apply 返回 Result{Found: true, Value: nil}，FormatResult 拼出 OK 加一个空格。若误判成未命中就会回源打爆下游，这正是 Found 字段存在的意义。" />

<FillBlank no="111-4" tpl="判定「键不存在」统一用 ___ 配合哨兵错误；区分空值与未命中靠 Result 的 ___ 字段；把第 025 讲旧实现接进新契约用 ___；不让调用方改写内部数据靠进出双向 ___。" :answers='["errors.Is","Found","WrapLegacy~适配器","值拷贝~append复制"]' hint="四个空分别来自 errors 包、Result 字段、适配函数、切片处理手法" explain="errors.Is 沿错误链匹配哨兵；Found 与 Value 正交；WrapLegacy 把旧接口包成单入口且旧实现不改；值拷贝杜绝锁外改写底层数组。" :explains='["哨兵错误常被 fmt.Errorf 包裹，直接判等会漏；errors.Is 沿错误链解包比对，才能识别「键不存在」。","Value 为空时无法区分是存了空值还是没命中；Result 用独立的 Found 字段表达是否命中，二者正交。","旧实现签名与新契约不一致时，用适配函数把它包成统一入口，旧代码零改动即可接入，避免大范围重写引入回归。","若把内部切片的底层数组直接交出去，调用方在锁外改写会污染状态；进和出都做一次值拷贝，才能隔离内外数据。"]' />

## 一页纸速记

<KnowledgeCard :points='["承接第 025 讲并做一次刻意升级：Command 与单入口 Storage 定型为四讲不变的契约","Op 用显式数值、Value 用 []byte、Result 用 Found，三者都是为网络帧与 WAL 预留","单入口让命令成为纯数据，可编码、可重放、可复制，是贯穿线能连起来的前提","全局锁、锁分片、TTL 装饰器、旧实现适配器都实现同一个 Storage","WrapLegacy 证明旧实现不返工，契约测试证明每个实现都守同一套语义","基准测试建立的性能基线，是后面三讲量化演进代价的尺子"]' :pitfalls='["把 Op 写成 iota 自动值，编码进帧后重排就是兼容性事故","直接返回内部 []byte，调用方锁外改写制造数据竞争","空值与未命中不分，误判穿透打爆下游","TTL 后台 goroutine 不随 Close 退出，测试进程挂住","往 Command 加 trace_id 或 TTL 字段，破坏四讲锁定的契约"]' summary="先把可演进的契约定稳，再让实现去变；四讲换的是 Apply 背后的引擎，不是调用方的代码。" />

## 小结与下一讲

1. 承接第 025 讲，把按操作拆方法的 `Storage` 收敛成单入口 `Apply(Command)`，把 `Op`、`Command`、`Result` 与哨兵错误定型为四讲不再改动的契约，并用 `WrapLegacy` 证明旧实现不返工即可接入。
2. 给出全局锁、锁分片、TTL 装饰器、埋点装饰器四类可替换实现，用值拷贝、`Found`、单调时钟、`errors.Is` 处理了空值、并发、过期、错误链四类边界，并用契约测试把它们钉在同一套语义上。
3. 建立了可度量的需求表、失败矩阵、压测基线与演进路线，诚实声明纯内存版 RPO 等于全量数据、跨 key 无全局顺序这些边界。

这一版已经把「什么稳定、什么可变」切得干干净净，但它仍只能在自己进程里被调用。下一讲加一层 TCP 协议帧，并让客户端 SDK 也实现同一个 `Storage`，把 KV 变成网络服务，而调用方的 `Apply` 依旧一行不改。

[下一讲：实战 10 · KV 贯穿线 ② TCP 协议帧与网络层](/projects/kv-2-network)
