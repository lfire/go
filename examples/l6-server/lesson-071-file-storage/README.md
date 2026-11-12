# 第 71 讲 · 文件、对象存储与断点续传

- **阶段**：L6 · 服务端与数据
- **目标**：处理大文件上传与存储可靠性
- **知识点**：io.Reader/Writer / 分块与秒传 / 对象存储直传 / 校验与重试 / 临时文件清理
- **教程正文**：[第 71 讲 · 文件、对象存储与断点续传](/server/file-storage)
- **GitHub 目录**：[l6-server/lesson-071-file-storage](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-071-file-storage)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-071-file-storage/demo-01
go run ./lesson-071-file-storage/demo-02
go run ./lesson-071-file-storage/demo-03
go test ./lesson-071-file-storage/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
