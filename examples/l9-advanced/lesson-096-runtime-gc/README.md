# 第 96 讲 · GC 源码与真实调优案例

- **阶段**：L9 · 资深进阶与源码
- **目标**：从 gcStart 到 gcMarkDone 看懂一次 GC
- **知识点**：触发条件 / 后台标记 worker / 辅助标记与屏障 / 调步算法 / GOGC/MemoryLimit 实战
- **教程正文**：[第 96 讲 · GC 源码与真实调优案例](/advanced/runtime-gc)
- **GitHub 目录**：[l9-advanced/lesson-096-runtime-gc](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-096-runtime-gc)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-096-runtime-gc/demo-01
go run ./lesson-096-runtime-gc/demo-02
go run ./lesson-096-runtime-gc/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
