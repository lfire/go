---
title: 常见坑与反模式 · 32 条错误与正确写法对照
description: 按基础、切片与 map、接口与 nil、错误与 defer、并发、工程与部署六组整理的 32 条 Go 常见坑，每条含错误写法、正确写法与原因。
---

# 常见坑与反模式 · 32 条对照

> 这些坑几乎都不是「语法不会」，而是「我以为」和「实际行为」之间的差距。每条理清楚三件事：错误长什么样、正确怎么写、为什么会错。所有代码块都能独立编译（`go build` 通过）；标注为错误写法的代码能编译，但结果不对、运行时 panic，或存在并发与资源隐患。

<Callout type="idea" title="怎么用这一页" text="先按目录跳到你正在写的那类代码，扫一眼错误写法看自己有没有中招。建议把正确写法抄进一个临时文件跑一遍，用输出验证理解，比只读文字记得牢。" />

<CheckList id="pitfalls-scan" title="提交代码前的快速自查" :items='["append 的结果都赋回原变量了吗", "map 都在写入前 make 了吗", "返回 error 的地方有没有把 typed nil 当成 nil", "所有 error 都被检查，包装错误用的是 %w 吗", "循环里的 defer 是否该收窄作用域", "共享变量有没有加锁，或改用 channel", "响应体、文件、锁是否都配了 defer 释放"]' />

## 一、基础

### 1. 短变量声明遮蔽了外层变量

**错误写法**

```go
package main

import "fmt"

func main() {
	var err error
	if true {
		// := 新建了同名的局部变量，外层的 err 被遮蔽
		err := fmt.Errorf("内部错误")
		_ = err
	}
	fmt.Println("外层 err:", err) // 仍然是 nil
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	var err error
	if true {
		// 用 = 赋值，而不是重新声明
		err = fmt.Errorf("内部错误")
	}
	fmt.Println("外层 err:", err) // 能拿到内层赋的值
}
```

**原因**：`:=` 至少声明一个新变量，同名时会在内层新建并遮蔽外层，赋值请用 `=`。[回看变量与作用域](/foundation/variables)

### 2. 按字节索引中文字符串

**错误写法**

```go
package main

import "fmt"

func main() {
	s := "你好"
	// string 是字节序列，s[0] 只是一个 UTF-8 字节
	fmt.Println("首字节:", s[0], "长度:", len(s))
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {
	s := "你好"
	r := []rune(s) // 转成 rune 切片，按字符处理
	fmt.Println("首字符:", string(r[0]), "字符数:", utf8.RuneCountInString(s))
}
```

**原因**：中文在 UTF-8 里占多个字节，`len` 返回字节数、`s[i]` 返回字节，要按字符处理得用 `[]rune` 或 `range`。[回看字符串](/foundation/strings) · [字符串底层](/core/string-internals)

### 3. 以为传数组进函数会修改原数组

**错误写法**

```go
package main

import "fmt"

func modify(a [3]int) {
	a[0] = 99 // 改的只是传进来的副本
}

func main() {
	arr := [3]int{1, 2, 3}
	modify(arr)
	fmt.Println(arr) // 仍是 [1 2 3]
}
```

**正确写法**

```go
package main

import "fmt"

func modify(a *[3]int) {
	a[0] = 99 // 通过指针改到原数组
}

func main() {
	arr := [3]int{1, 2, 3}
	modify(&arr)
	fmt.Println(arr) // [99 2 3]
}
```

**原因**：数组是值类型，赋值与传参都整体拷贝；要就地修改请传切片或数组指针。[回看数组与切片](/basics/arrays-slices) · [值语义](/core/value-semantics)

### 4. 用 == 比较两个浮点数

**错误写法**

```go
package main

import "fmt"

func main() {
	a, b := 0.1+0.2, 0.3
	fmt.Println("是否相等:", a == b) // false，精度误差
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"math"
)

func main() {
	a, b := 0.1+0.2, 0.3
	// 比较差值是否小于一个极小阈值
	fmt.Println("是否近似相等:", math.Abs(a-b) < 1e-9)
}
```

**原因**：浮点运算有精度误差，判等应看差值是否落在容差内，而不是用 `==`。[回看数值类型](/foundation/numeric)

### 5. 误以为 iota 从 1 开始

**错误写法**

```go
package main

import "fmt"

const (
	Monday = iota // 实际是 0
	Tuesday       // 1
)

func main() {
	// 想当然地用 1 表示周一
	fmt.Println("把 1 当周一:", 1 == Monday) // false
}
```

**正确写法**

```go
package main

import "fmt"

const (
	Monday = iota + 1 // 显式从 1 开始
	Tuesday           // 2
)

func main() {
	fmt.Println("周一:", Monday, "周二:", Tuesday)
}
```

**原因**：`iota` 从 0 开始逐行递增，需要别的起点就显式写出来，不要靠猜。[回看常量与 iota](/foundation/variables)

## 二、切片与 map

### 6. append 不接住返回值

**错误写法**

```go
package main

import "fmt"

func main() {
	s := []int{1, 2}
	_ = append(s, 3) // 结果被丢弃，没有赋回 s
	fmt.Println(s)   // 仍是 [1 2]
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	s := []int{1, 2}
	s = append(s, 3) // 接住可能变化后的切片头
	fmt.Println(s)   // [1 2 3]
}
```

**原因**：append 可能因扩容换掉底层数组，必须把返回值赋回原变量才不会丢。[回看数组与切片](/basics/arrays-slices) · [切片底层](/core/slice-internals)

### 7. 子切片共享底层数组被 append 改写

**错误写法**

```go
package main

import "fmt"

func main() {
	a := []int{1, 2, 3, 4}
	b := a[:2]        // 与 a 共享底层数组，容量仍是 4
	b = append(b, 99) // 原地写进了 a[2]
	fmt.Println("a:", a) // [1 2 99 4]
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	a := []int{1, 2, 3, 4}
	b := make([]int, 2)
	copy(b, a[:2])    // 拷到独立底层数组
	b = append(b, 99)
	fmt.Println("a:", a, "b:", b) // a 不受影响
}
```

**原因**：子切片共享底层数组，append 落在剩余容量内时会覆盖原数据，用 `copy` 或 full slice expression 隔离。[回看切片底层](/core/slice-internals)

### 8. 以为 range 里改变量会改到原切片

**错误写法**

```go
package main

import "fmt"

func main() {
	xs := []int{1, 2, 3}
	for _, v := range xs {
		v *= 2 // 改的只是元素副本
	}
	fmt.Println(xs) // 仍是 [1 2 3]
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	xs := []int{1, 2, 3}
	for i := range xs {
		xs[i] *= 2 // 通过下标改到真实元素
	}
	fmt.Println(xs) // [2 4 6]
}
```

**原因**：`range` 的第二个值是元素副本，要修改原切片请用下标。[回看 for range 陷阱](/basics/range-pitfalls)

### 9. 对 nil map 直接写入

**错误写法**

```go
package main

import "fmt"

func main() {
	var m map[string]int // 零值是 nil
	m["go"] = 1         // 运行时 panic：写入 nil map
	fmt.Println(m)
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	m := make(map[string]int) // 先初始化再写入
	m["go"] = 1
	fmt.Println(m)
}
```

**原因**：nil map 可读不可写，写入前必须 `make`；这与 nil 切片可直接 append 正好相反。[回看 map](/basics/maps) · [map 底层](/core/map-internals)

<Compare title="两种 nil 容器别搞混" :panels='[{"label":"nil 切片","code":"var s []int\ns = append(s, 1)","note":"零值即可直接 append，无需初始化"},{"label":"nil map","code":"var m map[int]int\nm[0] = 1","note":"写入会 panic，必须先 make"}]' note="记住一句话：切片零值能用，map 零值只读。" />

### 10. 改了 map 里结构体副本却没写回

**错误写法**

```go
package main

import "fmt"

type Counter struct{ N int }

func main() {
	m := map[string]Counter{"a": {N: 1}}
	c := m["a"] // 拿到的是值副本
	c.N = 2     // 只改了副本
	fmt.Println(m["a"].N) // 仍是 1
}
```

**正确写法**

```go
package main

import "fmt"

type Counter struct{ N int }

func main() {
	m := map[string]Counter{"a": {N: 1}}
	c := m["a"]
	c.N = 2
	m["a"] = c // 显式写回 map
	fmt.Println(m["a"].N) // 2
}
```

**原因**：map 索引返回的是值副本，改动后必须写回，或者干脆用指针作 value。[回看 map](/basics/maps)

### 11. 依赖 map 的遍历顺序

**错误写法**

```go
package main

import "fmt"

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	// 误以为每次都会按 a b c 的固定顺序输出
	for k, v := range m {
		fmt.Println(k, v)
	}
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"sort"
)

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // 显式排序，输出才稳定
	for _, k := range keys {
		fmt.Println(k, m[k])
	}
}
```

**原因**：map 遍历顺序被刻意随机化，需要稳定顺序就把键收集起来排序。[回看 map 遍历](/basics/maps)

### 12. 循环 append 却不预分配容量

**错误写法**

```go
package main

import "fmt"

func main() {
	var s []int
	for i := 0; i < 1000; i++ {
		s = append(s, i) // 触发多次扩容与内存拷贝
	}
	fmt.Println(len(s))
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	s := make([]int, 0, 1000) // 一次预分配到位
	for i := 0; i < 1000; i++ {
		s = append(s, i)
	}
	fmt.Println(len(s))
}
```

**原因**：长度已知时预分配 `cap`，能省掉 append 过程中的多次扩容与拷贝。[回看切片扩容](/core/slice-internals)

<Callout type="warn" title="切片组一句话" text="切片相关的坑，八成都能归到一句话：切片头是值、底层数组是共享的。想清楚这两点，append、子切片、range 副本的问题就都串起来了。" />

## 三、接口与 nil

### 13. typed nil 赋给接口后不等于 nil

**错误写法**

```go
package main

import "fmt"

type MyErr struct{}

func (e *MyErr) Error() string { return "出错了" }

func makeErr() error {
	var p *MyErr = nil
	return p // 接口里装的是「类型 *MyErr，值 nil」
}

func main() {
	fmt.Println(makeErr() == nil) // false
}
```

**正确写法**

```go
package main

import "fmt"

type MyErr struct{}

func (e *MyErr) Error() string { return "出错了" }

func makeErr(fail bool) error {
	if !fail {
		return nil // 显式返回真正的 nil 接口
	}
	return &MyErr{}
}

func main() {
	fmt.Println(makeErr(false) == nil) // true
}
```

**原因**：接口由动态类型与动态值组成，typed nil 的动态类型非空，接口整体就不等于 nil。[回看 interface 底层](/core/interface-internals) · [错误处理](/basics/errors)

### 14. 类型断言不带 comma-ok

**错误写法**

```go
package main

import "fmt"

func main() {
	var x any = "hello"
	n := x.(int) // 类型不符，运行时 panic
	fmt.Println(n)
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	var x any = "hello"
	// 用 comma-ok 判定，类型不符时不会 panic
	if n, ok := x.(int); ok {
		fmt.Println(n)
	} else {
		fmt.Println("不是 int 类型")
	}
}
```

**原因**：不带 ok 的断言在类型不符时会 panic，用 comma-ok 或 type switch 才安全。[回看接口与断言](/basics/interfaces)

### 15. 值接收者改不了结构体字段

**错误写法**

```go
package main

import "fmt"

type Counter struct{ N int }

func (c Counter) Inc() { c.N++ } // 值接收者拿到的是副本

func main() {
	c := Counter{}
	c.Inc()
	fmt.Println(c.N) // 0
}
```

**正确写法**

```go
package main

import "fmt"

type Counter struct{ N int }

func (c *Counter) Inc() { c.N++ } // 指针接收者改到原值

func main() {
	c := Counter{}
	c.Inc()
	fmt.Println(c.N) // 1
}
```

**原因**：值接收者操作副本，要修改字段必须用指针接收者；接收者类型还决定方法集与接口实现。[回看方法与接收者](/basics/methods) · [接口](/basics/interfaces)

### 16. 比较装着不可比较类型的接口

**错误写法**

```go
package main

import "fmt"

func main() {
	var a any = []int{1, 2}
	var b any = []int{1, 2}
	fmt.Println(a == b) // 运行时 panic：比较不可比较类型
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	a, b := []int{1, 2}, []int{1, 2}
	fmt.Println(slices.Equal(a, b)) // true，逐元素比较
}
```

**原因**：接口比较时若动态类型不可比较（切片、map、函数）会 panic，比较内容请用 `slices.Equal` 或 `reflect.DeepEqual`。[回看接口](/basics/interfaces) · [interface 底层](/core/interface-internals)

## 四、错误与 defer

### 17. 用 %v 包装错误导致错误链断裂

**错误写法**

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func main() {
	// %v 只保留错误文本，错误链被切断
	err := fmt.Errorf("查询失败: %v", ErrNotFound)
	fmt.Println(errors.Is(err, ErrNotFound)) // false
}
```

**正确写法**

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func main() {
	// %w 把原始错误挂进错误链，可供上层判定
	err := fmt.Errorf("查询失败: %w", ErrNotFound)
	fmt.Println(errors.Is(err, ErrNotFound)) // true
}
```

**原因**：`%w` 把原始错误挂进错误链，`errors.Is` / `errors.As` 才能沿链匹配；`%v` 只保留文本。[回看错误处理](/basics/errors) · [错误设计](/engineering/error-design)

### 18. 忽略 error 返回值

**错误写法**

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	n, _ := strconv.Atoi("abc") // 忽略了解析失败
	fmt.Println("结果:", n)      // 0，错误被吞掉
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"strconv"
)

func main() {
	// 显式接收并检查 error，失败时立即返回
	n, err := strconv.Atoi("abc")
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Println("结果:", n)
}
```

**原因**：忽略 error 会让失败以零值悄悄继续，问题被推迟到更难排查的地方才暴露。[回看错误处理](/basics/errors)

### 19. 以为 defer 的参数在执行时才求值

**错误写法**

```go
package main

import "fmt"

func main() {
	i := 0
	defer fmt.Println("defer:", i) // 注册时就把 0 存好了
	i = 100
	fmt.Println("当前:", i)
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	i := 0
	defer func() { fmt.Println("defer:", i) }() // 执行时才读 i
	i = 100
	fmt.Println("当前:", i)
}
```

**原因**：defer 的函数参数在注册那一刻就求值，想读最新值要包一层闭包。[回看 defer](/basics/defer-panic) · [defer 实现](/core/defer-internals)

### 20. 命名返回值被 defer 意外改写

**错误写法**

```go
package main

import "fmt"

func calc() (n int) {
	defer func() { n++ }() // 悄悄改了返回值
	return 1
}

func main() {
	fmt.Println(calc()) // 2，而非预期的 1
}
```

**正确写法**

```go
package main

import "fmt"

func calc() int {
	n := 1
	return n // 返回值明确，不受 defer 影响
}

func main() {
	fmt.Println(calc()) // 1
}
```

**原因**：命名返回值是函数级变量，defer 能修改它；除非刻意用 defer 统一处理错误，否则别让返回值变得难以预测。[回看 defer](/basics/defer-panic) · [多返回值](/foundation/callables)

### 21. recover 没写在 defer 里

**错误写法**

```go
package main

import "fmt"

func main() {
	// 直接调用 recover 拦不住 panic
	if r := recover(); r != nil {
		fmt.Println("恢复:", r)
	}
	panic("出错了")
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	// recover 必须写在 defer 注册的函数里才有效
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("恢复:", r)
		}
	}()
	panic("出错了")
}
```

**原因**：`recover` 只在 defer 调用的函数里有效，直接调用永远返回 nil。[回看 defer / panic / recover](/basics/defer-panic)

## 五、并发

### 22. 循环里启动 goroutine 捕获循环变量

**错误写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Print(i, " ") // 旧版本里多半都打印 3
		}()
	}
	wg.Wait()
	fmt.Println()
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Print(n, " ") // 每个协程拿到自己的副本
		}(i)
	}
	wg.Wait()
	fmt.Println()
}
```

**原因**：把循环变量作为参数传进去，就不依赖 Go 1.22 的循环变量语义变化，任何版本都正确。[回看 for range 陷阱](/basics/range-pitfalls) · [goroutine](/concurrency/goroutine)

### 23. WaitGroup.Add 放在 goroutine 内部

**错误写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		go func() {
			wg.Add(1) // Wait 可能在计数增加前就返回
			defer wg.Done()
			fmt.Println(i)
		}()
	}
	wg.Wait()
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1) // 在启动之前累加计数
		go func(n int) {
			defer wg.Done()
			fmt.Println(n)
		}(i)
	}
	wg.Wait()
}
```

**原因**：`Add` 必须在启动 goroutine 之前调用，否则 `Wait` 可能在计数增加前就返回。[回看 sync 原语](/concurrency/sync-primitives)

### 24. 多 goroutine 并发写 map 未加锁

**错误写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	m := make(map[int]int)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m[n] = n // 并发写会触发运行时 fatal error
		}(i)
	}
	wg.Wait()
	fmt.Println(len(m))
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	m := make(map[int]int)
	var mu sync.Mutex
	var wg sync.WaitGroup
	// 每次写入前加锁，保证 map 的并发安全
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mu.Lock()
			m[n] = n
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	fmt.Println(len(m))
}
```

**原因**：map 不是并发安全的，并发读写必须加锁、用 `sync.Map`，或干脆用 channel 串行化。[回看 map](/basics/maps) · [sync 原语](/concurrency/sync-primitives) · [数据竞争](/concurrency/race)

### 25. 向已关闭的 channel 发送数据

**错误写法**

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	close(ch)
	ch <- 1 // 运行时 panic：向已关闭通道发送
	fmt.Println("不会执行到这里")
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	ch := make(chan int, 1)
	ch <- 1
	close(ch) // 由唯一发送方在发完后关闭
	for v := range ch {
		fmt.Println(v)
	}
}
```

**原因**：关闭应由唯一发送方负责，关闭后不能再发送，重复关闭同样会 panic。[回看 channel](/concurrency/channel)

### 26. 循环里反复用 time.After 累积定时器

**错误写法**

```go
package main

import (
	"fmt"
	"time"
)

func main() {
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		select {
		case v := <-ch:
			fmt.Println(v)
		case <-time.After(time.Millisecond): // 每轮新建一个定时器
			fmt.Println("超时")
		}
	}
}
```

**正确写法**

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch := make(chan int)
	for i := 0; i < 3; i++ {
		select {
		case v := <-ch:
			fmt.Println(v)
		case <-ctx.Done(): // 复用一个到期信号，不累积定时器
			fmt.Println("整体超时")
			return
		}
	}
}
```

**原因**：循环里每轮 `time.After` 都会新建定时器，高频下会暂时堆积；复用一个 context 或 timer 更稳。[回看 select](/concurrency/select) · [context](/concurrency/context)

### 27. 含锁的结构体按值传递

**错误写法**

```go
package main

import (
	"fmt"
	"sync"
)

type Safe struct {
	mu sync.Mutex
	n  int
}

func (s Safe) Inc() { // 值接收者把锁拷贝了一份
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}

func main() {
	var s Safe
	s.Inc()
	fmt.Println(s.n) // 0，锁也形同虚设
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"sync"
)

type Safe struct {
	mu sync.Mutex
	n  int
}

func (s *Safe) Inc() { // 指针接收者共用同一把锁
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}

func main() {
	var s Safe
	s.Inc()
	fmt.Println(s.n) // 1
}
```

**原因**：`sync.Mutex` 等同步原语不能拷贝，含锁结构体一律用指针传递，否则每个副本各锁各的。[回看 sync 原语](/concurrency/sync-primitives) · [方法接收者](/basics/methods)

<Callout type="warn" title="并发组一句话" text="只要出现 goroutine，就把 go test -race 当成默认动作：编译通过不代表并发正确，很多坑只在运行时以概率暴露。" />

## 六、工程与部署

### 28. range 里对元素取地址全指向同一处

**错误写法**

```go
package main

import "fmt"

func main() {
	xs := []int{1, 2, 3}
	ps := make([]*int, 0, len(xs))
	for _, v := range xs {
		ps = append(ps, &v) // 都指向同一个循环变量
	}
	for _, p := range ps {
		fmt.Print(*p, " ") // 3 3 3
	}
	fmt.Println()
}
```

**正确写法**

```go
package main

import "fmt"

func main() {
	xs := []int{1, 2, 3}
	ps := make([]*int, 0, len(xs))
	for i := range xs {
		ps = append(ps, &xs[i]) // 指向切片里真正的元素
	}
	for _, p := range ps {
		fmt.Print(*p, " ") // 1 2 3
	}
	fmt.Println()
}
```

**原因**：`range` 的值变量是被复用的副本，取它的地址会得到同一个地址；要取元素地址请用下标。[回看 for range 陷阱](/basics/range-pitfalls)

### 29. http.Client 忘了设置超时

**错误写法**

```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	// 默认客户端没有超时，对端不响应就会一直等
	client := &http.Client{}
	fmt.Println("超时设置:", client.Timeout) // 0 表示不超时
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	// 显式给出总超时，或用 context 做更细粒度的控制
	client := &http.Client{Timeout: 5 * time.Second}
	fmt.Println("超时设置:", client.Timeout)
}
```

**原因**：默认 `http.Client` 没有超时，慢对端会拖垮调用方，生产环境务必设 `Timeout` 或透传 context。[回看 net/http](/server/net-http) · [context](/concurrency/context)

### 30. 响应体忘记关闭导致连接泄漏

**错误写法**

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		return
	}
	// 忘了 resp.Body.Close()，连接无法归还连接池
	fmt.Println(resp.Status)
}
```

**正确写法**

```go
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func main() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		return
	}
	defer resp.Body.Close() // 及时释放连接
	fmt.Println(resp.Status)
}
```

**原因**：响应体必须关闭，否则底层连接无法复用，长期运行会耗尽连接池。[回看 net/http](/server/net-http) · [httptest](/engineering/mock-test)

### 31. 忽略 json.Unmarshal 的错误

**错误写法**

```go
package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	var v struct{ N int }
	_ = json.Unmarshal([]byte("bad json"), &v) // 错误被吞掉
	fmt.Println(v.N)                           // 0，其实数据无效
}
```

**正确写法**

```go
package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	var v struct{ N int }
	// 检查解码错误，脏数据不会以零值继续流转
	if err := json.Unmarshal([]byte("bad json"), &v); err != nil {
		fmt.Println("解码失败:", err)
		return
	}
	fmt.Println(v.N)
}
```

**原因**：忽略解码错误会让脏数据以零值继续流转，编解码结果一定要检查 error。[回看 JSON](/basics/json)

### 32. 用 fmt.Println 打线上日志

**错误写法**

```go
package main

import "fmt"

func main() {
	user, action := "alice", "login"
	// 无级别、无结构化字段，事后难以检索和采样
	fmt.Println("用户操作", user, action)
}
```

**正确写法**

```go
package main

import "log/slog"

func main() {
	// 结构化字段 + 级别，便于检索、过滤与关联 trace_id
	slog.Info("用户操作", "user", "alice", "action", "login")
}
```

**原因**：`fmt.Println` 没有级别和结构化字段，线上排障请用 `slog` 之类的结构化日志并带上请求标识。[回看日志](/engineering/logging)

<FlowTimeline title="遇到诡异行为时的排查顺序" direction="vertical" :steps='[{"title":"先复现","desc":"写一段最小可运行代码，确认现象能稳定出现","tone":"blue"},{"title":"再归类","desc":"结果不对先查值语义与副本，panic 先查 nil 与边界","tone":"orange"},{"title":"查并发","desc":"只要涉及 goroutine，就用 go test -race 跑一遍","tone":"purple"},{"title":"做回归","desc":"修好后补一条表驱动测试，防止同一个坑再踩一次","tone":"green"}]' />

<KnowledgeCard :points='["结果不对，先想副本与值语义", "运行时 panic，先想 nil、越界与通道状态", "并发问题一律先上 -race", "每个坑都留一段最小复现和一条回归测试"]' :pitfalls='["把 nil 切片和 nil map 当成一回事", "以为 defer 的参数在执行时才求值", "append 的结果不赋回原变量"]' summary="坑不在语法，而在「我以为」和「实际」之间的那道缝。" />

<Callout type="tip" title="继续巩固" text="把这一页和语法速查表、综合自测配合着用：速查表查写法，本页查反例，自测检验是否真的记住了。完整原理请顺着每条的回看链接回到对应讲次。" />

## 相关页面

[语法速查表](/practice/cheatsheet) · [综合自测](/practice/) · [零基础常见问题](/practice/faq) · [面试与能力自评](/practice/interview) · [单元测试](/engineering/unit-test) · [代码规范与 lint](/engineering/code-style)
