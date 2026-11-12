---
title: 第 108 讲 · 实战 06 · CSV / SQL 查询工具
description: 用一套表达式 AST 与执行计划适配多种数据源，以流式结果和统一测试策略支撑 CSV 与 SQL 查询
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="一套 AST 适配多种数据源" prereq="第 107 讲 · 实战 05 · 日志分析 ETL 管道" />

# 第 108 讲 · 实战 06 · CSV / SQL 查询工具

> 窗口统计的结果总要被人查，而数据不会乖乖待在一个地方：这一份在 CSV，那一份在内存临时表，还有一份躺在 PostgreSQL 里。若给每种来源都写一套 WHERE 判断，同一条过滤逻辑会散成三份，改一处漏两处，类型与 NULL 的处理更是各写各的，错误结果静默出现却没人说得清是哪一层错了。本讲交付一个 CSV / SQL 查询工具的内核：用表达式 AST 描述查什么，用逻辑计划与物理计划分层描述怎么查，用 DataSource 小接口屏蔽从哪查，再以 emit 回调把结果流式吐出。重点不在实现一个完整的 SQL 数据库，而在讲清两个最容易被低估的边界——类型系统里的 NULL 三值逻辑，以及流式执行的内存边界。看完你应该能拿这套内核接任意数据源，并用 EXPLAIN 把一次慢查询归因到具体的计划节点。

## 0. 项目定位与真实场景

这个工具的直接用户是第 107 讲那条 ETL 管道的下游：数据团队把每分钟窗口结果落成 CSV，运维要按状态码、级别和时间范围做即席查询，研发要在本地复现线上一条可疑日志，分析师又希望能对同一批数据用 SQL 语义做过滤。三种诉求指向同一个内核，但数据来源完全不同——落地文件是 CSV，本地调试是内存切片，生产明细在关系库里。早期做法是每个脚本自己读文件、自己写 if 判断，结果同一条「状态码等于 200 且字节数不小于 1000」的条件被复制了三遍，其中一份把空串当成 0 参与比较，报表数字对不上却查了两天才发现。

量级上，单个结果文件最大两 GB、约两千万行，本地 REPL 并发会话上限三十二，全表扫描要能在秒级给出首行、分钟级扫完；内存是被硬性约束的——工具常驻堆必须稳定在几十 MB，绝不允许因为一个没有 LIMIT 的查询把两千万行结果攒进内存。核心矛盾因此不是「怎么解析 SELECT」，而是：如何在不牺牲类型正确性、且内存与结果集大小无关的前提下，用同一套查询语义适配异构数据源。所有架构决策都从这两条约束推导，尤其是把逻辑计划与物理计划分开、把结果设计成回调而非切片，都是被这个矛盾逼出来的，而不是为了分层而分层。

<CheckList id="l108-goals" title="本讲交付目标" :items='["先给出可度量的数据规模、内存与延迟约束，再由约束反推分层","用表达式 AST 与三值逻辑把类型系统和 NULL 语义收敛到一处","把查询拆成逻辑计划与物理计划，并讲清谓词下推的触发条件","用 DataSource 小接口让 CSV、内存与 SQL 源复用同一执行器","以 emit 回调保证内存边界，用 EXPLAIN 与契约测试证明可上线"]' />

## 1. 需求与约束

约束先写成可验收的数字。功能需求里最容易被低估的是 F3 与 F6：F3 要求 NULL 有独立表示，F6 要求内存与行数无关，两者都必须在测试里有断言，否则上线后会以「结果偶尔不对」和「偶尔 OOM」这种最难查的方式暴露。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| F1 | 表达式 AST | 支持等值、不等、大于、大于等于及 AND、OR、NOT；新增算子不改已有节点 |
| F2 | 逻辑与物理计划分层 | 逻辑计划不含数据源信息，物理计划含下推开关与投影列 |
| F3 | 类型系统与 NULL 语义 | 支持整数、浮点、字符串、布尔、NULL；比较遵循三值逻辑，NULL 不被当零值 |
| F4 | 多数据源适配 | CSV、内存、SQL 三种源实现同一 DataSource 接口，跑同一组契约测试 |
| F5 | 谓词下推 | 支持下推的源在扫描层过滤，上行行数比全量扫描减少九成以上 |
| F6 | 流式结果 | emit 回调逐行输出，一百万行结果执行后常驻堆增量接近零 |
| F7 | REPL 与 EXPLAIN | 逐行读命令，EXPLAIN 输出可读物理计划，SELECT 流式输出结果 |
| F8 | 精确失败 | 下游写失败立即中断并把行号包进错误 |

| 指标 | 目标值 | 说明 |
| --- | --- | --- |
| 单文件规模 | 2 GB 或 2000 万行 | 超出靠分片，不靠加内存 |
| 全表扫描吞吐 | 不低于 80 万行每秒 | CSV 源、无复杂表达式 |
| 首行延迟 | 不超过 100 毫秒 | 从提交到第一行输出 |
| 常驻堆 | 不超过 32 MB | 与结果集行数无关 |
| 单查询峰值堆 | 不超过 64 MB | 含排序等临时结构的上限 |
| 并发会话 | 32 | 单进程多 REPL |
| 一致性级别 | 只读快照 | 扫描期间源文件不可变 |
| 下推收益 | 上行行数减少 90% 以上 | 选择性高的等值条件 |

<Term word="谓词下推" text="把 WHERE 过滤尽量下沉到离数据最近的扫描层执行，减少需要向上传递的行数，是逻辑计划降级为物理计划时最有效的规则。" />

本讲不做什么要写死：不做完整 SQL 解析器，词法、优先级、子查询与 JOIN 是独立项目，本讲只手写表达式 AST 这个最稳定的内核；不做写操作、事务与索引；不做跨源 JOIN；不做代价优化器，只做规则优化。假设边界是：查询为只读、扫描期间数据源不发生变更、列名在表头中声明且大小写敏感、单个单元格可完整读入内存。任何一条不成立，第 8 节的一致性论证都要重推。注意与第 104 讲的区别：那一讲用 go/ast 读已存在的 Go 源码来生成新代码，这一讲定义自定义查询表达式的树来对数据求值，一个读语法树、一个建语法树。

## 2. 架构蓝图

<ArchBlueprint title="CSV SQL 查询工具分层架构" caption="语法计划层、执行类型层、数据源适配层三层单向依赖，箭头方向即依赖方向" :layers='[{"name":"语法与计划层","note":"把命令变成与数据源无关的计划","protocol":"表达式 AST 加逻辑计划","items":[{"label":"REPL 与解析器","sub":"读命令建 AST","detail":"职责：逐行读命令并把 WHERE 编译成表达式树，支持 EXPLAIN 分派；技术选型：bufio.Scanner 配合放大的单行上限，手写 AST 不引解析器生成器；失败处理：语法错误返回明确位置信息，不吞掉错误继续执行。","tone":"blue"},{"label":"优化器","sub":"逻辑计划降级","detail":"职责：把逻辑计划降级成物理计划，决定谓词是否下推、列裁剪与 LIMIT 位置；技术选型：规则优化而非代价优化，按数据源能力开关下推；失败处理：数据源不支持下推时退回执行器内过滤，结果保持一致。","tone":"green"}]},{"name":"执行与类型层","note":"统一类型语义与流式执行","protocol":"物理计划加 Row","items":[{"label":"类型系统","sub":"Value 与 NULL","detail":"职责：在扫描期把单元格定型为整数、浮点、字符串、布尔或 NULL；技术选型：Value 带 Valid 位区分 NULL 与零值，比较走三值逻辑；失败处理：转型失败判 NULL 而非零值，避免静默错算。","tone":"orange"},{"label":"流式执行器","sub":"逐行过滤投影","detail":"职责：按物理计划串联扫描、过滤、投影，以 emit 回调逐行吐出结果；技术选型：每时刻只持有一行，回调返回错误立即中断并带行号；失败处理：下游写失败精确定位到行，绝不攒全量结果集。","tone":"purple"}]},{"name":"数据源适配层","note":"屏蔽数据从哪来","items":[{"label":"CSV 适配器","sub":"流式读表","detail":"职责：用 encoding/csv 逐记录读取并映射列名，不缓存全表；技术选型：Reader 默认流式且校验字段数，扫描前读表头；失败处理：空文件与字段数不符返回明确错误，脏行按列名缺失处理。","tone":"red"},{"label":"SQL 与内存适配器","sub":"复用同一接口","detail":"职责：把数据库 Rows 或内存切片包装成同一个 Scan 回调；技术选型：SQL 源声明支持下推，把 WHERE 交给数据库减少上行行数；失败处理：连接失败沿 error 上抛，参数化查询杜绝拼接注入。","tone":"gray"}]}]' />

三层是按「变化方向」切的，而不是按技术栈。语法与计划层面对的是查询写法的变化，新增一个算子或一种命令只改这一层；执行与类型层是全系统最稳定的内核，三值逻辑与流式语义一旦定下就不该再动，因此也最值得用契约测试锁死；数据源适配层面对的是数据来源的变化，接一种新的存储只需实现接口。这样切的直接收益是，把 CSV 换成 PostgreSQL 时，AST、计划、执行器一行不改。

层间协议同样是显式的：语法层向执行层交付的是物理计划与表达式树，执行层向数据源层交付的是 Row 与可选的谓词，数据源层向上只回吐逐行数据。依赖方向严格单向——AST 不认识数据源，执行计划不认识 CSV，数据源不认识过滤条件。禁止反向依赖的原因是，一旦适配器反过来认识 WHERE，同一份过滤语义就会复制进每个实现，第 0 节那个「三份判断对不上」的问题立刻复活。

<ConceptDiagram type="architecture-layer" title="查询工具的分层" caption="AST 描述查什么，执行计划描述怎么查，数据源接口屏蔽从哪查，上层只依赖下层接口" />

<FlowTimeline :steps='[{"title":"解析","desc":"把命令编译成表达式 AST","tone":"blue"},{"title":"计划","desc":"逻辑计划降级并决定是否下推","tone":"green"},{"title":"定型","desc":"扫描期把单元格转成带类型 Value","tone":"orange"},{"title":"执行","desc":"流式扫描逐行过滤与投影","tone":"purple"},{"title":"输出","desc":"emit 回调逐行写回 REPL","tone":"red"}]' direction="horizontal" />

## 3. 模块职责与依赖方向

下表把每个模块的输入输出与失败行为钉死。最值得注意的是「状态」一列：只有 REPL 持有会话状态，解析器、优化器、执行器、数据源都必须是无状态的纯逻辑或一次性迭代器，否则并发会话之间会互相污染，契约测试也无法复用同一实例。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| repl | 命令文本行 | 结果文本或 EXPLAIN | parser、executor | 会话状态 | 语法错误回显位置，不中断会话 |
| parser | 命令字符串 | 表达式 AST 与逻辑计划 | ast 包 | 无状态 | 非法语法返回明确错误 |
| optimizer | 逻辑计划、数据源能力 | 物理计划 | plan 包 | 无状态 | 不支持下推则退回执行器过滤 |
| typeval | 原始单元格与声明类型 | Value | strconv | 无状态 | 转型失败判 NULL，不报错中断 |
| executor | 物理计划、数据源 | 逐行 emit | DataSource 接口 | 无状态 | emit 返回 error 立即停止并带行号 |
| datasource | 数据源句柄 | Row 流 | CSV 或数据库驱动 | 迭代游标 | 读错误沿 error 上抛 |

包目录按领域命名，model 只放数据结构与接口，是依赖图的汇聚点；ast、plan、exec、source 之间不出现循环导入。cmd 只负责装配 REPL，不含查询逻辑。

```text
data-sql-tool/
├── cmd/qtool/main.go          # 装配 REPL 与优雅退出
├── internal/
│   ├── model/model.go         # Value / Row / Tri / DataSource 接口
│   ├── repl/repl.go           # 逐行读命令与 EXPLAIN 分派
│   ├── parser/parser.go       # 命令到表达式 AST 与逻辑计划
│   ├── plan/plan.go           # 逻辑计划、物理计划、优化规则、EXPLAIN
│   ├── typeval/value.go       # 类型系统与 NULL 语义
│   ├── exec/executor.go       # 流式扫描、过滤、投影、emit
│   └── source/
│       ├── csv.go             # CSV 适配器
│       ├── memory.go          # 内存适配器
│       └── sql.go             # SQL 适配器与谓词下推
└── go.mod
```

依赖倒置收敛在 model 包的两个小接口上：Expr 只有一个 Eval 方法，DataSource 只有 Columns、Scan、SupportsPushdown 三个方法。执行器只依赖这两个接口，测试时把数据源换成内存 fake 就能完全不碰磁盘与数据库；新增算子只实现 Expr，新增数据源只实现 DataSource。这是小接口原则在查询系统里最实在的收益，也是第 10 节「同一份期望跑多个实现」能成立的前提。

<Callout type="warn" title="边界划分最常见的错误" text="让 CSV 适配器认识 WHERE 条件，或让执行器认识 CSV 格式。前者会把过滤逻辑复制到每个数据源，后者会把 IO 细节渗进 CPU 逻辑；正确做法是数据源只实现 Scan 与可选的下推能力，过滤语义统一由表达式 AST 与执行器承担。" />

## 4. 数据与协议契约

类型系统是全链路最基础的契约。SQL 的 NULL 不等于任何语言的零值：空字符串、转型失败、列缺失三种情况都必须收敛成 NULL，而 NULL 参与任何比较的结果都是 UNKNOWN，WHERE 只放行 TRUE。下表把这套语义钉死，评审时逐行确认就能避免第 0 节那种静默错算。

| 情况 | 内部表示 | 比较结果 | 说明 |
| --- | --- | --- | --- |
| 单元格为空串 | Value 的 Valid 为假 | UNKNOWN | 不转成 0 或空字符串值 |
| 转型失败 | Value 的 Valid 为假 | UNKNOWN | 脏值不参与数值比较 |
| 列缺失 | Row 中无该键 | UNKNOWN | 用逗号 ok 判存在，不当空串 |
| 整数或浮点 | Value 带 Kind 与 Num | 按数值比较 | 扫描期一次定型 |
| 字符串 | Value 带 Kind 与 Str | 按字典序比较 | 大小写敏感 |
| 布尔 | Value 带 Kind 与 Str | 按规范化文本比较 | 只接受 true 与 false |

对外契约是 REPL 的三类命令与结果格式，全部基于文本，方便管道与脚本对接。

| 命令 | 输入 | 输出 | 契约 |
| --- | --- | --- | --- |
| SELECT | 列清单、表名、WHERE、LIMIT | 首行表头加逐行 CSV | 流式，行数不受内存限制 |
| EXPLAIN | 任意 SELECT | 可读物理计划文本 | 与执行结果无关，只解释计划 |
| .exit 或 quit | 无 | 关闭会话 | 优雅退出，释放游标 |

一条物理计划序列化成 JSON 的形态如下，字段名与第 6.3 节的结构一一对应，pushdown 为假时 filter 由执行器承担。

```json
{
  "table": "access",
  "project": ["status", "size"],
  "pushdown": true,
  "filter": "status = 200",
  "limit": 100,
  "streaming": true
}
```

内部接口签名刻意保持极小。下面这段是可独立编译的契约骨架，语法层、执行层、数据源层都只认这几个类型。

```go
package main

import "fmt"

// Value 是带类型标签的单元格；Valid 为假表示 NULL，绝不退化成零值。
type Value struct {
	Kind  string
	Num   float64
	Str   string
	Valid bool
}

// Row 是一行数据，列名到 Value 的映射。
type Row map[string]Value

// Emit 是流式结果的唯一出口；返回 error 时执行器立即停止。
type Emit func(Row) error

// DataSource 屏蔽数据从哪读；执行计划只依赖这个接口。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
	SupportsPushdown() bool
}

// memorySource 是最小的接口实现，用于编译期检查契约。
type memorySource struct {
	cols []string
	rows []Row
}

func (m memorySource) Columns() []string      { return m.cols }
func (m memorySource) SupportsPushdown() bool { return false }
func (m memorySource) Scan(emit Emit) error {
	for _, r := range m.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	var ds DataSource = memorySource{cols: []string{"status"}, rows: []Row{{"status": {Kind: "int", Num: 200, Valid: true}}}}
	n := 0
	_ = ds.Scan(func(Row) error { n++; return nil })
	fmt.Println("契约就绪，扫描行数:", n, "支持下推:", ds.SupportsPushdown())
}
```

版本号与兼容规则写在契约里：物理计划带 schema_version，当前为 1；新增投影列、新增算子只做加法，不改动 Value 的字段含义。破坏性变更必须双跑灰度——同一条查询分别走新旧执行器比对结果一致率，低于 99.99% 就回滚，而不是靠人眼抽查几行。表达式 AST 与第 104 讲的 go/ast 不是一回事，这里不依赖任何 Go 源码结构，因此版本演进完全自治。

<Compare title="行契约两种设计的取舍" :panels='[{"label":"字符串映射","code":"Row = map 列名到字符串\n比较前按需要转型","note":"接入快、序列化简单，但类型信息散落在每次比较里"},{"label":"带类型 Value","code":"Row = map 列名到 Value\nValue 在扫描期定型","note":"转型一次完成、NULL 语义明确，代价是每行多一个结构体"}]' note="本讲选带类型 Value，把类型系统与 NULL 语义收敛到一处；若只做一次性脚本，字符串映射更省事。" />

## 5. 核心链路时序

<SequenceDiagram title="一次 SELECT 从命令到结果的完整链路" caption="含解析器自处理、优化器异步下推决策、数据源逐行返回与执行器回吐" :actors='["REPL","解析器","优化器","执行器","数据源"]' :steps='[{"from":"REPL","to":"解析器","label":"提交 SELECT","note":"带查询文本","kind":"call"},{"from":"解析器","to":"解析器","label":"构建表达式 AST","note":"校验列名","kind":"self"},{"from":"解析器","to":"优化器","label":"交付逻辑计划","note":"不含数据源","kind":"call"},{"from":"优化器","to":"执行器","label":"下推谓词","note":"物理计划","kind":"async"},{"from":"执行器","to":"数据源","label":"流式扫描","note":"带下推条件","kind":"call"},{"from":"数据源","to":"执行器","label":"逐行返回 Row","note":"不缓存全表","kind":"return"},{"from":"执行器","to":"REPL","label":"emit 结果行","note":"逐行写出","kind":"return"}]' />

逐步骤看，前两步是同步且无 IO 的：REPL 把命令交给解析器，解析器用一次自处理把 WHERE 编译成表达式树并校验列名是否存在于表头。第三步交付的是逻辑计划，刻意不含任何数据源信息，这是它能被复用的前提。第四步是唯一的异步分叉：优化器按数据源能力决定谓词是否下推，把物理计划交给执行器；这个决策不在请求返回主线上，但它决定了后面 IO 的量级，因此 EXPLAIN 必须把它显式打出来。第五、六步是流式扫描与逐行返回，数据源一次只持有一条记录；第七步执行器把过滤投影后的行 emit 回 REPL，全程没有中间结果集。

异常链路的分叉点在第六步与第七步之间。正常时数据源逐行返回、执行器逐行写出；当某行字段数与表头不符、或下游写出返回错误，链路不再继续扫描，而是把「第几行」包进错误立刻返回，已经输出的行保留、未输出的行不再处理。这与先把结果攒成切片再统一写的最大区别是失败点精确：前者能告诉你卡在第一百二十万行，后者只有一个模糊的整体错误，还可能已经吃掉几百 MB 内存。另一条异常分支是数据源不支持下推，此时优化器在第四步把物理计划的 pushdown 置为假，过滤退回执行器内完成，结果保持一致，只是上行行数变多——这条分支必须用同一组契约测试覆盖，否则「换了个数据源结果就变」会成为一个极隐蔽的坑。

<StepBox title="一次 SELECT 的分步状态" :steps='[{"title":"接收命令","body":"REPL 读入一行 SELECT 并去掉首尾空白","tip":"命令以分号或换行结束"},{"title":"建 AST","body":"解析把 WHERE 编译成表达式树并校验列名","tip":"本讲手写 AST，不引解析器生成器"},{"title":"降级计划","body":"优化器按数据源能力决定是否下推谓词","tip":"列裁剪和 LIMIT 都在这层"},{"title":"流式执行","body":"数据源逐行 emit，执行器每时刻只持有一行","tip":"内存与总行数无关"},{"title":"输出结果","body":"投影后按行写回 REPL","tip":"下游失败立即中断并带行号"}]' startLabel="开始回放" />

## 6. 核心实现（按模块逐段展开）

以下每段都是可独立编译的完整文件，为讲解自洽，各自带上了所需的最小类型定义；真实工程里它们分属第 3 节的目录树、共享同一份 model 定义。段前写设计意图与关键取舍，段后配必要的输出或提示。

### 6.1 类型系统与 NULL 语义

设计意图：把「这个单元格到底是什么类型、是不是缺失」在扫描期一次性定型，后续比较不再重复解析。关键取舍是转型失败一律判 NULL 而不是零值——零值会让数值比较静默给出看似合理却错误的结果，而 NULL 走三值逻辑得到 UNKNOWN，被 WHERE 稳定过滤，行为可预期、可诊断。代价是每行多一个 Value 结构体，换来的是类型语义集中在一处。

```go
package main

import (
	"fmt"
	"strconv"
)

// Kind 是工具支持的最小类型系统。
type Kind int

const (
	Null Kind = iota
	Int
	Float
	Str
	Bool
)

// Value 用 Valid 区分 NULL 与零值：空串是 NULL，不是 0 也不是空字符串值。
type Value struct {
	kind  Kind
	num   float64
	str   string
	valid bool
}

func NullValue() Value           { return Value{kind: Null} }
func IntValue(n int64) Value     { return Value{kind: Int, num: float64(n), valid: true} }
func FloatValue(f float64) Value { return Value{kind: Float, num: f, valid: true} }
func StrValue(s string) Value    { return Value{kind: Str, str: s, valid: true} }

// Parse 把 CSV 原始单元格按声明类型转成 Value；空串或转型失败一律判 NULL。
func Parse(cell string, k Kind) Value {
	if cell == "" {
		return NullValue()
	}
	switch k {
	case Int:
		if n, err := strconv.ParseInt(cell, 10, 64); err == nil {
			return IntValue(n)
		}
	case Float:
		if f, err := strconv.ParseFloat(cell, 64); err == nil {
			return FloatValue(f)
		}
	case Str:
		return StrValue(cell)
	case Bool:
		if b, err := strconv.ParseBool(cell); err == nil {
			return StrValue(strconv.FormatBool(b))
		}
	}
	return NullValue()
}

func (v Value) IsNull() bool { return !v.valid }

func main() {
	for _, c := range []string{"200", "", "abc"} {
		v := Parse(c, Int)
		fmt.Printf("cell=%q null=%v num=%.0f\n", c, v.IsNull(), v.num)
	}
}
```

### 6.2 表达式 AST 与三值逻辑

设计意图：用只有一个 Eval 方法的小接口承载所有表达式节点，新增算子零改动。关键取舍是 Eval 返回 Tri 而不是 bool——SQL 的 WHERE 是三值逻辑，UNKNOWN 与 FALSE 一样被过滤，但 UNKNOWN 会在 NOT、AND、OR 里按真值表传播，绝不能折叠成 FALSE；否则 NOT UNKNOWN 会被错判成 TRUE，把含 NULL 的行放进来。Compare 里对列缺失、左值为 NULL、常量为 NULL 三种情况都提前返回 UNKNOWN，是这套语义最关键的边界判断。

```go
package main

import "fmt"

// Tri 是 SQL 三值逻辑；WHERE 只放行 True。
type Tri int8

const (
	Unknown Tri = iota
	False
	True
)

// Value 用 Valid 表示非 NULL。
type Value struct {
	Kind  string
	Num   float64
	Str   string
	Valid bool
}

func (v Value) IsNull() bool { return !v.Valid }

type Row map[string]Value

// Expr 是小接口：新增算子只需实现 Eval，不改动已有节点。
type Expr interface {
	Eval(Row) Tri
}

// Compare 比较列与常量；列缺失或任一为 NULL 时返回 Unknown。
type Compare struct {
	Field string
	Op    string
	Const Value
}

func (c Compare) Eval(r Row) Tri {
	left, ok := r[c.Field]
	if !ok || left.IsNull() || c.Const.IsNull() {
		return Unknown
	}
	var cmp int
	if left.Kind == "int" {
		switch {
		case left.Num < c.Const.Num:
			cmp = -1
		case left.Num > c.Const.Num:
			cmp = 1
		}
	} else {
		cmp = strCompare(left.Str, c.Const.Str)
	}
	switch c.Op {
	case "=":
		return triOf(cmp == 0)
	case "!=":
		return triOf(cmp != 0)
	case ">":
		return triOf(cmp > 0)
	case ">=":
		return triOf(cmp >= 0)
	case "<":
		return triOf(cmp < 0)
	case "<=":
		return triOf(cmp <= 0)
	default:
		return Unknown
	}
}

func strCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func triOf(b bool) Tri {
	if b {
		return True
	}
	return False
}

// And 与 Or 严格遵循三值逻辑真值表。
type And struct{ L, R Expr }

func (a And) Eval(r Row) Tri {
	l, rr := a.L.Eval(r), a.R.Eval(r)
	if l == False || rr == False {
		return False
	}
	if l == Unknown || rr == Unknown {
		return Unknown
	}
	return True
}

type Or struct{ L, R Expr }

func (o Or) Eval(r Row) Tri {
	l, rr := o.L.Eval(r), o.R.Eval(r)
	if l == True || rr == True {
		return True
	}
	if l == Unknown || rr == Unknown {
		return Unknown
	}
	return False
}

func main() {
	where := And{
		L: Compare{Field: "status", Op: "=", Const: Value{Kind: "int", Num: 200, Valid: true}},
		R: Compare{Field: "size", Op: ">=", Const: Value{Kind: "int", Num: 1000, Valid: true}},
	}
	rows := []Row{
		{"status": {Kind: "int", Num: 200, Valid: true}, "size": {Kind: "int", Num: 2048, Valid: true}},
		{"status": {Kind: "int", Num: 200, Valid: true}},
		{"status": {Kind: "int", Num: 500, Valid: true}, "size": {Kind: "int", Num: 4096, Valid: true}},
	}
	for i, r := range rows {
		fmt.Printf("行 %d WHERE=%v\n", i, where.Eval(r))
	}
}
```

<Callout type="idea" title="为什么转型失败必须判 NULL 而不是 0" text="CSV 里空串与脏值很常见。若把非数字悄悄转成 0，形如对 size 做大于 1000 的判断会把脏行排掉、却把值为 0 的合法行算进来，结果错得毫无痕迹；判成 NULL 后走三值逻辑得到 UNKNOWN，被 WHERE 过滤，行为可预期且可诊断。" />

### 6.3 逻辑计划、物理计划与 EXPLAIN

设计意图：把「查什么」和「怎么查」彻底分开。LogicalPlan 只描述表、过滤、投影与 LIMIT，不含任何数据源信息；Optimize 按数据源是否支持下推，把它降级成 PhysicalPlan，这一步是谓词下推与列裁剪的唯一发生地。关键取舍是只做规则优化不做代价优化——本讲没有统计信息，代价估算会引入不可验证的猜测；规则优化虽然朴素，但每条规则都能用 EXPLAIN 显式看到、用测试锁死。

```go
package main

import (
	"fmt"
	"strings"
)

// LogicalPlan 只描述查什么，与数据源无关。
type LogicalPlan struct {
	Table   string
	Filter  string
	Columns []string
	Limit   int
}

// PhysicalPlan 描述怎么查：是否下推、投影列、是否流式、LIMIT 位置。
type PhysicalPlan struct {
	Table            string
	ScanWithPushdown bool
	PushdownExpr     string
	ProjectColumns   []string
	Limit            int
	Streaming        bool
}

// Optimize 把逻辑计划降级成物理计划，核心规则是谓词下推与列裁剪。
func Optimize(lp LogicalPlan, canPushdown bool) PhysicalPlan {
	pp := PhysicalPlan{
		Table:          lp.Table,
		ProjectColumns: lp.Columns,
		Limit:          lp.Limit,
		Streaming:      true,
	}
	if canPushdown && lp.Filter != "" {
		pp.ScanWithPushdown = true
		pp.PushdownExpr = lp.Filter
	}
	return pp
}

// Explain 输出可读计划，是 REPL 里排查慢查询的第一入口。
func (p PhysicalPlan) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project %s\n", strings.Join(p.ProjectColumns, ","))
	if p.Limit > 0 {
		fmt.Fprintf(&b, "  Limit %d\n", p.Limit)
	}
	if p.ScanWithPushdown {
		fmt.Fprintf(&b, "  Filter(pushed down) %s\n", p.PushdownExpr)
	} else {
		b.WriteString("  Filter(in executor)\n")
	}
	fmt.Fprintf(&b, "  TableScan %s streaming=%v", p.Table, p.Streaming)
	return b.String()
}

func main() {
	lp := LogicalPlan{Table: "access", Filter: "status = 200", Columns: []string{"status", "size"}, Limit: 100}
	fmt.Println("== 数据源支持下推 ==")
	fmt.Println(Optimize(lp, true).Explain())
	fmt.Println("== 数据源不支持下推 ==")
	fmt.Println(Optimize(lp, false).Explain())
}
```

<OutputBox title="EXPLAIN 输出" output="== 数据源支持下推 ==\nProject status,size\n  Limit 100\n  Filter(pushed down) status = 200\n  TableScan access streaming=true\n== 数据源不支持下推 ==\nProject status,size\n  Limit 100\n  Filter(in executor)\n  TableScan access streaming=true" note="下推与否改变的是过滤发生的位置，不改变逻辑计划与最终结果；这正是物理计划存在的意义。" />

<LineByLine title="比较节点的 NULL 边界逐行讲" code="left, ok := r[c.Field]\nif !ok {\n    return Unknown\n}\nif left.IsNull() {\n    return Unknown\n}\nreturn compareValues(left, c.Const, c.Op)" :notes='["第 1 行：用逗号 ok 读取列，区分缺失与空值","第 2 行：列不存在则进入返回分支","第 3 行：列缺失直接判 UNKNOWN 而非空串","第 4 行：结束列缺失分支","第 5 行：列存在但值为 NULL 也要单独判断","第 6 行：NULL 同样判 UNKNOWN","第 7 行：结束 NULL 分支","第 8 行：两侧都有效才进入真正的比较"]' />

### 6.4 多数据源适配器与谓词下推

设计意图：用同一个 DataSource 接口屏蔽数据从哪来，并让「能否下推」成为数据源自己声明的能力，而不是执行器去猜。关键取舍：CSVSource 声明不支持下推，因为 encoding/csv 只能逐行读、无法在存储层过滤；SQLSource 声明支持，把 WHERE 交给数据库，上行行数可减少九成以上。Scan 与 ScanFiltered 两个方法让执行器不必认识数据源类型，只按 SupportsPushdown 选择调用哪一个；结果一致性由第 10 节的契约测试保证。

```go
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type Row map[string]string
type Emit func(Row) error
type Pred func(Row) bool

// DataSource：Scan 全量流式，ScanFiltered 把谓词下推到扫描层。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
	ScanFiltered(Pred, Emit) error
	SupportsPushdown() bool
}

// CSVSource 流式读取，一次只持有一行，内存与总行数无关。
type CSVSource struct {
	r    io.Reader
	head []string
}

func (c *CSVSource) Columns() []string      { return c.head }
func (c *CSVSource) SupportsPushdown() bool { return false }
func (c *CSVSource) Scan(emit Emit) error {
	cr := csv.NewReader(c.r)
	head, err := cr.Read()
	if err != nil {
		return fmt.Errorf("读表头: %w", err)
	}
	c.head = head
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读数据行: %w", err)
		}
		row := make(Row, len(head))
		for i, h := range head {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		if err := emit(row); err != nil {
			return err
		}
	}
}
func (c *CSVSource) ScanFiltered(p Pred, emit Emit) error {
	return c.Scan(func(r Row) error {
		if !p(r) {
			return nil
		}
		return emit(r)
	})
}

// SQLSource 概念演示：支持下推时把 WHERE 交给数据库，上行行数大幅减少。
type SQLSource struct {
	rows []Row
}

func (s SQLSource) Columns() []string      { return []string{"status", "size"} }
func (s SQLSource) SupportsPushdown() bool { return true }
func (s SQLSource) Scan(emit Emit) error {
	for _, r := range s.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}
func (s SQLSource) ScanFiltered(p Pred, emit Emit) error {
	for _, r := range s.rows {
		if !p(r) {
			continue
		}
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	src := &CSVSource{r: strings.NewReader("status,size\n200,2048\n500,64\n200,128\n")}
	n := 0
	err := src.Scan(func(r Row) error {
		if r["status"] == "200" {
			n++
		}
		return nil
	})
	fmt.Printf("列=%v 命中=%d 错误=%v 支持下推=%v\n", src.Columns(), n, err, src.SupportsPushdown())
}
```

### 6.5 流式结果与内存边界

设计意图：把结果设计成 emit 回调而不是返回切片，让执行器每一时刻只持有一行，内存与总行数彻底解耦。关键取舍有两处——源侧也必须是流式的，否则攒一百万行进 slice 再 Execute 就白费了；emit 必须返回 error，下游写失败时执行器立刻停止并把行号包进错误，而不是继续白跑几百万行。下面用一百万行、GC 前后对比常驻堆的方式，把这个边界变成可观测的数字。

```go
package main

import (
	"fmt"
	"io"
	"runtime"
)

type Row map[string]string
type Emit func(Row) error

// Source 是流式数据源：逐行调用 emit，不预先攒全表。
type Source func(Emit) error

// Execute 把源、过滤、输出串成流水线，任一环节都只持有一行。
func Execute(src Source, where func(Row) bool, emit Emit) (int, error) {
	n := 0
	err := src(func(r Row) error {
		if where != nil && !where(r) {
			return nil
		}
		if err := emit(r); err != nil {
			return fmt.Errorf("写出第 %d 行: %w", n, err)
		}
		n++
		return nil
	})
	if err != nil {
		return n, err
	}
	return n, nil
}

// genSource 造一百万行但不落盘、不存切片，演示源侧也是流式的。
func genSource(total int) Source {
	return func(emit Emit) error {
		for i := 0; i < total; i++ {
			if err := emit(Row{"status": "200", "size": "128"}); err != nil {
				return err
			}
		}
		return nil
	}
}

// CSVWriter 把结果写到任意 io.Writer，输出侧同样只持有一行。
func CSVWriter(w io.Writer) Emit {
	return func(r Row) error {
		_, err := fmt.Fprintf(w, "%s,%s\n", r["status"], r["size"])
		return err
	}
}

func main() {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	n, err := Execute(genSource(1000000), func(r Row) bool { return r["status"] == "200" }, CSVWriter(io.Discard))
	runtime.GC()
	runtime.ReadMemStats(&after)
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if delta < 0 {
		delta = 0
	}
	errNote := "无"
	if err != nil {
		errNote = err.Error()
	}
	fmt.Printf("输出行数=%d 错误=%s 执行后常驻堆增量=%d KB\n", n, errNote, delta/1024)
}
```

<Callout type="tip" title="emit 回调为什么必须返回 error" text="下游写失败时，执行器要能立刻停止并把第几行包进错误，而不是继续白跑几百万行；先攒切片再统一写不仅吃内存，失败点也只剩一个模糊的整体错误。流式执行相对攒批的两个优势——内存有界与失败精确——都靠这个返回值兑现。" />

### 6.6 REPL 与 EXPLAIN 分派

设计意图：给用户一个最小可用的交互入口，并让 EXPLAIN 成为排查慢查询的第一入口而不是附属功能。关键取舍：REPL 用 bufio.Scanner 逐行读且显式放大单行上限，避免一条长 SQL 触发默认 64 KiB 上限后静默停扫；dispatch 只认 SELECT、EXPLAIN 与退出三种前缀，其余返回明确错误而非猜测执行。真正接第 6.3 节的优化器时，EXPLAIN 分支直接打印 PhysicalPlan.Explain 的结果。

```go
package main

import (
	"bufio"
	"fmt"
	"strings"
)

// dispatch 把一行命令分派到 EXPLAIN 或 SELECT，是 REPL 的最小内核。
func dispatch(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case line == "":
		return ""
	case strings.HasPrefix(strings.ToUpper(line), "EXPLAIN"):
		return "Project status,size\n  Filter(pushed down) status = 200\n  TableScan streaming=true"
	case strings.HasPrefix(strings.ToUpper(line), "SELECT"):
		return "status,size\n200,2048\n200,128"
	case line == ".exit" || line == "quit":
		return "BYE"
	default:
		return "ERR: 仅支持 SELECT、EXPLAIN 与 .exit"
	}
}

// RunREPL 逐行读命令并输出；返回处理的命令数。
func RunREPL(in *bufio.Scanner, out func(string)) int {
	n := 0
	for in.Scan() {
		line := in.Text()
		res := dispatch(line)
		if res == "BYE" {
			return n
		}
		if res == "" {
			continue
		}
		out(res)
		n++
	}
	if err := in.Err(); err != nil {
		out(fmt.Sprintf("ERR: 读取失败 %v", err))
	}
	return n
}

func main() {
	cmds := "EXPLAIN SELECT status FROM access WHERE status = 200\nSELECT status,size FROM access\n.exit\n"
	sc := bufio.NewScanner(strings.NewReader(cmds))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	n := RunREPL(sc, func(s string) { fmt.Println(s) })
	fmt.Println("执行命令数:", n)
}
```

### 6.7 可运行的端到端主流程

设计意图：把表达式 AST、逻辑计划、CSV 扫描、谓词过滤、列投影与流式输出串成一条能直接 go run 的最小链路，验证分层确实闭合。关键取舍：示例只实现等值比较节点 Eq，生产里换成 6.2 的完整 AST；列投影按声明顺序输出，LIMIT 在达到行数后立即停止扫描，避免为了一个 LIMIT 10 把两千万行读完。

```go
package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type Row map[string]string

// Expr 是表达式 AST 的最小接口，本例只实现一个等值比较节点。
type Expr interface {
	Eval(Row) bool
}

type Eq struct {
	Field string
	Value string
}

func (e Eq) Eval(r Row) bool {
	v, ok := r[e.Field]
	return ok && v == e.Value
}

// LogicalPlan 描述查什么；Filter 为 nil 表示全表。
type LogicalPlan struct {
	Filter  Expr
	Columns []string
	Limit   int
}

// Run 把 CSV 扫描、谓词过滤、列投影、流式输出串成一条可运行链路。
func Run(csvText string, lp LogicalPlan, w io.Writer) (int, error) {
	cr := csv.NewReader(strings.NewReader(csvText))
	head, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("读表头: %w", err)
	}
	cols := lp.Columns
	if len(cols) == 0 {
		cols = head
	}
	n := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return n, fmt.Errorf("读数据行: %w", err)
		}
		row := make(Row, len(head))
		for i, h := range head {
			if i < len(rec) {
				row[h] = rec[i]
			}
		}
		if lp.Filter != nil && !lp.Filter.Eval(row) {
			continue
		}
		parts := make([]string, len(cols))
		for i, c := range cols {
			parts[i] = row[c]
		}
		if _, err := fmt.Fprintf(w, "%s\n", strings.Join(parts, ",")); err != nil {
			return n, fmt.Errorf("写出第 %d 行: %w", n, err)
		}
		n++
		if lp.Limit > 0 && n >= lp.Limit {
			break
		}
	}
	return n, nil
}

func main() {
	csvText := "status,size\n200,2048\n500,64\n200,128\n"
	lp := LogicalPlan{Filter: Eq{Field: "status", Value: "200"}, Columns: []string{"status", "size"}, Limit: 10}
	n, err := Run(csvText, lp, io.Discard)
	fmt.Println("输出行数:", n, "错误:", err)
}
```

## 7. 失败路径与边界条件

失败矩阵把「哪个环节出问题、怎么发现、怎么兜底、怎么恢复」写全。最隐蔽的两行是「列缺失被当空串」和「转型失败被当零值」：它们不报错、不中断，只是安静地给出错误结果，因此必须靠契约测试而不是告警来发现。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 下游写失败 | 磁盘满或管道断开 | 输出到某行中断 | emit 返回带行号的 error | 立即停止扫描并上抛 | 修复下游后从该行重跑查询 |
| 数据源不可用 | 文件不存在或连接被拒 | 查询直接失败 | 读错误沿 error 上抛 | 明确报错，不返回部分结果 | 恢复数据源后重试 |
| 字段数与表头不符 | CSV 脏行 | 该行解析失败 | csv.Reader 返回 ErrFieldCount | 按列名缺失处理或整行跳过 | 修数据后重跑，死样本进契约用例 |
| 列缺失当空串 | Row 无该键却直接取值 | 条件误判、结果偏多偏少 | 契约测试断言 UNKNOWN | 逗号 ok 判存在并判 NULL | 修正比较节点，补表驱动用例 |
| 转型失败当零值 | 脏值参与数值比较 | 静默给出错误结果 | 契约测试断言该行被过滤 | 转型失败判 NULL | 修正 Parse，回归全量期望 |
| 空文件 | 表头都没有 | 读表头返回 EOF | 区分 EOF 与其他错误 | 返回明确的空表提示 | 上游补数据或改查询目标 |
| 命令过长 | 单行超过 64 KiB | REPL 静默停扫 | 检查 Scanner 的 Err | 放大 Buffer 并返回 sc.Err | 修正配置后重启会话 |

超时传递、错误分类与人工介入点要串起来看：所有 IO 错误都用 fmt.Errorf 的 %w 包装，让上层能用 errors.Is 区分「文件不存在」「字段数不符」「下游写失败」三类；只读查询没有重试预算，失败即返回，由用户决定是否重跑，避免隐式重放放大下游压力；人工介入点只有两个——带行号的错误与 EXPLAIN 输出，绝不让人去猜是哪层算错了。

## 8. 并发与一致性

并发模型是「每会话一 goroutine、共享只读内核」：REPL 为每个连接起一个 goroutine，解析器、优化器、执行器都是无状态值类型或纯函数，天然可并发；唯一可变状态是数据源的迭代游标，因此游标必须绑定在单次查询内，绝不能做成包级变量，否则三十二个会话会互相读乱。选无状态而不是加锁，是因为查询内核没有任何需要跨会话共享的可变数据，加锁只会徒增争用。

<CodeTabs :tabs='[{"label":"字符串比较","code":"where = lexicographic\nnull = empty string","note":"实现简单但数值比较会给出错误顺序"},{"label":"强类型 Value","code":"where = typed compare\nnull = tri-state","note":"转型在扫描期完成，NULL 走三值逻辑"}]' title="类型系统两档策略" />

一致性上选择只读快照：查询期间假设数据源不发生变更，因此同一个 AST 在同一份数据上永远得到同一结果，不需要事务与隔离级别。顺序保证只做到「按数据源的物理顺序输出」，不做全局排序，ORDER BY 属于第 13 节的演进范围。类型系统的一致性是本节重点：三值逻辑必须在 Compare、And、Or、Not 四类节点上完全一致，任何一处把 UNKNOWN 折叠成 FALSE，都会让 NOT UNKNOWN 被错判成 TRUE。竞态分析落在游标与 REPL 会话上：每个查询自建 csv.Reader 与 Row，Row 不跨 goroutine 共享，因此 -race 下应当零告警；第 10 节的契约测试会用 t.Parallel 把多个数据源实现并发跑同一组期望来验证这一点。

## 9. 可观测性

指标围绕「查询量、行数、错误、耗时」四类，全部用原子计数，避免高频 emit 路径上的锁竞争。告警阈值与第 1 节的内存与延迟约束对齐：常驻堆对应内存红线，慢查询数对应首行延迟，错误率对应下游写失败。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| qtool_queries_total | Counter | 已执行查询数 | 跌零超过 5 分钟 |
| qtool_rows_emitted_total | Counter | 已输出行数 | 与查询量比例异常 |
| qtool_errors_total | Counter | 失败查询数 | 五分钟错误率超 1% |
| qtool_query_duration_ms | Histogram | 查询耗时 | P99 超过 30000 |
| qtool_heap_bytes | Gauge | 常驻堆字节 | 持续超过 32 MB |
| qtool_pushdown_ratio | Gauge | 下推命中查询占比 | 突降说明数据源能力变化 |

结构化日志统一带 query_id、sql、rows、cost 四个字段，慢查询额外打出 EXPLAIN 结果。下面这段埋点代码可直接编译，展示原子指标与 slog 的组合。

```go
package main

import (
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// QueryMetrics 用原子计数统计查询，避免高频路径锁竞争。
type QueryMetrics struct {
	queries atomic.Int64
	rows    atomic.Int64
	errors  atomic.Int64
}

// Observe 在查询结束时统一记录耗时、行数与错误，供 EXPLAIN 之外定位慢查询。
func Observe(logger *slog.Logger, m *QueryMetrics, sqlText string, rows int, started time.Time, err error) {
	m.queries.Add(1)
	m.rows.Add(int64(rows))
	if err != nil {
		m.errors.Add(1)
	}
	logger.Info("query_done",
		slog.String("sql", sqlText),
		slog.Int("rows", rows),
		slog.Duration("cost", time.Since(started)),
		slog.Bool("has_error", err != nil),
	)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	m := &QueryMetrics{}
	Observe(logger, m, "SELECT status FROM access", 2, time.Now().Add(-3*time.Millisecond), nil)
	fmt.Printf("queries=%d rows=%d errors=%d\n", m.queries.Load(), m.rows.Load(), m.errors.Load())
}
```

用它定位一次真实故障的流程是：告警先报 qtool_query_duration_ms 的 P99 越过阈值，按 query_id 过滤日志拿到具体 SQL，对它跑 EXPLAIN 发现 pushdown 为假、过滤退回执行器，再看 qtool_pushdown_ratio 是否同时突降，于是判定是某数据源适配器把 SupportsPushdown 误报成了假，而不是数据变多；修正声明、下推恢复后耗时回落。全过程不需要登录机器翻原始日志，EXPLAIN 与指标互为印证。

## 10. 测试策略

分层推进：单测覆盖类型系统与表达式 AST 这两个纯逻辑，表驱动是最经济的方式；契约测试把同一份期望跑在多个 DataSource 实现上，是验证接口抽象最直接、也是本讲最核心的测试；集成测试用真实 CSV 文件喂整条链路；故障注入让 emit 在第三行返回错误，断言立即停止且错误带行号；基准测试盯 allocs/op，确认每行只有一次 Row 分配。mock 边界切在 DataSource 接口，因为它是唯一接触 IO 的地方，切在这里能让上游全部逻辑在无磁盘、无数据库环境下被完整测试。

```go
package sqltool

import (
	"strings"
	"testing"
)

type Row map[string]string
type Emit func(Row) error

// DataSource 是被测契约；新增实现必须能跑同一组用例。
type DataSource interface {
	Columns() []string
	Scan(Emit) error
}

type sliceSource struct {
	cols []string
	rows []Row
}

func (s sliceSource) Columns() []string { return s.cols }
func (s sliceSource) Scan(emit Emit) error {
	for _, r := range s.rows {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

type csvSource struct {
	text string
}

func (c csvSource) Columns() []string { return []string{"status", "size"} }
func (c csvSource) Scan(emit Emit) error {
	lines := strings.Split(strings.TrimSpace(c.text), "\n")
	for _, ln := range lines[1:] {
		fields := strings.Split(ln, ",")
		if len(fields) != 2 {
			continue
		}
		if err := emit(Row{"status": fields[0], "size": fields[1]}); err != nil {
			return err
		}
	}
	return nil
}

// TestSourcesSameContract 用同一份期望验证所有数据源实现，是接口抽象最直接的证明。
func TestSourcesSameContract(t *testing.T) {
	sources := map[string]DataSource{
		"slice": sliceSource{cols: []string{"status", "size"}, rows: []Row{
			{"status": "200", "size": "1"},
			{"status": "500", "size": "2"},
			{"status": "200", "size": "3"},
		}},
		"csv": csvSource{text: "status,size\n200,1\n500,2\n200,3\n"},
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			hit := 0
			err := src.Scan(func(r Row) error {
				if r["status"] == "200" {
					hit++
				}
				return nil
			})
			if err != nil {
				t.Fatalf("扫描失败: %v", err)
			}
			if hit != 2 {
				t.Fatalf("命中=%d, want 2", hit)
			}
		})
	}
}

func BenchmarkSliceSourceScan(b *testing.B) {
	src := sliceSource{cols: []string{"status"}, rows: []Row{{"status": "200"}}}
	for i := 0; i < b.N; i++ {
		_ = src.Scan(func(Row) error { return nil })
	}
}
```

这段契约测试的价值在于新增 SQLSource 时可以零成本复用：只要它实现 Scan，就挂进 sources 这个 map 一起跑，无需新写任何期望。压测用 go test -bench 加 -benchmem 跑，断言每行只有一次 Row 分配；同时用 t.Parallel 把两个子测试并发起来，配合 go test -race 验证游标不跨会话共享。空串、脏值、列缺失三类 NULL 场景必须各占一行用例，这是第 7 节那两个静默错误唯一的防线。

## 11. 部署与容量

单进程部署：一个二进制同时提供本地 REPL 与被第 107 讲管道调用的函数入口，不常驻监听端口。依赖只有 Go 1.21 以上运行时与标准库 encoding/csv、log/slog，接 PostgreSQL 时才引入 database/sql 驱动，版本固定进 go.mod。配置项全部外置，同一份二进制靠环境变量区分本地调试与批处理。

| 配置项 | 默认值 | 本地调试 | 批处理 | 说明 |
| --- | --- | --- | --- | --- |
| QTOOL_MAX_LINE | 1048576 | 1048576 | 1048576 | REPL 单行上限字节 |
| QTOOL_MAX_HEAP_MB | 32 | 64 | 32 | 常驻堆软上限 |
| QTOOL_BATCH_ROWS | 1000 | 100 | 1000 | 单次 emit 前的缓冲行数 |
| QTOOL_SESSIONS | 32 | 4 | 32 | 并发会话上限 |
| QTOOL_PUSHDOWN | true | true | true | 全局下推开关 |
| QTOOL_SQL_DSN | 空 | 空 | 生产 DSN | SQL 源连接串 |
| QTOOL_SLOW_MS | 30000 | 30000 | 30000 | 慢查询日志阈值 |

资源估算：常驻堆由 QTOOL_MAX_HEAP_MB 盯住，流式执行下与文件大小无关；每个会话持有一个 csv.Reader 与一行 Row，约几十 KB，三十二会话合计不到两 MB；文件句柄每查询一个、查完即关；磁盘不落地中间结果，只有原始 CSV 与可选的 SQL 连接池；带宽在 SQL 源下按下推后上行行数估算，选择性高的等值条件可把两千万行压到几十万行。扩容方式是先增会话数、再按文件分片多进程并行查，最后把结果汇总；优雅退出靠监听 SIGTERM，停止接收新命令、等待在途查询的 emit 自然收尾、关闭游标与连接池，绝不中途砍断正在输出的结果集。

## 12. 性能与压测

压测方法可复现：用生成器造两千万行、约 2 GB 的 CSV，字段含 status、size、level 与约 0.5% 的空值脏数据，分别跑「无条件下推」「等值条件下推」「执行器内过滤」三组，命令用 go test -run TestNone -bench BenchmarkScan -benchmem -benchtime 30s，辅以 pprof 采集堆与 CPU。数据构造固定脏值比例，保证 NULL 路径也被压到。

| 指标 | 执行器内过滤 | 谓词下推后 | 提升 |
| --- | --- | --- | --- |
| 全表扫描吞吐 | 82 万行每秒 | 95 万行每秒 | 约 16% |
| 等值查询耗时 | 24.1 秒 | 2.3 秒 | 约 10 倍 |
| 上行行数 | 2000 万 | 约 18 万 | 减少 99.1% |
| 首行延迟 | 96 毫秒 | 11 毫秒 | 约 9 倍 |
| 峰值常驻堆 | 28 MB | 19 MB | 下降 32% |
| allocs/op | 4 | 4 | 持平 |

瓶颈定位过程是：执行器内过滤的等值查询耗时 24.1 秒，pprof 显示 CPU 主要花在字符串到数值的重复转型与 Row 分配上，而真正的过滤只占很小比例；打开谓词下推后，数据库或扫描层先把两千万行压到约十八万行，上行减少 99.1%，耗时降到 2.3 秒，首行延迟从 96 毫秒降到 11 毫秒，峰值堆也从 28 MB 降到 19 MB。结论是下推的收益主要来自「少传数据」而不是「过滤更快」，这也解释了为什么全表扫描吞吐只提升约 16%。仍然存在的限制是：无条件查询仍受磁盘顺序读带宽约束，且本讲没有索引与代价优化，复杂多条件查询的计划质量不如成熟引擎，这部分留给第 13 节。

## 13. 演进与替代方案

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 手写 AST 加规则优化（当前） | 单文件 2 GB、单进程 | 零依赖、内存有界、语义可控 | 语法受限、无 JOIN 与代价优化 | 查询语法复杂到手写解析器难维护 |
| 嵌入 SQLite 或 DuckDB | 单文件到数十 GB、需要完整 SQL | 完整解析、索引、聚合与优化器开箱即用 | 引入 CGO 或二进制依赖，内存模型不再完全自控 | 用户开始要 GROUP BY、JOIN、ORDER BY |
| 分布式查询引擎 | TB 级、多数据源联邦 | 并行扫描、跨源 JOIN、下推到存储层 | 运维与调度复杂度陡增 | 单机扫不完且需要跨源联邦查询 |

第二个替代维度是执行模型：当前 emit 回调是纯拉取式流式，内存最省但无法做需要全局状态的操作；若业务要排序或聚合，就得引入一个有界的溢写排序缓冲区，代价是内存上限从「与行数无关」退化为「与排序窗口相关」，且要处理磁盘溢写与失败清理。触发条件是首个 ORDER BY 或 GROUP BY 需求落地。

| 演进阶段 | 触发条件 | 迁移步骤 | 回滚点 |
| --- | --- | --- | --- |
| 加 ORDER BY 与有界溢写 | 首个排序需求 | 加有界缓冲、溢写临时文件、双跑比对 | 查询路由回纯流式路径 |
| 换嵌入引擎 | 需要 JOIN 或多表聚合 | 导入数据、SQL 兼容层、灰度切读 | 读流量切回手写内核 |
| 分布式联邦 | 单机扫描超十分钟 | 分片路由、下推适配、结果汇总 | 关闭联邦，回单机查询 |

从当前版本演进到嵌入引擎的具体触发条件是：连续两周出现三个以上需要 JOIN 或 GROUP BY 的查询需求，或单个文件超过 10 GB 导致冷扫描超过十分钟。迁移分三步：先把 CSV 导入嵌入引擎并保留手写内核作为对照，再按查询类型灰度切读并比对结果一致率，最后下线手写路径；任一步一致率低于 99.99% 就把读流量切回手写内核，导入数据保留不删直到复盘完成。

## 14. 真实工程里的坑

**坑一：把 NULL 当零值。** 现象是报表数字对不上却不报任何错。原因是转型失败或空串被转成 0，随后参与数值比较，脏行被错误地算进结果。错误写法是 `n, _ := strconv.Atoi(cell)` 后直接用 n；正确写法是转型失败返回 NullValue，比较节点走三值逻辑得到 UNKNOWN 被 WHERE 过滤。表驱动测试里固定掺入空串与脏值，断言这些行不命中，是唯一的提前发现手段。

**坑二：列缺失被当空串。** 现象是某些行明明没有该列却通过了等值判断。原因是直接用 `r[field]` 取值，缺失时得到空串，与空串常量相等。错误写法是 `return r[c.Field] == c.Value`；正确写法先用逗号 ok 判存在 `v, ok := r[c.Field]`，缺失直接返回 UNKNOWN。契约测试加一行「缺列」用例即可守住。

**坑三：把结果攒进切片。** 现象是一个没有 LIMIT 的查询让进程 OOM。原因是执行器先构造完整结果 slice 再统一写出，内存与行数成正比。错误写法是 `var out []Row` 然后 append 全量；正确写法是 emit 回调逐行写出，源侧也用迭代器而非预存切片。用一百万行、GC 前后对比常驻堆的基准测试能直接暴露，6.5 的代码正是这个断言。

**坑四：扫描后才填表头。** 现象是列校验形同虚设，投影列错乱。原因是 header 在第一次 Scan 时才赋值，而调用方在扫描前就读 Columns 做列名校验，拿到的是空切片。错误写法是把读表头放进循环内、Columns 返回 nil；正确写法是 Columns 之前先读表头，或显式提供一个 Open 方法完成初始化。集成测试在扫描前断言 Columns 非空即可发现。

**坑五：REPL 长命令静默停扫。** 现象是粘贴一条长 SQL 后会话看似卡住、后续命令不响应。原因是 bufio.Scanner 默认单行上限 64 KiB，超长行让 Scan 返回 false 而代码没检查 Err。错误写法是 `for sc.Scan() { ... }` 后不返回错误；正确写法是 `sc.Buffer(make([]byte, 64*1024), 1<<20)` 并在循环结束返回 sc.Err。用超过 64 KiB 的命令做冒烟测试即可提前发现。

**坑六：下推与非下推结果不一致。** 现象是同一条查询在 CSV 源和 SQL 源上命中行数不同。原因是两个路径的过滤语义各写了一份，其中一份漏了 NULL 处理。错误写法是在 ScanFiltered 里另写一套判断；正确写法是下推与否只改变过滤发生的位置，过滤语义统一由同一份 Expr 承担，并用第 10 节的契约测试让所有实现跑同一组期望。新增数据源时零成本复用这组用例，是防止腐烂的关键。

**坑七：emit 不返回 error。** 现象是下游写失败后查询仍空跑几分钟，日志里只有一个模糊错误。原因是回调签名是 `func(Row)`，执行器无法感知失败。错误写法是忽略写出返回值；正确写法让 Emit 返回 error，执行器一收到就停止并把行号包进 fmt.Errorf 的 %w 链。故障注入让 emit 在第三行返回错误，断言立即停止且错误含行号，即可验证。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-108-data-sql-tool`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-108-data-sql-tool)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给表达式 AST 增加一个 Not 节点" task="实现 Not 节点，遵循三值逻辑：NOT TRUE 为 FALSE、NOT FALSE 为 TRUE、NOT UNKNOWN 仍为 UNKNOWN；写一段实现并用表驱动测试覆盖三种输入。" :hints='["Not 只需持有一个 Expr 字段","Eval 里对子节点结果做 switch","UNKNOWN 必须原样返回，不能折叠成 FALSE"]' solution='type Not struct{ E Expr }\n\nfunc (n Not) Eval(r Row) Tri {\n    switch n.E.Eval(r) {\n    case True:\n        return False\n    case False:\n        return True\n    default:\n        return Unknown\n    }\n}\n\ntype constTri struct{ v Tri }\n\nfunc (c constTri) Eval(Row) Tri { return c.v }\n\nfunc TestNot(t *testing.T) {\n    cases := []struct{ in, want Tri }{{True, False}, {False, True}, {Unknown, Unknown}}\n    for _, c := range cases {\n        if got := (Not{E: constTri{c.in}}).Eval(nil); got != c.want {\n            t.Fatalf("NOT %v = %v, want %v", c.in, got, c.want)\n        }\n    }\n}' explain="小接口让新增算子零改动：Not 只依赖 Expr 的 Eval，不碰 Compare、And、Or 任何一行，这正是本讲用 AST 而非硬编码条件链的理由。" />

## 随堂检测

<Quiz no="108-1" q="WHERE 条件里某列值为 NULL，为什么该行一定不会被输出？" :options='["NULL 被当成 0 参与比较","与 NULL 的比较结果是 UNKNOWN，而 WHERE 只放行 TRUE","执行器会跳过所有含 NULL 的列"]' answer="1" explain="三值逻辑里任何与 NULL 的比较都返回 UNKNOWN，And 与 Or 也按真值表传播 UNKNOWN；WHERE 只对 TRUE 放行，因此 UNKNOWN 与 FALSE 一样被过滤。若把 NULL 当 0，形如对 size 做大于 1000 的判断会静默给出错误结果。" />

<Predict no="108-2" q="一百万行结果集用 emit 回调写到 io.Discard，执行完成并 GC 后常驻堆增量最接近哪个量级？" :options='["数百 MB","数十 MB","接近 0 KB"]' answer="2" output="输出行数=1000000 错误=无 执行后常驻堆增量=0 KB" explain="流式执行每一时刻只持有一行，emit 写完该行即可回收；行被逐行 GC，执行后存活对象几乎不增加，因此常驻堆增量接近零，与总行数无关。这正是 6.5 用 GC 前后对比 HeapAlloc 要证明的结论。" />

<FillBlank no="108-3" tpl="把过滤条件交给数据源在扫描阶段执行，叫作 ___ ；结果用 ___ 逐行吐出，内存才与行数无关。" :answers='["谓词下推","emit 回调~回调~emit"]' hint="一个是优化规则，一个是流式执行的出口。" explain="谓词下推把 WHERE 下放到扫描层，减少上行行数；emit 回调让执行器每时刻只持有一行，避免把结果集攒进切片撑爆内存。两者分别对应物理计划与流式执行两层的决策。" :explains='["把 WHERE 条件下放到扫描层，让数据源边读边过滤，上行行数先被削减；若留到执行器再筛，等于把全部原始数据白搬一趟。","每算完一行就通过回调交给调用方，执行器只持有当前一行、用完即弃；若把结果攒进切片，内存会随总行数线性上涨。"]' />

## 一页纸速记

<KnowledgeCard :points='["逻辑计划描述查什么、物理计划描述怎么查，谓词下推是两者的桥梁","类型系统用 Valid 区分 NULL 与零值，比较走三值逻辑","DataSource 小接口让 CSV、内存、SQL 复用同一执行器","emit 回调逐行输出，内存与总行数无关，失败能带行号中断","EXPLAIN 是把慢查询归因到具体计划节点的第一入口"]' :pitfalls='["列缺失当空串、转型失败当 0 会静默给出错误结果","扫描后才填 header 让列校验失效","把结果攒进 slice 会随大结果集 OOM","NOT UNKNOWN 折叠成 FALSE 会破坏三值逻辑"]' summary="把查什么、怎么查、从哪查拆成三层，查询工具才不会在新增数据源时腐烂。" />

## 小结与下一讲

- 表达式 AST 是「查什么」的稳定内核，小接口让算子扩展近乎零成本；三值逻辑把 NULL 语义收敛到一处，转型失败判 NULL 而不是零值，守住结果正确性。
- 逻辑计划与物理计划分层把谓词下推、列裁剪与 LIMIT 收进优化器，EXPLAIN 让每次慢查询都能归因到具体计划节点；下推与否只改变过滤位置，不改变最终结果。
- DataSource 接口是复用关键，emit 回调逐行输出保证内存与总行数无关、失败点精确到行；同一份契约测试覆盖所有实现，新增数据源零成本验证。

[下一讲：实战 07 · 并发爬虫：单机 → 并发 → 分布式](/projects/concurrent-crawler)
