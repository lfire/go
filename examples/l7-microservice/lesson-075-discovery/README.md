# 第 75 讲 · 服务注册发现与健康检查

- **阶段**：L7 · RPC 与微服务
- **目标**：让服务实例变化对调用方透明
- **知识点**：注册与发现模型 / 心跳与摘除 / Consul/etcd/Nacos / 健康检查 / 客户端与服务端发现
- **教程正文**：[第 75 讲 · 服务注册发现与健康检查](/microservice/discovery)
- **GitHub 目录**：[l7-microservice/lesson-075-discovery](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-075-discovery)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-075-discovery/demo-01
go run ./lesson-075-discovery/demo-02
go run ./lesson-075-discovery/demo-03
go run ./lesson-075-discovery/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
