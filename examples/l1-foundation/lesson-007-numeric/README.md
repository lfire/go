# 第 07 讲 · 数值、布尔、字符与类型转换

- **阶段**：L1 · 编程地基
- **目标**：避开数值溢出与隐式转换的坑
- **知识点**：整型与平台相关类型 / 浮点与精度 / byte/rune / 显式转换 / math 与 strconv
- **教程正文**：[第 07 讲 · 数值、布尔、字符与类型转换](/foundation/numeric)
- **GitHub 目录**：[l1-foundation/lesson-007-numeric](https://github.com/lfire/go/tree/main/examples/l1-foundation/lesson-007-numeric)

## 运行

在 `examples/l1-foundation` 目录下执行：

```bash
go run ./lesson-007-numeric/demo-01
go run ./lesson-007-numeric/demo-02
go run ./lesson-007-numeric/demo-03
go run ./lesson-007-numeric/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run`（刻意错误示范，`go vet` 会告警，属预期） |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
