# 第 29 讲 · map 底层：hmap、桶与渐进扩容

- **阶段**：L3 · 核心机制与运行时
- **目标**：理解哈希表的实现与性能特征
- **知识点**：hmap/bmap 结构 / 哈希与桶定位 / 溢出桶 / 等量/翻倍扩容 / 迁移与遍历
- **教程正文**：[第 29 讲 · map 底层：hmap、桶与渐进扩容](/core/map-internals)
- **GitHub 目录**：[l3-core/lesson-029-map-internals](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-029-map-internals)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-029-map-internals/demo-01
go run ./lesson-029-map-internals/demo-02
go run ./lesson-029-map-internals/demo-03
go run ./lesson-029-map-internals/demo-04
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
