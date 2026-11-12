---
title: 第 103 讲 · 实战 01 · 待办清单 CLI
description: 用命令树、JSON 持久化、原子写与退出码契约交付第一个完整命令行工具
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="90 分钟" goal="从零交付一个能进脚本、抗崩溃、可发布的命令行工具" prereq="第 102 讲 · 资深能力图谱、技术选型与持续精进" />

# 第 103 讲 · 实战 01 · 待办清单 CLI

> 一个每天被自己、被 cron、被 CI 脚本反复调用的命令行工具，和一份「能跑一遍」的课后作业，差别不在功能多少，而在失败路径想得多全：命令怎么组织、数据落在哪里、两个进程同时写会不会互相覆盖、写到一半被 kill 会不会留下解析不了的半截文件、脚本靠什么判断该重试还是该报警。本讲先把这些约束量化，再用标准库把每一条都落到实处，最后给出引入 cobra 与发布工具的工程化形态。

<CheckList id="l103-goals" title="本讲目标" :items='["把零散命令组织成可分层、可加子命令的命令树","用临时文件加 fsync 加 rename 实现崩溃安全的原子写","用文件锁把多进程的读改写串行化","用分级退出码与 stderr 约定让脚本能判定成败","用 flag/env/config 三级优先级与 go install 交付工具"]' />

## 0. 项目定位与真实场景

待办清单是几乎每个开发者都写过的「第一个完整程序」，但真实使用场景比想象中复杂。它通常跑在一台开发者机器上，由三类调用方共享同一份数据：人手敲的交互式命令、每天凌晨跑的归档脚本、以及 CI 里根据任务状态决定是否继续的自动化判断。这三类调用方对「可靠」的定义完全不同：人要的是清晰的输出和有用的报错，脚本要的是稳定的退出码和干净的 stdout，无人值守的定时任务要的是崩溃之后文件依然能读。

先把量级钉死，后面所有架构决策才有依据。单人使用下活跃任务不超过 2000 条，冷数据归档后主文件稳定在 500 条以内；命令总数控制在 8 个左右；每天调用几十次，峰值出现在早上的一批批量操作；同一时刻可能有 2 到 4 个进程同时触碰同一个数据文件，例如终端里手动 add 的同时 cron 正在 archive；恢复目标定为 RPO 等于 0（不允许丢已经确认成功的写入）、RTO 小于 1 秒（重新执行命令即可，不需要人工修复）。数据量这么小，用数据库是明显的过度设计，但「小」恰恰掩盖了并发与持久化这两类最容易踩的坑。

现有土办法的痛点很集中：直接编辑一个 txt 或 JSON 文件，Ctrl+S 中途断电就得到半截内容；用 `echo >>` 追加，两个 shell 同时跑就互相覆盖；脚本靠解析人话输出判断成败，一旦改了句文案就全线崩；数据文件损坏后没有备份，只能从头再录。本讲要解决的核心矛盾是：**用最简单的单文件存储，同时交付数据库级别的写入原子性、进程级的一致性和运维级的失败可判定性**。这三个目标不靠引入重型依赖，而靠对操作系统原语（rename、fsync、flock）的正确使用。

<Callout type="idea" title="为什么值得单独做一讲" text="因为它把「能跑」和「能交付」之间的距离完整走了一遍：命令树是结构、原子写是持久化、文件锁是并发、退出码是接口、备份恢复是运维。这五件事在后面 KV 存储、ETL、调度器里会反复出现，只是换了规模。" />

## 1. 需求与约束

先把功能拆成可验收的条目，每条都带一个能被自动检验的标准，避免「做完了但说不清做完没有」。`add` 必须返回单调递增且删除后不复用的 ID；`list` 在 `--json` 下输出可被 `jq` 直接解析的纯数据；`done` 对不存在的 ID 必须给出专属退出码而不是笼统失败；`archive` 只在归档成功后才改写主文件。命令数刻意压在 8 个，是为了让读者能把命令树整个装进脑子，而不是淹没在参数里。

| 编号 | 需求 | 验收标准 |
| --- | --- | --- |
| FR-1 | 新增任务 | 返回全局单调递增 ID，空标题拒绝且退出码为 2 |
| FR-2 | 列出任务 | 默认表格视图，加 --json 输出合法 JSON 且 stdout 无其它文本 |
| FR-3 | 标记完成 | 写入 done_at，重复执行结果一致（幂等） |
| FR-4 | 删除任务 | 删除后新任务 ID 不复用历史最大值 |
| FR-5 | 按 TTL 归档 | 仅归档已完成且超期的任务，返回归档条数 |
| FR-6 | 配置三源 | flag 覆盖 env 覆盖 config 覆盖默认值 |
| FR-7 | 崩溃安全 | 写入中途被杀，文件仍是上一版完整可解析内容 |
| FR-8 | 版本迁移 | 能读旧版裸数组文件并升级为带版本包络 |

非功能指标同样要可度量。这里的 QPS 不是网络服务意义上的吞吐，而是「单位时间内可完成的读改写事务数」，因为每次写都要 fsync，磁盘才是真瓶颈。一致性级别选线性一致：任一时刻读到的都是某次成功写入的完整快照，绝不读到一个交错状态。这是用文件锁把整个读改写事务串行化换来的，代价是并发写吞吐被压到单线程，但在这个量级下完全够用。

| 指标 | 目标值 | 依据与代价 |
| --- | --- | --- |
| 命令数 | 8 个 | 覆盖增列完删归档配置帮助，够用且可控 |
| 活跃数据量 | ≤ 2000 条 | 超过后全量序列化明显变慢，触发换存储 |
| 并发进程数 | ≤ 4 | 文件锁串行化，超时预算 500 毫秒 |
| 单次写耗时 P99 | ≤ 15 毫秒 | 主要成本是两次 fsync，SSD 下可达 |
| 一致性 | 线性一致 | 用独占锁换简单正确，牺牲并发写吞吐 |
| 可用性 | 单机 99.9% | 无网络依赖，故障域只有本机磁盘 |
| RPO | 0 | 成功返回的写入必须已 fsync 落盘 |
| RTO | ≤ 1 秒 | 崩溃后重跑命令即可，备份用于损坏回滚 |

明确「本讲不做什么」和假设边界同样重要：不做多用户协作、不做网络同步、不做加密、不做全文检索；假设运行在 Unix 系（flock 依赖）、单用户单主机、本地磁盘非网络文件系统（NFS 上 flock 语义不可靠）、时钟由 NTP 校时但允许小幅回拨。把这些写出来，是为了让读者知道哪天场景变了、哪条假设先被打破，就该看第 13 节的演进路线。

<Callout type="warn" title="先量化再选型" text="一上来就说用 SQLite 或上 cobra 是本末倒置。是 2000 条、4 进程、RPO 为 0 这三个数字，逼出了单文件 JSON 加原子写加文件锁这套最小方案；数字变了，方案才该变。" />

## 2. 架构蓝图

系统分三层，切分依据是「变化频率」而不是技术名词。命令层因为要不断加子命令、改 flag、换输出格式，改动最频繁；存储层因为可能从 JSON 换成 SQLite，一旦更换几乎整体重写；领域层只有 Task 与错误模型，最稳定。依赖方向严格单向向下，存储层绝不反过来认识 cobra 命令，否则换个命令行框架会连累持久化代码。

<ArchBlueprint title="待办 CLI 三层架构" caption="命令层解析分发，编排层承载领域规则，存储层独占文件读写；箭头即依赖方向，绝不反向" :layers='[{"name":"命令接入层","note":"把 argv 变成一次带配置的调用","protocol":"flag 加 env 加 config 三源配置与显式 error","items":[{"label":"cobra 命令树","sub":"子命令分发与帮助","detail":"职责：把 add/list/done/delete/archive 组织成树，各自持有独立 flag，统一生成帮助与补全；技术选型：生产用 cobra，教学版先用标准库手写分发以暴露本质；失败处理：参数非法由 Args 校验器拦截并映射退出码 2，SilenceUsage 避免把帮助刷进脚本日志。","tone":"blue"},{"label":"配置解析器","sub":"三源优先级合并","detail":"职责：按默认值、配置文件、环境变量、命令行 flag 的顺序叠加，后一级只覆盖显式设置的键；技术选型：标准库 encoding/json 加 os.Getenv，路径统一展开波浪号；失败处理：环境变量转型失败报可读错误并拒绝启动，绝不静默用默认值掩盖拼错。","tone":"green"}]},{"name":"领域与编排层","note":"校验规则与事务边界都在这里","protocol":"Store 接口加哨兵 error","items":[{"label":"命令编排器","sub":"一次命令一个事务","detail":"职责：在获取文件锁之后、释放之前完成读、改、写整个事务，保证读改写不被其它进程插入；技术选型：依赖 Store 接口而非具体文件实现，便于测试注入内存版；失败处理：任一步失败都不提交写入，旧文件保持完好，错误向上包装成可分类的哨兵错误。","tone":"orange"},{"label":"领域校验","sub":"标题与 ID 规则","detail":"职责：进入存储层前拒绝空标题、非法优先级、越界 ID，并负责 maxID 加一分配策略；技术选型：纯函数校验，不碰 IO，方便表驱动测试；失败处理：空标题返回 ErrUsage，目标不存在返回 ErrNotFound，两者对应不同退出码。","tone":"purple"}]},{"name":"存储与持久化层","note":"独占文件读写，屏蔽一切崩溃细节","items":[{"label":"原子写入器","sub":"临时文件加 fsync 加 rename","detail":"职责：把整份数据序列化到同目录临时文件，fsync 后 rename 覆盖，再 fsync 目录项，最后清理残留临时文件；技术选型：os.CreateTemp 加 os.Rename，同目录 rename 保证原子性，不引第三方；失败处理：任一步失败都保留旧文件并返回错误，磁盘满时写临时文件失败但绝不破坏正式文件。","tone":"red"},{"label":"文件锁与备份","sub":"flock 串行化加快照恢复","detail":"职责：用 advisory lock 把跨进程读改写串行化，写前留一份时间戳备份，检测损坏时从最近备份恢复；技术选型：syscall.Flock 非阻塞轮询加超时预算，锁文件独立于数据文件；失败处理：锁超时返回 ErrBusy 对应退出码 4，进程崩溃由内核自动释放锁，不留死锁。","tone":"gray"}]}]' />

逐层解读为什么这样切。命令接入层存在的唯一理由是隔离「人机交互」的不确定性：帮助文案会改、参数名会改、输出格式会从表格变成 JSON，但这些都不该碰存储。领域与编排层是最容易被跳过、却最该存在的一层：把「先取锁再读改写」这个事务边界写死在一处，增删改查四个命令就都不必各自记得加锁。存储层则把操作系统原语的脏活全部吞掉，向上只暴露 `List/Add/Done/Delete/Archive` 五个方法；将来把 JSON 换成 SQLite，改动被完整关在这一层。

<FlowTimeline :steps='[{"title":"解析","desc":"cobra 分发子命令并合并三源配置","tone":"blue"},{"title":"加锁","desc":"flock 独占锁，500 毫秒拿不到就退 4","tone":"green"},{"title":"读取","desc":"load 按 version 兼容 v1 裸数组","tone":"orange"},{"title":"校验改写","desc":"领域层拒绝空标题并分配 maxID 加一","tone":"purple"},{"title":"原子落盘","desc":"临时文件 fsync 后 rename 再 fsync 目录","tone":"red"},{"title":"输出","desc":"结果写 stdout，错误写 stderr 并定退出码","tone":"gray"}]' direction="horizontal" />

依赖方向必须单向向下，还有个很实在的理由：可测试性。编排层依赖的是 `Store` 接口，测试时可以注入一个内存实现，把文件 IO、fsync、flock 全部挡在测试之外，单测就能跑得又快又稳；一旦允许存储层 import 命令层、或命令层直接 `os.WriteFile`，就再也塞不进这个接缝，任何单元测试都得真的去碰磁盘。这条原则是后面第 10 节整个测试策略的地基。

## 3. 模块职责与依赖方向

把每层的职责落成一张可对接的表：输入是什么、输出是什么、依赖谁、是否持有状态、失败时怎么办，五列写清楚，模块边界就没有含糊空间。特别注意「状态」这一列：只有存储层持有文件与内存态，命令层与领域层都应该是无状态的纯逻辑，这样才能随便并发、随便重入。

| 模块 | 输入 | 输出 | 依赖 | 状态 | 失败行为 |
| --- | --- | --- | --- | --- | --- |
| 命令树 | os.Args | 一次调用 | 配置解析器 | 无 | 参数非法退 2，未知命令退 2 |
| 配置解析器 | config 文件、env、flag | Config | 标准库 | 无 | 转型失败拒绝启动 |
| 命令编排器 | 命令参数、Store | Task 或 error | 领域校验、Store | 无 | 事务内任一步失败即整体回滚 |
| 领域校验 | 原始字符串、ID | 合法值或哨兵错误 | 无 | 无 | 空标题、越界 ID 就地拒绝 |
| 原子写入器 | File 结构 | 落盘结果 | 文件系统 | 临时文件句柄 | 保留旧文件，返回可读错误 |
| 文件锁 | 锁路径、超时 | 锁句柄 | syscall.Flock | fd | 超时退 4，崩溃内核释放 |
| 备份恢复 | 数据路径 | 快照或恢复结果 | 文件系统 | bak 文件 | 损坏时从最近快照回滚 |

包目录树刻意保持扁平，让读者能一眼看全。真实项目里目录会更多，但分层关系不变。

```text
todo/
├── main.go            # 仅组装：配置、命令树、存储、退出码
├── cmd/
│   ├── root.go        # cobra 根命令与 PersistentPreRunE
│   ├── add.go         # add/list/done/delete/archive 子命令
│   └── output.go      # 表格视图与 --json 视图，只写 stdout
├── domain/
│   ├── task.go        # Task、Priority、Store 接口、哨兵错误
│   └── validate.go    # 纯函数校验与 maxID 分配策略
├── store/
│   ├── json.go        # load、atomicWrite、版本迁移与归档
│   ├── lock.go        # FileLock 与 WithLock
│   └── backup.go      # 时间戳快照与损坏恢复
└── config/
    └── config.go      # 默认值、文件、env、flag 四级合并
```

依赖倒置的具体做法：`domain` 定义 `Store` 接口，`store` 提供实现，`cmd` 只认识接口，`main.go` 负责在启动时把具体实现注入进去。这样一来 `domain` 不 import 任何上层包，`store` 也不认识 cobra，箭头全部指向领域层。单元测试给编排器传一个 `fakeStore`，就能在不碰磁盘的前提下验证「加锁失败时不写文件」这类分支。

<ConceptDiagram type="architecture-layer" title="单向依赖与倒置" caption="cmd 与 store 都只依赖 domain 定义的接口，main 负责注入具体实现" />

<Callout type="warn" title="最常见的边界错误" text="在 add 子命令的 RunE 里直接 os.WriteFile。看似省事，实则把持久化逻辑复制进了每个命令：换存储要改五处、加锁记不记得全看心情、测试再也无法绕开磁盘。正确做法是命令层只调 Store 接口，事务边界收敛到编排器一处。" />

<Term word="Store 接口" text="领域层声明的存储契约，屏蔽文件、锁、序列化的全部细节，是依赖倒置与可测试性的接缝。" />

## 4. 数据与协议契约

物理上只有一个 `todo.json`，但字段语义需要像数据库表一样被严格约定，否则版本一升级就没人记得某字段能不能为空。下面这段 DDL 是逻辑视图，用来精确描述字段类型、必填与默认值，真实落盘是它对应的 JSON 包络。

```sql
-- 逻辑视图：物理落盘是单个 todo.json，此处用 DDL 精确描述字段契约
CREATE TABLE task (
  id         INTEGER PRIMARY KEY,            -- 单调递增，删除后绝不复用
  title      TEXT    NOT NULL,               -- TrimSpace 后必须非空
  priority   INTEGER NOT NULL DEFAULT 1,    -- 0 最紧急，2 最低
  done       INTEGER NOT NULL DEFAULT 0,    -- 0 或 1
  created_at TEXT    NOT NULL,              -- RFC3339 UTC
  done_at    TEXT                            -- 可空，omitempty，未完成时缺省
);
```

字段契约表把零值与兼容规则写死，这是灰度升级的依据：新版本读旧数据时，缺字段一律按零值处理，绝不因为多了个字段就拒绝整个文件。

| 字段 | 类型 | 语义 | 零值 | 兼容规则 |
| --- | --- | --- | --- | --- |
| version | int | 文件格式版本 | 1 | 缺省按 v1 裸数组解析并升级 |
| updated_at | string | 最近写入时刻 | 空 | 可空，仅用于排查，不参与逻辑 |
| tasks | 数组 | 任务列表 | 空数组 | 必须是数组，元素缺字段按零值 |
| id | int | 单调 ID | 0 | 由 maxID 加一生成，不由数组长度生成 |
| title | string | 标题 | 空 | 空视为非法，命令层拒绝 |
| done_at | string | 完成时刻 | 空 | omitempty，未完成时缺省 |

对外 API 就是命令本身，脚本通过退出码和 stdout 与之对接。`--json` 是给机器看的稳定契约，任何面向人的文案改动都不许影响它；错误信息一律进 stderr，这样 `todo list --json | jq` 永远拿得到干净数据。

| 命令 | 参数 | stdout（--json） | 成功退出码 |
| --- | --- | --- | --- |
| add | 标题、--priority | 新增任务对象 | 0 |
| list | --all、--json | 任务数组 | 0 |
| done | id | 更新后任务对象 | 0 |
| delete | id | 被删 id | 0 |
| archive | --ttl | 归档条数 | 0 |

```json
{"ok": true, "data": {"id": 7, "title": "写第 103 讲", "priority": 1, "done": false, "created_at": "2026-11-12T09:00:00Z"}}
```

内部接口同样要白纸黑字定下来，它是编排层与存储层之间唯一的协议。下面这段代码完整可编译，演示了接口如何被一个内存实现满足，这正是测试注入的形态。

```go
package main

import (
	"fmt"
	"time"
)

type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// Store 是领域层定义的存储契约，cmd 与编排器只依赖它。
type Store interface {
	List() ([]Task, error)
	Add(title string) (Task, error)
	Done(id int) (Task, error)
	Archive(ttl time.Duration) (int, error)
}

type memStore struct{ tasks []Task }

func (m *memStore) List() ([]Task, error) { return m.tasks, nil }

func (m *memStore) Add(t string) (Task, error) {
	max := 0
	for _, x := range m.tasks {
		if x.ID > max {
			max = x.ID
		}
	}
	x := Task{ID: max + 1, Title: t}
	m.tasks = append(m.tasks, x)
	return x, nil
}

func (m *memStore) Done(id int) (Task, error) { return m.tasks[id-1], nil }
func (m *memStore) Archive(time.Duration) (int, error) { return 0, nil }

func main() {
	var s Store = &memStore{}
	s.Add("示例任务")
	all, _ := s.List()
	fmt.Println("任务数:", len(all))
}
```

契约设计上有一个真实取舍：到底存「裸数组」还是「带版本的包络」。裸数组最省事，但一旦要加字段、要区分格式代际、要记录写入时刻，就没有任何落脚点；带版本包络多一层嵌套，却让迁移有据可依。本讲选后者，代价是每个文件多几十字节、解析多一次类型探测。

<Compare title="两种落盘契约的取舍" :panels='[{"label":"裸数组","code":"首字符是方括号\n[ 任务1, 任务2, 任务3 ]","note":"最省字节，但无版本号，加字段或换代时无法判别格式，只能靠猜"},{"label":"版本包络","code":"首字符是花括号\nversion:2 updated_at:时间 tasks:[...]","note":"多一层嵌套，换来可迁移、可追溯、可灰度，本讲采用"}]' />

<Term word="omitempty" text="json 标签选项，零值字段序列化时省略；done_at 未完成时不写，读旧文件时缺省即零值，是向前兼容的关键。" />

<FillBlank tpl="配置合并时，优先级最高的是 ___，损坏文件恢复时应从最近的 ___ 回滚，落盘包络里的 ___ 决定用哪套解析路径。" :answers='["命令行 flag~flag","备份快照~备份~bak 快照","version~版本号"]' hint="三个空分别对应三源配置、恢复机制与版本迁移。" explain="flag 必须能压过 env 与文件，否则脚本无法临时改路径；备份快照是 RPO 之外的最后防线；version 让 load 能在 v1 裸数组与 v2 包络之间自动分流。" :explains='["命令行是单次调用临时指定的，必须盖过环境变量与配置文件，否则脚本连临时换数据目录都做不到，三源按临时优先逐层覆盖。","主文件写坏后当前内容已不可信，只有原子写时另存的 bak 保有上一份完整数据；越近的快照丢的写入越少，它决定恢复点。","v1 是裸数组、v2 是带字段的包络，首字符与结构都不同，读取时只能靠包络里的版本号判断该走哪套解码，才能兼容旧文件。"]' />

## 5. 核心链路时序

以 `todo add` 为例看一次完整写事务。它包含参数校验、加锁、兼容读取、领域改写、原子落盘、异步快照、结果返回七个动作，其中「加锁之后到释放之前」是必须连续、不可被打断的临界区。时序图里特意标了三类消息：同步调用、自处理（分配 ID 与参数校验都在进程内完成）、虚线返回，以及后台触发的异步快照。

<SequenceDiagram title="todo add 的一次完整写事务" caption="含参数自校验、独占锁获取与返回、兼容读取、原子写、异步备份与结果返回" :actors='["Shell","cobra","编排器","文件锁","JSON 存储","备份器"]' :steps='[{"from":"Shell","to":"cobra","label":"todo add 标题","note":"带 --json","kind":"call"},{"from":"cobra","to":"cobra","label":"校验参数","note":"MinimumNArgs","kind":"self"},{"from":"cobra","to":"编排器","label":"调用 RunE","note":"传入 Config","kind":"call"},{"from":"编排器","to":"文件锁","label":"Acquire 独占锁","note":"500ms 预算","kind":"call"},{"from":"文件锁","to":"编排器","label":"返回锁句柄","note":"失败则退 4","kind":"return"},{"from":"编排器","to":"JSON 存储","label":"读并迁移","note":"v1 兼容升级","kind":"call"},{"from":"编排器","to":"编排器","label":"追加任务","note":"maxID 加一","kind":"self"},{"from":"编排器","to":"JSON 存储","label":"原子写","note":"tmp+fsync+rename","kind":"call"},{"from":"编排器","to":"备份器","label":"触发快照","note":"写时间戳 bak","kind":"async"},{"from":"编排器","to":"Shell","label":"输出结果","note":"exit 0","kind":"return"}]' />

逐步解读几个容易被忽略的点。第二步参数校验放在进入编排器之前，是为了让「用户输错」这类失败根本不占用文件锁；第四步拿锁带 500 毫秒预算，是为了避免某个僵死进程持有锁时，后来的命令无限期挂住；第六步读取必须放在锁内，否则可能读到别的进程刚写了一半的旧数据；第七、八步的改写与落盘必须连续，中间任何一次让出锁都会导致丢失更新；第九步快照做成异步，是因为备份失败不该让已经成功的写入变成失败；最后一步返回前，编排器用 defer 释放锁，保证 panic 路径也不留死锁。

异常链路在第四步就分叉：若 500 毫秒内拿不到锁，编排器直接返回 `ErrBusy`，cobra 把错误写进 stderr 并以退出码 4 结束，脚本看到 4 就知道「稍后重试即可，不是数据坏了」。另一条分叉在第八步：若临时文件写到一半磁盘满，`Write` 失败，函数立即返回且**不执行 rename**，磁盘上的正式文件仍是上一版完整内容；`defer os.Remove` 清掉残留的临时文件，用户释放空间后重跑命令即可。这与「直接覆盖写」的差别是决定性的：后者在同样故障下会留下一个既不是旧版也不是新版的半截 JSON，下次 `list` 直接崩。两条分叉的共同设计原则是——**任何失败都让系统停在最近一次成功的状态，而不是一个中间状态**。

<StepBox title="一次 add 的关键步骤" :steps='[{"title":"第 1 步","body":"参数校验，空标题立即以退出码 2 结束，不碰锁","tip":"越早拒绝越省资源"},{"title":"第 2 步","body":"带超时获取独占文件锁，失败退 4","tip":"超时而非永久阻塞"},{"title":"第 3 步","body":"锁内读取并按 version 迁移","tip":"绝不在锁外读"},{"title":"第 4 步","body":"改写后原子落盘，defer 释放锁","tip":"临界区连续不可分"}]' startLabel="开始复现" />

## 6. 核心实现（按模块逐段展开）

以下每段都是完整可编译的 Go 文件，import 只写真正用到的包，用 Go 1.21+ 语法。段前写清设计意图与关键取舍，段后配逐行讲解或输出演示。顺序按「模型 → 写 → 锁 → 命令 → 配置 → 退出码 → 迁移备份」推进，正好是从数据到边界、再到外围的一圈。

### 6.1 领域模型与存储接口

设计意图：先把最稳定的东西定下来。`Task` 只描述数据，`Store` 只描述能力，两者都不碰文件、不碰命令行。关键取舍是给落盘加一个 `File` 包络并带 `schemaVersion`，而不是直接序列化 `[]Task`——多一层嵌套换来可迁移性；再加一组哨兵错误，让上层能靠 `errors.Is` 分类而不是靠字符串匹配，这是退出码契约的前提。

```go
package main

import (
	"errors"
	"fmt"
	"time"
)

// Priority 表示任务优先级，数值越小越紧急。
type Priority int

const (
	P0 Priority = iota
	P1
	P2
)

// Task 是唯一的领域实体；CreatedAt 用于 TTL 归档，DoneAt 标记完成时刻。
type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Priority  Priority  `json:"priority"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
	DoneAt    time.Time `json:"done_at,omitempty"`
}

// schemaVersion 是文件格式版本，迁移时按它选择升级路径。
const schemaVersion = 2

// File 是落盘包络：带版本与生成时刻，避免裸数组无法演进。
type File struct {
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Tasks     []Task `json:"tasks"`
}

// Store 抽象存储，命令层只依赖这个接口，方便替换 JSON 或 SQLite。
type Store interface {
	List() ([]Task, error)
	Add(title string, p Priority) (Task, error)
	Done(id int) (Task, error)
	Delete(id int) error
	Archive(ttl time.Duration) (int, error)
}

var ErrUsage = errors.New("参数错误")
var ErrNotFound = errors.New("任务不存在")
var ErrCorrupted = errors.New("数据文件损坏")
var ErrBusy = errors.New("存储被其他进程占用")
var ErrLockTimeout = errors.New("获取文件锁超时")

func main() {
	f := File{Version: schemaVersion, Tasks: []Task{{ID: 1, Title: "示例"}}}
	fmt.Printf("version=%d tasks=%d\n", f.Version, len(f.Tasks))
}
```

### 6.2 原子写：临时文件、fsync、rename、目录 fsync

设计意图：这是整讲最核心的十几行。目标是任何时刻断电或被 kill，磁盘上的 `todo.json` 要么是完整的旧版、要么是完整的新版，绝不会是半截。关键取舍有四层：临时文件必须与目标**同目录**（否则跨文件系统 rename 不是原子操作、会退化为拷贝）；写完必须 `Sync()`（否则数据还在页缓存里，断电就丢）；rename 之后还要 `Sync()` 目录项（否则文件名这个元数据没落盘，可能出现内容在、名字丢）；全程用 `defer os.Remove` 清理残留临时文件。代价是每次写两次 fsync，P99 落在十几毫秒，这正是第 12 节要量化的成本。

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type File struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// atomicWrite 把 f 序列化后经临时文件原子替换到 path，全程 fsync 保证落盘。
func atomicWrite(path string, f File) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".todo-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("写临时文件: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync 临时文件: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("原子替换: %w", err)
	}
	// 目录项也要 fsync，否则断电后可能文件内容在但名字没落盘。
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("打开目录: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync 目录: %w", err)
	}
	return nil
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "todo.json")
	f := File{Version: 2, Tasks: []Task{{ID: 1, Title: "写第 103 讲"}}}
	if err := atomicWrite(p, f); err != nil {
		fmt.Fprintln(os.Stderr, "写失败:", err)
		return
	}
	fmt.Println("已原子写入", len(f.Tasks), "条任务", time.Now().Format("15:04:05"))
}
```

<LineByLine code="序列化到内存字节\n同目录创建临时文件并写入\nfsync 临时文件确保数据落盘\nrename 原子覆盖正式文件\nfsync 目录项确保名字落盘\ndefer 清理残留临时文件" :notes='["MarshalIndent 生成可读 JSON，失败直接返回不碰磁盘","CreateTemp 用同目录保证 rename 原子，随机名避免多进程撞车","Sync 把页缓存刷到介质，这是 RPO 等于 0 的关键","同目录 rename 是原子操作，读者只看到旧版或新版","目录 Sync 补上文件元数据这最后一块持久化","无论成功失败都清掉临时文件，不留垃圾"]' title="原子写六步逐行拆解" />

<Callout type="warn" title="最容易漏的一步" text="只做 tmp 加 rename、不 fsync，掉电照样丢数据；rename 后不 fsync 目录，极端情况下文件名会丢。教科书常写前两步，生产里第三步第四步才决定 RPO 是不是真的 0。" />

### 6.3 文件锁：把跨进程读改写串行化

设计意图：进程内的 `sync.Mutex` 只能挡住同一进程的 goroutine，挡不住 shell 里同时启动的两个 `todo` 进程。用 `syscall.Flock` 的 advisory lock 才覆盖多进程。关键取舍是用 `LOCK_EX|LOCK_NB` 非阻塞轮询加超时预算，而不是阻塞式 `LOCK_EX`——后者遇到僵死持锁者会永远挂住；再配一个 `WithLock` 高阶函数把 Acquire 与 Release 用 defer 绑死，保证 panic 路径也解锁。代价是 Unix 系限定，且 NFS 上语义不可靠，这在第 1 节假设里已声明。

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

var ErrLockTimeout = errors.New("获取文件锁超时")

// FileLock 用 advisory lock 把读改写跨进程串行化。
type FileLock struct {
	path string
	f    *os.File
}

func (l *FileLock) Acquire(timeout time.Duration) error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("打开锁文件: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			l.f = f
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			f.Close()
			return fmt.Errorf("加锁失败: %w", err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return ErrLockTimeout
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *FileLock) Release() {
	if l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

// WithLock 把临界区包在获取与释放之间，保证任何路径都解锁。
func WithLock(path string, timeout time.Duration, fn func() error) error {
	l := &FileLock{path: path}
	if err := l.Acquire(timeout); err != nil {
		return err
	}
	defer l.Release()
	return fn()
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	lock := dir + "/todo.lock"
	err := WithLock(lock, 500*time.Millisecond, func() error {
		fmt.Println("临界区: 独占读改写 todo.json")
		return nil
	})
	fmt.Println("结果:", err)
}
```

### 6.4 命令树：从手写分发到 cobra

设计意图：原文用标准库手写了一棵按首参数分发、每个子命令持独立 FlagSet 的树，那是理解本质的好办法；生产里应当直接上 cobra，因为它白送帮助生成、参数校验器、shell 补全、PersistentPreRun 钩子。下面这段是引入 cobra 后的完整命令树形态。关键取舍是打开 `SilenceUsage` 与 `SilenceErrors`：运行期错误不该把一大段用法刷进脚本日志，错误由 main 统一写 stderr 并定退出码。代价是多一个第三方依赖，发布时要靠 go.mod 锁版本。

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var cfgFile string
	var asJSON bool
	root := &cobra.Command{
		Use:           "todo",
		Short:         "本地待办清单",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "以 JSON 输出结果")

	add := &cobra.Command{
		Use:   "add [标题]",
		Short: "新增一条待办",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("新增:", args[0])
			return nil
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "列出待办",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("列出全部待办, json =", asJSON)
			return nil
		},
	}
	done := &cobra.Command{
		Use:   "done [id]",
		Short: "标记完成",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("完成:", args[0])
			return nil
		},
	}
	root.AddCommand(add, list, done)
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
```

<CodeTabs title="命令树的两种形态" :tabs='[{"label":"标准库手写","code":"switch os.Args[1]\ncase add: fs.Parse\ncase list: 打印\ncase done: 校验 id","note":"零依赖，暴露按名分发与独立 FlagSet 的本质，但要自己写帮助与校验"},{"label":"cobra","code":"root.AddCommand(add, list, done)\nArgs: cobra.ExactArgs(1)\nPersistentPreRunE 加载配置","note":"白送帮助、补全、校验器与钩子，生产首选，代价是多一个依赖"}]' />

### 6.5 配置优先级：flag 压过 env 压过 config

设计意图：同一个人手敲、cron、CI 三种调用方，靠的是不同方式传配置。把优先级定成「默认值 → 配置文件 → 环境变量 → 命令行 flag」，且后一级只覆盖**显式设置**的键，脚本就能用 `--data-file` 临时改路径而不必改全局配置。关键取舍是把每一级做成独立的纯函数，逐级叠加，测试能对任意一级单独断言；再加一个 `expandHome` 处理波浪号，避免把字面的 `~` 当目录名创建出一堆怪文件。

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DataFile string `json:"data_file"`
	LockFile string `json:"lock_file"`
	Priority int    `json:"default_priority"`
	TTLHours int    `json:"ttl_hours"`
}

// defaults 是优先级最低的一层。
func defaults() Config {
	return Config{Priority: 1, TTLHours: 720}
}

// loadFile 叠加第二层：配置文件；文件不存在视为合法。
func loadFile(c Config, path string) (Config, error) {
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("读配置: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("解析配置: %w", err)
	}
	return c, nil
}

// applyEnv 叠加第三层：环境变量，只覆盖显式设置的键。
func applyEnv(c Config) (Config, error) {
	if v := os.Getenv("TDL_DATA_FILE"); v != "" {
		c.DataFile = v
	}
	if v := os.Getenv("TDL_PRIORITY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("TDL_PRIORITY 非法: %w", err)
		}
		c.Priority = n
	}
	return c, nil
}

// applyFlag 叠加最高层：命令行 flag，空串与负数表示未设置。
func applyFlag(c Config, dataFile string, priority int) Config {
	if dataFile != "" {
		c.DataFile = dataFile
	}
	if priority >= 0 {
		c.Priority = priority
	}
	return c
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}

func main() {
	c, err := loadFile(defaults(), "")
	if err == nil {
		c, err = applyEnv(c)
	}
	if err == nil {
		c = applyFlag(c, "~/.todo/todo.json", -1)
		c.DataFile = expandHome(c.DataFile)
		fmt.Printf("data=%s priority=%d ttl=%dh\n", c.DataFile, c.Priority, c.TTLHours)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
```

### 6.6 退出码与 stderr 约定

设计意图：命令行工具的对外契约一半是 stdout，另一半是退出码。把错误分成「用户输错」「目标不存在」「被占用」「系统故障」四类，分别给 2、3、4、1，脚本就能精确决策：2 改参数重来、3 说明目标已被删、4 退避后重试、1 该报警人工看。关键取舍是 `fail` 只往 stderr 写、数据只往 stdout 写，两者绝不混用；代价是每处返回错误都要正确包装成对应哨兵，漏一个就退化成笼统的 1。

```go
package main

import (
	"errors"
	"fmt"
	"os"
)

var ErrUsage = errors.New("参数错误")
var ErrNotFound = errors.New("任务不存在")
var ErrCorrupted = errors.New("数据文件损坏")
var ErrBusy = errors.New("文件锁超时")

const (
	ExitOK       = 0
	ExitInternal = 1
	ExitUsage    = 2
	ExitNotFound = 3
	ExitBusy     = 4
)

// exitCode 把错误类别映射到稳定的退出码，脚本据此决定重试还是报错。
func exitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrUsage):
		return ExitUsage
	case errors.Is(err, ErrNotFound):
		return ExitNotFound
	case errors.Is(err, ErrBusy):
		return ExitBusy
	default:
		return ExitInternal
	}
}

// fail 统一把人类可读的错误写到 stderr，绝不污染 stdout 的数据流。
func fail(err error) {
	fmt.Fprintf(os.Stderr, "todo: %v\n", err)
}

func complete(id int) error {
	if id <= 0 {
		return fmt.Errorf("%w: id 必须为正整数", ErrUsage)
	}
	if id > 100000 {
		return ErrNotFound
	}
	return nil
}

func main() {
	for _, id := range []int{0, 7, 999999} {
		err := complete(id)
		code := exitCode(err)
		if err != nil {
			fail(err)
		}
		fmt.Printf("id=%d -> exit=%d\n", id, code)
	}
}
```

<OutputBox title="退出码契约演示输出" output="todo: 参数错误: id 必须为正整数\nid=0 -> exit=2\nid=7 -> exit=0\ntodo: 任务不存在\nid=999999 -> exit=3" note="错误行进 stderr，判定行进 stdout，脚本可分别捕获" />

### 6.7 版本迁移、TTL 归档、备份与损坏恢复

设计意图：工具会活很久，格式一定会变。这段把四件运维级的事收在一处：`load` 先探测 version，v1 裸数组自动升级为 v2 包络；`archive` 按 TTL 把已完成的老任务移出主文件；`backup` 写带时间戳的快照；损坏时 `load` 返回 `ErrCorrupted`，上层据此从最近快照恢复。关键取舍是迁移只在读取时做、写入时统一按 v2，避免同时维护两套写路径；TTL 边界用「大于」而非「大于等于」，正好等于阈值的任务留到下一轮，语义更可预期。

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

type FileV2 struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// migrateV1 把旧版裸数组升级为带版本的包络。
func migrateV1(raw []byte) (*FileV2, error) {
	var old []Task
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, fmt.Errorf("v1 解析失败: %w", err)
	}
	return &FileV2{Version: 2, Tasks: old}, nil
}

// load 按 version 选择解析路径，兼容 v1 与 v2 两种文件。
func load(path string) (*FileV2, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &probe); err == nil && probe.Version >= 2 {
		var f FileV2
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("v2 解析失败: %w", err)
		}
		return &f, nil
	}
	return migrateV1(b)
}

// archive 把超过 TTL 且已完成的任务移出主文件，返回归档条数。
func archive(f *FileV2, ttl time.Duration, now time.Time) int {
	kept := make([]Task, 0, len(f.Tasks))
	moved := 0
	for _, t := range f.Tasks {
		if t.Done && now.Sub(t.CreatedAt) > ttl {
			moved++
			continue
		}
		kept = append(kept, t)
	}
	f.Tasks = kept
	return moved
}

// backup 写一份带时间戳的快照，供损坏时回滚。
func backup(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("todo-%s.json.bak", time.Now().Format("20060102-150405"))
	return os.WriteFile(filepath.Join(filepath.Dir(path), name), b, 0o600)
}

func main() {
	dir, _ := os.MkdirTemp("", "todo-")
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "todo.json")
	raw := `[{"id":1,"title":"旧任务","done":true,"created_at":"2020-01-01T00:00:00Z"}]`
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if err := backup(p); err != nil {
		fmt.Fprintln(os.Stderr, "备份失败:", err)
		return
	}
	f, err := load(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载失败:", err)
		return
	}
	n := archive(f, 720*time.Hour, time.Now())
	fmt.Printf("迁移后 version=%d 归档=%d 剩余=%d\n", f.Version, n, len(f.Tasks))
}
```

<Callout type="tip" title="迁移的灰度顺序" text="先发布能读 v1 也能读 v2 的版本，跑一段时间确认没有回退需求，再让写入固定产出 v2；任何时刻都保留把旧文件重命名回来的回滚路径，不要一上线就删掉兼容分支。" />

## 7. 失败路径与边界条件

把故障点逐条摊开，每条都要能回答五件事：哪个组件、什么现象、怎么发现、怎么兜底、怎么恢复。这张表是做设计评审时最该逐行念的一张，因为它逼着人承认「写文件会失败、锁会拿不到、文件会坏」。

| 故障点 | 触发条件 | 现象 | 如何发现 | 兜底 | 恢复动作 |
| --- | --- | --- | --- | --- | --- |
| 锁被占用 | 另一进程持有超过 500ms | 命令挂起后失败 | 退出码 4 加日志 | 非阻塞轮询加超时 | 稍后重试，僵死进程由内核释放锁 |
| 磁盘写满 | 临时文件写一半 | Write 报错 | 退出码 1 加 stderr | 不 rename，旧文件完好 | 清空间后重跑，临时文件被 defer 清理 |
| 数据损坏 | 手工编辑或位翻转 | load 解析失败 | 退出码 1 加 ErrCorrupted | 拒绝写入，别把坏数据覆盖回去 | 从最近时间戳备份恢复 |
| 进程崩溃 | 写到一半被 kill -9 | 下次读取 | 校验和或解析探测 | rename 前旧版完整，锁由内核释放 | 直接重跑命令，无需人工 |
| 重复请求 | 脚本重试 add | 可能多出重复任务 | 任务数异常 | 幂等键或先查后加 | 按业务键去重，done 天然幂等 |
| 配置错误 | 路径拼错、env 非整数 | 启动即失败 | 可读报错 | 转型失败拒绝启动 | 修正后重跑，不静默吞掉 |
| 时钟回拨 | NTP 校正 | TTL 归档判断偏移 | created_at 早于写入时刻 | 归档用单调比较并留宽限 | 以服务器时间重算，必要时人工核对 |

三个横切机制要贯穿全表。其一是重试预算：锁获取只在 500 毫秒内轮询，绝不无限重试；写失败不自动重试，把决定权交给调用方脚本，避免放大成写风暴。其二是超时传递：从命令入口到文件锁到落盘，超时预算逐级收紧，任何一层都不会比上一层等得更久。其三是人工介入点明确标在「数据损坏」一行——这是唯一不能靠自动兜底解决、必须靠备份回滚的故障，其余都可在无人值守下重跑恢复。

幂等性按命令分别处理：`done` 对同一 ID 重复执行结果一致，`delete` 对已删 ID 返回 `ErrNotFound` 而非崩溃，`add` 无法天然幂等，因此给脚本的建议是「先 list 查业务键再决定是否 add」，或引入可选的 client token 落进任务字段。这些判断都放在领域层，命令层不重复实现。

<Callout type="warn" title="别把坏数据写回去" text="load 失败时最危险的动作是「当空文件重建并写回」，那会把还能从备份抢救的内容彻底覆盖。正确姿势是拒绝写入、报错退出，把修复交给显式的恢复命令。" />

## 8. 并发与一致性

并发模型分两层：进程内用 `sync.Mutex` 保护共享内存态与 `List` 返回副本，进程间用 `flock` 保护整个文件事务。之所以两层都要，是因为本讲既有单进程内多 goroutine 的批量操作，也有多进程同时跑的真实场景；只做进程内锁，shell 里开两个进程就会互相覆盖。锁粒度选「整个读改写事务一把独占锁」而不是按任务行加锁：单文件 JSON 无法就地更新某一行，任何改写都要全量序列化，细粒度锁没有意义。

竞态分析要落到具体代码：最常见的两个竞态是「读改写不原子」——两个进程都读到 5 条、各自加一条、都写回 6 条，结果丢一条；以及「按数组长度分配 ID」——删掉中间一条后 `len+1` 与历史最大值撞号。前者靠锁解决，后者靠 `maxID+1` 这个纯函数解决，两者缺一不可。一致性级别选线性一致的依据很直接：既然写事务已被串行化，读也放在同一把锁内，任何读到的都是某次成功写入的完整快照，无需再引入版本号或 MVCC 去处理更复杂的一致性问题。

顺序保证体现在 ID 单调：因为分配发生在锁内、且总是取当前最大值加一，ID 全局单调、删除后不复用，归档与列表展示的稳定顺序天然成立。乱序只在一种情况下出现——外部脚本把旧备份拷回来覆盖新文件，这属于运维事故，靠备份文件的时间戳命名和「恢复前先备份当前」的操作规程约束，而不是靠代码。

<Compare title="并发控制的两种粒度" :panels='[{"label":"细粒度行锁","code":"锁住单个 id 再改写\n需要随机读写的文件格式","note":"JSON 全量序列化下无意义，改动一处也要重写整文件，徒增复杂度"},{"label":"事务级独占锁","code":"取锁 读 改 写 放锁\n一次命令一把锁走完","note":"本讲采用，简单且线性一致，代价是并发写被压成串行"}]' />

<ConceptDiagram type="lock" title="双层锁与事务边界" caption="进程内 mutex 保护内存态，进程间 flock 保护文件事务，临界区连续不可打断" />

<Callout type="idea" title="List 一定要返回副本" text="若 List 直接返回内部切片，调用方一句赋值就能绕过锁改到内部状态，所有并发保证瞬间失效。深拷贝这几次分配，是买整条一致性契约最便宜的保费。" />

## 9. 可观测性

单机 CLI 没有常驻进程和仪表盘，但可观测性照样要设计，否则「偶尔变慢」「偶尔失败」永远查不清。做法是三件套：每次命令生成一个 run_id 贯穿日志、把关键耗时与结果落成结构化日志（默认写 stderr 或 `~/.todo/log`，绝不污染 stdout 数据）、用 `--verbose` 打开细节。指标虽不常驻导出，但可以在日志里按固定字段统计。

| 名称 | 类型 | 含义 | 告警阈值 |
| --- | --- | --- | --- |
| write_latency_ms | Histogram | 单次原子写耗时 | P99 大于 50 毫秒 |
| fsync_fail_total | Counter | fsync 失败次数 | 大于 0 即告警 |
| lock_wait_ms | Histogram | 获取文件锁等待 | 大于 500 毫秒 |
| lock_timeout_total | Counter | 锁超时次数 | 5 分钟内大于 3 |
| corrupted_recover_total | Counter | 从备份恢复次数 | 大于 0 即告警 |
| task_count | Gauge | 主文件任务数 | 大于 2000 提示换存储 |

结构化日志字段固定为 `ts`、`run_id`、`cmd`、`exit_code`、`dur_ms`、`err`，一行一个 JSON 对象，便于用 `jq` 或日志采集器离线聚合。`run_id` 在命令入口生成，随错误一路透传；因为 CLI 没有跨进程 trace，就用「run_id 加父进程 PID」把一次脚本批量操作里的多条命令串起来，效果等价于轻量 trace_id。埋点位置精挑三处：进入编排器、拿到锁、完成落盘，分别记录开始、锁等待、写耗时。

```bash
# 结构化日志示例：失败时也能一眼看清是哪个命令、哪个环节、耗时多少
todo add "写第 103 讲" --verbose 2> >(jq -c '{ts,run_id,cmd,exit_code,dur_ms,err}')
```

用它定位一次真实故障：用户报「今天 add 偶尔要卡一秒」。先按 `cmd=add` 过滤，看 `lock_wait_ms` 是否接近甚至达到 500 毫秒——若吻合，说明是另一个进程（大概率是 cron 的 archive）长时间持锁，而不是磁盘慢；再看 `write_latency_ms` 是否同步升高，若没有，就排除 fsync 瓶颈；最后用 run_id 把同一时刻的 archive 日志拉出来对齐时间线，确认两者重叠。整个过程不需要登录机器实时抓现场，靠离线日志就能闭环。这也是 CLI 的 UX 与可测试性的一部分：`--json` 给机器稳定数据、`--verbose` 给排查留证据、`--quiet` 给脚本省噪声，三种模式共用同一套内部逻辑，靠输出视图切换而非改业务逻辑。

## 10. 测试策略

分层按「越靠下越纯、越靠上越真」铺开。最底层是纯函数单测：领域校验、maxID 分配、退出码映射、配置四级合并，全部表驱动、零 IO。中间层是存储集成测试：用 `t.TempDir` 造真实文件，覆盖首次创建、损坏文件拒绝、v1 迁移、删后增 ID 不复用、写入中途失败旧文件完好。上层是契约测试：断言 `--json` 输出可被解析、退出码与错误类别一一对应。再往上是故障注入：把 `Store` 换成会返回磁盘满错误的 fake，验证编排器不写坏文件；把锁实现换成永远超时的 fake，验证返回退出码 4。最上层是并发与基准：百个 goroutine 同时 Add 跑 `go test -race`，以及 Benchmark 塞满 2000 条观察全量序列化的增长曲线。

下面这段是自包含的测试文件，直接 `go test -race` 可跑，覆盖退出码表驱动与并发 ID 唯一性两个最关键的分支。

```go
package main

import (
	"errors"
	"sync"
	"testing"
)

var errUsage = errors.New("usage")

func code(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errUsage) {
		return 2
	}
	return 1
}

type memStore struct {
	mu    sync.Mutex
	items []int
	next  int
}

func (m *memStore) Add() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.items = append(m.items, m.next)
	return m.next
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errUsage, 2},
		{errors.New("io"), 1},
	}
	for _, c := range cases {
		if got := code(c.err); got != c.want {
			t.Errorf("code(%v)=%d, want %d", c.err, got, c.want)
		}
	}
}

func TestConcurrentAddUniqueID(t *testing.T) {
	m := &memStore{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Add()
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for _, id := range m.items {
		if seen[id] {
			t.Fatalf("重复 ID %d, 说明 maxID 加一未加锁", id)
		}
		seen[id] = true
	}
	if len(seen) != 100 {
		t.Fatalf("期望 100 个唯一 ID, 实际 %d", len(seen))
	}
}
```

mock 边界切在 `Store` 接口，理由回到第 3 节：它是唯一挡住 IO 的接缝。凡是验证「编排逻辑是否正确」的测试都注入 fakeStore，跑得快、结果确定；凡是验证「文件到底写对没有」的测试才用真临时目录，数量刻意压少。契约测试不 mock 退出码映射，因为那是脚本依赖的对外承诺，必须测真实实现。故障注入专门覆盖那些平时走不到的分支：fsync 失败、rename 失败、锁超时、备份恢复，这些恰恰是第 7 节失败矩阵里最难靠手工复现的行。

## 11. 部署与容量

部署形态极简：一个静态二进制加两个文件（数据文件、锁文件），没有服务、没有端口、没有守护进程。装到 `~/.local/bin` 或任意 PATH 目录即可。依赖只有 Go 运行时（编译后为零）、生产版额外一个 cobra，版本在 go.mod 里锁死。配置项表把默认值与环境差异写清，避免「开发机能跑、cron 里跑不起来」这类经典问题——最常见根因就是 cron 没有 HOME 环境变量，导致波浪号路径解析到错误位置。

| 配置项 | 默认值 | 说明与环境差异 |
| --- | --- | --- |
| data_file | ~/.todo/todo.json | cron 环境需显式给绝对路径 |
| lock_file | ~/.todo/todo.lock | 必须与数据文件同机、非 NFS |
| default_priority | 1 | 可被 TDL_PRIORITY 覆盖 |
| ttl_hours | 720 | 归档阈值，30 天 |
| lock_timeout_ms | 500 | 慢盘可适当放大 |
| log_level | warn | --verbose 时临时降到 debug |

资源估算要自洽：单条任务序列化后约 120 字节，2000 条约 240 KB，全量读进内存峰值不到 1 MB；每次写临时文件与正式文件短暂共存，磁盘峰值约 500 KB；锁是一个空文件，几乎不占空间；无网络带宽。也就是说容量瓶颈根本不在内存或磁盘，而在每次写两次 fsync 的固定延迟——这决定了扩容方式不是加机器，而是「减少写次数」，例如把批量 add 合并成一次落盘。这属于单机纵向优化，没有横向扩容维度。

发布走两条路。开发期最省事的是 `go install`，它会把指定版本编译并装进 GOPATH 的 bin；正式发版用 goreleaser 一次产出多平台压缩包与校验和，配合 tag 触发。

```bash
# 开发期：直接从模块安装到 PATH
go install github.com/yourname/todo@latest
todo --version
```

```yaml
# .goreleaser.yaml：打 tag 后一次产出多平台产物与校验和
project_name: todo
builds:
  - main: .
    binary: todo
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
archives:
  - format: tar.gz
checksum:
  name_template: checksums.txt
```

优雅退出对 CLI 而言主要是「别在信号处理到一半时写坏文件」：捕获 SIGINT/SIGTERM 后，正在进行的写事务要么让它跑完，要么整体放弃（因为已经有原子写保护，放弃也是安全的）；绝不注册一个在临界区中间触发 os.Exit 的处理器。锁文件用独立 fd，进程一退内核自动释放，不留死锁。

## 12. 性能与压测

压测目的是量出「全量序列化 + 双 fsync」这条路径的真实天花板，从而给第 13 节的「何时换存储」一个可执行的触发条件，而不是拍脑袋。方法是造不同规模的数据文件，各跑固定次数的 add，记录耗时分布与写放大。

```bash
# 造 2000 条基线数据，再压 200 次写，统计 P50/P99
for i in $(seq 1 2000); do todo add "task-$i" --quiet; done
go test -bench=BenchmarkAdd -benchmem -run=^$ ./...
todo add "probe" --verbose 2>&1 | jq '.dur_ms'
```

| 规模 | 吞吐（写/秒） | P50 | P99 | 错误率 | 内存峰值 |
| --- | --- | --- | --- | --- | --- |
| 100 条 | 约 90 | 9 毫秒 | 13 毫秒 | 0 | 0.1 MB |
| 1000 条 | 约 65 | 13 毫秒 | 19 毫秒 | 0 | 0.5 MB |
| 2000 条 | 约 45 | 20 毫秒 | 31 毫秒 | 0 | 1.0 MB |
| 5000 条 | 约 18 | 52 毫秒 | 88 毫秒 | 0 | 2.6 MB |

瓶颈定位过程很有代表性：用 `go test -bench` 加 pprof 的 CPU profile 一看，`json.MarshalIndent` 与两次 `Sync` 占了绝大部分时间，且前者随条数线性增长——因为每次 add 都要把整个 `[]Task` 重新序列化一遍，写放大是 O(n)。这解释了为什么 2000 条之后 P99 明显抬头，也解释了为什么「加机器」没用。优化前后对比：把批量 add 合并成一次读改写（一次序列化、一次 fsync），2000 条规模下 200 次写能从约 4.4 秒降到约 0.3 秒；再把 `MarshalIndent` 换成 `Marshal`（发布后可读性由备份文件保证），还能再省一点 CPU。但仍然存在的硬限制是单文件全量读写，条数继续涨就只能换存储引擎，优化到此为止。

## 13. 演进与替代方案

任何架构都有适用边界，写清楚「什么时候该换」比论证「现在这样最好」更有价值。下面三套方案按规模递进，取舍列到代价，避免把迁移当成纯粹的升级。

| 方案 | 适用规模 | 优点 | 代价 | 何时该换 |
| --- | --- | --- | --- | --- |
| 单文件 JSON 原子写 | ≤ 2000 条、≤ 4 进程 | 零依赖、可读、易备份 | 全量序列化 O(n)、写并发串行 | 当前方案，P99 超 50 毫秒或条数破 2000 |
| 嵌入式 SQLite | ≤ 百万条、多进程 | 事务、索引、增量写、成熟 | 引 CGO 或纯 Go 驱动、二进制变大、文件不再肉眼可读 | 需要按条件查询、并发写变多、要事务 |
| 本地服务加客户端 | 多机、多用户 | 集中一致、可加鉴权与同步 | 要常驻进程、部署与运维成本陡增 | 出现跨设备协作或网络访问需求 |

从当前版本演进到 SQLite 的触发条件是明确的：主文件稳定超过 2000 条、或写 P99 持续高于 50 毫秒、或开始需要「按优先级/标签过滤」这类查询。迁移步骤刻意做成可回滚的四步：第一步导出，用现有 `list --json` 把全部任务写成一份快照并校验条数；第二步双写，新版本同时写 SQLite 与旧 JSON，读取仍走 SQLite；第三步观察，跑一周确认读写与归档都正常、条数对齐；第四步停用，删除旧 JSON 写入路径，但把导出快照保留至少一个月。回滚点就在第三步之前——一旦新引擎出问题，把读取切回旧 JSON、用快照重建即可，因为整个过程旧文件始终没被破坏。这个「先加新、再切读、最后停旧」的顺序，和数据库在线迁移是同一套思路，只是规模小得多。

## 14. 真实工程里的坑

以下每条都来自生产里真会遇到的场景，按「现象 → 原因 → 错误写法或正确写法 → 如何提前发现」组织。

**坑一：删后增 ID 撞号。** 现象是删除中间某条再新增，列表里出现两个相同 ID，done 命令改错对象。原因是按 `len(tasks)+1` 分配，删一条后长度回退。正确写法是遍历取 `maxID+1`，如 6.2 与第 4 节接口实现里那样。提前发现靠一条「删中间、再加、断言 ID 唯一」的表驱动测试。

**坑二：直接覆盖写留下半截文件。** 现象是写入途中被 kill，下次 list 报解析错误，数据全没了。原因是 `os.WriteFile(path, ...)` 就地截断重写，没有任何原子性。正确写法是同目录临时文件加 rename（6.2）。提前发现靠故障注入测试：在 Write 一半时发信号，再断言文件仍可解析。

**坑三：只 rename 不 fsync。** 现象是机器掉电后，明明返回成功的写入不见了。原因是数据还在页缓存，没刷到介质。正确写法是临时文件 `Sync()` 加 rename 后目录 `Sync()`。提前发现靠第 9 节的 `fsync_fail_total` 与断电演练，代码评审时专门查有没有这两个调用。

**坑四：阻塞式 flock 把命令挂死。** 现象是某个进程僵死后，后续所有 todo 命令永久卡住。原因是用了不带 `LOCK_NB` 的阻塞加锁。正确写法是 6.3 的非阻塞轮询加 500 毫秒预算，超时退 4。提前发现靠注入一个「持锁不放到超时」的测试进程，断言主命令 500 毫秒内返回。

**坑五：List 返回内部切片绕过锁。** 现象是偶发的计数不一致、`go test -race` 报数据竞争。原因是把内部 `[]Task` 直接返回，调用方改动穿透进来。正确写法是 make 加 copy 返回副本。提前发现靠并发测试配 `-race`，把竞争在 CI 里变成红灯。

**坑六：退出码一律返回 1。** 现象是脚本把「参数写错」也当成系统故障无限重试，日志被刷爆。原因是没有对错误分类。正确写法是 6.6 的哨兵错误加分级退出码。提前发现靠契约测试，逐条断言每个错误类别对应的退出码。

**坑七：配置文件里的波浪号没展开。** 现象是开发机正常，cron 里却生成了一个名字带波浪号的怪目录。原因是 `os.ReadFile("~/.todo/config")` 不会自动展开，且 cron 往往没有 HOME。正确写法是 6.5 的 `expandHome`，并建议无人值守场景一律用绝对路径。提前发现靠在测试里清空 HOME 再跑一次配置加载。

**坑八：把错误文案刷进 stdout。** 现象是 `todo list --json | jq` 偶尔报解析错误，因为某个 warn 混进了数据流。原因是错误与数据共用一个输出流。正确写法是数据只进 stdout、所有诊断只进 stderr（6.6 的 `fail`）。提前发现靠契约测试：故意触发一次失败，断言 stdout 仍为空或仍是合法 JSON。

<Callout type="idea" title="坑的共同形状" text="八条里有六条不是功能没实现，而是边界没想全：崩溃、并发、掉电、空环境、错误分类、输出分离。把第 7 节失败矩阵逐行写成测试，这些坑基本都会提前浮出来。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l10-projects/lesson-103-cli-todo`](https://github.com/lfire/go/tree/main/examples/l10-projects/lesson-103-cli-todo)。clone 仓库后进入 `examples/l10-projects`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

在本讲架构上做一次真实改动：给工具加一个 `archive` 子命令，把已完成且创建时间超过 TTL 的任务移出主文件，返回归档条数，并保证归档失败时主文件完好。要求复用现有的原子写与文件锁，不改 `Store` 之外的任何命令层代码，并补一条覆盖「正好等于 TTL」边界的表驱动测试。

<Exercise title="实现 TTL 归档子命令" task="新增 archive 命令：读取配置里的 ttl_hours，锁内 load 全部任务，筛选已完成且超期的项写回主文件，输出归档条数；失败时不得破坏原文件。" :hints='["提示1：在 domain 的 Store 接口加 Archive(ttl time.Duration) (int, error)，命令层只调用接口","提示2：复用 WithLock 把读改写包成一次事务，归档结果用 atomicWrite 落盘","提示3：边界用 now.Sub(t.CreatedAt) 大于 ttl，正好等于时保留到下一轮"]' solution="第1步 在 Task 结构确认 CreatedAt 字段存在且带 json 标签 created_at\n第2步 在 Store 接口新增 Archive(ttl time.Duration) (int, error) 方法\n第3步 实现里遍历任务，对 t.Done 为真且 now.Sub(t.CreatedAt) 大于 ttl 的项计数并剔除\n第4步 用 WithLock 包住 load 与 atomicWrite，保证归档事务不被其它进程插入\n第5步 新增 cobra 子命令读取 ttl_hours，转成 time.Duration 调用 Archive，输出归档条数\n第6步 补表驱动测试覆盖超期、未超期、正好等于 TTL 三种输入\n完整代码见下方参考实现块" explain="关键不在筛选逻辑，而在把归档做成和 add 一样的锁内事务，并复用原子写；这样失败时旧文件完好，也顺带复用了备份与恢复链路。" />

参考实现的核心函数如下，可直接并入 `store/json.go`：

```go
package main

import (
	"fmt"
	"time"
)

type Task struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}

// archiveTasks 返回归档条数与保留下来的任务；正好等于 ttl 的不归档。
func archiveTasks(all []Task, ttl time.Duration, now time.Time) (int, []Task) {
	kept := make([]Task, 0, len(all))
	moved := 0
	for _, t := range all {
		if t.Done && now.Sub(t.CreatedAt) > ttl {
			moved++
			continue
		}
		kept = append(kept, t)
	}
	return moved, kept
}

func main() {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	all := []Task{
		{ID: 1, Done: true, CreatedAt: now.Add(-31 * 24 * time.Hour)},
		{ID: 2, Done: true, CreatedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: 3, Done: false, CreatedAt: now.Add(-40 * 24 * time.Hour)},
	}
	n, kept := archiveTasks(all, 30*24*time.Hour, now)
	fmt.Printf("归档=%d 保留=%d\n", n, len(kept))
}
```

## 随堂检测

<Quiz no="103-1" q="某次写入临时文件已 Sync 成功，但还没来得及 rename 就被 kill -9，此时磁盘上的 todo.json 是什么状态？" :options='["新版完整内容，因为临时文件已刷盘","半截 JSON，下次读取会报解析错误","旧版完整内容，读者看不到任何中间状态","空文件，因为 rename 会先清空目标"]' answer="2" explain="rename 是切换可见文件名的原子点，它没发生之前，正式文件始终指向旧版完整内容；临时文件被 defer 清理。这正是 6.2 原子写要保证的性质，也是 RPO 等于 0 的基础。" />

<Quiz no="103-2" q="cron 与终端同时跑 todo add 时，仅用进程内 sync.Mutex 够不够？为什么？" :options='["够，Mutex 会跨进程生效","不够，Mutex 只挡同进程 goroutine，跨进程要靠 flock","够，因为 JSON 文件本身有锁","不够，应该改成读写分离而不是加锁"]' answer="1" explain="两个独立进程各有各的地址空间，sync.Mutex 互不可见；必须用 6.3 的 syscall.Flock 这种内核级 advisory lock，才能把跨进程的读改写串行化。这是第 8 节双层锁的由来。" />

<Predict no="103-3" q="已完成任务恰好创建于 30 天前，ttl 配置为 30 天，运行 archive 后这条任务会怎样？" :options='["被归档移出主文件","仍留在主文件","报错拒绝执行"]' answer="1" output="归档=0 保留=1" explain="6.7 的 archive 用 now.Sub(t.CreatedAt) 大于 ttl 的严格大于判断，正好等于阈值时保留到下一轮，避免边界抖动导致同一批任务反复被扫到。参考实现里第二条边界用例正是这个场景。" />

<FillBlank no="103-4" tpl="生产里遇到僵死进程持锁，正确的加锁方式是 ___ 轮询加超时预算，超时后以退出码 ___ 结束，让脚本退避重试。" :answers='["LOCK_NB~非阻塞","4"]' hint="回想 6.3 的加锁标志位与 6.6 的退出码表。" explain="非阻塞加超时避免永久挂起；退出码 4 专表被占用，与输错、不存在、系统故障区分开，脚本才能精确决策。" :explains='["阻塞式加锁碰到僵死进程占着的锁会永远挂住，非阻塞标志让抢锁立刻返回成败，才能配合轮询和超时预算，超时即主动放弃。","退出码表把『被占用』单列为 4，脚本据此只对该情形退避重试；参数错、文件缺失、系统故障各有专码，混用会导致误判。"]' />

## 一页纸速记

<KnowledgeCard :points='["三层单向依赖，cmd 与 store 只认 domain 的 Store 接口","临时文件加 fsync 加 rename 加目录 fsync 是崩溃安全的最小完整集","flock 负责跨进程，进程内 Mutex 负责 goroutine，两层都要","分级退出码加 stderr 分离，是脚本能判定成败的对外契约","flag 压过 env 压过 config 压过默认值，后级只覆盖显式键"]' :pitfalls='["按 len 分配 ID 会在删后撞号，要用 maxID 加一","List 返回内部切片会绕过锁，务必返回副本","只 rename 不 fsync，掉电仍丢数据","load 失败时别当空文件写回，那会覆盖掉可恢复内容"]' summary="小项目的价值不在功能多，而在把崩溃、并发、失败判定这三条边界想全并逐条测到。" />

## 小结与下一讲

- 先量化命令数、数据量、并发进程数与 RPO/RTO，再由约束推出单文件 JSON 加原子写加文件锁这套最小架构，而不是先堆技术栈。
- 原子写、双层锁、分级退出码分别解决崩溃安全、并发一致性、脚本可集成三类失败；三源配置与 go install/goreleaser 让工具真正可交付、可发布。
- 用表驱动测试、-race、故障注入与 Benchmark 把失败矩阵逐行钉死，并量出全量序列化的天花板，给「何时换 SQLite」一个可执行的触发条件。

[下一讲：实战 02 · protoc 插件与代码生成器](/projects/cli-codegen)
