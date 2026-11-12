# L7 · RPC 与微服务 示例工程

本目录对应教程 **L7** 的 11 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l7-microservice
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 072 | [`lesson-072-rpc-basics`](./lesson-072-rpc-basics/README.md) | RPC 原理与自研最小 RPC |
| 073 | [`lesson-073-protobuf`](./lesson-073-protobuf/README.md) | Protobuf 与 gRPC 四种流 |
| 074 | [`lesson-074-grpc-advanced`](./lesson-074-grpc-advanced/README.md) | gRPC 拦截器、metadata、错误与超时 |
| 075 | [`lesson-075-discovery`](./lesson-075-discovery/README.md) | 服务注册发现与健康检查 |
| 076 | [`lesson-076-load-balance`](./lesson-076-load-balance/README.md) | 负载均衡与 resolver 源码 |
| 077 | [`lesson-077-config-center`](./lesson-077-config-center/README.md) | 配置中心与动态配置 |
| 078 | [`lesson-078-gateway`](./lesson-078-gateway/README.md) | API 网关、鉴权与限流 |
| 079 | [`lesson-079-resilience`](./lesson-079-resilience/README.md) | 熔断、降级、重试与雪崩防护 |
| 080 | [`lesson-080-tracing`](./lesson-080-tracing/README.md) | OpenTelemetry 链路追踪 |
| 081 | [`lesson-081-metrics`](./lesson-081-metrics/README.md) | Prometheus / Grafana 与 RED 指标 |
| 082 | [`lesson-082-framework`](./lesson-082-framework/README.md) | 自研微服务框架：从 0 到 1 |
