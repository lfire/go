# 第 004 讲 · 工具链全景：build / test / fmt / vet / mod

- **阶段**：L0 · 导学与学习路线
- **目标**：掌握日常开发最常用的命令与排查手段
- **知识点**：常用命令地图 / gofmt 与 goimports / go vet 与静态检查 / go mod tidy / go generate 与交叉编译
- **教程正文**：[第 004 讲 · 工具链全景：build / test / fmt / vet / mod](/guide/toolchain)
- **GitHub 目录**：[l0-guide/lesson-004-toolchain](https://github.com/lfire/go/tree/main/examples/l0-guide/lesson-004-toolchain)

## 运行

在 `examples/l0-guide` 目录下执行：

```bash
go run ./lesson-004-toolchain/demo-01
go run ./lesson-004-toolchain/demo-02
go run ./lesson-004-toolchain/demo-03
go run ./lesson-004-toolchain/demo-04
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run`（刻意错误示范，`go vet` 会告警，属预期） |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `demo-04/` | 章节第 4 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
