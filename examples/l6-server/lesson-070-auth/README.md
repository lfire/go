# 第 070 讲 · 认证授权：Session / JWT / OAuth2 / RBAC

- **阶段**：L6 · 服务端与数据
- **目标**：设计安全且可演进的身份体系
- **知识点**：认证与授权分离 / Session 与 JWT / 令牌刷新与吊销 / OAuth2 流程 / RBAC 与权限模型 / 常见漏洞
- **教程正文**：[第 070 讲 · 认证授权：Session / JWT / OAuth2 / RBAC](/server/auth)
- **GitHub 目录**：[l6-server/lesson-070-auth](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-070-auth)

## 运行

在 `examples/l6-server` 目录下执行：

```bash
go run ./lesson-070-auth/demo-01
go run ./lesson-070-auth/demo-02
go run ./lesson-070-auth/demo-03
go test ./lesson-070-auth/...
```

## 文件说明

| 目录 | 内容 |
| --- | --- |
| `demo-01/` | 章节第 1 段 Go 示例，可直接 `go run` |
| `demo-02/` | 章节第 2 段 Go 示例，可直接 `go run` |
| `demo-03/` | 章节第 3 段 Go 示例，可直接 `go run` |
| `case-04/` | 章节第 4 段测试 / 基准，`go test` 运行 |

> 章节内的代码与讲解仍按教学需要完整保留；这里提供的是可 clone、可运行的工程化版本。
