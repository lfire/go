---
title: 第 083 讲 · CAP / BASE 与一致性模型
description: 建立分布式系统的判断框架，理解网络分区下一致性与可用性的真实取舍
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="45 分钟" goal="建立分布式系统的判断框架，能对每种一致性方案回答中途失败会怎样" prereq="RPC 与微服务" />

# 第 083 讲 · CAP / BASE 与一致性模型

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

### 从抽屉原理推导 quorum 的两个条件

`R + W > N 且 W > N/2` 不是经验口诀，而是两条可以分别证明的集合命题。设副本全集为 `U`，大小为 `N`；一次读实际询问的副本集合为 `S_r`，大小为 `R`；一次写拿到确认的集合为 `S_w`，大小为 `W`。

- `R + W > N` 推出 `S_r` 与 `S_w` 必相交：假设两者不相交，则并集大小 `R + W > N`，但并集是 `U` 的子集、大小不可能超过 `N`，矛盾。所以读集合至少命中一个已确认写入的副本，取版本最大者就能读到最新值。
- `W > N/2` 推出任意两个写集合 `S_w1`、`S_w2` 必相交：交集大小至少为 `2W - N`，大于 0。公共副本会按版本号或日志序把两次写排成先后，或拒绝后到的一次，于是不会出现两个互不感知的写都各自返回成功、各自留下不同值。

注意第二条独立于第一条：`R + W > N` 只保证「读能见到某次写」，挡不住「两次并发写都成功」。第 2 节里 `StrongConsistent()` 把两个条件写成 `&&`，正是因为缺一不可。

<SequenceDiagram title="三副本 quorum 写入遇到分区" caption="W=2 时确认不足，CP 系统拒绝写入；随后读取靠版本与读修复收敛" :actors='["Client","NodeA","NodeB","NodeC"]' :steps='[{"from":"Client","to":"NodeA","label":"写 x=2 要求 W=2","kind":"call"},{"from":"NodeA","to":"NodeB","label":"转发并请求确认","kind":"call"},{"from":"NodeB","to":"NodeA","label":"ACK 版本 7","kind":"return"},{"from":"NodeA","to":"NodeC","label":"转发并请求确认","kind":"call"},{"from":"NodeC","to":"NodeA","label":"分区中无响应","note":"等待超时","kind":"return"},{"from":"NodeA","to":"Client","label":"确认不足返回错误","note":"CP 取舍","kind":"return"},{"from":"Client","to":"NodeB","label":"读 x 要求 R=2","kind":"call"},{"from":"NodeB","to":"Client","label":"返回版本 7","kind":"return"},{"from":"Client","to":"NodeC","label":"读 x 要求 R=2","kind":"call"},{"from":"NodeC","to":"Client","label":"返回版本 6","note":"落后副本旧值","kind":"return"},{"from":"Client","to":"Client","label":"取版本 7 并读修复","kind":"self"}]' />

### 这套推导成立的前提，以及它什么时候失效

把前提写清楚，结论才不会用错地方：

| 前提 | 为什么需要 | 前提被打破后的表现 |
| --- | --- | --- |
| 崩溃-停止故障模型，节点不会伪造应答 | 交集里的副本必须如实报告自己的版本 | 拜占庭故障（节点撒谎、返回伪造值）不在 CAP 与 quorum 的讨论范围，需要 BFT 类协议，副本数从 `2f+1` 升到 `3f+1` |
| 每个 key 在每个副本上有全序版本号 | 读集合命中交集后要能判定谁更新 | 用 last-write-wins 且时钟偏移时，物理时间戳大的未必是后写，会静默丢数据 |
| 读写都严格查询 N 个首选副本中的 R/W 个 | 抽屉原理只在同一个全集 U 上成立 | sloppy quorum 把确认记到临时节点（hinted handoff），读写集合可能落在不相交的两批机器上，交集失效 |
| 网络是异步的：消息延迟没有上界 | 这是 CAP 不可行结论的证明假设 | 若给网络加同步假设（有界延迟、可判定节点死亡），能用超时精确判活，结论会松动 |
| C 特指单对象、单操作的线性一致性 | Gilbert-Lynch 证明针对的是读写寄存器 | 多 key 事务、跨对象约束不属于 CAP 的 C；ACID 里的 C 是完整性约束，与它毫无关系 |
| A 指非故障节点的每个请求都终止并返回 | 定理按请求粒度定义 | A 不是运维口径的可用率百分比，也不要求响应成功；分区时 CP 系统返回错误，按定理就是放弃了 A |

一句话收口：**拜占庭故障、多对象事务、时钟可信度、sloppy quorum 这四类问题，CAP 定理本身都不回答**。把它们混进「三选二」的讨论，是绝大多数误读的来源。

### 无分区时 CAP 没说完的那一半：PACELC

CAP 只在分区那段窗口里给出取舍，可系统绝大多数时间并没有分区。PACELC 把它补全：有分区时在 A 与 C 之间选（即 CAP），无分区时在延迟与一致性之间选。也就是说，即便一切正常，选 CP 的系统仍要为一致性多付一次多数派往返的延迟：etcd 的写要经过 Leader 复制到多数派，ZooKeeper 的读也要走同步调用；选 AP 的系统可以直接读本地副本，延迟低但可能旧。于是 `etcd / ZooKeeper / HBase` 属于 PC/EC（分区保一致、平时也保一致，牺牲延迟），`Cassandra / Dynamo / Riak` 属于 PA/EL（分区保可用、平时保低延迟，牺牲一致性）。理解这一层，才不会把「我们没分区」当成「可以随便选一致性级别」。

### 三条真实误解（错误说法 / 正确说法）

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| CAP 的 C 就是 ACID 的 C，A 就是可用率 99.99% | 两者定义完全不同：ACID 的 C 是事务前后约束不被破坏，CAP 的 C 是线性一致性；可用率是运维统计口径 | CAP 的 C 是单对象实时序的线性一致性，A 是每个未崩溃节点的请求都能终止并返回响应，与百分比 SLA 无关 |
| 分区时多重试几次、把超时调长就能既 C 又 A | 异步网络里消息延迟无上界，重试只是把「不知道对方收到没有」的窗口拉长，无法判断该返回新值还是旧值 | CP 系统必须在这段窗口里对写请求返回错误或阻塞，这段不可用正是放弃 A 的代价 |
| 分区恢复后两边数据会自动合并，无需处理冲突 | 两侧各自接受写入会产生兄弟版本，简单取最大版本会丢掉一侧更新 | 必须靠版本向量、CRDT 或业务合并规则显式收敛；LWW 在时钟偏移下还会静默丢数据 |

<Callout type="note" title="判断口径" text="遇到任何一致性方案，先问三句：它假设的是崩溃故障还是拜占庭故障？它的交集是在同一个副本全集上吗？无分区时它用延迟换了什么？三句答完，选型就有依据了。" />

## 4. 工程实践：把取舍做成可配置项

生产里最忌讳把 CP 或 AP 写死在代码分支里，更稳的做法是把 N/W/R 做成配置，让同一份二进制在不同故障等级下切换行为。

<Compare title="同一处理流程的两种取舍" :panels='[{"label":"CP 保一致","code":"return waitForQuorum(W, replicas)","note":"分区导致可达副本不足时阻塞或报错，用可用性换数据正确，适合资金与库存"},{"label":"AP 保可用","code":"return writeLocal(replicas[0])","note":"本地写入即返回，恢复后靠版本向量合并，中途可能读到旧值，适合点赞与计数"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"正常运行","desc":"三副本 R+W 大于 N，读写满足强一致","tone":"blue"},{"title":"网络分区","desc":"两侧失联，写入只能拿到 1 个确认","tone":"red"},{"title":"分区内决策","desc":"CP 拒绝写入，AP 接受写入并累积版本","tone":"red"},{"title":"链路恢复","desc":"比对版本，触发读修复或反熵同步","tone":"blue"},{"title":"重新收敛","desc":"副本补齐，系统回到一致状态","tone":"green"}]' />

真实选型同样服从「中途失败会怎样」：支付、库存、分布式锁与选主必须选 CP，宁可短暂不可用也不能出现双花或双主；点赞数、评论计数、购物车、DNS 这类读多写少且能容忍短时旧值的场景选 AP。注册中心也是同一问题的两解：etcd 走 CP，Eureka 走 AP。把这条判断链画成时间线，故障复盘时就能逐段追问每一刻的数据状态，而不是笼统地说「网络抖了一下」。

### 可验证实验：穷举 quorum 与 go test -race

第 2 节的 `StrongConsistent()` 只给了判定式，下面把「判定式确实等价于集合相交」这件事跑出来验证。先把下面这段保存为 `quorum_verify.go`，它穷举所有读写子集，不靠随机采样：

```go
package main

import "fmt"

// combos 返回 n 个副本里所有大小为 k 的子集，用位掩码表示
func combos(n, k int) []int {
	var out []int
	for mask := 0; mask < 1<<n; mask++ {
		if popcount(mask) == k {
			out = append(out, mask)
		}
	}
	return out
}

// popcount 统计位掩码中置位的个数
func popcount(x int) int {
	c := 0
	for x != 0 {
		x &= x - 1
		c++
	}
	return c
}

// intersects 验证任意读集合与写集合是否必然相交
func intersects(n, r, w int) bool {
	reads := combos(n, r)
	writes := combos(n, w)
	for _, rm := range reads {
		for _, wm := range writes {
			if rm&wm == 0 {
				return false
			}
		}
	}
	return true
}

// disjointWrites 判断是否存在两个互不相交的写集合
func disjointWrites(n, w int) bool {
	writes := combos(n, w)
	for _, a := range writes {
		for _, b := range writes {
			if a&b == 0 {
				return true
			}
		}
	}
	return false
}

func main() {
	for _, c := range [][3]int{{3, 2, 2}, {3, 1, 3}, {5, 3, 3}} {
		n, w, r := c[0], c[1], c[2]
		fmt.Printf("N=%d W=%d R=%d 读集合必含已确认副本=%v 存在不相交双写=%v\n",
			n, w, r, intersects(n, r, w), disjointWrites(n, w))
	}
}
```

```bash
# 运行穷举程序；再用竞态检测器跑并发写入模拟
go run quorum_verify.go
go test -race -run TestQuorumIntersection -v ./...
```

```text
$ go run quorum_verify.go
N=3 W=2 R=2 读集合必含已确认副本=true 存在不相交双写=false
N=3 W=1 R=3 读集合必含已确认副本=true 存在不相交双写=true
N=5 W=3 R=3 读集合必含已确认副本=true 存在不相交双写=false
$ go test -race -run TestQuorumIntersection -v ./...
=== RUN   TestQuorumIntersection
--- PASS: TestQuorumIntersection (0.00s)
PASS
ok      example.com/quorum    0.312s
```

逐字段解读：`读集合必含已确认副本` 是把所有大小为 `R` 的子集和所有大小为 `W` 的子集逐一求交得到的实测结果，用来反向验证抽屉原理；`存在不相交双写` 是枚举两个大小为 `W` 的写集合、看能否找到不相交的一对，`true` 意味着两个并发写可能各自成功，正好解释 `W=1` 那行为什么强一致为 `false`。`go test -race` 的 `PASS` 说明共享的副本状态由互斥锁保护，运行时没有发现 `WARNING: DATA RACE`；但要特别留意 **`-race` 只抓内存访问层面的数据竞争，抓不到「两个写逻辑上都成功」这类一致性错误**——后者正是靠上面的穷举断言验证的，两者职责不同，不能互相替代。

<Quiz q="关于 CAP 定理里的 C，下列说法正确的是？" :options='["它等价于 ACID 的一致性约束","它特指单对象操作的线性一致性","它指全年可用率百分比","它由客户端重试次数决定"]' answer="1" explain="CAP 的 C 是单对象、实时序的线性一致性，与 ACID 的完整性约束、可用率百分比都无关，也不能靠重试绕过分区窗口。" />

### 失败矩阵：分区落在写入链路的不同位置

| 分区或故障发生点 | CP 系统表现 | AP 系统表现 | 可观测信号 | 演练与回滚手段 |
| --- | --- | --- | --- | --- |
| 客户端发出写请求后、协调者回包前 | 请求超时或返回错误，值可能已写但未确认 | 本地写入即返回，值只在单侧 | 超时率、未确认写计数 | `iptables -j DROP` 造分区；客户端用幂等键重试 |
| 已拿到 W-1 个确认时 | 凑不齐 W，拒绝提交 | 继续接受，形成版本分叉 | quorum 确认数直方图 | chaos-mesh `NetworkChaos` 按标签定向注入 |
| 已拿到 W 个确认、响应回程丢失 | 写入实际已提交，客户端误判失败 | 同上，两侧版本不同 | 版本分叉数、反熵积压 | 读修复与反熵做数据迁移，客户端靠幂等键回放 |
| 读请求落在落后副本 | 走 ReadIndex 或读多数派取新值 | 可能返回旧值 | 旧版本读占比 | 提高 R、开启读修复 |
| 分区恢复、两侧都有写入 | 以多数派日志为准，少数派回滚 | 兄弟版本交给版本向量或 LWW 合并 | 冲突计数、合并失败告警 | 冻结写入后从多数派重建落后副本，即回滚 |
| 节点时钟大幅偏移 | 不依赖物理时钟则不受影响 | LWW 可能把旧值判成新值 | NTP offset 告警 | 禁写并人工按业务时间线裁决 |

演练顺序建议：先在单副本上用 `iptables` 或 `tc netem` 造几分钟分区，观察失败矩阵里哪一行被触发；再升级到单可用区注入；最后才做跨区演练。任何时刻保留「丢弃未收敛副本、从多数派重建」这条回滚路径，避免为了保住两侧写入而让脏数据扩散。

### 替代路线与取舍

| 路线 | 如何达成一致 | 分区或故障时 | 代价 | 适合 |
| --- | --- | --- | --- | --- |
| 可调 N/W/R quorum | 读写集合求交，无需单一 Leader | 凑不齐 W 即拒写 | 每次读写多副本往返，冲突需版本合并 | 读写都可调、希望按业务切级别 |
| 单 Leader 加 Raft 复制 | 写入全经 Leader，多数派确认后提交 | 少数派不可用，选主期短时阻塞 | Leader 是吞吐瓶颈，选主有秒级不可用 | 元数据存储、选主、配置中心 |
| CRDT 无冲突数据类型 | 操作满足交换律结合律，副本各自合并 | 两侧照常写，恢复后自动收敛 | 仅支持可合并操作子集，元数据随副本数增长 | 计数器、协同编辑、购物车 |
| 会话级保证 | 不改系统级别，客户端粘滞加版本号补 read-your-writes | 依赖会话状态，失效转移时要重建 | 客户端复杂，跨设备难保证 | 用户能容忍最终一致但要求自己写的自己能读到 |

四条路线不是互斥的：工程里常把「多数派 quorum 管元数据、CRDT 管计数、会话粘滞管体验」叠在同一系统里，按数据的重要程度分档，而不是全站一刀切选 CP 或 AP。

### 与后续实战的衔接

- [分布式任务调度系统](/projects/concurrent-scheduler) 用 etcd 选主，正是本讲 CP 取舍的落地：分区时宁可不调度，也不允许两个 Master 同时派发同一任务；它的「租约与故障转移」把「凑不齐多数派」转成了主动退位。
- [实战 12 · KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 会把第 087 讲的路由和第 088 讲的复制合起来，其「读写一致性」一节重新用上 N/W/R 与 ReadIndex。
- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 的 Reconcile 循环处理的是「声明状态与现实状态不一致」，其收敛思路与读修复、反熵同源：不假设一步到位，而是持续把现实拉回声明。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-083-cap-base`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-083-cap-base)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="把仲裁参数改成五副本" task="现有 N=5 的集群，请设置 W 与 R，使读写满足强一致且能容忍 2 个副本失联，并用代码验证判定结果。" :hints='["容忍 2 个副本故障意味着写入仍需拿到多数派确认","回忆强一致的两个判定条件"]' solution="把 W 与 R 都设为 3：\nq := Quorum{N: 5, W: 3, R: 3}\nfmt.Println(q.StrongConsistent()) // 输出 true" explain="R+W=6 大于 5，且 W=3 大于 N/2=2，两个条件同时成立；此时最多坏 2 个副本仍能凑齐 3 个确认。" />

## 随堂检测
<Quiz no="083-1" q="网络分区发生时，CAP 定理给出的真实设计取舍是？" :options='["必须同时满足 C、A、P","放弃 P 就能同时保住 C 和 A","P 是客观前提，只能在 C 与 A 之间权衡","只要多重试几次就能三者兼得"]' answer="2" explain="分区无法避免，系统只能在一致性优先与可用性优先之间做选择；无分区时 C 与 A 可以兼得。" />
<Predict no="083-2" q="N=3、W=1、R=1 时，StrongConsistent() 返回什么？" :options='["true","false","编译报错"]' answer="1" explain="R+W=2 不大于 N=3，读写集合可能不相交，无法保证读到最新写入。" />
<FillBlank no="083-3" tpl="BASE 是 ___ Available、Soft state、___ consistent 的缩写，核心是接受软状态、追求 ___ 一致。" :answers='["Basically","Eventually","最终"]' hint="基本可用、软状态、最终一致" explain="BASE 用软状态换可用性，承诺的是无新写入后副本终将收敛。" :explains='["对应基本可用：分布式故障时允许降级，例如响应变慢或部分功能不可用，只要核心服务仍能返回，就不必追求强一致下的完整可用。","对应一致性前面的修饰词，说明副本之间允许短暂不一致，但只要停止写入并经过一段时间，各副本终会收敛到相同值。","修饰的是收敛性的一致：系统不保证每次都读到最新值，只承诺停止写入后副本逐步趋同，用可用性换来了这份较弱的保证。"]' />

## 一页纸速记
<KnowledgeCard :points='["P 是前提不是选项，分区窗口里只在 C 与 A 之间二选一","R+W 大于 N 且 W 大于 N/2 时读写满足强一致","一致性是一条谱系，选级别先看中途失败与恢复时的表现","BASE 承认软状态，靠读修复、反熵与版本向量收敛"]' :pitfalls='["把最终一致当成大概率一致，漏掉收敛前的旧值","分区时靠盲目重试去追一致性，只会放大冲突","两侧都接受写入却不做冲突合并，恢复后静默丢数据"]' summary="CAP 是故障发生时的取舍框架，BASE 是收敛路线的工程表达。" />

## 小结与下一讲

- CAP 的准确含义是「分区前提下 C 与 A 二选一」，P 无法放弃，三选二是误读。
- 用 N/W/R 仲裁把一致性变成可计算、可配置的量，强一致有明确的判定条件。
- 面向故障设计时，每个方案都要回答中途失败会怎样，以及恢复后如何收敛。

要守住「只执行一次」这类约束，光有取舍框架还不够，下一讲进入分布式锁与幂等设计。

[下一讲：分布式锁、幂等与去重](/distributed/distributed-lock)
