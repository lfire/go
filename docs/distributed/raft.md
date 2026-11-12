---
title: 第 88 讲 · Raft 共识与 etcd 实践
description: 理解强一致系统如何达成共识，并推演脑裂与 Leader 故障下的中途失败
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★★" time="55 分钟" goal="理解强一致系统如何达成共识，能推演脑裂与 Leader 故障下的状态" prereq="一致性哈希、分片与路由" />

# 第 88 讲 · Raft 共识与 etcd 实践

> 分片把数据摊到多台机器，可「谁是主节点」「哪条日志算已提交」这类问题，必须让所有节点给出同一个答案。etcd、Consul、TiKV 背后都是 Raft。它不像 2PC 那样靠一个协调者发号施令，而是靠任期、选举与多数派日志复制，让系统在部分节点宕机时既能继续服务、又绝不出现两个都自认为合法的主。本讲把这套机制拆开，并持续追问：网络一分为二、旧主还活着、日志复制到一半主节点崩了，系统会留下什么状态？

## 这一讲你会得到什么
<CheckList id="goal-88" title="本讲目标" :items='["说清任期、角色与一任期一票如何支撑 Leader 选举","推演脑裂时少数派为何无法提交、恢复后旧主为何退位","解释日志只有被多数派复制才提交的原因","用租约加 CAS 加 watch 实现一个最小选主"]' />

## 1. 先建立直觉
<ConceptDiagram type="raft" title="Raft：任期、选举与多数派日志复制" />

把一群节点想成一个议会：任何时刻最多有一位被多数承认的议长（Leader），所有提案都由他记录并分发；议员（Follower）一段时间没收到议长心跳，就自己站出来竞选，任期号加一，看谁能拿到过半选票。任期号就像届数，届数旧的人说话没人听——这条简单规则，是脑裂后能自动收敛的关键。

「多数派」是整套设计的支点：三节点集群里，过半就是两台；五节点就是三台。任意两个多数派必然至少有一个公共节点，而一个节点一届只能投一票，于是同一届不可能选出两个 Leader。分区时，落在少数派那侧的节点凑不齐票，既选不出新主，旧主的提案也无人附议，只能干等；多数派那侧照常选出新主继续服务。等网络恢复，旧主看到更新的任期号，立刻退位成 Follower。

这是典型的「面向故障设计」：Raft 并不承诺不脑裂，而是让脑裂的少数派**写不进去**，把故障代价限制在可用性上，而不是数据正确性上。

## 2. 语法与规则

先把选举状态机写成可运行的代码，把脑裂与退位直接跑出来。

```go
package main

import "fmt"

type Role string

const (
	Follower  Role = "Follower"
	Candidate Role = "Candidate"
	Leader    Role = "Leader"
)

// Node 是 Raft 节点的最小状态
type Node struct {
	id       string
	term     int
	role     Role
	votedFor string
}

// StartElection 发起选举：任期加一、先投给自己
func (n *Node) StartElection() {
	n.term++
	n.role = Candidate
	n.votedFor = n.id
}

// GrantVote 一任期只投一票，且只投给任期不小于自己的候选者
func (n *Node) GrantVote(candidate string, term int) bool {
	if term < n.term {
		return false
	}
	if term > n.term {
		n.term = term
		n.votedFor = ""
		n.role = Follower
	}
	if n.votedFor != "" && n.votedFor != candidate {
		return false
	}
	n.votedFor = candidate
	return true
}

// StepDown 收到更高任期消息时退位，这是脑裂后旧主收敛的关键
func (n *Node) StepDown(higherTerm int) {
	if higherTerm > n.term {
		n.term = higherTerm
		n.role = Follower
	}
}

func main() {
	n1 := &Node{id: "n1", role: Follower, term: 1}
	n2 := &Node{id: "n2", role: Follower, term: 1}
	n3 := &Node{id: "n3", role: Leader, term: 1} // 旧主落在少数派一侧

	n1.StartElection()
	votes := 1
	if n2.GrantVote("n1", n1.term) {
		votes++
	}
	if votes > 3/2 {
		n1.role = Leader
	}
	fmt.Println("新主 term:", n1.term, "得票:", votes, "，达到多数派:", votes > 3/2)
	fmt.Println("n2 同任期再投给旧主:", n2.GrantVote("n3", 2))
	fmt.Println("旧主自认为 Leader 但拿不到多数派:", n3.role == Leader)
	n3.StepDown(n1.term)
	fmt.Println("分区恢复后旧主退位为:", n3.role)
}
```

<OutputBox title="运行结果" output="新主 term: 2 得票: 2 ，达到多数派: true\nn2 同任期再投给旧主: false\n旧主自认为 Leader 但拿不到多数派: true\n分区恢复后旧主退位为: Follower" />

第二行就是「一任期一票」：n2 已经把票投给 n1，同届再收到旧主的拉票直接拒绝。第三行点明脑裂的真相——旧主还自认为是 Leader，但它那一侧只有一票，任何写请求都凑不齐多数派确认，于是**不会有任何脏数据被提交**。最后一行是恢复后的收敛：看到更高任期就退位。

<LineByLine code="type Node struct {\nid string\nterm int\nrole Role\nvotedFor string\n}" :notes='["Node 是一个 Raft 节点的最小状态集合","id 标识节点，日志与心跳都靠它寻址","term 是任期号，单调递增，用来识别过期主节点","role 在 Follower、Candidate、Leader 三者间切换","votedFor 记录本任期投给谁，保证一任期一票"]' />

再看日志复制。Raft 的核心约束是：**只有被多数派复制的日志才能提交**。Leader 在复制中途宕机时，那些只在它本地、还没扩散出去的日志，新主可以不认；已经提交的日志则绝不丢失。

```go
package main

import "fmt"

// LogEntry 是一条日志，Term 记录它由哪个任期的 Leader 写入
type LogEntry struct {
	Index int
	Term  int
	Cmd   string
}

// RaftLog 用副本复制进度演示多数派提交
type RaftLog struct {
	entries  []LogEntry
	commit   int
	replicas map[string]int
}

func NewRaftLog() *RaftLog {
	return &RaftLog{replicas: make(map[string]int)}
}

func (l *RaftLog) Append(term int, cmd string) LogEntry {
	e := LogEntry{Index: len(l.entries) + 1, Term: term, Cmd: cmd}
	l.entries = append(l.entries, e)
	return e
}

func (l *RaftLog) Replicate(node string, index int) {
	if index > l.replicas[node] {
		l.replicas[node] = index
	}
}

// TryCommit 从后往前找最高的一条「被多数派复制」的日志并提交
func (l *RaftLog) TryCommit(total int) {
	for i := len(l.entries); i > l.commit; i-- {
		copies := 1 // Leader 自己
		for _, idx := range l.replicas {
			if idx >= i {
				copies++
			}
		}
		if copies > total/2 {
			l.commit = i
			fmt.Printf("索引 %d 已复制到 %d/%d 个节点，提交\n", i, copies, total)
			return
		}
	}
	fmt.Println("没有日志达到多数派，本次不提交")
}

func main() {
	l := NewRaftLog()
	e1 := l.Append(2, "set x=1")
	e2 := l.Append(2, "set y=2")

	// 只把第一条复制到 n2，凑成多数派
	l.Replicate("n2", e1.Index)
	l.TryCommit(3)
	fmt.Println("当前提交索引:", l.commit, "，未提交命令:", e2.Cmd)

	// Leader 在同步第二条之前宕机：n3 完全落后
	fmt.Println("n3 的复制进度:", l.replicas["n3"], "（落后副本）")

	// 分区恢复后把两条都补齐，再提交
	l.Replicate("n3", e2.Index)
	l.TryCommit(3)
	fmt.Println("补齐后提交索引:", l.commit)
}
```

`TryCommit` 里的 `copies` 从 Leader 自己算起，只有超过半数才推进 `commit`。这正是「中途失败会怎样」的答案：Leader 崩在复制半路，最多丢掉那些未提交、且没扩散出去的日志；已经提交的部分因为存在于多数派上，新主一定能找到并保留。

## 3. 深入一步

把机制与它防住的故障一一对应，安全性就不是口号了：

| 机制 | 防住什么 | 中途失败会怎样 | 安全性依据 |
| --- | --- | --- | --- |
| 任期 term | 识别过期 Leader | 旧主用低任期发心跳被拒 | 任期单调递增，见更大任期即退位 |
| 心跳与选举超时 | 发现主节点故障 | 超时未收心跳就发起选举 | 随机超时减少选票平分 |
| 一任期一票 | 同届出现双主 | 候选者拿不到多数派则重试 | 两个多数派必有公共节点，该节点只投一票 |
| 日志复制 | 让副本追平 | 主崩在半路，未提交日志可被覆盖 | 只有多数派复制的日志才提交 |
| 成员变更 | 加减节点 | 一次性变更可能出现两个不相交多数派 | 联合共识或逐次单节点变更 |

<Term word="脑裂" text="网络分区后两侧各自出现主节点的假象；Raft 靠多数派保证少数派侧无法提交，恢复后旧主退位。" />
<Term word="日志匹配特性" text="若两条日志在同一索引有相同任期，则此前所有日志都相同，副本据此快速对齐。" />
<Term word="联合共识" text="成员变更时让新旧两套配置同时生效，避免出现两个互不相交的多数派。" />
<Term word="ReadIndex" text="线性一致读先向多数派确认自己仍是主、再读状态机，避免直接读本地拿到旧值。" />

**成员变更是最容易被低估的中途失败**。设想三节点一次性扩到五节点：旧配置的多数派是原三台里的两台，新配置的多数派是新五台里的三台，这两组可能完全没有交集，于是同一时刻出现两个各自合法的多数派，双主成立。Raft 的解法要么引入联合共识（过渡期需新旧配置都过半），要么每次只增删一个节点——单节点变更下新旧多数派必然相交。

<Callout type="warn" title="真实风险" text="别在偶数节点上部署 Raft：四节点的多数派是三台，容错能力和三节点一样却多花钱。也别把线性一致读写成直接读本地状态机，那样会读到旧值；要走 ReadIndex 或受时钟约束的 Lease Read。" />

etcd 把这些能力打包成几个朴素操作：写入经 Raft 提议、线性读走 ReadIndex、watch 基于 revision 推送变更、选主靠租约加 CAS。哪里都能看到「面向故障」的影子：租约让宕机的主自动释放，CAS 让抢主只有一个人成功，watch 让变更对客户端可见。

## 4. 工程实践：用 etcd 做选主

下面用标准库实现一个最小选主：一个带租约的键，谁先抢到谁当主；主不再续租，租约过期后别人就能接管；watch 把主变更推给订阅者。这正是 etcd `campaign + lease + watch` 的语义。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// Election 模拟 etcd 选主：一个带租约的键，谁先创建谁当主
type Election struct {
	mu       sync.Mutex
	leader   string
	deadline time.Time
	watches  []chan string
}

// Campaign 尝试抢主：键不存在或租约过期时用 CAS 写入自己
func (e *Election) Campaign(node string, ttl time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if e.leader != "" && now.Before(e.deadline) {
		return false // 主还活着
	}
	prev := e.leader
	e.leader = node
	e.deadline = now.Add(ttl)
	e.notify("leader=" + node)
	fmt.Println(node, "抢主成功，前任:", prev)
	return true
}

// KeepAlive 主节点续租，续不上就说明租约已丢
func (e *Election) KeepAlive(node string, ttl time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.leader != node || time.Now().After(e.deadline) {
		return false
	}
	e.deadline = time.Now().Add(ttl)
	return true
}

// Watch 返回事件通道，模拟 etcd 的 watch
func (e *Election) Watch() <-chan string {
	ch := make(chan string, 8)
	e.mu.Lock()
	e.watches = append(e.watches, ch)
	e.mu.Unlock()
	return ch
}

func (e *Election) notify(ev string) {
	for _, ch := range e.watches {
		select {
		case ch <- ev:
		default:
		}
	}
}

func main() {
	e := &Election{}
	w := e.Watch()

	e.Campaign("n1", 300*time.Millisecond)
	fmt.Println("n1 正常续租:", e.KeepAlive("n1", 300*time.Millisecond))
	fmt.Println("n2 并发抢主:", e.Campaign("n2", 300*time.Millisecond))

	// 模拟 n1 宕机：停止续租，等租约自然过期
	time.Sleep(350 * time.Millisecond)
	fmt.Println("n2 在租约过期后抢主:", e.Campaign("n2", 300*time.Millisecond))

	// watch 收到两次主变更
	fmt.Println("watch 事件:", <-w, <-w)
}
```

<CodeTabs title="etcd 常用操作语义" :tabs='[{"label":"写","code":"etcdctl put key value","note":"写请求由 Leader 经 Raft 复制、多数派确认后提交"},{"label":"读","code":"etcdctl get key","note":"线性一致读走 ReadIndex，默认不返回旧值"},{"label":"监听","code":"etcdctl watch key","note":"基于 revision 推送变更，客户端可感知主更替"},{"label":"选主","code":"campaign with lease","note":"租约加 CAS 抢主，主宕机租约过期自动释放"}]' />

<Compare title="写路径的两种选择" :panels='[{"label":"绕过共识","code":"store.Set(key, value)","note":"直接写本地，各副本可能分叉，故障时无法判定谁对"},{"label":"经 Raft 提议","code":"raft.Propose(key, value)","note":"只有多数派接受的日志才提交，主宕机也不丢已提交数据"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"正常服务","desc":"Leader 持续发心跳，日志复制到多数派","tone":"blue"},{"title":"网络分区","desc":"集群裂成两半，少数派侧旧主仍活着","tone":"red"},{"title":"多数派选主","desc":"新任期选出新 Leader，继续提交日志","tone":"red"},{"title":"旧主受限","desc":"少数派凑不齐确认，写请求全部无法提交","tone":"red"},{"title":"恢复收敛","desc":"旧主见高任期退位，补齐落后日志","tone":"green"}]' />

选主还有一个真实的边界：如果主节点只是网络隔离、进程却还活着并继续对外服务，租约没到期之前它并不知道自己已经失势。所以强正确场景要给主节点加 fencing 令牌，或让下游拒绝旧主的写入。Raft 保证的是日志层面不出现双主提交，业务层面的「旧主还在写」仍要靠租约与围栏配合——这与上一讲分布式锁的教训是同一条。

## 动手练一练
<Exercise title="解释脑裂下的写入" task="三节点集群裂成 2 台与 1 台两半，旧主落在 1 台那侧且进程仍活着。它还能成功提交写请求吗？为什么？" :hints='["想想多数派是几台","旧主那侧能收到几个确认"]' solution="不能。三节点的多数派是 2 台，旧主一侧只有它自己，任何写请求都凑不齐多数派确认；\n多数派那侧会选出新任期的主继续服务；\n旧主既无法提交，也不会产生脏数据，分区恢复后见到更高任期就退位" explain="Raft 把脑裂的代价限制在可用性上：少数派不可用，但数据不会被两个主同时改写。" />

## 随堂检测
<Quiz no="88-1" q="三节点集群发生分区，旧主在 1 台那侧、仍自认为 Leader，它的写请求会怎样？" :options='["正常提交","提交后异步同步","凑不齐多数派确认，无法提交","导致双主脏写"]' answer="2" explain="多数派需要 2 台确认，旧主一侧只有自己，写请求永远停在未提交状态，不会产生脏数据。" />
<Predict no="88-2" q="一次性把 3 节点 Raft 集群直接改成 5 节点，最大的风险是什么？" :options='["写入变慢","可能出现两个互不相交的多数派，导致双主","选举超时变长","日志被清空"]' answer="1" output="可能出现两个互不相交的多数派，导致双主" explain="新旧配置的多数派可能没有公共节点，所以要用联合共识或每次只变更一个节点。" />
<FillBlank no="88-3" tpl="Raft 节点有三种角色：___、Candidate 与 ___；只有被 ___ 复制的日志才能提交。" :answers='["Follower","Leader","多数派"]' hint="追随者、候选者、领导者，以及过半确认" explain="角色随任期切换；多数派提交保证已提交日志在 Leader 更替后绝不丢失。" />

## 一页纸速记
<KnowledgeCard :points='["任期单调递增、一任期一票、多数派必然相交，三者共同防住双主","脑裂时少数派无法提交，恢复后旧主见高任期退位","日志只有多数派复制才提交，主崩在半路最多丢未提交日志","成员变更用联合共识或逐次单节点，线性读走 ReadIndex"]' :pitfalls='["偶数节点部署，容错能力没提升却增加成本","直接读本地状态机当线性一致读，拿到旧值","主节点网络隔离但进程存活，业务侧仍继续写而不加围栏"]' summary="Raft 用多数派把故障代价压在可用性上，换取数据层面的强一致。" />

## 小结与下一讲

- 任期、随机超时与一任期一票让同一届最多选出一个 Leader，任期号让过期主自动退位。
- 脑裂时少数派写不进去，日志只有被多数派复制才提交，已提交数据不会因主更替丢失。
- 成员变更需联合共识或逐次单节点，etcd 选主靠租约、CAS 与 watch，业务侧仍要补围栏。

共识保证的是「一套系统内部不分裂」，而真正面对机房故障、流量洪峰与人为变更时，还要把冗余、容灾与演练体系化。

[下一讲：高可用：多活、容灾与混沌工程](/distributed/high-availability)
