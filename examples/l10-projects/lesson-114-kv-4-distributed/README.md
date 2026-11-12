# 第 114 讲 · 实战 12 · KV 贯穿线 ④ 分片、复制与 Raft

- **阶段**：L10 · 工程实战
- **目标**：把 KV 演进成分布式存储
- **知识点**：分片路由 / 副本复制 / Raft 集成 / 读写一致性 / 成员变更与压测
- **教程正文**：[第 114 讲 · 实战 12 · KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed)
- **GitHub 目录**：[l10-projects/lesson-114-kv-4-distributed](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-114-kv-4-distributed)

## 运行

在 `examples/l10-projects` 目录下执行：

```bash
go test ./lesson-114-kv-4-distributed/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `pkg-kv/` | 章节第 10 段测试 / 基准，`go test` 运行 |
| `pkg-kv/` | 第 1, 2, 3, 4, 5, 6, 7, 8, 9 段库代码 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
