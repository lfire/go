---
title: 第 85 讲 · 分布式事务：2PC / TCC / Saga / 消息最终一致
description: 按业务与故障代价选一致性方案，把每个方案的中途失败讲清楚
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★★" time="60 分钟" goal="按业务选对一致性方案，逐个回答中途失败会怎样" prereq="分布式锁、幂等与去重" />

# 第 85 讲 · 分布式事务：2PC / TCC / Saga / 消息最终一致

> 单机事务靠数据库的 ACID 就能兜住：一句 `ROLLBACK`，所有改动消失。可一旦「下单」要同时写订单库、扣库存库、动账户库，本地事务就管不到别的库了。分布式事务没有银弹，只有一组各有代价的方案。这一讲不背名词，只做一件事：把 2PC、TCC、Saga 与可靠消息摆到同一张 table 上，逐个追问——如果事务执行到一半、协调者或某个服务宕了，系统会留下什么状态，又靠什么收回来。

## 这一讲你会得到什么
<CheckList id="goal-85" title="本讲目标" :items='["说清本地事务的边界以及跨库后为什么失效","推演 2PC 在协调者宕机时的阻塞与部分提交","用 Saga 编排器实现逆序补偿并解释补偿失败怎么办","用本地消息表保证业务与消息不丢，并说明幂等消费的作用"]' />

## 1. 先建立直觉
<ConceptDiagram type="saga" title="Saga：正向步骤失败后逆序补偿" />

把一次跨服务下单想成多人接力盖章：单机的 `BEGIN ... COMMIT` 像一个人同时拿着三支笔，出错就全划掉；分布式场景是三个人各管一摊，谁也没法一句话让所有人同时撤销。于是出现两条思路：**要么追求「同时成功」**，让所有人先举手同意、再由一个人喊提交（2PC / TCC）；**要么接受「先后成功」**，先干完前面的，后面出事就按相反顺序做反向操作把账抹平（Saga / 消息最终一致）。

前者强一致但脆弱——喊提交的人半路倒下，剩下的人就僵住；后者高可用但承认中间态，必须把「反向操作」写得和正向操作一样可靠。这就是面向故障设计的核心取舍：你要么为「协调者故障」付出阻塞的代价，要么为「中途失败」付出补偿的复杂度。

## 2. 语法与规则

先看最经典的 2PC。它分 Prepare 与 Commit 两阶段：协调者先问所有参与者能否提交，全部同意才下发提交。下面用标准库模拟，并直接注入两种故障。

```go
package main

import (
	"errors"
	"fmt"
)

// Participant 模拟一个参与者：先预提交锁定资源，再真正提交
type Participant struct {
	Name      string
	prepared  bool
	committed bool
	failAt    string // 注入故障：prepare / commit / none
}

func (p *Participant) Prepare() error {
	if p.failAt == "prepare" {
		return errors.New(p.Name + " 在 Prepare 阶段宕机")
	}
	p.prepared = true
	return nil
}

func (p *Participant) Commit() error {
	if p.failAt == "commit" {
		return errors.New(p.Name + " 在 Commit 阶段宕机")
	}
	p.committed = true
	return nil
}

func (p *Participant) Rollback() {
	p.prepared = false
}

// TwoPC 返回是否最终提交，并把中途失败留下的状态打印出来
func TwoPC(ps []*Participant, coordinatorFails bool) bool {
	// 阶段一：询问所有参与者能否提交
	for _, p := range ps {
		if err := p.Prepare(); err != nil {
			fmt.Println("Prepare 失败，全局回滚:", err)
			for _, q := range ps {
				q.Rollback()
			}
			return false
		}
	}
	fmt.Println("全部 Prepare 成功，进入提交阶段")

	if coordinatorFails {
		fmt.Println("协调者在发出 Commit 前宕机：参与者已锁定资源却无人通知，只能阻塞等待")
		return false
	}
	// 阶段二：逐个提交，任何一处失败都无法整体回退
	for _, p := range ps {
		if err := p.Commit(); err != nil {
			fmt.Println("Commit 中途失败:", err, "，已提交的参与者无法回退，出现部分提交")
			return false
		}
	}
	return true
}

func main() {
	ok := TwoPC([]*Participant{{Name: "订单库"}, {Name: "库存库"}, {Name: "账户库"}}, false)
	fmt.Println("正常两阶段提交:", ok)

	fmt.Println("--- 注入故障：协调者宕机 ---")
	ps := []*Participant{{Name: "订单库"}, {Name: "库存库"}, {Name: "账户库"}}
	TwoPC(ps, true)
	fmt.Println("参与者 prepared 状态:", ps[0].prepared, ps[1].prepared, ps[2].prepared, "（资源被挂住）")
}
```

<OutputBox title="运行结果" output="全部 Prepare 成功，进入提交阶段\n正常两阶段提交: true\n--- 注入故障：协调者宕机 ---\n全部 Prepare 成功，进入提交阶段\n协调者在发出 Commit 前宕机：参与者已锁定资源却无人通知，只能阻塞等待\n参与者 prepared 状态: true true true （资源被挂住）" />

输出把 2PC 的软肋暴露得很清楚：Prepare 之后每个参与者都锁着资源、投了赞成票，此时协调者一宕，没人知道该提交还是回滚，参与者只能一直等——这就是**同步阻塞**与**协调者单点**。更糟的是 Commit 阶段才宕机：前面已经提交的库无法回退，后面没提交的库停在原地，出现**部分提交**，数据从此不一致。这两点正是 XA 在生产里被谨慎使用的原因。

<LineByLine code="type Step struct {\nName string\nForward func() error\nCompensate func()\n}" :notes='["一个 Saga 步骤由三部分组成，正向与反向成对出现","Name 标识步骤，失败时用于定位补偿顺序","Forward 是正向操作，返回 error 表示本步失败","Compensate 是补偿操作，把已完成的改动抹平","结构体结束，编排器据此逆序调用补偿"]' />

既然 2PC 卡在「同时」，Saga 换了个思路：允许先后成功，失败时按相反顺序逐个补偿。

```go
package main

import "fmt"

// Step 是一个正向步骤及其对应的补偿步骤
type Step struct {
	Name       string
	Forward    func() error
	Compensate func()
}

// Saga 顺序执行正向步骤；任一步失败，逆序补偿已成功的步骤。
// 这就是「中途失败会怎样」的答案：不留部分成功，靠补偿回到一致。
func Saga(steps []Step) error {
	done := make([]Step, 0, len(steps))
	for _, s := range steps {
		if err := s.Forward(); err != nil {
			fmt.Println("正向失败于:", s.Name, "->", err)
			for i := len(done) - 1; i >= 0; i-- {
				fmt.Println("补偿:", done[i].Name)
				done[i].Compensate()
			}
			return err
		}
		done = append(done, s)
		fmt.Println("完成:", s.Name)
	}
	return nil
}

func main() {
	state := map[string]bool{"创建订单": false, "扣库存": false, "冻结余额": false}
	steps := []Step{
		{"创建订单", func() error { state["创建订单"] = true; return nil },
			func() { state["创建订单"] = false; fmt.Println("  撤销订单") }},
		{"扣库存", func() error { state["扣库存"] = true; return nil },
			func() { state["扣库存"] = false; fmt.Println("  归还库存") }},
		{"冻结余额", func() error { return fmt.Errorf("账户服务超时") },
			func() { state["冻结余额"] = false }},
	}
	err := Saga(steps)
	fmt.Println("Saga 结果:", err)
	fmt.Println("最终状态 创建订单:", state["创建订单"], "扣库存:", state["扣库存"], "冻结余额:", state["冻结余额"])
}
```

第三步「冻结余额」超时，编排器立刻逆序补偿「扣库存」「创建订单」，最终三个标记都回到 false。要特别注意：Saga 没有全局隔离，补偿完成前其他请求可能读到「订单已建、库存已扣」的中间态；而且补偿本身也可能失败，所以补偿操作必须可重试、幂等，并配人工兜底，否则「回滚失败」会变成比原故障更麻烦的状态。

## 3. 深入一步

把四类方案按「中途失败会怎样」横向排开，选型就有了依据：

| 方案 | 一致性 | 中途失败的表现 | 收回方式 | 适合场景 |
| --- | --- | --- | --- | --- |
| 本地事务 ACID | 强一致 | 单库内失败自动回滚，跨库立即失效 | 数据库 ROLLBACK | 单库单服务 |
| 2PC / XA | 强一致 | 协调者宕机则参与者阻塞；Commit 阶段宕机则部分提交 | 超时回滚或人工介入 | 短事务、强一致、可容忍阻塞 |
| TCC | 最终一致 | Confirm 或 Cancel 失败，需重试；还有空回滚与悬挂 | 重试 Confirm 或 Cancel | 资金、库存等高一致要求 |
| Saga | 最终一致 | 正向失败留下中间态，补偿失败会卡住 | 逆序补偿 + 重试 | 长事务、跨多服务 |
| 可靠消息 | 最终一致 | 投递失败或重复投递 | 后台重扫 + 幂等消费 | 异步解耦、可容忍延迟 |

<Term word="TCC" text="Try 预留资源、Confirm 确认、Cancel 释放的三阶段模型，每个阶段都要幂等，还要防空回滚与悬挂。" />
<Term word="本地消息表" text="把待发消息与业务数据放进同一个本地事务，再由后台任务扫描投递，从根上避免业务成功却消息丢失。" />
<Term word="补偿事务" text="对已完成的正向操作做一次语义反向操作，不是数据库回滚，因此必须可重试、幂等且不依赖时序。" />

**2PC 的阻塞**与 **Saga 的中间态**是同一枚硬币的两面：前者把故障代价放在可用性上，后者放在业务复杂度上。TCC 则试图取中间——Try 阶段只冻结不扣减，Confirm 真正扣减，Cancel 解冻；代价是每个接口都要写三份逻辑，且必须处理三个经典问题：幂等（Confirm 被重试）、空回滚（Try 没执行却收到 Cancel）、悬挂（Cancel 比 Try 先到）。

<Callout type="warn" title="真实风险" text="绝不要先提交业务再发消息，也不要先发消息再提交业务：两个动作不在同一事务里，中间宕机必然丢一边。补偿操作同样可能超时或失败，若不设计重试与幂等，Saga 只会把一次失败变成一次卡死。" />

工程上不必自己造轮子：DTM 与 Seata 这类框架把 Saga / TCC 的步骤状态持久化下来，负责补偿重试、超时告警与防悬挂，让业务只写正向与反向两个函数。但框架替你管的是编排，替不了的是「补偿到底该做什么」这一业务判断。

## 4. 工程实践：本地消息表与幂等消费

可靠消息最终一致是最常用、也最贴合「面向故障」的一类：把消息和业务数据同事务落库，再由后台投递器扫描未发送记录。这样「下单成功却没通知库存」这类缝隙从源头被堵死；投递确认丢失导致消息被重扫时，消费端按业务键幂等即可。

```go
package main

import (
	"fmt"
	"sync"
)

// Outbox 模拟本地消息表：业务数据与消息在同一事务里写入
type Outbox struct {
	mu      sync.Mutex
	records []record
}

type record struct {
	biz    string
	status string // pending / sent
}

// CommitBizAndMessage 用一次本地事务同时落业务数据和待发消息
func (o *Outbox) CommitBizAndMessage(biz string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = append(o.records, record{biz: biz, status: "pending"})
	fmt.Println("本地事务提交:", biz, "+ 消息 pending")
}

// Deliver 后台任务扫描未发送消息并投递，投递成功才标记 sent
func (o *Outbox) Deliver(consumer map[string]bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for i := range o.records {
		r := &o.records[i]
		if r.status != "pending" {
			continue
		}
		// 消费端按 biz 幂等：重复投递也只生效一次
		if !consumer[r.biz] {
			consumer[r.biz] = true
			fmt.Println("投递并消费:", r.biz)
		} else {
			fmt.Println("重复投递被幂等挡住:", r.biz)
		}
		r.status = "sent"
	}
}

func main() {
	o := &Outbox{}
	consumer := make(map[string]bool)
	o.CommitBizAndMessage("扣款:order-1")
	o.CommitBizAndMessage("扣款:order-2")

	o.Deliver(consumer) // 正常投递
	// 模拟投递确认丢失、定时任务把同一条消息重新置为待发
	o.records[0].status = "pending"
	o.Deliver(consumer) // 重复投递，靠幂等消费兜底
}
```

<CodeTabs title="四类方案的核心骨架" :tabs='[{"label":"2PC","code":"PREPARE all; COMMIT all","note":"先统一投票再统一提交，协调者故障即阻塞"},{"label":"TCC","code":"TRY reserve; CONFIRM or CANCEL","note":"预留与确认分离，三阶段都要幂等"},{"label":"Saga","code":"forward steps; compensate on fail","note":"正向失败就逆序补偿，接受中间态"},{"label":"可靠消息","code":"commit biz AND outbox in one tx","note":"业务与消息同事务，后台重扫加幂等消费"}]' />

<Compare title="同一故障两种写法的后果" :panels='[{"label":"危险写法","code":"commitBiz(); sendMessage()","note":"两步之间宕机，业务已成功但消息永远丢失，下游永不感知"},{"label":"本地消息表","code":"tx.Commit(biz, outboxRow)","note":"消息与业务同生共死，投递失败可由后台任务重扫补发"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"Try 预留","desc":"冻结库存与余额，不真正扣减","tone":"blue"},{"title":"正向执行","desc":"创建订单并推进各服务本地事务","tone":"blue"},{"title":"中途失败","desc":"账户服务超时，编排器判定整体失败","tone":"red"},{"title":"逆序补偿","desc":"先解冻余额再归还库存，抹平中间态","tone":"red"},{"title":"收敛完成","desc":"全部回到一致状态，失败可重试或告警","tone":"green"}]' />

选型的判断链其实很短：能放在一个库就用本地事务；必须跨库且强一致、事务又短，才考虑 2PC / XA；跨多个服务、链路长、能容忍秒级不一致，用 Saga 或可靠消息；涉及资金且要求尽量少的中间态，用 TCC。无论哪种，都要先回答「协调者或某一步挂了会怎样」，再决定要不要上框架。

## 动手练一练
<Exercise title="写出逆序补偿" task="Saga 在第三步失败时，前两步已成功。请说明补偿的执行顺序，并写出对应的调用形式。" :hints='["补偿要与正向顺序相反","想一想栈的弹出次序"]' solution="已成功的步骤要逆序补偿：\ndone[2].Compensate()\ndone[1].Compensate()\ndone[0].Compensate()\n先撤销最近一步，再依次向前" explain="先做的正向操作后撤销，后做的先撤销，才能让状态按原路退回；顺序写反会留下脏数据。" />

## 随堂检测
<Quiz no="85-1" q="2PC 的协调者在 Prepare 全部成功、尚未发出 Commit 时宕机，会发生什么？" :options='["参与者自动回滚","参与者自动提交","参与者锁着资源阻塞等待，无人能决定","数据自动保持一致"]' answer="2" explain="Prepared 状态的参与者既不能自行提交也不能自行回滚，只能等协调者恢复，这就是同步阻塞与单点问题。" />
<Predict no="85-2" q="先提交业务再发消息，若两步之间进程崩溃会怎样？" :options='["消息会由框架补发","业务成功但消息永久丢失，下游无感知","业务自动回滚","消息重复投递"]' answer="1" output="业务成功但消息永久丢失，下游无感知" explain="两个动作不在同一事务，中间失败必然丢一边，所以要改用本地消息表把消息与业务一起落库。" />
<FillBlank no="85-3" tpl="TCC 的三个阶段是 ___、Confirm 与 ___；Saga 失败时按 ___ 顺序补偿。" :answers='["Try","Cancel","逆序~相反"]' hint="预留、确认、释放，以及补偿的方向" explain="Try 预留、Confirm 确认、Cancel 释放；Saga 用逆序补偿把中间态抹平。" />

## 一页纸速记
<KnowledgeCard :points='["本地事务只管单库，跨库必须选分布式方案","2PC 强一致但协调者故障会阻塞，Commit 阶段故障会部分提交","Saga 接受中间态，失败逆序补偿，补偿必须可重试且幂等","本地消息表加幂等消费，堵住业务成功却消息丢失的缝隙"]' :pitfalls='["把补偿写成一次性操作，超时后再无重试与兜底","先写库后发消息，中间宕机导致下游永久不一致","TCC 不做幂等与防悬挂，出现空回滚或资源悬挂"]' summary="选事务方案的本质，是选你愿意为中途失败付出哪一种代价。" />

## 小结与下一讲

- 2PC / XA 追求强一致，代价是协调者单点、同步阻塞与 Commit 阶段的部分提交风险。
- TCC 用三阶段换一致性，Saga 用逆序补偿换可用性，二者都要求补偿可重试、幂等。
- 可靠消息把消息与业务同事务落库，配合后台重扫与幂等消费，是工程中最常用的最终一致方案。

事务里每个步骤、每条消息、每次补偿都需要一个能全局定位的身份，这就引出了下一讲的全局唯一 ID 与时钟问题。

[下一讲：分布式 ID、时钟与雪花算法](/distributed/distributed-id)
