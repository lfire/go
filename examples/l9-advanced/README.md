# L9 · 资深进阶与源码 示例工程

本目录对应教程 **L9** 的 9 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l9-advanced
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 094 | [`lesson-094-runtime-startup`](./lesson-094-runtime-startup/README.md) | runtime 源码：进程启动与初始化 |
| 095 | [`lesson-095-runtime-scheduler`](./lesson-095-runtime-scheduler/README.md) | 调度器源码：schedule / findRunnable / sysmon |
| 096 | [`lesson-096-runtime-gc`](./lesson-096-runtime-gc/README.md) | GC 源码与真实调优案例 |
| 097 | [`lesson-097-runtime-sync`](./lesson-097-runtime-sync/README.md) | channel / mutex 源码与锁竞争 |
| 098 | [`lesson-098-compiler-ssa`](./lesson-098-compiler-ssa/README.md) | 编译器源码：SSA、逃逸与内联 |
| 099 | [`lesson-099-memory-model`](./lesson-099-memory-model/README.md) | Go 内存模型与 happens-before 精解 |
| 100 | [`lesson-100-perf-case`](./lesson-100-perf-case/README.md) | 极致性能优化：从 pprof 到 30w QPS |
| 101 | [`lesson-101-architecture`](./lesson-101-architecture/README.md) | 架构设计：模块拆分、演进与重构 |
| 102 | [`lesson-102-senior-path`](./lesson-102-senior-path/README.md) | 资深能力图谱、技术选型与持续精进 |
