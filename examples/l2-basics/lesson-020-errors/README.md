# 第 20 讲 · 错误处理：error、包装与错误链

- **阶段**：L2 · 语法核心
- **目标**：写出可诊断、可判定的错误处理
- **知识点**：error 是值 / fmt.Errorf %w / errors.Is/As / 自定义错误类型 / 哨兵错误 / 错误码的边界
- **教程正文**：[第 20 讲 · 错误处理：error、包装与错误链](/basics/errors)
- **GitHub 目录**：[l2-basics/lesson-020-errors](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-020-errors)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-020-errors/demo-01
go run ./lesson-020-errors/demo-02
go run ./lesson-020-errors/demo-03
go run ./lesson-020-errors/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
