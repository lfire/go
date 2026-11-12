---
title: 阶段四 · 并发编程 · 阶段导览
description: 掌握 CSP 与 GMP，能写出正确、可诊断的并发程序
---

<StageHeader stage="L4 · 并发编程" level="★★ → ★★★★★" time="约 510 分钟" goal="掌握 CSP 与 GMP，能写出正确、可诊断的并发程序" prereq="核心机制" />

# 阶段四 · 并发编程

> Go 最吸引人的地方在这里，最容易写出隐蔽 bug 的地方也在这里。我们把 CSP 思想、GMP 调度、channel 底层、同步原语、context 与常见并发模式串成一条线，最后用 race detector 把「我以为是安全的」变成「我能证明是安全的」。

## 这一阶段学完你能做什么

<CheckList id="goal-L4" title="阶段目标" :items='["能画出 GMP 模型并解释一次调度循环","能选对 channel / Mutex / atomic 并说明取舍","能用 context 统一取消一整棵调用树","能用 -race 定位并修复数据竞争"]' />

<ConceptDiagram type="gmp" title="阶段四 · 并发编程 · 一张图先看全貌" />

## 讲次安排

本阶段共 **12 讲**（第 38–49 讲），合计约 510 分钟，难度 2–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 38 | [并发与并行：CSP 思想](/concurrency/concurrency-intro) | ★★☆☆☆ | 30 分钟 | 并发≠并行 · CSP 与共享内存 · Go 的取舍 |
| 39 | [goroutine：创建、栈与生命周期](/concurrency/goroutine) | ★★☆☆☆ | 35 分钟 | go 关键字 · 初始栈与增长 · 调度单位 |
| 40 | [GMP 调度模型全景](/concurrency/gmp) | ★★★★☆ | 50 分钟 | G/M/P 各自职责 · 全局与本地队列 · P 的数量 |
| 41 | [调度循环：抢占、窃取与 handoff](/concurrency/scheduler) | ★★★★★ | 50 分钟 | schedule 循环 · work stealing · 系统调用与 handoff |
| 42 | [channel：语义、缓冲与关闭](/concurrency/channel) | ★★★☆☆ | 40 分钟 | 无缓冲/缓冲 · 发送接收阻塞规则 · 关闭与遍历 |
| 43 | [channel 底层 hchan 与收发队列](/concurrency/hchan) | ★★★★★ | 45 分钟 | hchan 结构 · sendq/recvq · 缓冲区环形队列 |
| 44 | [select、超时与多路复用](/concurrency/select) | ★★★☆☆ | 40 分钟 | select 随机选择 · default 非阻塞 · time.After 与 timer 泄漏 |
| 45 | [sync：Mutex / RWMutex / WaitGroup / Once / Cond](/concurrency/sync-primitives) | ★★★☆☆ | 45 分钟 | 互斥锁与互斥粒度 · 读写锁 · WaitGroup |
| 46 | [atomic 与 sync.Pool](/concurrency/atomic-pool) | ★★★★☆ | 40 分钟 | 原子操作与 CAS · atomic.Value · sync.Pool 生命周期 |
| 47 | [context：取消、超时、值传递](/concurrency/context) | ★★★☆☆ | 40 分钟 | Context 接口 · WithCancel/Timeout/Deadline · 取消传播 |
| 48 | [并发模式：pipeline / fan-out / worker pool](/concurrency/patterns) | ★★★★☆ | 50 分钟 | 管道与阶段 · 扇出扇入 · worker pool |
| 49 | [数据竞争、内存模型与 race detector](/concurrency/race) | ★★★★☆ | 45 分钟 | happens-before · 竞态典型样例 · -race 原理与开销 |

## 怎么学这一阶段

务必打开交互实验台逐语句走一遍 slice 扩容之外的两个场景：GMP 调度与 channel 收发。并发的直觉靠静态文字很难建立。

<Callout type="warn" title="最容易踩的坑" text="不确定是否该用 channel 时，先问「是在传递数据，还是在传递信号，还是只是要互斥」。多数同步需求用 sync 包更直白。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L4-pre" q="无缓冲 channel 的发送操作在什么时刻返回？" :options='["把值放进缓冲区就返回","创建 goroutine 后立即返回","有接收者把值接走时才返回","永远不会返回"]' answer="2" explain="无缓冲 channel 是同步交接：发送方会阻塞到接收方就绪，两者在运行时直接配对拷贝。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段三 · 核心机制](/core/) · [第 38 讲开始 →](/concurrency/concurrency-intro) · [下一阶段：阶段五 · 工程化与质量](/engineering/)
