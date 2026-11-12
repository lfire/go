# 第 008 讲 · 字符串、rune 与高效拼接

- **阶段**：L1 · 编程地基
- **目标**：理解 UTF-8 并写出不乱码、不慢的字符串代码
- **知识点**：string 不可变 / byte 与 rune / range 遍历 / strings.Builder / 拼接性能对比
- **教程正文**：[第 008 讲 · 字符串、rune 与高效拼接](/foundation/strings)
- **GitHub 目录**：[l1-foundation/lesson-008-strings](https://github.com/lfire/go/tree/main/examples/l1-foundation/lesson-008-strings)

## 运行

在 `examples/l1-foundation` 目录下执行：

```bash
go run ./lesson-008-strings/demo-01
go run ./lesson-008-strings/demo-02
go run ./lesson-008-strings/demo-03
go run ./lesson-008-strings/demo-04
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
