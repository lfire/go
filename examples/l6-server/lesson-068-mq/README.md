# 第 68 讲 · 消息队列：Kafka / NATS / 可靠投递

- **阶段**：L6 · 服务端与数据
- **目标**：用消息解耦并保证不丢不重
- **知识点**：队列与发布订阅 / 分区与顺序 / 确认与重试 / 幂等消费 / 事务消息 / 选型对比
- **教程正文**：[第 68 讲 · 消息队列：Kafka / NATS / 可靠投递](/server/mq)
- **GitHub 目录**：[l6-server/lesson-068-mq](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-068-mq)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-068-mq/demo-01
go run ./lesson-068-mq/demo-02
go run ./lesson-068-mq/demo-03
go test ./lesson-068-mq/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
