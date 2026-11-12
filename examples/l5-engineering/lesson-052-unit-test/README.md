# 第 52 讲 · 单元测试与表驱动测试

- **阶段**：L5 · 工程化与质量
- **目标**：写出稳定、可读、可维护的测试
- **知识点**：testing 基础 / 表驱动 / t.Run 子测试 / t.Helper / 断言库取舍 / 可测试性设计
- **教程正文**：[第 52 讲 · 单元测试与表驱动测试](/engineering/unit-test)
- **GitHub 目录**：[l5-engineering/lesson-052-unit-test](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-052-unit-test)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-052-unit-test/demo-02
go test ./lesson-052-unit-test/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `case-01/` | 章节第 1 段测试 / 基准，`go test` 运行 |
| `case-03/` | 章节第 3 段测试 / 基准，`go test` 运行 |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
