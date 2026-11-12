// 由 .workbuddy/gen_course.py 生成；进度相关的函数保持手写。
/** 学习进度：全部保存在浏览器本地，SSR 安全 */
import { withBase } from 'vitepress'

export type Stage = 'L0' | 'L1' | 'L2' | 'L3' | 'L4' | 'L5' | 'L6' | 'L7' | 'L8' | 'L9' | 'L10'

export interface Lesson { path: string; title: string; stage: Stage }

/**
 * 可打卡页面清单：11 个阶段导览 + 118 讲 + 附录页。
 * 新增一页时，在 .workbuddy/gen_course.py 的 LESSONS 里补一条并重新运行，
 * 侧栏、进度、路线图与总目录会一起更新，不会出现对不上的情况。
 */
export const LESSONS: Lesson[] = [
  { path: '/guide/', title: '阶段导览 · 导学', stage: 'L0' },
  { path: '/foundation/', title: '阶段导览 · 编程地基', stage: 'L1' },
  { path: '/basics/', title: '阶段导览 · 语法核心', stage: 'L2' },
  { path: '/core/', title: '阶段导览 · 核心机制', stage: 'L3' },
  { path: '/concurrency/', title: '阶段导览 · 并发编程', stage: 'L4' },
  { path: '/engineering/', title: '阶段导览 · 工程化', stage: 'L5' },
  { path: '/server/', title: '阶段导览 · 服务端与数据', stage: 'L6' },
  { path: '/microservice/', title: '阶段导览 · RPC 与微服务', stage: 'L7' },
  { path: '/distributed/', title: '阶段导览 · 分布式云原生', stage: 'L8' },
  { path: '/advanced/', title: '阶段导览 · 资深进阶', stage: 'L9' },
  { path: '/projects/', title: '阶段导览 · 工程实战', stage: 'L10' },
  { path: '/guide/opening', title: '第 001 讲 开篇：Go 是什么，为什么值得学', stage: 'L0' },
  { path: '/guide/setup', title: '第 002 讲 装好环境：工具链、工作区与 Modules', stage: 'L0' },
  { path: '/guide/hello', title: '第 003 讲 第一行 Go 代码：从源码到可执行文件', stage: 'L0' },
  { path: '/guide/toolchain', title: '第 004 讲 工具链全景：build / test / fmt / vet / mod', stage: 'L0' },
  { path: '/foundation/concepts', title: '第 005 讲 程序、数据、值与类型', stage: 'L1' },
  { path: '/foundation/variables', title: '第 006 讲 变量、常量、iota 与作用域', stage: 'L1' },
  { path: '/foundation/numeric', title: '第 007 讲 数值、布尔、字符与类型转换', stage: 'L1' },
  { path: '/foundation/strings', title: '第 008 讲 字符串、rune 与高效拼接', stage: 'L1' },
  { path: '/foundation/memory', title: '第 009 讲 栈、堆、值语义与指针', stage: 'L1' },
  { path: '/foundation/callables', title: '第 010 讲 函数、多返回值、变参与闭包', stage: 'L1' },
  { path: '/foundation/packages', title: '第 011 讲 包、模块与依赖管理', stage: 'L1' },
  { path: '/foundation/capstone', title: '第 012 讲 阶段综合：CLI 单位换算器', stage: 'L1' },
  { path: '/basics/arrays-slices', title: '第 013 讲 数组与切片：定义与操作', stage: 'L2' },
  { path: '/basics/control-flow', title: '第 014 讲 控制流：if / for / switch / goto', stage: 'L2' },
  { path: '/basics/range-pitfalls', title: '第 015 讲 for range 与闭包捕获陷阱', stage: 'L2' },
  { path: '/basics/maps', title: '第 016 讲 map：哈希、遍历与 Set 技巧', stage: 'L2' },
  { path: '/basics/structs', title: '第 017 讲 结构体、标签与内存对齐', stage: 'L2' },
  { path: '/basics/methods', title: '第 018 讲 方法与接收者：值还是指针', stage: 'L2' },
  { path: '/basics/interfaces', title: '第 019 讲 接口：隐式实现与组合', stage: 'L2' },
  { path: '/basics/errors', title: '第 020 讲 错误处理：error、包装与错误链', stage: 'L2' },
  { path: '/basics/defer-panic', title: '第 021 讲 defer / panic / recover', stage: 'L2' },
  { path: '/basics/generics', title: '第 022 讲 泛型：类型参数与约束', stage: 'L2' },
  { path: '/basics/reflect', title: '第 023 讲 反射 reflect 与 go generate', stage: 'L2' },
  { path: '/basics/json', title: '第 024 讲 JSON、序列化与常见坑', stage: 'L2' },
  { path: '/basics/capstone', title: '第 025 讲 阶段综合：内存版 KV 存储', stage: 'L2' },
  { path: '/core/value-semantics', title: '第 026 讲 值语义深潜：赋值、传参、拷贝成本', stage: 'L3' },
  { path: '/core/escape-analysis', title: '第 027 讲 逃逸分析与编译器决策', stage: 'L3' },
  { path: '/core/slice-internals', title: '第 028 讲 切片底层：三元组、扩容与共享陷阱', stage: 'L3' },
  { path: '/core/map-internals', title: '第 029 讲 map 底层：hmap、桶与渐进扩容', stage: 'L3' },
  { path: '/core/interface-internals', title: '第 030 讲 interface 底层：iface / eface 与 nil 陷阱', stage: 'L3' },
  { path: '/core/string-internals', title: '第 031 讲 字符串底层与零拷贝转换', stage: 'L3' },
  { path: '/core/allocator', title: '第 032 讲 内存分配器：mcache / mcentral / mheap', stage: 'L3' },
  { path: '/core/gc', title: '第 033 讲 垃圾回收：三色标记、屏障与 GOGC', stage: 'L3' },
  { path: '/core/defer-internals', title: '第 034 讲 defer 实现与 open-coded 优化', stage: 'L3' },
  { path: '/core/startup', title: '第 035 讲 程序启动、init 顺序与包初始化', stage: 'L3' },
  { path: '/core/compiler', title: '第 036 讲 编译、链接、SSA 与内联', stage: 'L3' },
  { path: '/core/unsafe-cgo', title: '第 037 讲 unsafe、cgo 与跨边界成本', stage: 'L3' },
  { path: '/concurrency/concurrency-intro', title: '第 038 讲 并发与并行：CSP 思想', stage: 'L4' },
  { path: '/concurrency/goroutine', title: '第 039 讲 goroutine：创建、栈与生命周期', stage: 'L4' },
  { path: '/concurrency/gmp', title: '第 040 讲 GMP 调度模型全景', stage: 'L4' },
  { path: '/concurrency/scheduler', title: '第 041 讲 调度循环：抢占、窃取与 handoff', stage: 'L4' },
  { path: '/concurrency/channel', title: '第 042 讲 channel：语义、缓冲与关闭', stage: 'L4' },
  { path: '/concurrency/hchan', title: '第 043 讲 channel 底层 hchan 与收发队列', stage: 'L4' },
  { path: '/concurrency/select', title: '第 044 讲 select、超时与多路复用', stage: 'L4' },
  { path: '/concurrency/sync-primitives', title: '第 045 讲 sync：Mutex / RWMutex / WaitGroup / Once / Cond', stage: 'L4' },
  { path: '/concurrency/atomic-pool', title: '第 046 讲 atomic 与 sync.Pool', stage: 'L4' },
  { path: '/concurrency/context', title: '第 047 讲 context：取消、超时、值传递', stage: 'L4' },
  { path: '/concurrency/patterns', title: '第 048 讲 并发模式：pipeline / fan-out / worker pool', stage: 'L4' },
  { path: '/concurrency/race', title: '第 049 讲 数据竞争、内存模型与 race detector', stage: 'L4' },
  { path: '/engineering/project-layout', title: '第 050 讲 项目结构与分层设计', stage: 'L5' },
  { path: '/engineering/code-style', title: '第 051 讲 代码规范、gofmt、vet 与 golangci-lint', stage: 'L5' },
  { path: '/engineering/unit-test', title: '第 052 讲 单元测试与表驱动测试', stage: 'L5' },
  { path: '/engineering/mock-test', title: '第 053 讲 Mock：gomock / sqlmock / httptest', stage: 'L5' },
  { path: '/engineering/bench-fuzz', title: '第 054 讲 基准测试、模糊测试与覆盖率', stage: 'L5' },
  { path: '/engineering/logging', title: '第 055 讲 日志：slog / zap 与结构化采样', stage: 'L5' },
  { path: '/engineering/config', title: '第 056 讲 配置：flag / env / viper / 配置中心', stage: 'L5' },
  { path: '/engineering/error-design', title: '第 057 讲 错误码、错误栈与统一响应', stage: 'L5' },
  { path: '/engineering/patterns-design', title: '第 058 讲 设计模式与 Functional Options / wire', stage: 'L5' },
  { path: '/engineering/profiling', title: '第 059 讲 性能剖析：pprof / trace / 火焰图', stage: 'L5' },
  { path: '/engineering/cicd', title: '第 060 讲 CI/CD、Makefile、代码生成与发布', stage: 'L5' },
  { path: '/server/net-http', title: '第 061 讲 net/http 深入：Handler、ServeMux 与中间件链', stage: 'L6' },
  { path: '/server/web-framework', title: '第 062 讲 路由与框架：Gin / Echo / Chi', stage: 'L6' },
  { path: '/server/rest-api', title: '第 063 讲 REST API 设计、校验与 OpenAPI', stage: 'L6' },
  { path: '/server/websocket', title: '第 064 讲 WebSocket / SSE 与实时推送', stage: 'L6' },
  { path: '/server/database-sql', title: '第 065 讲 database/sql、连接池与事务', stage: 'L6' },
  { path: '/server/orm', title: '第 066 讲 GORM / sqlx / sqlc 与迁移', stage: 'L6' },
  { path: '/server/redis', title: '第 067 讲 Redis 缓存：穿透 / 击穿 / 雪崩与一致性', stage: 'L6' },
  { path: '/server/mq', title: '第 068 讲 消息队列：Kafka / NATS / 可靠投递', stage: 'L6' },
  { path: '/server/search', title: '第 069 讲 Elasticsearch 与倒排索引', stage: 'L6' },
  { path: '/server/auth', title: '第 070 讲 认证授权：Session / JWT / OAuth2 / RBAC', stage: 'L6' },
  { path: '/server/file-storage', title: '第 071 讲 文件、对象存储与断点续传', stage: 'L6' },
  { path: '/microservice/rpc-basics', title: '第 072 讲 RPC 原理与自研最小 RPC', stage: 'L7' },
  { path: '/microservice/protobuf', title: '第 073 讲 Protobuf 与 gRPC 四种流', stage: 'L7' },
  { path: '/microservice/grpc-advanced', title: '第 074 讲 gRPC 拦截器、metadata、错误与超时', stage: 'L7' },
  { path: '/microservice/discovery', title: '第 075 讲 服务注册发现与健康检查', stage: 'L7' },
  { path: '/microservice/load-balance', title: '第 076 讲 负载均衡与 resolver 源码', stage: 'L7' },
  { path: '/microservice/config-center', title: '第 077 讲 配置中心与动态配置', stage: 'L7' },
  { path: '/microservice/gateway', title: '第 078 讲 API 网关、鉴权与限流', stage: 'L7' },
  { path: '/microservice/resilience', title: '第 079 讲 熔断、降级、重试与雪崩防护', stage: 'L7' },
  { path: '/microservice/tracing', title: '第 080 讲 OpenTelemetry 链路追踪', stage: 'L7' },
  { path: '/microservice/metrics', title: '第 081 讲 Prometheus / Grafana 与 RED 指标', stage: 'L7' },
  { path: '/microservice/framework', title: '第 082 讲 自研微服务框架：从 0 到 1', stage: 'L7' },
  { path: '/distributed/cap-base', title: '第 083 讲 CAP / BASE 与一致性模型', stage: 'L8' },
  { path: '/distributed/distributed-lock', title: '第 084 讲 分布式锁、幂等与去重', stage: 'L8' },
  { path: '/distributed/distributed-tx', title: '第 085 讲 分布式事务：2PC / TCC / Saga / 消息最终一致', stage: 'L8' },
  { path: '/distributed/distributed-id', title: '第 086 讲 分布式 ID、时钟与雪花算法', stage: 'L8' },
  { path: '/distributed/sharding', title: '第 087 讲 一致性哈希、分片与路由', stage: 'L8' },
  { path: '/distributed/raft', title: '第 088 讲 Raft 共识与 etcd 实践', stage: 'L8' },
  { path: '/distributed/high-availability', title: '第 089 讲 高可用：多活、容灾与混沌工程', stage: 'L8' },
  { path: '/distributed/docker', title: '第 090 讲 容器化：多阶段构建与镜像瘦身', stage: 'L8' },
  { path: '/distributed/kubernetes', title: '第 091 讲 Kubernetes 部署与服务暴露', stage: 'L8' },
  { path: '/distributed/gitops', title: '第 092 讲 Helm / GitOps / 灰度发布', stage: 'L8' },
  { path: '/distributed/sre', title: '第 093 讲 SRE：SLO、告警、容量与稳定性治理', stage: 'L8' },
  { path: '/advanced/runtime-startup', title: '第 094 讲 runtime 源码：进程启动与初始化', stage: 'L9' },
  { path: '/advanced/runtime-scheduler', title: '第 095 讲 调度器源码：schedule / findRunnable / sysmon', stage: 'L9' },
  { path: '/advanced/runtime-gc', title: '第 096 讲 GC 源码与真实调优案例', stage: 'L9' },
  { path: '/advanced/runtime-sync', title: '第 097 讲 channel / mutex 源码与锁竞争', stage: 'L9' },
  { path: '/advanced/compiler-ssa', title: '第 098 讲 编译器源码：SSA、逃逸与内联', stage: 'L9' },
  { path: '/advanced/memory-model', title: '第 099 讲 Go 内存模型与 happens-before 精解', stage: 'L9' },
  { path: '/advanced/perf-case', title: '第 100 讲 极致性能优化：从 pprof 到 30w QPS', stage: 'L9' },
  { path: '/advanced/architecture', title: '第 101 讲 架构设计：模块拆分、演进与重构', stage: 'L9' },
  { path: '/advanced/senior-path', title: '第 102 讲 资深能力图谱、技术选型与持续精进', stage: 'L9' },
  { path: '/projects/cli-todo', title: '第 103 讲 实战 01 · 待办清单 CLI', stage: 'L10' },
  { path: '/projects/cli-codegen', title: '第 104 讲 实战 02 · protoc 插件与代码生成器', stage: 'L10' },
  { path: '/projects/net-shortlink', title: '第 105 讲 实战 03 · 短链服务', stage: 'L10' },
  { path: '/projects/net-image-server', title: '第 106 讲 实战 04 · 图片服务与对象存储', stage: 'L10' },
  { path: '/projects/data-log-etl', title: '第 107 讲 实战 05 · 日志分析 ETL 管道', stage: 'L10' },
  { path: '/projects/data-sql-tool', title: '第 108 讲 实战 06 · CSV / SQL 查询工具', stage: 'L10' },
  { path: '/projects/concurrent-crawler', title: '第 109 讲 实战 07 · 并发爬虫：单机 → 并发 → 分布式', stage: 'L10' },
  { path: '/projects/concurrent-scheduler', title: '第 110 讲 实战 08 · 分布式任务调度系统', stage: 'L10' },
  { path: '/projects/kv-1-memory', title: '第 111 讲 KV 贯穿线 ① 内存版与接口抽象', stage: 'L10' },
  { path: '/projects/kv-2-network', title: '第 112 讲 KV 贯穿线 ② TCP 协议帧与网络层', stage: 'L10' },
  { path: '/projects/kv-3-async', title: '第 113 讲 KV 贯穿线 ③ WAL、持久化与并发控制', stage: 'L10' },
  { path: '/projects/kv-4-distributed', title: '第 114 讲 KV 贯穿线 ④ 分片、复制与 Raft', stage: 'L10' },
  { path: '/projects/micro-ecommerce', title: '第 115 讲 实战 09 · 电商微服务系统', stage: 'L10' },
  { path: '/projects/micro-seckill', title: '第 116 讲 实战 10 · 秒杀与高并发库存', stage: 'L10' },
  { path: '/projects/cloud-operator', title: '第 117 讲 实战 11 · Kubernetes Operator 与可观测平台', stage: 'L10' },
  { path: '/projects/ai-gateway', title: '第 118 讲 实战 12 · LLM 网关与流式推理管道', stage: 'L10' },
  { path: '/practice/', title: '综合自测', stage: 'L0' },
  { path: '/practice/cheatsheet', title: '语法速查表', stage: 'L0' },
  { path: '/practice/pitfalls', title: '常见坑与反模式', stage: 'L0' },
  { path: '/practice/faq', title: '零基础常见问题', stage: 'L0' },
  { path: '/practice/interview', title: '面试与能力自评', stage: 'L0' },
  { path: '/practice/ecosystem', title: 'Go 生态与选型地图', stage: 'L0' },
  { path: '/guide/curriculum', title: '课程总目录（118 讲）', stage: 'L0' },
]

export const STAGE_LABEL: Record<Stage, string> = {
  L0: '导学', L1: '编程地基', L2: '语法核心', L3: '核心机制', L4: '并发编程', L5: '工程化', L6: '服务端与数据', L7: 'RPC 与微服务', L8: '分布式云原生', L9: '资深进阶', L10: '工程实战',
}
export const STAGE_COLOR: Record<Stage, string> = {
  L0: '#868e96', L1: '#2b8a3e', L2: '#1c7ed6', L3: '#7048e8', L4: '#e8590c', L5: '#0ca678', L6: '#1c7ed6', L7: '#7048e8', L8: '#c92a2a', L9: '#495057', L10: '#c92a2a',
}

// 与同一域名下的其它教程站点共用 localStorage，键名必须区分，避免进度互相串数据。
const KEY = 'go-course-done-v1'
const CHECK_KEY = 'go-course-checks-v1'

/**
 * useRoute().path 会带部署 base（如 /go/guide/），而 LESSONS 里记的是 /guide/。
 * 统一归一化后再存储、比较，打卡才能正确计入进度。
 */
function basePrefix(): string {
  try {
    const b = withBase('/')
    return b === '/' ? '' : b.replace(/\/$/, '')
  } catch { return '' }
}

export function normPath(path: string): string {
  let p = String(path || '').replace(/\.html$/, '')
  const base = basePrefix()
  if (base && p.startsWith(base + '/')) p = p.slice(base.length)
  else if (base && p === base) p = '/'
  return p
}

export function readDone(): string[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return (Array.isArray(raw) ? raw : []).map((p) => normPath(String(p)))
  } catch { return [] }
}

export function isDone(path: string) { return readDone().includes(normPath(path)) }

export function setDone(path: string, done: boolean) {
  if (typeof window === 'undefined') return
  const key = normPath(path)
  const cur = new Set(readDone())
  done ? cur.add(key) : cur.delete(key)
  localStorage.setItem(KEY, JSON.stringify([...cur]))
  window.dispatchEvent(new CustomEvent('go-progress-change'))
}

export function readChecks(id: string): boolean[] {
  if (typeof window === 'undefined') return []
  try {
    const all = JSON.parse(localStorage.getItem(CHECK_KEY) || '{}')
    return all[id] || []
  } catch { return [] }
}

export function setChecks(id: string, arr: boolean[]) {
  if (typeof window === 'undefined') return
  let all: Record<string, boolean[]> = {}
  try { all = JSON.parse(localStorage.getItem(CHECK_KEY) || '{}') } catch { /* ignore */ }
  all[id] = arr
  localStorage.setItem(CHECK_KEY, JSON.stringify(all))
}
