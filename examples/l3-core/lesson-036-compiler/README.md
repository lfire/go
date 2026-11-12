# 第 36 讲 · 编译、链接、SSA 与内联

- **阶段**：L3 · 核心机制与运行时
- **目标**：看懂 Go 编译产物与优化决策
- **知识点**：编译流程 / SSA 中间表示 / 内联与预算 / 逃逸分析输出 / go tool objdump 与 nm
- **教程正文**：[第 36 讲 · 编译、链接、SSA 与内联](/core/compiler)
- **GitHub 目录**：[l3-core/lesson-036-compiler](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-036-compiler)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-036-compiler/demo-01
go run ./lesson-036-compiler/demo-02
go run ./lesson-036-compiler/demo-03
go run ./lesson-036-compiler/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
