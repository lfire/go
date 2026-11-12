# 第 48 讲 · 并发模式：pipeline / fan-out / worker pool

- **阶段**：L4 · 并发编程
- **目标**：把常见并发结构写成可复用套路
- **知识点**：管道与阶段 / 扇出扇入 / worker pool / errgroup / singleflight / 限速与背压
- **教程正文**：[第 48 讲 · 并发模式：pipeline / fan-out / worker pool](/concurrency/patterns)
- **GitHub 目录**：[l4-concurrency/lesson-048-patterns](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-048-patterns)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-048-patterns/demo-01
go run ./lesson-048-patterns/demo-02
go run ./lesson-048-patterns/demo-03
go test ./lesson-048-patterns/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
