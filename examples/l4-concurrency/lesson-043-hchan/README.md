# 第 043 讲 · channel 底层 hchan 与收发队列

- **阶段**：L4 · 并发编程
- **目标**：解释 channel 的行为为何如此
- **知识点**：hchan 结构 / sendq/recvq / 缓冲区环形队列 / 突然唤醒与直接拷贝 / select 实现基础
- **教程正文**：[第 043 讲 · channel 底层 hchan 与收发队列](/concurrency/hchan)
- **GitHub 目录**：[l4-concurrency/lesson-043-hchan](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-043-hchan)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-043-hchan/demo-01
go run ./lesson-043-hchan/demo-02
go run ./lesson-043-hchan/demo-03
go test ./lesson-043-hchan/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
