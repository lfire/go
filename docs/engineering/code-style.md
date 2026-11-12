---
title: 第 51 讲 · 代码规范、gofmt、vet 与 golangci-lint
description: 让代码风格由工具保证而非评审争论，掌握 Effective Go 命名与注释、gofmt / goimports / go vet 与 lint 规则集并接入 CI
---

<StageHeader stage="L5 · 工程化与质量" level="★★☆☆☆" time="35 分钟" goal="让代码风格由工具保证" prereq="第 50 讲 · 项目结构与分层设计" />

# 第 51 讲 · 代码规范、gofmt、vet 与 golangci-lint

> 团队里最没价值的争论，是花半小时讨论缩进该用几个空格、大括号该不该换行。Go 的解法很干脆：把能机械判定的事全部交给工具，人只讨论设计与取舍。这一讲把这套「自动关卡」搭起来，重点不是记住多少条规则，而是理解每道关卡各自负责什么、为什么必须放进 CI 才算数。

## 这一讲你会得到什么
<CheckList id="goal-51" title="本讲目标" :items='["写出符合 Effective Go 的命名与导出注释","分清 gofmt、goimports、go vet、golangci-lint 各自的职责","写出一份克制的 golangci-lint 配置并接入 CI"]' />

## 1. 先建立直觉

把提交代码想成过安检：第一道门整理衣着（gofmt 统一排版），第二道门整理随身物品（goimports 增删并分组 import），第三道门用 X 光看有没有可疑物件（go vet 找 Printf 动词不匹配、锁被拷贝这类隐患），第四道门是汇总各条规定的总检（golangci-lint 一次跑完 errcheck、staticcheck、revive 等规则）。任何一道门没过，行李就退回去，评审席上不再讨论这些。

<ConceptDiagram type="pipeline" title="代码规范的四道自动关卡" labels="源码提交::开发者只写业务|gofmt / goimports::统一排版与 import|go vet + staticcheck::发现可疑写法|golangci-lint::汇总规则集并卡 CI" caption="风格问题交给工具，代码评审只讨论设计与取舍" />

这里有个工程判断：**规则要少而准，并且必须自动执行**。写在 Wiki 里没人看的规范等于没有；开几十条规则再用 `//nolint` 逐行压制，比不开还糟——它让告警失去意义，也掩盖了真正的问题。

## 2. 语法与规则

先看命名与注释。Go 的惯例是短、一致、导出即文档：接收者用一两个字母，不用 `this / self`；包名小写单词、不用下划线；每个导出的类型、函数、常量都紧邻一行以它名字开头的说明注释，`go doc` 直接靠这个生成文档。

```go
// 良好命名与注释的示范：导出标识符一律带文档注释
package main

import (
	"fmt"
	"strings"
)

// User 表示系统中的一个用户。导出类型必须紧邻说明性注释。
type User struct {
	ID   int64
	Name string
}

// DisplayName 返回用于展示的名称，去掉首尾空白并加前缀。
// 接收者用短名 u，不用 this 或 self。
func (u User) DisplayName() string {
	return "用户:" + strings.TrimSpace(u.Name)
}

func main() {
	u := User{ID: 7, Name: "  ada  "}
	fmt.Println(u.DisplayName())
}
```

<OutputBox title="运行结果" output="用户:ada" />

第二块代码处理错误。规范里最容易被跳过、也最该由工具守住的一条：函数返回的 error 不能静默丢弃，包装时要用 `%w` 保留错误链。

```go
// 错误必须被检查并带上下文：errcheck 与 %w 的规范化写法
package main

import (
	"errors"
	"fmt"
)

var errNotFound = errors.New("记录不存在")

// load 模拟一次查询，返回的错误必须被上层处理。
func load(id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("load: 缺少 id: %w", errNotFound)
	}
	return "data-" + id, nil
}

func main() {
	v, err := load("")
	if err != nil {
		// 用 errors.Is 判定，而不是拿字符串去比
		fmt.Println("是否未找到:", errors.Is(err, errNotFound))
		fmt.Println("带上下文的错误:", err)
		return
	}
	fmt.Println(v)
}
```

```text
是否未找到: true
带上下文的错误: load: 缺少 id: 记录不存在
```

第三块演示 gofmt 与 go vet 关注的东西：import 分组、常量 `iota`、以及 Printf 动词与参数类型匹配。这些代码能编译，但写法是否规范由工具说了算。

```go
// gofmt 与 goimports 只管排版与 import，不改变语义
package main

import (
	"fmt"
	"time"
)

// 常量分组与 iota 是 Effective Go 推荐的写法
const (
	LevelDebug int = iota
	LevelInfo
	LevelWarn
)

func main() {
	t := time.Date(2024, 1, 2, 15, 4, 0, 0, time.UTC)
	// 动词与参数匹配，go vet 会检查 Printf 家族的调用
	fmt.Printf("级别=%d 时间=%s\n", LevelWarn, t.Format("15:04"))
}
```

<LineByLine title="逐行看一段规范代码" code="const (\n    LevelDebug int = iota\n    LevelInfo\n    LevelWarn\n)\nfunc main() {\n    fmt.Println(LevelWarn)\n}" :notes='["常量成组声明，便于阅读与扩展","iota 从 0 开始，首个常量显式带类型","后续常量自动沿用上一行的表达式","LevelWarn 因此等于 2","分组结束","入口函数","输出整型常量，无需格式化动词","函数闭合"]' />

## 3. 深入一步

四个工具容易混，关键区别在于「会不会改文件」和「失败时说什么」：

| 工具 | 管什么 | 会改代码吗 | 失败时的典型信息 |
| --- | --- | --- | --- |
| gofmt | 缩进、对齐、括号位置 | 会，原地格式化且幂等 | 无，它只负责改 |
| goimports | 增删与分组 import | 会 | 无 |
| go vet | Printf 动词、copylocks、不可达代码 | 不改 | 报告参数类型与动词不匹配 |
| golangci-lint | 汇总 errcheck、staticcheck、revive 等 | 不改 | 报告未检查的 error 或命名问题 |

<Term word="errcheck" text="golangci-lint 中的一条规则，专门找出被丢弃的 error 返回值，是 Go 项目最该优先开启的检查。" />

<Callout type="warn" title="易错点：把 lint 当审美警察" text="一上来启用全部 linter，再靠满屏的 nolint 注释压掉告警，会让真正的错误淹没在噪音里。正确做法是先开 errcheck、govet、staticcheck、goimports 少数几条，全绿之后再按痛点逐条加。" />

### 深入一步：规则集、误报与可复现实验

以下以 golangci-lint v1.59~v1.61 为准：它是团队用得最多的 v1 分支，配置用 `linters.disable-all: true` 加 `enable` 白名单；v2（2024 年起）把这套改成 `linters.default: none` 并把 `linters-settings` 扁平化，升级时必须迁移，别直接套旧文件。规则大致分四层：正确性问题（errcheck、govet、staticcheck）优先全开且不放宽，风格一致性（goimports、revive）按团队约定配参数，性能与坏味道（ineffassign、gocritic）逐步加，实验性规则先观察再启用。

误报要分类处理，不能一律 `//nolint`。真 bug 就改代码；风格争议改规则参数，例如 revive 的 `var-naming` 允许特定缩写、goimports 用 `local-prefixes` 指定本仓库前缀；第三方生成代码或历史遗留按 `issues.exclude-rules` 的 path 精确排除；确实要压制时写成 `//nolint:errcheck // 关闭只读文件，失败可忽略`，并开启 `nolintlint` 要求带具体 linter 名与理由，防止一行 nolint 压掉全部检查。

可复现实验有四条：`golangci-lint linters` 列出本机可用规则，`golangci-lint run --enable-only=errcheck ./...` 单跑一条看噪声，`golangci-lint config verify`（v1.57 起）校验配置是否合法，`golangci-lint run --new-from-rev=HEAD~1 ./...` 只对相对上一提交新增的行报问题，这是给存量老仓库渐进引入 lint 最有效的手段。生产里让本地与 CI 执行同一条命令、同一个版本，用 `--timeout` 和缓存目录 `~/.cache/golangci-lint` 控制耗时。常见误用是：nolint 不带理由、本地与 CI 版本不一致导致结果漂移、把格式检查与单测混在一个 step 难以定位、以及忘了 `run.tests: true` 漏掉测试文件。

再补两条落地经验。`//nolint` 的作用域是所在行或紧邻的代码块，写成 `//nolint:errcheck` 只压这一条规则，裸 `//nolint` 会把整行所有告警一起吞掉，所以 `nolintlint` 的 `require-specific` 必须开。`issues.exclude-rules` 的 `path` 是正则，按目录排除生成代码时锚定前缀比宽泛匹配安全；`golangci-lint run --fix` 能自动修 goimports 一类问题，但别放进 CI 让它改文件，只用于本地批量整理。`run.skip-dirs` 跳过整个目录，`issues.exclude-dirs` 只压制该目录的告警，二者语义不同别混用。把 `--new-from-rev` 与 `only-new-issues` 配合，可让老仓库 lint 平滑落地。

| 规则 | 负责检查 | 常见误报 / 噪声 | 推荐处理 |
| --- | --- | --- | --- |
| errcheck | 被丢弃的 error | `defer f.Close()` 未检查 | 显式处理或带理由的 nolint |
| govet | Printf 动词、copylocks | 自定义 printf 包装不认识 | 用 printf.funcs 注册 |
| staticcheck | 弃用 API、无效分支 | SA1019 对旧依赖告警 | 升级依赖或按路径排除 |
| revive | 命名、导出注释 | 缩写与注释风格争议 | 配参数，不逐行压制 |
| goimports | import 增删分组 | 与本地前缀冲突 | 设 local-prefixes |
| ineffassign | 无效赋值 | 噪声极少 | 直接修 |

<SequenceDiagram title="一次 PR 的 CI 检查链" caption="格式、静态检查、lint、单测逐道过关，结果回写后才允许合并" :actors='["开发者","GitHub","Actions","gofmt/vet","golangci-lint","go test"]' :steps='[{"from":"开发者","to":"GitHub","label":"git push 或开 PR","kind":"call"},{"from":"GitHub","to":"Actions","label":"触发 workflow","kind":"call"},{"from":"Actions","to":"gofmt/vet","label":"跑格式与 vet","kind":"call"},{"from":"gofmt/vet","to":"Actions","label":"返回结果","kind":"return"},{"from":"Actions","to":"golangci-lint","label":"跑规则集","kind":"call"},{"from":"golangci-lint","to":"golangci-lint","label":"汇总各规则告警","kind":"self"},{"from":"golangci-lint","to":"Actions","label":"报告问题","kind":"return"},{"from":"Actions","to":"go test","label":"跑单测与竞态","kind":"call"},{"from":"go test","to":"Actions","label":"通过","kind":"return"},{"from":"Actions","to":"GitHub","label":"回写检查状态","kind":"async"},{"from":"GitHub","to":"开发者","label":"允许或阻断合并","kind":"return"}]' />

## 4. 工程实践

规范只有在 CI 里强制才成立。下面是一份克制的 `.golangci.yml`：只开必要规则，并允许测试文件里放宽 errcheck。

```yaml
# .golangci.yml
run:
  timeout: 5m
  tests: true
linters:
  disable-all: true
  enable:
    - errcheck       # 不允许静默丢弃 error
    - govet
    - staticcheck
    - ineffassign
    - goimports
    - revive         # 命名、注释、导出规则
issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
```

把它接进流水线，让「格式不对」和「测试挂了」一样阻断合并：

```yaml
# .github/workflows/lint.yml（节选）
- uses: actions/setup-go@v5
  with:
    go-version: '1.22'
- name: 格式与静态检查
  run: |
    test -z "$(gofmt -l .)"
    go vet ./...
    golangci-lint run --timeout 5m
```

还有一点容易被跳过：工具链版本要锁定。`gofmt` 在不同 Go 版本上对个别写法的处理可能变化，linter 升级也会新增规则，若不固定版本，同一份代码今天全绿明天红灯，团队就会开始怀疑门禁本身。把 Go 版本与 golangci-lint 版本一起写进工作流，并让本地通过同一组命令执行，规范才真正稳定。

<Compare title="靠评审，还是靠工具" :panels='[{"label":"写法 A · 人肉保证","code":"// 评审里反复争论缩进\nf, _ := os.Open(path)\n// 错误被丢弃，线上静默失败","note":"风格靠记忆，错误靠自觉，问题往往到线上才暴露"},{"label":"写法 B · 工具 + CI","code":"f, err := os.Open(path)\nif err != nil {\n    return err\n}\ndefer f.Close()","note":"errcheck 卡住丢弃，gofmt 统一排版，合入前必过"}]' />

`test -z "$(gofmt -l .)"` 是常被忽视的一招：`gofmt -l` 会列出所有未格式化的文件，全部合规时输出为空，于是这条命令天然成为一道断言。注意 Go 1.22 起循环变量语义有变化，staticcheck 等工具对旧版本与新版本的提示会不同，升级工具链时最好连 linter 版本一起锁定，避免 CI 结果突然漂移。

### 一份可直接运行的配置与 CI 接入

下面这份配置比入门版多了 printf 注册、nolint 约束与生成代码排除。

```yaml
# .golangci.yml（golangci-lint v1.59+，保存后可直接 golangci-lint run）
run:
  timeout: 5m
  tests: true
  go: '1.22'
linters:
  disable-all: true
  enable:
    - errcheck
    - govet
    - staticcheck
    - ineffassign
    - goimports
    - revive
    - nolintlint
linters-settings:
  govet:
    settings:
      printf:
        funcs:
          - logf
  errcheck:
    check-type-assertions: true
  goimports:
    local-prefixes: github.com/your-org/your-repo
  nolintlint:
    require-explanation: true
    require-specific: true
issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
    - path: ^internal/generated/
      linters:
        - revive
```

```yaml
# .github/workflows/lint.yml（完整可运行）
name: lint
on:
  pull_request:
  push:
    branches: [main]
concurrency:
  group: lint-${{ github.ref }}
  cancel-in-progress: true
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.59.1
          args: --timeout 5m
      - name: 单测与竞态
        run: go test -race ./...
```

```bash
# 本地与 CI 锁定同一版本、执行同一条命令
GOLANGCI_LINT_VERSION=v1.59.1
go install github.com/golangci/golangci-lint/cmd/golangci-lint@$GOLANGCI_LINT_VERSION
golangci-lint config verify                     # 校验 .golangci.yml 是否合法
golangci-lint run --timeout 5m ./...            # 与 CI 完全一致
golangci-lint run --new-from-rev=HEAD~1 ./...   # 存量仓库只卡新增行
```

用一段故意留隐患的代码验证门禁：它能编译，但 `defer f.Close()` 没检查返回值，`logf` 又是 vet 默认不认识的 printf 包装。

```go
package main

import (
	"fmt"
	"os"
)

// logf 是自定义的 printf-like 包装，go vet 默认不认识它
func logf(format string, args ...any) {
	fmt.Printf("[app] "+format+"\n", args...)
}

func readConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() // errcheck 会关注 Close 的错误是否被忽略
	logf("opened %s", path)
	return nil
}

func main() {
	if err := readConfig("config.yaml"); err != nil {
		logf("config error: %v", err)
	}
}
```

```bash
go vet -printf.funcs=logf ./...
golangci-lint run --enable-only=errcheck ./...
```

<OutputBox title="golangci-lint run 的真实告警" output="main.go:18:2: Error return value of f.Close is not checked (errcheck)" note="文件:行:列 + linter 名 + 消息，足够定位。" />

告警行按 `文件:行:列` 定位，括号里的 `errcheck` 是命中的 linter 名；`--enable-only` 可单独复现一条规则。三类误报按上表分流：真 bug 就写成 `if err := f.Close(); err != nil { return err }`；只读文件关闭可忽略时用带理由的 `//nolint:errcheck`，由 `nolintlint` 卡住无理由压制；自定义 `logf` 交给 govet 的 `printf.funcs` 注册，而非关掉整条规则。

<Quiz no="51-3" q="给存量老仓库首次接入 lint，哪条命令能只检查新增代码？" :options='["run --fast","run --new-from-rev=HEAD~1","run --disable-all","run --fix"]' answer="1" explain="--new-from-rev 只报告相对基准提交新增行的问题，让老代码先不阻塞合入；--fast 只减耗时、--fix 自动修、--disable-all 关掉全部规则，都不解决存量噪声。" />

## 动手练一练

<Exercise title="补齐被丢弃的错误" task="下面这段读文件代码丢掉了错误。请改成检查错误并返回，使 errcheck 通过。\nfunc read(path string) []byte {\n    b, _ := os.ReadFile(path)\n    return b\n}" :hints='["把第二个返回值接住，别用下划线丢弃","函数签名要能返回 error","若还要加上下文，可用 fmt.Errorf 加 %w 包装"]' solution="func read(path string) ([]byte, error) {\n    b, err := os.ReadFile(path)\n    if err != nil {\n        return nil, err\n    }\n    return b, nil\n}" explain="接住 error 并原样返回，errcheck 不再报警；需要上下文时再用 %w 包装，调用方即可用 errors.Is 追到原始错误。" />

## 随堂检测

<Quiz no="51-1" q="下面哪项最适合交给 gofmt，而不是靠代码评审？" :options='["函数该不该拆分","缩进、对齐与大括号位置","命名是否达意","错误该不该包装"]' answer="1" explain="gofmt 只做机械排版且幂等；拆分、命名、错误语义属于设计判断，工具不该也不越界。" />

<Predict no="51-2" q="某仓库所有文件都已格式化，执行 gofmt -l . 会输出什么？" :options='["列出全部 .go 文件路径","什么都不输出","输出 gofmt 版本号","报编译错误"]' answer="1" output="（无输出，退出码为 0）" explain="gofmt -l 只列出需要重新格式化的文件；全部合规时输出为空，因此常被写成 test -z 形式的 CI 断言。" />

## 一页纸速记

<KnowledgeCard :points='["导出标识符紧邻一行以其名字开头的注释，接收者用短名","error 必须检查，包装用 %w，判定用 errors.Is","gofmt / goimports 会改文件，vet 与 golangci-lint 只报告","先开 errcheck、govet、staticcheck、goimports 少数几条规则"]' :pitfalls='["规则开太多再用 nolint 逐行压制","规范只写在 Wiki，没有接进 CI 强制","把设计讨论也交给工具，或反过来靠评审争论缩进"]' summary="代码规范的目标是把可机械判定的一致性交给工具，让人的注意力留给设计与取舍。" />

## 小结与下一讲

- Effective Go 的命名与注释追求短、一致、可生成文档。
- 四道关卡职责不同：排版、import、可疑写法、汇总规则，逐层递进。
- 规则少而准，写进配置并接入 CI，才算真正落地。

[下一讲：单元测试与表驱动测试](/engineering/unit-test)
