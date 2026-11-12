# 第 033 讲 · 垃圾回收：三色标记、屏障与 GOGC

- **阶段**：L3 · 核心机制与运行时
- **目标**：理解 GC 触发、停顿与调优
- **知识点**：三色标记 / 写屏障与混合屏障 / STW 与并发标记 / GOGC 与 MemoryLimit / GC 友好代码
- **教程正文**：[第 033 讲 · 垃圾回收：三色标记、屏障与 GOGC](/core/gc)
- **GitHub 目录**：[l3-core/lesson-033-gc](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-033-gc)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-033-gc/demo-01
go run ./lesson-033-gc/demo-02
go run ./lesson-033-gc/demo-03
go run ./lesson-033-gc/demo-04
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
