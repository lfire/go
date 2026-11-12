# 第 30 讲 · interface 底层：iface / eface 与 nil 陷阱

- **阶段**：L3 · 核心机制与运行时
- **目标**：解释接口为何有成本以及经典 nil 问题
- **知识点**：iface 与 eface / itab 与方法表 / 动态派发成本 / typed nil / 断言与反射
- **教程正文**：[第 30 讲 · interface 底层：iface / eface 与 nil 陷阱](/core/interface-internals)
- **GitHub 目录**：[l3-core/lesson-030-interface-internals](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-030-interface-internals)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-030-interface-internals/demo-01
go run ./lesson-030-interface-internals/demo-02
go run ./lesson-030-interface-internals/demo-03
go run ./lesson-030-interface-internals/demo-04
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

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
