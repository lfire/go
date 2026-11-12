# L10 · 工程实战 示例工程

本目录对应教程 **L10** 的 16 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l10-projects
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 103 | [`lesson-103-cli-todo`](./lesson-103-cli-todo/README.md) | 实战 01 · 待办清单 CLI |
| 104 | [`lesson-104-cli-codegen`](./lesson-104-cli-codegen/README.md) | 实战 02 · protoc 插件与代码生成器 |
| 105 | [`lesson-105-net-shortlink`](./lesson-105-net-shortlink/README.md) | 实战 03 · 短链服务 |
| 106 | [`lesson-106-net-image-server`](./lesson-106-net-image-server/README.md) | 实战 04 · 图片服务与对象存储 |
| 107 | [`lesson-107-data-log-etl`](./lesson-107-data-log-etl/README.md) | 实战 05 · 日志分析 ETL 管道 |
| 108 | [`lesson-108-data-sql-tool`](./lesson-108-data-sql-tool/README.md) | 实战 06 · CSV / SQL 查询工具 |
| 109 | [`lesson-109-concurrent-crawler`](./lesson-109-concurrent-crawler/README.md) | 实战 07 · 并发爬虫：单机 → 并发 → 分布式 |
| 110 | [`lesson-110-concurrent-scheduler`](./lesson-110-concurrent-scheduler/README.md) | 实战 08 · 分布式任务调度系统 |
| 111 | [`lesson-111-kv-1-memory`](./lesson-111-kv-1-memory/README.md) | 实战 09 · KV 贯穿线 ① 内存版与接口抽象 |
| 112 | [`lesson-112-kv-2-network`](./lesson-112-kv-2-network/README.md) | 实战 10 · KV 贯穿线 ② TCP 协议帧与网络层 |
| 113 | [`lesson-113-kv-3-async`](./lesson-113-kv-3-async/README.md) | 实战 11 · KV 贯穿线 ③ WAL、持久化与并发控制 |
| 114 | [`lesson-114-kv-4-distributed`](./lesson-114-kv-4-distributed/README.md) | 实战 12 · KV 贯穿线 ④ 分片、复制与 Raft |
| 115 | [`lesson-115-micro-ecommerce`](./lesson-115-micro-ecommerce/README.md) | 实战 13 · 电商微服务系统 |
| 116 | [`lesson-116-micro-seckill`](./lesson-116-micro-seckill/README.md) | 实战 14 · 秒杀与高并发库存 |
| 117 | [`lesson-117-cloud-operator`](./lesson-117-cloud-operator/README.md) | 实战 15 · Kubernetes Operator 与可观测平台 |
| 118 | [`lesson-118-ai-gateway`](./lesson-118-ai-gateway/README.md) | 实战 16 · LLM 网关与流式推理管道 |
