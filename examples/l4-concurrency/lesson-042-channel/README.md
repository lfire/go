# 第 042 讲 · channel：语义、缓冲与关闭

- **阶段**：L4 · 并发编程
- **目标**：用 channel 正确传递数据与信号
- **知识点**：无缓冲/缓冲 / 发送接收阻塞规则 / 关闭与遍历 / 只发送/只接收 / nil channel
- **教程正文**：[第 042 讲 · channel：语义、缓冲与关闭](/concurrency/channel)
- **GitHub 目录**：[l4-concurrency/lesson-042-channel](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-042-channel)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-042-channel/demo-01
go run ./lesson-042-channel/demo-02
go run ./lesson-042-channel/demo-03
go test ./lesson-042-channel/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
