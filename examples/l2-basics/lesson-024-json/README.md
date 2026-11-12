# 第 024 讲 · JSON、序列化与常见坑

- **阶段**：L2 · 语法核心
- **目标**：写出稳定兼容的 API 数据契约
- **知识点**：encoding/json 编解码 / tag 与 omitempty / 时间/数字精度 / 未知字段与兼容 / 流式编解码
- **教程正文**：[第 024 讲 · JSON、序列化与常见坑](/basics/json)
- **GitHub 目录**：[l2-basics/lesson-024-json](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-024-json)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-024-json/demo-01
go run ./lesson-024-json/demo-02
go run ./lesson-024-json/demo-03
go run ./lesson-024-json/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
