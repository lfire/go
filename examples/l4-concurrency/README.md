# L4 · 并发编程 示例工程

本目录对应教程 **L4** 的 12 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l4-concurrency
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 038 | [`lesson-038-concurrency-intro`](./lesson-038-concurrency-intro/README.md) | 并发与并行：CSP 思想 |
| 039 | [`lesson-039-goroutine`](./lesson-039-goroutine/README.md) | goroutine：创建、栈与生命周期 |
| 040 | [`lesson-040-gmp`](./lesson-040-gmp/README.md) | GMP 调度模型全景 |
| 041 | [`lesson-041-scheduler`](./lesson-041-scheduler/README.md) | 调度循环：抢占、窃取与 handoff |
| 042 | [`lesson-042-channel`](./lesson-042-channel/README.md) | channel：语义、缓冲与关闭 |
| 043 | [`lesson-043-hchan`](./lesson-043-hchan/README.md) | channel 底层 hchan 与收发队列 |
| 044 | [`lesson-044-select`](./lesson-044-select/README.md) | select、超时与多路复用 |
| 045 | [`lesson-045-sync-primitives`](./lesson-045-sync-primitives/README.md) | sync：Mutex / RWMutex / WaitGroup / Once / Cond |
| 046 | [`lesson-046-atomic-pool`](./lesson-046-atomic-pool/README.md) | atomic 与 sync.Pool |
| 047 | [`lesson-047-context`](./lesson-047-context/README.md) | context：取消、超时、值传递 |
| 048 | [`lesson-048-patterns`](./lesson-048-patterns/README.md) | 并发模式：pipeline / fan-out / worker pool |
| 049 | [`lesson-049-race`](./lesson-049-race/README.md) | 数据竞争、内存模型与 race detector |
