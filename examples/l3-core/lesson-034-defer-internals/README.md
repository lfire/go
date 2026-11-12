# 第 34 讲 · defer 实现与 open-coded 优化

- **阶段**：L3 · 核心机制与运行时
- **目标**：知道 defer 为何从慢变快
- **知识点**：堆分配 defer / 栈上 defer / 开放编码 / 性能对比 / 仍需注意的场景
- **教程正文**：[第 34 讲 · defer 实现与 open-coded 优化](/core/defer-internals)
- **GitHub 目录**：[l3-core/lesson-034-defer-internals](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-034-defer-internals)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-034-defer-internals/demo-01
go run ./lesson-034-defer-internals/demo-02
go run ./lesson-034-defer-internals/demo-03
go run ./lesson-034-defer-internals/demo-04
go test ./lesson-034-defer-internals/...
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
| `demo-08/` | 章节第 8 段 Go 示例，可直接 `go run` |
| `demo-09/` | 章节第 9 段 Go 示例，可直接 `go run` |
| `case-10/` | 章节第 10 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
