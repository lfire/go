# Go 零基础通关 · 配套示例工程

教程里每一讲的代码，除了在章节中按教学需要完整讲解外，这里另提供一份
**可 clone、可编译、可运行**的工程化版本。两者内容一致，用途不同：
章节负责讲清楚「为什么」，这里负责让你「跑起来」。

仓库：[github.com/lfire/go](https://github.com/lfire/go) · 分支 `main` · 共 118 讲。

## 使用

```bash
git clone https://github.com/lfire/go.git
cd go/examples/<阶段目录>
go build ./...
go test ./...
go run ./lesson-001-opening/demo-01   # 按讲次 README 里的命令运行
```

每个阶段是独立的 Go module（Go 1.21+），互不干扰；
每讲目录下 `demo-NN/` 是可运行程序、`case-NN/` 是测试与基准、`pkg-*/` 是库代码。

> 少数 `demo-NN/` 是章节里**刻意演示错误写法**的教学片段（源码注释会标明，例如用错 `fmt` 格式符、`string(int)` 的码点陷阱），`go vet` / `go test` 会对它们告警，属预期。批量验证请用 `go build ./...` 与 `go test -vet=off ./...`；各讲 README 也标注了这些目录。

## 阶段索引

| 阶段 | 讲数 | 目录 |
| --- | --- | --- |
| L0 · 导学与学习路线 | 4 | [`examples/l0-guide`](./l0-guide/README.md) |
| L1 · 编程地基 | 8 | [`examples/l1-foundation`](./l1-foundation/README.md) |
| L10 · 工程实战 | 16 | [`examples/l10-projects`](./l10-projects/README.md) |
| L2 · 语法核心 | 13 | [`examples/l2-basics`](./l2-basics/README.md) |
| L3 · 核心机制与运行时 | 12 | [`examples/l3-core`](./l3-core/README.md) |
| L4 · 并发编程 | 12 | [`examples/l4-concurrency`](./l4-concurrency/README.md) |
| L5 · 工程化与质量 | 11 | [`examples/l5-engineering`](./l5-engineering/README.md) |
| L6 · 服务端与数据 | 11 | [`examples/l6-server`](./l6-server/README.md) |
| L7 · RPC 与微服务 | 11 | [`examples/l7-microservice`](./l7-microservice/README.md) |
| L8 · 分布式与云原生 | 11 | [`examples/l8-distributed`](./l8-distributed/README.md) |
| L9 · 资深进阶与源码 | 9 | [`examples/l9-advanced`](./l9-advanced/README.md) |

## 与章节的对应

每讲正文的「动手练一练」前都放了本节示例工程的 GitHub 链接；
每讲目录的 `README.md` 也列出了该讲每段代码对应的目录名。
