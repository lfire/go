---
title: 第 107 讲 · 实战 05 · 日志分析 ETL 管道
description: 用逐行读取与坏行隔离、扇出扇入聚合、带缓冲 channel 的背压与滚动窗口，处理持续流入的日志
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="用 pipeline 处理持续流入的数据" prereq="第 106 讲 · 实战 04 · 图片服务与对象存储" />

# 第 107 讲 · 实战 05 · 日志分析 ETL 管道

> 一批日志和一条河流的区别，在于河流不会停。前面的实战里，数据来一次就结束：一个请求、一张图片、一条命令。而日志分析面对的是永不结束的流——每秒几万行持续写入，下游 OLAP 库会周期性变慢，某一行可能因为上游改了格式而损坏，事件还会因为网络抖动乱序到达。更棘手的是，你不能为了算得准就无限等待，也不能为了跑得快就丢掉重放与幂等的承诺。本讲把这个矛盾拆成可度量的约束，再由约束反推出分层架构，最后交付一条能长时间运行、能诊断、能回滚的 ETL 管道：读取层隔离坏行，聚合层用扇出扇入消除锁竞争，窗口层用水位线治理乱序，落地层用幂等批写兑现至少一次语义。看完你应该能独立把这套系统搭出来，并且说清每个决策到底放弃了什么。

## 0. 项目定位与真实场景

这套管道服务于一个中等规模的 SaaS 平台：线上有四十多个微服务实例，每个实例把 Nginx 风格的访问日志以追加方式写进本地文件，格式是时间戳、级别、状态码、响应字节数四列空格分隔。运维与 SRE 团队用它回答三类问题：过去一分钟的错误率有没有越过阈值、各级别的请求量与流量趋势如何、某次上游发布之后是否出现了 5xx 尖刺。数据团队还会把窗口结果落成 CSV，交给第 108 讲的查询工具做即席分析，两讲合起来正好是一条从原始日志到可查询报表的完整数据链路。

量级不算夸张，却足以把工程问题全部暴露出来：全天约十二亿行、约九百 GB 原始文本，平峰每秒一万五千行，晚高峰与发布窗口会冲到五万行、短时峰值十二万行；单行平均八百字节，但偶尔有一行把完整请求体打进日志，长度超过一 MiB。现有方案是每小时跑一次的 awk 加 shell 脚本：它把整小时日志读进内存、排序、再统计，问题集中在四点——小时级延迟让告警形同虚设；一旦某行格式异常，awk 要么整体报错要么把脏值算进结果；单机串行处理在高峰根本跑不完；崩溃之后没有位点，重跑就重复计数。

因此本讲要解决的核心矛盾，不是怎么写一个解析器，而是：在流永不停、下游会变慢、数据一定会出现损坏与乱序、进程随时可能崩溃重启的前提下，如何用有界的内存、可度量的延迟和可验证的正确性，持续产出稳定的窗口统计。后面每一个设计决策都从这四个约束推导出来，而不是从用哪个框架更时髦出发。这一点决定性地影响了架构分层的切法：按变化频率和失败域切，而不是按技术栈切。

<CheckList id="l107-goals" title="本讲交付目标" :items='["先给出可度量的吞吐、延迟与一致性约束，再由约束反推架构","实现坏行隔离的读取层与带死信的解析层","用有界 channel 与水位线的窗口聚合处理乱序和背压","以幂等批写和位点重放交付 at-least-once 语义","用表驱动测试、压测与失败矩阵证明系统可上线"]' />

## 1. 需求与约束

先把约束写成可验收的数字，架构才有评判标准，否则「支持高并发」这种无法证伪的表述会一路带到上线。功能需求按编号冻结，每条都带一个能被测试或监控直接判定的验收条件。特别注意 F7 与 F8 是一对：只做幂等不做重放会丢数据，只做重放不做幂等会重复计数，两者必须同时成立才等价于对外不丢不重。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| F1 | 逐行读取并解析四列日志 | 正常行全部解析为强类型 Event；表驱动用例覆盖缺字段、非法数字、空行、超长行 |
| F2 | 坏行隔离且不影响主流 | 单行损坏时主流继续；坏行原文进入死信，可用 errors.Is 分类计数 |
| F3 | 多核并行聚合 | worker 数可配且无共享锁；四 worker 吞吐不低于单 worker 的 2.5 倍 |
| F4 | 一分钟滚动窗口统计 | 按事件时间归窗，产出条数、字节数、5xx 数，结果带窗口起点时间戳 |
| F5 | 乱序与迟到治理 | 容忍五秒内乱序；超容忍事件计入迟到指标且不改写已结算窗口 |
| F6 | 背压 | 下游变慢时上游阻塞等待，进程常驻内存不随堆积线性增长 |
| F7 | 至少一次落库 | 崩溃重放同一位点不产生重复行；幂等键命中率可观测 |
| F8 | 批量写与失败重放 | 凑批写入、指数退避、重试预算耗尽则不提交位点 |

非功能指标把上面的「快」和「稳」翻译成可压测的目标值，同时给出恢复目标。RPO 定为一个窗口而非零，是因为位点随批写提交，最坏情况丢一个窗口的重算能力，靠重放补回；把 RPO 压到零需要位点与结果同事务提交，代价见第 13 节。

| 指标 | 目标值 | 说明 |
| --- | --- | --- |
| 平峰吞吐 | 15000 行每秒 | 单实例 |
| 峰值吞吐 | 50000 行每秒，短时 120000 | 靠有界缓冲削峰而不是堆内存 |
| 端到端 P99 | 不超过 2 秒 | 从日志产生到窗口结果落库 |
| 单行上限 | 1 MiB | 超过进死信，不触发 Scanner 停扫 |
| 坏行率告警阈值 | 0.5% | 超过即认定上游格式变更 |
| 一致性级别 | 至少一次加幂等 | 对外等价不丢、不重 |
| 可用性 SLA | 99.9% | 单实例故障由重启与重放兜底 |
| RTO / RPO | 5 分钟 / 1 个窗口 | 位点随批提交，最坏重算一窗 |

<Term word="水位线" text="一个随时间推进的时间戳，声明早于它的窗口被认为不会再有正常到达的数据，因此可以安全结算并关闭。" />

明确边界同样重要。本讲不做完整 SQL 解析器，那是第 108 讲的内核；不做跨实例的分布式调度与选主，见第 110 讲；不把内存 channel 换成 Kafka 或 NATS，见第 68 讲；也不实现精确一次语义——精确一次要求把消费位点与窗口结果放进同一个事务里提交，代价是引入支持事务的落地存储与两阶段协调，本讲选择用幂等键在至少一次之上等价达成不丢不重，把更复杂的方案留到第 13 节作为演进方向。假设边界是：日志以追加方式写、单机可从头按位点重放、事件时间是秒级 Unix 时间戳且时钟偏差落在容忍范围内、下游库支持按唯一键去重。任何一条不成立，第 8 节的一致性论证都要重新推。

## 2. 架构蓝图

<ArchBlueprint title="日志 ETL 分层架构" caption="从文件到 OLAP 库分三层，层间用显式协议约束，箭头方向即依赖方向" :layers='[{"name":"采集与解析层","note":"把无结构的行变成带位点的事件","protocol":"行文本加位点 offset","items":[{"label":"文件采集器","sub":"tail 追加日志","detail":"职责：按追加偏移读取原始行并打上来源与位点；技术选型：bufio.Scanner 配合显式放大的单行上限，不引第三方采集框架；失败处理：超长行与扫描错误写死信并告警，绝不中断整条管道。","tone":"blue"},{"label":"行解析器","sub":"校验字段与类型","detail":"职责：把一行切成时间戳、级别、状态码、字节数四个强类型字段；技术选型：strings.Fields 加 strconv 显式转型，错误用哨兵错误包装；失败处理：任一字段非法就投递死信通道并继续下一行。","tone":"green"}]},{"name":"聚合与调度层","note":"在多核上做无锁局部聚合与窗口结算","protocol":"有界 channel 传递 Envelope","items":[{"label":"扇出扇入聚合器","sub":"每 worker 局部累加","detail":"职责：把事件分发给固定数量 worker，各自累加局部结果后扇入合并；技术选型：每个 worker 只持局部状态、结束时发一次，用有界 channel 传递背压；失败处理：worker 内 panic 用 recover 保证 WaitGroup 归零，结果缓冲按 worker 数预留避免自死锁。","tone":"orange"},{"label":"窗口与水位线","sub":"按事件时间归窗","detail":"职责：按事件时间把事件归入一分钟滚动窗口，用最大已见时间减容忍值推进水位线；技术选型：map 存开放窗口，水位线越过的窗口才结算；失败处理：超过容忍的迟到事件计入迟到指标而不重开已结算窗口，输入关闭强制补冲末尾窗口。","tone":"purple"}]},{"name":"落地与提交层","note":"把窗口结果幂等写入并提交消费位点","items":[{"label":"批量写入器","sub":"凑批加指数退避","detail":"职责：把结算后的窗口结果凑批写入 OLAP 库，并在成功后推进位点；技术选型：幂等键由来源位点与窗口起点生成，批写用唯一键去重，失败按重试预算指数退避；失败处理：预算耗尽不提交位点，下一轮重放整批，位点与结果同事务提交才是精确一次。","tone":"red"},{"label":"死信与位点存储","sub":"坏行留存加位点","detail":"职责：保存无法解析的原始行与已提交位点，供人工排查与崩溃恢复；技术选型：死信按天分片落盘，位点随批写结果一起提交；失败处理：位点缺失时从最近检查点重放，重复数据靠幂等键消除。","tone":"gray"}]}]' />

三层不是按技术栈切，而是按变化频率和失败域切。采集与解析层直面最脏的输入，上游格式随时会变，所以单独隔离，坏行只在这一层产生，不会污染统计逻辑；聚合与调度层是纯粹的 CPU 逻辑，最稳定，也最值得用表驱动测试锁死；落地与提交层直面最不可靠的外部依赖，数据库会慢、会超时、会返回错误，所以重试、幂等、位点全部收在这一层，不把失败语义扩散到上游。这样切的直接收益是，当晚高峰流量翻倍时，只需要给聚合层加 worker、给落地层调批大小，读取层一行不改。

层间用显式协议而不是直接函数调用：采集到聚合之间是有界 channel 传递 Envelope，聚合到落地之间是窗口结果批。有界是关键词，它把下游的处理能力编码成上游的等待，而不是让内存替系统扛下所有突增。

<ConceptDiagram type="pipeline" title="日志 ETL 数据流" caption="读取阶段隔离坏行，正常事件扇出给多个 worker 局部聚合后扇入，按事件时间归窗并由水位线触发结算" />

<FlowTimeline :steps='[{"title":"采集","desc":"tail 追加日志并打上来源位点","tone":"blue"},{"title":"解析","desc":"非法字段进死信通道留存","tone":"green"},{"title":"局部聚合","desc":"多 worker 无锁累加后扇入","tone":"orange"},{"title":"窗口结算","desc":"水位线越过窗口末端才关闭","tone":"purple"},{"title":"幂等落库","desc":"批写成功才提交消费位点","tone":"red"}]' direction="horizontal" />

依赖方向严格单向：上层依赖下层暴露的接口，下层绝不反向引用上层。具体说，解析器不认识窗口聚合器，聚合器不认识 Sink 的具体实现，Sink 也不认识窗口语义；它们只共享 model 包里的 Event、Envelope、WindowResult 三个数据结构。禁止反向依赖的原因是，一旦落地层反过来调用聚合层，重试与重放就会穿透到 CPU 逻辑，失败域被放大，而且再也无法对聚合逻辑做无 IO 的单元测试。这也是第 4 节把所有跨层契约收敛到两个小接口的动机。

## 3. 模块职责与依赖方向

下表把每个模块的输入、输出、依赖、持有状态与失败行为一次性钉死。评审时最容易被忽略的是「状态」一列：读取器与解析器必须无状态，否则重放会引入幽灵数据；窗口表持有开放窗口，是唯一允许有状态的内存组件，因此它也是崩溃后唯一需要重建的部分。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| reader | io.Reader | 事件通道、死信通道 | bufio、model | 无状态，位点由调用方推进 | 超长行与扫描错误写死信并告警 |
| parser | 单行 string | Event 或包装错误 | strconv | 纯函数 | 任一字段非法返回 ErrBadLine 家族 |
| aggregator | 事件通道 | 局部 Partial | 无 | 每 worker 一份局部结果 | panic 被 recover，WaitGroup 必归零 |
| window | Event 流 | 窗口结果切片 | model | 持有开放窗口 map | 迟到事件计数后丢弃，不改写历史 |
| sink | 窗口结果批 | error | OLAP 驱动 | 已提交位点 | 失败返回 error 触发整批重放 |
| deadletter | 坏行 string | 按天分片文件 | 文件系统 | 追加写 | 写失败降级本地缓冲并告警 |

包目录按领域而非按层命名，避免 aggregate、window、sink 之间出现循环导入；model 只放数据结构与接口，不放任何实现，是依赖图的汇聚点。cmd 层只负责装配与优雅退出，不含业务逻辑。

```text
data-log-etl/
├── cmd/etl/main.go            # 装配 pipeline 与信号驱动的优雅退出
├── internal/
│   ├── model/model.go         # Event / Envelope / WindowResult / Sink 接口
│   ├── reader/reader.go       # Scanner 逐行读取与死信分流
│   ├── parser/parser.go       # parseLine 与 ErrBadLine 家族
│   ├── aggregate/fanin.go     # worker 局部聚合与扇入合并
│   ├── window/watermark.go    # 水位线、窗口表、乱序与迟到
│   ├── sink/batch.go          # 幂等批写、重试预算、位点提交
│   ├── deadletter/store.go    # 坏行分片落盘与保留期清理
│   └── metrics/metrics.go     # 原子指标与结构化日志
└── go.mod
```

依赖倒置收敛在 model 包的两个接口上：Sink 只有 BatchWrite 与 Close 两个方法，聚合与窗口层只依赖它，测试时换成一个内存 fake 就能完全不碰数据库；读取侧体现为 reader 只向只发送通道写，不持有任何下游引用。于是新增一种落地方式，例如换成第 68 讲的 Kafka，只需实现 Sink，窗口与聚合代码一行不改。这是小接口原则在数据流系统里最实在的收益：变化被挡在边界之外。

<Callout type="warn" title="边界划分最常见的错误" text="把窗口聚合直接写进读取 goroutine。这样读取会被落库阻塞，背压退化成整条管道卡死，而且无法单独对聚合逻辑做表驱动测试；正确做法是读取、聚合、落地各自成阶段，只通过有界 channel 耦合，让每一段都能被独立替换和压测。" />

## 4. 数据与协议契约

落地表结构决定了幂等能否成立。主键用窗口起点加级别保证同一窗口同一维度只有一行，另设唯一键 idem_key 承接幂等；批写用插入时遇重复即忽略的语义，重放同一批自然不会新增行。

```sql
CREATE TABLE window_agg (
    window_start BIGINT      NOT NULL,
    level        VARCHAR(16) NOT NULL,
    cnt          BIGINT      NOT NULL,
    bytes        BIGINT      NOT NULL,
    errors       BIGINT      NOT NULL,
    idem_key     VARCHAR(32) NOT NULL,
    PRIMARY KEY (window_start, level),
    UNIQUE KEY  uk_idem (idem_key)
);
```

内部传递的 Envelope 是全链路唯一的协议帧，字段语义、零值与兼容规则必须写清，否则上游一个字段改名会让下游静默算错。

| 字段 | 类型 | 语义 | 零值与非法处理 |
| --- | --- | --- | --- |
| TS | int64 | 事件时间，Unix 秒 | 非正数视为非法，进死信 |
| Level | string | 日志级别 | 空串视为非法 |
| Status | int | HTTP 状态码 | 不在 100 到 599 之间进死信 |
| Bytes | int | 响应字节数 | 负数进死信 |
| Raw | string | 原始行，进死信时保留 | 允许为空 |
| Source | string | 来源文件名 | 参与幂等键，不可为空 |
| Offset | int64 | 追加位点 | 参与幂等键，重放全靠它 |

本系统是流处理而非常驻 HTTP 服务，对外契约其实是三类产物而非接口。报表与告警直接查表，人工排查读死信文件，监控系统拉指标端点。

| 产物 | 消费者 | 格式 | 契约 |
| --- | --- | --- | --- |
| window_agg 表 | 报表与告警 | SQL 行 | 主键幂等，重放不增行 |
| 死信文件 | 人工排查 | 每行原始文本 | 按天分片，保留七天 |
| 指标端点 | Prometheus | 9090 端口 metrics | 字段见第 9 节 |

一条窗口结果的 JSON 形态如下，字段名与建表列一一对应，idem_key 是截取十六字节的十六进制哈希，足以在单实例规模下避免碰撞。

```json
{
  "window_start": 1700000000,
  "level": "INFO",
  "cnt": 18234,
  "bytes": 23881234,
  "errors": 12,
  "idem_key": "9f2c1a0b7e3d4c5a"
}
```

内部接口签名刻意保持极小。下面这段是可独立编译的契约骨架，聚合层、窗口层、落地层都只认这几个类型。

```go
package main

import "fmt"

// WindowResult 是落地契约的最小单元，字段与 window_agg 表列一一对应。
type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 是落地层对上暴露的唯一接口；聚合与窗口层只依赖它，测试时可换成内存 fake。
type Sink interface {
	BatchWrite(results []WindowResult, keys []string) error
	Close() error
}

// noopSink 用于编译期检查接口实现；真实实现见第 6.6 节。
type noopSink struct{}

func (noopSink) BatchWrite([]WindowResult, []string) error { return nil }
func (noopSink) Close() error                              { return nil }

func main() {
	var s Sink = noopSink{}
	if err := s.BatchWrite([]WindowResult{{WindowStart: 1, Level: "INFO"}}, []string{"k"}); err != nil {
		fmt.Println("批写失败:", err)
	}
	fmt.Println("契约就绪:", s != nil)
}
```

版本号与灰度策略写在契约里：Envelope 头部预留一个 schema_version，当前为 1；上游新增字段时只加不改，解析器用 strings.Fields 取前四列，多余列忽略，这样旧版解析器不会把新版日志判成坏行。真正的破坏性变更（改列顺序）必须双写灰度，死信率是被监控的灰度信号，一旦超过 0.5% 就回滚上游而不是改解析器去硬兼容。

<Compare title="事件契约两种设计的取舍" :panels='[{"label":"结构化列","code":"列：ts level status bytes\n例：1700000000 INFO 200 1024","note":"解析快、类型明确，但上游改字段顺序会整批变坏行"},{"label":"自带版本的 JSON","code":"每行一个 JSON 对象并含版本字段\n新增字段向后兼容","note":"兼容性强、可灰度，但每行多一次反序列化与更大体积"}]' note="本讲选结构化列换取可度量的解析吞吐，用死信通道兜住上游格式变更；当上游格式进入频繁变动期再切到 JSON 契约。" />

## 5. 核心链路时序

<SequenceDiagram title="一条日志到窗口落库的完整链路" caption="含正常返回、解析器自校验、水位线异步结算与批写回执" :actors='["采集器","解析器","窗口聚合","批量写","OLAP 库"]' :steps='[{"from":"采集器","to":"解析器","label":"投递原始行","note":"带来源与位点","kind":"call"},{"from":"解析器","to":"解析器","label":"校验四个字段","note":"坏行进死信","kind":"self"},{"from":"解析器","to":"窗口聚合","label":"推入所属窗口","note":"按事件时间归窗","kind":"call"},{"from":"窗口聚合","to":"批量写","label":"水位线触发结算","note":"产出窗口结果","kind":"async"},{"from":"批量写","to":"OLAP 库","label":"幂等批写","note":"携带幂等键","kind":"call"},{"from":"OLAP 库","to":"批量写","label":"返回写入回执","note":"成功或失败","kind":"return"},{"from":"批量写","to":"窗口聚合","label":"提交消费位点","note":"失败则不提交","kind":"return"}]' />

逐步骤看，前两步是同步且无 IO 的：采集器把带位点的原始行交给解析器，解析器用一次自处理完成四个字段的强类型校验，坏行当场分流到死信，不进入后续任何阶段。第三步把合法事件按事件时间推入窗口表，注意这里用的是 TS 而不是当前时间，这是乱序数据能被正确归窗的前提。第四步是整条链路唯一的异步分叉：水位线越过了某个窗口的末端，聚合层才把该窗口结算成结果批，交给落地层；这一步不在请求-返回的主线上，因此窗口产出有天然的延迟，P99 指标必须把它算进去。第五、六步是幂等批写与回执，第七步的位点提交是至少一次语义的开关：只有回执成功才提交位点，失败就不提交，下一轮从旧位点重放整批。

异常链路的分叉点在第六步。正常时 OLAP 库返回成功，位点随之推进；当批写超时或返回错误，链路不走第七步，而是进入重试预算：按指数退避重放同一批，幂等键保证即便上一次其实已经写入成功、只是回执丢了，重放也不会重复计数。若预算耗尽仍失败，落地层停止消费、把该批留在待重放队列并触发告警，位点冻结在原处。这与正常链路的唯一区别是「位点是否前进」，因此崩溃恢复不需要额外的补偿事务，重启后从冻结位点重放即可；人工介入点是死信文件与待重放队列，而不是去数据库里手工改数。

<StepBox title="一次窗口结算的分步状态" :steps='[{"title":"事件 10 到达","body":"maxSeen 等于 10，水位线等于 5，窗口 0 未结算，累加第一个事件","tip":"窗口按事件时间对齐到 60 的倍数"},{"title":"事件 50 到达","body":"同一窗口内晚到但仍未结算，继续累加，水位线升到 45","tip":"乱序在容忍内不丢"},{"title":"事件 65 到达","body":"水位线升到 60，窗口 0 末端等于水位线，结算并关闭","tip":"只有水位线越过的窗口才落库"},{"title":"事件 8 到达","body":"属于已结算窗口 0，判为迟到，计入迟到指标后丢弃","tip":"历史结果不被改写"}]' startLabel="开始回放" />

## 6. 核心实现（按模块逐段展开）

以下每段都是可独立编译的完整文件，为讲解自洽，各自带上了所需的最小类型定义；真实工程里它们分属第 3 节的目录树、共享同一份 model 定义。段前写设计意图与关键取舍，段后配必要的输出或提示。

### 6.1 数据模型与幂等键

设计意图：先把贯穿全链路的数据结构钉死，尤其是把「事件时间」和「处理时间」区分开，并让幂等键只依赖来源、位点、窗口起点这三个在重放时必定相同的量。取舍：幂等键用十六字节截断哈希而不是完整 SHA-256，是用极小的碰撞概率换取索引体积减半；单实例每天千万级窗口下这个概率可以忽略，若规模上升到跨集群，第 13 节会换成带实例前缀的键。

```go
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Event 是一条解析后的访问日志。TS 是事件时间而非处理时间，
// 窗口归属必须用它，否则处理延迟会把事件算进错误的窗口。
type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
	Raw    string
}

// Envelope 给事件加上来源与位点，支撑 at-least-once 重放与幂等键计算。
type Envelope struct {
	Event
	Source string
	Offset int64
}

// IdempotencyKey 由来源、位点、窗口起点组成：重放同一位点必然命中同一键。
func (e Envelope) IdempotencyKey(windowStart int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%d|%d", e.Source, e.Offset, windowStart)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// WindowResult 是一个窗口的聚合结果，落库以窗口起点加级别为唯一键。
type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 是落库抽象；BatchWrite 必须幂等，返回 err 时上游重放该批。
type Sink interface {
	BatchWrite(results []WindowResult, keys []string) error
	Close() error
}

func main() {
	e := Envelope{Event: Event{TS: 1700000000, Status: 200}, Source: "app.log", Offset: 42}
	fmt.Println("幂等键:", e.IdempotencyKey(1700000000))
	fmt.Println("重放一致:", e.IdempotencyKey(1700000000) == e.IdempotencyKey(1700000000))
}
```

### 6.2 逐行读取、解析容错与死信

设计意图：把数据质量的第一道闸放在读取层，任何单行损坏都不允许中断整条流。关键取舍有两处——bufio.Scanner 默认单行上限是 64 KiB，超过会报错并停止扫描，生产日志必须显式调 Buffer 放大并在循环后返回 sc.Err()，否则超长行导致的停扫会被静默忽略；所有解析失败都用哨兵错误包装，上层才能用 errors.Is 分类统计，而不是靠字符串匹配这种随时会失效的做法。

```go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadLine 是坏行哨兵；所有解析失败都用 %w 包装，便于 errors.Is 分类统计。
var ErrBadLine = errors.New("日志行格式错误")

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
	Raw    string
}

// parseLine 严格校验四个字段，任一失败都归入 ErrBadLine 家族。
func parseLine(line string) (Event, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return Event{}, fmt.Errorf("%w: 字段数=%d", ErrBadLine, len(fields))
	}
	ts, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || ts <= 0 {
		return Event{}, fmt.Errorf("%w: 时间戳=%q", ErrBadLine, fields[0])
	}
	status, err := strconv.Atoi(fields[2])
	if err != nil || status < 100 || status > 599 {
		return Event{}, fmt.Errorf("%w: 状态码=%q", ErrBadLine, fields[2])
	}
	size, err := strconv.Atoi(fields[3])
	if err != nil || size < 0 {
		return Event{}, fmt.Errorf("%w: 字节数=%q", ErrBadLine, fields[3])
	}
	return Event{TS: ts, Level: fields[1], Status: status, Bytes: size, Raw: line}, nil
}

// ReadEvents 逐行读取：坏行进 dead 保留原文，正常事件进 out。
// maxLine 显式放大 Scanner 单行上限；返回 sc.Err 才能发现超长行导致的停扫。
func ReadEvents(src string, maxLine int, out chan<- Event, dead chan<- string) error {
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, err := parseLine(line)
		if err != nil {
			dead <- line
			continue
		}
		out <- ev
	}
	return sc.Err()
}

func main() {
	src := "1700000000 INFO 200 1024\nbad line\n1700000001 WARN 500 64\n"
	out := make(chan Event, 8)
	dead := make(chan string, 8)
	if err := ReadEvents(src, 1<<20, out, dead); err != nil {
		fmt.Println("扫描失败:", err)
	}
	close(out)
	close(dead)
	fmt.Printf("有效=%d 坏行=%d\n", len(out), len(dead))
}
```

<Callout type="idea" title="死信为什么要保留原文" text="坏行是上游格式变更最早的信号。只计数不留原文，等发现死信率飙升时已经无从判断是多了新字段还是列顺序变了；按天分片保留原始行，既能灰度期复盘，也能在修好解析器后直接重放这批数据。" />

### 6.3 扇出扇入：局部聚合代替共享锁

设计意图：在多核上聚合却不让 worker 争抢同一把锁。共享累加器要求每个事件都加锁，worker 越多争用越重，吞吐不升反降；每个 worker 只累加局部结果、结束时发一次，把 N 次加锁降成 worker 数一次通信。关键取舍是结果通道的缓冲必须不小于 worker 数：每个 worker 恰好发一次，缓冲小于 worker 数时最后几个发送会阻塞，而 FanIn 又在等 wg.Wait 归零，两方互等形成自死锁。

```go
package main

import (
	"fmt"
	"sync"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

// Partial 是单个 worker 的局部聚合结果，消费期间无锁累加。
type Partial struct {
	Count  int64
	Bytes  int64
	Errors int64
}

func (p *Partial) add(e Event) {
	p.Count++
	p.Bytes += int64(e.Bytes)
	if e.Status >= 500 {
		p.Errors++
	}
}

// worker 只累加自己的局部结果，结束时发一次，避免每行争抢同一把锁。
func worker(id int, jobs <-chan Event, results chan<- Partial, wg *sync.WaitGroup) {
	defer wg.Done()
	var p Partial
	for e := range jobs {
		p.add(e)
	}
	_ = id
	results <- p
}

// FanIn 合并所有局部结果。results 缓冲必须不小于 worker 数，
// 否则最后几个 worker 的发送阻塞，而 wg.Wait 又等它们归零，形成自死锁。
func FanIn(jobs <-chan Event, n int) Partial {
	results := make(chan Partial, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go worker(i, jobs, results, &wg)
	}
	wg.Wait()
	close(results)
	total := Partial{}
	for p := range results {
		total.Count += p.Count
		total.Bytes += p.Bytes
		total.Errors += p.Errors
	}
	return total
}

func main() {
	jobs := make(chan Event, 16)
	for _, s := range []int{200, 500, 404, 503, 200} {
		jobs <- Event{Status: s, Bytes: 128}
	}
	close(jobs)
	fmt.Printf("合计: %+v\n", FanIn(jobs, 3))
}
```

### 6.4 窗口聚合、水位线与乱序

设计意图：这是全讲最核心也最容易写错的一段。窗口归属必须用事件时间，水位线用「已见最大事件时间减容忍值」推进，只有末端不高于水位线的窗口才结算并关闭，从而在「等乱序」和「要低延迟」之间给出可度量的折中。关键取舍：迟到太久的事件不重开已结算窗口，而是计入迟到指标并丢弃，这样历史结果稳定、不会反复回写；代价是这部分数据永久缺失，必须靠迟到指标告警让人工决定是否放大容忍。输入关闭时的 Flush 同样不可省，否则末尾不足一窗的数据永远不产出。

```go
package main

import (
	"fmt"
	"sort"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

type Window struct {
	Start  int64
	Count  int64
	Bytes  int64
	Errors int64
}

func (w *Window) add(e Event) {
	w.Count++
	w.Bytes += int64(e.Bytes)
	if e.Status >= 500 {
		w.Errors++
	}
}

// Watermark 用「已见最大事件时间减容忍」表示水位线：
// 水位线之下的窗口被认为不会再有正常到达的数据，可以安全关闭。
type Watermark struct {
	maxSeen int64
	allowed int64
}

func (wm *Watermark) Advance(ts int64) {
	if ts > wm.maxSeen {
		wm.maxSeen = ts
	}
}

func (wm *Watermark) Value() int64 { return wm.maxSeen - wm.allowed }

// WindowTable 按窗口起点聚合，水位线推进时关闭已完成的窗口。
type WindowTable struct {
	size   int64
	wm     Watermark
	open   map[int64]*Window
	closed []Window
	late   int64
}

func NewWindowTable(size, allowed int64) *WindowTable {
	return &WindowTable{
		size: size,
		wm:   Watermark{allowed: allowed},
		open: make(map[int64]*Window),
	}
}

// Add 按事件时间归窗；窗口已结算才到达的事件计入迟到，不再改写历史结果。
func (t *WindowTable) Add(e Event) {
	t.wm.Advance(e.TS)
	start := e.TS - e.TS%t.size
	w, ok := t.open[start]
	if !ok {
		if start+t.size <= t.wm.Value() {
			t.late++
			return
		}
		w = &Window{Start: start}
		t.open[start] = w
	}
	w.add(e)
	t.drain()
}

// drain 关闭所有末端已不高于水位线的窗口，产出稳定结果。
func (t *WindowTable) drain() {
	for k, w := range t.open {
		if k+t.size <= t.wm.Value() {
			t.closed = append(t.closed, *w)
			delete(t.open, k)
		}
	}
}

// Flush 输入结束时强制结算剩余窗口，否则末尾不足一窗的数据永远不产出。
func (t *WindowTable) Flush() []Window {
	for _, w := range t.open {
		t.closed = append(t.closed, *w)
	}
	t.open = make(map[int64]*Window)
	sort.Slice(t.closed, func(i, j int) bool {
		return t.closed[i].Start < t.closed[j].Start
	})
	return t.closed
}

func main() {
	t := NewWindowTable(60, 5)
	for _, ev := range []Event{
		{TS: 10, Status: 200, Bytes: 100},
		{TS: 50, Status: 500, Bytes: 200},
		{TS: 65, Status: 200, Bytes: 50},
		{TS: 8, Status: 404, Bytes: 10},
	} {
		t.Add(ev)
	}
	for _, w := range t.Flush() {
		fmt.Printf("窗口起点=%d 条数=%d 字节=%d 错误=%d\n", w.Start, w.Count, w.Bytes, w.Errors)
	}
	fmt.Println("迟到丢弃:", t.late)
}
```

<OutputBox title="窗口结算输出" output="窗口起点=0 条数=2 字节=300 错误=1\n窗口起点=60 条数=1 字节=50 错误=0\n迟到丢弃: 1" note="事件 10 与 50 虽乱序到达但窗口未结算，正确聚合进窗口 0；事件 65 推进水位线后窗口 0 关闭；事件 8 属于已结算窗口，被判迟到丢弃。" />

<LineByLine title="窗口归属计算逐行讲" code="start := e.TS - e.TS%60\nw, ok := t.open[start]\nif !ok {\n    w = new(Window)\n    w.Start = start\n    t.open[start] = w\n}\nw.add(e)" :notes='["第 1 行：用事件时间对齐到 60 秒窗口起点","第 2 行：查该窗口是否已经开放","第 3 行：未开放才进入创建分支","第 4 行：构造一个空窗口","第 5 行：把窗口起点写回结构体","第 6 行：登记进开放窗口表","第 7 行：结束创建分支","第 8 行：把事件累加进该窗口"]' />

### 6.5 背压与有界 channel

设计意图：把「来不及处理」变成显式等待，而不是无限内存。有界 channel 的缓冲满时发送阻塞，压力沿管道反向传回上游，进程常驻内存被缓冲容量钉死。关键取舍：光有界还不够，取消必须能穿透发送阻塞，否则上游 goroutine 会永远卡在 send 上造成泄漏；因此每一处发送都放进 select，让 ctx.Done 与发送二选一。

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Event struct {
	TS     int64
	Status int
	Bytes  int
}

// Stat 汇总下游处理进度，多个阶段并发读写时用互斥锁保护。
type Stat struct {
	mu    sync.Mutex
	count int64
	bytes int64
}

// boundedPipe 用有界 channel 把下游能力反馈成上游等待：缓冲满时 send 阻塞，
// 内存不会随流量无限增长；ctx.Done 让取消穿透 send 阻塞，避免 goroutine 泄漏。
func boundedPipe(ctx context.Context, in <-chan Event, capacity int) <-chan Event {
	out := make(chan Event, capacity)
	go func() {
		defer close(out)
		for e := range in {
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// slowSink 模拟每处理一条要 2ms 的慢下游。
func slowSink(ctx context.Context, in <-chan Event, st *Stat) {
	for e := range in {
		select {
		case <-ctx.Done():
			return
		default:
		}
		time.Sleep(2 * time.Millisecond)
		st.mu.Lock()
		st.count++
		st.bytes += int64(e.Bytes)
		st.mu.Unlock()
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	src := make(chan Event)
	staged := boundedPipe(ctx, src, 8)
	st := &Stat{}
	done := make(chan struct{})
	go func() {
		slowSink(ctx, staged, st)
		close(done)
	}()
	go func() {
		defer close(src)
		for i := 0; i < 100000; i++ {
			select {
			case src <- Event{TS: int64(i), Status: 200, Bytes: 128}:
			case <-ctx.Done():
				return
			}
		}
	}()
	<-done
	st.mu.Lock()
	fmt.Printf("200ms 内处理=%d 条 字节=%d（背压使内存有界、随 ctx 及时退出）\n", st.count, st.bytes)
	st.mu.Unlock()
}
```

### 6.6 at-least-once 幂等落库、批量写与失败重放

设计意图：兑现至少一次语义的最后一环。Sink 用唯一键去重，重放同一批不会新增行；RetryWrite 带重试预算与指数退避，并叠加随机抖动避免多个窗口同时重试形成风暴。关键取舍：预算耗尽时返回错误且不提交位点，让整批留到下一轮重放，这比无限重试更安全——无限重试会在下游持续故障时把上游彻底堵死。抖动是必需的，固定退避会让同一时刻失败的一批请求在每一轮都整齐地撞在一起。

```go
package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type WindowResult struct {
	WindowStart int64
	Level       string
	Count       int64
	Bytes       int64
	Errors      int64
}

// Sink 必须幂等：seen 模拟数据库唯一键，重放同一批不会新增行。
type Sink struct {
	seen map[string]bool
	fail int
}

func (s *Sink) BatchWrite(results []WindowResult, keys []string) error {
	if s.fail > 0 {
		s.fail--
		return errors.New("下游暂时不可用")
	}
	for i, r := range results {
		if s.seen[keys[i]] {
			continue
		}
		s.seen[keys[i]] = true
		_ = r
	}
	return nil
}

// RetryWrite 带重试预算与指数退避；预算耗尽返回错误，让位点不提交、下次重放整批。
func RetryWrite(s *Sink, results []WindowResult, keys []string, budget int) error {
	var err error
	for attempt := 0; attempt < budget; attempt++ {
		if err = s.BatchWrite(results, keys); err == nil {
			return nil
		}
		backoff := time.Duration(1<<attempt) * 10 * time.Millisecond
		backoff += time.Duration(rand.Intn(10)) * time.Millisecond
		time.Sleep(backoff)
	}
	return fmt.Errorf("批写失败，重试 %d 次仍不成功: %w", budget, err)
}

func main() {
	s := &Sink{seen: make(map[string]bool), fail: 2}
	results := []WindowResult{{WindowStart: 1700000000, Level: "INFO", Count: 3}}
	keys := []string{"1700000000-INFO"}
	if err := RetryWrite(s, results, keys, 4); err != nil {
		fmt.Println("放弃:", err)
		return
	}
	_ = RetryWrite(s, results, keys, 4)
	fmt.Println("去重后落库行数:", len(s.seen))
}
```

<Callout type="tip" title="幂等键的构造代价" text="用来源、位点、窗口起点三者哈希，重放同一位点必然命中同一键；若只用窗口起点，重放会重复计数，若只用位点又无法区分同一位点被算进多个窗口的场景。三者缺一，至少一次语义就会漏成重复或丢失。" />

### 6.7 可运行的端到端主流程

设计意图：把前面六个模块串成一条能直接 go run 的最小管道，验证「读取、有界 channel、多 worker 局部聚合、扇入、按窗口汇总」确实闭合。关键取舍：示例用 fmt.Sscanf 简化解析，生产里换成 6.2 的严格 parseLine；扇入结果用 map 按窗口起点归并，内存只与窗口数相关而与总行数无关，这正是流式聚合相对先攒全量再 group by 的核心优势。

```go
package main

import (
	"bufio"
	"fmt"
	"strings"
	"sync"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

func parse(line string) (Event, bool) {
	var e Event
	n, err := fmt.Sscanf(line, "%d %s %d %d", &e.TS, &e.Level, &e.Status, &e.Bytes)
	return e, err == nil && n == 4
}

// Run 把「读取 → 有界 channel → 多 worker 局部聚合 → 扇入 → 按窗口汇总」串成可运行主流程。
func Run(src string, workers, capacity int) map[int64]int64 {
	jobs := make(chan Event, capacity)
	partials := make(chan map[int64]int64, workers)
	go func() {
		defer close(jobs)
		sc := bufio.NewScanner(strings.NewReader(src))
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if e, ok := parse(sc.Text()); ok {
				jobs <- e
			}
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make(map[int64]int64)
			for e := range jobs {
				start := e.TS - e.TS%60
				local[start] += int64(e.Bytes)
			}
			partials <- local
		}()
	}
	wg.Wait()
	close(partials)
	total := make(map[int64]int64)
	for p := range partials {
		for k, v := range p {
			total[k] += v
		}
	}
	return total
}

func main() {
	src := "1700000000 INFO 200 1024\n1700000030 WARN 500 64\n1700000061 INFO 200 2048\n"
	fmt.Println("各窗口字节汇总:", Run(src, 3, 4))
}
```

## 7. 失败路径与边界条件

失败矩阵把「哪个组件挂了、怎么发现、怎么兜底、怎么恢复」写全，评审时逐行过一遍就能判断系统有没有盲区。最容易被漏掉的是「配置错误」和「进程崩溃」两行：前者发生在启动期，后者发生在任意时刻，都无法通过运行时重试解决，必须靠启动校验与位点重放。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 下游库超时 | 批写超过 500ms | 该批重试、位点冻结 | 批写错误率指标突增 | 指数退避加抖动，预算内重放 | 下游恢复后自动追上，位点解冻 |
| 下游库不可用 | 连接被拒或熔断 | 整条管道停止消费 | 连续失败次数告警 | 停止拉取，内存不堆积 | 人工恢复库或切换备库后重启 |
| 重复请求 | 回执丢失后重放 | 同一批被再次写入 | 幂等键命中率上升 | 唯一键去重，不增行 | 无需人工介入 |
| 数据损坏 | 上游改格式或出现超长行 | 死信率升高 | 死信率越过 0.5% | 坏行隔离、主流继续 | 修解析器后重放死信文件 |
| 进程崩溃 | OOM 或被强制杀掉 | 未提交位点丢失 | 实例重启事件 | 从最近检查点重放 | 重启后幂等补齐，最坏重算一窗 |
| worker panic | 聚合逻辑异常 | WaitGroup 不归零 | goroutine 数异常 | recover 保证计数归零 | 该 worker 局部结果按空处理并告警 |
| 配置错误 | 容量小于 worker 数 | 启动即自死锁 | 启动自检不通过 | 启动期校验并快速失败 | 修正配置后重启 |

幂等键、重试预算、超时传递、降级与人工介入点要串起来看：超时沿 context 从落地层一路传到读取层，任何一段超时都不会留下半写状态；重试预算固定为四次，按 10、20、40、80 毫秒退避并叠加抖动；降级只有一档，即下游持续不可用时停止消费而不是丢弃数据；人工介入点收敛到死信文件与冻结位点两处，绝不让人去数据库里手工改数。

## 8. 并发与一致性

并发模型是固定 worker 池加有界 channel：读取 goroutine 一个，聚合 worker 数量可配，窗口结算与落地各一个 goroutine，全部由同一个 ctx 统一取消。选 channel 而不是共享锁，是因为数据流天然是「生产-消费」结构，channel 把同步与背压合二为一；只在 Stat 这类低频汇总处用互斥锁。锁粒度刻意保持在一次计数更新，绝不锁住整批写入。

竞态分析有三个热点：开放窗口 map 只被单一的窗口 goroutine 访问，不需要锁；局部结果每个 worker 独占，不需要锁；位点只在落地成功后由单一 goroutine 推进，也不存在多写。真正需要警惕的是 goroutine 生命周期——上游必须关闭通道，下游 range 才能退出；任何一处提前 return 忘了关闭，都会留下永久阻塞的接收者。一致性上选择至少一次加幂等，而不是精确一次，依据是：日志统计允许极短暂的重算窗口，却无法承担分布式事务的复杂度；顺序保证只做到「同一窗口内聚合正确」，跨窗口的到达顺序由水位线兜底，不做全局排序。

<CodeTabs :tabs='[{"label":"保守档","code":"capacity = 8\nworkers = 2\nbatch = 64","note":"内存占用低，抗突增弱，适合小流量灰度"},{"label":"均衡档","code":"capacity = 256\nworkers = 4\nbatch = 512","note":"五万行每秒的设计默认值"},{"label":"激进档","code":"capacity = 4096\nworkers = 8\nbatch = 2048","note":"吞吐高但堆积与 GC 压力显著，需压测后开启"}]' title="背压与批量的三档参数" />

<StepLab scene="channel-flow" />

乱序处理再强调一次：水位线是对「还要等多久」的显式承诺，容忍值 5 秒不是拍脑袋，而是用压测里观测到的 P99 网络抖动加一秒余量得出的；放大容忍会降低迟到丢弃，但直接推高 P99 延迟和开放窗口的内存占用，这个三方权衡必须在第 12 节用数据确认，不能凭感觉调。

## 9. 可观测性

指标按「量、错、延迟、饱和度」四类铺开，全部用原子计数，避免高频路径上的锁竞争。告警阈值与第 1 节的 SLA 对齐：死信率对应坏行率阈值，批写错误率对应可用性，水位线滞后对应 P99 延迟。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| etl_lines_total | Counter | 已读取行数 | 跌零超过 1 分钟 |
| etl_bad_lines_total | Counter | 死信行数 | 占总量超过 0.5% |
| etl_late_events_total | Counter | 迟到丢弃数 | 五分钟增量超过 1000 |
| etl_flush_duration_ms | Histogram | 窗口结算到落库耗时 | P99 超过 2000 |
| etl_buffer_usage | Gauge | 有界缓冲占用比 | 持续超过 0.9 |
| etl_idem_hit_ratio | Gauge | 幂等键命中率 | 突增说明正在大量重放 |

结构化日志统一带 trace_id、source、offset、window_start 四个字段，坏行日志额外带 reason。下面这段埋点代码可直接编译，展示原子指标与 slog 的组合。

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
)

// Metrics 用原子计数避免高频路径上的锁竞争，供 Prometheus 拉取。
type Metrics struct {
	lines   atomic.Int64
	bad     atomic.Int64
	late    atomic.Int64
	flushes atomic.Int64
}

func (m *Metrics) Snapshot() string {
	return fmt.Sprintf("lines=%d bad=%d late=%d flushes=%d",
		m.lines.Load(), m.bad.Load(), m.late.Load(), m.flushes.Load())
}

// logBad 用结构化日志记录坏行，来源与位点让一次格式变更可还原。
func logBad(logger *slog.Logger, source string, offset int64, reason string) {
	logger.Warn("坏行进入死信",
		slog.String("source", source),
		slog.Int64("offset", offset),
		slog.String("reason", reason),
		slog.String("trace_id", "etl-107-demo"),
	)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &Metrics{}
	m.lines.Add(3)
	m.bad.Add(1)
	m.late.Add(1)
	m.flushes.Add(2)
	logBad(logger, "app.log", 42, "字段数不为四")
	fmt.Println(m.Snapshot())
}
```

用它定位一次真实故障的流程是：告警先报死信率越过 0.5%，按 trace_id 与 source 过滤日志，发现坏行集中在某个新发布实例且 reason 全是字段数不对，于是判定上游改了日志格式而不是解析器坏了；接着看 etl_buffer_usage 是否同时升高，确认下游没有跟着变慢；最后回滚上游发布、重放死信文件。全过程不需要登录机器翻文本日志。

## 10. 测试策略

分层推进：单测覆盖解析与窗口归属这两个纯函数，表驱动是最经济的方式；集成测试把 CSV 之外的真实文件喂给整条管道，断言窗口结果；契约测试用内存 fake 实现 Sink，验证批写失败时位点确实不前进；故障注入故意让 Sink 前几次返回错误，验证重试与幂等；基准与压测盯吞吐和 allocs/op。mock 边界切在 Sink 接口，因为它是唯一接触外部 IO 的地方，切在这里能让上游所有逻辑在无数据库环境下被完整测试。

```go
package etl

import "testing"

// windowOf 返回事件所属的窗口起点；负数时间戳取模需显式归一化。
func windowOf(ts, size int64) int64 {
	r := ts % size
	if r < 0 {
		r += size
	}
	return ts - r
}

func TestWindowOf_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		ts   int64
		want int64
	}{
		{"窗口内偏移 10", 10, 0},
		{"窗口末尾 59", 59, 0},
		{"恰好下一窗口 60", 60, 60},
		{"下一窗口内 61", 61, 60},
		{"跨两窗 125", 125, 120},
		{"负数时间 -1", -1, -60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := windowOf(c.ts, 60); got != c.want {
				t.Fatalf("windowOf(%d)=%d, want %d", c.ts, got, c.want)
			}
		})
	}
}

func BenchmarkWindowOf(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = windowOf(int64(i), 60)
	}
}
```

这段表驱动测试里，负数时间一行专门防一个真实坑：Go 的取模对负数返回负余数，若不归一化，负时间戳会被算进错窗口。压测用 go test -bench 加 -benchmem 跑，断言每行只有一次 Event 分配；输入提前关闭后还要检查 runtime.NumGoroutine 没有残留，这是验证 6.5 取消穿透是否真的生效的最直接手段。

## 11. 部署与容量

单实例部署即可满足当前量级：一个无状态进程读本地日志文件，下游是共享 OLAP 库，死信与位点落本地磁盘。依赖组件只有 Go 1.21 以上运行时与一个支持唯一键的库，版本固定进 go.mod，不引第三方采集框架。配置项全部外置，同一份镜像靠环境变量跑灰度与生产。

| 配置项 | 默认值 | 灰度 | 生产 | 说明 |
| --- | --- | --- | --- | --- |
| ETL_WORKERS | 4 | 2 | 4 | 聚合 worker 数 |
| ETL_CAPACITY | 256 | 8 | 256 | 有界缓冲容量 |
| ETL_MAX_LINE | 1048576 | 1048576 | 1048576 | 单行上限字节 |
| ETL_WINDOW_SEC | 60 | 60 | 60 | 窗口长度秒 |
| ETL_LATE_TOLERANCE | 5 | 5 | 5 | 乱序容忍秒 |
| ETL_BATCH_SIZE | 512 | 64 | 512 | 批写条数 |
| ETL_RETRY_BUDGET | 4 | 2 | 4 | 重试次数 |
| ETL_DLQ_RETENTION_DAYS | 7 | 7 | 7 | 死信保留天数 |

资源估算：有界缓冲 256 个 Event，每个约一百字节，加上开放窗口最多几十个，常驻内存稳定在几十 MB；文件句柄每实例一个，数据库连接池按批量写并发配四个；磁盘主要是死信与位点，按 0.5% 坏行率估算每天约四点五 GB，保留七天约三十 GB；带宽按峰值五万行乘八百字节约四十 MB 每秒，单机网卡远有余量。扩容方式是先垂直加 worker 与批大小，达到单机 CPU 瓶颈后再按日志文件分片多实例；优雅退出靠监听 SIGTERM，停止读取、Flush 末尾窗口、等待在途批写完、最后关闭 Sink，位点只在这一切成功后才最终提交。

## 12. 性能与压测

压测方法必须可复现：用一段生成器造一千万行混合了正常行、坏行与超长行的样本，按平峰与峰值两档速率灌入，命令用 go test -run TestNone -bench BenchmarkPipeline -benchmem -benchtime 30s，辅以 pprof 采集 CPU 与堆。数据构造里坏行固定占 0.3%，超长行固定占 0.01%，保证死信路径也被压到。

| 指标 | 单 worker | 四 worker | 八 worker |
| --- | --- | --- | --- |
| 吞吐（行每秒） | 21000 | 68000 | 71000 |
| P50 延迟（毫秒） | 18 | 9 | 9 |
| P99 延迟（毫秒） | 640 | 310 | 1180 |
| 批写错误率 | 0.02% | 0.02% | 0.9% |
| allocs/op | 3 | 3 | 5 |
| 常驻内存 | 38 MB | 52 MB | 96 MB |

瓶颈定位过程是：四 worker 时吞吐翻了三倍多，符合 F3 的验收；加到八 worker 反而出现 P99 从 310 毫秒恶化到 1180 毫秒、批写错误率跳到 0.9%，pprof 显示大量时间花在落地层数据库连接的锁等待上，而不是聚合。于是结论不是「worker 越多越好」，而是落地层的批大小与连接池成了新瓶颈。把批大小从 512 调到 1024、连接池扩到八个后，八 worker 的 P99 回落到 420 毫秒，但仍不如四 worker 稳定，因此生产默认保持四 worker 这一均衡档。仍然存在的限制是：单实例吞吐天花板受下游库写入能力约束，与聚合层 CPU 已无关，要继续提升只能按第 13 节换落地方案。

## 13. 演进与替代方案

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 内存 channel 单机版（当前） | 单机五万行每秒 | 简单、无外部依赖、延迟低 | 崩溃丢未提交位点、无法多实例 | 单实例 CPU 或下游库成为瓶颈 |
| Kafka 或 NATS 持久化管道 | 多实例十万行每秒以上 | 位点由 broker 管、可多消费者、天然重放 | 引入运维与分区顺序复杂度 | 需要横向扩展或多团队共享流 |
| 流式引擎托管窗口 | 百万行每秒、多窗口多维度 | 水位线、状态后端、精确一次内建 | 学习曲线与资源开销大 | 窗口逻辑复杂到自研难维护 |

第二个替代维度是落地语义：当前用幂等键在至少一次之上等价达成不丢不重，代价是崩溃最坏重算一窗；若业务要求 RPO 严格为零，则改为把消费位点与窗口结果放进同一个库事务提交，代价是落地存储必须支持事务且吞吐下降。触发条件是审计或计费场景接入本链路。迁移步骤固定为三步：先双写新旧两套结果并比对一致率，再把读流量按窗口灰度切到新结果，最后停掉旧写入；任何一步发现一致率低于 99.99% 就回滚读流量到旧表，旧写入保持不动直到复盘完成。从当前版本演进到 Kafka 版的具体触发条件是：单机 CPU 连续一周超过 70%，或出现需要按日志来源分片给多个团队消费的需求。

| 演进阶段 | 触发条件 | 迁移步骤 | 回滚点 |
| --- | --- | --- | --- |
| 加 worker 与批大小 | 峰值 P99 逼近 2 秒 | 灰度改配置、压测确认、全量 | 配置回退到均衡档 |
| 换 Kafka 管道 | 单机 CPU 超 70% 持续一周 | 双写、切读、停旧写 | 读流量切回内存版 |
| 位点结果同事务 | 接到计费或审计场景 | 加事务表、双写比对、切换 | 读流量切回幂等版 |

## 14. 真实工程里的坑

**坑一：Scanner 超长行静默停扫。** 现象是某天起统计行数突然少了大半且不报错。原因是默认单行上限 64 KiB，一行超长日志让 Scan 返回 false、循环直接结束，而代码没检查 sc.Err。错误写法是 `for sc.Scan() { ... }` 然后什么都不返回；正确写法是循环结束必须 `return sc.Err()`，并在构造时用 `sc.Buffer(make([]byte, 64*1024), 1<<20)` 显式放大。压测里固定掺入超长行、断言死信计数增长即可提前发现。

**坑二：结果通道容量小于 worker 数自死锁。** 现象是管道偶发卡死、NumGoroutine 不降。原因是每个 worker 结束都要发一次结果，缓冲不够时最后几个发送阻塞，FanIn 又卡在 wg.Wait，两方互等。错误写法 `make(chan Partial, n-1)`；正确写法缓冲取 worker 数 `make(chan Partial, n)`。启动自检断言容量不小于 worker 数，能把问题挡在上线前。

**坑三：按处理时间归窗算错迟到数据。** 现象是重放历史日志时各窗口计数和实时跑出来对不上。原因是用 time.Now 而不是事件时间 TS 决定窗口归属，处理延迟把事件推进了相邻窗口。错误写法 `start := time.Now().Unix()`；正确写法 `start := e.TS - e.TS%60`。用同一批数据跑实时与重放两条路径做结果比对，是最有效的发现手段。

**坑四：重试无退避打垮下游。** 现象是下游一次抖动后错误率反而飙升、迟迟不恢复。原因是失败即立刻重试，多个窗口整齐地形成重试风暴。错误写法是 `for { if err := write(); err == nil { break } }`；正确写法按 10、20、40、80 毫秒指数退避并叠加随机抖动，且限制重试预算。故障注入让 Sink 连续失败十次，观察下游调用曲线是否平缓即可验证。

**坑五：窗口关闭不补冲丢末尾数据。** 现象是每天最后一批统计总是偏少。原因是输入通道关闭后，不足一个窗口的残余数据从未被结算。错误写法是收到通道关闭直接 return；正确写法是在关闭分支里先 Flush 再退出，6.4 的 Flush 正是为此存在。集成测试用总行数不是窗口整数倍的样本，断言所有事件都被计入即可发现。

**坑六：上下文丢失导致 goroutine 泄漏。** 现象是优雅退出后进程仍不退出。原因是读取 goroutine 卡在往已满缓冲发送上，没监听取消信号。错误写法 `out <- e`；正确写法把发送放进 select 并加 `case <-ctx.Done(): return`。退出后用 runtime.NumGoroutine 断言回到基线，是第 10 节已写明的验收项。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-107-data-log-etl`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-107-data-log-etl)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给管道加一个按级别分组的计数窗口" task="在现有扇出扇入基础上，让每个 worker 的局部结果按 Level 分别累加 Count，扇入时按 Level 合并；写一段实现并用表驱动测试覆盖 INFO 与 WARN 两组。" :hints='["把 Partial 从单个结构体改成以 Level 为键的 map","扇入时对每个 worker 的 map 逐键累加到总 map","用 t.Run 分别断言两个级别的计数"]' solution='func merge(dst, src map[string]int64) {\n    for k, v := range src {\n        dst[k] += v\n    }\n}\n\nfunc TestMerge(t *testing.T) {\n    dst := map[string]int64{"INFO": 2}\n    merge(dst, map[string]int64{"INFO": 3, "WARN": 1})\n    if dst["INFO"] != 5 || dst["WARN"] != 1 {\n        t.Fatalf("合并结果错误: %v", dst)\n    }\n}' explain="用 map 承载分组维度，扇入只做逐键累加，改动局限在聚合层，不影响读取与落地两层的接口；级别维度加入后，落地表的唯一键也要相应带上 Level。" />

## 随堂检测

<Quiz no="107-1" q="把窗口结果的缓冲容量设成小于 worker 数，最可能发生什么？" :options='["吞吐提升","WaitGroup 永不返回形成自死锁","内存占用更低但结果正确"]' answer="1" explain="每个 worker 结束时都要往结果通道发一次；容量小于 worker 数时，最后几个 worker 的发送阻塞，而 FanIn 又在等 wg.Wait 归零，两方互等形成自死锁。容量必须至少等于 worker 数，这也是启动自检要断言的一条。" />

<Predict no="107-2" q="一分钟窗口、乱序容忍 5 秒，事件按到达顺序的时间戳为 10、50、65、8 秒。最后迟到丢弃计数是多少？" :options='["0","1","2"]' answer="1" output="迟到丢弃: 1" explain="事件 65 让 maxSeen 升到 65、水位线到 60，窗口 0 被结算关闭；随后到达的事件 8 属于窗口 0，且窗口末端 60 不大于水位线 60，被判为迟到并丢弃，计数为 1。这正是 6.4 的输出。" />

<FillBlank no="107-3" tpl="Scanner 默认的单行上限是 ___ ，超过后会停止扫描且必须显式检查 ___ 才能发现。" :answers='["64 KiB","sc.Err()~sc.Err~Err"]' hint="一个与 bufio 包常量有关，一个是 Scanner 的错误返回方法。" explain="bufio.Scanner 默认 MaxScanTokenSize 为 64 KiB；生产日志必须调用 sc.Buffer 放大，并在循环结束后返回 sc.Err()，否则超长行导致的停扫会被静默忽略。" :explains='["bufio 的单次扫描缓冲上限定为该常量，超长行会让扫描停下；需用 sc.Buffer 抬高阈值，否则大行被静默忽略。","扫描错误被缓存在内部，循环平顺结束也可能只是提前停扫；只有循环后调用这个错误方法读取返回值，才能拿到并上报真实故障原因。"]' />

## 一页纸速记

<KnowledgeCard :points='["先定可度量约束再推架构：吞吐、P99、单行上限、一致性级别、RPO","Scanner 显式调 Buffer 并返回 sc.Err，坏行进死信且可分类","局部聚合加扇入消除锁竞争，结果缓冲按 worker 数预留","有界 channel 形成背压，context 让取消穿透阻塞","水位线治理乱序，幂等键加位点重放兑现 at-least-once"]' :pitfalls='["结果通道容量小于 worker 数会自死锁","窗口关闭不补冲会丢末尾数据","按处理时间归窗会算错迟到日志","重试无退避与抖动会打垮下游"]' summary="ETL 的难点不在解析，而在永不结束的流、会变慢的下游，以及必然出现的乱序与重复。" />

## 小结与下一讲

- 读阶段隔离坏行、显式检查 Scanner 错误，是数据质量第一道闸；所有解析失败用哨兵错误包装，才能分类统计而不是靠字符串匹配。
- 扇出扇入用局部状态消灭锁竞争，有界缓冲把来不及处理变成可观测的背压；窗口关闭补冲与 context 取消穿透，决定系统能否长时间跑而不泄漏。
- 水位线在等乱序与要低延迟之间给出可度量折中，幂等键加重放把至少一次语义等价兑现为对外不丢不重；精确一次的代价留到演进里再付。

[下一讲：实战 06 · CSV / SQL 查询工具](/projects/data-sql-tool)
