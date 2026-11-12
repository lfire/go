# 第 026 讲 · 值语义深潜：赋值、传参、拷贝成本

- **阶段**：L3 · 核心机制与运行时
- **目标**：从机器视角理解 Go 的默认行为
- **知识点**：一切皆值 / 结构体拷贝 / 数组与切片传参差别 / 大对象传递成本 / 编译器优化
- **教程正文**：[第 026 讲 · 值语义深潜：赋值、传参、拷贝成本](/core/value-semantics)
- **GitHub 目录**：[l3-core/lesson-026-value-semantics](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-026-value-semantics)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-026-value-semantics/demo-01
go run ./lesson-026-value-semantics/demo-02
go run ./lesson-026-value-semantics/demo-03
go run ./lesson-026-value-semantics/demo-04
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

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
