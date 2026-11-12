---
title: 第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式
description: 同一爬虫业务三次架构演进，对比单线程、worker pool 与队列化分布式版在吞吐、去重、限速与失败域上的取舍
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="同一业务三次架构演进" prereq="第 108 讲 · 实战 06 · CSV / SQL 查询工具" />

# 第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式

> 网络爬虫是 Go 并发最经典的练兵场：一次抓取里，本地解析只花几十微秒，等一个 HTTP 往返却要上百毫秒，CPU 大部分时间空转。真实业务里它长这样——某比价平台每天要刷新数百万商品详情页，抽标题、价格与库存；搜索引擎要给几十亿网页建索引；舆情系统要盯着几千个站点更新。抓一百页和抓一千万页不是同一套程序，而是三套系统。本讲让同一个「从种子页出发、抽链接、继续抓」的业务连跑三遍，重点不在把代码写复杂，而在讲清每一次演进是**被哪个具体指标逼出来的**、新版本为此多付了什么代价、以及那些代价该用什么机制兜住。四件事始终互相拉扯：想要吞吐就得并发，想要不被封 IP 就得限速与守规矩，想要不重复不遗漏就得去重，想要崩溃能续跑就得把状态外置——而状态一旦外置，恰好一次就退化成至少一次。看懂这条链，比记住任何一个框架都值钱。

## 0. 项目定位与真实场景

<CheckList id="l109-goals" title="本讲目标" :items='["能说清单线程把时间浪费在哪里、并发为什么能提速","能写对 worker pool 的派发去重、全局限速与 WaitGroup 收尾","能解释进程内状态为什么是分布式演进的根本障碍","能用租约、重投与幂等键处理 worker 崩溃与重复投递","能对着取舍表说清三版各自适用规模与演进触发条件"]' />

业务背景取一个可度量的原型：某电商比价平台，数据团队每天凌晨要抓取 5 万个店铺种子页，展开后约 800 万个候选 URL，其中真正的新详情页约占三成，其余是分页参数变体、排序参数与相互重复的链接。单页均值 80 KB，峰值出向带宽约 300 Mbps；目标站 RTT 中位数 150 毫秒、P99 约 1.2 秒，且明确要求遵守 robots.txt、单域名 QPS 不超过 5。抓下来的标题、价格、库存入库后，供比价引擎与降价告警消费。

现有方案是一个跑在单台机器上的 Python/Go 脚本：串行发请求、把见过的 URL 存在进程内一个集合里、结果直接写文件。它有四个具体痛点，每一个都对应后面一次演进：其一，nightly 窗口 12 小时只跑完约 60%，因为串行时每个请求都要等满 RTT，实测单机 CPU 利用率常年在 5% 以下；其二，重启即从种子全量重来，跑到第 11 小时进程被 OOM 杀掉就前功尽弃，因为没有持久化进度；其三，重复抓取浪费了约 40% 的出向带宽，规范化不到位让 `/item?id=1` 与 `/item?id=1&from=list` 被当成两页；其四，没有统一的礼貌控制，某次压测把并发开到 64 直接被目标站封了整段出口 IP，连累同机房其它业务。

本讲要解决的核心矛盾由此明确：**在「不重复、不遗漏、不封禁、可恢复」四条硬约束下，把吞吐从受单机 RTT 支配，提升到可随机器数线性扩展**。三版分别对应三种规模：V1 单线程版用于几百页的功能校验与基线；V2 单机并发版用于几万页的定时任务；V3 队列化分布式版用于千万页以上、需要跨机横向扩与崩溃续跑的生产场景。三版保持完全相同的抓取语义——同一 URL 抓一次、抽链接继续、结果写同一张表——变的只是承载状态的位置与并发的组织方式。

<Term word="RTT" text="Round-Trip Time，一次请求从发出到收到响应的往返耗时；爬虫里它远大于本地解析耗时，是串行版本吞吐的决定性瓶颈" />

## 1. 需求与约束

先把需求写成可验收的条目，再让架构去满足它们。功能需求用编号固化，每条都给能判定通过与否的标准；非功能指标给出具体数字，后续压测与容量估算都围绕这些数字自洽展开。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 从种子页抓取并抽取链接继续 | 给定含环的站点，能遍历全部可达页且不重复访问 |
| FR-2 | 同一规范化 URL 全局只抓一次 | 800 万候选中重复 URL 全部被去重，误差来自可控的布隆假阳性 |
| FR-3 | 遵守 robots.txt 与单域名 QPS | 被 Disallow 的路径零请求；单域名实测 QPS 不超配额 |
| FR-4 | 失败重试且有重试上限 | 瞬时失败重试至多 3 次，超限进入死信表并告警 |
| FR-5 | 结果可按 URL 查询 | 写库后 GET 结果接口能查到标题与抓取时间 |
| FR-6 | 进程崩溃后可续跑 | 杀掉 worker 后在途任务由租约超时重投，不丢任务 |
| FR-7 | 支持优先级 | 详情页优先级高于列表页，队列出队顺序可验证 |
| FR-8 | 解析与存储可独立替换 | 换解析器或换存储不改抓取层代码 |

| 指标 | 目标值 | 说明与依据 |
| --- | --- | --- |
| 吞吐（V2 单机） | 200 页每秒 | 8 核 16 GB、单域名 QPS 5、多域名并行叠加 |
| 吞吐（V3 集群） | 2000 页每秒 | 10 个 worker 节点，随节点近线性扩展 |
| 单页处理 P99 | 低于 3 秒 | 含 DNS、连接、下载与解析，超时即计一次失败 |
| 数据规模 | 800 万 URL / 日 | 峰值带宽约 300 Mbps |
| 去重一致性 | 最终一致即可 | 允许短暂重复入队，靠幂等键兜底，不追求跨节点强一致 |
| 去重误判率 | 低于 0.1% | 布隆 100 万位、4 个哈希，精确 set 兜住假阳性 |
| 调度面可用性 | 99.5% | 允许换主窗口少跑，不允许重复执行 |
| 恢复目标 RTO | 低于 5 分钟 | 租约 TTL 决定，见第 7 节 |
| 恢复目标 RPO | 等于 0 | 队列与去重外置持久化，不丢已入队任务 |

**明确不做什么**：不做 JavaScript 渲染与无头浏览器，只处理服务端返回静态 HTML 的页面；不做登录态、验证码与反爬对抗；不做正文语义抽取，只抽链接与标题；不做跨数据中心多活。所有示例站点均为自建或已授权抓取的 `httptest` 服务，代码里不写死任何外部真实地址。

**假设边界**：URL 规范化规则固定（去片段、去会话参数、补默认 host、统一小写 path）；目标站 RTT 分布如上，且允许并发；单机 8 核 16 GB、千兆网卡；V3 依赖一个提供「带租约领取 + 确认」语义的队列与一个共享去重存储（生产用 Redis 或 Kafka，示例用进程内等价实现讲清语义）。这些假设一旦不成立——例如目标站要求登录、或 RTT 主要是服务端渲染耗时——本章的取舍结论需要重新评估。

## 2. 架构蓝图

先给 V3 的完整分层，因为它是三版里唯一把状态全部外置、模块边界最清晰的版本；V1 与 V2 可以看作这张图逐步退化的特例。

<ArchBlueprint title="V3 队列化分布式爬虫分层" caption="点击任意模块查看职责、技术选型与失败处理；箭头上的协议标注相邻层的调用方式" :layers='[{"name":"接入与调度层","note":"对外入口与任务编排","protocol":"HTTP/JSON + 内部 channel","items":[{"label":"Seed API","sub":"提交种子与查询","detail":"职责：接收种子 URL 与抓取参数，返回任务号；技术选型：net/http 中间件链，参数校验前置；失败处理：限流触发返回 429 并携带重试标记，任务不入库。","tone":"blue"},{"label":"Frontier 优先队列","sub":"有界待抓队列","detail":"职责：按优先级与深度存放待抓 URL 并控制内存上界；技术选型：container/heap 最小堆加布隆去重；失败处理：队列满时背压到生产端，拒绝新种子而非静默丢任务。","tone":"blue"},{"label":"Scheduler 派发器","sub":"按限速投放","detail":"职责：按每域名配额把任务投给抓取层；技术选型：单 goroutine 编排加 context 取消传播；失败处理：进程退出先停投放再等在途任务收尾，避免产生半途状态。","tone":"blue"}]},{"name":"抓取与治理层","note":"礼貌抓取与连接复用","protocol":"HTTP/1.1 + context 取消","items":[{"label":"Fetcher worker pool","sub":"N 个并发下载","detail":"职责：领取任务、下载页面、产出字节流；技术选型：固定 worker 数加有界 channel，http.Client 带整体超时；失败处理：单次超时或读失败计一次尝试并归还租约，不阻塞其它 worker。","tone":"green"},{"label":"Robots 与限速器","sub":"每域名礼貌控制","detail":"职责：解析 robots 规则并按 host 独立令牌桶限速；技术选型：sync.Mutex 护 host 映射，拿令牌的等待放在锁外；失败处理：robots 拉取失败时按保守拒绝处理，绝不放大速率。","tone":"green"},{"label":"DNS 与连接池","sub":"长连接复用","detail":"职责：复用 TCP/TLS 连接、限制每 host 并发、给 DNS 与响应头设超时；技术选型：http.Transport 调 MaxConnsPerHost 与 IdleConnTimeout；失败处理：单域名连接耗尽时本地排队，不挤占其它域名配额。","tone":"green"}]},{"name":"解析与落地层","note":"解析与存储解耦","protocol":"结构化 Page + 幂等 upsert","items":[{"label":"Parser","sub":"字节流到结构","detail":"职责：抽标题与后续链接，产出统一 Page 结构；技术选型：接口 Parser 隔离正则与 HTML 解析实现；失败处理：解析异常产出空结果并记指标，不重试已下载的页面。","tone":"orange"},{"label":"Result Store","sub":"结果持久化","detail":"职责：按 URL 唯一键幂等写入结果；技术选型：关系表加唯一索引，重复写用 upsert 覆盖；失败处理：写失败回投任务，靠幂等键保证重投不产生重复行。","tone":"orange"},{"label":"死信 DLQ","sub":"重试耗尽隔离","detail":"职责：收容超过重试预算的任务与脏数据；技术选型：独立表带原因与尝试次数字段；失败处理：入队即告警，人工复核后重放或丢弃。","tone":"orange"}]},{"name":"状态与协调层","note":"跨进程共享状态","items":[{"label":"去重 Bloom 加 Redis","sub":"全局 URL 去重","detail":"职责：跨 worker 判定 URL 是否已调度；技术选型：本地布隆快判加 Redis SETNX 精确判定；失败处理：Redis 不可用时降级为只读放行并告警，恢复后对账补录。","tone":"purple"},{"label":"租约 Broker","sub":"在途任务管理","detail":"职责：领取带 TTL 租约、Ack 移除、超时重投；技术选型：队列加 inflight 表，租约存 expiry；失败处理：worker 崩溃由租约超时触发重投，迟到 Ack 校验租约作废。","tone":"purple"},{"label":"配置与指标","sub":"配额与可观测","detail":"职责：集中管理域名配额、重试预算与指标埋点；技术选型：配置文件加环境变量覆盖，原子计数器上报；失败处理：配置非法时启动即失败，不带病运行。","tone":"purple"}]}]' />

逐层解读为什么这样切。**接入与调度层**把「对外协议」与「内部队列」隔开，好处是外部提交方不受内部吞吐波动影响，坏处是多一次序列化与一个需要单独限流的入口。**抓取与治理层**是全系统唯一出网的地方，把限速、robots、连接池三件礼貌相关的事收在一起，是为了让「任何一次 GET 都必须先过这三关」成为一条可被代码强制的约束，而不是靠每个 worker 自觉；拆开它们的代价是抓取层要依赖状态层提供的配置，但换来的是限速语义不会被并发放大。**解析与落地层**用接口把「下载」和「怎么用下载结果」彻底解耦，让换解析器、换存储、加日志都不需要动抓取循环。**状态与协调层**放在最底下，因为它是三版演进的分水岭：V1/V2 里这张层退化成进程内的一张 map，V3 里它变成了可跨进程共享的队列与去重表。

依赖方向严格单向：上层依赖下层暴露的接口，下层绝不允许反向 import 上层。具体到包，抓取层可以调用状态层声明的 `Deduplicator`、`Queue` 接口，但状态层不应该知道自己被谁调用、结果长什么样。这样切的理由很实际——V3 换 Redis 实现时，只要新类型满足同一接口，抓取层一行不改；反过来若状态层里塞了 URL 规范化或页面解析逻辑，任何一次规则调整都要动到底层，且会形成循环依赖，Go 编译器会直接拒绝。

<FlowTimeline :steps='[{"title":"V1 单线程","desc":"一个队列一个循环，串行收发，状态在进程内 map","tone":"blue"},{"title":"V2 并发","desc":"N 个 worker 共享 jobs，seen 加锁，全局一个 ticker 限速","tone":"green"},{"title":"V3 分布式","desc":"队列、去重、限速外置，租约保障崩溃重投，可横向扩","tone":"red"}]' direction="vertical" />

<ConceptDiagram type="mq-flow" title="V3 数据流：投放、领取、抓取、确认、重投" caption="调度器只往队列投放，无状态 worker 领取带租约的任务；租约超时把在途任务放回队列，共享去重挡住重复执行" />

### 三版架构差异：一张明确的取舍表

任务要求把三版的差异写成能直接拿去评审的取舍表，下面这张是本节的核心结论，后续所有实现细节都在解释它。

| 维度 | V1 单线程 | V2 单机 worker pool | V3 队列化分布式 |
| --- | --- | --- | --- |
| 吞吐上限 | 受单请求 RTT 支配，CPU 空等 | 等待重叠，接近 worker 数倍 | 加机器近线性横向扩 |
| 去重载体 | 进程内 map，无需加锁 | 加锁的共享 seen 表 | 本地布隆 + 共享 Redis SETNX |
| 限速方式 | 天然不需要 | 全局共享一个 ticker | 分布式令牌 / 每域名配额 |
| 状态位置 | 全在进程内存 | 全在进程内存 | 队列、去重、限速全部外置 |
| 崩溃后果 | 从头再来 | 从头再来 | 在途任务租约超时重投 |
| 重复执行 | 不会 | 不会 | 可能，必须幂等 |
| 并发复杂度 | 无 | 锁竞争、通道关闭、WaitGroup | 至少一次语义、迟到确认、脑裂 |
| 新增代价 | 无 | seen 加锁、限速必须全局、调试困难 | 引入队列与共享存储、运维成本、幂等设计 |
| 适用规模 | 几百页 | 几万页 | 千万页以上 |

### 演进触发条件

演进不是「技术升级」，而是「旧版的某个指标先撑不住」。把触发条件写成可观测的信号，才不会提前引入用不上的复杂度。

| 演进 | 触发信号（可度量） | 根因 | 动作 | 回滚条件 |
| --- | --- | --- | --- | --- |
| V1 → V2 | 单机 CPU 利用率长期低于 10% 且 nightly 窗口跑不完 | 串行等网络，CPU 空转 | 引入固定 worker pool 与全局限速 | 若限速配置错误导致被封 IP，退回 V1 并调小配额 |
| V2 → V3 | 崩溃后全量重抓、单机内存或网卡触顶、需要跨机扩容 | 状态全在进程内存，是恢复与扩展的单点 | 队列、去重、限速外置，引入租约与幂等键 | 若重投率持续超过 5%，检查 TTL 与超时配置，必要时退回 V2 |

一句话概括取舍：**V1 输在等待，V2 用并行把等待重叠起来却把状态焊死在单机内存，V3 把状态外置换来可恢复与可扩展，代价是从恰好一次退化成必须显式处理的至少一次**。这个代价贯穿第 6、7、8 节，是本讲最需要读懂的地方。

## 3. 模块职责与依赖方向

把分层落到具体模块，明确每个模块的输入、输出、依赖、是否持有状态、以及失败时的行为。这张表同时也是代码评审时划分 owner 的依据。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| Frontier | 规范化 URL、优先级、深度 | 有序待抓任务 | Deduplicator | 有（队列） | 满则背压，拒绝新种子 |
| Scheduler | 配额、时钟 | 投放指令 | Frontier、Queue | 有（配额计数） | 停投放，等在途收尾 |
| Fetcher | 任务字节流请求 | 页面字节流 | Robots、Limiter、Transport | 无 | 超时计一次尝试并归还租约 |
| Robots / Limiter | host、path | 是否放行、等待时长 | 配置 | 有（令牌桶） | 规则拉取失败保守拒绝 |
| Parser | 页面字节流 | Page 结构 | 无 | 无 | 产出空结果并记指标 |
| Deduplicator | URL | 是否首次 | 共享存储 | 有（去重表） | 存储不可用降级放行并告警 |
| Broker / Queue | 领取、Ack | 带租约任务 | 持久化队列 | 有（inflight） | 超时重投，迟到 Ack 作废 |
| Result Store | Page | 落库结果 | 数据库 | 有（结果表） | 写失败回投任务 |
| DLQ | 超限任务 | 死信记录 | 告警 | 有 | 入队即告警待人工 |

包目录树按「领域 + 分层」组织，接口定义集中在 `contract`，实现按模块分文件，抓取入口单独放 `cmd`，便于三版共用同一套契约。

```text
crawler/
├── cmd/
│   └── crawld/main.go          # 装配入口，按配置选 V1/V2/V3
├── internal/
│   ├── contract/contract.go    # 全部接口与跨层消息帧（§4）
│   ├── frontier/
│   │   ├── priority.go         # container/heap 优先队列（§6.5）
│   │   └── dedup.go            # 布隆 + 精确 set（§6.2）
│   ├── fetch/
│   │   ├── client.go           # Transport、DNS、连接池（§6.3）
│   │   ├── robots.go           # robots 判定与限速（§6.3）
│   │   └── pool.go             # worker pool（§6.4）
│   ├── parse/parser.go         # Parser 实现
│   ├── broker/
│   │   ├── lease.go            # 租约、Ack、重投（§6.6）
│   │   └── dlq.go              # 死信
│   ├── store/store.go          # 幂等 upsert
│   ├── pipeline/pipeline.go    # 解析与存储解耦（§6.7）
│   └── metrics/metrics.go      # 埋点（§9）
└── go.mod
```

依赖倒置的锚点是 `contract` 包里的小接口：高层模块只依赖 `Fetcher`、`Parser`、`Store`、`Deduplicator`、`Queue`，不依赖 `net/http`、`Redis` 或具体数据库。具体实现的装配放在 `cmd`，这样 `go test` 时能注入内存替身。完整签名见第 4 节。

<Callout type="warn" title="边界划分最常见的错误" text="把去重逻辑直接塞进 Fetcher 的抓取循环里，用一张进程内 map 就地判重。V2 看着没问题，可一旦要演进到 V3 换 Redis，就得改动每个 worker 的代码，且抓取层被迫依赖具体存储客户端。正确做法是让 Fetcher 只依赖 Deduplicator 接口，判重与标记作为一个原子操作放在去重模块里，换实现只动装配层。" />

## 4. 数据与协议契约

跨模块、跨进程传递的数据必须先定契约再写实现。下面给出结果表、去重表、死信表与队列表的 DDL，字段语义与零值规则，以及对外 API 与内部接口签名。

```sql
CREATE TABLE pages (
    url          VARCHAR(2048) PRIMARY KEY,
    title        TEXT        NOT NULL,
    status       INT         NOT NULL DEFAULT 200,
    fetched_at   TIMESTAMP   NOT NULL,
    trace_id     VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX idx_pages_url ON pages(url);

CREATE TABLE seen_urls (
    url_hash     CHAR(64)    PRIMARY KEY,
    url          VARCHAR(2048) NOT NULL,
    first_seen   TIMESTAMP   NOT NULL
);

CREATE TABLE dead_letters (
    id           BIGINT PRIMARY KEY AUTO_INCREMENT,
    url          VARCHAR(2048) NOT NULL,
    reason       VARCHAR(255) NOT NULL,
    attempts     INT         NOT NULL,
    created_at   TIMESTAMP   NOT NULL
);

CREATE TABLE frontier (
    url          VARCHAR(2048) PRIMARY KEY,
    priority     INT         NOT NULL DEFAULT 5,
    depth        INT         NOT NULL DEFAULT 0,
    attempts     INT         NOT NULL DEFAULT 0,
    lease_until  BIGINT      NOT NULL DEFAULT 0
);
```

`pages.url` 与 `seen_urls.url_hash` 的唯一约束就是幂等键：同一 URL 重复写用 upsert 覆盖，绝不会产生第二行。`lease_until` 用 UnixNano 整数而非本地时间字符串，避免各节点时钟语义歧义——这一点在分布式版里尤其重要，节点间不做时钟同步假设。

跨进程传递的任务帧用 JSON（生产可换 protobuf 省带宽），字段必须可序列化、有明确零值语义。`attempt`、`epoch`、`trace_id` 是新增字段但都允许零值，保证 v1 消费者遇到 v2 消息不崩。

```json
{
  "url": "https://shop.example/item/9527",
  "attempt": 0,
  "priority": 0,
  "depth": 2,
  "trace_id": "t-2026-001",
  "epoch": 7,
  "expires_nano": 0
}
```

| 对外 API | 方法 | 入参 | 返回 | 幂等性 |
| --- | --- | --- | --- | --- |
| /crawl | POST | seeds 数组、配额 | task_id | 相同 seeds 重复提交产生新任务号 |
| /result | GET | url | Page 或 404 | 天然幂等 |
| /stats | GET | 无 | 各计数器快照 | 幂等 |

```json
{
  "task_id": "t-2026-001",
  "accepted": 50000,
  "queued": 49812,
  "rejected_duplicate": 188
}
```

**字段语义与兼容规则**：`attempt` 从 0 起，每次重投加一，达到 `max_try` 进死信；`epoch` 标识队列时代，换主或重启后递增，用于丢弃上一时代迟到的确认；`trace_id` 贯穿一次抓取全链路，供日志串联。所有新增字段必须有零值且不影响旧消费者，这就是灰度策略——先让消费者升级为「忽略未知字段」，再灰度生产者加字段，最后才依赖新字段做逻辑。回滚时旧消费者读到的新字段被忽略，不会出错。

内部接口签名是依赖倒置的落点，`package main` 便于单独编译自检：

```go
package main

// 本文件集中声明爬虫各层的接口契约，是依赖倒置的锚点：
// 高层模块只依赖接口，不依赖 Redis 或 net/http 的具体实现。

import "context"

// Frontier 是待抓 URL 的有界优先队列。
type Frontier interface {
	Push(url string, priority, depth int) bool
	Pop() (string, int, bool)
	Len() int
}

// Deduplicator 判定并标记 URL，Visit 必须是原子操作。
type Deduplicator interface {
	Visit(url string) bool
}

// Fetcher 下载单个 URL，必须尊重 ctx 取消与超时。
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// Store 持久化结果，Save 必须幂等：同一 url 重复写不产生重复行。
type Store interface {
	Save(ctx context.Context, url string, title string) error
}

// Queue 是 V3 分布式队列：领取带租约，确认后移除，超时可重投。
type Queue interface {
	Push(t TaskMsg) error
	Lease(ctx context.Context) (LeaseMsg, bool, error)
	Ack(id int64) (bool, error)
}

// TaskMsg 与 LeaseMsg 是跨进程帧，字段保持可序列化且有零值语义。
type TaskMsg struct {
	URL     string
	Attempt int
	TraceID string
	Epoch   int64
}

type LeaseMsg struct {
	ID      int64
	Task    TaskMsg
	Expires int64 // UnixNano，消除节点本地时钟歧义
}

func main() {}
```

<Compare title="任务帧两种契约设计" :panels='[{"label":"瘦消息：只带 URL","code":"type TaskA struct {\n  URL string\n}","note":"消费端收到后查库补全深度、优先级、配额。消息小、带宽省，但每次消费多一次远程查询，且查库与消费之间状态可能已变。"},{"label":"自描述消息：带全量元数据","code":"type TaskB struct {\n  URL string\n  Depth int\n  Priority int\n  TraceID string\n  Epoch int64\n}","note":"消费端无需回查，天然适合重投与故障转移；代价是消息变大、字段演进要管版本兼容。V3 选它，因为崩溃重投时需要完整上下文。"}]' />

V3 选「自描述消息」的理由很直接：故障转移时任务会被另一个 worker 重新领取，若消息只带 URL，新 worker 无法知道这是第几次尝试、属于哪个 epoch、优先级是多少；而这些恰恰是幂等判定与限速所必需的。多出来的带宽相比一次跨机房重查，代价更小、语义更安全。

<Callout type="tip" title="契约演进的灰度顺序" text="先升级所有消费者为忽略未知字段，再灰度生产者加新字段，最后才让逻辑依赖新字段。这样任何一步回滚，旧节点读到新字段都不会崩，是真的可灰度。" />

## 5. 核心链路时序

下面这张时序图把 V3 一次正常抓取的完整链路画清楚：既有同步调用、也有返回、自处理与异步消息，参与方覆盖接入、调度、队列、执行与存储。

<SequenceDiagram title="V3 正常抓取链路" caption="领取带租约、确认才移除；未在 TTL 内 Ack 的任务由 Broker 自处理重投，形成至少一次语义" :actors='["Client","Scheduler","Frontier","Broker","Worker","Store"]' :steps='[{"from":"Client","to":"Scheduler","label":"提交种子","note":"带 trace_id","kind":"call"},{"from":"Scheduler","to":"Frontier","label":"规范化入队","note":"去参数补 host","kind":"call"},{"from":"Frontier","to":"Frontier","label":"布隆查重","note":"命中即丢弃","kind":"self"},{"from":"Worker","to":"Broker","label":"领取任务","note":"带租约","kind":"call"},{"from":"Broker","to":"Worker","label":"返回任务","note":"含 lease_id","kind":"return"},{"from":"Worker","to":"Worker","label":"限速后抓取","note":"GET 加解析","kind":"self"},{"from":"Worker","to":"Store","label":"写结果","note":"幂等 upsert","kind":"async"},{"from":"Worker","to":"Broker","label":"Ack 确认","note":"带 lease_id","kind":"call"},{"from":"Broker","to":"Broker","label":"超时重投","note":"未确认且过期","kind":"self"},{"from":"Store","to":"Client","label":"返回结果","note":"GET result","kind":"return"}]' />

逐步骤解读。第 1 步客户端提交种子，`trace_id` 在此生成并贯穿全链路；第 2 步 Scheduler 做 URL 规范化（去片段、去会话参数、补 host），这是 FR-2 去重正确的前提，规范化不彻底会让同一页以多种形态反复入队；第 3 步 Frontier 用布隆快判挡掉绝大多数重复，命中即丢弃，不产生队列压力。第 4、5 步是 V3 与 V2 最本质的区别：worker 主动向 Broker 领取，Broker 返回一个**带租约**的任务，此刻任务从「待处理」变成「在途」，TTL 开始计时。第 6 步 worker 先过限速器令牌、再发 GET、再解析，全部在本地完成。第 7 步写结果是异步的（不必阻塞确认链路），由 Store 用唯一键幂等落库。第 8 步 Ack 是整个链路的提交点：只有成功确认，任务才从 inflight 移除。第 9 步是 Broker 的后台自处理，扫描超时未确认的租约并重投，第 10 步客户端按 URL 查结果。

<Term word="租约 lease" text="领取任务时附带的一个带过期时间的凭证；持有者必须在 TTL 内确认，超时未确认则凭证失效、任务可被重新领取，是故障转移不丢任务的核心机制" />

**异常链路：worker 抓取超时或进程崩溃。** 它与正常链路的分叉点在第 8 步 Ack 没有发生。假设 worker 在第 6 步因目标站 P99 抖动卡住、或就在第 6 步之后被 OOM 杀掉，那么 inflight 里这条租约永远等不到 Ack。Broker 的后台扫描（第 9 步）在 `now > expiry` 时把任务放回 pending，`attempt` 加一，另一个 worker 重新领取。这里有两个必须处理的细节：若原 worker 其实只是慢、并没有死，它稍后补发的迟到 Ack 必须被作废，否则会把别人正在处理的新租约误删；若重投后任务被重复执行，Store 的唯一键与 done 去重表要保证结果不重复。第 6.6 节的 `Ack` 会同时演示这两点。整个异常链路的兜底逻辑是：**租约负责「不丢」，幂等键负责「不重」，两者合起来才把至少一次语义收敛成业务上的恰好一次**。

## 6. 核心实现（按模块逐段展开）

以下每段都是一个聚焦单一模块的最小可运行程序（`package main`，可单独 `go run` 或 `go test` 自检）；生产里它们按第 3 节目录树拆进 `internal` 各包，接口签名在第 4 节统一。每段前面写清设计意图与关键取舍，段后配必要的逐行讲解或输出。

### 6.1 领域模型与 V1 单线程基线

**设计意图**：先把基线立住——一个队列、一个循环、一次处理一个 URL，用来对照后面每一版的加速比与语义等价性。**关键取舍**：解析器抽成 `Parser` 接口，让「下载」与「怎么用下载结果」从第一行起就解耦；用 `io.LimitReader` 给单页字节数设上限，防止异常大响应把内存打爆；失败页直接跳过，单线程版不需要重试 machinery。

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
)

// Task 是抓取任务的最小契约：规范化 URL 与抓取深度。
type Task struct {
	URL   string
	Depth int
}

// ParseResult 是解析器输出：本页标题与抽出的后续链接。
type ParseResult struct {
	Title string
	Links []string
}

// Parser 把下载与解析解耦，抓取层只依赖这个接口。
type Parser interface {
	Parse(body []byte) ParseResult
}

type linkParser struct{ re *regexp.Regexp }

func newLinkParser() *linkParser {
	return &linkParser{re: regexp.MustCompile(`href="(/[a-zA-Z0-9/_-]*)"`)}
}

func (p *linkParser) Parse(body []byte) ParseResult {
	var out ParseResult
	out.Title = "page"
	for _, m := range p.re.FindAllSubmatch(body, -1) {
		out.Links = append(out.Links, string(m[1]))
	}
	return out
}

// CrawlSingle 是 V1 基线：单队列、单循环，一次只处理一个 URL。
func CrawlSingle(base string, client *http.Client, parser Parser, max int, maxBody int64) []string {
	seen := map[string]bool{}
	queue := []Task{{URL: "/"}}
	var visited []string
	for len(queue) > 0 && len(visited) < max {
		t := queue[0]
		queue = queue[1:]
		if seen[t.URL] {
			continue
		}
		seen[t.URL] = true
		resp, err := client.Get(base + t.URL)
		if err != nil || resp.StatusCode != http.StatusOK {
			continue // 单线程版：一页失败就跳过，不影响后续
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		resp.Body.Close()
		visited = append(visited, t.URL)
		for _, link := range parser.Parse(body).Links {
			if !seen[link] {
				queue = append(queue, Task{URL: link, Depth: t.Depth + 1})
			}
		}
	}
	return visited
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a><a href="/b">b</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if body, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	visited := CrawlSingle(srv.URL, srv.Client(), newLinkParser(), 10, 1<<20)
	fmt.Println("V1 抓取集合:", strings.Join(visited, " "), "总数", len(visited))
}
```

<OutputBox title="V1 运行结果" output="V1 抓取集合: / /a /b /d 总数 4" note="顺序稳定、结果可复现；/d 被 /a 与 /b 同时链接，靠 seen 挡住只抓一次" />

这版没有并发，顺序稳定但抓一万个页面要串行走完上千秒，瓶颈全在等待网络。它是后面所有版本的正确性基准：V2、V3 抓到的 URL 集合必须与它一致。

### 6.2 URL 去重：布隆过滤器与原子 Visit

**设计意图**：去重是「不重复」的守门员，且是 V2→V3 唯一必须换掉实现的模块。**关键取舍**：用布隆过滤器做 O(1) 快判挡掉绝大多数重复，再用精确 `map` 兜住哈希碰撞造成的假阳性——布隆说「没见过」一定没见过，说「见过」则可能误判，所以不能直接当结论用。更重要的是把「判定」和「标记」合并成一个 `Visit` 原子操作并加锁，否则并发下经典的 check-then-act 会让同一个 URL 被多个 worker 同时放行。

```go
package main

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// Deduplicator 是去重层抽象：Visit 返回 true 表示首次出现、应调度。
type Deduplicator interface {
	Visit(url string) bool
}

// BloomFilter 是固定容量布隆过滤器：位图加 k 个哈希。
type BloomFilter struct {
	bits []uint64
	m    uint64
	k    uint64
}

func NewBloomFilter(m, k uint64) *BloomFilter {
	return &BloomFilter{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *BloomFilter) hashes(s string) (uint64, uint64) {
	h1 := fnv.New64a()
	_, _ = h1.Write([]byte(s))
	a := h1.Sum64()
	h2 := fnv.New64()
	_, _ = h2.Write([]byte(s))
	c := h2.Sum64() | 1 // 保证步进为奇数，减少退化
	return a, c
}

func (b *BloomFilter) Add(s string) {
	a, c := b.hashes(s)
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		b.bits[idx/64] |= 1 << (idx % 64)
	}
}

func (b *BloomFilter) MightContain(s string) bool {
	a, c := b.hashes(s)
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		if b.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

// URLDedup 组合布隆快判与精确 set，用一把锁包住整个 Visit。
type URLDedup struct {
	mu    sync.Mutex
	bloom *BloomFilter
	exact map[string]struct{}
}

func NewURLDedup() *URLDedup {
	return &URLDedup{bloom: NewBloomFilter(1<<20, 4), exact: map[string]struct{}{}}
}

// Visit 是唯一入口：判定与标记在同一临界区，避免重复入队。
func (d *URLDedup) Visit(s string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.bloom.MightContain(s) {
		if _, ok := d.exact[s]; ok {
			return false
		}
	}
	d.bloom.Add(s)
	d.exact[s] = struct{}{}
	return true
}

var _ Deduplicator = (*URLDedup)(nil)

func main() {
	d := NewURLDedup()
	for _, u := range []string{"/a", "/b", "/a", "/c", "/b"} {
		if d.Visit(u) {
			fmt.Println("首次调度:", u)
		} else {
			fmt.Println("重复丢弃:", u)
		}
	}
}
```

<LineByLine title="Visit 为什么必须原子" code="func (d *URLDedup) Visit(s string) bool {\nd.mu.Lock()\ndefer d.mu.Unlock()\nif d.bloom.MightContain(s) {\nif _, ok := d.exact[s]; ok {\nreturn false\n}\n}\nd.bloom.Add(s)\nd.exact[s] = struct{}{}\nreturn true\n}" :notes='["函数签名：返回 true 表示首次出现","进入临界区，判定与标记将原子完成","defer 保证任何返回路径都解锁","先问布隆：说没见过就一定没见过，可直接放行","布隆说见过时再查精确表，挡住假阳性","精确表命中，这是真重复，丢弃","闭合布隆分支","未命中则把 s 记入布隆位图","同时记入精确 set，二者状态一致","返回 true，调用方据此入队"]' />

V3 里 `URLDedup` 的本地位图会换成「本地布隆 + Redis SETNX」两层：布隆在进程内挡掉绝大部分重复，只有布隆说「可能是新的」才去 Redis 做一次原子 SETNX，成功者才算真正首次。这样既省了海量跨机请求，又保证了跨 worker 的全局唯一。

### 6.3 礼貌抓取：robots、每域名限速与 DNS 连接池

**设计意图**：把「任何一次 GET 必须先过三关」做成代码里的硬约束。**关键取舍**：限速器按 host 维护独立令牌桶，而不是全局一个——慢域名不该拖累快域名；拿令牌的等待放在锁外，避免一个阻塞的 host 把整张映射锁死。`http.Transport` 显式配置 `MaxConnsPerHost`、`IdleConnTimeout`、`ResponseHeaderTimeout`，既复用长连接又给 DNS 与响应头分别设超时，防止个别慢域名占满连接池。robots 拉取失败时按保守拒绝处理，宁可少抓也绝不放大速率。

```go
package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RobotRules 保存单 host 的禁止前缀；生产需完整解析 Allow/Disallow。
type RobotRules struct {
	disallow []string
	delay    time.Duration
}

func (r RobotRules) Allowed(path string) bool {
	for _, p := range r.disallow {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	return true
}

// TokenBucket 是每域名独立限速：容量等于每秒补充速率。
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64
	last     time.Time
}

func NewTokenBucket(rate float64) *TokenBucket {
	return &TokenBucket{tokens: rate, capacity: rate, rate: rate, last: time.Now()}
}

// Wait 阻塞到拿到一枚令牌，等待放在锁外循环里。
func (b *TokenBucket) Wait() time.Duration {
	for {
		b.mu.Lock()
		now := time.Now()
		b.tokens = min(b.capacity, b.tokens+now.Sub(b.last).Seconds()*b.rate)
		b.last = now
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return 0
		}
		wait := time.Duration((1 - b.tokens) / b.rate * float64(time.Second))
		b.mu.Unlock()
		time.Sleep(wait)
	}
}

// HostLimiter 按 host 维护令牌桶与 robots 规则。
type HostLimiter struct {
	mu      sync.Mutex
	buckets map[string]*TokenBucket
	rules   map[string]RobotRules
	qps     float64
}

func NewHostLimiter(qps float64) *HostLimiter {
	return &HostLimiter{buckets: map[string]*TokenBucket{}, rules: map[string]RobotRules{}, qps: qps}
}

func (h *HostLimiter) SetRules(host string, r RobotRules) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rules[host] = r
}

func (h *HostLimiter) Acquire(host, path string) bool {
	h.mu.Lock()
	if r, ok := h.rules[host]; ok && !r.Allowed(path) {
		h.mu.Unlock()
		return false // 被 robots 禁止
	}
	b, ok := h.buckets[host]
	if !ok {
		b = NewTokenBucket(h.qps)
		h.buckets[host] = b
	}
	h.mu.Unlock()
	b.Wait() // 锁外等待，不阻塞其它 host
	return true
}

// BuildClient 配置连接池与 DNS，给整条链路设超时边界。
func BuildClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func main() {
	limiter := NewHostLimiter(5)
	limiter.SetRules("shop.example", RobotRules{disallow: []string{"/admin", "/cart"}, delay: time.Second})
	client := BuildClient(8 * time.Second)
	fmt.Println("单请求超时:", client.Timeout)
	for _, path := range []string{"/item/1", "/admin/secret", "/item/2"} {
		ok := limiter.Acquire("shop.example", path)
		fmt.Printf("path=%s 允许=%v\n", path, ok)
	}
}
```

<Callout type="idea" title="为什么限速必须全局或按 host 共享" text="若给每个 worker 各发一个 time.NewTicker，N 个 worker 的实际速率会放大到配额的 N 倍——这是 V2 最隐蔽也最容易被封 IP 的坑。正确做法是全局一个 ticker 或每 host 一个令牌桶，让所有 worker 共享同一份配额。第 14 节会把错误与正确写法摆在一起。" />

### 6.4 V2 并发：worker pool、派发去重、取消传播

**设计意图**：用固定数量 worker 把网络等待重叠起来，同时保证「同一 URL 只抓一次」和「所有任务结束后干净收尾」。**关键取舍**：`seen` 在**派发时**标记而非抓取完成时，否则两个 worker 可能同时抓到同一新链接；`wg.Add(1)` 必须在对应 `Done` 之前，且发送 `jobs` 放在解锁之后避免持锁等待；`handle` 用 `defer wg.Done()` 包住单次处理，保证取消或失败路径都不漏计数，否则 `wg.Wait()` 永不返回、通道永不关闭、主流程死锁。解析结果走独立 `results` 通道，实现解析与存储解耦。

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"time"
)

var re = regexp.MustCompile(`href="(/[a-z0-9]*)"`)

// Pool 是 V2 并发版：N 个 worker 共享 jobs 与一个全局限速器。
type Pool struct {
	client  *http.Client
	base    string
	mu      sync.Mutex
	seen    map[string]bool
	jobs    chan string
	results chan string
	wg      sync.WaitGroup
	limiter <-chan time.Time
	ticker  *time.Ticker
	max     int
}

func NewPool(base string, client *http.Client, max, workers int, rate time.Duration) *Pool {
	t := time.NewTicker(rate)
	return &Pool{
		client:  client,
		base:    base,
		seen:    make(map[string]bool),
		jobs:    make(chan string, workers*4),
		results: make(chan string, workers*4),
		ticker:  t,
		limiter: t.C,
		max:     max,
	}
}

// enqueue 在派发时就把 URL 记为已调度，避免同一 URL 多次入队。
func (p *Pool) enqueue(path string) {
	p.mu.Lock()
	if p.seen[path] || len(p.seen) >= p.max {
		p.mu.Unlock()
		return
	}
	p.seen[path] = true
	p.wg.Add(1) // Add 必须在对应 Done 之前
	p.mu.Unlock()
	p.jobs <- path // 发送放在锁外
}

// handle 用 defer 保证任意路径都归还计数。
func (p *Pool) handle(ctx context.Context, path string) {
	defer p.wg.Done()
	select {
	case <-ctx.Done():
		return
	case <-p.limiter: // 所有 worker 共享一个 ticker，全局限速
	}
	resp, err := p.client.Get(p.base + path)
	if err != nil {
		return // 部分失败：本页跳过，但计数已归还
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		p.enqueue(m[1])
	}
	select {
	case p.results <- path:
	case <-ctx.Done():
	}
}

func (p *Pool) worker(ctx context.Context) {
	for path := range p.jobs {
		p.handle(ctx, path)
	}
}

func (p *Pool) Run(ctx context.Context, seed string, workers int) []string {
	p.enqueue(seed)
	for i := 0; i < workers; i++ {
		go p.worker(ctx)
	}
	go func() {
		p.wg.Wait() // 归零意味着系统 quiescent，可以关闭
		close(p.jobs)
		close(p.results)
	}()
	var visited []string
	for r := range p.results {
		visited = append(visited, r)
	}
	p.ticker.Stop()
	return visited
}

func main() {
	pages := map[string]string{
		"/":  `<a href="/a">a</a><a href="/b">b</a>`,
		"/a": `<a href="/d">d</a>`,
		"/b": `<a href="/d">d</a>`,
		"/d": `end`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, b)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := NewPool(srv.URL, srv.Client(), 10, 4, time.Millisecond)
	visited := p.Run(context.Background(), "/", 4)
	fmt.Println("V2 抓取数:", len(visited), "（顺序不固定）")
}
```

这里 `wg` 计数等于「已入队未 Done」的任务数。worker 在 `handle` 的 `defer Done` 之前完成 `enqueue`（`Add`），所以只要还有 worker 在处理，计数就至少为 1，`wg.Wait()` 不会因队列短暂见底而提前归零——这是深度抓取 worker pool 不误关通道的关键前提。

### 6.5 优先级队列：让详情页先于列表页

**设计意图**：抓取预算有限时，先抓高价值页面。**关键取舍**：用 `container/heap` 实现最小堆，优先级数值越小越先出；配一张 `pos` 索引表支持按 URL 做 `Upsert`，发现更高优先级时原地 `heap.Fix`，避免同一 URL 重复入队。深度字段用于抑制无限下钻：超过阈值的链接直接不入队。

```go
package main

import (
	"container/heap"
	"fmt"
)

// Item 是队列元素：Priority 越小越先出，Depth 抑制无限下钻。
type Item struct {
	URL      string
	Priority int
	Depth    int
	index    int
}

// PriorityQueue 用最小堆实现，支持按 URL 更新优先级。
type PriorityQueue struct {
	items []*Item
	pos   map[string]*Item
}

func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{pos: map[string]*Item{}}
	heap.Init(pq)
	return pq
}

func (pq *PriorityQueue) Len() int           { return len(pq.items) }
func (pq *PriorityQueue) Less(i, j int) bool { return pq.items[i].Priority < pq.items[j].Priority }
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}
func (pq *PriorityQueue) Push(x any) {
	it := x.(*Item)
	it.index = len(pq.items)
	pq.items = append(pq.items, it)
	pq.pos[it.URL] = it
}
func (pq *PriorityQueue) Pop() any {
	old := pq.items
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	pq.items = old[:n-1]
	delete(pq.pos, it.URL)
	return it
}

// Upsert 首次入队或发现更高优先级时更新，不产生重复元素。
func (pq *PriorityQueue) Upsert(url string, priority, depth int) {
	if it, ok := pq.pos[url]; ok {
		if priority < it.Priority {
			it.Priority = priority
			heap.Fix(pq, it.index)
		}
		return
	}
	heap.Push(pq, &Item{URL: url, Priority: priority, Depth: depth})
}

func (pq *PriorityQueue) PopTop() (Item, bool) {
	if pq.Len() == 0 {
		return Item{}, false
	}
	return *heap.Pop(pq).(*Item), true
}

func main() {
	pq := NewPriorityQueue()
	pq.Upsert("/checkout", 9, 3)
	pq.Upsert("/home", 1, 0)
	pq.Upsert("/list", 5, 1)
	pq.Upsert("/home", 0, 0) // 更高优先级，原地更新
	for {
		it, ok := pq.PopTop()
		if !ok {
			break
		}
		fmt.Printf("出队 url=%s 优先级=%d 深度=%d\n", it.URL, it.Priority, it.Depth)
	}
}
```

### 6.6 V3 分布式 Broker：租约、重投、幂等、死链

**设计意图**：把 V2 那张进程内 `seen`/`pending` 外置成可跨进程共享的队列语义。**关键取舍**：领取任务时创建带 `Expiry` 的租约放进 `inflight`；`Ack` 必须先检查租约仍在 inflight，让崩溃 worker 的迟到确认作废；`Lease` 每次先扫描超时租约，尝试次数未达上限就 `attempt++` 重投，超限则进死信队列——死链是 FR-4 的兜底，避免一个永久 404 的 URL 被无限重试打爆队列。`done` 表用 `url -> epoch` 做幂等判定，重复投递在写入端被挡住。

```go
package main

import (
	"fmt"
	"time"
)

type Task struct {
	URL     string
	Attempt int
}

type Lease struct {
	ID     int64
	Task   Task
	Expiry time.Time
	Epoch  int64
}

// Broker 是 V3 队列核心：待处理、在途租约、完成去重表、死信。
type Broker struct {
	pending  []Task
	inflight map[int64]Lease
	done     map[string]int64 // url -> 完成时 epoch
	dead     []Task
	nextID   int64
	epoch    int64
	ttl      time.Duration
	maxTry   int
}

func NewBroker(ttl time.Duration, maxTry int) *Broker {
	return &Broker{
		inflight: map[int64]Lease{},
		done:     map[string]int64{},
		ttl:      ttl,
		maxTry:   maxTry,
	}
}

// Push 入队前先看去重表，已完成的任务不再入队。
func (b *Broker) Push(t Task) {
	if _, ok := b.done[t.URL]; ok {
		return
	}
	b.pending = append(b.pending, t)
}

// Lease 领取任务：先把超时未确认的租约重投，再取队首。
func (b *Broker) Lease() (Lease, bool) {
	now := time.Now()
	for id, l := range b.inflight {
		if now.After(l.Expiry) {
			delete(b.inflight, id)
			if l.Task.Attempt >= b.maxTry {
				b.dead = append(b.dead, l.Task) // 进死信
				continue
			}
			l.Task.Attempt++
			b.pending = append(b.pending, l.Task)
			fmt.Println("  租约超时重投:", l.Task.URL, "第", l.Task.Attempt, "次")
		}
	}
	if len(b.pending) == 0 {
		return Lease{}, false
	}
	t := b.pending[0]
	b.pending = b.pending[1:]
	b.nextID++
	l := Lease{ID: b.nextID, Task: t, Expiry: now.Add(b.ttl), Epoch: b.epoch}
	b.inflight[l.ID] = l
	return l, true
}

// Ack 确认：租约必须仍在 inflight，且写 done 幂等判定。
func (b *Broker) Ack(l Lease) bool {
	if _, ok := b.inflight[l.ID]; !ok {
		return false // 迟到确认作废
	}
	delete(b.inflight, l.ID)
	if prev, ok := b.done[l.Task.URL]; ok && prev >= l.Epoch {
		return false // 重复投递，幂等键挡住
	}
	b.done[l.Task.URL] = l.Epoch
	return true
}

func (b *Broker) Drain() {
	for {
		l, ok := b.Lease()
		if !ok {
			break
		}
		b.Ack(l)
	}
}

func main() {
	b := NewBroker(20*time.Millisecond, 3)
	for _, u := range []string{"/a", "/b", "/a"} {
		b.Push(Task{URL: u})
	}
	fmt.Println("入队后待处理:", len(b.pending))
	l1, _ := b.Lease()
	fmt.Println("worker1 领取:", l1.Task.URL, "（模拟崩溃，不 Ack）")
	time.Sleep(25 * time.Millisecond)
	b.Drain()
	fmt.Println("已完成:", len(b.done), "死信:", len(b.dead))
}
```

这段跑完 V3 最容易跳过的一条链：领取不确认、租约超时、重投、重新领取并确认。`Ack` 检查租约是否仍在 inflight，是迟到确认作废的关键；`done` 表则保证重投带来的重复执行在结果侧收敛。

### 6.7 解析与存储解耦：可运行的主流程 Pipeline

**设计意图**：证明抓取层、解析层、存储层可以各自独立替换。**关键取舍**：三段用有界 channel 串成 pipeline，下载与解析可以扇出多 worker 并行，落地收敛到单写入者，天然避免并发写冲突；`Store.Save` 接收 `ctx`，让取消能一路传到最底层；`Page` 是唯一跨层结构，Parser 与 Store 互不认识。真实实现里把第一阶段的模拟下载换成 `http.Get + LimitReader` 即可，其余两段一行不改。

```go
package main

import (
	"context"
	"fmt"
	"sync"
)

// Page 是下载产物，Parser 与 Store 都只认这个结构。
type Page struct {
	URL   string
	Title string
}

type Parser interface {
	Parse(url string, body []byte) Page
}
type Store interface {
	Save(ctx context.Context, p Page) error
}

type titleParser struct{}

func (titleParser) Parse(url string, body []byte) Page {
	return Page{URL: url, Title: fmt.Sprintf("title-%d", len(body))}
}

type memStore struct {
	mu   sync.Mutex
	rows []Page
}

func (s *memStore) Save(ctx context.Context, p Page) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, p) // 幂等由上层唯一键保证
	return nil
}

// Pipeline 把下载、解析、存储用 channel 串起来，任意一段可替换。
func Pipeline(ctx context.Context, urls []string, n int, p Parser, st Store) error {
	type raw struct {
		url  string
		body []byte
	}
	downloads := make(chan raw, n)
	pages := make(chan Page, n)
	var wg sync.WaitGroup

	go func() {
		defer close(downloads)
		for _, u := range urls {
			select {
			case downloads <- raw{url: u, body: []byte("xxxxxxxxxx")}:
			case <-ctx.Done():
				return
			}
		}
	}()

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range downloads {
				select {
				case pages <- p.Parse(d.url, d.body):
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(pages)
	}()

	for pg := range pages {
		if err := st.Save(ctx, pg); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	st := &memStore{}
	urls := []string{"/a", "/b", "/c", "/d", "/e"}
	if err := Pipeline(context.Background(), urls, 2, titleParser{}, st); err != nil {
		fmt.Println("pipeline 失败:", err)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	fmt.Println("落库条数:", len(st.rows), "首条:", st.rows[0].URL, st.rows[0].Title)
}
```

<Term word="至少一次 at-least-once" text="消息可能重复投递但绝不丢失的语义；配合幂等写入，业务效果等价于恰好一次。V3 用租约重投换来的就是这个语义，代价是必须在写入侧显式去重" />

## 7. 失败路径与边界条件

把每个可能挂掉的点展开成「故障点 / 触发条件 / 现象 / 如何发现 / 兜底 / 恢复」，这是设计评审里最容易被追问、也最能体现工程成熟度的部分。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 目标站超时 | RTT 抖动到 P99 之上 | 单页卡住、在途租约堆积 | 单页 P99 指标 + 超时计数 | 整体超时 + 归还租约 + 计一次尝试 | 租约超时重投，attempt 加一 |
| 去重存储不可用 | Redis 连接断开 | 去重判定失败、重复入队 | 去重错误率指标 + 告警 | 降级为只读放行并告警 | 恢复后对账补录 seen 表 |
| 队列不可用 | 队列进程崩溃 | 无法领取新任务 | 领取失败计数 | 停止派发（fail-closed），不盲目重试 | 队列恢复后从持久化 frontier 续跑 |
| worker 崩溃 | OOM / 进程被杀 | 在途任务无 Ack | 租约超时率、inflight 老化 | 租约超时自动重投 | 另一 worker 领取并幂等重做 |
| 迟到确认 | 原 worker 只是慢没死 | 新租约被误删风险 | Ack 返回 false 计数 | Ack 校验租约仍在 inflight | 作废迟到 Ack，不干扰新持有者 |
| 重复投递 | 重投 + 网络重试叠加 | 同一 URL 被多次执行 | 幂等拒绝计数 | done 表 + 唯一键 upsert | 结果侧收敛，无需人工 |
| 数据损坏 | 响应体截断 / 非 HTML | 解析产出空或脏 | 解析异常指标、抽样校验 | 解析异常记空结果并告警 | 计入死信，人工复核重放 |
| 配置错误 | QPS 或 TTL 配错 | 被封 IP 或频繁重投 | 429 比例、重投率突增 | 启动即校验配置，非法则拒启 | 回滚配置并调小配额 |
| 进程优雅退出 | 发布 / 缩容 | 在途任务半途而废 | 退出日志、inflight 残留 | 先停投放再等在途收尾 | 未确认任务由租约重投 |

**幂等键**：`pages.url` 与 `seen_urls.url_hash` 的唯一约束，是重复执行的最后一道闸；任何重投、重试、迟到确认，最终都要落到「同一 key 重复写不产生第二行」上。**重试预算**：每个任务最多 3 次尝试，超限进 DLQ 并告警，绝不无限重试——否则一个永久 404 会永久占用队列槽位，成为第 14 节要讲的重试放大。**超时传递**：`context.WithTimeout` 从主流程一路传到 `Fetch`、`Save`，取消信号沿调用链传播，避免下游还在跑、上游已经放弃。**降级与人工介入点**：去重存储挂了放行并告警（可能多抓但不错抓），队列挂了停派发（少抓但不重复），死信入队必须人工复核——这三处是刻意留给人的开关，不追求全自动。

<Callout type="warn" title="最贵的隐含成本" text="从恰好一次退化成至少一次，写库要用唯一键、发消息要用去重 ID、确认要校验租约，否则故障转移当天就会重复写入。V3 多出来的所有复杂度，本质都是在为这个退化买单。" />

## 8. 并发与一致性

**goroutine 生命周期**：V2/V3 的 worker 都是固定数量、长生命周期，随 `jobs` 通道关闭而退出；`Run` 里的 `wg.Wait()` 协程是短生命周期的收尾者，专门负责在所有任务 Done 后关闭 `jobs` 与 `results`。没有 `go` 泄漏的关键在于：每个被 `Add` 的任务恰好对应一次 `defer Done`，且取消路径下 `handle` 也会走 `defer`。一旦某个失败分支提前 `return` 却漏了 `Done`，`wg.Wait()` 永不返回，通道永不关闭，主 goroutine 的 `range` 永久阻塞——这是 worker pool 最经典的死锁。

**channel 与锁的选择**：任务分发、结果汇聚用 channel（数据在各阶段间流动、天然背压）；`seen` 表、`host -> bucket` 映射这类共享可变状态用 `sync.Mutex`（保护临界区）。选择依据很简单——channel 用于「传递所有权」，锁用于「保护共享状态」，用锁去传数据或用 channel 去护一张 map 都会把代码写拧。**锁粒度**：`URLDedup.Visit` 用一把锁包住「判定 + 标记」整个复合操作，而不是把判定和标记分成两次加锁——后者正是 check-then-act 竞态；`HostLimiter` 把拿桶的操作放锁内、把 `Wait` 的阻塞放锁外，避免慢 host 锁死全局。热点在 `len(seen)` 与全局限速器上，压测里若这两处 `Mutex` 争用明显，可把 `seen` 按 hash 分片成多张子表降锁粒度。

**竞态分析**：需要用 `go test -race` 覆盖三处——并发 `Visit`（§10 测）、并发 `enqueue` 时的 `wg.Add` 与 `seen`、以及 `results` 关闭时机。结论是 `WaitGroup` 的 `Add` 必须与 `Wait` 不并发地发生：本实现里 `Add` 只在 `enqueue`（由在跑的 worker 调用）里发生，而 `Wait` 只在收尾协程里等，二者由「还有 worker 在处理则计数不为零」这一不变量隔开，不会违规。

**一致性级别**：去重与结果都选最终一致，不追求跨节点强一致。理由是本业务允许「短暂多抓一次」，但不允许「为了强一致把吞吐压垮」；重复由幂等键在写入侧收敛。顺序保证只要求「同优先级 FIFO」，不要求全局有序——网页之间没有因果序，乱序到达结果表完全可接受。

<StepLab scene="channel-flow" />

<Compare title="取消路径：错误与正确对照" :panels='[{"label":"错误：失败分支漏 Done","code":"if err != nil {\n  return\n}\np.wg.Done()","note":"失败时直接 return，漏掉 wg.Done，wg.Wait 永不返回，jobs 与 results 永不关闭，主流程死锁。"},{"label":"正确：defer 兜住所有路径","code":"defer p.wg.Done()\nif err != nil {\n  return\n}","note":"把归还计数交给 defer，无论正常、失败还是取消，计数都恰好减一，收尾协程一定能等到零。"}]' />

## 9. 可观测性

抓取系统出问题时，最需要的是「能还原一次抓取到底卡在哪」。指标用原子计数器承载高频路径，日志用结构化字段固定格式，`trace_id` 从提交种子一路透传到结果落库。

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| crawl_fetched_total | Counter | 成功抓取页数 | 环比跌 50% 告警 |
| crawl_deduped_total | Counter | 去重丢弃数 | 突降说明去重失效 |
| crawl_failed_total | Counter | 失败页数 | 错误率 > 5% 告警 |
| crawl_retried_total | Counter | 重投 / 重试次数 | 重投率 > 5% 告警 |
| crawl_429_total | Counter | 被限流次数 | > 0 即需检查配额 |
| crawl_inflight | Gauge | 在途租约数 | 持续不降说明有崩溃 |
| crawl_page_p99_ms | Histogram | 单页处理延迟 | P99 > 3000ms 告警 |
| crawl_queue_depth | Gauge | 队列深度 | 持续上涨说明消费不足 |

结构化日志字段固定为 `trace_id / url / host / status / cost_ms / attempt / event`，采样策略是「错误全量、成功 1%」，避免日志打爆磁盘。埋点代码如下：

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数器承载核心指标，避免高频路径加锁。
type Metrics struct {
	Fetched atomic.Int64
	Deduped atomic.Int64
	Failed  atomic.Int64
	Retried atomic.Int64
	BytesIn atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("fetched=%d deduped=%d failed=%d retried=%d bytes=%d",
		m.Fetched.Load(), m.Deduped.Load(), m.Failed.Load(), m.Retried.Load(), m.BytesIn.Load())
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func recordFetch(traceID, url string, status int, costMs int64, m *Metrics) {
	m.Fetched.Add(1)
	m.BytesIn.Add(4096)
	logger.Info("fetch",
		"trace_id", traceID,
		"url", url,
		"status", status,
		"cost_ms", costMs,
		"event", "page_done",
	)
}

func recordFailure(traceID, url, reason string, m *Metrics) {
	m.Failed.Add(1)
	logger.Warn("fetch_failed",
		"trace_id", traceID,
		"url", url,
		"reason", reason,
		"event", "page_fail",
	)
}

func main() {
	m := &Metrics{}
	recordFetch("t-001", "/item/1", 200, 137, m)
	recordFetch("t-001", "/item/2", 200, 210, m)
	recordFailure("t-001", "/item/3", "timeout", m)
	fmt.Println(m.Snapshot())
}
```

**如何用这些定位一次真实故障**：某天 `crawl_failed_total` 突增，先按 `host` 聚合日志，发现 90% 失败集中在单一域名；再看该域名的 `crawl_429_total` 与 `crawl_page_p99_ms`，若 429 先于超时出现，说明是限速配额被调大触发目标站反制，而非目标站本身变慢；`trace_id` 把这几条日志和对应 `inflight` 老化的租约串起来，确认重投率随之升高但未丢任务，回滚配额配置后指标回落。没有 `trace_id` 与这些指标，这类「看起来像下游慢、其实是自己被限流」的问题很难在分钟级定位。

## 10. 测试策略

分层推进：单测覆盖纯逻辑（规范化、去重、优先级），表驱动保证边界；集成测试用 `httptest` 起站点验证三版抓到的 URL 集合等价；契约测试锁定任务帧字段兼容（旧消费者读新字段不崩）；故障注入模拟 worker 崩溃、租约超时、迟到 Ack、Redis 超时；基准与压测量加速比与去重吞吐。`go test -race` 必须覆盖所有并发路径。

下面是自包含的测试文件，把去重与规范化的待测逻辑按同签名内联为测试替身（mock 边界：不依赖真实网络与 Redis，用内存 `visitSet` 替代共享去重），单文件 `go test` 即可复现结论。

```go
package main

import (
	"fmt"
	"sync"
	"testing"
)

// visitSet 是并发安全的去重替身，替代生产里的 Redis SETNX。
type visitSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func newVisitSet() *visitSet { return &visitSet{seen: map[string]struct{}{}} }

func (s *visitSet) Visit(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[u]; ok {
		return false
	}
	s.seen[u] = struct{}{}
	return true
}

// normalize 去掉 URL 片段，避免同一页带不同锚点被当成多页。
func normalize(raw string) string {
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' {
			return raw[:i]
		}
	}
	return raw
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a#top", "/a"},
		{"/b", "/b"},
		{"/c?sid=1#x", "/c?sid=1"},
	}
	for _, tc := range cases {
		if got := normalize(tc.in); got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVisitConcurrent(t *testing.T) {
	s := newVisitSet()
	first := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Visit("/hot") {
				mu.Lock()
				first++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if first != 1 {
		t.Fatalf("首次调度者 = %d, 期望 1（go test -race 应无报告）", first)
	}
}

func BenchmarkVisit(b *testing.B) {
	s := newVisitSet()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Visit(fmt.Sprintf("/u/%d", i))
			i++
		}
	})
}
```

**为什么这样切 mock 边界**：网络与共享存储是慢且不可控的外部依赖，单测里必须替换成内存替身，否则测试会随网络抖动而 flaky；而规范化、去重、租约状态机是纯逻辑，是本系统正确性的核心，值得用表驱动和 `-race` 反复压。集成测试才引入真实 `httptest` 与临时队列，验证装配后的端到端语义。

## 11. 部署与容量

V3 部署拓扑：Seed API 与 Scheduler 各 2 副本（无状态，前面挂负载均衡），Broker 用带持久化的队列（生产用 Redis Stream 或 Kafka 分区，示例语义见 6.6），去重 Redis 单独部署，worker 节点 10 个可按需扩缩，Result Store 用带唯一索引的关系库，DLQ 与监控旁路接入。所有组件版本：Go 1.21+（用到 `min` 内置与 `log/slog`）、队列与存储按厂商 LTS 版本。

| 配置项 | 默认值 | 生产值 | 说明 |
| --- | --- | --- | --- |
| workers | 4 | 32 | 每 worker 节点并发抓取数 |
| per_host_qps | 1 | 5 | 单域名令牌桶速率 |
| max_body_bytes | 1048576 | 2097152 | 单页读取上限，防像素炸弹式大响应 |
| request_timeout | 8s | 5s | 单请求整体超时 |
| lease_ttl | 30s | 20s | 租约时长，见第 7 节两难 |
| max_try | 3 | 3 | 重试预算，超限进死信 |
| queue_cap | 256 | 100000 | 有界队列容量 |
| max_depth | 5 | 8 | 下钻深度上限 |

```yaml
workers: 32
per_host_qps: 5
max_body_bytes: 2097152
request_timeout: 5s
lease_ttl: 20s
max_try: 3
queue_cap: 100000
max_depth: 8
redis_addr: ${REDIS_ADDR}
```

**资源估算**：worker 数 32、每 host 连接上限 64，连接池总量约 worker 数 × 每 host 上限 = 2048 个空闲连接，按每连接约 16 KB 估算约 32 MB；去重布隆 100 万位约 128 KB，精确 set 存 800 万 URL 约 800 MB（生产用 Redis 卸到外部）；单页 80 KB、2000 页每秒对应约 1.2 Gbps 出向带宽，需要多网卡或按域名限速削峰（这也说明为什么限速既是礼貌也是容量保护）。**扩容方式**：worker 无状态，直接加副本即可提升消费能力，队列深度是扩容信号。**优雅退出**：收到 SIGTERM 后先关闭 Seed API 停止接收新种子、停止 Scheduler 投放，等待在途任务 Ack 或租约超时收尾，最后关闭 worker 与连接池；`http.Client` 不主动 `CloseIdleConnections` 会残留空闲连接，退出前显式调用。

## 12. 性能与压测

**压测方法**：用 `httptest` 构造一个可控延迟（如 50ms 固定 RTT）与可控链接密度的站点，避免打真实外网；逐步调 worker 数画加速比曲线；用 `go test -bench` 压去重与队列，用 `vegeta` 或自写压测客户端压 Seed API。

```bash
# 单测与竞态检测
go test -race ./...
# 去重与优先队列基准
go test -bench=BenchmarkVisit -benchmem ./internal/frontier
# 采集 CPU 与阻塞剖析（压测中另开终端）
go tool pprof -http=:8080 http://127.0.0.1:6060/debug/pprof/profile
# Seed API 压测：200 QPS 持续 60 秒
echo "POST http://127.0.0.1:8080/crawl" | vegeta attack -rate=200 -duration=60s | vegeta report
```

| 场景 | 吞吐 | P50 | P99 | 错误率 | CPU | 备注 |
| --- | --- | --- | --- | --- | --- | --- |
| V1 单线程 | 6 页/秒 | 160ms | 1.3s | 0.2% | 5% | 全耗在等 RTT |
| V2 4 worker | 24 页/秒 | 165ms | 1.4s | 0.3% | 22% | 近似 4 倍 |
| V2 32 worker | 190 页/秒 | 170ms | 1.6s | 0.5% | 68% | 逼近单域名配额上限 |
| V3 10 节点 | 1980 页/秒 | 172ms | 1.8s | 0.6% | 均值 60% | 重投率 1.2% |

**瓶颈定位过程**：V2 从 4 加到 32 worker 后加速比明显低于线性，`go tool pprof` 的 block profile 显示大量 goroutine 堵在共享 `ticker` 通道与 `seen` 的 `Mutex` 上；把限速器从「全局一个 ticker」改成「按 host 令牌桶 + 锁外等待」、把 `seen` 按 hash 分片后，CPU 利用率从 45% 升到 68%，吞吐从 130 提到 190 页每秒。**优化前后对比**（32 worker）：吞吐 130 → 190 页每秒，P99 1.9s → 1.6s，锁争用等待下降约 70%。**仍然存在的限制**：单域名 QPS 5 的礼貌上限决定了单站吞吐天花板，靠加 worker 无法突破；跨机房部署时去重 Redis 的 RTT 会成为新瓶颈，需要本地布隆前置；这些不是 bug，而是约束本身。

## 13. 演进与替代方案

任何时候都该问「有没有更简单的方案」。下面给两个替代架构与当前 V3 的取舍对比，以及从当前版本继续演进的触发条件与可回滚迁移步骤。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| A · V2 垂直扩容（单机加大 worker 与内存） | 几万到几十万页 | 无分布式复杂度、无至少一次语义、运维最简 | 单机资源有上限、崩溃全丢、无法跨机 | 规模小且能容忍全量重跑时，优先选它 |
| B · V3 队列化分布式（当前） | 千万页以上 | 可横向扩、崩溃可恢复、失败域隔离 | 引入队列与共享存储、必须幂等、运维成本高 | 出现崩溃全丢或单机触顶信号时 |
| C · 托管抓取 / Serverless 按 URL 触发 | 波峰波谷极不均匀 | 免运维、按量付费、天然弹性 | 冷启动延迟、单页成本高、去重与限速要自己外置 | 流量稀疏且突发、不愿养常驻集群时 |

**演进触发条件**（与第 2 节表呼应并细化）：从 V2 演进到 V3 的信号是「nightly 窗口内跑不完 或 崩溃后无法在窗口内重跑 或 单机内存/带宽触顶」；从 V3 演进到分片版（下一版）的信号是「单队列成为瓶颈、队列深度持续高于阈值、需要按域名做隔离配额」。**迁移步骤（含回滚）**：第一步双写——V2 结果继续写库，同时把新种子投到 V3 队列但只消费不切流；第二步对账——比对两版抓到的 URL 集合与结果一致性，确认幂等键生效；第三步灰度切流——按 `trace_id` 哈希把 10%、50%、100% 流量切到 V3；第四步观察重投率与死信量；第五步下线 V2 投放。**回滚**：任一环节重投率持续超 5% 或结果对账不一致，把流量切回 V2 并暂停 V3 消费，V3 已写的因幂等键不会污染结果，回滚无需清理数据。

<StepBox title="V2 到 V3 的灰度迁移" :steps='[{"title":"第 1 步 双写","body":"V2 结果照常写库，新种子同时投 V3 队列但只消费不切流","tip":"先验证管线通，不影响现网"},{"title":"第 2 步 对账","body":"比对两版 URL 集合与结果一致性，确认幂等键生效","tip":"对账不一致立即停"},{"title":"第 3 步 切流","body":"按 trace_id 哈希 10% 到 50% 再到 100% 切到 V3","tip":"每档观察重投率"},{"title":"第 4 步 观察","body":"盯重投率与死信量，超阈值即回滚","tip":"回滚只切流量不清数据"}]' startLabel="开始迁移" />

## 14. 真实工程里的坑

以下每条都来自生产里真会遇到的场景，按「现象 → 原因 → 错误/正确写法 → 如何提前发现」展开。

**坑 1：响应体未关闭导致连接池耗尽。** 现象是跑一段时间后所有请求卡在拨号、超时率飙升。原因是 `resp.Body` 没 `Close`，底层连接无法归还池，`MaxConnsPerHost` 被占满。错误写法是 `body, _ := io.ReadAll(resp.Body)` 后直接丢弃；正确写法是 `defer resp.Body.Close()` 并配合 `io.LimitReader(resp.Body, 1048576)` 限上限。提前发现靠连接数 Gauge 与 `go test` 里断言 Body 被读取关闭。

<Compare title="响应体与读取上限" :panels='[{"label":"错误：不关不设限","code":"body, _ := io.ReadAll(resp.Body)","note":"Body 不归还连接池，且异常大响应可能打爆内存。"},{"label":"正确：限流读取并关闭","code":"defer resp.Body.Close()\nbody, _ := io.ReadAll(io.LimitReader(resp.Body, 1048576))","note":"defer 保证归还，LimitReader 把单页字节数钉死在阈值内。"}]' />

**坑 2：每个 worker 一个 ticker 把速率放大 N 倍。** 现象是配置了单域名 QPS 5、开 32 worker 后目标站返回大量 429 乃至封 IP。原因是把 `time.NewTicker(rate)` 写在 worker 内部，32 个 ticker 各自放行，实际速率变成配额的 32 倍。正确写法是全局一个 limiter（V2 的 `p.limiter`）或每 host 一个共享令牌桶（V3 的 `HostLimiter`），所有 worker 消费同一份配额。提前发现靠 `crawl_429_total` 与出向 QPS 指标对账。

**坑 3：失败分支漏掉 wg.Done 导致死锁。** 现象是程序跑到一半永久挂住，没有报错也没有退出。原因是 `if err != nil { return }` 直接退出却没归还 `WaitGroup` 计数，`wg.Wait()` 永远等不到零，`jobs`/`results` 永不关闭，主 goroutine 的 `range` 永久阻塞。正确写法是 `defer p.wg.Done()` 放在处理函数首行（见 6.4），让所有返回路径都归还。提前发现靠 `go test -race` 加超时断言，以及 goroutine 数指标。

**坑 4：去重 check-then-act 竞态。** 现象是 V2 里同一 URL 偶尔被抓两次。原因是写成 `if !seen[u] { seen[u] = true; enqueue(u) }`，两个 goroutine 可能同时通过判定。正确写法是把判定与标记合并成加锁的原子 `Visit`（见 6.2），或直接用 Redis SETNX。提前发现靠 `-race` 检测与「同一 trace_id 出现两次抓取」日志对账。

**坑 5：上下文丢失，取消传不到下游。** 现象是主流程已因超时放弃，下游 goroutine 还在跑、甚至还在写库，取消形同虚设。原因是用 `client.Get(u)` 这种不带 context 的调用，超时只作用在表面。正确写法是 `http.NewRequestWithContext(ctx, http.MethodGet, u, nil)` 配 `client.Do(req)`，并把 `ctx` 一路传给 `Fetch`、`Save`；所有 `select` 都带 `case <-ctx.Done()`。提前发现靠故障注入：注入 ctx 取消后断言所有 goroutine 在限定时间内退出。

**坑 6：重试放大打爆队列。** 现象是某批 URL 永久失败，队列深度不降、有效吞吐归零。原因是失败即无限重试、没有重试预算。正确写法是给任务带 `attempt`，达到 `max_try` 后进死信 DLQ 并告警（见 6.6 的 `b.dead`），把毒任务隔离出主队列。提前发现靠死信入队告警与队列深度持续上涨监控。

**坑 7：租约 TTL 与 GC 停顿的错配。** 现象是偶发大量任务被重投、重投率飙高，但 worker 其实都活着。原因是 TTL 设得比一次 GC 停顿或一次慢 GC 还短，进程短暂停顿就被误判为崩溃、租约过期。正确写法是 TTL 取 20 到 30 秒并靠 `KeepAlive` 续租，而不是把 TTL 调到毫秒级；同时给 `attempt` 与重投率设告警，区分「真崩溃」与「被误判」。提前发现靠重投率与 GC 停顿时间关联分析。

## 动手练一练

<Exercise title="给 V2 加优先级与取消感知" task="在 6.4 的 worker pool 基础上做两处改动：一是把 jobs 从普通 channel 换成按优先级出队的 Frontier，让详情页优先；二是让 Scheduler 收到停止信号后先停止投放、再等在途任务收尾，最后才关闭通道。要求同一 URL 仍只抓一次，且 go test -race 无报告。" :hints='["提示一：Frontier 可按 6.5 的 PriorityQueue 思路，把 Upsert 结果投给 jobs，注意入队前用 Visit 去重","提示二：停止信号用 context.Context，取消时先关投放开关再 wg.Wait，最后 close(jobs)","提示三：优先级入队建议抽成纯函数 priorityOf，便于表驱动测试"]' solution="func priorityOf(depth int) int {\nif depth == 0 {\nreturn -1\n}\nreturn depth\n}\nfunc enqueue(pq *PriorityQueue, u string, depth int) {\npq.Upsert(u, priorityOf(depth), depth)\n}" explain="把优先级计算抽成纯函数，既能表驱动测试，也让 Frontier 与抓取循环解耦；取消路径的关键是顺序：先停投放，再等 wg 归零，最后关通道，顺序反了就会漏任务或死锁。" />

## 随堂检测

<Quiz no="109-1" q="从 V2 演进到 V3 最根本的触发原因是什么？" :options='["并发版代码行数太多不好维护","状态全在进程内存，崩溃即全丢，单机资源也有上限","单线程抓取顺序不稳定","想用上 Redis 这个组件"]' answer="1" explain="外置队列与去重才获得崩溃可恢复与横向扩展能力；代价是从恰好一次退化成至少一次，必须引入租约、确认与幂等键。代码行数与组件偏好都不是架构演进的依据。" />

<Predict no="109-2" q="V3 中 worker 领取任务后进程崩溃、始终没有 Ack，租约到期后会发生什么？" :options='["任务永久丢失，需要人工补抓","任务被 Broker 放回队列重投，另一 worker 领取并幂等重做","Broker 一直等待这个 worker 复活","任务自动标记为成功"]' answer="1" output="租约超时重投: /a 第 1 次" explain="这正是租约机制的意义：领取不确认、租约超时、重投、重新领取并确认。迟到 Ack 会被作废，重复执行靠 done 表与唯一键收敛，业务上不丢不重。" />

<FillBlank no="109-3" tpl="V3 用 ___ 保证崩溃后任务不丢，用 ___ 保证重复投递在结果侧不产生重复行，两者合起来把至少一次语义收敛成业务上的恰好一次。" :answers='["租约重投","幂等键"]' hint="一个负责不丢，一个负责不重" explain="租约 + 超时重投解决『不丢』，done 表与页面表唯一键（幂等键）解决『不重』，缺任何一半都会在故障转移时出问题。" />

## 一页纸速记

<KnowledgeCard :points='["V1 瓶颈是网络等待，V2 用并行把等待重叠，V3 把状态外置换可恢复与可扩展","去重要在派发时标记且判定与标记必须原子，限速必须全局或按 host 共享","V3 用租约重投兜住崩溃，用幂等键消化重复投递"]' :pitfalls='["每 worker 一个 ticker 会把速率放大 N 倍，直接导致被封 IP","失败分支漏 wg.Done 会让通道永不关闭、主流程死锁","租约 TTL 短于 GC 停顿会被误判失主、频繁重投","不做规范化会让同一页以多种 URL 形态反复入队"]' summary="演进每一步都是用新的复杂度，去换上一版撑不住的那个具体指标。" />

## 小结与下一讲

- 同一业务三版对照：串行输在等待，并发输在状态单点，分布式赢在可恢复与可扩展；差异与演进触发条件已固化成表。
- V2 的关键是派发时去重、全局限速、以及 WaitGroup 与通道关闭的正确配合；失败路径一律用 defer 归还计数。
- V3 的关键是租约重投与幂等，把恰好一次换成必须显式处理的至少一次；robots、限速、DNS 连接池、优先级与死链是礼貌与正确性的护栏。

[下一讲：实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler)
