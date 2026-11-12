# 第 076 讲 · 负载均衡与 resolver 源码

- **阶段**：L7 · RPC 与微服务
- **目标**：理解并定制 gRPC 的负载均衡
- **知识点**：resolver/balancer 接口 / pick_first 与 round_robin / 策略对比 / 自定义实现 / 一致性哈希
- **教程正文**：[第 076 讲 · 负载均衡与 resolver 源码](/microservice/load-balance)
- **GitHub 目录**：[l7-microservice/lesson-076-load-balance](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-076-load-balance)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-076-load-balance/demo-01
go run ./lesson-076-load-balance/demo-02
go run ./lesson-076-load-balance/demo-03
go run ./lesson-076-load-balance/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
