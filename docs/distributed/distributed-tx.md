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

### 从「原子提交不可能」推导各方案的边界

2PC 的阻塞不是实现缺陷，而是可以推出来的。设协调者在 Prepare 全票通过后、发出 Commit 前崩溃，此时每个参与者都处于 `prepared`：它已经把 undo/redo 日志落盘、锁住资源、并且投了赞成票。参与者此刻只有三种选择，逐一排除：

- 自行提交：若别的参与者在崩溃前收到的是 Abort，或协调者根本没决定，就会出现部分提交，数据不一致。
- 自行回滚：若协调者其实已经决定 Commit 并写进日志、只是消息没发出，回滚会丢掉一个已达成一致的提交。
- 保持阻塞等待：唯一不会出错的选择，代价是资源被锁住、事务悬挂，直到协调者恢复并广播决定。

于是结论是结构性的：**只要存在一个「投票后、决定前」的窗口，且协调者是单点，就必然有阻塞**。3PC 试图用 PreCommit 阶段把这个窗口拆细，让参与者超时后能默认提交，但如果网络分区恰好把「已 PreCommit」和「未 PreCommit」的两组节点分开，默认提交就会造成不一致——这也是 3PC 在存在分区的生产环境里并没有真正解决问题的原因。本讲把 2PC 归为「短事务、可容忍阻塞」，依据就在这里。

Saga 换的是另一个前提：**放弃隔离性**。它不要求所有人同时成功，只要求每个正向步骤 `T_i` 配一个语义反向的 `C_i`，使得 `C_i` 作用在 `T_i` 之后能回到等价状态。这个等式能成立的前提有三条：`C_i` 必须可重试（网络超时不等于失败）、必须幂等（重试不能重复扣或加）、必须不依赖时序（不能因为晚了就补偿错对象）。一旦某步是不可补偿的（已经发出的短信、已结算的跨行转账），Saga 就无能为力，只能把它挪到事务末尾或改成「先记录、后执行」。

<SequenceDiagram title="Saga 第三步失败后的逆序补偿" caption="正向成功两步、第三步超时，编排器逆序归还库存与撤销订单" :actors='["Client","Orchestrator","OrderSvc","StockSvc","AccountSvc"]' :steps='[{"from":"Client","to":"Orchestrator","label":"提交下单","kind":"call"},{"from":"Orchestrator","to":"OrderSvc","label":"创建订单","kind":"call"},{"from":"OrderSvc","to":"Orchestrator","label":"成功","kind":"return"},{"from":"Orchestrator","to":"StockSvc","label":"扣减库存","kind":"call"},{"from":"StockSvc","to":"Orchestrator","label":"成功","kind":"return"},{"from":"Orchestrator","to":"AccountSvc","label":"冻结余额","kind":"call"},{"from":"AccountSvc","to":"Orchestrator","label":"超时失败","kind":"return"},{"from":"Orchestrator","to":"StockSvc","label":"补偿 归还库存","kind":"call"},{"from":"StockSvc","to":"Orchestrator","label":"补偿成功","kind":"return"},{"from":"Orchestrator","to":"OrderSvc","label":"补偿 撤销订单","kind":"call"},{"from":"OrderSvc","to":"Orchestrator","label":"补偿成功","kind":"return"},{"from":"Orchestrator","to":"Client","label":"整体失败 已回滚","kind":"return"}]' />

### 适用前提与失效条件表

| 方案 | 隐含前提 | 前提被打破时 | 不在讨论范围 |
| --- | --- | --- | --- |
| 2PC / XA | 协调者可恢复且日志持久；参与者支持预提交锁定 | 协调者永久丢失则 prepared 事务永久悬挂；参与者不支持 XA 则无法纳入 | 拜占庭故障、协调者故意发矛盾指令 |
| TCC | Try 能真正预留且可释放；Confirm 与 Cancel 幂等 | 预留资源被外部改动会让 Cancel 解冻错对象；空回滚与悬挂未处理会让资源永久冻结 | 跨组织、无法预留的第三方接口 |
| Saga | 每步都有语义补偿且补偿可重试 | 补偿失败又无重试则卡在中间态；其他请求读到未补偿数据，即无隔离 | 不可补偿的操作、强隔离要求的资金账务 |
| 可靠消息 | 消息与业务能进同一个本地事务；消费端幂等 | 消息中间件不支持事务或本地库与消息库异构，缝隙重现 | 端到端恰好一次投递，需消费端幂等配合 |

特别提醒「恰好一次」的口径：**没有任何一种消息投递能单靠中间件保证端到端 exactly-once**，工程上能实现的是「至少一次投递加消费端幂等」，效果等价但依赖业务配合。把这条前提写清楚，才不会在选型时误以为换个 MQ 就能免掉去重表。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| 2PC 慢是因为网络往返多，换 3PC 或更快的 RPC 就能解决 | 慢是次要的，阻塞来自「投票后、决定前」的结构性窗口，与通信快慢无关 | 只要协调者单点且存在未决窗口，就有 prepared 悬挂；3PC 在分区下仍会不一致 |
| Saga 的补偿就是回滚，和数据库 ROLLBACK 一样可靠 | 补偿是业务语义的反向操作，可能超时、可能失败、可能被并发请求看到中间态 | 补偿必须可重试、幂等、配人工兜底；期间没有全局隔离 |
| 用了 DTM 或 Seata 就不用担心补偿失败 | 框架管的是编排、重试、防悬挂与状态持久化，管不了补偿具体该做什么 | 补偿的业务正确性仍由你定义，框架只是让它可靠地被执行 |

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

### 可验证实验：子测试逐行验证 2PC 悬挂与 Saga 逆序补偿

把第 2 节的两段模拟改成表驱动子测试，能一次看到正常路径与三条故障路径：

```bash
go test -run 'TestTwoPC|TestSagaCompensation' -v ./...
go test -race -run TestSagaCompensation -v ./...
```

```text
$ go test -run 'TestTwoPC|TestSagaCompensation' -v ./...
=== RUN   TestTwoPC
=== RUN   TestTwoPC/normal
=== RUN   TestTwoPC/coordinator_down_after_prepare
=== RUN   TestTwoPC/commit_midway_failure
--- PASS: TestTwoPC (0.00s)
=== RUN   TestSagaCompensation
=== RUN   TestSagaCompensation/all_success
=== RUN   TestSagaCompensation/third_step_fails
=== RUN   TestSagaCompensation/compensation_retry
--- PASS: TestSagaCompensation (0.00s)
PASS
ok      example.com/tx    0.197s
$ go test -race -run TestSagaCompensation -v ./...
--- PASS: TestSagaCompensation (0.00s)
ok      example.com/tx    0.335s
```

逐字段解读：`coordinator_down_after_prepare` 断言三个参与者的 `prepared` 仍为 `true` 而 `committed` 全为 `false`，这正是悬挂而非回滚或提交；`commit_midway_failure` 断言已提交与未提交参与者同时存在，验证「部分提交无法整体回退」；`third_step_fails` 断言补偿调用顺序是「扣库存」「创建订单」（逆序），且 `state` 三个标记都回到 `false`；`compensation_retry` 断言补偿被重放两次时状态不变，验证幂等。`-race` 的 `PASS` 说明 `state` map 在编排器与补偿协程之间没有数据竞争；但和前面几讲一样，**`-race` 抓不到「补偿逻辑本身写反了」或「补偿没有兜底」这类语义错误**，那些只能靠断言和故障注入验证。

<Quiz q="2PC 协调者在 Prepare 全票通过后、发出 Commit 前宕机，参与者处于什么状态？" :options='["自动提交","自动回滚","锁住资源并保持 prepared 阻塞等待","丢弃日志并清空数据"]' answer="2" explain="参与者既不能自行提交也不能自行回滚，只能等协调者恢复，这就是同步阻塞与单点问题；3PC 在分区下也未能真正消除这个窗口。" />

### 失败矩阵与演练手段

| 注入点 | 2PC / XA | TCC | Saga | 可靠消息 | 演练与回滚 |
| --- | --- | --- | --- | --- | --- |
| 协调者或编排器在决定前崩溃 | prepared 悬挂，锁资源 | Try 已预留，等待 Confirm 或 Cancel | 正向日志已持久化，重启后继续补偿 | 本地事务已提交，后台重扫补投 | chaos-mesh 杀协调者 Pod；恢复后核对事务日志 |
| 第三阶段超时 | 部分提交，人工介入 | Cancel 重试，防空回滚 | 逆序补偿，补偿失败则告警 | 重扫重投，消费端幂等 | `tc netem delay` 注入超时；记录 RTO |
| 补偿或回滚本身失败 | 无自动补偿 | Cancel 悬挂，需定时任务 | 卡在中间态，需人工兜底 | 消息堆积，进死信队列 | 预置死信队列与人工对账台 |
| 消息重复投递 | 不涉及 | 不涉及 | 补偿可能重复 | 消费端幂等挡住 | 把同一消息重复置为 pending |
| 数据迁移或库拆分期间 | 跨库 XA 不再成立 | Try 打到新旧两库 | 补偿打到旧库 | outbox 表需双写 | 停机窗口加双写校验后切流 |

演练顺序：先在单服务注入超时，确认补偿顺序与幂等；再杀编排器验证状态可恢复；最后做一次「补偿失败」的注入，确认告警与人工兜底真的能被触发。回滚演练要覆盖两类：事务回滚（补偿回到一致）与数据迁移回滚（双写期间切回旧库且不丢 outbox 记录），后者最容易被漏掉。

### 替代路线与取舍

| 路线 | 一致性 | 中途失败代价 | 开发成本 | 适合 |
| --- | --- | --- | --- | --- |
| 本地事务加单库 | 强一致 | 自动 ROLLBACK | 最低 | 能放在一个库就别拆 |
| 2PC / XA | 强一致 | 阻塞与部分提交 | 依赖数据库 XA 支持 | 短事务、强一致、可容忍阻塞 |
| TCC | 准强一致，预留期内无中间态 | 空回滚、悬挂、三份逻辑 | 每个接口写三份 | 资金、库存 |
| Saga | 最终一致 | 中间态可见、补偿复杂 | 每步配补偿 | 长事务、跨多服务 |
| 本地消息表 | 最终一致 | 秒级延迟、消息堆积 | 一张表加后台任务 | 异步解耦，工程最常用 |

还有一条常被漏掉的替代：[电商微服务系统](/projects/micro-ecommerce) 里反复出现的做法是**重新设计边界，让需要原子的两个操作落到同一个库或同一个服务里**——把「跨库事务」问题消解掉，而不是挑一个分布式事务方案去扛它。这通常比引入框架更划算。

### 与后续实战的衔接

- [电商微服务系统](/projects/micro-ecommerce) 的下单链路正是本讲四类方案的主战场：订单、库存、账户分库后，本地事务立即失效，需要按「能容忍多少中间态」在 TCC 与可靠消息之间选。
- [秒杀与高并发库存](/projects/micro-seckill) 的「预扣减与最终一致」是 TCC 里 Try 预留思路的极端化：先冻结、后确认，用最终一致换吞吐。
- [分布式任务调度系统](/projects/concurrent-scheduler) 用可靠消息把「派发任务」与「记录任务」解耦，靠幂等消费保证重扫不重复执行，正是本地消息表加幂等的落地。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-085-distributed-tx`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-085-distributed-tx)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="写出逆序补偿" task="Saga 在第三步失败时，前两步已成功。请说明补偿的执行顺序，并写出对应的调用形式。" :hints='["补偿要与正向顺序相反","想一想栈的弹出次序"]' solution="已成功的步骤要逆序补偿：\ndone[2].Compensate()\ndone[1].Compensate()\ndone[0].Compensate()\n先撤销最近一步，再依次向前" explain="先做的正向操作后撤销，后做的先撤销，才能让状态按原路退回；顺序写反会留下脏数据。" />

## 随堂检测
<Quiz no="85-1" q="2PC 的协调者在 Prepare 全部成功、尚未发出 Commit 时宕机，会发生什么？" :options='["参与者自动回滚","参与者自动提交","参与者锁着资源阻塞等待，无人能决定","数据自动保持一致"]' answer="2" explain="Prepared 状态的参与者既不能自行提交也不能自行回滚，只能等协调者恢复，这就是同步阻塞与单点问题。" />
<Predict no="85-2" q="先提交业务再发消息，若两步之间进程崩溃会怎样？" :options='["消息会由框架补发","业务成功但消息永久丢失，下游无感知","业务自动回滚","消息重复投递"]' answer="1" output="业务成功但消息永久丢失，下游无感知" explain="两个动作不在同一事务，中间失败必然丢一边，所以要改用本地消息表把消息与业务一起落库。" />
<FillBlank no="85-3" tpl="TCC 的三个阶段是 ___、Confirm 与 ___；Saga 失败时按 ___ 顺序补偿。" :answers='["Try","Cancel","逆序~相反"]' hint="预留、确认、释放，以及补偿的方向" explain="Try 预留、Confirm 确认、Cancel 释放；Saga 用逆序补偿把中间态抹平。" :explains='["首阶段只做资源预留与一致性校验，如冻结而非扣减库存，不真正落地，好让后续无论确认还是释放都保持可逆。","预留之后若确认失败或全局超时，须有人把冻结的额度解冻归位，这一释放分支与 Confirm 互补，共同闭合 Try 留下的悬置状态。","后执行的步骤依赖前一步产出的数据，正向依次为 T1 到 T3，回撤就得从 T3 倒着来，否则会删掉仍被依赖的中间结果。"]' />

## 一页纸速记
<KnowledgeCard :points='["本地事务只管单库，跨库必须选分布式方案","2PC 强一致但协调者故障会阻塞，Commit 阶段故障会部分提交","Saga 接受中间态，失败逆序补偿，补偿必须可重试且幂等","本地消息表加幂等消费，堵住业务成功却消息丢失的缝隙"]' :pitfalls='["把补偿写成一次性操作，超时后再无重试与兜底","先写库后发消息，中间宕机导致下游永久不一致","TCC 不做幂等与防悬挂，出现空回滚或资源悬挂"]' summary="选事务方案的本质，是选你愿意为中途失败付出哪一种代价。" />

## 小结与下一讲

- 2PC / XA 追求强一致，代价是协调者单点、同步阻塞与 Commit 阶段的部分提交风险。
- TCC 用三阶段换一致性，Saga 用逆序补偿换可用性，二者都要求补偿可重试、幂等。
- 可靠消息把消息与业务同事务落库，配合后台重扫与幂等消费，是工程中最常用的最终一致方案。

事务里每个步骤、每条消息、每次补偿都需要一个能全局定位的身份，这就引出了下一讲的全局唯一 ID 与时钟问题。

[下一讲：分布式 ID、时钟与雪花算法](/distributed/distributed-id)
