# 第 046 讲 · atomic 与 sync.Pool

- **阶段**：L4 · 并发编程
- **目标**：理解无锁与对象复用的收益和代价
- **知识点**：原子操作与 CAS / atomic.Value / sync.Pool 生命周期 / GC 清空 / 适用与误用
- **教程正文**：[第 046 讲 · atomic 与 sync.Pool](/concurrency/atomic-pool)
- **GitHub 目录**：[l4-concurrency/lesson-046-atomic-pool](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-046-atomic-pool)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-046-atomic-pool/demo-01
go run ./lesson-046-atomic-pool/demo-02
go run ./lesson-046-atomic-pool/demo-03
go test ./lesson-046-atomic-pool/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
