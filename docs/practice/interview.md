---
title: 面试与能力自评 · 六维度考点清单
description: 按语言基础、运行时、并发、工程化、分布式、架构六个维度组织的能力自评清单与高频考点答案要点，附综合设计题解题思路。
---

# 面试与能力自评 · 六维度考点清单

> 面试考的不是「背过多少名词」，而是「能不能把机制讲清楚、把取舍说明白」。这一页把常见考点按六个维度摊开：先用清单自评，再对照答案要点查漏，最后用一道综合设计题练「从需求到架构」的完整表达。

<Callout type="idea" title="自评标准" text="对每一项问自己三句话：它是什么、为什么这样设计、我怎么验证它。三句都能讲清楚算通过；只会背定义算待补；能顺带说出踩过的坑和排查命令，就是加分项。" />

<Callout type="tip" title="怎么用这一页" text="先逐维度勾选清单，诚实标记不会的项；再看表格里的答案要点，把讲不圆的补回对应讲次；最后限时二十分钟，把综合设计题按步骤口头讲一遍。" />

## 维度一 · 语言基础

<CheckList id="interview-language" title="语言基础自评" :items='["能说清值传递与引用语义的区别", "能解释切片扩容与共享底层数组的行为", "能讲明接口隐式实现与 typed nil 陷阱", "能说出 defer 的执行顺序与参数求值时机", "能区分 %w 与 %v 在错误链上的作用"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| 一切皆值 | 赋值与传参都拷贝；切片、map、通道是含指针的描述符，拷贝的是描述符而非底层数据 | [值语义深潜](/core/value-semantics) |
| 切片扩容与别名 | 容量不足时分配新数组；append 必须接住返回值；子切片共享底层数组可能互相覆盖 | [切片底层](/core/slice-internals) |
| 接口与 nil | 方法集匹配即隐式实现；接口由类型与值组成，typed nil 的接口不等于 nil | [interface 底层](/core/interface-internals) · [接口与组合](/basics/interfaces) |
| defer | 后进先出，函数返回前执行；参数在注册时求值，闭包执行时才读外部变量 | [defer / panic](/basics/defer-panic) |
| 错误处理 | %w 保留原始错误形成错误链，errors.Is 与 errors.As 才能沿链判定与提取 | [错误处理](/basics/errors) |

<Callout type="warn" title="常见失分点" text="只说「切片是引用类型」不够准确，要说清它是「含指针的值」；只说「接口是 nil 判断有坑」不够，要能画出类型与值两部分。" />

## 维度二 · 运行时

<CheckList id="interview-runtime" title="运行时自评" :items='["能画出 G、M、P 三者职责与队列关系", "能讲清三色标记与写屏障各自解决什么", "能判断一个变量何时会逃逸到堆", "能说出内存分配器的三级缓存结构", "能解释接口动态派发的成本来源"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| GMP 模型 | G 是协程、M 是系统线程、P 是调度上下文与本地队列；P 数量默认等于 CPU 核数，空闲时会 work stealing | [GMP 调度模型](/concurrency/gmp) |
| GC | 并发三色标记清除，混合写屏障维护不变式，STW 只保留在少数阶段；GOGC 与 MemoryLimit 控制触发节奏 | [垃圾回收](/core/gc) |
| 逃逸分析 | 指针在函数返回后仍被引用、闭包捕获、过大对象等会逃逸到堆；用 go build -gcflags -m 验证 | [逃逸分析](/core/escape-analysis) |
| 分配器 | 按 size class 分级，mcache 无锁分配、mcentral 与 mheap 逐级补给，小对象还有 tiny allocator | [内存分配器](/core/allocator) |
| 接口成本 | iface 与 eface 通过 itab 查方法表，动态派发有一次间接跳转，且可能触发逃逸 | [interface 底层](/core/interface-internals) |

## 维度三 · 并发

<CheckList id="interview-concurrency" title="并发自评" :items='["能说清 channel 的关闭规则与 nil 通道行为", "能用 context 做跨 goroutine 的取消与超时", "能在 Mutex、WaitGroup、Once 之间选对原语", "能解释数据竞争与 happens-before", "能写出 worker pool 或 errgroup 这类并发结构"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| channel | 由唯一发送方关闭；关闭后仍可取完缓冲；重复关闭或向已关闭通道发送会 panic；nil 通道永久阻塞，可用于禁用 select 分支 | [channel](/concurrency/channel) |
| context | WithCancel、WithTimeout、WithDeadline 逐层派生，取消信号级联传播；值传递要克制，只放请求级元数据 | [context](/concurrency/context) |
| sync 原语 | WaitGroup 等一组任务、Mutex 保护临界区、RWMutex 读多写少、Once 做一次性初始化；含锁结构体不可拷贝 | [sync 原语](/concurrency/sync-primitives) |
| 数据竞争 | 无同步约束的并发读写是未定义行为；同步原语建立 happens-before；用 go test -race 检测 | [数据竞争](/concurrency/race) · [内存模型](/advanced/memory-model) |
| 并发模式 | pipeline 分阶段、fan-out 扇出、worker pool 控并发、errgroup 聚合错误、singleflight 防缓存击穿 | [并发模式](/concurrency/patterns) |

<Callout type="warn" title="并发题怎么答才不虚" text="别只报 API 名字，给一个具体场景：什么时候会死锁、什么时候会泄漏、用哪条命令定位。能说出 go test -race 的开销与局限，通常比背定义更有说服力。" />

## 维度四 · 工程化

<CheckList id="interview-engineering" title="工程化自评" :items='["能说出一个可维护项目的目录分层与依赖方向", "能写出表驱动测试并用 mock 隔离外部依赖", "能用结构化日志与外置配置支撑多环境", "能设计不泄漏细节的统一错误响应", "能走完 pprof 定位到验证的优化闭环"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| 项目结构 | cmd 放入口、internal 放内部实现、pkg 放可复用库；依赖方向朝内，按领域还是按层取决于团队规模 | [项目结构](/engineering/project-layout) |
| 测试与 mock | 表驱动加 t.Run 子测试；httptest、sqlmock、gomock 隔离依赖；可测试性来自依赖倒置 | [单元测试](/engineering/unit-test) · [Mock](/engineering/mock-test) |
| 日志与配置 | slog 输出结构化字段并关联 trace_id；配置外置，同一份镜像跑多套环境 | [日志](/engineering/logging) · [配置](/engineering/config) |
| 错误设计 | 内部分类加错误码，映射到 HTTP 状态或 gRPC status，对外不泄露堆栈与敏感细节 | [错误设计](/engineering/error-design) |
| 性能与发布 | 先建 Benchmark 基线，pprof 定位 CPU 与分配，优化后用 benchstat 验证；CI 里跑 vet 与 lint | [性能剖析](/engineering/profiling) · [CI/CD](/engineering/cicd) |

## 维度五 · 分布式

<CheckList id="interview-distributed" title="分布式自评" :items='["能准确解释 CAP 里的 P 为什么不是可选项", "能设计幂等键与去重表防重复消费", "能说清分布式锁的租约与看门狗", "能对比 2PC、TCC、Saga 与消息最终一致", "能讲出 Raft 选主与日志复制的关键约束"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| CAP 与 BASE | 分区是客观前提，发生时在强一致与可用性之间取舍；BASE 用软状态换最终一致 | [CAP 与一致性](/distributed/cap-base) |
| 幂等 | 业务唯一键配合去重表或状态机，让重复请求不产生副作用，优于依赖基础设施的恰好一次 | [分布式锁与幂等](/distributed/distributed-lock) |
| 分布式锁 | SETNX 加租约与唯一持有者标识，看门狗续期防过期误删，释放要校验持有者 | [分布式锁](/distributed/distributed-lock) · [Redis](/server/redis) |
| 分布式事务 | 2PC 强一致但阻塞，TCC 与 Saga 靠补偿，本地消息表走可靠消息最终一致，按业务容忍度选 | [分布式事务](/distributed/distributed-tx) |
| 共识与 ID | Raft 靠任期、多数派与日志复制保证安全；Snowflake 用时间戳加机器号生成趋势递增 ID | [Raft 与 etcd](/distributed/raft) · [分布式 ID](/distributed/distributed-id) |

## 维度六 · 架构

<CheckList id="interview-architecture" title="架构自评" :items='["能用依赖倒置划清模块边界", "能说明服务拆分依据与过度拆分的代价", "能设计冗余、限流与降级的高可用方案", "能用 RED 与 SLO 描述系统健康度", "能讲清渐进式重构与决策记录的价值"]' />

| 高频考点 | 答案要点 | 回看 |
| --- | --- | --- |
| 分层与边界 | 依赖朝抽象与稳定方向，接口由使用方定义；识别变化方向再拆模块 | [架构设计](/advanced/architecture) |
| 网关与服务治理 | 统一入口承担路由、鉴权、限流与协议转换，避免每个服务各做一套 | [API 网关](/microservice/gateway) · [熔断降级](/microservice/resilience) |
| 高可用 | 冗余与故障域隔离、容量规划、限流降级、演练与预案，面向故障设计 | [高可用与容灾](/distributed/high-availability) |
| 可观测性 | RED 指标量化请求、追踪串起跨服务调用、SLO 与错误预算指导发布节奏 | [Prometheus 指标](/microservice/metrics) · [SRE](/distributed/sre) |
| 演进与成长 | 绞杀者模式渐进替换旧系统，架构决策记录保留取舍依据 | [资深能力图谱](/advanced/senior-path) |

## 综合设计题 · 高并发短链服务

**题目**：设计一个短链服务，日均新增约 100 万条映射，读写比约 100 比 1，要求跳转延迟低、可水平扩展，并说明如何防刷与保证稳定。

答题时先澄清约束（峰值 QPS、是否要自定义短码、过期策略、SLA），再按下面的顺序推进。面试官看的是分层是否清晰、每一步有没有取舍依据，而不是背出一个「标准答案」。

<FlowTimeline title="从需求到落地的八步推演" direction="vertical" :steps='[{"title":"澄清需求与约束","desc":"问清峰值读写 QPS、短码是否可自定义、过期与统计需求、可用性目标","tone":"blue"},{"title":"容量估算","desc":"按读写比估算峰值读 QPS、日增存储量与缓存容量，反推分片与机器规模","tone":"blue"},{"title":"接口与数据模型","desc":"只暴露生成与跳转两个核心接口，定义长链到短码的映射与索引","tone":"green"},{"title":"发号与短码","desc":"用分布式 ID 加 Base62 生成，避免随机碰撞，保证趋势递增利于索引","tone":"green"},{"title":"缓存与读路径","desc":"热点映射放 Redis，权衡 301 与 302，处理穿透、击穿与 TTL 抖动","tone":"orange"},{"title":"存储与分片","desc":"按短码分片、冷热分离、过期清理，预留扩容与再平衡方案","tone":"orange"},{"title":"防护与稳定","desc":"网关限流防刷、超时与降级、优雅退出，避免单点与雪崩","tone":"purple"},{"title":"可观测与压测","desc":"接入 RED 指标与链路追踪，做全链路压测并准备容量预案","tone":"red"}]' />

| 推演环节 | 主要讲次 |
| --- | --- |
| 接口与幂等 | [REST API 设计](/server/rest-api) · [短链服务实战](/projects/net-shortlink) |
| 发号与 ID | [分布式 ID](/distributed/distributed-id) |
| 缓存三大问题 | [Redis 缓存](/server/redis) |
| 分片与扩容 | [一致性哈希与分片](/distributed/sharding) |
| 限流与降级 | [API 网关](/microservice/gateway) · [熔断降级](/microservice/resilience) |
| 指标与稳定性 | [Prometheus 指标](/microservice/metrics) · [SRE](/distributed/sre) |

<Quiz no="INT-1" q="被问到 GC 如何做到低停顿时，下面哪一句回答最贴切？" :options='["Go 使用引用计数，对象不用了立刻回收，所以没有停顿", "Go 用并发三色标记清除，大部分标记与业务并发执行，靠写屏障维护正确性，STW 只留很短阶段", "Go 每次 GC 都全程暂停，靠调大 GOGC 把次数降到最少", "停顿由编译器在编译期完全消除，运行期不再回收内存"]' answer="1" explain="低停顿来自「标记与用户代码并发 + 混合写屏障 + 极短的 STW 阶段」，而不是没有停顿；GOGC 调的是触发频率，不是单次停顿时长。回看垃圾回收一讲。" />

<KnowledgeCard :points='["自评按是什么、为什么、怎么验证三句来答", "六个维度均衡，别只准备语法题", "分布式题先澄清约束再分层展开", "设计题要给出取舍依据与验证手段"]' :pitfalls='["把切片笼统说成引用类型而讲不清描述符", "只会背 CAP 三个字母，说不出分区时的取舍", "设计题跳过容量估算直接报技术栈"]' summary="能把一个机制讲给完全不懂的人听明白，才是真会。" />

<Callout type="tip" title="继续准备" text="把不会的项按维度记下来，顺着表格里的回看链接补完，再回到综合自测检验是否真的记住。六维度都勾满之后，试着把短链设计题换成一个自己熟悉的系统重新讲一遍。" />

## 相关页面

[综合自测](/practice/) · [语法速查表](/practice/cheatsheet) · [常见坑与反模式](/practice/pitfalls) · [零基础常见问题](/practice/faq) · [Go 生态与选型地图](/practice/ecosystem) · [资深能力图谱](/advanced/senior-path)
