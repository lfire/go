---
title: 第 18 讲 · 方法与接收者：值还是指针
description: 理解方法集，分清值与指针接收者，写出不踩坑的接收者设计
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="30 分钟" goal="理解方法集，写出不踩坑的接收者" prereq="第 17 讲 · 结构体、标签与内存对齐" />

# 第 18 讲 · 方法与接收者：值还是指针

> 结构体把数据装好了，接下来要让它有行为：计数、校验、序列化。Go 没有类，但可以给类型绑定函数，调用时写成 `x.F()`，读起来和面向对象一样自然。真正需要想清楚的是那个写在 `func` 后面的接收者：用值还是用指针？这个选择决定方法能不能改到原值，也决定类型能不能满足某个接口。学完本讲，你能给出有依据的判断，而不是凭感觉挑一个。

## 这一讲你会得到什么
<CheckList id="l18-goals" title="本讲目标" :items='["会定义方法","分清两种接收者","用方法集判断接口实现","处理 nil 接收者"]' />

## 1. 先建立直觉

把方法想成「绑定到某个类型上的普通函数」：接收者其实就是隐式的第一个参数。既然是参数，值接收者拿到的就是一份拷贝，改它动不到原件；指针接收者拿到的是地址，改的是同一份数据。Go 在调用侧帮你自动做取地址和解引用，所以 `c.Add()` 与 `(&c).Add()` 常常都能写，但语义差别藏在方法内部。

<ConceptDiagram type="iface" title="接收者决定方法集" labels="值接收者::T 与 *T 可调|指针接收者::只有 *T 可调|接口实现::看方法集|nil 接收者::可为 nil" caption="方法集决定接口实现" />

<FlowTimeline title="一次方法调用" :steps='[{"title":"写调用","desc":"c.Add(1)","tone":"blue"},{"title":"取地址","desc":"自动转换","tone":"orange"},{"title":"进方法","desc":"改副本或原值","tone":"green"},{"title":"返回","desc":"结果留在 c 上","tone":"purple"}]' direction="horizontal" />

## 2. 语法与规则

方法定义只比函数多一个接收者声明，写在 `func` 与方法名之间。下面这段把两种接收者放在同一个类型上对比：值接收者改的是副本，指针接收者改到原值；反过来，指针变量也能调用值接收者方法，编译器会自动解引用。

```go
package main

import "fmt"

type Counter struct {
	N int
}

// 值接收者：拿到副本
func (c Counter) AddValue(delta int) {
	c.N += delta
}

// 指针接收者：改到原值
func (c *Counter) Add(delta int) {
	c.N += delta
}

func main() {
	c := Counter{N: 1}
	c.AddValue(10)
	fmt.Println("值接收者后 N =", c.N)
	c.Add(10)
	fmt.Println("指针接收者后 N =", c.N)

	p := &Counter{N: 1}
	p.AddValue(5) // 指针也能调值方法
	fmt.Println("指针调值方法后 N =", p.N)
}
```

<OutputBox title="运行结果" output="值接收者后 N = 1\n指针接收者后 N = 11\n指针调值方法后 N = 1" />

<LineByLine code="func (c *Counter) Add(d int) {\n    c.N += d\n}\nc.Add(1)" :notes='["接收者写在 func 后的括号里","指针接收者改到原始结构体","方法体到此结束","值变量调用时编译器自动取地址"]' title="方法定义逐行拆解" />

<Term word="方法集" text="类型可调用的方法集合；T 只含值接收者方法，*T 含两类。" />

## 3. 深入一步

方法集的意义在接口实现时显形：接口要求的方法必须完整出现在赋给它的那个动态类型的方法集里。因此用值接收者实现的方法，`T` 与 `*T` 都满足接口；只用指针接收者实现的方法，只有 `*T` 满足，`T` 不满足。编译期写一行 `var _ Interface = (*T)(nil)` 是常用的断言习惯。

```go
package main

import "fmt"

type Speaker interface {
	Speak() string
}

type Dog struct{}

func (d Dog) Speak() string { return "wang" }

type Cat struct{}

func (c *Cat) Speak() string { return "miao" }

func main() {
	var s Speaker
	s = Dog{} // 值类型即满足接口
	fmt.Println(s.Speak())

	s = &Cat{} // 只有指针满足
	fmt.Println(s.Speak())

	// s = Cat{} 会编译失败：Cat 的方法集不含 Speak
	var _ Speaker = (*Cat)(nil) // 编译期断言
}
```

<OutputBox title="运行结果" output="wang\nmiao" />

| 场景 | 值接收者 `T` | 指针接收者 `*T` |
| --- | --- | --- |
| 能否改原值 | 否，改的是副本 | 是 |
| `T` 可调用 | 是 | 是，编译器取地址 |
| `*T` 可调用 | 是 | 是 |
| 接口实现 | `T` 与 `*T` 都满足 | 仅 `*T` 满足 |
| 大结构体成本 | 每次拷贝一份 | 只传一个指针 |
| nil 调用 | 不适用 | 可以，需方法内判空 |

<Callout type="warn" title="易错点" text="值接收者改不了原对象；赋接口先确认方法集。" />

`nil` 接收者是另一个必须想到的边界：指针接收者完全可能为 `nil`，方法若不加判断就会解引用崩溃。反过来，允许 `nil` 接收者能写出很优雅的递归代码，比如链表求和。注意这和「接口变量是否为 nil」是两件事，下一讲会专门拆开。

```go
package main

import "fmt"

type Node struct {
	Value int
	Next  *Node
}

// nil 接收者可被直接调用
func (n *Node) Sum() int {
	if n == nil {
		return 0
	}
	return n.Value + n.Next.Sum()
}

func main() {
	list := &Node{1, &Node{2, &Node{3, nil}}}
	fmt.Println("链表和 =", list.Sum())

	var empty *Node
	fmt.Println("nil 接收者 =", empty.Sum())
}
```

<OutputBox title="运行结果" output="链表和 = 6\nnil 接收者 = 0" />

## 4. 工程实践

选接收者有三条实用标准：方法要修改状态、结构体较大、或类型里已有其他指针接收者方法时，用指针；只是读数据、结构体很小、且希望值类型也能满足接口时，用值。同一个类型尽量保持接收者风格一致，混用会让方法集变得难记。接口实现尽量由使用方定义，实现方不知道自己该满足哪些接口。

<Compare title="接收者的选择" :panels='[{"label":"写法 A","code":"func (c Counter) Total() int {\n    return c.N\n}","note":"只读小对象，值也满足接口"},{"label":"写法 B","code":"func (c *Counter) Add(d int) {\n    c.N += d\n}","note":"改状态或对象大，用指针"}]' />

<Callout type="tip" title="切片元素调指针方法" text="切片元素可取地址；map 的 value 不能取址，得先取出或改存指针。" />

把接收者看穿，其实就一句话：它是隐式的第一个参数。`c.Add(1)` 与函数 `Counter.Add(c, 1)` 在编译器眼里等价，所以值接收者一定会把整个结构体拷一份，字段越多越贵；指针接收者只传一个地址。想亲眼确认，可以写方法表达式 `f := (*Counter).Add; f(&c, 1)` 调一次，或跑 `go build -gcflags=-m`，输出里能看到哪些接收者被判定为逃逸到堆。方法集也不玄学：`go/types` 包的 `types.NewMethodSet` 能列出 `T` 与 `*T` 各自有哪些方法，`go doc go/types.NewMethodSet` 就有签名；`cmd/compile/internal/types2` 在类型检查阶段算的正是这张表。这里只借它说明「为什么值类型赋不进指针实现的接口」，不读源码。

最该记住的一条工程红线是：含 `sync.Mutex` 的类型千万别用值接收者。值接收者会把锁连同字段一起拷贝，两个副本各锁各的，等于没加锁；`go vet` 的 `copylocks` 检查器会直接报 `passes lock by value`，这条命令比 code review 更靠谱。并发下再跑 `go test -race`，把锁去掉就会看到 `DATA RACE`，加上就干净——两个命令配合，能自己把「为什么必须指针接收者」验证一遍。这也是「同一类型的接收者风格要一致」的硬理由：只要有一个方法用了指针，其余即便只读也跟着用指针，免得方法集被切得七零八落。

真实踩坑还有两类。其一是接口赋值拖到运行前才发现：函数参数写着接口，传值进去时编译失败，报错说某个方法不在方法集里；在项目里提前写一行 `var _ Interface = (*T)(nil)`，签名一改就当场编译报错，不用等到调用点。其二是 nil 接收者：指针接收者完全可能为空，方法里直接 `c.N++` 就是 panic；像链表求和那样开头判空返回零值，或者像账户扣款那样返回明确错误，都是把边界收进方法内部。注意这跟「接口变量整体是不是 nil」是两回事，后者留到下一讲。

<ConceptDiagram type="value-pointer" title="值接收者与指针接收者" caption="值接收者拿到整份拷贝，指针接收者只拿到一个地址，改的是同一份数据" />

下面两段代码把「并发安全」与「nil 接收者」写成能跑的例子。第一段给计数器加上互斥锁：含锁的类型只能用指针接收者，100 个 goroutine 并发累加仍得到确定结果。

```go
package main

import (
	"fmt"
	"sync"
)

// SafeCounter 内含互斥锁，只能用指针接收者：
// 值接收者会把锁一并拷贝，两个副本各锁各的，等于没锁。
type SafeCounter struct {
	mu sync.Mutex
	n  int
}

func (c *SafeCounter) Add(delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += delta
}

func (c *SafeCounter) Value() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func main() {
	var c SafeCounter
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Add(1)
		}()
	}
	wg.Wait()
	fmt.Println("并发累加结果 =", c.Value())
}
```

<OutputBox title="运行结果" output="并发累加结果 = 100" />

第二段让指针接收者把边界收进方法内部：余额不足、金额非法、接收者为 nil 都返回明确错误，而不是默默改坏数据或崩掉。

```go
package main

import (
	"errors"
	"fmt"
)

type Account struct {
	Balance int
}

// Withdraw 用指针接收者改状态；余额不足或接收者为 nil 都返回明确错误。
func (a *Account) Withdraw(amount int) error {
	if a == nil { // 指针接收者可能为 nil，先挡住
		return errors.New("账户为空")
	}
	if amount <= 0 || amount > a.Balance {
		return fmt.Errorf("取款 %d 不合法", amount)
	}
	a.Balance -= amount
	return nil
}

func main() {
	a := &Account{Balance: 100}
	if err := a.Withdraw(30); err == nil {
		fmt.Println("取款后余额 =", a.Balance)
	}
	fmt.Println("超额被拒 =", a.Withdraw(1000) != nil)

	var empty *Account
	fmt.Println("nil 接收者被挡住 =", empty.Withdraw(1) != nil)
}
```

<OutputBox title="运行结果" output="取款后余额 = 70\n超额被拒 = true\nnil 接收者被挡住 = true" />

| 写法 | 现象 | 原因与修法 |
| --- | --- | --- |
| 含 `sync.Mutex` 却用值接收者 | 并发仍出错或锁失效 | 值接收者把锁拷了一份，改指针接收者 |
| 大结构体用值接收者 | 每次调用都整份拷贝 | 改指针，`go build -gcflags=-m` 看逃逸 |
| 值类型赋给只由 `*T` 实现的接口 | 编译失败 | 把方法改成值接收者，或传地址 |
| 指针接收者方法里直接解引用 | nil 调用时 panic | 方法开头判 `if c == nil` |
| 给别包的类型加方法 | 编译失败 | 只能给本包定义的类型加方法 |

<Predict no="18-3" q="SafeCounter 内含 sync.Mutex，若把 Add 误写成值接收者 func (c SafeCounter) Add，再起 100 个 goroutine 各加 1，go vet 与 go test -race 分别会怎样？" :options='["都通过，结果稳定","vet 报 passes lock by value，race 报 DATA RACE","只 race 报错","编译失败"]' answer="1" output="go vet 报 passes lock by value，go test -race 报 DATA RACE" explain="值接收者把 Mutex 拷了一份，各副本锁不住同一份数据；copylocks 静态能查出拷贝锁，race 运行时能查出并发读写，两道检查都指向「必须改成指针接收者」。" />

## 动手练一练

<Exercise title="让结构体可自增" task="给 Counter 加方法 Inc，调用后 N 加 1 并打印。" :hints='["要改原值用指针接收者","调用自动取地址"]' solution="func (c *Counter) Inc() {\n    c.N++\n}\nc := Counter{}\nc.Inc()\nfmt.Println(c.N)" explain="指针接收者改到原值，输出 1。" />

## 随堂检测

<Quiz no="18-1" q="关于方法集，正确的是？" :options='["T 含指针接收者方法","*T 只能调指针方法","值接收者让 T 与 *T 都满足接口","能给其他包类型加方法"]' answer="2" explain="T 只含值接收者方法；非本地类型不能加方法。" />

<Predict no="18-2" q="Cat 仅由 *Cat 实现 Speaker，把 Cat{} 赋给 Speaker 会怎样？" :options='["正常编译","编译失败","运行时 panic","输出空串"]' answer="1" output="编译失败：Cat does not implement Speaker" explain="值类型方法集只含值接收者方法。" />

## 一页纸速记

<KnowledgeCard :points='["接收者是隐式第一参数","值改副本，指针改原值","*T 含两类方法","指针接收者可为 nil"]' :pitfalls='["值接收者改字段无效","值类型赋给指针实现的接口","给非本地类型加方法"]' summary="改状态或对象大用指针。" />

## 小结与下一讲

- 方法 = 带接收者的函数；值接收者拿副本，指针接收者改原值，调用侧自动转换。
- `T` 的方法集只含值接收者方法，`*T` 含两类；这直接决定接口能否赋值。
- 指针接收者可能为 `nil`，方法内判空；选接收者看修改需求、体积与一致性。

方法集是接口实现的基础，下一讲正式进入 Go 的抽象核心。

[下一讲：接口：隐式实现与组合](/basics/interfaces)
