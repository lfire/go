# 第 31 讲 · 字符串底层与零拷贝转换

- **阶段**：L3 · 核心机制与运行时
- **目标**：知道字符串转换的代价与安全边界
- **知识点**：string 头部结构 / 不可变与共享 / []byte 转换拷贝 / unsafe 零拷贝 / 适用与风险
- **教程正文**：[第 31 讲 · 字符串底层与零拷贝转换](/core/string-internals)
- **GitHub 目录**：[l3-core/lesson-031-string-internals](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-031-string-internals)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-031-string-internals/demo-01
go run ./lesson-031-string-internals/demo-02
go run ./lesson-031-string-internals/demo-03
go run ./lesson-031-string-internals/demo-04
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
