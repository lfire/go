---
title: 阶段三 · 核心机制 · 阶段导览
description: 从「会用」到「知道为什么」：底层数据结构、内存与运行时
---

<StageHeader stage="L3 · 核心机制" level="★★★ → ★★★★★" time="约 505 分钟" goal="从「会用」到「知道为什么」：底层数据结构、内存与运行时" prereq="语法核心" />

# 阶段三 · 核心机制

> 从这里开始，我们从「会用」转向「知道为什么」。切片为什么共享、接口为什么有 nil 陷阱、GC 什么时候触发、defer 为什么变快了 —— 这些问题都能在数据结构与运行时里找到确定答案。这一阶段是后面性能调优与源码精读的地基。

## 这一阶段学完你能做什么

<CheckList id="goal-L3" title="阶段目标" :items='["能看懂 -gcflags=-m 的逃逸分析输出","能解释 slice / map / interface 的内存布局","能画出一次 GC 的三色标记过程并做基础调优","知道编译、链接、unsafe 与 cgo 的边界成本"]' />

<ConceptDiagram type="allocator" title="阶段三 · 核心机制 · 一张图先看全貌" />

## 讲次安排

本阶段共 **12 讲**（第 026–037 讲），合计约 505 分钟，难度 3–5 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 026 | [值语义深潜：赋值、传参、拷贝成本](/core/value-semantics) | ★★★☆☆ | 35 分钟 | 一切皆值 · 结构体拷贝 · 数组与切片传参差别 |
| 027 | [逃逸分析与编译器决策](/core/escape-analysis) | ★★★★☆ | 40 分钟 | 栈分配与逃逸 · 常见逃逸场景 · 查看逃逸分析 |
| 028 | [切片底层：三元组、扩容与共享陷阱](/core/slice-internals) | ★★★★☆ | 45 分钟 | SliceHeader · 共享底层数组 · 扩容策略与版本差异 |
| 029 | [map 底层：hmap、桶与渐进扩容](/core/map-internals) | ★★★★☆ | 45 分钟 | hmap/bmap 结构 · 哈希与桶定位 · 溢出桶 |
| 030 | [interface 底层：iface / eface 与 nil 陷阱](/core/interface-internals) | ★★★★☆ | 45 分钟 | iface 与 eface · itab 与方法表 · 动态派发成本 |
| 031 | [字符串底层与零拷贝转换](/core/string-internals) | ★★★★☆ | 35 分钟 | string 头部结构 · 不可变与共享 · []byte 转换拷贝 |
| 032 | [内存分配器：mcache / mcentral / mheap](/core/allocator) | ★★★★☆ | 45 分钟 | size class · span 与 object · 三级缓存 |
| 033 | [垃圾回收：三色标记、屏障与 GOGC](/core/gc) | ★★★★☆ | 50 分钟 | 三色标记 · 写屏障与混合屏障 · STW 与并发标记 |
| 034 | [defer 实现与 open-coded 优化](/core/defer-internals) | ★★★★☆ | 35 分钟 | 堆分配 defer · 栈上 defer · 开放编码 |
| 035 | [程序启动、init 顺序与包初始化](/core/startup) | ★★★★☆ | 40 分钟 | 依赖图与初始化顺序 · init 的使用边界 · runtime 初始化 |
| 036 | [编译、链接、SSA 与内联](/core/compiler) | ★★★★☆ | 45 分钟 | 编译流程 · SSA 中间表示 · 内联与预算 |
| 037 | [unsafe、cgo 与跨边界成本](/core/unsafe-cgo) | ★★★★★ | 45 分钟 | unsafe.Pointer 规则 · uintptr 陷阱 · cgo 调用开销 |

## 怎么学这一阶段

不要求背源码，重点是建立「代码 → 机器行为」的因果链。每讲配合图示看，比逐行读 runtime 源码有效得多。

<Callout type="warn" title="最容易踩的坑" text="这一阶段的结论都可用命令验证：go build -gcflags=-m、go tool compile -S、go tool objdump。别只记结论。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L3-pre" q="局部变量一定分配在栈上吗？" :options='["一定","不一定，逃逸分析会把它放到堆","只有用 new 才在堆","取决于是否开启 GC"]' answer="1" explain="编译器的逃逸分析决定变量位置：返回局部变量指针、被闭包捕获、存入 interface 等都可能让它逃逸到堆。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段二 · 语法核心](/basics/) · [第 026 讲开始 →](/core/value-semantics) · [下一阶段：阶段四 · 并发编程](/concurrency/)
