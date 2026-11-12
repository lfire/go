---
title: 综合自测 · 跨阶段混合测验
description: 覆盖语法基础、运行时、并发、工程化与分布式的 13 道混合测验，每题附解析与按维度回看链接。
---

# 综合自测 · 跨阶段混合测验

> 学完若干阶段后，用这一页做一次「跨阶段体检」。题目刻意打乱了顺序：真实工作与面试里，没有人会先告诉你这道题考的是哪一讲。先凭理解作答，再对照解析查漏补缺。

<ProgressTracker />

<Callout type="idea" title="怎么用这一页" text="逐题提交答案，组件会立即判定对错并展开解析。做完对照文末的「错题回看导航」，按维度跳回对应讲次巩固。上方的进度组件汇总的是你在各讲末尾的打卡记录，只保存在你自己的浏览器里，不会上传。" />

<Callout type="tip" title="测验范围" text="共 13 道单选题，覆盖五个维度：语法基础、运行时与内存、并发、工程化与质量、分布式与云原生。不记分、不限时，随时可以点「再试一次」重做。" />

## 一、语法基础

<Quiz no="1" q="执行 var s []int 之后，关于 s 的说法哪一项是正确的？" :options='["s 是 nil 切片，len 与 cap 都为 0，可以直接 append", "s 是长度为 0 的非 nil 切片，append 之前必须先 make", "s 尚未初始化，读取 s[0] 会正常返回零值 0", "s 与 make([]int, 0) 完全等价，底层已经分配了数组"]' answer="0" explain="nil 切片是切片的合法零值：长度和容量都是 0，append 会按需分配底层数组，因此可以直接追加。但要特别注意，nil map 却不能直接写入，两者行为不同，这是最容易混淆的一组零值。可回看变量零值讲与数组与切片讲。" />

<Quiz no="2" q="在 Go 1.21 中，for _, v := range xs 内部启动的 goroutine 直接引用 v，常见现象与修复方式是？" :options='["所有 goroutine 往往打印出最后一个元素；应把 v 作为参数传入闭包，或写 v := v 重新绑定", "输出顺序完全随机；给 fmt.Println 加锁即可修复", "这段代码无法通过编译；必须改成按下标遍历", "会发生死锁；把结果 channel 改成带缓冲即可"]' answer="0" explain="Go 1.22 之前，循环变量在整个循环里复用同一份存储，闭包捕获的是它的地址，因此 goroutine 真正执行时读到的多半是最后一轮的值。Go 1.22 起每次迭代都会新建变量，但为了兼容旧版本、也让意图清晰，仍推荐显式传参或重新绑定。可回看 for range 陷阱讲与 goroutine 讲。" />

<Callout type="warn" title="版本差异要记牢" text="循环变量语义在 Go 1.22 发生了变化，但线上项目可能仍跑在旧版本上。写并发代码时显式传参，是最不依赖版本、也最好读的做法。" />

## 二、运行时与内存

<Quiz no="3" q="关于 Go 的垃圾回收器，下列说法最准确的是？" :options='["采用引用计数，对象引用归零时立即回收", "采用三色标记清除，大部分标记工作与用户 goroutine 并发执行，并用写屏障保证正确性", "采用分代复制算法，每次 GC 都需要全程暂停程序", "由开发者手动调用 free 触发，运行时不会自动回收"]' answer="1" explain="Go 使用并发的三色标记清除：只在少数阶段有极短的 STW，通过混合写屏障维护三色不变式，让标记与业务代码并发进行。它的设计目标是压低停顿，而不是彻底消灭停顿。可回看垃圾回收讲。" />

<Quiz no="4" q="某函数返回 error 接口，函数内部写 var p *MyErr = nil; return p。调用方判断 err != nil 的结果是？" :options='["false，因为 p 本身是 nil", "true，因为接口保存了动态类型，只有类型与值都为 nil 时接口才等于 nil", "编译失败，指向结构体的 nil 不能作为接口值返回", "取决于 GOGC 的取值，结果不确定"]' answer="1" explain="接口由动态类型与动态值两部分组成。把一个 typed nil 指针赋给接口后，动态类型并不为空，于是接口整体就不等于 nil，这是最经典的接口陷阱。正确做法是显式 return nil。可回看 interface 底层讲与错误处理讲。" />

<Quiz no="5" q="下列哪一种写法最可能让局部变量逃逸到堆上？" :options='["把局部变量的地址作为函数返回值返回", "两个局部 int 相加后赋给第三个局部变量", "为结构体定义值接收者方法", "声明一个 const 常量"]' answer="0" explain="编译器发现指针在函数返回后仍会被引用，栈帧随函数结束就失效了，因此只能把对象分配到堆上。可以用 go build -gcflags -m 观察输出里的逃逸提示来验证。可回看逃逸分析讲与值语义讲。" />

## 三、并发

<Quiz no="6" q="关于 channel 的关闭与收发，下列说法正确的是？" :options='["向已关闭的 channel 发送会 panic；接收方仍可取完缓冲中的值，之后得到零值与 false", "再次关闭一个已关闭的 channel 会安全地返回 false", "应当由接收方负责关闭 channel", "在 nil channel 上发送和接收会立即返回零值"]' answer="0" explain="关闭应由唯一发送方负责；重复关闭、向已关闭通道发送都会 panic；nil 通道上的收发会永久阻塞，这一特性常被用来在 select 里禁用某个分支。可回看 channel 讲与 hchan 底层讲。" />

<Quiz no="7" q="一个 HTTP 请求需要向下游多个 goroutine 统一施加超时与取消，首选做法是？" :options='["设置一个全局布尔标志，让各 goroutine 不断轮询", "用 context.WithTimeout 创建上下文，并作为参数逐层透传给下游", "让每个 goroutine 自己 time.Sleep 到点退出", "增大 channel 缓冲容量，让任务自然跑完"]' answer="1" explain="context 就是为取消信号的树状传播而设计的：父上下文取消会级联通知所有派生上下文，标准库与 gRPC 都按此约定透传，避免 goroutine 泄漏。可回看 context 讲与并发模式讲。" />

<Quiz no="8" q="想检测程序里隐蔽的数据竞争，应该使用哪条命令？" :options='["go vet ./...", "go test -race ./... 或 go run -race main.go", "gofmt -w .", "go build -gcflags -m ./..."]' answer="1" explain="race detector 会在运行时插桩，记录内存访问与同步事件，报告缺少 happens-before 约束的并发读写。它带来数倍开销，适合测试与预发环境而非常驻生产。可回看数据竞争讲与内存模型讲。" />

## 四、工程化与质量

<Quiz no="9" q="既要给错误补充上下文、又要让上层能用 errors.Is 判定哨兵错误，正确写法是？" :options='["用 fmt.Errorf 配合 %v 格式化原始错误", "用 fmt.Errorf 配合 %w 包装原始错误，再由上层用 errors.Is 或 errors.As 判定", "用 errors.New 重新构造一个只含错误文本的新错误", "直接 panic，把错误抛给最上层统一处理"]' answer="1" explain="%w 会把原始错误挂到错误链上，errors.Is 与 errors.As 才能沿链逐层匹配；换成 %v 只保留文本，链就断了，上层再也无法判定错误类型。可回看错误处理讲与错误设计讲。" />

<Quiz no="10" q="要定位一个 Go 服务的 CPU 热点，合理的第一步是？" :options='["凭经验先给疑似热点路径加缓存", "写基准测试建立基线，再用 go test -bench 与 pprof 采集 CPU profile", "把日志级别调到 Debug 逐行打印耗时", "直接把 GOGC 调大以减少 GC 次数"]' answer="1" explain="先量化再优化：Benchmark 提供可复现的基线，pprof 的 CPU profile 与火焰图指出真正耗时的函数，改完再用 benchstat 对比确认没有回归。凭直觉改代码经常优化错地方。可回看性能剖析讲与基准测试讲。" />

<Quiz no="11" q="构建体积最小、可复现的 Go 服务镜像，常用做法是？" :options='["直接用官方 golang 镜像作为运行时镜像", "多阶段构建：编译阶段静态编译，运行阶段用 scratch 或 distroless 只放二进制", "把源码打进镜像，容器启动时执行 go run", "必须基于 Alpine 并动态链接 glibc"]' answer="1" explain="关闭 CGO 后 Go 能产出静态二进制，配合多阶段构建，运行镜像里只保留可执行文件与非 root 用户，既小又减少攻击面，也不依赖基础镜像的 libc。可回看容器化讲。" />

## 五、分布式与云原生

<Quiz no="12" q="关于 CAP 定理，下列理解最准确的是？" :options='["一致性、可用性、分区容错性三者可以同时完全满足", "网络分区是必须面对的客观前提，发生分区时只能在强一致与可用性之间取舍", "只要选择 CP，系统就永远不会丢失任何写入", "CA 系统在发生网络分区时依然能正常对外服务"]' answer="1" explain="P 不是可选项，而是「分区一定会发生」这一事实。分区期间若继续保证强一致，就得拒绝部分请求（牺牲 A）；若继续对外服务，就得接受可能读到旧数据（牺牲 C）。可回看 CAP 与一致性模型讲。" />

<Quiz no="13" q="消息队列消费者可能重复收到同一条消息，最通用可靠的防护是？" :options='["把消费重试次数调到最大", "在业务侧设计幂等键，配合去重表或状态机做幂等消费", "完全依赖消息队列宣称的恰好一次投递", "关闭消费者的 ack 确认机制"]' answer="1" explain="端到端的恰好一次很难仅靠基础设施保证，通用做法是把消费逻辑做成幂等：用业务唯一键去重，或让重复操作被状态机挡住而不产生副作用。可回看分布式锁与幂等讲、消息队列讲。" />

## 错题回看导航

| 维度 | 本组考点 | 建议回看 |
| --- | --- | --- |
| 语法基础 | 零值、for range 闭包捕获 | [变量与常量](/foundation/variables) · [for range 陷阱](/basics/range-pitfalls) · [数组与切片](/basics/arrays-slices) |
| 运行时与内存 | GC、interface nil、逃逸 | [垃圾回收](/core/gc) · [interface 底层](/core/interface-internals) · [逃逸分析](/core/escape-analysis) |
| 并发 | channel、context、race | [channel](/concurrency/channel) · [context](/concurrency/context) · [数据竞争](/concurrency/race) |
| 工程化与质量 | 错误链、pprof、镜像 | [错误处理](/basics/errors) · [性能剖析](/engineering/profiling) · [容器化](/distributed/docker) |
| 分布式与云原生 | CAP、幂等 | [CAP 与一致性](/distributed/cap-base) · [分布式锁与幂等](/distributed/distributed-lock) · [消息队列](/server/mq) |

<CheckList id="practice-mixed-selfcheck" title="自测后自查" :items='["能说清 nil 切片与 nil map 在写入行为上的差异", "能解释接口为什么会出现 typed nil 陷阱", "能说出三色标记与写屏障各自解决什么问题", "能区分 context 取消传播与共享标志位轮询", "能说明 %w 与 %v 在错误链上的差别", "能讲清幂等键如何防止重复消费的副作用"]' />

<KnowledgeCard :points='["测验按五个维度混合编排，刻意不预先提示考点", "每道解析都指向可回看的真实讲次", "错题优先回看对应讲次的代码，再重做一遍"]' :pitfalls='["只记选项不理解机制，换个问法就会答错", "把 go vet 当成数据竞争检测工具", "误以为 channel 应该由接收方关闭"]' summary="能把错题背后的机制讲给别人听，才算真正掌握。" />

<Callout type="warn" title="别只背答案" text="把每道题背后的机制用自己的话讲一遍，再顺着导航链接回看对应讲次的可运行示例，效果远好于反复刷同一套题。错的题隔几天回来重做，检验是否真的补上了。" />

## 继续练习

[语法速查表](/practice/cheatsheet) · [常见坑与反模式](/practice/pitfalls) · [零基础常见问题](/practice/faq) · [面试与能力自评](/practice/interview) · [Go 生态与选型地图](/practice/ecosystem) · [课程总目录](/guide/curriculum)
