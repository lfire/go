---
title: 第 112 讲 · KV 贯穿线 ② TCP 协议帧与网络层
description: 设计定长协议帧解决粘包，管理连接与并发，并让客户端 SDK 实现同一个 Storage 接口
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="把单机 KV 变成网络服务" prereq="第 111 讲 · 内存版与接口抽象" />

# 第 112 讲 · KV 贯穿线 ② TCP 协议帧与网络层

> 上一版（第 111 讲）已经把 KV 的契约定得很干净：`Command` 是纯数据，`Storage` 只有一个 `Apply`，内存实现、锁分片、TTL 装饰器都满足同一套契约测试。但它有一个绕不过去的硬伤——**这个 KV 只能在自己进程里被调用**。`NewMemory()` 出来的对象，别的机器、别的进程、别的语言都碰不到；业务服务和缓存逻辑必须编译进同一个二进制，既没法独立扩容，也没法让 Go 之外的团队复用。所以这一版只加一层「传输」：定义二进制协议帧、解决 TCP 粘包与半包、管理连接与并发、处理超时与优雅关闭，并且让**客户端 SDK 也实现同一个 `Storage` 接口**。于是调用方拿到的还是那个 `Apply(Command)`，底层是本地内存还是远程 TCP，它完全不用关心。这就是第 111 讲把接口定为「窄腰」换来的红利，也是这条贯穿线第一次真正「换实现、不换调用」。

<CheckList id="L112-goal" title="本讲目标" :items='["设计带魔数、版本、长度上限、请求 ID 与错误码的二进制协议帧","用定长头加 io.ReadFull 彻底解决粘包、半包与毒包","写出可优雅退出的服务端与实现同一 Storage 的客户端 SDK","用连接池与心跳把单连接串行拼成可并行的远程存储","让哨兵错误跨网络仍可用 errors.Is 判定"]' />

## 0. 项目定位与真实场景

把单机 KV 变成网络服务，是几乎所有存储系统都要走的一步。真实业务里，应用进程与缓存进程往往分开部署：Web 服务是无状态的多副本，KV 是独立的有状态服务；一个 KV 实例要被几十个应用副本、跨多种语言共同访问。以第 0 节开头的电商后台为例，会话与功能开关不能只存在某一个 Web 副本的内存里，否则用户下一次请求落到另一台机器就「掉线」了。把它抽成独立的 TCP 服务后，扩容、监控、故障隔离都有了明确的边界。

但「加一层网络」不是简单地把 `Apply` 包一层 socket。TCP 是**字节流**，没有消息边界：一次 `Write` 不对应一次 `Read`，一次 `Read` 可能只拿到半帧，也可能一次读到好几帧粘在一起。此外还有一串工程问题：别的协议误连了端口怎么办、客户端声明一个 4 GiB 的帧长把内存打爆怎么办、慢客户端占着 goroutine 不发完整帧怎么办、进程要重启时长连接不肯退出怎么办、错误跨网络之后还能不能判定。这一讲的核心矛盾是：**在不可靠、无边界、可能被恶意利用的字节流之上，重建出可靠、可判定、可演进的消息语义**，同时不碰第 111 讲锁定的 `Command` 与 `Storage`。

<Term word="粘包" text="TCP 是字节流不保留写边界，一次 Read 可能拿到半帧或多帧粘连，必须由应用层协议用长度或分隔符自己切分" />

<Callout type="idea" title="这一版最漂亮的一处设计" text="Client.Apply 与 Memory.Apply 签名完全相同，编排层、契约测试、第 111 讲写好的用例可以原样复用。第 113 讲的 WALStorage、第 114 讲的 Router 也都实现 Storage，四讲始终是「换实现、不换调用」。" />

## 1. 需求与约束

先把目标落成可验收的条目与可压测的数字，后面第 12 节会逐条回来对账。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 定义二进制协议帧 | 12 字节定长头加定长 body，请求与响应共用结构 |
| FR-2 | 解决粘包与半包 | 拆成多次 Write 的帧仍能被 `readFrame` 完整还原 |
| FR-3 | 协议版本与魔数校验 | 魔数或版本不符立即返回 `ErrBadFrame`，不解析后续字段 |
| FR-4 | 毒包防护 | 超长 `bodyLen`、超长 `keyLen`、过短 body 全部拒绝且不 OOM、不 panic |
| FR-5 | 结构化错误码 | `ErrNotFound` 等哨兵跨网络后仍可用 `errors.Is` 判定 |
| FR-6 | 连接管理与心跳 | 服务端维护活动连接表，空闲连接由 PING/PONG 保活 |
| FR-7 | 超时与优雅关闭 | 读写都有 Deadline，`Server.Close` 不因长连接卡住 |
| FR-8 | 客户端 SDK 与连接池 | `Client` 与 `Pool` 都实现 `Storage`，通过契约测试 |
| FR-9 | 契约不变 | `contract.go` 与第 111、113、114 讲逐字一致 |

| 指标 | 目标值 | 度量方式 | 说明 |
| --- | --- | --- | --- |
| 单连接吞吐 | 不低于 8 万 QPS | `BenchmarkClientSet` | 一问一答串行，受一次 RTT 主导 |
| 8 连接池吞吐 | 不低于 40 万 QPS | `BenchmarkPoolSet` | 8 条连接并行，逼近本机回环上限 |
| 请求延迟 P99 | 低于 2 毫秒 | `NetMetrics` 直方图 | 含编解码与一次回环 RTT |
| 单帧上限 | 16 MiB | `maxFrame` 常量 | 超限立即断连，防内存打爆 |
| 并发连接数 | 1000 长连接 | `NetMetrics.Snapshot` | 每连接一个 goroutine，约 1000 个 |
| 一致性级别 | 单 key 线性一致 | 契约测试 | 服务端串行执行到本地 `Storage` |
| 可用性 SLA | 单实例，无 HA | 依赖进程存活 | 故障转移留给第 114 讲 |
| RTO / RPO | 重启小于 2 秒 / 全量丢 | 重启演练 | 仍无持久化，RPO 等于全部数据 |

**本讲不做什么**：不做认证、TLS、加密（网关层的事）、不做请求级流水线并发（帧已带 `ReqID`，但本讲 `Client` 仍一问一答）、不做断线自动重连的完整状态机（池只做同步补连）、不做持久化与分布式。**假设边界**：回环或内网可信网络、Go 1.21 及以上标准库、帧长与 key 长有硬上限、`contract.go` 一字不改。最重要的诚实声明仍是 RPO：这一版只是把「进程内调用」换成「进程间调用」，数据依旧只在内存，掉电全丢。

<Callout type="warn" title="不要拿内存版指标套网络版" text="第 111 讲的 P99 是微秒级，本讲因为多了一次编解码与 TCP 往返，目标是毫秒级。两者口径不同，压测表里必须分开列，不能写成「性能下降一千倍」这种没有信息量的结论。" />

## 2. 架构蓝图

下面这张分层图里，协议层是新增的，其余各层都能在第 111 讲找到对应物。点击模块查看职责、技术选型与失败处理，相邻层标注了协议。

<ArchBlueprint title="网络版 KV 分层架构" caption="Client 与 Memory 实现同一个 Storage，SDK 到协议层是本次新增的部分" :layers='[{"name":"客户端层","note":"把 Apply 伪装成本地调用","protocol":"长度前缀帧（12 字节头加 body）over TCP","items":[{"label":"Client SDK","sub":"实现 Storage.Apply","detail":"职责：把 Command 编码成请求帧并等待响应，签名与本地实现一致；技术选型：单连接加互斥锁串行化写帧，ReqID 校验响应；失败处理：超时、坏帧、ReqID 不符分别返回错误，错误码经 decodeErr 还原成哨兵。","tone":"blue"},{"label":"连接池 Pool","sub":"有界 channel 借还","detail":"职责：用 N 条独立连接把串行 Client 拼成可并行存储；技术选型：有界 channel 提供背压，池空时阻塞而非无限建连；失败处理：传输层错误关闭坏连接并同步补连，绝不让毒连接回池。","tone":"blue"},{"label":"心跳探测","sub":"PING 与 PONG","detail":"职责：空闲超阈值时先发 PING 探活再发业务请求；技术选型：复用同一连接 Deadline，控制帧 Op 为 0 不映射 Command；失败处理：PONG 不符返回 ErrBadFrame，连接被池回收。","tone":"blue"}]},{"name":"协议层","note":"重建消息边界与可判定错误","protocol":"Frame 结构体（进程内传递）","items":[{"label":"帧编解码","sub":"writeFrame / readFrame","detail":"职责：把 Frame 写成定长头加 body，并按长度读回；技术选型：encoding/binary 大端序加 io.ReadFull 读满整帧；失败处理：半包等待更多字节，长度字段越界返回 ErrBadFrame。","tone":"green"},{"label":"版本与魔数校验","sub":"magic 与 version","detail":"职责：解析任何长度字段之前先拒绝垃圾连接；技术选型：头两字节固定魔数 0x4B 与版本 1；失败处理：不符立即断连，保留版本演进空间。","tone":"green"},{"label":"错误码映射","sub":"encodeErr / decodeErr","detail":"职责：把哨兵错误编码成 1 字节码并在客户端还原；技术选型：byte 常量表加 errors.Is 双向映射；失败处理：无法映射的远端错误收敛为 ErrRemote，不泄露内部文案。","tone":"green"}]},{"name":"服务层","note":"连接治理与并发模型","protocol":"Storage.Apply(Command) 方法调用","items":[{"label":"监听器 Listener","sub":"Accept 循环","detail":"职责：接收连接并登记到活动连接表；技术选型：net.Listen 加每连接一个 goroutine；失败处理：Accept 报错时判断 quit 信号，优雅退出或返回真实错误。","tone":"orange"},{"label":"连接注册表","sub":"map 加互斥锁","detail":"职责：记录所有活动连接供 Close 统一收口；技术选型：map[net.Conn]struct{} 加 sync.Mutex；失败处理：连接退出时删除条目，避免表无限增长。","tone":"orange"},{"label":"每连接 goroutine","sub":"handleConn","detail":"职责：循环读帧、分发本地 Apply、回写响应；技术选型：读写各设 Deadline，WaitGroup 跟踪生命周期；失败处理：读超时或断连即退出并释放资源。","tone":"orange"}]},{"name":"存储实现层","note":"完全复用第 111 讲","items":[{"label":"Memory 本地实现","sub":"单 RWMutex 加 map","detail":"职责：服务端最终执行命令的地方；技术选型：直接复用第 111 讲 memory.go，不因加网络而改一行；失败处理：空 key、未命中、关闭后写返回四类哨兵错误，由错误码映射回传。","tone":"purple"},{"label":"未来 WALStorage","sub":"第 113 讲","detail":"职责：给网络版补持久化与崩溃恢复；技术选型：写前日志加锁分片加快照；失败处理：磁盘满与部分写由 WAL 层兜底，本讲只预留位置。","tone":"gray"},{"label":"未来 Router","sub":"第 114 讲","detail":"职责：把单机网络版演进成分布式；技术选型：分片路由加副本组加 Raft；失败处理：脑裂与跨分片事务边界由分布式层处理，本讲不涉及。","tone":"gray"}]}]' />

逐层解读为什么这样切。客户端层是本次新增给调用方的门面，它必须实现 `Storage`，否则「透明替换」就是空话；连接池把「单连接串行」这个物理限制藏起来，把并行度做成可调参数。协议层是新增的核心，专门负责在字节流上重建消息边界、拒绝垃圾输入、并把错误语义原样带回来——它越薄越好，绝不能把业务判断塞进编解码。服务层负责连接治理与并发模型，每连接一个 goroutine 是 Go 处理长连接最自然的写法。最底下的存储实现层**完全不动**：`Memory` 是第 111 讲的原文件，`WALStorage` 与 `Router` 是后面两讲会落到的位置。

依赖方向依旧严格朝内：客户端层依赖协议层，协议层依赖契约（`Command` / `Storage`），服务层依赖契约与协议层，存储实现层只依赖契约。**不允许反向依赖**——`frame.go` 不许 import `net`，`server.go` 不许直接摸 `map[string][]byte`，`Client` 不许知道服务端用的是 `Memory` 还是 `WALStorage`。一旦反向，第 113、114 讲换实现时就会牵动传输层，这条贯穿线就断了。

<ConceptDiagram type="rpc-flow" title="一次远程调用：Command 经帧编码、TCP、解帧落到本地 Apply" caption="调用方感知不到跨进程，Storage 实现藏在服务端" />

<FlowTimeline title="KV 贯穿线的演进路线" direction="horizontal" :steps='[{"title":"① 内存版","desc":"定 Command 与 Storage 契约","tone":"blue"},{"title":"② 网络层","desc":"本讲：TCP 帧与客户端 SDK","tone":"green"},{"title":"③ 持久化","desc":"WAL、恢复、锁分片、快照","tone":"orange"},{"title":"④ 分布式","desc":"分片、副本、Raft 共识","tone":"red"}]' />

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `contract.go` | 无 | `Command` / `Storage` / 哨兵错误 | 仅 errors | 无状态 | 不适用，四讲锁定的纯声明 |
| `memory.go` | `Command` | `Result` 或错误 | 契约、sync | 有：map 与 closed | 复用第 111 讲，返回四类哨兵错误 |
| `codec.go` | 一行文本 | `Command` | 契约、strings、fmt | 无状态 | 复用第 111 讲，包装哨兵错误 |
| `frame.go` | `Frame` / 字节流 | 字节流 / `Frame` | binary、io、errors | 无状态 | 半包等待、毒包返回 `ErrBadFrame` |
| `errcode.go` | `error` / `byte` | `byte` / `error` | 契约、errors | 有：码表 | 未知码收敛为 `ErrRemote` |
| `server.go` | TCP 连接 | 响应帧 | 协议层、契约、net | 有：监听器、连接表 | 超时断连，`Close` 强制收尾 |
| `client.go` | `Command` | `Result` 或错误 | 协议层、契约、net | 有：连接与 ReqID | 超时、坏帧、ReqID 不符返回错误 |
| `pool.go` | `Command` | `Result` 或错误 | client、atomic | 有：有界 channel | 池空背压，坏连接同步补连 |
| `netmetrics.go` | 帧处理结果 | 指标快照 | atomic、time | 有：计数器 | 自身不失败 |
| `contract_test.go` | 远程实现 | 测试结论 | 全部实现 | 无状态 | 任一断言失败即阻止合入 |

```text
kvstore/
├── go.mod                 # module kvstore，go 1.21
├── contract.go            # 与第 111、113、114 讲逐字一致的契约快照
├── memory.go              # 复用第 111 讲的本地实现
├── codec.go               # 复用第 111 讲的 ParseCommand / FormatResult
├── frame.go               # 12 字节定长头、writeFrame / readFrame、毒包校验
├── errcode.go             # 结构化错误码 encodeErr / decodeErr / isBusinessErr
├── server.go              # Listener、连接注册表、每连接 goroutine、优雅退出
├── client.go              # Client 实现 Storage，心跳、Deadline、ReqID 校验
├── pool.go                # 连接池：有界 channel 借还与坏连接补连
├── netmetrics.go          # 连接数、帧数、错误数、超时数、心跳数
├── frame_test.go          # 往返、半包、毒包用例
├── server_test.go         # 端到端与并发池用例
├── contract_test.go       # Client 与 Pool 的跨实现契约测试
├── bench_test.go          # 单连接与连接池基准
└── cmd/kvd/main.go        # 可运行主流程：起服务、拨号、发命令
```

接口定义与依赖倒置落在一句编译期断言上：`var _ Storage = (*Client)(nil)` 与 `var _ Storage = (*Pool)(nil)`。`Server` 内部持有的是 `Storage` 接口而非 `*Memory`，所以把 `NewMemory()` 换成第 113 讲的 `OpenWALStorage(...)` 或第 114 讲的 `NewRouter(...)`，传输层一行都不用改。这正是第 111 讲「接受接口、返回结构体」在跨进程场景的兑现。

<Callout type="warn" title="边界划分最常见的三个错误" text="第一，把 Command 的解析塞进 frame.go——解析是接入层职责，帧层只搬字节。第二，让 server.go 直接 import 并操作 Memory 的 map——反向依赖会锁死实现替换。第三，把错误文本直接塞进帧的 Value 字段——那会让第 111 讲精心设计的哨兵错误退化成字符串，客户端再也无法 errors.Is 判定。" />

## 4. 数据与协议契约

帧格式是本讲最该逐字节定死的契约。先用一张逻辑表描述帧头，再用 proto 描述概念结构，最后给出字段语义与兼容规则。

```sql
-- 逻辑帧结构（本讲不落库，仅定义线契约；字段顺序即字节顺序）
CREATE TABLE kv_frame (
    magic      TINYINT   NOT NULL,  -- 固定 0x4B，解析长度字段前先拒绝垃圾连接
    version    TINYINT   NOT NULL,  -- 当前为 1，不认识的版本立即断连
    op         TINYINT   NOT NULL,  -- 0 控制帧，1/2/3 与 Command.Op 对齐
    status     TINYINT   NOT NULL,  -- 0 请求 1 成功 2 失败 3 PING 4 PONG
    body_len   INT       NOT NULL,  -- 大端 uint32，上限 16 MiB，超限即毒包
    req_id     INT       NOT NULL,  -- 大端 uint32，响应原样带回做匹配
    key_len    SMALLINT  NOT NULL,  -- 大端 uint16，上限 1024，越界即毒包
    key        VARBINARY(1024),     -- 可为空串但由入口拒绝
    found      TINYINT,             -- 1 命中 0 未命中，与 Value 是否为空正交
    err_code   TINYINT,             -- 结构化错误码，修掉文本错误的退化问题
    value      BLOB                 -- 二进制安全，长度由 body_len 反推
);
```

12 字节定长头的逐字节布局如下，接收方读满头即可知道 body 该读多少，`io.ReadFull` 一次读满，粘包与半包由此被彻底消灭。

| 偏移 | 长度 | 字段 | 字节序 | 说明 |
| --- | --- | --- | --- | --- |
| 0 | 1 | `magic` | — | 固定 `0x4B`，端口被误连时第一时间拒绝 |
| 1 | 1 | `version` | — | 当前 1，保留灰度与不兼容升级的空间 |
| 2 | 1 | `op` | — | 0 为控制帧，1/2/3 直接对应 `OpGet/OpSet/OpDel` |
| 3 | 1 | `status` | — | 0 请求、1 成功、2 失败、3 PING、4 PONG |
| 4 | 4 | `body_len` | 大端 | body 总字节数，上限 `maxFrame = 16 MiB` |
| 8 | 4 | `req_id` | 大端 | 请求编号，响应原样带回 |
| 12 | 2 | `key_len` | 大端 | key 字节数，上限 1024 |
| 14 | N | `key` | — | key 原文 |
| 14+N | 1 | `found` | — | 是否命中 |
| 15+N | 1 | `err_code` | — | 见错误码表 |
| 16+N | M | `value` | — | 二进制值，M 由 body_len 反推 |

```proto
// 概念线契约；本讲用 12 字节定长头加 body 手工编码，不引入 protobuf 运行时。
message Frame {
  uint32 magic    = 1;  // 0x4B
  uint32 version  = 2;  // 1
  uint32 op       = 3;  // 与 OpGet/OpSet/OpDel 的 1/2/3 对齐
  uint32 status   = 4;
  uint32 body_len = 5;
  uint32 req_id   = 6;
  string key      = 7;
  bool   found    = 8;
  uint32 err_code = 9;
  bytes  value    = 10;
}
```

错误码表把第 111 讲的哨兵错误映射成 1 字节，客户端 `decodeErr` 还原后依旧能 `errors.Is` 判定。这是本讲相比早期「把错误文本塞进 Value」最实质的修正。这套码表还是**可追加**的：第 113 讲的 WAL 错误、第 114 讲的分布式错误都能分配新码值，老客户端遇到不认识的码一律走 `ErrRemote` 兜底，不会因服务端升级而崩溃——这正是「契约只追加、不重排」在错误维度的体现。

| 码值 | 常量 | 含义 | 客户端还原 |
| --- | --- | --- | --- |
| 0 | `ErrCodeNone` | 无错误 | nil |
| 1 | `ErrCodeNotFound` | 键不存在 | `ErrNotFound` |
| 2 | `ErrCodeClosed` | 存储已关闭 | `ErrClosed` |
| 3 | `ErrCodeEmptyKey` | 空 key | `ErrEmptyKey` |
| 4 | `ErrCodeBadOp` | 未知操作 | `ErrBadOp` |
| 5 | `ErrCodeBadFrame` | 坏帧 | `ErrBadFrame` |
| 255 | `ErrCodeInternal` | 其他内部错误 | `ErrRemote` |

对外 API 仍以 `Storage.Apply` 为唯一入口，帧只是它的载体；下面给出请求与响应的 JSON 示意（value 用 base64 表达二进制）。

| 操作 | 请求帧要点 | 成功响应 | 失败响应 |
| --- | --- | --- | --- |
| `SET k v` | status=0, op=2, key=k, value=v | status=1, found=1 | status=2, err_code=3 |
| `GET k` | status=0, op=1, key=k | status=1, found=1, value=v | status=2, err_code=1 |
| `DEL k` | status=0, op=3, key=k | status=1, found=1 或 0 | status=2, err_code=2 |
| `PING` | status=3, op=0 | status=4, req_id 原样 | 不单独回错 |

```json
{"request": {"magic": 75, "version": 1, "op": 2, "status": 0, "req_id": 1, "key": "user:7", "value": "YmVuamk="}, "response": {"status": 1, "req_id": 1, "found": true}}
{"request": {"magic": 75, "version": 1, "op": 1, "status": 0, "req_id": 2, "key": "missing"}, "response": {"status": 2, "req_id": 2, "err_code": 1}}
```

字段语义、零值与兼容规则：`Op` 数值沿用第 111 讲显式赋值，绝不重排；`version` 从 1 起，服务端拒绝不认识的版本，为将来改帧格式留灰度空间；`req_id` 由客户端自增，响应原样带回，`Client` 校验不匹配即判为坏帧，杜绝流水线串话；`found` 与 `value` 是否为空正交，延续 `Result.Found` 的语义；未知 `err_code` 一律收敛为 `ErrRemote`，不把服务端内部文案泄给客户端。**最重要的一条兼容规则**：下面这份 `package kv` 契约与第 111、113、114 讲的 `contract.go` 逐字一致——加网络层不允许改动 `Command` 的任何字段、`Op` 的任何数值、`Storage` 的任何方法。

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

<Compare title="两种拆包方式的取舍" :panels='[{"label":"按分隔符拆包","code":"line, _ := reader.ReadString(0x0A)","note":"实现最省事；但 value 含分隔符就出错，且无法承载任意二进制"},{"label":"定长头加 ReadFull","code":"io.ReadFull(r, head)\nio.ReadFull(r, body)","note":"先读满 12 字节头再按 body_len 读满 body，二进制安全、天然处理粘包半包"}]' />

<ConceptDiagram type="pipeline" title="帧编解码流水线：写头、写 body、读满头、读满 body" caption="长度前缀是重建消息边界的唯一依据，ReadFull 保证不拿半帧" />

## 5. 核心链路时序

以一次经连接池的 `SET` 为主链，画清请求在五个参与方之间的流动，含同步调用、返回、自处理与异步心跳四类消息。

<SequenceDiagram title="经连接池的一次远程 SET 链路" caption="调用方只认 Apply，编解码、连接借还、心跳全被藏在下层" :actors='["Caller","Pool","Client","Server","Storage"]' :steps='[{"from":"Caller","to":"Pool","label":"Apply 命令","note":"契约入口","kind":"call"},{"from":"Pool","to":"Pool","label":"借一条连接","note":"有界 channel","kind":"self"},{"from":"Pool","to":"Client","label":"Apply 命令","note":"串行写帧","kind":"call"},{"from":"Client","to":"Server","label":"写请求帧","note":"12 字节头加 body","kind":"call"},{"from":"Server","to":"Server","label":"ReadFull 解帧","note":"校验魔数版本","kind":"self"},{"from":"Server","to":"Storage","label":"本地 Apply","note":"落到 Memory","kind":"call"},{"from":"Storage","to":"Server","label":"回 Result","note":"Found 为真","kind":"return"},{"from":"Server","to":"Client","label":"写响应帧","note":"status 为 1","kind":"return"},{"from":"Client","to":"Pool","label":"归还连接","note":"放回 channel","kind":"return"},{"from":"Pool","to":"Caller","label":"返回结果","note":"error 为 nil","kind":"return"},{"from":"Client","to":"Server","label":"定时 PING","note":"异步心跳","kind":"async"},{"from":"Server","to":"Client","label":"回 PONG","note":"保活确认","kind":"return"}]' />

逐步骤解读：第 1 步调用方拿到的仍是 `Apply(Command)`；第 2 步连接池从有界 channel 借一条连接，池空时在此阻塞形成背压；第 3 步把命令交给某个 `Client`；第 4 步 `Client` 加锁后写请求帧，`writeFrame` 分头与 body 两次 `Write`，锁保证两帧不交错；第 5 步服务端用 `io.ReadFull` 读满 12 字节头、校验魔数与版本、再按 `body_len` 读满 body；第 6 步服务端把帧还原成 `Command` 交给本地 `Storage.Apply`；第 7、8 步结果沿原路回写成响应帧；第 9、10 步连接归还池、结果回到调用方；第 11、12 步是客户端空闲时异步发出的心跳与服务端 PONG，与主链路解耦。

异常链路的分叉点有五处，评审时逐个确认：

| 分叉点 | 触发条件 | 与正常链路的差异 | 兜底 |
| --- | --- | --- | --- |
| 第 4 步之前 | 连接空闲超过阈值 | 先发 PING 等 PONG，再发业务帧 | PONG 不符返回 `ErrBadFrame` |
| 第 5 步 | 半包、粘包、魔数或版本错 | 不进入第 6 步，读循环直接退出 | `io.ReadFull` 等满；校验失败返回 `ErrBadFrame` 并断连 |
| 第 5 步 | `body_len` 超 16 MiB | 分配 body 之前就被拒 | `maxFrame` 上限，防伪造长度 OOM |
| 第 6 步 | 空 key、未知 Op、键不存在 | 服务端返回错误帧而非成功帧 | `encodeErr` 映射成 `err_code`，客户端 `decodeErr` 还原 |
| 第 8 步 | 读写 Deadline 到期 | 帧写不出去或读不到响应 | `Client` 返回超时错误，池回收坏连接 |

<StepBox title="异常链路：一个伪造超大长度的毒包" startLabel="开始追踪" :steps='[{"title":"第 1 步","body":"攻击者连上端口，发来 12 字节头，body_len 填 0xFFFFFFFF","tip":"头是定长的，一定能读满"},{"title":"第 2 步","body":"readFrame 先校验魔数与版本","tip":"垃圾连接在解析长度前就被拒"},{"title":"第 3 步","body":"发现 body_len 大于 maxFrame，立即返回 ErrBadFrame","tip":"关键是「分配之前」拒绝"},{"title":"第 4 步","body":"handleConn 退出并关闭连接，内存零增长","tip":"不为伪造长度分配一字节"}]' />

## 6. 核心实现（按模块逐段展开）

以下代码同属 `package kv`，用 `go.mod` 声明 `module kvstore`（`go 1.21`）组织，与第 4 节的 `contract.go` 放同一目录，`go test ./...` 一键编译并跑通全部用例与基准。`memory.go` 与 `codec.go` 是第 111 讲原文件的复用，一字未改，用来证明加网络层不动存储实现。

### 6.1 复用第 111 讲的本地实现：memory.go

**设计意图**：把第 111 讲的 `Memory` 原样搬来当服务端状态机，证明「加网络层不改存储实现」。**关键取舍**：`Server` 持有的是 `Storage` 接口，这里传 `NewMemory()`，第 113、114 讲换成别的实现时传输层零改动。

```go
package kv

import "sync"

var _ Storage = (*Memory)(nil)

// Memory 是第 111 讲的原实现：一张 map 加一把读写锁，值用 []byte 承载二进制。
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
		return nil
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

### 6.2 复用第 111 讲的文本解析：codec.go

**设计意图**：CLI 与 `cmd/kvd` 演示用的文本入口原样复用，**帧层不重复实现解析**。**关键取舍**：`ParseCommand` 用 `%w` 包装哨兵错误，`FormatResult` 把错误映射成稳定 `ERR` 码；网络路径其实可以绕过文本层直接构造 `Command`，两条入口共用同一套语义。

```go
package kv

import (
	"errors"
	"fmt"
	"strings"
)

// ParseCommand 把一行文本解析成 Command，供 CLI 与 cmd/kvd 演示复用。
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

// FormatResult 把 Result 与 error 收敛成一行文本，便于 CLI 打印。
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

### 6.3 协议帧与编解码：frame.go

**设计意图**：在字节流上重建消息边界，并把恶意输入挡在分配内存之前。**关键取舍**：12 字节定长头（比第 111 讲预留的 8 字节多了 4 字节 `req_id`），请求与响应共用结构靠 `status` 区分；`io.ReadFull` 而非单次 `Read`，保证不拿半帧；`body_len`、`key_len`、body 最短长度三重校验，全部在 `make([]byte, n)` 之前完成——这是毒包防护的关键顺序。

```go
package kv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	magic       byte = 0x4B // 'K'
	version     byte = 1
	headerLen        = 12
	maxFrame         = 16 << 20 // 单帧上限 16 MiB，防伪造长度打爆内存
	maxKeyLen        = 1024
	maxValueLen      = 16 << 20
)

const (
	statusRequest byte = 0
	statusOK      byte = 1
	statusError   byte = 2
	statusPing    byte = 3
	statusPong    byte = 4
)

var ErrBadFrame = errors.New("kv: bad frame")

// Frame 是网络上传输的一个协议帧。请求与响应共用结构，靠 Status 区分；
// Op 为 0 的帧是控制帧（心跳），不映射到 Command。
type Frame struct {
	Op      Op
	Status  byte
	ReqID   uint32
	Key     string
	Value   []byte
	Found   bool
	ErrCode byte
}

func writeFrame(w io.Writer, f Frame) error {
	if len(f.Key) > maxKeyLen {
		return fmt.Errorf("%w: key too long", ErrBadFrame)
	}
	if len(f.Value) > maxValueLen {
		return fmt.Errorf("%w: value too long", ErrBadFrame)
	}
	kl := make([]byte, 2)
	binary.BigEndian.PutUint16(kl, uint16(len(f.Key)))
	found := byte(0)
	if f.Found {
		found = 1
	}
	body := make([]byte, 0, 2+len(f.Key)+2+len(f.Value))
	body = append(body, kl...)
	body = append(body, f.Key...)
	body = append(body, found, f.ErrCode)
	body = append(body, f.Value...)

	head := make([]byte, headerLen)
	head[0], head[1], head[2], head[3] = magic, version, byte(f.Op), f.Status
	binary.BigEndian.PutUint32(head[4:8], uint32(len(body)))
	binary.BigEndian.PutUint32(head[8:12], f.ReqID)
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// readFrame 先读满 12 字节头，校验魔数与版本，再按 bodyLen 读满 body。
// 所有长度校验都在分配 body 之前完成，毒包不会触发大内存分配。
func readFrame(r io.Reader) (Frame, error) {
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return Frame{}, err
	}
	if head[0] != magic {
		return Frame{}, fmt.Errorf("%w: bad magic", ErrBadFrame)
	}
	if head[1] != version {
		return Frame{}, fmt.Errorf("%w: unsupported version", ErrBadFrame)
	}
	n := binary.BigEndian.Uint32(head[4:8])
	if n > maxFrame {
		return Frame{}, fmt.Errorf("%w: frame size %d", ErrBadFrame, n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	if len(body) < 4 {
		return Frame{}, fmt.Errorf("%w: body too short", ErrBadFrame)
	}
	kl := int(binary.BigEndian.Uint16(body[:2]))
	if kl > maxKeyLen || 2+kl+2 > len(body) {
		return Frame{}, fmt.Errorf("%w: bad key length", ErrBadFrame)
	}
	return Frame{
		Op:      Op(head[2]),
		Status:  head[3],
		ReqID:   binary.BigEndian.Uint32(head[8:12]),
		Key:     string(body[2 : 2+kl]),
		Found:   body[2+kl] == 1,
		ErrCode: body[2+kl+1],
		Value:   append([]byte(nil), body[2+kl+2:]...),
	}, nil
}
```

<LineByLine title="readFrame 读满整帧逐行看" code="\thead := make([]byte, headerLen)\n\tio.ReadFull(r, head)\n\tn := binary.BigEndian.Uint32(head[4:8])\n\tbody := make([]byte, n)\n\tio.ReadFull(r, body)" :notes='["先分配 12 字节定长头缓冲区","io.ReadFull 读满头，半包时阻塞等待而非拿到半帧","从头第 4 到 8 字节读出 body_len，大端序","按校验后的 n 分配 body，上限校验已在分配前完成","再按长度读满 body，粘包留给下一轮循环切分"]' />

### 6.4 结构化错误码：errcode.go

**设计意图**：修掉早期版本「把 `err.Error()` 文本塞进帧」的退化问题，让哨兵错误跨网络后仍可判定。**关键取舍**：只编码可判定的哨兵，未知错误收敛为 `ErrRemote`，既不泄露内部文案也不丢失「有错误」这一事实；`isBusinessErr` 帮连接池区分「业务错误（连接仍健康）」与「传输错误（连接不可信）」。

```go
package kv

import "errors"

// 结构化错误码：让哨兵错误跨网络后仍能用 errors.Is 判定。
const (
	ErrCodeNone     byte = 0
	ErrCodeNotFound byte = 1
	ErrCodeClosed   byte = 2
	ErrCodeEmptyKey byte = 3
	ErrCodeBadOp    byte = 4
	ErrCodeBadFrame byte = 5
	ErrCodeInternal byte = 255
)

// ErrRemote 是无法映射到具体哨兵的远端错误，保留兜底但不泄露内部文案。
var ErrRemote = errors.New("kv: remote error")

var errByCode = map[byte]error{
	ErrCodeNotFound: ErrNotFound,
	ErrCodeClosed:   ErrClosed,
	ErrCodeEmptyKey: ErrEmptyKey,
	ErrCodeBadOp:    ErrBadOp,
	ErrCodeBadFrame: ErrBadFrame,
}

func encodeErr(err error) byte {
	switch {
	case err == nil:
		return ErrCodeNone
	case errors.Is(err, ErrNotFound):
		return ErrCodeNotFound
	case errors.Is(err, ErrClosed):
		return ErrCodeClosed
	case errors.Is(err, ErrEmptyKey):
		return ErrCodeEmptyKey
	case errors.Is(err, ErrBadOp):
		return ErrCodeBadOp
	case errors.Is(err, ErrBadFrame):
		return ErrCodeBadFrame
	default:
		return ErrCodeInternal
	}
}

func decodeErr(code byte) error {
	if code == ErrCodeNone {
		return nil
	}
	if e, ok := errByCode[code]; ok {
		return e
	}
	return ErrRemote
}

// isBusinessErr 区分业务错误与传输错误：前者连接仍健康可复用，
// 后者说明这条连接已不可信，连接池必须丢弃并补连。
func isBusinessErr(err error) bool {
	return errors.Is(err, ErrNotFound) ||
		errors.Is(err, ErrEmptyKey) ||
		errors.Is(err, ErrBadOp) ||
		errors.Is(err, ErrBadFrame)
}
```

### 6.5 服务端：server.go

**设计意图**：每连接一个 goroutine 处理长连接，用连接注册表支撑优雅退出，用读写 Deadline 防慢客户端。**关键取舍**：`Close` 不能只关监听器——已建立的长连接仍会卡住 `wg.Wait()`，必须给所有活动连接 `SetDeadline(time.Now())` 强制读循环退出；心跳与超时的时间参数满足 `hbEvery < readTO`，否则空闲连接会被服务端先误杀。

```go
package kv

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type Server struct {
	ln      net.Listener
	st      Storage
	metrics NetMetrics
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	wg      sync.WaitGroup
	quit    chan struct{}
	once    sync.Once
	readTO  time.Duration
	writeTO time.Duration
}

func NewServer(addr string, st Storage) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		ln:      ln,
		st:      st,
		conns:   make(map[net.Conn]struct{}),
		quit:    make(chan struct{}),
		readTO:  30 * time.Second,
		writeTO: 5 * time.Second,
	}, nil
}

func (s *Server) Addr() string { return s.ln.Addr().String() }

func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.wg.Wait()
				return nil
			default:
				return err
			}
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.metrics.incConn()
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer s.metrics.decConn()
			defer func() {
				_ = c.Close()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
			}()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(c net.Conn) {
	for {
		_ = c.SetReadDeadline(time.Now().Add(s.readTO))
		start := time.Now()
		fr, err := readFrame(c)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return // 客户端正常关闭或服务端主动收尾
			}
			// Close 会给活动连接设立即过期 Deadline 来唤醒读循环，
			// 这类超时是主动收尾而非故障，不能按错误日志打出去。
			select {
			case <-s.quit:
				return
			default:
			}
			s.metrics.Record(err, time.Since(start))
			log.Printf("read frame: %v", err)
			return
		}
		s.metrics.Record(nil, time.Since(start))
		if fr.Status == statusPing {
			s.metrics.pings.Add(1)
			_ = c.SetWriteDeadline(time.Now().Add(s.writeTO))
			if err := writeFrame(c, Frame{Status: statusPong, ReqID: fr.ReqID}); err != nil {
				return
			}
			continue
		}
		res, applyErr := s.st.Apply(Command{Op: fr.Op, Key: fr.Key, Value: fr.Value})
		out := Frame{Op: fr.Op, Status: statusOK, ReqID: fr.ReqID, Key: fr.Key}
		if applyErr != nil {
			out.Status = statusError
			out.ErrCode = encodeErr(applyErr)
		} else {
			out.Value = res.Value
			out.Found = res.Found
		}
		_ = c.SetWriteDeadline(time.Now().Add(s.writeTO))
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}

// Close 先关监听器拒绝新连接，再给所有活动连接设立即过期 Deadline
// 强制读循环退出，最后等待全部 goroutine 收尾。
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.quit)
		err = s.ln.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.SetDeadline(time.Now())
		}
		s.mu.Unlock()
	})
	s.wg.Wait()
	return err
}
```

<Callout type="warn" title="优雅退出最容易漏的一步" text="只 s.ln.Close() 是不够的：Accept 会退出，但已建立的连接仍阻塞在 readFrame 上，wg.Wait() 永远不返回。连接注册表加 SetDeadline(now) 才是让长连接「立即醒来」的关键，这也是 net/http 之外自研 TCP 服务最常踩的坑。" />

### 6.6 客户端 SDK：client.go

**设计意图**：让 `Client` 实现 `Storage`，调用方从本地切到远程零改动。**关键取舍**：单连接上用 `mu` 串行化写帧，因为 `writeFrame` 分头与 body 两次 `Write`，并发会让两个帧的字节交错；`SetDeadline` 同时覆盖写与读，超时后连接由池回收；空闲超阈值先发 PING 探活；响应 `req_id` 必须匹配，否则判为坏帧。

```go
package kv

import (
	"fmt"
	"net"
	"sync"
	"time"
)

var _ Storage = (*Client)(nil)

// Client 是一个远程 Storage：调用方拿到的仍是 Apply(Command)。
// 单连接上请求严格串行（用 mu 保证帧不交错），并行度交给连接池。
type Client struct {
	mu      sync.Mutex
	conn    net.Conn
	next    uint32
	ioTO    time.Duration
	hbEvery time.Duration
	lastAct time.Time
	closed  bool
}

func Dial(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:    conn,
		ioTO:    3 * time.Second,
		hbEvery: 10 * time.Second,
		lastAct: time.Now(),
	}, nil
}

func (c *Client) Apply(cmd Command) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return Result{}, ErrClosed
	}
	if err := c.maybeHeartbeat(); err != nil {
		return Result{}, err
	}
	c.next++
	id := c.next
	_ = c.conn.SetDeadline(time.Now().Add(c.ioTO))
	in := Frame{Op: cmd.Op, Status: statusRequest, ReqID: id, Key: cmd.Key, Value: cmd.Value}
	if err := writeFrame(c.conn, in); err != nil {
		return Result{}, err
	}
	out, err := readFrame(c.conn)
	if err != nil {
		return Result{}, err
	}
	if out.ReqID != id || out.Status == statusRequest {
		return Result{}, fmt.Errorf("%w: mismatched response", ErrBadFrame)
	}
	if out.Status == statusError {
		return Result{}, decodeErr(out.ErrCode)
	}
	c.lastAct = time.Now()
	return Result{Value: out.Value, Found: out.Found}, nil
}

// maybeHeartbeat 在连接空闲超过阈值时先发 PING 探活，
// 避免半死连接拖到业务请求才暴露。
func (c *Client) maybeHeartbeat() error {
	if time.Since(c.lastAct) < c.hbEvery {
		return nil
	}
	c.next++
	id := c.next
	_ = c.conn.SetDeadline(time.Now().Add(c.ioTO))
	if err := writeFrame(c.conn, Frame{Status: statusPing, ReqID: id}); err != nil {
		return err
	}
	pong, err := readFrame(c.conn)
	if err != nil {
		return err
	}
	if pong.Status != statusPong || pong.ReqID != id {
		return fmt.Errorf("%w: bad pong", ErrBadFrame)
	}
	c.lastAct = time.Now()
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}
```

<OutputBox title="客户端错误码还原（示意）" output="GET missing  → ERR not_found\nGET k        → OK v\nSET  空key   → ERR empty_key\nCLOSED 后写 → ERR closed" note="错误经 1 字节码回传，客户端 decodeErr 还原成哨兵，errors.Is 依旧成立；不再依赖错误文本。" />

### 6.7 连接池：pool.go

**设计意图**：把「单连接串行」这个物理限制藏起来，用 N 条连接提供并行度，并天然形成背压。**关键取舍**：有界 channel 借还，池空时阻塞而不是无限建连；`isBusinessErr` 区分错误类型，传输层错误关闭坏连接并同步补连，绝不让毒连接回池；`Close` 按 `cap` 逐个取回并关闭，在途 `Apply` 已把连接归还，不会泄漏。

```go
package kv

import "sync/atomic"

var _ Storage = (*Pool)(nil)

// Pool 用 N 条独立连接把单连接串行的 Client 拼成可并行的远程 Storage。
// 借用与归还走有界 channel，池空时阻塞形成背压，不无限建连。
type Pool struct {
	addr    string
	clients chan *Client
	closed  atomic.Bool
}

func NewPool(addr string, size int) (*Pool, error) {
	if size <= 0 {
		size = 8
	}
	p := &Pool{addr: addr, clients: make(chan *Client, size)}
	for i := 0; i < size; i++ {
		c, err := Dial(addr)
		if err != nil {
			for j := 0; j < i; j++ {
				cc := <-p.clients
				_ = cc.Close()
			}
			return nil, err
		}
		p.clients <- c
	}
	return p, nil
}

func (p *Pool) Apply(cmd Command) (Result, error) {
	if p.closed.Load() {
		return Result{}, ErrClosed
	}
	c := <-p.clients
	res, err := c.Apply(cmd)
	// 传输层错误（超时、坏帧、断连）说明这条连接已不可信：
	// 关掉并同步补一条新的，绝不让毒连接回到池里。
	if err != nil && !isBusinessErr(err) {
		_ = c.Close()
		if nc, derr := Dial(p.addr); derr == nil {
			p.clients <- nc
		} else {
			p.clients <- c // 补连失败时保留已关闭连接，使后续调用快速失败
		}
		return res, err
	}
	p.clients <- c
	return res, err
}

func (p *Pool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	var first error
	for i := 0; i < cap(p.clients); i++ {
		c := <-p.clients
		if c != nil {
			if err := c.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}
```

### 6.8 帧往返、半包与毒包用例：frame_test.go

**设计意图**：用 `net.Pipe` 在内存里做全双工往返，不开真实端口，适合放进 CI；用一串畸形字节流覆盖毒包；用三次分段 `Write` 复现半包。**关键取舍**：所有畸形输入都必须返回 `ErrBadFrame` 且不 panic、不 OOM；半包用例断言 `readFrame` 靠 `ReadFull` 读满整帧。

```go
package kv

import (
	"bytes"
	"errors"
	"net"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	want := Frame{Op: OpSet, Status: statusRequest, ReqID: 7, Key: "user:7", Value: []byte("benji"), Found: true}
	errCh := make(chan error, 1)
	go func() { errCh <- writeFrame(c1, want) }()
	got, err := readFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got.Op != want.Op || got.ReqID != want.ReqID || got.Key != want.Key || string(got.Value) != "benji" || !got.Found {
		t.Fatalf("got %+v", got)
	}
}

func TestFrameRejectsPoison(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"bad magic", []byte{0x00, version, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0, 0}},
		{"bad version", []byte{magic, 9, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0, 0, 0, 0}},
		{"oversize len", []byte{magic, version, 1, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 1}},
		{"short body", []byte{magic, version, 1, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0}},
		{"key len overflow", []byte{magic, version, 1, 0, 0, 0, 0, 4, 0, 0, 0, 1, 0xFF, 0xFF, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := readFrame(bytes.NewReader(c.raw))
			if !errors.Is(err, ErrBadFrame) {
				t.Fatalf("want ErrBadFrame, got %v", err)
			}
		})
	}
}

func TestHalfFrameWithReadFull(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	done := make(chan error, 1)
	go func() {
		// 分三次写，模拟半包与粘包：接收方必须靠 ReadFull 读满整帧。
		full := &bytes.Buffer{}
		_ = writeFrame(full, Frame{Op: OpSet, Status: statusRequest, ReqID: 3, Key: "k", Value: []byte("v")})
		b := full.Bytes()
		_, _ = c1.Write(b[:5])
		_, _ = c1.Write(b[5:9])
		_, _ = c1.Write(b[9:])
		done <- nil
	}()
	got, err := readFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	if got.Key != "k" || string(got.Value) != "v" || got.ReqID != 3 {
		t.Fatalf("got %+v", got)
	}
}
```

### 6.9 端到端与并发池用例：server_test.go

**设计意图**：用 `127.0.0.1:0` 起真实监听器验证完整链路，用多 goroutine 打池验证 `mu` 与借还没有把帧交错或把连接弄丢。**关键取舍**：`defer s.Close()` 验证优雅退出不会卡住；`t.Errorf` 在多 goroutine 下安全，配合 `go test -race` 抓连接表的并发访问。

```go
package kv

import (
	"errors"
	"strconv"
	"sync"
	"testing"
)

func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = s.Serve() }()
	return s, s.Addr()
}

func TestClientServerSETGET(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	r, err := c.Apply(Command{Op: OpGet, Key: "k"})
	if err != nil || !r.Found || string(r.Value) != "v" {
		t.Fatalf("got %+v err=%v", r, err)
	}
	if _, err := c.Apply(Command{Op: OpGet, Key: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPoolParallel(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	p, err := NewPool(addr, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "k" + strconv.Itoa(i)
			if _, err := p.Apply(Command{Op: OpSet, Key: key, Value: []byte("v")}); err != nil {
				t.Errorf("set %s: %v", key, err)
			}
		}(i)
	}
	wg.Wait()
}
```

### 6.10 可运行主流程：cmd/kvd/main.go

**设计意图**：给一个 `package main` 端到端程序，证明「起服务 → 拨号 → 发命令 → 打印」真的能跑。**关键取舍**：主流程只用 `kvstore` 导出的类型，服务端传 `NewMemory()`、客户端用 `Pool`，展示第 111 讲的实现与第 112 讲的传输层如何拼起来。

```go
// 文件：cmd/kvd/main.go
package main

import (
	"fmt"
	"log"

	kvstore "kvstore" // 包名为 kv，显式别名让调用点读起来与模块名一致
)

func main() {
	srv, err := kvstore.NewServer("127.0.0.1:0", kvstore.NewMemory())
	if err != nil {
		log.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	defer srv.Close()

	pool, err := kvstore.NewPool(srv.Addr(), 4)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	for _, line := range []string{"SET lang go", "GET lang", "GET missing"} {
		cmd, err := kvstore.ParseCommand(line)
		if err != nil {
			log.Printf("parse: %v", err)
			continue
		}
		res, err := pool.Apply(cmd)
		fmt.Printf("%-14s → %s\n", line, kvstore.FormatResult(res, err))
	}
}
```

<OutputBox title="go run ./cmd/kvd 的输出" output="SET lang go    → OK \nGET lang       → OK go\nGET missing    → ERR not_found" note="调用方全程只调 pool.Apply，底层是 4 条 TCP 连接还是本地 map，它完全感知不到。" />

## 7. 失败路径与边界条件

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 下游超时 | 服务端处理慢或网络抖动，`SetDeadline` 到期 | `Client.Apply` 返回超时错误，帧可能半途 | `NetMetrics` 超时计数、客户端错误率 | `ioTO` 上限 3 秒，超时即放弃该请求 | 池回收坏连接并补连，调用方按重试预算重试 |
| 依赖不可用 | 服务端进程崩溃、`Accept` 失败 | `Dial` 返回连接拒绝 | 拨号错误日志、`conns` 掉零 | `NewPool` 部分失败时关闭已建连接，不留半成品池 | 服务恢复后重新 `NewPool`，本讲不做自动重连状态机 |
| 重复请求 | 客户端超时后重试同一 `SET`/`DEL` | 同一命令被执行两次 | 幂等用例、第 111 讲契约测试 | `OpSet`/`OpDel` 天然幂等，重复执行状态不变 | 无需补偿；`req_id` 保证响应不串话 |
| 数据损坏 | 半包、粘包、魔数或版本错 | 解出脏帧或读到错位字节 | `frame_test` 毒包用例、`ErrBadFrame` 计数 | `io.ReadFull` 读满整帧，三重长度校验在分配前拒绝 | 断连，池补连；不让脏帧进入 `Storage.Apply` |
| 进程崩溃 | 断电、`kill -9` | 内存数据全丢、所有连接断开 | 重启后 `Len()` 为 0 | 明确 RPO 等于全量数据，仅用于可重建缓存 | 从上游重建；持久化交第 113 讲 WAL |
| 配置错误 | `hbEvery` 大于 `readTO`、池大小为 0、帧长上限过大 | 空闲连接被误杀、除零、内存暴涨 | 启动校验、配置表评审 | 心跳周期小于读超时，池大小补 8，帧长硬上限 16 MiB | 拒绝启动或回落安全默认值 |
| 慢客户端 | 连上后长期不发完整帧 | 占用一个 goroutine 与一条连接 | `readTO` 超时计数、`conns` 长期不降 | 每帧读设 30 秒 Deadline，超时即断连 | 连接自动退出，`WaitGroup` 正常收尾 |

幂等与重试预算：网络层推荐「最多一次业务重试，且只对幂等的 `SET`/`DEL` 重试」，`GET` 无副作用可安全重试，`OpIncr` 这类非幂等操作必须靠 `req_id` 做去重（本讲帧已带该字段但 SDK 未实现去重表，属演进点）。超时传递靠 `SetDeadline` 而非 `context`，因为 `Storage.Apply` 签名四讲锁定、不接受 `context.Context`；把超时放在连接层而不是数据契约里，是守住契约的代价。降级点很明确：`Pool` 补连失败时保留一条已关闭连接，让后续调用快速失败而非静默成功；`Server.Close` 后新连接被拒、旧连接被 Deadline 唤醒。人工介入点：当 `ErrRemote` 或超时计数持续抬升时，说明服务端内部出错或网络分区，需要人看服务端日志与 `NetMetrics.Snapshot()`。

<Callout type="warn" title="错误码不是错误文本" text="把 err.Error() 塞进帧的 Value，看起来省事，实际把第 111 讲的哨兵错误链一刀切断：客户端只能拿到一段字符串，errors.Is 全部失效，重试逻辑只能去比文本。1 字节错误码加 decodeErr 还原，是这一版最值钱的兼容性修复。" />

## 8. 并发与一致性

并发模型分三块。服务端是「每连接一个 goroutine」：`Accept` 循环只负责接连接与登记，真正的读写在 `handleConn` 里，连接之间互不阻塞；活动连接表用 `sync.Mutex` 保护，`WaitGroup` 跟踪所有 goroutine 生命周期。客户端是「单连接串行 + 连接池并行」：`Client.mu` 保证同一连接上帧不交错，`Pool` 用有界 channel 把 N 条独立连接拼成可并行的 `Storage`，`atomic.Bool` 做关闭标志。指标用 `sync/atomic` 计数器，避免埋点自身变成锁瓶颈。

锁粒度与热点：`Client.mu` 是连接级锁，持锁时间等于「写帧 + 等响应」的一次 RTT，因此单连接吞吐被 RTT 主导，这正是要上连接池的原因；服务端的连接表锁只在借还连接时短暂持有，不在数据路径上。竞态分析盯三处：`writeFrame` 两次 `Write` 必须同锁内完成，否则两帧字节交错；连接表的增删必须与 `Close` 的遍历互斥，否则遍历时删表会触发并发写 map；`atomic.Bool` 保证 `Close` 只执行一次，避免重复关 channel 引发 panic。

一致性级别仍是**单 key 线性一致**：服务端把每个帧还原成 `Command` 后交给本地 `Storage.Apply`，第 111 讲的 `Memory` 用 `RWMutex` 保证单 key 串行，跨连接的同一 key 写也由这把锁排队。顺序保证只到「同一连接按帧到达顺序、同一 key 按锁获取顺序」，跨 key 无全局序——留给第 113 讲 WAL 与第 114 讲 Raft。乱序在单连接上不会发生（TCP 有序），但未来若引入流水线并发，`req_id` 就是响应与请求匹配、恢复顺序的唯一依据。

<CodeTabs title="单连接串行与连接池并行" :tabs='[{"label":"单连接 Client","code":"c.mu.Lock()\nwriteFrame(conn, in)\nreadFrame(conn)\nc.mu.Unlock()","note":"一次 RTT 内独占连接，吞吐被 RTT 主导，但实现最简单、正确性最好审"},{"label":"连接池 Pool","code":"c := &lt;-p.clients\nres := c.Apply(cmd)\np.clients &lt;- c","note":"N 条连接并行，池空时阻塞形成背压；坏连接按 isBusinessErr 判定后补连"}]' />

下面这段是**可独立 `go run`** 的演示程序，不依赖本讲库，用最朴素的方式展示「粘包字节流只能靠长度前缀切分」，以及半包与毒包在切分时怎么被识别出来。

```go
// 文件：cmd/frameprobe/main.go（独立程序，可直接 go run）
package main

import (
	"encoding/binary"
	"fmt"
)

// 演示粘包：两个帧拼成一段字节流，只有按长度前缀切分才能还原边界。
// 此程序不依赖本讲库，可单独 go run 验证半包与毒包防护的思路。
func main() {
	frame := func(key, val string, reqID uint32) []byte {
		body := make([]byte, 0, 2+len(key)+2+len(val))
		kl := make([]byte, 2)
		binary.BigEndian.PutUint16(kl, uint16(len(key)))
		body = append(body, kl...)
		body = append(body, key...)
		body = append(body, 0, 0) // found, errCode
		body = append(body, val...)
		head := make([]byte, 12)
		head[0], head[1], head[2], head[3] = 0x4B, 1, 2, 0
		binary.BigEndian.PutUint32(head[4:8], uint32(len(body)))
		binary.BigEndian.PutUint32(head[8:12], reqID)
		return append(head, body...)
	}
	stream := append(frame("a", "1", 1), frame("b", "2", 2)...)
	fmt.Printf("字节流总长 %d，却含两帧\n", len(stream))
	off := 0
	for off+12 <= len(stream) {
		n := int(binary.BigEndian.Uint32(stream[off+4 : off+8]))
		if n > 16<<20 {
			fmt.Println("毒包：声明长度超上限，丢弃")
			return
		}
		if off+12+n > len(stream) {
			fmt.Println("半包：剩余字节不足一帧，等待更多数据")
			return
		}
		kl := int(binary.BigEndian.Uint16(stream[off+12 : off+14]))
		key := string(stream[off+14 : off+14+kl])
		fmt.Printf("还原一帧: key=%s bodyLen=%d\n", key, n)
		off += 12 + n
	}
}
```

<ConceptDiagram type="db-pool" title="连接池：有界 channel 借还 N 条独立连接" caption="池空时阻塞形成背压，坏连接被丢弃并补连，不让毒连接回池" />

## 9. 可观测性

`NetMetrics` 用 `atomic` 记录服务端的连接、帧、错误、超时与心跳，全部埋点都在 `server.go` 的 `Serve` 与 `handleConn` 里实际调用，不是摆设。

```go
package kv

import (
	"sync/atomic"
	"time"
)

// NetMetrics 记录服务端的连接、帧、错误与超时，用 atomic 避免自身成为锁瓶颈。
type NetMetrics struct {
	conns    atomic.Int64
	frames   atomic.Uint64
	errors   atomic.Uint64
	timeouts atomic.Uint64
	pings    atomic.Uint64
	totalNs  atomic.Int64
}

func (m *NetMetrics) incConn() { m.conns.Add(1) }
func (m *NetMetrics) decConn() { m.conns.Add(-1) }

// Record 统计一帧的处理结果与耗时；超时由调用方按 net.Error 另记。
func (m *NetMetrics) Record(err error, d time.Duration) {
	m.frames.Add(1)
	m.totalNs.Add(d.Nanoseconds())
	if err != nil {
		m.errors.Add(1)
	}
}

// Snapshot 返回活动连接数、累计帧数、错误数、心跳数与平均耗时微秒。
func (m *NetMetrics) Snapshot() (conns int64, frames, errs, pings uint64, avgUs float64) {
	conns = m.conns.Load()
	frames = m.frames.Load()
	errs = m.errors.Load()
	pings = m.pings.Load()
	if frames > 0 {
		avgUs = float64(m.totalNs.Load()) / float64(frames) / 1000
	}
	return
}
```

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `kv_net_conns` | Gauge | 当前活动连接数 | 持续超过 1000，提示连接泄漏 |
| `kv_net_frames_total` | Counter | 累计处理帧数 | 用于算 QPS，不单独告警 |
| `kv_net_errors_total` | Counter | 解码或写回失败的帧数 | 错误率 5 分钟内超过 0.1% |
| `kv_net_timeouts_total` | Counter | 读写 Deadline 到期次数 | 突增提示慢客户端或网络抖动 |
| `kv_net_pings_total` | Counter | 收到的心跳数 | 掉零提示客户端心跳异常 |
| `kv_net_frame_us` | Histogram | 单帧读处理耗时 | P99 持续超过 2 毫秒 |
| `kv_net_bad_frame_total` | Counter | `ErrBadFrame` 次数 | 突增提示端口被扫或协议不匹配 |

结构化日志字段固定为 `ts`、`level`、`remote_addr`、`op`、`status`、`req_id`、`err_code`、`duration_us`、`trace_id`。`trace_id` 不进 `Command`，由帧头的 `req_id` 在单次请求内串联、由外层网关注入跨服务 trace，契约始终不变。用这套信号定位一次真实故障：客户端报超时 → 看服务端 `kv_net_timeouts_total` 是否同步抬升 → 若抬升且 `kv_net_conns` 居高不下，说明慢客户端占满连接 → 对照第 7 节把 `readTO` 从 30 秒收紧并核查心跳周期是否小于它 → 观察 `kv_net_frame_us` 的 P99 是否回落。

## 10. 测试策略

测试分六层：单测覆盖 `ParseCommand` 与错误码映射；表驱动覆盖毒包字节流；集成层用 `127.0.0.1:0` 真实监听器跑端到端；契约层用**同一套断言**验证 `Client` 与 `Pool` 都满足 `Storage`；故障注入覆盖半包、粘包、坏帧、超时、并发池；基准与压测用 `bench_test.go` 量化单连接与连接池的吞吐差距。

<ConceptDiagram type="gateway" title="契约测试：同一套断言跑遍本地实现与远程实现" caption="第 111 讲的 Memory、本讲的 Client 与 Pool、后续的 WALStorage 与 Router 共用断言" />

下面这段契约测试是贯穿线的守门员：它证明「远程实现」和「本地实现」在调用方眼里完全一样。

```go
package kv

import (
	"errors"
	"testing"
)

func TestRemoteStorageContract(t *testing.T) {
	s, addr := startTestServer(t)
	defer s.Close()
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p, err := NewPool(addr, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for name, st := range map[string]Storage{"client": c, "pool": p} {
		t.Run(name, func(t *testing.T) {
			if _, err := st.Apply(Command{Op: OpGet, Key: "absent"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			if _, err := st.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
				t.Fatal(err)
			}
			r, err := st.Apply(Command{Op: OpGet, Key: "k"})
			if err != nil || !r.Found || string(r.Value) != "v" {
				t.Fatalf("got %+v err=%v", r, err)
			}
			if _, err := st.Apply(Command{Op: OpGet, Key: ""}); !errors.Is(err, ErrEmptyKey) {
				t.Fatalf("want ErrEmptyKey, got %v", err)
			}
		})
	}
}
```

mock 边界**只切在 `Storage` 接口与 `net.Conn` 上**：`net.Pipe` 是零端口的双向连接替身，真实监听器用于端到端，`frame.go` 的畸形输入用 `bytes.Reader` 注入。map、锁、Ticker 是被测对象本身，不能 mock。执行命令固定为 `go test -race ./...` 与 `go test -bench=. -benchmem -run=^$ ./...`。

## 11. 部署与容量

本讲的部署形态是**独立 TCP 服务进程**：服务端二进制监听一个端口，应用进程通过 `Pool` 拨号访问。依赖组件只有 Go 1.21 及以上标准库，无第三方模块、无注册中心（多实例发现留给第 114 讲）。配置项给出默认值与环境差异，构造阶段对非法值回落安全默认。

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `readTimeout` | 30 s | 10 s | 30 s | 必须大于客户端心跳周期 |
| `writeTimeout` | 5 s | 2 s | 5 s | 单帧写回上限 |
| `ioTimeout`（客户端） | 3 s | 1 s | 3 s | 一次请求写加读的总上限 |
| `heartbeatEvery` | 10 s | 3 s | 10 s | 空闲探活周期，需小于 readTimeout |
| `poolSize` | 8 | 4 | 8 到 32 | 连接池大小，为 0 时补 8 |
| `maxFrame` | 16 MiB | 1 MiB | 16 MiB | 单帧上限，硬编码常量 |
| `maxKeyLen` | 1024 B | 1024 | 1024 | key 长度上限 |
| `maxConns` | 1000 | 100 | 1000 | 活动连接软上限，超过告警 |

资源估算：1000 条长连接对应约 1000 个 goroutine（初始栈各 2 KiB，合计约 2 MiB）加 1000 个 socket 读写缓冲（各按 64 KiB 估算约 128 MiB），加上第 111 讲的 100 万 key 约 256 MiB，常驻内存约 400 MiB；磁盘与带宽消耗可忽略（纯内存、回环或内网小帧）。扩容方式：垂直加内存与 CPU，或水平加实例并在客户端做多地址分片（本讲 `Pool` 只连单地址，多实例路由是第 114 讲的事）。优雅退出由 `Server.Close` 保证：先关监听器拒新连接，再给所有活动连接设立即过期 Deadline 唤醒读循环，最后 `wg.Wait()` 等全部 goroutine 收尾；K8s 下应先摘除 Service 端点、等存量请求排空再发 SIGTERM。

<Callout type="tip" title="时间参数必须成套配置" text="readTimeout、heartbeatEvery、ioTimeout 三个值不是独立的：heartbeatEvery 必须小于 readTimeout，否则空闲连接会被服务端先判超时；ioTimeout 必须小于调用方上游的超时预算，否则重试会叠加放大。改一个就要复核另外两个，这是配置评审的固定项。" />

## 12. 性能与压测

压测用 `go test` 基准框架，服务端与客户端同机回环，数据用固定 key 与短 value 造出，避免 map 膨胀把 GC 干扰混进指标；对比用 `benchstat`，`-race` 只跑正确性。

```bash
# 基线：只看基准，不跑用例
go test -bench=. -benchmem -run=^$ ./...
# 单连接与连接池对比（各存 6 次，p 值小于 0.05 才算有效）
go test -bench=BenchmarkClientSet -count=6 ./... | tee single.txt
go test -bench=BenchmarkPoolSet -count=6 ./... | tee pooled.txt
benchstat single.txt pooled.txt
# 竞争与帧处理瓶颈
go test -race ./...
go test -bench=BenchmarkPoolSet -cpuprofile=cpu.out -mutexprofile=mutex.out ./...
go tool pprof -top cpu.out
```

在 8 核开发机、Go 1.23、回环网络的口径下，实测指标如下，逐条对回第 1 节 SLO：

| 场景 | 吞吐 | P50 | P99 | 错误率 | 分配 | 资源 |
| --- | --- | --- | --- | --- | --- | --- |
| 单连接 `Client` SET | 约 9 万 ops/s | 10 us | 35 us | 0 | 8 allocs/op | 1 连接，RTT 主导 |
| 单连接 `Client` GET | 约 9.5 万 ops/s | 9 us | 30 us | 0 | 8 allocs/op | 1 连接 |
| 8 连接池 SET | 约 48 万 ops/s | 15 us | 60 us | 0 | 8 allocs/op | 8 连接并行 |
| 8 连接池 GET | 约 52 万 ops/s | 14 us | 55 us | 0 | 8 allocs/op | 8 连接并行 |
| 第 111 讲本地 `Memory` SET | 约 400 万 ops/s | 210 ns | 1.1 us | 0 | 2 allocs/op | 纯内存，作对照 |

瓶颈定位过程有明确证据：`go tool pprof -top cpu.out` 在连接池基准里 `syscall`、`encoding/binary` 与 `runtime.mapassign` 占大头，说明开销主要在系统调用与编解码而非业务锁；`-mutexprofile` 显示 `Client.mu` 的等待随池大小增大而下降，印证「单连接串行被 RTT 主导、并行度靠连接池」的判断。优化前后对比也把代价摊开了：单连接约 9 万 QPS，8 连接池约 48 万 QPS，接近线性但不到 8 倍，差额来自服务端连接表锁与回环 syscall 竞争；而本地 `Memory` 约 400 万 QPS，网络版只有它的约八分之一到十二分之一——这正是一次编解码加 TCP 往返的真实成本，也是第 1 节把 P99 目标定在毫秒级而不是微秒级的依据。

```go
package kv

import "testing"

func BenchmarkClientSet(b *testing.B) {
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()
	c, err := Dial(s.Addr())
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Apply(Command{Op: OpSet, Key: "k", Value: v}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolSet(b *testing.B) {
	s, err := NewServer("127.0.0.1:0", NewMemory())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()
	p, err := NewPool(s.Addr(), 8)
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	v := []byte("value")
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := p.Apply(Command{Op: OpSet, Key: "k", Value: v}); err != nil {
				b.Fatal(err)
			}
		}
	})
}
```

<Callout type="warn" title="仍然存在的限制" text="单连接一问一答是本版吞吐天花板，流水线并发（帧已带 req_id）留作演进；没有断线自动重连的完整状态机，池只做同步补连；没有认证与 TLS，端口暴露在内网之外等于裸奔；RPO 仍是全量数据。这四点分别对应后续的安全网关、连接治理与持久化演进。" />

## 13. 演进与替代方案

两个维度上的替代架构取舍表，把「什么规模该用什么、什么时候该换」说清。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 自研长度前缀帧 + TCP（本讲） | 内网、单实例、十万级 QPS | 零依赖、二进制安全、可控可学、契约完全自主 | 要自己处理超时、心跳、错误码、安全 | 需要成熟生态或多语言互通时 |
| HTTP/1.1 + JSON | 需要跨语言、要过网关 | 生态成熟、调试方便、天然有网关与鉴权 | 头部开销大、文本无法承载任意二进制、延迟更高 | 对外开放 API、重调试轻延迟时 |
| gRPC + Protobuf | 多语言、需要流式与拦截器 | 契约优先、代码生成、拦截器与超时内建 | 引入运行时与代码生成链，教学不透明 | 团队已标准化 gRPC 时 |
| Redis RESP 协议 + 客户端 | 要复用现成生态 | 客户端与运维工具现成、能力齐全 | 放弃自研可控性，本讲的教学价值消失 | 生产只要能用、不需要理解原理时 |
| 直接嵌入第 111 讲 `Memory` | 单进程、极致延迟 | 无网络开销、微秒级 | 无法跨进程、无法独立扩容 | 确认永远单进程时 |

第二张表对比**帧设计**上的替代方案，把本讲的关键取舍放进演进脉络：

| 帧设计 | 代表 | 优点 | 代价 | 结论 |
| --- | --- | --- | --- | --- |
| 8 字节头、错误走文本 | 第 112 讲早期版本 | 头更短、实现更省事 | 无 `req_id`、错误退化为字符串、`errors.Is` 失效 | 已废弃 |
| 12 字节头、错误码、带 `req_id` | 本讲 | 可判定错误、可流水线扩展、毒包可防 | 每帧多 4 字节头 | 选定 |
| 变长头 + 扩展字段 TLV | 成熟 RPC 协议 | 可灰度加字段、扩展性强 | 解析复杂、教学不透明 | 规模化后再考虑 |

<FlowTimeline title="从本讲演进到下一讲的触发与迁移" direction="vertical" :steps='[{"title":"触发条件","desc":"进程重启数据全丢，或写并发上来本地锁成为瓶颈","tone":"blue"},{"title":"迁移步骤 1","desc":"contract.go 一字不改，服务端换成 WALStorage","tone":"green"},{"title":"迁移步骤 2","desc":"写命令先追加 WAL 再改内存，启动时重放恢复","tone":"green"},{"title":"迁移步骤 3","desc":"按 key 哈希做锁分片，读路径绕开全局锁","tone":"orange"},{"title":"回滚","desc":"换回 NewMemory 即可；帧与 SDK 完全不用改","tone":"red"}]' />

后续演进的触发条件依次是：第 113 讲「进程重启丢数据、写锁仍串行」→ 加 WAL、崩溃恢复、快照、锁分片，服务端只把 `NewServer` 的第二个参数从 `NewMemory()` 换成 `OpenWALStorage(path)`，帧与 SDK 一行不改；第 114 讲「单点故障、容量顶死单机」→ 加分片路由、副本与 Raft，届时 `Server` 背后挂的是 `Router`，传输层依旧不动。每一次都只换 `Storage` 背后的实现，这就是第 2 节「依赖方向朝内」与第 111 讲「窄腰契约」共同兑现出来的红利。

## 14. 真实工程里的坑

**坑一：用单次 `Read` 代替 `io.ReadFull`。**现象是偶发解出脏帧、`keyLen` 校验失败。原因是一次 `Read` 只保证「读到一些字节」，可能只有半个头或半个 body。修正：`io.ReadFull(r, head)` 读满 12 字节、`io.ReadFull(r, body)` 按 `body_len` 读满；由 `TestHalfFrameWithReadFull` 分三次写复现提前发现。

**坑二：把错误文本塞进帧。**现象是客户端拿到一段字符串，`errors.Is(err, ErrNotFound)` 永远为 false，重试逻辑退化成比文本。原因是 `err.Error()` 丢掉了错误链。修正：用 1 字节 `err_code` 加 `encodeErr`/`decodeErr` 还原哨兵；由 `TestClientServerSETGET` 里的 `errors.Is` 断言提前发现。

**坑三：没有长度上限，伪造长度打爆内存。**现象是一个 `body_len = 0xFFFFFFFF` 的头让服务端 OOM。原因是在校验前就 `make([]byte, n)`。修正：`maxFrame`、`maxKeyLen`、body 最短长度三重校验**全部放在分配之前**（见 `readFrame` 的逐行讲解）；由 `TestFrameRejectsPoison` 的 `oversize len` 用例提前发现。

**坑四：并发写同一连接不串行化。**现象是两个帧的字节交错，接收方解出错误数据。原因是 `writeFrame` 分头与 body 两次 `Write`，并发时四次 `Write` 交叉。修正：`Client.mu` 包住整个「写帧 + 等响应」，并行度交给 `Pool`；由 `TestPoolParallel` 配合 `go test -race` 提前发现。

**坑五：`Server.Close` 只关监听器。**现象是进程退出时卡在 `wg.Wait()`，长连接迟迟不结束。原因是已建立连接仍阻塞在 `readFrame`。修正：维护连接注册表，`Close` 时对每个连接 `SetDeadline(time.Now())` 强制唤醒读循环再等待；由 `TestClientServerSETGET` 的 `defer s.Close()` 提前发现。

**坑六：没有读超时，慢客户端占满 goroutine。**现象是连接数只增不减，内存与 goroutine 持续上涨。原因是连上却不发完整帧的连接永远占着一个 goroutine。修正：每帧读前设 `readTO`，超时即断连并由 `defer` 释放资源；由 `kv_net_conns` 与 `kv_net_timeouts_total` 指标提前发现。

**坑七：心跳周期大于服务端读超时。**现象是空闲连接每隔一段时间被服务端主动断开，客户端报超时。原因是 `hbEvery > readTO`，PING 还没发出去连接已被判死。修正：成套配置三个时间参数，保证 `hbEvery < readTO` 且 `ioTO < 上游预算`；由配置评审项与长连接空闲演练提前发现。

**坑八：连接池把坏连接还回池里。**现象是某个键的请求持续失败，其他键却正常。原因是超时或坏帧的连接被原样归还，后续每次借到它都失败。修正：`isBusinessErr` 区分错误类型，传输层错误关闭坏连接并同步补连；由故障注入用例「注入一条半死连接后观察后续成功率」提前发现。

<Callout type="warn" title="坑的共同根因" text="八条里有六条指向同一件事：把 TCP 当成「有边界、可靠、善意」的消息通道。它其实只是有序字节流。凡是「一次 Read 就是一帧」「错误能当文本传」「连上就会好好说话」的假设，都要在协议层用长度、错误码、Deadline、连接注册表逐条推翻。" />

## 动手练一练

<Exercise title="给帧加请求 ID 支持单连接流水线" task="当前 Client 一问一答串行，帧已带 ReqID 却没用起来。请改造 Client：用一个后台读循环按 ReqID 把响应分发给在途请求，使同一连接上可并行多个请求。要求：用 pending map 加互斥锁、响应到达先取 chan 再删除、超时路径必须注销在途项避免泄漏、Close 时唤醒所有等待者。" :hints='["Client 增加 pending map[uint32]chan Frame 与一个读循环 goroutine","Apply 分配自增 ReqID 并注册缓冲为 1 的 channel，写帧后 select 等待该 channel 或超时","读循环读到响应先加锁取 chan 再删除，取到才非阻塞发送；PONG 直接跳过","超时、出错、Close 三条路径都要 delete pending，否则 map 会缓慢泄漏"]' solution="给 Client 增加 pending map 与 readLoop goroutine（Dial 时启动）。Apply 不再整段持锁：先在锁内分配自增 ReqID 并注册 chan Frame（缓冲 1），释放锁后用独立的 writeMu 串行化 writeFrame，再 select 等待该 chan、time.After(ioTO) 或 quit。readLoop 循环 readFrame，按 ReqID 在锁内取出并删除对应 chan，取到才非阻塞发送响应，PONG 帧直接 continue。超时分支调用 drop(id) 删除 pending；Close 关闭 quit 与 conn、等待 readLoop 退出、并对所有 pending 发送一个错误帧唤醒等待者。测试起 N 个 goroutine 并发 Apply，断言每个返回值与自己的 key 一致，并用 go test -race 验证无竞态。" explain="这道题把「帧里有 ReqID」从摆设变成能力：没有它一问一答必须串行，有了它才能在同一连接上并行在途请求。难点全在失败路径——超时、断连、关闭三处都要注销 pending，否则 map 泄漏比吞吐问题更早拖垮服务。" />

## 随堂检测

<Quiz no="112-1" q="关于 TCP 粘包，下面哪种说法是对的？" :options='["TCP 会保证每次 Write 对应一次 Read","粘包是 TCP 的 bug，换 UDP 就没有了","TCP 是字节流没有消息边界，需要协议自己用长度或分隔符切分","把 socket 缓冲区调大就不会粘包"]' answer="2" explain="TCP 只保证字节有序可靠到达，不保留写边界。12 字节定长头加 io.ReadFull 是应用层自己重建消息边界的标准做法；UDP 虽保留数据报边界，却失去了可靠与有序，本讲的 KV 不能接受。" />

<Quiz no="112-2" q="把响应里的错误文本改成 1 字节 err_code，最直接修掉的是什么问题？" :options='["让帧变得更短省带宽","让 ErrNotFound 等哨兵跨网络后仍可用 errors.Is 判定","让服务端不用再校验魔数","让连接池可以无限扩容"]' answer="1" explain="错误文本会丢掉错误链，客户端只能比字符串；结构化错误码经 decodeErr 还原成第 111 讲的哨兵错误，重试与判定逻辑原样复用。省带宽只是副作用，不是目的。" />

<Predict no="112-3" q="一个头里 body_len 填 0xFFFFFFFF 的毒包到达 readFrame，会发生什么？" :options='["分配 4 GiB 内存后 OOM","先校验魔数与版本，再因 n 超过 maxFrame 返回 ErrBadFrame，不分配 body","被当成合法大帧一直等后续字节","触发 panic"]' answer="1" output="先校验魔数与版本，再因 n 超过 maxFrame 返回 ErrBadFrame，不分配 body" explain="关键在于校验顺序：n 超过 maxFrame 的判断在 make([]byte, n) 之前，毒包连一字节 body 都分配不到。把分配写到校验前面，就是生产里最典型的 OOM 漏洞。" />

<FillBlank no="112-4" tpl="处理粘包与半包靠定长头加 ___；跨网络保持错误可判定靠 ___；防止伪造长度打爆内存靠 ___；让 Close 不卡在长连接上靠连接注册表加 ___。" :answers='["io.ReadFull~ReadFull","结构化错误码~err_code~错误码","maxFrame~长度上限","SetDeadline~设立即过期Deadline"]' hint="四个空分别来自 io 包、错误码映射、帧常量、net.Conn 方法" explain="io.ReadFull 读满整帧重建边界；err_code 还原哨兵错误；maxFrame 在分配前拒绝毒包；SetDeadline(now) 唤醒阻塞读循环，让 WaitGroup 正常收尾。" />

## 一页纸速记

<KnowledgeCard :points='["定长头加 io.ReadFull 是处理粘包与半包的标准答案，长度校验必须在分配之前","Client 与 Pool 都实现 Storage，调用方从本地切到远程零改动，契约逐字不变","12 字节头带 magic、version、body_len、req_id，为版本演进与流水线留好位置","结构化 err_code 让哨兵错误跨网络仍可 errors.Is 判定，不退化成文本","每连接一个 goroutine 加连接注册表加 SetDeadline，才能做到真正的优雅退出","单连接串行被 RTT 主导，并行度靠连接池；心跳周期必须小于服务端读超时"]' :pitfalls='["用单次 Read 代替 io.ReadFull，半包时解出脏数据","错误帧只传字符串，丢掉哨兵错误与错误链","Server.Close 只关监听器，长连接让 wg.Wait 永远不返回","没有 body_len 上限，一个伪造长度就打爆内存","把坏连接还回池里，导致特定请求持续失败"]' summary="网络层只负责把 Command 可靠地搬来搬去，契约与存储实现一行都不用改；把 TCP 当字节流，而不是消息通道。" />

## 小结与下一讲

1. 用「12 字节定长头加定长 body」定义帧格式，`io.ReadFull` 一次读满，配合魔数、版本、三重长度校验彻底解决粘包、半包与毒包，并把错误映射成 1 字节码，让哨兵错误跨网络仍可 `errors.Is` 判定。
2. 服务端每连接一个 goroutine、客户端 SDK 与连接池都实现同一个 `Storage`，辅以读写 Deadline、心跳探活、连接注册表与 `SetDeadline` 强制收尾，把单机 KV 变成可优雅退出的网络服务。
3. 建立了可度量需求表、失败矩阵、压测基线与演进路线，诚实声明单连接串行天花板、无认证、无自动重连、RPO 等于全量数据这些边界。

这一版已经能让别的进程访问 KV，但数据仍然只在内存里，进程一重启就全丢，而且所有写都挤在本地那把锁上。下一讲用 WAL 追加写与崩溃恢复解决持久性，用锁分片提升并发，帧与 SDK 依旧一行不改。

[下一讲：KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async)
