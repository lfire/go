---
title: 阶段五 · 工程化与质量 · 阶段导览
description: 把代码变成可维护、可测试、可发布、可诊断的工程
---

<StageHeader stage="L5 · 工程化" level="★★ → ★★★★" time="约 465 分钟" goal="把代码变成可维护、可测试、可发布、可诊断的工程" prereq="并发编程" />

# 阶段五 · 工程化与质量

> 能跑起来的代码和能交给团队维护的代码之间，隔着这一整个阶段。项目结构、规范、测试、日志、配置、错误契约、依赖注入、性能剖析、CI/CD —— 这些决定了一个 Go 项目半年后还敢不敢改。

## 这一阶段学完你能做什么

<CheckList id="goal-L5" title="阶段目标" :items='["能搭出依赖方向清晰的项目骨架","能写表驱动测试、mock 外部依赖并读懂覆盖率","能设计前后端共用的错误码与结构化日志","能用 pprof / trace 走完一次性能诊断闭环"]' />

<ConceptDiagram type="test-pyramid" title="阶段五 · 工程化与质量 · 一张图先看全貌" />

## 讲次安排

本阶段共 **11 讲**（第 050–060 讲），合计约 465 分钟，难度 2–4 级。

| 讲次 | 标题 | 难度 | 时长 | 核心要点 |
| --- | --- | --- | --- | --- |
| 050 | [项目结构与分层设计](/engineering/project-layout) | ★★★☆☆ | 40 分钟 | cmd/internal/pkg/api · 按领域还是按层 · 依赖方向 |
| 051 | [代码规范、gofmt、vet 与 golangci-lint](/engineering/code-style) | ★★☆☆☆ | 35 分钟 | Effective Go 要点 · 命名与注释 · gofmt/goimports |
| 052 | [单元测试与表驱动测试](/engineering/unit-test) | ★★★☆☆ | 40 分钟 | testing 基础 · 表驱动 · t.Run 子测试 |
| 053 | [Mock：gomock / sqlmock / httptest](/engineering/mock-test) | ★★★★☆ | 45 分钟 | 依赖倒置与接口 · gomock 生成 · sqlmock |
| 054 | [基准测试、模糊测试与覆盖率](/engineering/bench-fuzz) | ★★★☆☆ | 40 分钟 | Benchmark 写法 · benchstat 对比 · 内存分配指标 |
| 055 | [日志：slog / zap 与结构化采样](/engineering/logging) | ★★★☆☆ | 40 分钟 | 结构化日志 · 级别与上下文 · trace_id 关联 |
| 056 | [配置：flag / env / viper / 配置中心](/engineering/config) | ★★★☆☆ | 40 分钟 | 十二要素与配置外置 · flag 与环境变量 · viper |
| 057 | [错误码、错误栈与统一响应](/engineering/error-design) | ★★★★☆ | 45 分钟 | 错误分类与错误码 · 错误栈封装 · 内部错误到 HTTP/gRPC 状态 |
| 058 | [设计模式与 Functional Options / wire](/engineering/patterns-design) | ★★★★☆ | 45 分钟 | Functional Options · 工厂与单例 · 策略与中间件 |
| 059 | [性能剖析：pprof / trace / 火焰图](/engineering/profiling) | ★★★★☆ | 50 分钟 | CPU/Mem/Block/Mutex profile · 火焰图读法 · trace 与调度延迟 |
| 060 | [CI/CD、Makefile、代码生成与发布](/engineering/cicd) | ★★★☆☆ | 45 分钟 | Makefile 与脚本 · GitHub Actions · go generate 与代码生成 |

## 怎么学这一阶段

这一阶段的内容最好配一个自己的小项目同步实践：每学一讲就回去改一点，比只看例子吸收得多。

<Callout type="warn" title="最容易踩的坑" text="性能优化必须有基线：先 go test -bench，再 pprof 定位，改完用 benchstat 对比。凭感觉改代码经常会变慢。" />

## 阶段前测（不记分，先猜再看解析）

<Predict no="L5-pre" q="下面哪个最符合 Go 项目里「可测试性」的做法？" :options='["在业务函数里直接 new 一个数据库连接","把外部依赖抽象成接口，从构造函数传入","把所有逻辑写成包级函数","用全局变量保存客户端"]' answer="1" explain="依赖倒置 + 构造注入，才能在单测里替换成 mock，而不碰真实数据库或网络。" />

## 全站学习进度

<ProgressTracker />

[← 上一阶段：阶段四 · 并发编程](/concurrency/) · [第 050 讲开始 →](/engineering/project-layout) · [下一阶段：阶段六 · 服务端与数据](/server/)
