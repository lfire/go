---
title: 阶段十 · 工程实战 · 阶段导览
description: 把前十个阶段的知识在 8 类真实场景中反复用熟
---

<StageHeader stage="L10 · 工程实战" level="★ → ★★★★★" time="约 1070 分钟" goal="把前十个阶段的知识在 8 类真实场景中反复用熟" prereq="资深进阶" />

# 阶段十 · 工程实战

> 知识不用就会忘。这一阶段把前十个阶段的能力放进 10 类真实工程场景里反复使用：从 CLI 与代码生成器，到网络服务、数据管道、并发系统、存储中间件、微服务、云原生与 AI 工程。难度按 ★ 到 ★★★★★ 递进，其中 KV Server 是一条贯穿 4 讲的演进线，同一个系统从内存版一路长到分片复制与 Raft。

## 这一阶段学完你能做什么

<CheckList id="goal-L10" title="阶段目标" :items='["能独立完成从需求、设计、编码到压测的完整项目","能对比同一问题的多种架构并说明取舍","能把并发、错误、可观测性等能力组合使用","能形成可展示、可复盘的作品集"]' />

<ConceptDiagram type="architecture-layer" title="阶段十 · 工程实战 · 一张图先看全貌" />

## 讲次安排

本阶段共 **16 讲**（第 103–118 讲），合计约 1070 分钟，难度 1–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 103 | [实战 01 · 待办清单 CLI](/projects/cli-todo) | ★☆☆☆☆ | 50 分钟 | cobra 命令树 · JSON 持久化 · 错误与退出码 |
| 104 | [实战 02 · protoc 插件与代码生成器](/projects/cli-codegen) | ★★★☆☆ | 60 分钟 | protoreflect · text/template · go/ast 解析 |
| 105 | [实战 03 · 短链服务](/projects/net-shortlink) | ★★☆☆☆ | 55 分钟 | 发号与 Base62 · 缓存与 301/302 · 防刷与过期 |
| 106 | [实战 04 · 图片服务与对象存储](/projects/net-image-server) | ★★★☆☆ | 60 分钟 | multipart 与流式读 · 图片处理 · ETag 与缓存 |
| 107 | [实战 05 · 日志分析 ETL 管道](/projects/data-log-etl) | ★★★☆☆ | 60 分钟 | 逐行读取与解析 · 扇出扇入聚合 · 错误与重试 |
| 108 | [实战 06 · CSV / SQL 查询工具](/projects/data-sql-tool) | ★★☆☆☆ | 55 分钟 | 表达式与 AST · 执行计划 · 多数据源适配 |
| 109 | [实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler) | ★★★★☆ | 70 分钟 | 单线程版 · worker pool 并发版 · 去重与限速 |
| 110 | [实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler) | ★★★★☆ | 70 分钟 | 任务模型与 cron · etcd 选主 · 租约与故障转移 |
| 111 | [实战 09 · KV 贯穿线 ① 内存版与接口抽象](/projects/kv-1-memory) | ★★☆☆☆ | 55 分钟 | Command 与 Storage 接口 · 内存实现 · 错误模型 |
| 112 | [实战 10 · KV 贯穿线 ② TCP 协议帧与网络层](/projects/kv-2-network) | ★★★☆☆ | 60 分钟 | 协议帧设计 · 粘包与编解码 · 连接管理 |
| 113 | [实战 11 · KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async) | ★★★★☆ | 70 分钟 | WAL 追加写 · 崩溃恢复 · 锁分片 |
| 114 | [实战 12 · KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) | ★★★★★ | 90 分钟 | 分片路由 · 副本复制 · Raft 集成 |
| 115 | [实战 13 · 电商微服务系统](/projects/micro-ecommerce) | ★★★★☆ | 90 分钟 | 服务拆分与契约 · 用户/商品/订单/库存 · gRPC + HTTP 网关 |
| 116 | [实战 14 · 秒杀与高并发库存](/projects/micro-seckill) | ★★★★★ | 80 分钟 | 流量削峰 · 预扣减与最终一致 · 限流与排队 |
| 117 | [实战 15 · Kubernetes Operator 与可观测平台](/projects/cloud-operator) | ★★★★☆ | 75 分钟 | CRD 与控制器 · Reconcile 循环 · 指标与日志聚合 |
| 118 | [实战 16 · LLM 网关与流式推理管道](/projects/ai-gateway) | ★★★★☆ | 70 分钟 | 多供应商适配 · SSE 流式转发 · 令牌与限流 |

## 怎么学这一阶段

不要照抄代码。每个项目先自己写一版设计（接口、数据结构、失败路径），再对照教程里的实现找差异，收获最大。

<Callout type="warn" title="最容易踩的坑" text="项目页给的是架构蓝图与关键实现，真正的成长来自自己补全边界条件、写测试、做压测并复盘。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L10-pre" q="把一个单机 KV 演进成分布式 KV，最先应该明确的是？" :options='["用哪个 Web 框架","一致性与可用性的目标，以及失败模型","日志用什么颜色","是否使用泛型"]' answer="1" explain="分布式系统的设计起点是失败模型与一致性目标，它决定了复制、分片、冲突处理等所有后续选择。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段九 · 资深进阶与源码](/advanced/) · [第 103 讲开始 →](/projects/cli-todo)

## 场景地图：分类 × 难度

<ScenarioMatrix />

## KV Server 贯穿线

同一套存储系统连着 4 讲逐步长大，用来体会「架构是被需求逼出来的」：

<FlowTimeline direction="vertical" :steps='[{"title":"① 内存版与接口抽象","desc":"先定义 Command 与 Storage，把可演进的点留出来","tone":"blue"},{"title":"② TCP 协议帧与网络层","desc":"设计帧格式、处理粘包、做连接管理与客户端 SDK","tone":"green"},{"title":"③ WAL、持久化与并发控制","desc":"追加写、崩溃恢复、锁分片与快照","tone":"orange"},{"title":"④ 分片、复制与 Raft","desc":"引入副本与共识，面对网络分区与成员变更","tone":"red"}]' />

<Callout type="idea" title="项目怎么用" text="先看架构蓝图，再自己写一版设计，最后对照关键实现找差异。把边界条件、测试和压测补齐，才算真正做完一个项目。" />
