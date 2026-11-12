# 第 085 讲 · 分布式事务：2PC / TCC / Saga / 消息最终一致

- **阶段**：L8 · 分布式与云原生
- **目标**：按业务选对一致性方案
- **知识点**：本地事务与 ACID / 2PC 与 XA / TCC 三阶段 / Saga 补偿 / 可靠消息与本地消息表 / DTM/Seata
- **教程正文**：[第 085 讲 · 分布式事务：2PC / TCC / Saga / 消息最终一致](/distributed/distributed-tx)
- **GitHub 目录**：[l8-distributed/lesson-085-distributed-tx](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-085-distributed-tx)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-085-distributed-tx/demo-01
go run ./lesson-085-distributed-tx/demo-02
go run ./lesson-085-distributed-tx/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
