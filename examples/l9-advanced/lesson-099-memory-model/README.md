# 第 99 讲 · Go 内存模型与 happens-before 精解

- **阶段**：L9 · 资深进阶与源码
- **目标**：对并发正确性给出严格依据
- **知识点**：官方内存模型定义 / 同步原语的保证 / 数据竞争的形式化 / 错误示例剖析 / 与硬件内存序关系
- **教程正文**：[第 99 讲 · Go 内存模型与 happens-before 精解](/advanced/memory-model)
- **GitHub 目录**：[l9-advanced/lesson-099-memory-model](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-099-memory-model)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-099-memory-model/demo-01
go run ./lesson-099-memory-model/demo-02
go run ./lesson-099-memory-model/demo-03
go run ./lesson-099-memory-model/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
