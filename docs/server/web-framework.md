---
title: 第 62 讲 · 路由与框架：Gin / Echo / Chi
description: 拆解路由树与分组、绑定与校验、中间件生态，对比 Gin / Echo / Chi 的设计取向，学会按场景选框架而不是凭习惯
---

<StageHeader stage="L6 · 服务端与数据" level="★★★☆☆" time="40 分钟" goal="按场景选框架而不是凭习惯" prereq="第 61 讲 · net/http" />

# 第 62 讲 · 路由与框架：Gin / Echo / Chi

> 上一讲我们用标准库写清了 Handler 与中间件。既然如此，为什么还需要框架？因为真实项目里，「把 URL 分到函数」「把 JSON 绑到结构体并校验」「统一记日志和恢复 panic」这些事会重复几百次。框架把这些重复劳动收成三件套：路由树、绑定校验、中间件生态。这一讲我们不止会用 API，还要看穿它替我们做了什么，最后给出可落地的选型依据。

## 这一讲你会得到什么

<CheckList id="62-goal" title="本讲目标" :items='["理解路由树与分组前缀的本质，知道参数从哪来","会用 Chi、Gin 写出分组路由、绑定校验与自定义中间件","能从兼容性、生态、可维护性三个维度对比三者","明白为什么 benchmark 上的 QPS 不该是选型的唯一指标"]' />

## 1. 先建立直觉

把框架想成上一讲那个快递中心的「升级版流水线」。标准库给了你分拣员（ServeMux）和一条传送带（Handler 链），但每个工位都得自己搭。框架额外做了三件事：

第一，**路由树**。`ServeMux` 在 Go 1.22 之前基本是线性匹配加一点最长前缀，而 Gin、Echo、Chi 把注册的模式编译成前缀树（radix tree），匹配 `/v1/users/:id` 这类带参数的路径时，复杂度与路由数量无关。第二，**分组**：`Group("/v1")` 并不是什么魔法，它只是给这一批路由统一拼一个前缀，并允许在分组上挂只对组内生效的中间件。第三，**绑定与校验**：把请求体、查询串、路径参数按 tag 映射进结构体，顺手做必填、长度、格式检查。

<ConceptDiagram type="middleware-onion" title="框架把中间件管线做成洋葱，业务 Handler 在最内层" caption="Use 注册的中间件按注册顺序构成外层到内层的管线；分组中间件只作用于该分组" />

还有一个关键差异值得先记住：Chi 的路由器**本身就是一个 `http.Handler`**，中间件签名也是 `func(http.Handler) http.Handler`，所以标准库生态里的任何中间件、任何 `httptest` 工具都能直接用；Gin 和 Echo 则定义了自己的 `Context` 类型，性能与开发体验更好，但与标准库生态之间需要适配层。这个取舍会贯穿后面所有讲次。

## 2. 语法与规则

先看 Chi：它最贴近标准库，适合作为「从 net/http 过渡」的第一站。下面是一段完整、可运行的服务，包含分组路由、路径参数与中间件。

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	r := chi.NewRouter()
	// middleware 包里的组件都是标准 func(Handler) Handler，可自由替换。
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(5 * time.Second))

	r.Route("/v1/users", func(r chi.Router) {
		r.Get("/", listUsers)
		r.Get("/{id}", getUser)
		r.Post("/", createUser)
	})

	log.Println("listening on :8080")
	// chi.Router 实现了 http.Handler，直接传给标准库 Server。
	log.Fatal(http.ListenAndServe(":8080", r))
}

func listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []User{{ID: 1, Name: "gopher"}})
}

func getUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, User{ID: id, Name: "gopher"})
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
```

<OutputBox title="运行结果" output="GET /v1/users 返回 200，响应体是包含一条用户记录的 JSON 数组\nGET /v1/users/42 返回 200，id 为 42\nGET /v1/users/abc 返回 400，提示 invalid id" note="路由参数是字符串，转成业务类型时的错误必须自己处理，框架不会替你兜底" />

<LineByLine code="r := chi.NewRouter()\nr.Use(middleware.Logger)\nr.Route(prefix, register)\nr.Get(pattern, handler)\nhttp.ListenAndServe(addr, r)" :notes='["创建路由器，内部维护一棵前缀树","注册全局中间件，作用于其后所有路由","把一批路由挂到统一前缀下，register 是注册闭包","在分组内注册一条 GET 路由及其处理函数","把路由器当作标准 http.Handler 交给 Server"]' title="Chi 装配过程逐行看" />

再看 Gin。它用自带的 `*gin.Context` 承载请求与响应，绑定校验靠结构体 tag，中间件靠 `c.Next()` 划分前后置。

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type CreateUserReq struct {
	Name  string `json:"name" binding:"required,min=2,max=32"`
	Email string `json:"email" binding:"required,email"`
	Age   int    `json:"age" binding:"gte=0,lte=150"`
}

// costMiddleware 展示 c.Next() 的前后置语义，和标准库的洋葱完全一致。
func costMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next() // 先执行后续中间件与业务 handler
		log.Printf("path=%s status=%d cost=%v",
			c.FullPath(), c.Writer.Status(), time.Since(start))
	}
}

func main() {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), costMiddleware())

	v1 := r.Group("/v1")
	{
		users := v1.Group("/users")
		users.GET("", listUsers)
		users.GET("/:id", getUser)
		users.POST("", createUser)
	}

	log.Fatal(r.Run(":8080"))
}

func listUsers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": []string{"gopher"}})
}

func getUser(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
}

func createUser(c *gin.Context) {
	var req CreateUserReq
	// ShouldBindJSON 只绑定、不写响应；BindJSON 会自动写 400 并中断。
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": req})
}
```

`c.Next()` 把中间件切成两半：调用前是前置逻辑，返回后是后置逻辑，这与上一讲手写的 `logging` 完全同构。区别只在于 Gin 把 `ResponseWriter` 包进了 `c.Writer`，于是可以直接读到 `Status()`，不用自己写 `statusRecorder`。

## 3. 深入一步

框架的「魔法」拆开看都很朴素。第三段代码用零依赖的标准库手写一个带分组和绑定的迷你路由器，用来验证这件事。

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type route struct {
	method  string
	prefix  string
	handler http.HandlerFunc
}

type miniRouter struct{ routes []route }

// Group 用闭包给子路由统一拼前缀，模拟 Gin 的 Group 实现思路。
func (m *miniRouter) Group(prefix string, register func(*miniRouter)) {
	sub := &miniRouter{}
	register(sub)
	for _, r := range sub.routes {
		r.prefix = prefix + r.prefix
		m.routes = append(m.routes, r)
	}
}

func (m *miniRouter) Handle(method, pattern string, h http.HandlerFunc) {
	m.routes = append(m.routes, route{method: method, prefix: pattern, handler: h})
}

func (m *miniRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, rt := range m.routes {
		if r.Method == rt.method && strings.HasPrefix(r.URL.Path, rt.prefix) {
			rt.handler(w, r)
			return
		}
	}
	http.NotFound(w, r)
}

type CreateReq struct {
	Name string `json:"name"`
}

// bind 演示「解码 + 校验」：tag 解析库只是把这里的 if 换成了反射。
func bind(r *http.Request, dst *CreateReq) error {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return err
	}
	if dst.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	return nil
}

func main() {
	m := &miniRouter{}
	m.Group("/v1", func(sub *miniRouter) {
		sub.Handle(http.MethodGet, "/users", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, "list users")
		})
		sub.Handle(http.MethodPost, "/users", func(w http.ResponseWriter, r *http.Request) {
			var req CreateReq
			if err := bind(r, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(req)
		})
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", m))
}
```

<Term word="路由树" text="把 URL 模式按公共前缀组织成树，匹配时逐段下行；带参数段用特殊节点记录参数名，复杂度与路由总数无关" />

| 维度 | Gin | Echo | Chi |
| --- | --- | --- | --- |
| 核心抽象 | 自有 `gin.Context` | 自有 `echo.Context` | 标准 `http.Handler` |
| 与标准库生态 | 需适配 | 需适配 | 无缝复用 |
| 路由实现 |  radix tree | radix tree | radix tree |
| 绑定校验 | 内置 validator | 内置 validator | 需自行组合 |
| 中间件生态 | 最丰富 | 丰富 | 复用 net/http 生态 |
| 适合场景 | 业务接口、快速交付 | 轻量 API、偏好可插拔 | 渐进改造、强标准库约束 |

<Callout type="warn" title="反模式：只看 benchmark 选框架" text="网上流传的 QPS 榜单测的几乎都是「纯路由匹配 + 返回短字符串」，而真实接口的时间花在数据库、缓存、序列化上，框架开销占比通常不到百分之几。为一个每秒几十次请求的内部系统挑「最快」的框架，却牺牲了团队熟悉度、中间件生态与可维护性，是典型的优化错焦点。先看兼容性与生态，再看性能。" />

<Callout type="idea" title="迁移技巧" text="如果老项目是纯 net/http，优先选 Chi：把现有 Handler 原样挂进去，中间件逐个替换，不需要一次性重写。若项目从零开始且需要大量参数校验与分组，Gin 的开发效率通常更高。" />

### 路由匹配的协议与结构推导

框架的「路由树」不是凭空优化，它要兑现的是 URL 匹配的一套明确语义。先看标准库：Go 1.22 的 `ServeMux` 在注册时把每个模式串编译进一棵按「方法加路径段」组织的匹配树，并在注册期做**冲突检测**——若两个模式能匹配同一请求（如 `GET /users/{id}` 与 `GET /users/new` 之外的重叠），直接 panic，而不是等到运行时随机命中。匹配时遵循「更具体者优先」：静态段胜于通配段 `{name}`，结尾 `{$}` 锚定路径末尾。请求到达前还会做路径清洁（消除 `.`、`..`、重复斜杠）并可能触发尾部斜杠的 301 规范重定向；`%2F` 被保留为段内字符而不解码成斜杠，否则 `/a%2Fb` 会被误判成两层路径。这些行为源自 RFC 3986 的路径语义加上 Go 自己的规则，框架的 radix tree 只是把同一套规则做得更快。

radix tree 的复杂度可以推导：它按公共前缀压缩节点，`/v1/users`、`/v1/orders` 共享 `/v1/` 这段只比较一次，匹配代价与**路径段数**相关，而与注册的路由总数基本无关（近似 O(段数)）。线性扫描的旧 `ServeMux` 是 O(路由数)，哈希精确匹配是 O(1) 却表达不了参数段与通配。所以「框架比标准库快」在 Go 1.22 之后已不是结构性差距，而是常数与生态差距——这也把选型问题从「谁快」拉回「谁替你做了什么」。

### 设计取舍：三条路线的代价

| 维度 | 方案 A：纯 net/http 1.22 | 方案 B：Chi | 方案 C：Gin / Echo |
| --- | --- | --- | --- |
| 核心抽象 | 标准 `http.Handler` | 标准 `http.Handler` | 自有 `Context` |
| 与标准库生态 | 原生 | 原生，中间件签名就是 `func(Handler) Handler` | 需适配层 |
| 绑定与校验 | 自己写 | 自己组合 | 内置 validator，开发最快 |
| 路由实现 | 内置匹配树 | radix tree | radix tree |
| 迁移成本 | 无 | 老 Handler 原样挂入即可 | Handler 签名被框架绑定，退出成本高 |
| 主要代价 | 校验、分组、日志样板代码多 | 生态相对少 | 与 `httptest`、反代等标准工具隔离 |

三个方案对应三种「最怕什么」：怕依赖、要长期可维护选 A；怕重写存量 net/http 代码选 B；怕交付慢、需要大量参数校验与分组选 C。为什么本讲不直接宣布赢家？因为按 Amdahl 定律，端到端耗时 `T = T_db + T_net + T_framework`，若 `T_framework` 占比不到百分之几，把它优化一半对 `T` 的影响几乎测不出来；而迁移成本、团队熟悉度、中间件生态是实打实的一次性支出。下面的实验二会把这个比例量出来。

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 通配遮蔽 | 静态段与参数段优先级配错 | `/users/new` 被当成 id= new | 打开路由注册日志 | 注册期冲突检测，静态段优先 |
| 绑定重复写 | 混用 `BindJSON` 与自定义错误响应 | 响应体写两次、状态码错乱 | 响应体出现两段 JSON | 全项目统一一种绑定契约 |
| 漏中断 | 中间件判断失败后没 `return` / `Abort` | 已返回 401 又叠加业务数据 | 响应头与 body 不一致 | 每个提前返回分支都终止 |
| 恢复层错位 | 恢复中间件被放到内层 | 业务 panic 穿透，连接被重置 | 5xx 突增、日志无堆栈 | 恢复放最外层 |
| 分组过深 | 五六层嵌套 Group | 请求实际经过哪些层无人说得清 | 逐层打印中间件名 | 按版本与鉴权边界划分 |
| 路径判断绕过 | 用原始 `r.URL.Path` 做鉴权 | `/a/../admin` 规范化后绕过前缀判断 | 网关与 Handler 日志路径不一致 | 基于规范化路径或 `PathValue` |
| 301 被当 404 | 客户端未跟随尾部斜杠重定向 | 带斜杠路径莫名失败 | curl -i 看状态行 | 统一规范路径并让客户端跟随 |

<SequenceDiagram title="分组路由、绑定与 405 的时序" :actors='["Client","ServeMux","GroupMW","Handler","Store"]' :steps='[{"from":"Client","to":"ServeMux","label":"请求 GET","note":"路径先清洁规范化","kind":"call"},{"from":"ServeMux","to":"ServeMux","label":"树形匹配","note":"方法加路径段按特异性","kind":"self"},{"from":"ServeMux","to":"GroupMW","label":"进组","note":"分组中间件前置","kind":"call"},{"from":"GroupMW","to":"Handler","label":"交棒","note":"绑定与字段校验","kind":"call"},{"from":"Handler","to":"Store","label":"查询","note":"透传 r.Context","kind":"call"},{"from":"Store","to":"Handler","label":"返回行","note":"结果回传","kind":"return"},{"from":"Handler","to":"GroupMW","label":"写 200","note":"统一序列化","kind":"return"},{"from":"GroupMW","to":"ServeMux","label":"后置","note":"耗时日志","kind":"return"},{"from":"ServeMux","to":"Client","label":"响应","note":"keep-alive 复用","kind":"return"},{"from":"Client","to":"ServeMux","label":"请求 POST","note":"方法不匹配","kind":"call"},{"from":"ServeMux","to":"Client","label":"405","note":"带 Allow 头","kind":"return"}]' />

## 4. 工程实践

### 可验证实验：把 405、绑定和路由开销跑出来看

**实验一：curl 观察方法与路径契约。** 启动第 2 节的 Chi 服务（或任一 Go 1.22 `ServeMux` 服务），分别发合法与非法方法的请求：

```bash
curl -i http://127.0.0.1:8080/v1/users/42
curl -i -X POST http://127.0.0.1:8080/v1/users/42
```

<OutputBox title="两次请求的状态行与关键头" output="GET  /v1/users/42  → HTTP/1.1 200 OK\nPOST /v1/users/42  → HTTP/1.1 405 Method Not Allowed\nAllow: GET, HEAD" note="405 由路由层直接产生，业务 Handler 根本没被调用" />

逐字段解释：`200 OK` 说明路径参数被正确提取（返回体里的 id 来自 `PathValue` 或 `chi.URLParam`）；`405 Method Not Allowed` 是 RFC 9110 要求的状态码，表示资源存在但方法不被允许——它与 `404`（资源不存在）不能混用，否则客户端无法判断该换方法还是换地址；`Allow: GET, HEAD` 是 405 必须携带的响应头，告诉客户端与缓存哪些方法可用，也是「路由契约写到协议层」的直接证据。把 `GET /v1/users/{id}` 改成 `GET /v1/users/{id}/` 再请求，还能看到 301 规范重定向，验证路径清洁规则确实生效。

**实验二：httptest 加 go test -race 断言路由，再用 benchmark 量出框架开销占比。** 下面这段完整可编译的测试文件把「方法不匹配返回 405 且带 Allow」与「框架开销远小于一次数据库往返」同时变成可执行断言：

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.PathValue("id")))
	})
	return mux
}

func TestMethodAndPathValue(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "42" {
		t.Fatalf("GET: code=%d body=%q", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	newMux().ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/users/42", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: code=%d, want 405", rec2.Code)
	}
	if allow := rec2.Header().Get("Allow"); allow == "" {
		t.Fatal("405 未带 Allow 头")
	}
}

func BenchmarkRouteOnly(b *testing.B) {
	mux := newMux()
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}
}

func BenchmarkRouteWithDB(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Millisecond) // 模拟一次数据库往返
	})
	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}
}
```

```bash
go test -race -run TestMethodAndPathValue -v ./...
go test -run '^$' -bench . -benchmem ./...
```

<OutputBox title="正确性用例" output="=== RUN   TestMethodAndPathValue\n--- PASS: TestMethodAndPathValue (0.00s)\nPASS" note="405 与 Allow 头都是可断言的协议行为，不靠肉眼" />

<OutputBox title="benchmark（示意量级，机器不同数值不同）" output="BenchmarkRouteOnly-8      1400000    852 ns/op\nBenchmarkRouteWithDB-8       1100  1050000 ns/op" note="两者相差约三个数量级，路由匹配耗时占比不到千分之一" />

逐字段解释：`=== RUN` / `--- PASS` 说明方法路由与 `PathValue` 在 Go 1.22 语义下成立；`-race` 未报 `WARNING: DATA RACE`，说明 `httptest.NewRecorder` 各用例独立。`1400000` 是 `b.N`（迭代次数），`852 ns/op` 是单次「纯路由匹配」耗时，`1100` 与 `1050000 ns/op` 是「路由加 1 毫秒模拟数据库」的迭代与耗时。关键不是绝对值而是**比值**：约 1000 倍意味着把路由再优化一半，端到端只省不到千分之一——这正是「性能不是唯一指标」的量化依据，也是为什么该优先看兼容性、生态与迁移成本。

<Predict q="一个纯 net/http 服务升级到 Go 1.22 后仍手写 switch r.Method，最可能漏掉什么协议行为？" :options='["404 与 405 的区分","自动 gzip 压缩","自动生成 OpenAPI","连接池复用"]' answer="0" output="方法不匹配被当成普通 404 处理，且响应缺少 Allow 头" explain="Go 1.22 的 ServeMux 会自行返回 405 并带 Allow；手写 switch 若统一回 404，客户端就无法区分资源不存在与方法不允许，缓存与 SDK 也会误判。" />

### 两条真实工程误解

**误解一：框架路由的 benchmark 高，换成它就等价于接口变快。** 错。榜单测的是「纯路由匹配加返回短字符串」，而真实接口耗时由数据库、缓存、序列化主导；上面 benchmark 显示路由开销占比不到千分之一。为一个每秒几十次请求的内部系统挑「最快」框架，却牺牲团队熟悉度与标准库生态，是典型的优化错焦点。正确顺序是先用真实链路压测定位瓶颈，再决定要不要动框架。

**误解二：用了框架就和 `net/http` 生态绝缘，所有中间件都得重写。** 错。这只在 Gin / Echo 这类自有 `Context` 的框架里成立；Chi 的路由器本身实现 `http.Handler`，中间件签名就是标准 `func(http.Handler) http.Handler`，第 61 讲手写的 `logging`、`recoverPanic` 可以原样挂进去，`httptest.NewServer` 也直接可用。误以为「框架等于隔离」，会让人在可以渐进改造的项目里做了一次性重写。

### 与后续阶段的呼应

- 本讲的「分组前缀加组内中间件」在 /microservice/gateway 会被放大成网关按 URL 前缀做路由、灰度与统一鉴权；在 /microservice/framework 则变成用 Options 装配拦截器链，本质仍是 `func(Handler) Handler` 这条契约。
- /microservice/grpc-advanced 用拦截器把绑定失败、错误码、Deadline 收成统一横切能力，正是本讲中间件模型的 RPC 版本。
- L10 的 /projects/net-shortlink 用极简路由扛高并发跳转，正好说明「小项目用标准库或 Chi 足够」；/projects/micro-ecommerce 走 gRPC 加 HTTP 网关，才会遇到「同一套绑定契约要同时服务 HTTP 与 RPC」的选型问题。

选型之外，日常写框架代码有几个高频坑。第一，`ShouldBindJSON` 与 `BindJSON` 的错误处理契约不同：前者只返回错误，后者会替你写 400 并 `Abort`。混用时最容易出现的 bug 是「调用了 BindJSON 又自己写一次响应」，导致重复写、状态码错乱。第二，中间件里判断失败后必须 `return` 或 `c.Abort()`，否则后续 handler 仍会执行，出现「已经返回 401 却又写了业务数据」的叠加响应。第三，别把业务逻辑堆进中间件，中间件只做横切关注点。

<Compare title="绑定失败时，谁负责写响应" :panels='[{"label":"写法 A · 自己掌控契约","code":"if err := c.ShouldBindJSON(req); err != nil {\nc.JSON(400, gin.H{msg: err.Error()})\nreturn\n}","note":"错误格式与状态码由统一响应层决定，便于前后端对齐"},{"label":"写法 B · 框架代劳","code":"if err := c.BindJSON(req); err != nil {\nreturn\n}","note":"自动写 400 并中断，但响应体格式不可控，容易与统一错误契约打架"}]' />

<FlowTimeline title="一次框架选型决策" :steps='[{"title":"存量代码","desc":"纯 net/http 且要渐进改造，倾向 Chi","tone":"blue"},{"title":"团队熟悉度","desc":"已有一套 Gin 中间件与规范，别为新鲜感换栈","tone":"purple"},{"title":"生态需求","desc":"需要现成的校验、限流、OpenAPI 插件","tone":"orange"},{"title":"绑定取舍","desc":"是否接受自有 Context 与标准库生态隔离","tone":"green"},{"title":"压测验证","desc":"用真实链路压测，而不是路由 benchmark","tone":"gray"}]' direction="horizontal" />

还有一种反模式是**盲目套分组**：把路由分成五六层嵌套 Group，每层都挂中间件，最后没人说得清某个请求到底会经过哪些层。分组应当按「资源版本 + 是否需要鉴权」这类真实边界划分，并在代码里用注释写清每层的职责。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-062-web-framework`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-062-web-framework)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 Chi 分组加一个只作用于写操作的鉴权中间件" task="在 /v1/users 分组下，让 GET 请求无需鉴权，POST 与 DELETE 需要校验请求头中的令牌。请写出中间件函数与分组注册代码。" :hints='["中间件签名是 func(http.Handler) http.Handler，从 r.Header.Get 取令牌","校验失败用 http.Error 写 401 并 return，注意不要漏掉 return","可以把写操作单独放进一个子分组，在该子分组上调用 Use"]' solution="func auth(next http.Handler) http.Handler {\nreturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\nif r.Header.Get(headerToken) == empty {\nhttp.Error(w, msg, http.StatusUnauthorized)\nreturn\n}\nnext.ServeHTTP(w, r)\n})\n}\nr.Route(resourcePath, func(r chi.Router) {\nr.Get(slash, listUsers)\nr.Group(func(r chi.Router) {\nr.Use(auth)\nr.Post(slash, createUser)\n})\n})" explain="Chi 的子分组 Use 只对组内路由生效，这正是按资源边界收拢横切能力的标准做法。" />

## 随堂检测

<Quiz no="62-1" q="关于 Chi 与 Gin 的取舍，下列说法正确的是？" :options='["Gin 的路由器天然兼容所有 net/http 中间件","Chi 的路由器本身实现 http.Handler，可直接复用标准库生态","Echo 完全不支持中间件","三者的绑定校验都必须手写"]' answer="1" explain="Chi 坚持标准库接口，中间件也是 func(Handler) Handler；Gin/Echo 使用自有 Context，需要适配层。" />

<Predict no="62-2" q="Gin 中间件里写了 c.Next()，其后的 log.Printf 会在什么时候执行？" :options='["在业务 handler 之前","在业务 handler 返回之后","永远不会执行","在每次请求开始和结束各执行一次"]' answer="1" output="先执行业务 handler 并写出响应，再打印 cost 日志" explain="c.Next() 会先把控制权交给后续中间件与业务 handler，返回后才继续执行本中间件剩余语句，这正是洋葱模型的后置部分。" />

## 一页纸速记

<KnowledgeCard :points='["框架三件套：路由树与分组、绑定与校验、中间件生态","Chi 兼容 net/http，Gin/Echo 用自有 Context 换取开发体验","Group 只是统一拼前缀并支持组内中间件，没有魔法","c.Next() 与标准库 next.ServeHTTP 一样，把中间件切成前置与后置"]' :pitfalls='["拿纯路由 benchmark 的 QPS 当选型唯一依据","混用 BindJSON 与自定义错误响应，导致重复写","中间件判断失败后漏写 return 或 Abort，业务继续执行","分组嵌套过深，中间件执行顺序无人说得清"]' summary="框架只是把 net/http 的重复劳动收成三件套；看懂接口，选型就有依据。" />

## 小结与下一讲

- 路由树解决匹配效率，分组解决前缀复用与中间件作用域，绑定校验解决参数样板代码。
- Chi 的最大价值是与标准库生态无缝；Gin / Echo 的价值是开箱即用的开发体验。
- 选型优先看兼容性、生态与团队熟悉度，压测要基于真实链路而不是路由榜单。

会用框架写接口之后，下一个问题是：接口本身该怎么设计才稳定、可演进？这就进入 REST、状态码与 OpenAPI 的话题。

[下一讲：REST API 设计、校验与 OpenAPI](/server/rest-api)
