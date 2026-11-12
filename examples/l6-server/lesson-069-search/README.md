# 第 069 讲 · Elasticsearch 与倒排索引

- **阶段**：L6 · 服务端与数据
- **目标**：理解搜索为何快以及何时该用
- **知识点**：倒排索引 / 分词与 mapping / 查询 DSL / 写入与刷新 / 数据同步与异构
- **教程正文**：[第 069 讲 · Elasticsearch 与倒排索引](/server/search)
- **GitHub 目录**：[l6-server/lesson-069-search](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-069-search)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-069-search/demo-01
go run ./lesson-069-search/demo-02
go run ./lesson-069-search/demo-03
go test ./lesson-069-search/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
