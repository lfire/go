# 第 072 讲 · RPC 原理与自研最小 RPC

- **阶段**：L7 · RPC 与微服务
- **目标**：从 HTTP 一步步推导出 RPC 要解决什么
- **知识点**：远程调用的本质 / 协议与序列化 /  stub 与编解码 / 超时与错误 / 最小可用实现
- **教程正文**：[第 072 讲 · RPC 原理与自研最小 RPC](/microservice/rpc-basics)
- **GitHub 目录**：[l7-microservice/lesson-072-rpc-basics](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-072-rpc-basics)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-072-rpc-basics/demo-01
go run ./lesson-072-rpc-basics/demo-02
go run ./lesson-072-rpc-basics/demo-03
go run ./lesson-072-rpc-basics/demo-04
go test ./lesson-072-rpc-basics/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |
| `demo-07/` | 章节第 7 段 Go 示例，可直接 `go run` |
| `case-05/` | 章节第 5 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
