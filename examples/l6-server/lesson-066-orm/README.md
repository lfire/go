# 第 066 讲 · GORM / sqlx / sqlc 与迁移

- **阶段**：L6 · 服务端与数据
- **目标**：在 ORM 与原生 SQL 之间做理性选择
- **知识点**：三者对比 / 模型与关联 / 预加载与 N+1 / 迁移工具 / SQL 可观测性
- **教程正文**：[第 066 讲 · GORM / sqlx / sqlc 与迁移](/server/orm)
- **GitHub 目录**：[l6-server/lesson-066-orm](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-066-orm)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-066-orm/demo-01
go run ./lesson-066-orm/demo-02
go run ./lesson-066-orm/demo-03
go test ./lesson-066-orm/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
