# 第 63 讲 · REST API 设计、校验与 OpenAPI

- **阶段**：L6 · 服务端与数据
- **目标**：设计出稳定、可演进的接口
- **知识点**：资源与状态码 / 幂等性 / 分页与过滤 / 参数校验 / OpenAPI 与文档生成 / 版本策略
- **教程正文**：[第 63 讲 · REST API 设计、校验与 OpenAPI](/server/rest-api)
- **GitHub 目录**：[l6-server/lesson-063-rest-api](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-063-rest-api)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-063-rest-api/demo-01
go run ./lesson-063-rest-api/demo-02
go run ./lesson-063-rest-api/demo-03
go test ./lesson-063-rest-api/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
