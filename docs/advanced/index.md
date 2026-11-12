---
title: 阶段九 · 资深进阶与源码 · 阶段导览
description: 具备源码级理解、极致性能调优与架构演进能力
---

<StageHeader stage="L9 · 资深进阶" level="★★★ → ★★★★★" time="约 490 分钟" goal="具备源码级理解、极致性能调优与架构演进能力" prereq="分布式云原生" />

# 阶段九 · 资深进阶与源码

> 资深与熟练的分水岭，是能不能解释「为什么慢」「为什么偶尔错」「改动会波及哪里」。这一阶段直接读 runtime 与编译器源码主线，配合完整性能案例、内存模型与架构演进方法，把前面所有知识收成可迁移的判断力。

## 这一阶段学完你能做什么

<CheckList id="goal-L9" title="阶段目标" :items='["能读懂调度器、GC、channel 的源码主路径","能用内存模型严格判断并发代码是否正确","能走完从 pprof 基线到验证防回归的优化闭环","能主导模块拆分与绞杀者式重构"]' />

<ConceptDiagram type="runtime-startup" title="阶段九 · 资深进阶与源码 · 一张图先看全貌" />

## 讲次安排

本阶段共 **9 讲**（第 94–102 讲），合计约 490 分钟，难度 3–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 94 | [runtime 源码：进程启动与初始化](/advanced/runtime-startup) | ★★★★★ | 55 分钟 | 入口汇编 · 参数与栈初始化 · schedinit |
| 95 | [调度器源码：schedule / findRunnable / sysmon](/advanced/runtime-scheduler) | ★★★★★ | 60 分钟 | schedule 主循环 · findRunnable 顺序 · 窃取与全局队列 |
| 96 | [GC 源码与真实调优案例](/advanced/runtime-gc) | ★★★★★ | 60 分钟 | 触发条件 · 后台标记 worker · 辅助标记与屏障 |
| 97 | [channel / mutex 源码与锁竞争](/advanced/runtime-sync) | ★★★★★ | 55 分钟 | chansend/chanrecv · selectgo · mutex 状态机 |
| 98 | [编译器源码：SSA、逃逸与内联](/advanced/compiler-ssa) | ★★★★★ | 55 分钟 | 编译前端与 IR · SSA 构造与优化 pass · 内联预算 |
| 99 | [Go 内存模型与 happens-before 精解](/advanced/memory-model) | ★★★★★ | 50 分钟 | 官方内存模型定义 · 同步原语的保证 · 数据竞争的形式化 |
| 100 | [极致性能优化：从 pprof 到 30w QPS](/advanced/perf-case) | ★★★★★ | 60 分钟 | 建立基线 · 定位 CPU/分配/锁 · 逐项优化与验证 |
| 101 | [架构设计：模块拆分、演进与重构](/advanced/architecture) | ★★★★★ | 55 分钟 | 识别变化方向 · 模块与边界 · 分层与依赖倒置 |
| 102 | [资深能力图谱、技术选型与持续精进](/advanced/senior-path) | ★★★☆☆ | 40 分钟 | 能力四维模型 · 技术选型方法 · 影响力与协作 |

## 怎么学这一阶段

读源码先抓主流程与关键字段，不要一上来钻进每个分支。配合 GODEBUG 与 trace 观察真实行为，理解会快很多。

<Callout type="warn" title="最容易踩的坑" text="这一阶段建议配合官方源码与提案（Go proposal / design doc）一起看，能理解设计取舍而不只是实现细节。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L9-pre" q="Go 内存模型里，什么才构成可靠的 happens-before？" :options='["两个 goroutine 的语句按源码顺序写","同一次运行里先执行完就一定能被看到","channel 收发、Mutex 加锁解锁、Once 等同步操作建立的序","只要变量是 volatile 类型"]' answer="2" explain="没有同步关系就没有顺序保证；数据竞争下程序行为是未定义的，不能靠观测结果推断。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段八 · 分布式与云原生](/distributed/) · [第 94 讲开始 →](/advanced/runtime-startup) · [下一阶段：阶段十 · 工程实战](/projects/)
