# 第 38 讲 · 并发与并行：CSP 思想

- **阶段**：L4 · 并发编程
- **目标**：建立正确的并发世界观
- **知识点**：并发≠并行 / CSP 与共享内存 / Go 的取舍 / 串行/并发/并行图示 / 何时不该并发
- **教程正文**：[第 38 讲 · 并发与并行：CSP 思想](/concurrency/concurrency-intro)
- **GitHub 目录**：[l4-concurrency/lesson-038-concurrency-intro](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-038-concurrency-intro)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-038-concurrency-intro/demo-01
go run ./lesson-038-concurrency-intro/demo-02
go run ./lesson-038-concurrency-intro/demo-03
go test ./lesson-038-concurrency-intro/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
