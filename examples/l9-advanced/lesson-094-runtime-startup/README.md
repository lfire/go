# 第 094 讲 · runtime 源码：进程启动与初始化

- **阶段**：L9 · 资深进阶与源码
- **目标**：读懂 rt0_go 到 main 的路径
- **知识点**：入口汇编 / 参数与栈初始化 / schedinit / 创建 main goroutine / mstart 与调度启动
- **教程正文**：[第 094 讲 · runtime 源码：进程启动与初始化](/advanced/runtime-startup)
- **GitHub 目录**：[l9-advanced/lesson-094-runtime-startup](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-094-runtime-startup)

## 运行

在 `examples/l9-advanced` 目录下执行：

```bash
go run ./lesson-094-runtime-startup/demo-01
go run ./lesson-094-runtime-startup/demo-02
go run ./lesson-094-runtime-startup/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
