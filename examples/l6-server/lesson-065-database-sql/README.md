# 第 65 讲 · database/sql、连接池与事务

- **阶段**：L6 · 服务端与数据
- **目标**：写出不漏连接、不误用事务的数据层
- **知识点**：驱动与 DSN / 连接池参数 / Query/Exec 选择 / 事务与隔离级别 / NULL 与扫描
- **教程正文**：[第 65 讲 · database/sql、连接池与事务](/server/database-sql)
- **GitHub 目录**：[l6-server/lesson-065-database-sql](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-065-database-sql)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-065-database-sql/demo-01
go run ./lesson-065-database-sql/demo-02
go run ./lesson-065-database-sql/demo-03
go test ./lesson-065-database-sql/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
