# 第 113 讲 · 实战 11 · KV 贯穿线 ③ WAL、持久化与并发控制

- **阶段**：L10 · 工程实战
- **目标**：让数据落盘且并发安全
- **知识点**：WAL 追加写 / 崩溃恢复 / 锁分片 / 快照 / 一致性校验
- **教程正文**：[第 113 讲 · 实战 11 · KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async)
- **GitHub 目录**：[l10-projects/lesson-113-kv-3-async](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-113-kv-3-async)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go test ./lesson-113-kv-3-async/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `pkg-kv/` | 章节第 10 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 第 1, 2, 3, 4, 5, 6, 7, 8, 9 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
