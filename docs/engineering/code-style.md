---
title: 第 051 讲 · 代码规范、gofmt、vet 与 golangci-lint
description: 让代码风格由工具保证而非评审争论，掌握 Effective Go 命名与注释、gofmt / goimports / go vet 与 lint 规则集并接入 CI
---

<StageHeader stage="L5 · 工程化与质量" level="★★☆☆☆" time="35 分钟" goal="让代码风格由工具保证" prereq="第 050 讲 · 项目结构与分层设计" />

# 第 051 讲 · 代码规范、gofmt、vet 与 golangci-lint

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

### 四道关卡为什么这样分工

把「四道门」拆开看，每一道的设计都对应一个具体的失败模式，而不是工具作者随手加的功能。

`gofmt` 刻意做成**没有配置项、且幂等**。一旦允许配置缩进宽度或大括号风格，团队就会回到它本想消灭的那场争论；幂等（格式化两次的结果与一次完全相同）才让 CI 能写出 `test -z "$(gofmt -l .)"` 这样的断言——若输出随运行次数抖动，断言就不可信。它在语法树层重排空白与对齐，不改语义，所以保存时自动格式化不会引入行为变化，这也是编辑器插件敢默认开启的前提。

`goimports` 比 `gofmt` 只多做一件事：增删并分组 import。它需要判断「某个包到底有没有被用到」，因此要加载类型信息，成本更高，也决定了它不能像 `gofmt` 那样内置在编译器工具链里。`go vet` 为什么只读不改：它的结论是启发式的「这里可疑」，存在误报，自动改写会把猜测变成事实；而且 `go test` 默认会跑 vet 的一个子集，它必须快、必须无副作用。`golangci-lint` 并不新增检查能力，它是聚合器：把多个 linter 并行调度、统一输出为「文件:行:列 + 消息 + 规则名」、统一缓存与超时，价值在于「一次调用、一份配置、一致结果」——这正是它能被写进 CI 单条命令的原因。

| 落地方式 | 挡住什么 | 典型漏网场景 | 代价 |
| --- | --- | --- | --- |
| 人工评审争论 | 设计问题 | 缩进、漏查 error 这类机械问题必然看漏 | 占用最贵的人力，且随团队规模失效 |
| 编辑器保存时格式化 | 本机产生的格式问题 | 关掉插件、脚本生成、命令行提交的代码 | 零 CI 成本，但只约束个人环境 |
| pre-commit hook | 提交前的格式与快检查 | 可被跳过，且不覆盖他人 PR | 本地可绕过，不能作为唯一门禁 |
| CI 门禁 | 所有合入代码 | 无，前提是版本锁定 | 需要维护版本与缓存，反馈比本地慢 |

结论是这几层互补而非互斥：编辑器减少往返，hook 提前拦截，CI 是唯一无法绕过的那道。

### 一条能自己跑的格式化实验

`gofmt -l` 只列文件名，看不出「差在哪」；配合 `-w` 与 `cat` 能完整看到格式化前后的样子，并顺手验证幂等。

```bash
printf 'package main\nimport "fmt"\nfunc main(){fmt.Println( "x" )}\n' > /tmp/fmt_demo.go
gofmt -l /tmp/fmt_demo.go   # 第一次：列出未格式化文件
gofmt -w /tmp/fmt_demo.go   # 原地写回
gofmt -l /tmp/fmt_demo.go   # 第二次：应无输出
cat /tmp/fmt_demo.go
```

```text
/tmp/fmt_demo.go
package main

import "fmt"

func main() { fmt.Println("x") }
```

逐字段看：第一行是第一次 `gofmt -l` 列出的文件名；空行之后是 `cat` 打印的格式化结果——package 与 import 之间、import 与函数之间各补了空行，花括号内侧补了空格，字符串两侧多余空格被去掉，但标识符、运算符、字符串内容一个没变，这就是「gofmt 不改语义」的直接证据。第二次 `gofmt -l` 没有任何输出，说明写回后已经合规，再跑一次也不会产生新差异，即幂等。把 `-l` 换成 `-d` 会打印改动前后的统一 diff，适合排查「这一大片空白是谁改的」；`-w` 才真正写文件，因此 CI 里只该用 `-l` 做断言，绝不能用 `-w` 让流水线去改代码。

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

### go vet 为什么只看得见静态形状：从 analysis 框架到 printf 检查器的输入

`go vet` 的每个检查器都是一个 `golang.org/x/tools/go/analysis.Analyzer`。`go vet` 通过 `unitchecker`（即 `go tool vet`）挂进构建流程，拿到的输入是已经解析好的 AST 与 `go/types` 产出的类型信息（`types.Info`）：**程序不执行、表达式不求值、也不做跨语句的常量传播**。因此分析器只看得到静态形状——字面量常量、调用点的函数名、参数的静态类型。以 `printf` 分析器（`go/analysis/passes/printf`）为例：它靠函数名约定（`Printf`、`Sprintf` 等）或 `-printf.funcs` 注册来认定「这是个 printf 调用」，再从第一个参数**必须是常量字符串**里解析 `%d / %s` 等动词，与后续参数的静态类型逐一比对。格式串一旦是变量，就没有常量可解析，检查直接放弃；自定义包装没注册，分析器根本不把它当 printf 调用。

从机制就能推出三条实践结论，且每条都可证伪：本讲配置里写 `printf.funcs` 注册 `logf`，不是「让 vet 更聪明」，而是补上「哪些函数算 printf」这条它无法自行推断的输入；动态拼出的格式串 vet 一定抓不到，因为静态形状里没有字符串常量；`copylocks` 能抓「按值拷贝含锁结构体」，是因为类型信息里能看到字段含 `sync.Locker`，而「加锁顺序导致死锁」要执行时序才判断得了，vet 的输入里根本没有——这正是误解二的机制层依据。vet 刻意保守、只报高置信度问题，也是被这份输入逼出来的：没有运行时信息，激进推断必然误报。

可复现实验（Go 1.23.8 实测）。把三种情形写进同一个文件，看 vet 分别能报什么：

```go
package main

import "fmt"

// logf 是自定义的 printf-like 包装，go vet 默认不认识它。
func logf(format string, args ...any) {
	fmt.Printf("[app] "+format+"\n", args...)
}

func main() {
	fmt.Printf("%d", "x") // 常量格式串：动词与参数静态可判
	format := "%d"
	fmt.Printf(format, "x") // 格式串是变量：静态形状里没有常量
	logf("%d", "x")         // 自定义包装：未注册时 vet 不检查
}
```

```bash
go vet main.go                     # A：不注册 logf
go vet -printf.funcs=logf main.go  # B：把 logf 注册为 printf-like
```

```text
A:
./main.go:11:2: fmt.Printf format %d has arg "x" of wrong type string
B:
./main.go:11:2: fmt.Printf format %d has arg "x" of wrong type string
./main.go:14:2: command-line-arguments.logf format %d has arg "x" of wrong type string
```

逐字段看：A 只报第 11 行——第一个参数是常量字符串 `"%d"`，分析器解析出整数动词，和静态类型 `string` 一比对就判定不匹配；第 12–13 行格式串来自变量 `format`，没有常量可解析，vet 一声不吭；第 14 行 `logf` 未被识别为 printf 调用，也不检查。B 用 `-printf.funcs=logf` 补上这条输入后，第 14 行立刻被报出来，包名前缀 `command-line-arguments` 是单文件 `go vet main.go` 模式下的临时包名，换成 `go vet ./...` 会显示真实包路径，告警内容一致。可见 vet 的检查能力等于「静态形状 + 注册信息」，两者缺一就失效。

**边界与失效条件**：AST 看不到运行时值、反射调用与生成代码里的动态调用；`-printf.funcs` 只补函数名识别，补不了动态格式串；vet 输出的是「可疑」，不是「错误」，所以只读不改、且允许 `-printf.funcs` 这类配置而不是硬编码规则。它证明不了并发时序与逻辑错误（那是 `-race` 与测试的职责）。

**版本前提**：本机 Go 1.23.8，A 中第 13 行不报是这一版本的行为；Go 1.24 起 printf 分析器新增了对「非常量格式串且无其他参数」调用的告警（`non-constant format string`），带参数的变量格式串仍无法核对动词与类型。`golangci-lint v1.59+` 的 `govet` 通过 `linters-settings.govet.settings.printf.funcs` 转发同一参数，升级工具链时要用 `golangci-lint config verify` 复验。

### 跨包结论怎么传递：`go/analysis` 的 facts、vet.cfg 与 `-vettool` 协议

前面说「vet 的每个检查器是一个 `analysis.Analyzer`，输入是 AST 加类型信息」，这句话只描述了**单个包**。可很多检查必须跨包：`printf` 分析器要认出 `a.Logf` 是不是 printf 包装，靠的是 `a` 包里 `Logf` 的定义；`copylocks` 要判断一个外部类型含不含锁，也得看那个类型在别的包里怎么声明。若每个分析器都把整棵依赖树重新解析一遍，`go vet` 会慢到无法放进 `go test` 默认流程。`go/analysis` 用 **facts** 解决这个问题：分析器在依赖包上算出一条条小结论，序列化后沿 import 边传给下游包。

机制分三层，都能在源码里追到。`golang.org/x/tools/go/analysis/analysis.go` 里 `Analyzer` 有两个方向不同的依赖字段：`Requires []*Analyzer` 是「同一个包内，跑我之前先跑谁」，源码注释称它为 horizontal（同一分析器图里的横向依赖）；`FactTypes []Fact` 声明「我会导入 / 导出哪几种 fact」，注释称它 vertical——**同一个分析器、跨不同包**的纵向依赖。一个 fact 是实现了 `analysis.Fact` 接口的指针类型，绑定到某个对象（`types.Object`，比如一个函数）或包上；分析器用 `pass.ExportObjectFact(obj, fact)` 写出、`pass.ImportObjectFact(obj, fact)` 读回。`unitchecker`（`go/analysis/unitchecker`）是把它接进 `go vet` 的胶水：它按 `vet.cfg` 里的配置，从 `PackageVetx` 指向的文件读入上游 facts，把本包算出的 facts 写进 `VetxOutput`，用 gob 序列化（`internal/facts` 的 `NewDecoder` / `Encode`）。

`go vet -x -work` 会把这套协议整条打出来。实验（Go 1.23.8，darwin/arm64；两个包，`a` 里定义 printf 包装 `Logf`，`main` 包调用 `a.Logf("%d", "x")`）：

```bash
GOTMPDIR=$PWD/gotmp go vet -x -work ./... 2>&1 | grep -E 'WORK=|vet.cfg|PackageVetx|VetxOutput|VetxOnly'
```

```text
WORK=/…/go-build949036376
b002/vet.cfg (ID=example.com/facts/a):
  "PackageVetx": { "fmt": "…/go-build/bc/bc21…e5b2d23a8caf69-d" },
  "VetxOnly": false,  "VetxOutput": "…/b002/vet.out"
b001/vet.cfg (ID=example.com/facts):
  "PackageVetx": {
    "example.com/facts/a": "…/b002/vet.out",
    "runtime": "…/go-build/0e/0ec6…7f4a8eaed5aa0b-d"
  },
  "VetxOnly": false,  "VetxOutput": "…/b001/vet.out"
```

逐字段：`ID` 是当前被分析的包；`PackageVetx` 是「包路径 → fact 文件路径」的映射——`b001`（main 包）这项里赫然写着 `example.com/facts/a` 指向 `b002/vet.out`，**它要读的上游 facts 恰好就是 `a` 包 `VetxOutput` 写出的那个文件**，这就是「跨包结论沿 import 边传递」的全部实现；`fmt`、`runtime` 指向 GOCACHE 里 `-d` 结尾的缓存文件，说明标准库的 facts 早已算好并被复用，不必重算。`VetxOnly` 是另一条关键线索：在完整输出里大量依赖包的 cfg 是 `"VetxOnly": true`，意思是「只为产 facts 而分析这个包，不要报它的诊断」，诊断只在 `go vet` 的目标包上出。`-x` 还显示每个包都被单独拉起一次 `…/pkg/tool/darwin_arm64/vet $WORK/bNNN/vet.cfg`——analysis 是**按包、分进程**跑的，没有常驻进程能共享内存，所以 facts 必须落盘。

再看 fact 文件本身长什么样：

```bash
ls -l $WORK/b002/vet.out $WORK/b001/vet.out
xxd -l 16 $WORK/b002/vet.out
```

```text
b002/vet.out  158 B   head=0dff81020102ff820001ff800000347f
  可见字符串: gobFact / PkgPath / Object / example.com/facts/a / *printf.isWrapper / isWrapper
b001/vet.out    0 B
```

逐字段：`0d ff 81 02 …` 是 gob 编码；`*printf.isWrapper` 是 printf 分析器导出的 fact 类型名，绑定的对象是 `example.com/facts/a.Logf`——即「`Logf` 是个 printf 包装」这条结论。`158 B` 就是它序列化后的全部内容。而 `b001/vet.out` 是 `0 B`：main 包只是**消费**上游 fact，自己没导出任何 fact。两条对照给出可证伪的结论：`go vet ./...` 能报出 `./main.go:5:15: … a.Logf format %d has arg "x" of wrong type string`，前提正是 `a` 包先把 `isWrapper` 写进了 `b002/vet.out`；把 `Logf` 的实现改成不再转发格式串，这条 fact 消失，跨包告警也随之消失。`VetxOnly=true` 的包被分析却不报告，因此「上游包里的可疑写法没被本次 vet 报出来」不是漏报，是协议设计——它有自己的目标包那一次。

facts 的数量关系可以直接代入：一个包要读入的 fact 文件数 = `len(PackageVetx)` = 它**直接 import 且启用了 fact 分析器**的包数（实测 main 包是 2：`a` 和 `runtime`），不是整棵传递依赖树——间接依赖的 fact 已经在上游各自的分析里被消化并重新导出。这把「跨包分析」的代价从 O(依赖树) 压到 O(直接依赖)，也解释了为什么 `go vet` 能塞进 `go test` 的默认流程。

`-vettool` 是把自定义分析器接进这条链的口子。`go vet -vettool=自定义工具路径` 让 `cmd/go` 不再调用内置的 `vet`，改调你给的工具；前提是它实现了 unitchecker 的协议：能用一份配置文件路径作为唯一参数启动，并响应 `-flags`（以 JSON 报告它支持哪些开关，供 `go vet` 探测）。用 `x/tools` 的 `singlechecker` 可以把任意 `Analyzer` 包成这样一个二进制：

```go
package main

import (
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(printf.Analyzer) }
```

```bash
go build -o myvet ./cmd/myvet
go vet -vettool=$PWD/myvet ./app
myvet -flags          # 协议探测：返回 JSON 开关列表
```

```text
app/app.go:5:20: fmt.Printf format %d has arg s of wrong type string
[{"Name":"V","Bool":true,"Usage":"print version and exit"}, {"Name":"all", …}]
```

逐字段：告警与内置 vet 同源（都是 printf 分析器），但这次工具是你自己的二进制；`-flags` 的输出是 JSON 数组，`Name` 是开关名、`Bool` 表示是否为布尔开关——`go vet` 正是靠它把 `-printf.funcs` 之类参数透传给自定义工具。由此推出一条可证伪的接入结论：把组织内部的检查规则做成 `Analyzer` 并用 `singlechecker` / `multichecker` 打包，就能以 `go vet -vettool` 复现 `go test` 里同一套按包调度与 fact 传递，而不必另起一套 CI 扫描；反过来，一个不吃 `vet.cfg`、不响应 `-flags` 的普通二进制传给 `-vettool`，`go vet` 会在协议探测阶段直接失败，而不是「跑起来了但不报」。

最后把 SSA 型分析器放回这张图：`go/analysis/passes/buildssa` 的 `Analyzer` 名字就叫 `buildssa`，它把包构造成 SSA 形式供别的分析器通过 `Requires` 依赖，`pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)` 取结果。它属于**包内 horizontal 依赖**——SSA 是按包构造的，不产生跨包 fact；需要流敏感或跨函数数据流（例如 nilness、逃逸雏形）的分析器走这条路，而 printf、copylocks 这类只需类型信息的走 AST / `types.Info`。前两版说「vet 只读不改、看静态形状」，补上边界：那是 AST 型分析器的输入；SSA 型分析器能看到控制流，但仍不执行程序，也仍不跨运行时边界。判断一个自定义检查器该用哪种，依据就是「结论是否需要跨包」——需要就声明 `FactTypes` 走 vertical，不需要就 `Requires` 走 horizontal。

**边界与失效条件**：facts 只沿 import 边向下游单向传递，兄弟包、被依赖包的依赖不会自动可见；`VetxOnly` 包不出诊断，所以「某个依赖包有可疑代码」要显式把它列进 `go vet` 的目标才报；`-vettool` 替换的是整套分析器，不是给内置 vet 追加一条，接错版本会同时丢掉所有内置检查；gob 编码的 fact 与导出它的分析器版本绑定，工具链与分析器版本不一致时 facts 可能被判为不兼容而失效。

**版本前提**：本机 Go 1.23.8（darwin/arm64）实测；`go/analysis` 接口与 `unitchecker` 协议以 `golang.org/x/tools@v0.24.0` 为准（本机模块缓存内版本，与 Go 1.23 的 vet 套件配套），`FactTypes` / `Requires` 的 horizontal / vertical 命名、`PackageVetx` / `VetxOutput` / `VetxOnly` 字段自该框架稳定以来未变；`go vet -vettool` 自 Go 1.12 起提供，`-flags` 协议同期。第 051 讲前两版分别讲了 vet 只看静态形状、`-printf.funcs` 注册，这里补的是「静态形状之外的跨包结论从哪来、以及怎么换成自己的分析器」；第 050 讲的 import 依赖图正是 `PackageVetx` 这张映射的来源。

### 三条最常见的误解

**误解一：gofmt 与 lint 全绿，就代表代码没有隐藏 bug。** 反例：仓库早期没开 errcheck，一段 `f, _ := os.Open(path)` 能同时通过编译、gofmt 与人工评审；当配置文件缺失时 `f` 为 nil、错误被下划线吞掉，服务拿着零值配置照常启动，把流量打到默认地址，日志里连一条 error 都没有。静态工具只覆盖「可机械判定」的维度：格式、未检查的 error、可疑写法；边界条件、并发时序、错误语义要靠第 052 讲的表驱动测试、第 049 讲的 `go test -race` 和设计评审来兜。把「门禁绿」等同于「无 bug」，等于把工具的能力边界当成了系统的正确性边界。

**误解二：`go vet` 能查出并发问题与逻辑死锁。** 反例：两个 goroutine 累加同一个全局计数器且不加锁，`go vet ./...` 一声不吭，只有 `go test -race ./...` 才会报数据竞争；vet 的 `copylocks` 只检查「把含锁的值按值拷贝」这类局部模式，查不出加锁顺序不对导致的死锁。原因是 vet 做的是不执行程序的静态启发式分析，它刻意保守、只报高置信度问题以控制误报；动态行为必须由运行时检测与测试覆盖。正确做法是把 vet 当成最低成本的兜底，而不是并发正确性的依据。

**误解三：`gofmt` 会改动程序语义，所以不敢在 CI 里强制。** 反例：团队担心自动格式化改坏逻辑，于是只把 gofmt 写进 Wiki，结果各人编辑器配置不同，同一个文件每次提交都带着大片空白 diff，真正的改动被淹没，评审效率反而更低。gofmt 在语法树层重排空白、对齐与换行，不触碰标识符与运算符，且幂等；前面实验里第二次 `gofmt -l` 为空就是证据。替代做法（靠人自觉、靠文档）省下了一次配置，却把成本转嫁到每一次评审上。

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

<Quiz q="给存量老仓库首次接入 lint，哪条命令能只检查新增代码？" :options='["run --fast","run --new-from-rev=HEAD~1","run --disable-all","run --fix"]' answer="1" explain="--new-from-rev 只报告相对基准提交新增行的问题，让老代码先不阻塞合入；--fast 只减耗时、--fix 自动修、--disable-all 关掉全部规则，都不解决存量噪声。" />

### 一次真实故障：错误被吞掉，告警为什么没响

现象：某次发布后，服务读不到新下发的路由配置，却没有任何错误日志，监控也一切正常，请求被静默打到了旧地址。排查路径：

1. 先查日志与指标，确认不是下游超时，而是「配置读取」这一步压根没报错；
2. 用 `grep -R '_, :=\|_, =\|_ = ' ./internal` 找出所有被下划线接走的返回值，定位到 `b, _ := os.ReadFile(path)`；
3. 补一行临时日志复现：文件不存在时 `b` 为 nil，函数继续往下走，把零值配置当成有效配置使用；
4. 根因不是这次写错，而是仓库从未开启 errcheck——Go 把 error 当普通值返回，语言不强制调用方处理，于是「忘记检查」是编译期合法、运行期静默的失败，靠人眼在几百个调用点里评审必然漏；
5. 修复分两层：改代码为 `b, err := os.ReadFile(path)` 并用 `fmt.Errorf("读配置: %w", err)` 包装，让上层能用 `errors.Is` 判定；同时在 `.golangci.yml` 启用 errcheck 并接进 CI，把这一类写法永久挡在门外。

这条规则为什么存在、替代做法与代价也就清楚了：靠评审（随调用点数量线性失效）、靠把函数签名改成 panic（把可恢复错误升级成进程崩溃，代价过大）、靠运行时兜底（发现时已经用错配置了）都不如静态检查来得早。errcheck 的价值正是把「语言允许的疏忽」变成「工具不允许的写法」。

<Callout type="warn" title="静默失败比报错更贵" text="报错会触发告警、有人处理；错误被吞掉时系统带着错误状态继续跑，等到数据不对才被发现，排查起点往往已经离现场很远。这也是 errcheck 被列为最优先开启规则的原因。" />

### 一次真实故障：周一早上所有 PR 集体红灯

现象：没有人改 lint 相关文件，某个周一早上所有 PR 的格式检查同时失败。排查路径：① 看 diff，只有业务文件改动，排除代码回归；② 翻 CI 日志，发现 golangci-lint 从 v1.59.1 变成了新版本——workflow 里写的是浮动版本或 action 自动升级；③ 新版本新增或收紧了默认规则，同一份代码被判出更多问题；④ 根因是规则集合没有被锁定，门禁的结论取决于上游哪天发版；⑤ 修复是把 `version: v1.59.1` 与 `go-version-file: go.mod` 钉死，升级时先在本地跑 `golangci-lint config verify` 与 `golangci-lint run`，确认新规则影响后再改配置。

这与前面「工具链版本要锁定」是同一个道理的两种表现：lint 是规则的集合，集合本身会随版本变，Go 版本又会改变 vet 与 staticcheck 的判断；不锁定，等于把「今天能不能合入」交给外部发版节奏。第 060 讲讲制品与回滚时会再遇到它——凡是影响构建结论的输入，都必须可复现。

### 与 L10 实战工程结构的呼应

L10 的实战工程从第一讲就把 `.golangci.yml` 与 workflow 一起提交进仓库：`/projects/kv-1-memory` 用最小规则集守住接口与错误，后面 `/projects/kv-2-network`、`/projects/kv-3-async` 逐步加并发与持久化代码时，靠的是同一套门禁防止新代码把旧约定冲掉；`/projects/cli-codegen` 生成的代码要靠 `issues.exclude-rules` 按目录精确排除，正是本讲误报分流用得上的地方。再往后，第 052 讲的表驱动测试、第 054 讲 fuzz 找到的边界、第 059 讲 pprof 定位到的热点，结论都需要落回测试与 CI 才能防止回归——工具链不是某一讲的知识，而是贯穿所有实战的地基。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l5-engineering/lesson-051-code-style`](https://github.com/lfire/go/tree/main/examples/l5-engineering/lesson-051-code-style)。clone 仓库后进入 `examples/l5-engineering`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="补齐被丢弃的错误" task="下面这段读文件代码丢掉了错误。请改成检查错误并返回，使 errcheck 通过。\nfunc read(path string) []byte {\n    b, _ := os.ReadFile(path)\n    return b\n}" :hints='["把第二个返回值接住，别用下划线丢弃","函数签名要能返回 error","若还要加上下文，可用 fmt.Errorf 加 %w 包装"]' solution="func read(path string) ([]byte, error) {\n    b, err := os.ReadFile(path)\n    if err != nil {\n        return nil, err\n    }\n    return b, nil\n}" explain="接住 error 并原样返回，errcheck 不再报警；需要上下文时再用 %w 包装，调用方即可用 errors.Is 追到原始错误。" />

## 随堂检测

<Quiz no="051-1" q="下面哪项最适合交给 gofmt，而不是靠代码评审？" :options='["函数该不该拆分","缩进、对齐与大括号位置","命名是否达意","错误该不该包装"]' answer="1" explain="gofmt 只做机械排版且幂等；拆分、命名、错误语义属于设计判断，工具不该也不越界。" />

<Predict no="051-2" q="某仓库所有文件都已格式化，执行 gofmt -l . 会输出什么？" :options='["列出全部 .go 文件路径","什么都不输出","输出 gofmt 版本号","报编译错误"]' answer="1" output="（无输出，退出码为 0）" explain="gofmt -l 只列出需要重新格式化的文件；全部合规时输出为空，因此常被写成 test -z 形式的 CI 断言。" />

## 一页纸速记

<KnowledgeCard :points='["导出标识符紧邻一行以其名字开头的注释，接收者用短名","error 必须检查，包装用 %w，判定用 errors.Is","gofmt / goimports 会改文件，vet 与 golangci-lint 只报告","先开 errcheck、govet、staticcheck、goimports 少数几条规则"]' :pitfalls='["规则开太多再用 nolint 逐行压制","规范只写在 Wiki，没有接进 CI 强制","把设计讨论也交给工具，或反过来靠评审争论缩进"]' summary="代码规范的目标是把可机械判定的一致性交给工具，让人的注意力留给设计与取舍。" />

## 小结与下一讲

- Effective Go 的命名与注释追求短、一致、可生成文档。
- 四道关卡职责不同：排版、import、可疑写法、汇总规则，逐层递进。
- 规则少而准，写进配置并接入 CI，才算真正落地。

[下一讲：单元测试与表驱动测试](/engineering/unit-test)
