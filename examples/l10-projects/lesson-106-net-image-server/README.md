# 第 106 讲 · 实战 04 · 图片服务与对象存储

- **阶段**：L10 · 工程实战
- **目标**：处理上传、缩略图、缓存与直传
- **知识点**：multipart 与流式读 / 图片处理 / ETag 与缓存 / 对象存储直传 / 限流与清理
- **教程正文**：[第 106 讲 · 实战 04 · 图片服务与对象存储](/projects/net-image-server)
- **GitHub 目录**：[l10-projects/lesson-106-net-image-server](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-106-net-image-server)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go run ./lesson-106-net-image-server/demo-01
go run ./lesson-106-net-image-server/demo-02
go run ./lesson-106-net-image-server/demo-03
go run ./lesson-106-net-image-server/demo-04
go test ./lesson-106-net-image-server/...
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
| `demo-08/` | 章节第 8 段 Go 示例，可直接 `go run` |
| `demo-09/` | 章节第 9 段 Go 示例，可直接 `go run` |
| `case-10/` | 章节第 10 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
