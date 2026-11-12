# 第 044 讲 · select、超时与多路复用

- **阶段**：L4 · 并发编程
- **目标**：写出不会卡死的并发控制
- **知识点**：select 随机选择 / default 非阻塞 / time.After 与 timer 泄漏 / done channel / 优先级技巧
- **教程正文**：[第 044 讲 · select、超时与多路复用](/concurrency/select)
- **GitHub 目录**：[l4-concurrency/lesson-044-select](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-044-select)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-044-select/demo-01
go run ./lesson-044-select/demo-02
go run ./lesson-044-select/demo-03
go test ./lesson-044-select/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
