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

## 4. 工程实践：从告警到治理闭环

<CodeTabs title="SLO 三件套" :tabs='[{"label":"SLO","code":"objective: 99.9","note":"目标值定下风险额度，不要定成 100"},{"label":"SLI","code":"good / total requests","note":"口径先统一，成功与失败的判定要可复现"},{"label":"预算","code":"1 - objective = 0.001","note":"一百万次请求允许一千次失败，是可分配的额度"}]' />

<Compare title="告警该看谁" :panels='[{"label":"原因告警","code":"alertOn(cpuHigh)","note":"CPU 高未必影响用户，容易半夜误报、积累告警疲劳"},{"label":"症状告警","code":"alertOn(errorBudgetBurnRate)","note":"只对影响用户的错误预算燃烧率呼叫，长短窗口联合降噪"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"定义 SLO","desc":"按用户体感定目标与统计口径","tone":"blue"},{"title":"持续测量","desc":"采集 SLI，滚动计算预算消耗","tone":"blue"},{"title":"预算告警","desc":"燃烧率越界，区分 page 与 ticket","tone":"red"},{"title":"决策发布","desc":"按剩余预算决定灰度或冻结","tone":"green"},{"title":"复盘回灌","desc":"压测找拐点，行动项回灌容量模型","tone":"green"}]' />

容量规划是这套闭环的另一端：按峰值流量与 SLO 反推需要多少副本，再用压测找到性能拐点，确认限流阈值与预案在真实压力下成立。压测不是跑一次出个数字，而是验证「流量翻三倍时，系统仍能满足 SLO 或按预算内的方式降级」。故障发生后，On-call 按预案先止血、再恢复、后定位，复盘只追问三件事：监控有没有先于用户发现、预案有没有生效、暴露了哪个此前不知道的隐藏依赖，最后把行动项回灌到容量模型与告警规则里。

这样，错误预算就不只是一个百分比，而是一整套可执行的判断链：它决定发布能不能继续、告警该不该叫人、容量该留多少、复盘该改什么。稳定性从「出事救火」变成「按额度管理风险」，这正是本阶段从分布式一路走到云原生的落点。

## 动手练一练
<Exercise title="算出预算并给出决策" task="某服务 SLO 为 99.9%，一个月共 1,000,000 次请求，已失败 420 次。请算出错误预算、已消耗比例，并给出发布决策。" :hints='["错误预算等于总请求数乘以允许失败比例","把消耗比例代入 Decide 的四档规则"]' solution="错误预算 = 总请求数乘以允许的失败比例：\n1000000 * 0.001 = 1000 次失败；\n已用 420 次，消耗 42%，预算仍充足可正常发版；\n若消耗到 100% 就冻结非必要变更，优先修稳定性" explain="把 SLO 折算成具体失败次数，再由消耗比例触发发布、灰度或冻结，稳定性就变成了可执行决策。" />

## 随堂检测
<Quiz no="93-1" q="某服务错误预算已消耗 100%，按 SLO 驱动的正确动作是？" :options='["照常发布新功能","冻结非必要变更，优先修稳定性","把 SLO 调低以恢复预算","增加告警数量"]' answer="1" explain="预算耗尽意味着本月风险额度用完，应停止承担变更风险，把人力转向稳定性修复。" />
<Predict no="93-2" q="短窗口燃烧率很高、长窗口燃烧率正常，多窗口告警规则会怎么判？" :options='["立即呼叫 On-call","只记工单，不半夜叫人","不告警","重启服务"]' answer="1" output="只记工单，不半夜叫人" explain="长短窗口同时高倍才升级为 page，单窗口高倍多为瞬时抖动，记工单即可，避免告警疲劳。" />
<FillBlank no="93-3" tpl="可测量的指标叫 ___；指标的目标值叫 ___；由 1 减去目标得到的允许失败额度叫 ___。" :answers='["SLI","SLO","错误预算"]' hint="指标、目标、额度三个层次" explain="SLI 是测量值，SLO 是目标，错误预算把目标折算成可分配的风险额度。" />

## 一页纸速记
<KnowledgeCard :points='["SLI 测量、SLO 定目标、错误预算折算成允许失败数","按剩余预算决定正常发布、灰度、只修不放或冻结变更","症状告警加长短窗口燃烧率，区分 page 与 ticket 来降噪","容量规划、压测预案、On-call 复盘回灌，构成治理闭环"]' :pitfalls='["把 SLO 定成 100%，预算恒为零导致永远不敢发布","对每个基础设施指标都告警，噪声淹没真正影响用户的呼叫","只报达标与否，不看预算消耗速度，错过快速逼近上限的信号"]' summary="SLO 与错误预算的价值，是把「稳不稳」变成「现在该做什么」的可执行决策。" />

## 小结与下一讲

- SLI 是测量值、SLO 是目标、错误预算是允许失败额度，三者把稳定性量化。
- 剩余预算直接驱动发布决策：充足发版、过半灰度、紧张只修、耗尽冻结。
- 症状告警配长短窗口燃烧率降噪，容量、压测、On-call 与复盘把闭环补齐。

到这里，从 CAP 的取舍一路走到 SLO 的治理，分布式与云原生这条线就完整了。下一阶段将下沉到 Go 运行时与编译器源码，去看这些能力所依赖的语言底座究竟如何运作。

[下一讲：资深进阶与源码](/advanced/)
