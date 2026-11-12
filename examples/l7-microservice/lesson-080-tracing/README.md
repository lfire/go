# 第 80 讲 · OpenTelemetry 链路追踪

- **阶段**：L7 · RPC 与微服务
- **目标**：把一次请求跨服务完整串起来
- **知识点**：Trace/Span/Context / 注入与提取 / 采样策略 / gRPC/HTTP 集成 / 与日志关联
- **教程正文**：[第 80 讲 · OpenTelemetry 链路追踪](/microservice/tracing)
- **GitHub 目录**：[l7-microservice/lesson-080-tracing](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-080-tracing)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-080-tracing/demo-01
go run ./lesson-080-tracing/demo-02
go run ./lesson-080-tracing/demo-03
go run ./lesson-080-tracing/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
