---
title: 语法速查表 · 一页查全 Go 常用写法
description: 变量与类型、控制流、切片与 map、结构体与接口、错误处理、并发、标准库与常用命令的速查表格与可编译示例。
---

# 语法速查表 · 一页查全 Go 常用写法

> 这一页不是教程，而是「手边字典」。写代码时想不起某个 API 或命令，回来扫一眼表格即可；每段示例代码都可以单独复制、编译、运行。想理解背后的为什么，顺着小节末尾的链接回到对应讲次。

<CheckList id="cheatsheet-usage" title="怎么用这份速查表" :items='["按左侧目录直接跳到要查的分类", "表格里的写法可以直接抄，示例代码可独立编译", "记不清原理时点站内链接回看完整讲次", "配合常见坑与反模式一页，检查自己有没有踩雷"]' />

<Callout type="tip" title="示例约定" text="下面所有 Go 代码块都以 package main 开头，import 完整、带中文注释，可以用 go run 直接跑通。示例统一使用 Go 1.21 及以上的语法。" />

## 1. 变量与类型

| 分类 | 写法 | 说明 |
| --- | --- | --- |
| 显式声明 | `var n int` | 未赋值时自动取零值 |
| 声明并赋值 | `var s string = "hi"` | 类型可省略，由右值推断 |
| 短声明 | `n := 10` | 只能在函数内，且至少要有一个新变量 |
| 多变量 | `a, b := 1, 2` | 一次声明多个变量 |
| 常量 | `const Pi = 3.14` | 编译期确定，运行期不可修改 |
| 枚举 | `const ( A = iota; B; C )` | iota 从 0 开始逐行递增 |
| 显式转换 | `int64(n)` | Go 没有隐式类型提升，必须写明 |
| 定义新类型 | `type UserID int64` | 与底层类型不同，赋值需显式转换 |
| 任意类型 | `any` | 即空接口，尽量少用以免丢失类型安全 |

常见类型的零值：

| 类型 | 零值 |
| --- | --- |
| 所有数值类型 | 0 |
| bool | false |
| string | 空字符串 |
| 指针、切片、map、channel、函数、接口 | nil |
| 数组、结构体 | 每个元素或字段各自取零值 |

<Compare title="var 与短声明怎么选" :panels='[{"label":"var 声明","code":"var count int\nvar enabled bool\nvar total float64","note":"适合包级变量、需要显式零值、或先声明后赋值的场景"},{"label":"短声明 := ","code":"count := 0\nenabled := true\nratio := 0.5","note":"适合函数内局部变量，简洁且类型由右值推断"}]' note="两条路线没有对错：需要零值初始化或包级作用域用 var，函数内直接给初值用短声明更地道。" />

```go
package main

import "fmt"

// 用 iota 定义一组递增的状态常量
type Status int

const (
	StatusNew Status = iota // 0
	StatusRunning           // 1，省略表达式会沿用上一行
	StatusDone              // 2
)

func main() {
	var n int    // 零值 0
	var s string // 零值空字符串
	var ok bool  // 零值 false
	name := "Go" // 短声明，类型由右值推断
	age, score := 3, 9.5
	fmt.Println(n, s, ok, name, age, score, StatusDone)
}
```

<Callout type="warn" title="零值不等于不能用" text="nil 切片可以直接 append，但 nil map 必须先 make 才能写入；对 nil 接口调用方法会 panic。把「零值可用」当成设计目标，而不是默认假设。" />

延伸阅读：[变量、常量与 iota](/foundation/variables) · [数值与类型转换](/foundation/numeric) · [字符串与 rune](/foundation/strings)。

## 2. 控制流

| 结构 | 写法要点 |
| --- | --- |
| if | 可带初始化语句 `if v := f(); v > 0 { ... }`，条件不加括号 |
| for | 唯一的循环关键字，支持三段式、条件式、`for range` 三种形态 |
| switch | 命中后自动 break，需要贯穿才写 fallthrough |
| switch 无表达式 | `switch { case x > 0: ... }` 可替代一长串 if else |
| type switch | `switch v := x.(type)` 按接口的动态类型分支 |
| defer | 后进先出，在所在函数返回前执行 |

```go
package main

import "fmt"

// classify 演示 type switch：按传入值的动态类型走不同分支
func classify(v any) string {
	switch x := v.(type) {
	case int:
		return fmt.Sprintf("整数 %d", x)
	case string:
		return fmt.Sprintf("字符串 %s", x)
	default:
		return "其他类型"
	}
}

func main() {
	// if 带初始化语句，变量 v 的作用域被限制在 if 内
	if v := 7; v%2 == 0 {
		fmt.Println("偶数")
	} else {
		fmt.Println("奇数")
	}
	// 三段式 for
	for i := 0; i < 3; i++ {
		fmt.Print(i, " ")
	}
	fmt.Println()
	// switch 自带 break，不会落到下一个 case
	switch n := 2; n {
	case 1:
		fmt.Println("一")
	case 2:
		fmt.Println("二")
	}
	fmt.Println(classify(42), classify("hi"))
}
```

延伸阅读：[控制流 if / for / switch](/basics/control-flow) · [for range 陷阱](/basics/range-pitfalls)。

## 3. 切片

| 操作 | 写法 | 说明 |
| --- | --- | --- |
| 创建 | `s := make([]int, 0, 8)` | 长度 0、容量 8，预分配减少扩容 |
| 字面量 | `s := []int{1, 2, 3}` | 直接给初值 |
| 追加 | `s = append(s, 4)` | 必须接住返回值，可能换底层数组 |
| 批量追加 | `s = append(s, other...)` | 用 `...` 展开另一个切片 |
| 复制 | `copy(dst, src)` | 拷到独立底层数组，返回拷贝元素数 |
| 长度容量 | `len(s)` / `cap(s)` | 元素个数 / 底层数组可容纳个数 |
| 子切片 | `s[low:high]` | 左闭右开，共享底层数组 |
| 限制容量 | `s[low:high:max]` | full slice expression，防止 append 越界改写 |

```go
package main

import "fmt"

func main() {
	// 预分配容量，避免 append 过程中反复扩容与拷贝
	s := make([]int, 0, 4)
	s = append(s, 1, 2, 3)
	fmt.Println("长度与容量:", len(s), cap(s))

	// copy 到独立底层数组，避免与原子切片共享内存
	backup := make([]int, len(s))
	copy(backup, s)
	backup[0] = 99
	fmt.Println("原切片:", s, "独立副本:", backup)

	// 截取下标 1 到 3 之间的元素（左闭右开）
	sub := s[1:3]
	fmt.Println("子切片:", sub)
}
```

延伸阅读：[数组与切片](/basics/arrays-slices) · [切片底层与扩容](/core/slice-internals)。

## 4. map

| 操作 | 写法 | 说明 |
| --- | --- | --- |
| 创建 | `m := make(map[string]int)` | 必须初始化后才能写入 |
| 字面量 | `m := map[string]int{"a": 1}` | 直接给初值 |
| 写入 | `m["k"] = v` | 键必须可比较 |
| 读取 | `v := m["k"]` | 键不存在时返回值类型的零值 |
| 判定存在 | `v, ok := m["k"]` | comma-ok 区分「不存在」与「值为零值」 |
| 删除 | `delete(m, "k")` | 删除不存在的键是安全的 |
| 遍历 | `for k, v := range m` | 顺序随机，不要依赖 |
| 集合 | `map[string]struct{}{}` | 空结构体作值，最省内存 |

```go
package main

import "fmt"

func main() {
	// map 必须 make 之后才能写入
	m := make(map[string]int)
	m["go"] = 1
	m["rust"] = 2

	// comma-ok 用来区分「键不存在」与「值恰好是零值」
	if v, ok := m["go"]; ok {
		fmt.Println("找到 go:", v)
	}
	if _, ok := m["java"]; !ok {
		fmt.Println("java 不存在")
	}

	// 空结构体作值，实现一个轻量集合
	set := map[string]struct{}{"a": {}, "b": {}}
	delete(set, "a")
	fmt.Println("集合剩余:", len(set), "映射大小:", len(m))
}
```

延伸阅读：[map 与 Set 技巧](/basics/maps) · [map 底层 hmap](/core/map-internals)。

## 5. 结构体、方法与接口

| 概念 | 写法 | 说明 |
| --- | --- | --- |
| 定义结构体 | `type User struct { ID int }` | 字段名大写才可被其它包访问 |
| 字面量 | `u := User{ID: 1}` | 推荐带字段名，避免顺序耦合 |
| 字段标签 | `json:"id,omitempty"` | 供序列化等场景读取 |
| 值接收者 | `func (u User) Name() string` | 拿到副本，不能改原值 |
| 指针接收者 | `func (u *User) SetID(n int)` | 可修改原值，大结构体省拷贝 |
| 定义接口 | `type Reader interface { Read() }` | 隐式实现，无需 implements 关键字 |
| 空接口 | `any` | 可接受任意类型，用类型断言取回 |
| 类型断言 | `v, ok := x.(T)` | 带 comma-ok 才不会 panic |

```go
package main

import (
	"fmt"
	"math"
)

// Shape 是小接口：只声明真正需要的方法
type Shape interface {
	Area() float64
}

type Circle struct {
	R float64
}

// 值接收者即可满足接口，无需显式声明实现关系
func (c Circle) Area() float64 {
	return math.Pi * c.R * c.R
}

func report(s Shape) {
	fmt.Printf("面积 %.2f\n", s.Area())
}

func main() {
	c := Circle{R: 2}
	report(c) // Circle 隐式实现了 Shape
}
```

<Callout type="idea" title="Go 的接口哲学" text="接口应该由使用方按需定义，尽量小；返回具体结构体、接受接口参数，往往比一上来就设计庞大的抽象层更好维护。" />

延伸阅读：[结构体与内存对齐](/basics/structs) · [方法与接收者](/basics/methods) · [接口与组合](/basics/interfaces) · [interface 底层](/core/interface-internals)。

## 6. 错误处理

| 操作 | 写法 | 说明 |
| --- | --- | --- |
| 构造错误 | `errors.New("...")` | 简单文本错误 |
| 格式化错误 | `fmt.Errorf("...: %v", err)` | 只保留文本，不保留错误链 |
| 包装错误 | `fmt.Errorf("...: %w", err)` | 保留原始错误，形成错误链 |
| 判定链 | `errors.Is(err, target)` | 沿链匹配哨兵错误 |
| 提取类型 | `errors.As(err, &target)` | 沿链取出自定义错误类型 |
| 自定义错误 | 实现 `Error() string` 方法 | 可携带字段与上下文 |
| 忽略错误 | `_ = f()` | 不推荐，除非确实无关紧要 |

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("未找到") // 哨兵错误，供上层判定

func load(id int) error {
	if id <= 0 {
		// %w 保留原始错误，让上层能沿错误链判定
		return fmt.Errorf("加载 %d: %w", id, ErrNotFound)
	}
	return nil
}

func main() {
	err := load(0)
	fmt.Println("错误信息:", err)
	fmt.Println("是否属于未找到:", errors.Is(err, ErrNotFound))
}
```

延伸阅读：[错误处理与错误链](/basics/errors) · [defer / panic / recover](/basics/defer-panic) · [错误码与统一响应](/engineering/error-design)。

## 7. 并发

| 原语 | 写法 | 说明 |
| --- | --- | --- |
| 启动协程 | `go f()` | 轻量，初始栈很小 |
| 创建通道 | `ch := make(chan int, 4)` | 第二参数是缓冲容量 |
| 发送接收 | `ch <- v` / `v := <-ch` | 无缓冲通道收发会同步阻塞 |
| 关闭通道 | `close(ch)` | 由唯一发送方负责 |
| 遍历通道 | `for v := range ch` | 通道关闭且取空后自动结束 |
| 多路复用 | `select { case ... }` | 多个 case 就绪时随机选一个 |
| 等待一组 | `sync.WaitGroup` | Add 在启动前，Done 用 defer |
| 互斥 | `sync.Mutex` | 共享内存读写时加锁 |
| 单次执行 | `sync.Once` | 单例与一次性初始化 |
| 取消超时 | `context.WithTimeout` | 逐层透传取消信号 |

```go
package main

import (
	"fmt"
	"sync"
)

// worker 从只读通道取任务，把结果写进只写通道
func worker(id int, jobs <-chan int, results chan<- int, wg *sync.WaitGroup) {
	defer wg.Done()
	for j := range jobs {
		results <- j * id
	}
}

func main() {
	jobs := make(chan int, 4)
	results := make(chan int, 4)
	var wg sync.WaitGroup

	for w := 1; w <= 2; w++ {
		wg.Add(1) // Add 必须在启动 goroutine 之前
		go worker(w, jobs, results, &wg)
	}
	for j := 1; j <= 4; j++ {
		jobs <- j
	}
	close(jobs) // 发送方关闭，worker 的 range 才会结束

	go func() {
		wg.Wait()
		close(results)
	}()
	sum := 0
	for r := range results {
		sum += r
	}
	fmt.Println("结果合计:", sum)
}
```

<Callout type="warn" title="并发三查" text="一查 WaitGroup.Add 是否放在 goroutine 外；二查 channel 是否由唯一发送方关闭；三查共享变量是否加锁或用原子操作。拿不准就用 go test -race 跑一遍。" />

延伸阅读：[channel](/concurrency/channel) · [select 与超时](/concurrency/select) · [sync 原语](/concurrency/sync-primitives) · [context](/concurrency/context) · [数据竞争](/concurrency/race)。

## 8. 常用标准库

| 包 | 用途 | 高频 API | 回看 |
| --- | --- | --- | --- |
| fmt | 格式化输入输出 | Printf、Sprintf、Errorf | [第一行 Go 代码](/guide/hello) |
| strings | 字符串处理 | Builder、Contains、Split、Join | [字符串](/foundation/strings) |
| strconv | 字符串与基本类型互转 | Atoi、Itoa、ParseFloat | [数值](/foundation/numeric) |
| sort | 排序 | Ints、Slice、Stable | [数组与切片](/basics/arrays-slices) |
| errors | 错误构造与判定 | New、Is、As | [错误处理](/basics/errors) |
| encoding/json | JSON 编解码 | Marshal、Unmarshal、NewDecoder | [JSON](/basics/json) |
| reflect | 运行时反射 | TypeOf、ValueOf | [反射](/basics/reflect) |
| sync | 并发原语 | Mutex、WaitGroup、Once | [sync 原语](/concurrency/sync-primitives) |
| context | 取消与超时 | WithCancel、WithTimeout | [context](/concurrency/context) |
| time | 时间与定时器 | Now、Since、After、NewTimer | [select 与超时](/concurrency/select) |
| net/http | HTTP 客户端与服务端 | Get、ListenAndServe、Client | [net/http](/server/net-http) |
| os / io | 文件与流 | Open、ReadAll、Create | [文件与对象存储](/server/file-storage) |
| log/slog | 结构化日志 | Info、With、NewJSONHandler | [日志](/engineering/logging) |
| testing | 测试与基准 | T、B、Run | [单元测试](/engineering/unit-test) |
| slices / maps | 泛型集合工具（Go 1.21 起） | slices.Sort、maps.Keys | [泛型](/basics/generics) |

```go
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func main() {
	// strings.Builder 高效拼接，避免循环中反复分配字符串
	var b strings.Builder
	for _, w := range []string{"Go", " 快", " 稳"} {
		b.WriteString(w)
	}
	// 标准库函数返回 error 时应当处理，而不是忽略
	n, err := strconv.Atoi("42")
	if err != nil {
		fmt.Println("转换失败:", err)
		return
	}
	data, err := json.Marshal(map[string]int{"n": n})
	if err != nil {
		fmt.Println("编码失败:", err)
		return
	}
	xs := []int{3, 1, 2}
	sort.Ints(xs) // 原地升序排序
	fmt.Println(b.String(), xs, string(data))
}
```

## 9. 常用命令

<CodeTabs title="日常最常敲的三类命令" :tabs='[{"label":"跑起来","code":"go run .","note":"编译到临时目录并立即执行，开发时最快"},{"label":"出制品","code":"go build -o app .\n./app","note":"生成可执行文件，便于分发与部署"},{"label":"查并发","code":"go run -race .","note":"带竞争检测执行，排查隐藏的数据竞争"}]' />

| 命令 | 作用 |
| --- | --- |
| `go mod init 模块路径` | 初始化模块，生成 go.mod |
| `go mod tidy` | 增删依赖并整理 go.sum |
| `go run .` | 编译并运行当前包 |
| `go build ./...` | 构建当前模块的全部包 |
| `go test ./...` | 运行全部测试 |
| `go test -race ./...` | 带数据竞争检测跑测试 |
| `go test -bench . -benchmem` | 跑基准测试并报告内存分配 |
| `go vet ./...` | 静态检查常见可疑写法 |
| `gofmt -w .` | 统一代码格式 |
| `go build -gcflags -m ./...` | 打印逃逸分析与内联决策 |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` | 交叉编译出静态二进制 |
| `go tool pprof -http :8080 采样文件` | 在浏览器里查看性能剖析 |

```bash
# 一个典型的新项目起步流程
go mod init example.com/demo   # 初始化模块
go mod tidy                    # 整理依赖
gofmt -w .                     # 统一格式
go vet ./...                   # 静态检查
go test -race ./...            # 带竞争检测跑测试
```

<KnowledgeCard :points='["短声明用于函数内直接赋初值，包级与需要零值初始化用 var", "append 一定接住返回值，map 一定先 make 再写", "错误用 %w 包装、errors.Is 或 errors.As 判定", "并发优先用 context 传递取消信号，共享内存才加锁"]' :pitfalls='["把 nil 切片和 nil map 当成同一套行为", "用 %v 包装错误导致错误链断裂", "依赖 map 遍历顺序，或让接收方关闭 channel"]' summary="语法记不住就查表，机制想不通就回看讲次。" />

延伸阅读：[工具链全景](/guide/toolchain) · [包与模块](/foundation/packages) · [代码规范与 lint](/engineering/code-style) · [基准测试](/engineering/bench-fuzz)。

## 继续练习

[综合自测](/practice/) · [常见坑与反模式](/practice/pitfalls) · [零基础常见问题](/practice/faq) · [面试与能力自评](/practice/interview) · [Go 生态与选型地图](/practice/ecosystem)
