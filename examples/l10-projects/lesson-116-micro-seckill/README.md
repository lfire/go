# 第 116 讲 · 实战 10 · 秒杀与高并发库存

- **阶段**：L10 · 工程实战
- **目标**：在极端流量下守住正确性
- **知识点**：流量削峰 / 预扣减与最终一致 / 限流与排队 / 热点账户 / 压测与预案
- **教程正文**：[第 116 讲 · 实战 10 · 秒杀与高并发库存](/projects/micro-seckill)
- **GitHub 目录**：[l10-projects/lesson-116-micro-seckill](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-116-micro-seckill)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-116-micro-seckill/demo-01
go run ./lesson-116-micro-seckill/demo-02
go run ./lesson-116-micro-seckill/demo-03
go run ./lesson-116-micro-seckill/demo-04
go test ./lesson-116-micro-seckill/...
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
| `demo-10/` | 章节第 10 段 Go 示例，可直接 `go run` |
| `demo-11/` | 章节第 11 段 Go 示例，可直接 `go run` |
| `case-09/` | 章节第 9 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
