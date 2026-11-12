# 第 088 讲 · Raft 共识与 etcd 实践

- **阶段**：L8 · 分布式与云原生
- **目标**：理解强一致系统如何达成共识
- **知识点**：Leader 选举 / 日志复制 / 安全性与脑裂 / 成员变更 / etcd 读写与 watch / 选主实战
- **教程正文**：[第 088 讲 · Raft 共识与 etcd 实践](/distributed/raft)
- **GitHub 目录**：[l8-distributed/lesson-088-raft](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-088-raft)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-088-raft/demo-01
go run ./lesson-088-raft/demo-02
go run ./lesson-088-raft/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
