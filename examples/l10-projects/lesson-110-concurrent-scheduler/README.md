# 第 110 讲 · 实战 08 · 分布式任务调度系统

- **阶段**：L10 · 工程实战
- **目标**：实现 Master-Worker 的高可用调度
- **知识点**：任务模型与 cron / etcd 选主 / 租约与故障转移 / 执行器与日志 / 分片与幂等
- **教程正文**：[第 110 讲 · 实战 08 · 分布式任务调度系统](/projects/concurrent-scheduler)
- **GitHub 目录**：[l10-projects/lesson-110-concurrent-scheduler](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-110-concurrent-scheduler)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-110-concurrent-scheduler/demo-01
go run ./lesson-110-concurrent-scheduler/demo-02
go run ./lesson-110-concurrent-scheduler/demo-03
go run ./lesson-110-concurrent-scheduler/demo-04
go test ./lesson-110-concurrent-scheduler/...
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
| `demo-08/` | 章节第 8 段 Go 示例，可直接 `go run` |
| `demo-09/` | 章节第 9 段 Go 示例，可直接 `go run` |
| `case-10/` | 章节第 10 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
