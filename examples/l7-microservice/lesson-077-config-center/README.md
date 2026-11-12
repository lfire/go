# 第 077 讲 · 配置中心与动态配置

- **阶段**：L7 · RPC 与微服务
- **目标**：做到配置变更不重启
- **知识点**：配置模型与命名空间 / 长轮询/监听 / 热更新与回滚 / 灰度与审计 / 客户端容错
- **教程正文**：[第 077 讲 · 配置中心与动态配置](/microservice/config-center)
- **GitHub 目录**：[l7-microservice/lesson-077-config-center](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-077-config-center)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-077-config-center/demo-01
go run ./lesson-077-config-center/demo-02
go run ./lesson-077-config-center/demo-03
go test ./lesson-077-config-center/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
