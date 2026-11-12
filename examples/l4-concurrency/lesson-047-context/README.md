# 第 047 讲 · context：取消、超时、值传递

- **阶段**：L4 · 并发编程
- **目标**：让并发任务可被统一叫停
- **知识点**：Context 接口 / WithCancel/Timeout/Deadline / 取消传播 / context.Value 克制使用 / 服务端标准用法
- **教程正文**：[第 047 讲 · context：取消、超时、值传递](/concurrency/context)
- **GitHub 目录**：[l4-concurrency/lesson-047-context](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-047-context)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-047-context/demo-01
go run ./lesson-047-context/demo-02
go run ./lesson-047-context/demo-03
go test ./lesson-047-context/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
