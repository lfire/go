# 第 57 讲 · 错误码、错误栈与统一响应

- **阶段**：L5 · 工程化与质量
- **目标**：设计前后端都能用的错误契约
- **知识点**：错误分类与错误码 / 错误栈封装 / 内部错误到 HTTP/gRPC 状态 / 不泄露细节 / 可观测性
- **教程正文**：[第 57 讲 · 错误码、错误栈与统一响应](/engineering/error-design)
- **GitHub 目录**：[l5-engineering/lesson-057-error-design](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-057-error-design)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-057-error-design/demo-01
go run ./lesson-057-error-design/demo-02
go run ./lesson-057-error-design/demo-03
go run ./lesson-057-error-design/demo-04
go test ./lesson-057-error-design/...
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
