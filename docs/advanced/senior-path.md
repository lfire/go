---
title: 第 102 讲 · 资深能力图谱、技术选型与持续精进
description: 用能力四维模型、加权选型方法与持续输出路径，把九个阶段的知识变成可持续的职业能力
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★☆☆" time="40 分钟" goal="把知识变成可持续的职业能力" prereq="第 94–101 讲 · 源码、性能与架构全部讲次" />

# 第 102 讲 · 资深能力图谱、技术选型与持续精进

> 走到这一讲，你已经读过 runtime 源码、做过性能优化、拆过模块边界。但「学过」和「能稳定地产出判断」之间还差一层：把零散知识组织成能力模型，把凭感觉的选型变成可解释的方法，把个人理解变成团队资产。本讲收束整个阶段，给出四维能力图谱、加权选型步骤与持续精进路径，并用三段可运行代码把方法落地。

## 这一讲你会得到什么
<CheckList id="L9-102" title="本讲目标" :items='["用能力四维模型给自己定位短板", "用加权矩阵做可解释的技术选型", "用输出倒逼输入，形成长期精进闭环"]' />

## 1. 先建立直觉
能力像系统架构一样分层：下层是语言与源码理解，决定你能看多深；往上是工程质量，决定你能不能稳定交付；再往上是系统与架构，决定你能不能在变化中做对取舍；最上层是影响力，决定你的判断能放大到多少人。下层不牢，上层的判断就成了空中楼阁；只停在底层，又容易变成「什么都懂一点、却推不动事情」的人。
<ConceptDiagram type="architecture-layer" title="资深能力分层图谱" labels="语言与源码::看得懂、写得出|工程质量::可测可维护|系统架构::分布式与稳定性|技术影响力::选型与输出" caption="能力像架构一样分层：下层是地基，上层放大下层的影响" />

## 2. 语法与规则
三段程序分别实现加权选型矩阵、四维自评、以及输出记录的统计。

```go
package main

import "fmt"

type Option struct {
	Name   string
	Scores map[string]float64
}

// 加权评分：先明确约束与权重，再算总分，避免凭熟悉度选型。
func main() {
	weights := map[string]float64{
		"性能":    0.3,
		"可维护性": 0.3,
		"生态":    0.2,
		"团队熟悉度": 0.2,
	}
	candidates := []Option{
		{Name: "方案 A", Scores: map[string]float64{"性能": 5, "可维护性": 3, "生态": 4, "团队熟悉度": 5}},
		{Name: "方案 B", Scores: map[string]float64{"性能": 3, "可维护性": 5, "生态": 5, "团队熟悉度": 3}},
	}
	for _, c := range candidates {
		total := 0.0
		for k, w := range weights {
			total += c.Scores[k] * w
		}
		fmt.Printf("%s 加权得分 = %.2f\n", c.Name, total)
	}
}
```

```go
package main

import "fmt"

type Dim struct {
	Name  string
	Score int
}

// 四维自评：找出最弱一维，把学习计划集中投在它上面。
func main() {
	dims := []Dim{
		{"语言与源码", 4},
		{"工程质量", 3},
		{"系统与架构", 3},
		{"影响力与协作", 2},
	}
	total := 0
	weakest := dims[0]
	for _, d := range dims {
		total += d.Score
		if d.Score < weakest.Score {
			weakest = d
		}
		fmt.Printf("%s = %d 分\n", d.Name, d.Score)
	}
	fmt.Println("平均分 =", float64(total)/float64(len(dims)))
	fmt.Println("最弱维度 =", weakest.Name)
}
```

```go
package main

import "fmt"

type Entry struct {
	Kind  string
	Title string
}

// 输出台账：把源码笔记、分享、PR、复盘都记下来，形成可回顾的复利。
func main() {
	log := []Entry{
		{"源码笔记", "runtime/proc.go 调度循环"},
		{"分享", "GMP 与异步抢占"},
		{"PR", "修复一处数据竞争"},
		{"复盘", "一次 p99 抖动"},
		{"分享", "GOGC 调优闭环"},
	}
	counts := map[string]int{}
	for _, e := range log {
		counts[e.Kind]++
	}
	for _, k := range []string{"源码笔记", "分享", "PR", "复盘"} {
		fmt.Println(k, counts[k])
	}
	fmt.Println("累计输出条数 =", len(log))
}
```

<OutputBox title="第三段程序的输出" output="源码笔记 1\n分享 2\nPR 1\n复盘 1\n累计输出条数 = 5" note="按固定顺序遍历 key，输出稳定可复现；真实台账可以换成读文件或数据库。" />

<LineByLine title="一次技术选型的四个动作" code="列约束与候选\n给维度定权重\n小范围验证\n记录成 ADR" :notes='["先把性能、成本、期限、团队能力这些硬约束写清楚", "按本次目标给各维度分配权重，而不是套用上次的比例", "用最小原型压测或试点，验证评分里最不确定的那一项", "把结论与放弃的理由写成决策记录，便于日后复盘"]' />

## 3. 深入一步
四维模型不是逐项刷满，而是定位「当前最该补的那一维」。判断标准各异：语言与源码看能否复现运行时行为；工程质量看测试与 p99 报告；系统架构看 ADR 与重构记录；影响力看评审与分享。选型同理，第一步永远是把约束写清楚，权重服务于本次目标。

<Term word="ADR" text="Architecture Decision Record，架构决策记录：把背景、备选方案、选定结论与放弃理由固化成短文档，让取舍可追溯。" />

| 维度 | 要回答的问题 | 可验证的产物 |
| --- | --- | --- |
| 语言与源码 | 能否解释并复现运行时行为 | 源码笔记、可复现实验 |
| 工程质量 | 能否稳定交付 | 测试、CI、p99 报告 |
| 系统与架构 | 能否在变化中演进 | ADR、拆分方案、重构记录 |
| 影响力与协作 | 能否放大团队产出 | 评审、分享、开源贡献 |

<Callout type="tip" title="输出倒逼输入" text="写清一篇源码笔记会逼你补齐没读懂的细节；做一次分享会逼你把结论组织成别人能反驳的形式。把输出当成学习闭环的验证环节，比多读几章更扎实。" />

## 4. 工程实践
把成长做成节奏：每周一条源码笔记，每月一次分享，每个项目留一条 ADR 与复盘；参与开源从修文档、补测试、报可复现 issue 开始，再逐步提交 PR。选型坚持「列约束、定权重、小步验证」，并把判断结果回写到权重经验里。

```bash
go test -race ./... && go vet ./...
```

<Compare title="选型的两种做法" :panels='[{"label":"写法 A：凭感觉","code":"pick(newestLib)\nship()","note":"以新潮程度代替约束分析，出问题难复盘"},{"label":"写法 B：加权加试点","code":"score(weights, candidates)\npilot(best, 5)\nwriteADR()","note":"结论可解释、可小步验证，决策记录让后续复盘有据可查"}]' />

## 动手练一练
<Exercise title="做一次四维自评" task="给第二段程序补上「按分数排序输出」的功能，并打印最弱维度建议投入的学习动作。" :hints='["用 sort.Slice 对 dims 排序", "建议动作可用 map 按维度名查表"]' solution="sort.Slice(dims, byScore)\nfmt.Println(dims[0].Name)\nfmt.Println(actions[dims[0].Name])" explain="排序后首项即最弱维度；建议动作查表后，自评直接转成学习计划。完整代码见第二段。" />

## 随堂检测
<Quiz no="102-1" q="做技术选型时，第一步最应该做什么？" :options='["列候选框架 star 数", "明确硬约束与本次目标", "选团队最熟悉的", "先写完代码再看"]' answer="1" explain="约束与目标决定权重；跳过这一步，后面的评分只是在给偏好找理由。" />
<Predict no="102-2" q="第二段程序里影响力与协作得 2 分，会输出哪个最弱维度？" :options='["语言与源码","工程质量","影响力与协作"]' answer="2" output="最弱维度 = 影响力与协作" explain="程序取 Score 最小的 Dim，2 分最低，因此定位到影响力与协作。" />

## 一页纸速记
<KnowledgeCard :points='["四维：语言源码、工程质量、系统架构、影响力", "选型先列约束，再定权重，小步验证", "输出倒逼输入，ADR 固化取舍"]' :pitfalls='["用流行度替代约束分析", "只补最舒服的一维", "判断不留记录"]' summary="资深不是知道更多，而是把判断变成可解释、可复现、可传递的方法。" />

## 小结与下一讲
1. 能力分四层，用自评找最弱一维并集中投入，别平均用力。
2. 技术选型按「约束、权重、试点、ADR」四步走，让结论可解释可复盘。
3. 用笔记、分享、PR 与复盘构成输出闭环，把个人理解沉淀成团队资产。

九个阶段到这里收束，接下来把全部知识放进真实场景反复用熟。

[下一讲：实战 01 · 待办清单 CLI](/projects/)
