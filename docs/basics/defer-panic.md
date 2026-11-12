---
title: 第 21 讲 · defer / panic / recover
description: 用 defer 保证清理动作一定执行，理解 LIFO 与参数求值时机，再用 panic 与 recover 为真正不可恢复的意外兜底。
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="35 分钟" goal="正确做资源清理与异常兜底" prereq="第 20 讲 · 错误处理：error、包装与错误链" />

# 第 21 讲 · defer / panic / recover

> 打开文件后要关、加锁后要解锁、拿到响应体后要释放、事务失败要回滚。这些收尾动作有个共同特点：只要函数中途 `return` 一次，写在末尾的那行清理就可能永远跑不到。与其在每条返回路径上都复制一遍，不如把「打开」和「收尾」绑在一起。Go 的 `defer` 就是为这件事生的；而 `panic` 与 `recover` 处理的是另一种情况——程序已经错到不该继续往下走。这一讲把两者一次讲清楚。

## 这一讲你会得到什么
<CheckList id="lesson-21" title="本讲目标" :items='["掌握 defer 的先进后出顺序与参数求值时机","避开命名返回值被 defer 改写的陷阱","说清 panic 如何沿调用栈传播、defer 何时仍会执行","知道 recover 只在 defer 函数里有效的原因","能判断什么场景该返回 error、什么场景才用 panic"]' />

## 1. 先建立直觉

把 `defer` 想成餐厅里叠便签。每上一道菜，服务员就在托盘边贴一张「餐后收走这只盘子」的便签；结账时从最上面那张开始收，也就是后贴的先收——这就是先进后出（LIFO）。更关键的一点：便签上写的内容是「贴的那一刻」就抄好的，不是结账时才临时去看。所以 `defer fmt.Println(x)` 记下的是声明时 `x` 的值，此后 `x` 怎么变都与它无关。

`panic` 则像拉下火警铃：当前函数立刻停手，但它已经贴好的便签仍会一张张收完，然后火警沿着楼层往上传，每一层都照样收完自己的便签。只有某层在 defer 里调用 `recover` 把警铃接住，整栋楼才恢复平静；否则一路传到最顶层，进程退出。

<ConceptDiagram type="defer-stack" title="defer 栈：后注册的先执行，参数在声明时即求值" />

先记住两条，后面所有细节都是它们的推论：注册顺序与执行顺序相反；参数在注册那一刻求值。

## 2. 语法与规则

先看 `defer` 的顺序与快照行为。注意最后一个注册的 defer 最先执行，它打印的 `x` 仍是声明时的 10。

```go
package main

import "fmt"

func demo() {
	defer fmt.Println("第 1 个 defer") // 最后执行
	defer fmt.Println("第 2 个 defer")
	defer fmt.Println("第 3 个 defer") // 最先执行

	x := 10
	defer fmt.Println("x 的快照:", x) // 此刻参数就固定为 10
	x = 99
	fmt.Println("函数体结束, x =", x)
}

func main() {
	demo()
}
```

<OutputBox title="运行结果" output="函数体结束, x = 99\nx 的快照: 10\n第 3 个 defer\n第 2 个 defer\n第 1 个 defer" note="最后注册的 defer 最先跑；x 被改成 99 也没影响快照，因为参数在声明那行就求值了。" />

真正容易翻车的是「返回值陷阱」。当函数使用命名返回值时，`return` 会先把值写进那个具名变量，然后才轮到 defer 执行；如果 defer 又改了同名变量，最终返回值就变了。匿名返回值没有这个交集。

```go
package main

import "fmt"

// 匿名返回值：return 先把结果拷贝走，defer 改的是另一个局部变量。
func plain() int {
	n := 1
	defer func() { n++ }()
	return n
}

// 命名返回值：return 把值写进具名变量 n，defer 再把它加一。
func named() (n int) {
	n = 1
	defer func() { n++ }()
	return n
}

func main() {
	fmt.Println(plain()) // 1
	fmt.Println(named()) // 2
}
```

<LineByLine title="紧贴资源获取写 defer" code="f, err := os.Open(name)\nif err != nil {\n    return err\n}\ndefer f.Close()\n// 后续放心使用 f" :notes='["打开文件，同时拿到 error","失败立刻返回，避免对 nil 调用 Close","注册清理：此后无论从哪条路径返回都会执行","业务代码不必再记得收尾"]' />

把 defer 紧跟在资源获取成功之后，是 Go 里最值得养成的习惯：判空在前、注册在后，中间不会插入别的返回分支，天然不会漏。

## 3. 深入一步

`panic` 会停止当前函数，然后按 LIFO 执行本函数已注册的 defer，再把控制权交给调用方；调用方同样先执行自己的 defer，如此逐层向上。若一路没人 `recover`，运行时打印调用栈并以非零状态退出。注意 `os.Exit` 是例外：它立刻结束进程，任何 defer 都不执行。

<FlowTimeline title="一次 panic 的向上传播" :steps='[{"title":"子函数 panic","desc":"停手，开始展开调用栈","tone":"red"},{"title":"执行本层 defer","desc":"按 LIFO 清理已占资源","tone":"orange"},{"title":"回到调用方","desc":"其 defer 继续执行","tone":"blue"},{"title":"遇到 recover","desc":"捕获后从捕获处继续","tone":"green"}]' direction="horizontal" />

`recover` 只在 defer 注册的函数体内直接调用才有效，并且必须在 panic 发生之前就注册好。它的返回值是 panic 时传入的值；没有 panic 时返回 `nil`。下面把意外转成一个普通 `error` 返回，主流程因此不受影响。

```go
package main

import "fmt"

// safeCall 在 defer 中 recover，把意外转成普通返回值。
func safeCall(job func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("任务中断: %v", r)
		}
	}()
	job()
	return nil
}

func risky() {
	fmt.Println("开始执行")
	panic("磁盘不可读")
}

func main() {
	err := safeCall(risky)
	fmt.Println(err != nil) // true
	fmt.Println("主流程继续运行")
}
```

<OutputBox title="运行结果" output="开始执行\ntrue\n主流程继续运行" note="panic 值不是 error，这里用 %v 把它转成文本；这是文本化，不是第 20 讲的错误链包装。" />

把各种情形放在一起对照：

| 情形 | defer 是否执行 | recover 能否接住 | 最终结果 |
| --- | --- | --- | --- |
| 函数正常返回 | 是，按 LIFO | 不适用 | 正常结束 |
| 本函数内 panic | 是，按 LIFO | 本函数已注册的 defer 可以 | 接住则继续，否则向上传 |
| 子函数 panic | 父函数 defer 照常执行 | 可在父函数的 defer 里接住 | 子函数不再续跑，控制权回到捕获处 |
| 调用 `os.Exit` | 否 | 否 | 立即退出，不跑任何 defer |
| 在普通代码里直接调 `recover` | 不适用 | 否，只会得到 `nil` | 位置不对，等于没写 |

<Term word="栈展开" text="panic 后逐层退出函数的过程：每层先按 LIFO 跑完自己的 defer，再把 panic 交给上一层，直到被 recover 或进程退出。" />

<Callout type="warn" title="易错点：参数快照、返回值与 recover 的位置" text="defer 的参数在声明时就求值，循环里注册 defer 还会把清理动作一直堆到函数结束才执行，长循环应拆成小函数或显式关闭。命名返回值会被 defer 改写，写库函数时优先用匿名返回值。recover 必须写在 defer 的函数体里且直接调用，包一层普通函数再调就失效了。" />

关于性能只点一句结论：Go 1.14 起编译器对大多数 `defer` 做了开放编码优化，常见场景开销已很小，不必为了省一次 defer 而手动复制清理代码；它的实现细节属于 [第 34 讲](/core/defer-internals)，这里不展开。

## 4. 工程实践

生产代码里 defer 最常见的四个位置是：`mu.Unlock()` 紧跟 `mu.Lock()`、`resp.Body.Close()` 紧跟成功的 HTTP 请求、`tx.Rollback()` 紧跟事务开启、文件 `Close()` 紧跟 `Open` 成功。判空一定要在注册之前，否则错误分支里会对 `nil` 调用 `Close`。

至于 `panic`，工程边界要划得很清楚：库函数一律返回 `error`，把判断权交给调用方，`panic` 只用于「继续跑下去只会更糟」的情况，例如数组越界这类程序员错误、或者 `main` / `init` 里发现配置缺失到根本无法启动。服务端的请求处理函数通常会在最外层挂一个带 `recover` 的中间件，防止单个请求的意外把整个进程带走——但那是兜底，不是常规错误处理方式，更不能替代上一讲的错误链。

<Compare title="清理动作的两种写法" :panels='[{"label":"推荐：判定成功后立刻 defer","code":"f, err := os.Open(name)\nif err != nil {\n    return err\n}\ndefer f.Close()\n// 任意分支 return 都会关文件","note":"清理与获取成对出现，新增分支也不会漏。"},{"label":"反例：写在函数末尾","code":"f, err := os.Open(name)\n// ... 中途多个 return ...\nf.Close()","note":"任何一个提前返回都会跳过 Close，句柄泄漏。"}]' />

## 动手练一练

<Exercise title="用 defer 把 panic 转成 error" task="补全 protect 函数：要求无论传入的函数是否 panic，都能返回一个 error；不发生 panic 时返回 nil。想想 recover 该放在哪里、返回值该怎么被改写。" solution="func protect(job func()) (err error) {\n    defer func() {\n        if r := recover(); r != nil {\n            err = fmt.Errorf(`panic: %v`, r)\n        }\n    }()\n    job()\n    return nil\n}" :hints='["recover 必须写在 defer 的函数体里，并且直接调用。","要把值写回 err，就得用命名返回值。"]' explain="命名返回值 err 让 defer 有机会改写最终结果；recover 拿到 panic 值后赋给 err，函数于是以普通 error 的形式收场。" />

## 随堂检测

<Quiz no="21-1" q="关于 defer，下面哪项说法是正确的？" :options='["defer 按注册顺序依次执行","defer 的参数在函数返回那一刻才求值","defer 按后进先出执行，且参数在声明时求值","一个函数里最多只能写一个 defer"]' answer="2" explain="defer 用栈保存，后注册的先执行；参数在 defer 语句执行时就求值，与之后的变量变化无关。" />

<Predict no="21-2" q="第二段代码里 plain() 与 named() 的输出依次是什么？" :options='["1 1","1 2","2 1","2 2"]' answer="1" output="1\n2" explain="plain 的返回值在 return 时已拷贝，defer 改的是局部变量；named 使用命名返回值，defer 把具名变量 n 加一后成了真正的返回结果。" />

## 一页纸速记

<KnowledgeCard :points='["defer 后进先出，参数在声明时求值","清理动作紧跟资源获取成功之后注册","panic 逐层展开栈，每层仍会跑完自己的 defer","recover 只在 defer 函数体内直接调用才有效"]' :pitfalls='["命名返回值被 defer 悄悄改写，结果出人意料","在循环里堆 defer，清理一直拖到函数结束","os.Exit 不执行任何 defer","把 recover 当常规错误处理，丢掉错误链"]' summary="defer 管收尾，panic 管停手，recover 只在 defer 里把意外接回普通流程。" />

## 小结与下一讲

- `defer` 用栈实现：后注册先执行，参数在注册时求值；紧跟资源获取写，最不容易漏。
- 命名返回值与 defer 有交集，写库函数优先用匿名返回值。
- `panic` 会逐层执行 defer 再向上传；`recover` 只在 defer 函数体内有效，且属于兜底手段。

下一讲进入泛型：当同一段逻辑要为 `int`、`string` 各写一遍时，怎么用一个类型参数消灭重复，又不把代码搞得更难读。

[下一讲：泛型：类型参数与约束](/basics/generics)
