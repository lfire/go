---
title: 第 89 讲 · 高可用：多活、容灾与混沌工程
description: 面向故障设计而不是祈祷不故障，重点拆解重试放大、故障域与演练
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="50 分钟" goal="面向故障设计，能用冗余、限流与演练把故障关进可控范围" prereq="Raft 共识与 etcd 实践" />

# 第 89 讲 · 高可用：多活、容灾与混沌工程

> 前面几讲都在处理「某个组件坏了怎么办」，这一讲把它体系化：不假设机器不宕、机房不倒、流量不涨，而是主动问「故障真的发生时，系统会怎样」。高可用不是把可用性写成几个 9，而是一组工程动作——把冗余放进彼此独立的故障域、给容量留余量、用限流挡住洪峰、把预案拿去真实演练，并在事后复盘。本讲尤其要拆穿一个最常见的假象：**下游故障时，重试到底是救了系统，还是把它压垮。**

## 这一讲你会得到什么
<CheckList id="goal-89" title="本讲目标" :items='["用故障域与 N+1 冗余解释单点和机房级故障的差别","说清多活架构的收益与跨地域数据冲突代价","算出多层重试的放大倍数并给出四项防护","用令牌桶实现限流，用健康检查完成故障域摘除","读懂一份混沌注入配置并说出爆炸半径的控制方法"]' />

## 1. 先建立直觉
<ConceptDiagram type="circuit-breaker" title="面向故障：限流、熔断与故障隔离" />

高可用有一条朴素到容易被忽视的原则：**冗余必须放在不同的故障域里**。两台机器放在同一个机架、同一个交换机、同一个电源下，一次断电一起没，这不叫冗余，叫复制了一份风险。真正的故障域要按「共享什么就一起坏什么」来划分：进程、机器、机架、可用区、地域，一层比一层大。

第二原则是容量留余量。三个可用区各扛三分之一的流量，看着均衡，可一个区整体宕掉后，剩下两区要各扛一半——如果平时就跑到八成水位，这一刻必然被压垮。所以多活不是「把流量摊开」，而是「任何一域消失后，其余域仍能接住」，也就是常说的 N+1 余量。

第三原则最反直觉：**故障发生时要少做，而不是多做**。下游已经超时，上游若立刻重试、每一层都重试，请求量会成倍叠加，把一个局部抖动放大成全站雪崩。这一讲的重试放大，是「面向故障设计」里最值得单独拎出来的真实风险。

## 2. 语法与规则

先实现最基础的保护手段——令牌桶限流：按固定速率补令牌，取到令牌才放行，超出的请求直接拒绝，把流量削成系统能承受的形状。

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// TokenBucket 按固定速率补令牌，取到才放行
type TokenBucket struct {
	mu       sync.Mutex
	capacity float64
	rate     float64 // 每秒补充的令牌数
	tokens   float64
	last     time.Time
}

func NewTokenBucket(capacity, rate float64) *TokenBucket {
	return &TokenBucket{capacity: capacity, rate: rate, tokens: capacity, last: time.Now()}
}

// Allow 先按流逝时间补令牌，再尝试取一个
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func main() {
	b := NewTokenBucket(5, 2) // 容量 5，每秒补 2 个
	pass, reject := 0, 0
	for i := 0; i < 12; i++ {
		if b.Allow() {
			pass++
		} else {
			reject++
		}
	}
	fmt.Println("突发 12 个请求，放行:", pass, "拒绝:", reject)
	fmt.Println("限流后系统仍可服务:", pass > 0 && reject > 0)
}
```

<OutputBox title="运行结果" output="突发 12 个请求，放行: 5 拒绝: 7\n限流后系统仍可服务: true" />

容量 5 的桶先放掉前 5 个突发请求，其余 7 个被挡在门外。注意这里「拒绝」不是失败，而是主动选择：宁可让一部分用户稍后重试，也不让洪峰把数据库连接池打满、把整个服务拖死。

<LineByLine code="type Zone struct {\nName string\nHealthy bool\nWeight int\n}" :notes='["Zone 建模一个可用区，也就是一个独立故障域","Name 标识可用区，路由与摘除都按它定位","Healthy 由健康检查维护，决定该域是否接流量","Weight 预留给多活权重，用于按容量分配比例","结构体结束，故障转移就是改 Healthy 再重算路由"]' />

接着把最危险的现象量化。下面不依赖随机数，纯用算术算出多层重试在最坏情况下会把一次用户请求放大成多少次下游调用。

```go
package main

import "fmt"

// amplified 计算多层重试链最坏情况下的放大倍数。
// depth 是调用层数，retries 是每层重试次数。
func amplified(depth, retries int) int {
	if depth == 1 {
		return retries + 1
	}
	return (retries + 1) * amplified(depth-1, retries)
}

func main() {
	for _, depth := range []int{1, 2, 3, 4} {
		fmt.Printf("调用层数 %d、每层重试 2 次 -> 最坏下游调用 %d 次\n", depth, amplified(depth, 2))
	}
	fmt.Println("下游完全故障时，重试不会带来成功，只会放大压力")
	fmt.Println("三层调用链无预算时放大到:", amplified(3, 2), "倍")
}
```

输出是 3、9、27、81：三层调用链、每层重试两次，一次用户请求最坏变成 27 次下游调用。若下游此时是 100% 故障，这 27 次全部失败，纯粹是在给已经垂死的系统加担子。这就是**重试放大**，也是雪崩最常见的点火方式。

## 3. 深入一步

把高可用手段按「中途失败会怎样」排开，选型就从口号变成了清单：

| 手段 | 防住什么 | 中途失败会怎样 | 代价 |
| --- | --- | --- | --- |
| 冗余 / 无单点 | 单机宕机 | 备节点接管，健康检查有延迟窗口 | 成本翻倍 |
| 故障域隔离 | 机架、可用区故障 | 单域全挂，流量压向其余域 | 需预留 N+1 容量 |
| 多活 | 地域级故障 | 跨地域同步有延迟，可能写冲突 | 单元化与路由复杂 |
| 限流 / 熔断 | 洪峰、下游雪崩 | 超限请求被拒，保住核心链路 | 牺牲部分可用性 |
| 混沌演练 | 未知的故障路径 | 注入故障暴露隐藏依赖 | 需可控爆炸半径 |

<Term word="故障域" text="共享同一套电源、网络或机房、因而会一起失效的一组资源，冗余必须跨故障域放置才有意义。" />
<Term word="重试放大" text="多层调用链各自重试，在下游故障时把请求量按乘积放大，足以把局部故障推成全站雪崩。" />
<Term word="爆炸半径" text="一次演练或故障能影响到的最大范围，混沌工程必须把它限制在单可用区、小比例流量内。" />
<Term word="RTO / RPO" text="恢复时间目标与恢复点目标，分别规定多久必须恢复、最多能丢多少数据，是容灾预案的量化基线。" />

<Callout type="warn" title="重试放大的四项防护" text="别让重试裸奔：一要设全局重试预算，超过就快速失败；二要指数退避加随机抖动，避免所有客户端同时重发形成脉冲；三要只对可重试错误重试，参数错误这类永久失败重试纯属浪费；四要配熔断与幂等，熔断在下游持续失败时直接断开，幂等保证重放不产生重复数据。" />

**多活**比「主备」更难的地方，正在中途失败：单地域整体不可用时，流量要切到其他地域，可跨地域的数据同步往往有秒级延迟，两地的写入可能冲突。工程上常用**单元化**规避——按用户维度把数据封闭在同一个单元里，请求尽量不跨地域，切流时整单元迁移，而不是让两份数据同时可写。这也呼应了前面 CAP 的取舍：跨地域多活接受的，多半是最终一致。

### 从乘法推导重试放大，从 N+1 推导容量水位

第 2 节 `amplified(depth, retries)` 的递归不是凭空写的。设每层最多尝试 `r+1` 次（1 次原始加 r 次重试），调用链深度 `d`，且**每层独立重试、没有全局预算**：第 1 层产生 `r+1` 次调用，第 2 层对每一次又各自产生 `r+1` 次，于是最坏下游调用数是 `(r+1)^d`。d=3、r=2 即 `3^3 = 27`，与输出一致。这条公式也直接给出四项防护的理论依据：

- **全局重试预算**：把整条链的总尝试数封顶为 `B`，放大倍数就从 `(r+1)^d` 降为与深度无关的量级。
- **指数退避加抖动**：不改变最坏次数，但把重试在时间上摊开，避免所有客户端在同一时刻重发形成脉冲；抖动让各客户端的退避时刻互不重合。
- **只重试可重试错误**：永久失败（参数错误、鉴权失败）不该进入乘法链，等价于把这些分支的 `r` 置 0。
- **熔断**：下游持续失败时把该分支整体断开，等价于把 `r` 置 0 并快速失败。

N+1 容量可以同样算出来：设总流量 `T`，`k` 个对等故障域均分，每域平时 `T/k`；一个域整体宕掉后剩余 `k-1` 域各承担 `T/(k-1)`。若平时水位是 `w`（即每域容量为 `T/(k·w)`），切流后水位变成 `w · k/(k-1)`。k=3、w=0.8 时是 `0.8 × 1.5 = 1.2`——**超过 100%，必然被打垮**。所以「平时跑到八成」不是保守，是不满足 N+1；要满足就得让 `w` 不超过 `(k-1)/k`，三域即不超过 66.7%。这把第 1 节「任何一域消失后其余域仍能接住」从口号变成一条能算的不等式。

<SequenceDiagram title="故障域摘除与流量切流" caption="健康检查标记不健康，路由摘除该域，剩余域水位上升触发限流" :actors='["Client","LB","ZoneA","ZoneB","HealthCheck"]' :steps='[{"from":"HealthCheck","to":"ZoneB","label":"连续探测失败","kind":"call"},{"from":"ZoneB","to":"HealthCheck","label":"超时无响应","kind":"return"},{"from":"HealthCheck","to":"LB","label":"标记 zone-b 不健康","kind":"call"},{"from":"Client","to":"LB","label":"请求到达","kind":"call"},{"from":"LB","to":"ZoneA","label":"只路由到健康域","kind":"call"},{"from":"ZoneA","to":"LB","label":"水位升至 1.2 倍","note":"容量告警","kind":"return"},{"from":"LB","to":"LB","label":"触发限流与熔断","kind":"self"},{"from":"LB","to":"Client","label":"部分请求快速失败","note":"保住核心链路","kind":"return"},{"from":"HealthCheck","to":"ZoneB","label":"恢复后重新探测","kind":"call"},{"from":"ZoneB","to":"HealthCheck","label":"探测成功","kind":"return"},{"from":"HealthCheck","to":"LB","label":"重新纳入路由","kind":"call"},{"from":"LB","to":"ZoneA","label":"水位回落","kind":"call"}]' />

### 结论的前提与失效条件

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 冗余有效 | 副本放在彼此独立的故障域 | 同机架、同电源、同可用区时一次故障一起没，冗余只是复制风险 |
| N+1 能接住单域故障 | 单域故障、其余域健康、流量可整体迁移 | 多域同时故障、或有状态服务无法跨域迁移、或切流本身有延迟 |
| 重试放大公式 `(r+1)^d` | 各层独立重试且下游完全失败 | 下游部分可用时重试确实能提升成功率；公式是最坏上界，不是常态 |
| 多活可切流 | 数据能跨地域同步或按单元封闭 | 跨地域同步有秒级延迟，两地同时可写会冲突；单元化能规避但要求路由按用户维度严格封闭 |
| 限流保住核心链路 | 能区分核心与非核心请求 | 限流规则不分级，把核心请求一起拒掉 |

范围说明：本讲讨论的是**崩溃、机房或可用区级故障与流量洪峰**，不涉及拜占庭故障与跨地域网络被恶意篡改；也不把「数据一致性」纳入——多活下的写冲突属于第 83 讲 CAP 的取舍，这里默认最终一致。RTO 与 RPO 的量化基线也必须事先与业务约定，否则演练完了无法判断达标与否。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| 两台机器就是高可用，冗余到位了 | 同机架同电源下，一次断电两台一起没 | 冗余必须跨故障域，并按 `w` 不超过 `(k-1)/k` 预留容量 |
| 下游超时多试几次总能成功 | 下游完全故障时重试只放大压力，乘法链把局部抖动推成雪崩 | 重试要配全局预算、退避抖动、熔断与幂等，且只对可重试错误重试 |
| 混沌演练就是把故障注入进去，越真实越好 | 不限定爆炸半径的演练本身就是事故 | 从单 Pod、单可用区、小比例流量逐级放大，任何时刻可一键撤销 |

## 4. 工程实践：故障域摘除与混沌演练

下面模拟健康检查把一个可用区整体摘除，其余区接住全部流量。注意代码里那句提醒：摘除之后剩余域的负载立刻上升，这正是 N+1 容量必须提前算好的原因。

```go
package main

import (
	"fmt"
	"sync"
)

// Zone 是一个可用区，Healthy 决定是否接流量
type Zone struct {
	Name    string
	Healthy bool
	Weight  int
}

// Router 只在健康区里轮询分配流量
type Router struct {
	mu    sync.Mutex
	zones []Zone
}

// Pick 全部不健康才返回空，避免把请求打向已死的域
func (r *Router) Pick(req int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	healthy := make([]int, 0, len(r.zones))
	for i, z := range r.zones {
		if z.Healthy {
			healthy = append(healthy, i)
		}
	}
	if len(healthy) == 0 {
		return ""
	}
	idx := healthy[req%len(healthy)]
	return r.zones[idx].Name
}

// FailZone 把某个可用区整体摘除，模拟机房级故障
func (r *Router) FailZone(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.zones {
		if r.zones[i].Name == name {
			r.zones[i].Healthy = false
			fmt.Println("摘除故障域:", name)
		}
	}
}

func main() {
	r := &Router{zones: []Zone{
		{"zone-a", true, 1}, {"zone-b", true, 1}, {"zone-c", true, 1},
	}}
	for i := 0; i < 3; i++ {
		fmt.Println("请求", i, "路由到:", r.Pick(i))
	}
	// 模拟一个机房整体宕机
	r.FailZone("zone-b")
	for i := 0; i < 4; i++ {
		fmt.Println("故障后请求", i, "路由到:", r.Pick(i))
	}
	fmt.Println("剩余健康区承担全部流量，需要提前留足容量余量")
}
```

<CodeTabs title="故障时的三道防线" :tabs='[{"label":"限流","code":"bucket.Allow()","note":"削平突发洪峰，超出容量的请求直接拒绝"},{"label":"熔断","code":"breaker.Allow()","note":"下游持续失败就断开，不再把请求送过去等死"},{"label":"降级","code":"return fallback()","note":"非核心功能返回兜底值，把资源让给核心链路"}]' />

<Compare title="重试的两种写法" :panels='[{"label":"裸重试","code":"retryForever(downstream)","note":"下游故障时请求量指数放大，叠加多层调用直接引发雪崩"},{"label":"带预算退避","code":"retryWithBudget(downstream, 3)","note":"全局预算封顶，指数退避加抖动，配合熔断与幂等"}]' />

演练不能停在纸面。下面是一份把某个可用区网络分区 5 分钟的混沌注入配置，重点看它如何用 `selector` 把爆炸半径限制在单个标签域内：

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: zone-b-partition
spec:
  action: partition
  selector:
    labelSelectors:
      zone: zone-b
  duration: 5m
```

<FlowTimeline direction="horizontal" :steps='[{"title":"注入故障","desc":"对单可用区做网络分区，限定爆炸半径","tone":"blue"},{"title":"健康检查","desc":"连续失败超阈值，标记该域不健康","tone":"red"},{"title":"摘除切流","desc":"路由把流量切到健康域，容量水位上升","tone":"red"},{"title":"限流兜底","desc":"洪峰触发限流与熔断，保住核心链路","tone":"red"},{"title":"恢复复盘","desc":"撤销注入，记录 RTO 与预案缺口","tone":"green"}]' />

演练的完整闭环是「注入 → 观测 → 摘除 → 限流 → 恢复 → 复盘」，而且必须从最小爆炸半径开始：先单个 Pod、再单个可用区、最后才敢做地域级，任何时刻都能一键撤销。复盘不追责个人，只追问三件事：监控有没有先于用户发现、预案有没有真的生效、这次故障暴露了哪个此前不知道的隐藏依赖。把这些写进文档并回灌到限流阈值与容量模型，高可用才是可持续的工程能力，而不是一次救火。

### 可验证实验：混沌注入加 vegeta 压测，逐字段读输出

把第 4 节那份 NetworkChaos 配置真正应用起来，再用 vegeta 观察限流前后的差异：

```bash
# 只对 zone-b 标签的 Pod 做 5 分钟网络分区
kubectl apply -f networkchaos-zone-b.yaml
kubectl get networkchaos zone-b-partition -o wide
# 观察 Pod 与端点变化
kubectl get pods -l zone=zone-b -o wide
# 压测：限流关闭与开启各打 30 秒
echo "GET http://myservice.internal/" | vegeta attack -rate=500 -duration=30s | vegeta report
```

```text
$ kubectl get networkchaos zone-b-partition -o wide
NAME                 AGE   ACTION     DURATION   SELECTOR
zone-b-partition     12s   partition  5m         zone=zone-b
$ kubectl get pods -l zone=zone-b -o wide
NAME                        READY   STATUS    RESTARTS   AGE   IP           NODE
myservice-zone-b-7d9c8      0/1     Running   0          3m   10.42.1.17   node-b
$ vegeta report
Requests      [total, rate, throughput]         15000, 500.00, 331.20
Duration      [total, attack, wait]             30s, 30s, 2.1s
Latencies     [min, mean, 50, 90, 95, 99, max] 2ms, 51ms, 12ms, 180ms, 320ms, 1.2s, 3.4s
Bytes In      [total, mean]                     480000, 32.00
Success       [ratio]                           66.24%
Status Codes  [code:count]                     200:9936  429:4250  503:814
```

逐字段解读：`Requests [total, rate, throughput]` 里 `total=15000` 是发出的请求数（500 每秒乘以 30 秒），`throughput=331.20` 是实际成功处理的每秒数，两者之差正是被限流与故障挡掉的部分；`Latencies` 的 `99=1.2s`、`max=3.4s` 说明尾部请求在排队或等待下游超时，这是雪崩前兆；`Success [ratio]=66.24%` 对应 `Status Codes` 里 `200:9936`，`429:4250` 是令牌桶主动拒绝，`503:814` 是故障域摘除后短暂无健康端点。对照演练前（无限流、无分区）通常 `Success` 接近 100%、`throughput` 接近 `rate`、99 分位在几十毫秒——把两组报告放一起，就能看出「限流牺牲部分可用性、保住核心链路」到底换来了什么。`kubectl get pods` 里 `READY 0/1` 正是就绪探针失败、端点被摘除的直接证据；`SELECTOR zone=zone-b` 确认注入被限制在单个标签域，也就是爆炸半径受控。

<Quiz q="三个对等可用区按 N+1 设计，平时单区水位上限约为多少？" :options='["50%","66.7%","80%","100%"]' answer="1" explain="k=3 时要让平时水位不超过 (k-1)/k 即三分之二，切流后剩余两区的水位才不会超过 100%；跑到八成水位单域宕机就会被压垮。" />

### 失败矩阵与演练手段

| 注入点 | 现象 | 防护 | 演练手段 | 回滚 |
| --- | --- | --- | --- | --- |
| 单 Pod 被杀 | 副本数下降，其余接住 | 冗余加健康检查 | `kubectl delete pod` | 控制器自动重建 |
| 单可用区网络分区 | 该域端点消失，其余水位上升 | 故障域隔离加 N+1 容量 | NetworkChaos `partition`，`selector` 限定标签 | `kubectl delete networkchaos` 一键撤销 |
| 下游服务完全超时 | 多层重试放大成雪崩 | 重试预算、退避抖动、熔断、幂等 | `tc netem` 注入延迟加裸重试对照 | 关闭重试预算回退到快速失败 |
| 流量洪峰 3 倍 | 连接池打满、P99 飙升 | 令牌桶限流加降级 | `vegeta -rate` 逐步加压找拐点 | 调低限流阈值、扩容 |
| 地域级故障切流 | 跨地域同步延迟、写冲突 | 单元化加按用户维度路由 | 预发环境整单元切流演练 | 切回原单元，核对冲突 |
| 演练脚本误配 selector | 故障扩散到全集群 | 爆炸半径审批加白名单 | 先在单 Pod 验证 selector | 立刻删除注入对象 |

演练闭环务必走满「注入、观测、摘除、限流、恢复、复盘」，并记录 RTO（从注入到恢复服务的秒数）与 RPO（有无数据丢失）。回滚不只是删掉混沌对象，还要确认限流阈值、路由权重、容量水位都回到演练前的基线，否则「演练结束」会留下隐藏的降级状态。

### 替代路线与取舍

| 路线 | 故障时行为 | 数据一致性 | 成本 | 适合 |
| --- | --- | --- | --- | --- |
| 主备加故障转移 | 备接管，有秒级到分钟级 RTO | 多为强一致或半同步 | 低，备机常闲 | 有状态数据库、非核心服务 |
| 多活（单元化） | 整单元切流，用户无感 | 多为最终一致，需单元封闭 | 高，全地域常驻容量 | 核心在线服务、地域级容灾 |
| 多活（两地同时可写） | 切流无延迟 | 需冲突合并，最复杂 | 最高 | 极少数能容忍写冲突的业务 |
| 纯冗余加限流降级 | 不切流，只削峰与兜底 | 不涉及 | 最低 | 无状态服务、流量型故障 |
| 异地冷备 | 恢复慢（小时级 RTO），数据有丢失 | 依赖备份频率，RPO 大 | 最低 | 合规归档、可接受长 RTO |

取舍主线是「RTO 与 RPO 越严格，成本越高」：从冷备到主备、再到单元化多活，本质都是在买「恢复时间」和「少丢数据」。别为了「几个 9」直接上多活，先确认业务真正能容忍的 RTO 与 RPO 是多少。

### 与后续实战的衔接

- [分布式任务调度系统](/projects/concurrent-scheduler) 的「租约与故障转移」就是主备加故障转移这条路线的具体化：Master 宕机后靠 etcd 租约过期触发重新选主，RTO 取决于租约时长。
- [秒杀与高并发库存](/projects/micro-seckill) 的「压测与预案」会直接复用本讲的 vegeta 加压、找性能拐点、验证限流阈值这套流程。
- [Kubernetes Operator 与可观测平台](/projects/cloud-operator) 的「发布与运维」把混沌演练与告警规则收进控制器，让爆炸半径与回滚动作变成可声明、可重复执行的配置。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-089-high-availability`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-089-high-availability)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="算清重试放大" task="三层调用链、每层重试 2 次，最坏会把一次用户请求放大成多少次下游调用？列出至少三项防护措施。" :hints='["每层最多尝试 3 次，逐层相乘","想想预算、退避与熔断"]' solution="每层 3 次尝试，三层相乘：3 * 3 * 3 = 27 倍；\n下游完全故障时重试只放大压力不带来成功；\n设全局重试预算、指数退避加随机抖动、只重试可重试错误；\n配合熔断与幂等，把放大倍数封顶" explain="重试放大是雪崩最常见的点火方式，防护的核心是把无界重试变成有预算、有退避、可熔断的有界行为。" />

## 随堂检测
<Quiz no="89-1" q="下游服务已经持续超时，上游多层调用链仍各自重试，最可能的后果是？" :options='["成功率上升","请求量指数放大，把局部故障推成雪崩","延迟明显下降","自动触发故障转移"]' answer="1" explain="每层重试相乘放大请求量，下游完全故障时重试不带来成功，只给垂死系统加压。" />
<Predict no="89-2" q="三个可用区各扛三分之一流量，一个区整体宕机后，其余两区的负载会变成多少？" :options='["仍各三分之一","各二分之一","各四分之一","保持不变"]' answer="1" output="各二分之一" explain="总流量不变、健康域减少，剩余域负载上升，所以平时必须按 N+1 预留容量。" />
<FillBlank no="89-3" tpl="冗余必须放在不同的 ___ 里才有效；限流常用 ___ 算法；混沌演练要先控制 ___。" :answers='["故障域","令牌桶","爆炸半径"]' hint="共享什么就一起坏什么、削峰、最小影响范围" explain="故障域决定冗余是否真冗余，令牌桶削平突发，爆炸半径决定演练能影响多大范围。" :explains='["副本若共用电源、机架或可用区，一次故障会一并打掉所有实例，冗余形同虚设；只有分散到彼此独立的失效单元才真能容错。","桶以固定速率补令牌、请求取到才放行，桶满即丢弃，因而既把突发流量削成平稳输出，又允许一定程度的短时积压。","注入故障前先界定会被波及的实例、流量与依赖边界，从单实例小流量起步，一旦失控立即熔断，避免演练变成真实全站事故。"]' />

## 一页纸速记
<KnowledgeCard :points='["冗余跨故障域放置，并按 N+1 预留容量","多活切流要面对跨地域同步延迟与写冲突，常用单元化规避","重试放大按层相乘，必须用预算、退避抖动、熔断与幂等封顶","混沌演练从最小爆炸半径起步，闭环是注入到复盘"]' :pitfalls='["下游故障时无限重试，把局部抖动放大成全站雪崩","平时把容量跑到八成水位，单域宕机后其余域直接被压垮","演练不限定爆炸半径，把实验变成真实事故"]' summary="高可用不是祈祷不故障，而是把每一次故障都关进事先量好的边界里。" />

## 小结与下一讲

- 故障域、N+1 容量与限流是面向故障设计的三块地基，冗余必须跨故障域才有效。
- 重试放大是最隐蔽的雪崩点火方式，靠重试预算、退避抖动、熔断与幂等封顶。
- 混沌演练用可控爆炸半径主动暴露隐藏依赖，闭环到 RTO 记录、预案修正与复盘。

把系统做成能扛故障的样子之后，下一步是把它稳定、可复现地交付出去——从容器镜像开始。

[下一讲：容器化：多阶段构建与镜像瘦身](/distributed/docker)
