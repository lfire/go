# 第 090 讲 · 容器化：多阶段构建与镜像瘦身

- **阶段**：L8 · 分布式与云原生
- **目标**：交付一个小而可复现的 Go 镜像
- **知识点**：静态编译与 CGO_ENABLED / 多阶段 Dockerfile / distroless/scratch / 层缓存 / 非 root 与安全
- **教程正文**：[第 090 讲 · 容器化：多阶段构建与镜像瘦身](/distributed/docker)
- **GitHub 目录**：[l8-distributed/lesson-090-docker](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-090-docker)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-090-docker/demo-01
go run ./lesson-090-docker/demo-02
go run ./lesson-090-docker/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
