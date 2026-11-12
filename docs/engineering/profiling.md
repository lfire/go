---
title: 第 59 讲 · 性能剖析：pprof / trace / 火焰图
description: 建立可复现的性能诊断流程，掌握 CPU / Mem / Block / Mutex profile、火焰图读法、trace 与线上安全采集
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="50 分钟" goal="建立可复现的性能诊断流程" prereq="第 58 讲 · 设计模式与 Functional Options / wire" />

# 第 59 讲 · 性能剖析：pprof / trace / 火焰图

> 「接口变慢了」是一句没法行动的结论。可行动的是：CPU 时间花在哪个函数、谁在疯狂分配、哪些 goroutine 卡在锁上。pprof 与 trace 把运行时状态变成可查的证据，但工具本身不产生判断——这一讲的重点是**读图**和**安全采集**：知道宽平顶代表什么，也知道线上哪些开关不能常开。

## 这一讲你会得到什么
<CheckList id="goal-59" title="本讲目标" :items='["采集 CPU / Heap / Block / Mutex 四类 profile，并说清各自回答什么问题","读懂火焰图的横轴、纵轴与宽平顶，定位自身耗时的函数","用 trace 看调度延迟，并掌握线上安全采集的边界"]' />

## 1. 先建立直觉

把 profile 想成给运行中的程序拍 X 光：CPU profile 是每隔一小段时间问一句「现在在跑谁」，问得多了，谁出现得多谁就宽；heap profile 是拍一张内存快照，看谁占着对象不放；block 与 mutex profile 是看谁在排队等锁。火焰图把这些堆栈叠起来，越宽的横条越值得先动刀。

<ConceptDiagram type="perf-loop" title="优化闭环：基线 → 定位 → 改动 → 验证" />

关键心态是：**没有基线的优化只是猜测**。先跑一次 Benchmark 或采一份 profile，改且只改一处，再用 benchstat 或第二份 profile 证明差异。凭感觉换实现，经常越改越慢。

## 2. 语法与规则

第一块代码手工采集一份 CPU profile，适合离线复现一段确定的热点。

```go
// 手动采集 CPU profile：适合离线复现一段热点代码
package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
)

// hot 故意制造一段 CPU 热点，便于在 profile 里看到
func hot(n int) int {
	s := ""
	for i := 0; i < n; i++ {
		s += "x"
	}
	return len(s) + strings.Count(s, "x")
}

func main() {
	f, err := os.Create("cpu.pprof")
	if err != nil {
		fmt.Println("创建 profile 文件失败:", err)
		return
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Println("启动 CPU profile 失败:", err)
		return
	}
	defer pprof.StopCPUProfile()

	fmt.Println("热点结果:", hot(20000))
	fmt.Println("已写出 cpu.pprof，可用 go tool pprof 分析")
}
```

<OutputBox title="运行结果" output="热点结果: 40000\n已写出 cpu.pprof，可用 go tool pprof 分析" note="StartCPUProfile 与 StopCPUProfile 必须成对，defer 保证异常路径也能落盘。" />

第二块展示线上最常用的接入方式：副作用导入 `net/http/pprof` 会注册一组 `/debug/pprof/` 端点。生产上要把它挂在内网独立端口，而不是业务端口。

```go
// 引入 net/http/pprof 后会注册诊断端点，务必只挂内网端口
package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof" // 副作用导入：把处理器注册到 DefaultServeMux
	"net/http/httptest"
	"strings"
)

func main() {
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	fmt.Println("诊断端点状态码:", rec.Code)
	fmt.Println("是否列出 heap:", strings.Contains(rec.Body.String(), "heap"))
}
```

```text
诊断端点状态码: 200
是否列出 heap: true
```

第三块说明 block 与 mutex 采样默认关闭，必须按需打开、用完复位；heap profile 则可直接写出。

```go
// 内存、阻塞与锁竞争 profile：各有开关，别全量常开
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
)

func writeHeap() error {
	f, err := os.Create("heap.pprof")
	if err != nil {
		return err
	}
	defer f.Close()
	return pprof.WriteHeapProfile(f)
}

func main() {
	// 阻塞与互斥锁采样默认关闭，按需打开，结束后复位
	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)
	defer runtime.SetBlockProfileRate(0)
	defer runtime.SetMutexProfileFraction(0)

	if err := writeHeap(); err != nil {
		fmt.Println("写 heap profile 失败:", err)
		return
	}
	fmt.Println("已写出 heap.pprof")
	fmt.Println("goroutine 至少为 1:", runtime.NumGoroutine() != 0)
}
```

```text
已写出 heap.pprof
goroutine 至少为 1: true
```

<LineByLine title="逐行看 CPU profile 采集" code="f, err := os.Create(cpu.pprof)\ndefer f.Close()\nif err := pprof.StartCPUProfile(f); err != nil {\n    return\n}\ndefer pprof.StopCPUProfile()\n_ = hot(20000)" :notes='["创建输出文件，profile 是二进制采样数据","确保文件句柄被关闭","开始按固定频率采样当前调用栈","启动失败就直接返回，不再继续","if 条件闭合","defer 保证采样在所有路径上都被停止并写出","真正被测的热点代码放在起止之间"]' />

## 3. 深入一步

四类 profile 回答的问题互不重叠，选错工具等于白采：

| profile | 采集方式 | 回答什么问题 |
| --- | --- | --- |
| cpu | StartCPUProfile 或 /debug/pprof/profile | CPU 时间花在哪个函数 |
| heap | WriteHeapProfile 或 /debug/pprof/heap | 谁在分配、谁还活着 |
| block | SetBlockProfileRate 或 /debug/pprof/block | 谁在等 channel、等系统调用 |
| mutex | SetMutexProfileFraction 或 /debug/pprof/mutex | 锁竞争热点在哪 |
| trace | runtime/trace 的 Start | 调度延迟、GC 停顿、goroutine 状态 |

采集之前还要想清楚目标：是要降低延迟，还是要省内存，两者往往互相牵制。目标写成可测的数字，例如把某接口的 P99 从 80 毫秒压到 50 毫秒，profile 才知道该往哪个方向读；否则一份报告里 CPU、分配、锁都有热点，反而无从下手。

<Term word="火焰图" text="把采样到的调用栈按层级叠成的图，横轴宽度表示该函数在采样中出现的占比，纵轴表示调用栈深度。" />

<Callout type="warn" title="易错点：火焰图怎么读" text="横轴是采样占比而不是时间顺序，颜色只是区分函数不代表冷热。要找的是「宽平顶」：某个函数自己占很宽且上方没有更宽的子栈，说明时间花在它自身；若宽条上是层层子调用，该看 -cum 累计值，别急着优化最外层。" />

## 4. 工程实践

一组可以直接照抄的命令：

```bash
# CPU：交互式看最热函数，或直接出火焰图
go tool pprof cpu.pprof
(pprof) top
(pprof) list hot
go tool pprof -http=:8081 cpu.pprof

# 从运行中的服务在线采 30 秒 CPU
go tool pprof -http=:8081 http://127.0.0.1:6060/debug/pprof/profile?seconds=30

# 内存：分别看累计分配与当前存活
go tool pprof -alloc_space http://127.0.0.1:6060/debug/pprof/heap
go tool pprof -inuse_space http://127.0.0.1:6060/debug/pprof/heap

# 阻塞与锁竞争
go tool pprof http://127.0.0.1:6060/debug/pprof/block
go tool pprof http://127.0.0.1:6060/debug/pprof/mutex

# trace：看调度延迟、GC 停顿与 goroutine 生命周期
go tool trace -http=:8082 trace.out
```

读图时有两个高价值信号：若 `runtime.mallocgc` 或 GC 相关函数占了宽条，问题多半在分配而非算法，应回到 `B/op` 与 `allocs/op`；若锁与 channel 相关函数很宽，别急着加机器，先看是不是锁粒度太大。trace 则能补上 profile 看不到的时间维度——goroutine 在可运行队列里等了多久、一次 GC 停顿了多久。

还要留意可比性：不同负载、不同机器、不同 Go 版本采到的 profile 不能直接比。定位时应在同一压测流量下采基线与改动后各一份，用 `top` 与 `-cum` 交叉验证，再决定动哪里；否则很容易把流量波动误读成优化收益。

<Compare title="凭感觉与看证据" :panels='[{"label":"写法 A · 凭感觉","code":"// 反例：没有证据就改\n// 觉得 strings.Builder 慢，换成加号\n// 上线后 P99 反而升高","note":"结论不可复现，也没有回归防线"},{"label":"写法 B · 看证据","code":"// 正例：先定位再动手\ngo tool pprof -http=:8081 cpu.pprof\n// 只改 top 里的宽平顶函数\n// 改完用 benchstat 验证","note":"一次只动一处，差异有数据支撑"}]' />

线上安全采集是硬边界：CPU 采样有约百分之几的开销，heap 抓取会触发一次 GC，block 与 mutex 全量常开明显拖慢服务，因此默认关闭、按需短时打开并立刻复位。端点必须只监听 `127.0.0.1` 的独立诊断端口，加访问控制，绝不随业务端口对外暴露。

也别把 profile 当成一次性动作：优化合入后，应把基线与结论写进提交信息或性能文档，并在回归门禁里保留同一组基准。这样下次有人改回旧写法时能立刻发现，经验才不会随人员流动而丢失。

<Callout type="tip" title="一条可执行的检查" text="把诊断端口写进部署清单：只绑 127.0.0.1、不进 Ingress、不进 Service；每次线上采集记录起止时间与采样率，采完确认开关已复位。" />

## 动手练一练

<Exercise title="写出 heap profile" task="补全一个函数：创建指定路径的文件，把当前堆 profile 写进去，并确保文件被关闭。" :hints='["用 os.Create 创建文件","pprof.WriteHeapProfile 接收 io.Writer","用 defer 关闭句柄"]' solution="func writeHeap(path string) error {\n    f, err := os.Create(path)\n    if err != nil {\n        return err\n    }\n    defer f.Close()\n    return pprof.WriteHeapProfile(f)\n}" explain="WriteHeapProfile 接受任意 io.Writer，写文件时务必 defer Close，避免采样数据没落盘。" />

## 随堂检测

<Quiz no="59-1" q="火焰图里某个函数的横条越宽，通常代表什么？" :options='["它被调用的次数最少","它在采样中出现的占比越高，通常越耗 CPU","它的代码行数最多","它一定发生了内存泄漏"]' answer="1" explain="横轴是采样占比而非时间；宽说明该函数或其调用栈频繁出现在采样里，通常对应 CPU 热点。" />

<Predict no="59-2" q="代码里没有调用 runtime.SetBlockProfileRate，直接访问 /debug/pprof/block 会看到什么？" :options='["完整的阻塞采样数据","几乎没有数据的空 profile","编译报错","自动打开全量采样"]' answer="1" output="（几乎没有数据的空 profile）" explain="block 采样默认关闭，必须显式设置采样率才会记录，这是为了避免线上常开带来额外开销。" />

## 一页纸速记

<KnowledgeCard :points='["CPU 看耗时、Heap 看分配、Block 看等待、Mutex 看锁竞争","火焰图横轴是采样占比，优先盯宽平顶函数","-alloc_space 看累计分配，-inuse_space 看当前存活","trace 补上调度延迟与 GC 停顿的时间维度"]' :pitfalls='["没有基线就凭感觉优化，改完不做对比","把横轴当时间顺序、把颜色当冷热","线上常开 block / mutex 采样，或把诊断端口对外暴露"]' summary="可复现的诊断闭环是：先有基线，再用 profile 定位，一次只改一处，最后用数据验证。" />

## 小结与下一讲

- 四类 profile 各管一段，`go tool pprof` 与 `-http` 火焰图是主要读图手段。
- 横轴是采样占比，宽平顶才是自身耗时的函数；`-cum` 区分自己耗与被调函数耗。
- 线上采集要短时、按需、只绑内网，采完复位开关。

[下一讲：CI/CD、Makefile、代码生成与发布](/engineering/cicd)
