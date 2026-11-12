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

## 动手练一练
<Exercise title="解释虚拟节点的作用" task="为什么每个物理节点要映射多个虚拟节点，而不是只占环上一个点？扩容时它带来什么好处？" :hints='["想想节点很少时落点会不会挤在一起","迁移区间被切细之后会怎样"]' solution="一个物理节点只对应一个点时，环上落点容易集中，少量节点会让某台扛走大段区间；\n把它拆成上百个虚拟点打散到环上，各节点负责的区间更细更均匀；\n扩容时新增节点接走的是许多细碎小段，迁移更平滑、压力也更分散" explain="虚拟节点用「多落点」换取分布均匀与迁移细碎，是一致性哈希能落地的关键补丁。" />

## 随堂检测
<Quiz no="87-1" q="一致性哈希环上新增一台节点，需要迁移的 key 主要是哪些？" :options='["全部 key","哈希值最小的那批 key","顺时针落在新节点与其前驱之间那几段区间的 key","随机一半 key"]' answer="2" explain="新增节点只接管它与前驱之间的区间，其余 key 的归属不变，这正是迁移量约等于 1/N 的原因。" />
<Predict no="87-2" q="按 tenant_id 取模分片，某个超大客户数据量占近半，一致性哈希能解决这个热点吗？" :options='["能，虚拟节点会自动打散","不能，同一个键永远落到同一个点，需拆键或单独分片","能，只要多扩容几台","会报错"]' answer="1" output="不能，同一个键永远落到同一个点，需拆键或单独分片" explain="一致性哈希优化的是节点变化时的迁移范围，单键热点要靠加后缀拆分、缓存或专属分片处理。" />
<FillBlank no="87-3" tpl="取模分片从 N 扩到 N+1 台时迁移比例接近 ___；一致性哈希加一台约迁移 ___ 的 key；抑制倾斜靠 ___。" :answers='["1~全部~大部分","1/N~四分之一","虚拟节点"]' hint="前者牵一发动全身，后者只动相邻段" explain="取模改除数导致近全量重迁；一致性哈希只动相邻区间；虚拟节点把区间切细以抑制倾斜。" />

## 一页纸速记
<KnowledgeCard :points='["取模分片扩容几乎全量重迁，迁移中途失败很难回退","一致性哈希让节点与 key 同处一个环，顺时针命中第一个虚拟点","加一台或宕一台只影响相邻区间，迁移量约等于 1/N","虚拟节点抑制倾斜，分片键选错要靠拆键与缓存救"]' :pitfalls='["节点少却不用虚拟节点，落点聚集造成严重倾斜","以为一致性哈希能解决单 key 热点，结果分片被打爆","迁移期间不双读回源，路由更新后读不到数据"]' summary="好分片方案的核心指标，是节点变化时只需迁移多少、以及中途失败能否回退。" />

## 小结与下一讲

- 取模分片实现简单，但扩容迁移量接近全量，是生产事故的常见来源。
- 一致性哈希把变更影响面压到相邻区间，虚拟节点负责把分布切细、抑制倾斜。
- 分片键决定热点与跨分片查询，迁移期要靠版本号、双读回源与校验管住不一致窗口。

分片解决了「数据放哪」，但当多台机器要就「谁是主节点、哪条日志已提交」达成同一结论时，需要的是共识算法。

[下一讲：Raft 共识与 etcd 实践](/distributed/raft)
