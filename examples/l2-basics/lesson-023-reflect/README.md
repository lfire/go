# 第 023 讲 · 反射 reflect 与 go generate

- **阶段**：L2 · 语法核心
- **目标**：理解框架背后的元编程能力
- **知识点**：Type 与 Value / 可设置性与 CanSet / 标签读取 / 反射三定律 / go generate 与代码生成
- **教程正文**：[第 023 讲 · 反射 reflect 与 go generate](/basics/reflect)
- **GitHub 目录**：[l2-basics/lesson-023-reflect](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-023-reflect)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-023-reflect/demo-01
go run ./lesson-023-reflect/demo-02
go run ./lesson-023-reflect/demo-03
go run ./lesson-023-reflect/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
