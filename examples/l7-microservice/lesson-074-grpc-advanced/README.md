# 第 74 讲 · gRPC 拦截器、metadata、错误与超时

- **阶段**：L7 · RPC 与微服务
- **目标**：把横切能力统一收进拦截器
- **知识点**：Unary/Stream 拦截器 / metadata 透传 / status 错误码 / Deadline 传播 / 认证与日志
- **教程正文**：[第 74 讲 · gRPC 拦截器、metadata、错误与超时](/microservice/grpc-advanced)
- **GitHub 目录**：[l7-microservice/lesson-074-grpc-advanced](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-074-grpc-advanced)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-074-grpc-advanced/demo-01
go run ./lesson-074-grpc-advanced/demo-02
go run ./lesson-074-grpc-advanced/demo-03
go test ./lesson-074-grpc-advanced/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
