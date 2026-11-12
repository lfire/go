---
title: 课程总目录 · 118 讲
description: 从零基础到资深的完整 Go 学习路线，11 个阶段、118 讲、10 类工程场景。
---

<StageHeader stage="全站导览" level="★~★★★★★" time="全程约 120 小时" goal="按阶段递进，随时能看到自己走到哪" prereq="无" />

# 课程总目录 · 118 讲

> 十一个阶段按「先建直觉 → 再学语法 → 再拆底层 → 再做工程 → 最后做架构与源码」排列。每一讲都带可运行示例、图示、随堂检测与动手练习。

<PathMap />

<Callout type="idea" title="怎么用这份目录" text="按顺序走最省脑力；已有经验的人可以直接跳到对应阶段。每讲末尾都能打卡，首页会汇总各阶段完成度，进度只存在你自己的浏览器里。" />

## 导学 · 建立地图

**阶段目标**：先看见整张地图：Go 是什么、怎么跑起来、这套教程怎么用

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [导学阶段导览](/guide/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 001 | [开篇：Go 是什么，为什么值得学](/guide/opening) | ★☆☆☆☆ | 20 分钟 | Go 的诞生与设计哲学 · 静态类型与编译型 · 适用边界与典型雇主场景 · 与其他语言的取舍 |
| 002 | [装好环境：工具链、工作区与 Modules](/guide/setup) | ★☆☆☆☆ | 25 分钟 | 安装与版本管理 · GOPATH 到 Go Modules · go env 与代理 · VS Code/GoLand 与 Delve 调试 |
| 003 | [第一行 Go 代码：从源码到可执行文件](/guide/hello) | ★☆☆☆☆ | 25 分钟 | package main 与 import · func main 入口 · fmt.Println · go run 与 go build 的差别 |
| 004 | [工具链全景：build / test / fmt / vet / mod](/guide/toolchain) | ★★☆☆☆ | 30 分钟 | 常用命令地图 · gofmt 与 goimports · go vet 与静态检查 · go mod tidy · go generate 与交叉编译 |

## 阶段一 · 编程地基

**阶段目标**：不假设任何编程基础，补齐值、类型、内存、函数、包这些地基

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [编程地基阶段导览](/foundation/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 005 | [程序、数据、值与类型](/foundation/concepts) | ★☆☆☆☆ | 25 分钟 | 程序=数据+处理 · 值与变量的区别 · 静态类型意味着什么 · 类型系统的价值 |
| 006 | [变量、常量、iota 与作用域](/foundation/variables) | ★☆☆☆☆ | 30 分钟 | var 与 := · 零值 · const 与 iota 枚举 · 包级/函数级作用域 · 导出与命名 |
| 007 | [数值、布尔、字符与类型转换](/foundation/numeric) | ★☆☆☆☆ | 30 分钟 | 整型与平台相关类型 · 浮点与精度 · byte/rune · 显式转换 · math 与 strconv |
| 008 | [字符串、rune 与高效拼接](/foundation/strings) | ★★☆☆☆ | 30 分钟 | string 不可变 · byte 与 rune · range 遍历 · strings.Builder · 拼接性能对比 |
| 009 | [栈、堆、值语义与指针](/foundation/memory) | ★★☆☆☆ | 35 分钟 | 栈帧与堆 · 值传递 · 指针与取址 · nil · 什么时候该用指针 |
| 010 | [函数、多返回值、变参与闭包](/foundation/callables) | ★★☆☆☆ | 35 分钟 | 多返回值与命名返回 · 变参 · 函数作为值 · 闭包与捕获 · defer 初识 |
| 011 | [包、模块与依赖管理](/foundation/packages) | ★★☆☆☆ | 30 分钟 | package 与 import · internal 可见性 · go.mod/go.sum · 语义化版本与 replace · init 与导入副作用 |
| 012 | [阶段综合：CLI 单位换算器](/foundation/capstone) | ★★☆☆☆ | 40 分钟 | flag 解析 · 输入校验与错误返回 · 表格驱动测试 · 分层的小项目结构 |

## 阶段二 · 语法核心

**阶段目标**：用 Go 写出结构完整、能处理错误的小程序

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [语法核心阶段导览](/basics/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 013 | [数组与切片：定义与操作](/basics/arrays-slices) | ★★☆☆☆ | 35 分钟 | 数组是值 · 切片创建与 make · len/cap · append 与 copy · 切片表达式 |
| 014 | [控制流：if / for / switch / goto](/basics/control-flow) | ★☆☆☆☆ | 30 分钟 | if 初始化语句 · 唯一的 for · switch 自动 break 与 fallthrough · type switch · goto 的合理用法 |
| 015 | [for range 与闭包捕获陷阱](/basics/range-pitfalls) | ★★★☆☆ | 30 分钟 | range 的副本语义 · 循环变量捕获 · Go 1.22 的变化 · 指针切片与 map 遍历 · 实战排查 |
| 016 | [map：哈希、遍历与 Set 技巧](/basics/maps) | ★★☆☆☆ | 30 分钟 | 创建与 CRUD · comma-ok · 遍历无序 · map[string]struct{} 实现 Set · 并发不安全 |
| 017 | [结构体、标签与内存对齐](/basics/structs) | ★★☆☆☆ | 35 分钟 | 定义与字面量 · 匿名字段与嵌入 · json tag · 内存对齐与字段顺序 · 空结构体 |
| 018 | [方法与接收者：值还是指针](/basics/methods) | ★★☆☆☆ | 30 分钟 | 方法定义 · 值/指针接收者差别 · 方法集与接口实现 · nil 接收者 · 何时用指针 |
| 019 | [接口：隐式实现与组合](/basics/interfaces) | ★★★☆☆ | 40 分钟 | 隐式实现 · 小接口原则 · 接口组合 · 空接口与 any · 类型断言与 type switch · 接受接口返回结构体 |
| 020 | [错误处理：error、包装与错误链](/basics/errors) | ★★★☆☆ | 40 分钟 | error 是值 · fmt.Errorf %w · errors.Is/As · 自定义错误类型 · 哨兵错误 · 错误码的边界 |
| 021 | [defer / panic / recover](/basics/defer-panic) | ★★★☆☆ | 35 分钟 | defer LIFO 与参数求值 · 返回值陷阱 · panic 传播 · recover 只在 defer 有效 · 该不该用 panic |
| 022 | [泛型：类型参数与约束](/basics/generics) | ★★★☆☆ | 40 分钟 | 类型参数语法 · constraints 与 comparable · 泛型函数与类型 · 类型推断 · 适用场景与代价 |
| 023 | [反射 reflect 与 go generate](/basics/reflect) | ★★★★☆ | 40 分钟 | Type 与 Value · 可设置性与 CanSet · 标签读取 · 反射三定律 · go generate 与代码生成 |
| 024 | [JSON、序列化与常见坑](/basics/json) | ★★☆☆☆ | 35 分钟 | encoding/json 编解码 · tag 与 omitempty · 时间/数字精度 · 未知字段与兼容 · 流式编解码 |
| 025 | [阶段综合：内存版 KV 存储](/basics/capstone) | ★★★☆☆ | 50 分钟 | 接口抽象 Storage · CRUD 与错误设计 · 并发保护 · 命令解析 · 为后续贯穿线埋点 |

## 阶段三 · 核心机制

**阶段目标**：从「会用」到「知道为什么」：底层数据结构、内存与运行时

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [核心机制阶段导览](/core/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 026 | [值语义深潜：赋值、传参、拷贝成本](/core/value-semantics) | ★★★☆☆ | 35 分钟 | 一切皆值 · 结构体拷贝 · 数组与切片传参差别 · 大对象传递成本 · 编译器优化 |
| 027 | [逃逸分析与编译器决策](/core/escape-analysis) | ★★★★☆ | 40 分钟 | 栈分配与逃逸 · 常见逃逸场景 · 查看逃逸分析 · 指针返回 · 减少堆分配的实践 |
| 028 | [切片底层：三元组、扩容与共享陷阱](/core/slice-internals) | ★★★★☆ | 45 分钟 | SliceHeader · 共享底层数组 · 扩容策略与版本差异 · append 别名陷阱 · full slice expression |
| 029 | [map 底层：hmap、桶与渐进扩容](/core/map-internals) | ★★★★☆ | 45 分钟 | hmap/bmap 结构 · 哈希与桶定位 · 溢出桶 · 等量/翻倍扩容 · 迁移与遍历 |
| 030 | [interface 底层：iface / eface 与 nil 陷阱](/core/interface-internals) | ★★★★☆ | 45 分钟 | iface 与 eface · itab 与方法表 · 动态派发成本 · typed nil · 断言与反射 |
| 031 | [字符串底层与零拷贝转换](/core/string-internals) | ★★★★☆ | 35 分钟 | string 头部结构 · 不可变与共享 · []byte 转换拷贝 · unsafe 零拷贝 · 适用与风险 |
| 032 | [内存分配器：mcache / mcentral / mheap](/core/allocator) | ★★★★☆ | 45 分钟 | size class · span 与 object · 三级缓存 · tiny allocator · 大对象路径 |
| 033 | [垃圾回收：三色标记、屏障与 GOGC](/core/gc) | ★★★★☆ | 50 分钟 | 三色标记 · 写屏障与混合屏障 · STW 与并发标记 · GOGC 与 MemoryLimit · GC 友好代码 |
| 034 | [defer 实现与 open-coded 优化](/core/defer-internals) | ★★★★☆ | 35 分钟 | 堆分配 defer · 栈上 defer · 开放编码 · 性能对比 · 仍需注意的场景 |
| 035 | [程序启动、init 顺序与包初始化](/core/startup) | ★★★★☆ | 40 分钟 | 依赖图与初始化顺序 · init 的使用边界 · runtime 初始化 · 全局变量的代价 · 启动耗时分析 |
| 036 | [编译、链接、SSA 与内联](/core/compiler) | ★★★★☆ | 45 分钟 | 编译流程 · SSA 中间表示 · 内联与预算 · 逃逸分析输出 · go tool objdump 与 nm |
| 037 | [unsafe、cgo 与跨边界成本](/core/unsafe-cgo) | ★★★★★ | 45 分钟 | unsafe.Pointer 规则 · uintptr 陷阱 · cgo 调用开销 · 内存所有权 · WASM/插件替代方案 |

## 阶段四 · 并发编程

**阶段目标**：掌握 CSP 与 GMP，能写出正确、可诊断的并发程序

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [并发编程阶段导览](/concurrency/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 038 | [并发与并行：CSP 思想](/concurrency/concurrency-intro) | ★★☆☆☆ | 30 分钟 | 并发≠并行 · CSP 与共享内存 · Go 的取舍 · 串行/并发/并行图示 · 何时不该并发 |
| 039 | [goroutine：创建、栈与生命周期](/concurrency/goroutine) | ★★☆☆☆ | 35 分钟 | go 关键字 · 初始栈与增长 · 调度单位 · 泄漏与排查 · GOMAXPROCS |
| 040 | [GMP 调度模型全景](/concurrency/gmp) | ★★★★☆ | 50 分钟 | G/M/P 各自职责 · 全局与本地队列 · P 的数量 · 调度上下文 · 一张图记住模型 |
| 041 | [调度循环：抢占、窃取与 handoff](/concurrency/scheduler) | ★★★★★ | 50 分钟 | schedule 循环 · work stealing · 系统调用与 handoff · 异步抢占 · sysmon 职责 |
| 042 | [channel：语义、缓冲与关闭](/concurrency/channel) | ★★★☆☆ | 40 分钟 | 无缓冲/缓冲 · 发送接收阻塞规则 · 关闭与遍历 · 只发送/只接收 · nil channel |
| 043 | [channel 底层 hchan 与收发队列](/concurrency/hchan) | ★★★★★ | 45 分钟 | hchan 结构 · sendq/recvq · 缓冲区环形队列 · 突然唤醒与直接拷贝 · select 实现基础 |
| 044 | [select、超时与多路复用](/concurrency/select) | ★★★☆☆ | 40 分钟 | select 随机选择 · default 非阻塞 · time.After 与 timer 泄漏 · done channel · 优先级技巧 |
| 045 | [sync：Mutex / RWMutex / WaitGroup / Once / Cond](/concurrency/sync-primitives) | ★★★☆☆ | 45 分钟 | 互斥锁与互斥粒度 · 读写锁 · WaitGroup · Once 与单例 · Cond 与广播 · 常见死锁 |
| 046 | [atomic 与 sync.Pool](/concurrency/atomic-pool) | ★★★★☆ | 40 分钟 | 原子操作与 CAS · atomic.Value · sync.Pool 生命周期 · GC 清空 · 适用与误用 |
| 047 | [context：取消、超时、值传递](/concurrency/context) | ★★★☆☆ | 40 分钟 | Context 接口 · WithCancel/Timeout/Deadline · 取消传播 · context.Value 克制使用 · 服务端标准用法 |
| 048 | [并发模式：pipeline / fan-out / worker pool](/concurrency/patterns) | ★★★★☆ | 50 分钟 | 管道与阶段 · 扇出扇入 · worker pool · errgroup · singleflight · 限速与背压 |
| 049 | [数据竞争、内存模型与 race detector](/concurrency/race) | ★★★★☆ | 45 分钟 | happens-before · 竞态典型样例 · -race 原理与开销 · 误报与漏报 · 修复策略对比 |

## 阶段五 · 工程化与质量

**阶段目标**：把代码变成可维护、可测试、可发布、可诊断的工程

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [工程化阶段导览](/engineering/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 050 | [项目结构与分层设计](/engineering/project-layout) | ★★★☆☆ | 40 分钟 | cmd/internal/pkg/api · 按领域还是按层 · 依赖方向 · DO/DTO/VO · 避免过度设计 |
| 051 | [代码规范、gofmt、vet 与 golangci-lint](/engineering/code-style) | ★★☆☆☆ | 35 分钟 | Effective Go 要点 · 命名与注释 · gofmt/goimports · go vet · lint 规则集与 CI 接入 |
| 052 | [单元测试与表驱动测试](/engineering/unit-test) | ★★★☆☆ | 40 分钟 | testing 基础 · 表驱动 · t.Run 子测试 · t.Helper · 断言库取舍 · 可测试性设计 |
| 053 | [Mock：gomock / sqlmock / httptest](/engineering/mock-test) | ★★★★☆ | 45 分钟 | 依赖倒置与接口 · gomock 生成 · sqlmock · httptest · gomock/gomonkey 边界 |
| 054 | [基准测试、模糊测试与覆盖率](/engineering/bench-fuzz) | ★★★☆☆ | 40 分钟 | Benchmark 写法 · benchstat 对比 · 内存分配指标 · fuzz test · 覆盖率与误区 |
| 055 | [日志：slog / zap 与结构化采样](/engineering/logging) | ★★★☆☆ | 40 分钟 | 结构化日志 · 级别与上下文 · trace_id 关联 · 性能与采样 · 日志与隐私 |
| 056 | [配置：flag / env / viper / 配置中心](/engineering/config) | ★★★☆☆ | 40 分钟 | 十二要素与配置外置 · flag 与环境变量 · viper · 配置校验 · 热加载与灰度 |
| 057 | [错误码、错误栈与统一响应](/engineering/error-design) | ★★★★☆ | 45 分钟 | 错误分类与错误码 · 错误栈封装 · 内部错误到 HTTP/gRPC 状态 · 不泄露细节 · 可观测性 |
| 058 | [设计模式与 Functional Options / wire](/engineering/patterns-design) | ★★★★☆ | 45 分钟 | Functional Options · 工厂与单例 · 策略与中间件 · 依赖注入与 wire · 避免 Java 式套壳 |
| 059 | [性能剖析：pprof / trace / 火焰图](/engineering/profiling) | ★★★★☆ | 50 分钟 | CPU/Mem/Block/Mutex profile · 火焰图读法 · trace 与调度延迟 · 线上安全采集 · 优化闭环 |
| 060 | [CI/CD、Makefile、代码生成与发布](/engineering/cicd) | ★★★☆☆ | 45 分钟 | Makefile 与脚本 · GitHub Actions · go generate 与代码生成 · 版本与 changelog · 制品与回滚 |

## 阶段六 · 服务端与数据

**阶段目标**：独立交付一个生产可用的后端服务与数据访问层

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [服务端与数据阶段导览](/server/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 061 | [net/http 深入：Handler、ServeMux 与中间件链](/server/net-http) | ★★★☆☆ | 45 分钟 | Handler 接口 · ServeMux 路由 · Request/ResponseWriter · 中间件洋葱模型 · 超时与优雅退出 |
| 062 | [路由与框架：Gin / Echo / Chi](/server/web-framework) | ★★★☆☆ | 40 分钟 | 路由树与分组 · 绑定与校验 · 中间件生态 · 三者对比 · 性能不是唯一指标 |
| 063 | [REST API 设计、校验与 OpenAPI](/server/rest-api) | ★★★☆☆ | 45 分钟 | 资源与状态码 · 幂等性 · 分页与过滤 · 参数校验 · OpenAPI 与文档生成 · 版本策略 |
| 064 | [WebSocket / SSE 与实时推送](/server/websocket) | ★★★★☆ | 45 分钟 | 协议握手 · gorilla/websocket · 心跳与断线重连 · SSE 取舍 · 广播与连接管理 |
| 065 | [database/sql、连接池与事务](/server/database-sql) | ★★★☆☆ | 45 分钟 | 驱动与 DSN · 连接池参数 · Query/Exec 选择 · 事务与隔离级别 · NULL 与扫描 |
| 066 | [GORM / sqlx / sqlc 与迁移](/server/orm) | ★★★☆☆ | 45 分钟 | 三者对比 · 模型与关联 · 预加载与 N+1 · 迁移工具 · SQL 可观测性 |
| 067 | [Redis 缓存：穿透 / 击穿 / 雪崩与一致性](/server/redis) | ★★★★☆ | 50 分钟 | 缓存模式 · 三大经典问题 · TTL 抖动 · 双写一致 · 分布式锁初探 · 热 key |
| 068 | [消息队列：Kafka / NATS / 可靠投递](/server/mq) | ★★★★☆ | 50 分钟 | 队列与发布订阅 · 分区与顺序 · 确认与重试 · 幂等消费 · 事务消息 · 选型对比 |
| 069 | [Elasticsearch 与倒排索引](/server/search) | ★★★★☆ | 45 分钟 | 倒排索引 · 分词与 mapping · 查询 DSL · 写入与刷新 · 数据同步与异构 |
| 070 | [认证授权：Session / JWT / OAuth2 / RBAC](/server/auth) | ★★★★☆ | 50 分钟 | 认证与授权分离 · Session 与 JWT · 令牌刷新与吊销 · OAuth2 流程 · RBAC 与权限模型 · 常见漏洞 |
| 071 | [文件、对象存储与断点续传](/server/file-storage) | ★★★☆☆ | 45 分钟 | io.Reader/Writer · 分块与秒传 · 对象存储直传 · 校验与重试 · 临时文件清理 |

## 阶段七 · RPC 与微服务

**阶段目标**：理解并自建微服务基础设施，而不是只会调框架 API

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [RPC 与微服务阶段导览](/microservice/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 072 | [RPC 原理与自研最小 RPC](/microservice/rpc-basics) | ★★★★☆ | 50 分钟 | 远程调用的本质 · 协议与序列化 ·  stub 与编解码 · 超时与错误 · 最小可用实现 |
| 073 | [Protobuf 与 gRPC 四种流](/microservice/protobuf) | ★★★☆☆ | 45 分钟 | proto 语法与默认值 · 代码生成 · 一元与三种流 · 兼容性规则 · 版本演进 |
| 074 | [gRPC 拦截器、metadata、错误与超时](/microservice/grpc-advanced) | ★★★★☆ | 50 分钟 | Unary/Stream 拦截器 · metadata 透传 · status 错误码 · Deadline 传播 · 认证与日志 |
| 075 | [服务注册发现与健康检查](/microservice/discovery) | ★★★★☆ | 45 分钟 | 注册与发现模型 · 心跳与摘除 · Consul/etcd/Nacos · 健康检查 · 客户端与服务端发现 |
| 076 | [负载均衡与 resolver 源码](/microservice/load-balance) | ★★★★☆ | 45 分钟 | resolver/balancer 接口 · pick_first 与 round_robin · 策略对比 · 自定义实现 · 一致性哈希 |
| 077 | [配置中心与动态配置](/microservice/config-center) | ★★★☆☆ | 40 分钟 | 配置模型与命名空间 · 长轮询/监听 · 热更新与回滚 · 灰度与审计 · 客户端容错 |
| 078 | [API 网关、鉴权与限流](/microservice/gateway) | ★★★★☆ | 50 分钟 | 网关职责 · 路由与协议转换 · 统一鉴权 · 限流算法 · Kong/APISIX 对比 · 自研轻量网关 |
| 079 | [熔断、降级、重试与雪崩防护](/microservice/resilience) | ★★★★☆ | 50 分钟 | 超时与重试预算 · 熔断器状态机 · 降级与兜底 · 隔离与舱壁 · 雪崩推演 |
| 080 | [OpenTelemetry 链路追踪](/microservice/tracing) | ★★★★☆ | 50 分钟 | Trace/Span/Context · 注入与提取 · 采样策略 · gRPC/HTTP 集成 · 与日志关联 |
| 081 | [Prometheus / Grafana 与 RED 指标](/microservice/metrics) | ★★★☆☆ | 45 分钟 | Counter/Gauge/Histogram · RED/USE 方法 · PromQL 入门 · 自定义指标 · 告警规则 |
| 082 | [自研微服务框架：从 0 到 1](/microservice/framework) | ★★★★★ | 60 分钟 | 框架边界与需求 · Options 装配 · 服务生命周期 · 统一错误与日志 · 可观测内建 · 与 kratos/go-zero 对照 |

## 阶段八 · 分布式与云原生

**阶段目标**：从单机思维升级到分布式与云原生思维，能担责稳定性

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [分布式云原生阶段导览](/distributed/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 083 | [CAP / BASE 与一致性模型](/distributed/cap-base) | ★★★★☆ | 45 分钟 | CAP 的准确含义 · 一致性强弱谱系 · BASE 与最终一致 · 分区与容错 · 设计取舍案例 |
| 084 | [分布式锁、幂等与去重](/distributed/distributed-lock) | ★★★★☆ | 50 分钟 | SETNX 与租约 · 看门狗与续期 · Redlock 争议 · 幂等键设计 · 去重表与状态机 |
| 085 | [分布式事务：2PC / TCC / Saga / 消息最终一致](/distributed/distributed-tx) | ★★★★★ | 60 分钟 | 本地事务与 ACID · 2PC 与 XA · TCC 三阶段 · Saga 补偿 · 可靠消息与本地消息表 · DTM/Seata |
| 086 | [分布式 ID、时钟与雪花算法](/distributed/distributed-id) | ★★★☆☆ | 40 分钟 | UUID 与自增局限 · Snowflake 结构 · 时钟回拨 · 号段与 Leaf · 趋势递增与索引 |
| 087 | [一致性哈希、分片与路由](/distributed/sharding) | ★★★★☆ | 45 分钟 | 取模分片的问题 · 一致性哈希与虚拟节点 · 分片键选择 · 扩容与再平衡 · 热点与倾斜 |
| 088 | [Raft 共识与 etcd 实践](/distributed/raft) | ★★★★★ | 55 分钟 | Leader 选举 · 日志复制 · 安全性与脑裂 · 成员变更 · etcd 读写与 watch · 选主实战 |
| 089 | [高可用：多活、容灾与混沌工程](/distributed/high-availability) | ★★★★☆ | 50 分钟 | 冗余与故障域 · 多活架构 · 容量与限流 · 演练与注入 · 恢复预案与复盘 |
| 090 | [容器化：多阶段构建与镜像瘦身](/distributed/docker) | ★★★☆☆ | 45 分钟 | 静态编译与 CGO_ENABLED · 多阶段 Dockerfile · distroless/scratch · 层缓存 · 非 root 与安全 |
| 091 | [Kubernetes 部署与服务暴露](/distributed/kubernetes) | ★★★★☆ | 55 分钟 | Pod/Deployment/Service · ConfigMap/Secret · 探针与优雅退出 · 资源配额 · Ingress 与 HPA |
| 092 | [Helm / GitOps / 灰度发布](/distributed/gitops) | ★★★★☆ | 50 分钟 | Helm Chart 结构 · GitOps 与 Argo CD · 滚动/蓝绿/金丝雀 · 配置与密钥管理 · 回滚演练 |
| 093 | [SRE：SLO、告警、容量与稳定性治理](/distributed/sre) | ★★★★☆ | 50 分钟 | SLI/SLO/错误预算 · 告警降噪 · 容量规划 · 压测与预案 · On-call 与复盘文化 |

## 阶段九 · 资深进阶与源码

**阶段目标**：具备源码级理解、极致性能调优与架构演进能力

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [资深进阶阶段导览](/advanced/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 094 | [runtime 源码：进程启动与初始化](/advanced/runtime-startup) | ★★★★★ | 55 分钟 | 入口汇编 · 参数与栈初始化 · schedinit · 创建 main goroutine · mstart 与调度启动 |
| 095 | [调度器源码：schedule / findRunnable / sysmon](/advanced/runtime-scheduler) | ★★★★★ | 60 分钟 | schedule 主循环 · findRunnable 顺序 · 窃取与全局队列 · sysmon 与抢占 · 关键字段速查 |
| 096 | [GC 源码与真实调优案例](/advanced/runtime-gc) | ★★★★★ | 60 分钟 | 触发条件 · 后台标记 worker · 辅助标记与屏障 · 调步算法 · GOGC/MemoryLimit 实战 |
| 097 | [channel / mutex 源码与锁竞争](/advanced/runtime-sync) | ★★★★★ | 55 分钟 | chansend/chanrecv · selectgo · mutex 状态机 · 饥饿模式与自旋 · 锁竞争定位 |
| 098 | [编译器源码：SSA、逃逸与内联](/advanced/compiler-ssa) | ★★★★★ | 55 分钟 | 编译前端与 IR · SSA 构造与优化 pass · 内联预算 · 逃逸分析实现 · 查看与验证 |
| 099 | [Go 内存模型与 happens-before 精解](/advanced/memory-model) | ★★★★★ | 50 分钟 | 官方内存模型定义 · 同步原语的保证 · 数据竞争的形式化 · 错误示例剖析 · 与硬件内存序关系 |
| 100 | [极致性能优化：从 pprof 到 30w QPS](/advanced/perf-case) | ★★★★★ | 60 分钟 | 建立基线 · 定位 CPU/分配/锁 · 逐项优化与验证 · GC 与内存换 CPU · 防止回归 |
| 101 | [架构设计：模块拆分、演进与重构](/advanced/architecture) | ★★★★★ | 55 分钟 | 识别变化方向 · 模块与边界 · 分层与依赖倒置 · 绞杀者重构 · 架构文档与决策记录 |
| 102 | [资深能力图谱、技术选型与持续精进](/advanced/senior-path) | ★★★☆☆ | 40 分钟 | 能力四维模型 · 技术选型方法 · 影响力与协作 · 学习路径 · 开源与输出 |

## 阶段十 · 工程实战

**阶段目标**：把前十个阶段的知识在 8 类真实场景中反复用熟

| 讲次 | 标题 | 难度 | 建议时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 导览 | [工程实战阶段导览](/projects/) | ★ | 10 分钟 | 阶段地图与前置说明 |
| 103 | [实战 01 · 待办清单 CLI](/projects/cli-todo) | ★☆☆☆☆ | 50 分钟 | cobra 命令树 · JSON 持久化 · 错误与退出码 · 单元测试 · 发布与安装 |
| 104 | [实战 02 · protoc 插件与代码生成器](/projects/cli-codegen) | ★★★☆☆ | 60 分钟 | protoreflect · text/template · go/ast 解析 · 生成与格式化 · 错误码注册案例 |
| 105 | [实战 03 · 短链服务](/projects/net-shortlink) | ★★☆☆☆ | 55 分钟 | 发号与 Base62 · 缓存与 301/302 · 防刷与过期 · 数据模型 · 压测 |
| 106 | [实战 04 · 图片服务与对象存储](/projects/net-image-server) | ★★★☆☆ | 60 分钟 | multipart 与流式读 · 图片处理 · ETag 与缓存 · 对象存储直传 · 限流与清理 |
| 107 | [实战 05 · 日志分析 ETL 管道](/projects/data-log-etl) | ★★★☆☆ | 60 分钟 | 逐行读取与解析 · 扇出扇入聚合 · 错误与重试 · 窗口统计 · 落库与背压 |
| 108 | [实战 06 · CSV / SQL 查询工具](/projects/data-sql-tool) | ★★☆☆☆ | 55 分钟 | 表达式与 AST · 执行计划 · 多数据源适配 · 流式结果 · 测试策略 |
| 109 | [实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler) | ★★★★☆ | 70 分钟 | 单线程版 · worker pool 并发版 · 去重与限速 · 队列化分布式版 · 演进对比 |
| 110 | [实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler) | ★★★★☆ | 70 分钟 | 任务模型与 cron · etcd 选主 · 租约与故障转移 · 执行器与日志 · 分片与幂等 |
| 111 | [KV 贯穿线 ① 内存版与接口抽象](/projects/kv-1-memory) | ★★☆☆☆ | 55 分钟 | Command 与 Storage 接口 · 内存实现 · 错误模型 · 基准测试 · 可扩展点 |
| 112 | [KV 贯穿线 ② TCP 协议帧与网络层](/projects/kv-2-network) | ★★★☆☆ | 60 分钟 | 协议帧设计 · 粘包与编解码 · 连接管理 · 并发处理 · 客户端 SDK |
| 113 | [KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async) | ★★★★☆ | 70 分钟 | WAL 追加写 · 崩溃恢复 · 锁分片 · 快照 · 一致性校验 |
| 114 | [KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) | ★★★★★ | 90 分钟 | 分片路由 · 副本复制 · Raft 集成 · 读写一致性 · 成员变更与压测 |
| 115 | [实战 09 · 电商微服务系统](/projects/micro-ecommerce) | ★★★★☆ | 90 分钟 | 服务拆分与契约 · 用户/商品/订单/库存 · gRPC + HTTP 网关 · 分布式事务 · 搜索与联调 |
| 116 | [实战 10 · 秒杀与高并发库存](/projects/micro-seckill) | ★★★★★ | 80 分钟 | 流量削峰 · 预扣减与最终一致 · 限流与排队 · 热点账户 · 压测与预案 |
| 117 | [实战 11 · Kubernetes Operator 与可观测平台](/projects/cloud-operator) | ★★★★☆ | 75 分钟 | CRD 与控制器 · Reconcile 循环 · 指标与日志聚合 · 告警规则 · 发布与运维 |
| 118 | [实战 12 · LLM 网关与流式推理管道](/projects/ai-gateway) | ★★★★☆ | 70 分钟 | 多供应商适配 · SSE 流式转发 · 令牌与限流 · 超时与重试 · 成本与可观测 |

## 练习与速查

| 页面 | 用途 |
| --- | --- |
| [综合自测](/practice/) | 跨阶段混合题，检验是否真的串起来了 |
| [语法速查表](/practice/cheatsheet) | 一页纸查语法、内建函数与标准库 |
| [常见坑与反模式](/practice/pitfalls) | 高频踩坑清单与修正写法 |
| [零基础常见问题](/practice/faq) | 环境、报错、概念困惑速查 |
| [面试与能力自评](/practice/interview) | 按能力维度自评 + 常见考点 |
| [Go 生态与选型地图](/practice/ecosystem) | 框架、库、工具的分类与选型建议 |

<ProgressTracker />
