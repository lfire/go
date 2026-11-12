---
title: 阶段八 · 分布式与云原生 · 阶段导览
description: 从单机思维升级到分布式与云原生思维，能担责稳定性
---

<StageHeader stage="L8 · 分布式云原生" level="★★★ → ★★★★★" time="约 545 分钟" goal="从单机思维升级到分布式与云原生思维，能担责稳定性" prereq="RPC 与微服务" />

# 阶段八 · 分布式与云原生

> 从单机思维切换到分布式与云原生思维：网络会分区、时钟会漂移、机器会宕机。这一阶段处理一致性、事务、共识、高可用、容器、K8s、GitOps 与 SRE，目标是让你能对线上稳定性负责，而不只是把服务部署上去。

## 这一阶段学完你能做什么

<CheckList id="goal-L8" title="阶段目标" :items='["能用 CAP / BASE 判断一致性方案的取舍","能设计幂等、分布式锁与最终一致事务","能交付可复现的小镜像并部署到 K8s","能用 SLO 与错误预算驱动稳定性决策"]' />

<ConceptDiagram type="cap" title="阶段八 · 分布式与云原生 · 一张图先看全貌" />

## 讲次安排

本阶段共 **11 讲**（第 083–093 讲），合计约 545 分钟，难度 3–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 083 | [CAP / BASE 与一致性模型](/distributed/cap-base) | ★★★★☆ | 45 分钟 | CAP 的准确含义 · 一致性强弱谱系 · BASE 与最终一致 |
| 084 | [分布式锁、幂等与去重](/distributed/distributed-lock) | ★★★★☆ | 50 分钟 | SETNX 与租约 · 看门狗与续期 · Redlock 争议 |
| 085 | [分布式事务：2PC / TCC / Saga / 消息最终一致](/distributed/distributed-tx) | ★★★★★ | 60 分钟 | 本地事务与 ACID · 2PC 与 XA · TCC 三阶段 |
| 086 | [分布式 ID、时钟与雪花算法](/distributed/distributed-id) | ★★★☆☆ | 40 分钟 | UUID 与自增局限 · Snowflake 结构 · 时钟回拨 |
| 087 | [一致性哈希、分片与路由](/distributed/sharding) | ★★★★☆ | 45 分钟 | 取模分片的问题 · 一致性哈希与虚拟节点 · 分片键选择 |
| 088 | [Raft 共识与 etcd 实践](/distributed/raft) | ★★★★★ | 55 分钟 | Leader 选举 · 日志复制 · 安全性与脑裂 |
| 089 | [高可用：多活、容灾与混沌工程](/distributed/high-availability) | ★★★★☆ | 50 分钟 | 冗余与故障域 · 多活架构 · 容量与限流 |
| 090 | [容器化：多阶段构建与镜像瘦身](/distributed/docker) | ★★★☆☆ | 45 分钟 | 静态编译与 CGO_ENABLED · 多阶段 Dockerfile · distroless/scratch |
| 091 | [Kubernetes 部署与服务暴露](/distributed/kubernetes) | ★★★★☆ | 55 分钟 | Pod/Deployment/Service · ConfigMap/Secret · 探针与优雅退出 |
| 092 | [Helm / GitOps / 灰度发布](/distributed/gitops) | ★★★★☆ | 50 分钟 | Helm Chart 结构 · GitOps 与 Argo CD · 滚动/蓝绿/金丝雀 |
| 093 | [SRE：SLO、告警、容量与稳定性治理](/distributed/sre) | ★★★★☆ | 50 分钟 | SLI/SLO/错误预算 · 告警降噪 · 容量规划 |

## 怎么学这一阶段

分布式部分的每个方案都要问「失败在中间会怎样」。把补偿、重试、幂等三件事想清楚，比记住框架名字重要。

<Callout type="warn" title="最容易踩的坑" text="Go 静态编译 + 多阶段构建能把运行镜像压到几 MB，这既减小攻击面，也显著加快扩缩容速度。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L8-pre" q="发生网络分区时，CAP 告诉我们的实际取舍是？" :options='["C、A、P 三者必须同时满足","放弃 P，保住 C 和 A","P 无法放弃，只能在 C 与 A 之间权衡","只要重试就都能满足"]' answer="2" explain="分区是客观存在的前提，真正的设计选择是一致性优先还是可用性优先。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段七 · RPC 与微服务](/microservice/) · [第 083 讲开始 →](/distributed/cap-base) · [下一阶段：阶段九 · 资深进阶与源码](/advanced/)
