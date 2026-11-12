---
title: 第 81 讲 · Prometheus / Grafana 与 RED 指标
description: 自建 Counter、Gauge、Histogram 与 exposition 端点，用 RED 方法让系统状态可量化可告警
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★☆☆" time="45 分钟" goal="让系统状态可量化、可告警" prereq="第 80 讲 OpenTelemetry 链路追踪" />

# 第 81 讲 · Prometheus / Grafana 与 RED 指标

> 链路追踪回答「这一次请求为什么慢」，指标回答「整个系统现在健康吗、趋势在变坏吗」。前者是个体样本，后者是聚合数据，两者互补而不可互相替代。Prometheus 用拉取模型定期抓取服务暴露的 `/metrics`，把样本存成时间序列，再由 PromQL 计算速率与分位数、Grafana 画图、Alertmanager 告警。本讲用标准库把 Counter、Gauge、Histogram 和 exposition 文本格式各造一遍，推导 RED 三个指标怎么算，并给出一条能用的告警规则。目标仍是理解而非调 API——知道了桶与累计计数是怎么回事，才不会把 `rate` 用错对象、把平均值当成延迟真相。

## 这一讲你会得到什么
<CheckList id="l81-goals" title="本讲目标" :items='["区分 Counter、Gauge、Histogram 的增减特性与各自适用场景","手写 Prometheus exposition 文本格式并通过 /metrics 端点暴露","从前后两份累计快照推导 RED 的 Rate、Errors、Duration，理解为何用累计值","读懂 PromQL 的 rate 与 histogram_quantile，写出一条错误率告警规则"]' />

## 1. 先建立直觉

把监控想成汽车仪表盘。里程表只会往上走（Counter：累计请求数），油箱指针可上可下（Gauge：当前在途请求数），而「这箱油各段速度的耗时分布」需要把观测值分段统计（Histogram：延迟分桶）。Prometheus 的巧妙之处在于它**只存累计值、不算瞬时值**：抓取到的是「到现在一共处理了多少请求」，速率、错误率、分位数全都在查询时用两个时间点的差值算出来。这样即使偶尔漏抓一次，也不会丢掉趋势。

面向在线服务，业界把最该盯的三件事浓缩成 **RED**：Rate（每秒请求数，看流量与容量）、Errors（错误占比，看正确性）、Duration（耗时分布，看体验）。它比「CPU、内存、磁盘」更贴近用户感受；而 USE 方法（利用率、饱和度、错误数）更适合看资源。两者结合，才算既有用户视角又有系统视角。

<ConceptDiagram type="rpc-flow" title="RED 指标在调用链上的采集点" caption="桩与拦截器在请求进出时累计 Rate、Errors、Duration，再由 /metrics 端点暴露给 Prometheus 抓取" />

<FlowTimeline :steps='[{"title":"请求进入","desc":"Counter 累计请求数，Gauge 记在途加一","tone":"blue"},{"title":"执行业务","desc":"记录开始时间，结果标记成功或失败","tone":"green"},{"title":"请求结束","desc":"Histogram 观测耗时，错误 Counter 视情况累加","tone":"green"},{"title":"暴露样本","desc":"/metrics 输出 exposition 文本","tone":"orange"},{"title":"抓取存储","desc":"Prometheus 定期拉取并落成时间序列","tone":"orange"},{"title":"计算告警","desc":"PromQL 算 RED，超过阈值触发告警","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 三种指标类型

Counter 只增不减、Gauge 可增可减、Histogram 把观测值按上界分桶累计并同时记录总和与次数。注意 Histogram 的桶是「累计桶」：每个桶记的是小于等于该上界的总次数，所以桶计数必然单调不减，这正是 `histogram_quantile` 能算分位数的前提。

```go
package main

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Counter 只增不减，用于累计请求数、错误数。
type Counter struct{ v atomic.Uint64 }

func (c *Counter) Inc()            { c.v.Add(1) }
func (c *Counter) Value() uint64   { return c.v.Load() }

// Gauge 可增可减，表示瞬时值，如当前在途请求数、队列长度。
type Gauge struct{ v atomic.Int64 }

func (g *Gauge) Inc()         { g.v.Add(1) }
func (g *Gauge) Dec()         { g.v.Add(-1) }
func (g *Gauge) Value() int64 { return g.v.Load() }

// Histogram 把观测值分桶累计，同时记录总和与次数。
type Histogram struct {
	mu      sync.Mutex
	bounds  []float64
	buckets []uint64
	sum     float64
	count   uint64
}

func NewHistogram(bounds []float64) *Histogram {
	b := append([]float64(nil), bounds...)
	sort.Float64s(b)
	return &Histogram{bounds: b, buckets: make([]uint64, len(b))}
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.count++
	for i, upper := range h.bounds {
		if v <= upper { // 累计桶：记录小于等于该上界的总次数
			h.buckets[i]++
		}
	}
}

func (h *Histogram) Snapshot() ([]uint64, float64, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]uint64(nil), h.buckets...), h.sum, h.count
}

func main() {
	var req Counter
	var inflight Gauge
	lat := NewHistogram([]float64{10, 50, 100, 200, 500})

	for _, ms := range []float64{5, 12, 60, 130, 400} {
		req.Inc()
		inflight.Inc()
		lat.Observe(ms)
		inflight.Dec()
	}
	buckets, sum, count := lat.Snapshot()
	fmt.Println("累计请求:", req.Value(), "在途:", inflight.Value())
	fmt.Println("分桶累计:", buckets, "总和:", sum, "次数:", count)
}
```

<OutputBox title="运行结果" output="累计请求: 5 在途: 0\n分桶累计: [1 2 3 4 5] 总和: 607 次数: 5" note="桶计数单调不减：5 毫秒落进全部 5 个桶，400 毫秒只落进最大桶，这是累计桶语义。" />

### 2.2 exposition 文本与 /metrics 端点

Prometheus 抓取的是纯文本：每组指标先写 HELP 与 TYPE，再写带标签的样本行。服务只要开一个 `/metrics` handler 输出这种格式，就能被抓取——这也是各语言 client 库最朴素的实现。

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// exposition 手写 Prometheus 文本格式：类型声明加样本行。
func exposition(reqTotal, errTotal uint64, p95 float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP http_requests_total 累计请求数\n")
	fmt.Fprintf(&b, "# TYPE http_requests_total counter\n")
	fmt.Fprintf(&b, "http_requests_total{service=\"order\"} %d\n", reqTotal)
	fmt.Fprintf(&b, "# TYPE http_errors_total counter\n")
	fmt.Fprintf(&b, "http_errors_total{service=\"order\"} %d\n", errTotal)
	fmt.Fprintf(&b, "# TYPE http_request_duration_seconds gauge\n")
	fmt.Fprintf(&b, "http_request_duration_seconds{quantile=\"0.95\"} %g\n", p95)
	return b.String()
}

func main() {
	// 1. 直接看 exposition 文本
	fmt.Println("--- exposition 文本 ---")
	fmt.Print(exposition(1000, 12, 0.18))

	// 2. 通过 /metrics 端点暴露给 Prometheus 抓取
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, exposition(1000, 12, 0.18))
	}))
	defer ts.Close()
	resp, _ := http.Get(ts.URL + "/metrics")
	defer resp.Body.Close()
	fmt.Println("抓取状态:", resp.StatusCode, "类型:", resp.Header.Get("Content-Type"))
}
```

<LineByLine code="h.sum += v\nh.count++\nfor i, upper := range h.bounds {\n    h.buckets[i]++\n}" :notes='["把本次观测值累加进总和，供后续算平均耗时","观测次数加一，是分位数计算的分母","遍历每个桶上界，定位本次观测应计入哪些桶","对应桶计数加一，累计桶由此保持单调不减"]' title="Histogram 观测逐行拆解" />

## 3. 深入一步

### 3.1 从累计快照推导 RED

Prometheus 存的是累计值，所以 Rate 要用 `rate` 对两个时间点的差值求每秒，Errors 是错误增量除以请求增量，Duration 是耗时增量除以请求增量。下面用两份快照把这个推导完整走一遍，并顺手做一次阈值告警判定。

```go
package main

import (
	"fmt"
	"time"
)

// Sample 是某一时刻抓到的一组累计计数快照。
type Sample struct {
	At       time.Time
	Requests uint64
	Errors   uint64
	Duration float64 // 累计耗时秒数
}

// RED 是面向服务最核心的三指标：速率、错误占比、平均耗时。
type RED struct {
	Rate     float64
	Errors   float64
	Duration float64
}

func ComputeRED(prev, cur Sample) RED {
	dt := cur.At.Sub(prev.At).Seconds()
	if dt <= 0 {
		return RED{}
	}
	dReq := float64(cur.Requests - prev.Requests)
	dErr := float64(cur.Errors - prev.Errors)
	dDur := cur.Duration - prev.Duration
	r := RED{Rate: dReq / dt, Duration: dDur / dReq}
	if dReq > 0 {
		r.Errors = dErr / dReq
	}
	return r
}

func main() {
	prev := Sample{At: time.Unix(0, 0), Requests: 1000, Errors: 10, Duration: 120}
	cur := Sample{At: time.Unix(60, 0), Requests: 4600, Errors: 58, Duration: 552}
	r := ComputeRED(prev, cur)
	fmt.Printf("Rate=%.1f/s Errors=%.2f Duration=%.3fs\n", r.Rate, r.Errors, r.Duration)
	// 简单告警判定：错误率超过 1% 就报警
	fmt.Println("触发错误率告警:", r.Errors > 0.01)
}
```

几条最常用的 PromQL，正好对应 RED 三项：

```text
# Rate：每秒请求速率
sum(rate(http_requests_total{service="order"}[5m]))
# Errors：错误占比
sum(rate(http_errors_total[5m])) / sum(rate(http_requests_total[5m]))
# Duration：P95 延迟
histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))
```

| 指标类型 | 增减特性 | 典型用途 | Prometheus 类型 |
| --- | --- | --- | --- |
| Counter | 只增不减 | 累计请求数、错误数 | counter |
| Gauge | 可增可减 | 在途请求、队列长度、温度 | gauge |
| Histogram | 分桶累计 | 延迟分布、分位数 | histogram |
| Summary | 客户端直接算分位 | 精确分位数 | summary |

| 方法 | 面向 | 三个指标 | 适用场景 |
| --- | --- | --- | --- |
| RED | 服务与请求 | Rate、Errors、Duration | 微服务、在线接口 |
| USE | 资源 | Utilization、Saturation、Errors | 主机、磁盘、连接池 |

<Term word="累计桶" text="Histogram 每个桶记录小于等于该上界的总次数，计数单调不减，是算分位数的基础" />
<Term word="rate" text="PromQL 中用窗口内累计值的差除以时间得到每秒速率，只能用于 Counter" />
<Term word="RED 方法" text="用请求速率、错误率、耗时分布三个指标刻画面向用户的服务健康度" />

<Callout type="warn" title="别把 rate 用在 Gauge 上，也别只看平均延迟" text="rate 靠差值算速率，用在可增可减的 Gauge 上会算出负数等荒谬结果；Counter 还要注意进程重启后归零，PromQL 的 rate 已能自动处理这种重置。延迟方面，平均值会被长尾掩盖，用户体验取决于 P95、P99，必须靠 Histogram 分位数来看。" />

## 4. 工程实践

一条能用的错误率告警规则长这样：`rate` 取 5 分钟窗口，错误速率除以总请求速率，超过 1% 并持续 2 分钟才告警。加 `for` 是为了过滤瞬时抖动，`labels` 决定路由到哪个值班组。

```yaml
groups:
  - name: order-service
    rules:
      - alert: HighErrorRate
        expr: |
          sum(rate(http_errors_total{service="order"}[5m]))
          / sum(rate(http_requests_total{service="order"}[5m])) > 0.01
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: 订单服务 5 分钟错误率超过 1%
```

<Compare :panels='[{"label":"只记平均值","code":"avg := totalDuration / totalRequests\nlog(avg)","note":"平均值会被少量慢请求拉高、也会被大量快请求掩盖，看不出长尾体验"},{"label":"Histogram 分位数","code":"p95 := histogram.Quantile(0.95)\nalert(p95)","note":"分位数直接反映长尾，能对齐 SLO 里的 95 分位延迟目标"}]' title="延迟该看平均值还是分位数" />

落地时还有几条经验。label 是时间序列的乘数，`user_id`、`order_id` 这类高基数字段绝不能当 label，否则序列数爆炸会把 Prometheus 打垮——它们应放进日志与链路追踪，指标里只留 `service`、`method`、`status` 这类低基数维度。Histogram 的桶边界要按 SLO 目标来设，比如目标 P99 小于 200 毫秒，就该在 200 附近多放几个桶，边界太粗算出的分位数会失真。埋点位置选在第 74 讲的拦截器里：请求进出统一累计，业务代码零侵入，还能顺带把 trace_id 关联进日志。告警要分层：RED 面向用户症状优先报，USE 面向资源原因其次报，避免半夜被一堆 CPU 抖动吵醒却没人知道接口已经出问题。最后，Grafana 面板建议固定「流量、错误率、P95/P99、饱和度」四行，配成团队共用模板，出问题时不用临时找图。

## 动手练一练
<Exercise title="给 Histogram 加上 le 标签的 exposition 输出" task="扩展 2.2 的 exposition，让 Histogram 按 Prometheus 约定输出一组带 le 标签的累计桶样本行，外加 _sum、_count，使 histogram_quantile 能直接计算。" :hints='["每个桶一行 http_request_duration_seconds_bucket{le=上界} 累计次数","额外补一个 le=+Inf 的桶，其值等于总观测次数","再输出 _sum 与 _count 两行，类型声明为 histogram"]' solution="要点一：遍历 bounds，逐行输出 bucket 样本，le 用字符串形式的上界，值为累计桶计数。\n要点二：追加 le 为 +Inf 的桶，值等于 count；真实客户端还会输出 _sum 与 _count 汇总行。\n要点三：TYPE 声明为 histogram，这样 PromQL 的 histogram_quantile 才能按 le 聚合算分位数。" explain="Prometheus 的 Histogram 就是靠一组带 le 的累计桶样本表达的，补齐它们，自建指标才能被标准查询函数消费。" />

## 随堂检测
<Quiz no="81-1" q="累计请求数应该用哪种指标类型，为什么不能用 Gauge？" :options='["Gauge，因为请求数会上下波动","Counter，因为只增不减，配合 rate 才能用差值算窗口速率","Histogram，因为请求数要分桶","Summary，因为要算分位数"]' answer="1" explain="累计计数天然只增，Counter 的差值除以时间就是速率；Gauge 可增可减会让 rate 算出负数，语义也不匹配。" />

<Predict no="81-2" q="代码 3.1 里两份快照相隔 60 秒，请求数从 1000 涨到 4600、错误数从 10 涨到 58，算出的 Rate 与错误率大致是？" :options='["Rate 60/s，错误率约 1.3%","Rate 4600/s，错误率 5.8%","Rate 76/s，错误率 0.1%","Rate 3600/s，错误率 48%"]' answer="0" output="Rate=60.0/s Errors=0.01 Duration=0.120s；触发错误率告警: true" explain="60 秒新增 3600 个请求即 60 QPS；新增 48 个错误占比 48/3600 约 1.33%，超过 1% 阈值，因此触发告警。" />

## 一页纸速记
<KnowledgeCard :points='["Counter 只增不减、Gauge 可增可减、Histogram 按累计桶分桶，三者用途不可混用","exposition 是纯文本：HELP 与 TYPE 声明加带标签样本行，/metrics 端点暴露给 Prometheus 拉取","RED = Rate 速率、Errors 错误占比、Duration 耗时分布，从累计值的前后差值推导","PromQL 用 rate 算速率、histogram_quantile 算分位数；告警加 for 过滤抖动"]' :pitfalls='["把 rate 用在 Gauge 上，或对重启归零的 Counter 手写差值","用平均延迟代替 P95/P99，掩盖长尾体验","把 user_id 等高基数字段当 label，导致时间序列爆炸"]' summary="一句话记住：指标只存累计值，所有速率与分位数都在查询时算出来；盯服务用 RED，盯资源用 USE。" />

## 小结与下一讲

- **三种类型**：Counter 累计、Gauge 瞬时、Histogram 分桶累计；exposition 文本加 `/metrics` 端点即可被抓取。
- **RED 推导**：Rate、Errors、Duration 全由前后两份累计快照的差值得出，延迟要看分位数而非平均值。
- **查询与告警**：`rate` 与 `histogram_quantile` 是核心 PromQL，告警加 `for` 抗抖动，label 必须低基数。

[下一讲：自研微服务框架：从 0 到 1](/microservice/framework)
