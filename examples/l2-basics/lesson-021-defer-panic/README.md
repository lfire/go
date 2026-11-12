# 第 021 讲 · defer / panic / recover

- **阶段**：L2 · 语法核心
- **目标**：正确做资源清理与异常兜底
- **知识点**：defer LIFO 与参数求值 / 返回值陷阱 / panic 传播 / recover 只在 defer 有效 / 该不该用 panic
- **教程正文**：[第 021 讲 · defer / panic / recover](/basics/defer-panic)
- **GitHub 目录**：[l2-basics/lesson-021-defer-panic](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-021-defer-panic)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-021-defer-panic/demo-01
go run ./lesson-021-defer-panic/demo-02
go run ./lesson-021-defer-panic/demo-03
go run ./lesson-021-defer-panic/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
