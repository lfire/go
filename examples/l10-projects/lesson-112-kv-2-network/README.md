# 第 112 讲 · 实战 10 · KV 贯穿线 ② TCP 协议帧与网络层

- **阶段**：L10 · 工程实战
- **目标**：把单机 KV 变成网络服务
- **知识点**：协议帧设计 / 粘包与编解码 / 连接管理 / 并发处理 / 客户端 SDK
- **教程正文**：[第 112 讲 · 实战 10 · KV 贯穿线 ② TCP 协议帧与网络层](/projects/kv-2-network)
- **GitHub 目录**：[l10-projects/lesson-112-kv-2-network](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-112-kv-2-network)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-112-kv-2-network/demo-11
go run ./lesson-112-kv-2-network/demo-12
go test ./lesson-112-kv-2-network/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-11/` | 章节第 11 段 Go 示例，可直接 `go run` |
| `demo-12/` | 章节第 12 段 Go 示例，可直接 `go run` |
| `pkg-kv/` | 章节第 9 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 章节第 10 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 章节第 14 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 章节第 15 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 第 1, 2, 3, 4, 5, 6, 7, 8, 13 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
