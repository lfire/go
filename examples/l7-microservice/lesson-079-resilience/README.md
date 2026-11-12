# 第 79 讲 · 熔断、降级、重试与雪崩防护

- **阶段**：L7 · RPC 与微服务
- **目标**：让局部故障不演变成全站故障
- **知识点**：超时与重试预算 / 熔断器状态机 / 降级与兜底 / 隔离与舱壁 / 雪崩推演
- **教程正文**：[第 79 讲 · 熔断、降级、重试与雪崩防护](/microservice/resilience)
- **GitHub 目录**：[l7-microservice/lesson-079-resilience](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-079-resilience)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-079-resilience/demo-01
go run ./lesson-079-resilience/demo-02
go run ./lesson-079-resilience/demo-03
go run ./lesson-079-resilience/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
