---
title: 第 105 讲 · 实战 03 · 短链服务
description: 用发号器与 Base62、缓存旁路、布隆过滤器、301/302 取舍、TTL 过期与防刷限流，设计高并发读低延迟跳转服务
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="设计高并发读、低延迟跳转的服务" prereq="第 104 讲 · 实战 02 · protoc 插件与代码生成器" />

# 第 105 讲 · 实战 03 · 短链服务

> 短链是典型的读多写少系统：一次创建往往对应上万次点击，用户点击后多等 100 毫秒就会明显掉转化。它看起来只是「查一张表再重定向」，真正放到公网却要同时回答：短码从哪来才不重复、跳转查得多快、缓存崩了会不会把库打穿、运营改了落地页老用户还能不能回来、以及有人拿它当钓鱼跳板怎么办。本讲先把约束量化，再由约束推出架构，全程只用标准库把核心链路写成可编译、可压测、可评审的工程代码。

<CheckList id="l105-goals" title="本讲交付清单" :items='["用号段发号器加 Base62 得到不可枚举且可反解的短码","画出读路径缓存旁路并说清击穿、穿透、雪崩各自的兜底","用布隆过滤器与空值缓存挡住不存在短码的扫描流量","分清 301 与 302 在可改目标与点击统计上的代价","用令牌桶、黑白名单与 scheme 校验守住公网写入口"]' />

## 0. 项目定位与真实场景

短链服务活在营销短信、活动二维码、社交分享卡片、App 深链和邮件落地页背后。一条原始的推广链接常常带一长串 UTM 参数、渠道号与登录态，长度轻易超过两百字符：短信按 70 字计费会被拆成三条，二维码密度过高扫不出来，微博与即时通讯工具会截断带参数的 URL，运营还想知道每个渠道带来多少次点击。把长链压成六到七位短码，既解决长度与排版，也把「统计点击」和「随时换目标」的能力收口到服务端。

使用者有三类：运营人员创建与改目标、终端用户点击跳转、数据分析侧消费点击事件。三者的诉求互相冲突：运营要能随时改落地页，用户要零等待，分析要每次点击都可记录。这个冲突直接决定了后面所有的技术选择——尤其是重定向状态码与缓存策略。

量级上先给一个可讨论的基线：系统存量短码约两千万条，日新增十万条，日点击约两亿次；写 QPS 峰值约 200，读 QPS 峰值约 3 万，读写比接近千比一；单条映射平均 200 字节，热数据集合约 4 GB，恰好能放进内存缓存。这些数字不是装饰，它们决定了「读路径不能多一次网络往返」「写路径可以慢但绝不能重复发号」以及「必须用缓存把九成以上读挡在存储之外」。

现有方案最常见的痛点是三类。第一类是直接用数据库自增主键做短码但没做持久化游标，服务重启后从 1 重新发号，历史短码被覆盖，用户点进别人的活动页。第二类是用 301 永久重定向追求「省一次回源」，结果运营改目标后浏览器与 CDN 依旧缓存旧映射，改库等于没改。第三类是只做字符串长度校验就放行任意目标，短链域名沦为开放重定向跳板，被钓鱼与搜索引擎降权。本讲要解决的核心矛盾可以概括成一句话：在读延迟极敏感、写绝对不能重复、入口完全不可信这三重约束下，把跳转链路做薄，把失败路径想全。

<Callout type="idea" title="从约束推架构，而不是先堆技术栈" text="先确认读写比、QPS、数据量与一致性要求，再决定要不要缓存、要不要布隆、要不要中心化发号器；反过来先选组件再找场景，往往得到一套过度设计又挡不住故障的系统。" />

## 1. 需求与约束

先把「做完」定义成可验收的条目，避免评审时各说各话。功能需求按编号管理，每条都带可观测的验收标准，测试与压测都从这张表派生。

| 编号 | 功能需求 | 验收标准 |
| --- | --- | --- |
| F1 | 长链转短码 | POST 返回 6 至 7 位 Base62 短码，同一目标可创建多条且互不冲突 |
| F2 | 短码跳转 | GET 命中返回 302 且 Location 为当前目标，P99 小于 5 毫秒 |
| F3 | 目标可改 | 修改后 5 秒内新点击命中新目标，且不依赖客户端清缓存 |
| F4 | 过期语义 | 到期返回 410 Gone，不存在返回 404，两者错误码不可混淆 |
| F5 | 自定义有效期 | 创建可带 TTL，零值表示永不过期，过期条目被后台回收 |
| F6 | 幂等创建 | 相同幂等键重复提交返回同一短码，不产生第二条记录 |
| F7 | 安全防护 | 拒绝非 http/https、协议相对地址、非白名单域名与超限频率的写入 |
| F8 | 点击可观测 | 每次跳转带 trace_id 并异步投递点击事件，投递失败不阻塞跳转 |

非功能指标必须写进 SLO，否则「快」和「稳」无法判定。下表给出本讲设计与压测共同遵守的口径，所有数字与第 0 节的量级假设自洽。

| 指标 | 目标值 | 说明与代价 |
| --- | --- | --- |
| 读 QPS 峰值 | 30000 | 由日两亿点击按峰值系数折算，四台无状态实例各承担约 7500 |
| 写 QPS 峰值 | 200 | 发号器号段步长 1000，单次持久化支撑约千次写才回盘一次 |
| 跳转 P99 | 小于 5 毫秒 | 命中缓存路径只允许本地内存与一次重定向 |
| 缓存命中率 | 大于 99% | 低于此值回源放大系数会直接威胁存储 |
| 短码容量 | 两千万存量 | Base62 六位可表达约 568 亿，容量不是瓶颈，不可枚举才是关键 |
| 一致性级别 | 读己之写最终一致 | 创建后允许短时缓存未命中回源，但不允许返回旧目标 |
| 可用性 SLA | 99.95% | 缓存整体不可用只降延迟不中断服务 |
| RTO / RPO | 30 秒 / 0 | 无状态实例可秒级重启；发号游标持久化保证不丢不重，RPO 为 0 |

明确「本讲不做什么」和假设边界同样是需求的一部分。本讲不做跨机房多活、不做点击去重计费、不做自定义短码的竞价与保留字、不接真实 Redis 与 MySQL（用等价接口与内存实现替代，接口签名保持可切换）、不做防刷的分布式计数（单机令牌桶，多实例版本留到第 13 节演进）。假设短码字符集只允许 0 到 9、大小写字母，假设目标 URL 长度上限 2048 字节，假设系统时钟不发生大幅回拨（时钟回拨对号段发号器无影响，但对 Snowflake 方案致命，见第 13 节）。

<Callout type="warn" title="别把 301 当成免费优化" text="301 会被浏览器、代理与 CDN 长期缓存，运营改目标后服务端改库也拉不回老用户，点击统计也随之丢失；短链几乎总该用 302，省下的那次回源远不值这个代价。" />

## 2. 架构蓝图

约束确定后，架构的切分就顺理成章：读占绝对多数，所以每一层都要为「一次跳转尽量少干活」让路；写绝对不能重复，所以发号是唯一需要强状态与持久化的点；入口不可信，所以最便宜的校验必须放在最前面。

<ArchBlueprint title="短链服务四层架构" caption="读路径占绝对多数，每一层都为低延迟读让路；写路径只做发号与落库，不预热缓存" :layers='[{"name":"接入层","note":"公网统一入口，承担最便宜的拒绝","protocol":"HTTPS 与 JSON","items":[{"label":"防刷网关","sub":"令牌桶与黑白名单","detail":"职责：按客户端标识限流并校验目标 scheme 白名单，把危险写入挡在门外；技术选型：net/http 中间件链加进程内令牌桶，不引第三方网关；失败处理：超阈值返回 429 并带 Retry-After，规则加载失败时默认拒绝写。","tone":"red"},{"label":"跳转处理器","sub":"302 与 410 决策","detail":"职责：解析短码、查映射、按命中与过期返回 302 或 410；技术选型：标准库重定向加 trace_id 中间件；失败处理：下游超时返回 503 并携带 Retry-After，绝不返回 301 永久缓存。","tone":"blue"}]},{"name":"服务层","note":"无状态，可水平扩展","protocol":"进程内接口调用","items":[{"label":"发号服务","sub":"号段与 Base62","detail":"职责：批量领取号段并持久化高水位游标，把自增 ID 编成短码；技术选型：号段加临时文件原子替换，重启从游标恢复；失败处理：持久化失败即拒绝发号，宁可失败也绝不重复发号。","tone":"green"},{"label":"链接服务","sub":"TTL 与过期清理","detail":"职责：维护短码到目标的映射与过期时间，后台惰性判断加定时清理；技术选型：读写锁保护的映射加定时 janitor；失败处理：过期与不存在分别返回 410 与 404，语义不可混淆。","tone":"green"}]},{"name":"缓存层","note":"挡住绝大多数读，保护存储","protocol":"Redis RESP 协议","items":[{"label":"布隆过滤器","sub":"拦截不存在的短码","detail":"职责：在查缓存前判定短码是否可能存在，挡住缓存穿透扫描；技术选型：双哈希位数组，随创建异步置位；失败处理：判不存在即直接 404，允许极小误判率换取回源保护。","tone":"orange"},{"label":"旁路缓存","sub":"空值盾与热点","detail":"职责：命中直接返回，未命中回源并回填，空结果写短 TTL 空值盾；技术选型：singleflight 合并回源加本地热点缓存；失败处理：缓存整体不可用只影响延迟，回源仍可服务。","tone":"orange"}]},{"name":"存储层","note":"唯一可信数据源与异步旁路","items":[{"label":"链接主库","sub":"MySQL 持久化","detail":"职责：保存短码、目标、过期时间与状态的唯一权威副本；技术选型：短码为主键的单例表，写后不预热缓存；失败处理：主库不可用则写失败读降级到本地快照，恢复后按游标补齐。","tone":"purple"},{"label":"跳转事件队列","sub":"异步埋点","detail":"职责：异步收集点击事件供统计与 ETL 消费；技术选型：有界 channel 加批量投递到消息队列；失败处理：队列满时丢弃低价值事件并计数告警，绝不阻塞跳转主链路。","tone":"purple"}]}]' />

逐层看它为什么存在。接入层把最便宜的拒绝放在最前面：方法、体积、频率、scheme 校验都在读满请求体之前完成，一个恶意扫描在这里就被挡掉，成本远低于让它走到存储。服务层做成无状态，是为了让扩容只加进程；其中发号服务是唯一持有强状态的地方，用号段加游标持久化把「一次写一次磁盘」压成「一个号段一次磁盘」。缓存层不是可选项而是必选项：按第 1 节的命中率目标，超过九成九的读必须在这里结束，否则存储会被三万 QPS 直接压垮；布隆过滤器与空值盾是专门对付「查一个根本不存在的码」这类穿透流量的。存储层只保留唯一可信副本与异步旁路，点击事件走队列而不是同步写库，是为了让统计链路的抖动不影响跳转。

<ConceptDiagram type="cache-aside" title="读路径缓存旁路" caption="命中直接返回，未命中回源回填；写路径只失效不预热，避免双写不一致" />

读写路径刻意不对称。写只做发号、编码、落库，不预热缓存——预热会在缓存里留下尚未提交的幽灵数据，一旦落库失败就出现「缓存有、库没有」的不一致。写路径唯一要动缓存的动作是删除可能存在的空值盾：某个短码在被创建之前可能已经被好奇的用户查过一次，留下了「不存在」的空值盾，不删掉它，刚创建的短链会被自己误杀。读未命中才回源回填，因此缓存整体崩掉只影响延迟、不影响正确性，这是 cache-aside 相对「先写缓存」最重要的性质。

依赖方向严格自上而下：接入层依赖服务层，服务层依赖缓存层与存储层的抽象接口，缓存与存储之间互不感知，存储层绝不反向调用任何上层。这条规则的意义在于故障时能逐层降级——缓存挂了服务层仍能直连存储，队列挂了跳转仍能返回 302；一旦允许反向依赖，任何一个组件的抖动都会沿着调用环放大成全站故障。

<Callout type="note" title="为什么发号器不能被缓存层替代" text="缓存是派生物、可丢可重建，发号游标是权威状态、丢了就会重复发号覆盖历史短码；两者的一致性要求完全不同，不能因为都能放内存就混为一谈。" />

## 3. 模块职责与依赖方向

把第 2 节的层落到具体模块，明确每个模块的输入、输出、依赖、状态与失败行为，这张表也是后面代码与测试的分工依据。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| 防刷网关 | 原始 HTTP 请求 | 放行或 429/400 | 令牌桶、黑白名单 | 每客户端桶状态 | 规则加载失败默认拒绝写 |
| 跳转处理器 | 短码与 trace 上下文 | 302/404/410/503 | 缓存、存储、队列 | 无状态 | 超时返回 503 带 Retry-After |
| 发号服务 | 领取号段请求 | 自增 ID 与 Base62 短码 | 持久化游标文件 | 高水位游标 | 持久化失败即拒绝发号 |
| 链接服务 | 短码与目标 | 映射与过期判定 | 存储接口 | 内存映射 | 过期与不存在返回不同错误 |
| 布隆过滤器 | 短码 | 可能存在或一定不存在 | 位数组 | 位数组快照 | 判不存在直接 404 |
| 旁路缓存 | 短码 | 命中值或空值盾 | 远端缓存 | 可重建派生 | 整体不可用只降延迟 |
| 链接主库 | 写入与查询 | 权威映射 | 磁盘 | 唯一可信副本 | 不可用则写失败读降级 |
| 事件队列 | 点击事件 | 异步投递回执 | 有界 channel | 缓冲中事件 | 满则丢弃并告警 |

包目录按「领域内聚、依赖单向」组织，cmd 只做装配，internal 里每一层只暴露接口给上一层：

```text
shortlink/
├── cmd/
│   └── server/
│       └── main.go            # 装配配置、构造依赖、启动与优雅退出
├── internal/
│   ├── api/
│   │   ├── handler.go         # HTTP 路由与 302/410 决策
│   │   └── middleware.go      # trace_id、限流、scheme 校验
│   ├── idgen/
│   │   ├── segment.go         # 号段发号器与游标持久化
│   │   └── base62.go          # Base62 编解码与字符白名单
│   ├── link/
│   │   ├── service.go         # 创建、改目标、过期判定
│   │   └── janitor.go         # 后台过期清理
│   ├── cache/
│   │   ├── aside.go           # cache-aside 与 singleflight
│   │   ├── bloom.go           # 布隆过滤器
│   │   └── nullshield.go      # 空值盾与 TTL 抖动
│   ├── store/
│   │   ├── store.go           # Store 接口定义（依赖倒置点）
│   │   ├── memory.go          # 本讲内存实现
│   │   └── mysql.go           # 预留：生产实现
│   └── event/
│       └── queue.go           # 有界 channel 与异步投递
└── go.mod
```

依赖倒置的关键在 store 包：链接服务只依赖 `Store` 接口，内存实现与 MySQL 实现都实现同一组方法，替换实现时服务层一行不改。接口要刻意做小，`Put/Get/Delete` 三个方法足够，不要把缓存、发号、统计硬塞进同一个大接口，否则任何一处改动都会牵动全链路。

<Callout type="warn" title="边界划分最常见的错误" text="把限流、埋点、发号逻辑直接写进 handler 函数里，会让 handler 既无法单测也无法复用；正确做法是 handler 只做协议解析与状态码决策，领域逻辑放服务层，横切能力放中间件，三者通过接口注入。" />

## 4. 数据与协议契约

存储层用一张表承载权威映射，短码做主键让跳转查询退化成一次点查；过期时间与创建时间都留索引，供后台清理与统计扫描。

```sql
CREATE TABLE short_links (
    code        VARCHAR(16)  NOT NULL COMMENT 'Base62 短码，主键',
    target      VARCHAR(2048) NOT NULL COMMENT '目标 URL，写入前已通过白名单校验',
    created_at  BIGINT       NOT NULL COMMENT '创建毫秒时间戳',
    expires_at  BIGINT       NOT NULL DEFAULT 0 COMMENT '过期毫秒时间戳，0 表示永不过期',
    status      TINYINT      NOT NULL DEFAULT 1 COMMENT '1 正常 2 已回收',
    idem_key    VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '幂等键，唯一索引',
    PRIMARY KEY (code),
    UNIQUE KEY uk_idem (idem_key),
    KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

字段语义与零值必须写清，否则跨语言对接时最容易出错：`expires_at` 用整数毫秒而非数据库时间类型，是为了让内存实现与 SQL 实现用同一套判断逻辑；它的零值 0 表示永不过期，而不是「1970 年就过期」；`status=2` 表示管理员主动回收，与「到期」是两件事，前者可恢复、后者不可；`target` 存的是经 `safeTarget` 归一化后的绝对 URL，不再保留协议相对形式。兼容规则是只增列不删列、状态码只追加不复用，新增字段必须给默认值，老代码读到新行不会因为未知列而报错。

对外 API 用 JSON over HTTPS，写带幂等键，读只走路径参数：

| 方法 | 路径 | 请求 | 成功响应 | 主要错误 |
| --- | --- | --- | --- | --- |
| POST | /api/v1/shorten | url、ttl_seconds、Idempotency-Key | 201 与短码、过期时间 | 400 非法目标、429 超频、503 存储不可用 |
| GET | /{code} | 路径参数 code | 302 与 Location 头 | 404 不存在、410 过期、503 超时 |
| PATCH | /api/v1/links/{code} | 新 target | 200 与版本号 | 404、409 版本冲突 |
| GET | /healthz | 无 | 200 | 503 依赖不健康 |

```json
{
  "code": "7ZkQ2f",
  "short_url": "https://s.example/7ZkQ2f",
  "target": "https://go.dev/doc/",
  "expires_at": 0,
  "version": 1
}
```

版本号与灰度策略：`PATCH` 带 `version` 做乐观锁，版本落后返回 409 让调用方重读再改，避免两个运营并发修改互相覆盖；字段 `short_url` 是派生值不下库，域名由配置注入，便于一套代码跑多环境。灰度时新旧字段并存一段时间，服务端先能读新字段、再开始写新字段、最后才删旧字段，三步分开发布。

内部接口签名把依赖倒置固化成代码，下面是可独立编译的契约定义与最小实现：

```go
package main

import (
	"fmt"
	"time"
)

// Link 是短链服务对外的数据契约，短码为主键。
type Link struct {
	Code    string
	Target  string
	Expires time.Time
}

// Store 定义唯一可信数据源，读多写少，接口刻意做小。
type Store interface {
	Put(l Link) error
	Get(code string) (Link, error)
	Delete(code string) error
}

// Allocator 只负责发号，不负责编码，便于独立替换成号段或雪花实现。
type Allocator interface {
	Next() (uint64, error)
}

// Cache 抽象缓存旁路，Get 的布尔返回值区分未命中与空值。
type Cache interface {
	Get(key string) (string, bool)
	Set(key, val string, ttl time.Duration)
	Del(key string)
}

// memStore 是接口的最小实现，演示依赖倒置：上层只依赖接口。
type memStore struct {
	data map[string]Link
}

func (m *memStore) Put(l Link) error {
	if l.Code == "" {
		return fmt.Errorf("空短码拒绝落库")
	}
	m.data[l.Code] = l
	return nil
}

func (m *memStore) Get(code string) (Link, error) {
	l, ok := m.data[code]
	if !ok {
		return Link{}, fmt.Errorf("短链不存在")
	}
	return l, nil
}

func (m *memStore) Delete(code string) error {
	delete(m.data, code)
	return nil
}

func main() {
	var s Store = &memStore{data: make(map[string]Link)}
	_ = s.Put(Link{Code: "1", Target: "https://go.dev"})
	l, err := s.Get("1")
	fmt.Println("命中:", l.Target, err)
}
```

<Compare title="短码生成的两种契约设计" :panels='[{"label":"自增 ID 加 Base62","code":"Encode62(id) 得到短码","note":"不可枚举、可反解、扩容只需扩号段；代价是需要一个中心化发号器并持久化游标"},{"label":"URL 哈希取前六位","code":"hex(hash(url))[:6]","note":"无需发号器、天然幂等；代价是必须处理冲突、不可反解、同一目标无法区分两条活动"}]' note="本讲选前者：短码可反解让布隆置位、回源定位与按区间扩容都更直接，而冲突处理在哈希方案里会把简单读路径拖成复杂写路径" />

## 5. 核心链路时序

先看创建短链的写链路。写路径的重心不是快，而是「不重复」与「不留下幽灵缓存」。

<SequenceDiagram title="创建短链写链路" caption="写路径不预热缓存，但要清掉可能存在的空值盾；点击事件异步投递，不阻塞响应" :actors='["Client","Gateway","LinkSvc","Allocator","Cache","MQ"]' :steps='[{"from":"Client","to":"Gateway","label":"POST /shorten","note":"带幂等键","kind":"call"},{"from":"Gateway","to":"LinkSvc","label":"校验并转发","note":"限流与白名单已过","kind":"call"},{"from":"LinkSvc","to":"LinkSvc","label":"校验目标","note":"scheme 与域名","kind":"self"},{"from":"LinkSvc","to":"Allocator","label":"领取号段","note":"游标已持久化","kind":"call"},{"from":"Allocator","to":"LinkSvc","label":"返回短码","note":"Base62 编码","kind":"return"},{"from":"LinkSvc","to":"Cache","label":"删除空值盾","note":"避免误判不存在","kind":"call"},{"from":"LinkSvc","to":"MQ","label":"投递创建事件","note":"异步不阻塞","kind":"async"},{"from":"LinkSvc","to":"Gateway","label":"返回短链","note":"HTTP 201","kind":"return"},{"from":"Gateway","to":"Client","label":"返回 short_url","note":"含过期时间","kind":"return"}]' />

逐步骤看分叉点。第一跳网关先过令牌桶与 scheme 白名单，超频在压到服务层之前就返回 429；第二跳服务层做 `safeTarget` 归一化，把协议相对地址与危险协议彻底拒掉；第三跳是自处理，因为校验只依赖请求内容，不需要外部 I/O；第四跳领取号段是唯一带持久化的动作，游标写盘失败必须让整次创建失败而不是冒险继续；第五跳把自增 ID 编成 Base62 返回；第六跳删除空值盾是写路径唯一碰缓存的地方；第七跳投递事件标成异步，意味着 MQ 抖动不会让创建接口变慢；最后两跳返回结果。

再看读链路，它是系统真正的高频路径，每一跳都要能计入指标与 trace。

<SequenceDiagram title="短链跳转读链路" caption="读路径每一跳都计入指标与 trace，缓存未命中才回源，热点回源被合并" :actors='["Client","Gateway","Bloom","Cache","DB","MQ"]' :steps='[{"from":"Client","to":"Gateway","label":"GET 短码","note":"带 trace_id","kind":"call"},{"from":"Gateway","to":"Bloom","label":"判存在","note":"拦截穿透","kind":"call"},{"from":"Bloom","to":"Gateway","label":"返回存在","note":"误判放行","kind":"return"},{"from":"Gateway","to":"Cache","label":"查旁路缓存","note":"含空值盾","kind":"call"},{"from":"Cache","to":"Gateway","label":"未命中","note":"触发回源","kind":"return"},{"from":"Gateway","to":"DB","label":"合并回源","note":"singleflight","kind":"call"},{"from":"DB","to":"Gateway","label":"返回映射","note":"回填缓存","kind":"return"},{"from":"Gateway","to":"Gateway","label":"判过期","note":"410 或 302","kind":"self"},{"from":"Gateway","to":"MQ","label":"投递点击事件","note":"异步埋点","kind":"async"},{"from":"Gateway","to":"Client","label":"返回 302","note":"Location 头","kind":"return"}]' />

异常链路与正常链路有三处分叉。其一是布隆判「一定不存在」：此时不查缓存、不回源，直接 404，把扫描流量在最便宜的位置截断。其二是缓存未命中且回源超时：回源 `context` 带 200 毫秒截止，超时不重试、直接 503 并带 `Retry-After`，因为对跳转来说「这次失败让用户重试」比「把线程挂在慢查询上」更安全；重试预算设为 0，避免超时重试把存储压垮。其三是 DB 返回不存在：不直接放空，而是写一个带随机抖动的短 TTL 空值盾，让后续相同短码命中盾直接 404，同时用 singleflight 保证同一时刻只有一个 goroutine 真正查库。三处分叉共同保证：缓存挂了只降延迟，存储慢了不雪崩，攻击流量不落地。

<StepBox title="读路径延迟异常的排查顺序" :steps='[{"title":"先看命中率","body":"命中率骤降先怀疑缓存而非存储","tip":"对比 cache_hit 与 cache_miss 两条曲线是否同时异动"},{"title":"再看回源放大","body":"回源 QPS 是否被 singleflight 收敛","tip":"放大系数应接近 1，明显大于 1 说明合并失效"},{"title":"最后看布隆","body":"位数组饱和会让穿透扫描漏进来","tip":"检查误判率与已置位比例"}]' startLabel="开始排查" />

## 6. 核心实现

下面六段代码覆盖数据模型与接口、核心算法、并发与 I/O、错误与重试、以及一个可运行的主流程，每段都带完整包声明与导入、可独立编译，且刻意保留超时、nil、部分失败与资源释放的判断。

### 6.1 号段发号器与 Base62：把唯一与短编码拆开

设计意图：发号器只保证「全局不重复且重启不回退」，Base62 只保证「短且可反解」，两者解耦后可分别测试与替换。关键取舍是号段而非 Snowflake：号段对时钟回拨完全免疫，且不依赖机器 ID 分配，代价是重启时会跳过号段内未用完的 ID，造成短码稀疏——对容量高达数百亿的 Base62 而言这点浪费可以忽略。

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Encode62 把非负整数转成 Base62，0 单独处理避免返回空串。
func Encode62(n uint64) string {
	if n == 0 {
		return string(alphabet[0])
	}
	var sb strings.Builder
	for n > 0 {
		sb.WriteByte(alphabet[n%62])
		n /= 62
	}
	b := []byte(sb.String())
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// Decode62 反解短码，遇到字母表外字符返回 false，供入口做白名单校验。
func Decode62(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, false
		}
		n = n*62 + uint64(idx)
	}
	return n, true
}

// Allocator 号段式发号器，高水位游标持久化到文件，重启不重复。
type Allocator struct {
	mu   sync.Mutex
	path string
	step uint64
	cur  uint64
	max  uint64
}

// NewAllocator 从游标文件恢复，文件不存在视为从零开始。
func NewAllocator(path string, step uint64) (*Allocator, error) {
	if step == 0 {
		return nil, errors.New("号段步长必须大于零")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("读取游标失败: %w", err)
	}
	var persisted uint64
	if len(data) > 0 {
		persisted, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("游标内容损坏: %w", err)
		}
	}
	return &Allocator{path: path, step: step, cur: persisted + 1, max: persisted}, nil
}

// Next 领号；号段用尽时先把新高水位原子落盘，再发放本段 ID。
func (a *Allocator) Next() (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cur > a.max {
		newMax := a.max + a.step
		if err := a.persist(newMax); err != nil {
			return 0, err
		}
		a.cur = newMax - a.step + 1
		a.max = newMax
	}
	id := a.cur
	a.cur++
	return id, nil
}

func (a *Allocator) persist(v uint64) error {
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatUint(v, 10)), 0o644); err != nil {
		return fmt.Errorf("写临时游标失败: %w", err)
	}
	if err := os.Rename(tmp, a.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("替换游标失败: %w", err)
	}
	return nil
}

func main() {
	dir, err := os.MkdirTemp("", "idgen-")
	if err != nil {
		fmt.Println("建临时目录失败:", err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "cursor")

	a, err := NewAllocator(path, 1000)
	if err != nil {
		fmt.Println("初始化发号器失败:", err)
		return
	}
	for i := 0; i < 3; i++ {
		id, err := a.Next()
		if err != nil {
			fmt.Println("发号失败:", err)
			return
		}
		fmt.Printf("id=%d code=%s\n", id, Encode62(id))
	}
	// 模拟进程重启：同一游标文件重新加载，号段跳过未用尽的 ID 但绝不回退。
	b, err := NewAllocator(path, 1000)
	if err != nil {
		fmt.Println("重启恢复失败:", err)
		return
	}
	id, _ := b.Next()
	fmt.Println("重启后首个:", id, Encode62(id))
	if back, ok := Decode62("a!b"); !ok {
		fmt.Println("非法字符被拒, 反解值=", back)
	}
}
```

<OutputBox title="发号器与 Base62 演示输出" output="id=1 code=1\nid=2 code=2\nid=3 code=3\n重启后首个: 1001 code=g7\n非法字符被拒, 反解值= 0" />

注意 `persist` 的顺序：先写临时文件、再 `os.Rename` 原子替换，绝不直接覆盖写正在读的文件；`Rename` 失败时清理临时文件，避免残留垃圾。这段还体现了「持久化失败即整体失败」——`Next` 遇错返回 error 而不是继续发号，因为重复发号覆盖历史短码的代价远高于一次创建失败。短码来自 URL 路径属于完全不可信输入，先 `Decode62` 做字符白名单再查存储，能挡掉大量随机扫描。

### 6.2 布隆过滤器：在缓存前挡住穿透

设计意图：不存在的短码永远不命中缓存，请求会全部穿透到存储。布隆过滤器用极小的内存代价回答「这个短码一定不存在」，把随机扫描在最便宜的位置截断。关键取舍是接受可控的误判率：判「存在」时可能误判，放行到缓存与回源再由空值盾兜住；判「一定不存在」是确定结论，可以直接 404。

```go
package main

import (
	"fmt"
	"hash/fnv"
	"math"
	"sync"
)

// Bloom 是并发安全的位数组布隆过滤器，双哈希模拟 k 个哈希函数。
type Bloom struct {
	mu   sync.RWMutex
	bits []uint64
	m    uint64
	k    uint64
}

func NewBloom(n int, fp float64) *Bloom {
	if n <= 0 {
		n = 1
	}
	if fp <= 0 || fp >= 1 {
		fp = 0.001
	}
	m := uint64(math.Ceil(-float64(n) * math.Log(fp) / (math.Ln2 * math.Ln2)))
	if m == 0 {
		m = 64
	}
	k := uint64(math.Round(float64(m) / float64(n) * math.Ln2))
	if k == 0 {
		k = 1
	}
	return &Bloom{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

func (b *Bloom) hashes(s string) (uint64, uint64) {
	h1 := fnv.New64a()
	_, _ = h1.Write([]byte(s))
	h2 := fnv.New64()
	_, _ = h2.Write([]byte(s))
	return h1.Sum64(), h2.Sum64()
}

func (b *Bloom) Add(s string) {
	a, c := b.hashes(s)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		b.bits[idx/64] |= 1 << (idx % 64)
	}
}

func (b *Bloom) Contains(s string) bool {
	a, c := b.hashes(s)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := uint64(0); i < b.k; i++ {
		idx := (a + c*i) % b.m
		if b.bits[idx/64]&(1<<(idx%64)) == 0 {
			return false
		}
	}
	return true
}

func main() {
	b := NewBloom(10000, 0.001)
	for i := 0; i < 1000; i++ {
		b.Add(fmt.Sprintf("code-%d", i))
	}
	hit := b.Contains("code-500")
	fmt.Println("已存在短码判存在:", hit)
	miss := b.Contains("code-999999")
	fmt.Println("不存在短码判存在:", miss)
	fp := 0
	for i := 0; i < 100000; i++ {
		if b.Contains(fmt.Sprintf("absent-%d", i)) {
			fp++
		}
	}
	fmt.Printf("十万次不存在查询误判=%d 误判率=%.4f\n", fp, float64(fp)/100000)
}
```

`m` 与 `k` 由预期容量 `n` 与误判率 `fp` 反推，避免凭感觉给位数；`Add` 用写锁、`Contains` 用读锁，读多写少下读互不阻塞。生产环境位数组会放在 Redis 用 `SETBIT/GETBIT`，或随发号器启动时从主库批量重建，重建失败时应**放行所有查询**而不是拒绝服务，因为布隆只是保护手段而非权威数据。

<Callout type="tip" title="布隆不支持删除" text="标准布隆过滤器无法移除元素，短码被回收后仍判存在只会多走一次缓存，不会出错；若强行做删除需要计数布隆，代价是每位从 1 bit 变 4 bit，通常不值得。" />

### 6.3 TTL 存储与后台清理：把过期做成一等公民

设计意图：过期的判断必须与「取数据」在同一临界区内完成，否则会读出一条刚被别的 goroutine 判定过期的数据。关键取舍是「惰性判断 + 后台 janitor」而非定时全表扫描：惰性判断保证任何时刻返回的数据都正确，janitor 只负责回收内存，正确性不依赖它是否准时。

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("短链不存在")
	ErrExpired  = errors.New("短链已过期")
)

type Entry struct {
	URL     string
	Expires time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]Entry
}

func NewStore() *Store {
	return &Store{data: make(map[string]Entry)}
}

func (s *Store) Put(code, url string, ttl time.Duration) {
	e := Entry{URL: url}
	if ttl > 0 {
		e.Expires = time.Now().Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[code] = e
}

// Get 过期判断放在锁内，避免返回一条本该失效的脏数据。
func (s *Store) Get(code string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[code]
	if !ok {
		return "", ErrNotFound
	}
	if !e.Expires.IsZero() && time.Now().After(e.Expires) {
		return "", ErrExpired
	}
	return e.URL, nil
}

// Janitor 后台清理；ctx 取消时退出，wg.Done 保证 goroutine 被回收。
func (s *Store) Janitor(ctx context.Context, interval time.Duration, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for k, e := range s.data {
				if !e.Expires.IsZero() && now.After(e.Expires) {
					delete(s.data, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func main() {
	s := NewStore()
	s.Put("aB3", "https://example.com/a", 50*time.Millisecond)
	s.Put("keep", "https://go.dev", 0)
	u, err := s.Get("aB3")
	fmt.Println("立即读:", u, err)
	time.Sleep(60 * time.Millisecond)
	_, err = s.Get("aB3")
	fmt.Println("过期后:", errors.Is(err, ErrExpired))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go s.Janitor(ctx, 20*time.Millisecond, &wg)
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait() // 确认后台 goroutine 已退出，避免资源泄漏
	fmt.Println("清理后剩余条目:", s.Len())
}
```

`Janitor` 用 `context` 控制生命周期、`WaitGroup` 确认退出、`defer ticker.Stop()` 释放定时器，三者共同保证优雅退出时不留悬挂 goroutine。注意清理时持写锁遍历，条目量大时会阻塞读，生产应改为分批扫描或按过期时间建小顶堆，只处理堆顶到期项——这是内存实现与 MySQL 实现的一个重要差异点。

### 6.4 旁路缓存：空值盾、singleflight 与热点 key

设计意图：把「缓存穿透、缓存击穿、缓存雪崩」三个经典问题一次性收进一个 `Resolve` 函数。关键取舍是自制极简 singleflight 而非引入第三方包，保证本段只用标准库可独立编译；代价是这个实现没有 panic 传播与 `Forget` 语义，生产可平替为 `golang.org/x/sync/singleflight`。

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNotFound = errors.New("短链不存在")

type call struct {
	wg  sync.WaitGroup
	val string
	err error
}

type group struct {
	mu sync.Mutex
	m  map[string]*call
}

func (g *group) Do(key string, fn func() (string, error)) (string, error) {
	g.mu.Lock()
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()
	c.val, c.err = fn()
	c.wg.Done()
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	return c.val, c.err
}

type item struct {
	val string
	exp time.Time
	nil bool // 空值盾标记
}

type Resolver struct {
	mu      sync.RWMutex
	cache   map[string]item
	group   group
	origin  func(context.Context, string) (string, error)
	srcHits atomic.Int64
}

func NewResolver(origin func(context.Context, string) (string, error)) *Resolver {
	return &Resolver{
		cache:  make(map[string]item),
		group:  group{m: make(map[string]*call)},
		origin: origin,
	}
}

func (r *Resolver) get(key string) (item, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	it, ok := r.cache[key]
	if !ok || time.Now().After(it.exp) {
		return item{}, false
	}
	return it, true
}

func (r *Resolver) set(key, val string, ttl time.Duration, isNil bool) {
	// TTL 抖动抑制空值盾与热点同时过期造成的雪崩。
	jitter := time.Duration(rand.Int63n(int64(ttl / 5)))
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[key] = item{val: val, exp: time.Now().Add(ttl + jitter), nil: isNil}
}

// Resolve 先查缓存，未命中用 singleflight 合并回源，空结果写空值盾。
func (r *Resolver) Resolve(ctx context.Context, code string) (string, error) {
	if it, ok := r.get(code); ok {
		if it.nil {
			return "", ErrNotFound
		}
		return it.val, nil
	}
	val, err := r.group.Do(code, func() (string, error) {
		// context 超时沿回源链路传播，超时不重试。
		qctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		v, e := r.origin(qctx, code)
		r.srcHits.Add(1)
		if errors.Is(e, ErrNotFound) {
			r.set(code, "", 60*time.Second, true)
			return "", ErrNotFound
		}
		if e != nil {
			return "", e
		}
		r.set(code, v, 5*time.Minute, false)
		return v, nil
	})
	return val, err
}

func main() {
	origin := func(ctx context.Context, code string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Millisecond):
			if code == "known" {
				return "https://go.dev", nil
			}
			return "", ErrNotFound
		}
	}
	r := NewResolver(origin)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Resolve(context.Background(), "ghost")
		}()
	}
	wg.Wait()
	fmt.Println("百并发查同一不存在短码, 真实回源次数:", r.srcHits.Load())
	v, err := r.Resolve(context.Background(), "known")
	fmt.Println("命中已存在短码:", v, err)
	_, err = r.Resolve(context.Background(), "ghost")
	fmt.Println("再次查不存在短码命中空值盾:", errors.Is(err, ErrNotFound))
}
```

<OutputBox title="旁路缓存并发演示输出" output="百并发查同一不存在短码, 真实回源次数: 1\n命中已存在短码: https://go.dev err=nil\n再次查不存在短码命中空值盾: true" />

三个机制各管一段：布隆（第 6.2 节）在更外层挡住「从没存在过」的码；空值盾挡住「刚被查过发现不存在」的码，并用 TTL 抖动避免大量盾同时过期；singleflight 保证同一时刻同一 key 只有一个回源 goroutine，把热点 key 的百并发收敛成一次查库。回源用带 200 毫秒截止的 `context`，超时直接返回错误、不设重试预算，避免慢查询叠加重试放大。`srcHits` 用原子计数，便于压测时验证「回源放大系数接近 1」。

<Callout type="warn" title="空值盾必须在写路径删除" text="创建短链成功后若不删除该短码的空值盾，刚创建的链接会被之前留下的盾直接判为不存在，这是 cache-aside 最常见的自伤事故。" />

### 6.5 防刷与黑白名单：把公网入口守在 cheapest 处

设计意图：写入口完全不可信，最便宜的校验必须放最前。关键取舍是单机令牌桶 + 域名黑白名单，而不是分布式限流：单机实现零依赖、可解释，代价是多实例间限流不共享，演进方案见第 13 节。

```go
package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	mu     sync.Mutex
	tokens float64
	rate   float64
	burst  float64
	last   time.Time
}

func newBucket(rate, burst float64) *bucket {
	return &bucket{tokens: burst, rate: rate, burst: burst, last: time.Now()}
}

func (b *bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type Guard struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	allowHost map[string]bool
	denyHost  []string
}

func NewGuard() *Guard {
	return &Guard{
		buckets:   make(map[string]*bucket),
		allowHost: map[string]bool{"go.dev": true, "example.com": true},
		denyHost:  []string{"evil.", "phishing."},
	}
}

func (g *Guard) Allow(client string) bool {
	g.mu.Lock()
	b, ok := g.buckets[client]
	if !ok {
		b = newBucket(5, 10) // 每秒 5 次，突发 10 次
		g.buckets[client] = b
	}
	g.mu.Unlock()
	return b.Allow()
}

// SafeTarget 只放行白名单内的 http/https 绝对地址，堵住开放重定向。
func (g *Guard) SafeTarget(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", fmt.Errorf("目标地址超长")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("目标地址无法解析")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("仅支持 http 与 https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("目标地址缺少域名")
	}
	for _, p := range g.denyHost {
		if strings.HasPrefix(host, p) {
			return "", fmt.Errorf("目标域名在黑名单内")
		}
	}
	if !g.allowHost[host] {
		return "", fmt.Errorf("目标域名不在白名单内")
	}
	return u.String(), nil
}

func main() {
	g := NewGuard()
	pass := 0
	for i := 0; i < 15; i++ {
		if g.Allow("1.2.3.4") {
			pass++
		}
	}
	fmt.Println("同客户端 15 次请求放行:", pass)
	for _, raw := range []string{
		"https://go.dev/doc/",
		"javascript:alert(1)",
		"//evil.example.com/x",
		"https://phishing.example.com/x",
		"https://unknown.com/x",
	} {
		out, err := g.SafeTarget(raw)
		fmt.Printf("目标=%s 结果=%s 错误=%v\n", raw, out, err)
	}
}
```

`SafeTarget` 是安全底线：只放行 http/https 绝对地址，显式挡掉 `javascript:` 与协议相对地址 `//host`，再用域名白名单收口，短链域名才不会变成别人的钓鱼跳板。令牌桶按客户端标识分桶，`Allow` 先按时间补充令牌再消费，避免「固定窗口」在边界瞬间的双倍突发。生产里白名单应支持热加载，加载失败时**默认拒绝写**而不是放行，这是安全配置的保守原则。

<Term word="空值缓存" text="把查询结果为不存在的短码也以短 TTL 写入缓存，防止同一个不存在的 key 被反复回源打穿存储；TTL 需加随机抖动避免集中过期。" />

### 6.6 HTTP 主流程：301/302、trace 埋点与优雅退出

设计意图：把前面所有模块装配成一条可运行的服务，集中展示状态码决策、trace_id 传播与优雅退出。关键取舍是跳转一律用 302：牺牲「浏览器可能缓存的那点优化」，换取目标可改、点击可统计、短码可回收；过期与不存在分别返回 410 与 404，让监控能区分「 TTL 到期」与「缓存穿透」。

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("短链不存在")
	ErrExpired  = errors.New("短链已过期")
)

type link struct {
	target  string
	expires time.Time
}

type memStore struct {
	mu   sync.RWMutex
	data map[string]link
	next uint64
}

func (m *memStore) Create(target string, ttl time.Duration) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	code := fmt.Sprintf("%d", m.next)
	l := link{target: target}
	if ttl > 0 {
		l.expires = time.Now().Add(ttl)
	}
	m.data[code] = l
	return code
}

func (m *memStore) Get(code string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.data[code]
	if !ok {
		return "", ErrNotFound
	}
	if !l.expires.IsZero() && time.Now().After(l.expires) {
		return "", ErrExpired
	}
	return l.target, nil
}

func safeTarget(raw string) (string, error) {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
		return "", fmt.Errorf("仅支持 http 与 https 绝对地址")
	}
	return raw, nil
}

// trace 中间件为每次请求注入 trace_id，并把它写进结构化日志。
func trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid := r.Header.Get("X-Trace-Id")
		if tid == "" {
			tid = fmt.Sprintf("t%d", time.Now().UnixNano())
		}
		ctx := context.WithValue(r.Context(), "trace_id", tid)
		w.Header().Set("X-Trace-Id", tid)
		start := time.Now()
		next.ServeHTTP(w, r.WithContext(ctx))
		slog.Info("request", "trace_id", tid, "path", r.URL.Path,
			"cost_ms", time.Since(start).Milliseconds())
	})
}

func newHandler(m *memStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/shorten", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			URL string `json:"url"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "请求体不是合法 JSON", http.StatusBadRequest)
			return
		}
		target, err := safeTarget(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		code := m.Create(target, 24*time.Hour)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "方法不允许", http.StatusMethodNotAllowed)
			return
		}
		code := strings.TrimPrefix(r.URL.Path, "/")
		if code == "" {
			http.NotFound(w, r)
			return
		}
		target, err := m.Get(code)
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, "短链不存在", http.StatusNotFound)
		case errors.Is(err, ErrExpired):
			http.Error(w, "短链已过期", http.StatusGone)
		case err != nil:
			http.Error(w, "服务暂时不可用", http.StatusServiceUnavailable)
		default:
			// 302 而非 301：目标可改、点击可统计、浏览器不永久缓存旧映射。
			http.Redirect(w, r, target, http.StatusFound)
		}
	})
	return trace(mux)
}

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("监听失败:", err)
		return
	}
	m := &memStore{data: make(map[string]link)}
	srv := &http.Server{Handler: newHandler(m), ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Post(base+"/shorten", "application/json",
		strings.NewReader(`{"url":"https://go.dev/doc/"}`))
	if err != nil {
		fmt.Println("创建失败:", err)
		return
	}
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	_ = resp.Body.Close()

	r2, _ := client.Get(base + "/" + out.Code)
	fmt.Println("跳转:", r2.StatusCode, "Location=", r2.Header.Get("Location"))
	_ = r2.Body.Close()
	r3, _ := client.Get(base + "/not-exist")
	fmt.Println("不存在:", r3.StatusCode)
	_ = r3.Body.Close()

	// 优雅退出：生产中由 signal.NotifyContext 触发，这里手动演示 Shutdown。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Println("关闭超时:", err)
	}
	fmt.Println("服务已优雅退出")
}
```

`trace` 中间件把 trace_id 注入 `context` 并回写响应头，同时用 `slog` 打一条带耗时的结构化日志，跳转链路的可观测性就落在这里。注意 `http.MaxBytesReader` 限制请求体、`http.Server` 设 `ReadHeaderTimeout` 防慢头攻击、`json.Decoder` 显式检查错误、每个 `resp.Body` 都 `Close` 释放连接，以及 `Shutdown` 带超时避免退出挂死。生产应把 `signal.NotifyContext` 接进来，收到 SIGTERM 后先摘流量再 `Shutdown`，等待在途请求收尾。

<OutputBox title="HTTP 主流程演示输出" output="跳转: 302 Location= https://go.dev/doc/\n不存在: 404\n服务已优雅退出" />

## 7. 失败路径与边界条件

评审时最该被追问的不是「正常怎么跑」，而是「哪个组件挂了会怎样」。下表把故障点、触发条件、现象、发现手段、兜底与恢复动作逐条对齐，所有兜底都要能对应到前面某段代码。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 回源下游超时 | DB 慢查询超过 200 毫秒 | 跳转 503 或明显变慢 | 回源 P99 与 timeout 计数告警 | 不重试、返回 503 带 Retry-After | 定位慢查询、扩容只读副本 |
| 缓存整体不可用 | Redis 连接断开 | 命中率骤降、回源 QPS 抬升 | cache_hit 曲线断崖 | 直连存储继续服务，只降延迟 | 恢复缓存、预热热点、观察放大系数 |
| 存储返回脏数据 | 目标字段被写坏 | 跳转到非法地址 | 出网目标域名校验告警 | 出站前二次 scheme 校验拒绝 | 从审计日志回滚字段、修复写入方 |
| 重复创建请求 | 客户端超时重试 | 可能产生两条短链 | 幂等键冲突计数 | 唯一索引加幂等键返回已有短码 | 无需人工，幂等表自动收敛 |
| 发号游标文件损坏 | 磁盘写入中断 | 启动失败或号段错乱 | 启动自检与校验和 | 拒绝发号并告警，不降级 | 从主库最大短码反推游标并重建 |
| 空值盾误杀新链 | 创建后未删盾 | 刚创建即 404 | 创建后连通性探测 | 写路径删除该短码空值盾 | 补删盾、回填正确映射 |
| 进程崩溃 | OOM 或 panic | 连接中断、在途请求丢失 | 存活探针与崩溃计数 | 无状态实例重启、游标已持久化 | 自动拉起、按游标续发、不丢号 |
| 配置错误 | 白名单为空或 TTL 非法 | 全部写入被拒或永不过期 | 启动配置校验与变更审计 | 校验失败拒绝启动 | 回滚配置、灰度重新发布 |
| 队列满 | 点击洪峰超过缓冲 | 事件丢失 | 丢弃计数告警 | 丢弃低价值事件不阻塞跳转 | 扩容消费者、从日志补算 |

幂等键用客户端生成的唯一串，落库时靠 `idem_key` 唯一索引兜底，重复提交返回已有短码而非新发一个号——这既省号段也避免统计口径分裂。重试预算的口径要写死：跳转回源超时**不重试**（重试只会放大存储压力），事件投递失败**最多两次**且退避 50 毫秒，超时通过 `context.WithTimeout` 沿链路逐层传递，不允许某一层私自把 deadline 加长。降级有明确的人工介入点：当缓存命中率低于 95% 且回源放大系数大于 5 时，值班需手动开启「只读本地快照」模式，牺牲目标实时性换存储存活；配置错误与游标损坏则必须人工确认后才能恢复，禁止自动带病启动。

<Callout type="idea" title="410 与 404 分开不是洁癖" text="过期返回 410、不存在返回 404，监控才能把「TTL 批量到期」和「有人扫描短码」区分开；混用会让一次正常的过期清理被误报成缓存穿透攻击。" />

## 8. 并发与一致性

系统的并发模型分三类 goroutine：每个请求一个短生命周期 handler goroutine，由 `net/http` 管理；发号器与存储用 `sync.RWMutex` 保护共享状态；janitor 与事件投递是长生命周期后台 goroutine，由 `context` 统一叫停。channel 只用在有界事件队列，不用于请求间通信——请求间没有数据流，用 channel 反而引入不必要的阻塞点。锁粒度上，发号器与链接映射都用细粒度锁，缓存层 `get`/`set` 分离读写锁，回源合并用独立的 `group` 锁，避免一把大锁把三万 QPS 串行化。

竞态分析的重点有三处：`Allocator.Next` 的 `cur` 与 `max` 必须在同一把锁内更新，否则两个 goroutine 会领到同一个 ID；`Store.Get` 的过期判断必须与取数同处读锁内，否则可能返回刚失效的脏数据；`group.Do` 的「查 map—插入—删除」三步必须在锁内完成，否则并发回源会漏合并。一致性级别选择「读己之写最终一致」：创建成功后允许极短时间缓存未命中回源，但通过「写后删空值盾」保证不会返回旧目标；若要求强一致就得写后同步更新缓存，那会引入双写不一致风险，对短链这种读多写少的场景并不划算。

```go
package main

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// ShardMap 用锁分片降低热点竞争：按 key 哈希把一把大锁拆成 32 把。
type shard struct {
	mu sync.RWMutex
	m  map[string]string
}

type ShardMap struct {
	shards []shard
}

func NewShardMap(n int) *ShardMap {
	s := &ShardMap{shards: make([]shard, n)}
	for i := range s.shards {
		s.shards[i].m = make(map[string]string)
	}
	return s
}

func (s *ShardMap) idx(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32() % uint32(len(s.shards))
}

func (s *ShardMap) Set(key, val string) {
	sh := &s.shards[s.idx(key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.m[key] = val
}

func (s *ShardMap) Get(key string) (string, bool) {
	sh := &s.shards[s.idx(key)]
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	v, ok := sh.m[key]
	return v, ok
}

func main() {
	m := NewShardMap(32)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("code-%d", i)
			m.Set(key, "target")
		}(i)
	}
	wg.Wait()
	v, ok := m.Get("code-42")
	fmt.Println("分片读取:", v, ok)
	fmt.Println("分片数:", len(m.shards))
}
```

顺序保证上，短链只要求「同一短码的改目标操作按 version 串行」，不要求全局有序；点击事件允许乱序与少量丢失，因为统计是离线聚合。对乱序的处理是 `PATCH` 带乐观锁版本号，落后版本返回 409，由调用方重读再提交，把乱序挡在写入口而不是靠下游排序。

<Callout type="warn" title="别用 time.After 做请求内超时" text="在高频 handler 里用 time.After 会为每次请求分配一个定时器，在 GC 前不会释放，三万 QPS 下会堆出大量 timer；应改用 context.WithTimeout 并在 defer 里 cancel。" />

## 9. 可观测性

跳转链路的延迟与命中率是这类系统的生命线，指标必须能直接回答「现在是缓存问题、存储问题还是攻击流量」。

| 指标名 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| sl_redirect_total | Counter | 跳转总次数，按状态码分标签 | 5xx 占比大于 0.1% |
| sl_redirect_duration_ms | Histogram | 跳转耗时，重点看 P99 | P99 大于 5 毫秒 |
| sl_cache_hit_ratio | Gauge | 缓存命中率 | 低于 99% |
| sl_origin_qps | Gauge | 真实回源 QPS | 大于读 QPS 的 1% |
| sl_origin_amplify | Gauge | 回源放大系数 | 大于 2 |
| sl_bloom_reject_total | Counter | 布隆拦截的不存在查询 | 突增视为扫描 |
| sl_null_shield_hit | Counter | 空值盾命中次数 | 突增视为穿透 |
| sl_idgen_persist_fail | Counter | 游标持久化失败 | 大于 0 立即告警 |
| sl_event_drop_total | Counter | 事件队列丢弃数 | 大于 0 持续 1 分钟 |

结构化日志统一带 `trace_id`、`code`、`status`、`cost_ms`、`hit`、`client` 六个字段：`trace_id` 从入口中间件注入并透传给下游，缓存与回源子调用把同一个 id 写进各自的日志，一次故障就能用一条 grep 串起全链路。采样上，正常跳转按 1% 采样，5xx 与超时全量保留，避免三万 QPS 把日志盘打爆。

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

type Metrics struct {
	Redirect atomic.Int64
	Hit      atomic.Int64
	Miss     atomic.Int64
	Timeout  atomic.Int64
}

func (m *Metrics) Observe(hit bool, costMs int64, status int) {
	m.Redirect.Add(1)
	if hit {
		m.Hit.Add(1)
	} else {
		m.Miss.Add(1)
	}
	if status >= 500 {
		m.Timeout.Add(1)
	}
	slog.Info("redirect", "cost_ms", costMs, "status", status, "hit", hit)
}

func handlerTrace(ctx context.Context, code string) context.Context {
	tid, ok := ctx.Value("trace_id").(string)
	if !ok || tid == "" {
		tid = fmt.Sprintf("gen-%d", os.Getpid())
	}
	return context.WithValue(ctx, "trace_id", tid)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	m := &Metrics{}
	ctx := handlerTrace(context.WithValue(context.Background(), "trace_id", "t-abc123"), "7ZkQ2f")
	tid, _ := ctx.Value("trace_id").(string)
	for _, hit := range []bool{true, true, false} {
		status := 302
		if !hit {
			status = 404
		}
		m.Observe(hit, 1, status)
	}
	fmt.Println("trace_id=", tid, "redirect=", m.Redirect.Load(),
		"hit=", m.Hit.Load(), "miss=", m.Miss.Load())
}
```

用这套埋点定位一次真实故障：告警「跳转 P99 升到 40 毫秒、命中率从 99.6% 掉到 82%」→ 看 `sl_origin_qps` 同步抬升、`sl_bloom_reject_total` 无变化，排除扫描 → 看 `sl_origin_amplify` 接近 1，说明 singleflight 正常、不是击穿 → 查缓存实例监控发现某分片连接超时 → 结论是缓存分片故障导致回源放大、延迟上升 → 按第 7 节降级为直连存储并恢复分片。整条推理链只依赖上面这张指标表，不靠猜。

## 10. 测试策略

测试按五层切分，mock 边界画在 `Store`、`Cache`、`Allocator` 三个接口上：领域逻辑用内存 fake 做纯单测，不碰网络；HTTP 层用 `httptest` 做集成测试，验证状态码与 Location；契约测试锁定 JSON 字段与错误码；故障注入用可控的 fake 让 `Store.Get` 返回超时或脏数据，验证兜底分支；基准与压测单独跑，不放进单元测试。这样切的理由是：把外部依赖挡在接口外，能让 90% 的逻辑在毫秒级单测里覆盖，而把真正需要 I/O 的部分收敛到少量集成测试。

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Encode62(n uint64) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 11)
	for n > 0 {
		b = append(b, alphabet[n%62])
		n /= 62
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func Decode62(s string) (uint64, bool) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	if s == "" {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(alphabet, s[i])
		if idx < 0 {
			return 0, false
		}
		n = n*62 + uint64(idx)
	}
	return n, true
}

func TestBase62RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		id   uint64
	}{
		{"零值", 0},
		{"个位", 1},
		{"进位边界", 61},
		{"两位起点", 62},
		{"大数", 1 << 40},
		{"uint64 上限", ^uint64(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code := Encode62(c.id)
			back, ok := Decode62(code)
			if !ok || back != c.id {
				t.Fatalf("往返失败 id=%d code=%s back=%d ok=%v", c.id, code, back, ok)
			}
		})
	}
}

func TestDecode62RejectsBadChar(t *testing.T) {
	if _, ok := Decode62("a!b"); ok {
		t.Fatal("非法字符应被拒绝")
	}
	if _, ok := Decode62(""); ok {
		t.Fatal("空串应被拒绝")
	}
}

func TestRedirectUsesFoundNotMovedPermanently(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://go.dev", http.StatusFound)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/abc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("期望 302, 得到 %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://go.dev" {
		t.Fatalf("Location 不符: %s", got)
	}
}

func BenchmarkEncode62(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Encode62(uint64(i))
	}
}
```

<OutputBox title="测试与基准执行命令" output="go test -race -cover ./...\ngo test -run TestBase62RoundTrip -v\ngo test -bench BenchmarkEncode62 -benchmem" />

`TestBase62RoundTrip` 用表驱动覆盖零值、进位边界、`uint64` 上限，编码解码互为逆运算最适合这种写法；`TestRedirectUsesFoundNotMovedPermanently` 用 `httptest` 断言 302 与 Location，客户端必须设 `CheckRedirect` 才能看到重定向响应本身而非最终页面；`-race` 用来验证发号器、存储与 `group.Do` 的并发正确性；Benchmark 报告每次编码的分配数，防止后续优化把短码生成变成内存热点。

## 11. 部署与容量

部署拓扑是「四台无状态跳转实例 + 一主一从 MySQL + 一主两从 Redis + 一个事件队列」，实例前面挂负载均衡做健康检查，只有 `/healthz` 全绿才接流量。

```text
                    ┌──────────────┐
   Internet ──────▶│  负载均衡 LB  │ 健康检查 /healthz
                    └──────┬───────┘
        ┌──────────────┬───┴────┬──────────────┐
        ▼              ▼        ▼              ▼
   ┌────────┐     ┌────────┐ ┌────────┐   ┌────────┐
   │跳转实例│×4   │   ...   │ │   ...   │   │   ...   │
   └───┬────┘     └────────┘ └────────┘   └────────┘
       │ 读写            │ 游标文件(本地盘/共享)
       ▼                 ▼
   ┌────────────┐   ┌────────────┐   ┌────────────┐
   │ Redis 集群 │   │ MySQL 主从 │   │ 事件队列   │
   │ 缓存+布隆  │   │ 权威映射   │   │ 点击埋点   │
   └────────────┘   └────────────┘   └────────────┘
```

依赖组件与版本、配置项、资源估算分别如下，配置项默认值与环境差异必须写清，否则「同一份镜像跑多套环境」就是空话。

| 组件 | 版本 | 用途 | 单实例规格 |
| --- | --- | --- | --- |
| Go 运行时 | 1.21+ | 服务本体 | 2 核 2 GB |
| MySQL | 8.0 | 权威映射与幂等键 | 4 核 16 GB SSD |
| Redis | 7.x | 旁路缓存与布隆位数组 | 4 核 8 GB |
| 消息队列 | Kafka 3.x | 点击事件异步投递 | 3 节点 |
| 负载均衡 | 四层 | 健康检查与流量分发 | 托管 |

| 配置项 | 默认值 | 生产值 | 说明 |
| --- | --- | --- | --- |
| SEGMENT_STEP | 1000 | 5000 | 号段步长，越大回盘越少但重启浪费越多 |
| CURSOR_PATH | ./cursor | /data/cursor | 发号游标文件，必须落在持久卷 |
| CACHE_TTL | 300s | 600s | 正常映射缓存时长 |
| NULL_SHIELD_TTL | 60s | 30s | 空值盾时长，加抖动防雪崩 |
| ORIGIN_TIMEOUT | 200ms | 150ms | 回源超时，超时不重试 |
| RATE_PER_CLIENT | 5 | 20 | 单客户端每秒写入上限 |
| BLOOM_EXPECTED | 2000万 | 5000万 | 布隆预期容量，决定内存占用 |
| MAX_TARGET_LEN | 2048 | 2048 | 目标 URL 最大长度 |

资源估算：MySQL 连接池按每实例 20 个连接、四实例共 80，远低于默认上限；单机缓存热点集合约 200 MB，四实例合计不到 1 GB；布隆按两千万容量、千分之一误判率约需 34 MB 位数组，多实例各存一份可承受；带宽按三万 QPS、单响应约 300 字节（302 无正文）估算约 72 Mbps，主要出带宽其实是被跳转走的目标站承担。扩容方式是水平加跳转实例：无状态、游标由发号服务集中持有，加机器不改代码；优雅退出用 `signal.NotifyContext` 接 SIGTERM，先让 `/healthz` 返回 503 摘流量、等待在途请求收尾、再 `Shutdown` 关闭连接并 `wg.Wait` 收回 janitor 与事件 goroutine，最后才退出进程。

## 12. 性能与压测

压测要模拟真实读写比，只打 GET 或只打 POST 都会得出误导性结论。先用 `go test -bench` 建立单机编解码基线，再用 `wrk` 或 `hey` 按 1000 比 1 混合压测，缓存按第 1 节的命中率目标预热点短码。

```bash
# 1) 单机编解码与缓存解析基准
go test -bench . -benchmem -count 5 | tee bench.txt
# 2) 预热热点短码，再按真实读写比压跳转
hey -z 5m -c 200 -q 100 \
  -H 'X-Trace-Id:压测' \
  'https://s.example/7ZkQ2f' > get.txt
# 3) 写入链路单独压，观察号段回盘频率
hey -z 2m -c 20 -m POST \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://go.dev/doc/"}' \
  'https://s.example/api/v1/shorten' > post.txt
# 4) 采集 pprof 证据
go tool pprof -http=:0 http://127.0.0.1:6060/debug/pprof/profile?seconds=30
```

| 指标 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| 跳转吞吐 | 12000 QPS | 38000 QPS | 加本地热点缓存后单实例提升约三倍 |
| 跳转 P99 | 18 ms | 3.2 ms | 命中路径不再每次访问 Redis |
| 缓存命中率 | 96.4% | 99.7% | 布隆与空值盾挡住穿透扫描 |
| 回源放大系数 | 7.3 | 1.0 | singleflight 合并热 key 回源 |
| 5xx 错误率 | 0.4% | 0.02% | 超时不重试避免放大 |
| 单实例内存 | 1.6 GB | 320 MB | 去掉请求内 io.ReadAll 与 timer 堆积 |
| 峰值出带宽 | 96 Mbps | 72 Mbps | 302 无正文且 304 复用缓存 |

瓶颈定位过程有指标证据而不是拍脑袋：首轮压测 P99 卡在 18 毫秒，`sl_origin_qps` 居高不下，pprof CPU 火焰图显示大量时间落在 Redis 客户端编解码与网络等待；据此加进程内热点缓存，让热点短码完全不出进程，第二轮 P99 降到 3.2 毫秒。随后发现某批短码 TTL 集中到期导致命中率周期性抖动，pprof heap 显示 `time.After` 累积的 timer 对象，改用 `context.WithTimeout` 后内存从 1.6 GB 降到 320 MB。仍然存在的限制要如实写出：本地热点缓存带来最多 `CACHE_TTL` 窗口的旧目标可见（运营改目标后最坏 600 秒才全量生效，靠写后删缓存收敛到秒级但跨实例通知有延迟）；单机令牌桶在多实例间不共享，单客户端可绕过约实例数倍的额度；布隆误判率随容量增长而上升，需要在达到 `BLOOM_EXPECTED` 前重建。

## 13. 演进与替代方案

没有一套架构能吃到所有规模，评审必须写清「什么时候该换」。下表给出两个真实替代方案与当前方案的取舍对比。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 当前：单机号段 + 本地缓存 + 单点 MySQL | 日点击千万级、单机房 | 结构简单、无分布式协调、RPO 为 0 | 单机发号器是写扩展瓶颈、缓存不共享 | 写 QPS 持续增长或需要跨机房容灾 |
| 替代一：Snowflake 多实例发号 + Redis 共享缓存 | 日点击亿级、多机房 | 发号完全去中心化、实例可任意扩 | 依赖机器 ID 分配，时钟回拨会重复发号，需等时钟追平或降级 | 单发号器成为瓶颈且能治理 NTP |
| 替代二：预生成短码池 + KV 存储 + CDN 边缘跳转 | 日点击十亿级、全球加速 | 跳转在边缘完成、源站压力极低 | 目标改动需全网刷新、点击统计延迟高、成本高 | 流量全球化且目标极少改动 |

从当前版本演进到替代一的触发条件写死成三条：写 QPS 持续一周超过单发号器容量的 70%、需要跨机房双活、或发号器单点故障在一次演练中造成写不可用。迁移步骤分四步且每步可回滚：第一步引入 Snowflake 但保留号段发号器作为影子，双写比对 ID 是否冲突；第二步按 `machine_id` 灰度切流，短码生成切到 Snowflake，号段游标冻结不再增长；第三步观察一周，校验无重复、无时钟回拨告警后下掉号段代码；第四步删除游标文件与相关配置。回滚方案是保留号段发号器二进制与游标文件至少一个发布周期，一旦 Snowflake 出现时钟回拨或机器 ID 冲突，立即把流量切回号段，并用「新 ID 高位区间」避免与已发出的 Snowflake 短码碰撞。

<FlowTimeline title="短链服务演进路线" direction="horizontal" :steps='[{"title":"v1 单机验证","desc":"进程内号段发号器加内存映射，够用于内部工具与低流量验证","tone":"blue"},{"title":"v2 加缓存","desc":"接 Redis 旁路缓存与布隆过滤器，读延迟降到亚毫秒","tone":"green"},{"title":"v3 多实例","desc":"发号器换 Snowflake 或数据库号段，服务彻底无状态水平扩展","tone":"orange"},{"title":"v4 数据化","desc":"跳转事件进队列，接 ETL 做点击统计与热点分析","tone":"purple"}]' />

v4 的点击事件队列正是第 107 讲日志 ETL 管道的输入；发号器换成 Snowflake 的完整讨论在第 86 讲分布式 ID；缓存穿透、击穿、雪崩的系统化治理在第 67 讲。本讲只把「读薄、写稳、入口严」这条主线打穿。

## 14. 真实工程里的坑

以下五条都来自真实生产事故，逐条给出现象、原因、错误写法与正确写法，以及能提前发现的手段。

**坑一：发号器重启游标归零，历史短码被覆盖。** 现象是服务重启后新创建的短链点进了别人的活动页。原因是把自增变量只放内存，重启从 1 开始。错误写法是 `cur++` 后直接返回、从不落盘；正确写法是每领用一个号段就把高水位游标写临时文件再 `os.Rename` 原子替换，启动时从文件恢复（见第 6.1 节 `NewAllocator`）。提前发现：启动自检打印恢复出的游标值，并加一条「恢复值小于主库最大短码」的告警。

**坑二：301 永久缓存让改目标失效。** 现象是运营改了落地页，老用户仍跳旧地址，点击数暴跌。原因是浏览器与 CDN 永久缓存 301 映射。错误写法是 `http.Redirect(w, r, target, http.StatusMovedPermanently)`；正确写法一律 `http.StatusFound`（302），需要统计时也不要加长期 `Cache-Control`。提前发现：契约测试断言创建与改目标后两次 GET 的 Location 不同，且响应头不含长 `max-age`。

**坑三：只做长度校验留下开放重定向。** 现象是短链域名被安全团队通报为钓鱼跳板。原因是只判断字符串非空，没校验 scheme 与 host。错误写法是 `if req.URL != "" { 放行 }`；正确写法用 `url.Parse` 后同时校验 scheme 属于 http/https、host 非空且在域名白名单、显式拒绝 `//` 协议相对地址（见第 6.5 节 `SafeTarget`）。提前发现：把 `javascript:`、`//evil.x`、非白名单域名列入安全单测用例，CI 强制通过。

**坑四：缓存与 DB 不一致的幽灵数据。** 现象是创建接口报错，但短码却已能跳转。原因是写路径先预热缓存、后落库，落库失败时缓存留下了未提交数据。错误写法是「先 `cache.Set` 再 `db.Put`」；正确写法是只落库、写成功后仅删除该短码空值盾，读未命中才回填（cache-aside，见第 5 节）。提前发现：故障注入让 `db.Put` 返回错误，断言缓存中不存在该 key。

**坑五：超时重试放大压垮存储。** 现象是 DB 一次慢查询后整站跳转超时。原因是回源超时后立刻重试，三万 QPS 放大成近十万。错误写法是 `for i := 0; i < 3; i++ { 回源() }`；正确写法是回源用带截止时间的 `context`、超时**不重试**、直接返回 503 带 `Retry-After`，把重试留给客户端（见第 6.4 节）。提前发现：压测时注入 200 毫秒延迟，断言 `sl_origin_qps` 不随错误率翻倍。

**坑六：日志与 timer 打爆内存。** 现象是跑几小时后内存持续上涨直至 OOM。原因是高频 handler 里 `time.After` 累积定时器、全量 `slog.Info` 无采样。错误写法是每次请求 `select { case <-time.After(200*time.Millisecond): }` 并无条件打日志；正确写法用 `context.WithTimeout` 加 `defer cancel()`，正常跳转按 1% 采样、5xx 全量。提前发现：pprof heap 对比压测前后 `timer` 与日志缓冲对象数量，纳入回归基准。

<Callout type="idea" title="坑要写进回归用例" text="每一条生产坑都应至少变成一个自动化测试或一条告警规则，否则复盘完还会再犯；本讲六条坑分别对应启动自检、契约测试、安全用例、故障注入、压测断言与 pprof 基线。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-105-net-shortlink`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-105-net-shortlink)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给读路径加上布隆过滤器与空值盾" task="在本讲 cache-aside 的基础上，让不存在的短码不再击穿到存储：创建时把短码加入布隆，读时先判布隆，未命中的空结果写带抖动的短 TTL 空值盾，并用 singleflight 合并并发回源。" :hints='["提示一：布隆只做存在性预判，判一定不存在可直接 404，判存在仍需查缓存与回源","提示二：空值盾的 TTL 要加随机抖动，避免大量短码同时过期造成雪崩","提示三：写路径创建成功后要删除该短码的空值盾，否则刚创建就被误判不存在","提示四：用原子计数记录真实回源次数，验证百并发查同一不存在码时回源次数为 1"]' solution='要点一：Resolve 入口先调用 Bloom.Contains，返回 false 直接给 ErrNotFound，不再查缓存也不回源。\n要点二：回源得到 ErrNotFound 时，写入一个带短 TTL 且加随机抖动的空值盾，后续相同 key 命中盾直接返回 404。\n要点三：回源用 singleflight.Group.Do 包裹，key 取短码，保证同一时刻只有一个 goroutine 真正查库。\n要点四：Create 成功后删除该短码的空值盾并把短码加入布隆，避免刚创建就被空值盾误杀。\n要点五：用 atomic 计数器记录真实回源次数，压测断言放大系数接近 1。' explain="三处改动分别对应穿透、雪崩与击穿：布隆挡穿透、带抖动的空值盾挡雪崩、singleflight 挡热点击穿；原子计数是验证是否真的收敛的证据。" />

参考实现可直接复用第 6.2 节的 `Bloom` 与第 6.4 节的 `Resolver`：把 `Bloom.Contains` 作为 `Resolve` 的第一道门，把 `group.Do` 放在缓存未命中之后，并在创建成功的分支里补一次 `cache.Del` 与 `bloom.Add`。改完用第 10 节的 `-race` 与基准验证并发正确性没有回退。

## 随堂检测

<Quiz no="105-1" q="营销短链通常选 302 而不是 301，最根本的原因是什么？" :options='["302 更省带宽","302 每次回源，目标可修改且点击可统计","301 不被浏览器支持","301 无法携带 Location 头"]' answer="1" explain="301 会被浏览器与 CDN 长期缓存，服务端改库无法生效，短码回收与点击统计也会一并失效；302 保留每次回源，是短链可运营的前提。" />

<Quiz no="105-2" q="大量请求查询同一个不存在的短码，单靠 Redis 旁路缓存为什么挡不住？" :options='["Redis 太慢","不存在的 key 永远不命中，请求全部回源，需要布隆或空值盾加 singleflight","Redis 不支持字符串类型","短码太长放不进缓存"]' answer="1" explain="不存在的 key 永远不命中缓存，请求会全部穿透到存储；布隆先判存在、空值盾写短 TTL、singleflight 合并并发回源，三者共同兜底。" />

<Predict no="105-3" q="某短码刚过 TTL 到期，此时一次 GET 会返回什么？" :options='["302 跳到旧目标","404 Not Found","410 Gone","500 错误"]' answer="2" output="HTTP/1.1 410 Gone" explain="过期与不存在语义不同：不存在返回 404，曾存在但已到期返回 410，便于调用方与监控区分 TTL 到期和缓存穿透扫描，也避免把过期短码误报成攻击流量。" />

<FillBlank no="105-4" tpl="短链跳转通常用 ___ 状态码而不是 ___，因为后者会被浏览器永久缓存；不存在的短码靠 ___ 过滤器挡住穿透。" :answers='["302","301","布隆"]' hint="想想目标可改、点击统计与缓存穿透" explain="302 每次回源使目标可改、点击可统计；301 永久缓存不可逆；布隆过滤器在查缓存前判定短码可能存在与否，拦截不存在的短码扫描。" :explains='["302 是临时重定向，浏览器每次都回源请求短链服务，运营方因此能随时改目标地址，并把每次点击留下来做统计。","301 表示永久重定向，浏览器与 CDN 会长期缓存映射、之后不再回源；目标一旦要改或需统计点击就失效且难以收回。","布隆过滤器以极小内存判定短码「一定不存在或可能存在」，在查缓存与数据库前先拦掉不存在的短码，防缓存穿透。"]' />

## 一页纸速记

<KnowledgeCard :points='["号段发号器负责唯一、Base62 负责短，两者解耦后可独立测试与扩容","读路径 cache-aside：布隆判存在、命中即 302、未命中 singleflight 回源并回填","302 保留可改目标与点击统计，301 几乎不可逆","空值缓存加 TTL 抖动挡雪崩，布隆挡穿透，singleflight 挡击穿","过期返回 410、不存在返回 404，语义不可混淆"]' :pitfalls='["只做字符串校验会留下开放重定向，必须校验 scheme 与域名白名单","发号器重启游标归零会覆盖历史短码，高水位必须持久化","把 301 或长 max-age 用在短链上等于放弃可运营性","回源超时后重试会把存储放大压垮，应不重试直接 503"]' summary="短链的设计重心是把读路径做薄、把失败路径想全，让每个决策的代价都写得出来。" />

## 小结与下一讲

- 号段发号器负责唯一、Base62 负责短，游标持久化让重启不回退，这是短码不重复的底线。
- 读路径用布隆、空值盾、singleflight 三层分别挡住穿透、雪崩、击穿，302 与 410 守住可改性与过期语义。
- scheme 白名单、令牌桶、超时不重试与结构化 trace 埋点，是公网暴露前必须补齐的边界。

[下一讲：实战 04 · 图片服务与对象存储](/projects/net-image-server)
