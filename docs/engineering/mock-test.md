---
title: 第 053 讲 · Mock：gomock / sqlmock / httptest
description: 用依赖倒置留出可替换的缝隙，掌握手写 fake、gomock 生成、sqlmock 与 httptest，并认清 mock 过度与 gomonkey 的边界
---

<StageHeader stage="L5 · 工程化与质量" level="★★★★☆" time="45 分钟" goal="隔离外部依赖，测真正的逻辑" prereq="第 052 讲 · 单元测试与表驱动测试" />

# 第 053 讲 · Mock：gomock / sqlmock / httptest

> 单测一旦连上真实数据库或真实第三方接口，就不再是单测了：它慢、偶发失败、还得准备一堆外部状态。Mock 不是「把依赖替换掉」这么一句口号，而是先问清楚——**哪些是系统边界，哪些是自己的逻辑**。这一讲把边界划出来，再比较几种替换手段，最后重点讲什么时候不该 mock。

## 这一讲你会得到什么
<CheckList id="goal-53" title="本讲目标" :items='["用依赖倒置把外部依赖变成可替换的接口缝","按场景在手写 fake、gomock、sqlmock、httptest 之间做选择","识别 mock 过度，理解 gomonkey 为何只适合遗留代码兜底"]' />

## 1. 先建立直觉

把业务服务想成一台机器：规则、计算、状态流转是机器内部的齿轮，数据库、HTTP 接口、当前时间是接在机器外面的水管。测试内部齿轮时，你完全可以让齿轮真实咬合，只把外面的水管换成一根可控的短管。所谓依赖倒置，就是让机器按「我需要什么形状的接口」来设计接头，而不是焊死在某根具体水管上。

<ConceptDiagram type="architecture-layer" title="依赖倒置：接口是替换缝隙" />

关键判断在于**接口由谁定义**：应该由使用方（业务层）声明「我需要 Get 方法」，而不是由数据库驱动反过来规定业务层长什么样。这样测试替身和真实实现是平级的两个插头，换谁都不动机器。

## 2. 语法与规则

第一块代码是最小也最被低估的方案：手写 fake。逻辑简单时，几行结构体比生成一整个 mock 文件更清楚。

```go
// user_test.go —— 单独一个文件即可 go test 通过
package user

import (
	"fmt"
	"testing"
)

// Repo 是业务层声明的接口：替换缝隙由使用方定义。
type Repo interface {
	NameByID(id int) (string, error)
}

// Service 只依赖接口，真实实现与测试替身都从构造函数进来。
type Service struct{ repo Repo }

func New(r Repo) Service { return Service{repo: r} }

func (s Service) Greet(id int) (string, error) {
	name, err := s.repo.NameByID(id)
	if err != nil {
		return "", fmt.Errorf("问候失败: %w", err)
	}
	return "你好, " + name, nil
}

// fakeRepo 是手写替身：只需固定返回值时，它最直观。
type fakeRepo struct {
	data map[int]string
	err  error
}

func (f fakeRepo) NameByID(id int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.data[id], nil
}

func TestGreet(t *testing.T) {
	cases := []struct {
		name string
		repo Repo
		want string
	}{
		{"正常", fakeRepo{data: map[int]string{1: "ada"}}, "你好, ada"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := New(c.repo).Greet(1)
			if err != nil {
				t.Fatalf("不应出错: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
```

<OutputBox title="go test -v 运行结果" output="=== RUN   TestGreet\n=== RUN   TestGreet/正常\n--- PASS: TestGreet (0.00s)\n    --- PASS: TestGreet/正常 (0.00s)\nPASS\nok      user    0.001s" note="接口在哪一层定义，决定了替换成本：定义在使用方，业务代码一行都不用改。" />

第二块用标准库 `httptest.NewRecorder` 测 Handler，不需要监听任何真实端口。

```go
// handler_test.go —— 用标准库 httptest 测 Handler，无需真实端口
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ping 是被测处理器（示例起见与测试同文件）
func Ping(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestPing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	rec := httptest.NewRecorder()
	Ping(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 got %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q", ct)
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Errorf("响应体 got %q", got)
	}
}
```

第三块反过来测「调用外部 API 的客户端」：`httptest.NewServer` 起一个临时服务，把它的 `Client()` 注入进去，连超时都由 context 控制。

```go
// client_test.go —— 外部 HTTP 依赖用 httptest.NewServer 顶替
package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// FetchName 是被测函数（与测试同文件，保证单文件可 go test）
func FetchName(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func TestFetchName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ada"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := FetchName(ctx, srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("不应出错: %v", err)
	}
	if got != "ada" {
		t.Errorf("got %q, want ada", got)
	}
}
```

<LineByLine title="逐行看替换缝隙" code="type Repo interface {\n    Get(id int) (string, error)\n}\ntype Service struct{ repo Repo }\nfunc New(r Repo) Service {\n    return Service{repo: r}\n}" :notes='["接口由使用方定义，这是可替换的缝隙","方法签名用业务语言，不暴露驱动细节","接口闭合","服务只持有接口字段","构造函数是唯一注入点","按值返回即可","依赖方向不再往下延伸"]' />

### 四种替换手段为什么各有边界

这些工具不是同一件事的四种实现，而是按「被测对象站在边界的哪一侧」分工的，背后都能从 Go 的语言能力推出来。

手写 fake 是基线：接口由使用方声明之后，替身只是该接口的另一个实现，编译期类型检查保证它满足接口，不需要任何框架或运行时魔法。`httptest.NewRecorder` 与 `httptest.NewServer` 的分工最容易被记反：`http.ResponseWriter` 本身是接口，测 Handler 时根本不需要 TCP 连接，Recorder 是它的内存实现，记录状态码、头与响应体；但当被测代码是「客户端」时，要覆盖的是真实的 `http.Client`、超时、重定向与序列化，Recorder 模拟不了网络行为，于是 NewServer 在本机随机端口起一个临时服务，把它的 `Client()` 注入进去。一句话——**测服务端用 Recorder，测客户端用 NewServer**。

`gomock` 为什么必须代码生成：Go 没有 Java 那种运行时动态代理，也无法用反射临时造出一个接口的实现；要校验「调用了几次、参数是什么」，只能预先为每个接口生成一个记录调用的 Mock 类型。这决定了它走 `go generate`、生成文件要入库，也决定了接口一改、生成物就会漂移。`sqlmock` 替换的是 `database/sql` 的驱动层，按预设返回行，它没有查询引擎，因此「SQL 在真实库上是否合法」「事务隔离」「锁行为」一概测不到。`gomonkey` 则直接改写函数的机器指令，绕过类型系统，代价是必须用 `-gcflags=-l` 关内联、与 `-race` 冲突、对编译器版本敏感——只在「代码不能改」时才值回这个代价。

| 手段 | 隔离到哪一层 | 起真依赖吗 | 测不到的东西 | 代价 |
| --- | --- | --- | --- | --- |
| 连真库 / 真实服务 | 无 | 是 | 无 | 慢、要外部状态、CI 不稳定 |
| 手写 fake | 自定义接口 | 否 | 真实实现自身的 bug | 最低，几行结构体 |
| httptest.NewRecorder | HTTP 服务端 | 否 | 真实网络、超时、TLS | 标准库，零依赖 |
| httptest.NewServer | HTTP 客户端 | 起本机临时端口 | 公网抖动 | 标准库，零依赖 |
| gomock | 接口调用 | 否 | 实现内部的逻辑 | 需生成并入库 |
| sqlmock | database/sql 驱动 | 否 | SQL 在真库是否合法、事务、锁 | 第三方依赖 |
| gomonkey | 任意函数指令 | 否 | 无 | 关内联、与竞态检测冲突、版本敏感 |

### 可验证实验：生成物有没有漂移

`gomock` 的隐藏成本不在写测试，而在「接口改了、生成文件忘了重跑」。下面三条命令把它变成 CI 里可判定的断言：

```bash
go install go.uber.org/mock/mockgen@v0.4.0
go generate ./...
git diff --exit-code -- '*_mock.go'
```

```text
（无输出，退出码 0：生成物与接口一致）
```

逐字段看：`go install ...@v0.4.0` 把 mockgen 钉死在固定版本，因为不同版本生成的代码风格（注释、方法顺序、辅助类型）会变，浮动版本会让 `git diff` 在无人改动时自己抖动；`go generate ./...` 按源码里的 `//go:generate` 指令重跑生成；`git diff --exit-code` 一旦发现重跑结果与仓库里提交的文件不一致，就以非零码退出。于是「接口签名改了却没重新生成」不再表现为运行时 panic，而是当场阻断合并。反过来，若平时就开着 `-race`，也能顺手确认 gomonkey 不适合进常规门禁：

```bash
go test -gcflags=-l ./...   # gomonkey 必须关内联才能打桩
go test -race -count=1 ./... # 常规门禁：与竞态检测一起跑，故不走指令改写这条路
```

## 3. 深入一步

生成式与专用 mock 有自己的位置。`gomock` 适合校验「被调用了几次、参数是什么」，`sqlmock` 适合只验证 SQL 与扫描逻辑而不起真库：

```bash
go install go.uber.org/mock/mockgen@latest
# 接口文件里写一行：//go:generate mockgen -source=repo.go -destination=mock_repo.go -package=user
go generate ./...
```

<CodeTabs title="三种替换手段的写法对照" :tabs='[{"label":"gomock","code":"ctrl := gomock.NewController(t)\ndefer ctrl.Finish()\nm := NewMockRepo(ctrl)\nm.EXPECT().Get(1).Return(name, nil)","note":"校验调用参数与次数，需先用 mockgen 生成 Mock 类型"},{"label":"sqlmock","code":"db, mock, err := sqlmock.New()\nmock.ExpectQuery(...).WillReturnRows(...)\nrepo := New(db)","note":"只验证 SQL 文本与扫描，不起真实数据库"},{"label":"gomonkey","code":"patches := gomonkey.ApplyFunc(time.Now, ...)\ndefer patches.Reset()\n// 必须 go test -gcflags=-l","note":"运行时改写函数指令，新项目不推荐"}]' />

| 方案 | 隔离对象 | 成本 | 适用与边界 |
| --- | --- | --- | --- |
| 手写 fake | 自己定义的接口 | 最低，几行结构体 | 只需固定返回值或错误 |
| gomock | 接口，代码生成 | 需 mockgen 与 go generate | 需要校验调用次数与参数 |
| sqlmock | database/sql 驱动 | 需引第三方包 | 只想验证 SQL 与扫描 |
| httptest | HTTP 边界 | 标准库，零依赖 | Handler 与外部 API 客户端 |
| gomonkey | 任意函数 | 需关内联、非并发安全 | 无法改动的遗留代码兜底 |

从成本看，接口缝一旦开好，手写 fake 与生成 mock 可以混用：大多数用例用几行 fake 固定返回值，只有少数需要验证「是否调用、调用几次」的用例才动用 gomock。把生成代码当成默认方案，反而会让简单用例背上不必要的模板负担。

<Term word="测试替身" text="在测试中顶替真实依赖的对象统称，包含 fake、stub、mock、spy；mock 特指能校验交互行为的那一类。" />

<Callout type="warn" title="易错点：gomonkey 的边界" text="gomonkey 靠运行时改写函数指令打桩，必须关闭内联（go test -gcflags=-l），对 Go 版本与编译器优化敏感，且并发下不安全。它只该用于「代码不能改」的遗留场景；新代码优先用接口加构造注入。" />

### 为什么 Go 的 mock 只能代码生成：从 itab 静态绑定到内联消除

「Go 没有运行时动态代理」不是一句笼统的对比，可以追到接口值的表示。一个非空接口值在运行时是 `(tab *itab, data unsafe.Pointer)`：`itab` 由「具体类型 + 接口类型」这一组合在编译/链接期静态生成，里面的 `itab.fun` 是填好的方法表；`r.Get(1)` 编译出来就是「从 itab 取出函数指针再间接 CALL」。方法表是编译期产物，运行时没有「给一个接口临时造一个实现」的通路——`reflect` 能构造某个已知类型的值，却不能让一个类型在运行时新增方法。Java 的 `java.lang.reflect.Proxy` 靠运行时生成字节码并加载类，Go 没有等价的类加载与代码改写机制。

于是 gomock 只能走**代码生成**：mockgen 解析接口定义，生成一个实现了该接口的具体结构体（每个方法都转成一次 `ctrl.Call` 记录），编译期就把它放进 itab 的方法表。要校验「调用了几次、参数是什么」，靠的正是这个预先生成的类型。这从机制上解释了上一节那三条结论：生成物必须入库、接口一改生成物必然漂移、以及为什么 CI 要用 `git diff --exit-code` 把它变成门禁——静态绑定下，「接口变了而实现没跟着变」本来就是编译不过或行为不一致，不是工具偷懒。

`gomonkey` 是唯一绕过类型系统的例外：它在运行时改写目标函数的机器指令（覆盖函数入口的跳转）。但编译器默认会内联小函数，调用点被展开成函数体后就**没有可改的函数入口**了，所以必须 `-gcflags=-l` 关内联；改写的是代码段（`.text`），因此与 `-race` 冲突、并发下不安全、对编译器版本敏感。

可复现实验（Go 1.23.8 实测），用编译器的内联/去虚拟化诊断把这两条机制一起看出来：

```go
package main

import "fmt"

type Repo interface{ Get(id int) string }

type mem struct{}

func (m mem) Get(id int) string { return "ada" }

func call(r Repo) string { return r.Get(1) }

func plain() string { return "fixed" }

func main() {
	fmt.Println(call(mem{}))
	fmt.Println(plain())
}
```

```bash
go build -gcflags=-m main.go
```

```text
./main.go:9:6:  can inline mem.Get
./main.go:11:6: can inline call
./main.go:13:6: can inline plain
./main.go:16:18: inlining call to call
./main.go:16:18: devirtualizing r.Get to mem
./main.go:16:18: inlining call to mem.Get
./main.go:17:19: inlining call to plain
```

逐字段看：`can inline ...` 是编译器把三个小函数标记为可内联；`devirtualizing r.Get to mem` 说明当实参动态类型确定为 `mem` 时，编译器把原本经 itab 的间接调用**去虚拟化**成直接调用，紧接着 `inlining call to mem.Get` 又把函数体展开进调用点；`inlining call to plain` 同理。关键结论在这两行：调用点一旦被内联，改写 `mem.Get` 或 `plain` 的机器指令对这里已经无效——这正是 gomonkey 必须 `-gcflags=-l` 的实现层原因。而 `-l` 只是不内联，一般 `r Repo` 参数仍走 itab 间接跳转，方法表依旧是编译期静态生成的，运行时代理仍然造不出来。

**从机制推出实践结论**：手写 fake 与 mockgen 生成物都是「编译期就满足接口」的另一实现，由类型检查兜底，不需要运行时魔法；因此在 gomock、sqlmock、httptest 之间做选择时，判断依据仍是「被测对象站在边界哪一侧」，而不是「哪个库更像动态代理」。可证伪的对照：若真有运行时动态代理，接口改了 mock 应该自动适配、生成物不该漂移——但实测里接口一改，`go generate` 前后 `git diff` 必然不同，反证了静态绑定。

**边界与失效条件**：去虚拟化只在编译器能证明动态类型时发生（如本例直接传 `mem{}`），普通接口参数仍是间接跳转，但这不代表运行时可替换；`reflect.MakeFunc` 能造函数值，造不出实现新接口的类型；`-gcflags=-l` 会让基准变慢、也不能与 `-race` 一起进常规门禁，所以 gomonkey 只能用于「代码不能改」的遗留场景。这套机制能证明「替身是编译期合法的另一实现」，证明不了「替身行为与真实依赖一致」（那是误解二与集成测试的范围）。

**版本前提**：本实验在 Go 1.23.8（darwin/arm64）实测，mockgen 以 `go.uber.org/mock/mockgen@v0.4.0` 为准。去虚拟化与内联预算随编译器版本变化（见第 036 讲），`-gcflags=-m` 的具体行号与是否去虚拟化会变，但「接口方法表编译期生成」这一结论自 Go 1.0 起稳定。

### 从 itab 的两个字到机器码桩：为什么打桩既有接口约束、又绑死平台

前一节把「Go 的 mock 只能代码生成」追到了 itab 静态绑定与去虚拟化，这里再往下两层：接口值在内存里到底是哪两个字，以及 gomonkey 到底往函数入口写了哪些字节。前者解释 mock 的**类型约束**，后者解释它的**平台风险**，两者是同一个「编译期静态」事实的两个侧面。

先看布局。`src/runtime/runtime2.go` 里非空接口是 `type iface struct { tab *itab; data unsafe.Pointer }`（约 205 行），空接口是 `eface { _type *_type; data unsafe.Pointer }`。也就是说一个接口值是**恰好两个机器字**：第一个字指向元数据（itab 或 _type），第二个字指向数据。itab 本身（`runtime/type.go` 附近）含 `Inter *interfacetype`、`Type *_type`、`Hash uint32`、`Fun [1]uintptr`——`Fun` 是方法表的首元素，实际按方法数向后延伸。用 `unsafe` 可以把这两个字拆出来看：

```go
package main

import (
	"fmt"
	"unsafe"
)

type Getter interface{ Get() string }

type mem struct{ v string }

func (m mem) Get() string { return m.v }

func main() {
	var g Getter = mem{v: "ada"}
	x := (*[2]unsafe.Pointer)(unsafe.Pointer(&g))
	fmt.Printf("tab=%p data=%p\n", x[0], x[1])

	// 同一个具体值塞进另一个不同接口：data 指向同一份数据，tab 变了
	var g2 interface{ Get() string } = mem{v: "ada"}
	y := (*[2]unsafe.Pointer)(unsafe.Pointer(&g2))
	fmt.Printf("另一个接口的 tab=%p\n", y[0])
	fmt.Println("两个接口 tab 相同吗:", x[0] == y[0])
}
```

```bash
go run .
```

```text
tab=0x1022292e8 data=0x14000010070
另一个接口的 tab=0x102229308
两个接口 tab 相同吗: false
```

逐字段：`tab=0x1022292e8` 落在只读数据段（低地址、与代码段相邻），`data=0x14000010070` 落在堆上（darwin/arm64 的 `0x14...` 段），正对应「元数据静态生成、数据运行时分配」。同一个 `mem` 值塞进两个方法集相同但**接口类型不同**的 `Getter` 与匿名接口，`data` 仍指向各自装箱的数据，而 `tab` 不同（`...2e8` 与 `...308`，相差 0x20 恰好一个 itab 条目大小）——这从可观察输出证明 itab 是「接口类型 × 具体类型」这一**组合**的产物，不是类型自带的。

由此推出 mock 的一条硬约束：替换实现时，`data` 字可以指向任意新对象，但 `tab` 字必须指向一张「为新具体类型填好 `Fun` 方法表」的 itab。运行期没有通路去改这张表或临时造一个新类型实现接口，所以替身必须是**编译期就存在的另一个具体类型**（手写 fake 或 mockgen 生成物），由链接器把它的 itab 一起放好。呼应第 030 讲的 itab / getitab：那讲讲缓存命中，这里讲这个字为什么不能运行时换。可证伪的对照：用 `reflect.MakeFunc` 能造出一个函数值，却造不出实现某接口的新类型；把上面的 `g` 强改 `x[0]` 指向别处，下一次 `g.Get()` 取到的 `Fun[0]` 是垃圾指针，直接崩溃——表里必须是编译器填好的合法项。

gomonkey 是唯一绕过这层的例外，而它绕的正是「函数入口的机器码」。读 `github.com/agiledragon/gomonkey/v2@v2.3.1`（本机模块缓存内版本）的 `patch.go`：`ApplyCore` 拿到目标函数地址 `assTarget` 后调 `replace(assTarget, double)`，后者做三件事——`buildJmpDirective(double)` 生成一段跳转指令字节、`entryAddress(target, len(code))` 用 `reflect.SliceHeader{Data: p, Len: l, Cap: l}` 把**代码地址直接当成 `[]byte` 切片**、`copy(original, bytes)` 备份原字节再 `modifyBinary` 覆写。`modify_binary_darwin.go` 里 `modifyBinary` 先用 `syscall.Mprotect(page, PROT_READ|PROT_WRITE|PROT_EXEC)` 把代码页临时改成可写，写完再改回 `PROT_READ|PROT_EXEC`——`Reset()` 则用 `originals` 里那份备份还原。

要害在 `buildJmpDirective` 是**按 GOARCH 分文件**的，跳转桩字节完全不同：

| 平台 | 文件 | 跳转桩字节 | 长度 |
| --- | --- | --- | --- |
| amd64 | `jmp_amd64.go` | `48 BA` + 8 字节立即数（`MOV rdx, imm64`）、`FF 22`（`JMP [rdx]`） | 12 字节 |
| arm64 | `jmp_arm64.go` | 4 条 `MOVZ/MOVK x26`（拼 64 位地址）+ `4A 03 40 F9`（`LDR x10, [x26]`）+ `40 01 1F D6`（`BR x10`） | 24 字节 |

逐字段：amd64 用 10 字节把替身函数地址装进 `rdx`、再用 2 字节间接跳；arm64 没有「把 64 位立即数一次装进寄存器」的指令，得用 4 条 `MOVZ/MOVK` 分 16 位拼出地址（16 字节），再 `LDR` 取出真正的函数值指针、`BR` 跳过去，共 24 字节。两个数字直接给出可证伪的风险结论：

1. **桩长覆盖的函数入口指令数不同**。用 `go tool objdump -s` 看一个关了内联的小函数（Go 1.23.8，darwin/arm64，`go build -gcflags=-l`）：

```text
TEXT main.target(SB) main.go:15
  main.go:15  0x10009b180  90000000  ADRP 0(PC), R0
  main.go:15  0x10009b184  911dbc00  ADD $1903, R0, R0
  main.go:15  0x10009b188  d28000a1  MOVD $5, R1
  main.go:15  0x10009b18c  d65f03c0  RET
```

逐字段：4 条 arm64 指令、每条 4 字节、共 16 字节。而 arm64 的跳转桩要 24 字节——**覆写 24 字节会越过这 16 字节的函数体，踩到下一个函数**。所以打桩要求目标函数体不短于桩长，短函数要么被编译器插入别的东西、要么覆写越界；amd64 只需 12 字节，同一段代码在 amd64 能打、在 arm64 可能越界，行为随平台漂移。这是「对编译器与 GOARCH 敏感」最具体的机制层原因，比一句「平台相关」可验证得多。

2. **内联会让入口消失**。前一节用 `-gcflags=-m` 看到了去虚拟化与内联：一旦调用点被内联成函数体，就没有独立的 `TEXT main.target` 入口可供 `entryAddress` 定位，覆写也就无处下手——这正是 gomonkey 必须 `go test -gcflags=-l` 的字节层解释。

3. **与 `-race` 冲突**。竞态检测会在每次内存访问前后插桩、改变函数序言的指令序列；gomonkey 用固定字节备份 + 覆写，插桩后的序言长度与内容都不同，备份 / 还原按「原样字节」进行就会错位。加上覆写的是进程全局的 `.text` 段、而非某个接口值，多个 goroutine 同时跑被打了桩的函数谁都能看见，因此它不是并发安全的。

从机制推出取舍就很清楚了：手写 fake 与 mockgen 生成物动的是 `data` 字指向的对象、由编译器保证 `tab` 合法，跨平台、可与 `-race` 共存；gomonkey 动的是代码段字节、依赖具体 GOARCH 的跳转桩与函数序言布局，只适合「代码不能改」的遗留场景。可证伪对照：把上面那段打桩程序分别以 `-gcflags=-l` 与默认编译跑，只有关内联的那次桩生效；把 `GOARCH` 从 arm64 换成 amd64，跳转桩从 24 字节变 12 字节，同一目标函数的可打桩性随之改变。

**边界与失效条件**：`unsafe` 拆接口值依赖 `iface`/`eface` 的两字布局，Go 1.23.8 稳定但它属于 runtime 内部实现，不写入语言规范，跨大版本不能当 API 依赖；`Mprotect` 改整页权限，代码页通常只读且开启 W^X，苹果 silicon 等硬化的平台上还要额外处理代码签名 / 执行权限，`modify_binary_darwin.go` 的裸 `Mprotect` 在开启硬化运行时可能直接失败；`buildJmpDirective` 的具体字节随 gomonkey 版本与 Go 汇编 ABI 变化，`entryAddress` 用 `reflect.SliceHeader` 构造切片本身也依赖未导出的内存布局。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测，gomonkey 以 `github.com/agiledragon/gomonkey/v2@v2.3.1` 为准（本机模块缓存内版本），`mockgen` 以 `go.uber.org/mock@v0.4.0` 为准；`objdump` 的助记符与地址随 GOARCH、Go 版本变化，amd64 的桩字节取自 `jmp_amd64.go` 源码而非本机实跑。第 030 讲展开 itab 的哈希与缓存，本讲前面讲静态绑定与去虚拟化，这里补的是「接口值两个字的可观察布局」与「打桩覆写的真实字节长度」；第 049 讲的 `-race` 插桩正是这里冲突的来源。

### 三条最常见的误解

**误解一：mock 越多，测试越「单元」、越隔离。** 反例：把内部纯函数、工具方法甚至被测对象自己的方法都打上桩，测试的断言实际在验证「EXPECT 配置得对不对」，一重构内部实现就大面积报红，却从没抓到过一个真实 bug；更糟的是 `EXPECT()` 把调用次数与顺序写死进测试，等于把实现细节固化成契约。正确做法：只在系统边界（网络、数据库、时间、随机数、第三方 SDK）替换，内部逻辑一律真跑——判断口诀是「换掉边界之后，从入参到出参的计算路径是否仍被真实执行」。

**误解二：sqlmock / httptest 测过，就等于集成没问题。** 反例：sqlmock 用 `ExpectQuery("SELECT")` 匹配到一条语法看似正常、列名却写错的 SQL，测试通过，上线后真实库直接报「列不存在」；httptest 同理，它保证的是 Handler 的输入输出契约，覆盖不到真实网络下的超时、重试、TLS 与 gzip。这些工具占测试金字塔的中层，上面必须有少量连真库、连真服务的集成测试兜底，否则「全绿」只是边界之内的绿。

**误解三：接口越大越省事，一个 Repo 包办所有方法。** 反例：一个塞了二十个方法的 `Repo`，让手写 fake 被迫实现一堆用不到的空方法，gomock 生成的 Mock 也臃肿不堪，测试用例反而被接口污染。正确做法是按用例需要声明一两个方法的小接口（第 019 讲的小接口原则），替身保持几行，改动接口时受影响的测试也最少。

## 4. 工程实践

最常见的反模式是 **mock 过度**：把内部纯函数、工具方法、甚至被测对象自己的方法全打上桩，结果测试变成「验证 mock 的配置是否正确」，一重构就大面积报红，却从没抓到过真实 bug。判断标准很简单——只在系统边界替换：网络、数据库、时间、随机数、第三方 SDK；内部逻辑一律真跑。

接口的粒度也值得推敲：一个 `Repo` 若塞了二十个方法，替身就被迫实现一堆用不到的空方法，测试用例反而被接口污染拖累。按用例需要声明一两个方法的小接口，让替身保持几行代码，这是 Go 小接口原则在测试里的直接收益。

<Compare title="mock 过度与克制" :panels='[{"label":"写法 A · 全打桩","code":"// 反例：连纯函数都打桩\nctrl.EXPECT().Add(1, 2).Return(3)\n// 测试只验证了 mock 配置本身","note":"重构内部实现时测试全红，却测不到任何真实行为"},{"label":"写法 B · 只换边界","code":"// 正例：只替换真正的外部边界\nsrv := httptest.NewServer(...)\ndefer srv.Close()\n// 内部逻辑照常真实执行","note":"内部齿轮真实咬合，测试才真正保护业务逻辑"}]' />

另一个取舍是：`sqlmock` 能测出「SQL 写错了」，但测不出「SQL 在真实库上跑不通」，所以它不能替代集成测试；`httptest` 同理，它保证的是 Handler 的输入输出契约，不是真实网络下的行为。把这些工具摆在测试金字塔的合适层位，比多装几个库重要得多。

<Callout type="tip" title="一条可执行的检查" text="写测试前先问：这个依赖在 CI 里能不能稳定、免费、毫秒级地跑？答案是否，才考虑替换；答案是是，就让它真跑。" />

### 一次真实故障：mock 边界画错，测试全绿线上却报错

现象：订单服务的单测全部通过，上线后调用支付网关却持续报解析失败。排查路径：

1. 看线上日志，错误来自支付客户端的反序列化，不是网络超时，说明请求确实发出去、响应也回来了；
2. 回看测试，发现 `httptest.NewServer` 永远只返回一段手写的成功响应，而真实网关在业务失败时 HTTP 状态仍是 200、靠响应体里的业务码表达失败；
3. 更关键的一步：测试连「HTTP 客户端」这个对象也整个替换成了直接返回结构体的桩，等于把序列化、状态码判定、错误码解析这段**自己的代码**一起 mock 掉了，边界从「网络」被画进了系统内部；
4. 根因由此清楚：被测函数里从字节到结果的计算路径没有真实执行，覆盖率虚高但保护为零；
5. 修复：保留 `httptest.NewServer` 作为网络边界，让它按表驱动回放多种真实响应体（成功、业务失败、非法 JSON、超时），客户端与解析逻辑全部真跑；再把真实失败样本补成第 052 讲用例表里的一行。

```text
测试用的固定响应：{"code":0}
真实网关业务失败：{"code":500,"msg":"余额不足"}（HTTP 状态仍为 200）
真实网关异常响应：非 JSON 正文，或延迟到客户端超时
```

为什么这条边界规则存在：mock 的目的是隔离「不可控的外部」，不是让被测代码少跑。一旦把内部逻辑也替换掉，测试验证的只是替身自己的返回值。替代做法与代价也摆在这里——mock 整个客户端最省事但失去保护，连真实支付沙箱最真实却要网络、配额且不稳定，折中方案是契约测试：用采集到的真实响应样本回放，既不让被测路径变短，也不依赖对方在线。

### 把 mock 检查接进 CI

```yaml
# .github/workflows/test.yml（节选：生成物一致性 + 单测）
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod
    cache: true
- name: 生成物与接口一致性
  run: |
    go install go.uber.org/mock/mockgen@v0.4.0
    go generate ./...
    git diff --exit-code
- name: 单测与竞态
  run: go test -race -count=1 ./...
```

版本锁定理由：mockgen 钉在 `v0.4.0`，否则不同版本生成的 Mock 代码风格不同，`git diff --exit-code` 会在没有任何人改动时误报；`go-version-file: go.mod` 让生成代码与编译用的 Go 版本一致，避免生成物在本地能编译、在 CI 却不兼容；生成文件必须提交进仓库，CI 才有「重跑生成再比 diff」的基准——这正是第 060 讲 `go generate` 与制品校验的同一条思路。`-race` 常开，也顺带把「用 gomonkey 打桩」这条路挡在常规门禁之外。

### 与 L10 实战工程结构的呼应

L10 里这些边界会反复出现：`/projects/kv-1-memory` 的 `Storage` 接口是手写 fake 的主场，几行替身就能覆盖命令解析；`/projects/kv-2-network` 的客户端 SDK 用 `httptest.NewServer` 回放协议帧，正是本讲事故里该守住的边界；`/projects/net-shortlink` 的外部跳转、`/projects/micro-ecommerce` 的支付回调，都需要按真实响应体（含业务失败码）做表驱动回放。再往前，第 050 讲的依赖可替换结构是这一切的前提，第 052 讲的表驱动用例承接这里补出的边界值，第 054 讲 fuzz 找到的畸形输入同样可以灌进 httptest 的响应体里。边界画对了，后面每一层质量手段才有意义。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-053-mock-test`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-053-mock-test)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="把真实调用改成接口注入" task="下面函数直接调用 http.Get，导致测试必须联网。请把它改成接收一个接口，使测试可以传入固定值替身。\nfunc Load(url string) (string, error) {\n    resp, err := http.Get(url)\n    ...\n}" :hints='["先声明一个只有 Get 方法的接口","让函数或结构体从参数接收该接口","测试里传一个返回固定字符串的替身"]' solution="type Fetcher interface {\n    Get(url string) (string, error)\n}\nfunc Use(f Fetcher, url string) (string, error) {\n    return f.Get(url)\n}" explain="把外部 HTTP 收敛成接口后，测试替身与真实实现平级；既不需要联网，也不需要 gomonkey 打桩。" />

## 随堂检测

<Quiz no="053-1" q="关于依赖倒置中「接口该由谁定义」，哪项最符合 Go 的实践？" :options='["由数据库驱动定义，业务层去实现","由使用方（业务层）按自己需要声明小接口","放在一个公共 types 包里统一定义","用空接口 any 最灵活"]' answer="1" explain="接口由使用方声明，才能做到替换实现不动业务代码；小接口也更容易用 fake 顶替。" />

<Predict no="053-2" q="用 httptest.NewRecorder() 承接一个先 WriteHeader(http.StatusOK) 的 Handler，调用结束后 rec.Code 是多少？" :options='["0","200","500","-1"]' answer="1" output="200" explain="NewRecorder 默认 Code 为 200，显式 WriteHeader 之后保持该值；它只是内存里的 ResponseWriter 实现。" />

## 一页纸速记

<KnowledgeCard :points='["接口由使用方定义、实现由构造函数注入，是 mock 的前提","逻辑简单用手写 fake，需校验交互用 gomock","sqlmock 测 SQL 文本，httptest 测 HTTP 边界，都是标准边界替换","只在网络、数据库、时间、第三方这些边界替换"]' :pitfalls='["mock 过度：把内部纯函数也打桩，测试只验证 mock 配置","用 gomonkey 打桩新代码，忘了关内联或与 -race 冲突","把 sqlmock / httptest 当成集成测试的替代品"]' summary="Mock 的价值是隔离边界、保护真实逻辑，而不是把整台机器都换成假的。" />

## 小结与下一讲

- 依赖倒置留出接口缝，替换实现不动业务代码，这是可测试性的根。
- 手写 fake 最轻，gomock 校验交互，sqlmock 与 httptest 各守一类边界。
- 反模式是 mock 过度；gomonkey 只是遗留代码的兜底，不作为常规手段。

[下一讲：基准测试、模糊测试与覆盖率](/engineering/bench-fuzz)
