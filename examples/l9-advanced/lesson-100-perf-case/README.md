# 第 100 讲 · 极致性能优化：从 pprof 到 30w QPS

- **阶段**：L9 · 资深进阶与源码
- **目标**：走一遍完整的性能优化闭环
- **知识点**：建立基线 / 定位 CPU/分配/锁 / 逐项优化与验证 / GC 与内存换 CPU / 防止回归
- **教程正文**：[第 100 讲 · 极致性能优化：从 pprof 到 30w QPS](/advanced/perf-case)
- **GitHub 目录**：[l9-advanced/lesson-100-perf-case](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-100-perf-case)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-100-perf-case/demo-01
go run ./lesson-100-perf-case/demo-02
go run ./lesson-100-perf-case/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
