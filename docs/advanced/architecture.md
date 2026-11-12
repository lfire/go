---
title: 第 101 讲 · 架构设计：模块拆分、演进与重构
description: 按变化方向识别边界，用依赖倒置与绞杀者重构让系统在变化中保持可演进
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="55 分钟" goal="在变化中保持系统可演进" prereq="第 50 讲 · 项目结构与分层设计 / 第 58 讲 · 设计模式" />

# 第 101 讲 · 架构设计：模块拆分、演进与重构

> 第 50 讲给过一套项目骨架，第 58 讲讲过解耦手法，但真正难的是「业务变了之后，改一处还是要改十处」。架构设计的核心不是画漂亮的分层图，而是识别变化方向、把易变的部分隔离在边界后面，并让替换可以小步进行。本讲讲模块拆分依据、依赖倒置，以及生产里真正可用的绞杀者重构，并用可运行代码演示三个关键动作。

## 这一讲你会得到什么
<CheckList id="L9-101" title="本讲目标" :items='["按变化方向而不是按技术层划分模块边界", "用接口把领域层与基础设施层的依赖倒过来", "用绞杀者门面与双写校验做低风险重构"]' />

## 1. 先建立直觉
把系统想成不断加房间的楼。每加一间都要动承重墙，说明边界划错了；好的边界让新房间只接一根水管、一根电线。「识别变化方向」就是先问：哪些东西最可能一起变？把一起变的放同一模块，把稳定与易变的隔开。依赖倒置则像预埋标准插座：领域层只认接口，数据库、缓存、MQ 都是可插拔的插头。
<ConceptDiagram type="architecture-layer" title="分层与依赖倒置" caption="业务规则不依赖具体技术，替换实现不动核心" />

## 2. 语法与规则
三段程序分别演示领域层定义接口、绞杀者门面按灰度切流、以及双写校验确认等价。

```go
package main

import "fmt"

// 领域层只定义接口，不 import 任何具体存储实现。
type OrderRepo interface {
	Save(id string, amount int) error
}

// 基础设施层实现接口。
type MemRepo struct{ data map[string]int }

func (m *MemRepo) Save(id string, amount int) error {
	m.data[id] = amount
	return nil
}

// 应用层依赖抽象，而非具体实现。
type Service struct{ repo OrderRepo }

func (s *Service) Create(id string, amount int) error {
	return s.repo.Save(id, amount)
}

func main() {
	svc := Service{repo: &MemRepo{data: map[string]int{}}}
	if err := svc.Create("o-1", 99); err != nil {
		panic(err)
	}
	fmt.Println("依赖倒置装配完成")
}
```

```go
package main

import "fmt"

type Handler interface{ Handle(string) string }

type Legacy struct{}

func (Legacy) Handle(s string) string { return "legacy:" + s }

type Modern struct{}

func (Modern) Handle(s string) string { return "modern:" + s }

// 绞杀者门面：按 key 的哈希灰度，把一部分流量导向新实现。
type Facade struct {
	legacy    Handler
	modern    Handler
	modernPct int
}

func (f Facade) Handle(key string) string {
	if hash(key)%100 < f.modernPct {
		return f.modern.Handle(key)
	}
	return f.legacy.Handle(key)
}

func hash(s string) int {
	h := 0
	for i := range s {
		h = h*31 + int(s[i])
	}
	return h
}

func main() {
	f := Facade{legacy: Legacy{}, modern: Modern{}, modernPct: 25}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		fmt.Println(k, f.Handle(k))
	}
}
```

```go
package main

import "fmt"

type Price interface{ Get(sku string) int }

type OldPrice struct{}

func (OldPrice) Get(sku string) int { return len(sku) * 10 }

type NewPrice struct{}

func (NewPrice) Get(sku string) int { return len(sku) * 10 }

// 双写校验：重构中期同时调用新旧实现并逐条比对，确认等价后再放量。
func main() {
	old, next := OldPrice{}, NewPrice{}
	mismatch := 0
	for _, sku := range []string{"a", "bb", "ccc", "dddd"} {
		o, n := old.Get(sku), next.Get(sku)
		if o != n {
			mismatch++
		}
		fmt.Println(sku, o, n)
	}
	fmt.Println("不一致数 =", mismatch)
}
```

<OutputBox title="第三段程序的输出" output="a 10 10\nbb 20 20\nccc 30 30\ndddd 40 40\n不一致数 = 0" note="不一致数为 0 才具备继续放量的条件；一旦出现差异，先停下来定位，而不是提高灰度比例。" />

<LineByLine title="绞杀者重构的四个阶段" code="门面路由新旧实现\n双写并比对结果\n逐步提高灰度比例\n下线旧实现" :notes='["在调用方与实现之间加一层门面，先把流量拦下来", "同一请求同时走新旧两条路径，逐条比对差异", "确认等价后按比例放量，随时可一键回切", "新实现稳定后再删除旧代码与兼容分支"]' />

## 3. 深入一步
拆分依据决定改动的传播范围。按技术层拆上手快，但一个业务变更要横跨三层；按领域拆把同一概念的数据与规则放一起，变化被局部化，代价是必须先识别限界上下文。依赖倒置让边界成立：领域层声明接口，基础设施层去实现，编译期依赖统一指向抽象。

<Term word="绞杀者模式" text="在旧系统外围逐步长出新实现，用路由一点点接管功能，旧系统逐渐失去职责直到可以安全下线。" />

| 拆分依据 | 优点 | 代价 | 适用场景 |
| --- | --- | --- | --- |
| 按技术层 | 上手快、目录直观 | 一个需求跨多层改 | 早期原型、小工具 |
| 按领域上下文 | 变化被局部化 | 需要先识别边界 | 长期演进的核心系统 |
| 按变更频率 | 稳定与易变隔离 | 需要持续度量 | 高频迭代模块 |
| 按团队归属 | 职责清晰 | 易形成信息孤岛 | 多团队协作 |

<Callout type="warn" title="别搞大爆炸重写" text="一次性重写常在「新系统没对齐旧行为」与「旧系统还在加需求」之间被拖垮。绞杀者每一步都可回切、可验证；边界要按变化方向划，别按数据库表划。" />

### 源码精读与可复现验证（以 Go 1.21~1.23 为准，版本可能变化）

「依赖方向指向抽象」在 Go 里能被静态验证，靠的是工具链与类型检查两处源码。`src/cmd/go/internal/load/pkg.go` 的 `Package` 结构记录 `ImportPath`（本包路径）、`Imports`（直接导入列表）与 `Deps`（传递依赖闭包），`go list` 与 `go list -deps` 就是把这三个字段打印出来；`src/cmd/go/internal/list/` 负责按模板渲染这些字段。接口为什么不需要显式声明实现，答案在 `src/cmd/compile/internal/types2`：类型检查器在 `Info` 里记录每个标识符的 `Defs` 与 `Uses`，只要某类型的方法集覆盖了接口方法集就判定实现；`src/go/types` 的 `Implements` 与 `AssignableTo` 把这套判断暴露给静态分析工具。正因为实现是隐式的，领域层声明 `OrderRepo` 接口时完全不必 import 基础设施包，依赖倒置在 Go 里几乎没有额外语法成本。

一个可复现的观察实验：对第 1 段程序所在的包跑 `go list -f` 打印它的 `Imports`，确认里面只有标准库、没有任何 infra 包；然后故意在领域层加一行 `import "github.com/me/svc/internal/storage"`，`go list` 的输出里立刻出现 infra 路径，若形成环 `go build` 会直接报 import cycle，这就把「边界是否被破坏」变成了编译期可查的事实。把 `go list -deps` 的结果接进 CI，用一段脚本断言领域层的输出不含 infra 前缀，就是最低成本的架构守卫；第 2 段门面的 `modernPct` 灰度与第 3 段双写的不一致数，则要靠指标与 ADR 人工约定阈值，工具链管不到。

适用边界要写清：以上路径与字段以 Go 1.21~1.23 为准，`cmd/go/internal` 属于内部包，结构与函数每个版本都可能重构，别在自己的工具里直接 import 它，用 `go list` 命令输出即可；`go list` 只看包级静态 import，跨服务调用、插件注册与反射装配这类运行时依赖它看不见；`internal/` 的可见性是编译期规则，不等于业务边界；包依赖无环只说明能编译，不代表限界上下文划得合理。绞杀者重构里「不一致数为 0」「p99 稳定」这些放量条件都必须由团队针对业务定阈值，源码与工具不会替你决定。

一条命令就能把「领域层有没有偷偷依赖实现」查清楚：

```bash
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/order
go list -deps ./internal/order | tail -5
```

```text
github.com/me/svc/internal/order -> context errors
github.com/me/svc/internal/order
context
errors
fmt
sync
```

`-f` 参数里的模板变量 `.ImportPath` 展开为当前包的导入路径，`.Imports` 是它的直接导入列表，`join` 用空格把切片拼成一行（真实写法见上面的 bash 命令，用双花括号包裹这两个字段）；`->` 只是我加的分隔符，方便肉眼切分「谁依赖谁」。第一行输出 `github.com/me/svc/internal/order -> context errors` 说明这个领域包只直接依赖标准库的 `context` 与 `errors`，完全没有 import `internal/storage`、`internal/cache` 之类基础设施包——这正是第 1 段代码里 `OrderRepo` 接口由使用方定义、编译期依赖统一指向抽象的可验证证据。第二条 `go list -deps` 打印的是传递依赖闭包，`tail -5` 取末尾五行看闭包里有没有混入 infra；输出里 `github.com/me/svc/internal/order` 与 `context`、`errors`、`fmt`、`sync` 都是标准库或自身，说明闭包干净。反证也很直接：一旦领域层 import 了具体实现包，第一行右侧立刻多出 infra 路径，形成环时 `go build` 还会报 import cycle。`go list` 只读、不编译，适合接进 CI 当架构门禁；但它只能证明「包级静态依赖方向」，不能证明边界本身划得对。

<SequenceDiagram title="绞杀者门面的双写校验链路" caption="校验阶段对外仍返回旧结果，新实现只做影子调用并逐条比对" :actors='["Client","Facade","Legacy","Modern","校验器","指标/ADR"]' :steps='[{"from":"Client","to":"Facade","label":"带 key 请求","note":"统一走门面","kind":"call"},{"from":"Facade","to":"Facade","label":"哈希定路由","note":"读 modernPct","kind":"self"},{"from":"Facade","to":"Legacy","label":"调旧实现","note":"始终执行","kind":"call"},{"from":"Legacy","to":"Facade","label":"返回旧结果","note":"对外基准值","kind":"return"},{"from":"Facade","to":"Modern","label":"影子调新实现","note":"不影响返回","kind":"call"},{"from":"Modern","to":"Facade","label":"返回新结果","note":"仅用于比对","kind":"return"},{"from":"Facade","to":"校验器","label":"比对两份结果","note":"逐字段比对","kind":"call"},{"from":"校验器","to":"指标/ADR","label":"记不一致数","note":"同时记 p99","kind":"call"},{"from":"指标/ADR","to":"Facade","label":"差异为零准放量","note":"否则暂停灰度","kind":"return"},{"from":"Facade","to":"Client","label":"返回旧结果","note":"行为保持不变","kind":"return"}]' />

关键在第 10 步：校验阶段对 Client 返回的始终是 Legacy 的结果，Modern 只是影子调用。这样即便新实现有 bug，也不会影响线上行为，放量才真正可回切。

| 阶段 | 门面灰度 modernPct | 入口行为 | 校验动作 | 回切条件 | 进入下一阶段的条件 |
| --- | --- | --- | --- | --- | --- |
| 建门面 | 0 | 全部走 Legacy | 无 | 无 | 门面覆盖全部调用点 |
| 双写校验 | 0 | 仍走 Legacy | 影子调 Modern 并比对 | 不一致数大于 0 | 不一致数持续为 0 |
| 小步灰度 | 5 到 25 | 按 key 哈希切流 | 监控 p99 与错误率 | p99 陡升或出错 | 指标稳定一个观察窗 |
| 扩大灰度 | 50 | 半量走 Modern | 双写继续抽检 | 出现行为差异 | 差异为零且容量充足 |
| 全量 | 100 | 全部走 Modern | 只读回归测试 | 无 | 旧实现连续无流量 |
| 下线旧码 | 不适用 | 删除 Legacy 与兼容分支 | 编译与全量测试 | 编译失败即回退 | 同步归档 ADR |

六个阶段其实是一条只能前进、随时能回退的状态机：每一行都写清「什么情况下停、什么情况下走」，兼容分支才不会无限期留在代码里。

<Callout type="tip" title="源码阅读建议" text="想搞懂依赖方向为什么能被工具验证，先读 cmd/go/internal/load/pkg.go 里 Package 的 ImportPath、Imports、Deps 三个字段，再看 types2 的 Info.Defs 与 Uses 怎么判定隐式实现；这两处读完，go list 的输出和 Go 不需要 implements 声明这两件事就连成一条线了。" />

### 常见误读与反例

架构误读的特点是「静态检查过了就以为设计对了」。下面四条用 `go list` 输出、类型检查规则与第 3 节六阶段表就能推翻。

| 误读 | 为什么站不住（源码推导） | 正确写法或反例 |
| --- | --- | --- |
| 「依赖倒置就是下层包 import 上层包；Go 里要显式写 implements 才算实现接口」 | `cmd/compile/internal/types2` 在 `Info.Defs` 与 `Uses` 里按方法集判定**隐式实现**，只要方法集覆盖接口即成立；领域层声明 `OrderRepo` 时完全不必 import 基础设施包，编译期依赖统一指向抽象 | 第 2 段第一段代码：`Service` 依赖 `OrderRepo` 接口，`MemRepo` 实现它，两边方向都指向抽象；用 `go list -f` 打印领域包 `.Imports` 验证里面没有 infra 路径 |
| 「`go list -deps` 输出干净就证明限界上下文划对了，包依赖无环就等于架构好」 | `go list` 只看**包级静态 import**，跨服务调用、插件注册、反射装配这类运行时依赖它看不见；`internal/` 的可见性是编译期规则，不等于业务边界；无环只说明能编译 | 把 `go list -deps` 接进 CI 当最低成本的架构守卫，但边界是否合理仍要靠 ADR 与「改一处还是改十处」的实际变更来检验；第 3 节适用边界那段已写明这一层 |
| 「绞杀者校验阶段对调用方返回新实现结果，方便尽早验证；双写不一致就先提高灰度看看」 | 第 3 节时序图第 10 步：校验阶段对 Client 返回的**始终是 Legacy 的结果**，Modern 只做影子调用；六阶段表里「不一致数大于 0」的回切条件是暂停放量并定位差异 | 按第 3 节表逐阶段推进，只有「不一致数持续为 0」才允许提高 `modernPct`；带病放量会把行为差异放大成线上故障 |
| 「按数据库表划模块最直观，一张表一个包」 | 边界要按**变化方向**划，按表划会让一个业务变更横跨多个表与多个包，正好违背第 1 节「加房间不动承重墙」的判据 | 按限界上下文或变更频率划分，稳定与易变隔离；第 3 节拆分依据表里「按技术层」那行的代价就是这种划法的后果 |

<Quiz q="go list -deps 显示全部依赖里没有任何 infra 前缀，能证明什么？" :options='["限界上下文划分合理","领域层没有跨服务的运行时耦合","领域层在包级静态 import 上没有反向依赖基础设施","双写校验的不一致数为零"]' answer="2" explain="go list 只反映包级静态 import 方向，能证明编译期依赖没倒；跨服务调用、插件注册、反射装配与边界是否合理都管不到。" />

一句话记边界：`go list` 证明「包级静态依赖方向」，双写不一致数证明「新旧实现在样本上等价」，`modernPct` 证明「流量切了多少」；三者都不证明「边界划得对」，后者只能靠后续变更的实际传播范围与 ADR 复盘来检验。工具管得到的画进 CI，管不到的（放量阈值、p99 稳定）写进 ADR 由团队约定，这条分工正是第 3 节适用边界那段的核心。

### 与 L10 源码级实战的呼应

- KV 贯穿线 ① [内存版与接口抽象](/projects/kv-1-memory) 到 ④ [分片、复制与 Raft](/projects/kv-4-distributed)：`Storage` 接口在 ① 定义，② 加网络层、③ 加 WAL 与锁分片、④ 换成分片复制与 Raft，四次演进都不动接口调用方——这是「领域层定义接口、基础设施层实现」最完整的一条贯穿线，也是依赖倒置收益的实证。
- [实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler)：单线程版到 worker pool 并发版再到队列化分布式版的三次演进，正是绞杀者「门面、双写、灰度、下线」的缩小版；演进对比表里每一步都可回切，对应本讲六阶段状态机。
- [实战 09 · 电商微服务系统](/projects/micro-ecommerce)：服务拆分与契约先定边界再写实现，用户、商品、订单、库存各自按变化方向划分；本讲 ADR 正好用来记录「为什么订单不直接读库存表」这类跨服务依赖决策，避免边界在联调期被悄悄破坏。

## 4. 工程实践
取舍要落进文档：每个重要边界写一条 ADR，记下背景、备选、结论与放弃理由。重构期间把灰度比例、双写不一致数、回切开关做成指标，并约定下线旧代码的条件，避免兼容分支无限期留存。

```bash
go list -deps ./internal/order
```

<Compare title="重构方式对照" :panels='[{"label":"写法 A：整体重写","code":"stopOld()\nbuildNew()\nswitchAll()","note":"一次性切换，行为对齐难、无法回切"},{"label":"写法 B：绞杀者","code":"f := Facade{modernPct: 5}\nf = Facade{modernPct: 50}\nf = Facade{modernPct: 100}","note":"按灰度小步接管，配合双写校验，随时可回切到旧实现"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l9-advanced/lesson-101-architecture`](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-101-architecture)。clone 仓库后进入 `examples/l9-advanced`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="写一条架构决策记录" task="为「订单存储换成持久化」写一条 ADR，列出两个备选、结论与放弃理由。" :hints='["背景先写清约束，如数据量与恢复要求", "放弃的理由比结论更重要"]' solution="背景 := 单实例内存存储无法容灾\n备选 := []string{内存分片, 持久化 KV}\n结论 := 持久化 KV\n理由 := 需要故障恢复与多副本" explain="ADR 把取舍固化下来，日后回看能知道当时的约束。完整结构可用同样的字段列表表达。" />

## 随堂检测
<Quiz no="101-1" q="依赖倒置在 Go 里最直接的体现是？" :options='["下层包 import 上层包", "领域层定义接口，基础设施层实现", "所有类型放进一个包", "用全局变量传依赖"]' answer="1" explain="接口由使用方定义，实现方依赖它，编译期依赖方向统一指向抽象。" />
<Predict no="101-2" q="双写校验出现「不一致数大于 0」，正确做法是？" :options='["提高灰度到 100%", "暂停放量并定位差异", "跳过校验，新实现更快"]' answer="1" output="暂停放量并定位差异" explain="双写就是在放量前发现行为不等价；带病放量会把差异放大成线上故障。" />

## 一页纸速记
<KnowledgeCard :points='["边界按变化方向划", "领域层定义接口实现依赖倒置", "绞杀者：门面、双写、灰度、下线"]' :pitfalls='["一次性大爆炸重写", "按数据库表划模块", "兼容分支不下线"]' summary="好架构让每次变化都只落在最小范围里。" />

## 小结与下一讲
1. 先识别哪些东西会一起变，再据此划模块边界，稳定与易变要隔离。
2. 领域层声明接口、基础设施层实现，依赖方向统一指向抽象。
3. 重构用绞杀者门面加双写校验小步接管，配合 ADR 与可回切开关控制风险。

[下一讲：资深能力图谱、技术选型与持续精进](/advanced/senior-path)
