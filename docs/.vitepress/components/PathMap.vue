<script setup lang="ts">
import { withBase } from 'vitepress'

/** 由 .workbuddy/gen_course.py 生成，与 config.mts / utils.ts 同源。 */
const stages = [
  { lv: 'L0 · 导学', name: '导学', desc: '环境、第一行代码与工具链，建立全局认知', link: '/guide/', color: '#868e96', meta: '4 讲' },
  { lv: 'L1 · 编程地基', name: '编程地基', desc: '变量与类型、字符串、指针与值语义、函数与包', link: '/foundation/', color: '#2b8a3e', meta: '8 讲' },
  { lv: 'L2 · 语法核心', name: '语法核心', desc: '切片、map、结构体、方法、接口、错误、泛型、反射与序列化', link: '/basics/', color: '#1c7ed6', meta: '13 讲' },
  { lv: 'L3 · 核心机制', name: '核心机制', desc: '逃逸分析、切片/map/interface 底层、分配器、GC、编译链接、unsafe', link: '/core/', color: '#7048e8', meta: '12 讲' },
  { lv: 'L4 · 并发编程', name: '并发编程', desc: 'goroutine、GMP、channel、select、sync、atomic、context、并发模式与 race', link: '/concurrency/', color: '#e8590c', meta: '12 讲' },
  { lv: 'L5 · 工程化', name: '工程化', desc: '项目结构、规范、单测与 mock、日志配置、错误设计、pprof、CI/CD', link: '/engineering/', color: '#0ca678', meta: '11 讲' },
  { lv: 'L6 · 服务端与数据', name: '服务端与数据', desc: 'net/http、框架、REST、WebSocket、SQL/ORM、Redis、MQ、搜索、认证、对象存储', link: '/server/', color: '#1c7ed6', meta: '11 讲' },
  { lv: 'L7 · RPC 与微服务', name: 'RPC 与微服务', desc: 'RPC、gRPC、注册发现、负载均衡、配置中心、网关、熔断、追踪、监控、自研框架', link: '/microservice/', color: '#7048e8', meta: '11 讲' },
  { lv: 'L8 · 分布式云原生', name: '分布式云原生', desc: 'CAP、锁与幂等、分布式事务、ID、分片、Raft、高可用、Docker、K8s、GitOps、SRE', link: '/distributed/', color: '#c92a2a', meta: '11 讲' },
  { lv: 'L9 · 资深进阶', name: '资深进阶', desc: 'runtime 启动与调度源码、GC 源码、编译器 SSA、内存模型、性能案例、架构与成长', link: '/advanced/', color: '#495057', meta: '9 讲' },
  { lv: 'L10 · 工程实战', name: '工程实战', desc: 'CLI、网络、数据、并发、存储、微服务、云原生、跨语言、AI、安全治理，含 KV 贯穿线', link: '/projects/', color: '#c92a2a', meta: '16 讲' },
]
</script>

<template>
  <div class="go-path">
    <a v-for="(s, i) in stages" :key="i" class="go-path-card" :href="withBase(s.link)" :style="{borderTop:'3px solid '+s.color}">
      <div class="lv" :style="{color:s.color}">{{ s.lv }} <span style="color:var(--go-muted);font-weight:600">· {{ s.meta }}</span></div>
      <div class="nm">{{ s.name }}</div>
      <div class="ds">{{ s.desc }}</div>
      <div style="margin-top:.7rem;font-size:12px;font-weight:700" :style="{color:s.color}">进入阶段 →</div>
    </a>
  </div>
</template>
