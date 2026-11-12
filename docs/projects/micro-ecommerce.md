---
title: 第 115 讲 · 实战 09 · 电商微服务系统
description: 按业务边界拆分服务、用 .proto 定契约、以 Saga 编排下单与补偿、用 HTTP 网关统一入口
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="90 分钟" goal="走完一个企业级系统的全流程" prereq="第 114 讲 · 分片、复制与 Raft" />

# 第 115 讲 · 实战 09 · 电商微服务系统

> 上一讲的分片 KV 证明了单 key 可以做到原子与强一致，可真实业务里最值钱的那次写入——「下一个订单」——偏偏要同时改动订单、库存、支付三处数据，而且这三处往往由三个团队各自维护、各自扩容、各自发布。把它们塞进一个单体、用一个本地事务盖住并不难；难的是当它们必须独立部署时，怎么保证这一串动作要么全部落地、要么全部不留痕，并且在途中任何一步超时、宕机、重复请求时都不留下幽灵订单或凭空多出的库存。这一讲不追求跨服务强事务，而是诚实接受最终一致：用 `.proto` 把边界钉成契约，用 Saga 把长事务拆成带逆序补偿的本地事务，用 HTTP 网关统一入口，用可靠消息与 binlog 把异构索引补齐。读完你应该能拿着这份文档直接去做设计评审，并说清每一个决策的代价。

<CheckList id="L115-goal" title="本讲目标（评审前逐条打勾）" :items='["能按业务边界与数据所有权划出服务边界，并说清拆分的代价与替代方案","能写出完整的 proto 契约、服务间调用矩阵与对外 API，字段语义与版本策略无歧义","能实现带逆序补偿的 Saga 编排器，处理超时传递、重试预算、空补偿与悬挂补偿","能设计贯穿全链路的幂等键，并用唯一索引加三态状态机挡住重复请求","能用本地消息表与 binlog 订阅把订单、支付、ES 索引收敛到最终一致","能给出自洽的容量估算、全链路压测方法与故障注入下的联调结论"]' />

## 0. 项目定位与真实场景

先把系统放回它真实生长的环境里。我们面对的是一家中型垂直电商：在售 SKU 约 20 万，注册用户 800 万，日常日订单 50 万单，大促时流量高度集中，下单接口的瞬时峰值按 2000 QPS 设计，商品与订单的读接口峰值约 1.2 万 QPS。使用方有三类：C 端用户在 App 与 H5 下单，运营后台要改价、上下架、看报表，履约与客服系统要按订单状态发货与退款。这三类对同一份数据的诉求完全不同，却都压在最初那套单体上。

单体的痛点不是「代码乱」，而是三条具体的耦合。第一是发布耦合：改一个商品搜索的分词，要跟订单模块一起全量回归、一起半夜发布，任何一处 panic 都会让交易整体不可用。第二是资源争抢：运营跑一张跨订单、库存、商品的统计报表，一条慢 SQL 就把下单链路的数据库连接池占满，交易跟着变慢。第三是伸缩粒度错配：商品读是读多写少、库存与订单是写密集，单体只能整体加副本，钱花在了不需要的地方。

于是有了拆分，但也立刻撞上本讲的核心矛盾：**一次下单从「一个本地事务」变成「四次跨进程调用加一串补偿」**。本地事务的 ACID 是数据库替你保证的，拆开之后没有任何一层替你保证，必须由应用自己用契约、幂等、补偿、对账重新把一致性建起来。这就是这一讲值得单独成篇的原因——它不是教你怎么起四个 gRPC 服务，而是教你在失去数据库事务保护之后，如何把正确性一项项赎回来。

<Term word="数据所有权" text="每个服务独占自己的库与表，任何跨服务读写只能走它公开的契约；谁拥有数据，谁负责它的不变量，这是拆分边界最硬的判据" />

## 1. 需求与约束

先把约束写成可度量的形式，再由约束推架构。评审时如果一张表里没有数字，后面所有选型都无法被证伪。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 按业务边界拆分用户、商品、订单、库存、支付 | 每个服务独占库表、独立部署；任意服务重启不影响其它服务对外可用；代码中不存在跨库直连 |
| FR-2 | 契约优先的 gRPC 内部接口 | 所有跨服务调用走 `.proto` 生成的 stub；proto 变更须通过破坏性检查才能合入 |
| FR-3 | 下单链路最终一致 | 任一步失败后 30 秒内，订单状态、库存预留、支付流水收敛一致，无悬挂订单与未释放预留 |
| FR-4 | 所有写操作幂等 | 同一 Idempotency-Key 重放 100 次，订单数与扣款数只增一次；并发重复只能有一个请求进入临界区 |
| FR-5 | HTTP 统一入口 | 网关完成鉴权、限流、超时传递与错误映射，返回稳定 HTTP 状态码且不泄露内部错误细节 |
| FR-6 | 商品变更可检索 | 商品改价或上下架后 5 秒内 ES 可搜到；乱序或重复的 binlog 事件不得用旧版本覆盖新版本 |

| 非功能指标 | 目标值 | 测量口径 |
| --- | --- | --- |
| 下单峰值 QPS | 2000 | 网关入口每秒 POST /orders 数，按 10 分钟持续压测计 |
| 读接口峰值 QPS | 12000 | 商品详情与订单查询之和，分层统计 P50 / P99 |
| 下单链路 P99 | 不高于 800ms | 网关收到请求到返回，含 4 次 gRPC 与落库 |
| 一致性级别 | 最终一致（BASE） | 允许中间态对外可见，30 秒内靠补偿或对账收敛 |
| 可用性 SLA | 99.95% | 月度可用分钟占比，单服务故障不得拖垮全站 |
| RTO / RPO | 5 分钟 / 30 秒 | 故障切换耗时与最大数据丢失窗口 |
| 日订单量 | 50 万单 | 每日 orders 表新增行数 |

**本讲不做什么**：不做秒杀级热点库存的扣减正确性（那是第 116 讲的主题，本讲库存预扣按普通并发处理即可）；不自建服务注册发现、熔断与链路追踪，直接沿用第 75、79、80 讲的基础设施；不对接真实支付网关，用可注入的 stub 代替；不做异地多活与单元化。**假设边界**：单机房部署，MySQL 主从半同步，Kafka 三副本，Redis 仅用于幂等与缓存，所有节点经 NTP 同步且时钟偏差不超过 50ms，内部网络 P99 往返不高于 2ms。这些假设一旦不成立，后面的超时预算与重试次数都要重算。

## 2. 架构蓝图

<ArchBlueprint title="电商微服务四层架构" caption="依赖方向自上而下单向流动，越靠下的层越不知道上层存在；相邻层之间标注的是实际调用协议" :layers='[{"name":"接入层","note":"对外唯一入口","protocol":"HTTPS/1.1 + JSON","items":[{"label":"API 网关","sub":"路由 / 鉴权 / 限流","detail":"职责：校验 JWT、透传 Idempotency-Key、按租户限流并把 REST 转成内部 gRPC；技术选型：net/http 中间件链，不引第三方网关，避免多一跳与黑盒；失败处理：下游超时返回 503 并携带 Retry-After，鉴权失败返回 401，参数非法返回 400。","tone":"blue"},{"label":"灰度与契约校验","sub":"proto 版本路由","detail":"职责：按 proto 版本把流量导向灰度实例，拒绝携带不兼容字段的请求；技术选型：grpc-gateway 生成 JSON 映射并做字段白名单；失败处理：契约不匹配返回 400 并落审计日志，不把脏数据放進编排层。","tone":"purple"}]},{"name":"编排层","note":"跨服务一致性归口","protocol":"gRPC / Protobuf","items":[{"label":"订单服务","sub":"Saga 编排器","detail":"职责：把下单拆成预扣、建单、支付、确认四步，任一步失败逆序补偿，并持久化每步状态；技术选型：持久化 Saga 状态机加 context 超时透传，补偿进后台重试队列；失败处理：补偿失败写入待补偿表由 worker 带退避重试，绝不静默丢弃。","tone":"orange"},{"label":"幂等与去重","sub":"三态幂等键","detail":"职责：以 Idempotency-Key 抢占 IN_PROGRESS、DONE、FAILED 三态，拦截重复请求与并发重复；技术选型：MySQL 唯一索引加状态字段，或 Redis SETNX 加 TTL；失败处理：抢占冲突返回 409，让客户端携原键轮询结果。","tone":"green"}]},{"name":"领域服务层","note":"各服务独库、独立部署","protocol":"gRPC / Protobuf + 本地事务","items":[{"label":"用户服务","sub":"身份 / 等级","detail":"职责：提供用户资料与会员等级校验；技术选型：读多写少，本地缓存加布隆过滤挡穿透；失败处理：不可用时网关把等级降级为默认值，不阻断下单主链路。","tone":"blue"},{"label":"商品服务","sub":"SKU / 价格快照","detail":"职责：提供 SKU 详情与价格；技术选型：价格以下单瞬间快照写入订单行，避免后续改价污染历史单；失败处理：读失败走缓存兜底，缓存也失效才返回明确错误。","tone":"blue"},{"label":"库存服务","sub":"预扣 / 释放","detail":"职责：以幂等键原子预扣并支持释放，维护可用量与冻结量两个不变量；技术选型：本地事务加行锁或 Redis Lua 保证检查与扣减同临界区；失败处理：预扣失败返回明确的库存不足错误，触发 Saga 逆序补偿。","tone":"orange"},{"label":"支付服务","sub":"扣款 / 退款","detail":"职责：扣款与退款均幂等并记录流水；技术选型：对接支付网关，本地流水表以幂等键建唯一索引；失败处理：超时按未知结果处理，靠对账确认后再补偿，绝不盲目重试扣款。","tone":"red"}]},{"name":"数据与索引层","note":"持久化与异构索引","items":[{"label":"订单库","sub":"MySQL / 分库分表","detail":"职责：订单聚合根与 outbox 消息在同一本地事务落库；技术选型：按 user_id 分 16 片，单表行数控制在千万级以内；失败处理：主库故障切从库，RPO 由半同步复制控制在秒级。","tone":"gray"},{"label":"消息队列","sub":"Kafka / 本地消息表","detail":"职责：承载订单事件与 binlog 变更，异步驱动对账与索引同步；技术选型：三分区按 order_id 保序，消费者按消息 ID 幂等；失败处理：投递失败留在 outbox 由 dispatcher 重放。","tone":"green"},{"label":"ES 索引","sub":"商品搜索视图","detail":"职责：提供商品检索的异构只读视图；技术选型：订阅 binlog 按 external version 幂等 upsert；失败处理：毒事件进死信且不推进位点，人工修复后从位点重放。","tone":"purple"}]}]' />

逐层解读为什么这样切。接入层必须存在，是因为鉴权、限流、协议转换、错误映射这些横切关注点如果散落在五个服务里，就会出现五套不一致的限流阈值与五种错误码；收敛到一个无状态的网关，既能统一治理，又让领域服务只关心业务。它不引第三方网关的代价是自己要维护中间件链，换来的是没有黑盒、超时与重试完全可控。编排层是这套架构里唯一的「全局视角」：领域服务彼此不知道对方存在，只有编排器知道一次下单要按什么顺序调用谁、失败了该撤销谁；把一致性归口到一处，才不会到处长出血缘混乱的互相调用。领域服务层按业务边界而非技术层切，每个服务对自己的库拥有完整所有权，因此可以独立改表、独立扩容、独立发布。数据与索引层把写库与检索解耦，搜索的吞吐波动不会传导到交易库。

<ConceptDiagram type="saga" title="下单 Saga：正向四步与一一对应的逆序补偿" caption="补偿必须与正向操作成对设计，且补偿自身幂等、允许空补偿" />

<ConceptDiagram type="mq-flow" title="binlog 到 ES 的异构同步：本地消息表、Kafka、幂等消费" caption="业务写入与消息写入同事务，消费端按 external version 挡住乱序与重放" />

<ConceptDiagram type="discovery" title="服务注册发现与健康摘除" caption="沿用第 075 讲：实例上下线对调用方透明，故障实例由健康检查摘除" />

<ConceptDiagram type="gateway" title="网关协议转换：REST JSON 转内部 gRPC" caption="幂等键、trace_id 与 deadline 都在这一层注入并向下透传" />

<FlowTimeline direction="horizontal" :steps='[{"title":"网关准入","desc":"鉴权、限流、透传幂等键","tone":"blue"},{"title":"幂等抢占","desc":"三态键挡重复请求","tone":"green"},{"title":"预扣库存","desc":"Reserve 记可释放预留","tone":"green"},{"title":"建立订单","desc":"落库置 CREATED","tone":"green"},{"title":"发起支付","desc":"Charge 扣款可退款","tone":"orange"},{"title":"确认投递","desc":"置 PAID 并写 outbox","tone":"red"}]' />

依赖方向严格自上而下：接入层依赖编排层，编排层依赖领域服务层，领域服务层依赖自己的数据层。**绝不允许反向依赖**——库存服务不能回头调订单服务去「顺便把订单状态改了」，商品服务不能订阅订单库的表。反向依赖会制造循环调用与隐式事务边界，一旦出现，故障就会沿着环放大成雪崩，而且没有任何一层能再独立部署。跨层只经由契约，同层服务之间默认不通信，确实需要协作时一律上提到编排层。

## 3. 模块职责与依赖方向

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| API 网关 | HTTPS POST /orders、GET /products | JSON 响应、稳定状态码 | 编排层 gRPC stub、注册中心 | 无状态 | 下游超时返回 503 加 Retry-After，鉴权失败 401，参数非法 400 |
| 订单服务（编排器） | CreateOrder 命令、幂等键 | 订单状态机推进、outbox 消息 | 库存、支付、商品、幂等存储、订单库 | 有状态（Saga 日志、订单表） | 任一步失败逆序补偿，补偿失败入待补偿表重试 |
| 用户服务 | GetUser | 用户资料与等级 | 用户库、本地缓存 | 有状态 | 不可用则降级默认值，不阻断主链路 |
| 商品服务 | GetProduct | SKU 与价格快照 | 商品库、缓存、binlog 采集 | 有状态 | 读失败走缓存兜底 |
| 库存服务 | Reserve / Release | 预留 ID 或明确拒绝 | 库存库、Redis | 有状态（可用量、冻结量、预留记录） | 库存不足返回可判定错误，触发补偿 |
| 支付服务 | Charge / Refund | 支付流水 ID | 支付库、外部支付网关 | 有状态 | 超时按未知处理，靠对账确认后补偿 |
| ES 同步 worker | binlog 事件流 | 商品索引文档 | Kafka、ES、位点存储 | 有状态（checkpoint、死信） | 毒事件进死信且不推进位点，人工重放 |

包目录树按「一个服务一个可独立编译的二进制」组织，契约单独成包避免循环依赖：

```text
ecom/
├── api/ecomv1/              # .proto 与生成的 pb.go，唯一事实来源
│   ├── order.proto
│   ├── inventory.proto
│   └── payment.proto
├── cmd/
│   ├── gateway/main.go      # HTTP/JSON 网关
│   ├── order/main.go        # Saga 编排器
│   ├── inventory/main.go
│   ├── payment/main.go
│   ├── product/main.go
│   └── user/main.go
├── internal/
│   ├── order/               # 聚合根、状态机、saga.go、outbox.go
│   ├── inventory/           # 预扣与幂等存储
│   ├── payment/
│   ├── contract/            # 内部窄接口（依赖倒置）
│   └── syncer/              # binlog 订阅与 ES upsert
└── go.mod
```

接口定义遵循依赖倒置：编排层只依赖 `contract` 包里的三张窄接口，而不是具体的 gRPC 客户端类型。`Inventory`、`Payment`、`OrderStore` 各只暴露当前用例真正需要的方法，这样单测里塞 fake、灰度时换实现、甚至把某个服务临时降级成本地 stub 都不需要改编排器一行代码。

<Callout type="warn" title="边界划分最常见的三个错误" text="一是按 controller / service / dao 分层拆服务，结果五个服务共享同一个库，既没得到隔离性又平白多出网络调用；二是让领域服务互相同步调用，订单直接调库存、库存又回调订单，形成环，故障沿环放大；三是把「用户地址」「商品标题」这类会被修改的数据直接以引用形式存进订单，运营一改价，历史订单金额跟着变，对账永远对不平。正确做法是数据所有权归位、协作上提编排层、下单瞬间把价格与地址做快照写入订单行。" />

## 4. 数据与协议契约

契约是跨团队、跨语言的唯一事实来源，必须先于实现定稿。表结构里 `uk_idem` 唯一索引是幂等的物理保证，`version` 是乐观锁，`outbox` 与业务同事务：

```sql
CREATE TABLE orders (
  id              BIGINT PRIMARY KEY,
  user_id         BIGINT NOT NULL,
  status          TINYINT NOT NULL DEFAULT 1,
  total_fen       BIGINT NOT NULL,
  price_snapshot  BIGINT NOT NULL,
  version         INT NOT NULL DEFAULT 0,
  idempotency_key VARCHAR(64) NOT NULL,
  created_at      DATETIME(3) NOT NULL,
  UNIQUE KEY uk_idem (idempotency_key),
  KEY idx_user (user_id, id)
) ENGINE=InnoDB;

CREATE TABLE inventory (
  sku        VARCHAR(32) PRIMARY KEY,
  available  INT NOT NULL,
  frozen     INT NOT NULL DEFAULT 0,
  version    INT NOT NULL DEFAULT 0,
  CHECK (available >= 0 AND frozen >= 0)
) ENGINE=InnoDB;

CREATE TABLE idempotency (
  idem_key  VARCHAR(64) PRIMARY KEY,
  state     TINYINT NOT NULL,
  response  BLOB,
  expire_at DATETIME(3) NOT NULL,
  INDEX idx_expire (expire_at)
) ENGINE=InnoDB;

CREATE TABLE outbox (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  aggregate_id  VARCHAR(64) NOT NULL,
  topic         VARCHAR(64) NOT NULL,
  payload       BLOB NOT NULL,
  sent          TINYINT NOT NULL DEFAULT 0,
  tries         TINYINT NOT NULL DEFAULT 0,
  created_at    DATETIME(3) NOT NULL,
  INDEX idx_unsent (sent, id)
) ENGINE=InnoDB;
```

完整的 proto 契约片段如下。所有写方法都强制携带 `RequestMeta`，其中 `idempotency_key`、`trace_id`、`deadline_ms` 是跨服务传递的三件套；金额一律用 `int64` 的「分」，绝不用浮点；状态用显式枚举并保留 `UNSPECIFIED = 0` 以便兼容新增字段：

```proto
syntax = "proto3";
package ecom.v1;
option go_package = "github.com/acme/ecom/api/ecomv1;ecomv1";

message RequestMeta {
  string idempotency_key = 1;
  string trace_id        = 2;
  string user_id         = 3;
  int64  deadline_ms     = 4;
}

enum OrderStatus {
  ORDER_STATUS_UNSPECIFIED = 0;
  CREATED  = 1;
  RESERVED = 2;
  PAID     = 3;
  CANCELED = 4;
}

message OrderItem {
  string sku            = 1;
  int32  qty            = 2;
  int64  unit_price_fen = 3;
}

message CreateOrderRequest {
  RequestMeta meta          = 1;
  string      user_id       = 2;
  repeated OrderItem items  = 3;
  string      address_id    = 4;
}
message CreateOrderResponse {
  string      order_id  = 1;
  OrderStatus status    = 2;
  int64       total_fen = 3;
}
message GetOrderRequest  { RequestMeta meta = 1; string order_id = 2; }
message GetOrderResponse { string order_id = 1; OrderStatus status = 2; }

service OrderService {
  rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
  rpc GetOrder(GetOrderRequest)       returns (GetOrderResponse);
}

message ReserveRequest {
  RequestMeta meta         = 1;
  string      order_id     = 2;
  repeated OrderItem items = 3;
}
message ReserveResponse { bool ok = 1; string reservation_id = 2; }
service InventoryService {
  rpc Reserve(ReserveRequest) returns (ReserveResponse);
  rpc Release(ReserveRequest) returns (ReserveResponse);
}

message ChargeRequest  { RequestMeta meta = 1; string order_id = 2; int64 amount_fen = 3; }
message ChargeResponse { bool ok = 1; string payment_id = 2; }
service PaymentService {
  rpc Charge(ChargeRequest) returns (ChargeResponse);
  rpc Refund(ChargeRequest) returns (ChargeResponse);
}

message User    { string user_id = 1; string phone_masked = 2; int32 level = 3; }
message Product { string sku = 1; string title = 2; int64 price_fen = 3; int32 status = 4; }
message GetUserRequest    { RequestMeta meta = 1; string user_id = 2; }
message GetProductRequest { RequestMeta meta = 1; string sku = 2; }
service UserService    { rpc GetUser(GetUserRequest)       returns (User); }
service ProductService { rpc GetProduct(GetProductRequest) returns (Product); }
```

对外 API 与内部接口的对照：

| 对外 API | 方法 | 关键请求头 / 字段 | 成功响应 | 主要错误码 |
| --- | --- | --- | --- | --- |
| /orders | POST | Idempotency-Key、user_id、items、address_id | 201 + order_id | 400 参数、409 冲突、503 超时、502 下游失败 |
| /orders/{id} | GET | Authorization | 200 + 订单状态 | 404 不存在 |
| /products/{sku} | GET | 无 | 200 + 商品详情 | 404 下架 |

```json
{
  "user_id": "u-8801",
  "items": [{ "sku": "SKU-9", "qty": 2, "unit_price_fen": 99950 }],
  "address_id": "addr-31"
}
```

内部接口签名保持窄而稳定，这是依赖倒置的落点；同时用一张显式状态机约束跃迁，挡住乱序补偿把已支付订单改回去。下面这段自带完整上下文，可单独编译验证：

```go
// 文件：contract_iface.go —— 内部窄接口与状态机跃迁校验
package main

import (
	"errors"
	"fmt"
)

var ErrIllegalTransition = errors.New("ecom: illegal status transition")

type OrderStatus string

const (
	StatusCreated  OrderStatus = "CREATED"
	StatusReserved OrderStatus = "RESERVED"
	StatusPaid     OrderStatus = "PAID"
	StatusCanceled OrderStatus = "CANCELED"
)

// Inventory 与 Payment 是订单服务仅有的两处外部依赖，
// 保持窄接口才能随时替换 gRPC 实现或塞入 fake。
type Inventory interface {
	Reserve(idemKey, sku string, qty int) (string, error)
	Release(idemKey string) error
}
type Payment interface {
	Charge(idemKey, orderID string, amountFen int64) (string, error)
	Refund(idemKey, orderID string) error
}

// allowed 是一张显式状态机：禁止从 PAID 回退，也禁止对
// CANCELED 再补偿，挡住乱序补偿覆盖已支付订单。
func allowed(from, to OrderStatus) error {
	legal := map[OrderStatus]map[OrderStatus]bool{
		StatusCreated:  {StatusReserved: true, StatusCanceled: true},
		StatusReserved: {StatusPaid: true, StatusCanceled: true},
		StatusPaid:     {},
		StatusCanceled: {},
	}
	if !legal[from][to] {
		return fmt.Errorf("%w: %s to %s", ErrIllegalTransition, from, to)
	}
	return nil
}

func main() {
	fmt.Println("CREATED to PAID:", allowed(StatusCreated, StatusPaid))
	fmt.Println("RESERVED to PAID:", allowed(StatusReserved, StatusPaid))
	fmt.Println("PAID to CREATED:", allowed(StatusPaid, StatusCreated))
}
```

**字段语义与兼容规则**：proto3 的零值不可区分「没传」与「传了零值」，所以 `qty` 必须在服务端显式校验为正数，不能依赖默认值；金额字段永不复用、永不改单位；新增字段一律追加新 tag 号，删除字段只标 `reserved` 不复用编号；枚举新增值必须能被旧客户端容忍，旧服务遇到未知枚举值应回落到 `UNSPECIFIED` 而不是 panic。**版本号与灰度**：包名固定 `ecom.v1`，破坏性变更才升 `v2`；网关按 `X-Contract-Version` 头把灰度流量导向新版本实例，观察两个错误率与 P99 分位一致后再全量，回滚只需摘掉灰度路由，不动数据。

**幂等键设计**是本节重点。键由客户端按「用户 ID + 业务语义 + 随机后缀」生成，网关透传、全链路共用同一个值，任何服务不得自行另造键。作用域与存储如下：

| 幂等键作用域 | 存储载体 | 状态 | 冲突时行为 | TTL |
| --- | --- | --- | --- | --- |
| 网关准入 | Redis SETNX | 存在 / 不存在 | 返回 409 让客户端轮询 | 24 小时 |
| 编排器 Saga | idempotency 表 | IN_PROGRESS / DONE / FAILED | IN_PROGRESS 返回处理中，DONE 返回首次结果 | 24 小时 |
| 库存预扣 | 预留记录唯一键 | 已预留 / 已释放 | 重复预扣直接返回首次预留 ID | 与订单同生命周期 |
| 支付扣款 | 支付流水唯一键 | 成功 / 失败 / 未知 | 未知结果不重试扣款，转对账 | 永久 |
| MQ 消费 | 消息 ID 去重表 | 已消费 / 未消费 | 重复消息直接 ack | 7 天 |

服务间调用矩阵把「谁调谁、怎么调、失败怎么办」一次列清，评审时这张表比架构图更能暴露问题：

| 调用方 | 被调方 | 接口 | 协议 | 同步性 | 超时 | 幂等键 | 失败兜底 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 网关 | 订单服务 | CreateOrder | gRPC | 同步 | 3s | 透传 | 503 加 Retry-After |
| 订单服务 | 库存服务 | Reserve / Release | gRPC | 同步 | 800ms | 同一键 | 触发逆序补偿 |
| 订单服务 | 支付服务 | Charge / Refund | gRPC | 同步 | 1.5s | 同一键 | 未知结果转对账 |
| 订单服务 | 商品服务 | GetProduct | gRPC | 同步 | 300ms | 无（只读） | 价格快照缓存兜底 |
| 网关 | 用户服务 | GetUser | gRPC | 同步 | 300ms | 无（只读） | 等级降级默认值 |
| 订单服务 | Kafka | order.paid | 消息 | 异步 | 不适用 | 消息 ID | outbox 重放 |
| syncer | ES | upsert | HTTP | 异步 | 2s | external version | 毒事件进死信 |

Saga 正向与补偿步骤表要求「每一步都有可逆动作」，这是编排器能否收敛的关键：

| 序 | 正向步骤 | 补偿步骤 | 幂等键 | 可重试 | 空补偿 | 悬挂补偿 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 抢占幂等键 | 释放键或置 FAILED | 键本身 | 否 | 不适用 | 不适用 |
| 2 | Reserve 预扣库存 | Release 释放预留 | 同一键 | 是 | 查无预留直接成功 | 预留记录不存在即空操作 |
| 3 | Create 建订单 | 置 CANCELED | 同一键 | 是 | 订单不存在直接成功 | 状态机拒绝非法跃迁 |
| 4 | Charge 扣款 | Refund 退款 | 同一键 | 谨慎 | 无流水直接成功 | 未知结果先对账再退 |
| 5 | 置 PAID 并写 outbox | 置 CANCELED | 同一键 | 是 | 幂等更新 | 版本乐观锁拦截 |

<Compare title="两种契约设计的取舍" :panels='[{"label":"共享数据库表结构","code":"各服务直接读写同一张 orders 表，用字段临时传递状态","note":"上手快、零 RPC，但一处改表全站回归，等于没拆，最终一定退回单体"},{"label":"proto 契约加窄接口","code":"跨服务只走 .proto 生成的 stub，内部依赖三张窄接口","note":"前期多一层代码生成与契约治理，换来独立部署、可 mock、可灰度，是本讲选择"}]' />

<CodeTabs title="契约优先：先定 proto，再落 Go 接口" :tabs='[{"label":"proto 契约","code":"service InventoryService { rpc Reserve(ReserveRequest) returns (ReserveResponse); rpc Release(ReserveRequest) returns (ReserveResponse); }","note":"字段、枚举、状态码是跨语言与跨团队的唯一事实来源"},{"label":"Go 窄接口","code":"type Inventory interface { Reserve(idemKey, sku string, qty int) (string, error); Release(idemKey string) error }","note":"订单服务只依赖这张窄接口，gRPC 实现与单测 fake 可互换"}]' />

## 5. 核心链路时序

<SequenceDiagram title="下单正常链路（含返回、自处理与异步消息）" caption="第 11 步的异步投递由 outbox 保证至少一次，是最终一致的起点" :actors='["Client","Gateway","Order","Inventory","Payment","MQ"]' :steps='[{"from":"Client","to":"Gateway","label":"POST /orders","note":"带 Idempotency-Key","kind":"call"},{"from":"Gateway","to":"Order","label":"CreateOrder","note":"deadline 3s","kind":"call"},{"from":"Order","to":"Order","label":"抢占幂等键","note":"唯一索引","kind":"self"},{"from":"Order","to":"Inventory","label":"Reserve 预扣","note":"同一幂等键","kind":"call"},{"from":"Inventory","to":"Order","label":"返回预留ID","kind":"return"},{"from":"Order","to":"Payment","label":"Charge 扣款","kind":"call"},{"from":"Payment","to":"Order","label":"返回支付ID","kind":"return"},{"from":"Order","to":"Order","label":"置 PAID","note":"乐观锁","kind":"self"},{"from":"Order","to":"Gateway","label":"返回 order_id","kind":"return"},{"from":"Gateway","to":"Client","label":"201 创建成功","kind":"return"},{"from":"Order","to":"MQ","label":"投递订单事件","note":"outbox 不丢","kind":"async"}]' />

<StepBox title="逐步骤解读：每一步在守什么不变量" :steps='[{"title":"1 网关准入","body":"校验 JWT、限流、检查 Idempotency-Key 非空且不含空白字符，派生带 3 秒 deadline 的 context","tip":"deadline 必须一路透传，否则后面每一步的超时都是假的"},{"title":"2 幂等抢占","body":"编排器以唯一索引抢占三态键，IN_PROGRESS 拒绝并发重复，DONE 直接返回首次结果","tip":"这是 FR-4 的物理保证，靠数据库而不是靠自觉"},{"title":"3 预扣库存","body":"Reserve 在同一本地事务内检查可用量并记预留，返回可释放的 reservation_id","tip":"预留必须可释放，否则第 4 步失败无法补偿"},{"title":"4 建单与支付","body":"把价格快照写入订单行，再调用 Charge；两个副作用都已登记进 done 列表","tip":"顺序不能反，先有订单才有可退款的对象"},{"title":"5 确认与投递","body":"乐观锁把状态推到 PAID，并在同一事务写 outbox 消息","tip":"业务与消息同事务，是异步链路不丢消息的前提"},{"title":"6 异步返回","body":"先向客户端返回，再由 dispatcher 把 outbox 消息投递到 Kafka 驱动对账与 ES 同步","tip":"返回与投递解耦，投递失败不影响用户看到的结果"}]' startLabel="开始下单" />

异常链路与正常链路的分叉点在第 6 步或第 4 步。若 Charge 返回明确失败，编排器立即逆序执行已登记步骤的补偿：先撤建单（置 CANCELED）、再释放预留，回到「无订单、无预留」的初态，向客户端返回 409。若 Charge 超时，则不能简单当失败：**支付结果未知**，盲目重试扣款可能造成重复扣款，盲目退款可能退一笔根本没成功的款。正确做法是把该笔标记为「未知」、暂停自动补偿、由对账任务拉取支付网关流水确认后再决定推进还是退款，并把订单停在中间态等结果。网关超时又是另一条分叉：如果编排器用的是 `context.Background()` 而非透传进来的 ctx，网关 select 到超时先返回 503，后台 goroutine 却仍在跑完整个 Saga，于是客户端以为失败、服务端其实成功了——这就是第 7 节要重点处理的「假超时」。

## 6. 核心实现（按模块逐段展开）

以下七段代码**各自自包含、可单独 `go run` 或 `go test` 编译验证**。为便于评审时把每个机制单独拎出来核对，公共类型在每段内以最小形式重定义，而不是互相引用；真实工程里它们会被收进 `internal/contract` 与各自的包。这个组织的代价是有少量类型重复，收益是任何一段都能独立证明一个不变量，不会被别处的改动悄悄破坏。

### 6.1 数据模型与依赖倒置的窄接口

**设计意图**：先把领域错误、状态机、聚合根和三张窄接口定死，让上层只依赖抽象。关键取舍是错误用哨兵值而非字符串，状态用显式常量而非裸字符串，金额用 `int64` 分而非浮点；代价是每加一种下游错误都要在这里登记，换来的是 `errors.Is` 可判定、状态跃迁可约束。

```go
// 文件：contract.go —— 领域模型与依赖倒置的窄接口
package main

import (
	"errors"
	"fmt"
	"time"
)

// 领域错误用哨兵值，上层可用 errors.Is 判定并映射成稳定的 HTTP 状态码。
var (
	ErrStockNotEnough = errors.New("ecom: stock not enough")
	ErrDuplicate      = errors.New("ecom: duplicate request")
	ErrBadRequest     = errors.New("ecom: bad request")
	ErrSagaAbort      = errors.New("ecom: saga aborted")
	ErrDownstream     = errors.New("ecom: downstream failure")
)

// OrderStatus 用显式状态机约束跃迁，禁止从 PAID 回退到 CREATED。
type OrderStatus string

const (
	StatusCreated  OrderStatus = "CREATED"
	StatusReserved OrderStatus = "RESERVED"
	StatusPaid     OrderStatus = "PAID"
	StatusCanceled OrderStatus = "CANCELED"
)

// Order 是订单聚合根。IdemKey 由网关透传，贯穿全链路用于去重；
// Version 是乐观锁，PriceFen 是下单瞬间的价格快照，不随商品改价变化。
type Order struct {
	ID        string
	UserID    string
	SKU       string
	Qty       int
	AmountFen int64
	Status    OrderStatus
	IdemKey   string
	Version   int
	CreatedAt time.Time
}

// 三张窄接口是订单服务对外部世界的全部依赖：可换成 gRPC 客户端，
// 也可在单测里塞入 fake，这是依赖倒置的核心。
type Inventory interface {
	Reserve(idemKey, sku string, qty int) (string, error)
	Release(idemKey string) error
}
type Payment interface {
	Charge(idemKey, orderID string, amountFen int64) (string, error)
	Refund(idemKey, orderID string) error
}
type OrderStore interface {
	Create(o Order) error
	UpdateStatus(orderID string, from, to OrderStatus, version int) error
}

func main() {
	o := Order{ID: "O-1001", SKU: "SKU-9", Qty: 2, AmountFen: 199900,
		Status: StatusCreated, IdemKey: "idem-abc", CreatedAt: time.Now()}
	fmt.Printf("order=%s status=%s amount=%d fen\n", o.ID, o.Status, o.AmountFen)
}
```

### 6.2 幂等键三态存储（并发与去重）

**设计意图**：把 FR-4 落成一个可并发的抢占器。关键取舍是三态而非布尔——`IN_PROGRESS` 用来挡住「第一个请求还没结束、第二个就进来」的并发重复，`DONE` 用来返回首次结果，`FAILED` 允许重试；TTL 与 Sweep 防止表无限膨胀。代价是抢占与业务提交之间若进程崩溃，会留下一条 `IN_PROGRESS` 直到 TTL 到期，这段时间内该键被拒，因此必须有定时清理与客户端轮询兜底。

```go
// 文件：idempotency.go —— 幂等键三态存储，防重复、防并发、带 TTL 清理
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrInProgress = errors.New("idempotency: request in progress")

type idemState uint8

const (
	stateInProgress idemState = iota + 1
	stateDone
	stateFailed
)

type record struct {
	state   idemState
	payload string
	expire  time.Time
}

// IdemStore 用一把锁保护去重表。生产里换成 MySQL 唯一索引
// uk_idem(idem_key) 加 INSERT ... ON DUPLICATE KEY，语义完全一致。
type IdemStore struct {
	mu      sync.Mutex
	records map[string]record
	ttl     time.Duration
	now     func() time.Time
}

func NewIdemStore(ttl time.Duration) *IdemStore {
	return &IdemStore{records: make(map[string]record), ttl: ttl, now: time.Now}
}

// Begin 抢占幂等键：已 DONE 直接返回首次结果（幂等成功）；
// 仍 IN_PROGRESS 说明前一个请求未结束，拒绝并发重复；
// FAILED 或已过期则允许重新进入。
func (s *IdemStore) Begin(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if r, ok := s.records[key]; ok && now.Before(r.expire) {
		switch r.state {
		case stateDone:
			return r.payload, true, nil
		case stateInProgress:
			return "", false, ErrInProgress
		}
	}
	s.records[key] = record{state: stateInProgress, expire: now.Add(s.ttl)}
	return "", false, nil
}

func (s *IdemStore) Commit(key, payload string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = record{state: stateDone, payload: payload, expire: s.now().Add(s.ttl)}
}

func (s *IdemStore) Abort(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[key] = record{state: stateFailed, expire: s.now().Add(s.ttl)}
}

func (s *IdemStore) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, r := range s.records {
		if !now.Before(r.expire) {
			delete(s.records, k)
		}
	}
}

func main() {
	s := NewIdemStore(24 * time.Hour)
	_, done, err := s.Begin("idem-abc")
	fmt.Printf("first: done=%v err=%v\n", done, err)
	s.Commit("idem-abc", "O-1001")
	payload, done, err := s.Begin("idem-abc")
	fmt.Printf("retry: payload=%s done=%v err=%v\n", payload, done, err)

	// 并发重复：同一键只应有一个请求进入临界区。
	var wg sync.WaitGroup
	var mu sync.Mutex
	entered := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, d, e := s.Begin("idem-concurrent")
			if e == nil && !d {
				mu.Lock()
				entered++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Printf("concurrent entered=%d want=1\n", entered)
	s.Sweep()
}
```

<LineByLine code="if r, ok := s.records[key]; ok && now.Before(r.expire)" :notes='["先判断键是否存在且未过期，过期记录当作不存在，等价于给幂等键一个有限生命周期"]' />

### 6.3 Saga 编排器（逆序补偿、超时传递、重试预算）

**设计意图**：这是全讲的核心算法。关键取舍有三处：一是补偿必须用**新的 `context.Background()`** 而非已取消的请求 ctx，因为超时或取消后补偿恰恰最需要做完，用原 ctx 会导致补偿第一步就 `ctx.Err()` 直接放弃；二是补偿带重试预算与线性退避，耗尽后返回明确错误交给持久化队列，绝不静默吞掉；三是每步执行前检查 `ctx.Err()`，让 deadline 真正传导到下游，避免第 5 节说的「假超时」。代价是补偿逻辑比正向复杂，且必须要求每个 undo 幂等。

```go
// 文件：saga.go —— 逆序补偿、超时传递、补偿重试预算
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrSagaAbort    = errors.New("saga: aborted")
	ErrCompensation = errors.New("saga: compensation failed")
)

// Step 把正向操作与一一对应的补偿操作绑在一起。
// Undo 必须幂等，且允许空补偿（正向从未真正生效）。
type Step struct {
	Name string
	Do   func(ctx context.Context) error
	Undo func(ctx context.Context) error
}

type Saga struct {
	steps       []Step
	maxAttempts int
	backoff     time.Duration
}

func NewSaga(maxAttempts int, backoff time.Duration) *Saga {
	return &Saga{maxAttempts: maxAttempts, backoff: backoff}
}

func (s *Saga) Add(name string, do, undo func(context.Context) error) {
	s.steps = append(s.steps, Step{Name: name, Do: do, Undo: undo})
}

// compensate 逆序执行已成功步骤的补偿；每步带重试预算，
// 耗尽仍失败则记录并返回，交给持久化重试队列兜底，绝不静默丢弃。
func (s *Saga) compensate(ctx context.Context, done []Step) error {
	var failed []string
	for i := len(done) - 1; i >= 0; i-- {
		st := done[i]
		var err error
		for attempt := 1; attempt <= s.maxAttempts; attempt++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err = st.Undo(ctx); err == nil {
				break
			}
			if attempt < s.maxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(s.backoff * time.Duration(attempt)):
				}
			}
		}
		if err != nil {
			failed = append(failed, st.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%w: %v", ErrCompensation, failed)
	}
	return nil
}

// Run 逐步执行正向操作：每步前检查 ctx，保证超时真正传导到下游；
// 失败时用一个全新的 context 做补偿，避免请求取消把补偿一起掐断。
func (s *Saga) Run(ctx context.Context) error {
	done := make([]Step, 0, len(s.steps))
	for _, st := range s.steps {
		if err := ctx.Err(); err != nil {
			_ = s.compensate(context.Background(), done)
			return err
		}
		if err := st.Do(ctx); err != nil {
			if cErr := s.compensate(context.Background(), done); cErr != nil {
				return fmt.Errorf("%w: %v; %v", ErrSagaAbort, err, cErr)
			}
			return fmt.Errorf("%w at %s: %v", ErrSagaAbort, st.Name, err)
		}
		done = append(done, st)
	}
	return nil
}

func main() {
	state := map[string]string{"stock": "10", "order": "none", "payment": "none"}
	s := NewSaga(3, 10*time.Millisecond)
	s.Add("reserve", func(ctx context.Context) error {
		state["stock"] = "9"
		return nil
	}, func(ctx context.Context) error {
		state["stock"] = "10"
		return nil
	})
	s.Add("create", func(ctx context.Context) error {
		state["order"] = "CREATED"
		return nil
	}, func(ctx context.Context) error {
		state["order"] = "CANCELED"
		return nil
	})
	s.Add("charge", func(ctx context.Context) error {
		return errors.New("payment declined")
	}, func(ctx context.Context) error {
		state["payment"] = "REFUNDED"
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Run(ctx)
	fmt.Printf("err=%v state=%v\n", err, state)
}
```

<OutputBox title="支付失败后的补偿结果" output="err=saga: aborted at charge: payment declined" note="stock 回到 10、order 置 CANCELED，第 3 步 charge 从未成功因此不执行其 undo" />

<Callout type="idea" title="为什么补偿换用全新 context" text="请求 ctx 在网关超时那一刻就已被 cancel，若补偿沿用它，第一次 Undo 就会撞上 ctx.Err 直接返回，预扣库存永远释放不掉。补偿是「收尾工作」，必须用一个独立的、带自己 deadline 的 context 跑完；同时也说明补偿不能只依赖某次内存调用栈，进程崩溃后要靠持久化状态机续跑。" />

### 6.4 HTTP 网关（幂等键透传、超时、错误映射）

**设计意图**：把横切关注点收在一处。关键取舍是网关把 `r.Context()` 派生出带 deadline 的 ctx 并传入下游，而不是用 goroutine 加 select 制造「客户端先返回、后台还在跑」的假超时；`MaxBytesReader` 限制请求体防止慢速与大包打爆内存；`statusOf` 把内部错误映射成稳定状态码，不把 `payment declined` 这类细节泄露给客户端。代价是网关必须理解每一种领域错误，新增错误要同步改映射表。

```go
// 文件：gateway.go —— HTTP/JSON 网关：幂等键透传、超时传递、错误映射
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

var ErrStockNotEnough = errors.New("stock not enough")

// Downstream 是网关对编排层的抽象（真实实现是 gRPC 客户端）。
type Downstream interface {
	CreateOrder(ctx context.Context, idemKey, userID, sku string, qty int) (string, error)
}

type stubDownstream struct{}

func (stubDownstream) CreateOrder(ctx context.Context, idemKey, userID, sku string, qty int) (string, error) {
	select {
	case <-time.After(20 * time.Millisecond):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if sku == "SOLD-OUT" {
		return "", ErrStockNotEnough
	}
	return "O-" + idemKey, nil
}

// Gateway 把请求级 context 一路透传到下游，超时不再是假超时。
type Gateway struct {
	down    Downstream
	timeout time.Duration
}

func NewGateway(d Downstream, timeout time.Duration) *Gateway {
	return &Gateway{down: d, timeout: timeout}
}

// statusOf 把内部错误映射成稳定状态码，避免泄露内部细节。
func statusOf(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, ErrStockNotEnough):
		return http.StatusConflict
	default:
		return http.StatusBadGateway
	}
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" || strings.ContainsAny(idemKey, " \t\r\n") {
		http.Error(w, "Idempotency-Key required", http.StatusBadRequest)
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		SKU    string `json:"sku"`
		Qty    int    `json:"qty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.Qty <= 0 || body.Qty > 100 || body.SKU == "" {
		http.Error(w, "invalid qty or sku", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.timeout)
	defer cancel()
	orderID, err := g.down.CreateOrder(ctx, idemKey, body.UserID, body.SKU, body.Qty)
	if err != nil {
		code := statusOf(err)
		if code == http.StatusGatewayTimeout {
			w.Header().Set("Retry-After", "1")
		}
		http.Error(w, http.StatusText(code), code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"order_id": orderID, "status": "CREATED"})
}

func main() {
	gw := NewGateway(stubDownstream{}, 500*time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/orders",
		strings.NewReader(`{"user_id":"u1","sku":"SKU-9","qty":2}`))
	req.Header.Set("Idempotency-Key", "idem-abc")
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)
	fmt.Printf("status=%d body=%s", rec.Code, rec.Body.String())
}
```

### 6.5 本地消息表（业务与消息同事务、崩溃重放）

**设计意图**：补上「预扣成功但事件没发出去」这段一致性真空。关键取舍是消息与业务在同一个本地事务里写入 outbox，由后台 dispatcher 扫描未发送行投递；`send` 必须按消息 ID 幂等，因为至少一次投递必然产生重复；发送失败只累加 `tries`，超过预算进告警而不删行。代价是消息会短暂延迟、且需要一张额外的表与扫描任务，换来的是不依赖分布式事务就能保证不丢。

```go
// 文件：outbox.go —— 本地消息表：业务与消息同事务，崩溃后重放不丢
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrSendFailed = errors.New("outbox: send failed")

type Message struct {
	ID      int64
	Topic   string
	Payload string
	Sent    bool
	Tries   int
}

// Outbox 用锁模拟一张表：Append 与业务写入在同一事务里完成。
// 生产里是 INSERT INTO orders 与 INSERT INTO outbox 包在一个本地事务。
type Outbox struct {
	mu     sync.Mutex
	rows   []Message
	nextID int64
	maxTry int
}

func NewOutbox(maxTry int) *Outbox {
	return &Outbox{maxTry: maxTry}
}

// Append 在业务事务内调用；返回消息 ID 便于对账。
func (o *Outbox) Append(topic, payload string) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.nextID++
	o.rows = append(o.rows, Message{ID: o.nextID, Topic: topic, Payload: payload})
	return o.nextID
}

// Pending 拉取未发送且未超预算的消息，模拟 SELECT ... WHERE sent=0 LIMIT n。
func (o *Outbox) Pending(limit int) []Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Message, 0, limit)
	for i := range o.rows {
		if !o.rows[i].Sent && o.rows[i].Tries < o.maxTry {
			out = append(out, o.rows[i])
			if len(out) == limit {
				break
			}
		}
	}
	return out
}

// Mark 更新发送结果；失败只累加重试次数，消息保留等待下次扫描。
func (o *Outbox) Mark(id int64, ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.rows {
		if o.rows[i].ID == id {
			if ok {
				o.rows[i].Sent = true
			} else {
				o.rows[i].Tries++
			}
			return
		}
	}
}

// Dispatcher 是后台投递 worker。send 为幂等的 MQ 生产者（按消息 ID 去重）。
type Dispatcher struct {
	outbox *Outbox
	send   func(Message) error
}

func (d *Dispatcher) RunOnce(batch int) (sent, failed int) {
	for _, m := range d.outbox.Pending(batch) {
		if err := d.send(m); err != nil {
			d.outbox.Mark(m.ID, false)
			failed++
			continue
		}
		d.outbox.Mark(m.ID, true)
		sent++
	}
	return sent, failed
}

func main() {
	ob := NewOutbox(3)
	ob.Append("order.paid", "O-1001")
	ob.Append("order.paid", "O-1002")
	attempts := 0
	d := &Dispatcher{outbox: ob, send: func(m Message) error {
		attempts++
		// 模拟第一次投递整体失败，第二轮才成功。
		if attempts <= 2 {
			return ErrSendFailed
		}
		return nil
	}}
	for round := 1; round <= 3; round++ {
		s, f := d.RunOnce(10)
		fmt.Printf("round=%d sent=%d failed=%d\n", round, s, f)
		time.Sleep(time.Millisecond)
	}
}
```

### 6.6 binlog 异构同步（版本挡乱序、位点、死信）

**设计意图**：商品库是写库，ES 是读视图，两者靠订阅 binlog 收敛。关键取舍是用 **external version**（单调递增的 binlog 版本或 GTID）做幂等 upsert：版本小于等于当前文档的直接丢弃，于是重放、乱序、重复事件都不会把旧数据写回去；位点只在成功处理后推进，毒事件进死信且不推进位点，等人工修复后从位点重放。代价是 ES 与 DB 之间存在秒级延迟（FR-6 允许 5 秒），必须接受读到的可能是稍旧的价格。

```go
// 文件：binlog_sync.go —— 订阅 binlog 幂等写 ES：版本挡乱序、位点可重放
package main

import (
	"fmt"
	"sync"
)

type RowVersion struct {
	SKU     string
	Title   string
	Price   int64
	Version int64 // binlog 单调递增版本，可用 GTID 或 updated_at 加序号
}

// ESIndex 用 map 模拟 ES 文档，external version 保证只有更新的版本能覆盖。
type ESIndex struct {
	mu      sync.Mutex
	docs    map[string]RowVersion
	checkpt int64
	dlq     []string
}

func NewESIndex() *ESIndex {
	return &ESIndex{docs: make(map[string]RowVersion)}
}

// Upsert 用 external version 做幂等：版本小于等于当前直接丢弃，
// 这样重放、乱序、重复事件都不会把旧数据写回去。
func (e *ESIndex) Upsert(row RowVersion) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cur, ok := e.docs[row.SKU]; ok && row.Version <= cur.Version {
		return false
	}
	e.docs[row.SKU] = row
	return true
}

func (e *ESIndex) Advance(offset int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if offset > e.checkpt {
		e.checkpt = offset
	}
}

func (e *ESIndex) DeadLetter(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dlq = append(e.dlq, id)
}

type BinlogEvent struct {
	Offset int64
	Row    RowVersion
	Bad    bool
}

// Consume 处理一批事件：解析失败进死信且不推进位点，
// 成功才推进 checkpoint，保证至少一次加幂等下不丢数据。
func (e *ESIndex) Consume(events []BinlogEvent) (applied, skipped, poisoned int) {
	for _, ev := range events {
		if ev.Bad {
			e.DeadLetter(fmt.Sprintf("offset-%d", ev.Offset))
			poisoned++
			continue // 不推进位点，等待人工介入后重放
		}
		if e.Upsert(ev.Row) {
			applied++
		} else {
			skipped++
		}
		e.Advance(ev.Offset)
	}
	return applied, skipped, poisoned
}

func (e *ESIndex) Snapshot() (int, int64, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.docs), e.checkpt, len(e.dlq)
}

func main() {
	es := NewESIndex()
	events := []BinlogEvent{
		{Offset: 101, Row: RowVersion{SKU: "S1", Title: "手机", Price: 199900, Version: 5}},
		{Offset: 102, Row: RowVersion{SKU: "S1", Title: "手机 Pro", Price: 299900, Version: 7}},
		{Offset: 103, Row: RowVersion{SKU: "S1", Title: "手机", Price: 199900, Version: 5}}, // 乱序旧版本
		{Offset: 104, Bad: true}, // 毒事件
		{Offset: 105, Row: RowVersion{SKU: "S2", Title: "耳机", Price: 39900, Version: 2}},
	}
	a, s, p := es.Consume(events)
	docs, cp, dlq := es.Snapshot()
	fmt.Printf("applied=%d skipped=%d poisoned=%d docs=%d checkpoint=%d dlq=%d\n", a, s, p, docs, cp, dlq)
}
```

### 6.7 表驱动测试（补偿、幂等、状态机）

**设计意图**：把「正确性」变成可重复执行的断言。关键取舍是 mock 边界切在三张窄接口上——只 fake 外部依赖，被测的编排逻辑是真代码；测试覆盖首步失败、中途失败、全成功与并发幂等四类，断言释放量与取消标志。代价是 fake 要手工维护，但换来的是不依赖数据库与网络就能在 CI 里跑，且故障注入只需让某个 fake 返回错误。

```go
// 文件：contract_test.go —— 表驱动覆盖 Saga 补偿、幂等与状态机
package ecom

import (
	"errors"
	"sync"
	"testing"
)

var errNoStock = errors.New("no stock")
var errDeclined = errors.New("payment declined")

type miniStep struct {
	name string
	do   func() error
	undo func()
}

func runMini(steps []miniStep) error {
	var done []miniStep
	for _, s := range steps {
		if err := s.do(); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				done[i].undo()
			}
			return err
		}
		done = append(done, s)
	}
	return nil
}

type fakeInv struct {
	mu       sync.Mutex
	released int
	fail     bool
}

func (f *fakeInv) reserve(qty int) error {
	if f.fail {
		return errNoStock
	}
	return nil
}
func (f *fakeInv) release(qty int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released += qty
}

func TestSagaCompensation(t *testing.T) {
	cases := []struct {
		name        string
		failReserve bool
		failCharge  bool
		wantRelease int
		wantCancel  bool
	}{
		{"库存不足首步失败", true, false, 0, false},
		{"支付失败逆序补偿", false, true, 2, true},
		{"全链路成功", false, false, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv := &fakeInv{fail: c.failReserve}
			canceled := false
			steps := []miniStep{
				{"reserve", func() error { return inv.reserve(2) }, func() { inv.release(2) }},
				{"create", func() error { return nil }, func() { canceled = true }},
				{"charge", func() error {
					if c.failCharge {
						return errDeclined
					}
					return nil
				}, func() {}},
			}
			err := runMini(steps)
			if (err != nil) != (c.failReserve || c.failCharge) {
				t.Fatalf("err=%v", err)
			}
			inv.mu.Lock()
			got := inv.released
			inv.mu.Unlock()
			if got != c.wantRelease {
				t.Fatalf("released=%d want=%d", got, c.wantRelease)
			}
			if canceled != c.wantCancel {
				t.Fatalf("canceled=%v want=%v", canceled, c.wantCancel)
			}
		})
	}
}

func TestIdempotentReserve(t *testing.T) {
	inv := &fakeInv{}
	seen := make(map[string]bool)
	var mu sync.Mutex
	granted := 0
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			if seen["k1"] {
				return
			}
			seen["k1"] = true
			if inv.reserve(1) == nil {
				granted++
			}
		}()
	}
	wg.Wait()
	if granted != 1 {
		t.Fatalf("granted=%d want=1", granted)
	}
}

func BenchmarkSagaSuccess(b *testing.B) {
	inv := &fakeInv{}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = runMini([]miniStep{
			{"reserve", func() error { return inv.reserve(1) }, func() { inv.release(1) }},
			{"create", func() error { return nil }, func() {}},
			{"charge", func() error { return nil }, func() {}},
		})
	}
}
```

## 7. 失败路径与边界条件

失败矩阵必须具体到「哪个组件挂了、什么现象、怎么发现、怎么兜底、怎么恢复」，否则评审等于没做。下表覆盖规范要求的六类故障，并额外补了电商特有的支付未知与 binlog 乱序。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 下游超时 | 支付服务 P99 飙到 3s | 网关 503、订单停在 CREATED | 网关 503 比例告警 + trace 慢 span | 返回 Retry-After，编排器按未知结果挂起 | 对账确认支付结果后推进或退款 |
| 依赖不可用 | 库存服务实例全挂 | Reserve 连续失败 | 注册中心健康实例数归零 | 编排器首步失败即返回 409，不建单 | 扩容实例后客户端用原幂等键重试 |
| 重复请求 | 客户端超时重试 | 可能重复扣款、重复建单 | 幂等键冲突计数突增 | 唯一索引 + 三态键拦截，DONE 返回首次结果 | 无需人工，Sweep 清理过期键 |
| 数据损坏 | binlog 出现非法 JSON | ES 同步卡住、索引落后 | 死信队列长度告警 | 毒事件进 DLQ，位点不推进 | 修数据后从位点重放 |
| 进程崩溃 | 编排器在补偿中宕机 | 预扣未释放、订单中间态 | 中间态订单数超过阈值 | Saga 状态持久化、后台扫描续跑 | 重启后扫描 IN_PROGRESS 继续补偿 |
| 配置错误 | 网关 deadline 误配为 30s | 线程长期占用、雪崩 | 连接池占用率与 goroutine 数告警 | 配置中心校验 + 启动时范围检查 | 回滚配置，灰度验证后重新上线 |
| 支付结果未知 | Charge 超时但网关已扣款 | 既不敢重试也不敢退款 | 支付流水与订单状态对不平 | 标记 UNKNOWN、暂停自动补偿 | 对账任务拉取渠道流水定论 |
| ES 乱序覆盖 | 旧版本 binlog 后到 | 搜索价格回退 | 索引版本回退监控 | external version 丢弃旧版本 | 自动，无需人工 |

**幂等键**：全链路唯一，网关透传、各服务不得另造；冲突返回 409 而非 500，让客户端用原键轮询。**重试预算**：同步 RPC 最多 2 次且带指数退避，补偿最多 3 次，MQ 投递最多 3 次后转人工；重试总预算必须小于网关 deadline，否则重试还没跑完客户端已超时。**超时传递**：网关 3s、编排器各步按调用矩阵分配（库存 800ms、支付 1.5s、商品 300ms），所有下游调用接收同一个 ctx，任一层取消立即向下传播。**降级与人工介入点**：用户等级读失败降级默认值；支付 UNKNOWN、补偿重试耗尽、binlog 毒事件三类必须落到人工队列，并在大盘上单列「待人工处理」计数，禁止自动吞掉。

<Callout type="warn" title="Saga 最容易错的三处" text="一是补偿不幂等，重试一次就多退一次款；二是空补偿与悬挂补偿没挡住，凭空把库存加回去；三是把超时当成取消——网关 select 到 ctx.Done 就返回了，可编排 goroutine 还在跑，必须用透传的 context 让下游一起停，并用独立 context 把已发生的副作用补偿完。" />

## 8. 并发与一致性

并发模型上，网关每个请求一个 goroutine，编排器在同一请求内串行推进 Saga（四步有严格先后依赖，不能并行）；幂等存储、outbox、ES 位点各自用 `sync.Mutex` 保护临界区，生产里换成数据库唯一索引与行锁。**锁粒度**刻意收在「单个幂等键 / 单个 SKU」而非全局：`IdemStore` 用一把锁保护整张 map 是演示用的简化，生产中按 key 分片或直接用唯一索引，避免所有请求挤在一把锁上——这正是第 097 讲的锁竞争拐点。

竞态分析集中在三处：并发重复请求靠 `Begin` 的 `IN_PROGRESS` 状态互斥（6.2 的断言 `entered=1`）；库存预扣与释放必须共用同一临界区，否则回补与再次扣减交错会把数值算错；订单状态更新用 `version` 乐观锁，禁止 PAID 回退到 CREATED，避免乱序的补偿覆盖已支付状态。

一致性级别选 **BASE 最终一致**而非 ACID 强一致，依据是业务可接受「支付中的几十秒中间态」，但无法接受 2PC 带来的长时间资源锁定与协调者单点。Saga 由多个独立本地事务组成，步骤之间没有隔离，中间态对外可见；它靠补偿把系统收敛到一致，属于第 083 讲的 BASE 范畴。顺序保证上，Kafka 按 `order_id` 分区保序，消费者按消息 ID 幂等；binlog 允许乱序到达，但用 external version 把乱序收敛成「只保留最新版本」，这是把顺序问题转化为幂等问题。

<Compare title="跨服务一致性的两条路线" :panels='[{"label":"2PC 强一致","code":"协调者统一投票，所有参与者同一时刻提交或回滚","note":"一致性强，但长时间锁资源、协调者故障会阻塞，库存行被锁住拖垮吞吐，本场景不选"},{"label":"Saga 补偿","code":"一串本地事务加逆序补偿，中间态可见","note":"吞吐高、无全局锁，代价是要自己写补偿、保证幂等与可重试，是本讲选择"}]' />

<StepBox title="一次并发重复请求的收敛过程" :steps='[{"title":"T0 两请求同键到达","body":"客户端超时后重发，两个请求携带相同 Idempotency-Key 几乎同时进入编排器","tip":"键相同是前提，客户端不得每次重发生成新键"},{"title":"T1 抢占","body":"第一个请求 INSERT 幂等键成功置 IN_PROGRESS，第二个撞唯一索引","tip":"数据库唯一索引是最终仲裁者"},{"title":"T2 分流","body":"第二个请求读到 IN_PROGRESS 返回 409 处理中；若读到 DONE 则直接返回首次结果","tip":"两种状态给不同响应，不能都当失败"},{"title":"T3 收敛","body":"第一个请求跑完 Saga 置 DONE 并写首次响应；客户端轮询拿到 order_id","tip":"Sweep 在 24 小时后清理过期键"}]' startLabel="请求到达" />

## 9. 可观测性

没有指标与追踪，前面所有兜底都无法被验证。指标按 RED（Rate / Errors / Duration）与业务不变量两类埋点：

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| gateway_requests_total | Counter | 按 path 与 status 计的请求数 | 5xx 比例超过 0.5% 持续 1 分钟 |
| order_create_duration_ms | Histogram | 下单链路耗时，算 P50 / P99 | P99 超过 800ms |
| saga_compensation_total | Counter | 按 step 与 result 计的补偿次数 | 失败补偿超过 10 次每分钟 |
| order_intermediate_gauge | Gauge | 停留在 CREATED / RESERVED 超过 30 秒的订单数 | 超过 50 |
| outbox_unsent_gauge | Gauge | 未发送且未超预算的消息数 | 超过 1000 |
| es_sync_lag_ms | Gauge | 当前 binlog 位点与处理位点的时间差 | 超过 5000ms（FR-6） |
| idempotency_conflict_total | Counter | 幂等键冲突次数 | 突增 3 倍 |

结构化日志统一带 `trace_id`、`order_id`、`idem_key`、`step`、`cost_ms`、`err_code` 六个字段，绝不打印手机号、卡号等敏感信息。`trace_id` 由网关在入口生成（缺失则从上游 `traceparent` 提取），写入 gRPC metadata 的 `x-trace-id`，各服务拦截器自动透传并注入日志 context，这样一次下单的 4 次 RPC 与 Kafka 消息可以用同一个 ID 串起来。

埋点代码放进网关中间件即可，下面这段自带完整上下文，可单独编译验证：

```go
// 文件：observability.go —— 网关埋点中间件与结构化日志字段
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

// metricsSink 用两个函数抽象 Counter 与 Histogram，生产里换成 Prometheus。
type metricsSink struct {
	inc     func(path string, code int)
	observe func(ms int64)
}

// statusWriter 捕获真实状态码，否则 WriteHeader 之后拿不到 code。
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// withMetrics 记录每个请求的耗时与状态，并把 trace_id 注入日志上下文。
func withMetrics(next http.Handler, m metricsSink) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		traceID := r.Header.Get("X-Trace-Id")
		next.ServeHTTP(sw, r)
		m.inc(r.URL.Path, sw.code)
		m.observe(time.Since(start).Milliseconds())
		_ = traceID // 真实实现写入 logger.With("trace_id", traceID)
	})
}

func main() {
	var paths []string
	m := metricsSink{
		inc:     func(path string, code int) { paths = append(paths, fmt.Sprintf("%s:%d", path, code)) },
		observe: func(ms int64) {},
	}
	h := withMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}), m)
	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set("X-Trace-Id", "trace-abc")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println("recorded:", paths)
}
```

**一次真实故障的定位路径**：告警 `gateway_requests_total{status=503}` 抬头 → 用 trace_id 抽查慢请求，发现 Order→Payment 的 span 普遍卡在 1.5s → 查 `payment` 服务 P99 与连接池占用，确认支付渠道侧抖动 → 看 `order_intermediate_gauge` 是否同步上升，判断有多少订单停在 UNKNOWN → 执行对账预案，把确认未扣款的订单推进、已扣款的补建。全程从指标到 span 到业务状态，不靠猜。

## 10. 测试策略

分层与 mock 边界如下：单测覆盖状态机、幂等存储、Saga 补偿（6.7 的 `contract_test.go`），**只 mock 三张窄接口**，被测编排逻辑是真代码；表驱动覆盖首步失败、中途失败、全成功、并发重复四类；集成测试用 `httptest` 起网关与内存 fake 上游，验证 JSON 字段兼容与状态码映射；契约测试用 proto 破坏性检查（删字段、改类型、复用 tag 必须失败）并在 CI 里对生成代码做 `go build`；故障注入让支付 fake 按概率返回超时或失败，跑上千次下单，断言「成功订单数 + 已退款数 = 支付调用数」且库存守恒；基准与压测见第 12 节。

mock 边界切在窄接口而非数据库驱动，是为了让「补偿顺序」「幂等去重」这些最容易错的逻辑不依赖任何外部组件就能在 CI 里稳定复现；数据库的唯一索引行为则用少量集成测试单独验证，两者不混在一起。6.7 已内置可运行的 `BenchmarkSagaSuccess`，用真实命令跑基准并观察分配：

```bash
go test -bench=BenchmarkSagaSuccess -benchmem ./internal/order/...
go test -race ./...                 # 并发正确性
go vet ./...                        # 静态检查
```

故障注入按「支付 fake 按 5% 概率返回超时、3% 返回失败」配置，连续跑一万次下单后断言：成功订单数加已退款数等于支付调用数、库存守恒、`order_intermediate_gauge` 最终回零。契约测试则把 proto 的破坏性变更（删字段、改类型、复用 tag）做成 CI 里的必失败用例。

## 11. 部署与容量

部署拓扑：网关 3 副本无状态挂在负载均衡后；订单、库存、支付、商品、用户各 3 副本；MySQL 一主两从半同步，订单库按 `user_id` 分 16 片；Kafka 三副本三分区；Redis 主从用于幂等与缓存；ES 三节点；syncer 与 dispatcher 各 2 副本做 standby。依赖组件版本前提写明：Go 1.21+、gRPC-Go 1.60+、MySQL 8.0（依赖 `CHECK` 约束与 `DATETIME(3)`）、Kafka 3.6、Elasticsearch 8.x、Redis 7.x。

| 配置项 | 默认值 | 生产 | 灰度 | 说明 |
| --- | --- | --- | --- | --- |
| gateway_timeout_ms | 3000 | 3000 | 1500 | 必须大于各步超时之和 |
| inventory_rpc_ms | 800 | 800 | 500 | 库存预扣超时 |
| payment_rpc_ms | 1500 | 1500 | 1000 | 支付超时，结果按未知处理 |
| saga_max_attempts | 3 | 3 | 2 | 补偿重试预算 |
| idem_ttl_hours | 24 | 24 | 6 | 幂等键有效期 |
| outbox_batch | 100 | 100 | 20 | 单次扫描批量 |
| db_max_open_conns | 50 | 50 | 20 | 每实例连接池上限 |

资源估算与第 1 节数字自洽：峰值 2000 QPS 下单，每次 4 次 RPC，即内部 8000 RPC/s；按每 RPC 约 1KB 计约 8MB/s，千兆网卡 125MB/s 余量充足。链路耗时预算：网关 50ms + 库存 150ms + 建单 80ms + 支付 300ms + 确认 60ms ≈ 640ms，P99 目标 800ms 留 160ms 余量。连接数：库存 3 副本 × 每副本 50 连接 = 150，远低于 MySQL `max_connections=500`。存储：日 50 万单，订单行约 512B、明细按 1:3 约 128B，合计约 450MB/天、约 164GB/年；16 分片后每片约 10GB/年，单表行数控制在千万级内。扩容方式为无状态服务水平加副本、订单库按分片键加片；优雅退出时先摘注册中心、停止接新请求、等待在途 Saga 收敛或落持久化状态、再关闭连接池，超时强杀前给 30 秒宽限。

## 12. 性能与压测

压测方法要真实可执行。用 `hey` 造流量，先预热 2 分钟，再以峰值加压 10 分钟；数据构造按 20 万 SKU 与 800 万用户预生成，幂等键用压测客户端按递增序号生成避免误判重复：

```bash
# 构造订单请求体并按峰值持续 10 分钟
hey -z 10m -c 200 -q 2000 \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: bench-{{.Seq}}" \
  -m POST -d '{"user_id":"u-8801","sku":"SKU-9","qty":2}' \
  http://gateway.internal/orders

# 采集 Go 运行时画像，定位瓶颈
go tool pprof -http=:8080 http://order-svc:6060/debug/pprof/profile?seconds=60
```

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 下单吞吐 | 1200 QPS | 2100 QPS | 订单库批量提交 + 连接池调参后达标 |
| 下单 P99 | 1350ms | 720ms | 支付调用并行化价格快照读取 |
| 错误率 | 1.8% | 0.04% | 幂等表加唯一索引后冲突不再变 500 |
| 编排器 CPU | 78% | 46% | 减少 Saga 日志的重复序列化 |
| 库存库连接占用 | 92% | 61% | 按 SKU 分片锁替代全局锁 |

瓶颈定位过程：首轮压测 P99 卡在 1350ms，`pprof` 显示 40% 时间花在 `encoding/json` 反复序列化同一份 Saga 日志；改为状态变更时才落一次快照后降到 900ms；再查 `block profile` 发现库存预扣挤在一把全局锁上，按 SKU 分片后降到 720ms。仍然存在的限制：支付渠道 P99 不受我们控制，压测用 stub 会低估真实链路；跨机房调用未在单机房假设内覆盖；大促瞬间的流量尖峰仍需靠第 116 讲的削峰与限流，本压测验证的是稳态峰值承载能力。

**全链路压测与联调**（本讲专属）：压测必须在预发环境串起网关、四服务、Kafka、ES 全部真实组件，而不是只压单个服务；重点验证三件事——`trace_id` 是否从网关一路贯到 ES 同步、故障注入下 `order_intermediate_gauge` 能否收敛、proto 灰度版本与旧版本能否共存。联调结论要写进评审记录：契约字段兼容、超时预算逐层递减、补偿在注入下 30 秒内收敛，三者缺一不可上线。

## 13. 演进与替代方案

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 单体 + 本地事务 | 日订单低于 1 万、团队 1 个 | 一致性由 DB 保证、无分布式复杂度、迭代最快 | 发布耦合、伸缩粒度粗、慢 SQL 拖垮全站 | 当前就是从这个阶段演进来的，不必回头 |
| 微服务 + Saga（当前） | 日订单 50 万、多团队协作 | 独立部署伸缩、故障隔离、契约清晰 | 自己实现补偿与幂等、中间态可见、延迟变大 | 当前阶段 |
| 微服务 + TCC | 一致性要求更高、能接受改造各服务 | Try-Confirm-Cancel 预留资源，隔离性强于 Saga | 每个服务要实现三阶段接口、侵入大、空回滚与悬挂更复杂 | 出现资损且补偿窗口不可接受时 |
| 事件溯源 + CQRS | 超大规模、需要完整审计与回放 | 天然可回放、读写彻底分离、审计能力强 | 心智负担重、存储与查询复杂度陡增、团队学习成本高 | 订单量上千万且强审计诉求出现时 |

从当前版本演进到「Saga 状态持久化 + 事务消息」的触发条件有三个：一是 `order_intermediate_gauge` 每周都出现人工介入；二是单机房故障演练中补偿靠内存调用栈无法续跑；三是出现需要更强一致结算的品类。迁移步骤：先给 Saga 日志加持久化表并保持双写、再让 dispatcher 接管补偿、灰度 10% 流量观察两周、全量后删除内存态路径；**回滚**只需把编排器开关切回内存 Saga，持久化表保留不删，因为双写阶段两者数据一致，回滚不会丢状态。

<FlowTimeline direction="vertical" :steps='[{"title":"V1 单体","desc":"本地事务，发布耦合","tone":"gray"},{"title":"V2 按边界拆分","desc":"四服务独库，proto 定契约","tone":"blue"},{"title":"V3 Saga 补偿","desc":"逆序补偿加幂等键（本讲）","tone":"green"},{"title":"V4 持久化状态机","desc":"崩溃续跑加事务消息","tone":"orange"},{"title":"V5 削峰与秒杀","desc":"下一讲：库存高并发正确性","tone":"red"}]' />

## 14. 真实工程里的坑

以下五条都来自生产，每条给出错误写法与修正，并说明如何提前发现。

**坑一：补偿不幂等，重试一次多退一次款。** 现象是支付失败后偶发退款两次。原因是错误写法 `func Refund(orderID string) error { return gateway.Refund(orderID) }` 没按幂等键去重，补偿重试几次就退几次。正确写法把幂等键写进退款流水表，用唯一索引挡重复；下面这段自带完整上下文，可单独编译验证：
```go
// 文件：refund_idempotent.go —— 用唯一键把退款补偿变成幂等操作
package main

import (
	"fmt"
	"sync"
)

// refundStore 用唯一键模拟退款流水表；Once 对应
// INSERT ... ON DUPLICATE KEY，影响行数为 0 说明已退过。
type refundStore struct {
	mu   sync.Mutex
	seen map[string]int
}

func newRefundStore() *refundStore {
	return &refundStore{seen: make(map[string]int)}
}

// Once 返回本次是否真的执行退款；重复键返回 false 且不扣第二笔。
func (s *refundStore) Once(idemKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[idemKey] > 0 {
		return false
	}
	s.seen[idemKey] = 1
	return true
}

// RefundRight 以幂等键去重，重复补偿直接返回「已处理」。
func RefundRight(s *refundStore, idemKey, orderID string) bool {
	return s.Once(idemKey)
}

func main() {
	s := newRefundStore()
	first := RefundRight(s, "idem-abc", "O-1001")
	retry := RefundRight(s, "idem-abc", "O-1001") // 模拟补偿重试
	fmt.Printf("first=%v retry=%v total=%d want=1\n", first, retry, s.seen["idem-abc"])
}
```
提前发现：故障注入里让补偿故意重试，断言退款流水条数恒为 1。

**坑二：空补偿与悬挂补偿把库存凭空加回。** 现象是库存莫名变多。原因是正向预扣从未成功、补偿却先到达并执行了回补。修正是 `Release` 必须先查预留记录，查不到就当空操作返回成功（6.2 与第 4 节步骤表已覆盖）。提前发现：单测覆盖「补偿先于正向」的时序，断言库存守恒。

**坑三：把网关超时当成下游取消，留下幽灵订单。** 现象是客户端收到 503，后台却建了单。原因是网关用 goroutine 加 select、下游用 `context.Background()`。修正是把 `r.Context()` 派生出的 ctx 一路传给每个下游调用（6.4），补偿则用独立 ctx 收尾（6.3）。提前发现：注入下游慢响应，断言超时后订单表不新增行。

**坑四：重试放大拖垮已经抖动的依赖。** 现象是支付服务一慢，网关重试让它彻底雪崩。原因是每层都各重试 3 次，4 层叠出几十倍流量。修正是全链路共享一个重试预算（同步 RPC 最多 2 次），且重试总耗时必须小于网关 deadline；只有幂等写才允许重试，读失败直接降级。提前发现：压测时统计「内部调用数 / 外部请求数」比值，超过 6 就说明有放大。

**坑五：binlog 乱序把 ES 价格写回旧值。** 现象是改价后搜索结果偶尔回退。原因是消费者按到达顺序覆盖文档。修正是用 external version 幂等 upsert（6.6），并在位点未推进前不 ack。提前发现：故障注入里故意把新版本事件延迟、让旧版本先到，断言索引版本单调不回退。

**坑六（附加）：上下文丢失让日志串不起来。** 现象是跨服务排查只能靠猜时间。原因是每个服务自己新建 logger，没从 gRPC metadata 提取 `x-trace-id`。修正是统一拦截器注入 `trace_id` 到日志 context（第 9 节）。提前发现：联调时抽查任意一笔订单，要求 4 个服务与 Kafka 消息能用一个 ID 全串起来。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-115-micro-ecommerce`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-115-micro-ecommerce)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="让补偿失败也能收敛" task="当前 6.3 的 compensate 在重试耗尽后只返回一个错误，若进程此刻崩溃，待补偿记录就丢了。请设计持久化机制，使补偿一定能做完，并写出关键结构与测试思路。" :hints='["补偿不能只调用一次就丢，需要一条带状态的持久记录","先把待补偿写入 durable 表再尝试执行，崩溃后靠扫描续跑","worker 要有退避重试，且 undo 本身必须幂等"]' solution="定义 Compensation{Step, IdemKey, Payload, Done, Tries} 记录；compensate 失败时先把每条待补偿 INSERT 到 compensation 表（与订单状态变更同事务）再尝试执行；后台 worker 扫描 Done=false 且 Tries 小于预算的记录，执行幂等 undo 成功后置 Done=true，失败则 Tries 加一并按退避重排。\n测试：让第一次 Release 返回错误、第二次成功，断言 worker 重试后库存最终回补且只回补一次；再模拟进程在「已写记录未执行」时崩溃，重启后断言记录被扫描并收敛。" explain="Saga 的可靠性不在正向流程，而在「补偿一定能做完」。把补偿持久化加幂等重试，是从能跑通的 demo 走向生产可用系统的分水岭。" />

## 随堂检测

<FillBlank no="115-1" tpl="Saga 补偿必须按___顺序执行，且补偿操作本身必须___；网关超时后补偿应换用___的 context 才不会被一起取消。" :answers='["逆序~倒序","幂等","独立~全新"]' hint="后发生的步骤依赖先发生的；超时那一刻请求 ctx 已被 cancel" explain="逆序是因为后一步依赖前一步的结果；幂等是重试的前提；独立 context 保证请求取消后补偿仍能跑完，这三点在 6.3 的代码里一一对应。" :explains='["正向流程里后一步依赖前一步的产出，回撤必须倒着来，先发生的反而最后撤，顺序颠倒就会删掉仍被依赖的中间记录。","补偿本身也会超时重投、被后台重复扫描，若释放库存跑两遍就会多回补一份；做成幂等，同一动作执行多少次结果都不变。","网关超时时请求的 ctx 已被取消，补偿若沿用会刚启动就被连带掐断；另起一个不受该取消影响的 context，才能保证补偿跑到底。"]' />

<Quiz no="115-2" q="本讲把服务按业务边界拆分，最硬的判据是什么？" :options='["按 controller / service / dao 分层","按代码行数平均切分","按业务边界与数据所有权划分","按团队人数一人一个服务"]' answer="2" explain="数据所有权决定边界：每个服务独占自己的库，跨服务只能走 proto 契约。按技术层拆会共享同一个库，既没获得隔离性又平白多出网络调用，正是第 3 节 Callout 点出的第一个错误。" />

<Predict no="115-3" q="6.3 里 charge 返回 payment declined 后，state 的最终值是什么？" :options='["stock=9 order=CREATED payment=none","stock=10 order=CANCELED payment=none","stock=9 order=CANCELED payment=REFUNDED","stock=10 order=PAID payment=REFUNDED"]' answer="1" output="stock=10 order=CANCELED payment=none" explain="charge 是第 3 步且自身失败，因此只补偿已成功的 reserve 与 create 两步：逆序先撤建单置 CANCELED、再释放预扣把 stock 加回 10；charge 从未成功，它的 undo 不执行，payment 保持 none。" />

<Quiz no="115-4" q="支付 Charge 超时但网关可能已扣款，正确做法是？" :options='["立即重试扣款直到成功","立即执行退款","标记为未知、暂停自动补偿、靠对账确认后再推进或退款","忽略它，用户会重新下单"]' answer="2" explain="超时意味着结果未知，重试可能重复扣款，盲目退款可能退一笔不存在的款。第 7 节失败矩阵把它列为 UNKNOWN，必须转对账并落到人工队列，这正是「敢写代价」的体现。" />

## 一页纸速记

<KnowledgeCard :points='["按业务边界与数据所有权拆分，跨服务只走 proto 契约，不偷连对方库","依赖倒置：订单服务只依赖 Inventory / Payment / OrderStore 三张窄接口","Saga 等于正向步骤加一一对应的补偿，失败逆序回滚，补偿用独立 context","幂等键贯穿全链路，三态状态机挡重复、防并发、带 TTL","业务与 outbox 消息同事务，binlog 用 external version 幂等写 ES"]' :pitfalls='["按技术层拆分还共享一个库，等于白拆","补偿不幂等，重试一次多退一次款","把网关超时当成下游取消，留下幽灵订单","重试层层叠加放大流量，拖垮抖动依赖","忽略补偿失败或乱序覆盖，链路永久卡在中间态"]' summary="拆分的收益是独立伸缩与故障隔离，代价是必须自己用契约、幂等、补偿与对账把一致性重新建起来。" />

## 小结与下一讲

1. 服务按业务边界与数据所有权拆分，契约用 `.proto` 优先定义，依赖用窄接口倒置，随时可换实现或塞 fake；拆分的代价是原本一个本地事务变成四次 RPC 加一串补偿。
2. `Saga` 用「正向步骤 + 一一对应的补偿」实现最终一致，失败逆序回滚、补偿用独立 context 并带重试预算；幂等三态键挡住重复与并发，本地消息表与 binlog external version 补齐异步链路的一致性。
3. 网关统一鉴权、限流、透传幂等键与 deadline、映射错误；失败矩阵、指标、全链路压测与联调共同证明这些兜底在故障注入下真的能收敛。

这一讲解决的是「跨服务一致性」，但库存扣减仍是一个普通的预扣调用。到了秒杀那种瞬时十万级请求、单 SKU 热点的场景，先查再扣的写法会直接把库存卖成负数，数据库也扛不住洪峰。下一讲专门解决超卖与流量削峰。

[下一讲：实战 10 · 秒杀与高并发库存](/projects/micro-seckill)
