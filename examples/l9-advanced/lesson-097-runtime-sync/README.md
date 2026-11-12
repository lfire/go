# 第 097 讲 · channel / mutex 源码与锁竞争

- **阶段**：L9 · 资深进阶与源码
- **目标**：解释高并发下的性能拐点
- **知识点**：chansend/chanrecv / selectgo / mutex 状态机 / 饥饿模式与自旋 / 锁竞争定位
- **教程正文**：[第 097 讲 · channel / mutex 源码与锁竞争](/advanced/runtime-sync)
- **GitHub 目录**：[l9-advanced/lesson-097-runtime-sync](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-097-runtime-sync)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-097-runtime-sync/demo-01
go run ./lesson-097-runtime-sync/demo-02
go run ./lesson-097-runtime-sync/demo-03
go run ./lesson-097-runtime-sync/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
