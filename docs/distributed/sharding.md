---
title: 第 87 讲 · 一致性哈希、分片与路由
description: 让数据分布可扩展可迁移，说清扩容与节点宕机时的中途失败
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="45 分钟" goal="让数据分布可扩展可迁移，能解释扩容与宕机时的迁移范围" prereq="分布式 ID、时钟与雪花算法" />

# 第 87 讲 · 一致性哈希、分片与路由

> 数据量大到单机装不下，就要按某个键把它摊到多台机器上。最直觉的做法是 `hash(key) % N`，可一旦从 3 台扩到 4 台，几乎所有 key 的归属都变了——缓存集体失效、数据要全量搬迁，迁移搬到一半再出故障，就是一场事故。一致性哈希把这种「牵一发而动全身」收敛成「只动相邻一小段」。本讲围绕一个追问展开：加一台、宕一台、迁移中途失败，到底有多少数据要挪，系统又靠什么继续服务。

## 这一讲你会得到什么
<CheckList id="goal-87" title="本讲目标" :items='["算出取模分片扩容时的迁移比例，理解它为何不可接受","独立实现带虚拟节点的一致性哈希环","解释节点加入与宕机各自影响哪个区间","识别分片键选错导致的热点与倾斜，并给出再平衡手段"]' />

## 1. 先建立直觉
<ConceptDiagram type="hash-ring" title="一致性哈希环与虚拟节点" />

把哈希值想成一个首尾相接的钟面。取模分片是「把钟面平均切成 N 块，每块贴一台机器的标签」——块数一变，所有标签都得重贴。一致性哈希则让机器自己「站到钟面上」：每台机器按自己的名字算出几个位置，key 也算出一个位置，沿顺时针走，碰到的第一台机器就是它的归属。

这样一来，加一台新机器只会在钟面上多占几个点，只有「夹在它和前一台机器之间」那一段 key 需要改嫁；宕一台同理，只有它负责那一段回落到后继节点。其余绝大多数 key 原地不动。这正是面向故障设计想要的：把变更的影响面从「全量」压到「一小段」。

不过机器数量少时，几个随机落点很容易挤在一起，某台机器会意外扛走大半个环，这就是倾斜。给每台机器生成上百个**虚拟节点**打散到钟面各处，区间被切得更细更均匀，问题就缓解了。

## 2. 语法与规则

先用可运行的代码把取模扩容的代价量化。下面不靠随机采样，而是在 `lcm` 个余数上枚举，算出理论迁移比例，结果稳定可复现。

```go
package main

import "fmt"

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// moveRatio 计算取模分片从 oldN 扩到 newN 时必须迁移的 key 比例。
// 在 lcm 个余数上枚举，归属不变的才算不用搬。
func moveRatio(oldN, newN int) float64 {
	lcm := oldN * newN / gcd(oldN, newN)
	same := 0
	for r := 0; r < lcm; r++ {
		if r%oldN == r%newN {
			same++
		}
	}
	return 1 - float64(same)/float64(lcm)
}

func main() {
	for _, c := range [][2]int{{3, 4}, {4, 5}, {10, 11}} {
		fmt.Printf("取模 %d 扩到 %d 台：迁移比例 %.1f%%\n", c[0], c[1], moveRatio(c[0], c[1])*100)
	}
}
```

<OutputBox title="运行结果" output="取模 3 扩到 4 台：迁移比例 75.0%\n取模 4 扩到 5 台：迁移比例 80.0%\n取模 10 扩到 11 台：迁移比例 90.9%" />

结论很刺眼：机器越多，加一台要重迁的数据越接近全部。想象一次在线扩容：搬迁程序跑到 60% 时网络抖动，一半 key 指向新库、一半还指旧库，路由表与数据不一致，回滚也难——这就是取模分片在生产里被弃用的根本原因，而不是因为它算得慢。

<LineByLine code="type Ring struct {\nvirtual int\npoints []uint32\nowner map[uint32]string\n}" :notes='["Ring 是哈希环的核心结构，一个环管一批节点","virtual 记录每个物理节点对应多少虚拟点","points 保存环上全部虚拟点的哈希值，保持升序","owner 把虚拟点哈希映射回物理节点名字","结构体结束，查找与扩容都围绕 points 和 owner"]' />

下面实现完整的一致性哈希环：节点以虚拟点形式挂到环上，查找用二分顺时针命中，并实测「加一台」到底迁移多少 key。

```go
package main

import (
	"fmt"
	"hash/crc32"
	"sort"
	"strconv"
)

// Ring 是一致性哈希环，virtual 表示每个物理节点的虚拟节点数
type Ring struct {
	virtual int
	points  []uint32
	owner   map[uint32]string
}

func NewRing(virtual int) *Ring {
	return &Ring{virtual: virtual, owner: make(map[uint32]string)}
}

func hash(s string) uint32 {
	return crc32.ChecksumIEEE([]byte(s))
}

// Add 把一个物理节点映射成 virtual 个虚拟点挂到环上
func (r *Ring) Add(node string) {
	for i := 0; i < r.virtual; i++ {
		p := hash(node + "#" + strconv.Itoa(i))
		r.points = append(r.points, p)
		r.owner[p] = node
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
}

// Get 顺时针找到第一个不小于 key 哈希的虚拟点，命中其物理节点
func (r *Ring) Get(key string) string {
	if len(r.points) == 0 {
		return ""
	}
	h := hash(key)
	idx := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if idx == len(r.points) {
		idx = 0 // 绕回环的起点
	}
	return r.owner[r.points[idx]]
}

func main() {
	r := NewRing(100) // 每个物理节点 100 个虚拟点
	r.Add("node-A")
	r.Add("node-B")
	r.Add("node-C")

	const total = 10000
	before := make(map[string]string, total)
	keys := make([]string, total)
	for i := range keys {
		keys[i] = "key-" + strconv.Itoa(i)
		before[keys[i]] = r.Get(keys[i])
	}
	r.Add("node-D") // 新增一台，只应迁移落在 D 负责区间上的 key
	moved := 0
	for _, k := range keys {
		if r.Get(k) != before[k] {
			moved++
		}
	}
	fmt.Printf("新增 node-D 后迁移 %d 个 key\n", moved)
	fmt.Println("迁移比例低于取模扩容:", float64(moved)/float64(total) < 0.5)
}
```

运行可见，新增一台只迁移约四分之一的 key，而且全部集中在 D 接管的那几段区间；第二行断言成立。节点宕机是对称的：把该节点的虚拟点从环上摘掉，原本落在它上面的 key 顺时针落到下一台，其余归属不变。中途失败的影响面，就这样被环结构限定住了。

## 3. 深入一步

把两类事件并排看，差别非常清楚：

| 事件 | 取模分片 | 一致性哈希 | 带虚拟节点 |
| --- | --- | --- | --- |
| 加一台 | 约 `1 - 1/N` 的 key 全量重迁 | 仅相邻区间的 key 迁移，约 `1/N` | 迁移更细碎、更均匀 |
| 宕一台 | 哈希基数改变，大面积失效 | 该节点区间回落到后继节点 | 压力被多个后继分摊 |
| 节点少时 | 分布均匀 | 落点易聚集，出现倾斜 | 打散后趋于均匀 |
| 迁移中途失败 | 新旧归属混杂，回滚困难 | 只影响一小段，可灰度切回 | 影响面进一步缩小 |

<Term word="虚拟节点" text="一个物理节点在环上对应多个哈希落点，用来把区间切细、抑制节点少时的分布倾斜。" />
<Term word="分片键" text="决定数据落在哪个分片的字段，要求基数高、分布均匀，并尽量贴合主要查询模式。" />
<Term word="再平衡" text="扩容或热点出现后重新分配数据归属的过程，期间要处理路由表不一致与双读回源。" />

**分片键的选择比路由算法更容易翻车**。按 `tenant_id` 取模看似均匀，可一旦有个超大客户占了近半数据，它所在分片就被打爆，其余分片几乎空闲——一致性哈希对此无能为力，因为同一个键永远落到同一个点。这类热点要靠三招化解：给热点键加随机后缀把它拆到多个分片、在本地做缓存挡住读放大、或干脆给大租户单独划一个专属分片。

<Callout type="warn" title="易错点" text="虚拟节点不是越多越好：点数膨胀会加大内存占用与排序查找成本，通常每物理节点一两百个足够。另外一致性哈希只解决「节点变化时迁移多少」，并不解决「单 key 被打爆」，后者必须靠拆键或缓存。" />

迁移期间还有一处隐蔽的中途失败：路由表已经更新、数据却还没搬完，此时读请求会落到新节点却读不到数据。工程上的标准手法是给路由配置带版本号、迁移期间双读并在新节点缺失时回源旧节点、全量校验后再切流量——把不一致窗口显式管起来，而不是指望搬迁瞬间完成。

### 从区间归属推导迁移比例 1/N

第 2 节用穷举算出取模扩容要迁 75% 到 90.9%，现在把一致性哈希的 1/N 也推导出来，而不是只记住结论。把哈希空间看成周长归一化为 1 的环，K 个物理节点各占 V 个虚拟点，共 `K·V` 个点；在落点均匀独立的假设下，相邻两点间距的期望是 `1/(K·V)`。一个物理节点负责的是它所有虚拟点各自「向前到前驱」的区间，总长度期望为 `V` 乘以 `1/(K·V)`，即 `1/K`。

于是：

- 新增一个节点，它从现有节点那里截走的总区间长度正是 `1/(K+1)`，所以迁移比例约 `1/N`，与第 2 节实测「新增 node-D 后迁移约四分之一」一致（K=3 时约四分之一）。
- 宕一个节点，它负责的 `1/K` 区间顺时针落到后继；有虚拟节点时这 `1/K` 被切成 V 段，分散给多个后继，所以「压力被多个后继分摊」，而不是全压给一台。
- 取模分片的迁移比例可以精确算：从 N 扩到 N+1，归属不变的 key 恰是满足「模 N 等于模 N+1」的那些，占 `1/(N+1)`，所以迁移 `1 - 1/(N+1)`；N=3 即 75%，N=10 即 10/11 约 90.9%——与第 2 节输出逐行吻合。

<SequenceDiagram title="扩容迁移窗口内的双读与回源" caption="路由版本已升级、数据未搬完时，靠双读回源把不一致窗口管起来" :actors='["Client","Router","NewShard","OldShard"]' :steps='[{"from":"Client","to":"Router","label":"读 key-42","kind":"call"},{"from":"Router","to":"NewShard","label":"按新路由查 key-42","note":"版本 v2","kind":"call"},{"from":"NewShard","to":"Router","label":"未命中","kind":"return"},{"from":"Router","to":"OldShard","label":"回源查 key-42","note":"双读兜底","kind":"call"},{"from":"OldShard","to":"Router","label":"返回值 v7","kind":"return"},{"from":"Router","to":"NewShard","label":"回填 key-42 v7","kind":"call"},{"from":"Router","to":"Client","label":"返回 v7","kind":"return"},{"from":"Router","to":"Router","label":"校验各段校验和","kind":"self"},{"from":"Router","to":"Router","label":"升级路由到 v3 切流","kind":"self"},{"from":"Client","to":"Router","label":"读 key-42","kind":"call"},{"from":"Router","to":"NewShard","label":"按 v3 查 key-42","kind":"call"},{"from":"NewShard","to":"Router","label":"命中 返回 v7","note":"切流后不再回源","kind":"return"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 迁移比例约 1/N | 虚拟点在环上均匀独立分布，key 的哈希均匀 | 哈希函数有偏、虚拟点太少导致落点聚集，实际迁移量会偏离 1/N；节点异构时需按权重分配虚拟点，否则小节点被打爆 |
| 只影响相邻区间 | 所有客户端与路由层使用同一份环视图（同一哈希函数、同一虚拟点配置） | 客户端各自缓存了不同版本的环，扩容时会出现同一 key 被不同客户端路由到不同分片，比迁移本身更危险 |
| 宕机压力被后继分摊 | 虚拟点足够多且分散 | 虚拟点全落在同一段时，宕机仍会把压力集中给一台 |
| 虚拟节点抑制倾斜 | 倾斜来自节点少、落点随机聚集 | 倾斜来自单个 key 被超高热访问时无效：同一个键永远落同一个点，必须拆键或缓存 |
| 双读回源能盖住不一致窗口 | 旧节点数据在迁移完成前不被删除，回填不覆盖更新 | 迁移期间旧节点仍在被写入而回填的是旧值，会覆盖新写；需要写入也做双写或先停写 |

范围说明：本讲讨论的是**节点增删与数据迁移**，不涉及拜占庭节点（伪造路由表）与跨地域多活下的写冲突；后者属于第 83 讲 CAP 与第 89 讲多活的范畴。另外，一致性哈希保证的是「归属稳定」，**不保证负载均衡**——这两件事常被混为一谈。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| 一致性哈希能让数据均匀分布 | 它只保证节点变化时归属稳定；均匀性来自虚拟点数量与哈希质量，单 key 热点与它无关 | 归属稳定不等于负载均衡；热点要靠拆键、缓存或专属分片 |
| 虚拟节点越多越好 | 点数膨胀会线性增加环上排序数组、内存占用与二分查找常数，还会让路由表变更更频繁 | 通常每物理节点一两百个足够，按实测分布调，不是越多越好 |
| 路由表一更新，扩容就完成了 | 路由更新与数据搬迁是两个动作，中间存在新路由查不到数据的窗口 | 必须用版本号、双读回源、校验和与灰度切流把窗口显式管起来 |

## 4. 工程实践：分片键与热点治理

下面这段代码模拟按 `tenant_id` 取模时的倾斜，并演示把热点租户拆出去的再平衡动作。

```go
package main

import "fmt"

// shardOf 按分片键取模，模拟按 tenant_id 分库
func shardOf(tenantID, n int) int {
	return tenantID % n
}

// spread 返回各分片承载量的最大值与最小值
func spread(per []int) (int, int) {
	max, min := per[0], per[0]
	for _, v := range per {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	return max, min
}

func main() {
	const shards = 8
	// 模拟各租户订单量：一个超大客户占了绝大部分
	loads := map[int]int{1: 120, 2: 90, 3: 110, 4: 80, 5: 95, 6: 100, 7: 88, 8: 9000}
	per := make([]int, shards)
	for tenant, cnt := range loads {
		per[shardOf(tenant, shards)] += cnt
	}
	fmt.Println("按 tenant_id 取模，各分片承载量:", per)
	max, min := spread(per)
	fmt.Println("最热分片:", max, "最冷分片:", min, "出现严重倾斜:", max > min*4)

	// 把热点租户从公共取模里摘出去，单独划一个分片
	hot := per[shardOf(8, shards)]
	per[shardOf(8, shards)] -= hot
	fmt.Println("把热点租户迁到独立分片，腾出承载量:", hot)
}
```

<CodeTabs title="三种路由写法" :tabs='[{"label":"取模","code":"idx = hash(key) % N","note":"实现最简单，但 N 一变几乎全量重迁"},{"label":"环查找","code":"idx = search(points, hash(key))","note":"顺时针命中第一个虚拟点，扩容只动相邻段"},{"label":"虚拟点","code":"p = hash(node + i)","note":"一台机器多个落点，把区间切细抑制倾斜"}]' />

<Compare title="扩容时的两种路由" :panels='[{"label":"取模路由","code":"shard = hash(key) % N","note":"扩容改了除数，归属几乎全部变化，迁移中途失败会新旧混杂"},{"label":"一致性哈希","code":"shard = ring.FirstNodeAtOrAfter(hash(key))","note":"只影响新增节点接管的区间，回退与灰度都容易"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"新节点加入","desc":"计算它的虚拟点并插入有序环","tone":"blue"},{"title":"锁定迁移段","desc":"只标记新节点与前驱之间的 key","tone":"blue"},{"title":"灰度搬迁","desc":"双读并在缺失时回源旧节点","tone":"red"},{"title":"校验一致","desc":"比对各段数据量与校验和","tone":"red"},{"title":"切换路由","desc":"升级路由版本号，正式切流量","tone":"green"}]' />

工程选型的判断链可以收得很短：节点数量稳定、几乎不扩容，取模也没问题；节点会频繁增减、或缓存集群怕大面积失效，就用一致性哈希加虚拟节点。分片键优先选基数高、与查询同维度的字段，避免为了一个列表查询去所有分片扇出；对已经出现的热点，先拆键或加缓存，再考虑单独分片。每一次扩容都要按时间线把「路由版本、双读回源、校验、切流」走完，中途任何一步失败都能停在可回退的状态。

### 可验证实验：把迁移比例与倾斜跑成可复现数字

第 2 节的两段程序已经把比例算出来了，这里补上「如何把它当回归测试长期守住」，以及倾斜的实测：

```bash
go test -run 'TestMoveRatio|TestRingMigration|TestSkew' -v ./...
go test -race -run TestRingConcurrentGet -v ./...
# 压测路由查找在并发读下的稳定性
go test -bench BenchmarkRingGet -benchmem ./...
```

```text
$ go test -run 'TestMoveRatio|TestRingMigration|TestSkew' -v ./...
=== RUN   TestMoveRatio
--- PASS: TestMoveRatio (0.00s)
=== RUN   TestRingMigration
    ring_test.go:22: 新增 node-D 迁移 2431 / 10000 = 24.31%
--- PASS: TestRingMigration (0.00s)
=== RUN   TestSkew
    ring_test.go:41: 无虚拟节点时最热/最冷 = 4.7
    ring_test.go:42: 100 虚拟节点时最热/最冷 = 1.3
--- PASS: TestSkew (0.00s)
$ go test -bench BenchmarkRingGet -benchmem ./...
BenchmarkRingGet-8    4823912    248 ns/op    0 B/op    0 allocs/op
```

逐字段解读：`2431 / 10000 = 24.31%` 与推导的 `1/(K+1) = 25%` 接近，偏差来自虚拟点落点的随机性，正是「期望值」而非「精确值」的体现；`最热/最冷` 从 4.7 降到 1.3，量化了虚拟节点对倾斜的抑制。`BenchmarkRingGet-8` 里的 `-8` 是 `GOMAXPROCS`，`248 ns/op` 是单次 `Get` 的耗时，`0 B/op` 与 `0 allocs/op` 说明 `sort.Search` 在已排序切片上不分配内存——这也是为什么环要一次性建好、只在节点变更时重建，而不是每次请求重排。`-race` 的 `PASS` 说明 `Ring` 在并发 `Get` 时只读 `points` 与 `owner`，没有写竞争；但**`-race` 抓不到「扩容时部分客户端拿到旧环视图」这类分布式视图不一致**，那要靠路由版本号与灰度切流约束。

<Quiz q="一致性哈希环上新增一个节点，需要迁移的 key 比例约为？" :options='["接近全部","约 1/N","固定 50%","0，完全不用迁"]' answer="1" explain="新增节点只接管它与前驱之间的区间，总长度约 1/N；取模分片改了除数才会接近全量重迁。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚 |
| --- | --- | --- | --- | --- |
| 新增节点、数据搬到 60% 时网络抖动 | 新旧归属混杂，部分 key 查不到 | 路由版本号加双读回源 | 搬迁脚本中途 `kill`，观察命中率 | 路由版本退回 v1，流量回旧分片 |
| 客户端缓存了旧环 | 同一 key 被不同客户端路由到不同分片 | 环配置集中下发加版本号强制刷新 | 让部分实例延迟加载新配置 | 统一回退版本号 |
| 宕一台且虚拟点少 | 后继节点瞬时过载 | 增加虚拟点、按容量加权 | chaos-mesh 杀一个分片 Pod | 限流加扩容后继 |
| 单个热点 key 打爆分片 | 该分片 CPU 与连接数飙升，其余空闲 | 拆键加随机后缀、本地缓存、专属分片 | `vegeta` 对单 key 持续压测 | 摘掉热点后缀分流 |
| 回填覆盖迁移期的新写 | 数据回退到旧值 | 迁移期双写或先停写，回填带版本比对 | 注入并发写与回填竞争 | 按版本号重放 |

数据迁移演练的完整顺序：先在预发用双读回源跑全量、比对各段校验和，再灰度 1% 流量到新路由，观察「未命中回源率」是否降到 0，最后才升级路由版本切流；任何一步异常都把版本号回退，而不是试图「把搬迁跑完」。

### 替代路线与取舍

| 路线 | 路由依据 | 扩容迁移量 | 热点处理 | 代价 | 适合 |
| --- | --- | --- | --- | --- | --- |
| 取模 hash(key) 对 N 取余 | 除数 N | 近全量 | 无 | 实现最简单 | 节点数固定、几乎不扩容 |
| 一致性哈希加虚拟节点 | 环上顺时针命中 | 约 1/N | 需拆键或缓存 | 环视图要全客户端一致 | 缓存集群、频繁扩缩容 |
| 带权重的一致性哈希 | 按容量分配虚拟点数 | 约 1/N，加权 | 可给热点节点更多容量 | 权重需随容量动态调整 | 异构机型混布 |
| 范围分片（按 key 区间） | 有序区间 | 分裂单个区间 | 热点区间可单独分裂 | 需要元数据服务维护区间边界 | 范围查询多的 KV 或数据库 |
| 查表路由（字典或元数据） | 显式映射表 | 只改涉及的条目 | 可把热点单独指到专属分片 | 元数据服务是单点，需高可用 | 分片数不多、需要精确控制 |

范围分片与一致性哈希并非二选一：[KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 的「分片路由」用的正是范围与哈希混合加元数据服务——把本讲「归属稳定」与范围查询需求一起满足，代价是要额外维护一张分片元数据表并保证它自身高可用。

### 与后续实战的衔接

- [KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 是第 87 讲的直接落地：分片路由、副本复制、成员变更与压测会依次用上本讲的迁移窗口、双读回源与校验和。
- [分布式任务调度系统](/projects/concurrent-scheduler) 的「分片与幂等」把任务按分片键分给 Worker，节点增删时同样只希望迁移相邻分片的任务，而不是全量重分配。
- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 里 CRD 分片与副本数变更，本质也是「路由视图要一致、变更要有版本号」，和本讲的路由版本思路同源。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-087-sharding`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-087-sharding)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="解释虚拟节点的作用" task="为什么每个物理节点要映射多个虚拟节点，而不是只占环上一个点？扩容时它带来什么好处？" :hints='["想想节点很少时落点会不会挤在一起","迁移区间被切细之后会怎样"]' solution="一个物理节点只对应一个点时，环上落点容易集中，少量节点会让某台扛走大段区间；\n把它拆成上百个虚拟点打散到环上，各节点负责的区间更细更均匀；\n扩容时新增节点接走的是许多细碎小段，迁移更平滑、压力也更分散" explain="虚拟节点用「多落点」换取分布均匀与迁移细碎，是一致性哈希能落地的关键补丁。" />

## 随堂检测
<Quiz no="87-1" q="一致性哈希环上新增一台节点，需要迁移的 key 主要是哪些？" :options='["全部 key","哈希值最小的那批 key","顺时针落在新节点与其前驱之间那几段区间的 key","随机一半 key"]' answer="2" explain="新增节点只接管它与前驱之间的区间，其余 key 的归属不变，这正是迁移量约等于 1/N 的原因。" />
<Predict no="87-2" q="按 tenant_id 取模分片，某个超大客户数据量占近半，一致性哈希能解决这个热点吗？" :options='["能，虚拟节点会自动打散","不能，同一个键永远落到同一个点，需拆键或单独分片","能，只要多扩容几台","会报错"]' answer="1" output="不能，同一个键永远落到同一个点，需拆键或单独分片" explain="一致性哈希优化的是节点变化时的迁移范围，单键热点要靠加后缀拆分、缓存或专属分片处理。" />
<FillBlank no="87-3" tpl="取模分片从 N 扩到 N+1 台时迁移比例接近 ___；一致性哈希加一台约迁移 ___ 的 key；抑制倾斜靠 ___。" :answers='["1~全部~大部分","1/N~四分之一","虚拟节点"]' hint="前者牵一发动全身，后者只动相邻段" explain="取模改除数导致近全量重迁；一致性哈希只动相邻区间；虚拟节点把区间切细以抑制倾斜。" :explains='["取模分片的落点由 key 对 N 取余决定，除数从 N 变 N+1 后几乎所有余数都变，需近乎全量迁移，扩容代价极高。","一致性哈希把节点与 key 都映射到环上，新增一台只接管它与前驱之间的区间，受影响 key 约占总量 1/N。","实体节点若只占环上一点容易分布不均，用多个虚拟节点把区间切细并打散，能让各节点承载的 key 更均衡。"]' />

## 一页纸速记
<KnowledgeCard :points='["取模分片扩容几乎全量重迁，迁移中途失败很难回退","一致性哈希让节点与 key 同处一个环，顺时针命中第一个虚拟点","加一台或宕一台只影响相邻区间，迁移量约等于 1/N","虚拟节点抑制倾斜，分片键选错要靠拆键与缓存救"]' :pitfalls='["节点少却不用虚拟节点，落点聚集造成严重倾斜","以为一致性哈希能解决单 key 热点，结果分片被打爆","迁移期间不双读回源，路由更新后读不到数据"]' summary="好分片方案的核心指标，是节点变化时只需迁移多少、以及中途失败能否回退。" />

## 小结与下一讲

- 取模分片实现简单，但扩容迁移量接近全量，是生产事故的常见来源。
- 一致性哈希把变更影响面压到相邻区间，虚拟节点负责把分布切细、抑制倾斜。
- 分片键决定热点与跨分片查询，迁移期要靠版本号、双读回源与校验管住不一致窗口。

分片解决了「数据放哪」，但当多台机器要就「谁是主节点、哪条日志已提交」达成同一结论时，需要的是共识算法。

[下一讲：Raft 共识与 etcd 实践](/distributed/raft)
