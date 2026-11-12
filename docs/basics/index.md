---
title: 阶段二 · 语法核心 · 阶段导览
description: 用 Go 写出结构完整、能处理错误的小程序
---

<StageHeader stage="L2 · 语法核心" level="★ → ★★★★" time="约 470 分钟" goal="用 Go 写出结构完整、能处理错误的小程序" prereq="编程地基" />

# 阶段二 · 语法核心

> 这一阶段的目标是「能独立写出结构完整的小程序」。我们把 Go 最常用的数据结构、错误处理、接口与泛型一次讲透，并用两个综合项目收口：内存版 KV 存储既是练习，也是后面贯穿 4 讲的实战线起点。

## 这一阶段学完你能做什么

<CheckList id="goal-L2" title="阶段目标" :items='["能熟练使用切片、map、结构体与方法建模","能用接口做抽象，而不是靠继承","能写出可判定的错误链与资源清理代码","能读懂并写出带类型约束的泛型代码"]' />

<ConceptDiagram type="slice-header" title="阶段二 · 语法核心 · 一张图先看全貌" />

## 讲次安排

本阶段共 **13 讲**（第 013–025 讲），合计约 470 分钟，难度 1–4 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 013 | [数组与切片：定义与操作](/basics/arrays-slices) | ★★☆☆☆ | 35 分钟 | 数组是值 · 切片创建与 make · len/cap |
| 014 | [控制流：if / for / switch / goto](/basics/control-flow) | ★☆☆☆☆ | 30 分钟 | if 初始化语句 · 唯一的 for · switch 自动 break 与 fallthrough |
| 015 | [for range 与闭包捕获陷阱](/basics/range-pitfalls) | ★★★☆☆ | 30 分钟 | range 的副本语义 · 循环变量捕获 · Go 1.22 的变化 |
| 016 | [map：哈希、遍历与 Set 技巧](/basics/maps) | ★★☆☆☆ | 30 分钟 | 创建与 CRUD · comma-ok · 遍历无序 |
| 017 | [结构体、标签与内存对齐](/basics/structs) | ★★☆☆☆ | 35 分钟 | 定义与字面量 · 匿名字段与嵌入 · json tag |
| 018 | [方法与接收者：值还是指针](/basics/methods) | ★★☆☆☆ | 30 分钟 | 方法定义 · 值/指针接收者差别 · 方法集与接口实现 |
| 019 | [接口：隐式实现与组合](/basics/interfaces) | ★★★☆☆ | 40 分钟 | 隐式实现 · 小接口原则 · 接口组合 |
| 020 | [错误处理：error、包装与错误链](/basics/errors) | ★★★☆☆ | 40 分钟 | error 是值 · fmt.Errorf %w · errors.Is/As |
| 021 | [defer / panic / recover](/basics/defer-panic) | ★★★☆☆ | 35 分钟 | defer LIFO 与参数求值 · 返回值陷阱 · panic 传播 |
| 022 | [泛型：类型参数与约束](/basics/generics) | ★★★☆☆ | 40 分钟 | 类型参数语法 · constraints 与 comparable · 泛型函数与类型 |
| 023 | [反射 reflect 与 go generate](/basics/reflect) | ★★★★☆ | 40 分钟 | Type 与 Value · 可设置性与 CanSet · 标签读取 |
| 024 | [JSON、序列化与常见坑](/basics/json) | ★★☆☆☆ | 35 分钟 | encoding/json 编解码 · tag 与 omitempty · 时间/数字精度 |
| 025 | [阶段综合：内存版 KV 存储](/basics/capstone) | ★★★☆☆ | 50 分钟 | 接口抽象 Storage · CRUD 与错误设计 · 并发保护 |

## 怎么学这一阶段

切片与接口是这一阶段的两个分水岭。遇到「结果和我想的不一样」，先去阶段三找底层解释，别靠死记规则。

<Callout type="warn" title="最容易踩的坑" text="错误处理先看 errors.Is / errors.As，再考虑自定义类型；不要一上来就设计庞大的错误码体系。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L2-pre" q="向一个 len=2、cap=2 的切片 append 一个元素后，原切片变量会发生什么？" :options='["一定原地扩容","一定分配到新数组，原切片头不变","编译器报错","取决于是否把 append 的结果赋回去"]' answer="3" explain="append 可能换底层数组，必须用 s = append(s, x) 接住返回值；不接住就等于丢弃新切片头。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段一 · 编程地基](/foundation/) · [第 013 讲开始 →](/basics/arrays-slices) · [下一阶段：阶段三 · 核心机制](/core/)
