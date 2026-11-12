# L6 · 服务端与数据 示例工程

本目录对应教程 **L6** 的 11 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l6-server
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 061 | [`lesson-061-net-http`](./lesson-061-net-http/README.md) | net/http 深入：Handler、ServeMux 与中间件链 |
| 062 | [`lesson-062-web-framework`](./lesson-062-web-framework/README.md) | 路由与框架：Gin / Echo / Chi |
| 063 | [`lesson-063-rest-api`](./lesson-063-rest-api/README.md) | REST API 设计、校验与 OpenAPI |
| 064 | [`lesson-064-websocket`](./lesson-064-websocket/README.md) | WebSocket / SSE 与实时推送 |
| 065 | [`lesson-065-database-sql`](./lesson-065-database-sql/README.md) | database/sql、连接池与事务 |
| 066 | [`lesson-066-orm`](./lesson-066-orm/README.md) | GORM / sqlx / sqlc 与迁移 |
| 067 | [`lesson-067-redis`](./lesson-067-redis/README.md) | Redis 缓存：穿透 / 击穿 / 雪崩与一致性 |
| 068 | [`lesson-068-mq`](./lesson-068-mq/README.md) | 消息队列：Kafka / NATS / 可靠投递 |
| 069 | [`lesson-069-search`](./lesson-069-search/README.md) | Elasticsearch 与倒排索引 |
| 070 | [`lesson-070-auth`](./lesson-070-auth/README.md) | 认证授权：Session / JWT / OAuth2 / RBAC |
| 071 | [`lesson-071-file-storage`](./lesson-071-file-storage/README.md) | 文件、对象存储与断点续传 |
