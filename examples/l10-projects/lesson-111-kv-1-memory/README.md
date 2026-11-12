# 第 111 讲 · 实战 09 · KV 贯穿线 ① 内存版与接口抽象

- **阶段**：L10 · 工程实战
- **目标**：定义可演进的存储抽象
- **知识点**：Command 与 Storage 接口 / 内存实现 / 错误模型 / 基准测试 / 可扩展点
- **教程正文**：[第 111 讲 · 实战 09 · KV 贯穿线 ① 内存版与接口抽象](/projects/kv-1-memory)
- **GitHub 目录**：[l10-projects/lesson-111-kv-1-memory](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-111-kv-1-memory)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-111-kv-1-memory/demo-09
go run ./lesson-111-kv-1-memory/demo-10
go test ./lesson-111-kv-1-memory/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-09/` | 章节第 9 段 Go 示例，可直接 `go run` |
| `demo-10/` | 章节第 10 段 Go 示例，可直接 `go run` |
| `pkg-kv/` | 章节第 7 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 章节第 8 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 章节第 12 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 第 1, 2, 3, 4, 5, 6, 11 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
