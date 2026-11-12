# 第 061 讲 · net/http 深入：Handler、ServeMux 与中间件链

- **阶段**：L6 · 服务端与数据
- **目标**：不依赖框架也能写清楚 HTTP 服务
- **知识点**：Handler 接口 / ServeMux 路由 / Request/ResponseWriter / 中间件洋葱模型 / 超时与优雅退出
- **教程正文**：[第 061 讲 · net/http 深入：Handler、ServeMux 与中间件链](/server/net-http)
- **GitHub 目录**：[l6-server/lesson-061-net-http](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-061-net-http)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-061-net-http/demo-01
go run ./lesson-061-net-http/demo-02
go run ./lesson-061-net-http/demo-03
go run ./lesson-061-net-http/demo-05
go test ./lesson-061-net-http/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
