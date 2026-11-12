# 第 59 讲 · 性能剖析：pprof / trace / 火焰图

- **阶段**：L5 · 工程化与质量
- **目标**：建立可复现的性能诊断流程
- **知识点**：CPU/Mem/Block/Mutex profile / 火焰图读法 / trace 与调度延迟 / 线上安全采集 / 优化闭环
- **教程正文**：[第 59 讲 · 性能剖析：pprof / trace / 火焰图](/engineering/profiling)
- **GitHub 目录**：[l5-engineering/lesson-059-profiling](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-059-profiling)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-059-profiling/demo-01
go run ./lesson-059-profiling/demo-02
go run ./lesson-059-profiling/demo-03
go run ./lesson-059-profiling/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
