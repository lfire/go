# 第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式

- **阶段**：L10 · 工程实战
- **目标**：同一业务三次架构演进
- **知识点**：单线程版 / worker pool 并发版 / 去重与限速 / 队列化分布式版 / 演进对比
- **教程正文**：[第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler)
- **GitHub 目录**：[l10-projects/lesson-109-concurrent-crawler](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-109-concurrent-crawler)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-109-concurrent-crawler/demo-01
go run ./lesson-109-concurrent-crawler/demo-02
go run ./lesson-109-concurrent-crawler/demo-03
go run ./lesson-109-concurrent-crawler/demo-04
go test ./lesson-109-concurrent-crawler/...
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
