# 第 098 讲 · 编译器源码：SSA、逃逸与内联

- **阶段**：L9 · 资深进阶与源码
- **目标**：理解优化器替你做了什么
- **知识点**：编译前端与 IR / SSA 构造与优化 pass / 内联预算 / 逃逸分析实现 / 查看与验证
- **教程正文**：[第 098 讲 · 编译器源码：SSA、逃逸与内联](/advanced/compiler-ssa)
- **GitHub 目录**：[l9-advanced/lesson-098-compiler-ssa](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-098-compiler-ssa)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-098-compiler-ssa/demo-01
go run ./lesson-098-compiler-ssa/demo-02
go run ./lesson-098-compiler-ssa/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
