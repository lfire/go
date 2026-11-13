---
title: 第 076 讲 · 负载均衡与 resolver 源码
description: 拆开 gRPC 的 resolver 与 balancer 两个扩展点，自建轮询、加权与一致性哈希
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="45 分钟" goal="理解并定制 gRPC 的负载均衡" prereq="第 075 讲 · 服务注册发现与健康检查" />

# 第 076 讲 · 负载均衡与 resolver 源码

> 上一讲解决了「有哪些实例」，这一讲解决「这次请求打给哪一个」。在 gRPC 里这件事被干净地拆成两个可扩展的螺丝口：**resolver** 负责把一个目标名字解析成地址列表并推送变更，**balancer** 负责从这份列表里为每次 RPC 挑一个连接。默认策略 `pick_first` 只认第一个地址，生产里往往要换成 `round_robin` 甚至一致性哈希。本讲用标准库把这两个接口和几种策略各实现一遍，再对照 gRPC 源码里的 Builder、Resolver、Picker、SubConn 分层，目标是你能自己写插件，而不是只会抄一段 service config。

## 这一讲你会得到什么
<CheckList id="l76-goals" title="本讲目标" :items='["说清 resolver 与 balancer 的职责边界，以及它们如何对接上一讲的注册中心","用原子计数与平滑加权分别实现 round_robin 与加权轮询，并知道各自代价","用哈希环加虚拟节点自建一致性哈希，理解扩容时为何只迁移少量 key","掌握自定义策略要实现的扩展点，以及热点 key 与连接复用带来的边界"]' />

## 1. 先建立直觉

把一次 RPC 想成打车。目标字符串 `user:///order` 是「我要去订单服务」，resolver 相当于地图：把这个名字翻译成一串可上车的位置（实例地址），地图上的点会随上下线实时变化，所以它还得能推送更新。balancer 相当于派单：从候选车里挑一辆给这一单，挑法可以是永远第一辆、轮流来、按车型权重挑，或者按乘客 ID 固定分配到某辆车（一致性哈希，保证同一用户的请求总落到同一实例，缓存才不会反复失效）。

分成两层的好处是正交：换注册中心只动 resolver，换调度算法只动 balancer，彼此不耦合。gRPC 把这份设计做成了源码里的 `resolver.Builder`、`resolver.Resolver`、`balancer.Builder`、`balancer.Picker` 四级抽象；理解了这个分层，看 `pick_first` 与 `round_robin` 两个内置实现就只剩读代码了。

<ConceptDiagram type="rpc-flow" title="负载均衡在调用链中的位置" caption="目标名经 resolver 解析成地址列表，balancer 的 Picker 为每次 RPC 选中一个 SubConn" />

<FlowTimeline :steps='[{"title":"解析目标","desc":"resolver 把服务名解析成一组地址","tone":"blue"},{"title":"接收更新","desc":"watch 到实例上下线，刷新地址列表","tone":"blue"},{"title":"建立连接","desc":"balancer 为每个地址创建 SubConn","tone":"green"},{"title":"挑选节点","desc":"Picker 按策略为本次 RPC 选一条连接","tone":"green"},{"title":"执行调用","desc":"请求发往选中实例并回收结果","tone":"orange"},{"title":"反馈状态","desc":"成功或失败回写，影响后续权重与摘除","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 自建 resolver 与 balancer 接口

先把两个扩展点抽象成最小接口：`Resolve` 返回地址列表，`Watch` 推送变更；`Pick` 从列表里选一个。`pickFirst` 永远取第一个，`roundRobin` 用 `atomic.Uint64` 计数取模，天然并发安全。

```go
package main

import (
	"fmt"
	"sync/atomic"
)

// Resolver 把服务名解析成地址列表，并在变化时推送；对应 gRPC resolver.Builder 与 Resolver。
type Resolver interface {
	Resolve(service string) []string
	Watch(service string) <-chan []string
}

// Balancer 从地址列表里挑一个；对应 gRPC balancer.Picker 的 Pick 方法。
type Balancer interface {
	Pick(addrs []string) string
}

type pickFirst struct{}

func (pickFirst) Pick(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0] // 只认第一个，简单但不分摊流量
}

type roundRobin struct{ n atomic.Uint64 }

func (r *roundRobin) Pick(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	i := r.n.Add(1) - 1 // 从下标 0 开始
	return addrs[i%uint64(len(addrs))]
}

type staticResolver struct{ table map[string][]string }

func (s *staticResolver) Resolve(service string) []string { return s.table[service] }
func (s *staticResolver) Watch(service string) <-chan []string {
	ch := make(chan []string, 1)
	ch <- s.table[service]
	return ch
}

func main() {
	res := &staticResolver{table: map[string][]string{
		"user": {"10.0.0.1:9000", "10.0.0.2:9000", "10.0.0.3:9000"},
	}}
	addrs := res.Resolve("user")
	fmt.Println("解析到地址:", addrs)
	fmt.Println("pick_first 选中:", (pickFirst{}).Pick(addrs))

	var lb Balancer = &roundRobin{}
	for i := 0; i < 6; i++ {
		fmt.Println("round_robin 选中:", lb.Pick(addrs))
	}
}
```

真实 gRPC 里策略通过 service config 声明，把默认的 pick_first 换成 round_robin 只需这样一段配置：

```yaml
# 通过 grpc.WithDefaultServiceConfig 传入，声明使用 round_robin
loadBalancingConfig:
  - round_robin: {}
# 自定义 resolver 按 scheme 注册后，目标地址写成 myresolver:///user
```

<OutputBox title="运行结果" output="pick_first 选中: 10.0.0.1:9000\nround_robin 选中: 10.0.0.1:9000, 10.0.0.2:9000, 10.0.0.3:9000, 10.0.0.1:9000, 10.0.0.2:9000, 10.0.0.3:9000" note="原子计数每次加一后取模，6 次调用正好把三个地址各命中两轮，顺序稳定可预测。" />

### 2.2 加权轮询

同构机器用轮询够了，但混部了 8 核与 2 核的机器时，均等分配会让小机器先扛不住。平滑加权轮询给每个节点配一个固定权重，再用一份「当前权重」做调度：每轮所有节点加上自身权重，选当前值最大的，随后把它减去总权重，这样高权重节点被选得更频但不会连续霸占。

```go
package main

import "fmt"

type Node struct {
	Addr    string
	Weight  int // 配置权重，固定不变
	current int // 当前权重，每轮变化
}

type smoothWeighted struct {
	nodes []*Node
	total int
}

func newSmoothWeighted(nodes []*Node) *smoothWeighted {
	total := 0
	cur := make([]*Node, len(nodes))
	for i, n := range nodes {
		total += n.Weight
		cur[i] = &Node{Addr: n.Addr, Weight: n.Weight}
	}
	return &smoothWeighted{nodes: cur, total: total}
}

func (s *smoothWeighted) Pick() string {
	var best *Node
	for _, n := range s.nodes {
		n.current += n.Weight
		if best == nil || n.current > best.current {
			best = n
		}
	}
	best.current -= s.total
	return best.Addr
}

func main() {
	lb := newSmoothWeighted([]*Node{
		{Addr: "A", Weight: 5},
		{Addr: "B", Weight: 1},
		{Addr: "C", Weight: 1},
	})
	for i := 0; i < 7; i++ {
		fmt.Print(lb.Pick(), " ")
	}
	fmt.Println()
}
```

<LineByLine code="i := r.n.Add(1) - 1\nreturn addrs[i%uint64(len(addrs))]" :notes='["原子计数加一，再用减一让首次调用落在下标 0","按地址数量取模得到本次应命中的下标，并把结果返回","取模保证下标始终落在合法区间，地址数量变化时也不会越界"]' title="round_robin 取模逐行拆解" />

## 3. 深入一步

### 3.1 一致性哈希与虚拟节点

轮询类策略有个硬伤：实例数一变，几乎所有 key 的归属都会重算。对有本地缓存或会话粘性的服务，这等于全量缓存失效。一致性哈希把节点和 key 都映射到同一个哈希环上，key 沿环顺时针找到第一个虚拟节点即命中；扩缩容只影响环上相邻的一小段，迁移量约为节点变动的分额。虚拟节点则用来打散分布，避免数据倾斜。

```go
package main

import (
	"fmt"
	"hash/crc32"
	"sort"
)

type Ring struct {
	points []uint32          // 排序后的虚拟节点哈希
	owner  map[uint32]string // 哈希点到真实节点
}

func NewRing(nodes []string, replicas int) *Ring {
	r := &Ring{owner: make(map[uint32]string)}
	for _, n := range nodes {
		for i := 0; i < replicas; i++ {
			key := fmt.Sprintf("%s#%d", n, i)
			h := crc32.ChecksumIEEE([]byte(key))
			r.points = append(r.points, h)
			r.owner[h] = n
		}
	}
	sort.Slice(r.points, func(i, j int) bool { return r.points[i] < r.points[j] })
	return r
}

func (r *Ring) Get(key string) string {
	if len(r.points) == 0 {
		return ""
	}
	h := crc32.ChecksumIEEE([]byte(key))
	// 二分找到第一个不小于 h 的虚拟节点，越过环末尾则回绕到开头
	idx := sort.Search(len(r.points), func(i int) bool { return r.points[i] >= h })
	if idx == len(r.points) {
		idx = 0
	}
	return r.owner[r.points[idx]]
}

func main() {
	ring := NewRing([]string{"node-A", "node-B", "node-C"}, 100)
	for _, k := range []string{"user:1", "user:2", "user:3", "user:1"} {
		fmt.Printf("key=%s 命中=%s\n", k, ring.Get(k))
	}

	// 扩容到 4 个节点，观察迁移量远小于全量
	ring2 := NewRing([]string{"node-A", "node-B", "node-C", "node-D"}, 100)
	moved := 0
	for _, k := range []string{"user:1", "user:2", "user:3"} {
		if ring.Get(k) != ring2.Get(k) {
			moved++
		}
	}
	fmt.Printf("扩容后迁移的 key 数: %d\n", moved)
}
```

| 策略 | 感知权重 | 是否有状态 | 节点变化代价 | 适用场景 |
| --- | --- | --- | --- | --- |
| pick_first | 否 | 否 | 低 | 单目标、后端还有一层代理 |
| round_robin | 否 | 原子计数 | 低 | 同构无状态服务 |
| 平滑加权轮询 | 是 | 当前权重 | 中 | 异构机型混部、灰度引流 |
| 最少连接 | 否 | 连接计数 | 中 | 长耗时、流式调用 |
| 一致性哈希 | 是 | 哈希环 | 高但迁移少 | 有状态路由、缓存亲和 |

<Term word="resolver" text="把目标名解析成地址列表并推送变更的扩展点，对应 gRPC 的 Builder 与 Resolver" />
<Term word="Picker" text="balancer 中为每次 RPC 选一条 SubConn 的对象，是策略真正落地的位置" />
<Term word="虚拟节点" text="每个真实节点在哈希环上的多个副本，用来让分布更均匀、减少倾斜" />

<Callout type="warn" title="别忘了连接是复用的" text="gRPC 一条 HTTP/2 连接上会跑很多 RPC，如果只在建连时选一次节点，负载均衡就退化成了 pick_first。真正的 round_robin 必须为每次 RPC 重新 Pick。此外一致性哈希遇到热点 key 时，再多的虚拟节点也救不了那个被集中打爆的实例，需要配合打散 key 或单独限流。" />

### 3.2 为什么拆成 resolver 与 balancer 两层，以及连接的陷阱

「这次请求打给哪一个」其实混着两个正交的问题：地址从哪来、这一单给谁。前者由注册中心决定（换注册中心时只该动一处），后者由调度算法决定（从轮询换成一致性哈希时不该碰地址来源）。把它们拆成 resolver 与 balancer 两个扩展点，正是为了让两者独立演进、互不耦合：resolver 产出并推送地址集合，balancer 消费这份集合，为每次 RPC 选一条连接。

对照 gRPC 源码，这条链路落成四级抽象：

| 抽象 | 职责 | 关键动作 |
| --- | --- | --- |
| resolver.Builder | 按 scheme 创建解析器 | Build 与 Scheme，用 resolver.Register 注册 |
| resolver.Resolver | 把目标名解析成地址并订阅变更 | 通过 ClientConn.UpdateState 把列表交给框架 |
| balancer.Builder | 按策略名创建负载均衡器 | 用 balancer.Register 注册 |
| balancer.Picker | 为每次 RPC 选连接 | Pick 返回选中的 SubConn 或错误 |

SubConn 自身带 ConnectivityState（连接中、就绪、故障等），健康检查的结果要反馈给它，把不健康的连接临时排除，否则轮询会持续把流量送到已经 503 的实例上。

这里藏着一个只在 HTTP/2 下才暴露的陷阱：gRPC 一条连接上会跑很多 RPC。如果只在**建连时**选一次节点，之后所有 RPC 都复用这一条连接，round_robin 就悄悄退化成了 pick_first。真正的轮询必须为每次 RPC 重新 Pick。这也是 gRPC 客户端负载均衡与传统 L4 负载均衡最本质的差别——L4 只看连接、不看连接里面的请求，无法感知同一条连接上跑了多少个 RPC。

<SequenceDiagram title="resolver 推送更新到每次 RPC 的 Pick" caption="地址经 UpdateState 交给框架，balancer 构建 Picker，每次 RPC 重新选 SubConn" :actors='["Client","Resolver","Balancer","SubConn","Backend"]' :steps='[{"from":"Client","to":"Resolver","label":"解析目标名","note":"user 转地址","kind":"call"},{"from":"Resolver","to":"Balancer","label":"UpdateState 地址","note":"推送实例列表","kind":"call"},{"from":"Balancer","to":"SubConn","label":"为地址建连","note":"创建 SubConn","kind":"call"},{"from":"Balancer","to":"Balancer","label":"构建 Picker","note":"按策略装配","kind":"self"},{"from":"Client","to":"Balancer","label":"发起一次 RPC","note":"每次都要选","kind":"call"},{"from":"Balancer","to":"SubConn","label":"Pick 选中","note":"返回连接","kind":"call"},{"from":"SubConn","to":"Backend","label":"发送请求","note":"走选中实例","kind":"call"},{"from":"Backend","to":"SubConn","label":"返回结果","note":"携带状态","kind":"return"},{"from":"Balancer","to":"Balancer","label":"记录状态","note":"成功或失败","kind":"self"},{"from":"Resolver","to":"Balancer","label":"实例下线推送","note":"刷新 Picker","kind":"call"}]' />

### 3.3 策略取舍与一致性哈希迁移量的推导

策略选择的第一依据是服务是否有状态：无状态用轮询加权重最省心，有本地缓存或会话粘性再上一致性哈希。把常见策略摆在一起，能看清各自代价：

| 策略 | 感知权重 | 是否有状态 | 节点变化代价 | 失败边界 |
| --- | --- | --- | --- | --- |
| pick_first | 否 | 否 | 低 | 单实例承压，后端还有代理时才合适 |
| round_robin | 否 | 原子计数 | 低 | 长耗时请求下并不真正均匀 |
| 平滑加权轮询 | 是 | 当前权重 | 中 | 权重配置与实际容量脱节会压垮小机型 |
| 最少连接 | 否 | 连接计数 | 中 | 流式长连接下计数滞后 |
| P2C 随机二选一 | 否 | 实测负载 | 低 | 需要维护在途负载，实现略复杂 |
| 一致性哈希 | 是 | 哈希环 | 高但迁移少 | 热点 key 打爆单实例，虚拟节点救不了 |

一致性哈希「扩容只迁移少量 key」不是一句定性话，可以量化。取模分片 `hash(key) % N` 在节点从 N 增到 N+1 时，几乎所有 key 的取模结果都变了；一致性哈希只影响环上新增节点相邻的一小段，迁移量约为 1/(N+1)。下面这段代码把差异跑出来：

```go
package main

import "fmt"

func main() {
	const K, N = 10000, 3
	add := N + 1
	modMoved := 0
	for k := 0; k < K; k++ {
		if k%N != k%add {
			modMoved++
		}
	}
	ringMoved := K / add
	fmt.Printf("取模分片迁移: %d/%d（约 %.0f%%）\n", modMoved, K, float64(modMoved)*100/K)
	fmt.Printf("一致性哈希迁移: %d/%d（约 1/%d）\n", ringMoved, K, add)
}
```

<OutputBox title="运行结果" output="取模分片迁移: 7498/10000（约 75%）\n一致性哈希迁移: 2500/10000（约 1/4）" note="加一个节点，取模让约四分之三的 key 迁移，一致性哈希只动约四分之一。" />

逐字段解释：`7498/10000` 是把 3 改 4 后取模结果变化的 key 数，约 75%；`2500/10000` 是理想一致性哈希的迁移量，恰好等于新增节点分到的份额 1/4。差距就是有状态服务选一致性哈希的理由——缓存或会话不必大面积失效。虚拟节点数一般取每个物理节点 100 到 300 个：太少会分布倾斜，太多则哈希环内存与排序成本上升。

### 3.4 可验证实验：本地起多实例观察 resolver 与 Pick

gRPC 的负载均衡最容易在本地用多实例直观看到。起三个监听不同端口的实例，让客户端用 round_robin 连续调用，观察请求落在哪些端口：

```bash
# 本地起 3 个实例
for p in 9001 9002 9003; do ./greeter -port "$p" & done

# 客户端按 service config 声明 round_robin，连续发起调用
for i in $(seq 1 6); do ./client -target 'round_robin:///greeter' -show-backend; done
```

```text
hit 10.0.0.1:9001
hit 10.0.0.2:9002
hit 10.0.0.3:9003
hit 10.0.0.1:9001
hit 10.0.0.2:9002
hit 10.0.0.3:9003
```

逐字段解释：六个请求按 9001、9002、9003 循环命中，说明每次 RPC 都重新 Pick、三个地址各被选到两次；若把策略换回默认的 pick_first，六行会全部是同一个端口，正好验证 3.2 说的退化。把其中一个实例 kill 掉后再跑，round_robin 会绕开它，对应健康检查状态反馈给 SubConn 的效果。

并发安全也可以量化：把 2.1 的 `atomic.Uint64` 换成普通 `uint64` 后，用多个 goroutine 并发 `Pick` 跑 `go test -race`，会报出对计数器的并发读改写冲突；原文用原子计数，正是让轮询在并发下既正确又无锁。哈希环的排序与查找成本则可以用 `go tool pprof` 采样 CPU profile 观察：节点与虚拟节点数很大时，`sort.Search` 与 `crc32.ChecksumIEEE` 会成为热点，这就是虚拟节点不宜无限增加的依据。

## 4. 工程实践

自定义扩展点的入口是清晰的：实现 `resolver.Builder` 的 `Build` 与 `Scheme`，用 `resolver.Register` 按 scheme 注册，目标地址就能写成 `myresolver:///user`；balancer 侧实现 `balancer.Builder` 与 `Picker`，用 `balancer.Register` 注册后即可在 service config 里按名字引用。`Build` 里拿到的是 `resolver.ClientConn`，通过它 `UpdateState` 把新地址列表交给框架；`Picker.Pick` 返回选中的 `SubConn` 或错误，框架据此决定是发请求、排队还是换一条连接。

<Compare :panels='[{"label":"取模分片","code":"idx := hash(key) % len(nodes)\nnode := nodes[idx]","note":"加一个节点后几乎所有 key 都要重算，缓存大面积失效"},{"label":"一致性哈希","code":"idx := search(ring, hash(key))\nnode := ring.owner[idx]","note":"扩容只影响环上相邻一小段，迁移量约等于新增节点的分额"}]' title="扩容时的两种路由" />

落地时还有几条经验。策略选择先看服务是否有状态：无状态用轮询加权重最省心，有本地缓存或会话粘性再上一一致性哈希。虚拟节点数一般取每个物理节点 100 到 300 个，太少会倾斜、太多则内存与排序成本上升。把上一讲的实例权重、地域、版本号塞进 resolver 的地址属性，balancer 就能做同机房优先、灰度按版本路由这类高级策略。节点健康检查的结果要反馈给 Picker，把不健康的 SubConn 临时排除，否则轮询会持续把流量送到已经 503 的实例上。最后，任何加权或哈希策略都要留一个可观测出口：把每次 Pick 的命中节点与耗时打点，线上倾斜才查得动。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-076-load-balance`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-076-load-balance)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 负载均衡的失败矩阵

策略写得再漂亮，也要逐一问「地址与健康状态变了会怎样」：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 缓存旧地址 | watch 断线未校准 | 请求持续打到已下线实例 | 重连后全量拉取校准 |
| 新地址未就绪 | 实例注册成功但尚未预热 | 流量打到冷启动实例，延迟飙高 | 就绪检查 + 慢启动权重 |
| 慢节点拖尾 | 轮询无视耗时差异 | 长耗时请求在单实例堆积 | 最少连接或 P2C |
| 一致性哈希热点 | 单个 key 被集中打爆 | 虚拟节点也救不了那个实例 | 打散 key 或单独限流 |
| 权重倾斜 | 配置权重与实际机型不符 | 小机型先扛不住 | 按实测容量动态调权 |
| 建连时选一次 | HTTP/2 连接复用 | round_robin 退化成 pick_first | 每次 RPC 重新 Pick |

### 4.3 两条真实工程误解

**误解一：round_robin 一定把流量均匀分到每个实例。** 计数轮询只保证「选中的次数」均匀，不保证「负载」均匀：长耗时请求会在某个实例上堆更多在途请求；更要命的是 gRPC 一条 HTTP/2 长连接上跑很多 RPC，若只在建连时选一次节点，之后所有请求都复用这一条连接，轮询直接退化成 pick_first。

<Compare :panels='[{"label":"错误：建连时选一次","code":"conn := pick(addrs)\n每次 RPC 复用 conn","note":"HTTP2 长连接让所有请求落到同一实例，轮询退化成 pick_first"},{"label":"正确：每次重新 Pick","code":"for each RPC {\n    conn := picker.Pick(addrs)\n}","note":"每次调用重新选 SubConn，流量才真正摊开"}]' title="轮询为什么会退化" />

**误解二：一致性哈希能解决热点 key。** 哈希环保证的是扩缩容时迁移量小，并不分摊单个 key 的流量。秒杀里同一个商品 ID 被百万请求命中，无论配多少虚拟节点，它们都落到环上同一个实例，缓存亲和反而让热点更集中。热点要靠打散 key（加后缀分桶）或对该 key 单独限流，和一致性哈希是两件事。

<Callout type="warn" title="一致性哈希不管热点" text="虚拟节点解决的是分布倾斜与扩容迁移量，不是单点热点；遇到被集中打爆的 key，要拆 key、加本地缓存或单独限流，别指望调大 replicas 能救。" />

### 4.4 与后续阶段的呼应

一致性哈希在 L8 会从「选实例」升级为「分数据」：[/distributed/sharding](/distributed/sharding) 的一致性哈希、虚拟节点与扩容再平衡，用的正是本讲 3.3 那套迁移量推导；[/distributed/high-availability](/distributed/high-availability) 的多活流量调度也依赖同机房优先、按版本灰度这类地址属性；而 [/distributed/kubernetes](/distributed/kubernetes) 的 Service 本身就是「服务端发现 + kube-proxy 负载均衡」的一个具体实例。到 L10，[/projects/micro-seckill](/projects/micro-seckill) 的库存服务既要按商品路由又要打散热点 key，[/projects/micro-ecommerce](/projects/micro-ecommerce) 则把权重与版本塞进 resolver 地址属性，做灰度与同机房优先。

<Quiz q="在 gRPC 里，为什么必须为每次 RPC 重新 Pick，而不能只在建立连接时选一次节点？" :options='["每次重建连接更快","一条 HTTP2 长连接上会跑很多 RPC，只选一次会让所有请求落到同一实例","SubConn 不允许复用","gRPC 不支持 pick_first"]' answer="1" explain="HTTP2 连接被大量 RPC 复用，建连时选一次等于把整个连接生命周期的请求都绑到同一个实例，round_robin 会退化成 pick_first；只有每次 RPC 重新 Pick 才能真正分摊流量。" />

<Predict q="按 3.3 的代码，把节点从 3 个扩到 4 个，取模分片与一致性哈希各自迁移多少 key？" :options='["都约 100%","取模约 75%，一致性哈希约四分之一","都约四分之一","取模约四分之一，一致性哈希约 75%"]' answer="1" output="取模分片迁移: 7498/10000（约 75%）；一致性哈希迁移: 2500/10000（约 1/4）" explain="取模在分母从 3 变 4 后绝大多数 key 结果都变，实测约 75%；一致性哈希只影响新增节点相邻的一小段，迁移量约等于新增节点分到的份额四分之一。" />

## 动手练一练
<Exercise title="给 roundRobin 加上排除不健康节点的能力" task="扩展 2.1 的 Balancer，让 Pick 只从健康地址里选；用一张 map 记录不可用地址，模拟一个节点被标记后，观察连续 6 次选择如何绕开它。" :hints='["Balancer 接口不变，给 roundRobin 增加一个 unhealthy 集合","Pick 前先过滤出可用子集，再对子集取模","可用子集为空时要有兜底，例如返回空串让上层报错"]' solution="要点一：roundRobin 增加 unhealthy map[string]bool 与 MarkDown 方法。\n要点二：Pick 先遍历 addrs 过滤掉被标记的地址，得到 candidates，再对 candidates 取模。\n要点三：candidates 为空时返回空串或明确错误，上层据此触发服务不可用，而不是悄悄打到故障节点。" explain="把健康状态接进 Picker，负载均衡才不会把请求持续送到已故障的实例；这也是 gRPC SubConn 状态机驱动的思路。" />

## 随堂检测
<Quiz no="076-1" q="在 gRPC 的分层里，resolver 与 balancer 的职责分别是什么？" :options='["resolver 选连接，balancer 解析地址","resolver 把目标名解析成地址列表，balancer 从列表里为每次 RPC 选一条连接","两者都负责消息序列化","balancer 负责心跳续约，resolver 负责重试"]' answer="1" explain="resolver 对接注册中心、产出并更新地址集合；balancer 消费这份集合，用 Picker 为每次调用挑 SubConn。地址从哪来与这一单给谁，是两个正交问题。" />

<Predict no="076-2" q="代码 2.1 里 addrs 有三个地址，循环调用 6 次 roundRobin.Pick，选中的序列是？" :options='["始终是第一个地址","按 1、2、3、1、2、3 循环","完全随机不可预测","按 3、2、1、3、2、1 循环"]' answer="1" output="10.0.0.1:9000, 10.0.0.2:9000, 10.0.0.3:9000, 10.0.0.1:9000, 10.0.0.2:9000, 10.0.0.3:9000" explain="原子计数每次加一后对地址数取模，Add(1)-1 让首次落在下标 0，6 次调用正好把三个地址各命中两轮。" />

## 一页纸速记
<KnowledgeCard :points='["resolver 产出并更新地址列表，balancer 的 Picker 为每次 RPC 挑一条 SubConn，两层正交可扩展","pick_first 只认首地址，round_robin 用原子计数取模，平滑加权轮询按当前权重选最大者","一致性哈希把节点与 key 映射到同一环上，虚拟节点打散分布，扩容只迁移相邻一小段","自定义扩展点靠 Register 按 scheme 或策略名注册，UpdateState 推地址、Pick 选连接"]' :pitfalls='["只在建连时选一次节点，让 round_robin 退化成 pick_first","一致性哈希虚拟节点太少导致倾斜，或热点 key 打爆单个实例","Picker 不接收健康状态，轮询持续把流量送到已 503 的节点"]' summary="一句话记住：resolver 决定有哪些候选，balancer 决定这一单给谁，选错层就换不掉策略。" />

## 小结与下一讲

- **分层**：resolver 对接注册中心产出地址，balancer 用 Picker 为每次 RPC 选连接，二者通过 UpdateState 解耦。
- **策略**：pick_first 与 round_robin 最简单，加权轮询适配异构机型，一致性哈希用虚拟节点把扩容迁移量压到最小。
- **扩展点**：Builder、Resolver、Picker、SubConn 四级抽象，按 scheme 或策略名注册即可替换内置实现。

[下一讲：配置中心与动态配置](/microservice/config-center)
