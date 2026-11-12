---
title: 第 113 讲 · KV 贯穿线 ③ WAL、持久化与并发控制
description: 用 WAL 追加写、CRC32C 校验与 fsync 三档策略做崩溃恢复，用锁分片提升并发，用快照与 compaction 遏制写放大
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="让数据落盘、崩溃可恢复且并发安全" prereq="第 112 讲 · TCP 协议帧与网络层" />

# 第 113 讲 · KV 贯穿线 ③ WAL、持久化与并发控制

> 第 112 讲把 KV 变成了网络服务：`Client` 也实现了 `Storage`，调用方拿到的还是 `Apply(Command)`，本地内存还是远程 TCP 完全透明。但只要进程一退出，或者机器一掉电，`Memory` 那张 map 里的数据就全部蒸发；更糟的是所有写命令都挤在一把全局 `RWMutex` 上，写密集时 16 个核只能排队用 1 个。第 112 讲自己在结尾列了四个待修点，其中「重启丢数据」和「单锁串行」是这一讲必须还的债，「错误帧退化成字符串」顺手一起修掉。做法是在 `Storage` 背后换一层实现：写命令先追加到**写前日志 WAL**并校验，再改分片内存表；启动时按「快照 → 重放 → 裁脏尾 → 开新段」四步把状态追回来；定期打快照截断旧日志。先把丑话说在前面：持久性不是免费的，这一讲全部设计都围绕一条可度量的取舍——**已确认的写一条都不丢（RPO），代价是每次写都要等一次磁盘；把等待合并成组提交，就得接受崩溃时丢一个窗口**。下面先给约束，再推架构，最后逐段给可编译的实现。`Apply(Command)` 从头到尾一个字不改。

## 0. 项目定位与真实场景

<CheckList id="L113-goal" title="本讲目标" :items='["用长度加 CRC32C 的记录格式实现 WAL 追加写，并给出 fsync 三档策略与各自的 RPO","实现崩溃恢复四步链路：加载快照、按位点重放、裁剪脏尾巴、新建段续写","用 32 把分片锁把写竞争摊开，并量化单条 WAL 流带来的写放大","兑现第 112 讲的遗留修复：把哨兵错误编码进帧 status 位，让 errors.Is 跨网络仍成立","定位磁盘满、部分写、位翻转、快照半写、锁顺序五类真实边界"]' />

这条贯穿线走到第三讲，系统已经不是教学玩具，而是要担责的落地存储。它承接的典型业务是**内部服务的本地持久化层**：会话状态、灰度开关、幂等去重表、限流计数这些「量不大但一条都不能丢」的数据。谁在用？是同机部署的业务进程，通过第 112 讲的 TCP 帧或进程内调用访问；运维同学负责容量与恢复演练。

先看量级，否则谈架构就是空话。单实例规划承载 2000 万 key，value 平均 256 字节，总逻辑数据量约 5 GiB；日写入 5 亿次，峰值写 6 万 ops/s、读 30 万 ops/s，读写比大约 1 比 5；单机配 16 核、32 GiB 内存、一块 960 GiB NVMe。现有方案有三条路都走不通：纯内存重启即失；每次写都做全量快照是 O(n) 的磁盘重写，6 万 QPS 下根本不可能；每条写直接 `fwrite` 到数据文件又会遇到随机 IO 与「写了一半崩溃」导致的整块数据损坏。

于是核心矛盾被逼成两句话：**第一，持久性与吞吐的取舍**——磁盘必须等，但不想每条都等；**第二，并发与顺序的取舍**——多核想并行写，可日志是单一追加流，顺序一旦乱了，重放就会得到另一个状态。整讲的每一个组件，都是为这两个矛盾里的某一侧定价。顺便说明，本讲不涉及副本、选主与跨机一致性，那是第 114 讲在单机持久化之上遇到的问题。

<Term word="写前日志" text="Write-Ahead Log：先把操作追加到只增不改的日志，再修改内存状态；崩溃后靠顺序重放把状态追回来，是数据库持久性的地基" />

## 1. 需求与约束

先把「做到什么算合格」写成可验收的条目，再谈怎么实现。功能需求按编号锁死，评审时逐条对照；非功能指标给出口径与测量方式，避免「很快」「很稳」这类无法验收的词。

**功能需求表**

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 已确认的写不丢 | `Apply` 返回成功后 kill -9 进程，重启重放该 key 仍在；三档策略各自声明 RPO |
| FR-2 | 崩溃恢复 | 断电或进程崩溃后 `OpenWALStorage` 自动恢复到崩溃前的已提交状态，无需人工介入 |
| FR-3 | 单 key 线性一致 | 任一读看到此前已完成的写；并发读写用 `go test -race` 无数据竞争 |
| FR-4 | 损坏可检测 | 位翻转、半条记录、伪造长度都触发 `ErrCorrupt` 或安全停止，绝不把脏数据带进状态机 |
| FR-5 | 日志可截断 | 快照成功后可删除被覆盖的旧段，恢复时间不再随历史写次数增长 |
| FR-6 | 错误码可判定 | 响应帧 status 位承载稳定错误码，客户端 `errors.Is(err, ErrNotFound)` 成立 |
| FR-7 | 资源故障可判定 | 磁盘满返回 `ErrNoSpace`、短写返回 `ErrPartialWrite`，两者都不污染状态机 |
| FR-8 | 优雅关闭 | SIGTERM 后组提交收尾、强制 Sync、关闭 fd，已返回成功的写全部落盘 |

**非功能指标表**

| 指标 | 目标 | 口径 | 测量方式 |
| --- | --- | --- | --- |
| 写吞吐（组提交） | 不低于 6 万 ops/s | 128 并发客户端、value 256B、含帧往返 | 端到端压测 + `benchstat` |
| 写吞吐（每条 fsync） | 不低于 8 千 ops/s | 同上，SyncEvery 档 | 同上 |
| 读吞吐 | 不低于 30 万 ops/s | 128 并发、命中率 100% | `BenchmarkShardTableGet` 与端到端 |
| 写 P99 延迟 | 不超过 2 ms | 组提交、不含网络 | 直方图埋点 |
| 读 P99 延迟 | 不超过 200 微秒 | 纯内存分片读 | 同上 |
| 数据量 | 2000 万 key / 约 5 GiB 逻辑值 | value 平均 256B | `ShardTable.Len` |
| 一致性级别 | 单 key 线性一致 | 写全局串行、读走分片读锁 | 并发用例 + race |
| 可用性 SLA | 单机 99.9%（月度） | 不含计划内重启 | 心跳探测 |
| 恢复目标 RTO | 不超过 10 秒 | 500 万 key 快照加尾部 1 万条日志 | 恢复计时埋点 |
| 恢复目标 RPO | 组提交不超过 5 ms，每条 fsync 近似 0 | 由 SyncPolicy 决定 | 故障注入 |
| WAL 段大小 / 分片数 | 64 MiB / 32 片 | 二者均可配置 | 配置项表 |

**fsync 三档取舍表**（这是本讲最贵的一个旋钮，必须先定价）

| 档位 | fsync 时机 | RPO | 相对吞吐 | 适用场景 |
| --- | --- | --- | --- | --- |
| `SyncEvery` | 每条 `Append` 后立即 `File.Sync` | 近似 0，只丢内核尚未返回的最后一条 | 最低，退化为磁盘 fsync 速率 | 支付幂等表、配置开关 |
| `SyncGroup` | 后台按窗口批量 Sync，默认 5 ms | 不超过一个窗口内未落盘的写 | 最高，fsync 次数被摊薄 | 会话、限流计数等可容忍毫秒级丢失 |
| `SyncNone` | 只 write 不 Sync，交给 OS 页缓存 | 掉电可能丢数秒 | 最高，但无持久性语义 | 纯缓存、可随时重建的数据 |

<Callout type="idea" title="为什么把 fsync 做成显式档位而不是一个布尔值" text="因为「要不要持久」不是非黑即白，而是「愿意为多长的 RPO 付多少吞吐」。写成三个枚举，调用方在配置里选档，压测表按档分别给数字，评审时才能对着 SLA 做决定。隐藏成一行 file.Sync() 的写法，出事时没人说得清丢了多少。" />

**明确本讲不做**：多副本与共识（第 114 讲）、数据压缩、二级索引与范围扫描、TTL 与过期回收（第 111 讲留的扩展点）、跨 key 事务、认证与 TLS、连接池与流水线（第 112 讲留的扩展点）。**假设边界**：单进程单实例、本地块设备、文件系统支持原子 `rename` 与 `ftruncate`；日志位点用字节偏移而非时间戳，因此不依赖墙上时钟，天然规避时钟回拨；不假设掉电时页缓存不丢——恰恰相反，只有 fsync 过的才算数。

## 2. 架构蓝图

<ArchBlueprint title="持久化版 KV 的四层架构" caption="接入层承接第 112 讲 TCP 帧；命令与并发层决定写顺序与锁粒度；持久化层只追加不改写；恢复层在启动时反向回填状态" :layers='[{"name":"接入层","note":"承接第 112 讲的 TCP 帧，只做编解码与错误码翻译","protocol":"长度前缀帧 v1 / 8 字节头","items":[{"label":"帧解码器","sub":"magic 与 version 校验","detail":"职责：把定长头加 body 还原成 Command 并拒绝超大帧；技术选型：io.ReadFull 加 16MiB 上限，不引第三方编解码库；失败处理：魔数或长度越界立即断连，计入坏帧指标。","tone":"blue"},{"label":"错误码编码器","sub":"status 位数值化","detail":"职责：把哨兵错误映射成稳定状态码写进响应帧；技术选型：codeOf 与 errOf 一张表两端共用，16 到 22 为本讲占用；失败处理：未知错误收敛为 255，客户端降级成普通错误并告警。","tone":"blue"},{"label":"连接处理器","sub":"每连接一协程","detail":"职责：串行读取帧并交回 Storage.Apply；技术选型：沿用第 112 讲每连接一个 goroutine；失败处理：读超时或半包断连，SetDeadline 兜住慢客户端。","tone":"blue"}]},{"name":"命令与并发层","note":"决定写顺序、锁粒度与 fsync 时机","protocol":"Command 值传递 / 分片路由","items":[{"label":"命令校验器","sub":"空 key 与非法 Op","detail":"职责：在写日志前拒绝空 key 与未知 Op；技术选型：validOp 与哨兵错误，沿用第 111 讲错误模型；失败处理：返回 ErrEmptyKey 或 ErrBadOp，绝不落一条脏日志。","tone":"green"},{"label":"写串行器","sub":"日志序等于状态序","detail":"职责：把追加日志与改内存串成不可切分的原子步骤；技术选型：WALStorage 外层 sync.Mutex；失败处理：日志写失败立即返回，状态机保持崩溃前一致。","tone":"green"},{"label":"锁分片表","sub":"32 个哈希桶","detail":"职责：把整表读写锁拆成 32 把分片锁；技术选型：FNV-1a 取模，进出 byte 切片一律值拷贝；失败处理：读路径不取写串行锁，锁顺序固定为外层写锁再分片锁。","tone":"green"},{"label":"组提交调度器","sub":"后台 fsync 协程","detail":"职责：按窗口把多条写合并成一次 fsync；技术选型：SyncGroup 档位加 time.Ticker 与 WaitGroup；失败处理：关闭时先收尾 Sync 再退出，pending 未落盘按 RPO 窗口兜底。","tone":"green"}]},{"name":"持久化层","note":"只追加、不改写，记录自带长度与校验和","protocol":"长度加 CRC32C 记录流 / O_APPEND","items":[{"label":"WAL 追加器","sub":"单写入口 Append","detail":"职责：把 Command 编成记录追加到当前段并返回全局位点；技术选型：os.File 以 O_APPEND 打开，配 SyncEvery 与 SyncGroup 与 SyncNone 三档；失败处理：ENOSPC 返回 ErrNoSpace，短写回滚到写入前边界并返回 ErrPartialWrite。","tone":"orange"},{"label":"段轮转器","sub":"64MiB 一段","detail":"职责：段达到阈值时 Sync 并关闭旧段、创建下一段；技术选型：wal 加序号加 log 命名，序号排序即重放顺序；失败处理：旧段 Sync 失败则中止轮转，写不继续。","tone":"orange"},{"label":"CRC 校验器","sub":"CRC32C 逐记录","detail":"职责：写入算校验和、读取先验校验和再解码；技术选型：crc32.Castagnoli 多项式，长度字段挡半条记录；失败处理：校验不符返回 ErrCorrupt 并停止重放，绝不带入脏数据。","tone":"orange"},{"label":"快照器","sub":"tmp 加 Rename","detail":"职责：把位点与全量键值写成一致视图并原子替换；技术选型：encoding/gob 编码，写临时文件 Sync 后 os.Rename；失败处理：半份快照由 LoadSnapshot 解码失败退回全量重放。","tone":"orange"}]},{"name":"恢复层","note":"启动时按固定四步把状态追回崩溃前","items":[{"label":"快照加载器","sub":"位点加全量数据","detail":"职责：读取最近快照得到起始位点与键值；技术选型：LoadSnapshot 返回 Offset 与 Data；失败处理：文件缺失或解码失败则忽略快照，从零重放全部日志。","tone":"purple"},{"label":"重放器","sub":"顺序幂等重放","detail":"职责：从位点起把记录交回状态机；技术选型：Replay 用 io.ReadFull 逐条读，OpSet 与 OpDel 幂等；失败处理：遇半条或 CRC 失败返回干净位点并停止。","tone":"purple"},{"label":"脏尾裁剪器","sub":"TruncateTo","detail":"职责：把日志目录裁到干净位点，丢弃崩溃尾巴；技术选型：os.Truncate 加删除后续段；失败处理：裁剪失败则拒绝启动，避免新写追加到脏尾巴之后。","tone":"purple"},{"label":"位点管理器","sub":"全局字节偏移","detail":"职责：维护跨段连续的全局位点供快照与 compaction 使用；技术选型：base 累加旧段大小加当前段 offset；失败处理：位点与数据在同一把写锁内读取，避免漏放或重放越界。","tone":"purple"}]}]' />

逐层解读。**接入层**只做「字节到 Command」的翻译，它存在的理由是第 112 讲已经把 TCP 帧定型，本讲不该让磁盘逻辑泄漏到网络层；把错误码数值化放在这里，是因为 status 位本就属于帧格式。**命令与并发层**是新增的调度中枢，它决定「谁先写、锁哪一片、什么时候算写成功」；把写串行器单独列出，是为了让「日志顺序等于状态变更顺序」这条不变量有明确的负责人，而不是散落在各处加锁。**持久化层**刻意保持极窄的职责：只追加、不改写、不解析业务，记录格式自带长度与校验和，因此恢复层可以完全独立地读它。**恢复层**只在启动时运行一次，方向与写路径相反：写路径是「日志 → 内存」，恢复路径是「磁盘 → 内存」。

<ConceptDiagram type="pipeline" title="WAL 写入流水线：编码 → 追加 → 组提交 → 生效" caption="四个阶段各自独立，前一阶段失败就不会进入下一阶段，因此状态机永远只看到可重放的写" />

<FlowTimeline direction="horizontal" :steps='[{"title":"加载快照","desc":"读位点与全量键值","tone":"blue"},{"title":"从位点重放","desc":"幂等补齐尾部记录","tone":"green"},{"title":"裁剪脏尾巴","desc":"TruncateTo 到干净位点","tone":"orange"},{"title":"新建段","desc":"OpenWAL 续写不混脏数据","tone":"purple"},{"title":"对外服务","desc":"状态与崩溃前一致","tone":"red"}]' />

依赖方向严格单向：接入层依赖命令层暴露的 `Storage`，命令层依赖持久化层的 `WAL` 与恢复层的 `Replay`，持久化层只知道 `Command` 与 `os.File`，恢复层只依赖记录格式。**不允许反向依赖**的理由很实际：一旦 WAL 里 import 了帧编解码或分片表，恢复路径就被迫拉起网络栈与锁，冷启动会变慢且更容易在半初始化状态下踩空；反过来让恢复层只认「长度 + CRC + 载荷」这三样东西，任何语言写的巡检工具都能离线读日志，这是运维上的救命通道。

## 3. 模块职责与依赖方向

模块职责表把「谁持有状态、失败时怎么表现」写死，评审时最容易漏的就是「状态」与「失败行为」两列。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `Frame/framecode` | 字节流 / error | `Command` / 状态码 | 无 | 无状态 | 坏帧断连，未知错误码收敛为 255 |
| `record` | `Command` | 记录字节 / 解码结果 | `crc32` | 无状态 | 校验不符返回 `ErrCorrupt` |
| `WAL` | `Command` | 全局位点 | `os.File`、`Metrics` | 当前段、base、pending | ENOSPC 与短写向上抛，状态机不变 |
| `Replay/TruncateTo` | 目录、位点 | 干净位点 | 记录格式 | 无状态（一次性） | 半条或 CRC 失败安全停止 |
| `ShardTable` | key / 命令 | `[]byte` / 全量视图 | 无 | 32 个 map | 未命中返回 ok 为假 |
| `WALStorage` | `Command` | `Result` | 上述全部 | 写锁、closed 标志 | 日志失败即返回，不半改内存 |
| `Snapshot/Compact` | 路径、位点 | 快照文件 / 删除段数 | `WALStorage`、`gob` | 无状态 | 半份快照退回全量重放 |
| `Metrics` | 埋点调用 | 指标视图 | `sync/atomic` | 原子计数器 | 不参与主链路，永不阻塞写 |

包目录（全部同属 `package kv`，落盘后 `go test ./...` 即可编译运行）：

```text
kv/
├── go.mod                # module kv，go 1.21
├── contract.go           # 契约：Op / Command / Result / Storage / 哨兵错误
├── framecode.go          # 帧 status 位错误码表（承接第 112 讲）
├── record.go             # WAL 记录编码与 CRC32C
├── wal.go                # 追加写、fsync 三档、段轮转
├── recovery.go           # 重放、段枚举、脏尾裁剪
├── shard.go              # 32 把锁的分片表
├── snapshot.go           # 快照与 compaction
├── wal_storage.go        # 装配、Apply、Close、指标视图
├── metrics.go            # 原子计数埋点
└── wal_test.go           # 表驱动测试、恢复测试、基准
```

依赖倒置体现在两处接口上：`Storage` 是四讲不变的窄腰，`WALStorage` 实现它；`WAL.Append` 只接受 `Command` 并返回位点，不知道上层是网络帧还是本地调用。于是第 114 讲把 `WALStorage` 换成 `ReplicaGroup` 时，接入层一行都不用动。

<Callout type="warn" title="边界划分最常见的两种错法" text="第一，把 WAL 追加写塞进帧解码器，理由是「反正都是写字节」——结果是离线恢复工具必须链接网络栈，且一个坏帧可能写坏日志。第二，让 ShardTable 知道文件路径或位点，理由是「快照方便」——结果是分片表被持久化细节污染，换存储引擎时 32 个桶全要重写。判断标准很简单：这个模块能不能单独用 net.Pipe 或临时目录做单元测试？能，边界就对了。" />

## 4. 数据与协议契约

**WAL 记录格式**（定长头加变长载荷，全部大端序）

| 字段 | 字节数 | 类型 | 语义 | 零值 / 兼容规则 |
| --- | --- | --- | --- | --- |
| `length` | 4 | uint32 | payload 字节数 | 为 0 视为半条记录；上限 16 MiB，超限即 `ErrCorrupt` |
| `crc32c` | 4 | uint32 | payload 的 CRC32C | 写入必填；校验不符一律停止重放，不跳过 |
| `op` | 1 | byte | 沿用第 111 讲 `OpGet/OpSet/OpDel` 数值 1/2/3 | 未知值返回 `ErrBadOp`，数值跨版本不改 |
| `keyLen` | 2 | uint16 | key 字节数 | 为空 key 时由上层拦截，不落盘 |
| `valLen` | 4 | uint32 | value 字节数 | 允许 0，表示空值是合法命中 |
| `key` | 变长 | bytes | 键 | 二进制安全，不含分隔符 |
| `value` | 变长 | bytes | 值 | 允许任意字节，含 0x00 与换行 |

```text
一条 SET user:1 benji 的记录（十六进制示意）
00 00 00 10   <- length = 16（op1 + keyLen2 + valLen4 + key7 + value?）
xx xx xx xx   <- crc32c(payload)
02            <- OpSet
00 07         <- keyLen = 7
00 00 00 05   <- valLen = 5
75 73 65 72 3a 31 62 65 6e 6a 69  <- key "user:1" + value "benji"
```

**帧 status 位错误码表**（承接第 112 讲 status 0 请求 / 1 成功 / 2 失败的旧约定，把 2 展开成稳定数值）

| 码值 | 含义 | 客户端还原 |
| --- | --- | --- |
| 1 | 成功 | `nil` |
| 16 | 键不存在 | `ErrNotFound` |
| 17 | 空 key | `ErrEmptyKey` |
| 18 | 未知 Op | `ErrBadOp` |
| 19 | 存储已关闭 | `ErrClosed` |
| 20 | 记录损坏 | `ErrCorrupt` |
| 21 | 磁盘满 | `ErrNoSpace` |
| 22 | 部分写 | `ErrPartialWrite` |
| 23 及以上 | 预留第 114 讲分布式错误 | 由下一讲扩展 |
| 255 | 未知内部错误 | 普通 error + 告警 |

对外没有新增 API，仍是第 111 讲的 `Storage.Apply(Command) (Result, error)`；新增的是**配置项契约**，部署时以 JSON 注入：

```json
{
  "dir": "/var/lib/kv/wal",
  "sync_policy": "group",
  "group_window_ms": 5,
  "segment_size_mib": 64,
  "snapshot": "/var/lib/kv/snapshot.gob",
  "shard_count": 32
}
```

生产环境还会用一张运维元数据表管段与位点，便于巡检与人工介入（应用本身不读它，只由运维工具写）：

```sql
CREATE TABLE wal_meta (
    segment_no   INTEGER PRIMARY KEY,
    file_path    TEXT NOT NULL,
    start_offset BIGINT NOT NULL,
    end_offset   BIGINT NOT NULL,
    sealed       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE snapshot_meta (
    id           INTEGER PRIMARY KEY,
    offset_bytes BIGINT NOT NULL,
    created_at   TEXT NOT NULL,
    checksum     TEXT NOT NULL
);
```

内部接口签名与四讲不变的契约快照如下（本讲新增三条持久化哨兵错误，`Op` 数值与字段一律不动）：

```go
// 文件：contract.go
package kv

import "errors"

// Op 用显式数值定义，第 112 讲把它编码进 TCP 帧，本讲写进 WAL，
// 第 114 讲写进 Raft 日志，数值必须长期稳定。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

// 哨兵错误沿用第 111 讲；本讲新增三条与持久化相关的错误，
// 调用方仍用 errors.Is 精确判定，不比对错误文本。
var (
	ErrNotFound     = errors.New("kv: key not found")
	ErrClosed       = errors.New("kv: storage closed")
	ErrEmptyKey     = errors.New("kv: empty key")
	ErrBadOp        = errors.New("kv: unknown op")
	ErrCorrupt      = errors.New("kv: wal record corrupt")
	ErrNoSpace      = errors.New("kv: no space left on device")
	ErrPartialWrite = errors.New("kv: partial write to wal")
)

// Command 是贯穿四讲唯一的数据载体：网络帧、WAL 记录、Raft 日志都只传递它。
type Command struct {
	Op    Op
	Key   string
	Value []byte
}

// Result 用 Found 把「键不存在」与「值恰好为空」区分开。
type Result struct {
	Value []byte
	Found bool
}

// Storage 是四讲唯一的抽象，Apply 是「执行一次命令」的完整语义。
type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

字段语义与兼容规则：`length` 与 `crc32c` 属于格式版本 1，未来若加字段只能追加在 payload 尾部并升 version，绝不能重排头部；`Op` 与 status 码值一旦发布就冻结，这是第 111 讲用显式数值而非 `iota` 的原因。灰度策略是「双写 + 双读」：升级期同时写旧段与新段，恢复时按 version 分派解码，观察一个快照周期无 `crc_failures` 再停写旧格式。

<Compare title="记录头放校验和还是记录尾放校验和" :panels='[{"label":"头部 length 加 crc","code":"length(4) | crc32c(4) | payload","note":"本讲做法：先读 8 字节就能判断记录是否完整、是否该分配内存，半条记录当场停住"},{"label":"尾部追加 crc","code":"payload | crc32c(4)","note":"读到尾才能校验，遇到伪造 length 会先申请大内存，恶意输入下容易被打爆"}]' />

## 5. 核心链路时序

<SequenceDiagram title="一次 SET 命令的持久化链路" caption="写串行锁把 Append 与生效串成原子步骤；组提交把 fsync 从每条一次合并成每窗口一次" :actors='["Client","Server","WriteMu","WAL","GroupSync","Shard"]' :steps='[{"from":"Client","to":"Server","label":"SET 帧","note":"status 位为请求 0","kind":"call"},{"from":"Server","to":"Server","label":"校验命令","note":"空 key 直接拒绝","kind":"self"},{"from":"Server","to":"WriteMu","label":"取写串行锁","note":"日志序等于状态序","kind":"call"},{"from":"WriteMu","to":"WAL","label":"Append 记录","note":"长度加 CRC32C","kind":"call"},{"from":"WAL","to":"GroupSync","label":"登记组提交","note":"仅 SyncGroup 档","kind":"async"},{"from":"GroupSync","to":"GroupSync","label":"按窗口 fsync","note":"默认 5ms 一批","kind":"self"},{"from":"GroupSync","to":"WAL","label":"落盘确认","note":"推进持久化位点","kind":"return"},{"from":"WAL","to":"Shard","label":"放行生效","note":"此写已可重放","kind":"call"},{"from":"Shard","to":"Shard","label":"分片锁改 map","note":"32 把锁之一","kind":"self"},{"from":"Shard","to":"Server","label":"返回 Result","note":"Found 为 true","kind":"return"},{"from":"Server","to":"Client","label":"回写 OK 帧","note":"错误码进 status","kind":"return"}]' />

逐步骤解读。第 1 步客户端按第 112 讲帧格式发来 `OpSet`；第 2 步服务端做纯内存校验，空 key 与非法 Op 在这里就被挡掉，**绝不落一条脏日志**；第 3 步取 `WALStorage.mu`，这把锁把「写日志 + 改内存」串成不可切分的整体；第 4 步 `Append` 编码记录、算 CRC32C、`O_APPEND` 写入，返回全局位点；第 5 到 7 步是 `SyncGroup` 特有的异步组提交，`SyncEvery` 时这三步收窄为第 4 步内一次同步 `File.Sync`；第 8 步日志已可重放，才允许改状态机；第 9 步只取目标分片的锁；第 10、11 步把 `Result` 与状态码回写。注意**返回给客户端的时机**：`SyncEvery` 是 fsync 完成后返回，`SyncGroup` 是 write 到页缓存后即返回，两者的 RPO 差异就藏在这里。

**异常链路一：磁盘满**。第 4 步 `Write` 返回 `ENOSPC`，`Append` 包装成 `ErrNoSpace` 直接返回，第 8 步不执行，状态机保持崩溃前一致；服务端把状态码 21 回给客户端，`disk_full_total` 加一，写入错误率告警触发。**分叉点在第 4 步之后**：正常链路继续到 Shard，异常链路在第 4 步立刻折返。**异常链路二：部分写**。若 `Write` 返回短写，`Append` 用 `Truncate` 把当前段裁回写入前边界并返回 `ErrPartialWrite`；若不裁，半条记录会留在日志尾，下次重启重放永远卡在同一处。**异常链路三：组提交未落盘就崩溃**。第 5 到 7 步之间掉电，页缓存里那批写丢失，但客户端已收到成功——这正是 `SyncGroup` 声明「RPO 不超过 5 ms」换来的吞吐，属于**有意为之的取舍**，不是 bug；要消灭它只能切回 `SyncEvery`。

<StepBox title="崩溃恢复的四步状态迁移" :steps='[{"title":"第 1 步","body":"LoadSnapshot 读到位点与全量键值，内存表先有 500 万个 key","tip":"快照缺失或半份就跳过，从零重放"},{"title":"第 2 步","body":"Replay 从位点起顺序应用尾部记录，OpSet 与 OpDel 幂等","tip":"遇半条或 CRC 失败立即停，返回干净位点"},{"title":"第 3 步","body":"TruncateTo 把日志裁到干净位点，丢掉崩溃留下的脏尾巴","tip":"不裁的话新写会追加到脏尾巴之后"},{"title":"第 4 步","body":"OpenWAL 以最大序号加一新建段，base 累加旧段大小","tip":"旧段只读，新写与崩溃痕迹彻底隔离"}]' startLabel="开始恢复" />

## 6. 核心实现（按模块逐段展开）

以下代码同属 `package kv`，分别存为 `contract.go`（已在第 4 节给出）、`framecode.go`、`record.go`、`wal.go`、`recovery.go`、`shard.go`、`snapshot.go`、`wal_storage.go`（本节）、`metrics.go`（第 9 节）、`wal_test.go`（第 10 节）。全部放进同一目录，`go.mod` 声明 `go 1.21`，执行 `go test -race ./...` 即可编译并跑通全部测试与基准；作者已在 Go 1.23.8 上实测通过。每段都保留完整 `package` 与 `import`，可独立落盘。

### 6.1 帧状态码表：兑现第 112 讲的遗留修复

**设计意图**：第 112 讲把 `err.Error()` 文本塞进 `status=2` 的响应帧，导致 `ErrNotFound` 到客户端退化成普通 error，`errors.Is` 失效。这里把错误编码成稳定数值。**关键取舍**：编码表两端共用一份、数值冻结并预留 23 以上给第 114 讲，代价是多维护一张表，换来的是跨网络仍可精确判定错误类型，磁盘满、部分写这类需要特殊重试策略的错误才不会被当成普通失败。

```go
// 文件：framecode.go
package kv

import (
	"errors"
	"fmt"
)

// 第 112 讲把错误文本塞进 status=2 的响应帧，导致 ErrNotFound 跨网络退化成
// 普通 error、errors.Is 失效。本讲把错误编码成稳定数值写进帧头的 status 字节，
// 客户端按同一张表还原成哨兵错误；数值与 Op 一样进入协议格式，长期不改。
// 16~22 为本讲占用，23 及以上预留给第 114 讲的分布式错误。
const (
	StatusOK     byte = 1
	CodeNotFound byte = 16
	CodeEmptyKey byte = 17
	CodeBadOp    byte = 18
	CodeClosed   byte = 19
	CodeCorrupt  byte = 20
	CodeNoSpace  byte = 21
	CodePartial  byte = 22
)

// codeOf 把服务端错误映射成帧状态码，未知错误统一收敛到 255。
func codeOf(err error) byte {
	switch {
	case err == nil:
		return StatusOK
	case errors.Is(err, ErrNotFound):
		return CodeNotFound
	case errors.Is(err, ErrEmptyKey):
		return CodeEmptyKey
	case errors.Is(err, ErrBadOp):
		return CodeBadOp
	case errors.Is(err, ErrClosed):
		return CodeClosed
	case errors.Is(err, ErrCorrupt):
		return CodeCorrupt
	case errors.Is(err, ErrNoSpace):
		return CodeNoSpace
	case errors.Is(err, ErrPartialWrite):
		return CodePartial
	default:
		return 255
	}
}

// errOf 在客户端把状态码还原成哨兵错误，errors.Is 再次成立。
func errOf(code byte) error {
	switch code {
	case StatusOK:
		return nil
	case CodeNotFound:
		return ErrNotFound
	case CodeEmptyKey:
		return ErrEmptyKey
	case CodeBadOp:
		return ErrBadOp
	case CodeClosed:
		return ErrClosed
	case CodeCorrupt:
		return ErrCorrupt
	case CodeNoSpace:
		return ErrNoSpace
	case CodePartial:
		return ErrPartialWrite
	default:
		return fmt.Errorf("kv: server status %d", code)
	}
}
```

<Callout type="warn" title="状态码不是随便排的" text="16 起跳是为了避开第 112 讲已占用的 0 请求、1 成功、2 失败三个值；23 以上留给第 114 讲的 ErrNotLeader 与 ErrNoQuorum，这样下一讲扩展错误时不需要改动本讲任何一行。把「数值是协议的一部分」当纪律，才不会在半年后因为重排枚举把线上旧日志解释成别的操作。" />

### 6.2 记录编码与 CRC32C：把损坏挡在状态机之外

**设计意图**：把 `Command` 编成自带长度与校验和的二进制记录。**关键取舍**：选 `crc32.Castagnoli`（CRC32C）而非 IEEE，因为前者对突发位错的检测更强且有硬件指令；长度字段与 CRC 各司其职——长度挡半条记录，CRC 挡位翻转，缺一不可。解码时用 `uint64` 做长度求和，避免 32 位平台上 `uint32` 转 `int` 溢出成负数导致切片越界。

```go
// 文件：record.go
package kv

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// WAL 记录格式：length(4, uint32 BE) | crc32c(4) | payload。
// payload = op(1) keyLen(2) valLen(4) key value。
// 长度挡住半条记录，CRC32C 挡住位翻转与扇区撕裂：两者缺一不可。
const (
	recordHead = 8
	maxRecord  = 16 << 20
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

func validOp(op Op) bool {
	return op == OpGet || op == OpSet || op == OpDel
}

func encodeCommand(cmd Command) []byte {
	buf := make([]byte, 0, 7+len(cmd.Key)+len(cmd.Value))
	buf = append(buf, byte(cmd.Op))
	var kl [2]byte
	binary.BigEndian.PutUint16(kl[:], uint16(len(cmd.Key)))
	buf = append(buf, kl[:]...)
	var vl [4]byte
	binary.BigEndian.PutUint32(vl[:], uint32(len(cmd.Value)))
	buf = append(buf, vl[:]...)
	buf = append(buf, cmd.Key...)
	buf = append(buf, cmd.Value...)
	return buf
}

func decodeCommand(b []byte) (Command, error) {
	if len(b) < 7 {
		return Command{}, ErrCorrupt
	}
	kl := uint64(binary.BigEndian.Uint16(b[1:3]))
	vl := uint64(binary.BigEndian.Uint32(b[3:7]))
	if kl+vl+7 != uint64(len(b)) {
		return Command{}, ErrCorrupt
	}
	if !validOp(Op(b[0])) {
		return Command{}, ErrBadOp
	}
	off := 7 + int(kl)
	return Command{
		Op:    Op(b[0]),
		Key:   string(b[7:off]),
		Value: append([]byte(nil), b[off:]...),
	}, nil
}

// encodeRecord 把命令封成带长度与 CRC32C 的完整记录。
func encodeRecord(cmd Command) ([]byte, error) {
	payload := encodeCommand(cmd)
	if len(payload)+recordHead > maxRecord {
		return nil, fmt.Errorf("%w: payload %d", ErrPartialWrite, len(payload))
	}
	rec := make([]byte, recordHead+len(payload))
	binary.BigEndian.PutUint32(rec[0:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(rec[4:8], crc32.Checksum(payload, crcTable))
	copy(rec[recordHead:], payload)
	return rec, nil
}

// decodeRecord 校验一条完整记录；CRC 不符返回 ErrCorrupt，绝不静默跳过。
func decodeRecord(rec []byte) (Command, error) {
	if len(rec) < recordHead {
		return Command{}, ErrCorrupt
	}
	n := uint64(binary.BigEndian.Uint32(rec[0:4]))
	if n+recordHead != uint64(len(rec)) || n > maxRecord {
		return Command{}, ErrCorrupt
	}
	payload := rec[recordHead:]
	if crc32.Checksum(payload, crcTable) != binary.BigEndian.Uint32(rec[4:8]) {
		return Command{}, ErrCorrupt
	}
	return decodeCommand(payload)
}
```

下面把写路径里最容易被省掉的边界判断逐行拆开看，这几行才是持久性真正落地的地方。

<LineByLine title="Append 的三道边界判断" code="w.mu.Lock()\ndefer w.mu.Unlock()\nif w.closed || w.f == nil {\n    return 0, ErrClosed\n}\nrec, err := encodeRecord(cmd)\nif err != nil {\n    return 0, err\n}\nw.offset += int64(len(rec))\nreturn w.base + w.offset, nil" :notes='["进入写入口先取锁，文件句柄、位点、pending 计数都由这把锁保护","用 defer 保证任何提前 return 都会释放锁，避免一次磁盘满拖死整个实例","关闭后或句柄为空必须拒绝，否则已关闭的文件被写入会留下无法重放的脏记录","编码在锁内做，长度超限与非法载荷在写盘之前就被挡掉","encodeRecord 失败直接返回，状态机与位点都不推进","只有 Write 成功才累加位点，位点即快照与恢复的分界线","返回全局位点，调用方据此判断这批写是否已被快照覆盖"]' />

<OutputBox title="记录往返与位翻转注入的测试输出" output="=== RUN   TestRecordRoundTrip\n--- PASS: TestRecordRoundTrip (0.00s)\n=== RUN   TestDecodeRecordRejectsBitFlip\n--- PASS: TestDecodeRecordRejectsBitFlip (0.00s)\nPASS" note="空值、OpDel、含 0x00 的二进制 value 都能往返；翻转最后一个字节必定得到 ErrCorrupt" />

### 6.3 WAL 追加写：fsync 三档、段轮转、磁盘满与部分写

**设计意图**：把「一次写」收敛到唯一入口 `Append`，一次调用完成编码、轮转判断、写入、按策略 fsync、位点推进。**关键取舍**：不套 `bufio.Writer`——第 112 讲那种用户态缓冲会让「Flush 了但没 Sync」的假持久性更隐蔽，直接 `os.File.Write` 让每一层的语义都可说清；组提交用后台 goroutine 按窗口批量 `Sync`，把 fsync 次数从「每条一次」降到「每 5 ms 一次」。**代价**：`SyncGroup` 在崩溃时丢一个窗口，这是明码标价写进 RPO 的。

```go
// 文件：wal.go
package kv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// SyncPolicy 决定 Append 何时真正把数据交给磁盘，是吞吐与 RPO 的核心旋钮。
type SyncPolicy uint8

const (
	SyncEvery SyncPolicy = iota // 每条 fsync，RPO≈0，吞吐最低
	SyncGroup                   // 后台按窗口批量 fsync，RPO=窗口
	SyncNone                    // 只 write 不 fsync，仅用于可丢数据场景
)

// WAL 只做一件事：把命令以「长度 + CRC + 载荷」追加到当前段。
// mu 同时保护文件句柄、位点与组提交计数；Append 是唯一写入口。
type WAL struct {
	mu          sync.Mutex
	dir         string
	f           *os.File
	base        int64 // 之前所有段的累计字节
	offset      int64 // 当前段内已写字节
	segIndex    int
	segmentSize int64
	policy      SyncPolicy
	window      time.Duration
	pending     int
	stop        chan struct{}
	wg          sync.WaitGroup
	closed      bool
	metrics     *Metrics
}

// OpenWAL 总是新建一个段：旧段保持只读，崩溃尾巴不会与新写混在一起。
// base 由当前所有段的大小累加得到，从而给出连续的全局位点。
func OpenWAL(dir string, policy SyncPolicy, window time.Duration, segSize int64, m *Metrics) (*WAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if segSize <= 0 {
		segSize = 64 << 20
	}
	if window <= 0 {
		window = 5 * time.Millisecond
	}
	segs, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	idx := 0
	var base int64
	for _, s := range segs {
		idx = s.index + 1
		base += s.size
	}
	name := filepath.Join(dir, fmt.Sprintf("wal-%06d.log", idx))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &WAL{
		dir: dir, f: f, base: base, segIndex: idx,
		segmentSize: segSize, policy: policy, window: window,
		stop: make(chan struct{}), metrics: m,
	}
	if policy == SyncGroup {
		w.wg.Add(1)
		go w.syncLoop()
	}
	return w, nil
}

// Append 追加一条写命令，返回写入后的全局位点。
// 三处边界判断是重点：段轮转、磁盘满、部分写。
func (w *WAL) Append(cmd Command) (int64, error) {
	rec, err := encodeRecord(cmd)
	if err != nil {
		return 0, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.f == nil {
		return 0, ErrClosed
	}
	if w.offset+int64(len(rec)) > w.segmentSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(rec)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return 0, fmt.Errorf("%w: %v", ErrNoSpace, err)
		}
		return 0, err
	}
	if n != len(rec) {
		// 部分写：把文件裁回写入前的边界，绝不让半条记录留在日志尾。
		if terr := w.f.Truncate(w.offset); terr != nil {
			return 0, fmt.Errorf("%w: rollback: %v", ErrPartialWrite, terr)
		}
		return 0, ErrPartialWrite
	}
	w.offset += int64(n)
	if w.policy == SyncGroup {
		w.pending++
	}
	if w.policy == SyncEvery {
		if err := w.f.Sync(); err != nil {
			return 0, err
		}
		if w.metrics != nil {
			w.metrics.syncCount.Add(1)
		}
	}
	if w.metrics != nil {
		w.metrics.walBytes.Add(int64(n))
	}
	return w.base + w.offset, nil
}

func (w *WAL) rotateLocked() error {
	if err := w.f.Sync(); err != nil {
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.base += w.offset
	w.offset = 0
	w.segIndex++
	name := filepath.Join(w.dir, fmt.Sprintf("wal-%06d.log", w.segIndex))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	return nil
}

func (w *WAL) syncLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.window)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.mu.Lock()
			if !w.closed && w.f != nil && w.pending > 0 {
				if err := w.f.Sync(); err == nil {
					w.pending = 0
					if w.metrics != nil {
						w.metrics.syncCount.Add(1)
					}
				}
			}
			w.mu.Unlock()
		case <-w.stop:
			w.mu.Lock()
			if w.f != nil {
				_ = w.f.Sync()
				if w.metrics != nil {
					w.metrics.syncCount.Add(1)
				}
			}
			w.mu.Unlock()
			return
		}
	}
}

// Offset 返回当前全局位点，供快照记录「日志读到这里」。
func (w *WAL) Offset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.base + w.offset
}

func (w *WAL) Close() error {
	if w.policy == SyncGroup {
		close(w.stop)
		w.wg.Wait()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.f == nil {
		return nil
	}
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}
```

<Callout type="warn" title="两个不能省的返回值判断" text="第一，Write 返回 n 小于 len(rec) 时必须 Truncate 回滚，否则日志尾留半条记录，后面每次重启都卡在同一处。第二，ENOSPC 必须包装成可判定的 ErrNoSpace，不能吞成普通 error——磁盘满是需要立刻停止接收写并告警、而不是重试三次就放过的故障，上层要据此切换只读降级。" />

### 6.4 崩溃恢复：重放、段枚举与脏尾裁剪

**设计意图**：恢复是冷启动唯一路径，必须做到「读到哪里算哪里、绝不越界」。`Replay` 逐条读完整记录并校验，遇半条或 CRC 失败返回干净位点；`TruncateTo` 把磁盘裁到该位点。**关键取舍**：恢复不联网、不取锁、不依赖时间戳，位点用字节偏移；代价是必须按顺序读所有段的记录头才能对齐边界，不能随机跳——这正是快照位点存在的意义，它把「从头扫」变成「从头扫但只应用尾部」。

```go
// 文件：recovery.go
package kv

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// segmentInfo 描述一个日志段；index 决定重放顺序，size 参与全局位点计算。
type segmentInfo struct {
	path  string
	index int
	size  int64
}

// listSegments 只认 wal-<序号>.log，忽略 snapshot.gob、*.tmp 等其它文件。
func listSegments(dir string) ([]segmentInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []segmentInfo
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "wal-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "wal-"), ".log"))
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, segmentInfo{
			path: filepath.Join(dir, name), index: idx, size: info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].index < out[j].index })
	return out, nil
}

// Replay 顺序读取所有段，只把位点不早于 fromOffset 的记录交给 apply。
// 返回已安全重放到的全局位点；遇到半条或 CRC 失败即停止，绝不把脏数据带进状态机。
func Replay(dir string, fromOffset int64, m *Metrics, apply func(Command) error) (int64, error) {
	segs, err := listSegments(dir)
	if err != nil {
		return 0, err
	}
	pos := int64(0)
	for _, s := range segs {
		f, err := os.Open(s.path)
		if err != nil {
			return pos, err
		}
		next, clean, err := replayOne(f, pos, fromOffset, m, apply)
		_ = f.Close()
		pos = next
		if err != nil {
			return pos, err
		}
		if !clean {
			// 崩溃点：段轮转保证脏尾巴只可能在最后一段，此处停止即安全。
			return pos, nil
		}
	}
	return pos, nil
}

func replayOne(f *os.File, start, fromOffset int64, m *Metrics, apply func(Command) error) (int64, bool, error) {
	pos := start
	for {
		head := make([]byte, recordHead)
		if _, err := io.ReadFull(f, head); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return pos, true, nil // 干净段尾
			}
			return pos, false, err
		}
		n := int64(binary.BigEndian.Uint32(head[0:4]))
		if n > maxRecord {
			if m != nil {
				m.crcFailures.Add(1)
			}
			return pos, false, nil
		}
		rec := make([]byte, int(recordHead+n))
		copy(rec, head)
		if _, err := io.ReadFull(f, rec[recordHead:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return pos, false, nil // 半条记录，崩溃点
			}
			return pos, false, err
		}
		cmd, err := decodeRecord(rec)
		if err != nil {
			if m != nil {
				m.crcFailures.Add(1)
			}
			return pos, false, nil
		}
		if pos >= fromOffset {
			if err := apply(cmd); err != nil {
				return pos, false, err
			}
		}
		pos += recordHead + n
	}
}

// TruncateTo 把日志目录裁剪到干净位点，丢弃崩溃留下的半条或损坏尾巴。
// 不清理的话，下次 Append 会追加到脏尾巴之后，恢复将永远卡在同一处。
func TruncateTo(dir string, lastGood int64) error {
	segs, err := listSegments(dir)
	if err != nil {
		return err
	}
	var covered int64
	for i, s := range segs {
		if covered >= lastGood {
			if err := os.Remove(s.path); err != nil {
				return err
			}
			continue
		}
		if covered+s.size > lastGood {
			if err := os.Truncate(s.path, lastGood-covered); err != nil {
				return err
			}
			for j := i + 1; j < len(segs); j++ {
				if err := os.Remove(segs[j].path); err != nil {
					return err
				}
			}
			return nil
		}
		covered += s.size
	}
	return nil
}
```

<Callout type="idea" title="为什么恢复后必须裁脏尾" text="只 Replay 不 TruncateTo 是最常见的半成品实现：本次启动看起来正常，但下一条 Append 会以 O_APPEND 追加到脏尾巴之后，下次重启 Replay 又在同一处停住，日志永远长不过那条坏记录。把「重放 + 裁剪」做成同一次启动的两个连续步骤，才算闭环；生产还会在裁剪前先把脏段拷贝一份留证。" />

### 6.5 锁分片：把一把大锁拆成 32 把

**设计意图**：第 111 讲 `Memory` 用一把 `RWMutex` 护住整张 map，写密集时所有 key 排队。这里按 key 的 FNV-1a 哈希取模分到 32 个桶，每桶一把锁。**关键取舍**：分片数固定为 32 且是 2 的幂，取模低成本、分布均匀；代价是跨 key 无法在一个原子操作里完成，且热点 key 仍会落在同一把锁上（分片不解决热点，只解决无相关的并发）。读写两侧对 `[]byte` 都做值拷贝，堵住第 111 讲强调过的切片逃逸。

```go
// 文件：shard.go
package kv

import "sync"

const shardCount = 32

type shard struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// ShardTable 把一张逻辑表拆成固定数量的分片，锁粒度从「整表」降到「单个哈希桶」。
// 32 是 2 的幂，取模可直接用位与，在 key 分布均匀时写竞争摊到 32 把锁上。
type ShardTable struct {
	shards [shardCount]shard
}

func NewShardTable() *ShardTable {
	t := &ShardTable{}
	for i := range t.shards {
		t.shards[i].data = make(map[string][]byte)
	}
	return t
}

// shardOf 用 FNV-1a 把 key 摊到分片；手写循环避免每次调用分配 hasher。
func shardOf(key string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h % shardCount
}

func (t *ShardTable) of(key string) *shard {
	return &t.shards[shardOf(key)]
}

// Get 返回副本，避免调用方持有内部切片后并发改写造成数据竞争。
func (t *ShardTable) Get(key string) ([]byte, bool) {
	sh := t.of(key)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	v, ok := sh.data[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), v...), true
}

func (t *ShardTable) Set(key string, val []byte) {
	sh := t.of(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.data[key] = append([]byte(nil), val...)
}

func (t *ShardTable) Del(key string) bool {
	sh := t.of(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	_, ok := sh.data[key]
	delete(sh.data, key)
	return ok
}

// Apply 只处理写命令，供写路径与重放路径共用；天然幂等。
func (t *ShardTable) Apply(cmd Command) {
	switch cmd.Op {
	case OpSet:
		t.Set(cmd.Key, cmd.Value)
	case OpDel:
		t.Del(cmd.Key)
	}
}

// Snapshot 在统一锁序下拷贝全量数据，供快照器生成一致视图。
func (t *ShardTable) Snapshot() map[string][]byte {
	all := make(map[string][]byte)
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.RLock()
		for k, v := range sh.data {
			all[k] = append([]byte(nil), v...)
		}
		sh.mu.RUnlock()
	}
	return all
}

func (t *ShardTable) Len() int {
	n := 0
	for i := range t.shards {
		sh := &t.shards[i]
		sh.mu.RLock()
		n += len(sh.data)
		sh.mu.RUnlock()
	}
	return n
}
```

<Term word="写放大" text="写一条逻辑数据实际写到磁盘的字节倍数；WAL 追加、快照全量重写、compaction 拷贝都会叠加进分子，是 LSM 系存储最核心的成本指标" />

### 6.6 快照与 compaction：给日志设一个终点

**设计意图**：日志只增不改，恢复时间与磁盘占用会随历史写次数线性增长。快照把某一时刻的全量状态与**当时的日志位点**一起落盘，之后恢复只需「加载快照 + 重放位点之后的记录」。**关键取舍**：先写 `.tmp`、`Sync`、再 `os.Rename`，保证崩溃后要么是旧快照要么是新快照，绝不留半份；位点与数据必须在同一把 `s.mu` 内取，否则位点算早了漏放、算晚了会重放已包含的写（好在有幂等兜底）。`Compact` 只删被快照完全覆盖且不是当前写指针的旧段。

```go
// 文件：snapshot.go
package kv

import (
	"encoding/gob"
	"fmt"
	"os"
)

// snapshotFile 是快照的磁盘格式：位点 + 全量键值。
// 位点记录生成快照时 WAL 的全局字节偏移，恢复时只重放其后的记录。
type snapshotFile struct {
	Offset int64
	Data   map[string][]byte
}

// Snapshot 生成一致视图并原子落盘：先写 tmp，Sync，再 Rename。
// 全程持有 s.mu，保证位点与数据来自同一时刻、且期间没有新写插入。
func (s *WALStorage) Snapshot(path string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	off := s.wal.Offset()
	snap := snapshotFile{Offset: off, Data: s.table.Snapshot()}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	if err := gob.NewEncoder(f).Encode(snap); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	// Rename 是同一文件系统内的原子操作：要么看到旧快照，要么看到完整新快照。
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	s.metrics.compactions.Add(1)
	return off, nil
}

// LoadSnapshot 读取快照；文件不存在或半份快照都返回错误，
// 由调用方退回「从零重放全部日志」这条安全降级路径。
func LoadSnapshot(path string) (int64, map[string][]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	var snap snapshotFile
	if err := gob.NewDecoder(f).Decode(&snap); err != nil {
		return 0, nil, fmt.Errorf("kv: decode snapshot: %w", err)
	}
	if snap.Data == nil {
		snap.Data = make(map[string][]byte)
	}
	return snap.Offset, snap.Data, nil
}

// Compact 删除位点之前已被快照完全覆盖的旧段，只保留最后一段供继续追加。
func Compact(dir string, safeOffset int64) (int, error) {
	segs, err := listSegments(dir)
	if err != nil {
		return 0, err
	}
	if len(segs) == 0 {
		return 0, nil
	}
	last := segs[len(segs)-1].index
	var covered int64
	removed := 0
	for _, s := range segs {
		if covered+s.size <= safeOffset && s.index < last {
			if err := os.Remove(s.path); err != nil {
				return removed, err
			}
			removed++
		}
		covered += s.size
	}
	return removed, nil
}
```

<Callout type="warn" title="半份快照比没有快照更糟" text="直接 os.Create 正式路径再写 gob，写到一半崩溃会留下一个能打开但解码失败的文件；恢复时若无条件信任它，就会用一个空状态覆盖真实数据。写临时文件加 Rename，并且让 LoadSnapshot 解码失败时退回全量重放，才把这条路径封死。" />

### 6.7 装配：WALStorage 实现同一个 Storage

**设计意图**：把前面所有零件按固定顺序装起来，并对外只暴露 `Apply` 与 `Close`。**关键取舍**：写路径取一把外层 `mu` 把 `Append` 与 `Apply` 串成原子步骤——这把锁是「日志顺序等于状态变更顺序」的唯一保障，因此写吞吐仍受单条 WAL 流限制；读路径刻意绕开它，只取分片读锁，于是读吞吐可随分片数扩展。**恢复顺序**固定为四步：加载快照、从位点重放、裁剪脏尾、新建段，任何一步失败都拒绝启动而不是带病服务。

```go
// 文件：wal_storage.go
package kv

import (
	"errors"
	"sync"
	"time"
)

var _ Storage = (*WALStorage)(nil)

// WALStorage 是第 113 讲的持久化实现：写先经 WAL 追加，再落到分片表。
// 外层 mu 把「Append + Apply」串成原子步骤，保证日志顺序与状态顺序一致；
// 读路径绕开它，只取目标分片的读锁。
type WALStorage struct {
	mu      sync.Mutex
	table   *ShardTable
	wal     *WAL
	metrics *Metrics
	closed  bool
}

type WALOptions struct {
	Dir         string
	SyncPolicy  SyncPolicy
	GroupWindow time.Duration
	SegmentSize int64
	Snapshot    string
}

// OpenWALStorage 的恢复顺序固定为：加载快照 → 从位点重放 → 裁剪脏尾巴 → 开新段。
// 四步缺一不可，尤其第三步决定了重启后能否继续安全追加。
func OpenWALStorage(opts WALOptions) (*WALStorage, error) {
	m := &Metrics{}
	table := NewShardTable()
	from := int64(0)
	if opts.Snapshot != "" {
		if off, data, err := LoadSnapshot(opts.Snapshot); err == nil {
			for k, v := range data {
				table.Set(k, v)
			}
			from = off
		}
	}
	lastGood, err := Replay(opts.Dir, from, m, func(cmd Command) error {
		table.Apply(cmd)
		m.replayRecords.Add(1)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := TruncateTo(opts.Dir, lastGood); err != nil {
		return nil, err
	}
	wal, err := OpenWAL(opts.Dir, opts.SyncPolicy, opts.GroupWindow, opts.SegmentSize, m)
	if err != nil {
		return nil, err
	}
	return &WALStorage{table: table, wal: wal, metrics: m}, nil
}

func (s *WALStorage) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op == OpGet {
		s.metrics.reads.Add(1)
		v, ok := s.table.Get(cmd.Key)
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: v, Found: true}, nil
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, ErrClosed
	}
	// 顺序不可颠倒：日志写成功才改内存，否则已确认的写会在崩溃后丢失。
	if _, err := s.wal.Append(cmd); err != nil {
		if errors.Is(err, ErrNoSpace) {
			s.metrics.diskFull.Add(1)
		}
		if errors.Is(err, ErrPartialWrite) {
			s.metrics.partialWrites.Add(1)
		}
		return Result{}, err
	}
	s.table.Apply(cmd)
	s.metrics.writes.Add(1)
	return Result{Found: true}, nil
}

func (s *WALStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.wal.Close()
}

// Metrics 暴露只读指标视图，供第 9 节的 /metrics 端点抓取。
func (s *WALStorage) Metrics() map[string]int64 {
	return s.metrics.Read()
}
```

<OutputBox title="全套测试与竞态检测的真实输出" output="go test -race ./...\nok  \tkvverify\t2.011s\n\ngo test ./...\nok  \tkvverify\t1.425s\n\ngo vet ./...   （无输出即通过）" note="Go 1.23.8 / darwin-arm64 / Apple M3 Pro 实测；恢复测试断言 150 个 key 全部追回且尾部恰好重放 50 条" />

## 7. 失败路径与边界条件

失败矩阵按「哪个组件挂了 → 现象 → 怎么发现 → 怎么兜底 → 怎么恢复」逐格写，不接受「加强监控」这种空话。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 进程崩溃（kill -9） | 写返回成功后立刻被杀 | 内存态丢失，日志在 | 启动时 `replay_records` 大于 0 | 顺序重放，OpSet/OpDel 幂等 | `OpenWALStorage` 四步自动完成 |
| 部分写 | 写盘时掉电或短写 | 日志尾半条记录 | `partial_writes` 加一；重放停在干净位点 | `Truncate` 回滚 + `ErrPartialWrite` | `TruncateTo` 裁脏尾后重启 |
| 磁盘满 | ENOSPC | `Write` 失败 | `disk_full_total` 突增、写入错误率告警 | 状态码 21，切只读降级，状态机不变 | 清日志或扩容后重启，绝不重试放大 |
| 位翻转 / 扇区撕裂 | 介质老化 | CRC 不符 | `crc_failures` 加一 | 停止重放，不带入脏数据 | 从上一份快照恢复 + 人工核对丢失窗口 |
| 快照写到一半崩溃 | 打快照时掉电 | 半份 gob 文件 | `LoadSnapshot` 解码失败 | 忽略该快照，从零重放全部日志 | 下次快照用 tmp + Rename 重写 |
| 组提交窗口内掉电 | SyncGroup 尚未 fsync | 最后一批写丢失 | 客户端超时 + RPO 告警 | 由配置声明 RPO 不超过 5 ms | 切 `SyncEvery` 可消灭，代价是吞吐 |
| 锁顺序颠倒 | 快照先取分片锁再取写锁 | 与写路径互等、死锁 | block profile 出现长等待 | 统一「先 s.mu 再分片锁」 | 修锁顺序，加死锁检测用例 |
| 慢盘 / fsync 抖动 | 磁盘负载高 | 写 P99 抬升 | 直方图 P99 与 `fsync_total` 背离 | 组提交合并等待，读不受影响 | 限流写入，隔离 WAL 盘 |
| 配置错误 | `GroupWindow` 写成 0 或负数 | 组提交退化为极密 Tick | 启动日志打印归一化后的默认值 | `OpenWAL` 归一化到 5 ms | 配置校验前置到部署流水线 |
| 重复请求 | 客户端超时重试同一 SET | 同一 key 被写两次 | `writes_total` 与业务量不符 | OpSet 幂等，结果一致 | 业务侧加幂等键，日志不额外去重 |
| 日志无限增长 | 从不打快照 | 恢复越来越慢、磁盘涨满 | 段数、`wal_bytes_total` 单调涨 | `Compact` 删被覆盖旧段 | 按段数阈值触发快照 |
| 时钟回拨 | NTP 回拨 | 本讲无影响 | — | 位点用字节偏移，不用时间戳 | 无需处理，设计上已规避 |

**幂等与重试预算**：`OpSet`/`OpDel` 天然幂等，因此重放与客户端重试都不会把状态写坏；但 `ErrNoSpace` 与 `ErrPartialWrite` **禁止自动重试**——前者重试只会放大磁盘压力，后者需要人工确认位点已回滚。重试预算统一为 2 次、指数退避 10 ms 起，仅用于网络层超时（第 112 讲范畴）；本层错误一律直接上抛。**超时传递**：`Apply` 不自带超时，由第 112 讲连接层的 `SetDeadline` 与上层 `context` 负责，避免存储层吞掉取消信号。**降级与人工介入点**：磁盘满切只读、连续 CRC 失败停止重放并保留脏段、快照连续两次失败触发人工巡检——这三处必须留人工开关，不能全自动绕过。

<Callout type="tip" title="at-least-once 与恰好一次的分界" text="重放语义是 at-least-once：一条日志可能被应用多次。系统靠「命令幂等」而不是「去重表」把它收敛成恰好一次的效果，代价是所有写命令都必须设计成可重复执行。第 114 讲 Raft 日志提交后重放，用的还是这一条不变量。" />

## 8. 并发与一致性

**并发模型与 goroutine 生命周期**：本讲有三层锁，各自职责不重叠。`WALStorage.mu` 串行「写日志 + 改内存」，只在写命令上存在；`WAL.mu` 保护文件句柄、位点与 pending 计数，`Append`、`Offset`、`rotateLocked`、`syncLoop` 都取它；`shard.mu` 保护单个桶的 map。唯一长期运行的 goroutine 是 `SyncGroup` 档位下的 `syncLoop`，由 `OpenWAL` 启动、`Close` 中 `close(stop)` 加 `wg.Wait()` 收尾，退出前强制 Sync 一次，保证优雅关闭不丢已返回的写。选 `sync.Mutex` 而不是 channel，是因为这里保护的是「一段必须原子的状态变更」，不是消息传递。

**锁粒度与热点**：32 个分片把无相关 key 的写竞争摊开，但两个不变量限制了收益。其一，WAL 是单一追加流，`WALStorage.mu` 让所有写在日志这一步仍串行，因此**分片只提速读路径与改内存这一步**，不提速 fsync；其二，热点 key 永远落在同一分片，分片数加到 256 也无效，需要业务侧拆 key 或第 114 讲的分片路由。竞态分析用 `go test -race` 覆盖：多 goroutine 混合读写、快照与写并发、组提交与 Close 并发，实测无数据竞争。

**一致性级别选择依据**：单机单进程内提供**单 key 线性一致**——任一读都能看到此前已完成的写。依据是写路径全局串行、读路径取分片读锁，且 `Set` 用值拷贝写入、`Get` 返回值拷贝，不存在「读到写了一半的切片」。不提供跨 key 事务，因为事务要求多分片原子提交，会把这层简单的锁模型整体换成 2PC，收益远不抵成本；这个边界在第 114 讲会进一步放大为「跨分片无事务」。

**顺序保证与乱序处理**：日志顺序即状态顺序，靠 `WALStorage.mu` 保证；`OpSet`/`OpDel` 幂等，因此即使网络层重传或重放乱序，只要最终顺序一致，状态就收敛。第 112 讲单连接串行的帧不会乱序，但第 114 讲多副本必须靠 Raft 日志位序重建顺序——这是下一讲要还的债。

**写放大量化**：一条 value 为 256 B 的 SET，WAL 记录约为 `8（头）+ 7（命令固定部分）+ keyLen + 256` ≈ 280 B，相对逻辑值放大约 1.09 倍；但每个 64 MiB 段打一次快照时，要把 2000 万 key 约 5 GiB 全量重写一遍，摊到约 23 万条写（64 MiB 除以 280 B）上，快照部分再放大约 22 倍。总写放大 WA ≈ 1.09 + 5 GiB / (段大小 × 快照周期系数)，因此**快照不能太勤**：段太小或快照太密都会让磁盘被自己的重写打满。`Compact` 删旧段只回收空间、不产生拷贝，是本讲唯一不增加写放大的动作。降低 WA 的替代方案是 LSM-Tree 的分层合并，代价是读放大与实现复杂度，见第 13 节。

<CodeTabs title="单锁与分片锁的写入对照" :tabs='[{"label":"第 111 讲：整表一把锁","code":"s.mu.Lock()\ns.data[key] = val\ns.mu.Unlock()","note":"任意 key 的写都互斥，16 核只有一个在干活"},{"label":"本讲：按 key 分片","code":"sh := t.of(key)\nsh.mu.Lock()\nsh.data[key] = val\nsh.mu.Unlock()","note":"无相关 key 可并行；同一 key 仍串行，热点需业务侧拆"}]' />

## 9. 可观测性

指标全部用 `sync/atomic` 计数，避免可观测性路径反过来成为锁竞争点；命名与告警阈值直接对应第 7 节失败矩阵。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `writes_total` | Counter | 成功写命令数 | 环比跌 50% 持续 1 分钟 |
| `reads_total` | Counter | 读命令数 | 仅看板 |
| `wal_bytes_total` | Counter | 累计写入 WAL 字节 | 结合磁盘余量设 80% 预警 |
| `fsync_total` | Counter | 实际 fsync 次数 | 组提交档下长期为 0 表示 syncLoop 卡死 |
| `replay_records` | Gauge | 本次启动重放条数 | 超过 100 万提示该打快照了 |
| `compactions` | Counter | 快照次数 | 连续两次失败告警 |
| `partial_writes` | Counter | 短写次数 | 大于 0 立即告警 |
| `disk_full_total` | Counter | ENOSPC 次数 | 大于 0 立即 paging |
| `crc_failures` | Counter | 校验失败记录数 | 大于 0 立即 paging 并保留脏段 |

结构化日志固定字段：`ts`、`level`、`trace_id`、`op`、`key_hash`（不落原始 key，避免敏感信息）、`segment_no`、`offset`、`err_code`、`cost_ms`。`trace_id` 由第 112 讲帧头的请求 ID（第 112 讲动手练一练的扩展点）透传进 `Apply` 上下文，写日志与改内存两条路径打同一个 ID，于是「一次 SET 到底卡在 fsync 还是分片锁」可以一次关联出来。

埋点实现（独立文件，与第 6 节各段同属 `package kv`）：

```go
// 文件：metrics.go
package kv

import "sync/atomic"

// Metrics 用原子计数埋点，避免可观测性路径反过来成为锁竞争点。
// 字段与第 9 节的指标表一一对应，Read 导出一份供 /metrics 抓取的快照。
type Metrics struct {
	writes        atomic.Int64
	reads         atomic.Int64
	walBytes      atomic.Int64
	syncCount     atomic.Int64
	replayRecords atomic.Int64
	compactions   atomic.Int64
	partialWrites atomic.Int64
	diskFull      atomic.Int64
	crcFailures   atomic.Int64
}

func (m *Metrics) Read() map[string]int64 {
	return map[string]int64{
		"writes_total":    m.writes.Load(),
		"reads_total":     m.reads.Load(),
		"wal_bytes_total": m.walBytes.Load(),
		"fsync_total":     m.syncCount.Load(),
		"replay_records":  m.replayRecords.Load(),
		"compactions":     m.compactions.Load(),
		"partial_writes":  m.partialWrites.Load(),
		"disk_full_total": m.diskFull.Load(),
		"crc_failures":    m.crcFailures.Load(),
	}
}
```

**用它定位一次真实故障**：某次线上写成功率从 99.99% 掉到 60%，看板显示 `disk_full_total` 每分钟涨 40、`fsync_total` 同时归零、`writes_total` 断崖、`crc_failures` 不变。四个信号合起来指向「WAL 盘被别的进程写满，Append 直接 ENOSPC，组提交因无新写而不再 Sync」，而不是介质损坏或代码 bug。处置是隔离 WAL 盘、清理旧段后重启，恢复四步自动追回状态。若只盯「写错误率」这一个指标，很容易误判成网络问题去重启网关。

<Term word="RED 方法" text="Rate（请求率）、Errors（错误率）、Duration（耗时）三类指标；本讲的 writes_total、disk_full_total 与写 P99 直方图正好对应这三面" />

## 10. 测试策略

分层测试，mock 边界切在「文件系统」与「网络帧」两处，其余全部用真实实现：

| 层次 | 覆盖内容 | 手段 | 为什么这样切 |
| --- | --- | --- | --- |
| 单测 / 表驱动 | 记录编解码、错误码往返、分片分布 | `TestRecordRoundTrip`、`TestFrameCodeRoundTrip` | 纯函数，不需要任何外部依赖 |
| 集成 | 快照 + 尾部日志 + 重启恢复 | `TestRecoverAcrossSnapshotAndRestart` | 用 `t.TempDir` 的真实文件，验证四步闭环 |
| 故障注入 | 半条记录、位翻转 | `TestReplayStopsAtTornTail`、`TestDecodeRecordRejectsBitFlip` | 手工构造字节流，比 mock 更贴近真实损坏 |
| 契约 | `Storage` 编译期断言 | `var _ Storage = (*WALStorage)(nil)` | 接口一改立刻编译失败 |
| 并发 | 混合读写、快照与写并发 | `go test -race` | 锁顺序与值拷贝靠 race detector 兜底 |
| 基准 / 压测 | 三档 fsync、纯内存读基线 | `BenchmarkWALSetGroup/Every`、`BenchmarkShardTableGet` | `benchstat` 对比，量化 fsync 代价 |
| 混沌 | kill -9、磁盘满、慢盘 | 外部脚本 + 故障注入 | 验证 RPO 声明与恢复四步 |

可运行的测试与基准（含一处断言：重启后恰好重放快照之后的 50 条，用来证明位点没有算早或算晚）：

```go
// 文件：wal_test.go
package kv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordRoundTrip(t *testing.T) {
	cases := []Command{
		{Op: OpSet, Key: "user:1", Value: []byte("benji")},
		{Op: OpSet, Key: "k", Value: []byte("")},
		{Op: OpDel, Key: "gone"},
		{Op: OpSet, Key: "bin", Value: []byte{0, 1, 2, 255}},
	}
	for _, want := range cases {
		rec, err := encodeRecord(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeRecord(rec)
		if err != nil {
			t.Fatal(err)
		}
		if got.Op != want.Op || got.Key != want.Key || string(got.Value) != string(want.Value) {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
}

func TestDecodeRecordRejectsBitFlip(t *testing.T) {
	rec, err := encodeRecord(Command{Op: OpSet, Key: "k", Value: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	rec[len(rec)-1] ^= 0xFF
	if _, err := decodeRecord(rec); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestFrameCodeRoundTrip(t *testing.T) {
	errs := []error{ErrNotFound, ErrEmptyKey, ErrBadOp, ErrClosed, ErrCorrupt, ErrNoSpace, ErrPartialWrite}
	for _, want := range errs {
		got := errOf(codeOf(want))
		if !errors.Is(got, want) {
			t.Fatalf("code=%d got=%v want=%v", codeOf(want), got, want)
		}
	}
}

func TestReplayStopsAtTornTail(t *testing.T) {
	dir := t.TempDir()
	f, err := os.OpenFile(filepath.Join(dir, "wal-000000.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		rec, _ := encodeRecord(Command{Op: OpSet, Key: fmt.Sprintf("k%d", i), Value: []byte("v")})
		if _, err := f.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	// 追加 5 字节，模拟崩溃时只写出半条记录头。
	if _, err := f.Write([]byte{0, 0, 0, 20, 0}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var applied []string
	off, err := Replay(dir, 0, &Metrics{}, func(cmd Command) error {
		applied = append(applied, cmd.Key)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 3 {
		t.Fatalf("want 3 applied, got %d (%v)", len(applied), applied)
	}
	if off <= 0 {
		t.Fatalf("want positive clean offset, got %d", off)
	}
}

func TestRecoverAcrossSnapshotAndRestart(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snapshot.gob")
	opts := WALOptions{Dir: dir, SyncPolicy: SyncEvery, SegmentSize: 1 << 20, Snapshot: snap}
	s, err := OpenWALStorage(opts)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%03d", i), Value: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Snapshot(snap); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 150; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%03d", i), Value: []byte("v")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenWALStorage(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	for i := 0; i < 150; i++ {
		got, err := s2.Apply(Command{Op: OpGet, Key: fmt.Sprintf("k%03d", i)})
		if err != nil || !got.Found {
			t.Fatalf("key k%03d: err=%v found=%v", i, err, got.Found)
		}
	}
	if s2.Metrics()["replay_records"] != 50 {
		t.Fatalf("want 50 replayed records, got %d", s2.Metrics()["replay_records"])
	}
}

func TestShardDistribution(t *testing.T) {
	counts := make([]int, shardCount)
	for i := 0; i < 10000; i++ {
		counts[shardOf(fmt.Sprintf("key-%d", i))]++
	}
	for i, c := range counts {
		if c == 0 {
			t.Fatalf("shard %d got 0 keys", i)
		}
	}
}

func BenchmarkWALSetGroup(b *testing.B) {
	opts := WALOptions{Dir: b.TempDir(), SyncPolicy: SyncGroup, GroupWindow: 5 * time.Millisecond}
	s, err := OpenWALStorage(opts)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWALSetEvery(b *testing.B) {
	opts := WALOptions{Dir: b.TempDir(), SyncPolicy: SyncEvery}
	s, err := OpenWALStorage(opts)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShardTableGet(b *testing.B) {
	table := NewShardTable()
	for i := 0; i < 1024; i++ {
		table.Set(fmt.Sprintf("k%d", i), []byte("v"))
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		table.Get(fmt.Sprintf("k%d", i%1024))
	}
}
```

mock 边界之所以切在文件系统与网络帧，是因为中间那些纯逻辑（编码、分片、位点计算）用真实实现测才有意义；用 `net.Pipe` 或 `t.TempDir` 已经足够快，引入 gomock 反而会让「半条记录」这类故障注入变成对 mock 行为的断言，测不到真实字节布局。

## 11. 部署与容量

**部署拓扑**：单进程单实例，本地两块盘分离——WAL 与快照放 NVMe 系统盘之外的独立 200 GiB 盘，避免业务日志把 WAL 盘写满；第 112 讲的 TCP 监听仍在同进程内，`WALStorage` 作为 `Server` 的 `Storage` 注入。依赖只有 Go 1.21+ 标准库，无外部中间件；文件系统需支持原子 rename 与 ftruncate（ext4、xfs、APFS 均可）。

**配置项表**

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `dir` | `/var/lib/kv/wal` | `t.TempDir()` | 独立 NVMe 挂载点 | 段文件与快照目录 |
| `sync_policy` | `group` | `none` | `group` 或 `every` | 决定 RPO 与吞吐 |
| `group_window_ms` | 5 | 20 | 5 | 组提交窗口；为 0 时归一化到 5 |
| `segment_size_mib` | 64 | 1（便于测轮转） | 64 | 段轮转阈值 |
| `snapshot` | 空 | `snapshot.gob` | 独立盘路径 | 为空则不做快照恢复 |
| `shard_count` | 32 | 32 | 32 | 编译期常量，改动需重编译 |
| `max_record_mib` | 16 | 16 | 16 | 单记录上限，防伪造长度 |
| `snapshot_trigger_segments` | 64 | 2 | 64 | 累计段数达阈值触发快照 |
| `read_deadline_ms` | 5000 | 5000 | 3000 | 承接第 112 讲慢客户端超时 |
| `graceful_timeout_s` | 30 | 5 | 30 | SIGTERM 到强制退出的窗口 |

**资源估算**：内存约等于逻辑数据量 5 GiB 加 map 与切片开销，按 1.6 倍预留约 8 GiB，32 GiB 机器有余量；磁盘按「WAL 日增量 = 5 亿次 × 280 B ≈ 140 GiB/日」估算，必须靠 64 MiB 段轮转 + 每 64 段一次快照 + `Compact` 回收，稳定占用约 4 GiB 段文件 + 一份 5 GiB 快照 + 保留 1 份旧快照共约 14 GiB；文件描述符常驻 1 个活跃段 + 恢复期 N 个段，上限设为 1024；带宽因是本地盘不走网络，端到端压测的网卡瓶颈在第 112 讲帧往返而非本层。

**扩容方式与优雅退出**：单机垂直扩容上限明确——内存或磁盘到了就停，不靠加分片数硬撑（分片只降锁竞争，不增容量）；水平扩容是第 114 讲的事。优雅退出顺序：收到 SIGTERM → 第 112 讲 `Server.Close` 停止 accept → 等待在途请求收敛（`graceful_timeout_s`）→ `WALStorage.Close` 取写锁 → `WAL.Close` 先 `close(stop)` 让 syncLoop 收尾 Sync → `wg.Wait()` → 文件 Sync → 关闭 fd。任何一步超时则记日志并强退，靠四步恢复链路兜底。

## 12. 性能与压测

**压测方法**：微基准用 `go test` 内置 Benchmark，端到端用第 112 讲的客户端以 128 并发、Zipf 分布造热点 key、value 256 B，跑 3 分钟取稳态。

```bash
go test -run '^$' -bench 'BenchmarkWALSetGroup|BenchmarkWALSetEvery|BenchmarkShardTableGet' -benchmem -benchtime=3s ./...
go test -race ./...
go test -cpuprofile=cpu.prof -bench BenchmarkWALSetGroup -benchtime=10s ./...
go tool pprof -http=:8080 cpu.prof
```

**实测指标表**（Go 1.23.8 / darwin-arm64 / Apple M3 Pro / APFS，**单 goroutine 串行、纯 `Storage.Apply` 微基准**，不含第 112 讲网络帧往返；APFS 的 fsync 明显慢于 Linux NVMe，数字仅作相对对照）

| 场景 | ns/op | 折算 ops/s | B/op | allocs/op | 说明 |
| --- | --- | --- | --- | --- | --- |
| `SyncGroup` 组提交写 | 8981 | 约 11.1 万 | 56 | 4 | fsync 被摊薄到每 5 ms 一次 |
| `SyncEvery` 每条 fsync | 4257981 | 约 235 | 200 | 5 | 退化为磁盘 fsync 速率，慢约 474 倍 |
| 纯分片表读 | 98.18 | 约 1018 万 | 22 | 2 | 读路径不取写锁，无 WAL 开销 |

**瓶颈定位过程**：`SyncEvery` 的 4.26 ms/op 几乎全部落在 `File.Sync`，pprof 的 CPU profile 显示 `syscall` 与调度等待占主导而非用户态编码；`go tool pprof` 的 block profile 进一步显示 `WALStorage.mu` 的等待随并发上升——说明两处瓶颈：fsync 次数与单一写串行锁。前者用组提交解决（8981 ns/op，fsync 次数从每条一次降到每窗口一次）；后者只能靠第 114 讲把 WAL 按分片拆成多条流，本讲范围内无解。

**优化前后对比表**

| 维度 | 第 111 讲内存版 | 第 112 讲网络版 | 本讲组提交版 | 本讲每条 fsync 版 |
| --- | --- | --- | --- | --- |
| 重启后数据 | 全丢 | 全丢 | 可恢复 | 可恢复 |
| 写锁粒度 | 整表一把 | 整表一把 | 32 分片 + 写串行 | 同左 |
| 相对写延迟 | 1 倍（基线） | 加一次帧往返 | 加一次 write | 加一次 fsync |
| RPO | 无穷 | 无穷 | 不超过 5 ms | 近似 0 |
| 错误可判定 | 是 | 否（文本） | 是（状态码） | 是 |

**仍然存在的限制**：写吞吐被单条 WAL 流的 `WALStorage.mu` 卡住，多核无法线性扩展写；热点 key 不分片；值越大快照写放大越重；`SyncGroup` 的 RPO 窗口在掉电时是真实的数据丢失，需要业务确认可接受；本机 APFS 数字不能直接外推到生产 Linux NVMe，上线前必须用目标机型重跑。

## 13. 演进与替代方案

**替代架构取舍表**（四个方案按规模排，重点写清「何时该换」）

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 纯内存 + 定时全量快照 | 百万 key 以下、可丢数据 | 实现最简、读最快 | 快照间隔内全丢，快照 O(n) 阻塞 | 数据完全可重建、且写量极小 |
| WAL 追加 + 快照（本讲） | 千万 key、单实例 5 GiB 级 | 写顺序追加、恢复快、可精确控制 RPO | 快照全量重写带来写放大，写受单流限制 | 当前规模的最佳平衡点 |
| LSM-Tree（RocksDB 类） | 亿级 key、TB 级、写密集 | 顺序写、分层合并把写放大压到个位数、天然压缩 | 读放大、实现与调优复杂、依赖第三方库 | 数据量超过单盘或写放大不可接受 |
| mmap B+ 树（LMDB 类） | 读极多、写少、需范围扫描 | 读近乎零拷贝、支持事务与范围查询 | 写要 COW 整页、并发写受限、mmap 崩溃一致性难 | 需要二级索引或跨 key 事务时 |

第 1 个替代方案是「不做 WAL」的退化版，第 3、4 个是「换存储引擎」的重量级方案，都不是本讲该做的——评审时把它们列出来，是为了说明**当前选择不是因为简单，而是因为在 5 GiB、6 万 QPS 这个约束下综合成本最低**。尤其 LSM-Tree：它把写放大从约 20 倍压到个位数，但引入读放大与 compaction 抖动，对「读多写少、单 key 点查」的负载并不划算。

<FlowTimeline direction="horizontal" :steps='[{"title":"当前：单机 WAL","desc":"容量与可用性顶死在一台机器","tone":"blue"},{"title":"触发条件一","desc":"单盘容量或写吞吐触顶","tone":"green"},{"title":"触发条件二","desc":"要求机器宕机不停服","tone":"orange"},{"title":"下一版：分片加副本","desc":"Router 加 Raft 组，见第 114 讲","tone":"red"}]' />

**演进到下一版的触发条件与迁移步骤（含回滚）**：触发条件是「单盘容量超过 70%」「写 P99 持续超过 2 ms 且组提交已开到最小窗口」「要求机器宕机 RTO 小于 10 秒且不能接受单机 SLA」三者之一。迁移步骤：① 先以第 113 讲的 `WALStorage` 作为每个副本组内的本地状态机，不改动契约；② 引入 `Router` 做按 key 分片，双写旧单机与新集群一个快照周期；③ 用 `replay_records` 与 key 抽样校验两侧一致；④ 切读流量到集群，观察一个快照周期无 `crc_failures`；⑤ 下线旧单机。回滚：任何一步不一致，把读流量切回单机，丢弃集群侧数据重来——因为单机侧始终是权威写入方，回滚不丢数据。这正是第 114 讲开头要接住的场景。

## 14. 真实工程里的坑

**坑 1：把 Flush 当成 fsync。** 现象：进程 `Close` 正常、日志也「刷过」，机器一掉电最后几条写还是没了。原因：`bufio.Writer.Flush` 只把用户态缓冲交给内核页缓存，`File.Sync` 才真正等磁盘落盘。修正：去掉用户态缓冲，直接 `os.File.Write` 并按 `SyncPolicy` 显式 `Sync`，把「Flush 了却没持久化」这条路堵死。

<Compare title="刷盘语义对照" :panels='[{"label":"错误写法","code":"w.Write(rec)\nw.Flush()   // 只到页缓存","note":"掉电丢最后一批，还误以为已经持久化"},{"label":"正确写法","code":"f.Write(rec)\nif policy == SyncEvery { f.Sync() }","note":"Sync 明确落在策略档位里，RPO 可度量"}]' />

**坑 2：崩溃恢复后不裁脏尾巴。** 现象：第一次重启正常，之后每次重启都只能恢复到同一个位点，日志再也不增长。原因：半条记录留在段尾，`O_APPEND` 的新写追加在它后面，`Replay` 每次都在同一处停。修正：`Replay` 返回干净位点后必须调用 `TruncateTo`，把磁盘裁到该位点再 `OpenWAL` 新建段。提前发现：`replay_records` 连续多次启动都相同且偏小，就要怀疑脏尾。

**坑 3：快照直接写正式文件。** 现象：打快照时掉电，恢复后数据大面积缺失。原因：`os.Create(path)` 立刻截断旧快照，写到一半崩溃留下半份 gob，恢复时却无条件信任它。修正：写 `path + ".tmp"`、`Sync`、`Close`、`os.Rename` 原子替换；`LoadSnapshot` 解码失败时退回全量重放，绝不把半份快照当成权威状态。

**坑 4：位点与数据不在同一临界区。** 现象：恢复后偶发丢最后几条写，或重放条数比预期多。原因：先读 `Offset()` 再逐分片拷贝数据，中间插入了新写，位点算早了漏放。修正：`Snapshot` 全程持有 `s.mu`，位点与 `table.Snapshot()` 在同一把锁内取；本讲测试用「恰好重放 50 条」把这条钉死。

**坑 5：锁顺序不一致导致死锁。** 现象：定时快照触发时整个实例卡死。原因：快照路径先取分片读锁再取 `s.mu`，写路径相反。修正：全局统一「先 `s.mu`、再分片锁」，任何新代码都不得反向；用 `go test -race` 加 block profile 定期巡检等待链。

<Compare title="快照锁顺序对照" :panels='[{"label":"错误：先分片后写锁","code":"sh.mu.RLock()\ns.mu.Lock()","note":"与写路径互等，死锁只在快照时复现"},{"label":"正确：先写锁后分片","code":"s.mu.Lock()\nsh.mu.RLock()","note":"与写路径同序，不可能形成环形等待"}]' />

**坑 6：忽略 Write 的短写返回值。** 现象：偶发一条日志读不出来，之后所有恢复都停在同一处。原因：只检查 `err == nil` 就认为写完整了。修正：显式判断 `n != len(rec)`，用 `Truncate(w.offset)` 回滚并返回 `ErrPartialWrite`。提前发现：`partial_writes` 指标大于 0 立即告警。

**坑 7：CRC 失败后跳过继续读。** 现象：恢复出的状态「像是少了中间一段」，且没有报错。原因：把校验失败当成「跳过这条」继续解析，后面的记录被错位解释。修正：`decodeRecord` 返回 `ErrCorrupt` 时立即停止重放并返回干净位点，脏段保留待人工核对。本讲 `Replay` 的 `clean` 布尔量就是干这个的。

**坑 8：用 `time.Now()` 做日志序号。** 现象：NTP 回拨后恢复顺序错乱，新写被旧写覆盖。原因：墙上时钟不单调。修正：位点一律用字节偏移，任期与序号这类逻辑时钟留给第 114 讲 Raft。本讲从设计上就用字节位点规避了整类问题，但评审时要明确写出来，免得后来有人「顺手」加个时间戳排序。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-113-kv-3-async`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-113-kv-3-async)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给组提交加批量阈值，把 RPO 从固定窗口变成「窗口或批量先到」" task="当前 syncLoop 只按 time.Ticker 触发 fsync，低流量时一条写也要等满 5 ms，高流量时又可能攒太多写才落盘。请改造 syncLoop：让 pending 达到阈值时立即触发一次 Sync，同时保留窗口触发，并保证 Close 仍能收尾。" :hints='["给 WAL 增加 batchSize 字段，Append 里 pending 累加后判断是否达到阈值","可以在 Append 里直接判断并调用一次内部 syncLocked，也可以用一个带缓冲的信号 channel 通知 syncLoop","注意 Sync 必须在持 w.mu 的状态下进行，且不要与 ticker 分支重复计数","补一个测试：batchSize 设为 1 时，写过一条后 fsync_total 应当立刻增加"]' solution='给 WAL 增加 batchSize int 字段，OpenWAL 归一化默认值（例如 256）。在 Append 把 pending 累加后追加判断：若 policy 为 SyncGroup 且 pending 达到 batchSize，则就地调用 w.f.Sync() 并把 pending 归零、syncCount 加一。syncLoop 的 ticker 分支保持不变，用于处理未达阈值但窗口已到的尾巴；stop 分支保持不变用于收尾。注意两处 Sync 都要在 w.mu 保护下执行，避免与 Close 竞争。测试用 SyncGroup 且 batchSize 设为 1 打开实例，写一条 SET 后读取 Metrics() 的 fsync_total，断言大于 0。' explain="这道题的价值在于戳破「组提交等于固定窗口」的误解：真正可控的组提交是「批量或时间先到者触发」，批量约束了单批最大丢失量，时间约束了低流量下的最大等待延迟，两者合起来才把 RPO 与 P99 同时写进 SLA。" />

## 随堂检测

<Quiz no="113-1" q="某业务要求机器掉电后已确认的写一条都不丢，同时写 QPS 只有 2000。应该选哪个档位，主要代价是什么？" :options='["SyncNone，代价是没有持久性","SyncGroup，代价是丢 5 ms","SyncEvery，代价是写吞吐被磁盘 fsync 速率封顶","随便选，WAL 已经保证不丢"]' answer="2" explain="只有 SyncEvery 在 Append 返回前完成 File.Sync，才把 RPO 压到近似 0。代价是每条写都等一次磁盘 fsync，实测单 goroutine 约 235 ops/s，吞吐被磁盘封顶；本题 QPS 只有 2000，仍可能踩到这个上限，需要压测确认目标机型能扛住。SyncGroup 明确接受丢一个窗口，不满足需求。" />

<Predict no="113-2" q="日志末尾有 3 条完整记录，随后是 5 字节的半条记录头，Replay 的结果是什么？" :options='["报错并拒绝启动，要求人工清理","跳过半条记录继续读后面的内容","重放 3 条后返回干净位点并正常结束","把半条按 0 补齐后应用"]' answer="2" output="重放 3 条后返回干净位点并正常结束" explain="io.ReadFull 读不满 8 字节头时返回 ErrUnexpectedEOF，replayOne 把 clean 置为 false 并返回当前 pos；Replay 收到后停止遍历，返回该干净位点和 nil。随后 OpenWALStorage 用 TruncateTo 把这 5 字节裁掉，再 OpenWAL 新建段。报错、跳过或补零都会破坏「只读已完整记录」这条不变量。" />

<FillBlank no="113-3" tpl="WAL 记录用 ___ 字段挡半条记录、用 ___ 挡位翻转；崩溃恢复四步是加载快照、按位点重放、___、新建段；组提交的 RPO 由 ___ 决定。" :answers='["length~长度","CRC32C~CRC~校验和","裁剪脏尾巴~TruncateTo","组提交窗口~窗口~group_window_ms"]' hint="前两空来自记录格式，第三空来自恢复链路，第四空来自 SyncPolicy" explain="长度与 CRC32C 各司其职；TruncateTo 是「重放」之后、「开新段」之前不可省的一步；SyncGroup 的 RPO 上限就是配置里的组提交窗口，写进 SLA 的就是这个数字。" :explains='["记录头写入自身字节长度，恢复时按它读满一条；崩溃残留的半条记录因长度不符被识破并停在这里，不会把残缺字节当成合法数据。","长度只防截断，挡不住位翻转这类静默损坏；给每条附一份校验码，重放时重算比对，不符即判损坏，把悄悄变坏的数据挡在恢复之外。","按位点重放到末尾往往撞上没写完的脏尾巴，直接开新段会让后续记录接在残字节后；必须先按最后合法位点截断，再建新段接着写。","开启组提交后，写入先在内存攒够一个时间窗才落盘，宕机最多丢窗口内未刷盘的数据，所以窗口时长就是可承诺的 RPO 上限。"]' />

## 一页纸速记

<KnowledgeCard :points='["写顺序固定为：追加 WAL 并按策略 fsync，再改内存，保证日志顺序等于状态顺序","记录用长度加 CRC32C 双保险，重放遇半条或校验失败返回干净位点并停止","恢复四步闭环：加载快照、按位点重放、TruncateTo 裁脏尾、OpenWAL 新建段","锁分片让读绕开写串行锁，但单条 WAL 流仍限制写吞吐，这是下一讲的动因","fsync 三档把 RPO 与吞吐的取舍写成可配置项，而不是藏在代码里"]' :pitfalls='["把 Flush 当成 fsync，掉电后仍丢最后几条写","恢复后不裁脏尾巴，导致每次重启卡在同一位点","快照直接写正式文件，半份快照覆盖真实数据","位点与数据不在同一把锁内取，恢复漏放或越界","快照与写路径锁顺序相反，触发死锁"]' summary="持久性靠「先记日志再生效 + 幂等重放 + 裁脏尾」，并发靠把一把锁拆成 32 把，代价全部明码标价写进 RPO、写放大与 fsync 档位。" />

## 小结与下一讲

1. 用「长度 + CRC32C + 编码命令」实现 WAL 追加写，把 fsync 拆成 `SyncEvery`/`SyncGroup`/`SyncNone` 三档并各自声明 RPO，磁盘满与部分写都有可判定错误与回滚动作。
2. 崩溃恢复做成四步闭环：加载快照取位点、幂等重放尾部、`TruncateTo` 裁脏尾、`OpenWAL` 新建段；快照用临时文件加 `Rename` 原子替换，位点与数据在同一把锁内取。
3. 32 把分片锁让读绕开写串行锁，并量化了单条 WAL 流的写放大与吞吐上限；顺手把第 112 讲退化的错误文本改成帧状态码，让 `errors.Is` 跨网络重新成立。

但这一版再稳也只有一台机器：机器宕机服务就停，容量顶死在单机内存与磁盘，写吞吐被单条 WAL 流的写串行锁卡住。下一讲把 KV 演进成分布式存储，引入分片路由、多副本与 Raft 共识，`Apply(Command)` 依然一个字不改。

[下一讲：KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed)
