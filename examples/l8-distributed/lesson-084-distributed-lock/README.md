# 第 084 讲 · 分布式锁、幂等与去重

- **阶段**：L8 · 分布式与云原生
- **目标**：在并发下守住数据正确性
- **知识点**：SETNX 与租约 / 看门狗与续期 / Redlock 争议 / 幂等键设计 / 去重表与状态机
- **教程正文**：[第 084 讲 · 分布式锁、幂等与去重](/distributed/distributed-lock)
- **GitHub 目录**：[l8-distributed/lesson-084-distributed-lock](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-084-distributed-lock)

## 运行

在 `examples/l8-distributed` 目录下执行：

```bash
go run ./lesson-084-distributed-lock/demo-01
go run ./lesson-084-distributed-lock/demo-02
go run ./lesson-084-distributed-lock/demo-03
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
