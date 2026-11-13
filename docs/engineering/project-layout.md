---
title: 第 050 讲 · 项目结构与分层设计
description: 用 cmd / internal / pkg / api 搭出依赖方向清晰的骨架，分清按领域还是按层，划清 DO / DTO / VO 并克制过度设计
---

<StageHeader stage="L5 · 工程化与质量" level="★★★☆☆" time="40 分钟" goal="搭出能被团队接手的项目骨架" prereq="第 049 讲 · 数据竞争、内存模型与 race detector" />

# 第 050 讲 · 项目结构与分层设计

> 一个人写三天的脚本，全部塞进 main.go 毫无问题；可一旦要两个人协作、要半年后回来改，目录怎么切就决定了「改一处会不会牵连十处」。项目结构不是审美偏好，而是一份关于「谁可以依赖谁」的团队约定。这一讲把这份约定落成一棵新人一眼能看懂的目录树，并把判断标准定在可维护、可测试上，而不是目录看起来够不够「专业」。

## 这一讲你会得到什么
<CheckList id="goal-50" title="本讲目标" :items='["说清 cmd / internal / pkg / api 各自该放什么，以及 internal 为何能挡住误用","在按领域分包与按技术层分包之间做出有依据的取舍","用 DO / DTO / VO 划清数据边界，并识别过度设计"]' />

## 1. 先建立直觉

把项目想成一栋楼：`cmd` 是临街的大门，只负责把客人迎进来、把水电接通；`api` 与 handler 是前台，负责听懂外面说什么、把话翻译给里面；service 和 domain 是真正干活的房间，装着业务规则；数据库、缓存、第三方客户端这些实现细节，被锁在 `internal` 的地下室里。楼里有一条硬规矩：**信息可以向下传，依赖只能向下指**。前台可以叫业务层干活，业务层绝不能反过来 import 前台，否则前台一改，业务层全得跟着重新编译。

<ConceptDiagram type="layout" title="分层骨架：依赖只能向下" />

Go 用两个语言级机制把这条规矩写死：包导入不允许成环；`internal` 目录具有特殊可见性，只有它的父目录以内的包才能 import 它。于是「实现细节」被物理地关在了内部，外部模块想偷懒直接捞数据库连接都做不到。这不是限制，是保护——它让「谁被允许改动」变成编译器能检查的事实，而不是一份没人看的文档。

### 为什么「依赖只能向下」是编译期事实，而不是风格建议

把这条规矩往深追一层，会发现它不由团队投票决定，而由 Go 的编译模型决定。Go 的编译单元是**包**而不是文件：编译器先读每个包的 import 声明，画出一张有向图，再对这张图做拓扑排序，决定谁先编译、谁后编译。一旦图里出现环，就没有任何一个包能被「第一个」编译出来，于是 `go build` 直接报 `import cycle not allowed`。所以「依赖只能向下」实际是在说：包依赖图必须是一张有向无环图。业务层若反向 import `cmd`，而 `cmd` 本来就 import 了业务层，环当场闭合，构建失败——这类越界在 Go 里往往不需要 lint 规则去抓，编译器先挡住了。

`internal` 是同一套思路的延伸。Go 没有 `public / private / protected` 关键字，包外可见性只靠标识符首字母大小写；但「哪些包之间允许互相看见」没有对应的关键字。`internal` 补上了这一层：语言规定 `DIR/internal/...` 下的包只能被 `DIR/...` 范围内的包 import，这条检查由 `cmd/go` 在解析 import 时完成，属于构建期强制，不是运行时判断，也不是 `go vet` 那种可以降级的警告。于是「实现细节不许外泄」从一份没人读的文档，变成了编译器能执行的事实。

| 做法 | 靠什么挡住越界 | 什么时候会失效 | 代价 |
| --- | --- | --- | --- |
| 文档约定 + 人工评审 | 人的记忆与责任心 | 新人接手、赶工期、评审疲劳 | 零工具成本，但几乎必然被逐步绕过 |
| 命名前缀（如 `private_`） | 无，纯约定 | 任何时候 | 一样会漂移，且包名越来越长 |
| `internal/` 目录 | 编译器解析 import | 仅约束同模块父树内；跨模块需另想办法 | 目录层级固定，搬动文件要连 import 一起改 |
| 拆成独立 module | module 边界 + 版本发布 | 依赖方用 `replace` 可绕过 | 每次改动都要发版，联调链路最长 |

真实事故里，最常见的起点是把某个数据库实现放进了 `pkg`——理由是「说不定别的服务能复用」。随后另一个服务真的直接 import 了它，绕开了本服务的业务校验层，对同一张订单表直写，订单状态被改坏且没有留下审计记录。排查路径往往是这样走的：先从异常数据反查写入方，用 `go list -deps` 把那个服务的依赖图展开，才看到一条指向本服务 `pkg` 的箭头；再比对本服务的领域层，确认这条写入根本没经过状态机。修复不是补校验，而是把实现挪进 `internal`，让外部模块在下一次构建时直接编译失败——问题被挡在源头，而不是靠人记得住。

下面这组命令可以自己验证依赖方向与 `internal` 边界。第一条把每个包「自己到直接依赖」的关系打印出来：

```bash
go list -f '{{.ImportPath}} → {{join .Imports ", "}}' ./...
```

```text
example.com/shop/cmd/api → example.com/shop/internal/order, example.com/shop/internal/server
example.com/shop/internal/server → example.com/shop/internal/order
example.com/shop/internal/order → (无项目内依赖)
example.com/shop/pkg/money → (无)
```

逐字段看：`-f` 后面跟一个 Go 模板，点号开头的字段在输出时会被替换成对应值，`join` 负责把依赖列表拼成一行；因此每一行描述的是「这个包直接 import 了谁」。检查两件事：一是 `internal/...` 有没有反向指回 `cmd/...` 或 `api/...`，二是有没有 A 指向 B 且 B 指向 A。想看整条传递链而不是直接依赖，改用 `go list -deps ./cmd/api`，它会把间接引入的包全部展开，是定位「某个内部包究竟怎么被牵进来」的常用手段。

<Callout type="tip" title="一个排查习惯" text="遇到「这个包怎么会被引进来」时，别急着全局搜索，先用 go list -deps 从目标包展开依赖树，再用管道过滤可疑路径，比逐个文件翻 import 快得多。" />

再故意破坏一次，看看编译器的反应：在 `internal/order` 里临时加一句对 `cmd/api` 的 import，然后构建。

```bash
go build ./...
```

```text
package example.com/shop/internal/order
    imports example.com/shop/cmd/api
    imports example.com/shop/internal/order: import cycle not allowed
```

报错里的 `imports` 链把整条环打印出来，最后一行的 `import cycle not allowed` 说明这是硬错误：不能靠注释关掉，也不能降级成警告，只能回去改结构。这正是把结构约束交给编译器的价值——它不会疲倦，也不会在周五下午放行。

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

### DO / DTO / VO 分开，究竟在防哪一类事故

前面第二块代码给了三种对象各司其职的写法，这里把「为什么不能用一个结构体通吃」的推导补完。同一个业务概念在三个环节的**变化原因**完全不同：存储对象跟着数据库 schema 走（加索引、加软删标记、改状态机），传输对象跟着接口契约走（字段名由 proto 或 OpenAPI 固定，改名就是破坏性变更），展示对象跟着 UI 走（文案、金额格式化、多包一个展示字段）。三种变化原因压在同一个类型上，任何一方要改都得动它，改完还得确认另外两方没被波及——这就是耦合的定义。

更危险的不是麻烦，而是泄露。看一段能直接跑的代码：

```go
package main

import (
	"encoding/json"
	"fmt"
)

// UserDO 是存储对象，密码哈希与内部状态码只应存在于数据层内部。
type UserDO struct {
	ID           int64  `json:"id"`
	PasswordHash string `json:"password_hash"`
	Status       int    `json:"status"`
}

// UserVO 是展示对象，只暴露前端确实需要的字段。
type UserVO struct {
	Name string `json:"name"`
}

// toVO 是数据离开业务层的唯一出口。
func toVO(u UserDO) UserVO {
	return UserVO{Name: fmt.Sprintf("用户%d", u.ID)}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "序列化失败: " + err.Error()
	}
	return string(b)
}

func main() {
	u := UserDO{ID: 1001, PasswordHash: "x9f3c2", Status: 2}
	fmt.Println("直接返回 DO:", mustJSON(u))
	fmt.Println("返回 VO:    ", mustJSON(toVO(u)))
}
```

```text
直接返回 DO: {"id":1001,"password_hash":"x9f3c2","status":2}
返回 VO:     {"name":"用户1001"}
```

逐字段解释：`PasswordHash` 与 `Status` 因为带着 json tag，被 `json.Marshal` 一并序列化。真实事故往往是这样发生的：某个查询接口为了赶进度，直接把 repository 返回的 DO 交给 `json.NewEncoder(w).Encode(do)`，函数签名上写的仍然是「返回用户」，代码评审看不出异常，但响应体里已经多了密码哈希和内部状态码；这类泄露在测试里也不会被抓，因为断言通常只检查「状态码 200 与 id 存在」。分成 VO 之后，`toVO` 成为唯一出口，敏感字段在 VO 里根本没有对应字段，物理上带不出去——把安全保证从「人记得转换」变成「类型里没有这个字段」。

| 对象 | 面向 | 字段由谁定义 | 变化原因 | 直接对外序列化的风险 |
| --- | --- | --- | --- | --- |
| DO | 存储 | 数据库 schema / ORM | 加索引、改状态机、加软删字段 | 泄露内部状态码、时间戳、密码哈希 |
| DTO | 服务间传输 | proto / OpenAPI 契约 | 上下游协商的接口演进 | 字段名变更即破坏性变更，需版本化 |
| VO | 前端展示 | UI 需求 | 文案、格式化、展示字段 | 一般无敏感信息，但会与 UI 强耦合 |

代价也要讲清楚：多一层结构体、多一次字段拷贝，字段在两处重复，改字段名要同步；当领域对象多到几十个时，手写转换本身会成为负担，这时会用代码生成批量产出转换函数（呼应第 060 讲的 `go generate`）。所以判断标准回到一句话：**变化原因不同才拆，变化原因相同就别拆**。只有一个消费者、字段完全一致的场景硬拆三层，就是本讲易错点点名的过度设计。这也是第 053 讲 mock 边界的前置条件：DTO 定义得越贴近契约，替换外部服务时就越不用改业务代码。

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

### 按领域还是按层：把取舍拆成可判断的条件

「会一起改的东西放近」这条经验背后有更硬的机制。包是 Go 最小的编译与复用单元，也是改动的波及半径：改一个包，所有 import 它的包都要重新编译、重新跑测试。按技术层横向分包时，「调整订单折扣」这件事的代码散落在 handler、service、dao、model 四个目录里，而 dao 目录里还混着 user、payment 的表，一次改动波及多个包，还容易漏掉某一处。按领域纵向切分后，同一业务概念的改动落在一个包内，编译波及面最小，新人打开 `order/` 就看到全貌。

还有一条与成环相关的推导。Go 不允许 import 成环，而按层分包极易制造回指需求：比如在 `model` 里想复用 `service` 的一段校验，或在 `dao` 里回调 `service` 的钩子，方向一乱就成环，最后只能再抽一层 `common` 来打散，目录越拆越碎。按领域分包把这类循环消化在领域包内部，跨领域之间只保留少量单向接口。目录树往往也是组织结构的镜像：两个团队共改同一个横向 `service/` 包，合并冲突率最高；按领域切能把归属落到目录上，再用第 060 讲会讲的 CODEOWNERS 把责任固定下来。

| 判断条件 | 倾向按层 | 倾向按领域 |
| --- | --- | --- |
| 独立业务概念数量 | 只有 1 个 | 3 个以上且各自演进 |
| 一次需求改动波及的目录数 | 1 到 2 个 | 按层会波及 4 个以上 |
| 团队人数与并行分支 | 单人、单分支 | 多人多分支，需要明确归属 |
| 是否出现跨层反向 import 的冲动 | 没有 | 已有，存在成环风险 |
| 项目寿命 | 一次性原型 | 长期维护、需要接手 |

一个容易忽略的版本背景：Go 1.11 引入 module 之前，GOPATH 时代所有包共享 `src` 下同一个命名空间，横向分层布局更流行；module 加上 `internal` 让「领域包 + 内部实现」的隔离成本大幅下降，才成为主流。这不是审美变迁，是语言提供的隔离能力变了。`internal` 规则本身从早期版本至今保持一致，不随版本漂移。

### 改一个包为什么下游全要重编：从 build action 的内容哈希看依赖方向

「改一个包，所有 import 它的包都要重新编译」在 `cmd/go` 里有具体实现。`go build` 不按文件时间戳判断要不要重编，而是给每个包的编译动作（build action）算一个内容哈希，作为 `GOCACHE` 的键；哈希里包含该包源码内容、编译参数，以及**所有直接依赖包的 buildid**。于是依赖图上任一处变化都会沿 import 边向下游传递：A 变了，依赖 A 的 B 的哈希也变，B 必须重编。依赖方向因此同时是缓存失效的传播方向——被十个包引用的公共包改一个字段，十一个 action 一起失效；叶子包改动只波及它自己。这给「依赖只能向下」补了机制层含义：把稳定抽象放在被依赖侧、把易变实现藏在 `internal`，改实现时波及面才小。反过来，若把易变的类型放进被广泛 import 的 `pkg`，每次改动都会把下游全部重编一遍，这就是「`pkg` 只放无业务状态的纯工具」的一条可证伪依据。

可复现实验（Go 1.23.8 实测），把增量重编变成可数的输出：

```bash
go clean -cache && go build -x ./...                       # 首次：观察 compile 动作
go build -x ./...                                          # 第二次：全部命中缓存
printf '\n// touch\n' >> calc.go && go build -x ./...      # 改一个包后再构建
```

```text
首次 compile/link 动作数: 1
第二次 compile/link 动作数: 0
第三次重新出现一条 compile（省略若干与结论无关的 flags）：
/Users/benji/.gvm/gos/go1.23.8/pkg/tool/darwin_arm64/compile -o $WORK/b001/_pkg_.a -p example.com/cov -buildid qJB2XWR1IweHDlnhxm7O/qJB2XWR1IweHDlnhxm7O -pack ./calc.go
```

逐字段看：`-x` 打印每一步实际执行的命令；首次出现一条 `compile`，第二次一条都没有，说明命中 `GOCACHE` 后连编译器都没启动；改了 `calc.go` 后，`-p example.com/cov` 这个包的 `-buildid` 变了，该包及其下游重新执行 `compile`。判断依据是内容哈希是否命中，而不是「文件比目标新」——第二次文件没改，自然也不重编。`-o $WORK/b001/_pkg_.a` 是临时工作目录里的归档产物，`-pack ./calc.go` 指明本次编译的输入文件，`-buildid` 正是哈希的可见形式。

**边界与失效条件**：`GOCACHE` 只缓存编译中间产物，不缓存测试结论（那是另一套 key，见第 052 讲 `-count=1`）；`-gcflags`、`-trimpath`、`CGO_ENABLED`、Go 版本任一变化都会换一套哈希，跨版本不复用；`go clean -cache` 只清编译缓存，不动 `GOMODCACHE` 里的依赖源码。它证明的是「哪些包被重新编译」，不能证明「改动是否语义正确」，后者仍要靠测试。

**版本前提**：内容寻址的 build cache 自 Go 1.10 起引入；Go 1.20 起的 `-buildvcs`、1.21 起的 `tool` 指令会往构建信息里加字段，都参与哈希输入。本实验在 Go 1.23.8（darwin/arm64）实测，输出里的 GOROOT 路径随本机安装位置变化。

### 依赖图坏掉时工具看见了什么：`go list -e -json` 的 `DepsErrors` 与跨模块 Module 图

推导起点：`cmd/go` 加载一个包时，先解析 import，把每个 import 路径映射到「哪个 module 提供它」，再做类型检查。前两节看的 `go list -deps`、`go build` 都是**这条路走通之后**的输出；一旦某个 import 找不到提供者，`cmd/go` 默认「遇错即停」——连依赖图都不给，只在 stderr 报一句就退出。这在 CI 里很致命：你想知道「整个仓库有多少包受影响」，却只拿到第一条错误。

`-e`（error-tolerant）改变的就是这一点：它不让加载器在第一个错误处返回，而是把错误**作为数据**塞进 `go list` 的输出结构。`go list -json` 打印的是 `cmd/go/internal/load.Package` 的 JSON 形式，与「依赖是否解析成功」相关的四个字段各有明确职责：

| 字段 | 类型 | 记录什么 |
| --- | --- | --- |
| `Incomplete` | bool | 本包或其依赖的加载是否不完整，是「这份输出不可信」的总开关 |
| `Error` | 对象或 null | **本包自身**的错误（语法错、包子句冲突等） |
| `DepsErrors` | 数组 | **依赖边**上的错误：某个 import 找不到 module / 找不到包 |
| `Deps` | 字符串数组 | 已成功解析出的传递依赖（含标准库），坏掉的那条路径也在里面 |

关键区分是 `Error` 与 `DepsErrors`：前者是「我自己坏了」，后者是「我引用的别人坏了」。这个区分不是文案差异，而是 `PackageError` 里 `ImportStack` 字段的存在理由——它记的是「从当前包出发、经过哪些中间包、最终走到坏包」的导入链，链越长说明坏依赖被埋得越深。

实验（Go 1.23.8，darwin/arm64，故意 import 一个不存在的包；为免联网干扰设 `GOPROXY=off`）：

```bash
go mod init example.com/e50
printf 'package main\nimport _ "example.com/nope/missing"\nfunc main(){}\n' > main.go

go list -json ./...                 # A：默认遇错即停
go list -e -json ./... | jq '{ImportPath,Incomplete,Error,DepsErrors,Deps}'
```

输出形态：

```text
A: go: finding module for package example.com/nope/missing
   （stderr 报错后退出，stdout 只有半截 JSON）

B:
{
  "ImportPath": "example.com/e50",
  "Incomplete": true,
  "Error": null,
  "DepsErrors": [
    {
      "ImportStack": ["example.com/e50"],
      "Pos": "main.go:3:8",
      "Err": "cannot find module providing package example.com/nope/missing: module lookup disabled by GOPROXY=off"
    }
  ],
  "Deps": ["example.com/nope/missing", "internal/abi", "...", "runtime", "unsafe"]
}
```

逐字段解释：`Incomplete: true` 是总开关，CI 判「依赖图是否可用」应当看它而不是看退出码；`Error: null` 说明本包源码合法，问题不在自己；`DepsErrors` 只有一个元素，`ImportStack` 长度为 1（只有一个 `example.com/e50`），说明坏的是**直接** import，若换成「A 引 B、B 引坏包」，`ImportStack` 会是三项，链长直接数出依赖深度；`Pos: main.go:3:8` 精确到列，是加载器记下 import 语句位置的结果；`Err` 末段的 `module lookup disabled by GOPROXY=off` 说明失败发生在「查 module」这一步，而不是编译或类型检查——换成联网环境这里会是 404 或 no matching versions，但字段结构不变。`Deps` 里既有坏包路径也有 `internal/abi`、`runtime` 等标准库，证明 `-e` 是「保留已解析部分 + 标注失败部分」，不是丢弃整张图。

由此得到一条可证伪的 CI 结论：**「`go list` 退出码为 0」不等于「依赖图完整」**。要判断一个包能否被可靠分析，判据是 `Incomplete == false` 且 `DepsErrors` 为空；只看退出码，会把「坏依赖被 `-e` 吞下」或「某平台标签下的包没编进来」漏过去。反过来，把 `DepsErrors` 当成本包错误去改源码，会一直在 `main.go` 里找并不存在的语法错——`Error: null` 已经把责任划给了依赖边。

把视角从「包」抬到「模块」，跨模块依赖图是另一条命令。包级 `go list -deps` 回答「某个包传递引用了谁」，模块级 `go list -m -json all` 回答「这次构建用到哪些 module、各自什么版本、有没有被 replace」：

```bash
go list -m -json all | jq -c '{Path,Version,Main,Indirect,GoVersion,Replace,Dir}'
```

在一个主模块 `example.com/e60b` 用 `replace example.com/lib => ./lib` 的仓库里，实测输出两行：

```text
{"Path":"example.com/e60b","Version":null,"Main":true,"Indirect":null,"GoVersion":"1.23","Replace":null,"Dir":"/…/proj"}
{"Path":"example.com/lib","Version":"v0.0.0","Main":null,"Indirect":null,"GoVersion":"1.23","Replace":{"Path":"./lib","Dir":"/…/proj/lib"},"Dir":"/…/proj/lib"}
```

逐字段：`Main: true` 标出主模块，它没有 `Version`（工作区里的代码没有版本号，构建信息里显示成 `(devel)`）；`Version: "v0.0.0"` 是被 require 的依赖；`Indirect: true` 表示该依赖不是被直接 import、而是被别的依赖带进来的（该字段省略时等于 false）；`GoVersion` 是各模块 `go.mod` 声明的语言版本，它决定编译器按哪套语义处理这个模块；`Replace` 非空时是个对象，`Path` 是 `=>` 右边的目标（`./lib` 是本地目录），`Dir` 是解析后的实际路径——**此时构建信息里的 `dep` 行记的是 replace 之前的 `example.com/lib v0.0.0`，不是 `./lib`**，这一错位正是「本地 replace 联调通过、发版后拉到公网版本行为不同」的根因；`go list -deps .` 里出现的是 `example.com/lib`（包路径），与 `Replace.Dir` 一起构成「模块边界 → 本地目录 → 包」的完整映射。

可证伪结论：判断「这次构建到底用了哪份代码」，必须同时看 `go.mod` 的 `replace` 与 `go list -m -json` 的 `Replace.Dir`；只信 `dep` 版本行会以为用的是 `v0.0.0`，实际用的是工作区里的 `./lib`。用 `go mod edit -dropreplace example.com/lib` 去掉 replace 后重跑，`Replace` 变 null、`Dir` 指向模块缓存，两份二进制的 `debug.ReadBuildInfo().Deps` 也随之不同——这条对照能在构建期就发现「联调态」与「发布态」的漂移，而不必等线上行为不一致。

**边界与失效条件**：`-e` 只改加载器的容错策略，不改变错误本身，`go build` 仍会失败；`DepsErrors` 只覆盖「加载 / 查 module」阶段，类型检查错误落在 `Error`，编译错误两者都不含；`go list -m -json all` 在 `go.work` workspace、`vendor/` 模式、或 `-mod=mod` 自动补依赖时字段会变（`Replace` 也可能被 workspace 的 `use` 覆盖）；`GOPROXY=off` 只影响 `Err` 的措辞，不影响字段结构。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测，`jq` 仅为格式化工具；`DepsErrors` / `Incomplete` 字段与 `go list -m -json` 的 Module 结构自 Go 1.11（modules）起稳定，`GoVersion` 字段自 1.12 起加入，VCS 版本信息自 1.18 起进构建信息。第 050 讲前面用 `go list -deps` 看「依赖方向」，这里补的是「依赖图解析失败时工具把错误记在哪」，以及包图与模块图的分层——第 051 讲 vet 的 facts 跨包传递正是沿这张包图按拓扑序传递的。

### 两条最常见的误解

**误解一：`internal` 里的代码别人用不了，所以要复用的都该放 `pkg`。** 反例：把带业务状态的订单缓存 `OrderCache` 放进 `pkg` 供两个服务复用，结果两个进程各持一份缓存、失效逻辑各写一遍，读到脏数据却谁都查不到源头。正确做法：`pkg` 只放无业务状态、语义稳定的纯函数与类型（金额计算、时间格式化这类），需要跨服务共享的是契约（放 `api/` 下的 proto 或 OpenAPI），不是实现；带状态的实现一律留在 `internal`，由各自入口装配。

**误解二：目录越多越专业，先把 adapter / port / usecase 建好等代码来填。** 反例：一个只有三个接口的小服务建了十几个空目录，新人找一个 handler 要跳五层，而空目录不提供任何编译约束，纯属导航负担。正确做法：Go 的包并不要求一层一目录，同一个包内放多个文件完全合法；先用文件分职责，等业务概念稳定、确实需要独立边界时再升格成包。

<Callout type="warn" title="别把「有目录」当成「有边界」" text="目录只是名字，真正的边界是 import 方向与 internal 的可见性。一个没有依赖约束的分层目录，和一个平铺目录在编译器眼里没有区别；先让依赖方向单向，再谈目录好不好看。" />

<Quiz q="一个只有订单、支付两个业务概念、由一人维护的小服务，哪种目录策略更合适？" :options='["先建 handler、service、dao、model 四层目录，方便以后扩展","按 order、payment 切领域包，实现放 internal，暂不预建空目录","全部塞进 pkg，方便别的仓库复用","建满 adapter、port、usecase 空目录，等代码来填"]' answer="1" explain="独立概念不止一个时按领域切分收益明显，但仍要克制：等概念稳定再升格成包，预先造壳正是本讲点名的过度设计。" />

## 4. 工程实践

真实项目里最伤可维护性的，是业务层直接抓全局连接、抓全局配置：单测想替换依赖时无从下手，只能连真库。把依赖收进构造函数，改动成本立刻降下来。

<Compare title="依赖方向：倒灌与倒置" :panels='[{"label":"写法 A · 依赖倒灌","code":"func Total() int {\n    return globalDB.Query()\n}\n// 业务层直接抓全局连接，测试无法替换","note":"编译能过，但改数据库要动业务代码，单测必须连真库"},{"label":"写法 B · 依赖倒置","code":"type Repo interface {\n    Amount(id string) (int, error)\n}\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}","note":"业务层只认接口，实现与测试替身都从入口注入"}]' />

几个常见的反模式值得点名：把 `pkg` 当杂物间，什么工具都往里塞，等于没有边界；为了「复用」把只有一处用到的代码提成公共包，制造了不该有的依赖；`cmd` 里写业务逻辑，导致同一个服务想换一种启动方式都做不到。反过来，一个只有订单和支付两个概念的项目，目录控制在十个以内完全够用。

<Callout type="tip" title="一条可执行的检查" text="在 CI 里跑一遍包依赖图，若发现 internal 之外的包反向 import 了 internal，或者业务层 import 了 cmd，就说明依赖方向已经被破坏，应当当场修掉而不是记在文档里。" />

### 一次真实故障：全局连接如何把单测连上了共享库

先看现象：某次 CI 上 `go test ./...` 偶发把订单表的测试数据清空，开发本地却从不复现。排查路径是这样的：

1. 通读测试代码，没有任何 drop 或 delete，于是怀疑生产代码在包初始化阶段就做了副作用；
2. 用 `grep -R 'func init' ./internal` 找到 `internal/order/postgres.go` 的 `init()` 里直接 `sql.Open`，默认 DSN 指向一个共享测试库；
3. 用 `go list -deps ./internal/order` 展开依赖，确认业务包直接 import 了数据库驱动——依赖方向已经倒灌，业务层认识了本该由入口决定的存储；
4. 比对环境变量，发现 CI 注入的配置与本地不同，默认值恰好落到那台共享库；本地因为连不上而走了另一条分支，所以从不复现；
5. 根因收敛到一句：依赖不是从入口注入的，测试无法替换，只能连上「恰好能连上」的那个库。

修复方式与第二块代码的装配完全一致：把 `sql.Open` 收进 `cmd/api/main.go`，`OrderService` 只持有 `OrderRepository` 接口，测试传 `memoryRepo` 替身。构造函数注入把「连谁」变成入口的一次决定，生产与测试跑同一套业务代码、换的只是实现；而 `init()` 里的全局副作用，编译期看不出来，运行期也要等踩中默认值才暴露，只能靠结构约束提前消除。

替代做法不是没有，但代价要算清：用构建标签隔离集成测试，只是把问题挪到另一个测试集，业务代码仍不可替换；在 `TestMain` 里改写全局变量，本质上还是全局可变状态，开 `go test -race` 并行跑包时多个测试会互相覆盖。构造注入是唯一让「替换」由类型系统保证的做法，这也是第 052 讲表驱动测试与第 053 讲 mock 能成立的前提——结构没搭对，后面所有质量手段都会打折。

### 把结构检查放进 CI

下面的工作流把「依赖方向」变成每次 PR 都会跑的门禁：

```yaml
name: layout
on: [pull_request]
jobs:
  deps:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: 成环与静态检查
        run: |
          go build ./...
          go vet ./...
      - name: internal 不得反向依赖入口层
        run: |
          go list -deps ./internal/... | grep -E '^example.com/shop/(cmd|api)/' && exit 1 || true
```

逐条解释：`go build ./...` 让任何新出现的 import 环当场失败；`go vet ./...` 顺带抓拷贝锁、格式化动词不匹配这类结构性问题；最后一条从 `internal` 出发展开依赖，若结果里出现 `cmd` 或 `api` 下的包，说明内部实现反向依赖了入口，grep 命中即让步骤失败。`go-version-file: go.mod` 是关键的版本锁定：CI 使用 `go.mod` 里声明的 Go 版本，而不是浮动的 `latest`，否则本地用 1.21、CI 用 1.23 时，逃逸分析结论、`go vet` 规则集甚至诊断输出都可能不同，CI 结论无法在本地复现。`actions/checkout@v4` 与 `setup-go@v5` 钉住大版本，避免上游 action 的破坏性更新让整条流水线某天集体失效——这正是第 060 讲会展开的版本漂移问题。

<OutputBox title="门禁失败时的输出" output="internal/order 反向依赖 cmd/api，步骤以非零码退出" note="这类检查把「谁可以依赖谁」变成每次 PR 都会执行的断言，而不是评审时的一句提醒。" />

### 与 L10 实战工程结构的呼应

这套骨架不是纸上约定，L10 会反复用到它：`/projects/kv-1-memory` 里由业务层定义 `Storage` 接口、内存实现放在内部，正是本讲「接口归业务、实现藏 internal」的最小样本；`/projects/micro-ecommerce` 把用户、商品、订单、库存各切为独立领域包，是按领域分包的规模化版本；`/projects/cli-todo` 则演示了一个极小项目如何用不到十个目录收场，不必上四层。再往后，第 051 讲的 lint 规则集、第 052 讲的表驱动测试、第 053 讲的 mock 边界，全都建立在「依赖可替换」这一结构前提上。看懂了依赖方向，才看得懂后面每一道工序为什么那样设计。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-050-project-layout`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-050-project-layout)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="把全局依赖改成构造注入" task="下面这个函数直接读包级变量 db。请把它改成通过构造函数接收一个接口，使得测试可以传入一个返回固定值的替身。\n\nvar db = realDB{}\nfunc Total(id string) int { return db.Amount(id) }" :hints='["先声明一个只有 Amount 方法的接口，放在使用方一侧","让结构体持有该接口字段，并写一个 New 函数","保留原函数签名的话，也可以把接口作为参数直接传入"]' solution="type Repo interface {\n    Amount(id string) int\n}\ntype Service struct{ repo Repo }\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}\nfunc (s Service) Total(id string) int {\n    return s.repo.Amount(id)\n}" explain="接口由使用者定义、实现由入口注入，这就是依赖倒置。测试里传一个固定值替身即可，无需真实数据库。" />

## 随堂检测

<Quiz no="050-1" q="关于 Go 的 internal 目录，下面哪项说法正确？" :options='["任何包都能 import，只是命名上表示内部","只有 internal 父目录以内的包可以 import 它","它会被编译器优化掉，不参与构建","只能存放 _test.go 文件"]' answer="1" explain="internal 是 Go 的保留目录名，编译器会拒绝来自父目录树之外的 import，因此能物理隔离实现细节。" />

<Predict no="050-2" q="把 postgres.go 放进 myshop/internal/order 后，外部模块 myshop/other 尝试 import 它，会发生什么？" :options='["编译通过，运行时才报错","编译直接失败并提示不允许使用内部包","go mod tidy 自动把文件挪出去","只在 go vet 时给出警告"]' answer="1" output="build myshop/other: use of internal package myshop/internal/order not allowed" explain="internal 的可见性由编译器在构建期强制，不需要等到运行时，也不会只降级为警告。" />

## 一页纸速记

<KnowledgeCard :points='["cmd 只做装配，api 放对外契约，internal 藏实现，pkg 放可安全复用的纯工具","依赖只能向下：业务层定义接口，实现从构造函数注入","DO 对存储、DTO 对传输、VO 对展示，别用一个结构体通吃","按领域分包让相关改动聚在一起，按层分包适合极小项目"]' :pitfalls='["照搬四层架构、预先建一堆空目录","业务层反向 import cmd 或抓全局连接","把 pkg 当杂物间，制造没有必要的公共依赖"]' summary="目录结构的价值不在于好看，而在于把「谁可以依赖谁」变成编译器能检查的约束。" />

## 小结与下一讲

- `cmd / api / internal / pkg` 各有明确职责，`internal` 用可见性把实现细节锁在内部。
- 依赖方向只能向下，接口归业务层所有，实现由入口装配时注入。
- DO / DTO / VO 划清数据边界；目录数量随真实业务概念增长，拒绝预先造壳。

[下一讲：代码规范、gofmt、vet 与 golangci-lint](/engineering/code-style)
