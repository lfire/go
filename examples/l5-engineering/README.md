# L5 · 工程化与质量 示例工程

本目录对应教程 **L5** 的 11 讲，一个 Go module，
每讲一个子目录，里面的 `demo-NN/` 可直接 `go run`。

```bash
cd examples/l5-engineering
go build ./...
go test ./...
```

| 讲次 | 目录 | 主题 |
| --- | --- | --- |
| 050 | [`lesson-050-project-layout`](./lesson-050-project-layout/README.md) | 项目结构与分层设计 |
| 051 | [`lesson-051-code-style`](./lesson-051-code-style/README.md) | 代码规范、gofmt、vet 与 golangci-lint |
| 052 | [`lesson-052-unit-test`](./lesson-052-unit-test/README.md) | 单元测试与表驱动测试 |
| 053 | [`lesson-053-mock-test`](./lesson-053-mock-test/README.md) | Mock：gomock / sqlmock / httptest |
| 054 | [`lesson-054-bench-fuzz`](./lesson-054-bench-fuzz/README.md) | 基准测试、模糊测试与覆盖率 |
| 055 | [`lesson-055-logging`](./lesson-055-logging/README.md) | 日志：slog / zap 与结构化采样 |
| 056 | [`lesson-056-config`](./lesson-056-config/README.md) | 配置：flag / env / viper / 配置中心 |
| 057 | [`lesson-057-error-design`](./lesson-057-error-design/README.md) | 错误码、错误栈与统一响应 |
| 058 | [`lesson-058-patterns-design`](./lesson-058-patterns-design/README.md) | 设计模式与 Functional Options / wire |
| 059 | [`lesson-059-profiling`](./lesson-059-profiling/README.md) | 性能剖析：pprof / trace / 火焰图 |
| 060 | [`lesson-060-cicd`](./lesson-060-cicd/README.md) | CI/CD、Makefile、代码生成与发布 |
