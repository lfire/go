<script setup lang="ts">
import { computed, ref } from 'vue'
import { withBase } from 'vitepress'

/** 由 .workbuddy/gen_course.py 生成：10 大类工程场景，覆盖课程与实战讲次。 */
interface Row { cat: string; name: string; desc: string; level: number; prereq: string; link: string }
const rows: Row[] = [
  { cat: '命令行与工具链', name: '待办清单 CLI', desc: 'flag/cobra、文件持久化、退出码与测试', level: 1, prereq: 'L1 基础', link: '/projects/cli-todo' },
  { cat: '命令行与工具链', name: 'protoc 插件与代码生成器', desc: 'AST、模板、代码生成与错误码注册', level: 3, prereq: 'L2 反射', link: '/projects/cli-codegen' },
  { cat: '网络服务', name: '短链服务', desc: '发号、缓存、301/302 与防刷', level: 2, prereq: 'L6 net/http', link: '/projects/net-shortlink' },
  { cat: '网络服务', name: '图片服务与对象存储', desc: '上传、缩略图、ETag 与直传', level: 3, prereq: 'L6 文件', link: '/projects/net-image-server' },
  { cat: '网络服务', name: '实时推送与 WebSocket', desc: '长连接、心跳、广播与断线重连', level: 3, prereq: 'L6 WebSocket', link: '/server/websocket' },
  { cat: '数据处理', name: '日志分析 ETL 管道', desc: 'pipeline、窗口聚合、背压与落库', level: 3, prereq: 'L4 并发模式', link: '/projects/data-log-etl' },
  { cat: '数据处理', name: 'CSV / SQL 查询工具', desc: '表达式 AST 与多数据源适配', level: 2, prereq: 'L6 database/sql', link: '/projects/data-sql-tool' },
  { cat: '数据处理', name: '搜索与倒排索引', desc: 'ES mapping、分词与异构同步', level: 4, prereq: 'L6 搜索', link: '/server/search' },
  { cat: '并发系统', name: '并发爬虫三级演进', desc: '单机 → worker pool → 分布式', level: 4, prereq: 'L4 全阶段', link: '/projects/concurrent-crawler' },
  { cat: '并发系统', name: '分布式任务调度', desc: 'etcd 选主、租约与故障转移', level: 4, prereq: 'L8 Raft', link: '/projects/concurrent-scheduler' },
  { cat: '并发系统', name: '限流器与并发控制', desc: '令牌桶、errgroup、singleflight', level: 3, prereq: 'L4 并发模式', link: '/concurrency/patterns' },
  { cat: '存储与中间件', name: 'KV ① 内存版', desc: '接口抽象、命令模型与基准测试', level: 2, prereq: 'L2 综合', link: '/projects/kv-1-memory' },
  { cat: '存储与中间件', name: 'KV ② 网络协议帧', desc: 'TCP 粘包、编解码与客户端 SDK', level: 3, prereq: 'KV ①', link: '/projects/kv-2-network' },
  { cat: '存储与中间件', name: 'KV ③ WAL 与并发', desc: '追加写、崩溃恢复与锁分片', level: 4, prereq: 'KV ②', link: '/projects/kv-3-async' },
  { cat: '存储与中间件', name: 'KV ④ 分片复制 Raft', desc: '副本、一致性与成员变更', level: 5, prereq: 'KV ③ + L8', link: '/projects/kv-4-distributed' },
  { cat: '存储与中间件', name: '缓存与分布式锁', desc: '穿透/击穿/雪崩、SETNX 与租约', level: 4, prereq: 'L6 Redis', link: '/server/redis' },
  { cat: '微服务架构', name: '电商微服务系统', desc: '服务拆分、契约、事务与联调', level: 4, prereq: 'L7 全阶段', link: '/projects/micro-ecommerce' },
  { cat: '微服务架构', name: '秒杀与高并发库存', desc: '削峰、预扣减、限流与压测', level: 5, prereq: 'L7 + L8', link: '/projects/micro-seckill' },
  { cat: '微服务架构', name: '自研微服务框架', desc: 'Options 装配、拦截器与可观测内建', level: 5, prereq: 'L7 全阶段', link: '/microservice/framework' },
  { cat: '微服务架构', name: 'API 网关与熔断', desc: '统一入口、鉴权、限流与降级', level: 4, prereq: 'L7 网关', link: '/microservice/gateway' },
  { cat: '云原生与 SRE', name: 'Kubernetes Operator', desc: 'CRD、Reconcile 与平台工程', level: 4, prereq: 'L8 K8s', link: '/projects/cloud-operator' },
  { cat: '云原生与 SRE', name: '容器化与 GitOps 发布', desc: '多阶段构建、Helm 与灰度', level: 3, prereq: 'L8 Docker', link: '/distributed/gitops' },
  { cat: '云原生与 SRE', name: '全链路可观测平台', desc: 'Trace、Metrics、Log 三支柱联动', level: 4, prereq: 'L7 追踪监控', link: '/microservice/tracing' },
  { cat: '云原生与 SRE', name: '稳定性与混沌工程', desc: 'SLO、故障注入、预案与复盘', level: 5, prereq: 'L8 SRE', link: '/distributed/high-availability' },
  { cat: '跨语言与底层', name: 'cgo 与 unsafe 边界', desc: '跨语言调用成本与内存所有权', level: 5, prereq: 'L3 unsafe', link: '/core/unsafe-cgo' },
  { cat: '跨语言与底层', name: 'WASM 与插件系统', desc: '浏览器/边缘宿主上的 Go', level: 4, prereq: 'L3 编译链接', link: '/core/compiler' },
  { cat: '数据与 AI 工程', name: 'LLM 网关与流式推理', desc: '多供应商适配、SSE 与成本控制', level: 4, prereq: 'L6 WebSocket', link: '/projects/ai-gateway' },
  { cat: '数据与 AI 工程', name: '向量检索服务', desc: '高并发读、索引与近似检索', level: 4, prereq: 'L6 搜索 + L4', link: '/server/search' },
  { cat: '安全与治理', name: '认证授权与权限模型', desc: 'JWT、OAuth2、RBAC 与常见漏洞', level: 4, prereq: 'L6 认证', link: '/server/auth' },
  { cat: '安全与治理', name: '错误码与统一响应', desc: '内部错误到 HTTP/gRPC 契约', level: 3, prereq: 'L5 错误设计', link: '/engineering/error-design' },
]
const cats = [
  '全部',
  '命令行与工具链',
  '网络服务',
  '数据处理',
  '并发系统',
  '存储与中间件',
  '微服务架构',
  '云原生与 SRE',
  '跨语言与底层',
  '数据与 AI 工程',
  '安全与治理',
]
const active = ref('全部')
const shown = computed(() => active.value === '全部' ? rows : rows.filter(r => r.cat === active.value))
function badge(l: number) { return 'l' + Math.min(5, Math.max(1, l)) }
function stars(l: number) { return '★'.repeat(l) + '☆'.repeat(5 - l) }
</script>

<template>
  <div>
    <div style="display:flex;gap:.35rem;flex-wrap:wrap;margin-bottom:1rem">
      <button v-for="c in cats" :key="c" class="go-btn ghost"
        :style="{fontSize:'12px',padding:'5px 11px',borderColor: active===c ? 'var(--go-brand)':'var(--go-line)', color: active===c ? 'var(--go-brand)':'var(--go-ink)', background: active===c ? 'var(--go-brand-soft)':'transparent'}"
        @click="active = c">{{ c }}</button>
    </div>
    <div style="overflow:hidden;border:1px solid var(--go-line);border-radius:12px;box-shadow:var(--go-shadow)">
      <table class="go-matrix" style="margin:0;width:100%">
        <colgroup>
          <col style="width:14%" /><col style="width:20%" /><col style="width:40%" />
          <col style="width:11%" /><col style="width:15%" />
        </colgroup>
        <thead><tr><th>场景分类</th><th>项目</th><th>你会练到什么</th><th>难度</th><th>建议前置</th></tr></thead>
        <tbody>
          <tr v-for="(r, i) in shown" :key="i">
            <td><span class="go-badge" :class="badge(r.level)">{{ r.cat }}</span></td>
            <td><a :href="withBase(r.link)" style="font-weight:700;text-decoration:none">{{ r.name }}</a></td>
            <td style="color:var(--go-muted);line-height:1.65">{{ r.desc }}</td>
            <td class="go-stars">{{ stars(r.level) }}</td>
            <td style="color:var(--go-muted);font-size:12px">{{ r.prereq }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="go-sub" style="margin-top:.6rem">
      组织逻辑：<b>先按工程场景归类</b>（10 大类），<b>类内按难度从 ★ 到 ★★★★★ 递进</b>；
      KV Server 是一条贯穿 4 讲的演进线，把接口抽象、协议、持久化、并发与分布式串成完整历史。
    </div>
  </div>
</template>
