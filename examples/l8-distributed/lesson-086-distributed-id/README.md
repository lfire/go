# 第 86 讲 · 分布式 ID、时钟与雪花算法

- **阶段**：L8 · 分布式与云原生
- **目标**：生成全局唯一且有序的 ID
- **知识点**：UUID 与自增局限 / Snowflake 结构 / 时钟回拨 / 号段与 Leaf / 趋势递增与索引
- **教程正文**：[第 86 讲 · 分布式 ID、时钟与雪花算法](/distributed/distributed-id)
- **GitHub 目录**：[l8-distributed/lesson-086-distributed-id](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-086-distributed-id)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-086-distributed-id/demo-01
go run ./lesson-086-distributed-id/demo-02
go run ./lesson-086-distributed-id/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
