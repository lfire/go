---
title: 第 83 讲 · CAP / BASE 与一致性模型
description: 建立分布式系统的判断框架，理解网络分区下一致性与可用性的真实取舍
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="45 分钟" goal="建立分布式系统的判断框架，能对每种一致性方案回答中途失败会怎样" prereq="RPC 与微服务" />

# 第 83 讲 · CAP / BASE 与一致性模型

> 单机程序里，一次写入要么成功要么失败；一旦数据复制到多台机器，网络分区、机器宕机、时钟漂移就成了常态。CAP 与 BASE 不是需要背诵的缩写，而是一套判断框架：当故障发生在「写入到一半」时，系统究竟该返回错误、返回旧数据，还是硬撑着继续服务？本讲建立这套框架，让你在后面每一讲的一致性方案里，都能先问一句「中途失败会怎样」。

## 这一讲你会得到什么
<CheckList id="goal-83" title="本讲目标" :items='["准确说出 CAP 三个字母的含义，并纠正三选二的常见误读","用 N/W/R 仲裁参数把一致性变成可计算、可配置的量","区分强一致、因果一致与最终一致，说明分区时各自的表现","用 BASE 与读修复解释最终一致系统如何收敛"]' />

## 1. 先建立直觉
<ConceptDiagram type="cap" title="CAP：分区发生时只能在 C 与 A 之间取舍" />

设想三家连锁店共用一本账，店与店之间靠电话同步。电话线突然断了，东城店和西城店各自还在开门营业：如果规定「账不一致就停业核对」，你保住了账目正确，却损失了营业时间；如果规定「照常营业、各记各的」，你保住了可用性，但复线之后两本账必然对不上。这就是 CAP 要刻画的两难。

三个字母的准确含义要先立住：**C** 指线性一致性，即任何时刻所有节点读到同一份最新数据，读操作像发生在某个确定的瞬间；**A** 指每个未故障节点的请求都能在有限时间内拿到响应，但不承诺这个响应是最新的；**P** 指网络出现丢包、延迟甚至分区时，系统仍能继续运行。

最常被念错的一点是：P 根本不是可以「放弃」的选项。只要数据放在多台机器上，分区就是客观存在的前提，你无法用设计把它抹掉。CAP 真正说的是——**分区没有发生时，C 与 A 可以同时满足；分区发生的那段窗口里，必须在 C 与 A 之间二选一**。所谓「三选二」把 P 当成了可选开关，是对定理的误读。

顺着「面向故障设计」再追问一句：坚持 C，就得让拿不到多数派确认的一侧停止写入，代价是一次请求失败；坚持 A，两侧各自读写，恢复后必然出现冲突版本，必须靠版本号与收敛机制收拾残局。两种代价都真实存在，区别只在于你愿意先付哪一笔。

## 2. 语法与规则

把「一致性」从口号变成可计算的参数，最经典的抓手是 N、W、R 仲裁：N 是副本总数，W 是一次写入需要确认的副本数，R 是一次读取要查询的副本数。当 `R + W > N` 且 `W > N/2` 同时成立时，读集合与写集合必有交集，且不会出现两个并发写各自成功，于是读必能见到最新写入。

```go
package main

import "fmt"

// Quorum 描述一份数据的副本分布与读写所需确认数
type Quorum struct {
	N int // 副本总数
	W int // 一次写入需要的确认副本数
	R int // 一次读取需要查询的副本数
}

// StrongConsistent 用 R + W > N 与 W > N/2 判断读写是否满足强一致。
// 两个条件同时成立，读集合与写集合必有交集，且不会出现两个并发写都成功。
func (q Quorum) StrongConsistent() bool {
	return q.R+q.W > q.N && q.W > q.N/2
}

func main() {
	configs := []Quorum{
		{N: 3, W: 2, R: 2}, // 经典多数派，可容忍 1 个副本失联
		{N: 3, W: 1, R: 1}, // 追求低延迟，读写集合可能不相交
		{N: 3, W: 3, R: 1}, // 写全同步、读任意，写入可用性最低
	}
	for _, q := range configs {
		fmt.Printf("N=%d W=%d R=%d 强一致=%v\n", q.N, q.W, q.R, q.StrongConsistent())
	}
}
```

<OutputBox title="运行结果" output="N=3 W=2 R=2 强一致=true\nN=3 W=1 R=1 强一致=false\nN=3 W=3 R=1 强一致=true" />

第一组是教科书式多数派；第二组 R+W 只有 2，不大于 N，读到的完全可能是没被更新的那份副本；第三组虽然强一致，但 W=3 意味着任意一个副本故障写入就失败，可用性最差。可见一致性不是开关，而是一组参数的连续权衡。

再看如何把「故障」本身建进数据结构里：

<LineByLine code="type Replica struct {\nName string\nReachable bool\nVersion int\n}" :notes='["定义副本类型，建模分布式系统里的一份数据","Name 标识副本所在节点，便于定位故障域","Reachable 表达网络是否可达，这是面向故障设计的关键字段","Version 记录数据版本，用来检测哪个副本落后","结构体结束，后续读写判断都依赖这三个字段"]' />

接下来这段把「中途失败会怎样」直接跑出来：分区时坚持多数派写入会失败，而把 W 降到 1 就能继续服务，代价是另一侧读到旧值。

```go
package main

import "fmt"

// Replica 表示一个副本，Reachable 是对网络故障的直接建模
type Replica struct {
	Name      string
	Reachable bool
	Version   int
}

// Write 在要求 W 个确认的前提下提交写入。
// 分区导致可达副本不足时，选择一致性的系统会拒绝写入。
func Write(replicas []Replica, w int) (bool, string) {
	reachable := 0
	for _, r := range replicas {
		if r.Reachable {
			reachable++
		}
	}
	if reachable < w {
		return false, "CP 取舍：拒绝写入，宁可不可用也不制造冲突"
	}
	return true, "写入成功"
}

func main() {
	normal := []Replica{{"n1", true, 1}, {"n2", true, 1}, {"n3", true, 1}}
	// 分区：只有 n1 可达，n2 与 n3 落在另一侧
	partition := []Replica{{"n1", true, 1}, {"n2", false, 1}, {"n3", false, 1}}
	ok, why := Write(normal, 2)
	fmt.Println("正常 W=2：", ok, why)
	ok, why = Write(partition, 2)
	fmt.Println("分区 W=2：", ok, why)
	// 把 W 降到 1 相当于选可用性，接受写入但另一侧会读到旧值
	ok, _ = Write(partition, 1)
	fmt.Println("分区降级 W=1：", ok, "（可用，但可能读到旧值）")
}
```

运行后能清楚看到两种故障表现：W=2 在分区下返回 `false` 并附带拒绝原因，这是 CP 系统「宁可报错」的一面；W=1 则返回 `true`，是 AP 系统「先服务、后收敛」的一面。同一组副本，只因 W 不同，故障时的行为截然不同——这正是把一致性做成配置而不是口号的价值。

## 3. 深入一步

一致性并非只有「强」和「弱」两极，而是一条谱系。选级别时，真正要问的是分区与恢复这两个时刻分别会发生什么。

| 一致性级别 | 正常时读到什么 | 分区中途失败的表现 | 典型载体 |
| --- | --- | --- | --- |
| 线性一致（强一致） | 永远是最新写入 | 拿不到多数派就报错或阻塞，绝不返回旧值 | etcd、ZooKeeper |
| 顺序一致 | 全局操作顺序一致，值可能偏旧 | 落后副本短暂返回旧值，顺序不乱 | 部分主从复制 |
| 因果一致 | 有因果关系的写读保序 | 无因果的并发写可乱序，需额外合并 | 协同编辑、会话模型 |
| 最终一致（BASE） | 可能旧，无新写入后收敛 | 两侧仍可读写，恢复后靠读修复或反熵补齐 | DNS、Cassandra、缓存 |

<Term word="线性一致性" text="所有操作看起来像在某一个瞬间原子生效，读必定见到此前已完成的最新写入。" />
<Term word="读修复" text="读取时顺带比对多个副本的版本，把落后副本补齐，把收敛工作推迟到读取时刻。" />

BASE 是 **B**asically Available（基本可用）、**S**oft state（软状态）、**E**ventually consistent（最终一致）的缩写。它并不承诺「很快一致」，而是承认系统允许存在中间状态：软状态意味着副本之间可以暂时对不上，只要没有新的写入，经过一段时间所有副本终将收敛。下面这段代码演示最常见的收敛手段——读修复。

```go
package main

import "fmt"

// Node 保存一份带版本号的数据副本
type Node struct {
	Name    string
	Version int
	Value   string
}

// ReadRepair 读取全部可达副本，取版本最高者作为结果，并顺手补齐落后副本。
// 这就是最终一致系统把不一致的收敛推迟到读取时的典型做法。
func ReadRepair(nodes []*Node) string {
	latest := nodes[0]
	for _, n := range nodes {
		if n.Version > latest.Version {
			latest = n
		}
	}
	for _, n := range nodes {
		n.Version = latest.Version
		n.Value = latest.Value
	}
	return latest.Value
}

func main() {
	a := &Node{"n1", 3, "v3"}
	b := &Node{"n2", 2, "v2"} // 分区期间未收到更新
	c := &Node{"n3", 3, "v3"}
	fmt.Println("修复前 n2 =", b.Value)
	got := ReadRepair([]*Node{a, b, c})
	fmt.Println("读到 =", got, "，修复后 n2 =", b.Value)
}
```

读之前 n2 是 v2，一次读取既返回了最新的 v3，又把 n2 就地补齐为 v3。要注意：如果分区期间两侧各自接受了写入，单纯「取最大版本」会丢掉一侧的更新，真实系统必须配合版本向量或冲突合并方案，这正是最终一致最难、也最容易被低估的部分。

<Callout type="warn" title="易错点" text="别把最终一致理解成大概率一致：它保证的是收敛，不保证收敛前读到什么。若业务无法容忍任何旧值（如扣款、库存扣减），就不能选 AP，也别指望重试能补回一致性。" />

## 4. 工程实践：把取舍做成可配置项

生产里最忌讳把 CP 或 AP 写死在代码分支里，更稳的做法是把 N/W/R 做成配置，让同一份二进制在不同故障等级下切换行为。

<Compare title="同一处理流程的两种取舍" :panels='[{"label":"CP 保一致","code":"return waitForQuorum(W, replicas)","note":"分区导致可达副本不足时阻塞或报错，用可用性换数据正确，适合资金与库存"},{"label":"AP 保可用","code":"return writeLocal(replicas[0])","note":"本地写入即返回，恢复后靠版本向量合并，中途可能读到旧值，适合点赞与计数"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"正常运行","desc":"三副本 R+W 大于 N，读写满足强一致","tone":"blue"},{"title":"网络分区","desc":"两侧失联，写入只能拿到 1 个确认","tone":"red"},{"title":"分区内决策","desc":"CP 拒绝写入，AP 接受写入并累积版本","tone":"red"},{"title":"链路恢复","desc":"比对版本，触发读修复或反熵同步","tone":"blue"},{"title":"重新收敛","desc":"副本补齐，系统回到一致状态","tone":"green"}]' />

真实选型同样服从「中途失败会怎样」：支付、库存、分布式锁与选主必须选 CP，宁可短暂不可用也不能出现双花或双主；点赞数、评论计数、购物车、DNS 这类读多写少且能容忍短时旧值的场景选 AP。注册中心也是同一问题的两解：etcd 走 CP，Eureka 走 AP。把这条判断链画成时间线，故障复盘时就能逐段追问每一刻的数据状态，而不是笼统地说「网络抖了一下」。

## 动手练一练
<Exercise title="把仲裁参数改成五副本" task="现有 N=5 的集群，请设置 W 与 R，使读写满足强一致且能容忍 2 个副本失联，并用代码验证判定结果。" :hints='["容忍 2 个副本故障意味着写入仍需拿到多数派确认","回忆强一致的两个判定条件"]' solution="把 W 与 R 都设为 3：\nq := Quorum{N: 5, W: 3, R: 3}\nfmt.Println(q.StrongConsistent()) // 输出 true" explain="R+W=6 大于 5，且 W=3 大于 N/2=2，两个条件同时成立；此时最多坏 2 个副本仍能凑齐 3 个确认。" />

## 随堂检测
<Quiz no="83-1" q="网络分区发生时，CAP 定理给出的真实设计取舍是？" :options='["必须同时满足 C、A、P","放弃 P 就能同时保住 C 和 A","P 是客观前提，只能在 C 与 A 之间权衡","只要多重试几次就能三者兼得"]' answer="2" explain="分区无法避免，系统只能在一致性优先与可用性优先之间做选择；无分区时 C 与 A 可以兼得。" />
<Predict no="83-2" q="N=3、W=1、R=1 时，StrongConsistent() 返回什么？" :options='["true","false","编译报错"]' answer="1" output="false" explain="R+W=2 不大于 N=3，读写集合可能不相交，无法保证读到最新写入。" />
<FillBlank no="83-3" tpl="BASE 是 ___ Available、Soft state、___ consistent 的缩写，核心是接受软状态、追求 ___ 一致。" :answers='["Basically","Eventually","最终"]' hint="基本可用、软状态、最终一致" explain="BASE 用软状态换可用性，承诺的是无新写入后副本终将收敛。" />

## 一页纸速记
<KnowledgeCard :points='["P 是前提不是选项，分区窗口里只在 C 与 A 之间二选一","R+W 大于 N 且 W 大于 N/2 时读写满足强一致","一致性是一条谱系，选级别先看中途失败与恢复时的表现","BASE 承认软状态，靠读修复、反熵与版本向量收敛"]' :pitfalls='["把最终一致当成大概率一致，漏掉收敛前的旧值","分区时靠盲目重试去追一致性，只会放大冲突","两侧都接受写入却不做冲突合并，恢复后静默丢数据"]' summary="CAP 是故障发生时的取舍框架，BASE 是收敛路线的工程表达。" />

## 小结与下一讲

- CAP 的准确含义是「分区前提下 C 与 A 二选一」，P 无法放弃，三选二是误读。
- 用 N/W/R 仲裁把一致性变成可计算、可配置的量，强一致有明确的判定条件。
- 面向故障设计时，每个方案都要回答中途失败会怎样，以及恢复后如何收敛。

要守住「只执行一次」这类约束，光有取舍框架还不够，下一讲进入分布式锁与幂等设计。

[下一讲：分布式锁、幂等与去重](/distributed/distributed-lock)
