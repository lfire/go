# 第 117 讲 · 实战 15 · Kubernetes Operator 与可观测平台

- **阶段**：L10 · 工程实战
- **目标**：用 Go 管理 Go 服务
- **知识点**：CRD 与控制器 / Reconcile 循环 / 指标与日志聚合 / 告警规则 / 发布与运维
- **教程正文**：[第 117 讲 · 实战 15 · Kubernetes Operator 与可观测平台](/projects/cloud-operator)
- **GitHub 目录**：[l10-projects/lesson-117-cloud-operator](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-117-cloud-operator)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-117-cloud-operator/demo-01
go run ./lesson-117-cloud-operator/demo-02
go run ./lesson-117-cloud-operator/demo-03
go run ./lesson-117-cloud-operator/demo-04
go test ./lesson-117-cloud-operator/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |
| `demo-07/` | 章节第 7 段 Go 示例，可直接 `go run` |
| `case-08/` | 章节第 8 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
