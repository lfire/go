---
title: 第 047 讲 · context：取消、超时、值传递
description: 用 Context 统一叫停并发任务，掌握取消传播、超时控制、context.Value 的克制使用与服务端标准用法
---

<StageHeader stage="L4 · 并发编程" level="★★★☆☆" time="40 分钟" goal="让并发任务可被统一叫停" prereq="第 046 讲 · atomic 与 sync.Pool" />

# 第 047 讲 · context：取消、超时、值传递

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

### 再深一层：取消树的实现、ctx 丢传的代价与 lostcancel

「取消沿父到子单向广播」不是抽象口号，而是 `context/context.go` 里一张实实在在的树。`cancelCtx` 的字段是 `mu sync.Mutex`、`done atomic.Value`（惰性创建的 `chan struct{}`，第一次 cancel 时 close）、`children map[canceler]struct{}`、`err`、`cause`。派生子 context 时 `propagateCancel(parent, c)` 把子注册进父的 `children`；`cancel()` 先 `close(c.done)`、写下 `err`，再遍历 `children` 逐个调用 `child.cancel(...)`，最后把自己从父的 children 里摘掉。`timerCtx` 则在 `cancelCtx` 之上多挂 `timer *time.Timer` 与 `deadline`，`WithDeadline` 用 `time.AfterFunc` 到期触发 `c.cancel(true, DeadlineExceeded, ...)`。因此关闭 `Done` 与所有对 `ctx.Done()` 的接收之间建立 happens-before，父取消时子孙的 `Done` 同时关闭、反向不传播；也正因为 timer 挂在节点上，丢弃 `cancel` 就等于让这棵子树一直被父引用、计时器到点才释放，这便是「context 泄漏」的机器级来源。

最常见的正确性事故是「ctx 没传到底」。下游若用 `context.Background()` 另起炉灶，或在函数里干脆不 select 监听 `ctx.Done()`，上游的取消与 deadline 就完全失效：客户端断开了、整体超时了，goroutine 还在跑慢查询，直到自己跑完。本讲的 `slowNoCtx` 就是这个反面——它无视取消必然睡满 200ms；`slowWithCtx` 把 `time.After` 与 `ctx.Done()` 并进一次 select，20ms 超时一到立即返回 `context.DeadlineExceeded`。`TestCancel` 用 `go test -race -run=TestCancel -v ./...` 验证「返回及时（小于 100ms）且原因正确」，这就是可复现的对照。另一类高频错误是「丢了 cancel」：写成 `ctx, _ := context.WithCancel(...)` 把 cancel 丢掉后，`go vet ./...` 会精确报 `the cancel function returned by context.WithCancel should be called, not discarded, to avoid a context leak`；把 `defer cancel()` 补回去即消失。所以「ctx 作第一参数、派生必 defer cancel、阻塞调用都接受 ctx」都有工具与源码依据，不是教条。

生产定位：服务端入口从 `r.Context()` 取，客户端断开时 net/http 会取消它；用 `go tool pprof` 的 goroutine profile 看卡在下游 IO 却没带 ctx 的 G，用 `context.Cause(ctx)`（Go 1.20+）拿到自定义取消原因，日志里把 `trace_id` 与 `ctx.Err()` 一起打出来，就能把「谁先超时、哪一层没传递」还原成一条链路。跨进程还要把 deadline 转换成对端超时一并传出，否则上游超时、下游仍在跑。补一层实现细节：`valueCtx` 只是 `key, val any` 加一个父指针的链表节点，`Value(key)` 会沿父链一路向上查找、找不到就返回 nil，所以「取值判 ok」与「用自定义非导出类型做 key」都不是可有可无的讲究：前者防把 nil 当有效值，后者避免不同包用同一个 string 撞键后取到别人的数据。

```go
package main

import (
	"context"
	"testing"
	"time"
)

// 反例：ctx, _ := context.WithCancel(parent) 丢掉 cancel，
// go vet ./... 会报 "the cancel function ... should be called, not discarded"
// 错误对照：丢弃上游 ctx，另起炉灶，取消信号传不进来
func slowNoCtx() string {
	time.Sleep(200 * time.Millisecond)
	return "完成"
}

// 正确：透传 ctx，select 监听 Done，取消时立刻收手
func slowWithCtx(ctx context.Context) (string, error) {
	select {
	case <-time.After(200 * time.Millisecond):
		return "完成", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := slowWithCtx(ctx)
	if err != context.DeadlineExceeded {
		t.Fatalf("期望超时，实际 %v", err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("取消未及时生效，耗时 %v", d)
	}
	_ = slowNoCtx // 对照：它无视取消，必然跑满 200ms
}
```

```bash
go test -race -run=TestCancel -v ./...   # 验证取消及时且 Err 为 DeadlineExceeded
go vet ./...                             # 补全 defer cancel 后 lostcancel 告警消失
```

把取消链路上的典型失效整理成表，排障时按「现象到依据到修复」逐行对照：

| 失败场景 | 运行时现象 | 可验证的检测命令 | 修复要点 |
| --- | --- | --- | --- |
| 下游用 `context.Background()` 另起炉灶 | 上游取消、客户端断开都传不进来，goroutine 跑满自身耗时 | `go test -race -run=TestCancel -v ./...`；goroutine profile 见在途 G | 一律透传上游 ctx，阻塞调用都接受 ctx |
| 派生后丢掉 `cancel` | 子树一直被父引用、timer 到点才释放，context 泄漏 | `go vet ./...` 报 lostcancel | 每个派生都 `defer cancel()` |
| 子 deadline 长于父 | 父已超时子仍在等，预算失效 | 打日志比对 `Deadline()`；链路追踪看超时分叉 | 子 deadline 不超过父，用剩余预算派生 |
| 把业务参数塞进 `Value` | 签名失真、调用链难追踪、易撞键 | code review 与静态检查难覆盖，靠规范 | Value 只放 trace id 等横切元数据，key 自定义 |
| `ctx` 存进结构体长期持有 | 生命周期错位、取消信号滞后或错乱 | `go vet` 与静态检查 | ctx 只作函数第一参数随调用链传递 |

下面这张时序图把「入口取 ctx、派生更短预算、客户端断开触发级联取消、下游及时收手」串成一条链路，配合本讲前面的取消树一起看：

<SequenceDiagram title="一次请求里的取消传播链路" caption="客户端断开由 net/http 取消 root，沿 children 级联到 DB 子 ctx，下游 select 到 Done 立即返回" :actors='["客户端","Handler","ctx(root)","ctx(DB)","queryDB"]' :steps='[{"from":"客户端","to":"Handler","label":"发起请求","note":"携带 deadline","kind":"call"},{"from":"Handler","to":"ctx(root)","label":"r.Context()","note":"入口取根 ctx","kind":"call"},{"from":"Handler","to":"ctx(DB)","label":"WithTimeout","note":"派生更短预算","kind":"call"},{"from":"Handler","to":"queryDB","label":"带 ctx 调用","note":"select 监听 Done","kind":"call"},{"from":"客户端","to":"ctx(root)","label":"连接断开","note":"net/http 自动取消","kind":"async"},{"from":"ctx(root)","to":"ctx(DB)","label":"广播取消","note":"遍历 children 级联","kind":"call"},{"from":"ctx(DB)","to":"queryDB","label":"Done 关闭","note":"close 建立 happens-before","kind":"return"},{"from":"queryDB","to":"Handler","label":"返回错误","note":"立即收手不再等待","kind":"return"},{"from":"Handler","to":"客户端","label":"504 响应","note":"预算内给客户端答复","kind":"return"}]' />

<Quiz q="下游函数用 context.Background() 另起炉灶、且不监听 ctx.Done()，上游取消后会怎样？" :options='["下游立刻收到取消","下游无视取消，一直跑到自身耗时结束","编译器报错","context 自动把取消塞进 Background"]' answer="1" explain="Background 是不带取消的根，另起炉灶等于切断取消树；下游必须透传上游 ctx 并在阻塞处 select 监听 Done，TestCancel 用 20ms 超时验证 slowWithCtx 能及时返回 DeadlineExceeded。" />

<Callout type="warn" title="lostcancel 不是 lint 洁癖" text="丢掉 cancel 会让 cancelCtx 一直挂在父的 children 里、timerCtx 的 time.AfterFunc 计时器到点才释放。go vet 的 lostcancel 分析器能静态抓到这类写法，报错文案是 the cancel function returned by context.WithCancel should be called, not discarded。把每个派生都配 defer cancel()，既释放引用也及时停表。" />

## 4. 工程实践
真实服务里 context 贯穿始终：入口从 r.Context() 取，按剩余预算给数据库、RPC、缓存派生更短超时，出错就靠 Done 让在途 goroutine 收手。trace_id 靠 Value 透传，业务参数走函数签名。跨进程要把 deadline 一并传给对端，否则上游超时下游还在跑。context 管「何时停」，不管「做什么」。

<Compare :panels='[{"label":"写法 A：业务参数塞进 Value","code":"ctx = context.WithValue(ctx, userIDKey, uid); doWork(ctx)","note":"参数被藏进上下文，签名失真，调用链难追踪，属误用"},{"label":"写法 B：显式参数加元数据","code":"ctx = context.WithValue(ctx, traceKey, tid); doWork(ctx, uid)","note":"业务参数走签名，context 只承载横切元数据，职责清晰"}]' title="context.Value 的边界" />

### 与 L10 实战的呼应

取消树与 deadline 预算，是三个实战里跨层协作的公共协议。第 109 讲 [并发爬虫](/projects/concurrent-crawler) 的 Scheduler 用单 goroutine 编排加 `context` 取消传播，进程退出时先停投放再等在途任务收尾，`http.Client` 的整体超时也挂在同一个 ctx 上——正是本讲「取消沿父到子单向广播、阻塞调用都接受 ctx」的落地；下游若用 `context.Background()` 另起炉灶，整体超时就失效。第 110 讲 [分布式任务调度](/projects/concurrent-scheduler) 的优雅退位把发布窗口的在途任务收干净，租约 TTL 与 ctx 超时共同决定「旧 leader 到底死没死」，跨进程还要把 deadline 转成对端超时一并传出。第 116 讲 [秒杀与高并发库存](/projects/micro-seckill) 的排队请求要有超时释放，否则被拒绝的请求也会一直占着 goroutine；第 118 讲 [LLM 网关](/projects/ai-gateway) 把上游 deadline、重试预算与客户端断开串成一棵取消树，`trace_id` 靠 `Value` 透传、业务参数仍走签名，正是本讲两条纪律的合用。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l4-concurrency/lesson-047-context`](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-047-context)。clone 仓库后进入 `examples/l4-concurrency`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="给下游调用加上超时" task="基于 parent 派生 50ms 超时的 context，defer 释放 cancel，并用 NewRequestWithContext 把 ctx 绑定到一次请求。" :hints='["用 context.WithTimeout 并 defer cancel()","标准库提供 http.NewRequestWithContext","ctx 绑定进请求后超时或断开都能取消"]' solution="ctx, cancel := context.WithTimeout(parent, 50*time.Millisecond)\ndefer cancel()\nreq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)\nif err != nil {\n    return err\n}\nresp, err := http.DefaultClient.Do(req)\nif err != nil {\n    return err\n}\ndefer resp.Body.Close()\nreturn nil" explain="WithTimeout 给下游独立预算，defer cancel 保证提前返回也释放计时器；ctx 绑进请求后超时或断开，底层传输立刻中止。" />

## 随堂检测
<Quiz no="047-1" q="关于 context.Value，下面哪种用法是推荐的？" :options='["把可选参数都塞进 Value 简化签名","存放请求作用域 trace id，用自定义非导出类型做 key","用 string 做 key 方便各包共享","把数据库连接放进 Value 复用"]' answer="1" explain="Value 只承载跨切面元数据，key 用非导出自定义类型避免冲突，取值判 ok；业务参数走函数签名。" />
<Predict no="047-2" q="父 context 被 cancel 后，由它派生的两个子 context 会怎样？" :options='["只有直接调 cancel 的那个被取消","父与所有子孙的 Done 同时关闭","子不受影响需各自取消","编译报错"]' answer="1" output="父与所有子孙的 Done 同时关闭，Err 均为 context canceled" explain="context 是取消树，信号沿父到子单向广播，子取消不影响父或兄弟。" />

## 一页纸速记
<KnowledgeCard :points='["接口只有 Deadline、Done、Err、Value 四方法","WithCancel 手动，WithTimeout 相对，WithDeadline 绝对","取消沿父到子单向广播","ctx 作第一参数，cancel 必须 defer","Value 只放元数据，key 自定义"]' :pitfalls='["丢弃 cancel 导致计时器无法释放","把业务参数塞进 Value 当隐形参数","把 context 存进结构体"]' summary="context 管一次请求的生命周期：取消树统一叫停，deadline 控预算，Value 透传元数据" />

## 小结与下一讲

- Context 通过 Done、Err、Deadline、Value 把取消与超时变成可传递的标准协议。
- 取消沿父到子单向广播，派生的 cancel 都要 defer；服务端从 r.Context() 层层设超时。
- Value 只放元数据，业务参数走签名，key 用自定义类型并判 ok。

[下一讲：并发模式：pipeline / fan-out / worker pool](/concurrency/patterns)
