# 第 025 讲 · 阶段综合：内存版 KV 存储

- **阶段**：L2 · 语法核心
- **目标**：用接口、错误、结构体搭一个可扩展的小系统
- **知识点**：接口抽象 Storage / CRUD 与错误设计 / 并发保护 / 命令解析 / 为后续贯穿线埋点
- **教程正文**：[第 025 讲 · 阶段综合：内存版 KV 存储](/basics/capstone)
- **GitHub 目录**：[l2-basics/lesson-025-capstone](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-025-capstone)

## 运行

在 `examples/l2-basics` 目录下执行：

```bash
go run ./lesson-025-capstone/demo-01
go run ./lesson-025-capstone/demo-02
go run ./lesson-025-capstone/demo-03
go run ./lesson-025-capstone/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
