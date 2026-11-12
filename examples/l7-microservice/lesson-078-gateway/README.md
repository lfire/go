# 第 78 讲 · API 网关、鉴权与限流

- **阶段**：L7 · RPC 与微服务
- **目标**：设计统一入口并守住边界
- **知识点**：网关职责 / 路由与协议转换 / 统一鉴权 / 限流算法 / Kong/APISIX 对比 / 自研轻量网关
- **教程正文**：[第 78 讲 · API 网关、鉴权与限流](/microservice/gateway)
- **GitHub 目录**：[l7-microservice/lesson-078-gateway](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-078-gateway)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-078-gateway/demo-01
go run ./lesson-078-gateway/demo-02
go run ./lesson-078-gateway/demo-03
go test ./lesson-078-gateway/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
