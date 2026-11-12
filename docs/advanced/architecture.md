---
title: 第 101 讲 · 架构设计：模块拆分、演进与重构
description: 按变化方向识别边界，用依赖倒置与绞杀者重构让系统在变化中保持可演进
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="55 分钟" goal="在变化中保持系统可演进" prereq="第 50 讲 · 项目结构与分层设计 / 第 58 讲 · 设计模式" />

# 第 101 讲 · 架构设计：模块拆分、演进与重构

> 第 50 讲给过一套项目骨架，第 58 讲讲过解耦手法，但真正难的是「业务变了之后，改一处还是要改十处」。架构设计的核心不是画漂亮的分层图，而是识别变化方向、把易变的部分隔离在边界后面，并让替换可以小步进行。本讲讲模块拆分依据、依赖倒置，以及生产里真正可用的绞杀者重构，并用可运行代码演示三个关键动作。

## 这一讲你会得到什么
<CheckList id="L9-101" title="本讲目标" :items='["按变化方向而不是按技术层划分模块边界", "用接口把领域层与基础设施层的依赖倒过来", "用绞杀者门面与双写校验做低风险重构"]' />

## 1. 先建立直觉
把系统想成不断加房间的楼。每加一间都要动承重墙，说明边界划错了；好的边界让新房间只接一根水管、一根电线。「识别变化方向」就是先问：哪些东西最可能一起变？把一起变的放同一模块，把稳定与易变的隔开。依赖倒置则像预埋标准插座：领域层只认接口，数据库、缓存、MQ 都是可插拔的插头。
<ConceptDiagram type="architecture-layer" title="分层与依赖倒置" caption="业务规则不依赖具体技术，替换实现不动核心" />

## 2. 语法与规则
三段程序分别演示领域层定义接口、绞杀者门面按灰度切流、以及双写校验确认等价。

```go
package main

import "fmt"

// 领域层只定义接口，不 import 任何具体存储实现。
type OrderRepo interface {
	Save(id string, amount int) error
}

// 基础设施层实现接口。
type MemRepo struct{ data map[string]int }

func (m *MemRepo) Save(id string, amount int) error {
	m.data[id] = amount
	return nil
}

// 应用层依赖抽象，而非具体实现。
type Service struct{ repo OrderRepo }

func (s *Service) Create(id string, amount int) error {
	return s.repo.Save(id, amount)
}

func main() {
	svc := Service{repo: &MemRepo{data: map[string]int{}}}
	if err := svc.Create("o-1", 99); err != nil {
		panic(err)
	}
	fmt.Println("依赖倒置装配完成")
}
```

```go
package main

import "fmt"

type Handler interface{ Handle(string) string }

type Legacy struct{}

func (Legacy) Handle(s string) string { return "legacy:" + s }

type Modern struct{}

func (Modern) Handle(s string) string { return "modern:" + s }

// 绞杀者门面：按 key 的哈希灰度，把一部分流量导向新实现。
type Facade struct {
	legacy    Handler
	modern    Handler
	modernPct int
}

func (f Facade) Handle(key string) string {
	if hash(key)%100 < f.modernPct {
		return f.modern.Handle(key)
	}
	return f.legacy.Handle(key)
}

func hash(s string) int {
	h := 0
	for i := range s {
		h = h*31 + int(s[i])
	}
	return h
}

func main() {
	f := Facade{legacy: Legacy{}, modern: Modern{}, modernPct: 25}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		fmt.Println(k, f.Handle(k))
	}
}
```

```go
package main

import "fmt"

type Price interface{ Get(sku string) int }

type OldPrice struct{}

func (OldPrice) Get(sku string) int { return len(sku) * 10 }

type NewPrice struct{}

func (NewPrice) Get(sku string) int { return len(sku) * 10 }

// 双写校验：重构中期同时调用新旧实现并逐条比对，确认等价后再放量。
func main() {
	old, next := OldPrice{}, NewPrice{}
	mismatch := 0
	for _, sku := range []string{"a", "bb", "ccc", "dddd"} {
		o, n := old.Get(sku), next.Get(sku)
		if o != n {
			mismatch++
		}
		fmt.Println(sku, o, n)
	}
	fmt.Println("不一致数 =", mismatch)
}
```

<OutputBox title="第三段程序的输出" output="a 10 10\nbb 20 20\nccc 30 30\ndddd 40 40\n不一致数 = 0" note="不一致数为 0 才具备继续放量的条件；一旦出现差异，先停下来定位，而不是提高灰度比例。" />

<LineByLine title="绞杀者重构的四个阶段" code="门面路由新旧实现\n双写并比对结果\n逐步提高灰度比例\n下线旧实现" :notes='["在调用方与实现之间加一层门面，先把流量拦下来", "同一请求同时走新旧两条路径，逐条比对差异", "确认等价后按比例放量，随时可一键回切", "新实现稳定后再删除旧代码与兼容分支"]' />

## 3. 深入一步
拆分依据决定改动的传播范围。按技术层拆上手快，但一个业务变更要横跨三层；按领域拆把同一概念的数据与规则放一起，变化被局部化，代价是必须先识别限界上下文。依赖倒置让边界成立：领域层声明接口，基础设施层去实现，编译期依赖统一指向抽象。

<Term word="绞杀者模式" text="在旧系统外围逐步长出新实现，用路由一点点接管功能，旧系统逐渐失去职责直到可以安全下线。" />

| 拆分依据 | 优点 | 代价 | 适用场景 |
| --- | --- | --- | --- |
| 按技术层 | 上手快、目录直观 | 一个需求跨多层改 | 早期原型、小工具 |
| 按领域上下文 | 变化被局部化 | 需要先识别边界 | 长期演进的核心系统 |
| 按变更频率 | 稳定与易变隔离 | 需要持续度量 | 高频迭代模块 |
| 按团队归属 | 职责清晰 | 易形成信息孤岛 | 多团队协作 |

<Callout type="warn" title="别搞大爆炸重写" text="一次性重写常在「新系统没对齐旧行为」与「旧系统还在加需求」之间被拖垮。绞杀者每一步都可回切、可验证；边界要按变化方向划，别按数据库表划。" />

## 4. 工程实践
取舍要落进文档：每个重要边界写一条 ADR，记下背景、备选、结论与放弃理由。重构期间把灰度比例、双写不一致数、回切开关做成指标，并约定下线旧代码的条件，避免兼容分支无限期留存。

```bash
go list -deps ./internal/order
```

<Compare title="重构方式对照" :panels='[{"label":"写法 A：整体重写","code":"stopOld()\nbuildNew()\nswitchAll()","note":"一次性切换，行为对齐难、无法回切"},{"label":"写法 B：绞杀者","code":"f := Facade{modernPct: 5}\nf = Facade{modernPct: 50}\nf = Facade{modernPct: 100}","note":"按灰度小步接管，配合双写校验，随时可回切到旧实现"}]' />

## 动手练一练
<Exercise title="写一条架构决策记录" task="为「订单存储换成持久化」写一条 ADR，列出两个备选、结论与放弃理由。" :hints='["背景先写清约束，如数据量与恢复要求", "放弃的理由比结论更重要"]' solution="背景 := 单实例内存存储无法容灾\n备选 := []string{内存分片, 持久化 KV}\n结论 := 持久化 KV\n理由 := 需要故障恢复与多副本" explain="ADR 把取舍固化下来，日后回看能知道当时的约束。完整结构可用同样的字段列表表达。" />

## 随堂检测
<Quiz no="101-1" q="依赖倒置在 Go 里最直接的体现是？" :options='["下层包 import 上层包", "领域层定义接口，基础设施层实现", "所有类型放进一个包", "用全局变量传依赖"]' answer="1" explain="接口由使用方定义，实现方依赖它，编译期依赖方向统一指向抽象。" />
<Predict no="101-2" q="双写校验出现「不一致数大于 0」，正确做法是？" :options='["提高灰度到 100%", "暂停放量并定位差异", "跳过校验，新实现更快"]' answer="1" output="暂停放量并定位差异" explain="双写就是在放量前发现行为不等价；带病放量会把差异放大成线上故障。" />

## 一页纸速记
<KnowledgeCard :points='["边界按变化方向划", "领域层定义接口实现依赖倒置", "绞杀者：门面、双写、灰度、下线"]' :pitfalls='["一次性大爆炸重写", "按数据库表划模块", "兼容分支不下线"]' summary="好架构让每次变化都只落在最小范围里。" />

## 小结与下一讲
1. 先识别哪些东西会一起变，再据此划模块边界，稳定与易变要隔离。
2. 领域层声明接口、基础设施层实现，依赖方向统一指向抽象。
3. 重构用绞杀者门面加双写校验小步接管，配合 ADR 与可回切开关控制风险。

[下一讲：资深能力图谱、技术选型与持续精进](/advanced/senior-path)
