# 第 041 讲 · 调度循环：抢占、窃取与 handoff

- **阶段**：L4 · 并发编程
- **目标**：理解调度器如何保持 CPU 忙碌
- **知识点**：schedule 循环 / work stealing / 系统调用与 handoff / 异步抢占 / sysmon 职责
- **教程正文**：[第 041 讲 · 调度循环：抢占、窃取与 handoff](/concurrency/scheduler)
- **GitHub 目录**：[l4-concurrency/lesson-041-scheduler](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-041-scheduler)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-041-scheduler/demo-01
go run ./lesson-041-scheduler/demo-02
go run ./lesson-041-scheduler/demo-03
go test ./lesson-041-scheduler/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
