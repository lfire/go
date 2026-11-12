# 第 87 讲 · 一致性哈希、分片与路由

- **阶段**：L8 · 分布式与云原生
- **目标**：让数据分布可扩展、可迁移
- **知识点**：取模分片的问题 / 一致性哈希与虚拟节点 / 分片键选择 / 扩容与再平衡 / 热点与倾斜
- **教程正文**：[第 87 讲 · 一致性哈希、分片与路由](/distributed/sharding)
- **GitHub 目录**：[l8-distributed/lesson-087-sharding](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-087-sharding)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-087-sharding/demo-01
go run ./lesson-087-sharding/demo-02
go run ./lesson-087-sharding/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
