# 第 053 讲 · Mock：gomock / sqlmock / httptest

- **阶段**：L5 · 工程化与质量
- **目标**：隔离外部依赖，测真正的逻辑
- **知识点**：依赖倒置与接口 / gomock 生成 / sqlmock / httptest / gomock/gomonkey 边界
- **教程正文**：[第 053 讲 · Mock：gomock / sqlmock / httptest](/engineering/mock-test)
- **GitHub 目录**：[l5-engineering/lesson-053-mock-test](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-053-mock-test)

## 运行

在 `examples/l5-engineering` 目录下执行：

```bash
go run ./lesson-053-mock-test/demo-04
go run ./lesson-053-mock-test/demo-05
go test ./lesson-053-mock-test/...
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
