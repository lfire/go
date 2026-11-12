# 第 28 讲 · 切片底层：三元组、扩容与共享陷阱

- **阶段**：L3 · 核心机制与运行时
- **目标**：彻底掌握 slice 的内存行为
- **知识点**：SliceHeader / 共享底层数组 / 扩容策略与版本差异 / append 别名陷阱 / full slice expression
- **教程正文**：[第 28 讲 · 切片底层：三元组、扩容与共享陷阱](/core/slice-internals)
- **GitHub 目录**：[l3-core/lesson-028-slice-internals](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-028-slice-internals)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-028-slice-internals/demo-01
go run ./lesson-028-slice-internals/demo-02
go run ./lesson-028-slice-internals/demo-03
go run ./lesson-028-slice-internals/demo-04
go test ./lesson-028-slice-internals/...
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
| `case-08/` | 章节第 8 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
