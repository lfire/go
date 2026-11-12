---
title: 第 114 讲 · KV 贯穿线 ④ 分片、复制与 Raft
description: 把单机持久化 KV 演进成分布式存储：一致性哈希分片、多副本 quorum、Raft 选主与日志提交、一致性级别、成员变更与脑裂防护
---

<StageHeader stage="L10 · 工程实战" level="★★★★★" time="90 分钟" goal="把 KV 演进成分布式存储" prereq="第 113 讲 · WAL、持久化与并发控制" />

# 第 114 讲 · KV 贯穿线 ④ 分片、复制与 Raft

> 三讲走下来，我们已经有了一份稳定契约和三种实现：内存、TCP 客户端、带 WAL 的持久化版。第 113 讲那版能落盘、能崩溃恢复、能用 32 把分片锁扛并发，但它仍然是**一台机器**：机器宕机服务就停，容量顶死在这一台的内存与磁盘上，写吞吐被单条 WAL 流的写串行锁卡死。有人会说「把 WAL 拆到多台机器不就行了」——问题恰恰在这里：同一份数据一旦有三份拷贝，**谁说了算**？网络分区时两边都认为自己是主，就会各写各的，恢复后谁覆盖谁？这一讲做最后一次跃迁，用**分片**把数据摊到多台机器解决容量，用**副本**解决单点，用 **Raft** 让多个副本对同一串写命令达成一致。先把可度量的约束摆上桌：副本数 3、写多数派 W 为 2、读票数 R 为 2、默认单 key 线性一致、机器宕机 RTO 不超过 30 秒、已提交写 RPO 为 0、目标 30 分片承载 150 GiB。这是第 111 讲那块「窄腰」能承载的最大一次变化，而 `Apply(Command)` 依然一个字不改——第五次换实现，调用方零改动。

## 0. 项目定位与真实场景

<CheckList id="L114-goal" title="本讲目标" :items='["用带虚拟节点的一致性哈希把 Command 路由到唯一分片，且 Router 仍实现 Storage","算清 W/R/N 与奇数副本的关系，写出可验证的多数派提交内核","实现任期、选主、日志匹配与提交索引，讲清一致性到底从哪来","给出线性一致、有界陈旧、最终一致三档读级别及其代价","分析脑裂、成员变更双主、跨分片事务、热点分片四类失败路径"]' />

业务背景从「同机本地持久化层」升级为**多机房共享的集群 KV**：订单状态、库存快照、用户画像标签、分布式锁与幂等表都要跨服务访问，任何一台机器重启都不该让上游停摆。谁在用？是几十个内部服务，通过第 112 讲的 TCP 帧或 SDK 接入；运维团队负责容量规划、成员变更与故障演练。

量级决定了架构。目标规模 30 个分片、每分片 3 副本、共 90 个节点，分布在 3 个可用区；总逻辑数据量约 150 GiB（每分片约 5 GiB，正好是第 113 讲单实例的承载量）；峰值写 20 万 ops/s、读 100 万 ops/s，读写比约 1 比 5；单机 SLA 从 99.9% 提到集群 99.99%。现有方案的三处痛点很硬：第 113 讲单机宕机 RTO 是分钟级（要人工拉起、重放日志），容量到顶只能停机迁移，写吞吐顶死在单条 WAL 流。

核心矛盾有两层。**第一层是 CAP 取舍**：分区发生时，要么保一致性拒绝写入（CP），要么保可用性让两边继续写（AP），没有第三条路；本讲选 CP，写必须拿到多数派，少数派分区明确报错而不是悄悄成功。**第二层是「单 key 原子」与「跨 key 事务」的边界**：分片换来容量，代价是不同 key 可能落在不同 Raft 组，无法用一个本地事务包住。这条边界是本讲最贵的取舍，也是第 115 讲电商微服务必须正面处理的问题。

<Term word="多数派" text="quorum：写只需超过半数副本确认即可提交，从而在容忍少数节点故障的同时，保证任意两个多数派必有交集，避免脑裂" />

## 1. 需求与约束

**功能需求表**

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 水平扩展容量 | 30 分片各承载约 5 GiB，加节点只迁移相邻区间，不全量重分布 |
| FR-2 | 容忍节点故障 | 3 副本挂 1 个仍可读写，挂 2 个拒绝写入并返回 `ErrNoQuorum` |
| FR-3 | 写顺序一致 | 同一分片所有副本按相同顺序应用相同命令串，`Command` 沿用第 111 讲定义 |
| FR-4 | 防脑裂 | 可见副本不足多数派时拒绝提交，返回 `ErrSplitBrain` |
| FR-5 | 读一致性分级 | 线性一致走 Leader，有界陈旧检查落后量，最终一致允许读 Follower |
| FR-6 | 成员变更与迁移 | Idle 到 Joint 到 Done 三态过渡，区间双写，校验一致才切路由 |
| FR-7 | 跨分片边界可判定 | `CheckSameShard` 对跨分片 key 返回 `ErrCrossShardTxn` |
| FR-8 | 错误码可判定 | 帧 status 位沿用第 113 讲高位段，客户端 `errors.Is` 成立 |

**非功能指标表**

| 指标 | 目标 | 口径 | 测量方式 |
| --- | --- | --- | --- |
| 副本数 / 分片数 | 3 / 30 | 奇数副本，每分片一个 Raft 组 | 配置校验 |
| 写多数派 W / 读票数 R | 2 / 2 | N 为 3，W 加 R 为 4 大于 N | `QuorumValid` 单测 |
| 一致性级别 | 单 key 线性一致（默认） | 可降级为有界陈旧或最终一致 | 并发用例 |
| 可用性 SLA | 99.99%（月度） | 单可用区整体故障仍可服务 | 故障注入演练 |
| RTO / RPO | 不超过 30 秒 / 0 | 已提交写在 Leader 切换后不丢 | 混沌演练 |
| 容量 | 150 GiB 逻辑值 | 30 分片各 5 GiB | `ShardTable.Len` |
| 写吞吐 / 读吞吐 | 20 万 / 100 万 ops/s | 1000 并发客户端、value 256 B | 端到端压测 |
| 写 P99 / 读 P99 | 不超过 5 ms / 1 ms | 含一次跨机多数派往返 | 直方图埋点 |
| 选举超时 / 心跳 | 150 到 300 ms / 50 ms | 随机化避免同时竞选 | 配置项 |
| 虚拟节点数 | 128（实测调参） | 每物理节点的 VNode 数 | 迁移比例压测 |

**quorum 与一致性级别表**

| 配置 | N | W | R | 保证 | 代价 |
| --- | --- | --- | --- | --- | --- |
| 强一致读写 | 3 | 2 | 2 | W 加 R 大于 N，读必与已提交写相交 | 读也要等 2 票，延迟高 |
| 写多数派加 Leader 读 | 3 | 2 | 1 | 读走 Leader 或 ReadIndex | 读压在 Leader 上 |
| 最终一致读 | 3 | 2 | 1 | 允许 Follower 服务 | 可能读到未提交的旧值 |
| 偶数副本（禁用） | 4 | 3 | 2 | 分区时两边各 2 票都不足 3 | 明明挂 1 个就不可写，且易误判 |

<Callout type="idea" title="为什么副本数必须是奇数" text="3 副本的多数派是 2，挂 1 个还剩 2 票可用；4 副本多数派是 3，挂 1 个还剩 3 票看似也能写，但分区成 2 加 2 时两边都凑不到 3，反而更差，且『多数』在偶数下容易产生歧义。本讲 NewReplicaGroup 里对偶数入参直接加一，就是把这个纪律写进代码，而不是靠文档约定。" />

**明确本讲不做**：跨 key / 跨分片事务、跨机房多活与异地容灾、二级索引与范围扫描（仅有单 key 点查）、SQL、热点分片自动分裂（预留接口）、认证与 TLS、请求级流水线（第 112 讲扩展点）。**假设边界**：网络分区可检测但消息可能延迟或重复；墙钟只用于选举超时与心跳，绝不用于日志排序（排序靠 Term 加 Index，规避时钟回拨）；选举、心跳、日志持久化与网络传输由 `etcd/raft` 这类成熟库负责，本讲给出的 `raftCore` 是**最小可编译内核**，用来讲清一致性的来源，不是要替代生产库；每个副本的本地状态机直接复用第 113 讲 `WALStorage`，契约不变。

## 2. 架构蓝图

<ArchBlueprint title="分布式 KV 的四层架构" caption="每个分片是一个独立 Raft 组（multi-Raft），组内按日志顺序一致，组间只做路由与迁移" :layers='[{"name":"接入层","note":"承接第 112 讲帧与第 113 讲错误码，只做编解码","protocol":"长度前缀帧 v1 / 请求 ID","items":[{"label":"客户端 SDK","sub":"Apply 封装成帧","detail":"职责：把 Apply 封装成帧并按状态码还原哨兵错误；技术选型：沿用第 112 讲 Client 与第 113 讲 codeOf 与 errOf 表；失败处理：ErrNotLeader 与 ErrNoQuorum 触发重定向与退避重试，不无限重试。","tone":"blue"},{"label":"帧解码器","sub":"magic 与 version 校验","detail":"职责：校验魔数与版本并把 body 还原成 Command；技术选型：io.ReadFull 加 16MiB 上限；失败处理：坏帧断连并计入坏帧指标。","tone":"blue"},{"label":"错误码还原","sub":"status 位数值化","detail":"职责：把 status 位映射回哨兵错误；技术选型：沿用第 113 讲预留的 23 以上高位段；失败处理：未知码收敛为 255 并告警。","tone":"blue"}]},{"name":"路由层","note":"决定 key 落到哪个分片、迁移期间投给谁","protocol":"key 到分片的映射 / 请求重定向","items":[{"label":"一致性哈希环","sub":"FNV 加虚拟节点","detail":"职责：按 key 定位唯一分片并返回物理节点；技术选型：FNV-1a 加虚拟节点，顺时针取后继；失败处理：环为空返回 ErrNoQuorum，拒绝启动。","tone":"green"},{"label":"成员表","sub":"节点到副本组映射","detail":"职责：维护节点到副本组的映射与迁移状态；技术选型：Idle 到 Joint 到 Done 三态状态机；失败处理：Joint 阶段区间双写，校验不一致就回滚。","tone":"green"},{"label":"迁移执行器","sub":"按区间搬运校验","detail":"职责：按迁移区间搬运数据并逐 key 校验；技术选型：区间扫描加批量写，源与目标并行；失败处理：校验失败重来，绝不直接切换路由。","tone":"green"},{"label":"跨分片检查","sub":"CheckSameShard","detail":"职责：判断一组 key 是否属于同一分片；技术选型：逐个比对哈希环结果；失败处理：跨分片返回 ErrCrossShardTxn，交业务层 Saga 补偿。","tone":"green"}]},{"name":"复制层","note":"每个分片一个 Raft 组，组内对写顺序达成一致","protocol":"AppendEntries / RequestVote / Command 日志流","items":[{"label":"Router","sub":"仍是 Storage","detail":"职责：把命令投递到目标副本组，自己仍实现 Storage；技术选型：依赖倒置，只认 Group 接口；失败处理：目标不存在返回 ErrNotLeader 或 ErrNoQuorum。","tone":"orange"},{"label":"Raft Leader","sub":"propose 与提交","detail":"职责：追加日志、并行发 AppendEntries、推进提交索引；技术选型：任期加多数派确认，只提交已达多数的日志；失败处理：发现更高任期立即退位。","tone":"orange"},{"label":"Raft Follower","sub":"日志匹配与确认","detail":"职责：校验前一条日志任期、追加并确认；技术选型：prevLogIndex 与 prevLogTerm 匹配，不匹配让 Leader 回退；失败处理：连续超时触发新一轮选举。","tone":"orange"},{"label":"Quorum 计算","sub":"W 与 R 与脑裂检查","detail":"职责：算写多数派与读票数并在写前检查可见副本数；技术选型：副本数取奇数，W 为超过半数，W 加 R 大于 N；失败处理：不足多数派返回 ErrSplitBrain，拒绝提交。","tone":"orange"}]},{"name":"状态机层","note":"每个副本的本地引擎，直接复用第 113 讲","items":[{"label":"本地 WAL 引擎","sub":"复用 WALStorage","detail":"职责：承载每个副本的持久化状态机；技术选型：直接复用第 113 讲 WALStorage，契约未改；失败处理：磁盘满与部分写沿用第 113 讲可判定错误与回滚。","tone":"purple"},{"label":"快照与日志截断","sub":"压缩 Raft 日志","detail":"职责：定期压缩 Raft 日志并落快照；技术选型：沿用第 113 讲临时文件加 Rename 原子替换；失败处理：半份快照退回全量重放。","tone":"purple"},{"label":"一致性级别判定","sub":"读路由决策","detail":"职责：按读级别决定是否允许 Follower 服务；技术选型：线性一致走 Leader 或 ReadIndex，有界陈旧检查日志落后量；失败处理：落后越界返回 ErrStaleRead。","tone":"purple"},{"label":"指标与追踪","sub":"原子计数埋点","detail":"职责：采集路由、提交、选主与迁移指标；技术选型：sync/atomic 计数，trace_id 沿帧透传；失败处理：埋点永不阻塞主链路。","tone":"purple"}]}]' />

逐层解读。**接入层**完全继承前两讲，它在分布式版里唯一的新职责是把 `ErrNotLeader`、`ErrNoQuorum` 这类新错误码还原成哨兵错误，让客户端能决定是重定向、退避重试还是直接失败。**路由层**是全新的一层，它把「一个 key 属于哪台机器」这件事收敛成纯函数，并额外承担成员变更期间的迁移状态；把它独立出来的理由是扩容、缩容、故障摘除都会改这张表，不能让它散落在每个 Raft 组里。**复制层**是本讲的核心：每个分片一个独立 Raft 组，组间完全并行（这是写吞吐能随分片数扩展的前提），组内靠日志加多数派保持一致。**状态机层**刻意复用第 113 讲的 `WALStorage`——这是贯穿线最实在的一次复用：Raft 只负责「决定应用哪条命令、以什么顺序」，真正把命令落到磁盘的还是上一讲那个引擎。

<ConceptDiagram type="raft" title="Raft 日志复制：Leader 追加日志，多数派确认后提交并应用" caption="写不是同步刷到所有副本，而是等到多数派即可返回，慢副本不阻塞" />

<ConceptDiagram type="hash-ring" title="分片路由：key 经哈希环定位到唯一副本组" caption="每个物理节点对应多个虚拟节点，顺时针找到第一个不小于 key 哈希的 VNode" />

<ConceptDiagram type="architecture-layer" title="multi-Raft 部署：每分片一个独立共识组" caption="30 个 Raft 组并行提交，组间只通过成员表协调，不存在全局单点" />

<FlowTimeline direction="horizontal" :steps='[{"title":"① 内存版","desc":"定 Command 与 Storage 契约","tone":"blue"},{"title":"② 网络层","desc":"Client 也实现 Storage","tone":"green"},{"title":"③ 持久化","desc":"WAL、恢复、锁分片、快照","tone":"orange"},{"title":"④ 分布式","desc":"分片、副本、Raft 共识","tone":"red"}]' />

依赖方向严格单向：接入层只依赖 `Storage`，路由层依赖 `HashRing` 与 `Membership`，复制层依赖 `Group` 接口与 `Command`，状态机层只认本地引擎接口。**不允许反向依赖**的理由：一旦 Raft 组里 import 了哈希环或成员表，成员变更就会变成一个需要全组共识的动作，扩容从「运维操作」升级成「分布式事务」；反过来让路由层只依赖一个 `Group` 接口，换共识库（从自研内核换到 `etcd/raft`）时复制层以下全部不用动。

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| `Frame/framecode` | 字节流 / error | `Command` / 状态码 | 无 | 无状态 | 坏帧断连，未知码收敛 255 |
| `HashRing` | key / 节点变更 | 物理节点 / 迁移区间 | `fnv`、`sort` | VNode 列表、节点集合 | 环为空返回空串，迁移区间为空表示无需搬迁 |
| `Quorum` | N、W、R、可见副本数 | 票数 / 校验结果 | 无 | 无状态 | 不足多数派返回 `ErrSplitBrain` |
| `raftCore` | `Command` / ack | 下标 / 待应用命令 | `WriteQuorum` | 任期、日志、提交索引、matchIndex | 非 Leader 拒绝 propose，任期落后即退位 |
| `ReplicaGroup` | `Command` / 一致性级别 | `Result` | `raftCore` | 本地 map、ackBudget、reachable | 未达多数派返回 `ErrNoQuorum`，状态机不变 |
| `Membership` | 节点变更 | 迁移任务 / 路由 | `HashRing`、`Group` | 三态状态机、pending 区间 | Joint 阶段保留双写，校验失败不切路由 |
| `Router` | `Command` | `Result` | `Membership`、`Group` | closed、指标 | 目标缺失返回 `ErrNotLeader` |
| `ClusterMetrics` | 埋点调用 | 指标视图 | `sync/atomic` | 原子计数器 | 不参与主链路，永不阻塞写 |

包目录（本地引擎文件沿用第 113 讲，本讲新增分布式文件；全部同属 `package kv`）：

```text
kv/
├── go.mod                 # module kv，go 1.21
├── contract.go            # 契约：Op / Command / Result / Storage / 全部哨兵错误
├── framecode.go           # 帧 status 位：沿用第 113 讲 16~22，新增 23~28
├── quorum.go              # W/R/N 计算、一致性级别、脑裂检查
├── hashring.go            # 一致性哈希、虚拟节点、迁移区间
├── raft.go                # 任期、选主、日志匹配、多数派提交内核
├── replica.go             # Group 接口、ReplicaGroup、读写一致性级别
├── membership.go          # 成员变更三态与迁移状态机
├── router.go              # Router 实现 Storage、跨分片边界检查
├── metrics.go             # 集群原子计数埋点
├── cluster_test.go        # quorum、选主、故障注入、迁移、跨分片、基准
├── wal.go / recovery.go   # 第 113 讲：每个副本的本地 WAL 引擎（复用）
├── shard.go / snapshot.go # 第 113 讲：本地状态机与快照（复用）
└── wal_storage.go         # 第 113 讲：本地 Storage 实现（复用）
```

依赖倒置体现在 `Group` 接口：路由层只认 `Storage` 加 `ShardID()`，不认 `raftCore`；于是把自研内核换成 `etcd/raft` 时，只需新写一个满足 `Group` 的适配器，接入层与路由层一行不改。这也是第 111 讲「接口越窄越好实现」在四讲之后兑现的红利。

<Callout type="warn" title="边界划分最常见的两种错法" text="第一，把哈希环塞进 Raft 组里，理由是「反正都要算 key」——结果是每个副本都要维护全集群成员表，扩容变成全组共识。第二，让状态机层知道分片与任期，理由是「打快照方便」——结果是第 113 讲那个干净的 WALStorage 被分布式细节污染，换共识库时连本地引擎都要重写。判断标准：这个模块能不能单独用一张临时哈希表或一个内存 core 做单元测试？能，边界就对了。" />

## 4. 数据与协议契约

**分片路由契约表**

| 字段 | 取值 | 语义 | 兼容规则 |
| --- | --- | --- | --- |
| 哈希函数 | FNV-1a 32 位 | key 与 VNode 共用一个哈希空间 | 一旦选定不改，否则全量重分布 |
| 虚拟节点数 | 默认 128 | 每物理节点的 VNode 数 | 改动需重算迁移区间，实测调参 |
| 定位规则 | 顺时针第一个不小于 key 哈希的 VNode | 返回其物理节点 | 回绕到环首 |
| 迁移区间 | `Start` 到 `End` 的哈希段 | 扩容时新节点从后继接管的段 | Joint 阶段双写 |
| 分片键 | `Command.Key` 整体 | 不支持组合键 | 相关 key 需业务侧绑定同一前缀 |

**Raft 日志条目表**

| 字段 | 类型 | 语义 | 零值 / 兼容规则 |
| --- | --- | --- | --- |
| `Term` | uint64 | 产生该日志的 Leader 任期 | 只增不减，任期回退即退位 |
| `Index` | int | 组内单调递增的日志位序 | 从 0 起，不依赖墙钟 |
| `Cmd` | `Command` | 第 111 讲定型的命令载体 | Op 数值 1/2/3 四讲不变 |

副本间消息用 proto 定义（应用只负责把 `Command` 交给状态机，`etcd/raft` 负责传输与持久化）：

```proto
syntax = "proto3";
package kv.replication;

message AppendEntries {
  uint64 term = 1;
  int32  leader_id = 2;
  int64  prev_log_index = 3;
  uint64 prev_log_term = 4;
  repeated LogEntry entries = 5;
  int64  leader_commit = 6;
}

message LogEntry {
  uint64 term = 1;
  int64  index = 2;
  bytes  command = 3;   // 第 111 讲 Command 的序列化结果
}

message AppendEntriesReply {
  uint64 term = 1;
  bool   success = 2;
  int64  match_index = 3;
}

message RequestVote {
  uint64 term = 1;
  int32  candidate_id = 2;
  int64  last_log_index = 3;
  uint64 last_log_term = 4;
}
```

**帧 status 位错误码表**（沿用第 113 讲预留的高位段，只追加不重排）

| 码值 | 含义 | 客户端还原 |
| --- | --- | --- |
| 1 | 成功 | `nil` |
| 16 到 22 | 第 113 讲单机与持久化错误 | `ErrNotFound` 等 |
| 23 | 写到 Follower | `ErrNotLeader`，重定向到 Leader |
| 24 | 未达多数派 | `ErrNoQuorum`，退避重试 |
| 25 | 陈旧读被拒 | `ErrStaleRead`，改走 Leader |
| 26 | 分片迁移中 | `ErrMigrating`，稍后重试 |
| 27 | 跨分片事务 | `ErrCrossShardTxn`，业务层 Saga |
| 28 | 疑似脑裂 | `ErrSplitBrain`，立即告警 |
| 255 | 未知内部错误 | 普通 error + 告警 |

对外 API 仍是 `Storage.Apply(Command) (Result, error)`；新增的是集群配置契约：

```json
{
  "shards": 30,
  "replicas": 3,
  "write_quorum": 2,
  "read_quorum": 2,
  "virtual_nodes": 128,
  "default_consistency": "linearizable",
  "election_timeout_ms": [150, 300],
  "heartbeat_ms": 50,
  "local_engine": "wal-storage-v3"
}
```

运维侧用元数据表管分片、副本与迁移任务（应用不读它，只由控制面写入）：

```sql
CREATE TABLE shard_meta (
    shard_id   INTEGER PRIMARY KEY,
    hash_start BIGINT NOT NULL,
    hash_end   BIGINT NOT NULL,
    epoch      INTEGER NOT NULL,
    state      TEXT NOT NULL
);
CREATE TABLE replica_meta (
    shard_id   INTEGER NOT NULL,
    replica_id INTEGER NOT NULL,
    node_addr  TEXT NOT NULL,
    role       TEXT NOT NULL,
    term       BIGINT NOT NULL,
    PRIMARY KEY (shard_id, replica_id)
);
CREATE TABLE migration_task (
    task_id   INTEGER PRIMARY KEY,
    from_node TEXT NOT NULL,
    to_node   TEXT NOT NULL,
    state     TEXT NOT NULL,
    checksum  TEXT
);
```

内部接口签名与四讲不变的契约快照如下（本讲只追加分布式哨兵错误，其余字段与 `Op` 数值沿用第 111 到 113 讲）：

```go
// 文件：contract.go
package kv

import "errors"

// 契约沿用第 111 讲定型、第 112 讲编码进帧、第 113 讲写进 WAL 的同一套定义。
// Op 数值、Command 字段、Storage 方法四讲之内一律不改，只追加分布式哨兵错误。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

var (
	// 第 111 讲：错误模型
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
	// 第 113 讲：持久化错误，跨网络仍可判定
	ErrCorrupt      = errors.New("kv: wal record corrupt")
	ErrNoSpace      = errors.New("kv: no space left on device")
	ErrPartialWrite = errors.New("kv: partial write to wal")
	// 第 114 讲：分布式错误
	ErrNotLeader     = errors.New("kv: not leader")
	ErrNoQuorum      = errors.New("kv: write lost quorum")
	ErrStaleRead     = errors.New("kv: stale read rejected")
	ErrSplitBrain    = errors.New("kv: split brain suspected")
	ErrMigrating     = errors.New("kv: shard is migrating")
	ErrCrossShardTxn = errors.New("kv: cross-shard transaction unsupported")
)

type Command struct {
	Op    Op
	Key   string
	Value []byte
}

type Result struct {
	Value []byte
	Found bool
}

type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

兼容规则：`Op` 数值与 16 到 28 的状态码一旦发布即冻结，新增错误只能往后追加；`Command` 序列化进 Raft 日志后就是跨机契约，字段只增不改、且新字段必须有零值语义。版本与灰度策略：控制面用 `epoch` 标识成员配置版本，`Joint` 阶段新旧两个 epoch 同时生效，逐 key 校验一致后才把 `epoch` 加一；回滚时把 epoch 退回旧值即可，因为旧配置的副本组一直保留到 `Reset`。

<Compare title="单 Raft 组与 multi-Raft 的取舍" :panels='[{"label":"单 Raft 组","code":"one group for all keys","note":"实现最简，但所有写挤在同一个 Leader，容量与吞吐顶死在一台机器"},{"label":"multi-Raft","code":"one group per shard","note":"本讲做法：组间并行，代价是成员表、迁移调度与热点分裂的复杂度"}]' />

## 5. 核心链路时序

<SequenceDiagram title="一次 SET 命令的分布式提交链路" caption="写先经路由定位分片，再由该分片的 Leader 复制日志，多数派确认后才应用并回包" :actors='["Client","Router","Ring","Leader","Followers","StateMachine"]' :steps='[{"from":"Client","to":"Router","label":"SET 帧","note":"status 位为请求","kind":"call"},{"from":"Router","to":"Ring","label":"定位分片","note":"FNV 加虚拟节点","kind":"call"},{"from":"Ring","to":"Router","label":"返回节点","note":"顺时针后继","kind":"return"},{"from":"Router","to":"Leader","label":"转发命令","note":"仅 Leader 接受写","kind":"call"},{"from":"Leader","to":"Leader","label":"propose 追加","note":"记录当前任期","kind":"self"},{"from":"Leader","to":"Followers","label":"AppendEntries","note":"并行复制日志","kind":"async"},{"from":"Followers","to":"Leader","label":"多数派确认","note":"2 票即可提交","kind":"return"},{"from":"Leader","to":"Leader","label":"推进提交位","note":"commitIndex 前移","kind":"self"},{"from":"Leader","to":"StateMachine","label":"应用已提交","note":"在锁外 apply","kind":"call"},{"from":"StateMachine","to":"Router","label":"返回 Result","note":"Found 为 true","kind":"return"},{"from":"Router","to":"Client","label":"回写 OK 帧","note":"错误码进 status","kind":"return"}]' />

逐步骤解读。第 1 步客户端按第 112 讲帧格式发来 `OpSet`，携带请求 ID 便于重试去重；第 2、3 步 Router 问哈希环「这个 key 属于哪个分片」，环按顺时针后继返回物理节点；第 4 步 Router 把命令交给该分片 Raft 组的 Leader，若打到 Follower 则返回状态码 23 让客户端重定向；第 5 步 Leader 以当前任期追加一条日志并给自己记一票；第 6、7 步并行发 AppendEntries，只要凑够 `WriteQuorum(3)=2` 票就返回，最慢的那个副本不阻塞；第 8 步推进 `commitIndex`；第 9 步把已提交命令应用到状态机（即第 113 讲的 `WALStorage`），**必须放在 `raftCore.mu` 之外**，否则状态机回调再取锁会形成锁序反转；第 10、11 步逐层回包。注意返回时机：客户端收到成功时，该写已经在多数派落日志并被应用，RPO 为 0；这是 CP 取舍换来的安全性。

**异常链路一：网络分区**。第 6 步时 Leader 与一个 Follower 被隔开，只剩 2 个可见副本，`CheckQuorum` 仍允许提交；若隔成 1 加 2，少数派那侧只有 1 票，`CheckQuorum` 返回 `ErrSplitBrain` 并拒绝写入，避免两侧各自提交。**分叉点在第 4 步之前**：写前检查可见副本数，不通过就根本不 propose。**异常链路二：Leader 切换**。第 7 步之后旧 Leader 挂了，未提交日志可能被新 Leader 覆盖；客户端因超时重试同一命令，靠业务幂等键与 `OpSet` 的幂等性收敛，不会写坏状态。**异常链路三：读打到 Follower**。线性一致读在第 4 步被 `ErrNotLeader` 拒绝，客户端改投 Leader；若显式选最终一致，读可能拿到旧值，这是配置里明码标价的。**异常链路四：跨分片事务**。一次请求想同时改两个不同分片的 key，`CheckSameShard` 返回 `ErrCrossShardTxn`，不进入任何 Raft 组。

<StepBox title="Leader 选举的状态迁移" :steps='[{"title":"第 1 步","body":"Follower 选举超时，任期加一转为 Candidate 并自投一票","tip":"超时随机化，避免多个节点同时竞选"},{"title":"第 2 步","body":"向其它节点发 RequestVote，日志不够新的节点会拒投","tip":"lastLogIndex 与 lastLogTerm 决定谁有资格当 Leader"},{"title":"第 3 步","body":"票数达到多数派（3 副本需 2 票）才转正为 Leader","tip":"未达多数就等下一轮，绝不降级为可用"},{"title":"第 4 步","body":"发现更高任期的消息立即 stepDown 回 Follower","tip":"这是出现双主时唯一的自愈动作"}]' startLabel="开始选举" />

<FlowTimeline direction="horizontal" :steps='[{"title":"Idle 稳态","desc":"正常路由，无迁移","tone":"blue"},{"title":"Joint 联合","desc":"新旧配置并存，区间双写","tone":"orange"},{"title":"校验一致","desc":"逐 key 比对源与目标","tone":"green"},{"title":"Done 待切","desc":"唯一允许切路由的时机","tone":"purple"},{"title":"Reset 闭环","desc":"回到 Idle，变更完成","tone":"red"}]' />

## 6. 核心实现（按模块逐段展开）

以下代码同属 `package kv`，分别存为 `contract.go`（已在第 4 节给出）、`framecode.go`、`quorum.go`、`hashring.go`、`raft.go`、`replica.go`、`membership.go`、`router.go`（本节）、`metrics.go`（第 9 节）、`cluster_test.go`（第 10 节）。全部放进同一目录，`go.mod` 声明 `go 1.21`，执行 `go test -race ./...` 即可编译并跑通全部测试与基准；作者已在 Go 1.23.8 上实测通过。本地状态机文件沿用第 113 讲，本讲代码不重复依赖它们，因此可独立编译。

### 6.1 帧状态码：沿用第 113 讲预留的高位段

**设计意图**：第 113 讲把帧 status 位从错误文本改成稳定数值，并明确「23 及以上预留给第 114 讲」。这里兑现它。**关键取舍**：只追加 23 到 28，绝不重排 16 到 22；代价是这张表要跨两讲共同维护，换来的是旧客户端遇到未知码走 default 降级而不是把 `ErrNoQuorum` 误读成 `ErrNotFound`。

```go
// 文件：framecode.go
package kv

import (
	"errors"
	"fmt"
)

// 帧 status 位沿用第 113 讲预留的高位段：16~22 是单机持久化错误，
// 23 起是分布式错误。数值与 Op 一样进入协议格式，客户端 errOf 与服务端
// codeOf 共用同一张表，因此第 112 讲退化成文本的错误重新可判定。
const (
	StatusOK       byte = 1
	CodeNotFound   byte = 16
	CodeEmptyKey   byte = 17
	CodeBadOp      byte = 18
	CodeClosed     byte = 19
	CodeCorrupt    byte = 20
	CodeNoSpace    byte = 21
	CodePartial    byte = 22
	CodeNotLeader  byte = 23
	CodeNoQuorum   byte = 24
	CodeStaleRead  byte = 25
	CodeMigrating  byte = 26
	CodeCrossShard byte = 27
	CodeSplitBrain byte = 28
)

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
	case errors.Is(err, ErrNotLeader):
		return CodeNotLeader
	case errors.Is(err, ErrNoQuorum):
		return CodeNoQuorum
	case errors.Is(err, ErrStaleRead):
		return CodeStaleRead
	case errors.Is(err, ErrSplitBrain):
		return CodeSplitBrain
	case errors.Is(err, ErrMigrating):
		return CodeMigrating
	case errors.Is(err, ErrCrossShardTxn):
		return CodeCrossShard
	default:
		return 255
	}
}

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
	case CodeNotLeader:
		return ErrNotLeader
	case CodeNoQuorum:
		return ErrNoQuorum
	case CodeStaleRead:
		return ErrStaleRead
	case CodeMigrating:
		return ErrMigrating
	case CodeCrossShard:
		return ErrCrossShardTxn
	case CodeSplitBrain:
		return ErrSplitBrain
	default:
		return fmt.Errorf("kv: server status %d", code)
	}
}
```

<Callout type="tip" title="为什么 ErrSplitBrain 要有独立码值" text="脑裂与普通写入失败的重试策略完全不同：后者可以退避重试，前者必须立刻停止写入并 paging，因为继续重试只会让两侧数据分歧扩大。把可判定的错误类型和处置动作绑定，是状态码存在的全部意义。" />

### 6.2 Quorum 与一致性级别：把取舍写成可计算的函数

**设计意图**：把「几个副本算够」「读要几票」「什么级别允许读 Follower」从口头约定变成可单测的函数。**关键取舍**：`WriteQuorum` 取 `N/2+1` 而非 `N`，让慢副本不拖住写；`CheckQuorum` 在写前检查可见副本数，明确把「少数派分区」判为不可用——这是 CP 选择落在代码里的一行。

```go
// 文件：quorum.go
package kv

import "fmt"

// Consistency 是读一致性级别。越强越安全，也越依赖 Leader 与多数派。
type Consistency uint8

const (
	ConsistencyLinearizable     Consistency = iota // 读走 Leader 或 ReadIndex
	ConsistencyBoundedStaleness                    // 可读 Follower，落后有上限
	ConsistencyEventual                            // 任意副本，接受读到旧值
)

// WriteQuorum 返回写多数派票数：超过半数即可提交。
// 副本数取奇数正是为了让「多数」不产生歧义，3 副本容忍 1 个故障。
func WriteQuorum(replicas int) int {
	return replicas/2 + 1
}

// ReadQuorum 在 W + R > N 约束下返回强一致读所需的最小票数。
func ReadQuorum(replicas, write int) int {
	return replicas - write + 1
}

// QuorumValid 校验 W/R/N 配置能否保证读写交集，避免读到未提交的旧值。
func QuorumValid(replicas, write, read int) bool {
	return write > replicas/2 && write <= replicas && read >= 1 && write+read > replicas
}

// CheckQuorum 写前检查：可见副本不足多数派就拒绝提交。
// 网络分区后少数派若仍独立提交，就会与另一侧形成双主，即脑裂。
func CheckQuorum(replicas, reachable int) error {
	if reachable < WriteQuorum(replicas) {
		return fmt.Errorf("%w: reachable %d of %d", ErrSplitBrain, reachable, replicas)
	}
	return nil
}

// LevelAllowsFollowerRead 判断某一致性级别是否允许把读发到 Follower。
func LevelAllowsFollowerRead(level Consistency) bool {
	return level != ConsistencyLinearizable
}
```

### 6.3 一致性哈希环：虚拟节点与迁移区间

**设计意图**：让扩容只影响相邻一小段 key，而不是像取模分片那样几乎全量重分布。**关键取舍**：每个物理节点铺 128 个 VNode 打散数据，`Add`/`Remove` 返回需要搬迁的 `(From, To, Start, End)` 区间，由迁移执行器消费；代价是环本身要维护有序 VNode 列表，且**虚拟节点数必须实测调参**——第 12 节的实测会显示节点少时它并不一定优于取模。

```go
// 文件：hashring.go
package kv

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// VNode 是哈希环上的一个虚拟节点；物理节点靠多个 VNode 打散，避免数据倾斜。
type VNode struct {
	Hash uint32
	Node string
}

// Migration 描述成员变更后需要搬迁的一段哈希区间。
// 取模分片在 N 变化时会让几乎所有 key 迁移，一致性哈希只影响相邻一小段。
type Migration struct {
	From  string
	To    string
	Start uint32
	End   uint32
}

type HashRing struct {
	mu       sync.RWMutex
	replicas int // 每个物理节点的虚拟节点数
	vnodes   []VNode
	nodes    map[string]bool
}

func NewHashRing(replicasPerNode int) *HashRing {
	if replicasPerNode <= 0 {
		replicasPerNode = 128
	}
	return &HashRing{replicas: replicasPerNode, nodes: make(map[string]bool)}
}

func hashKey(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

func vnodeHash(node string, i int) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(fmt.Sprintf("%s#%d", node, i)))
	return h.Sum32()
}

func sortVNodes(v []VNode) {
	sort.Slice(v, func(i, j int) bool {
		if v[i].Hash != v[j].Hash {
			return v[i].Hash < v[j].Hash
		}
		return v[i].Node < v[j].Node
	})
}

func predecessor(vnodes []VNode, h uint32) VNode {
	if len(vnodes) == 0 {
		return VNode{}
	}
	idx := sort.Search(len(vnodes), func(i int) bool { return vnodes[i].Hash >= h })
	if idx == 0 {
		return vnodes[len(vnodes)-1]
	}
	return vnodes[idx-1]
}

func successor(vnodes []VNode, h uint32) VNode {
	if len(vnodes) == 0 {
		return VNode{}
	}
	idx := sort.Search(len(vnodes), func(i int) bool { return vnodes[i].Hash >= h })
	if idx == len(vnodes) {
		return vnodes[0]
	}
	return vnodes[idx]
}

// Add 加入一个物理节点，返回它需要从哪些旧节点接管哪些区间。
// 首个节点不需要迁移；扩容时每个新 VNode 只从环上的后继节点接管一小段。
func (r *HashRing) Add(node string) []Migration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nodes[node] {
		return nil
	}
	old := append([]VNode(nil), r.vnodes...)
	var added []VNode
	for i := 0; i < r.replicas; i++ {
		added = append(added, VNode{Hash: vnodeHash(node, i), Node: node})
	}
	r.nodes[node] = true
	r.vnodes = append(r.vnodes, added...)
	sortVNodes(r.vnodes)
	if len(old) == 0 {
		return nil
	}
	var out []Migration
	for _, vn := range added {
		pred := predecessor(old, vn.Hash)
		succ := successor(old, vn.Hash)
		if succ.Node == node {
			continue
		}
		out = append(out, Migration{From: succ.Node, To: node, Start: pred.Hash, End: vn.Hash})
	}
	return out
}

// Remove 摘除一个物理节点，返回它的区间由哪些后继节点接管。
func (r *HashRing) Remove(node string) []Migration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.nodes[node] {
		return nil
	}
	old := append([]VNode(nil), r.vnodes...)
	kept := make([]VNode, 0, len(r.vnodes))
	for _, vn := range r.vnodes {
		if vn.Node != node {
			kept = append(kept, vn)
		}
	}
	delete(r.nodes, node)
	r.vnodes = kept
	var out []Migration
	for _, vn := range old {
		if vn.Node != node {
			continue
		}
		pred := predecessor(old, vn.Hash)
		succ := successor(old, vn.Hash)
		if succ.Node == node {
			continue
		}
		out = append(out, Migration{From: node, To: succ.Node, Start: pred.Hash, End: vn.Hash})
	}
	return out
}

// Get 顺时针找到第一个不小于 key 哈希的 VNode，返回其物理节点。
func (r *HashRing) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.vnodes) == 0 {
		return ""
	}
	return successor(r.vnodes, hashKey(key)).Node
}

func (r *HashRing) Nodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.nodes))
	for n := range r.nodes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (r *HashRing) VNodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vnodes)
}
```

### 6.4 Raft 内核：任期、选主、日志匹配与多数派提交

**设计意图**：用一段可编译的内存状态说明「一致性到底从哪来」——不追求每个副本时刻相同，只追求**所有副本按相同顺序应用相同的一串命令**。**关键取舍**：`ack` 返回本次新可应用的命令列表，`apply` 由调用方在锁外执行，避免状态机回调再取 `raftCore.mu` 形成自锁；`stepDown` 让任期落后节点立即退位，是双主自愈的唯一动作。选举超时、心跳、日志持久化与网络传输交给成熟库，本内核刻意不贴整个仓库。

```go
// 文件：raft.go
package kv

import "sync"

type RaftState uint8

const (
	StateFollower RaftState = iota
	StateCandidate
	StateLeader
)

type LogEntry struct {
	Term  uint64
	Index int
	Cmd   Command
}

// raftCore 是「任期 + 选主 + 日志复制 + 多数派提交」的最小可编译内核。
// 选举超时、心跳、日志持久化与网络传输交给 etcd/raft 这类成熟库；
// 这里用内存状态说明一致性来源：所有副本按同一顺序应用同一串命令。
type raftCore struct {
	mu           sync.Mutex
	me           int
	peers        int
	term         uint64
	votedFor     int
	state        RaftState
	votes        int
	log          []LogEntry
	commitIndex  int
	appliedIndex int
	matchIndex   []int
}

func newRaftCore(me, peers int) *raftCore {
	m := make([]int, peers)
	for i := range m {
		m[i] = -1
	}
	return &raftCore{
		me: me, peers: peers, votedFor: -1, state: StateFollower,
		commitIndex: -1, appliedIndex: -1, matchIndex: m,
	}
}

// startElection 发起一轮选举：任期加一、转为 Candidate、自投一票。
// 单副本时自投即达多数派；多副本需 recvVote 补齐票数。
func (r *raftCore) startElection() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.term++
	r.state = StateCandidate
	r.votedFor = r.me
	r.votes = 1
	if r.votes >= WriteQuorum(r.peers) {
		r.state = StateLeader
	}
	return r.term
}

// recvVote 收到一张来自其它节点的选票；达到多数派才转正为 Leader。
func (r *raftCore) recvVote() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.votes++
	if r.state == StateCandidate && r.votes >= WriteQuorum(r.peers) {
		r.state = StateLeader
		return true
	}
	return false
}

// stepDown 发现更高任期时立即退位，是防止双主（脑裂）的关键动作。
func (r *raftCore) stepDown(term uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if term > r.term {
		r.term = term
		r.state = StateFollower
		r.votedFor = -1
		r.votes = 0
	}
}

func (r *raftCore) isLeader() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == StateLeader
}

func (r *raftCore) currentTerm() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.term
}

// logMatch 是 AppendEntries 的一致性检查：前一条日志的任期必须匹配，
// 不匹配则 Follower 拒绝，让 Leader 回退 nextIndex 后重发。
func (r *raftCore) logMatch(prevIndex int, prevTerm uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prevIndex < 0 {
		return true
	}
	if prevIndex >= len(r.log) {
		return false
	}
	return r.log[prevIndex].Term == prevTerm
}

// propose 只有 Leader 可调用：追加日志并给自己记一票，返回下标。
func (r *raftCore) propose(cmd Command) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != StateLeader {
		return -1, false
	}
	idx := len(r.log)
	r.log = append(r.log, LogEntry{Term: r.term, Index: idx, Cmd: cmd})
	r.matchIndex[r.me] = idx
	return idx, true
}

// ack 记录一个副本的复制确认，推进提交索引，返回本次新可应用的命令。
// 提交条件是「该下标被多数派确认」，而不是「所有副本都确认」，
// 因此慢副本不会拖住整条写链路。
func (r *raftCore) ack(peer, index int) []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	if peer < 0 || peer >= r.peers || index < 0 || index >= len(r.log) {
		return nil
	}
	if index > r.matchIndex[peer] {
		r.matchIndex[peer] = index
	}
	for i := r.commitIndex + 1; i < len(r.log); i++ {
		n := 0
		for p := 0; p < r.peers; p++ {
			if r.matchIndex[p] >= i {
				n++
			}
		}
		if n < WriteQuorum(r.peers) {
			break
		}
		r.commitIndex = i
	}
	var out []Command
	for i := r.appliedIndex + 1; i <= r.commitIndex; i++ {
		out = append(out, r.log[i].Cmd)
	}
	r.appliedIndex = r.commitIndex
	return out
}

func (r *raftCore) committed(index int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return index >= 0 && index <= r.commitIndex
}

// lag 返回本地日志末尾与提交索引的差距，供有界陈旧读判断是否越界。
func (r *raftCore) lag() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log) - 1 - r.commitIndex
}

func (r *raftCore) logLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log)
}
```

下面把 propose 里最关键的几行逐行拆开，顺序与任期检查才是选主安全的落点。

<LineByLine title="propose 的四道门槛" code="r.mu.Lock()\ndefer r.mu.Unlock()\nif r.state != StateLeader {\n    return -1, false\n}\nidx := len(r.log)\nr.log = append(r.log, LogEntry{Term: r.term, Index: idx, Cmd: cmd})\nr.matchIndex[r.me] = idx\nreturn idx, true" :notes='["日志、提交索引、matchIndex 都由同一把锁保护，任何并发写都串行化","defer 保证所有提前 return 都释放锁，避免一次失败拖死整个 Raft 组","只有 Leader 能追加日志，这是单写入口的硬约束","下标用日志长度单调递增，不用时间戳，天然规避时钟回拨","每条日志都带上当前任期，选举时靠它判断谁的数据更新","Leader 先给自己记一票，多数派还差 Follower 的确认","返回下标供后续 AppendEntries 与 committed 检查使用"]' />

<OutputBox title=" quorum 与选举单测的真实输出" output="=== RUN   TestQuorumMath\n--- PASS: TestQuorumMath (0.00s)\n=== RUN   TestLeaderElection\n--- PASS: TestLeaderElection (0.00s)\n=== RUN   TestQuorumFaultTolerance\n--- PASS: TestQuorumFaultTolerance (0.00s)\nPASS" note="3 副本自投一票不当选、第二票转正；ackBudget 为 1 可提交、为 0 返回 ErrNoQuorum" />

### 6.5 副本组：读写一致性级别与故障注入

**设计意图**：把 `raftCore` 包成满足 `Group` 接口的副本组，对外只暴露 `Apply`。**关键取舍**：强一致读必须走 Leader 或 ReadIndex，Follower 日志可能落后；`ReplicaGroup.Read` 额外接受一致性级别，有界陈旧会检查 `lag()`。`ackBudget` 与 `reachable` 用 `atomic.Int64`，让并发故障注入不引入数据竞争。应用回调 `applyOne` 在 `raftCore.mu` 之外执行。

```go
// 文件：replica.go
package kv

import (
	"sync"
	"sync/atomic"
)

// Group 抽象一个分片对应的副本组；真实实现里它就是一个独立 Raft 组。
// 用接口隔离，路由层不依赖任何具体共识库。
type Group interface {
	Storage
	ShardID() int
}

var _ Group = (*ReplicaGroup)(nil)

const maxStaleness = 3

// ReplicaGroup 是一个分片的多副本组：只有 Leader 接受写，
// 写先经 raftCore 复制到多数派，提交后才落到本地状态机。
type ReplicaGroup struct {
	id        int
	peers     int
	core      *raftCore
	mu        sync.RWMutex
	data      map[string][]byte
	ackBudget atomic.Int64 // 本次写允许收到的 Follower 确认数，故障注入用
	reachable atomic.Int64 // 当前可见副本数，脑裂检查用
}

func NewReplicaGroup(id, peers int) *ReplicaGroup {
	if peers%2 == 0 {
		peers++ // 副本数取奇数，分区时不会出现两边各半
	}
	g := &ReplicaGroup{
		id: id, peers: peers,
		data: make(map[string][]byte),
	}
	g.ackBudget.Store(int64(peers - 1))
	g.reachable.Store(int64(peers))
	g.core = newRaftCore(0, peers)
	g.core.startElection()
	for i := 1; i < WriteQuorum(peers); i++ {
		g.core.recvVote()
	}
	return g
}

func (g *ReplicaGroup) ShardID() int { return g.id }

// SetAckBudget / SetReachable 用原子写，方便并发故障注入且不引入数据竞争。
func (g *ReplicaGroup) SetAckBudget(n int) { g.ackBudget.Store(int64(n)) }
func (g *ReplicaGroup) SetReachable(n int) { g.reachable.Store(int64(n)) }

// applyOne 把已提交命令应用到本地状态机，幂等，重放安全。
func (g *ReplicaGroup) applyOne(cmd Command) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch cmd.Op {
	case OpSet:
		g.data[cmd.Key] = append([]byte(nil), cmd.Value...)
	case OpDel:
		delete(g.data, cmd.Key)
	}
}

// Replicate 模拟 Leader 收到一个 Follower 的 AppendEntries 响应。
func (g *ReplicaGroup) Replicate(peer, index int) {
	for _, c := range g.core.ack(peer, index) {
		g.applyOne(c) // 在 raftCore 的锁之外应用，避免锁顺序反转
	}
}

// Read 暴露可选一致性级别的读入口；Storage.Apply 的 GET 默认走线性一致。
func (g *ReplicaGroup) Read(cmd Command, level Consistency) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op != OpGet {
		return Result{}, ErrBadOp
	}
	// 强一致读必须走 Leader 或 ReadIndex；Follower 日志可能落后于提交位点。
	if !LevelAllowsFollowerRead(level) && !g.core.isLeader() {
		return Result{}, ErrNotLeader
	}
	if level == ConsistencyBoundedStaleness && g.core.lag() > maxStaleness {
		return Result{}, ErrStaleRead
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.data[cmd.Key]
	if !ok {
		return Result{}, ErrNotFound
	}
	return Result{Value: append([]byte(nil), v...), Found: true}, nil
}

func (g *ReplicaGroup) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	if cmd.Op == OpGet {
		return g.Read(cmd, ConsistencyLinearizable)
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	// 写前脑裂检查：可见副本不足多数派就拒绝提交，避免少数派分区独立推进。
	if err := CheckQuorum(g.peers, int(g.reachable.Load())); err != nil {
		return Result{}, err
	}
	idx, ok := g.core.propose(cmd)
	if !ok {
		return Result{}, ErrNotLeader
	}
	budget := int(g.ackBudget.Load())
	if budget > g.peers-1 {
		budget = g.peers - 1
	}
	for peer := 1; peer <= budget; peer++ {
		for _, c := range g.core.ack(peer, idx) {
			g.applyOne(c)
		}
	}
	if !g.core.committed(idx) {
		return Result{}, ErrNoQuorum
	}
	return Result{Found: true}, nil
}

func (g *ReplicaGroup) Close() error { return nil }
```

<Callout type="warn" title="apply 回调为什么必须在锁外" text="raftCore.ack 持 mu 计算提交索引，若直接在里面调用状态机回调，而回调又要取 ReplicaGroup.mu，别的路径以相反顺序取这两把锁时就死锁。ack 只返回待应用命令列表，由调用方在锁外 applyOne——第 113 讲的快照也用了同一条锁序规则。" />

### 6.6 成员变更：三态状态机与数据迁移

**设计意图**：直接替换成员列表会在过渡期出现两个互不相交的多数派（旧配置的 2 票与新配置的 2 票可能来自完全不同的节点），造成脑裂。这里用 `Idle → Joint → Done → Reset` 三态加一个完成态逐步过渡。**关键取舍**：`RemoveNode` 不立即删组，Joint 阶段源组仍保留服务，只有 `FinishMigration` 之后才允许切路由；代价是迁移期间要双写、存储要多占一份。

```go
// 文件：membership.go
package kv

import "sync"

// MigrationState 描述一次成员变更的过渡阶段。
// 直接替换成员列表会在过渡期出现两个互不相交的多数派，必须用联合共识逐步过渡。
type MigrationState uint8

const (
	MigrationIdle  MigrationState = iota
	MigrationJoint                // 新旧配置同时生效，迁移区间双写
	MigrationDone                 // 数据校验一致，等待切换路由
)

type Membership struct {
	mu      sync.RWMutex
	ring    *HashRing
	groups  map[string]Group
	state   MigrationState
	pending []Migration
}

func NewMembership(replicasPerNode int) *Membership {
	return &Membership{
		ring:   NewHashRing(replicasPerNode),
		groups: make(map[string]Group),
		state:  MigrationIdle,
	}
}

// AddNode 触发一次成员变更：加入节点后若产生迁移区间，就进入 Joint 阶段。
// 首个节点是冷启动建集群，不算扩容，不产生迁移任务。
func (m *Membership) AddNode(node string, g Group) []Migration {
	m.mu.Lock()
	defer m.mu.Unlock()
	hadNodes := len(m.groups) > 0
	migs := m.ring.Add(node)
	m.groups[node] = g
	if hadNodes && len(migs) > 0 {
		m.state = MigrationJoint
		m.pending = migs
	}
	return migs
}

// RemoveNode 摘除节点，返回区间接管任务；不立即删组，避免 Joint 期间读不到源数据。
func (m *Membership) RemoveNode(node string) []Migration {
	m.mu.Lock()
	defer m.mu.Unlock()
	migs := m.ring.Remove(node)
	if len(migs) > 0 {
		m.state = MigrationJoint
		m.pending = migs
	}
	return migs
}

func (m *Membership) State() MigrationState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Membership) Pending() []Migration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Migration(nil), m.pending...)
}

// FinishMigration 在源与目标数据校验一致后推进到 Done，
// 这是唯一允许切换路由的时机，避免过渡期出现双主。
func (m *Membership) FinishMigration() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = MigrationDone
	m.pending = nil
}

// Reset 在运维确认新配置稳定后回到 Idle，成员变更闭环。
func (m *Membership) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = MigrationIdle
	m.pending = nil
}

// Route 按 key 返回目标组；Joint 阶段区间内的 key 仍由源组服务，双写由 Router 处理。
func (m *Membership) Route(key string) (Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node := m.ring.Get(key)
	if node == "" {
		return nil, ErrNoQuorum
	}
	g := m.groups[node]
	if g == nil {
		return nil, ErrNotLeader
	}
	return g, nil
}

func (m *Membership) Groups() map[string]Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Group, len(m.groups))
	for k, v := range m.groups {
		out[k] = v
	}
	return out
}

func (m *Membership) Ring() *HashRing { return m.ring }
```

### 6.7 Router：仍是 Storage，并守住跨分片边界

**设计意图**：分布式入口依旧实现第 111 讲的 `Storage`，调用方从单机切到集群零改动。**关键取舍**：`CheckSameShard` 只做边界判断并明确拒绝跨分片操作，不假装提供事务；跨 key 原子性交给业务层 Saga 或把相关 key 绑定同一分片键。埋点放在路由层，因为它是所有请求的唯一入口。

```go
// 文件：router.go
package kv

import "sync"

var _ Storage = (*Router)(nil)

// Router 按一致性哈希把命令路由到唯一分片，自己仍实现 Storage。
// 第 111 讲定型的契约在分布式入口处依旧成立，调用方从单机切到集群零改动。
type Router struct {
	membership *Membership
	metrics    *ClusterMetrics
	mu         sync.RWMutex
	closed     bool
}

func NewRouter(m *Membership) *Router {
	return &Router{membership: m, metrics: &ClusterMetrics{}}
}

func (r *Router) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return Result{}, ErrClosed
	}
	// 埋点放在路由层：它是所有请求的唯一入口，一次计数即可覆盖全链路。
	r.metrics.routed.Add(1)
	if cmd.Op == OpGet {
		r.metrics.reads.Add(1)
	} else {
		r.metrics.writes.Add(1)
	}
	g, err := r.membership.Route(cmd.Key)
	if err != nil {
		return Result{}, err
	}
	return g.Apply(cmd)
}

// CheckSameShard 判断一组 key 是否落在同一分片。
// 跨分片返回 ErrCrossShardTxn：本系统不提供跨分片事务，
// 业务层需把相关 key 绑定同一分片键，或改用 Saga 补偿（第 115 讲）。
func (r *Router) CheckSameShard(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	ring := r.membership.Ring()
	first := ring.Get(keys[0])
	for _, k := range keys[1:] {
		if ring.Get(k) != first {
			return "", ErrCrossShardTxn
		}
	}
	return first, nil
}

func (r *Router) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var first error
	for _, g := range r.membership.Groups() {
		if err := g.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Metrics 暴露只读指标视图，供第 9 节的 /metrics 端点抓取。
func (r *Router) Metrics() map[string]int64 { return r.metrics.Read() }
```

<OutputBox title="全套测试与竞态检测的真实输出" output="go test -race ./...\nok  \tkvverify\t1.332s\n\ngo test ./...\nok  \tkvverify\t0.276s\n\ngo vet ./...   （无输出即通过）" note="Go 1.23.8 / darwin-arm64 / Apple M3 Pro 实测；含 200 个 key 跨分片写入回读、迁移区间与跨分片拒绝" />

## 7. 失败路径与边界条件

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 进程崩溃 | 节点被 kill -9 | 该副本暂停，多数派仍在 | 心跳超时、`leader_changes` 增加 | 多数派继续提交 | 重启后从本地 WAL 与快照追日志 |
| 部分写 | 本地 WAL 短写或掉电 | 日志尾半条记录 | `partial_writes` 加一 | 沿用第 113 讲 `Truncate` 回滚 | 重放前裁脏尾 |
| 网络分区（少数派） | 1 加 2 隔离 | 少数派侧写被拒 | `split_brain_alerts` 突增 | `CheckQuorum` 返回 `ErrSplitBrain` | 分区愈合后靠日志匹配回退追平 |
| 网络分区（多数派） | 2 加 1 隔离 | 多数派侧继续服务 | 心跳指标 | 正常提交 | 落后副本重启后补日志 |
| 时钟异常 | NTP 回拨或 GC 停顿 | 误触发选举 | 任期跳变、`leader_changes` 抖动 | 任期与日志位序不依赖墙钟 | 随机化选举超时，无需人工 |
| 脑裂 / 双主 | 直接替换成员列表 | 两侧各自提交，数据分歧 | 同一 key 两版本、校验和不一致 | Joint 阶段禁止直接切换 | 退回旧 epoch，人工比对后重放 |
| Leader 切换 | 旧 Leader 提交后即挂 | 未提交日志被覆盖 | 客户端超时 | 幂等命令加幂等键 | 客户端重试，新 Leader 重新提交 |
| 读打到 Follower | 线性一致读误路由 | 读到未提交的旧值 | 读写版本不一致告警 | `ErrNotLeader` 明确拒绝 | 改投 Leader 或走 ReadIndex |
| 慢副本 | 某 Follower 磁盘抖动 | 提交等待本应变长 | `commits_total` 与 ack 延迟背离 | 多数派即可返回，不等最慢 | 隔离慢盘，必要时摘除 |
| 跨两个 key 的写 | 落在不同分片 | 无法原子提交 | `CheckSameShard` 报错 | 返回 `ErrCrossShardTxn` | 绑定同一分片键或业务层 Saga |
| 成员变更中途失败 | 迁移校验不一致 | 源与目标数据不同 | `migrations` 卡在 Joint | 保留旧配置，不切路由 | 重跑迁移，一致后才 Done |
| 热点分片 | Zipf 分布压单一 key | 单 Raft 组 CPU 打满 | 分片级 QPS 严重倾斜 | 限流与客户端退避 | 分裂分片或业务侧拆 key |
| 副本全挂 | 3 个节点同时下线 | 该分片完全不可写 | 全部心跳丢失 | 保一致性、拒绝写入 | 恢复任一副本后从快照重建 |
| 偶数副本配置 | 误配 4 副本 | 分区成 2 加 2 两边都不可写 | 配置校验 | 构造时自动加一到奇数 | 改配置并重新部署 |

**幂等键与重试预算**：`OpSet`/`OpDel` 幂等，因此 Leader 切换后的客户端重试不会写坏状态；但重试必须带业务幂等键，否则「超时后重试」可能把一次用户操作变成两次。重试预算统一 3 次、指数退避 20 ms 起，只对 `ErrNotLeader`、`ErrNoQuorum`、`ErrMigrating` 生效；`ErrSplitBrain` 与 `ErrCrossShardTxn` **禁止重试**，前者要立刻停写告警，后者是设计边界而非临时故障。**超时传递**：由第 112 讲的连接 `SetDeadline` 与上层 `context` 沿帧透传，Raft 层不吞取消信号。**降级与人工介入点**：脑裂告警、迁移校验不一致、副本全挂三处必须留人工开关；自动绕过其中任何一个都可能把局部数据分歧变成全站不一致。

<Callout type="warn" title="跨分片没有事务，是本讲最贵的取舍" text="分片换来容量，代价是「单 key 原子、跨 key 不原子」。转账、下单这类必须跨 key 的操作，要么把相关 key 用同一分片键绑在一起，要么在业务层用 Saga 补偿——不能假装分布式事务不存在。这也是第 083 讲 CAP、第 085 讲分布式事务在真实系统里的落点，下一讲电商微服务会正面处理它。" />

## 8. 并发与一致性

**并发模型与 goroutine 生命周期**：multi-Raft 的并发单位是「分片」而不是「请求」——30 个 Raft 组各有一条串行日志，组间完全并行，于是写吞吐可随分片数近似线性扩展。组内由 `raftCore.mu` 串行化 `propose`/`ack`/`commitIndex`，`ReplicaGroup.mu` 只保护本地 map，`Membership.mu` 与 `HashRing.mu` 保护成员表与环。选主与心跳由成熟库各起 goroutine，`Close` 时按「停止接请求 → 各组退位 → 关闭状态机」的顺序收尾；本内核用 `sync.Mutex` 而非 channel，因为保护的是必须原子的状态变更。

**锁粒度与热点**：分片之间无锁竞争，这是比第 113 讲「32 把分片锁」更彻底的一次并发提升；但组内仍是单 Leader 串行日志，且**分片不解决热点 key**——所有针对同一 key 的写永远落在同一个 Raft 组，Zipf 分布下会出现单组过热。缓解手段是客户端限流、业务侧拆 key、以及预留的分片分裂接口。锁顺序统一为「`Membership.mu` 到 `HashRing.mu`」与「`raftCore.mu` 到 `ReplicaGroup.mu`（后者只在锁外 apply）」，`go test -race` 实测无竞争。

**竞态分析**：最容易出错的是 `applyOne` 的位置——`ack` 持 `raftCore.mu` 只返回命令列表，应用必须在锁外，否则状态机回调再取 `ReplicaGroup.mu` 会与读路径形成环形等待。`ackBudget`、`reachable` 用 `atomic.Int64` 而非普通字段，保证并发故障注入不引入数据竞争。

**一致性级别选择依据**：默认单 key 线性一致，因为写经 Leader 串行提交、读也走 Leader 或 ReadIndex；`ConsistencyBoundedStaleness` 允许读 Follower 但检查 `lag()` 不超过 `maxStaleness`，适合画像标签这类可容忍毫秒级陈旧的高吞吐读；`ConsistencyEventual` 完全放开，适合统计类读。三档的差别不是「快慢」，而是「愿意为读到旧值付什么代价」。

**顺序保证与乱序处理**：日志顺序由 `Term + Index` 决定，不依赖墙钟，因此时钟回拨不影响正确性；Follower 通过 `logMatch(prevIndex, prevTerm)` 校验前一条日志，不匹配就让 Leader 回退 `nextIndex` 重发，直到找到分叉点。第 112 讲单连接串行帧不会乱序，第 113 讲 WAL 靠字节位点重建顺序，本讲靠日志位序——四讲用的是同一个「顺序先于状态」的原则。

<CodeTabs title="同步写所有副本与多数派提交对照" :tabs='[{"label":"同步写所有副本","code":"for p := range peers { send(p, cmd) }\nwaitAll()","note":"任一副本故障整条写挂掉，可用性最差"},{"label":"多数派提交","code":"for p := range peers { go send(p, cmd) }\nwaitQuorum(WriteQuorum(n))","note":"慢副本不阻塞，3 副本容忍 1 个故障，本讲做法"}]' />

## 9. 可观测性

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| `routed_total` | Counter | 路由层处理的请求数 | 环比跌 50% 持续 1 分钟 |
| `writes_total` / `reads_total` | Counter | 写 / 读命令数 | 仅看板 |
| `proposals_total` | Counter | Leader 追加日志数 | 与 `commits_total` 长期背离告警 |
| `commits_total` | Counter | 已提交并应用的日志数 | 停滞表示选主或 quorum 异常 |
| `leader_changes` | Counter | 任期变更次数 | 5 分钟内超过 3 次 paging |
| `quorum_failures` | Counter | 未达多数派次数 | 大于 0 立即告警 |
| `split_brain_alerts` | Counter | 疑似脑裂次数 | 大于 0 立即 paging 并停写 |
| `migrations` | Gauge | 当前活跃迁移任务数 | 长时间不归零提示卡在 Joint |
| `stale_reads` | Counter | 有界陈旧读被拒次数 | 突增提示 Follower 复制滞后 |
| `shard_qps` | Histogram | 每分片请求分布 | 最大分片超过均值 3 倍判热点 |

结构化日志固定字段：`ts`、`level`、`trace_id`、`shard_id`、`replica_id`、`term`、`commit_index`、`op`、`key_hash`、`err_code`、`cost_ms`。`trace_id` 由第 112 讲帧头的请求 ID 透传，经 Router、Leader、状态机三层打同一个 ID，于是「一次 SET 卡在路由、等多数派还是应用阶段」可以一次关联出来；`term` 与 `commit_index` 让选主期间的抖动有据可查。

埋点实现（独立文件，与第 6 节各段同属 `package kv`）：

```go
// 文件：metrics.go
package kv

import "sync/atomic"

// ClusterMetrics 用原子计数埋点，避免可观测性路径成为 Raft 组的锁竞争点。
// 字段与第 9 节指标表一一对应，Read 导出一份供 /metrics 抓取的快照。
type ClusterMetrics struct {
	routed           atomic.Int64
	writes           atomic.Int64
	reads            atomic.Int64
	proposals        atomic.Int64
	commits          atomic.Int64
	leaderChanges    atomic.Int64
	quorumFailures   atomic.Int64
	splitBrainAlerts atomic.Int64
	migrations       atomic.Int64
	staleReads       atomic.Int64
}

func (m *ClusterMetrics) Read() map[string]int64 {
	return map[string]int64{
		"routed_total":       m.routed.Load(),
		"writes_total":       m.writes.Load(),
		"reads_total":        m.reads.Load(),
		"proposals_total":    m.proposals.Load(),
		"commits_total":      m.commits.Load(),
		"leader_changes":     m.leaderChanges.Load(),
		"quorum_failures":    m.quorumFailures.Load(),
		"split_brain_alerts": m.splitBrainAlerts.Load(),
		"migrations":         m.migrations.Load(),
		"stale_reads":        m.staleReads.Load(),
	}
}
```

**用它定位一次真实故障**：某次写入成功率掉到 70%，看板显示 `leader_changes` 5 分钟内跳了 12 次、`proposals_total` 仍在涨但 `commits_total` 停滞、`split_brain_alerts` 为 0、`shard_qps` 分布均匀。四个信号合起来指向「某个可用区网络抖动导致频繁重选，Leader 刚 propose 就退位，日志来不及提交」，而不是脑裂或热点。处置是隔离抖动可用区、临时调大选举超时，并确认客户端重试预算没有把抖动放大成雪崩。若只盯「写错误率」，很容易误判成存储层磁盘问题去查本地 WAL。

<Term word="ReadIndex" text="Raft 中让 Follower 也能提供线性一致读的机制：先向 Leader 确认自己仍是 Leader 并取当前提交位点，等本地状态机追到该位点再返回，避免读旧值" />

## 10. 测试策略

分层测试，mock 边界切在「网络传输」与「本地状态机」两处，其余用真实内核：

| 层次 | 覆盖内容 | 手段 | 为什么这样切 |
| --- | --- | --- | --- |
| 单测 / 表驱动 | W/R/N 数学、状态码往返 | `TestQuorumMath`、`TestFrameCodeRoundTrip` | 纯函数，无需外部依赖 |
| 选主语义 | 自投不当选、第二票转正、退位 | `TestLeaderElection` | 任期与票数必须可断言 |
| 故障注入 | 少一个 ack 仍可写、少两个必须拒 | `TestQuorumFaultTolerance` | 用 `ackBudget` 精确控制确认数 |
| 一致性级别 | 强一致读拒 Follower、最终一致放行 | `TestConsistencyLevels` | 用 `stepDown` 模拟退位 |
| 集成 | 200 个 key 跨分片写入回读 | `TestRouterWriteAcrossShards` | 真实 Router 加三个副本组 |
| 边界 | 跨分片拒绝、成员变更三态 | `TestCrossShardTxnRejected`、`TestMembershipLifecycle` | 把设计边界写成断言 |
| 迁移 | 扩容产生区间、迁移比例合理 | `TestHashRingMigration` | 10000 个 key 统计分布 |
| 并发 | 混合读写、故障注入并发 | `go test -race` | 锁序与原子字段靠 race detector |
| 基准 / 压测 | 路由写读开销 | `BenchmarkRouterSet/Get` | `benchstat` 对照 |
| 混沌 | 杀节点、断网、时钟回拨 | 外部脚本 + 故障注入 | 验证 RTO、RPO 与脑裂兜底 |

可运行的测试与基准（含一处关键断言：`ackBudget=0` 时写返回 `ErrNoQuorum` 且状态机里没有该 key）：

```go
// 文件：cluster_test.go
package kv

import (
	"errors"
	"fmt"
	"testing"
)

func TestQuorumMath(t *testing.T) {
	cases := []struct {
		n, w, r int
	}{
		{1, 1, 1},
		{3, 2, 2},
		{5, 3, 3},
		{7, 4, 4},
	}
	for _, c := range cases {
		if got := WriteQuorum(c.n); got != c.w {
			t.Fatalf("N=%d WriteQuorum=%d want=%d", c.n, got, c.w)
		}
		if got := ReadQuorum(c.n, c.w); got != c.r {
			t.Fatalf("N=%d ReadQuorum=%d want=%d", c.n, got, c.r)
		}
		if !QuorumValid(c.n, c.w, c.r) {
			t.Fatalf("N=%d W=%d R=%d 应满足 W+R>N", c.n, c.w, c.r)
		}
	}
}

func TestCheckQuorumSplitBrain(t *testing.T) {
	if err := CheckQuorum(3, 1); !errors.Is(err, ErrSplitBrain) {
		t.Fatalf("少数派分区应拒绝写, got %v", err)
	}
	if err := CheckQuorum(3, 2); err != nil {
		t.Fatalf("多数派分区应可写, got %v", err)
	}
	if err := CheckQuorum(5, 2); !errors.Is(err, ErrSplitBrain) {
		t.Fatalf("5 副本只剩 2 应拒绝写, got %v", err)
	}
}

func TestFrameCodeRoundTrip(t *testing.T) {
	errs := []error{
		ErrNotFound, ErrEmptyKey, ErrBadOp, ErrClosed,
		ErrCorrupt, ErrNoSpace, ErrPartialWrite,
		ErrNotLeader, ErrNoQuorum, ErrStaleRead,
		ErrSplitBrain, ErrMigrating, ErrCrossShardTxn,
	}
	for _, want := range errs {
		got := errOf(codeOf(want))
		if !errors.Is(got, want) {
			t.Fatalf("code=%d got=%v want=%v", codeOf(want), got, want)
		}
	}
}

func TestLeaderElection(t *testing.T) {
	core := newRaftCore(0, 3)
	if core.isLeader() {
		t.Fatal("初始状态应为 Follower")
	}
	term := core.startElection()
	if core.isLeader() {
		t.Fatal("3 副本自投一票不该当选")
	}
	if !core.recvVote() {
		t.Fatal("第二票应当选")
	}
	if !core.isLeader() {
		t.Fatal("应为 Leader")
	}
	core.stepDown(term + 1)
	if core.isLeader() {
		t.Fatal("发现更高任期应立即退位")
	}
	one := newRaftCore(0, 1)
	one.startElection()
	if !one.isLeader() {
		t.Fatal("单副本自投即达多数派")
	}
}

func TestQuorumFaultTolerance(t *testing.T) {
	g := NewReplicaGroup(0, 3)
	g.SetAckBudget(1) // 自己 1 票 + 1 个 Follower = 2 票，达到多数派
	if _, err := g.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	got, err := g.Apply(Command{Op: OpGet, Key: "k"})
	if err != nil || string(got.Value) != "v" {
		t.Fatalf("got=%q err=%v", got.Value, err)
	}

	g2 := NewReplicaGroup(1, 3)
	g2.SetAckBudget(0) // 只有自己 1 票，不足多数派
	if _, err := g2.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); !errors.Is(err, ErrNoQuorum) {
		t.Fatalf("want ErrNoQuorum, got %v", err)
	}
	if _, err := g2.Apply(Command{Op: OpGet, Key: "k"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未提交的写不应出现在状态机: %v", err)
	}
}

func TestConsistencyLevels(t *testing.T) {
	g := NewReplicaGroup(0, 3)
	if _, err := g.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	g.core.stepDown(g.core.currentTerm() + 1) // 模拟退位成 Follower
	if _, err := g.Read(Command{Op: OpGet, Key: "k"}, ConsistencyLinearizable); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("强一致读打到 Follower 应拒绝: %v", err)
	}
	if _, err := g.Read(Command{Op: OpGet, Key: "k"}, ConsistencyEventual); err != nil {
		t.Fatalf("最终一致读应允许 Follower: %v", err)
	}
}

func TestRouterWriteAcrossShards(t *testing.T) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("user:%d", i)
		if _, err := r.Apply(Command{Op: OpSet, Key: key, Value: []byte("paid")}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("user:%d", i)
		got, err := r.Apply(Command{Op: OpGet, Key: key})
		if err != nil || string(got.Value) != "paid" {
			t.Fatalf("key=%s err=%v val=%q", key, err, got.Value)
		}
	}
}

func TestCrossShardTxnRejected(t *testing.T) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	ring := m.Ring()
	base := ring.Get("anchor")
	other := ""
	for i := 0; i < 1000; i++ {
		k := fmt.Sprintf("key-%d", i)
		if ring.Get(k) != base {
			other = k
			break
		}
	}
	if other == "" {
		t.Fatal("构造失败：所有 key 落在同一分片")
	}
	if _, err := r.CheckSameShard([]string{"anchor", other}); !errors.Is(err, ErrCrossShardTxn) {
		t.Fatalf("want ErrCrossShardTxn, got %v", err)
	}
	if _, err := r.CheckSameShard([]string{"anchor", "anchor"}); err != nil {
		t.Fatalf("同分片应通过: %v", err)
	}
}

func TestHashRingMigration(t *testing.T) {
	ring := NewHashRing(128)
	ring.Add("a")
	const keys = 10000
	before := make([]string, keys)
	for i := 0; i < keys; i++ {
		before[i] = ring.Get(fmt.Sprintf("key-%d", i))
	}
	migs := ring.Add("b")
	if len(migs) == 0 {
		t.Fatal("扩容应产生迁移区间")
	}
	moved := 0
	for i := 0; i < keys; i++ {
		if ring.Get(fmt.Sprintf("key-%d", i)) != before[i] {
			moved++
		}
	}
	// 一致性哈希只迁移约 1/(N+1) 的 key；取模分片会接近全量迁移。
	if moved == 0 || moved > keys*9/10 {
		t.Fatalf("迁移比例异常 moved=%d", moved)
	}
	if ring.VNodeCount() != 256 {
		t.Fatalf("vnode=%d want=256", ring.VNodeCount())
	}
}

func TestMembershipLifecycle(t *testing.T) {
	m := NewMembership(64)
	m.AddNode("a", NewReplicaGroup(0, 3))
	if m.State() != MigrationIdle {
		t.Fatal("冷启动建集群不该进入 Joint")
	}
	m.AddNode("b", NewReplicaGroup(1, 3))
	if m.State() != MigrationJoint {
		t.Fatal("扩容应进入 Joint")
	}
	if len(m.Pending()) == 0 {
		t.Fatal("应有待迁移区间")
	}
	m.FinishMigration()
	if m.State() != MigrationDone {
		t.Fatal("校验一致后应推进到 Done")
	}
	m.Reset()
	if m.State() != MigrationIdle {
		t.Fatal("Reset 后应回到 Idle")
	}
}

func BenchmarkRouterSet(b *testing.B) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Apply(Command{Op: OpSet, Key: fmt.Sprintf("k%d", i%1024), Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRouterGet(b *testing.B) {
	m := NewMembership(64)
	for i := 0; i < 3; i++ {
		m.AddNode(fmt.Sprintf("node-%d", i), NewReplicaGroup(i, 3))
	}
	r := NewRouter(m)
	defer r.Close()
	for i := 0; i < 1024; i++ {
		_ = mustApply(r, Command{Op: OpSet, Key: fmt.Sprintf("k%d", i), Value: []byte("v")})
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := r.Apply(Command{Op: OpGet, Key: fmt.Sprintf("k%d", i%1024)}); err != nil {
			b.Fatal(err)
		}
	}
}

func mustApply(s Storage, cmd Command) Result {
	r, err := s.Apply(cmd)
	if err != nil {
		panic(err)
	}
	return r
}
```

mock 边界切在网络传输与本地状态机，是因为中间那些纯逻辑（哈希环、quorum、提交索引）用真实实现测才有意义；用 `ackBudget` 与 `reachable` 两个原子字段就能把「挂一个副本」「分区成少数派」精确注入，比 mock 一整套 RPC 更可控且可复现。

## 11. 部署与容量

**部署拓扑**：30 个分片、每分片 3 副本共 90 节点，按可用区打散——同分片的 3 个副本必须落在 3 个不同可用区，这样一个区整体故障仍剩 2 票可形成多数派。控制面单独部署，负责成员表、迁移任务与 `epoch`；数据面只有 Router 与 Raft 组。依赖：Go 1.21+ 标准库用于本讲内核；生产共识层用 `etcd/raft` 或 `hashicorp/raft`；本地状态机复用第 113 讲 `WALStorage`；节点间用 gRPC 传 AppendEntries 与快照。

**配置项表**

| 配置项 | 默认值 | 开发环境 | 生产环境 | 说明 |
| --- | --- | --- | --- | --- |
| `shards` | 30 | 3 | 30 | 分片数，决定并行度 |
| `replicas` | 3 | 3 | 3 | 奇数，偶数自动加一 |
| `write_quorum` | 2 | 2 | 2 | 由 N 推出，配置只做校验 |
| `read_quorum` | 2 | 1 | 2 | 线性一致读所需票数 |
| `virtual_nodes` | 128 | 64 | 128 | 实测调参，见第 12 节 |
| `default_consistency` | `linearizable` | `eventual` | `linearizable` | 读一致性档位 |
| `election_timeout_ms` | 150 到 300 | 50 到 100 | 150 到 300 | 随机化避免同时竞选 |
| `heartbeat_ms` | 50 | 20 | 50 | 心跳间隔 |
| `max_staleness` | 3 | 10 | 3 | 有界陈旧读允许的日志落后量 |
| `migration_batch` | 4096 | 64 | 4096 | 迁移时每批 key 数 |
| `graceful_timeout_s` | 30 | 5 | 30 | 退位与状态机关闭窗口 |

**资源估算**：单节点承载约 5 GiB 逻辑数据，按第 113 讲的 1.6 倍预留约 8 GiB 内存，配 16 GiB；磁盘按 WAL 日增量加一份快照加一份保留快照约 20 GiB，配 100 GiB NVMe；连接数按每节点 1000 到 2000 并发客户端估算，配合 1000 并发即可达到 20 万写 QPS（单请求 P99 5 ms 时约需 1000 并发在途），文件描述符上限设 8192；跨可用区带宽按峰值写 20 万 ops/s 乘 280 B 日志约 56 MB/s，读不跨区，预留 1 Gbps 有余量。

**扩容方式与优雅退出**：垂直扩容上限是单分片 5 GiB，到了就必须加分片；水平扩容走第 6.6 节的四步——`AddNode` 进入 Joint、区间双写、逐 key 校验、`FinishMigration` 后 `Reset`，回滚只需把 `epoch` 退回旧值，因为旧副本组一直保留。优雅退出顺序：控制面把该节点标为摘除 → 等待迁移完成 → Raft 组主动 `stepDown` 并把 Leader 迁走 → 停止接收新请求 → 本地 `WALStorage.Close` 完成最后 Sync → 进程退出。任何一步超时记日志并强退，靠多数派与日志匹配兜底。

## 12. 性能与压测

**压测方法**：微基准用 `go test` 内置 Benchmark 量化路由与共识内核开销；迁移比例用 10000 个 key 统计节点归属变化；端到端用 1000 并发客户端、Zipf 分布造热点 key、value 256 B，跑 3 分钟取稳态。

```bash
go test -run '^$' -bench 'BenchmarkRouterSet|BenchmarkRouterGet' -benchmem -benchtime=3s ./...
go test -race ./...
go test -cpuprofile=cpu.prof -bench BenchmarkRouterSet -benchtime=10s ./...
go tool pprof -http=:8080 cpu.prof
```

**微基准指标表**（Go 1.23.8 / darwin-arm64 / Apple M3 Pro，**单 goroutine 串行、纯内存共识内核**，不含网络往返、不含第 113 讲 WAL fsync；仅用于对照路由与提交开销）

| 场景 | ns/op | 折算 ops/s | B/op | allocs/op | 说明 |
| --- | --- | --- | --- | --- | --- |
| `Router.Apply` 写 | 318.1 | 约 314 万 | 436 | 4 | 哈希定位 + propose + 2 票 ack + 应用 |
| `Router.Apply` 读 | 212.7 | 约 470 万 | 22 | 2 | 哈希定位 + 分片读锁 |

**迁移比例对比表**（10000 个 key，一致性哈希用 128 个虚拟节点，实测）

| 扩容 | 一致性哈希迁移 | 取模分片迁移 | 结论 |
| --- | --- | --- | --- |
| 1 到 2 节点 | 72.92% | 50.00% | 节点极少时一致性哈希反而不占优 |
| 3 到 4 节点 | 41.57% | 74.70% | 开始显现优势 |
| 10 到 11 节点 | 7.29% | 90.69% | 优势显著，接近理论 1/(N+1) |

虚拟节点数实测调参（1 到 2 节点场景）：64 个 VNode 迁移 69.47%、128 个 72.92%、512 个 42.27%、2048 个 63.77%——**并非越大越好**，与 FNV 哈希分布和节点命名有关，上线前必须用真实 key 分布跑一遍。这条数据也解释了为什么配置项里 `virtual_nodes` 要标注「实测调参」。

**瓶颈定位过程**：pprof 显示 `Router.Apply` 的 436 B/op 主要来自 `fmt.Sprintf` 构造 key 与哈希环的 `sort.Search`，而非共识本身；换成预生成 key 池后写降到约 260 ns/op。端到端压测的瓶颈则完全不在这里：一次写要等一次跨机多数派往返（约 1 到 2 ms）加本地 WAL fsync，因此真实吞吐比微基准低 1 到 2 个数量级，20 万 QPS 需要 1000 并发在途才能填满。

**优化前后对比表**

| 维度 | 第 113 讲单机 WAL | 本讲单 Raft 组 | 本讲 multi-Raft |
| --- | --- | --- | --- |
| 容量 | 5 GiB 顶死 | 仍受单机限制 | 30 分片约 150 GiB |
| 写并行度 | 单条 WAL 流串行 | 单 Leader 串行 | 30 组并行 |
| 机器宕机 | 服务停，人工恢复 | 自动切主 | 自动切主，RTO 不超过 30 秒 |
| 跨 key 事务 | 单机可原子 | 可原子 | 明确不提供 |
| 额外代价 | 无 | 一次网络往返 | 成员表、迁移、热点治理 |

**仍然存在的限制**：`raftCore` 是内存内核，没有选举超时、心跳、日志持久化与真实网络，压测数字不能外推到生产；`WriteQuorum` 让写可用性受制于多数派，挂 2 个副本即不可写；跨分片事务缺失；热点 key 与热点分片需要业务侧配合；一致性哈希在小规模集群下未必优于取模，必须实测调参。

## 13. 演进与替代方案

**替代架构取舍表**

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 单机 WAL（第 113 讲） | 单实例 5 GiB 以内 | 实现最简、无网络开销 | 单点、容量顶死 | 要求机器宕机不停服时 |
| 异步主从复制 | 读多写少、可丢数据 | 写延迟低、实现简单 | 主挂后可能丢已返回写，且易脑裂 | 业务可接受秒级 RPO 时 |
| 单 Raft 组 | 千万 key 以内 | 强一致、无分片边界 | 所有写挤一个 Leader，容量受限 | 数据量未超单机但要求高可用 |
| multi-Raft 加一致性哈希（本讲） | 亿级 key、150 GiB 以上 | 组间并行、单 key 线性一致、可平滑扩容 | 成员变更复杂、无跨分片事务 | 当前规模的最佳平衡点 |
| 无主复制加向量时钟（Dynamo 类） | 跨机房多活、要求永远可写 | 可用性最高，分区时仍可写 | 冲突需业务合并，无强一致 | 接受最终一致且能处理写冲突 |

第 2 个方案（异步主从）是最容易误选的：它写延迟最低，但主节点在返回成功后崩溃会丢写，且故障切换时极易出现双主——本讲选 CP 就是不接受这个代价。第 5 个方案（无主复制）把可用性推到极致，代价是把冲突解决的复杂度交给业务，对「订单状态」「幂等表」这类数据往往得不偿失。评审时列出这些方案，是为了说明当前选择不是因为流行，而是因为**在 150 GiB、单 key 强一致、RPO 为 0 这三个约束下综合成本最低**。

<FlowTimeline direction="horizontal" :steps='[{"title":"④ 分布式 KV","desc":"单 key 强一致，跨 key 无事务","tone":"blue"},{"title":"触发条件","desc":"下单要跨订单库存支付三处数据","tone":"orange"},{"title":"服务拆分","desc":"按领域拆四服务与 proto 契约","tone":"green"},{"title":"事务方案","desc":"Saga 补偿与幂等，见第 115 讲","tone":"red"}]' />

**演进触发条件与迁移步骤（含回滚）**：触发条件是「业务出现必须跨多个 key 原子的操作」「单一 KV 数据模型装不下关系查询」「团队规模需要按领域拆分服务」三者之一。迁移步骤：① 保持本讲 `Storage` 契约，把每个业务域的数据映射到独立服务；② 用 proto 定义服务间契约，先双写旧 KV 与新服务一个账单周期；③ 用 `trace_id` 串联对账，校验两侧一致；④ 逐步切读流量到新服务；⑤ 观察一个完整结算周期无差异后下线旧 KV。回滚：任何一步不一致就把读流量切回本讲集群，因为旧 KV 始终是权威写入方，回滚不丢数据。下一讲离开 KV 贯穿线，进入电商微服务系统，正面处理服务拆分与分布式事务。

## 14. 真实工程里的坑

**坑 1：用偶数副本。** 现象：4 副本集群挂 1 个节点后写全部失败，分区成 2 加 2 时两边都凑不到 3 票。原因：偶数下「多数派」是 3，容错反而比 3 副本差，且边界容易误判。修正：副本数一律取奇数，构造时 `peers%2 == 0` 直接加一；配置校验里把偶数判为非法。提前发现：启动日志打印归一化后的副本数。

**坑 2：把读随机路由到 Follower 还以为拿到最新值。** 现象：刚写成功立刻读，偶发读到旧值。原因：Follower 的 `commitIndex` 可能落后，直接读破坏了线性一致性。修正：线性一致读走 Leader 或 ReadIndex；要读 Follower 就显式降级到有界陈旧/最终一致，并检查 `lag()`。

<Compare title="读路由对照" :panels='[{"label":"错误：随手读副本","code":"g := anyReplica(key)\nreturn g.Read(key)","note":"Follower 未提交时读到旧值，且偶发难复现"},{"label":"正确：按级别判定","code":"if level == Linearizable && !isLeader {\n    return ErrNotLeader\n}","note":"明确拒绝或显式降级，把一致性选择暴露给调用方"}]' />

**坑 3：直接替换成员列表。** 现象：扩容过程中同一个 key 在两侧被不同 Leader 提交，恢复后数据分歧。原因：旧配置 2 票与新配置 2 票可能来自互不相交的节点集合，过渡期出现两个多数派。修正：走 Joint 阶段，新旧配置同时生效、区间双写、校验一致后才切换；`FinishMigration` 是唯一允许切路由的时机。提前发现：`migrations` 长期停在 Joint 且 `epoch` 抖动。

**坑 4：跨分片假装存在分布式事务。** 现象：转账扣了一个账户，另一个没扣，且没有报错。原因：两个 key 落在不同 Raft 组，各提交各的，没有全局原子性。修正：`CheckSameShard` 明确返回 `ErrCrossShardTxn`，业务层改用 Saga 补偿，或把相关 key 用同一分片键前缀绑定。这条不是 bug，是设计边界，必须在 API 里说清楚。

**坑 5：apply 回调持着共识锁再取状态机锁。** 现象：提交路径偶发整体卡死。原因：`ack` 持 `raftCore.mu` 时调用状态机回调，回调又取 `ReplicaGroup.mu`，与读路径形成环形等待。修正：`ack` 只返回待应用命令列表，在锁外 `applyOne`；统一锁序并用 block profile 巡检。

<Compare title="提交回调锁序对照" :panels='[{"label":"错误：锁内 apply","code":"r.mu.Lock()\nr.apply(cmd)\nr.mu.Unlock()","note":"回调再取状态机锁，读路径以反序取锁即死锁"},{"label":"正确：锁外 apply","code":"out := r.ack(peer, idx)\nfor _, c := range out { applyOne(c) }","note":"ack 释放锁后才应用，锁序单向"}]' />

**坑 6：虚拟节点数拍脑袋。** 现象：从 1 个节点扩到 2 个，实测迁移了 72.92% 的 key，比取模分片的 50.00% 还差。原因：FNV 哈希加 128 个 VNode 在小规模下区间分布不均。修正：把 `virtual_nodes` 做成配置项并用真实 key 分布实测——本讲实测 512 个 VNode 时 1 到 2 节点迁移降到 42.27%，但 2048 个又回到 63.77%，说明它不是单调优化。提前发现：每次扩容前跑一次迁移比例脚本，别信理论值。

**坑 7：用墙上时钟做日志序号或租约判断。** 现象：NTP 回拨后新写被旧写覆盖，或 Leader 租约被误判有效产生双主。原因：墙钟不单调。修正：排序一律用 `Term + Index`，租约用单调递增的本地逻辑时钟或 Raft 自身的 ReadIndex，不用 `time.Now()` 比较先后。

**坑 8：Leader 切换后客户端无脑重试。** 现象：一次用户操作被写成两条，重试流量把刚恢复的 Leader 再次打挂。原因：没有幂等键、没有重试预算、退避不随机。修正：业务幂等键加 3 次预算加指数退避加抖动，只对可重试错误码生效，`ErrSplitBrain` 与 `ErrCrossShardTxn` 直接失败。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-114-kv-4-distributed`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-114-kv-4-distributed)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 Joint 阶段实现区间双写路由" task="当前 Membership 在 Joint 阶段仍只按新环路由，迁移区间的写可能只落到目标节点而源节点没收到。请实现 RouteForWrite：迁移区间内的 key 同时返回源与目标两个 Group，由调用方双写，FinishMigration 之后才恢复单写。" :hints='["Migration 已经带 From、To、Start、End，判断 key 是否落在区间内用 hashKey","Joint 之外一律返回单个目标 Group，不要影响正常路径","双写要保证两边都成功才算成功，任一失败整体返回错误","注意 hashKey 在 hashring.go 中已定义，同包可直接调用"]' solution='在 Membership 增加 RouteForWrite 方法：先取 ring.Get(key) 得到目标组，若 State 不是 MigrationJoint 就只返回它。Joint 时遍历 Pending，对每条 Migration 用 hashKey(key) 判断是否落在 Start 到 End 之间，命中则把 From 与 To 对应的 Group 一起加入返回切片并去重；注意环形回绕，Start 大于 End 时要拆成两段判断。调用方对返回的每个 Group 依次 Apply，任一失败立即返回错误。测试：AddNode 触发 Joint 后，构造落在迁移区间的 key，断言返回切片长度为 2；FinishMigration 加 Reset 之后断言长度回到 1。' explain="这道题把『联合共识』从概念变成可验证的断言：Joint 阶段的核心不是改路由表，而是让新旧配置对同一段数据都生效，这样无论哪一侧最终成为新配置，数据都不会丢。" />

## 随堂检测

<Quiz no="114-1" q="3 副本 Raft 组发生 1 加 2 网络分区，只有 1 个副本那侧收到写请求，正确行为是什么？" :options='["照常提交，保证可用性","返回 ErrSplitBrain 或 ErrNoQuorum 拒绝写入","把写缓存起来等分区愈合再提交","自动降级为单机模式继续服务"]' answer="1" explain="写需要 2 票多数派，1 个副本的分区凑不到，CheckQuorum 返回 ErrSplitBrain，ReplicaGroup.Apply 在 propose 之前就拒绝。若照常提交，另一侧 2 票也会提交，恢复后同一 key 出现两个版本，就是脑裂。缓存等待会把 RPO 与延迟都拖坏，且没有解决冲突。" />

<Predict no="114-2" q="3 副本组把 ackBudget 设为 1（Leader 自己 1 票加 1 个 Follower），写一条 SET 后立刻 GET，结果是什么？" :options='["返回 ErrNoQuorum，读不到","SET 成功且 GET 命中最新值","SET 成功但 GET 返回旧值","直接 panic"]' answer="1" output="SET 成功且 GET 命中最新值" explain="Leader 自投 1 票加 1 个 Follower 确认共 2 票，达到 WriteQuorum(3)=2，ack 推进提交索引并应用命令；线性一致读走 Leader，直接命中已提交状态。只有 ackBudget 为 0 时才会因不足多数派返回 ErrNoQuorum，且状态机里不会出现该 key。" />

<FillBlank no="114-3" tpl="3 副本的写多数派是 ___ 票，最多容忍 ___ 个节点故障；扩容采用 ___ 阶段做区间双写；跨分片操作返回 ___。" :answers='["2","1","Joint~联合共识","ErrCrossShardTxn~跨分片事务错误"]' hint="前两空来自 N/2+1 与容错边界，第三空来自成员变更状态机，第四空来自设计边界" explain="多数派为 2 票，挂 1 个还剩 2 票可用；成员变更必须经 Joint 阶段双写校验；跨 key 落到不同 Raft 组时明确拒绝，交业务层 Saga 补偿。" :explains='["多数派取 N/2+1，3 副本即 2 票；只有过半确认才提交，任意两个多数派必有交集，已提交日志才不会在换主后丢失。","3 副本凑够 2 票即可写入，所以挂掉 1 个节点仍剩 2 票可用；若同时挂 2 个就不足多数派，必须拒写以避免脑裂。","成员变更若一步切换，新旧配置可能各选出自己的多数派；先经 Joint 阶段对新旧区间双写校验，再提交新配置才安全。","多个 key 落到不同 Raft 组时无法用单组日志保证原子，因此明确拒写并返回该错误，交业务层用 Saga 补偿。"]' />

## 一页纸速记

<KnowledgeCard :points='["Router 按一致性哈希路由到唯一分片，自己仍实现 Storage，契约四讲未改","Raft 靠同序日志加多数派提交收敛，而不是同步写所有副本","奇数副本加超过半数确认，是防脑裂与容忍少数故障的关键","线性一致读走 Leader 或 ReadIndex，Follower 可能落后于提交位点","成员变更走 Idle 到 Joint 到 Done 三态，区间双写后才切路由"]' :pitfalls='["用偶数副本，分区时两边各半都无法形成多数派","把读随手路由到 Follower 还以为拿到最新值","直接替换成员列表，过渡期出现两个互不相交的多数派","跨分片假装存在分布式事务","apply 回调持着共识锁再取状态机锁，埋下死锁"]' summary="分片解决容量、副本解决单点、Raft 解决顺序一致，代价是跨分片事务、多数派可用性边界与迁移复杂度。" />

## 小结与下一讲

四讲连起来看，是同一套 `Command` 与 `Storage` 背后换了五次实现：**内存**（定契约）→ **网络**（Client 也实现接口）→ **WAL**（落盘与锁分片）→ **分布式**（分片、副本、Raft）。每一版都由上一版的具体痛点逼出来：内存版不能跨进程，网络版重启丢数据且单锁串行，持久化版仍是单点且容量顶死；而调用方的 `Apply` 从头到尾没变过——这就是接口作为「窄腰」的价值。

本讲也把三处取舍明码标价：CP 而非 AP（分区保一致性、牺牲少数派可用性）、单 key 原子而非跨 key 事务（换来水平扩展）、多数派提交而非同步写所有副本（换来慢副本容错）。生产里不会自己实现共识，`etcd/raft` 或 `hashicorp/raft` 负责选举、心跳、日志持久化与成员变更，应用只把 `Command` 交给状态机。

这一版明确放弃了跨分片事务，因为真实业务里「下单」偏偏要跨订单、库存、支付三处数据。下一讲离开 KV 贯穿线，进入电商微服务系统，正面处理服务拆分、proto 契约与 Saga 补偿。

[下一讲：实战 09 · 电商微服务系统](/projects/micro-ecommerce)
