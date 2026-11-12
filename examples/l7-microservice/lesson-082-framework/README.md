# 第 082 讲 · 自研微服务框架：从 0 到 1

- **阶段**：L7 · RPC 与微服务
- **目标**：把注册、配置、拦截、可观测收成一套框架
- **知识点**：框架边界与需求 / Options 装配 / 服务生命周期 / 统一错误与日志 / 可观测内建 / 与 kratos/go-zero 对照
- **教程正文**：[第 082 讲 · 自研微服务框架：从 0 到 1](/microservice/framework)
- **GitHub 目录**：[l7-microservice/lesson-082-framework](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-082-framework)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-082-framework/demo-01
go run ./lesson-082-framework/demo-02
go run ./lesson-082-framework/demo-03
go test ./lesson-082-framework/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
