# L2 · 语法核心 示例工程

本目录对应教程 **L2** 的 13 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l2-basics
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 013 | [`lesson-013-arrays-slices`](./lesson-013-arrays-slices/README.md) | 数组与切片：定义与操作 |
| 014 | [`lesson-014-control-flow`](./lesson-014-control-flow/README.md) | 控制流：if / for / switch / goto |
| 015 | [`lesson-015-range-pitfalls`](./lesson-015-range-pitfalls/README.md) | for range 与闭包捕获陷阱 |
| 016 | [`lesson-016-maps`](./lesson-016-maps/README.md) | map：哈希、遍历与 Set 技巧 |
| 017 | [`lesson-017-structs`](./lesson-017-structs/README.md) | 结构体、标签与内存对齐 |
| 018 | [`lesson-018-methods`](./lesson-018-methods/README.md) | 方法与接收者：值还是指针 |
| 019 | [`lesson-019-interfaces`](./lesson-019-interfaces/README.md) | 接口：隐式实现与组合 |
| 020 | [`lesson-020-errors`](./lesson-020-errors/README.md) | 错误处理：error、包装与错误链 |
| 021 | [`lesson-021-defer-panic`](./lesson-021-defer-panic/README.md) | defer / panic / recover |
| 022 | [`lesson-022-generics`](./lesson-022-generics/README.md) | 泛型：类型参数与约束 |
| 023 | [`lesson-023-reflect`](./lesson-023-reflect/README.md) | 反射 reflect 与 go generate |
| 024 | [`lesson-024-json`](./lesson-024-json/README.md) | JSON、序列化与常见坑 |
| 025 | [`lesson-025-capstone`](./lesson-025-capstone/README.md) | 阶段综合：内存版 KV 存储 |
