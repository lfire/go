# 第 81 讲 · Prometheus / Grafana 与 RED 指标

- **阶段**：L7 · RPC 与微服务
- **目标**：让系统状态可量化、可告警
- **知识点**：Counter/Gauge/Histogram / RED/USE 方法 / PromQL 入门 / 自定义指标 / 告警规则
- **教程正文**：[第 81 讲 · Prometheus / Grafana 与 RED 指标](/microservice/metrics)
- **GitHub 目录**：[l7-microservice/lesson-081-metrics](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-081-metrics)

## 运行

在 `examples/l7-microservice` 目录下执行：

```bash
go run ./lesson-081-metrics/demo-01
go run ./lesson-081-metrics/demo-02
go run ./lesson-081-metrics/demo-03
go run ./lesson-081-metrics/demo-04
go test ./lesson-081-metrics/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `case-05/` | 章节第 5 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
