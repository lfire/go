---
title: 第 107 讲 · 实战 05 · 日志分析 ETL 管道
description: 用逐行读取与坏行隔离、扇出扇入聚合、带缓冲 channel 的背压与滚动窗口，处理持续流入的日志
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="用 pipeline 处理持续流入的数据" prereq="第 106 讲 · 实战 04 · 图片服务与对象存储" />

# 第 107 讲 · 实战 05 · 日志分析 ETL 管道

> 前面几讲的数据来一次就结束。ETL 面对永不结束的流：日志持续到来、下游会变慢、单行会损坏、统计要按窗口产出。本讲用 pipeline 把这些串起来。

## 这一讲你会得到什么
<CheckList id="l107-goals" title="本讲目标" :items='["用 Scanner 逐行读取并把坏行隔离到死信通道","用扇出扇入让各 worker 只累加局部结果","用有界 channel 形成背压","用滚动窗口产出统计并说清窗口边界"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束 |
| --- | --- | --- |
| 输入 | 持续不断的日志行 | 不能假设读完了 |
| 容错 | 单行损坏不影响整条流 | 坏行进死信 |
| 并行 | 多核聚合 | worker 不共享可变状态 |
| 背压 | 下游变慢时上游减速 | 用有界缓冲 |
| 语义 | 至少一次 | 重启可能重复计数 |

<Callout type="tip" title="与第 106 讲的分界" text="图片服务流的是一个大对象，ETL 流的是无数小对象。前者怕内存峰值，后者怕无界队列、goroutine 泄漏与窗口边界。" />

本讲是至少一次语义：崩溃重启会重算窗口并可能重复计数；精确一次需把位点与结果同事务提交，见第 85 讲。

## 2. 架构蓝图

<ConceptDiagram type="pipeline" title="日志 ETL 的扇出扇入" caption="读阶段把坏行分流到死信，正常事件扇出给多个 worker，各自累加局部结果后扇入合并" />

共享 map 累加要每行加锁，越并行争用越重；每个 worker 只累加局部结果、结束时发一次，通信与锁都省掉。死信通道留下坏行，才能发现上游格式变更。

## 3. 关键模块实现

### 3.1 读取与解析：坏行隔离

```go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Event struct {
	TS     int64
	Level  string
	Status int
	Bytes  int
}

var ErrBadLine = errors.New("日志行格式错误")

// parseLine 把一行形如 "1700000000 INFO 200 1024" 的文本解析成 Event。
func parseLine(line string) (Event, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 {
		return Event{}, fmt.Errorf("%w: 字段数=%d", ErrBadLine, len(fields))
	}
	ts, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return Event{}, fmt.Errorf("%w: 时间戳", ErrBadLine)
	}
	status, err := strconv.Atoi(fields[2])
	if err != nil {
		return Event{}, fmt.Errorf("%w: 状态码", ErrBadLine)
	}
	size, err := strconv.Atoi(fields[3])
	if err != nil {
		return Event{}, fmt.Errorf("%w: 字节数", ErrBadLine)
	}
	return Event{TS: ts, Level: fields[1], Status: status, Bytes: size}, nil
}

// ReadEvents 逐行读取，坏行进 dead 通道而不是中断整条管道。
func ReadEvents(src string, out chan<- Event, dead chan<- string) error {
	sc := bufio.NewScanner(strings.NewReader(src))
	sc.Buffer(make([]byte, 64*1024), 1024*1024) // 允许最长 1 MiB 的单行
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, err := parseLine(line)
		if err != nil {
			dead <- line
			continue
		}
		out <- ev
	}
	return sc.Err() // Scanner 唯一的失败来源必须显式返回
}

func main() {
	src := "1700000000 INFO 200 1024\n" +
		"bad line here\n" +
		"1700000001 WARN 500 64\n" +
		"1700000002 INFO 200 2048\n"
	out := make(chan Event, 8)
	dead := make(chan string, 8)
	if err := ReadEvents(src, out, dead); err != nil {
		fmt.Println("读取失败:", err)
	}
	close(out)
	close(dead)
	var ok int
	for range out {
		ok++
	}
	var bad int
	for range dead {
		bad++
	}
	fmt.Printf("有效=%d 坏行=%d\n", ok, bad)
}
```

Scanner 默认单行上限 64 KiB，超过就报错且停止扫描，所以生产日志必须调 Buffer 并显式处理 sc.Err。把错误包成 ErrBadLine 家族，上层才能用 errors.Is 分类统计，而不是靠字符串匹配。

### 3.2 扇出扇入：局部聚合代替共享锁

```go
package main

import (
	"fmt"
	"sync"
)

type Result struct {
	Count  int
	Bytes  int
	Errors int
}

// worker 从 jobs 消费并累积局部结果，避免多个 goroutine 争抢同一把锁。
func worker(id int, jobs <-chan int, results chan<- Result) {
	r := Result{}
	for n := range jobs {
		r.Count++
		r.Bytes += n
		if n >= 500 {
			r.Errors++
		}
	}
	fmt.Printf("worker %d 处理完\n", id)
	results <- r
}

// FanIn 把多个局部结果合并成一个；每个 worker 只发一次，通信成本固定。
func FanIn(jobs <-chan int, n int) Result {
	results := make(chan Result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worker(id, jobs, results)
		}(i)
	}
	wg.Wait()
	close(results)
	total := Result{}
	for r := range results {
		total.Count += r.Count
		total.Bytes += r.Bytes
		total.Errors += r.Errors
	}
	return total
}

func main() {
	jobs := make(chan int, 16)
	for _, n := range []int{100, 200, 500, 700, 64, 2048, 404} {
		jobs <- n
	}
	close(jobs)
	total := FanIn(jobs, 3)
	fmt.Printf("合计: 条数=%d 字节=%d 错误=%d\n", total.Count, total.Bytes, total.Errors)
}
```

results 缓冲设为 worker 数：每个恰好发一次，WaitGroup 等齐再 close；缓冲小于 worker 数会让发送阻塞、wg.Wait 永不返回，形成自死锁。

### 3.3 窗口与背压：有界缓冲堵住上游

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Window struct {
	count  int
	errors int
}

// Sink 代表落库；真实实现换成批量写 DB，这里只记录每次冲出的内容。
type Sink struct {
	mu      sync.Mutex
	flushed []string
}

func (s *Sink) Write(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushed = append(s.flushed, line)
}

// RunWindow 用带缓冲的 channel 形成背压：下游慢，上游自然被堵住。
func RunWindow(in <-chan int, sink *Sink, period time.Duration, capacity int) {
	buf := make(chan int, capacity) // 容量即「最多允许堆积多少条」
	go func() {
		for n := range in {
			buf <- n // 缓冲满时阻塞，形成背压而非无限增长
		}
		close(buf)
	}()
	var w Window
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case n, ok := <-buf:
			if !ok {
				// 输入关闭，必须补冲一次，否则最后不足一个窗口的数据会丢。
				sink.Write(fmt.Sprintf("最终窗口 count=%d errors=%d", w.count, w.errors))
				return
			}
			w.count++
			if n >= 500 {
				w.errors++
			}
		case <-ticker.C:
			sink.Write(fmt.Sprintf("滚动窗口 count=%d errors=%d", w.count, w.errors))
			w.count, w.errors = 0, 0
		}
	}
}

func main() {
	in := make(chan int)
	sink := &Sink{}
	done := make(chan struct{})
	go func() {
		RunWindow(in, sink, 10*time.Millisecond, 4)
		close(done)
	}()
	for _, n := range []int{200, 500, 404, 200, 503, 200} {
		in <- n
	}
	close(in)
	<-done
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, l := range sink.flushed {
		fmt.Println(l)
	}
}
```

背压把来不及处理变成显式等待而非无限内存；无界队列在突增流量下会把内存与延迟一起推高最后 OOM，有界队列把压力传回上游。输入关闭时须补冲一次，否则末尾不足一窗的数据会丢。

## 4. 失败路径与边界条件

超长行要调 Buffer 并检查 sc.Err；worker panic 会让 WaitGroup 永不归零，需 recover；容量与发送次数不匹配造成自死锁；窗口关闭要补冲；重试无退避会打垮下游。最难排查的是 goroutine 泄漏：上游没关通道，下游永远阻塞在 range 上。

## 5. 测试与压测思路

解析用表驱动覆盖正常行、缺字段、非法数字、空行与超长行；让 Write 故意 sleep 验证背压；Benchmark 对比 worker 数 1、2、4、8 的吞吐与分配，要给曲线。

压测还要盯分配次数；输入提前关闭要确认 NumGoroutine 无残留。

## 6. 可继续扩展的方向

内存 channel 换成 Kafka 或 NATS 获得持久化与多消费者，见第 68 讲；引入水位线处理迟到数据；位点与结果同事务提交；窗口结果写成 CSV 正是第 108 讲输入。

## 随堂检测

<Quiz no="107-1" q="为什么每个 worker 累加局部结果？" :options='["更省内存","把每行一次的锁竞争降为每 worker 一次通信","共享锁不能用"]' answer="1" explain="共享累加器要求每行加锁；局部聚合无锁，只在结束时各发一个结构体。" />

## 一页纸速记

<KnowledgeCard :points='["Scanner 要调 Buffer 并检查 sc.Err","坏行进死信通道留存计数","局部聚合加扇入，用空间换掉锁竞争","有界 channel 形成背压，输入关闭时补冲最终窗口"]' :pitfalls='["容量小于 worker 数会自死锁","处理时间窗口会归错迟到日志"]' summary="ETL 的难点是流永不停、下游会变慢、坏行一定会出现。" />

## 小结与下一讲

- 读阶段隔离坏行、显式检查 Scanner 错误，是数据质量第一道闸。
- 扇出扇入用局部状态消灭锁竞争；有界缓冲把来不及变成可观测的背压。
- 窗口关闭补冲、goroutine 退出路径、至少一次语义，决定系统能否长时间跑。

[下一讲：实战 06 · CSV / SQL 查询工具](/projects/data-sql-tool)
