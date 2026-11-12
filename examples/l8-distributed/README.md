# L8 · 分布式与云原生 示例工程

本目录对应教程 **L8** 的 11 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l8-distributed
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 083 | [`lesson-083-cap-base`](./lesson-083-cap-base/README.md) | CAP / BASE 与一致性模型 |
| 084 | [`lesson-084-distributed-lock`](./lesson-084-distributed-lock/README.md) | 分布式锁、幂等与去重 |
| 085 | [`lesson-085-distributed-tx`](./lesson-085-distributed-tx/README.md) | 分布式事务：2PC / TCC / Saga / 消息最终一致 |
| 086 | [`lesson-086-distributed-id`](./lesson-086-distributed-id/README.md) | 分布式 ID、时钟与雪花算法 |
| 087 | [`lesson-087-sharding`](./lesson-087-sharding/README.md) | 一致性哈希、分片与路由 |
| 088 | [`lesson-088-raft`](./lesson-088-raft/README.md) | Raft 共识与 etcd 实践 |
| 089 | [`lesson-089-high-availability`](./lesson-089-high-availability/README.md) | 高可用：多活、容灾与混沌工程 |
| 090 | [`lesson-090-docker`](./lesson-090-docker/README.md) | 容器化：多阶段构建与镜像瘦身 |
| 091 | [`lesson-091-kubernetes`](./lesson-091-kubernetes/README.md) | Kubernetes 部署与服务暴露 |
| 092 | [`lesson-092-gitops`](./lesson-092-gitops/README.md) | Helm / GitOps / 灰度发布 |
| 093 | [`lesson-093-sre`](./lesson-093-sre/README.md) | SRE：SLO、告警、容量与稳定性治理 |
