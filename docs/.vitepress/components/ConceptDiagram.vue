<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue'
import ConceptDiagramSvg from './ConceptDiagramSvg.vue'

interface Node { x: number; y: number; w: number; h: number; label: string; sub?: string; tone?: string }
interface Arrow { x1: number; y1: number; x2: number; y2: number; label?: string }
interface Diagram { h: number; nodes: Node[]; arrows: Arrow[]; caption?: string }

const TONES: Record<string, string> = {
  orange: 'var(--go-brand)', blue: 'var(--go-blue)', green: 'var(--go-green)',
  purple: 'var(--go-purple)', red: 'var(--go-red)', gray: 'var(--go-muted)',
}

/**
 * 概念图数据与布局函数：由 .workbuddy/gen_assets.py 注入。
 */
type N = { label: string; sub?: string; tone?: string }

/** 横向流程：n 个节点等宽排列，自动补箭头。 */
function row(items: N[], caption: string, boxH = 62): Diagram {
  const n = items.length
  const gap = n > 1 ? 30 : 0
  const w = Math.min(158, Math.floor((640 - gap * (n - 1)) / n))
  const y = 48
  const total = n * w + (n - 1) * gap
  const x0 = Math.round((680 - total) / 2)
  const nodes: Node[] = items.map((it, i) => ({ x: x0 + i * (w + gap), y, w, h: boxH, ...it }))
  const arrows: Arrow[] = []
  for (let i = 0; i < n - 1; i++) {
    arrows.push({ x1: nodes[i].x + w, y1: y + boxH / 2, x2: nodes[i + 1].x, y2: y + boxH / 2 })
  }
  return { h: y + boxH + 46, nodes, arrows, caption }
}

/** 纵向分层：每层横排，层与层之间用一条箭头串起来。 */
function layers(rows: N[][], caption: string): Diagram {
  const boxH = 58, rowGap = 42, y0 = 38
  const nodes: Node[] = [], arrows: Arrow[] = []
  rows.forEach((r, ri) => {
    const n = r.length, gap = 24
    const w = Math.min(200, Math.floor((640 - gap * (n - 1)) / n))
    const total = n * w + (n - 1) * gap
    const x0 = Math.round((680 - total) / 2)
    const y = y0 + ri * (boxH + rowGap)
    r.forEach((it, i) => nodes.push({ x: x0 + i * (w + gap), y, w, h: boxH, ...it }))
    if (ri > 0) arrows.push({ x1: 340, y1: y - rowGap + 8, x2: 340, y2: y - 4 })
  })
  return { h: y0 + rows.length * boxH + (rows.length - 1) * rowGap + 40, nodes, arrows, caption }
}

/** 中心辐射：一个中心节点 + 若干外围节点。 */
function hub(center: N, leaves: N[], caption: string): Diagram {
  const cw = 200, ch = 62, cx = 210, cy = 96
  const nodes: Node[] = [{ x: cx, y: cy, w: cw, h: ch, ...center }]
  const arrows: Arrow[] = []
  const lw = 170, lh = 56, lx = 460
  leaves.forEach((it, i) => {
    const ly = 26 + i * 72
    nodes.push({ x: lx, y: ly, w: lw, h: lh, ...it })
    arrows.push({ x1: cx + cw, y1: cy + ch / 2, x2: lx, y2: ly + lh / 2 })
  })
  return { h: Math.max(190, 26 + leaves.length * 72 + 18), nodes, arrows, caption }
}

/** 双栏对照：用于「A 与 B 有什么不同」。 */
function cols(left: N[], right: N[], caption: string): Diagram {
  const nodes: Node[] = []
  const place = (list: N[], x: number) =>
    list.forEach((it, i) => nodes.push({ x, y: 38 + i * 72, w: 262, h: 58, ...it }))
  place(left, 36); place(right, 382)
  const h = Math.max(left.length, right.length) * 72 + 58
  return { h, nodes, arrows: [{ x1: 300, y1: h / 2, x2: 380, y2: h / 2, label: '对比' }], caption }
}

/** Go 课程概念图：全部由上面 4 种布局组合而成，坐标与配色保持统一。 */
const D: Record<string, Diagram> = {
  'hello-flow': row([
    { label: 'main.go', sub: '人类可读源码', tone: 'blue' },
    { label: 'go build', sub: '编译 + 链接', tone: 'orange' },
    { label: '可执行文件', sub: '静态机器码', tone: 'purple' },
    { label: '进程运行', sub: '输出结果', tone: 'green' },
  ], 'go run 不是解释执行，而是先编译成可执行文件再运行'),
  'value-pointer': cols(
    [{ label: '值变量 a', sub: '直接装着数据 42', tone: 'blue' }, { label: 'b := a', sub: '复制出一份新数据', tone: 'blue' }],
    [{ label: '指针 p', sub: '装着地址 0xc000', tone: 'orange' }, { label: '*p = 9', sub: '改的是同一份数据', tone: 'green' }],
    'Go 默认按值传递；指针让你显式共享同一份数据'),
  'stack-heap': cols(
    [{ label: '栈 Stack', sub: '随函数调用自动分配/回收', tone: 'blue' }, { label: '局部变量', sub: '生命周期随栈帧', tone: 'blue' }],
    [{ label: '堆 Heap', sub: '由 GC 负责回收', tone: 'orange' }, { label: '逃逸的对象', sub: '被外部引用时必须存活', tone: 'purple' }],
    '变量住在哪里由逃逸分析决定，不由 new / make 决定'),
  'slice-header': row([
    { label: 'slice 变量', sub: '栈上 3 个机器字', tone: 'orange' },
    { label: 'ptr / len / cap', sub: '指向底层数组', tone: 'blue' },
    { label: '底层数组', sub: '堆上连续元素', tone: 'green' },
  ], '切片本身只是一个描述符，真正的元素在底层数组里'),
  'slice-grow': layers([
    [{ label: 's := make([]int, 2, 2)', sub: 'len=2 cap=2', tone: 'blue' }],
    [{ label: 's = append(s, 3)', sub: '容量不够，分配新数组', tone: 'orange' }],
    [{ label: '拷贝旧元素 + 追加', sub: 'cap 通常翻倍', tone: 'purple' }, { label: '旧数组等待 GC', sub: '旧切片仍指向它', tone: 'red' }],
  ], '扩容会换底层数组，共享同一数组的其他切片看不到新元素'),
  'map-hmap': hub({ label: 'hmap', sub: 'count / B / buckets', tone: 'orange' }, [
    { label: 'bucket 数组', sub: '2^B 个桶', tone: 'blue' },
    { label: 'tophash + kv', sub: '每桶最多 8 对', tone: 'green' },
    { label: 'overflow 桶', sub: '冲突时链表延伸', tone: 'purple' },
    { label: '渐进扩容', sub: 'oldbuckets 迁移', tone: 'red' },
  ], 'map 是哈希表：定位桶 → 比 tophash → 比 key'),
  'struct-align': row([
    { label: 'bool', sub: '1 字节', tone: 'blue' },
    { label: 'padding', sub: '7 字节空洞', tone: 'red' },
    { label: 'int64', sub: '8 字节对齐', tone: 'green' },
    { label: 'bool', sub: '1 字节', tone: 'blue' },
  ], '字段顺序影响内存对齐，合理排序能省下 padding'),
  'iface': cols(
    [{ label: 'iface（非空接口）', sub: 'itab + data', tone: 'blue' }, { label: 'itab', sub: '类型 + 方法表', tone: 'purple' }],
    [{ label: 'eface（any）', sub: '_type + data', tone: 'orange' }, { label: 'typed nil', sub: '类型非空、值为 nil', tone: 'red' }],
    '接口变量是两个字：一个描述类型，一个指向数据'),
  'error-chain': row([
    { label: '底层错误', sub: 'sql: no rows', tone: 'red' },
    { label: 'fmt.Errorf("%w")', sub: '包装上下文', tone: 'orange' },
    { label: '调用方', sub: 'errors.Is / As', tone: 'green' },
  ], '%w 保留错误链，让上层既能加上下文又能判定原始错误'),
  'defer-stack': layers([
    [{ label: 'defer f1()', sub: '第 1 个入栈', tone: 'blue' }],
    [{ label: 'defer f2()', sub: '第 2 个入栈', tone: 'purple' }],
    [{ label: '函数 return', sub: '按 LIFO 执行', tone: 'orange' }],
  ], 'defer 先进后出；参数在声明时就求值'),
  'escape': cols(
    [{ label: '留在栈上', sub: '只在函数内使用', tone: 'green' }, { label: '零 GC 压力', sub: '函数返回即回收', tone: 'green' }],
    [{ label: '逃逸到堆', sub: '返回局部指针等', tone: 'red' }, { label: '产生分配', sub: 'go build -gcflags=-m 可见', tone: 'orange' }],
    '逃逸分析是编译期决策，目标是尽量减少堆分配'),
  'allocator': row([
    { label: 'goroutine', sub: '申请小对象', tone: 'blue' },
    { label: 'mcache', sub: 'P 私有、无锁', tone: 'green' },
    { label: 'mcentral', sub: '同规格 span', tone: 'orange' },
    { label: 'mheap / OS', sub: '大对象与兜底', tone: 'purple' },
  ], '三级缓存让绝大多数小对象分配无需加锁'),
  'gc-tricolor': row([
    { label: '白色', sub: '待考察 / 可回收', tone: 'gray' },
    { label: '灰色', sub: '已访问、子未扫', tone: 'orange' },
    { label: '黑色', sub: '已扫描完成', tone: 'green' },
    { label: '写屏障', sub: '并发期间保正确性', tone: 'purple' },
  ], '三色标记 + 混合写屏障，让标记过程几乎与业务并发'),
  'startup': row([
    { label: 'rt0_go', sub: '入口汇编', tone: 'gray' },
    { label: 'schedinit', sub: '初始化 P/M', tone: 'blue' },
    { label: 'main goroutine', sub: '新建 G 入队', tone: 'orange' },
    { label: 'mstart', sub: '开始调度', tone: 'green' },
  ], 'main 函数之前，运行时已经把调度器准备好了'),
  'compile-link': row([
    { label: '源码 .go', sub: '包依赖图', tone: 'blue' },
    { label: 'AST / 类型检查', sub: '前端', tone: 'purple' },
    { label: 'SSA 优化', sub: '内联 / 逃逸', tone: 'orange' },
    { label: '机器码 + 链接', sub: '可执行文件', tone: 'green' },
  ], '编译期做的优化，决定了运行时能有多轻'),
  'gmp': hub({ label: 'P · Processor', sub: '调度上下文 / 本地队列', tone: 'orange' }, [
    { label: 'G · goroutine', sub: '轻量执行单元', tone: 'blue' },
    { label: 'M · Machine', sub: '操作系统线程', tone: 'green' },
    { label: '本地运行队列', sub: '无锁、快速', tone: 'purple' },
    { label: '全局队列', sub: '兜底与均衡', tone: 'gray' },
  ], 'M 必须持有 P 才能执行 G；P 的数量由 GOMAXPROCS 决定'),
  'scheduler-loop': row([
    { label: 'schedule()', sub: '找一个可运行的 G', tone: 'orange' },
    { label: '本地 / 全局队列', sub: '先本地再全局', tone: 'blue' },
    { label: 'work stealing', sub: '从别的 P 偷一半', tone: 'purple' },
    { label: 'execute()', sub: '切到 G 的栈运行', tone: 'green' },
  ], '调度循环的核心目标：别让任何 P 闲着'),
  'hchan': cols(
    [{ label: 'sendq', sub: '阻塞的发送者', tone: 'red' }, { label: 'buf', sub: '环形缓冲区', tone: 'orange' }],
    [{ label: 'recvq', sub: '阻塞的接收者', tone: 'blue' }, { label: 'closed / lock', sub: '状态与互斥', tone: 'purple' }],
    'hchan 用两个等待队列把收发双方直接配对'),
  'select': hub({ label: 'select', sub: '随机选一个就绪 case', tone: 'orange' }, [
    { label: 'case 1', sub: 'channel 就绪', tone: 'blue' },
    { label: 'case 2', sub: 'channel 就绪', tone: 'green' },
    { label: 'default', sub: '都不就绪就走它', tone: 'purple' },
    { label: 'timer', sub: '超时兜底', tone: 'red' },
  ], 'select 的随机性避免了某个 case 被长期饿死'),
  'context-tree': layers([
    [{ label: 'context.Background()', sub: '根', tone: 'gray' }],
    [{ label: 'WithTimeout', sub: '服务入口', tone: 'orange' }, { label: 'WithCancel', sub: '子任务', tone: 'blue' }],
    [{ label: 'DB 调用', sub: '收到取消', tone: 'green' }, { label: 'RPC 调用', sub: '收到取消', tone: 'purple' }],
  ], '取消信号沿 context 树向下广播，所有派生任务一起停'),
  'pipeline': row([
    { label: '生成阶段', sub: 'gen() → chan', tone: 'blue' },
    { label: '处理阶段', sub: 'fan-out worker', tone: 'orange' },
    { label: '聚合阶段', sub: 'fan-in merge', tone: 'purple' },
    { label: '输出', sub: '消费结果', tone: 'green' },
  ], 'pipeline 把并发拆成可组合的阶段，每阶段只管一件事'),
  'layout': layers([
    [{ label: 'cmd/', sub: '程序入口', tone: 'gray' }],
    [{ label: 'api / handler', sub: '协议层', tone: 'blue' }],
    [{ label: 'service / domain', sub: '业务层', tone: 'orange' }],
    [{ label: 'data / internal', sub: '数据与实现细节', tone: 'green' }],
  ], '依赖方向只能向下，internal 阻止外部包误用内部实现'),
  'test-pyramid': layers([
    [{ label: 'E2E 测试', sub: '少而慢，覆盖关键链路', tone: 'red' }],
    [{ label: '集成测试', sub: '模块协作 / httptest', tone: 'orange' }],
    [{ label: '单元测试', sub: '多而快，表驱动', tone: 'green' }],
  ], '底座越厚，反馈越快，定位越准'),
  'http-chain': row([
    { label: 'Client', sub: 'HTTP 请求', tone: 'gray' },
    { label: 'ServeMux', sub: '路由匹配', tone: 'blue' },
    { label: 'Middleware', sub: '日志 / 鉴权', tone: 'orange' },
    { label: 'Handler', sub: '业务逻辑', tone: 'green' },
  ], '一个请求依次穿过路由、中间件，最后才到业务代码'),
  'middleware-onion': layers([
    [{ label: '请求进入', sub: '前置处理', tone: 'blue' }],
    [{ label: '中间件 A', sub: 'before / after', tone: 'orange' }],
    [{ label: '中间件 B', sub: 'before / after', tone: 'purple' }],
    [{ label: 'Handler', sub: '最内层', tone: 'green' }],
  ], '洋葱模型：请求向内穿透，响应按相反顺序出来'),
  'db-pool': hub({ label: 'sql.DB 连接池', sub: '复用物理连接', tone: 'orange' }, [
    { label: '空闲连接', sub: 'MaxIdleConns', tone: 'green' },
    { label: '在用连接', sub: '执行查询', tone: 'blue' },
    { label: '等待队列', sub: '超过 MaxOpenConns', tone: 'red' },
    { label: '生命周期', sub: 'ConnMaxLifetime', tone: 'purple' },
  ], 'sql.DB 是连接池而不是单个连接，别用短连接思路调它'),
  'cache-aside': row([
    { label: '读请求', sub: '先查缓存', tone: 'blue' },
    { label: '命中？', sub: '命中直接返回', tone: 'green' },
    { label: '回源 DB', sub: '未命中才查库', tone: 'orange' },
    { label: '回填缓存', sub: '设置 TTL', tone: 'purple' },
  ], 'Cache Aside 的核心：读时回填，写时删除而不是更新'),
  'mq-flow': row([
    { label: 'Producer', sub: '发送消息', tone: 'blue' },
    { label: 'Broker', sub: '持久化 / 分区', tone: 'orange' },
    { label: 'Consumer', sub: '拉取并处理', tone: 'purple' },
    { label: 'ACK', sub: '确认后才算完成', tone: 'green' },
  ], '可靠投递靠「确认 + 重试 + 消费端幂等」三者一起保证'),
  'rpc-flow': row([
    { label: '调用方', sub: '像调本地函数', tone: 'blue' },
    { label: 'Stub 序列化', sub: '编码参数', tone: 'orange' },
    { label: '网络传输', sub: 'TCP / HTTP2', tone: 'gray' },
    { label: '服务端 Stub', sub: '解码并分派', tone: 'green' },
  ], 'RPC 把「找到服务、编码、传输、解码、分派」藏在了接口后面'),
  'discovery': hub({ label: '注册中心', sub: '服务名 → 实例列表', tone: 'orange' }, [
    { label: '服务实例 A', sub: '注册 + 心跳', tone: 'blue' },
    { label: '服务实例 B', sub: '注册 + 心跳', tone: 'green' },
    { label: '消费者', sub: '订阅变更', tone: 'purple' },
    { label: '健康检查', sub: '摘除故障实例', tone: 'red' },
  ], '实例上下线对调用方透明，靠的是注册 + 心跳 + 订阅'),
  'gateway': row([
    { label: '客户端', sub: '统一入口', tone: 'gray' },
    { label: 'API 网关', sub: '鉴权 / 限流 / 路由', tone: 'orange' },
    { label: '内部服务', sub: 'gRPC / HTTP', tone: 'blue' },
    { label: '数据层', sub: 'DB / 缓存', tone: 'green' },
  ], '网关把横切关注点从每个服务里收拢到边界上'),
  'circuit-breaker': row([
    { label: 'Closed', sub: '正常放行', tone: 'green' },
    { label: 'Open', sub: '错误率超阈值，快速失败', tone: 'red' },
    { label: 'Half-Open', sub: '放少量探针请求', tone: 'orange' },
    { label: '恢复 / 再熔断', sub: '按结果回切', tone: 'purple' },
  ], '熔断器用「快速失败」换取下游恢复的时间'),
  'tracing': row([
    { label: 'Trace', sub: '一次完整请求', tone: 'gray' },
    { label: 'Span A', sub: '网关', tone: 'blue' },
    { label: 'Span B', sub: '订单服务', tone: 'orange' },
    { label: 'Span C', sub: '数据库', tone: 'green' },
  ], 'Trace 由嵌套的 Span 组成，靠 context 跨进程传播'),
  'cap': hub({ label: '分布式系统', sub: '网络分区必然存在', tone: 'orange' }, [
    { label: 'C 一致性', sub: '读到最新写', tone: 'blue' },
    { label: 'A 可用性', sub: '每次请求有响应', tone: 'green' },
    { label: 'P 分区容错', sub: '不能放弃', tone: 'red' },
    { label: '取舍', sub: 'CP 或 AP', tone: 'purple' },
  ], 'P 是前提，真正的选择发生在 C 与 A 之间'),
  'saga': row([
    { label: 'T1 创建订单', sub: '本地事务提交', tone: 'blue' },
    { label: 'T2 扣减库存', sub: '本地事务提交', tone: 'green' },
    { label: 'T3 支付失败', sub: '触发补偿', tone: 'red' },
    { label: 'C2 → C1', sub: '逆序补偿', tone: 'orange' },
  ], 'Saga 用「正向本地事务 + 逆向补偿」换取长事务的可用性'),
  'lock': row([
    { label: 'SETNX + 唯一值', sub: '抢锁', tone: 'blue' },
    { label: '设置租约 TTL', sub: '防死锁', tone: 'orange' },
    { label: '看门狗续期', sub: '业务没完就续', tone: 'purple' },
    { label: 'Lua 比对后释放', sub: '只删自己的锁', tone: 'green' },
  ], '分布式锁四要素：互斥、租约、续期、安全释放'),
  'snowflake': row([
    { label: '1 bit 符号', sub: '固定为 0', tone: 'gray' },
    { label: '41 bit 时间戳', sub: '毫秒级', tone: 'blue' },
    { label: '10 bit 机器', sub: '数据中心 + 节点', tone: 'orange' },
    { label: '12 bit 序列', sub: '同毫秒自增', tone: 'green' },
  ], '趋势递增的 64 位 ID，对索引友好且无需中心节点'),
  'hash-ring': hub({ label: '一致性哈希环', sub: '2^32 个槽位', tone: 'orange' }, [
    { label: '节点 A', sub: '负责一段区间', tone: 'blue' },
    { label: '虚拟节点', sub: '让分布更均匀', tone: 'purple' },
    { label: '新增节点', sub: '只迁移相邻数据', tone: 'green' },
    { label: '热点与倾斜', sub: '需要额外治理', tone: 'red' },
  ], '相比取模分片，扩容时只需迁移一小部分 key'),
  'raft': hub({ label: 'Leader', sub: '唯一写入口', tone: 'orange' }, [
    { label: 'Follower 1', sub: '复制日志', tone: 'blue' },
    { label: 'Follower 2', sub: '复制日志', tone: 'green' },
    { label: '多数派确认', sub: '提交并应用', tone: 'purple' },
    { label: '选举超时', sub: '重新选主', tone: 'red' },
  ], 'Raft 用「选举 + 日志复制 + 多数派」把共识讲清楚'),
  'docker-layer': layers([
    [{ label: 'build 阶段', sub: 'golang 镜像 + 编译', tone: 'blue' }],
    [{ label: '产物拷贝', sub: '只要二进制与证书', tone: 'orange' }],
    [{ label: '运行阶段', sub: 'scratch / distroless', tone: 'green' }],
  ], '多阶段构建把 Go 静态编译的优势变成几 MB 的镜像'),
  'k8s': hub({ label: 'Deployment', sub: '声明期望副本数', tone: 'orange' }, [
    { label: 'Pod', sub: '实际运行实例', tone: 'blue' },
    { label: 'Service', sub: '稳定访问入口', tone: 'green' },
    { label: 'Ingress', sub: '对外暴露路由', tone: 'purple' },
    { label: '探针 / HPA', sub: '自愈与扩缩', tone: 'red' },
  ], 'K8s 用声明式对象持续把实际状态收敛到期望状态'),
  'slo': cols(
    [{ label: 'SLI', sub: ' measurable 指标', tone: 'blue' }, { label: 'SLO', sub: '目标值，如 99.9%', tone: 'orange' }],
    [{ label: '错误预算', sub: '允许失败多少', tone: 'green' }, { label: '决策依据', sub: '能不能继续发版', tone: 'purple' }],
    'SLO 把「稳不稳定」变成一个可以讨论和决策的数字'),
  'runtime-startup': row([
    { label: '入口汇编', sub: '设置栈与参数', tone: 'gray' },
    { label: '运行时初始化', sub: '内存 / GC / P', tone: 'blue' },
    { label: 'main G 入队', sub: '创建首个 goroutine', tone: 'orange' },
    { label: '调度启动', sub: '进入 schedule()', tone: 'green' },
  ], 'Go 程序从一开始就是「被调度」出来的'),
  'ssa': row([
    { label: '源码', sub: '包与类型检查', tone: 'blue' },
    { label: 'AST', sub: '语法树', tone: 'purple' },
    { label: 'SSA', sub: '静态单赋值', tone: 'orange' },
    { label: '优化 Pass', sub: '内联 / 消除', tone: 'green' },
  ], 'SSA 让编译器的每一条优化都有清晰可验证的形式'),
  'perf-loop': row([
    { label: '建立基线', sub: 'benchmark', tone: 'gray' },
    { label: 'pprof 定位', sub: 'CPU / 分配 / 锁', tone: 'orange' },
    { label: '逐项优化', sub: '一次只改一处', tone: 'blue' },
    { label: '验证防回归', sub: 'benchstat 对比', tone: 'green' },
  ], '没有基线的优化只是猜测；闭环才能积累可复用的经验'),
  'architecture-layer': layers([
    [{ label: '接口层', sub: '协议与鉴权', tone: 'blue' }],
    [{ label: '应用层', sub: '编排用例', tone: 'orange' }],
    [{ label: '领域层', sub: '业务规则与接口', tone: 'purple' }],
    [{ label: '基础设施层', sub: 'DB / MQ / 缓存实现', tone: 'green' }],
  ], '依赖倒置让业务规则不依赖具体技术，替换实现不动核心'),
  placeholder: {
    h: 168,
    caption: '占位示意图：替换为课程需要的节点与箭头即可。',
    nodes: [
      { x: 40, y: 48, w: 170, h: 62, label: '输入', sub: '从这里开始', tone: 'blue' },
      { x: 255, y: 48, w: 170, h: 62, label: '处理', sub: '你的逻辑', tone: 'purple' },
      { x: 470, y: 48, w: 170, h: 62, label: '输出', sub: '到这里结束', tone: 'green' },
    ],
    arrows: [{ x1: 210, y1: 79, x2: 255, y2: 79 }, { x1: 425, y1: 79, x2: 470, y2: 79 }],
  },
}

const props = defineProps<{
  type?: string
  title?: string
  labels?: string
  caption?: string
}>()
const uid = useId().replace(/:/g, '')
const diagram = computed<Diagram>(() => {
  const base = D[props.type || 'placeholder'] || D['placeholder']
  if (!props.labels && !props.caption) return base
  let nodes = base.nodes
  if (props.labels) {
    const parts = String(props.labels || '').split('|').map((s) => s.trim())
    nodes = base.nodes.map((n, i) => {
      const raw = parts[i]
      if (!raw) return n
      const [label, sub] = raw.split('::').map((s) => s.trim())
      return { ...n, label, sub: sub || n.sub }
    })
  }
  return { ...base, nodes, caption: props.caption ?? base.caption }
})
/* 放大查看：滚轮无极缩放（以指针为锚点）+ 拖拽平移画布。
   画布使用固定的 680px 逻辑宽度（与 SVG viewBox 一致），缩放/平移全部由 transform 承担，
   这样窗口尺寸变化不会改变内容本身的坐标系，锚点缩放的计算也始终稳定。 */
const zoomOpen = ref(false)
const scale = ref(1)      // 绝对倍率：1 = 680px 逻辑宽度按 1:1 渲染
const fitScale = ref(1)   // 「适应窗口」倍率，同时作为缩放上下限与百分比的基准
const panX = ref(0)
const panY = ref(0)
const dragging = ref(false)
const viewport = ref<HTMLElement | null>(null)
const BASE_W = 680
const PAD = 28            // 画布四周留白
const MIN_RATIO = 0.4     // 相对适应窗口：最小缩到 40%
const MAX_RATIO = 8       // 相对适应窗口：最大放到 800%
const zoomLevel = computed(() => Math.round(scale.value / (fitScale.value || 1) * 100))
let prevOverflow = ''

const canvasStyle = computed(() => ({
  transform: `translate(${panX.value}px, ${panY.value}px) scale(${scale.value})`,
  transformOrigin: '0 0',
}))

/** 按视口与图的原始比例算出「整图完整可见」的倍率，并把画布居中。 */
function fitView() {
  const el = viewport.value
  if (!el) return
  const vw = el.clientWidth
  const vh = el.clientHeight
  const h = diagram.value.h || 200
  // 窄屏按宽度适配，固定 28px 留白会占掉太多，缩到视口的 3% 保证弹层里的图不小于正文
  const pad = Math.min(PAD, vw * 0.03)
  const s = Math.max(0.05, Math.min((vw - pad * 2) / BASE_W, (vh - pad * 2) / h))
  fitScale.value = s
  scale.value = s
  panX.value = (vw - BASE_W * s) / 2
  panY.value = (vh - h * s) / 2
}

/** 窗口尺寸变化时保持用户当前的相对倍率，只重算适配基准，不把画面突然打回 100%。 */
function onResize() {
  if (!zoomOpen.value) return
  const el = viewport.value
  if (!el) return
  const ratio = scale.value / (fitScale.value || 1)
  fitView()
  if (Math.abs(ratio - 1) > 0.001) {
    const r = el.getBoundingClientRect()
    zoomAt(r.left + r.width / 2, r.top + r.height / 2, ratio)
  }
}

function openZoom() {
  if (zoomOpen.value) return
  zoomOpen.value = true
  prevOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  // 等 flex 布局稳定后再算垂直居中，避免首帧 clientHeight 还没定下
  nextTick(() => requestAnimationFrame(() => requestAnimationFrame(fitView)))
}
function closeZoom() {
  if (!zoomOpen.value) return
  zoomOpen.value = false
  dragging.value = false
  pointers.clear()
  document.body.style.overflow = prevOverflow
  prevOverflow = ''
}

/** 以 (clientX, clientY) 为锚点缩放：锚点下的内容保持在原位置。 */
function zoomAt(clientX: number, clientY: number, factor: number) {
  const el = viewport.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const mx = clientX - rect.left
  const my = clientY - rect.top
  const cx = (mx - panX.value) / scale.value
  const cy = (my - panY.value) / scale.value
  const min = fitScale.value * MIN_RATIO
  const max = fitScale.value * MAX_RATIO
  const next = Math.min(max, Math.max(min, scale.value * factor))
  if (Math.abs(next - scale.value) < 0.0001) return
  scale.value = next
  panX.value = mx - cx * next
  panY.value = my - cy * next
}

function onWheel(e: WheelEvent) {
  // deltaY 越大缩得越多，用指数换算成连续倍率，触屏板的细小滚动也能平滑响应
  zoomAt(e.clientX, e.clientY, Math.exp(-e.deltaY * 0.0015))
}
function zoomByStep(factor: number) {
  const el = viewport.value
  if (!el) return
  const r = el.getBoundingClientRect()
  zoomAt(r.left + r.width / 2, r.top + r.height / 2, factor)
}
function onDblClick() { fitView() }

/** 单指拖拽平移；双指捏合缩放（touch-action:none 会禁用浏览器原生缩放，必须自己实现）。 */
interface Pointer { id: number; x: number; y: number }
const pointers = new Map<number, Pointer>()
let dragStart = { x: 0, y: 0, px: 0, py: 0, id: -1 }
let pinchStart = { dist: 1, mx: 0, my: 0 }

function startDrag(p: Pointer) {
  dragStart = { x: p.x, y: p.y, px: panX.value, py: panY.value, id: p.id }
}
function pinchState() {
  const [a, b] = [...pointers.values()]
  return {
    dist: Math.max(1, Math.hypot(a.x - b.x, a.y - b.y)),
    mx: (a.x + b.x) / 2,
    my: (a.y + b.y) / 2,
  }
}
function onPointerDown(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return
  try { (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId) } catch { /* 非活跃指针时忽略 */ }
  pointers.set(e.pointerId, { id: e.pointerId, x: e.clientX, y: e.clientY })
  if (pointers.size === 1) {
    dragging.value = true
    startDrag([...pointers.values()][0])
  } else if (pointers.size === 2) {
    dragging.value = false
    pinchStart = pinchState()
  }
}
function onPointerMove(e: PointerEvent) {
  if (!pointers.has(e.pointerId)) return
  pointers.set(e.pointerId, { id: e.pointerId, x: e.clientX, y: e.clientY })
  if (pointers.size >= 2) {
    const now = pinchState()
    zoomAt(now.mx, now.my, now.dist / pinchStart.dist)
    // 捏合的同时两指整体移动，也要跟着平移，观感才跟手
    panX.value += now.mx - pinchStart.mx
    panY.value += now.my - pinchStart.my
    pinchStart = now
    return
  }
  if (!dragging.value || e.pointerId !== dragStart.id) return
  panX.value = dragStart.px + (e.clientX - dragStart.x)
  panY.value = dragStart.py + (e.clientY - dragStart.y)
}
function onPointerUp(e: PointerEvent) {
  pointers.delete(e.pointerId)
  const rest = [...pointers.values()]
  if (rest.length === 1) {
    // 双指变成单指时无缝接着拖，避免画面跳一下
    dragging.value = true
    startDrag(rest[0])
  } else if (rest.length === 0) {
    dragging.value = false
    dragStart.id = -1
  }
}

function onKeydown(e: KeyboardEvent) {
  if (!zoomOpen.value) return
  if (e.key === 'Escape') closeZoom()
  else if (e.key === '+' || e.key === '=') zoomByStep(1.2)
  else if (e.key === '-' || e.key === '_') zoomByStep(1 / 1.2)
  else if (e.key === '0') fitView()
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', onResize)
  if (zoomOpen.value) document.body.style.overflow = prevOverflow
})
</script>

<template>
  <figure class="go-figure">
    <div class="go-fig-head">
      <div v-if="title" class="go-eyebrow">{{ title }}</div>
      <span v-else />
      <button class="go-zoom-btn" type="button" :aria-label="`放大查看：${title || '概念图'}`" @click="openZoom">
        ⤢ 放大查看
      </button>
    </div>

    <div class="go-fig-frame" role="button" tabindex="0"
      :aria-label="`点击放大查看：${title || '概念图'}`"
      @click="openZoom" @keydown.enter.prevent="openZoom" @keydown.space.prevent="openZoom">
      <ConceptDiagramSvg :diagram="diagram" :uid="uid" />
    </div>

    <figcaption v-if="diagram.caption" class="go-fig-caption">{{ diagram.caption }}</figcaption>

    <Teleport to="body">
      <div v-if="zoomOpen" class="go-lightbox" role="dialog" aria-modal="true"
        :aria-label="title || '放大查看概念图'" @click.self="closeZoom">
        <div class="go-lightbox-panel">
          <div class="go-lightbox-head">
            <b>{{ title || '概念图' }}</b>
            <div class="go-lightbox-actions">
              <button class="go-btn ghost" type="button" aria-label="缩小" @click="zoomByStep(1 / 1.2)">−</button>
              <span class="go-zoom-level" aria-live="polite">{{ zoomLevel }}%</span>
              <button class="go-btn ghost" type="button" aria-label="放大" @click="zoomByStep(1.2)">＋</button>
              <button class="go-btn ghost" type="button" @click="fitView">适应窗口</button>
              <button class="go-btn ghost" type="button" aria-label="关闭放大" @click="closeZoom">✕ 关闭</button>
            </div>
          </div>
          <div ref="viewport" class="go-lightbox-body" :class="{ dragging }"
            @wheel.prevent="onWheel" @pointerdown="onPointerDown" @pointermove="onPointerMove"
            @pointerup="onPointerUp" @pointercancel="onPointerUp" @dblclick="onDblClick">
            <div class="go-zoom-canvas" :style="canvasStyle">
              <ConceptDiagramSvg :diagram="diagram" :uid="uid + '-zoom'" />
            </div>
          </div>
          <div class="go-lightbox-foot">
            <span v-if="diagram.caption">{{ diagram.caption }} · </span>100% 为适应窗口 · 滚轮 / 双指捏合缩放 · 拖拽平移 · 双击或 0 键复位 · ESC 关闭
          </div>
        </div>
      </div>
    </Teleport>
  </figure>
</template>
