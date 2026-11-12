# 第 118 讲 · 实战 12 · LLM 网关与流式推理管道

- **阶段**：L10 · 工程实战
- **目标**：把 Go 的并发优势用在 AI 工程
- **知识点**：多供应商适配 / SSE 流式转发 / 令牌与限流 / 超时与重试 / 成本与可观测
- **教程正文**：[第 118 讲 · 实战 12 · LLM 网关与流式推理管道](/projects/ai-gateway)
- **GitHub 目录**：[l10-projects/lesson-118-ai-gateway](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-118-ai-gateway)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-118-ai-gateway/demo-01
go run ./lesson-118-ai-gateway/demo-02
go run ./lesson-118-ai-gateway/demo-03
go run ./lesson-118-ai-gateway/demo-04
go test ./lesson-118-ai-gateway/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |
| `demo-07/` | 章节第 7 段 Go 示例，可直接 `go run` |
| `case-08/` | 章节第 8 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
