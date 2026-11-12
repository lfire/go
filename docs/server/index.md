---
title: 阶段六 · 服务端与数据 · 阶段导览
description: 独立交付一个生产可用的后端服务与数据访问层
---

<StageHeader stage="L6 · 服务端与数据" level="★★★ → ★★★★" time="约 505 分钟" goal="独立交付一个生产可用的后端服务与数据访问层" prereq="工程化" />

# 阶段六 · 服务端与数据

> 进入真正的服务端开发。从 net/http 的 Handler 抽象出发，把路由、中间件、REST、长连接、数据库、缓存、消息队列、搜索、认证与文件存储逐一接上，最后你能独立交付一个生产可用的后端服务与数据访问层。

## 这一阶段学完你能做什么

<CheckList id="goal-L6" title="阶段目标" :items='["不依赖框架也能写清 HTTP 服务与中间件链","能正确配置连接池、事务与 ORM","能识别并解决缓存三大经典问题","能设计 JWT / OAuth2 / RBAC 身份体系"]' />

<ConceptDiagram type="http-chain" title="阶段六 · 服务端与数据 · 一张图先看全貌" />

## 讲次安排

本阶段共 **11 讲**（第 061–071 讲），合计约 505 分钟，难度 3–4 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 061 | [net/http 深入：Handler、ServeMux 与中间件链](/server/net-http) | ★★★☆☆ | 45 分钟 | Handler 接口 · ServeMux 路由 · Request/ResponseWriter |
| 062 | [路由与框架：Gin / Echo / Chi](/server/web-framework) | ★★★☆☆ | 40 分钟 | 路由树与分组 · 绑定与校验 · 中间件生态 |
| 063 | [REST API 设计、校验与 OpenAPI](/server/rest-api) | ★★★☆☆ | 45 分钟 | 资源与状态码 · 幂等性 · 分页与过滤 |
| 064 | [WebSocket / SSE 与实时推送](/server/websocket) | ★★★★☆ | 45 分钟 | 协议握手 · gorilla/websocket · 心跳与断线重连 |
| 065 | [database/sql、连接池与事务](/server/database-sql) | ★★★☆☆ | 45 分钟 | 驱动与 DSN · 连接池参数 · Query/Exec 选择 |
| 066 | [GORM / sqlx / sqlc 与迁移](/server/orm) | ★★★☆☆ | 45 分钟 | 三者对比 · 模型与关联 · 预加载与 N+1 |
| 067 | [Redis 缓存：穿透 / 击穿 / 雪崩与一致性](/server/redis) | ★★★★☆ | 50 分钟 | 缓存模式 · 三大经典问题 · TTL 抖动 |
| 068 | [消息队列：Kafka / NATS / 可靠投递](/server/mq) | ★★★★☆ | 50 分钟 | 队列与发布订阅 · 分区与顺序 · 确认与重试 |
| 069 | [Elasticsearch 与倒排索引](/server/search) | ★★★★☆ | 45 分钟 | 倒排索引 · 分词与 mapping · 查询 DSL |
| 070 | [认证授权：Session / JWT / OAuth2 / RBAC](/server/auth) | ★★★★☆ | 50 分钟 | 认证与授权分离 · Session 与 JWT · 令牌刷新与吊销 |
| 071 | [文件、对象存储与断点续传](/server/file-storage) | ★★★☆☆ | 45 分钟 | io.Reader/Writer · 分块与秒传 · 对象存储直传 |

## 怎么学这一阶段

先把 net/http 手写一遍再学框架，你会更容易看懂 Gin 的中间件与 Gin 之外的选择。数据层注意连接的生命周期。

<Callout type="warn" title="最容易踩的坑" text="缓存一致性没有银弹：先明确业务能接受多长的不一致窗口，再决定是删除缓存、延迟双删还是订阅 binlog。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L6-pre" q="sql.DB 是什么？" :options='["一个数据库连接","一个线程安全的连接池","一个 ORM 实例","一个事务对象"]' answer="1" explain="sql.DB 管理的是连接池，应当作为长生命周期对象复用，而不是每次查询都 Open。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段五 · 工程化与质量](/engineering/) · [第 061 讲开始 →](/server/net-http) · [下一阶段：阶段七 · RPC 与微服务](/microservice/)
