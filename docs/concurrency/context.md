---
title: 第 47 讲 · context：取消、超时、值传递
description: 用 Context 统一叫停并发任务，掌握取消传播、超时控制、context.Value 的克制使用与服务端标准用法
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="让并发任务可被统一叫停" prereq="第 46 讲 · atomic 与 sync.Pool" />

# 第 47 讲 · context：取消、超时、值传递

> 一个请求往往要同时查库、调 RPC、读缓存。若客户端断开或整体超时，下游 goroutine 还在傻跑就是资源浪费。context 是标准库的统一答案：管理一次请求的生命周期，把取消信号沿调用链传下去。本讲讲清接口、四种构造函数、取消树与最易滥用的 Value。

## 这一讲你会得到什么
<CheckList id="lesson-47" title="本讲目标" :items='["读懂 Context 四方法与三类取消原因","用 WithCancel、WithTimeout、WithDeadline 派生","理解取消沿父到子单向广播","克制使用 context.Value，掌握服务端标准用法"]' />

## 1. 先建立直觉
把 context 想成「总电闸 + 工作票」：总闸一拉所有分区断电，某分区跳闸却不影响总闸。票上写「几点收场」（Deadline）与少量跨部门编号（Value）。两条纪律：只管「何时停」，不写「唱哪首歌」的业务参数；每层都要把闸拉到底（调 cancel）。

<ConceptDiagram type="context-tree" title="context 取消树" caption="父被取消时，子与孙的 Done 同时关闭；反向不传播" />

## 2. 语法与规则
Context 接口只有四个方法：Deadline 返回截止时间，Done 返回取消时被关闭的 channel，Err 返回原因，Value 按键取值。下面演示三种派生方式。

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	// WithCancel：由调用方决定何时取消
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel() // 调用后 Done 被关闭
	}()
	<-ctx.Done()
	fmt.Println("WithCancel 原因：", ctx.Err())

	// WithTimeout：相对超时
	tctx, tcancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer tcancel() // 必须调用，及时释放计时器
	<-tctx.Done()
	fmt.Println("WithTimeout 原因：", tctx.Err())

	// WithDeadline：绝对时间点
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(50*time.Millisecond))
	defer dcancel()
	if _, ok := dctx.Deadline(); ok {
		fmt.Println("WithDeadline 设置了截止时间：true")
	}
	<-dctx.Done()
	fmt.Println("WithDeadline 原因：", dctx.Err())
}
```

取消信号沿「父到子」单向广播：只取消根，所有子孙同时收到。下面两个 worker 分别挂在 WithCancel 与 WithTimeout 派生的子 context 上。

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

func worker(ctx context.Context, name string, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			fmt.Println(name, "收到取消：", ctx.Err())
			return
		default:
			time.Sleep(10 * time.Millisecond) // 模拟工作
		}
	}
}

func main() {
	root, cancel := context.WithCancel(context.Background())
	childA, cancelA := context.WithCancel(root)
	defer cancelA()
	childB, cancelB := context.WithTimeout(root, 5*time.Second)
	defer cancelB()

	var wg sync.WaitGroup
	wg.Add(2)
	go worker(childA, "worker-A", &wg)
	go worker(childB, "worker-B", &wg)
	time.Sleep(50 * time.Millisecond)
	cancel() // 只取消 root，两个子同时收到
	wg.Wait()
	fmt.Println("全部 worker 已退出")
}
```

<OutputBox title="运行结果" output="WithCancel 原因： context canceled；WithTimeout 原因： context deadline exceeded；WithDeadline 设置了截止时间：true；WithDeadline 原因： context deadline exceeded" note="手动取消返回 canceled，超时返回 deadline exceeded；子继承父的取消，defer cancel 不可省" />

派生与释放的三行值得逐行看。

<LineByLine code="ctx, cancel := context.WithCancel(parent)\ndefer cancel()\ngo work(ctx)" :notes='["WithCancel 基于 parent 派生可取消的子 context，并返回 cancel","defer cancel 必须调用，提前返回也能释放父节点上的引用","把 ctx 作为函数第一参数透传，下游才能响应统一取消"]' title="派生 context 的骨架" />

## 3. 深入一步
服务端标准用法：从 http.Request 自带的 Context 出发（客户端断开时自动取消），把超时预算往下传，最外层只放少量请求作用域元数据。Value 的 key 用非导出自定义类型，避免包间撞键。

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type ctxKey string // 自定义非导出键类型

const traceKey ctxKey = "trace-id"

func handler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context() // 客户端断开自动取消
	ctx = context.WithValue(ctx, traceKey, "req-9527")

	// 给下游单独设更短的超时，慢依赖不拖住请求
	callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := queryDB(callCtx); err != nil {
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}
	fmt.Fprintln(w, "ok", traceID(ctx))
}

func traceID(ctx context.Context) string {
	if v, ok := ctx.Value(traceKey).(string); ok {
		return v // 取值必须判 ok
	}
	return "unknown"
}

func queryDB(ctx context.Context) error {
	select {
	case <-time.After(200 * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	http.HandleFunc("/", handler)
	_ = http.ListenAndServe(":8080", nil)
}
```

四种构造函数对比：

| 构造函数 | 取消触发 | 典型场景 | 注意 |
| --- | --- | --- | --- |
| WithCancel | 显式调 cancel | 手动叫停一组任务 | 必须调 cancel，否则父挂引用 |
| WithTimeout | 经过一段时长 | 下游调用限时 | 等价 Deadline 为当前加时长 |
| WithDeadline | 到绝对时间点 | 跨服务传统一截止 | 子 deadline 不超过父 |
| WithValue | 不触发取消 | 传 trace id 等元数据 | key 用自定义类型，取值判 ok |

<Term word="取消传播" text="信号沿 context 树由父向子单向广播：父取消则所有子孙同时取消，子取消不影响父或兄弟" />

<Callout type="warn" title="context.Value 要克制使用" text="Value 只应承载跨整条调用链的请求作用域元数据，如 trace id、认证主体。别当「隐形参数」省掉签名，别塞连接、配置，别用 string 做 key（易撞键）；取值用逗号 ok 断言。判断标准：这数据是否每个中间层都不该关心、却必须透传到最深处？若不是，写成显式参数。" />

<Callout type="tip" title="几条纪律" text="ctx 作函数第一参数，不存进结构体；顶层从 context.Background() 起步，别把 nil 当 context 传；派生的 cancel 必须 defer，WithTimeout 与 WithDeadline 尤其别丢弃。标准库阻塞调用都接受 ctx。" />

## 4. 工程实践
真实服务里 context 贯穿始终：入口从 r.Context() 取，按剩余预算给数据库、RPC、缓存派生更短超时，出错就靠 Done 让在途 goroutine 收手。trace_id 靠 Value 透传，业务参数走函数签名。跨进程要把 deadline 一并传给对端，否则上游超时下游还在跑。context 管「何时停」，不管「做什么」。

<Compare :panels='[{"label":"写法 A：业务参数塞进 Value","code":"ctx = context.WithValue(ctx, userIDKey, uid); doWork(ctx)","note":"参数被藏进上下文，签名失真，调用链难追踪，属误用"},{"label":"写法 B：显式参数加元数据","code":"ctx = context.WithValue(ctx, traceKey, tid); doWork(ctx, uid)","note":"业务参数走签名，context 只承载横切元数据，职责清晰"}]' title="context.Value 的边界" />

## 动手练一练
<Exercise title="给下游调用加上超时" task="基于 parent 派生 50ms 超时的 context，defer 释放 cancel，并用 NewRequestWithContext 把 ctx 绑定到一次请求。" :hints='["用 context.WithTimeout 并 defer cancel()","标准库提供 http.NewRequestWithContext","ctx 绑定进请求后超时或断开都能取消"]' solution="ctx, cancel := context.WithTimeout(parent, 50*time.Millisecond)\ndefer cancel()\nreq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)\nif err != nil {\n    return err\n}\nresp, err := http.DefaultClient.Do(req)\nif err != nil {\n    return err\n}\ndefer resp.Body.Close()\nreturn nil" explain="WithTimeout 给下游独立预算，defer cancel 保证提前返回也释放计时器；ctx 绑进请求后超时或断开，底层传输立刻中止。" />

## 随堂检测
<Quiz no="47-1" q="关于 context.Value，下面哪种用法是推荐的？" :options='["把可选参数都塞进 Value 简化签名","存放请求作用域 trace id，用自定义非导出类型做 key","用 string 做 key 方便各包共享","把数据库连接放进 Value 复用"]' answer="1" explain="Value 只承载跨切面元数据，key 用非导出自定义类型避免冲突，取值判 ok；业务参数走函数签名。" />
<Predict no="47-2" q="父 context 被 cancel 后，由它派生的两个子 context 会怎样？" :options='["只有直接调 cancel 的那个被取消","父与所有子孙的 Done 同时关闭","子不受影响需各自取消","编译报错"]' answer="1" output="父与所有子孙的 Done 同时关闭，Err 均为 context canceled" explain="context 是取消树，信号沿父到子单向广播，子取消不影响父或兄弟。" />

## 一页纸速记
<KnowledgeCard :points='["接口只有 Deadline、Done、Err、Value 四方法","WithCancel 手动，WithTimeout 相对，WithDeadline 绝对","取消沿父到子单向广播","ctx 作第一参数，cancel 必须 defer","Value 只放元数据，key 自定义"]' :pitfalls='["丢弃 cancel 导致计时器无法释放","把业务参数塞进 Value 当隐形参数","把 context 存进结构体"]' summary="context 管一次请求的生命周期：取消树统一叫停，deadline 控预算，Value 透传元数据" />

## 小结与下一讲

- Context 通过 Done、Err、Deadline、Value 把取消与超时变成可传递的标准协议。
- 取消沿父到子单向广播，派生的 cancel 都要 defer；服务端从 r.Context() 层层设超时。
- Value 只放元数据，业务参数走签名，key 用自定义类型并判 ok。

[下一讲：并发模式：pipeline / fan-out / worker pool](/concurrency/patterns)
