# 第 64 讲 · WebSocket / SSE 与实时推送

- **阶段**：L6 · 服务端与数据
- **目标**：实现长连接场景与连接治理
- **知识点**：协议握手 / gorilla/websocket / 心跳与断线重连 / SSE 取舍 / 广播与连接管理
- **教程正文**：[第 64 讲 · WebSocket / SSE 与实时推送](/server/websocket)
- **GitHub 目录**：[l6-server/lesson-064-websocket](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-064-websocket)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-064-websocket/demo-01
go run ./lesson-064-websocket/demo-02
go run ./lesson-064-websocket/demo-03
go test ./lesson-064-websocket/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
