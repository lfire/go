# 第 039 讲 · goroutine：创建、栈与生命周期

- **阶段**：L4 · 并发编程
- **目标**：知道协程轻在哪里、会怎么泄漏
- **知识点**：go 关键字 / 初始栈与增长 / 调度单位 / 泄漏与排查 / GOMAXPROCS
- **教程正文**：[第 039 讲 · goroutine：创建、栈与生命周期](/concurrency/goroutine)
- **GitHub 目录**：[l4-concurrency/lesson-039-goroutine](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-039-goroutine)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-039-goroutine/demo-01
go run ./lesson-039-goroutine/demo-02
go run ./lesson-039-goroutine/demo-03
go test ./lesson-039-goroutine/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
