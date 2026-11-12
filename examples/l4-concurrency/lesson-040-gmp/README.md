# 第 40 讲 · GMP 调度模型全景

- **阶段**：L4 · 并发编程
- **目标**：画出 G、M、P 的关系图
- **知识点**：G/M/P 各自职责 / 全局与本地队列 / P 的数量 / 调度上下文 / 一张图记住模型
- **教程正文**：[第 40 讲 · GMP 调度模型全景](/concurrency/gmp)
- **GitHub 目录**：[l4-concurrency/lesson-040-gmp](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-040-gmp)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-040-gmp/demo-01
go run ./lesson-040-gmp/demo-02
go run ./lesson-040-gmp/demo-03
go test ./lesson-040-gmp/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
