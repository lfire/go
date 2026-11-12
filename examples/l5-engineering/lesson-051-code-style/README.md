# 第 51 讲 · 代码规范、gofmt、vet 与 golangci-lint

- **阶段**：L5 · 工程化与质量
- **目标**：让代码风格由工具保证
- **知识点**：Effective Go 要点 / 命名与注释 / gofmt/goimports / go vet / lint 规则集与 CI 接入
- **教程正文**：[第 51 讲 · 代码规范、gofmt、vet 与 golangci-lint](/engineering/code-style)
- **GitHub 目录**：[l5-engineering/lesson-051-code-style](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-051-code-style)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-051-code-style/demo-01
go run ./lesson-051-code-style/demo-02
go run ./lesson-051-code-style/demo-03
go run ./lesson-051-code-style/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run`（刻意错误示范，`go vet` 会告警，属预期） |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
