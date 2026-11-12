---
title: 阶段七 · RPC 与微服务 · 阶段导览
description: 理解并自建微服务基础设施，而不是只会调框架 API
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★ → ★★★★★" time="约 530 分钟" goal="理解并自建微服务基础设施，而不是只会调框架 API" prereq="服务端与数据" />

# 阶段七 · RPC 与微服务

> 这一阶段不满足于「会用 gRPC」。我们从 HTTP 推导出 RPC 要解决的问题，再一路把注册发现、负载均衡、配置中心、网关、熔断、链路追踪、指标监控补齐，最后把它们收进一个自己写的最小微服务框架。

## 这一阶段学完你能做什么

<CheckList id="goal-L7" title="阶段目标" :items='["能从零实现一个最小 RPC 并说明每一步的取舍","能看懂拦截器、metadata、status 与 deadline 的传播","能为服务加上追踪、指标与熔断","能解释主流框架的设计并做定制"]' />

<ConceptDiagram type="discovery" title="阶段七 · RPC 与微服务 · 一张图先看全貌" />

## 讲次安排

本阶段共 **11 讲**（第 072–082 讲），合计约 530 分钟，难度 3–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 072 | [RPC 原理与自研最小 RPC](/microservice/rpc-basics) | ★★★★☆ | 50 分钟 | 远程调用的本质 · 协议与序列化 ·  stub 与编解码 |
| 073 | [Protobuf 与 gRPC 四种流](/microservice/protobuf) | ★★★☆☆ | 45 分钟 | proto 语法与默认值 · 代码生成 · 一元与三种流 |
| 074 | [gRPC 拦截器、metadata、错误与超时](/microservice/grpc-advanced) | ★★★★☆ | 50 分钟 | Unary/Stream 拦截器 · metadata 透传 · status 错误码 |
| 075 | [服务注册发现与健康检查](/microservice/discovery) | ★★★★☆ | 45 分钟 | 注册与发现模型 · 心跳与摘除 · Consul/etcd/Nacos |
| 076 | [负载均衡与 resolver 源码](/microservice/load-balance) | ★★★★☆ | 45 分钟 | resolver/balancer 接口 · pick_first 与 round_robin · 策略对比 |
| 077 | [配置中心与动态配置](/microservice/config-center) | ★★★☆☆ | 40 分钟 | 配置模型与命名空间 · 长轮询/监听 · 热更新与回滚 |
| 078 | [API 网关、鉴权与限流](/microservice/gateway) | ★★★★☆ | 50 分钟 | 网关职责 · 路由与协议转换 · 统一鉴权 |
| 079 | [熔断、降级、重试与雪崩防护](/microservice/resilience) | ★★★★☆ | 50 分钟 | 超时与重试预算 · 熔断器状态机 · 降级与兜底 |
| 080 | [OpenTelemetry 链路追踪](/microservice/tracing) | ★★★★☆ | 50 分钟 | Trace/Span/Context · 注入与提取 · 采样策略 |
| 081 | [Prometheus / Grafana 与 RED 指标](/microservice/metrics) | ★★★☆☆ | 45 分钟 | Counter/Gauge/Histogram · RED/USE 方法 · PromQL 入门 |
| 082 | [自研微服务框架：从 0 到 1](/microservice/framework) | ★★★★★ | 60 分钟 | 框架边界与需求 · Options 装配 · 服务生命周期 |

## 怎么学这一阶段

建议边学边画调用链图：一次请求穿过网关、服务 A、服务 B、数据库，每一步的超时与错误怎么传，画一遍就通了。

<Callout type="warn" title="最容易踩的坑" text="自研框架不是为了替代 kratos / go-zero，而是为了理解它们每个抽象在解决什么问题；生产环境优先用成熟方案。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L7-pre" q="gRPC 的 deadline 为什么要在客户端设置并沿调用链传播？" :options='["为了让服务端日志更好看","避免上游已超时、下游还在无谓地消耗资源","为了自动生成 proto 文件","只影响序列化格式"]' answer="1" explain="截止时间随 context 传播，各层都能及时放弃，这是防止雪崩的基础手段之一。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段六 · 服务端与数据](/server/) · [第 072 讲开始 →](/microservice/rpc-basics) · [下一阶段：阶段八 · 分布式与云原生](/distributed/)
