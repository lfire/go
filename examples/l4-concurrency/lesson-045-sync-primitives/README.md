# 第 045 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond

- **阶段**：L4 · 并发编程
- **目标**：在共享内存场景选对原语
- **知识点**：互斥锁与互斥粒度 / 读写锁 / WaitGroup / Once 与单例 / Cond 与广播 / 常见死锁
- **教程正文**：[第 045 讲 · sync：Mutex / RWMutex / WaitGroup / Once / Cond](/concurrency/sync-primitives)
- **GitHub 目录**：[l4-concurrency/lesson-045-sync-primitives](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-045-sync-primitives)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-045-sync-primitives/demo-01
go run ./lesson-045-sync-primitives/demo-02
go run ./lesson-045-sync-primitives/demo-03
go test ./lesson-045-sync-primitives/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
