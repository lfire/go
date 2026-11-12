# 第 91 讲 · Kubernetes 部署与服务暴露

- **阶段**：L8 · 分布式与云原生
- **目标**：把 Go 服务稳定跑在 K8s 上
- **知识点**：Pod/Deployment/Service / ConfigMap/Secret / 探针与优雅退出 / 资源配额 / Ingress 与 HPA
- **教程正文**：[第 91 讲 · Kubernetes 部署与服务暴露](/distributed/kubernetes)
- **GitHub 目录**：[l8-distributed/lesson-091-kubernetes](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-091-kubernetes)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-091-kubernetes/demo-01
go run ./lesson-091-kubernetes/demo-02
go run ./lesson-091-kubernetes/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
