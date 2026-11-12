# 第 032 讲 · 内存分配器：mcache / mcentral / mheap

- **阶段**：L3 · 核心机制与运行时
- **目标**：建立 Go 内存分配全景
- **知识点**：size class / span 与 object / 三级缓存 / tiny allocator / 大对象路径
- **教程正文**：[第 032 讲 · 内存分配器：mcache / mcentral / mheap](/core/allocator)
- **GitHub 目录**：[l3-core/lesson-032-allocator](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-032-allocator)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-032-allocator/demo-01
go run ./lesson-032-allocator/demo-02
go run ./lesson-032-allocator/demo-03
go run ./lesson-032-allocator/demo-04
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
