---
title: 第 061 讲 · net/http 深入：Handler、ServeMux 与中间件链
description: 吃透 Handler 接口、ServeMux 路由、Request 与 ResponseWriter、中间件洋葱模型以及超时与优雅退出，不依赖框架也能写清 HTTP 服务
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="45 分钟" goal="不依赖框架也能写清楚 HTTP 服务" prereq="阶段五 · 工程化与质量" />

# 第 061 讲 · net/http 深入：Handler、ServeMux 与中间件链

> Go 后端几乎都跑在 `net/http` 之上，Gin、Echo、Chi 本质上只是把这一层包得更顺手。这一讲我们不用任何第三方库，直接用标准库搭出带路由、中间件、超时与优雅退出的服务。把地基看透之后，后面学框架会变成一件轻松的事：你只需要问「它替我做了什么」。

## 这一讲你会得到什么

<CheckList id="61-goal" title="本讲目标" :items='["能用 http.Handler 与 HandlerFunc 写出可注册的处理器","理解 ServeMux 的方法路由与通配段，会用 r.PathValue 取参数","能手写中间件并按洋葱模型组装调用链","能配置 Server 超时并实现信号驱动的优雅退出"]' />

## 1. 先建立直觉

把一个 HTTP 服务想成快递分拣中心：客户端发来的请求是包裹，`ServeMux` 是按地址分拣的分拣员，`Middleware` 是安检、贴单、称重这些必经工位，`Handler` 是最终拆包处理的人。包裹进去时依次经过各工位，处理完的回执再按**相反顺序**从各工位出来——这就是洋葱模型。

整个标准库的核心抽象小到令人意外：`http.Handler` 是一个只有一个方法的接口。下面这段可独立编译的代码演示它的形状与适配器：

```go
package main

import "net/http"

// Handler 是 net/http 的核心抽象：只约定一个方法。
type Handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func main() {
	// HandlerFunc 把普通函数适配成 Handler，编译期即可验证接口满足。
	var _ http.Handler = http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {},
	)
}
```

任何类型只要实现 `ServeHTTP` 就是 Handler；普通函数则通过 `http.HandlerFunc` 这个适配器类型（它自己实现了 `ServeHTTP`）转成 Handler。正是「接口足够小 + 函数可适配」这两点，让中间件可以统一成 `func(http.Handler) http.Handler`：吃进一个 Handler，返回一个包了一层逻辑的新 Handler。

<ConceptDiagram type="http-chain" title="一次请求穿过路由与中间件才到达业务" caption="请求向内穿透，响应按相反顺序返回；每一层都可以在调用内层之前和之后各做一件事" />

为什么中间件要设计成「返回新 Handler」而不是一个回调列表？因为这样组合出来的结果仍然是一个 `http.Handler`，可以继续被包、被替换、被传给 `http.Server`，也能与标准库的 `httptest`、反向代理等所有认这个接口的工具无缝协作。这就是「接受接口、返回结构体」思想在标准库里最漂亮的一次落地。

## 2. 语法与规则

第一段代码给出一个完整、可运行的最小服务：自定义 Handler、`HandlerFunc` 适配、`ServeMux` 注册，以及 Go 1.22 起支持的方法与通配段路由。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
)

// greeting 实现 ServeHTTP，因此它就是一个 http.Handler。
type greeting struct {
	prefix string
}

func (g greeting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// PathValue 是 Go 1.22 新增的方法，读取路由模式里的通配段。
	name := r.PathValue("name")
	fmt.Fprintf(w, "%s, %s! method=%s\n", g.prefix, name, r.Method)
}

func health(w http.ResponseWriter, r *http.Request) {
	// 顺序很重要：先设响应头，再写状态码，最后写响应体。
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func main() {
	mux := http.NewServeMux()

	// Go 1.22 起模式串支持 "METHOD /path/{wildcard}"，方法不匹配自动返回 405。
	mux.Handle("GET /hello/{name}", greeting{prefix: "你好"})
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	// HandlerFunc 把普通函数适配成 Handler；编译期就能检查接口是否满足。
	var _ http.Handler = http.HandlerFunc(health)

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

<OutputBox title="运行结果" output="GET /hello/gopher 返回：你好, gopher! method=GET\nGET /health 返回：ok\nPOST /hello/gopher 返回：405 Method Not Allowed" note="方法不匹配由 ServeMux 直接处理，业务 Handler 根本不会被调用" />

<LineByLine code="func main() {\nmux := http.NewServeMux()\nmux.HandleFunc(pattern, hello)\nlog.Fatal(http.ListenAndServe(addr, mux))\n}" :notes='["程序入口，所有装配都在这里完成","创建一个独立的路由表，避免污染全局 DefaultServeMux","把 URL 模式与处理函数绑定，pattern 是示意用的模式串","启动服务并把同一个 mux 作为根 Handler 传入"]' title="最小服务骨架逐行看" />

`ResponseWriter` 有一条容易忽略的契约：一旦调用过 `WriteHeader` 或第一次 `Write`，响应头就发出去了，之后再改 `Header()` 不会生效。所以校验、鉴权这类可能提前返回的逻辑，必须在写响应体之前决定好状态码。

第二段代码手写中间件链，把日志与 panic 恢复叠到同一个 mux 上。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Middleware 是 Go HTTP 生态最通用的签名：吃一个 Handler，返回一个新 Handler。
type Middleware func(http.Handler) http.Handler

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("进入 %s %s", r.Method, r.URL.Path) // 前置：请求向内
		next.ServeHTTP(w, r)                            // 交棒给内层
		log.Printf("退出 %s 耗时 %v", r.URL.Path, time.Since(start)) // 后置：响应向外
	})
}

func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// chain 按参数顺序包成洋葱：第一个参数位于最外层。
// 必须逆序循环，否则后写的中间件反而先被执行。
func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "business logic")
	})
	// recoverPanic 放最外层，才能兜住业务和日志中间件里的 panic。
	handler := chain(mux, recoverPanic, logging)
	log.Fatal(http.ListenAndServe(":8080", handler))
}
```

注意 `chain` 里的逆序循环：`recoverPanic(logging(mux))` 才是想要的嵌套，若正序包裹会得到 `logging(recoverPanic(mux))`，panic 恢复层就跑到内层去了，日志中间件自身若 panic 便无人兜底。**中间件顺序不是风格问题，而是语义问题**：越靠外的层越早见到请求、越晚见到响应，也越适合放恢复、超时、链路追踪这类横切能力。

## 3. 深入一步

`Request` 与 `ResponseWriter` 分别代表输入与输出，二者都绑定在单次请求上。`r.Context()` 在客户端断开、请求超时或服务端关闭时会被取消，所有下游调用（数据库、RPC）都应把它透传下去，才能及时释放 goroutine。

<Term word="ServeMux" text="标准库的请求路由器：把 URL 模式匹配到对应 Handler，自身也实现 http.Handler，因此可以被中间件整体包裹" />

| `http.Server` 字段 | 作用 | 不设的后果 |
| --- | --- | --- |
| `ReadHeaderTimeout` | 限制读完请求头的时间 | 慢请求头攻击可长期占住连接与 goroutine |
| `ReadTimeout` | 限制读完整个请求（含 body） | 上传大 body 时连接被无限拖住 |
| `WriteTimeout` | 限制写响应的总时长 | 慢客户端让 handler 迟迟无法退出 |
| `IdleTimeout` | keep-alive 空闲连接存活时间 | 空闲连接堆积，fd 被耗尽 |

| 概念 | 本质 | 典型职责 |
| --- | --- | --- |
| `Handler` | 接口 | 定义「处理请求」的统一契约 |
| `HandlerFunc` | 适配器类型 | 让普通函数满足 `Handler` |
| `ServeMux` | 具体 Handler | 按方法与路径分发 |
| `Middleware` | 函数签名约定 | 日志、鉴权、恢复、限流 |

<Callout type="warn" title="反模式：把 context 存进结构体" text="常见错误是把 context.Context 塞进结构体字段长期持有，例如定义带 ctx 字段的 Service 并在构造时赋值。context 是请求作用域的值，应当作为函数第一个参数显式传递，或只在本次请求内通过 r.Context() 取得；存进结构体后取消信号会失效、生命周期会错乱，函数签名也会对调用者撒谎。" />

<Callout type="idea" title="版本差异" text="Go 1.22 之前 ServeMux 只匹配路径，方法判断要自己写 switch r.Method；1.22 起支持带方法和通配符的模式串，并新增 r.PathValue 读取通配段。新项目可以直接用，老项目升级时注意路径转义与重定向行为的变化。" />

### 协议与契约：为什么方法、状态码和超时不能凭感觉

`net/http` 的行为不是 Go 自己定的，而是 RFC 9110（HTTP Semantics）与 RFC 9112（HTTP/1.1 报文语法）的实现。把规范摊开，前面那些「顺序很重要」「方法不匹配返回 405」就有了推导链，而不只是需要死记的结论。

**方法语义与幂等。** RFC 9110 把方法拆成「安全（safe）」与「幂等（idempotent）」两个正交维度：安全指不改变服务端状态（GET、HEAD、OPTIONS、TRACE），幂等指同一请求执行一次与执行 N 次、服务端最终状态相同（GET、HEAD、PUT、DELETE、OPTIONS、TRACE）。POST 既不幂等也不安全，PATCH 通常也不幂等。为什么服务端工程师必须较真？因为「谁可以重放请求」是基础设施决定的：浏览器、CDN、网关、负载均衡器在超时或连接被重置时，默认只会自动重试**安全且幂等**的方法。把创建订单设计成 GET，或让 GET 承担扣库存，缓存、预取、重试就会替你悄悄多下几单。推导结论：方法不是路由标签，而是写给全链路中间件的一纸承诺。

**状态码是给机器读的契约。** 2xx / 4xx / 5xx 的首位直接决定缓存与告警：2xx 配合 `Cache-Control` 默认可缓存；4xx 表示请求方错了、重试同样请求不会有不同结果；5xx 表示服务端暂时失败、可以带退避重试。这正是第 063 讲把 400 与 422 分开、把 409 从 500 里摘出来的依据。混用的代价很具体：把限流写成 503 而不是 429，客户端重试逻辑会当成服务端故障而继续加大流量（重试放大）；把鉴权失败写成 404 而不是 401 / 403，SDK 不会触发重新登录。

**超时与取消是一条链，不是一个数。** `r.Context()` 在三种情况下被取消：客户端断开、服务端 `Server` 主动关闭或写超时、以及显式 `CancelFunc`。RFC 并不规定超时怎么设，但 Go 把四类超时拆开正是为了对齐 TCP 生命周期：`ReadHeaderTimeout` 对齐「请求行加头部必须在时限内到达」，`ReadTimeout` 覆盖到 body 读完，`WriteTimeout` 从读完请求头算起覆盖写回，`IdleTimeout` 管 keep-alive 空闲连接。推导：只设 `ReadTimeout` 而不设 `ReadHeaderTimeout`，Slowloris 仍能在头部阶段占住连接；只设 `WriteTimeout` 而让连接永久 keep-alive，fd 仍会被空闲连接耗尽。四个字段必须配套——这就是第 3 节那张表的「为什么」。

**连接复用与半开连接。** HTTP/1.1 默认持久连接（除非显式 `Connection: close`），一条 TCP 连接串行承载多个请求，`IdleTimeout` 到期后服务端主动发 FIN 回收。真正的难点是「半开连接（half-open）」：对端崩溃、被 NAT 超时清除、或中间设备静默丢包时，本端 TCP 状态仍是 ESTABLISHED 却再也收不到字节。服务端无法靠 accept 感知，只能靠 `IdleTimeout` 加读探测；客户端侧 `http.Transport` 的 `IdleConnTimeout` 与 `ResponseHeaderTimeout` 同理。所以「连接复用」与「超时」必须一起讲：复用提升了吞吐，也让僵尸连接更难被发现。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（本讲采用） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 启动服务 | `http.ListenAndServe(addr, mux)` | 显式构造 `http.Server` 配齐四类超时 | 零值 Server 无超时，Slowloris 可长期占住 goroutine 与 fd |
| 超时控制 | 每个 Handler 内用 `time.After` 自建 | `r.Context()` 透传加 Server 超时 | 自建超时无法取消下游 DB / RPC，goroutine 仍空转到点 |
| 中间件组合 | 框架式回调列表、全局注册 | `func(Handler) Handler` 返回新 Handler | 回调列表的结果不是 `http.Handler`，无法被 `httptest`、反代与 Server 直接复用 |
| 连接管理 | 每请求新建连接 | keep-alive 加 `IdleTimeout` | 三次握手与慢启动反复付出，高并发下延迟与 fd 都翻倍 |

再补两个「为什么不」的推导。其一，中间件为什么是「返回新 Handler」而不是往切片里 append 回调？因为组合结果必须仍是 `http.Handler`，才能继续被包、被替换、被交给 `http.Server` 与 `httptest.NewServer`；换成回调切片就丢了整个标准库生态，这是「接受接口、返回结构体」的直接收益。其二，为什么退出要配 `signal.NotifyContext` 加 `Shutdown` 而不是直接 `os.Exit`？因为 SIGTERM 到达时监听必须先关闭（拒绝新连接）、再等在途请求结束；直接退出等于腰斩在途请求，滚动发布时表现为随机 502。

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 读超时 | 慢请求头 / 慢 body | 连接长期占用，新连接排队 | `ReadHeaderTimeout` 命中日志、fd 数 | 设齐四类超时并透传 ctx |
| 写超时 | 慢客户端不读响应 | Handler 迟迟不返回，goroutine 堆积 | goroutine profile | `WriteTimeout` 加响应体分块 |
| 重试放大 | 网关对 5xx 无限重试 | 后端 QPS 成倍高于入口 | 入口与后端 QPS 对比 | 429 加退避加重试预算（见 /microservice/resilience） |
| 幂等缺失 | POST 超时被自动重放 | 重复下单、重复扣款 | 幂等键命中日志 | 幂等键加唯一索引（见 /server/rest-api） |
| 连接池耗尽 | 下游慢且无超时 | 延迟毛刺而非报错，等待队列变长 | 下游 `WaitCount` | 超时加并发上限加熔断 |
| 半开连接 | NAT 静默断连 | 借到死连接，请求偶发 EOF | `IdleTimeout` 命中统计 | 心跳、读探测、缩短 idle |
| 取消未透传 | 用 `context.Background()` 调下游 | 客户端已断，DB 慢查询仍在跑 | SQL 慢日志 | 全程 `r.Context()` |

<SequenceDiagram title="keep-alive 复用与取消传播" :actors='["Client","Server","Middleware","Handler","Downstream"]' :steps='[{"from":"Client","to":"Server","label":"请求 1","note":"建连并携带 keep-alive","kind":"call"},{"from":"Server","to":"Middleware","label":"进链","note":"日志与恢复前置","kind":"call"},{"from":"Middleware","to":"Handler","label":"交棒","note":"进入业务","kind":"call"},{"from":"Handler","to":"Downstream","label":"带 ctx 查询","note":"透传 r.Context","kind":"call"},{"from":"Downstream","to":"Handler","label":"返回","note":"结果回传","kind":"return"},{"from":"Handler","to":"Server","label":"写响应","note":"先设头再写码","kind":"return"},{"from":"Server","to":"Client","label":"响应 1","note":"连接不关闭","kind":"return"},{"from":"Client","to":"Server","label":"请求 2","note":"复用同一条连接","kind":"call"},{"from":"Client","to":"Client","label":"超时放弃","note":"ctx 到点","kind":"self"},{"from":"Server","to":"Handler","label":"取消","note":"Done 通道关闭","kind":"async"},{"from":"Handler","to":"Downstream","label":"取消下游","note":"驱动取消在途调用","kind":"call"}]' />

## 4. 工程实践

### 可验证实验：把复用、超时与取消跑出来看

**实验一：curl 观察 keep-alive 复用。** 启动第 4 节那个带超时的服务，把同一个 URL 写两次，curl 会在一条命令里串行发两个请求：

```bash
curl -sv --http1.1 http://127.0.0.1:8080/slow http://127.0.0.1:8080/slow -o /dev/null 2>&1 | grep -E "Connected to|Re-using|HTTP/1.1 200"
```

<OutputBox title="curl -v 关键行（按出现顺序）" output="第 1 次: Connected to 127.0.0.1 port 8080 (#0)\n第 1 次: HTTP/1.1 200 OK\n第 2 次: Re-using existing connection! (#0)\n第 2 次: HTTP/1.1 200 OK" note="同一个 #0 被复用，第二次没有再出现 Connected to" />

逐字段解释：`Connected to ... (#0)` 中 `#0` 是 curl 给这条 TCP 连接的内部编号，只在第一次出现说明第二次没有新建连接；`Re-using existing connection! (#0)` 证明连接被复用，省掉一次三次握手与慢启动；两条 `HTTP/1.1 200 OK` 说明 `/slow` 睡 3 秒但在 `WriteTimeout=10s` 内完成。做两个对照：把 `WriteTimeout` 改成 1 秒，第二次会拿到被截断的响应或读错误，直接验证写超时生效；加请求头 `Connection: close` 再跑，会出现两次 `Connected to`，把复用收益量化出来。

**实验二：httptest 加 go test -race 验证取消传播。** 下面是一段完整可编译的测试文件，把「客户端取消后服务端下游能否立刻停下」变成断言：

```go
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// cancelAware 模拟一个会透传 ctx 的下游调用；取消后应立即返回。
func cancelAware(ctx context.Context, started chan struct{}) error {
	close(started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return nil
	}
}

func TestRequestContextCancelledOnClientGone(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = cancelAware(r.Context(), started)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	done := make(chan struct{})
	go func() {
		_, _ = http.DefaultClient.Do(req)
		close(done)
	}()
	<-started
	cancel() // 模拟客户端断开
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("客户端已取消，但服务端未及时返回")
	}
}
```

```bash
go test -race -run TestRequestContextCancelledOnClientGone -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestRequestContextCancelledOnClientGone\n--- PASS: TestRequestContextCancelledOnClientGone (0.00s)\nPASS\nok   example.com/server   1.203s" note="取消后请求立刻返回，且 race 检测无数据竞争" />

逐字段解释：`=== RUN` 是用例开始；`--- PASS ... (0.00s)` 表示请求在 `cancel()` 后立刻返回，没有傻等 `cancelAware` 里那个 2 秒定时器，证明 `r.Context()` 的取消确实传播到了下游分支；`PASS` 与 `ok` 是包级结果；`-race` 未打印 `WARNING: DATA RACE`，说明 `started` 通道与 goroutine 之间同步正确。把 `cancelAware` 里的 `ctx.Done()` 分支删掉再跑，用例会在 1 秒兜底处 `t.Fatal`，于是「透传 ctx」从口号变成可证伪的断言。

<Predict q="把 Server 的 WriteTimeout 设成 1 秒、Handler 睡 3 秒，客户端最可能收到什么？" :options='["完整的 200 响应","被截断的响应或读超时错误","404","自动重试后成功"]' answer="1" output="写超时到点后连接被关闭，客户端收到不完整响应或 read: connection reset by peer" explain="WriteTimeout 从读完请求头开始计时，到点直接关连接，不会等 Handler 自然结束；长耗时接口必须自己用 ctx 控制节奏，而不是依赖更长的写超时。" />

### 两条真实工程误解

**误解一：设了 `WriteTimeout` 就不用在 Handler 里管 ctx 了。** 错。`WriteTimeout` 只保证「到点关连接」，它不会主动取消你正在跑的 goroutine 里的计算或下游调用；如果 Handler 用 `context.Background()` 发了 RPC，客户端已断、写超时已到，那段 RPC 仍会跑满自己的超时，继续占着 goroutine 与连接池。错误写法 `db.QueryRow(query)`，正确写法 `db.QueryRowContext(r.Context(), query)`，差别就在取消能否抵达。

**误解二：keep-alive 一定更快，连接留得越久越好。** 错。空闲连接占 fd 与服务端内存，而 NAT、负载均衡器常有自己的静默断连时间（常见 60 到 350 秒）；`IdleTimeout` 一旦大于链路中最短的那个超时，客户端就会借到一条「半开」的死连接，表现为偶发 EOF 或首个请求莫明失败。正确做法是把 `IdleTimeout` 设得略小于链路里最短的空闲断连时间，并让 `http.Transport.IdleConnTimeout` 与之一致。

### 与后续阶段的呼应

- 本讲的方法、状态码与取消契约会在 /server/rest-api 变成幂等键、400 与 422 的分工，在 /server/websocket 变成「升级后必须摘出短请求超时体系」的例外。
- L7 的 /microservice/grpc-advanced 把 `r.Context()` 超时升级为 gRPC 的 Deadline 传播与 metadata 透传，拦截器把超时与取消收成统一横切能力；/microservice/gateway 则把四类超时、鉴权、限流放到统一入口，Slowloris 防护由网关集中承担。
- L10 的 /projects/net-shortlink 里，跳转接口是典型「读多写少、必须低延迟」的场景，本讲的 keep-alive、显式超时与优雅退出是它扛住高并发跳转的前提；/projects/ai-gateway 的 SSE 流式转发则必须按本讲的结论单独放宽写超时。

最容易被忽略的生产事故来自这一行：`http.ListenAndServe(addr, mux)`。它内部构造的是一个**零值** `http.Server`，所有超时字段都是 0，也就是永不超时。一个恶意客户端只发请求头不发完，就能一直占住一个 goroutine 和一个文件描述符，这被称为 Slowloris。生产服务必须显式构造 `http.Server`，并且配合信号做优雅退出，否则滚动发布时 SIGTERM 一到，在途请求会被硬生生掐断。

<Compare title="同样是启动服务，差别在超时与退出" :panels='[{"label":"写法 A · 省事但危险","code":"http.ListenAndServe(addr, mux)","note":"零值 Server，没有任何超时；进程退出时在途请求被直接丢弃"},{"label":"写法 B · 生产可用","code":"srv := new(http.Server)\nsrv.Addr = addr\nsrv.Handler = mux\nsrv.ReadHeaderTimeout = 2 * time.Second\nsrv.WriteTimeout = 10 * time.Second\nsrv.ListenAndServe()","note":"显式超时 + 配合 Shutdown，慢连接有上限、发布可无损"}]' />

第三段代码给出完整、可运行的带超时与优雅退出的服务，包含 Handler 与路由注册。

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			fmt.Fprintln(w, "done")
		case <-r.Context().Done():
			// 客户端断开或超时就立刻退出，别让 goroutine 白等。
			log.Println("request cancelled")
		}
	})

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,  // 防慢请求头
		ReadTimeout:       5 * time.Second,  // 含 body 的读取上限
		WriteTimeout:      10 * time.Second, // 响应写回上限
		IdleTimeout:       60 * time.Second, // keep-alive 空闲回收
	}

	// 把 SIGINT / SIGTERM 转成 context 取消，这是服务端的标准写法。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Println("listening on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done() // 阻塞直到收到退出信号
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Println("server stopped")
}
```

`Shutdown` 会先关闭监听、拒绝新连接，再等在途请求结束；超过给定 context 的期限仍未结束才会强制返回。注意判断 `http.ErrServerClosed`：正常退出时 `ListenAndServe` 会返回这个哨兵错误，不算故障。

<FlowTimeline title="一次请求的完整生命周期" :steps='[{"title":"连接接入","desc":"Accept 后交给 Server 的 serve 循环","tone":"blue"},{"title":"只读请求头","desc":"受 ReadHeaderTimeout 约束","tone":"purple"},{"title":"路由匹配","desc":"ServeMux 按方法与路径选中 Handler","tone":"orange"},{"title":"中间件前置","desc":"日志、鉴权、恢复依次进入","tone":"blue"},{"title":"业务处理","desc":"读 body、查数据、写响应","tone":"green"},{"title":"响应返回","desc":"后置逻辑逆序执行，连接按 IdleTimeout 复用或回收","tone":"gray"}]' direction="horizontal" />

还有一个高频反模式：**每次请求都新建一个 ServeMux 或 Server**。它们都是并发安全、应当长生命周期复用的对象；在 Handler 内部反复 `http.NewServeMux()` 并注册，既浪费又有数据竞争风险。装配只应在 `main` 里做一次。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-061-net-http`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-061-net-http)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="写一个能记录状态码的耗时日志中间件" task="实现 logging 中间件，打印请求方法、路径、耗时与最终状态码。提示：http.ResponseWriter 是接口，默认实现不暴露状态码，需要自定义一个包装类型覆写 WriteHeader 把状态码记下来。" :hints='["定义 statusRecorder 嵌入 http.ResponseWriter，增加 code 字段","覆写 WriteHeader 记录第一次写入的状态码；若一次都没写，默认是 200","把 recorder 传给 next.ServeHTTP，后置逻辑里读 recorder.code 打日志"]' solution="type statusRecorder struct {\nhttp.ResponseWriter\ncode int\n}\nfunc (s *statusRecorder) WriteHeader(code int) {\ns.code = code\ns.ResponseWriter.WriteHeader(code)\n}\nfunc logging(next http.Handler) http.Handler {\nreturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\nrec := new(statusRecorder)\nrec.ResponseWriter = w\nrec.code = http.StatusOK\nstart := time.Now()\nnext.ServeHTTP(rec, r)\nlog.Printf(method, r.URL.Path, rec.code, time.Since(start))\n})\n}" explain="包装 ResponseWriter 是 Go 中间件取状态码的惯用手法，Gin、Chi 的日志中间件内部也是同样的思路。" />

## 随堂检测

<Quiz no="061-1" q="http.Handler 接口要求实现哪个方法？" :options='["ServeHTTP","HandlerFunc","Handle","Serve"]' answer="0" explain="Handler 只约定一个 ServeHTTP(ResponseWriter, *Request) 方法；HandlerFunc 是把函数适配成该接口的类型，不是接口方法。" />

<Predict no="061-2" q="用 chain(mux, recoverPanic, logging) 组装后，一次正常请求中日志的打印顺序是？" :options='["先打印进入，业务执行，最后打印退出","先打印退出，再打印进入，最后业务执行","只打印进入，退出永远不执行"]' answer="0" output="进入 GET /\nbusiness logic\n退出 / 耗时 12.3µs" explain="洋葱模型下前置逻辑在内层调用之前执行，后置逻辑在 next.ServeHTTP 返回之后执行，因此是进入、业务、退出。" />

## 一页纸速记

<KnowledgeCard :points='["Handler 是单方法接口，HandlerFunc 把函数适配成 Handler，Middleware 是 func(Handler) Handler","ServeMux 自身也是 Handler，Go 1.22 起支持方法路由与 r.PathValue 通配段","洋葱模型：请求向内穿透，响应逆序返回；中间件顺序决定语义","生产必须显式配置 Server 超时，并用 signal.NotifyContext 加 Shutdown 优雅退出"]' :pitfalls='["用零值 Server 启动，无任何超时，慢连接可拖垮服务","把 context 存进结构体字段长期持有，取消信号失效","在 Handler 里反复创建 ServeMux 或 Server","先 Write 再改响应头，改动不生效"]' summary="整个 net/http 都围绕一个极小的 Handler 接口展开，看懂它，就看懂了所有 Go Web 框架。" />

## 小结与下一讲

- `Handler` / `HandlerFunc` / `ServeMux` 三层抽象让路由、中间件与业务可以任意组合，且全程只依赖标准库。
- 中间件是「吃 Handler 返回 Handler」的函数，洋葱模型的顺序决定谁能兜底、谁先见请求。
- 生产服务要显式设置四类超时，并用信号驱动的 `Shutdown` 保证发布时在途请求不被掐断。

标准库已经够用，但真实项目里路由分组、参数绑定、校验这些重复劳动还是希望有人替我们做。下一讲就来对比 Gin、Echo、Chi。

[下一讲：路由与框架：Gin / Echo / Chi](/server/web-framework)
