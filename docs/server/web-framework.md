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

## 4. 工程实践

选型之外，日常写框架代码有几个高频坑。第一，`ShouldBindJSON` 与 `BindJSON` 的错误处理契约不同：前者只返回错误，后者会替你写 400 并 `Abort`。混用时最容易出现的 bug 是「调用了 BindJSON 又自己写一次响应」，导致重复写、状态码错乱。第二，中间件里判断失败后必须 `return` 或 `c.Abort()`，否则后续 handler 仍会执行，出现「已经返回 401 却又写了业务数据」的叠加响应。第三，别把业务逻辑堆进中间件，中间件只做横切关注点。

<Compare title="绑定失败时，谁负责写响应" :panels='[{"label":"写法 A · 自己掌控契约","code":"if err := c.ShouldBindJSON(req); err != nil {\nc.JSON(400, gin.H{msg: err.Error()})\nreturn\n}","note":"错误格式与状态码由统一响应层决定，便于前后端对齐"},{"label":"写法 B · 框架代劳","code":"if err := c.BindJSON(req); err != nil {\nreturn\n}","note":"自动写 400 并中断，但响应体格式不可控，容易与统一错误契约打架"}]' />

<FlowTimeline title="一次框架选型决策" :steps='[{"title":"存量代码","desc":"纯 net/http 且要渐进改造，倾向 Chi","tone":"blue"},{"title":"团队熟悉度","desc":"已有一套 Gin 中间件与规范，别为新鲜感换栈","tone":"purple"},{"title":"生态需求","desc":"需要现成的校验、限流、OpenAPI 插件","tone":"orange"},{"title":"绑定取舍","desc":"是否接受自有 Context 与标准库生态隔离","tone":"green"},{"title":"压测验证","desc":"用真实链路压测，而不是路由 benchmark","tone":"gray"}]' direction="horizontal" />

还有一种反模式是**盲目套分组**：把路由分成五六层嵌套 Group，每层都挂中间件，最后没人说得清某个请求到底会经过哪些层。分组应当按「资源版本 + 是否需要鉴权」这类真实边界划分，并在代码里用注释写清每层的职责。

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
