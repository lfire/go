# 第 015 讲 · for range 与闭包捕获陷阱

- **阶段**：L2 · 语法核心
- **目标**：避开 Go 面试与生产中最经典的坑
- **知识点**：range 的副本语义 / 循环变量捕获 / Go 1.22 的变化 / 指针切片与 map 遍历 / 实战排查
- **教程正文**：[第 015 讲 · for range 与闭包捕获陷阱](/basics/range-pitfalls)
- **GitHub 目录**：[l2-basics/lesson-015-range-pitfalls](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-015-range-pitfalls)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-015-range-pitfalls/demo-01
go run ./lesson-015-range-pitfalls/demo-02
go run ./lesson-015-range-pitfalls/demo-03
go run ./lesson-015-range-pitfalls/demo-04
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

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
