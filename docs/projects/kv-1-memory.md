---
title: 第 111 讲 · KV 贯穿线 ① 内存版与接口抽象
description: 承接第 25 讲的内存版 KV，把 Command 与 Storage 定型为单入口契约，为网络层、WAL、Raft 留出扩展点
---

<StageHeader stage="L10 · 工程实战" level="★★☆☆☆" time="55 分钟" goal="定义可演进的存储抽象" prereq="第 25 讲 · 内存版 KV" />

# 第 111 讲 · KV 贯穿线 ① 内存版与接口抽象

> 第 25 讲已经把 L2 的零件装成了一台能跑的小机器：`Storage` 接口定出 `Put/Get/Delete/Keys` 四个动作，`MemStore` 用 `sync.RWMutex` 护住 `map[string]string`，`Command` 把一行文本解析成可记录、可重放的操作，`Apply` 把它落到任意实现，错误用哨兵加 `errors.Is` 判定。那一讲末尾留了六个扩展点，明确指向第 111 到 114 讲。这一讲是贯穿线的正式起点：把这套抽象**定型**成能撑住网络帧、WAL 与 Raft 的形状。为此我们会做一次刻意的接口升级，并给出把第 25 讲 `MemStore` 原样接进来的适配器——升级不是推翻，而是让同一个系统能继续长大。看懂「什么必须稳定、什么允许变化」，是这条贯穿线真正要教的东西。

## 这一讲你会得到什么

<CheckList id="L111-goal" title="本讲目标" :items='["承接第 25 讲，写出 Command 与单入口 Storage 两份贯穿四讲不变的契约","理解 Result 里 Found 字段为什么能消掉一类隐蔽 bug","用读写锁给出内存实现，并用适配器接住第 25 讲的旧实现","用表驱动测试与基准测试给存储层建立可度量的基线"]' />

## 项目目标与约束

先把「上一版遇到了什么问题，所以这一版加了什么」讲清楚。第 25 讲那版作为教学实现是完整且正确的，但进入工程实战后有三处不够用：`Storage` 按操作拆成四个方法，每新增一种操作（CAS、TTL、批量写）都要改接口和全部实现，而网络帧、WAL、Raft 日志更希望只传递「一条命令」；`Command.Op` 是 `string`，适合文本解析却不适合编码进二进制帧，值也只有 `string`；`Get` 靠 error 区分「空值」与「键不存在」，一旦跨网络，错误链容易退化成一段文本。所以本讲做的是**定型加一次刻意升级**：把四个方法收敛成单入口 `Apply(Command)`，把 `Op` 换成显式数值的 `byte`、`Value` 换成 `[]byte`，用 `Result.Found` 补齐空值语义，同时给出适配器，让第 25 讲的 `MemStore` 不改一行就能接进新契约。

本讲的目标与约束如下：

- **目标**：承接第 25 讲并定型一次操作的统一描述 `Command` 与单入口 `Storage` 接口；给出内存实现、兼容适配器、错误模型与可复用的命令解析器。
- **约束**：只用标准库；单进程、微秒级延迟；**不做**持久化、网络、分布式——这三项分别留给第 112、113、114 讲。
- **不变量**：`Command` 的字段、`Op` 的数值、`Storage.Apply` 的语义，四讲之内一律不改。这是整条演进线能连起来的前提。

<Term word="契约" text="接口 + 数据结构 + 错误集合，调用方与实现方共同遵守、长期不改动的一组约定" />

## 架构蓝图

下面这张分层图里，只有「契约层」是稳定的，其余三层都可以被替换——这正是第 25 讲留下的扩展点，也是后续三讲要逐层长出来的地方。

<ConceptDiagram type="architecture-layer" title="KV 系统的分层：命令层 → 契约层 → 存储实现层" caption="网络层、WAL、Raft 都是「实现层」的不同形态，契约层始终不动" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"第 25 讲","desc":"内存版：Storage 与 MemStore","tone":"blue"},
  {"title":"① 内存版","desc":"本讲：定型契约、接住旧实现","tone":"blue"},
  {"title":"② 网络层","desc":"下一讲：TCP 帧与客户端 SDK","tone":"green"},
  {"title":"③ 持久化","desc":"WAL、恢复、锁分片、快照","tone":"orange"},
  {"title":"④ 分布式","desc":"分片、副本、Raft 共识","tone":"red"}
]' />

<Callout type="idea" title="为什么先写接口而不是先写功能" text="接口是依赖的「窄腰」：命令解析、网络收发、日志追加、Raft 复制都只依赖 Command 这一个数据载体，于是新增一层时不必改动任何已有层。这也是第 19 讲「接受接口、返回结构体」在真实项目里的用法。" />

## 关键实现

以下代码同属 `package kv`，分别存为 `contract.go`、`memory.go`、`codec.go`、`legacy_adapter.go`、`memory_test.go`，放在同一目录用 `go test ./...` 即可编译运行。

### 1. 契约：Command 与 Storage

```go
// 文件：contract.go
package kv

import "errors"

// Op 用显式数值定义，因为第 112 讲会把它直接编码进 TCP 帧，
// 第 113、114 讲会把它写进 WAL 与 Raft 日志，数值必须长期稳定。
// 相比第 25 讲用 string 表示 put/get/del，byte 更适合持久化格式。
type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

// 哨兵错误沿用第 25 讲的思路，让调用方用 errors.Is 精确判定，不比对错误文本。
var (
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
)

// Command 是一次读写的统一描述，是整条贯穿线唯一的数据载体：
// 命令解析、网络帧、WAL 记录、Raft 日志都只传递它。
type Command struct {
	Op    Op
	Key   string
	Value []byte
}

// Result 用 Found 把「键不存在」与「值恰好为空」区分开，
// 避免调用方把 nil 值误判成未命中。
type Result struct {
	Value []byte
	Found bool
}

// Storage 是四讲唯一的抽象。Apply 必须是「执行一次命令」的完整语义：
// 可重放、可持久化、可复制到副本，任何一版实现都要满足它。
type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

### 2. 内存实现

```go
// 文件：memory.go
package kv

import "sync"

// 编译期断言：Memory 必须满足契约，接口一改这里立刻报错。
var _ Storage = (*Memory)(nil)

// Memory 是本讲契约的内存实现，沿用第 25 讲 MemStore 的读写锁思路，
// 只是把值换成 []byte 以承载二进制内容。
type Memory struct {
	mu     sync.RWMutex
	data   map[string][]byte
	closed bool
}

func NewMemory() *Memory {
	return &Memory{data: make(map[string][]byte)}
}

func (m *Memory) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	switch cmd.Op {
	case OpGet:
		m.mu.RLock()
		defer m.mu.RUnlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		v, ok := m.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		// 返回副本，避免调用方持有内部切片后并发改写造成数据竞争。
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	case OpSet:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		m.data[cmd.Key] = append([]byte(nil), cmd.Value...)
		return Result{Found: true}, nil
	case OpDel:
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return Result{}, ErrClosed
		}
		_, ok := m.data[cmd.Key]
		delete(m.data, cmd.Key)
		return Result{Found: ok}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
```

### 3. 命令解析与结果格式化

```go
// 文件：codec.go
package kv

import (
	"fmt"
	"strings"
)

// ParseCommand 把一行文本命令解析成 Command，供 CLI 与第 112 讲的网络层复用。
// 形如：GET k / SET k v / DEL k；value 中的空格用 Join 保留。
func ParseCommand(line string) (Command, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Command{}, ErrBadOp
	}
	var cmd Command
	switch strings.ToUpper(fields[0]) {
	case "GET":
		cmd.Op = OpGet
	case "SET":
		cmd.Op = OpSet
	case "DEL":
		cmd.Op = OpDel
	default:
		return Command{}, ErrBadOp
	}
	if len(fields) < 2 {
		return Command{}, ErrEmptyKey
	}
	cmd.Key = fields[1]
	if cmd.Op == OpSet {
		if len(fields) < 3 {
			return Command{}, ErrEmptyKey
		}
		cmd.Value = []byte(strings.Join(fields[2:], " "))
	}
	return cmd, nil
}

// FormatResult 把结果转成一行文本，方便 CLI 打印与网络层回包。
func FormatResult(r Result, err error) string {
	if err != nil {
		return "ERR " + err.Error()
	}
	if !r.Found {
		return "NIL"
	}
	return fmt.Sprintf("OK %s", r.Value)
}
```

### 4. 兼容适配器：把第 25 讲的 MemStore 接进来

```go
// 文件：legacy_adapter.go
package kv

import "errors"

// LegacyStore 是第 25 讲定义的 Storage 形状：按操作拆方法、值为 string。
// 保留这个接口是为了证明本讲不是推翻重来，旧实现可以被平滑接住。
type LegacyStore interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() []string
}

// WrapLegacy 把第 25 讲风格的实现适配成本讲的 Storage。
// legacyNotFound 由调用方传入旧包里的 ErrNotFound 哨兵，
// 适配器把它翻译成本讲的 ErrNotFound，让 errors.Is 在新旧两层都成立。
func WrapLegacy(l LegacyStore, legacyNotFound error) Storage {
	return &legacyAdapter{inner: l, notFound: legacyNotFound}
}

type legacyAdapter struct {
	inner    LegacyStore
	notFound error
}

func (a *legacyAdapter) Apply(cmd Command) (Result, error) {
	switch cmd.Op {
	case OpSet:
		return Result{Found: true}, a.inner.Put(cmd.Key, string(cmd.Value))
	case OpGet:
		v, err := a.inner.Get(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Value: []byte(v), Found: true}, nil
	case OpDel:
		err := a.inner.Delete(cmd.Key)
		if err != nil {
			if a.notFound != nil && errors.Is(err, a.notFound) {
				return Result{}, ErrNotFound
			}
			return Result{}, err
		}
		return Result{Found: true}, nil
	default:
		return Result{}, ErrBadOp
	}
}

func (a *legacyAdapter) Close() error { return nil }
```

`WrapLegacy` 是整条贯穿线最关键的一处承接：第 25 讲的 `MemStore`、`Parse`、`Apply` 全部不用改，包一层就进入了新体系。这也是「接口抽象要能延续」的字面意思——演进允许换形状，但必须给旧实现留一条不返工的路。

### 5. 表驱动测试、适配器验证与基准测试

```go
// 文件：memory_test.go
package kv

import (
	"errors"
	"strconv"
	"testing"
)

func TestMemoryCRUD(t *testing.T) {
	cases := []struct {
		name      string
		cmd       Command
		wantFound bool
		wantVal   string
		wantErr   error
	}{
		{"set", Command{Op: OpSet, Key: "a", Value: []byte("1")}, true, "", nil},
		{"get", Command{Op: OpGet, Key: "a"}, true, "1", nil},
		{"del", Command{Op: OpDel, Key: "a"}, true, "", nil},
		{"miss", Command{Op: OpGet, Key: "a"}, false, "", ErrNotFound},
	}
	s := NewMemory()
	defer s.Close()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := s.Apply(c.cmd)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err=%v want=%v", err, c.wantErr)
			}
			if got.Found != c.wantFound || string(got.Value) != c.wantVal {
				t.Fatalf("got found=%v val=%q", got.Found, got.Value)
			}
		})
	}
}

// tinyLegacy 复刻第 25 讲 MemStore 的方法集，用于验证适配器。
type tinyLegacy struct{ m map[string]string }

func (t *tinyLegacy) Put(k, v string) error { t.m[k] = v; return nil }
func (t *tinyLegacy) Get(k string) (string, error) {
	v, ok := t.m[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (t *tinyLegacy) Delete(k string) error {
	if _, ok := t.m[k]; !ok {
		return ErrNotFound
	}
	delete(t.m, k)
	return nil
}
func (t *tinyLegacy) Keys() []string {
	out := make([]string, 0, len(t.m))
	for k := range t.m {
		out = append(out, k)
	}
	return out
}

func TestWrapLegacy(t *testing.T) {
	legacy := &tinyLegacy{m: make(map[string]string)}
	s := WrapLegacy(legacy, ErrNotFound)
	if _, err := s.Apply(Command{Op: OpSet, Key: "lang", Value: []byte("go")}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Apply(Command{Op: OpGet, Key: "lang"})
	if err != nil || string(got.Value) != "go" {
		t.Fatalf("got=%q err=%v", got.Value, err)
	}
	if _, err := s.Apply(Command{Op: OpGet, Key: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkMemorySet(b *testing.B) {
	s := NewMemory()
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		key := "k" + strconv.Itoa(i%1024)
		if _, err := s.Apply(Command{Op: OpSet, Key: key, Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}
```

## 失败路径与边界条件

| 边界 | 现象 | 本讲的处理 |
| --- | --- | --- |
| 空 key | 把整个命名空间写坏 | 入口直接返回 `ErrEmptyKey` |
| 键不存在 | 返回 nil 还是报错？ | 用 `ErrNotFound` 哨兵错误，`errors.Is` 可判定 |
| 值为 nil 的键 | 容易与「未命中」混淆 | `Result.Found` 单独表达命中与否 |
| 返回内部切片 | 调用方改写引发数据竞争 | 读写两侧都用 `append` 复制一份 |
| 未知 Op / 关闭后写入 | 静默成功会埋雷 | 分别返回 `ErrBadOp` / `ErrClosed` |
| 并发读写 map | 运行时直接 panic | `RWMutex` 保护全部访问 |
| 旧哨兵错误跨包 | `errors.Is` 匹配不到 | `WrapLegacy` 把旧哨兵翻译成本讲 `ErrNotFound` |

<Callout type="warn" title="最容易忽略的一个坑" text="Set 时若直接存 cmd.Value 这个切片，调用方事后改动它就会绕开锁改写内部数据；Get 时若直接返回内部切片同样危险。存储层对进出的 []byte 一律做值拷贝，是这一层最值钱的三行代码。" />

## 深入一步：为什么说接口是「窄腰」

`Storage` 只有一个 `Apply` 方法，这是刻意的。第 25 讲按操作拆成四个方法的写法适合教学，但每加一种操作都要改接口；单入口让「新增操作」退化成「新增一个 Op 常量」。方法越少，能实现它的东西越多：内存 `map`、TCP 客户端、写前日志、Raft 状态机都能塞进同一位置。

<Compare title="从旧契约到新契约" :panels='[
  {"label":"第 25 讲：按操作拆","code":"Put(k,v) / Get(k) / Delete(k) / Keys()","note":"新增 Scan、CAS 就要改接口，四个方法跟着动"},
  {"label":"本讲：统一命令入口","code":"Apply(Command) (Result, error)","note":"新操作只加 Op 常量，网络帧与 WAL 天然复用同一条路径"}
]' />

`WrapLegacy` 是这条思路的第一个例子：第 25 讲的 `MemStore` 不必改，包一层就进入了新体系。命令模式还有一个隐藏好处：`Command` 是纯数据，可以序列化进网络帧、追加进 WAL、作为 Raft 日志复制。四讲里我们只需要换掉 `Apply` 背后的实现，调用方一行都不用动。

<Term word="命令模式" text="把一次操作封装成带类型的数据对象，从而能被排队、记录、重放与远程传输" />

## 测试与压测思路

- **正确性**：`TestMemoryCRUD` 用表驱动覆盖增删改查与未命中；哨兵错误用 `errors.Is` 断言，不比对错误文本。
- **兼容性**：`TestWrapLegacy` 验证第 25 讲风格的实现接入后读写正确、旧哨兵错误能被翻译。
- **并发**：`go test -race` 跑一个多 goroutine 混合读写的用例，确认 `RWMutex` 没有漏保护的路径。
- **基线**：`BenchmarkMemorySet` 用 `ReportAllocs` 记录每次写入的分配数；这个数字是后面三讲的对照基准——加上 WAL、网络、复制之后，延迟与分配各涨多少，都要拿它来比。
- **压测关注点**：单把 `RWMutex` 在写密集时是全局串行点，这正是第 25 讲留的扩展点之一，也是第 113 讲引入锁分片的直接动因。

## 可继续扩展的方向

- 加 `OpCAS`、`OpIncr`、`Scan(prefix)` 等操作：只加 `Op` 常量与 `Apply` 分支，接口不动。
- 加 TTL：在 `Result`/`Command` 里加时间戳字段，或包一层带过期时间的装饰器——对应第 25 讲「值只有 string」那个扩展点。
- 把 `Close` 与 `context.Context` 结合，支持超时取消——这是第 47 讲的内容。
- 最重要的下一步：让别的进程也能调用，也就是第 112 讲的网络层。

## 动手练一练

<Exercise title="给 KV 加一个批量写命令" task="新增 OpMSet，一次写入多个键值对，要求复用现有 Command 结构且不破坏 Storage 接口。写出解析与 Apply 分支。" :hints='["可以用 Value 承载一段编码后的多组键值，或新增一个字段","注意解析失败时要整批拒绝，不能写一半","记得补一条表驱动测试覆盖部分失败"]' solution="在 Command 增加 Items []KeyValue 字段（旧字段不动）；ParseCommand 遇到 MSET 时按 key=value 对解析并填充 Items；Apply 的 case OpMSet 先在锁内校验全部 key 非空且 Items 非空，再一次性写入 map，任意校验失败返回 ErrBadOp 且不改动任何数据；测试里覆盖「一个空 key 导致整批不生效」。" explain="关键点不是多写几行，而是「整批原子」：校验与写入必须在同一临界区，否则会留下半写状态，后面三讲里 WAL 与 Raft 同样要求一条命令不可分割。" />

## 随堂检测

<Quiz no="111-1" q="为什么 Op 常量要写成显式数值 OpGet Op = 1，而不是用 iota 让它自动增长？" :options='["为了少打字","因为第 112 讲要把 Op 编码进网络帧，数值跨版本必须稳定","iota 不支持 byte 类型","编译器要求显式赋值"]' answer="1" explain="第 25 讲用 string 表示 put/get/del，适合文本解析；一旦 Op 数值进入网络帧、WAL 与 Raft 日志，它就变成持久化的数据格式，重排顺序会让旧数据被解释成别的操作，属于兼容性事故。" />

<Predict no="111-2" q="先 SET k 为空值，再 GET k，FormatResult 会输出什么？" :options='["ERR ...","NIL","OK （后面为空）","OK nil"]' answer="2" output="OK " explain="空值是合法命中：Result.Found 为 true、Value 为 nil，fmt 用 %s 打印空字节切片得到空串，于是结果是 OK 加一个空格。这正是 Found 字段存在的意义。" />

<FillBlank no="111-3" tpl="判定「键不存在」应使用 ___ 配合哨兵错误；区分空值与未命中靠 Result 的 ___ 字段；把第 25 讲旧实现接进新契约用 ___。" :answers='["errors.Is~errors.Is(err, ErrNotFound)","Found","WrapLegacy~适配器"]' hint="前两空来自 errors 包与 Result 字段，最后一空是本讲的适配函数" explain="errors.Is 沿错误链匹配哨兵错误；Found 表达是否命中，与 Value 是否为空正交；WrapLegacy 把按操作拆开的旧接口包成单入口，旧实现不用改。" />

## 一页纸速记

<KnowledgeCard :points='["承接第 25 讲的 Storage、MemStore、Command 与哨兵错误，做一次刻意的接口升级","Command 是贯穿四讲唯一的数据载体，字段与 Op 数值锁定不改","Storage 只有 Apply 与 Close 两个方法，越窄越好实现","WrapLegacy 让第 25 讲的 MemStore 不改一行就能接入，是「延续」的实证","用 Found 区分空值与未命中，用 Benchmark 建立后续三讲的性能基线"]' :pitfalls='["把 Op 写成 iota 自动值，后续编码进帧就埋下兼容性地雷","直接把内部切片返回给调用方","错误用 fmt.Errorf 拼字符串，调用方无法用 errors.Is 判定","把接口升级做成推翻重来，不给旧实现留适配层"]' summary="先承接、再定型、后替换：让同一套系统从内存版一路长到分布式，而契约始终连得起来。" />

## 小结与下一讲

1. 承接第 25 讲的 `Storage`、`MemStore`、`Command` 与哨兵错误，用一次刻意的接口升级把 `Command` 与单入口 `Storage` 定型，并用 `WrapLegacy` 证明旧实现可平滑接入。
2. 内存实现用 `RWMutex` 做到并发安全，并用值拷贝、哨兵错误、`Found` 字段处理了四类边界。
3. 表驱动测试、适配器验证与基准测试建立了正确性与性能基线，后面每一讲都拿它做对照。

下一版会遇到的问题很直接：这个 KV 只能在自己进程里被调用。下一讲加一层 TCP 协议帧，并让客户端 SDK 也实现同一个 `Storage`，让调用方完全感知不到本地与远程的区别。

[下一讲：KV 贯穿线 ② TCP 协议帧与网络层](/projects/kv-2-network)
