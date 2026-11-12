# 第 095 讲 · 调度器源码：schedule / findRunnable / sysmon

- **阶段**：L9 · 资深进阶与源码
- **目标**：把 GMP 从概念落到代码
- **知识点**：schedule 主循环 / findRunnable 顺序 / 窃取与全局队列 / sysmon 与抢占 / 关键字段速查
- **教程正文**：[第 095 讲 · 调度器源码：schedule / findRunnable / sysmon](/advanced/runtime-scheduler)
- **GitHub 目录**：[l9-advanced/lesson-095-runtime-scheduler](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-095-runtime-scheduler)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-095-runtime-scheduler/demo-01
go run ./lesson-095-runtime-scheduler/demo-02
go run ./lesson-095-runtime-scheduler/demo-03
go run ./lesson-095-runtime-scheduler/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
