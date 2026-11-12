---
title: 第 93 讲 · SRE：SLO、告警、容量与稳定性治理
description: 用 SLO 与错误预算把稳定性变成可执行的发布与告警决策
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="50 分钟" goal="用 SLO 与错误预算驱动发布、告警与容量决策，能对稳定性负责" prereq="Helm / GitOps / 灰度发布" />

# 第 93 讲 · SRE：SLO、告警、容量与稳定性治理

> 前面把系统做成了能扛故障、能灰度、能回滚的样子，可还差一个最上层的问题：凭什么决定「现在还能不能继续发版」？凭感觉、凭催促、还是凭监控图上那条绿线？SRE 给的答案是把稳定性量化成 SLI 与 SLO，再把「还能冒多少险」折算成错误预算，让发布、告警、容量这些动作都有可执行的判断依据。本讲把这套治理闭环走完，并追问：预算烧到一半、告警半夜响起、容量见底时，到底该做什么。

## 这一讲你会得到什么
<CheckList id="goal-93" title="本讲目标" :items='["区分 SLI、SLO 与错误预算，并算出允许失败数","用剩余错误预算给出发布、灰度与冻结的可执行决策","用长短窗口燃烧率给告警降噪，区分 page 与 ticket","说出容量规划、压测预案与 On-call 复盘的闭环"]' />

## 1. 先建立直觉
<ConceptDiagram type="slo" title="SLO 与错误预算：从指标到发布决策" />

把 SLO 想成给系统定的「月度信用额度」。SLI 是实际测出来的指标，例如成功请求数除以总请求数；SLO 是这个指标的目标值，比如 99.9%；`1 - SLO` 就是错误预算——一个月里允许有 0.1% 的请求失败，换算成具体数字，是一百万次请求里可以失败一千次。

错误预算最重要的价值，不是事后写进周报，而是**事前决定动作**。它把「稳定性团队」和「业务团队」原本对立的目标变成同一把尺子：预算充足，就放心发新功能、承担变更风险；预算快烧完，就冻结非必要变更、把人力转去修稳定性。这样「能不能上线」不再是拍脑袋，而是有一条双方都认的量化依据。

顺着「面向故障设计」再问一句：一次故障消耗了多少预算、还剩多少、下一步是继续发版还是停下来止血——这三个问题有明确答案，稳定性才算被真正管理，而不是被祈祷。

## 2. 语法与规则

先把 SLO 与错误预算算清楚。下面这段定义了可用性、预算额度与已消耗比例，结果稳定可复现。

```go
package main

import "fmt"

// SLO 定义可用性目标与统计周期内的请求情况
type SLO struct {
	Target float64 // 例如 0.999 表示 99.9%
	Total  int     // 周期内总请求数
	Bad    int     // 失败请求数
}

// Availability 返回实际可用性
func (s SLO) Availability() float64 {
	if s.Total == 0 {
		return 1
	}
	return 1 - float64(s.Bad)/float64(s.Total)
}

// Budget 返回错误预算：周期内允许的最大失败数
func (s SLO) Budget() float64 {
	return (1 - s.Target) * float64(s.Total)
}

// Burned 返回已消耗的错误预算比例
func (s SLO) Burned() float64 {
	b := s.Budget()
	if b == 0 {
		return 0
	}
	return float64(s.Bad) / b
}

// Met 判断本周期是否达标
func (s SLO) Met() bool {
	return s.Availability() >= s.Target
}

func main() {
	s := SLO{Target: 0.999, Total: 1_000_000, Bad: 420}
	fmt.Printf("实际可用性: %.4f%%\n", s.Availability()*100)
	fmt.Printf("错误预算允许失败数: %.0f\n", s.Budget())
	fmt.Printf("已消耗预算: %.1f%%\n", s.Burned()*100)
	fmt.Println("本周期达标:", s.Met())
}
```

<OutputBox title="运行结果" output="实际可用性: 99.9580%\n错误预算允许失败数: 1000\n已消耗预算: 42.0%\n本周期达标: true" />

注意「达标」和「预算没用完」是两回事：这一周期可用性 99.958% 高于目标 99.9%，仍然算达标，但一千次允许的失败已经用掉 420 次，也就是 42% 的风险额度被消耗了。只看达标与否，会漏掉「正在快速接近上限」这个信号。

<LineByLine code="type SLO struct {\nTarget float64\nTotal int\nBad int\n}" :notes='["SLO 结构把目标与实测数据放在一起，决策才有依据","Target 是可用性目标，直接决定错误预算的大小","Total 是统计周期内的请求总量，是预算的分母","Bad 是失败请求数，故障每一次发生都会累加到这里","结构体结束，可用性、预算与消耗比例都由这三个字段推出"]' />

把预算变成决策，才是 SRE 与「看监控」的分水岭。下面这个函数把消耗比例映射成明确的发布动作：

```go
package main

import "fmt"

// Decide 根据已消耗的错误预算给出发布决策，把 SLO 变成可执行依据
func Decide(burnedPercent float64) string {
	switch {
	case burnedPercent >= 100:
		return "预算耗尽：冻结非必要变更，全力修稳定性"
	case burnedPercent >= 75:
		return "预算紧张：只放行修复与回滚，暂停新功能"
	case burnedPercent >= 50:
		return "预算过半：发布需走灰度并加强观测"
	default:
		return "预算充足：可正常发布新功能"
	}
}

func main() {
	for _, burned := range []float64{20, 55, 80, 100} {
		fmt.Printf("已消耗 %3.0f%% -> %s\n", burned, Decide(burned))
	}
}
```

四档输出的动作逐收紧：充足就正常发、过半要灰度、紧张只修不放、耗尽直接冻结。这条规则把「要不要发版」从争论变成查表，也把上一讲的灰度发布接到了一个可量化的触发条件上。

## 3. 深入一步

把治理体系里的概念排开，重点看每个概念对应什么动作：

| 概念 | 定义 | 可执行决策 |
| --- | --- | --- |
| SLI | 成功率、延迟等可测量指标 | 先统一口径，否则数字无意义 |
| SLO | SLI 的目标值，如 99.9% | 内部承诺，不是对外的绝对保证 |
| 错误预算 | `1 - SLO` 允许的不达标额度 | 充足发版、过半灰度、耗尽冻结 |
| 燃烧率 | 窗口内消耗预算的速度 | 快慢窗口联合决定 page 还是 ticket |
| 症状告警 | 只对影响用户的现象呼叫 | 降噪，避免告警疲劳 |

<Term word="SLI" text="服务质量指标，是可直接测量的量，例如成功请求占比与请求延迟分位值。" />
<Term word="SLO" text="服务质量目标，为 SLI 设定目标区间，是团队内部对稳定性的承诺。" />
<Term word="错误预算" text="由 1 减去 SLO 得出的允许失败额度，把「还能冒多少险」变成可分配的资源。" />
<Term word="燃烧率" text="某时间窗口内消耗错误预算的倍速，用来区分瞬时抖动与持续劣化。" />
<Term word="On-call" text="轮值响应机制，接到页面告警后按预案止血、升级与记录。" />

告警最怕的不是漏报，而是噪声。半夜因为 CPU 高两分钟被叫醒，一周几次之后，人对告警就麻木了，真正的故障反而被划走。**基于症状而非原因告警**是第一条：CPU 高不一定影响用户，错误率飙升一定影响用户。第二条是**多窗口燃烧率**：只有短窗口和长窗口同时高倍消耗预算，才升级为呼叫；短时抖动只记工单，工作时间处理。

```go
package main

import "fmt"

// BurnRate 是某窗口内错误率相对 SLO 允许错误率的倍数
func BurnRate(allowed, observed float64) float64 {
	if allowed == 0 {
		return 0
	}
	return observed / allowed
}

// Alert 用长短两个窗口的燃烧率联合判定，抑制瞬时抖动造成的误报
func Alert(shortBurn, longBurn float64) string {
	// 快窗口高倍且慢窗口也高，才升级为呼叫；否则只记工单
	if shortBurn >= 14 && longBurn >= 6 {
		return "page：立即呼叫 On-call"
	}
	if shortBurn >= 6 || longBurn >= 3 {
		return "ticket：记录工单，工作时间内处理"
	}
	return "ok：不告警"
}

func main() {
	allowed := 0.001 // 99.9% SLO 允许的错误率
	cases := []struct {
		name        string
		shortWindow float64
		longWindow  float64
	}{
		{"瞬时抖动", 0.008, 0.001},
		{"持续劣化", 0.015, 0.008},
		{"正常", 0.0004, 0.0003},
	}
	for _, c := range cases {
		fmt.Printf("%s -> %s\n", c.name, Alert(BurnRate(allowed, c.shortWindow), BurnRate(allowed, c.longWindow)))
	}
}
```

对应的 Prometheus 规则也只看症状——一小时内错误率超过允许值的十四倍并持续两分钟，才触发页面告警：

```yaml
- alert: ErrorBudgetBurn
  expr: |
    sum(rate(http_requests_total{code=~"5.."}[1h]))
    / sum(rate(http_requests_total[1h])) > 14 * 0.001
  for: 2m
  labels:
    severity: page
```

<Callout type="warn" title="易错点" text="别把 SLO 写成 100%：那既不可能达到，也会让错误预算恒为零、永远不敢发版。也别把每个基础设施指标都做成告警，噪声会把真正影响用户的呼叫淹没掉。错误预算是承担风险的额度，不是允许摆烂的额度。" />

### 从 1 减去 SLO 推导错误预算，从燃烧率推导告警窗口

第 2 节把错误预算写成「1 减去目标再乘以总请求数」，现在把「为什么这个量能当发布闸门」推完整。设统计周期 `T_window`（通常 28 或 30 天滚动），周期内总请求 `Total`，允许失败率 `ε = 1 - SLO`。错误预算 `B = ε · Total` 的物理含义是：**在仍能满足 SLO 的前提下，最多还能失败多少次**。于是任一时刻的剩余额度 `B - Bad` 就是「还能冒多少险」的量化值，发布决策 `Decide(Bad/B)` 只是把它映射成动作。

燃烧率把「速度」也引进来。定义 `burn` 为观测错误率除以 `ε`：burn 等于 1 表示恰好按预算速度消耗，会在周期末刚好用完；burn 等于 14 表示按 14 倍速度烧，全部预算会在 `T_window / 14` 内耗尽——对 30 天窗口约 51 小时。多窗口告警的阈值就是这么来的：

| 窗口 | burn 阈值 | 含义 | 动作 |
| --- | --- | --- | --- |
| 短窗口 1h | 达到 14 | 约 2 天内烧光预算 | 联合长窗口升级为 page |
| 短窗口 6h | 达到 6 | 约 5 天内烧光 | ticket 或 page 的次级条件 |
| 长窗口 1d 或 3d | 达到 6 或 3 | 确认不是瞬时抖动 | 联合判定 |
| 单窗口高、长窗口正常 | 不升级 | 瞬时抖动 | 只记 ticket |

`for: 2m` 这一字段同样是推导结果：它要求条件连续成立 2 分钟才触发，等价于滤掉采样毛刺；没有它，一次单批 5xx 就会 page。第 2 节 `Alert` 函数里「短窗口达到 14 倍且长窗口达到 6 倍才升级为呼叫」的逻辑与这里完全一致——**必须长短窗口同时高倍**，因为单独一个短窗口高倍既可能是真故障，也可能只是 1 分钟抖动。

<SequenceDiagram title="错误预算燃烧驱动发布决策" caption="从指标采集到预算告警，再到冻结或灰度发布" :actors='["Prometheus","AlertManager","Oncall","SRE","Release"]' :steps='[{"from":"Prometheus","to":"Prometheus","label":"采集 SLI 滚动窗口","kind":"self"},{"from":"Prometheus","to":"AlertManager","label":"燃烧率越界","kind":"call"},{"from":"AlertManager","to":"Oncall","label":"长短窗口联合判定 page","kind":"call"},{"from":"Oncall","to":"Oncall","label":"按预案先止血","kind":"self"},{"from":"Oncall","to":"SRE","label":"上报预算消耗","kind":"call"},{"from":"SRE","to":"Prometheus","label":"查询剩余预算","kind":"call"},{"from":"Prometheus","to":"SRE","label":"返回已消耗 80%","kind":"return"},{"from":"SRE","to":"Release","label":"只放行修复与回滚","note":"暂停新功能","kind":"call"},{"from":"Release","to":"Release","label":"灰度并加强观测","kind":"self"},{"from":"SRE","to":"SRE","label":"复盘回灌容量模型","kind":"self"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 错误预算等于允许失败数 | SLI 口径统一：什么算一次请求、什么算失败，统计周期一致 | 口径不一致时数字无意义；把客户端超时算成功、把重试后的成功覆盖掉原失败，都会低估失败数 |
| 99.9% 对应每百万 1000 次 | 请求量在周期内大致稳定 | 流量突增时总请求数变大、预算随之变大，同样的故障次数占比变小；需按滚动窗口而非固定总量 |
| 燃烧率能区分抖动与劣化 | 长短窗口采样覆盖同一 SLI、无数据缺口 | 采集断流、Counter 重启归零、或窗口内请求量骤降，都会让燃烧率计算失真 |
| 症状告警优于原因告警 | 症状（错误率、延迟）与用户体感一致 | 某些影响用户的故障没有直接症状指标（数据错、慢查询但 HTTP 200），需要补业务级 SLI |
| 预算耗尽就冻结发布 | 组织真的把预算当决策依据 | 若每次都人工豁免，预算就退化成周报数字，失去约束力 |

范围说明：本讲讨论的是**稳定性度量与决策流程**，不涉及指标系统本身被篡改，也不把「SLO 达标」等同于「用户体验好」——SLO 是内部承诺，不是对外 SLA，更不是 100% 保证。另外，错误预算针对的是**可归因于变更的风险消耗**；把计划内维护、外部依赖故障也算进失败数，会让预算失去「还能冒多少险」的含义，需要事先约定哪些计入。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| SLO 定成 100% 最负责 | 100% 既不可能达到，也让错误预算恒为零，等于永远不敢发版 | SLO 是承担风险的额度，留出 1 减去 SLO 才能用预算驱动发布 |
| 达标了就说明没问题 | 第 2 节 99.958% 达标但已烧掉 42% 预算；只看达标会漏掉正在快速逼近上限 | 同时看达标判断与消耗比例，以及消耗速度，即燃烧率 |
| 告警越多越安全 | 噪声会积累告警疲劳，真正的故障反被划走 | 只对影响用户的症状告警，用长短窗口燃烧率降噪，区分 page 与 ticket |

## 4. 工程实践：从告警到治理闭环

<CodeTabs title="SLO 三件套" :tabs='[{"label":"SLO","code":"objective: 99.9","note":"目标值定下风险额度，不要定成 100"},{"label":"SLI","code":"good / total requests","note":"口径先统一，成功与失败的判定要可复现"},{"label":"预算","code":"1 - objective = 0.001","note":"一百万次请求允许一千次失败，是可分配的额度"}]' />

<Compare title="告警该看谁" :panels='[{"label":"原因告警","code":"alertOn(cpuHigh)","note":"CPU 高未必影响用户，容易半夜误报、积累告警疲劳"},{"label":"症状告警","code":"alertOn(errorBudgetBurnRate)","note":"只对影响用户的错误预算燃烧率呼叫，长短窗口联合降噪"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"定义 SLO","desc":"按用户体感定目标与统计口径","tone":"blue"},{"title":"持续测量","desc":"采集 SLI，滚动计算预算消耗","tone":"blue"},{"title":"预算告警","desc":"燃烧率越界，区分 page 与 ticket","tone":"red"},{"title":"决策发布","desc":"按剩余预算决定灰度或冻结","tone":"green"},{"title":"复盘回灌","desc":"压测找拐点，行动项回灌容量模型","tone":"green"}]' />

容量规划是这套闭环的另一端：按峰值流量与 SLO 反推需要多少副本，再用压测找到性能拐点，确认限流阈值与预案在真实压力下成立。压测不是跑一次出个数字，而是验证「流量翻三倍时，系统仍能满足 SLO 或按预算内的方式降级」。故障发生后，On-call 按预案先止血、再恢复、后定位，复盘只追问三件事：监控有没有先于用户发现、预案有没有生效、暴露了哪个此前不知道的隐藏依赖，最后把行动项回灌到容量模型与告警规则里。

这样，错误预算就不只是一个百分比，而是一整套可执行的判断链：它决定发布能不能继续、告警该不该叫人、容量该留多少、复盘该改什么。稳定性从「出事救火」变成「按额度管理风险」，这正是本阶段从分布式一路走到云原生的落点。

### 可验证实验：Prometheus 查询加 vegeta 压测，逐字段读输出

把第 3 节那条 PromQL 真正查一遍，再用 vegeta 灌流量验证「预算消耗速度」随错误率变化：

```bash
# 直接查询 1 小时错误率
curl -s 'http://prometheus:9090/api/v1/query' \
  --data-urlencode 'query=sum(rate(http_requests_total{code=~"5.."}[1h]))/sum(rate(http_requests_total[1h]))' | jq '.data.result[0].value'
# 查询 30 天窗口内累计错误率，用于折算预算消耗
curl -s 'http://prometheus:9090/api/v1/query' \
  --data-urlencode 'query=1 - sum(increase(http_requests_total{code!~"5.."}[30d]))/sum(increase(http_requests_total[30d]))' | jq '.data.result[0].value'
# 压测：稳定速率下观察 P99 与成功率
echo "GET http://myservice.internal/" | vegeta attack -rate=200 -duration=60s | vegeta report
```

```text
$ curl -s ... | jq '.data.result[0].value'
[
  1735689600,
  "0.00042"
]
$ curl -s ... | jq '.data.result[0].value'
[
  1735689600,
  "0.00058"
]
$ vegeta report
Requests      [total, rate, throughput]         12000, 200.00, 199.60
Duration      [total, attack, wait]             60s, 60s, 120ms
Latencies     [min, mean, 50, 90, 95, 99, max] 1ms, 8ms, 6ms, 18ms, 27ms, 63ms, 210ms
Success       [ratio]                           99.80%
Status Codes  [code:count]                     200:11976  503:24
```

逐字段解读：第一个查询返回 Prometheus 的时间戳与值二元组，`1735689600` 是 Unix 时间戳，`0.00042` 是当前 1 小时错误率；与允许值 `ε=0.001` 相比燃烧率为 0.42，远低于第 3 节 `Alert` 的阈值，故不会告警。第二个查询得到 30 天累计错误率 `0.00058`，除以 `ε=0.001` 即已消耗约 58% 预算，代入第 2 节 `Decide(58)` 应落在「预算过半：发布需走灰度并加强观测」这一档。`vegeta report` 里 `rate=200.00` 是目标压力，`throughput=199.60` 是实际成功吞吐，差额来自 `503:24`；`Success 99.80%` 与 `Status Codes 200:11976` 自洽；`99=63ms` 是 P99 延迟，压测时要确认它仍在 SLO 的延迟目标内，否则即便成功率达标也不算满足 SLO——这正是「SLI 不止一个」的实测提醒。把 `-rate` 逐步抬到系统拐点，记录 P99 开始陡增、成功率开始下滑的那一级，就是容量规划里该留的余量。

<Quiz q="多窗口燃烧率告警里，什么情况升级为 page 呼叫？" :options='["任一短窗口燃烧率高倍","长短窗口同时高倍","长窗口正常但短窗口高","CPU 使用率高"]' answer="1" explain="长短窗口同时高倍才说明是持续劣化而非瞬时抖动；单窗口高倍多为抖动，只记工单，避免告警疲劳。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚或止血 |
| --- | --- | --- | --- | --- |
| 5xx 错误率 14 倍燃烧 | 预算在约 2 天内烧光 | 长短窗口 page | 用 `vegeta` 灌入 5xx 或 chaos 注入下游故障 | 熔断、降级、回滚上一版本 |
| 单分钟抖动 | 短窗口高、长窗口正常 | 多窗口判定，只记 ticket | 注入 60 秒脉冲错误 | 无需止血，工作时间排查 |
| Counter 重启归零 | increase 计算失真，燃烧率异常 | 用 rate 或 increase 并处理重置、指标持久化 | 重启被监控实例观察曲线 | 修正查询口径 |
| 指标采集断流 | 无数据，告警既不响也不静 | 采集链路自身告警 | 停掉 Prometheus scrape | 恢复采集，补算窗口 |
| 预算已耗尽仍发新功能 | 风险叠加，可能突破 SLO | 决策闸加流程约束 | 在预发把消耗比例设为 100% 看发布是否被拦 | 冻结变更，转去修稳定性 |
| 压测把生产打挂 | 真实用户受影响 | 只在预发或隔离环境压测，限定爆炸半径 | 预发灌流量找拐点 | 立即停止压测 |

演练闭环：先在预发用 `vegeta` 加 chaos 组合注入一次可控故障，验证监控确实先于用户发现、告警按 page 与 ticket 分级正确、发布闸门真的挡住了发版；再记录实际 RTO 与本次消耗的预算，最后把行动项回灌到容量模型与告警阈值。压测与混沌都要限定爆炸半径，别把演练做成真实事故——这一点与第 89 讲一致。

### 替代路线与取舍

| 路线 | 判断依据 | 降噪能力 | 代价 | 适合 |
| --- | --- | --- | --- | --- |
| 症状加多窗口燃烧率告警 | 错误预算消耗速度 | 强，区分 page 与 ticket | 需统一 SLI 口径与 30 天窗口数据 | 有成熟监控的中大型服务 |
| 固定阈值告警 | 单点阈值 | 弱，易噪声 | 实现最简单 | 小系统、指标少 |
| 纯 SLA 合规视角 | 对外承诺百分比 | 无风险额度概念 | 无法驱动发布节奏 | 对客合同约束 |
| 无预算、按发布窗口或人工评审 | 经验与排期 | 不适用 | 最灵活但不可量化 | 早期产品、低流量服务 |
| 基于请求成功率的单一 SLI | 成功率 | 中 | 漏掉延迟类劣化 | 只关心可用性的简单接口 |

取舍主线：**量化程度，对建设成本**。错误预算不是越早越好——SLI 口径没统一、采集不满一个完整周期之前，算出来的预算没有意义。务实路径是先固定阈值告警保底，再补齐 SLI 口径与滚动窗口，最后才把发布决策接进发布流程。

### 与后续实战的衔接

- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 的「指标与日志聚合」「告警规则」正是本讲 PromQL 与燃烧率规则的工程落地：把 SLO 写成 CRD，让 Reconcile 循环持续核对预算消耗。
- [秒杀与高并发库存](/projects/micro-seckill) 的「压测与预案」会复用本讲的 vegeta 加压、找 P99 拐点、验证限流阈值这套流程，并把结果回灌到容量模型。
- [Helm / GitOps / 灰度发布](/distributed/gitops) 的金丝雀分析指标（错误率、P99、饱和度）与本讲的 SLI 直接对接：第 92 讲「权重归零」的触发条件，就是这里「燃烧率越界」在发布侧的动作。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-093-sre`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-093-sre)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="算出预算并给出决策" task="某服务 SLO 为 99.9%，一个月共 1,000,000 次请求，已失败 420 次。请算出错误预算、已消耗比例，并给出发布决策。" :hints='["错误预算等于总请求数乘以允许失败比例","把消耗比例代入 Decide 的四档规则"]' solution="错误预算 = 总请求数乘以允许的失败比例：\n1000000 * 0.001 = 1000 次失败；\n已用 420 次，消耗 42%，预算仍充足可正常发版；\n若消耗到 100% 就冻结非必要变更，优先修稳定性" explain="把 SLO 折算成具体失败次数，再由消耗比例触发发布、灰度或冻结，稳定性就变成了可执行决策。" />

## 随堂检测
<Quiz no="93-1" q="某服务错误预算已消耗 100%，按 SLO 驱动的正确动作是？" :options='["照常发布新功能","冻结非必要变更，优先修稳定性","把 SLO 调低以恢复预算","增加告警数量"]' answer="1" explain="预算耗尽意味着本月风险额度用完，应停止承担变更风险，把人力转向稳定性修复。" />
<Predict no="93-2" q="短窗口燃烧率很高、长窗口燃烧率正常，多窗口告警规则会怎么判？" :options='["立即呼叫 On-call","只记工单，不半夜叫人","不告警","重启服务"]' answer="1" output="只记工单，不半夜叫人" explain="长短窗口同时高倍才升级为 page，单窗口高倍多为瞬时抖动，记工单即可，避免告警疲劳。" />
<FillBlank no="93-3" tpl="可测量的指标叫 ___；指标的目标值叫 ___；由 1 减去目标得到的允许失败额度叫 ___。" :answers='["SLI","SLO","错误预算"]' hint="指标、目标、额度三个层次" explain="SLI 是测量值，SLO 是目标，错误预算把目标折算成可分配的风险额度。" :explains='["SLI 是对服务质量的实际测量，例如成功请求占比或延迟分位数；它先回答「测什么、当前是多少」，是设目标的前提。","SLO 是给 SLI 设定的目标区间，例如成功率不低于 99.9%；它把主观的「够不够好」变成可考核、可对外承诺的阈值。","用 1 减去 SLO 得到允许失败的比例，如 0.1% 折算成预算；发布变更消耗它，超支就暂停上线，把稳定性量化成资源。"]' />

## 一页纸速记
<KnowledgeCard :points='["SLI 测量、SLO 定目标、错误预算折算成允许失败数","按剩余预算决定正常发布、灰度、只修不放或冻结变更","症状告警加长短窗口燃烧率，区分 page 与 ticket 来降噪","容量规划、压测预案、On-call 复盘回灌，构成治理闭环"]' :pitfalls='["把 SLO 定成 100%，预算恒为零导致永远不敢发布","对每个基础设施指标都告警，噪声淹没真正影响用户的呼叫","只报达标与否，不看预算消耗速度，错过快速逼近上限的信号"]' summary="SLO 与错误预算的价值，是把「稳不稳」变成「现在该做什么」的可执行决策。" />

## 小结与下一讲

- SLI 是测量值、SLO 是目标、错误预算是允许失败额度，三者把稳定性量化。
- 剩余预算直接驱动发布决策：充足发版、过半灰度、紧张只修、耗尽冻结。
- 症状告警配长短窗口燃烧率降噪，容量、压测、On-call 与复盘把闭环补齐。

到这里，从 CAP 的取舍一路走到 SLO 的治理，分布式与云原生这条线就完整了。下一阶段将下沉到 Go 运行时与编译器源码，去看这些能力所依赖的语言底座究竟如何运作。

[下一讲：资深进阶与源码](/advanced/)
