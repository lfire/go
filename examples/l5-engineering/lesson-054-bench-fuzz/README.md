# 第 54 讲 · 基准测试、模糊测试与覆盖率

- **阶段**：L5 · 工程化与质量
- **目标**：用数据而不是感觉谈性能与质量
- **知识点**：Benchmark 写法 / benchstat 对比 / 内存分配指标 / fuzz test / 覆盖率与误区
- **教程正文**：[第 54 讲 · 基准测试、模糊测试与覆盖率](/engineering/bench-fuzz)
- **GitHub 目录**：[l5-engineering/lesson-054-bench-fuzz](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-054-bench-fuzz)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-054-bench-fuzz/demo-04
go run ./lesson-054-bench-fuzz/demo-05
go test ./lesson-054-bench-fuzz/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `case-01/` | 章节第 1 段测试 / 基准，`go test` 运行 |
| `case-02/` | 章节第 2 段测试 / 基准，`go test` 运行 |
| `case-03/` | 章节第 3 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
