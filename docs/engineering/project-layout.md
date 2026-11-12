---
title: 第 50 讲 · 项目结构与分层设计
description: 用 cmd / internal / pkg / api 搭出依赖方向清晰的骨架，分清按领域还是按层，划清 DO / DTO / VO 并克制过度设计
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="搭出能被团队接手的项目骨架" prereq="第 49 讲 · 数据竞争与 race detector" />

# 第 50 讲 · 项目结构与分层设计

> 一个人写三天的脚本，全部塞进 main.go 毫无问题；可一旦要两个人协作、要半年后回来改，目录怎么切就决定了「改一处会不会牵连十处」。项目结构不是审美偏好，而是一份关于「谁可以依赖谁」的团队约定。这一讲把这份约定落成一棵新人一眼能看懂的目录树，并把判断标准定在可维护、可测试上，而不是目录看起来够不够「专业」。

## 这一讲你会得到什么
<CheckList id="goal-50" title="本讲目标" :items='["说清 cmd / internal / pkg / api 各自该放什么，以及 internal 为何能挡住误用","在按领域分包与按技术层分包之间做出有依据的取舍","用 DO / DTO / VO 划清数据边界，并识别过度设计"]' />

## 1. 先建立直觉

把项目想成一栋楼：`cmd` 是临街的大门，只负责把客人迎进来、把水电接通；`api` 与 handler 是前台，负责听懂外面说什么、把话翻译给里面；service 和 domain 是真正干活的房间，装着业务规则；数据库、缓存、第三方客户端这些实现细节，被锁在 `internal` 的地下室里。楼里有一条硬规矩：**信息可以向下传，依赖只能向下指**。前台可以叫业务层干活，业务层绝不能反过来 import 前台，否则前台一改，业务层全得跟着重新编译。

<ConceptDiagram type="layout" title="分层骨架：依赖只能向下" />

Go 用两个语言级机制把这条规矩写死：包导入不允许成环；`internal` 目录具有特殊可见性，只有它的父目录以内的包才能 import 它。于是「实现细节」被物理地关在了内部，外部模块想偷懒直接捞数据库连接都做不到。这不是限制，是保护——它让「谁被允许改动」变成编译器能检查的事实，而不是一份没人看的文档。

## 2. 语法与规则

先看一个能真实落地的目录树。注意它没有 `controller / model / dao` 这种照搬三层架构的空壳，而是按职责收拢：

```text
myshop/
├── go.mod
├── cmd/
│   └── api/
│       └── main.go            # 唯一入口：读配置、装配依赖、启动服务
├── api/
│   └── order/v1/             # protobuf / OpenAPI 契约，跨服务共享
├── internal/
│   ├── order/                # 按领域切分：订单的全部逻辑
│   │   ├── service.go        # 业务用例编排
│   │   ├── repository.go     # 接口（抽象，属于业务层）
│   │   ├── model.go          # DO / DTO / VO
│   │   └── postgres.go       # 接口的具体实现
│   ├── user/
│   └── server/               # HTTP 路由与 handler
└── pkg/
    └── money/                # 可被外部安全复用的纯工具，无业务状态
```

第一块代码演示依赖方向：接口由业务层定义，具体实现通过构造函数传进去，入口只做装配。

```go
// cmd/api/main.go 的等价可运行示例：装配根
package main

import (
	"fmt"
)

// OrderRepository 是领域层定义的抽象，属于业务规则，不依赖任何数据库。
type OrderRepository interface {
	AmountOf(id string) (int, error)
}

// OrderService 只依赖抽象接口，实现可以被自由替换。
type OrderService struct {
	repo OrderRepository
}

func NewOrderService(r OrderRepository) *OrderService {
	return &OrderService{repo: r}
}

func (s *OrderService) Total(id string) (int, error) {
	return s.repo.AmountOf(id)
}

// memoryRepo 是放在 internal 下的具体实现，外部包无法直接 import。
type memoryRepo struct {
	data map[string]int
}

func (m *memoryRepo) AmountOf(id string) (int, error) {
	v, ok := m.data[id]
	if !ok {
		return 0, fmt.Errorf("订单 %s 不存在", id)
	}
	return v, nil
}

func main() {
	// 唯一知道「谁实现谁」的地方：入口装配
	svc := NewOrderService(&memoryRepo{data: map[string]int{"A-1001": 280}})
	total, err := svc.Total("A-1001")
	if err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	fmt.Println("订单金额:", total)
}
```

<OutputBox title="运行结果" output="订单金额: 280" note="把 memoryRepo 换成 PostgreSQL 实现，OrderService 一行都不用改，这正是可测试的来源。" />

第二块代码解决另一个常见问题：同一个「订单」在存储、传输、展示三个环节长得并不一样，硬用同一个结构体会让字段互相污染。

```go
// internal/order/model.go 的等价可运行示例：三种数据对象各司其职
package main

import (
	"encoding/json"
	"fmt"
)

// OrderDO 面向存储：保留数据库字段与内部状态码。
type OrderDO struct {
	ID        int64 `json:"id"`
	Status    int   `json:"status"`
	CreatedAt int64 `json:"created_at"`
}

// OrderDTO 面向服务间传输：字段名跟随接口契约。
type OrderDTO struct {
	OrderID string `json:"order_id"`
	Amount  int    `json:"amount"`
}

// OrderVO 面向前端展示：带展示文案，金额已格式化。
type OrderVO struct {
	Title      string `json:"title"`
	AmountText string `json:"amount_text"`
}

func main() {
	do := OrderDO{ID: 1001, Status: 2, CreatedAt: 1700000000}
	dto := OrderDTO{OrderID: "A-1001", Amount: 280}
	vo := OrderVO{Title: "订单 A-1001", AmountText: "￥280.00"}
	for _, v := range []any{do, dto, vo} {
		b, err := json.Marshal(v)
		if err != nil {
			fmt.Println("序列化失败:", err)
			continue
		}
		fmt.Println(string(b))
	}
}
```

```text
{"id":1001,"status":2,"created_at":1700000000}
{"order_id":"A-1001","amount":280}
{"title":"订单 A-1001","amount_text":"￥280.00"}
```

第三块代码从入口再看一次：`cmd` 只负责装配，协议层认的是业务抽象，具体实现藏在 `internal` 里。

```go
// cmd/api/main.go 的等价示例：入口只做装配，协议层只认业务抽象
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

// Query 是业务层对外暴露的能力
type Query interface {
	Total(id string) (int, error)
}

// Handler 属于协议层，依赖的是业务抽象而非具体实现
type Handler struct{ q Query }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v, err := h.q.Total("A-1001")
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, "订单合计:", v)
}

// fakeQuery 是装配时注入的实现，真实项目里来自 internal/service
type fakeQuery struct{}

func (fakeQuery) Total(id string) (int, error) { return 280, nil }

func main() {
	// 唯一知道「谁实现谁」的地方：入口组装
	h := Handler{q: fakeQuery{}}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println("状态码:", rec.Code)
	fmt.Println("响应:", rec.Body.String())
}
```

```text
状态码: 200
响应: 订单合计: 280
```

<LineByLine title="逐行看依赖倒置" code="type Repo interface {\n    Get(id string) (string, error)\n}\ntype Service struct{ repo Repo }\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}" :notes='["业务层先声明自己需要的能力","用业务语言命名，不暴露存储细节","接口闭合，抽象归业务所有","服务只持接口字段，不认识具体类型","构造函数是唯一的注入点","按值返回即可，结构体很小","依赖方向到此为止，上层不再往下追"]' />

## 3. 深入一步

目录切法没有唯一答案，但两种切法的代价差别很大：

| 维度 | 按技术层分包 | 按领域分包 |
| --- | --- | --- |
| 目录形态 | handler、service、dao、model 横向铺开 | order、user、payment 纵向切分 |
| 改一个需求 | 每层都翻一遍，容易漏 | 多数改动落在一个目录内 |
| 新人定位 | 知道「在哪层」，不知道「归谁」 | 打开领域目录就看到全貌 |
| 适合 | 极小项目、一次性原型 | 有多个独立业务概念的长期项目 |

判断依据很朴素：**变更是否经常一起发生**。会一起改的东西放近，互不相关的东西隔开。Go 标准库本身就是按领域组织的，`net/http`、`encoding/json` 各自成包，而不是把所有的 `Reader` 都塞进一个 `reader/` 目录。

<Term word="internal" text="Go 的保留目录名，只有其父目录树内的包可以 import，用来在编译期隔离实现细节。" />

<Callout type="warn" title="易错点：过度设计" text="两三个接口的小服务照搬四层架构、预先建满 adapter / port / usecase 空目录，只会让新人迷路。目录数量应当随真实业务概念一起增长，而不是先造好壳等代码来填。" />

## 4. 工程实践

真实项目里最伤可维护性的，是业务层直接抓全局连接、抓全局配置：单测想替换依赖时无从下手，只能连真库。把依赖收进构造函数，改动成本立刻降下来。

<Compare title="依赖方向：倒灌与倒置" :panels='[{"label":"写法 A · 依赖倒灌","code":"func Total() int {\n    return globalDB.Query()\n}\n// 业务层直接抓全局连接，测试无法替换","note":"编译能过，但改数据库要动业务代码，单测必须连真库"},{"label":"写法 B · 依赖倒置","code":"type Repo interface {\n    Amount(id string) (int, error)\n}\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}","note":"业务层只认接口，实现与测试替身都从入口注入"}]' />

几个常见的反模式值得点名：把 `pkg` 当杂物间，什么工具都往里塞，等于没有边界；为了「复用」把只有一处用到的代码提成公共包，制造了不该有的依赖；`cmd` 里写业务逻辑，导致同一个服务想换一种启动方式都做不到。反过来，一个只有订单和支付两个概念的项目，目录控制在十个以内完全够用。

<Callout type="tip" title="一条可执行的检查" text="在 CI 里跑一遍包依赖图，若发现 internal 之外的包反向 import 了 internal，或者业务层 import 了 cmd，就说明依赖方向已经被破坏，应当当场修掉而不是记在文档里。" />

## 动手练一练

<Exercise title="把全局依赖改成构造注入" task="下面这个函数直接读包级变量 db。请把它改成通过构造函数接收一个接口，使得测试可以传入一个返回固定值的替身。\n\nvar db = realDB{}\nfunc Total(id string) int { return db.Amount(id) }" :hints='["先声明一个只有 Amount 方法的接口，放在使用方一侧","让结构体持有该接口字段，并写一个 New 函数","保留原函数签名的话，也可以把接口作为参数直接传入"]' solution="type Repo interface {\n    Amount(id string) int\n}\ntype Service struct{ repo Repo }\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}\nfunc (s Service) Total(id string) int {\n    return s.repo.Amount(id)\n}" explain="接口由使用者定义、实现由入口注入，这就是依赖倒置。测试里传一个固定值替身即可，无需真实数据库。" />

## 随堂检测

<Quiz no="50-1" q="关于 Go 的 internal 目录，下面哪项说法正确？" :options='["任何包都能 import，只是命名上表示内部","只有 internal 父目录以内的包可以 import 它","它会被编译器优化掉，不参与构建","只能存放 _test.go 文件"]' answer="1" explain="internal 是 Go 的保留目录名，编译器会拒绝来自父目录树之外的 import，因此能物理隔离实现细节。" />

<Predict no="50-2" q="把 postgres.go 放进 myshop/internal/order 后，外部模块 myshop/other 尝试 import 它，会发生什么？" :options='["编译通过，运行时才报错","编译直接失败并提示不允许使用内部包","go mod tidy 自动把文件挪出去","只在 go vet 时给出警告"]' answer="1" output="build myshop/other: use of internal package myshop/internal/order not allowed" explain="internal 的可见性由编译器在构建期强制，不需要等到运行时，也不会只降级为警告。" />

## 一页纸速记

<KnowledgeCard :points='["cmd 只做装配，api 放对外契约，internal 藏实现，pkg 放可安全复用的纯工具","依赖只能向下：业务层定义接口，实现从构造函数注入","DO 对存储、DTO 对传输、VO 对展示，别用一个结构体通吃","按领域分包让相关改动聚在一起，按层分包适合极小项目"]' :pitfalls='["照搬四层架构、预先建一堆空目录","业务层反向 import cmd 或抓全局连接","把 pkg 当杂物间，制造没有必要的公共依赖"]' summary="目录结构的价值不在于好看，而在于把「谁可以依赖谁」变成编译器能检查的约束。" />

## 小结与下一讲

- `cmd / api / internal / pkg` 各有明确职责，`internal` 用可见性把实现细节锁在内部。
- 依赖方向只能向下，接口归业务层所有，实现由入口装配时注入。
- DO / DTO / VO 划清数据边界；目录数量随真实业务概念增长，拒绝预先造壳。

[下一讲：代码规范、gofmt、vet 与 golangci-lint](/engineering/code-style)
