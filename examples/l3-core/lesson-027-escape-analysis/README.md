# 第 27 讲 · 逃逸分析与编译器决策

- **阶段**：L3 · 核心机制与运行时
- **目标**：看懂 -gcflags -m 并写出少分配的代码
- **知识点**：栈分配与逃逸 / 常见逃逸场景 / 查看逃逸分析 / 指针返回 / 减少堆分配的实践
- **教程正文**：[第 27 讲 · 逃逸分析与编译器决策](/core/escape-analysis)
- **GitHub 目录**：[l3-core/lesson-027-escape-analysis](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-027-escape-analysis)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-027-escape-analysis/demo-01
go run ./lesson-027-escape-analysis/demo-02
go run ./lesson-027-escape-analysis/demo-03
go run ./lesson-027-escape-analysis/demo-04
go test ./lesson-027-escape-analysis/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-08/` | 章节第 8 段 Go 示例，可直接 `go run` |
| `pkg-pgofinal/` | 章节第 7 段测试 / 基准，`go test` 运行 |
| `pkg-pgofinal/` | 第 6 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
