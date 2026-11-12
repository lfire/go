# L3 · 核心机制与运行时 示例工程

本目录对应教程 **L3** 的 12 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l3-core
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 026 | [`lesson-026-value-semantics`](./lesson-026-value-semantics/README.md) | 值语义深潜：赋值、传参、拷贝成本 |
| 027 | [`lesson-027-escape-analysis`](./lesson-027-escape-analysis/README.md) | 逃逸分析与编译器决策 |
| 028 | [`lesson-028-slice-internals`](./lesson-028-slice-internals/README.md) | 切片底层：三元组、扩容与共享陷阱 |
| 029 | [`lesson-029-map-internals`](./lesson-029-map-internals/README.md) | map 底层：hmap、桶与渐进扩容 |
| 030 | [`lesson-030-interface-internals`](./lesson-030-interface-internals/README.md) | interface 底层：iface / eface 与 nil 陷阱 |
| 031 | [`lesson-031-string-internals`](./lesson-031-string-internals/README.md) | 字符串底层与零拷贝转换 |
| 032 | [`lesson-032-allocator`](./lesson-032-allocator/README.md) | 内存分配器：mcache / mcentral / mheap |
| 033 | [`lesson-033-gc`](./lesson-033-gc/README.md) | 垃圾回收：三色标记、屏障与 GOGC |
| 034 | [`lesson-034-defer-internals`](./lesson-034-defer-internals/README.md) | defer 实现与 open-coded 优化 |
| 035 | [`lesson-035-startup`](./lesson-035-startup/README.md) | 程序启动、init 顺序与包初始化 |
| 036 | [`lesson-036-compiler`](./lesson-036-compiler/README.md) | 编译、链接、SSA 与内联 |
| 037 | [`lesson-037-unsafe-cgo`](./lesson-037-unsafe-cgo/README.md) | unsafe、cgo 与跨边界成本 |
