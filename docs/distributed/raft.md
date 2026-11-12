---
title: 第 088 讲 · Raft 共识与 etcd 实践
description: 理解强一致系统如何达成共识，并推演脑裂与 Leader 故障下的中途失败
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★★" time="55 分钟" goal="理解强一致系统如何达成共识，能推演脑裂与 Leader 故障下的状态" prereq="一致性哈希、分片与路由" />

# 第 088 讲 · Raft 共识与 etcd 实践

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

### 从多数派相交推导 Raft 的安全性

Raft 的每条安全性结论都能从「任意两个多数派必相交 + 一任期一票」推出来，不必背：

设集群节点数 `N`，多数派阈值 `q = floor(N/2) + 1`。任取两个多数派 `Q1`、`Q2`，交集大小至少为 `2q - N`：N 为奇数时等于 1，N 为偶数时等于 2。所以二者至少共享一个节点。

- **同一任期最多一个 Leader**：两个候选者要各自拿到一个多数派的选票，这两个多数派必相交于某节点，而该节点一任期只能投一票，故不可能两人都过半。
- **已提交日志不丢**：一条日志被提交，意味着它存在于某个多数派 `Q_c` 上；之后任何新 Leader 必须拿到某个多数派 `Q_n` 的选票，`Q_c` 与 `Q_n` 必相交，那个公共节点一定带着这条已提交日志。再叠加「选举限制」（候选者的日志必须至少和投票者一样新），新 Leader 必然包含全部已提交日志。这就是第 2 节 `TryCommit` 里 `copies > total/2` 的严格依据。
- **日志匹配特性**：若两条日志在索引 `i` 处任期相同，则它们此前所有条目相同。因为 Leader 在同一任期、同一索引只创建一条日志，且 AppendEntries 带着 `prevLogIndex` 与 `prevLogTerm` 做一致性检查，不匹配就回退 `nextIndex` 重试。这条特性让副本对齐从「全量比对」降成「从分歧点往后覆盖」。

<SequenceDiagram title="分区时少数派旧主无法提交" caption="多数派侧选出新任期 Leader，旧主侧写请求凑不齐确认" :actors='["Client","OldLeader","NewLeader","Follower"]' :steps='[{"from":"Client","to":"OldLeader","label":"写 x=2","kind":"call"},{"from":"OldLeader","to":"Follower","label":"复制日志","kind":"call"},{"from":"Follower","to":"OldLeader","label":"分区不可达","kind":"return"},{"from":"OldLeader","to":"Client","label":"无法提交 阻塞","note":"少数派侧","kind":"return"},{"from":"NewLeader","to":"Follower","label":"请求投票 任期 3","kind":"call"},{"from":"Follower","to":"NewLeader","label":"同意投票","kind":"return"},{"from":"NewLeader","to":"NewLeader","label":"确认过半 成为主","kind":"self"},{"from":"Client","to":"NewLeader","label":"写 x=2","kind":"call"},{"from":"NewLeader","to":"Follower","label":"复制新日志","kind":"call"},{"from":"Follower","to":"NewLeader","label":"ACK 达到多数派","kind":"return"},{"from":"NewLeader","to":"OldLeader","label":"广播更高任期","kind":"call"},{"from":"OldLeader","to":"OldLeader","label":"退位并补齐日志","kind":"self"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 同一任期最多一个 Leader | 节点不会在同一任期投两票；投票记录持久化 | 投票状态未落盘、重启后忘记投过票，可能重复投票 |
| 已提交日志绝不丢失 | 提交定义为多数派复制，且选举有日志新旧限制 | 把 Leader 本地写入误当提交；或允许日志较旧的节点当选 |
| 少数派无法提交 | 分区是干净的两组互不通信，节点能区分可达与不可达 | 网络只是严重延迟而非断开时，旧主可能凑齐迟到的 ACK 误判提交；需要租约或 ReadIndex 配合 |
| 线性一致读 | 走 ReadIndex 或 Lease Read | 直接读本地状态机；或 Lease Read 依赖的时钟在节点间漂移超过租约 |
| 成员变更不出现双主 | 联合共识或逐次单节点变更 | 一次性从 3 改到 5，新旧多数派可能不相交 |

范围上，Raft 假设的是**崩溃-停止故障（非拜占庭）**：节点可能宕机、重启、延迟、丢消息，但不会伪造选票、不会篡改日志内容、不会假装收到 AppendEntries。若把恶意节点纳入模型，需要 PBFT 类协议，法定人数从 `2f+1` 升到 `3f+1`，且消息复杂度从线性升到平方级。本讲与第 083 讲 CAP 一样，不讨论拜占庭。另外，Raft 保证的是**日志层面不出现双主提交**，业务层面「旧主进程还活着并继续写下游」仍要靠租约与 fencing token（第 084 讲）来挡，这不是 Raft 的职责。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| Raft 能防止脑裂 | Raft 不阻止物理上出现两个自认为 Leader 的节点，只是让少数派侧写不进去 | 它把脑裂的代价限制在可用性上，正确性靠多数派；旧主仍可能对外提供旧数据 |
| Leader 把日志写到本地就算提交了 | 提交必须由多数派确认；本地未扩散的日志在新主上台后可以被覆盖 | `commit` 只在超过半数副本时推进，第 2 节 `e2` 就是未提交命令 |
| 偶数节点更保险、容得更多故障 | 4 节点多数派是 3，与 3 节点多数派 2 的容错能力相同（都只容 1 台），却多一台成本 | 用奇数节点；要容 f 台故障就部署 `2f+1` 台 |

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

### 可验证实验：本地三节点模拟选主加 etcdctl 读状态

先用三个本地进程模拟一次选主（每个进程一个 id，靠文件锁模拟租约），再用真实 etcdctl 把一个三节点集群的状态逐字段读出来：

```bash
# 终端 1/2/3：以不同 id 启动三个选主进程
go run ./cmd/elect/main.go -id n1 -peers 3 &
go run ./cmd/elect/main.go -id n2 -peers 3 &
go run ./cmd/elect/main.go -id n3 -peers 3 &
# 观察谁抢到主，再 kill 掉主进程，看故障转移
# 真实 etcd 集群的状态
etcdctl --endpoints=127.0.0.1:2379,127.0.0.1:22379,127.0.0.1:32379 endpoint status -w table
etcdctl --endpoints=127.0.0.1:2379 member list -w table
# 竞态检测：并发 Campaign 与 KeepAlive
go test -race -run TestElectionConcurrent -v ./...
```

```text
$ go run ./cmd/elect/main.go -id n1 -peers 3
n1 抢主成功 term=1 lease=300ms
n2 抢主失败：主仍存活
n3 抢主失败：主仍存活
[kill n1 后] n2 租约过期后抢主成功 term=2
$ etcdctl endpoint status -w table
+-----------------+------------------+---------+---------+-----------+------------+-----------+
|    ENDPOINT     |        ID        | VERSION | DB SIZE | IS LEADER | RAFT TERM | RAFT INDEX|
+-----------------+------------------+---------+---------+-----------+------------+-----------+
| 127.0.0.1:2379  | 8e9e05c52164694d |  3.5.9  |  25 kB  |    true   |     7     |   10241   |
| 127.0.0.1:22379 | 91bc3c398fb3c146 |  3.5.9  |  25 kB  |   false   |     7     |   10241   |
| 127.0.0.1:32379 | fd422379fda50e79 |  3.5.9  |  25 kB  |   false   |     7     |   10241   |
+-----------------+------------------+---------+---------+-----------+------------+-----------+
$ go test -race -run TestElectionConcurrent -v ./...
--- PASS: TestElectionConcurrent (0.00s)
ok      example.com/raft    0.356s
```

逐字段解读：`IS LEADER` 为 `true` 的只有一行，正是「同一任期最多一个 Leader」的实测；`RAFT TERM` 三节点都是 7，说明它们处在同一任期、没有分区导致任期分叉；`RAFT INDEX` 三者相等且都为 10241，说明日志已复制到全部副本，对应第 2 节 `TryCommit` 的提交索引；`DB SIZE` 相同说明没有落后副本需要补齐。若人为对某节点做 NetworkChaos 分区，会看到该节点的 `RAFT TERM` 落后、`IS LEADER` 为 false 而 `RAFT INDEX` 停滞——这就是「少数派写不进去」的可观测信号。`go test -race` 的 `PASS` 确认第 4 节 `Election` 里 `leader`、`deadline`、`watches` 都由 `sync.Mutex` 保护；**`-race` 抓不到「两个节点都自认为持主」这类语义问题**，那要靠 `Campaign` 的 CAS 语义与租约断言来验证。

<Quiz q="Raft 里一条日志被称为已提交，条件是什么？" :options='["Leader 写入本地即可","被多数派复制","被任意两个节点复制","客户端收到响应"]' answer="1" explain="只有被多数派复制的日志才提交，这样 Leader 更替后新主一定能从相交的多数派里找到它，已提交数据绝不丢失。" />

### 失败矩阵与演练手段

| 注入点 | 预期表现 | 观测信号 | 演练手段 | 恢复或回滚 |
| --- | --- | --- | --- | --- |
| 杀掉 Leader 进程 | 其余节点选举超时后选出新主 | RAFT TERM 递增，IS LEADER 换节点 | `docker kill` 主容器 | 旧主回来后见高任期退位并补齐日志 |
| 网络分区成 2 加 1 | 2 侧继续提交，1 侧停写 | 1 侧 RAFT INDEX 停滞 | chaos-mesh `NetworkChaos partition` | 撤销分区，落后节点追平 |
| 一次性 3 改 5 成员 | 可能出现两个不相交多数派 | 两个节点都自称 Leader | 避免该操作，改用逐次单节点变更 | 回退到上一次成员配置 |
| 旧主网络隔离但进程存活 | 日志层不双主，但旧主可能仍写下游 | 下游出现旧主写入 | 隔离网络但保留进程 | 下游靠 fencing token 拒绝旧令牌 |
| 磁盘写满或投票状态丢失 | 重启后忘记已投过票 | 同任期出现两张选票 | 注入磁盘故障 | 用持久化投票状态，重建节点 |

演练顺序：先做「杀主」验证故障转移耗时（记录 RTO），再做 2 加 1 分区验证少数派停写，最后做一次成员变更演练，确认用逐次单节点而非一次性变更。回滚要覆盖两类：分区撤销后的日志追平，以及误操作成员配置后的配置回退。

### 替代路线与取舍

| 路线 | 共识方式 | 容错 | 故障时 | 代价 | 适合 |
| --- | --- | --- | --- | --- | --- |
| Raft | 强 Leader 加多数派日志 | `2f+1` 容 f 台崩溃 | 少数派不可用，选主期秒级阻塞 | 写入全经 Leader，吞吐受单点限制 | etcd、Consul、TiKV、配置与元数据 |
| Paxos 或 Multi-Paxos | 无强制 Leader，提案编号多数派 | 同上 | 活锁需靠 Leader 选举缓解 | 理解与实现成本高 | 理论基座、Chubby 类系统 |
| Zab | 类 Raft 的主备广播 | 同上 | 与 Raft 接近 | 生态绑定 ZooKeeper | ZooKeeper |
| Gossip 加最终一致 | 无需多数派 | 分区两侧都可写 | 冲突靠版本向量或 CRDT 合并 | 无强一致，收敛有延迟 | 大规模 AP 集群、成员关系 |
| 主从异步复制 | 无共识 | 主宕可能丢已提交数据 | 需人工判定或半同步 | 实现最简 | 可接受秒级数据丢失的场景 |

取舍主线：要「配置或元数据绝不能分叉」就选 Raft 这类 CP 共识，代价是写入吞吐与选主期可用性；要「大规模节点关系与高可用」就选 Gossip 最终一致。两者常在同一系统里分层使用——元数据用 Raft，业务数据用最终一致。

### 与后续实战的衔接

- [分布式任务调度系统](/projects/concurrent-scheduler) 的「etcd 选主、租约与故障转移」几乎是本讲第 4 节的完整工程化：Master 抢主、续租、故障转移，再叠加分片与幂等。
- [KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 会把 Raft 集成进每个分片，本讲的成员变更、日志匹配与 ReadIndex 会直接决定其「读写一致性」一节的实现。
- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 里的控制器本身也常靠 leader election 保证只有一个实例在 Reconcile，用的正是 etcd 租约加 CAS 这套机制。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-088-raft`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-088-raft)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="解释脑裂下的写入" task="三节点集群裂成 2 台与 1 台两半，旧主落在 1 台那侧且进程仍活着。它还能成功提交写请求吗？为什么？" :hints='["想想多数派是几台","旧主那侧能收到几个确认"]' solution="不能。三节点的多数派是 2 台，旧主一侧只有它自己，任何写请求都凑不齐多数派确认；\n多数派那侧会选出新任期的主继续服务；\n旧主既无法提交，也不会产生脏数据，分区恢复后见到更高任期就退位" explain="Raft 把脑裂的代价限制在可用性上：少数派不可用，但数据不会被两个主同时改写。" />

## 随堂检测
<Quiz no="088-1" q="三节点集群发生分区，旧主在 1 台那侧、仍自认为 Leader，它的写请求会怎样？" :options='["正常提交","提交后异步同步","凑不齐多数派确认，无法提交","导致双主脏写"]' answer="2" explain="多数派需要 2 台确认，旧主一侧只有自己，写请求永远停在未提交状态，不会产生脏数据。" />
<Predict no="088-2" q="一次性把 3 节点 Raft 集群直接改成 5 节点，最大的风险是什么？" :options='["写入变慢","可能出现两个互不相交的多数派，导致双主","选举超时变长","日志被清空"]' answer="1" output="可能出现两个互不相交的多数派，导致双主" explain="新旧配置的多数派可能没有公共节点，所以要用联合共识或每次只变更一个节点。" />
<FillBlank no="088-3" tpl="Raft 节点有三种角色：___、Candidate 与 ___；只有被 ___ 复制的日志才能提交。" :answers='["Follower","Leader","多数派"]' hint="追随者、候选者、领导者，以及过半确认" explain="角色随任期切换；多数派提交保证已提交日志在 Leader 更替后绝不丢失。" :explains='["节点启动默认处于被动状态，只接收并响应领导者的心跳与日志复制；一旦选举超时没收到心跳，才自增任期转去竞选。","Raft 用唯一领导者串行接收写请求并向各节点定序复制日志，避免多写点产生冲突；它和候选者、追随者构成可切换的状态机。","过半确认后，任意两个多数派集合必有交集，新当选的领导者一定带着已提交的日志，从而保证换主后已提交数据不丢失。"]' />

## 一页纸速记
<KnowledgeCard :points='["任期单调递增、一任期一票、多数派必然相交，三者共同防住双主","脑裂时少数派无法提交，恢复后旧主见高任期退位","日志只有多数派复制才提交，主崩在半路最多丢未提交日志","成员变更用联合共识或逐次单节点，线性读走 ReadIndex"]' :pitfalls='["偶数节点部署，容错能力没提升却增加成本","直接读本地状态机当线性一致读，拿到旧值","主节点网络隔离但进程存活，业务侧仍继续写而不加围栏"]' summary="Raft 用多数派把故障代价压在可用性上，换取数据层面的强一致。" />

## 小结与下一讲

- 任期、随机超时与一任期一票让同一届最多选出一个 Leader，任期号让过期主自动退位。
- 脑裂时少数派写不进去，日志只有被多数派复制才提交，已提交数据不会因主更替丢失。
- 成员变更需联合共识或逐次单节点，etcd 选主靠租约、CAS 与 watch，业务侧仍要补围栏。

共识保证的是「一套系统内部不分裂」，而真正面对机房故障、流量洪峰与人为变更时，还要把冗余、容灾与演练体系化。

[下一讲：高可用：多活、容灾与混沌工程](/distributed/high-availability)
