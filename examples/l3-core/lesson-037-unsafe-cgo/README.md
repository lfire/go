# 第 037 讲 · unsafe、cgo 与跨边界成本

- **阶段**：L3 · 核心机制与运行时
- **目标**：在必要时安全地使用底层能力
- **知识点**：unsafe.Pointer 规则 / uintptr 陷阱 / cgo 调用开销 / 内存所有权 / WASM/插件替代方案
- **教程正文**：[第 037 讲 · unsafe、cgo 与跨边界成本](/core/unsafe-cgo)
- **GitHub 目录**：[l3-core/lesson-037-unsafe-cgo](https://github.com/lfire/go/tree/main/examples/l3-core/lesson-037-unsafe-cgo)

## 运行

在 `examples/l3-core` 目录下执行：

```bash
go run ./lesson-037-unsafe-cgo/demo-01
go run ./lesson-037-unsafe-cgo/demo-02
go run ./lesson-037-unsafe-cgo/demo-03
go run ./lesson-037-unsafe-cgo/demo-04
go test ./lesson-037-unsafe-cgo/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |
| `demo-05/` | 章节第 5 段 Go 示例，可直接 `go run` |
| `demo-06/` | 章节第 6 段 Go 示例，可直接 `go run` |
| `demo-07/` | 章节第 7 段 Go 示例，可直接 `go run` |
| `demo-10/` | 章节第 10 段 Go 示例，可直接 `go run` |
| `pkg-purewb/` | 章节第 9 段测试 / 基准，`go test` 运行 |
| `pkg-purewb/` | 第 8 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
