---
title: 第 79 讲 · 熔断、降级、重试与雪崩防护
description: 用重试预算、熔断状态机、降级兜底与舱壁隔离，让局部故障不演变成全站雪崩
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="让局部故障不演变成全站故障" prereq="第 78 讲 API 网关、鉴权与限流" />

# 第 79 讲 · 熔断、降级、重试与雪崩防护

> 分布式系统里最反直觉的一条规律是：为了「更可靠」而加的重试，往往正是把小故障放大成全站雪崩的元凶。下游稍微变慢，上游纷纷超时重试，瞬时请求量成倍翻，本就吃力的下游被彻底压垮，接着相邻服务被拖垮，故障沿调用链级联扩散。本讲用标准库把重试预算、熔断状态机、降级兜底和舱壁隔离各造一遍，并推演一次雪崩是怎么被「好意的重试」点燃的。学完你会明白：稳定性不是加了多少重试，而是有没有给失败设好边界。

## 这一讲你会得到什么
<CheckList id="l79-goals" title="本讲目标" :items='["理解重试放大的数学，用重试预算把重试量限制成总请求的一个比例","自建 Closed、Open、HalfOpen 三态熔断器，说清冷却与半开探测的迁移条件","用舱壁隔离给每个依赖单独的并发配额，并配合降级兜底保住核心链路","能推演一次雪崩从下游变慢到全站失效的完整链条，并给出每道防线"]' />

## 1. 先建立直觉

把下游服务想成一条单车道的收费站。平时车流畅通；某天一个收费员手慢了，后面开始排队。如果每个排队的司机一看没过就「掉头重新排一次」（重试），车道上的车不是变少而是成倍变多，几分钟后整条路彻底堵死——这就是雪崩。正确的做法不是让司机多试几次，而是三件事：限定「最多补几次票」（重试预算）、发现队伍太长就暂时封路让大家走备用道（熔断加降级）、并且给这条车道单独留一块场地，别让它把旁边车道的空间也吃光（舱壁隔离）。

对应到工程：超时决定「等多久算放弃」，重试预算决定「能补几次」，熔断器决定「要不要暂时别打了」，降级决定「打不通时给用户什么」，舱壁决定「这个依赖最多占多少并发」。五者缺一，故障就会有缝可钻。

<ConceptDiagram type="circuit-breaker" title="熔断器三态与雪崩防线" caption="失败累积触发熔断，冷却后半开探测，恢复则闭合；重试预算与舱壁在两侧守住流量" />

<FlowTimeline :steps='[{"title":"下游变慢","desc":"单点延迟升高，请求开始堆积","tone":"blue"},{"title":"超时触发","desc":"上游等待超时后开始重试","tone":"blue"},{"title":"重试放大","desc":"每个原始请求变成多次，流量成倍翻","tone":"orange"},{"title":"资源耗尽","desc":"连接池与 goroutine 被打满","tone":"orange"},{"title":"级联失败","desc":"相邻依赖相继被拖垮","tone":"orange"},{"title":"全站雪崩","desc":"无熔断与隔离时故障扩散到整条链路","tone":"orange"}]' direction="vertical" />

## 2. 语法与规则

### 2.1 熔断器状态机

熔断器有三态：`Closed` 正常放行并累计失败；失败达到阈值切到 `Open`，一段时间内所有调用直接短路；冷却期满进入 `HalfOpen`，只放少量探测流量，成功够数才回到 `Closed`，再失败则重新熔断。半开态是关键——否则熔断永远没机会自愈。

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

type Breaker struct {
	mu           sync.Mutex
	state        State
	failures     int
	threshold    int
	openUntil    time.Time
	cooldown     time.Duration
	halfOpenMax  int
	halfOpenSucc int
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{state: Closed, threshold: threshold, cooldown: cooldown, halfOpenMax: 2}
}

var ErrOpen = errors.New("circuit open")

// Allow 在调用前判定是否放行；Open 期间直接短路，冷却到期转 HalfOpen。
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Open:
		if time.Now().Before(b.openUntil) {
			return ErrOpen
		}
		b.state = HalfOpen
		b.halfOpenSucc = 0
		return nil
	default:
		return nil
	}
}

// Record 在调用后记录成功或失败，驱动状态迁移。
func (b *Breaker) Record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		switch b.state {
		case HalfOpen:
			b.halfOpenSucc++
			if b.halfOpenSucc >= b.halfOpenMax {
				b.state = Closed
				b.failures = 0
			}
		default:
			b.failures = 0
		}
		return
	}
	switch b.state {
	case HalfOpen:
		b.trip() // 探测仍失败，立刻重新熔断
	default:
		b.failures++
		if b.failures >= b.threshold {
			b.trip()
		}
	}
}

func (b *Breaker) trip() {
	b.state = Open
	b.openUntil = time.Now().Add(b.cooldown)
	b.failures = 0
}

func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return [...]string{"Closed", "Open", "HalfOpen"}[b.state]
}

func main() {
	b := NewBreaker(3, 300*time.Millisecond)
	// 连续 3 次失败，触发熔断
	for i := 0; i < 3; i++ {
		if err := b.Allow(); err == nil {
			b.Record(fmt.Errorf("downstream 503"))
		}
	}
	fmt.Println("失败后状态:", b.State(), "再次调用:", b.Allow())

	// 冷却到期后转半开，探测成功才恢复
	time.Sleep(320 * time.Millisecond)
	_ = b.Allow()
	b.Record(nil)
	_ = b.Allow()
	b.Record(nil)
	fmt.Println("探测恢复后状态:", b.State())
}
```

<OutputBox title="运行结果" output="失败后状态: Open 再次调用: circuit open\n探测恢复后状态: Closed" note="连续失败把熔断器推到 Open 并短路调用；冷却后经两次成功的半开探测，才谨慎地回到 Closed。" />

### 2.2 重试预算与退避

无预算的重试是雪崩的点火器。重试预算的思路很朴素：别按「每个请求重试几次」算，而是给整个进程一个「重试占总请求的比例」上限，按正常请求量慢慢攒额度，想重试先取额度，取不到就直接降级。再叠加指数退避与随机抖动，避免所有客户端齐步重试。

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// RetryBudget 把重试量限制为总请求的一个比例，防止下游变慢时重试自我放大。
type RetryBudget struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	ratio    float64 // 每来一个正常请求补充 ratio 个重试额度
}

func NewRetryBudget(capacity, ratio float64) *RetryBudget {
	return &RetryBudget{tokens: 0, capacity: capacity, ratio: ratio}
}

func (b *RetryBudget) OnRequest() { // 每个原始请求补充一点额度
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens += b.ratio
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
}

func (b *RetryBudget) Take() bool { // 想重试先取一个额度
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// backoff 指数退避加随机抖动，避免大量客户端在同一瞬间齐步重试。
func backoff(attempt int, base time.Duration) time.Duration {
	d := base << attempt
	jitter := time.Duration(rand.Int63n(int64(d / 2)))
	return d + jitter
}

func main() {
	budget := NewRetryBudget(3, 0.2) // 最多攒 3 个额度，每请求补 0.2
	allowed, denied := 0, 0
	for i := 0; i < 20; i++ {
		budget.OnRequest()
		if budget.Take() {
			allowed++
		} else {
			denied++
		}
	}
	fmt.Printf("20 次请求: 允许重试 %d 次, 预算拒绝 %d 次\n", allowed, denied)
	_ = backoff // 真实调用里按 attempt 递增使用指数退避
}
```

<OutputBox title="运行结果" output="20 次请求: 允许重试 4 次, 预算拒绝 16 次" note="ratio 为 0.2 时，20 个请求只攒出 4 个重试额度；即使下游全挂，重试流量也被死死压在 20% 以内。" />

<LineByLine code="b.mu.Lock()\ndefer b.mu.Unlock()\nb.tokens += b.ratio" :notes='["加锁保护共享额度，允许多个 goroutine 并发补充与取用","defer 保证返回时一定解锁，避免异常路径把锁占死","按固定比例补充重试额度，让重试量只随真实请求量增长"]' title="重试额度补充逐行拆解" />

## 3. 深入一步

### 3.1 舱壁隔离与降级兜底

舱壁（bulkhead）借自造船：用隔舱把船体分成互不漏水的区段，一个舱进水不至于沉船。服务里就是给每个下游依赖一个独立的并发配额，慢依赖最多占满自己的 quota，吃不掉全局的 goroutine 与连接。配合降级，配额满或调用失败时立刻返回兜底值，保住核心链路。

```go
package main

import (
	"errors"
	"fmt"
	"time"
)

// Bulkhead 用带缓冲 channel 当信号量，把某个下游的故障关在独立舱内。
type Bulkhead struct{ sem chan struct{} }

func NewBulkhead(n int) *Bulkhead { return &Bulkhead{sem: make(chan struct{}, n)} }

var ErrBusy = errors.New("bulkhead full")

func (b *Bulkhead) Do(fn func() (string, error)) (string, error) {
	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		return fn()
	default:
		return "", ErrBusy // 配额满，立即失败而非排队
	}
}

// CallWithFallback 把下游调用与降级兜底收在一处：忙或失败都返回兜底值。
func CallWithFallback(b *Bulkhead, fallback string) string {
	out, err := b.Do(func() (string, error) {
		time.Sleep(50 * time.Millisecond) // 模拟慢下游
		return "real-data", nil
	})
	if err != nil {
		return fallback
	}
	return out
}

func main() {
	b := NewBulkhead(1)
	b.sem <- struct{}{} // 手动占满唯一配额，模拟慢下游把舱占住
	fmt.Println("舱壁满时返回:", CallWithFallback(b, "cached-data"))
	<-b.sem // 释放配额
	fmt.Println("恢复后返回:", CallWithFallback(b, "cached-data"))
}
```

| 防护手段 | 解决什么 | 关键参数 | 失效风险 |
| --- | --- | --- | --- |
| 超时 | 慢调用长期占用资源 | 全链路 deadline | 设置过长或各层不一致 |
| 重试预算 | 故障时重试流量放大 | 重试占比、额度上限 | 无预算导致重试风暴 |
| 熔断器 | 持续失败仍反复打下游 | 失败阈值、冷却窗口 | 阈值过敏或过于迟钝 |
| 降级 | 依赖不可用时保核心链路 | 兜底值、开关 | 兜底本身又依赖故障源 |
| 舱壁隔离 | 单一依赖吃光全部并发 | 每依赖并发上限 | 配额过大等于没隔离 |

<Term word="重试预算" text="按总请求量的固定比例发放重试额度，从根本上限制重试流量被放大的倍数" />
<Term word="半开态 HalfOpen" text="熔断冷却后只放少量探测请求，成功够数才闭合，失败则重新熔断" />
<Term word="舱壁 Bulkhead" text="给每个依赖独立的并发配额，让单一依赖的慢或挂不会耗尽全局资源" />

<Callout type="warn" title="重试只对幂等且可恢复的错误做" text="写操作重试可能造成重复下单，必须先有幂等键；InvalidArgument、Unauthenticated 这类确定性错误重试纯属浪费。更要命的是重试放大：若下游已过载，每次失败重试 3 次会把请求量变成 4 倍，形成正反馈，越重试越慢、越慢越重试。" />

## 4. 工程实践

把五道防线按调用顺序串起来才有效：先设超时（不再无限等），再用重试预算决定要不要补一次并配退避，然后过熔断器（已熔断就直接短路），接着进舱壁（拿不到配额立刻降级），最后任何失败都走兜底。任何一环缺席，压力都会漏到下一环。

<Compare :panels='[{"label":"无预算重试","code":"retry := func() { call() }\nretry(); retry(); retry() // 固定 3 次","note":"下游一慢，每个请求都放大成 3 次，瞬时流量翻几倍，直接把下游打死"},{"label":"带预算重试","code":"if budget.Take() {\n    callWithBackoff()\n} else { return fallback() }","note":"重试量被限制为总请求的一个比例，配合退避，下游再慢也不会被自己人打垮"}]' title="重试会不会引发雪崩" />

落地时还有几条经验。超时与重试预算要一起算：若上游 deadline 只剩 100 毫秒，重试三次连一次都跑不完，不如直接放弃；一般把重试次数压到 1 到 2 次，并让所有重试共享同一个 deadline。熔断阈值用「最近窗口内的失败率」比单纯连续失败数更稳，避免偶发抖动误熔断；半开探测的成功数别设太小，防止一个恰好成功的请求就把流量全放回去。降级要分级：能返回缓存旧值最好，其次返回精简字段，最差也要给一个明确、快速、无副作用的空结果，绝不能让兜底逻辑再去调那个已经挂掉的下游。舱壁配额按依赖的重要性区分，核心依赖多给一些、非核心少给并优先降级。最后，熔断、降级、拒绝次数都必须打点上报——没有指标，你永远不知道防线到底有没有生效。

## 动手练一练
<Exercise title="给熔断器加失败率窗口与指标上报" task="扩展 2.1 的 Breaker：用「最近 N 次调用里失败占比」替代连续失败计数来触发熔断，并在每次状态迁移与短路时打印一行结构化日志。" :hints='["用一个定长切片做滑动窗口，记录每次调用成功或失败","窗口填满后计算失败比例，超过阈值才 trip","状态迁移时统一走一个 setState 辅助函数，在里面打日志"]' solution="要点一：Breaker 增加 window []bool 与 cursor，Record 时按环形写入并统计失败比例。\n要点二：窗口未满时先不熔断，避免启动期样本不足误判；达到阈值再调 trip。\n要点三：抽出 setState(next State)，所有迁移都经过它，在其中打印 from、to、时间与当前失败率。" explain="失败率窗口比连续计数更能容忍偶发抖动；把状态迁移收进统一入口，指标与日志才不会漏报。" />

## 随堂检测
<Quiz no="79-1" q="下游已经过载变慢，客户端对每个失败请求固定重试 3 次，最可能引发什么？" :options='["下游自动扩容并恢复","重试使总请求量成倍放大，把下游彻底压垮形成雪崩","错误率保持不变","超时被自动取消，毫无影响"]' answer="1" explain="重试把每个原始请求放大成多次，在下游本就吃力时形成正反馈：越慢越重试、越重试越慢，最终耗尽连接与 goroutine 并沿调用链级联扩散。" />

<Predict no="79-2" q="代码 3.1 里舱壁容量 1，手动占满配额后调用 CallWithFallback，再释放后调用，两次返回分别是什么？" :options='["两次都 cached-data","先 cached-data，后 real-data","两次都 real-data","先 real-data，后 cached-data"]' answer="1" output="舱壁满时返回: cached-data；恢复后返回: real-data" explain="舱壁满时 b.Do 走 default 立即返回 ErrBusy，CallWithFallback 命中降级返回兜底值；释放配额后真实调用成功，返回 real-data。" />

## 一页纸速记
<KnowledgeCard :points='["重试预算按总请求比例发放额度，把重试放大倍数锁死，是防雪崩的第一道闸","熔断器 Closed、Open、HalfOpen 三态，冷却后靠半开探测自愈，成功够数才闭合","舱壁给每个依赖独立并发配额，降级兜底保核心链路，二者让故障被关在局部","超时、预算、熔断、降级、舱壁按调用顺序串联，任一环缺席压力都会漏下去"]' :pitfalls='["对非幂等写操作或确定性错误做无脑重试，越重试越糟","熔断后没有半开探测，服务恢复也永远不敢放流量","兜底逻辑又去调用那个已经挂掉的下游，降级形同虚设"]' summary="一句话记住：稳定性不是敢重试，而是给超时、重试、失败都设好边界，把故障关在最小舱室里。" />

## 小结与下一讲

- **重试预算**：用比例而非次数限制重试，配指数退避与抖动，从源头掐断重试风暴。
- **熔断状态机**：三态加冷却与半开探测，让下游故障时停止无效打压、恢复后自动放量。
- **隔离与降级**：舱壁配额把慢依赖关在局部，兜底值保住核心链路，五道防线按调用顺序串起来。

[下一讲：OpenTelemetry 链路追踪](/microservice/tracing)
