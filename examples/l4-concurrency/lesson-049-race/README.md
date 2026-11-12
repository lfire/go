# 第 049 讲 · 数据竞争、内存模型与 race detector

- **阶段**：L4 · 并发编程
- **目标**：能发现并修复隐蔽的并发 bug
- **知识点**：happens-before / 竞态典型样例 / -race 原理与开销 / 误报与漏报 / 修复策略对比
- **教程正文**：[第 049 讲 · 数据竞争、内存模型与 race detector](/concurrency/race)
- **GitHub 目录**：[l4-concurrency/lesson-049-race](https://github.com/lfire/go/tree/main/examples/l4-concurrency/lesson-049-race)

## 运行

在 `examples/l4-concurrency` 目录下执行：

```bash
go run ./lesson-049-race/demo-01
go run ./lesson-049-race/demo-02
go run ./lesson-049-race/demo-03
go test ./lesson-049-race/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
