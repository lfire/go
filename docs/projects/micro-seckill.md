---
title: 第 116 讲 · 实战 10 · 秒杀与高并发库存
description: 流量削峰、限流排队、原子预扣杜绝超卖，讲清预扣与落库之间的最终一致
---

<StageHeader stage="L10 · 工程实战" level="★★★★★" time="80 分钟" goal="在极端流量下守住正确性" prereq="第 115 讲 · 电商微服务系统" />

# 第 116 讲 · 实战 10 · 秒杀与高并发库存

> 上一讲把电商系统拆成了四个服务，用 Saga 守住了跨服务一致性，可那里的库存扣减仍是一个普通的预扣调用，日常下单毫无问题。秒杀是另一种物理世界：某款限量商品只有 100 件，开抢瞬间涌进十万级 QPS，数据库既扛不住这个洪峰，「先查余量再扣减」这种写法在并发下又会把库存直接卖成负数。这一讲只围绕两条铁律展开：**绝不能超卖**，以及**预扣与落库之间如何做到最终一致**。为了守住它们，我们会主动接受两个反直觉的取舍——宁可少卖，也宁可让绝大多数请求被快速拒绝。秒杀架构的全部难点，不在于让系统跑得更快，而在于敢在流量最凶的时候主动把请求挡在门外，并把每一处「读后再写」都收进一个原子步骤里。

<CheckList id="L116-goal" title="本讲目标（评审前逐条打勾）" :items='["能复现先查后扣导致超卖的根因，并给出至少两种原子化修复方案的对照","能把检查、判重、扣减合成一个临界区，配合幂等键杜绝重复扣减","能用令牌桶与一次性配额把十万级洪峰削成数据库可承受的匀速写","能设计有界队列、结果轮询与超时释放，讲清预扣到落库的最终一致","能拆分热点账户与热点库存，说明分段库存的代价与碎片处理","能给出数字自洽的压测模型、容量估算与降级熔断预案"]' />

## 0. 项目定位与真实场景

把场景钉死：大促当天 20:00 开抢一款限量 100 件的商品，价格极具吸引力，预约用户 80 万。历史数据显示开抢后 30 秒内会涌入约 10 万 QPS 的瞬时峰值请求，其中绝大多数注定抢不到。使用方有三类：C 端用户要一个明确结果（抢到、没抢到、排队中），运营要能随时冻结活动、看库存水位、改限流阈值，风控要拦住脚本与黄牛的批量请求。这三类诉求都压在同一条链路上，而链路最末端只有一个库存数字 100 和一个扛不住高并发写的订单库。

直接把第 115 讲的下单系统拿来扛秒杀，会同时崩在三个地方。第一是数据库写：2000 QPS 的设计容量对上 10 万 QPS 的洪峰，连接池瞬间打满、事务相互等锁，P99 从 800ms 劣化到几十秒。第二是正确性：库存服务里如果写成「先 SELECT 余量、判断大于 0、再 UPDATE 减一」，两个并发事务会读到同一个旧值，各自判断通过、各自扣减，100 件库存卖出 137 件，这就是超卖，属于资损与客诉，绝不允许。第三是热点：所有请求都打向同一个 SKU、同一行库存记录、同一个 Redis key，串行化之后吞吐被锁死在单点上限，加机器也没用。

于是本讲的核心矛盾浮现出来：**系统必须在「瞬时十万级请求」与「只能成交 100 单」之间做一次极度不对称的收敛**。这个不对称决定了架构必须把绝大多数请求在最外层就拒绝掉，只放极少量请求进入真正的扣减与落库；同时必须承认内存里的预扣与数据库里的订单之间存在一段真空期，用可靠消息、幂等消费与定时对账把它补上。一句话概括取舍：绝不超卖是硬约束，可以少卖是可接受的代价，请求被拒绝是设计目标而非故障。

<Term word="超卖" text="卖出的数量超过实际库存，根因是检查余量与扣减余量之间没有被同一个原子操作包住，两个并发读者都基于同一个旧值做了扣减" />

## 1. 需求与约束

先把约束写成可度量的数字，后面所有限流阈值、队列长度、worker 数都由它反推，而不是拍脑袋。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 绝不超卖 | 100 件库存、任意并发下成功抢到的请求数恒等于 100，余量精确为 0，不得为负 |
| FR-2 | 流量削峰 | 10 万 QPS 入口洪峰被削成不超过 200 QPS 的匀速写库，数据库写入峰值可观测且不超阈 |
| FR-3 | 快速失败 | 未抢到与被限流的请求在 50ms 内返回明确结果，不得长时间挂起或拖垮下游 |
| FR-4 | 幂等去重 | 同一用户同一活动重复点击 100 次，只扣一次库存、只生成一个订单 |
| FR-5 | 结果可查 | 抢到的请求返回排队号，用户每秒轮询一次，最多三次可查到终态 |
| FR-6 | 最终一致 | 预扣成功到订单落库的真空期内若进程崩溃，靠重放与对账在 5 分钟内收敛，库存守恒 |

| 非功能指标 | 目标值 | 测量口径 |
| --- | --- | --- |
| 入口瞬时峰值 | 100000 QPS | 压测注入速率，持续 30 秒共约 300 万次请求 |
| 令牌桶放行速率 | 10000 QPS | 网关削峰后进入应用层的速率 |
| 一次性配额 | 300 个 | 库存 100 件乘以放大系数 3，只有这些请求能进入预扣 |
| 原子预扣 P99 | 不高于 5ms | Redis 单条 Lua 的执行耗时 |
| 落库写入峰值 | 不高于 200 QPS | 4 个 worker 每个 50 QPS 之和 |
| 一致性级别 | 不超卖 + 最终一致 | 允许少卖与中间态，禁止多卖 |
| 可用性 SLA | 99.9% | 秒杀窗口内入口可用分钟占比 |
| RTO / RPO | 3 分钟 / 0 单丢失 | 故障切换耗时，已预扣成功必须能收敛成订单或回补 |

**明确取舍**：一致性上选「不超卖 + 最终一致」而不是「强一致 + 全成功」——不引入 2PC 锁住库存行，因为那会让 10 万 QPS 全部堵在协调者上；可用性上选「限流快速失败」而不是「让所有请求排队到超时」，因为 99% 的请求注定抢不到，让它们排队纯属浪费。**本讲不做什么**：不做多 SKU 联合秒杀的分布式事务、不做跨地域多活、不做真实支付渠道对接（用 stub）、不自建服务发现与链路追踪（沿用第 75、80 讲）。**假设边界**：单机房，Redis 一主两从且 Lua 原子执行，MySQL 单主写上限按 3000 QPS 估，时钟经 NTP 同步偏差不超过 50ms，内部网络 P99 往返不高于 2ms。

## 2. 架构蓝图

<ArchBlueprint title="秒杀六段削峰架构" caption="越靠前越要把请求挡掉，数据库永远只承受匀速的小流量；依赖方向自上而下单向流动" :layers='[{"name":"流量接入层","note":"把无效流量挡在最外","protocol":"HTTPS / CDN 静态化","items":[{"label":"CDN 与静态页","sub":"页面 / 按钮 / 答题","detail":"职责：承载静态资源与答题验证码，把脚本与无效点击挡在边缘；技术选型：页面静态化加动态令牌，抢购按钮按时解锁；失败处理：边缘故障直接降级为静态公告页，不回源。","tone":"blue"},{"label":"网关准入","sub":"鉴权 / 黑名单 / 参数","detail":"职责：校验登录态、过滤黑名单用户与设备、校验活动 ID 与用户 ID；技术选型：net/http 中间件链，本地缓存黑名单；失败处理：非法请求返回 400，黑名单返回 403，不进入限流层。","tone":"purple"}]},{"name":"削峰层","note":"把洪峰摊平成匀速","protocol":"进程内令牌桶 / Redis 配额","items":[{"label":"令牌桶","sub":"稳态限流 1 万 QPS","detail":"职责：按速率补充令牌，把 10 万 QPS 削成 1 万 QPS；技术选型：单机令牌桶加本地计数，无跨节点依赖；失败处理：无令牌返回 429 并携带 Retry-After，绝不向下游透传。","tone":"green"},{"label":"一次性配额","sub":"库存乘以 3 放号","detail":"职责：活动开始一次性放 300 个令牌，抢完即止，只放这些请求进入预扣；技术选型：Redis 原子 DECR 或进程内配额池；失败处理：配额耗尽返回售罄，后续请求不再碰库存。","tone":"orange"},{"label":"熔断降级器","sub":"保护下游","detail":"职责：统计连续失败，超阈值打开熔断并在冷却后半开试探；技术选型：三态状态机，降级时返回静态兜底；失败处理：熔断期间直接拒绝新请求，避免下游雪崩。","tone":"red"}]},{"name":"预扣与状态层","note":"正确性的核心","protocol":"Redis Lua / 内存有界队列","items":[{"label":"Redis 原子预扣","sub":"判重 + 判余量 + 扣减","detail":"职责：一条 Lua 内完成检查、判重、扣减、写预扣记录，返回是否抢到；技术选型：单 key Lua 或分段多 key；失败处理：返回售罄即不再重试扣减，避免放大。","tone":"orange"},{"label":"分段库存","sub":"100 拆 10 段各 10","detail":"职责：把单 key 热点摊到多把锁上，让不同段的扣减真正并行；技术选型：userID 哈希定位主段，不足时探测其它段；失败处理：碎片由探测回收，总余量对账校验。","tone":"green"},{"label":"预约状态机","sub":"预留 / 已付 / 已放","detail":"职责：以条件更新维护 RESERVED、PAID、RELEASED 三态；技术选型：状态判断与变更同临界区或同一条 Lua；失败处理：已支付记录拒绝释放，杜绝把库存凭空加回。","tone":"blue"},{"label":"有界排队队列","sub":"容量 200","detail":"职责：抢到的请求入队，worker 匀速消费写库；技术选型：有界 channel，非阻塞入队；失败处理：队列满立即回补预扣并快速失败，防止少卖与不一致。","tone":"purple"}]},{"name":"落库与对账层","note":"最终一致的兜底","items":[{"label":"订单 worker","sub":"4 副本匀速写","detail":"职责：从队列取号幂等写订单，失败按预算重试；技术选型：按排队号或用户 ID 建唯一索引；失败处理：重试耗尽进对账表并触发回补，绝不静默丢弃。","tone":"gray"},{"label":"库存 DB","sub":"MySQL 单主","detail":"职责：持久化最终库存与订单，承受不超过 200 QPS 匀速写；技术选型：条件更新保证余量不为负；失败处理：主库抖动时 worker 退避重试，不放大写流量。","tone":"gray"},{"label":"延迟队列与对账","sub":"15 分钟释放","detail":"职责：超时未支付回补库存，定时扫描预扣与订单差异；技术选型：延迟队列加条件更新，对账以库存为准、以回补为兜底；失败处理：毒事件进人工队列，禁止自动吞掉。","tone":"red"}]}]' />

逐层解读为什么这样切。流量接入层存在的唯一理由是「越早拒绝越便宜」：一个静态页由 CDN 扛，成本远低于一次到网关的动态请求，更远低于一次 Redis 写；把答题、验证码、黑名单放在这里，能在不碰核心链路的情况下挡掉脚本流量。削峰层是本讲与普通高并发系统最大的区别：令牌桶负责把 10 万 QPS 摊成 1 万 QPS 的稳态，一次性配额负责把「注定抢不到」的请求彻底挡在预扣之外——它不补水，因为秒杀是一次性事件，按速率补水的常规令牌桶反而会把请求持续放进临界区。预扣与状态层是正确性核心，所有可能破坏「成交数等于库存数」这个不变量的操作都必须在这里收进原子步骤。落库与对账层只承受被前三层筛过的极少量匀速流量，它的职责不是扛洪峰，而是把内存预扣的结果可靠地固化下来。

<ConceptDiagram type="gateway" title="网关闭口：鉴权、限流、配额放号" caption="99% 以上的请求在这一层就被快速拒绝，不会碰到库存与数据库" />

<ConceptDiagram type="circuit-breaker" title="熔断器三态：closed / open / half-open" caption="下游连续失败时主动断开，冷却后放一个试探请求，避免故障放大成雪崩" />

<ConceptDiagram type="mq-flow" title="预扣成功到订单落库：可靠消息、幂等消费、定时对账" caption="真空期靠至少一次投递加幂等消费补齐，对账以库存为准、以回补为兜底" />

<ConceptDiagram type="cache-aside" title="Redis 预扣与 DB 落库的分工" caption="Redis 抗瞬时热点写，DB 只承受匀速持久化，两者之间允许秒级延迟" />

<FlowTimeline direction="horizontal" :steps='[{"title":"静态拦截","desc":"CDN 与答题挡脚本","tone":"blue"},{"title":"令牌桶","desc":"10 万削成 1 万 QPS","tone":"green"},{"title":"配额放号","desc":"只放 300 个进预扣","tone":"green"},{"title":"原子预扣","desc":"Lua 判重判余量扣减","tone":"orange"},{"title":"排队削峰","desc":"有界队列匀速消费","tone":"orange"},{"title":"落库对账","desc":"worker 写单并回补","tone":"red"}]' />

依赖方向严格自上而下：接入层依赖削峰层，削峰层依赖预扣层，预扣层依赖落库层。**绝不允许反向依赖**——订单 worker 不能回头去改 Redis 库存水位，对账任务不能绕过状态机直接 UPDATE 余量，否则预扣层精心维护的原子性会被下游的一次乱序写破坏。层与层之间只经由明确的接口或消息，同层组件（如分段库存的各段）彼此不通信，需要协调时上提到编排逻辑。

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| 网关准入 | HTTPS 请求 | 放行或 400 / 403 | 黑名单缓存、鉴权 | 无状态 | 非法请求直接拒绝，不进入限流层 |
| 令牌桶 | 放行请求 | 令牌或 429 | 本地计数 | 有状态（令牌数、时间戳） | 无令牌返回 429 加 Retry-After |
| 一次性配额 | 令牌桶放行的请求 | 300 个准入名额 | Redis 或进程内计数 | 有状态（剩余配额） | 配额耗尽返回售罄 |
| Redis 原子预扣 | 用户 ID、活动 ID | 是否抢到、预扣记录 | Redis Lua | 有状态（余量、去重表、状态机） | 售罄返回明确错误，不重试扣减 |
| 有界队列 | 抢到的用户 ID | 排队号 | 内存 channel | 有状态（队列长度） | 队列满立即回补并快速失败 |
| 订单 worker | 排队任务 | 订单行 | 订单库、幂等表 | 有状态（重试次数） | 重试耗尽进对账表并触发回补 |
| 延迟队列 / 对账 | 预扣记录、订单表 | 回补或告警 | 库存、订单库 | 有状态（位点、死信） | 毒事件进人工队列，位点不推进 |

包目录树按「一个入口二进制 + 可替换的窄接口」组织：

```text
seckill/
├── cmd/gateway/main.go        # HTTP 入口与准入
├── internal/
│   ├── limit/                 # limiter.go 令牌桶 / quota.go 一次性配额
│   ├── stock/                 # stock.go 原子预扣 / segmented.go 分段库存
│   ├── queue/                 # queue.go 有界队列与 worker
│   ├── reconcile/             # reconcile.go 超时释放与对账
│   ├── resilience/            # circuit.go 熔断与降级
│   └── contract/              # 三张窄接口与领域错误
└── go.mod
```

接口定义遵循依赖倒置：网关只依赖 `Quota`、`Stock`、`TicketSink` 三张窄接口（见 6.4 的注入方式），具体实现可以是 Redis、内存 fake 或压测桩，替换时不需要改编排逻辑一行代码。这也是 6.8 能用纯内存实现跑不超卖断言的原因。

<Callout type="warn" title="秒杀边界划分最常见的三个错误" text="一是把限流放在数据库前面很近的位置，洪峰已经打到连接池才被拒绝，等于没挡；二是让订单 worker 直接回写 Redis 余量，绕过了预扣状态机，一次乱序就把库存加错；三是把「抢到」与「下单成功」当成同一件事，预扣成功就给用户看成功页，worker 写库失败后既没订单也没回补，用户被静默欺骗。正确做法是挡在最外层、下游只经由消息与状态机、给用户的是可轮询的排队号而不是终态承诺。" />

## 4. 数据与协议契约

Redis 侧的数据结构与 key 约定必须先定，Lua 脚本才能保持单条原子：

| Key / 结构 | 类型 | 内容 | 作用 |
| --- | --- | --- | --- |
| sk:{act}:stock | String | 剩余库存整数 | 原子扣减的对象，分段时拆成 sk:{act}:seg:{i} |
| sk:{act}:dedup | Set | 已受理用户 ID | 判重，同一用户只成功一次 |
| sk:{act}:resv | Hash | 用户 ID 到状态与时间戳 | 预约状态机，供超时释放条件更新 |
| sk:{act}:quota | String | 剩余配额整数 | 一次性放号，抢完即止 |

原子预扣的 Lua 脚本把「判重 → 判余量 → 扣减 → 写记录」合成一次 Redis 调用，杜绝 read-modify-write 之间的缝：

```text
-- KEYS[1]=库存 key, KEYS[2]=去重集合, KEYS[3]=预约 hash
-- ARGV[1]=用户 ID, ARGV[2]=当前时间戳
if redis.call('SISMEMBER', KEYS[2], ARGV[1]) == 1 then
  return 0                              -- 幂等：重复请求不扣减
end
local left = tonumber(redis.call('GET', KEYS[1]) or '0')
if left <= 0 then
  return -1                             -- 售罄
end
redis.call('DECR', KEYS[1])
redis.call('SADD', KEYS[2], ARGV[1])
redis.call('HSET', KEYS[3], ARGV[1], 'RESERVED')
return 1                                 -- 抢到
```

数据库侧表结构里，`uk_user_act` 唯一索引是幂等的物理保证，`CHECK` 约束是绝不超卖的最后一道防线：

```sql
CREATE TABLE seckill_stock (
  sku        VARCHAR(32) PRIMARY KEY,
  total      INT NOT NULL,
  available  INT NOT NULL,
  version    INT NOT NULL DEFAULT 0,
  CHECK (available >= 0 AND available <= total)
) ENGINE=InnoDB;

CREATE TABLE seckill_order (
  id          BIGINT PRIMARY KEY,
  activity_id VARCHAR(32) NOT NULL,
  user_id     BIGINT NOT NULL,
  status      TINYINT NOT NULL,
  created_at  DATETIME(3) NOT NULL,
  UNIQUE KEY uk_user_act (activity_id, user_id)
) ENGINE=InnoDB;

CREATE TABLE reservation (
  idem_key   VARCHAR(64) PRIMARY KEY,
  user_id    BIGINT NOT NULL,
  state      TINYINT NOT NULL,
  expire_at  DATETIME(3) NOT NULL,
  INDEX idx_state_expire (state, expire_at)
) ENGINE=InnoDB;
```

内部接口签名保持窄而稳定，下面这段自带完整上下文，可单独编译验证：

```go
// 文件：contract.go —— 秒杀契约：窄接口与领域错误
package main

import (
	"errors"
	"fmt"
)

var (
	ErrSoldOut   = errors.New("seckill: sold out")
	ErrNoTicket  = errors.New("seckill: no ticket")
	ErrDuplicate = errors.New("seckill: duplicate")
)

// 三个窄接口把限流配额、原子预扣、可靠落库解耦，便于分别替换实现。
type Quota interface {
	Take() bool
}
type Stock interface {
	Deduct(userID string) (bool, error)
	Refund(userID string) error
	Left() int
}
type TicketSink interface {
	Enqueue(userID string) bool
	Result(userID string) string
}

// OrderStatus 是排队结果的状态机，轮询接口只返回终态或处理中。
type OrderStatus string

const (
	StatusQueued   OrderStatus = "QUEUED"
	StatusPaid     OrderStatus = "PAID"
	StatusReleased OrderStatus = "RELEASED"
)

func main() {
	var s OrderStatus = StatusQueued
	fmt.Printf("initial=%s\n", s)
}
```

对外 API 与轮询协议（客户端拿到排队号后每秒轮询，最多三次）：

| 对外 API | 方法 | 关键字段 | 成功响应 | 主要错误码 |
| --- | --- | --- | --- | --- |
| /seckill | POST | activity_id、user_id、token | 202 + 排队号 | 400 参数、403 黑名单、429 限流、409 售罄、503 繁忙 |
| /seckill/result | GET | user_id、ticket | 200 + QUEUED / PAID / RELEASED | 404 无记录 |

```json
{
  "activity_id": "act-2026-1111",
  "user_id": "u-8801",
  "token": "answer-verify-token"
}
```

预扣状态机与幂等键设计是本节重点。状态判断与变更必须原子，否则「已支付后释放」会把库存凭空加回：

| 状态 | 含义 | 允许的下一状态 | 触发动作 |
| --- | --- | --- | --- |
| RESERVED | 已预扣、未支付 | PAID、RELEASED | 支付成功条件更新为 PAID；超时条件更新为 RELEASED 并回补 |
| PAID | 已支付 | 终态 | 拒绝任何释放，防止竞态回补 |
| RELEASED | 已回补 | 终态 | 拒绝重复释放，防止库存漂移 |

| 幂等键作用域 | 载体 | 冲突行为 | TTL |
| --- | --- | --- | --- |
| 用户去重 | Redis Set sk:{act}:dedup | 重复请求返回首次结果，不再扣减 | 活动结束 |
| 预扣记录 | reservation 表唯一键 | 重复预扣直接返回首次预留 ID | 15 分钟 |
| 订单落库 | uk_user_act 唯一索引 | 重复消费直接 ack | 永久 |
| MQ 消费 | 消息 ID 去重表 | 重复消息直接 ack | 7 天 |

组件调用矩阵把「谁调谁、怎么调、失败怎么办」一次列清：

| 调用方 | 被调方 | 接口 / 命令 | 协议 | 同步性 | 超时 | 幂等键 | 失败兜底 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 客户端 | 网关 | POST /seckill | HTTPS | 同步 | 3s | token | 429 快速失败 |
| 网关 | 令牌桶 | Allow | 进程内 | 同步 | 不适用 | 无 | 429 加 Retry-After |
| 网关 | 配额 | Take | Redis | 同步 | 50ms | 活动 ID | 售罄 |
| 网关 | Redis | Lua 预扣 | Redis | 同步 | 5ms | 用户 ID | 售罄，不重试扣减 |
| 网关 | 队列 | Enqueue | 内存 channel | 同步 | 不适用 | 排队号 | 失败立即 Refund 回补 |
| worker | 订单库 | INSERT | SQL | 异步 | 1s | uk_user_act | 重试三次后进对账 |
| 对账 | Redis / DB | 条件更新 | Redis / SQL | 异步 | 2s | reservation 键 | 毒事件进人工队列 |

<Compare title="两种库存写法的取舍" :panels='[{"label":"先查后扣（错误）","code":"SELECT stock; if 余量大于 0 then UPDATE 减一","note":"检查与扣减是两步，并发下都读到旧值，直接把库存卖成负数"},{"label":"原子条件更新","code":"UPDATE stock SET available=available-1 WHERE sku=? AND available 大于 0","note":"检查与扣减由同一条语句或同一条 Lua 完成，影响行数即抢到数，是本讲选择"}]' />

<CodeTabs title="修复超卖的三条路线" :tabs='[{"label":"数据库条件更新","code":"UPDATE seckill_stock SET available=available-1 WHERE sku=? AND available 大于 0","note":"最简单、不引入新组件，但受连接池限制，扛不住十万级洪峰"},{"label":"Redis 单条 Lua","code":"判重、判余量、扣减、写记录合成一次 Redis 调用","note":"单线程原子、吞吐高，代价是要处理与 DB 的最终一致，是本讲主线"},{"label":"分段库存","code":"把 100 件拆成 10 段各 10 件，按用户哈希落到不同 key","note":"把单 key 热点摊开，代价是要处理碎片与总余量对账"}]' />

## 5. 核心链路时序

<SequenceDiagram title="一次抢购：限流、预扣、排队、落库与轮询" caption="第 9 步的异步写库与第 11 步的轮询共同构成预扣到终态的真空期" :actors='["Client","Gateway","Limiter","Stock","Queue","Order"]' :steps='[{"from":"Client","to":"Gateway","label":"POST /seckill","note":"带答题 token","kind":"call"},{"from":"Gateway","to":"Limiter","label":"取令牌","note":"1 万 QPS","kind":"call"},{"from":"Limiter","to":"Gateway","label":"放行或拒绝","kind":"return"},{"from":"Gateway","to":"Stock","label":"Lua 原子预扣","note":"检查扣减合一","kind":"call"},{"from":"Stock","to":"Gateway","label":"返回抢到","kind":"return"},{"from":"Gateway","to":"Queue","label":"投递排队号","note":"非阻塞入队","kind":"call"},{"from":"Queue","to":"Gateway","label":"返回受理","kind":"return"},{"from":"Gateway","to":"Client","label":"202 排队中","kind":"return"},{"from":"Queue","to":"Order","label":"匀速写库","note":"4 worker","kind":"async"},{"from":"Order","to":"Order","label":"置 PAID","note":"唯一索引","kind":"self"},{"from":"Client","to":"Gateway","label":"轮询结果","note":"每秒一次","kind":"call"},{"from":"Gateway","to":"Client","label":"返回 PAID","kind":"return"}]' />

<StepBox title="逐步骤解读：每一步在守什么不变量" :steps='[{"title":"1 准入与限流","body":"校验 token 与黑名单，令牌桶把 10 万 QPS 削成 1 万 QPS，一次性配额只放 300 个进入预扣","tip":"越早拒绝越便宜，被拒请求绝不碰库存与数据库"},{"title":"2 原子预扣","body":"一条 Lua 内判重、判余量、扣减、写预约记录，返回是否抢到","tip":"检查与扣减之间没有缝，是 FR-1 的根本保证"},{"title":"3 入队与回补","body":"抢到的请求非阻塞入队；队列满立即 Refund 回补，避免抢到却没订单","tip":"入队失败必须回补，否则既少卖又不一致"},{"title":"4 异步落库","body":"4 个 worker 匀速取号，按 uk_user_act 幂等写订单，失败按预算重试","tip":"数据库只承受不超过 200 QPS 的匀速写"},{"title":"5 轮询结果","body":"用户凭排队号每秒轮询，最多三次读到 QUEUED 到 PAID 的终态","tip":"给用户的是排队号，不是成功承诺"},{"title":"6 超时收敛","body":"15 分钟未支付的预约由延迟任务条件回补，对账任务校验库存守恒","tip":"条件更新只释放仍为 RESERVED 的记录"}]' startLabel="开抢" />

异常链路与正常链路有三处分叉。第一处在第 2 步：配额或库存不足直接返回 429 / 409，不重试扣减——重试只会把已经售罄的 key 再打一遍，属于典型的重试放大。第二处在第 6 步：队列已满说明下游饱和，`Enqueue` 返回 false，必须立刻 `Refund` 回补预扣再返回 503，否则用户「抢到」了却永远等不到订单，既少卖又不一致。第三处最隐蔽，在第 9 步：worker 写库成功但进程随后崩溃，或消息投递丢失，预扣记录停在 RESERVED、订单表却没有对应行，这段真空期不会自己愈合，必须靠可靠消息至少一次投递加消费幂等、再加定时对账把差异扫出来并回补。与第 115 讲 Saga 不同的是，秒杀链路没有逆序补偿那一套，它的兜底是「库存守恒 + 条件回补」，因为这里唯一不能破的就是余量不为负。

## 6. 核心实现（按模块逐段展开）

以下八段代码**各自自包含、可单独 `go run` 或 `go test` 编译验证**。为便于评审把每个机制单独拎出来核对，公共类型在每段内以最小形式重定义；真实工程里它们会收进 `internal/contract` 与各自的包。代价是少量类型重复，收益是任何一段都能独立证明一个不变量。

### 6.1 原子预扣：把检查与扣减合成一步

**设计意图**：把 FR-1 落成代码。关键取舍是「检查余量、判重、扣减、写预约记录」四件事必须处在同一个 `sync.Mutex` 临界区内（生产里换成一条 Redis Lua），形成严格串行序列；`Refund` 用条件更新只释放仍为 `RESERVED` 的记录，同时挡住空回补、重复回补与已支付回补。代价是单 key 串行化会成为热点（6.5 用分段解决），但这是换取绝不超卖必须付的代价。

```go
// 文件：stock.go —— 原子预扣：检查、判重、扣减同临界区
package main

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrSoldOut       = errors.New("seckill: sold out")
	ErrNoReservation = errors.New("seckill: no reservation")
	ErrAlreadyPaid   = errors.New("seckill: already paid")
)

type resState uint8

const (
	resReserved resState = iota + 1
	resPaid
	resReleased
)

type reservation struct {
	userID string
	state  resState
}

// Stock 用一把锁把检查、判重、扣减合成一个原子操作，
// 对应 Redis 里一条 Lua 脚本，杜绝先查后扣造成的超卖。
type Stock struct {
	mu      sync.Mutex
	left    int
	records map[string]reservation
}

func NewStock(total int) *Stock {
	return &Stock{left: total, records: make(map[string]reservation)}
}

// Deduct 返回是否本次新抢到。四步处在同一临界区，形成严格串行序列。
func (s *Stock) Deduct(userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[userID]; ok {
		return false, nil // 幂等：重复请求返回首次结果，不再扣减
	}
	if s.left <= 0 {
		return false, ErrSoldOut
	}
	s.left--
	s.records[userID] = reservation{userID: userID, state: resReserved}
	return true, nil
}

// MarkPaid 用条件更新把 RESERVED 置为 PAID；只有当前仍是预留中才成功，
// 这样超时释放任务与支付不会竞态把库存凭空加回。
func (s *Stock) MarkPaid(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[userID]
	if !ok || r.state != resReserved {
		return ErrAlreadyPaid
	}
	r.state = resPaid
	s.records[userID] = r
	return nil
}

// Refund 条件回补：仅当状态仍为 RESERVED 时释放，拒绝空回补与已支付回补。
func (s *Stock) Refund(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[userID]
	if !ok || r.state != resReserved {
		return ErrNoReservation
	}
	s.left++
	r.state = resReleased
	s.records[userID] = r
	return nil
}

func (s *Stock) Left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}

func main() {
	s := NewStock(100)
	granted := 0
	for i := 0; i < 1000; i++ {
		ok, _ := s.Deduct(fmt.Sprintf("u-%d", i))
		if ok {
			granted++
		}
	}
	fmt.Printf("granted=%d left=%d\n", granted, s.Left())
	_ = s.Refund("u-0")
	fmt.Printf("after refund left=%d\n", s.Left())
	_ = s.Refund("u-0") // 重复回补被挡
	fmt.Printf("after duplicate refund left=%d\n", s.Left())
}
```

### 6.2 令牌桶与一次性配额

**设计意图**：演示两层削峰的区别。关键取舍是 `Limiter` 按速率补水用于稳态限流，`Quota` 不补水、按「库存乘以放大系数」一次性放号专治秒杀——如果只用补水令牌桶，10 万 QPS 会被持续放进预扣临界区，把 Redis 打爆；只用一次性配额又无法平滑流量。两者串联，正好把 10 万削到 1 万再削到 300。代价是多一层计数与配置，好处是漏斗每一级都可观测、可灰度调参。

```go
// 文件：limiter.go —— 令牌桶稳态限流 + 一次性配额放号
package main

import (
	"fmt"
	"sync"
	"time"
)

// Limiter 是标准库实现的令牌桶：按速率补充令牌，把洪峰削成匀速。
type Limiter struct {
	mu       sync.Mutex
	rate     float64
	capacity float64
	tokens   float64
	last     time.Time
}

func NewLimiter(rate, capacity float64) *Limiter {
	return &Limiter{rate: rate, capacity: capacity, tokens: capacity, last: time.Now()}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.tokens += now.Sub(l.last).Seconds() * l.rate
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// Quota 是一次性令牌池：活动开始按库存乘以放大系数放号，抢完即止。
// 与 Limiter 的区别是不补水，专治秒杀这种一次性洪峰。
type Quota struct {
	mu   sync.Mutex
	left int
}

func NewQuota(n int) *Quota { return &Quota{left: n} }

func (q *Quota) Take() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.left <= 0 {
		return false
	}
	q.left--
	return true
}

func (q *Quota) Left() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.left
}

func main() {
	l := NewLimiter(10000, 10000)
	passed := 0
	for i := 0; i < 100000; i++ {
		if l.Allow() {
			passed++
		}
	}
	q := NewQuota(300)
	admitted := 0
	for i := 0; i < passed; i++ {
		if q.Take() {
			admitted++
		}
	}
	fmt.Printf("limiter passed=%d quota admitted=%d quota left=%d\n", passed, admitted, q.Left())
}
```

<OutputBox title="削峰漏斗实测" output="limiter passed=10000 quota admitted=300 quota left=0" note="10 万次请求经令牌桶剩 1 万，再经一次性配额只剩 300 个进入预扣，与第 1 节指标完全对齐" />

### 6.3 有界队列：把洪峰摊平成匀速写库

**设计意图**：解耦「内存预扣成功」与「异步写库」。关键取舍是 `Enqueue` 用带 `default` 的 `select` 做非阻塞入队，队列满立即返回 false 让上层回补并快速失败，绝不阻塞等待把线程挂死；worker 落库失败按预算重试，耗尽后通过 `onFail` 回调回补预扣，绝不静默丢弃。代价是队列长度需要按库存与 worker 能力估算（容量 200 = 库存 100 的两倍），配小了会误杀、配大了失去背压意义。

```go
// 文件：queue.go —— 有界队列把洪峰摊平成匀速写库，失败回补不丢单
package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// OrderSink 抽象最终落库；Save 必须按排队号或用户 ID 幂等。
type OrderSink interface {
	Save(ctx context.Context, userID string) error
}

type countingSink struct{ saved int64 }

func (c *countingSink) Save(ctx context.Context, userID string) error {
	atomic.AddInt64(&c.saved, 1)
	return nil
}

// Queue 的 worker 匀速消费，失败按预算重试，仍失败交给对账兜底。
type Queue struct {
	ch     chan string
	sink   OrderSink
	wg     sync.WaitGroup
	maxTry int
	onFail func(userID string)
	failed int64
}

func NewQueue(size, workers int, sink OrderSink, onFail func(string)) *Queue {
	q := &Queue{ch: make(chan string, size), sink: sink, maxTry: 3, onFail: onFail}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for userID := range q.ch {
		ok := false
		for attempt := 1; attempt <= q.maxTry; attempt++ {
			if q.sink.Save(context.Background(), userID) == nil {
				ok = true
				break
			}
			time.Sleep(time.Duration(attempt) * time.Millisecond)
		}
		if !ok {
			atomic.AddInt64(&q.failed, 1)
			q.onFail(userID) // 回补预扣，避免抢到却没有订单
		}
	}
}

// Enqueue 非阻塞入队：队列满说明下游饱和，直接返回让上层回补并快速失败。
func (q *Queue) Enqueue(userID string) bool {
	select {
	case q.ch <- userID:
		return true
	default:
		return false
	}
}

func (q *Queue) Close() {
	close(q.ch)
	q.wg.Wait()
}

func main() {
	sink := &countingSink{}
	refunded := 0
	var mu sync.Mutex
	q := NewQueue(200, 4, sink, func(string) {
		mu.Lock()
		refunded++
		mu.Unlock()
	})
	for i := 0; i < 100; i++ {
		q.Enqueue(fmt.Sprintf("u-%d", i))
	}
	q.Close()
	fmt.Printf("saved=%d failed=%d refunded=%d\n",
		atomic.LoadInt64(&sink.saved), atomic.LoadInt64(&q.failed), refunded)
}
```

<LineByLine code="close(q.ch)\nq.wg.Wait()" :notes='["关闭 channel，让各 worker 的 range 循环自然退出，不再接收新任务","等待所有在途任务写完再返回，避免优雅退出时丢掉队列里尚未落库的单"]' />

### 6.4 网关串联与结果轮询

**设计意图**：把横切编排收在一处，并给出「抢到不等于成功」的正确用户契约。关键取舍是四个依赖以函数注入，单测可任意替换；`Enqueue` 失败必须立刻 `Refund` 回补，否则出现「预扣成功却没有订单」；返回给用户的是 202 加排队号而不是 201 成功，终态由 `/seckill/result` 轮询给出。代价是客户端要多一次轮询逻辑，换来的是异步真空期对用户透明且不会说谎。

```go
// 文件：handler.go —— 网关串联：限流、预扣、入队、回补与结果轮询
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Handler 的四个依赖以函数注入，单测里可任意替换，演示聚焦编排与回补。
type Handler struct {
	take    func() bool
	deduct  func(string) (bool, error)
	refund  func(string) error
	enqueue func(string) bool
	mu      sync.Mutex
	result  map[string]string
}

func NewHandler(take func() bool, deduct func(string) (bool, error),
	refund func(string) error, enqueue func(string) bool) *Handler {
	return &Handler{take: take, deduct: deduct, refund: refund,
		enqueue: enqueue, result: make(map[string]string)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/seckill/result" {
		h.serveResult(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil || req.UserID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !h.take() {
		http.Error(w, "sold out", http.StatusTooManyRequests)
		return
	}
	ok, err := h.deduct(req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if !ok {
		h.reply(w, "already processed", http.StatusOK)
		return
	}
	if !h.enqueue(req.UserID) {
		_ = h.refund(req.UserID) // 队列满必须回补，否则抢到却没订单
		http.Error(w, "system busy", http.StatusServiceUnavailable)
		return
	}
	h.mu.Lock()
	h.result[req.UserID] = "QUEUED"
	h.mu.Unlock()
	h.reply(w, "queued", http.StatusAccepted)
}

func (h *Handler) serveResult(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	h.mu.Lock()
	status := h.result[userID]
	h.mu.Unlock()
	if status == "" {
		status = "NOT_FOUND"
	}
	h.reply(w, status, http.StatusOK)
}

func (h *Handler) reply(w http.ResponseWriter, status string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

func main() {
	quotaLeft, stockLeft := 300, 100
	seen, enqueued := make(map[string]bool), 0
	var mu sync.Mutex
	h := NewHandler(
		func() bool {
			mu.Lock()
			defer mu.Unlock()
			if quotaLeft <= 0 {
				return false
			}
			quotaLeft--
			return true
		},
		func(u string) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if seen[u] {
				return false, nil
			}
			if stockLeft <= 0 {
				return false, fmt.Errorf("sold out")
			}
			stockLeft--
			seen[u] = true
			return true, nil
		},
		func(u string) error {
			mu.Lock()
			defer mu.Unlock()
			if seen[u] {
				delete(seen, u)
				stockLeft++
			}
			return nil
		},
		func(u string) bool {
			mu.Lock()
			defer mu.Unlock()
			if enqueued >= 200 {
				return false
			}
			enqueued++
			return true
		},
	)
	accepted := 0
	for i := 0; i < 5000; i++ {
		req := httptest.NewRequest(http.MethodPost, "/seckill",
			strings.NewReader(fmt.Sprintf(`{"user_id":"u-%d"}`, i)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusAccepted {
			accepted++
		}
	}
	fmt.Printf("accepted=%d enqueued=%d stockLeft=%d\n", accepted, enqueued, stockLeft)
}
```

### 6.5 分段库存：拆开热点账户与热点 key

**设计意图**：解决 6.1 单把锁的串行瓶颈。关键取舍是把 100 件拆成 10 段各 10 件、每段一把锁，按用户哈希落到主段，命中不同段的扣减真正并行；主段售罄时顺序探测其它段回收碎片。代价是引入碎片与「总余量需要跨段求和」的对账复杂度，且哈希倾斜时仍可能有段先空——所以生产里常配合动态调段与总水位大盘。账户侧同理：把单一商户收款账户拆成多个子账户异步汇总，避免一行热点。

```go
// 文件：segmented.go —— 分段库存把单 key 热点摊到 N 把锁
package main

import (
	"fmt"
	"sync"
)

// Segment 是一段库存，各自一把锁，互不阻塞。
type Segment struct {
	mu   sync.Mutex
	left int
}

// SegmentedStock 把总量切成 N 段，请求按用户 ID 哈希落到某一段，
// 命中不同段的扣减可以真正并行，缓解单 key 串行瓶颈。
type SegmentedStock struct {
	segs []*Segment
	n    int
}

func NewSegmentedStock(total, n int) *SegmentedStock {
	s := &SegmentedStock{segs: make([]*Segment, n), n: n}
	base, extra := total/n, total%n
	for i := 0; i < n; i++ {
		q := base
		if i < extra {
			q++
		}
		s.segs[i] = &Segment{left: q}
	}
	return s
}

// Deduct 先按哈希定位主段，不足时顺序探测其它段，兼顾并行与碎片回收。
func (s *SegmentedStock) Deduct(userID string) bool {
	start := int(hash(userID) % uint64(s.n))
	for k := 0; k < s.n; k++ {
		seg := s.segs[(start+k)%s.n]
		seg.mu.Lock()
		if seg.left > 0 {
			seg.left--
			seg.mu.Unlock()
			return true
		}
		seg.mu.Unlock()
	}
	return false
}

func hash(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func (s *SegmentedStock) Left() int {
	total := 0
	for _, seg := range s.segs {
		seg.mu.Lock()
		total += seg.left
		seg.mu.Unlock()
	}
	return total
}

func main() {
	st := NewSegmentedStock(100, 10)
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if st.Deduct(fmt.Sprintf("u-%d", id)) {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	fmt.Printf("granted=%d left=%d want=100/0\n", granted, st.Left())
}
```

### 6.6 超时释放与对账：条件更新防竞态

**设计意图**：补上「预扣到落库」这段真空期最棘手的一处竞态。关键取舍是 `MarkPaid` 与 `Release` 都是条件更新——只当状态仍为 `RESERVED` 才生效，且判断与变更同临界区。支付先到则释放影响 0 条（不回补），释放先到则支付被拒，两种时序都不会把库存凭空加回。`Reconcile` 定时扫描超时预留并回补，对账以库存为准、以回补为兜底。代价是必须持久化预约时间戳，且延迟队列的时钟偏差要受第 1 节的 50ms 约束。

```go
// 文件：reconcile.go —— 超时释放与对账：条件更新防已支付后释放
package main

import (
	"fmt"
	"sync"
	"time"
)

type resState uint8

const (
	reserved resState = iota + 1
	paid
	released
)

type record struct {
	userID    string
	state     resState
	createdAt time.Time
}

// Ledger 同时保存预扣记录与库存水位；所有读状态加改状态都在同一把锁内。
type Ledger struct {
	mu      sync.Mutex
	left    int
	records map[string]record
}

func NewLedger(total int) *Ledger {
	return &Ledger{left: total, records: make(map[string]record)}
}

func (l *Ledger) Reserve(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.left <= 0 {
		return false
	}
	l.left--
	l.records[userID] = record{userID: userID, state: reserved, createdAt: time.Now()}
	return true
}

// MarkPaid 与 Release 都是条件更新：仅当状态仍为 reserved 才生效。
// 支付先到则 Release 不回补，释放先到则支付被拒，杜绝竞态。
func (l *Ledger) MarkPaid(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[userID]
	if !ok || r.state != reserved {
		return false
	}
	r.state = paid
	l.records[userID] = r
	return true
}

func (l *Ledger) Release(userID string, timeout time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[userID]
	if !ok || r.state != reserved || time.Since(r.createdAt) < timeout {
		return false
	}
	r.state = released
	l.records[userID] = r
	l.left++
	return true
}

// Reconcile 扫描所有预留中记录，释放超时的；返回释放条数供对账断言。
func (l *Ledger) Reconcile(timeout time.Duration) (freed int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, r := range l.records {
		if r.state == reserved && time.Since(r.createdAt) >= timeout {
			r.state = released
			l.records[id] = r
			l.left++
			freed++
		}
	}
	return freed
}

func (l *Ledger) Left() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.left
}

func main() {
	led := NewLedger(100)
	led.Reserve("u-1")
	led.Reserve("u-2")
	led.MarkPaid("u-1") // 已支付
	n := led.Reconcile(time.Nanosecond)
	_ = led.Release("u-1", time.Nanosecond) // 已支付记录不应被回补
	fmt.Printf("freed=%d left=%d want=1/99\n", n, led.Left())
}
```

<Callout type="warn" title="一致性缺口藏在预扣与落库之间" text="Redis 扣减成功后到订单真正写进数据库之间有一段真空期：进程崩溃、MQ 抖动、worker 报错都会让这段永远接不上。必须靠可靠消息加幂等消费加定时对账把缺口补上，且对账要以库存为准、以回补为兜底，而不是假装消息一定送达；直接回补还可能和已支付请求竞态，所以回补必须带状态条件。" />

### 6.7 限流降级与熔断

**设计意图**：让局部故障不演变成全站雪崩。关键取舍是三态状态机——连续失败超阈值打开熔断，`open` 期间直接拒绝新请求，`cooldown` 后进入 `half-open` 只放一个试探，成功回 `closed`、失败重新 `open`；降级由调用方在拿到 `ErrCircuitOpen` 时返回静态兜底。代价是熔断期间会误拒少量本可成功的请求，但这正是用可用性换下游不被打垮的取舍。

```go
// 文件：circuit.go —— 熔断器状态机与降级兜底
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("circuit: open")

type state uint8

const (
	closed state = iota
	open
	halfOpen
)

// Breaker 统计连续失败，超阈值打开熔断；冷却后半开试探，
// 试探成功回到 closed，失败重新 open。
type Breaker struct {
	mu        sync.Mutex
	state     state
	failures  int
	threshold int
	openUntil time.Time
	cooldown  time.Duration
	now       func() time.Time
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{state: closed, threshold: threshold, cooldown: cooldown, now: time.Now}
}

func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case open:
		if b.now().Before(b.openUntil) {
			return ErrCircuitOpen
		}
		b.state = halfOpen
	case halfOpen:
		return ErrCircuitOpen // 半开只放一个试探请求
	}
	return nil
}

func (b *Breaker) Report(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.failures = 0
		b.state = closed
		return
	}
	b.failures++
	if b.failures >= b.threshold || b.state == halfOpen {
		b.state = open
		b.openUntil = b.now().Add(b.cooldown)
	}
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [...]string{"closed", "open", "half-open"}[b.state]
}

func main() {
	b := NewBreaker(3, 50*time.Millisecond)
	for i := 0; i < 3; i++ {
		b.Report(false)
	}
	fmt.Println("after 3 failures:", b.State(), "allow:", b.Allow())
	time.Sleep(60 * time.Millisecond)
	fmt.Println("after cooldown allow:", b.Allow(), "state:", b.State())
	b.Report(true)
	fmt.Println("after probe success:", b.State())
}
```

### 6.8 表驱动测试：不超卖、回补幂等与竞态

**设计意图**：把「绝不超卖」变成可重复执行的断言。关键取舍是 mock 边界切在纯内存实现上——被测的扣减、状态机、条件更新都是真代码，不依赖 Redis 与数据库就能在 CI 里稳定复现；`TestNoOversell` 用 1000 个并发 goroutine 抢 100 件，断言成功数恰好 100、余量精确 0；`TestPaidVsReleaseRace` 显式覆盖支付先到与释放先到两种时序。代价是 Redis Lua 的真实原子性要用集成测试单独验证，两者不混在一起。

```go
// 文件：stock_test.go —— 表驱动覆盖不超卖、回补幂等与超时释放竞态
package seckill

import (
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errSoldOut = errors.New("sold out")

type resState uint8

const (
	reserved resState = iota + 1
	paid
	released
)

type record struct {
	state resState
	at    time.Time
}

type stock struct {
	mu      sync.Mutex
	remain  int
	records map[string]record
}

func newStock(n int) *stock {
	return &stock{remain: n, records: make(map[string]record)}
}

func (s *stock) deduct(u string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[u]; ok {
		return false, nil
	}
	if s.remain <= 0 {
		return false, errSoldOut
	}
	s.remain--
	s.records[u] = record{state: reserved, at: time.Now()}
	return true, nil
}

func (s *stock) markPaid(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[u]
	if !ok || r.state != reserved {
		return false
	}
	r.state = paid
	s.records[u] = r
	return true
}

func (s *stock) release(u string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[u]
	if !ok || r.state != reserved {
		return false
	}
	r.state = released
	s.records[u] = r
	s.remain++
	return true
}

func (s *stock) left() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.remain
}

func TestNoOversell(t *testing.T) {
	cases := []struct{ total, users int }{{100, 1000}, {1, 50}, {0, 10}}
	for _, c := range cases {
		s := newStock(c.total)
		var success int64
		var wg sync.WaitGroup
		for i := 0; i < c.users; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				if ok, _ := s.deduct("u" + strconv.Itoa(id)); ok {
					atomic.AddInt64(&success, 1)
				}
			}(i)
		}
		wg.Wait()
		want := int64(c.total)
		if c.total > c.users {
			want = int64(c.users)
		}
		if success != want {
			t.Fatalf("total=%d users=%d success=%d want=%d", c.total, c.users, success, want)
		}
		if s.left() != c.total-int(want) {
			t.Fatalf("left=%d", s.left())
		}
	}
}

func TestRefundIdempotent(t *testing.T) {
	s := newStock(1)
	if ok, _ := s.deduct("u1"); !ok {
		t.Fatal("want granted")
	}
	if !s.release("u1") {
		t.Fatal("want released")
	}
	if s.release("u1") {
		t.Fatal("duplicate release should fail")
	}
	if s.left() != 1 {
		t.Fatalf("left=%d", s.left())
	}
}

func TestPaidVsReleaseRace(t *testing.T) {
	cases := []struct {
		payFirst bool
		wantLeft int
	}{{true, 0}, {false, 1}}
	for _, c := range cases {
		s := newStock(1)
		_, _ = s.deduct("u1")
		if c.payFirst {
			s.markPaid("u1")
			s.release("u1")
		} else {
			s.release("u1")
			s.markPaid("u1")
		}
		if s.left() != c.wantLeft {
			t.Fatalf("payFirst=%v left=%d want=%d", c.payFirst, s.left(), c.wantLeft)
		}
	}
}

func BenchmarkDeduct(b *testing.B) {
	s := newStock(1 << 30)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = s.deduct("u" + strconv.Itoa(i))
	}
}
```

## 7. 失败路径与边界条件

失败矩阵必须具体到「哪个组件挂了、什么现象、怎么发现、怎么兜底、怎么恢复」，否则评审等于没做。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 队列满 | 入队速率超过 worker 消费 | 预扣成功却没订单 | 队列长度与 503 比例告警 | `Enqueue` 失败立即 `Refund` 回补并快速失败 | 扩容 worker 或调大队列后重新开放入口 |
| 落库失败被静默丢弃 | worker 写库报错只打日志 | 长期不一致、用户轮询不到 | 对账差异计数不为零 | 进重试队列加定时对账，绝不只打日志 | 重放消息或按库存回补 |
| 用户重复点击 | 脚本或手抖连点 | 重复扣减、重复下单 | 去重集合命中率突增 | 幂等键去重，重复请求返回首次结果 | 自动，无需人工 |
| 超时未支付 | 抢到后 15 分钟未付款 | 库存被永久占住 | 预约表 RESERVED 数超阈 | 延迟任务条件释放，`Refund` 幂等回补 | 回补后水位恢复，活动内可再放 |
| 热点单 key | 所有请求打同一 SKU | 单分片串行、CPU 空转在锁上 | 锁等待直方图与单 key QPS | 分段库存、本地库存加中央水位、请求合并 | 动态调段并重放碎片 |
| 限流误杀 | 配额或阈值配得过低 | 正常用户被 429 | 429 比例与转化漏斗突变 | 配额留余量、分层限流、灰度调参 | 回滚阈值并灰度验证 |
| 回补竞态 | 回补与已支付交错 | 库存被凭空加回 | 总水位大于总库存 | 回补带状态条件，仅 RESERVED 生效 | 对账以库存为准强制收敛 |
| Redis 主宕机 | 主库故障 | 预扣全部失败 | 健康实例数归零 | 入口降级为静态公告页，暂停活动 | 主从切换后重新预热库存与配额 |
| worker 崩溃 | 进程被 kill | 队列积压、订单落后 | 队列长度与消费位点差 | 消息至少一次投递加幂等消费 | 重启后重放未 ack 消息 |
| 配置错误 | 配额误配为 10 万 | 洪峰直灌数据库 | 落库 QPS 突破 200 告警 | 启动时范围校验加配置中心审计 | 回滚配置，冻结活动后重开 |

**幂等键**：用户 ID 加活动 ID 全链路唯一，重复预扣返回首次结果；订单落库靠 `uk_user_act` 唯一索引。**重试预算**：预扣售罄绝不重试扣减（否则放大热点），落库最多 3 次带退避，MQ 至少一次投递加消费幂等；重试总耗时必须小于轮询预算。**超时传递**：网关 3s、Redis Lua 5ms、worker 写库 1s、对账 2s，逐层递减，任一层超时立即返回可判定结果。**降级与人工介入点**：Redis 不可用降级静态页、下游连续失败打开熔断、对账毒事件与补偿重试耗尽三类必须落人工队列，并在大盘单列「待人工处理」计数，禁止自动吞掉。

<Callout type="idea" title="可少卖不可超卖" text="宁可因队列满、超时未支付、限流误杀而少成交几单并回补库存，也绝不让成交数超过真实库存。前者是运营可调的转化损失，后者是资损与客诉。所有边界判断都要朝这个方向倾斜：拿不准就拒绝，拿不准就回补，绝不拿不准就放行。" />

## 8. 并发与一致性

**超卖的根因**不是「并发太大」，而是 read-modify-write 之间有一道缝：两个事务同时 `SELECT` 到余量 1，各自判断通过，再各自 `UPDATE` 减 1，最终余量变成负一。把检查与扣减收进同一个原子操作，是唯一的正解。下面给出至少两种修复方案的对照，并说明各自代价。

| 方案 | 是否超卖 | 吞吐上限 | 复杂度 | 代价 | 适用 |
| --- | --- | --- | --- | --- | --- |
| 先查后扣（read 后 write 两步） | 会 | 低 | 最低 | 直接超卖，生产禁用 | 仅作反面教材 |
| 数据库条件更新 `WHERE available 大于 0` | 不会 | 受连接池限制约 3000 QPS | 低 | 扛不住十万级洪峰，行锁热点 | 中小规模或兜底校验 |
| Redis 单条 Lua / DECR | 不会 | 单节点 5 万到 8 万 QPS | 中 | 需处理与 DB 的最终一致 | 本讲主线 |
| 分段库存（100 拆 10 段） | 不会 | 更高，摊开热点 | 高 | 碎片、总水位对账、哈希倾斜 | 极端热点 SKU |

第一个修复方案（数据库条件更新）的等价 SQL 是 `UPDATE seckill_stock SET available=available-1 WHERE sku=? AND available 大于 0`，影响行数即抢到数；第二个修复方案（Redis Lua）见第 4 节脚本，把判重、判余量、扣减、写记录合成一次调用。两者共同点是「检查与变更同一步」，区别只是把临界区放在数据库行锁还是 Redis 单线程。第三种分段库存见 6.5，它不改变原子性，只是把一把锁拆成 N 把来摊开热点——和第 113 讲的锁分片、第 114 讲的分片路由是同一个思想在不同层面的复用。

并发模型上，网关每请求一个 goroutine，预扣临界区严格串行，队列 worker 固定 4 个匀速消费。**锁粒度**刻意收在「单个 SKU 或单个分段」而非全局；`sync.Mutex` 在单机演示够用，生产换成 Redis Lua 后由单线程保证原子，这正是第 097 讲锁竞争拐点给出的结论：并发越高，越多 goroutine 卡在同一把锁上，CPU 花在排队而非干活。

竞态分析集中在三处：预扣与回补共用同一临界区（6.1）；支付与超时释放靠条件状态机互斥（6.6）；订单落库靠唯一索引幂等。一致性级别选「不超卖 + 最终一致」而非强一致，依据是业务无法接受 2PC 把 10 万 QPS 全堵在协调者上，却能接受秒级延迟与少量少卖；中间态（RESERVED 但无订单）对外通过轮询暴露为 QUEUED，最终靠可靠消息与对账收敛到 PAID 或 RELEASED。顺序保证上，同一用户的重复请求被去重集合挡住，同一订单的消息按排队号幂等消费，乱序的释放事件被状态机条件挡住——把顺序问题统一转化为幂等问题。

<Compare title="扣减库存的两种写法（评审必看）" :panels='[{"label":"错误：先查后扣","code":"SELECT available; if available 大于 0 then UPDATE available=available-1","note":"检查与扣减两条语句，并发都读到旧值，100 件能卖出 137 件"},{"label":"正确：原子条件更新","code":"UPDATE seckill_stock SET available=available-1 WHERE sku=? AND available 大于 0","note":"影响行数即抢到数，检查与扣减由同一语句或同一条 Lua 完成"}]' />

<StepBox title="一次并发抢购的收敛过程" :steps='[{"title":"T0 洪峰到达","body":"30 秒内 10 万 QPS 打到网关，令牌桶先削成 1 万 QPS","tip":"越早拒绝越便宜"},{"title":"T1 配额放号","body":"一次性配额只放 300 个请求进入预扣，其余返回售罄","tip":"按库存乘以放大系数 3 设定"},{"title":"T2 原子预扣","body":"一条 Lua 内判重判余量扣减，前 100 个抢到，后 200 个售罄","tip":"严格串行，成交数恒等于库存数"},{"title":"T3 排队落库","body":"100 个请求入有界队列，4 worker 按 200 QPS 匀速写单","tip":"数据库写入峰值不超过 200 QPS"},{"title":"T4 收敛","body":"轮询返回 PAID，超时未支付的由条件释放回补，对账校验库存守恒","tip":"最终一致，允许少卖禁止多卖"}]' startLabel="开抢" />

## 9. 可观测性

没有水位大盘，秒杀就是闭眼开车。指标表如下，`accepted` 与 `left` 是最关键的两个业务不变量：

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| seckill_in_qps | Gauge | 入口瞬时 QPS | 超过 12 万触发预案评估 |
| seckill_rejected_total | Counter | 按原因计的拒绝数（限流 / 配额 / 售罄） | 限流占比超过 99.9% 需查误杀 |
| seckill_accepted_total | Counter | 预扣成功数 | 超过库存数立即冻结活动 |
| seckill_stock_left | Gauge | Redis 剩余库存 | 小于 0 或大于总量立即告警 |
| seckill_queue_depth | Gauge | 有界队列当前长度 | 超过 180 触发扩容 |
| seckill_db_write_qps | Gauge | 落库写入速率 | 超过 200 QPS 告警 |
| seckill_resv_reserved | Gauge | 停留 RESERVED 的记录数 | 超过 50 且持续增长查 worker |
| seckill_reconcile_diff | Gauge | 预扣与订单差异数 | 不为 0 持续 1 分钟即人工介入 |

结构化日志统一带 `trace_id`、`activity_id`、`user_id`、`ticket`、`stage`、`err_code` 六个字段，绝不打印手机号与验证码答案。`trace_id` 由网关生成并写入 MQ 消息头，各 worker 消费时透传，这样一次抢购从准入到落库能对上同一条链。埋点代码自带完整上下文，可单独编译验证：

```go
// 文件：metrics.go —— 秒杀水位与限流埋点（快照一致读取）
package main

import (
	"fmt"
	"sync"
)

// Metrics 一次 Snapshot 取全量，避免大盘各指标来自不同时刻。
type Metrics struct {
	mu       sync.Mutex
	accepted int64
	rejected int64
	soldOut  int64
	left     int
}

func (m *Metrics) IncAccepted()  { m.mu.Lock(); defer m.mu.Unlock(); m.accepted++ }
func (m *Metrics) IncRejected()  { m.mu.Lock(); defer m.mu.Unlock(); m.rejected++ }
func (m *Metrics) IncSoldOut()   { m.mu.Lock(); defer m.mu.Unlock(); m.soldOut++ }
func (m *Metrics) SetLeft(n int) { m.mu.Lock(); defer m.mu.Unlock(); m.left = n }

func (m *Metrics) Snapshot() (accepted, rejected, soldOut int64, left int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accepted, m.rejected, m.soldOut, m.left
}

func main() {
	m := &Metrics{}
	for i := 0; i < 1000; i++ {
		m.IncRejected()
	}
	for i := 0; i < 100; i++ {
		m.IncAccepted()
	}
	m.IncSoldOut()
	m.SetLeft(0)
	a, r, s, left := m.Snapshot()
	fmt.Printf("accepted=%d rejected=%d soldOut=%d left=%d\n", a, r, s, left)
}
```

**一次真实故障的定位路径**：告警 `seckill_accepted_total` 达到 101（超过库存 100）→ 立即冻结活动并查 `seckill_stock_left` 是否为负 → 用 trace_id 抽查超出的那笔，发现预扣与回补之间状态被覆盖 → 定位到 6.6 的条件更新被一次跳过状态的旧代码绕过 → 回滚该版本、按库存强制对账回补、补上「已支付拒绝释放」的单测。全程从业务不变量指标反查到具体代码，不靠猜。

## 10. 测试策略

分层与 mock 边界如下：单测覆盖扣减、状态机、条件更新与熔断（6.8 的 `stock_test.go`），**只 mock 纯内存实现**，被测的并发逻辑是真代码；表驱动覆盖库存 100/1/0 三档与支付释放两种时序；集成测试用 `httptest` 起网关，验证 202 排队号、轮询终态与队列满回补；契约测试用唯一索引与 CHECK 约束做破坏性校验（插入负余量、重复 uk_user_act 必须失败）；故障注入主动 kill worker、灌满队列、让落库返回错误，验证对账与回补能把状态收敛；基准与压测见第 12 节。mock 边界切在内存实现而非 Redis，是为了让「不超卖」「回补幂等」这些最容易错的逻辑不依赖任何外部组件就能在 CI 里稳定复现。

```bash
go test -race ./...                          # 并发正确性
go test -bench=BenchmarkDeduct -benchmem ./internal/stock/...
go vet ./...                                 # 静态检查
```

故障注入的不变量断言写进 CI：连续跑一万次抢购后，成功数恒等于库存数、余量恒为 0、成功订单数加已回补数等于预扣数、`seckill_reconcile_diff` 最终回零。这四条任何一条不过都不允许上线。

## 11. 部署与容量

部署拓扑：CDN 与静态页在最外层；网关 8 副本无状态挂负载均衡；Redis 一主两从，库存预热与 Lua 在主执行；订单库一主两从；队列与 worker 4 副本；对账与延迟任务 2 副本 standby；Kafka 三分区承载预扣事件。依赖组件版本前提写明：Go 1.21+、Redis 7.x（Lua 原子与 `SISMEMBER`）、MySQL 8.0（依赖 `CHECK` 约束）、Kafka 3.6。

| 配置项 | 默认值 | 大促 | 灰度 | 说明 |
| --- | --- | --- | --- | --- |
| limiter_rate_qps | 10000 | 10000 | 2000 | 令牌桶补充速率 |
| quota_tokens | 300 | 300 | 60 | 库存乘以放大系数 3 |
| queue_capacity | 200 | 200 | 40 | 库存的两倍，留背压余量 |
| worker_count | 4 | 4 | 2 | 每 worker 50 QPS，合计 200 |
| reserve_timeout_min | 15 | 15 | 5 | 超时未支付释放 |
| breaker_threshold | 3 | 3 | 5 | 连续失败熔断阈值 |
| redis_lua_timeout_ms | 5 | 5 | 10 | 单次 Lua 超时 |

**资源估算与第 1 节数字自洽**：入口 10 万 QPS，8 个网关副本各扛 1.25 万 QPS，net/http 加令牌桶无状态，4 核 8G 单副本足够；令牌桶削到 1 万 QPS，配额只放 300 个进入 Redis，单主处理 300 次 Lua 远低于 5 万到 8 万 QPS 能力；100 个成功请求入队，4 worker 每个 50 QPS 合计 200 QPS 落库，0.5 秒写完 100 单，远低于 MySQL 3000 QPS 上限；带宽按 10 万 QPS 乘每请求约 0.5KB 约 50MB/s，由 CDN 与边缘承载；内存侧预约记录 300 条乘约 256B 约 75KB，可忽略。**扩容方式**：网关与 worker 无状态水平加副本，Redis 与订单库按主从读写分离；**优雅退出**：先摘注册中心与入口、停止接新请求、等待队列排空与在途预扣收敛、再关闭连接池，超时强杀前留 30 秒宽限，且退出前必须把未 ack 消息落盘以便重放。

**预案演练**（本讲专属）：主动演练关闭入口、库存冻结、降级静态页、动态调限流阈值、扩容 worker 五类动作，每类都要验证 `seckill_stock_left` 不为负、`seckill_reconcile_diff` 能回零、用户轮询能拿到明确终态。演练结论写进评审记录：三类故障（worker 崩溃、队列打满、落库报错）下系统都能收敛，才允许开抢。

## 12. 性能与压测

压测方法要真实可执行。用 `hey` 以恒定 10 万 QPS 注入 30 秒（约 300 万请求），幂等键按递增用户 ID 生成避免误判重复；同时在订单库侧采集写入速率验证削峰：

```bash
# 以 10 万 QPS 恒定速率压 30 秒，观察漏斗各级
hey -z 30s -c 5000 -q 100000 \
  -H "Content-Type: application/json" \
  -m POST -d '{"activity_id":"act-2026-1111","user_id":"bench-{{.Seq}}"}' \
  http://seckill.internal/seckill

# 采集网关与 worker 的运行时画像
go tool pprof -http=:8080 http://gateway:6060/debug/pprof/profile?seconds=30
```

压测模型与容量估算必须自洽：10 万 QPS 乘 30 秒约 300 万请求 → 令牌桶放行 1 万 QPS 共 30 万 → 一次性配额只放 300 个进入预扣 → 100 个抢到库存 → 100 单以 200 QPS 匀速落库。断言四条：进入预扣的请求数不超过 300、成交数恒等于 100、超卖为 0、数据库写入峰值不超过 200 QPS；429 与 409 属预期结果，不计入错误率。

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 入口承载 | 1.2 万 QPS 即 502 | 稳定 10 万 QPS | 增加 CDN 静态化与令牌桶前置 |
| 预扣 P99 | 43ms | 4ms | 单条 Lua 替代多次往返 |
| 落库写入峰值 | 1800 QPS | 200 QPS | 有界队列加 4 worker 匀速消费 |
| 超卖数 | 37 单 | 0 单 | 检查与扣减合为原子步骤 |
| 网关 CPU | 91% | 54% | 拒绝逻辑前移，少做 JSON 解码 |

瓶颈定位过程：首轮压测网关在 1.2 万 QPS 就大量 502，`pprof` 显示 CPU 花在 JSON 解码与鉴权上——但 99% 的请求注定被拒，于是把限流与配额判断前移到解码之前，入口承载能力直接上到 10 万 QPS；再查 `block profile` 发现预扣挤在单把全局锁上，按 SKU 分段后 P99 从 43ms 降到 4ms；最后用队列把落库从尖峰 1800 QPS 摊成匀速 200 QPS。**仍然存在的限制**：压测用内存 stub 代替真实支付渠道，会低估链路尾延迟；多 SKU 联合秒杀与跨地域多活未在单机房假设内覆盖；Redis 主故障切换期间的预扣暂停仍需靠预案人工介入，不是全自动。

## 13. 演进与替代方案

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 单体 + 数据库条件更新 | 峰值低于 3000 QPS、单 SKU | 一致性强、无 Redis 与 MQ、实现最简 | 连接池与行锁成上限，扛不住十万级洪峰 | 当前压测证明 1.2 万 QPS 即打挂，必须演进 |
| Redis 预扣 + 队列（当前） | 峰值 10 万 QPS、单 SKU 热点 | 原子预扣、削峰明显、DB 匀速写 | 要处理最终一致、状态机与对账，允许少卖 | 当前阶段 |
| 本地库存 + 中央水位 | 多机房、超热点 SKU | 各机房本地扣减，跨机房交互极少 | 库存碎片难回收、总水位对账复杂、可能超卖风险 | 多活与超大规模热点出现时 |
| 请求合并 + 异步批扣 | 读多写少、可批量结算场景 | 把 N 次扣减合成一次批写，吞吐极高 | 用户结果延迟更大、批次失败影响面大 | 成交可延迟确认的场景 |

从当前版本演进到「本地库存加中央水位」的触发条件有三个：单 SKU 峰值持续超过 30 万 QPS、跨机房容灾被提上日程、分段库存的碎片率长期超过 20%。迁移步骤：先双写 Redis 分段与本地库存并比对水位、再按机房灰度切流、观察两周 `seckill_reconcile_diff` 恒为零后全量；**回滚**只需把网关切回中央 Redis 预扣，双写阶段两者水位一致，回滚不会造成超卖或库存漂移。

<FlowTimeline direction="vertical" :steps='[{"title":"V1 先查后扣","desc":"并发超卖，仅作反面教材","tone":"gray"},{"title":"V2 条件更新","desc":"数据库原子扣减，扛不住洪峰","tone":"blue"},{"title":"V3 限流加 Redis 预扣","desc":"原子预扣加削峰（本讲）","tone":"green"},{"title":"V4 分段与对账","desc":"摊开热点，补齐最终一致","tone":"orange"},{"title":"V5 本地库存多活","desc":"跨机房水位与批扣","tone":"red"}]' />

## 14. 真实工程里的坑

**坑一：先查后扣把库存卖成负数。** 现象是 100 件库存卖出 137 单。原因是 `SELECT` 与 `UPDATE` 之间有一道缝，并发都读到旧值。下面这段自带完整上下文，可单独编译对比两种写法的最终余量：
```go
// 文件：atomic_deduct.go —— 坑一：先查后扣超卖，原子临界区修复
package main

import (
	"fmt"
	"sync"
)

type store struct {
	mu   sync.Mutex
	left int
}

func (s *store) read() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.left
}

// deductWrong 是错误写法：读与写分成两步，并发下都读到旧值。
func (s *store) deductWrong() bool {
	if s.read() > 0 {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.left--
		return true
	}
	return false
}

// deductRight 把检查与扣减合成一个临界区，等价于
// UPDATE stock SET available=available-1 WHERE available 大于 0 的影响行数。
func (s *store) deductRight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.left <= 0 {
		return false
	}
	s.left--
	return true
}

func run(deduct func(*store) bool) int {
	s := &store{left: 100}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deduct(s)
		}()
	}
	wg.Wait()
	return s.left
}

func main() {
	fmt.Println("wrong final left:", run((*store).deductWrong))
	fmt.Println("right final left:", run((*store).deductRight))
}
```
提前发现：用 1000 个并发 goroutine 抢 100 件，断言余量恒不为负；`go test -race` 之外还要专门跑这条不变量。

**坑二：`Enqueue` 失败忘了 `Refund`。** 现象是用户「抢到」却永远查不到订单，库存被占死。原因是队列满时只返回 503 没回补预扣。修正是 6.4 里 `if !h.enqueue(...) { _ = h.refund(...) }`，回补与入队是同一失败分支的成对动作。提前发现：故障注入里把队列容量调到 1，断言「预扣数减订单数」等于回补数。

**坑三：落库失败只打日志。** 现象是长期不一致且无人察觉。原因是 worker 把 `Save` 的错误吞了。修正是 6.3 里失败按预算重试、耗尽后 `onFail` 回补并进对账表，绝不静默丢弃。提前发现：让落库 fake 连续返回错误，断言 `seckill_reconcile_diff` 能收敛回零。

**坑四：回补不做条件判断，和已支付竞态把库存凭空加回。** 现象是水位大于总库存。原因是释放任务直接 `left++` 没看状态。修正是 6.6 的条件更新，仅当状态仍为 `RESERVED` 才回补，`PAID` 与 `RELEASED` 一律拒绝。提前发现：单测显式覆盖支付先到与释放先到两种时序（6.8 的 `TestPaidVsReleaseRace`）。

**坑五：限流层层重试放大流量。** 现象是 Redis 一抖，网关重试把它彻底打挂。原因是每层都各重试三次，叠出几十倍流量。修正是共享重试预算：预扣售罄绝不重试、落库最多三次、总耗时小于网关 deadline，只有幂等写才允许重试。提前发现：压测时统计「内部调用数除以外请求数」比值，超过 6 就说明有放大。

**坑六（附加）：把「抢到」当成「下单成功」。** 现象是预扣成功就返回 201，worker 写库失败后用户被静默欺骗。修正是 6.4 返回 202 加排队号，终态交给 `/seckill/result` 轮询。提前发现：联调时让落库返回错误，断言用户拿到的是 QUEUED 而非成功承诺，且最终收敛为 RELEASED 或重试成功后的 PAID。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-116-micro-seckill`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-116-micro-seckill)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给回补加上超时释放且不产生竞态" task="实现一个后台任务：预扣后 15 分钟未支付就自动回补库存，要求与「用户已支付」不产生竞态导致库存被错误加回。写出状态设计与关键判断。" :hints='["预扣记录要有状态：预留中、已支付、已回补","释放任务必须带条件：仅当状态仍为预留中才回补","判断与状态变更要原子，可用一条 Lua 或同一把锁"]' solution="预扣记录存 status 字段（RESERVED / PAID / RELEASED）与 created_at；支付成功时执行条件更新「仅当当前是 RESERVED 才置为 PAID」；延迟任务到点时执行「仅当 status 仍为 RESERVED 且已超时才置为 RELEASED 并 left 加一」，PAID 状态会让这一步影响 0 行从而不回补。\n把判断与两次变更都包在同一临界区或同一条 Lua 里，对应 6.6 的 MarkPaid 与 Release。测试覆盖支付先到、释放先到、两者并发三种时序，断言已支付场景水位不变。" explain="竞态的本质是读状态与改状态之间又有一道缝，和第 8 节超卖是同一类问题；用条件更新把判断与变更合为一步，是通用解法。" />

## 随堂检测

<FillBlank no="116-1" tpl="超卖的根因是检查余量与扣减余量之间___；修复要把两者收进___；秒杀允许的取舍是可___不可___。" :answers='["有一道缝~不是原子操作","同一个原子操作~一个临界区~一条 Lua","少卖","多卖"]' hint="想想两个并发事务都读到旧值会发生什么，以及成交数能不能超过库存" explain="先查后扣的缝让两个读者基于同一旧值各扣一次；把检查与扣减合成一条条件更新或一条 Lua 即解决；少卖是运营损失可接受，多卖是资损绝不允许。" :explains='["查余量与扣余量是分离的两步，中间没有互斥，两个并发请求会基于同一份旧值各扣一次，合计扣穿真实库存，超卖正源于这道缝。","把判余量与扣减合成一次原子写，如一条条件更新或一段 Redis Lua，比较与赋值一次完成，别的请求读不到中间态。","预扣未释放或限流过严会让有货的商品提前显示售罄，只是少赚一笔收入，钱货仍对得上、不造成资损，属可接受的保守偏差。","成交数一旦超过实际库存，超出的订单要么发不了货要么得赔付，直接构成资金与信誉损失，且事后难以追回，因此这条底线绝不能破。"]' />

<Quiz no="116-2" q="库存 100 件、瞬时 10 万 QPS，本讲漏斗最终让数据库承受多少写入？" :options='["10 万 QPS","1 万 QPS","300 QPS","200 QPS"]' answer="3" explain="令牌桶把 10 万削成 1 万 QPS，一次性配额只放 300 个进入预扣，其中 100 个抢到，再由 4 个 worker 每个 50 QPS 匀速写库，DB 峰值 200 QPS。逐层收敛的数字在第 1、11、12 节完全一致。" />

<Predict no="116-3" q="6.1 用 1000 个并发 Deduct 抢 100 件，最终 granted 与 left 是多少？" :options='["granted=1000 left=-900","granted=100 left=0","granted 约 100 上下浮动 left 约 0","granted=1 left=99"]' answer="1" output="granted=100 left=0" explain="检查、判重、扣减、写记录四步在同一把锁内完成，形成严格串行序列，恰好前 100 个成功，其余全部拿到 ErrSoldOut，余量精确为 0，不会浮动也不会为负。" />

<Quiz no="116-4" q="预扣成功但落库失败，正确的兜底是哪一种？" :options='["忽略它，用户会重新下单","把预扣记进可靠消息并幂等消费，配合定时对账与回补","直接把库存加回去，让用户重试","把错误写进日志就完事"]' answer="1" explain="预扣与落库之间是一段一致性真空。必须用可靠消息保证至少一次投递、消费幂等，再用对账兜底；直接回补可能和已支付请求竞态，只写日志则永久不一致，忽略则用户被静默欺骗。" />

<Quiz no="116-5" q="为什么秒杀用「一次性配额」而不只用按速率补水的令牌桶？" :options='["令牌桶实现太复杂","补水令牌桶会把请求持续放进预扣临界区，挡不住一次性洪峰","Redis 不支持令牌桶","一次性配额更省内存"]' answer="1" explain="秒杀是一次性事件，注定抢不到的请求占 99% 以上。补水令牌桶按速率持续放行，会让本该被拒的请求不断打到 Redis；一次性配额按库存乘以系数放号、抢完即止，才把进入预扣的请求压到 300 个。" />

## 一页纸速记

<KnowledgeCard :points='["超卖根因是 read-modify-write 之间的缝，用一条原子操作把检查与扣减合起来","可少卖不可超卖：队列满、超时未支付都条件回补，成交数绝不超库存","令牌桶削稳态洪峰，一次性配额只放库存乘以 3 的请求进预扣","有界队列把 100 单摊成 200 QPS 匀速写库，给用户排队号而非成功承诺","预扣与落库之间靠可靠消息、幂等消费、条件状态机与定时对账兜底"]' :pitfalls='["先查后扣，把库存卖成负数","Enqueue 失败忘了 Refund，出现抢到却没订单","落库失败只打日志，不做重试与对账","回补不做条件判断，和已支付竞态把库存凭空加回","限流层层重试放大流量，拖垮抖动的 Redis"]' summary="秒杀的正确性不靠更高的并发，而靠敢在流量最凶时把请求挡在门外，并把每一处读后再写都收进一个原子步骤。" />

## 小结与下一讲

1. 超卖的根因是检查与扣减之间的缝，用互斥锁（生产用 Redis Lua 或数据库条件更新）把两者合为一步，并用幂等键去重；三种修复方案在吞吐、复杂度与一致性代价上各有取舍。
2. 令牌桶把 10 万 QPS 削成 1 万、一次性配额只放 300 个进预扣、有界队列把 100 单摊成 200 QPS 匀速落库；队列满或超时则条件回补，守住「可少卖不可超卖」。
3. 预扣与落库之间的真空期靠可靠消息、幂等消费、条件状态机与定时对账收敛；压测模型、容量估算与降级熔断预案的数字层层自洽，故障注入覆盖 worker 崩溃、队列打满、落库报错三类。

到这里，业务系统本身的高并发正确性已经讲透。剩下的挑战是「怎么把这么多 Go 服务稳定地管起来」——声明式地描述期望状态、让控制器自动收敛、把指标日志告警收成一套平台。下一讲用 Go 写一个 Kubernetes Operator。

[下一讲：实战 11 · Kubernetes Operator 与可观测平台](/projects/cloud-operator)
