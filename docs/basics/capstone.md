---
title: 第 25 讲 · 阶段综合：内存版 KV 存储
description: 用接口、错误、结构体与锁搭一个内存版 KV，做 CRUD、命令解析与并发保护，并为第 111 讲起的 KV 贯穿线留好扩展点。
---

<StageHeader stage="L2 · 语法核心" level="★★★☆☆" time="50 分钟" goal="用接口、错误、结构体搭一个可扩展的小系统" prereq="第 24 讲 · JSON、序列化与常见坑" />

# 第 25 讲 · 阶段综合：内存版 KV 存储

> 前面十二讲把 L2 的零件一个个拆开讲过：map、结构体、方法、接口、错误、defer。这一讲把它们装成一台能跑的小机器——一个支持 put、get、del 的内存 KV。重点不在代码量，而在分层：上层只认一个 `Storage` 接口，底层具体实现随便换。这也是一条贯穿线的起点，第 111 到 114 讲会把它一路演进成带 WAL、分片与 Raft 的分布式存储。今天留下的每个接缝，都是以后能长出新东西的地方。

## 这一讲你会得到什么
<CheckList id="lesson-25" title="本讲目标" :items='["用最小接口抽象存储，让上层不依赖具体实现","用哨兵错误与 errors.Is 设计 CRUD 的失败语义","用 sync.RWMutex 保护 map 的并发读写","把输入解析成可重放的 Command，与存储层解耦","认出为网络层、WAL、分片预留的扩展点"]' />

## 1. 先建立直觉

把 KV 想成前台储物柜。客人只和柜台打交道，柜台提供四个动作：存、取、丢、列出柜号。柜子内部是木格还是铁格、有没有监控、要不要留纸质台账，客人一概不关心。哪天商场把柜子全换了，只要柜台这四个动作没变，客人的用法就不用改。

代码里，「柜台」就是 `Storage` 接口，「木格柜」就是 `MemStore`。把变量声明成接口类型，就把「我要什么能力」和「能力怎么实现」分开了：这正是第 19 讲的依赖倒置。

<ConceptDiagram type="architecture-layer" title="依赖倒置：命令层只认 Storage 接口，实现放在基础设施层" />

架构图里的四层在这里落得很实：命令解析在接口层，编排在应用层，`Storage` 与 `Command` 在领域层，`MemStore` 在基础设施层。依赖方向一律朝内，替换实现不动核心。

## 2. 语法与规则

先给出完整的接口与内存实现。三个要点：错误用包级哨兵暴露、锁用 `sync.RWMutex` 区分读写、`Keys()` 返回一份新切片而不是内部数据。

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

// 三个包级哨兵错误：调用方用 errors.Is 判定，不比字符串。
var (
	ErrNotFound = errors.New("kv: key 不存在")
	ErrEmptyKey = errors.New("kv: key 不能为空")
)

// Storage 是 KV 对外的最小契约：上层只依赖它，不依赖 map。
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() []string
}

// MemStore 是内存实现：一张 map 加一把读写锁。
type MemStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemStore 返回具体类型，调用方按需赋给接口变量。
func NewMemStore() *MemStore {
	return &MemStore{data: make(map[string]string)}
}

func (s *MemStore) Put(key, value string) error {
	if key == "" {
		return ErrEmptyKey
	}
	s.mu.Lock()
	defer s.mu.Unlock() // 第 21 讲：解锁一定执行
	s.data[key] = value
	return nil
}

func (s *MemStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key] // 第 16 讲：comma-ok
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *MemStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return ErrNotFound
	}
	delete(s.data, key)
	return nil
}

func (s *MemStore) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

func main() {
	var store Storage = NewMemStore() // 声明为接口，换实现不动这里
	_ = store.Put("lang", "go")
	v, err := store.Get("lang")
	fmt.Println(v, err == nil) // go true

	_, err = store.Get("missing")
	fmt.Println(errors.Is(err, ErrNotFound)) // true

	_ = store.Delete("lang")
	fmt.Println(len(store.Keys())) // 0
}
```

<OutputBox title="运行结果" output="go true\ntrue\n0" note="变量是接口类型，但方法调用会动态派发到 MemStore；错误判定靠 errors.Is，不靠文本比较。" />

<LineByLine title="Keys 方法逐行看" code="func (s *MemStore) Keys() []string {\n    s.mu.RLock()\n    defer s.mu.RUnlock()\n    out := make([]string, 0, len(s.data))\n    for k := range s.data {\n        out = append(out, k)\n    }\n    return out\n}" :notes='["指针接收者，才能碰到同一把锁与同一张 map","读操作用读锁，多个读可以并行","defer 保证任何出口都会解锁","按当前元素个数预分配，避免反复扩容","遍历 map 取键，顺序不保证","把键追加进结果切片","返回新切片，调用方改动碰不到内部数据"]' />

接着把「输入」独立成命令层。`Command` 是一个普通结构体，天然能被序列化、被记录、被重放；`Apply` 只依赖一个更小的接口，于是第 1 段的 `MemStore` 和未来任何实现都能直接插进来。

```go
package main

import (
	"errors"
	"fmt"
	"strings"
)

// Storage 只声明命令层真正需要的三个方法，遵循小接口原则。
type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

// Command 是一条可记录、可重放的操作，适合写 WAL 或做协议帧。
type Command struct {
	Op    string
	Key   string
	Value string
}

// Parse 把一行文本解析成 Command。
func Parse(line string) (Command, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return Command{}, errors.New("空命令")
	}
	switch parts[0] {
	case "put":
		if len(parts) != 3 {
			return Command{}, errors.New("put 需要 key 与 value")
		}
		return Command{Op: "put", Key: parts[1], Value: parts[2]}, nil
	case "get", "del":
		if len(parts) != 2 {
			return Command{}, errors.New("该命令只接受一个 key")
		}
		return Command{Op: parts[0], Key: parts[1]}, nil
	default:
		return Command{}, fmt.Errorf("不支持的命令: %s", parts[0])
	}
}

// Apply 把 Command 应用到任意 Storage 实现。
func Apply(s Storage, c Command) (string, error) {
	switch c.Op {
	case "put":
		return "OK", s.Put(c.Key, c.Value)
	case "get":
		return s.Get(c.Key)
	case "del":
		return "OK", s.Delete(c.Key)
	default:
		return "", errors.New("未知操作")
	}
}

// tiny 是单线程演示用的最小实现；换成第 1 段的 MemStore 也兼容。
type tiny struct{ m map[string]string }

func (t *tiny) Put(k, v string) error { t.m[k] = v; return nil }
func (t *tiny) Get(k string) (string, error) {
	v, ok := t.m[k]
	if !ok {
		return "", errors.New("key 不存在")
	}
	return v, nil
}
func (t *tiny) Delete(k string) error { delete(t.m, k); return nil }

func main() {
	s := &tiny{m: make(map[string]string)}
	for _, line := range []string{"put lang go", "get lang", "del lang", "put ver 1.23"} {
		c, err := Parse(line)
		if err != nil {
			fmt.Println("解析失败:", err)
			continue
		}
		out, err := Apply(s, c)
		fmt.Println(line, "→", out, err == nil)
	}
}
```

<OutputBox title="运行结果" output="put lang go → OK true\nget lang → go true\ndel lang → OK true\nput ver 1.23 → OK true" note="命令层不认识 map，只认识 Storage；把输入换成网络字节流时，这里几乎不用改。" />

## 3. 深入一步

并发保护必须由实现自己兜住。`map` 并发读写会直接触发运行时的 fatal error，调用方不该也不该被要求记得加锁。下面把锁单独拿出来验证：`-race` 下也应干净通过。

```go
package main

import (
	"fmt"
	"sync"
)

// safeKV 只保留最小的一对方法，专门验证锁的作用。
type safeKV struct {
	mu sync.RWMutex
	m  map[string]string
}

func (s *safeKV) Put(k, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

func (s *safeKV) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

func main() {
	s := &safeKV{m: make(map[string]string)}
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Put(fmt.Sprintf("k%d", i), "v")
		}(i)
	}
	wg.Wait()
	fmt.Println(s.Size()) // 1000
}
```

<OutputBox title="运行结果" output="1000" note="可用 go run -race 验证：去掉 mu 之后，同样的程序会被 race detector 抓出并发写 map。" />

现在把可扩展点摊开。今天刻意留下的每个「缝」，都对应贯穿线里的一次演进：

| 今天的实现 | 留下的扩展点 | 后续讲次 |
| --- | --- | --- |
| `Storage` 四个方法 | 接口不变，替换实现即可接入新后端 | 第 111 讲 |
| `Command` 结构体 | 可序列化成协议帧、可重放 | 第 112 讲协议帧 |
| 哨兵错误 + `errors.Is` | 映射成网络层错误码，不泄露内部文案 | 第 112 讲 |
| 单条内存 `map` | 追加 WAL、崩溃恢复、快照 | 第 113 讲 |
| 全局一把 `RWMutex` | 按 key 分片加锁，提高并发度 | 第 113 讲 |
| 值只有 `string` | 加版本、TTL 与类型，再做分片与副本 | 第 114 讲 |

<FlowTimeline title="KV 贯穿线的演进路线" :steps='[{"title":"第 25 讲 内存版","desc":"Storage 接口与 MemStore","tone":"blue"},{"title":"第 111 讲 抽象定型","desc":"Command 与错误模型","tone":"green"},{"title":"第 112 讲 网络层","desc":"协议帧与 TCP 服务","tone":"orange"},{"title":"第 113 讲 持久化","desc":"WAL、快照与锁分片","tone":"purple"},{"title":"第 114 讲 分布式","desc":"分片、副本与 Raft","tone":"red"}]' direction="horizontal" />

<Term word="依赖倒置" text="上层定义接口、下层提供实现，业务规则不依赖具体技术；替换存储引擎时核心代码不用改。" />

<Callout type="warn" title="易错点：接口、错误与锁的边界" text="把变量声明成 Storage 之后，就再也访问不到 s.data，想加能力要给具体类型加方法而不是去改接口。Get 返回两个值，必须先判 err 再用 value，否则拿到的是空串。不要在持有锁时调用外部回调，容易和对方形成死锁；读用 RLock、写用 Lock，别为省事全用写锁。错误判定用 errors.Is，别拿错误文本去比。" />

## 4. 工程实践

这套分层最直接的收益是可测试性：`Apply` 只要一个满足 `Storage` 的对象，单测里塞一个 `fakeStore` 就能验证命令解析，不必起真实存储。构造函数 `NewMemStore` 返回具体指针、调用处再赋给接口变量，正好是「接受接口、返回结构体」——接口留在消费侧，定义得越小越好。

命令解析与存储解耦还有一层好处：未来网络层只需把字节流解成 `Command`，`Apply` 与 `MemStore` 原样复用；写 WAL 时也不必重新解析业务意图，直接把 `Command` 追加落盘即可。错误全部收敛成哨兵，映射到协议错误码时不会把内部文案泄给客户端。

<Compare title="依赖抽象与依赖实现" :panels='[{"label":"推荐：依赖 Storage 接口","code":"func Serve(s Storage, c Command) (string, error) {\n    return Apply(s, c)\n}","note":"换实现、塞 mock、加装饰器都不动上层，可测性好。"},{"label":"反例：直接传 map","code":"func Serve(m map[string]string, c Command) string {\n    m[c.Key] = c.Value\n    return m[c.Key]\n}","note":"把并发、错误、空 key 全漏给调用方，map 并发写还会崩。"}]' />

目前全局一把锁是最简单也最正确的选择，别急着上锁分片。等真有压测数据说明锁是瓶颈，再按第 113 讲的思路拆；过早优化会先把正确性赔进去。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-025-capstone`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-025-capstone)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条设计结论追到运行时与工具链，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：fatal error、动态派发与返回副本的依据

这一讲把前面十二讲的结论装成系统，几条设计可以追到规范与运行时依据。其一，`map` 并发写触发的是运行时 `fatal error: concurrent map writes`——`runtime/map.go` 的 `mapassign` 在写前检查 `h.flags` 里的 `hashWriting` 标志，发现同一个 `hmap` 已被另一个 goroutine 标记写入就直接 `throw`；`throw` 是 fatal error，不是 panic，`recover` 拦不住，所以 `MemStore` 必须自己持锁，不能把加锁责任推给调用方。其二，`Storage` 接口之所以能换实现，是第 19 讲的隐式实现加第 30 讲的 itab 动态派发：`var store Storage = NewMemStore()` 的两个字分别指向 itab 与 `*MemStore`，方法调用经 itab 的方法表间接跳转，编译期只检查 `MemStore` 的方法集是否覆盖 `Storage`。其三，`Keys()` 返回新切片而不是内部数据，依据是第 13 讲的「切片是描述符」——直接返回内部切片会把底层数组的写权限一起交出去。其四，`defer s.mu.Unlock()` 紧贴 `Lock()`，依据是第 21 讲的 LIFO 与参数快照。错误用哨兵加 `errors.Is`，则来自第 20 讲的 Unwrap 链。

### 两条真实误解：错误写法与正确写法

**误解一：`map` 并发写既然会报错，外面套一层 `recover` 就能兜住继续服务。** `recover` 只接 panic，不接 throw。

<Compare title="fatal error 能不能 recover" :panels='[{"label":"错误：以为 recover 能兜住","code":"defer func() { recover() }()\n// 多 goroutine 同时写同一个 map","note":"fatal error 是 throw，进程直接退出"},{"label":"正确：写前加锁","code":"mu.Lock()\nm[k] = v\nmu.Unlock()","note":"或改用 sync.Map，临界区尽量短"}]' />

运行时的 `throw` 会打印栈后直接结束进程，任何 deferred 函数都来不及成为「兜底」；这是把锁放进实现内部、而不是文档里提醒调用方的硬理由。

**误解二：`Keys()` 直接返回内部切片更省一次分配，调用方只读就没问题。** 返回描述符等于把底层数组的写权限交出去。

<Compare title="Keys 该返回什么" :panels='[{"label":"错误：返回内部切片","code":"func (s *MemStore) Keys() []string {\n    return s.keys\n}","note":"调用方改元素会回写内部状态，且绕过锁"},{"label":"正确：返回副本","code":"out := make([]string, 0, len(s.data))\nfor k := range s.data {\n    out = append(out, k)\n}\nreturn out","note":"副本独立，读锁只保护遍历这一段"}]' />

切片是描述符，返回它等于把底层数组一起交出去；`map` 本身也不可并发读写，读锁期间必须完成拷贝再释放。

### 可验证实验：命令、输出与逐字段解释

```bash
go test -race ./...
go vet ./...
```

```text
ok      example.com/kv  0.412s
# example.com/kv
./store.go:9:9: NewMemStore passes lock by value: example.com/kv.MemStore contains sync.RWMutex
```

| 输出片段 | 含义 |
| --- | --- |
| `ok example.com/kv 0.412s` | 包名与耗时；`-race` 未发现竞态 |
| `WARNING: DATA RACE`（去掉锁后会出现） | 定位并发读写同一内存地址 |
| `# example.com/kv` | vet 报告的包路径 |
| `./store.go:9:9` | 文件、行、列 |
| `passes lock by value` | copylocks：含锁类型按值返回或传参会复制锁 |
| `contains sync.RWMutex` | 触发检查的具体字段 |

两条命令互补：`-race` 在运行期抓并发读写，`go vet` 在静态期抓「锁被复制」；构造函数应返回 `*MemStore` 指针，正好避开这条告警。

下面这段用一百个 goroutine 并发写、再用接口替身验证可测试性，输出确定：

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

var ErrNotFound = errors.New("kv: key 不存在")

type Storage interface {
	Put(key, value string) error
	Get(key string) (string, error)
}

type MemStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func (s *MemStore) Put(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = v
	return nil
}

func (s *MemStore) Get(k string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[k]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// fakeStore 是单测替身：接口让 Apply 不必起真实存储。
type fakeStore struct{}

func (fakeStore) Put(k, v string) error { return nil }
func (fakeStore) Get(k string) (string, error) {
	return "fake", nil
}

func Apply(s Storage, put bool, k, v string) (string, error) {
	if put {
		return "OK", s.Put(k, v)
	}
	return s.Get(k)
}

func main() {
	s := &MemStore{data: make(map[string]string)}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = s.Put(fmt.Sprintf("k%d", n), "v")
		}(i)
	}
	wg.Wait()
	v, err := s.Get("k7")
	fmt.Println("并发写后取 k7 =", v, "出错 =", err != nil)
	out, _ := Apply(fakeStore{}, false, "x", "")
	fmt.Println("替身测试 =", out)
}
```

<OutputBox title="运行结果" output="并发写后取 k7 = v 出错 = false\n替身测试 = fake" />

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.9 | `sync.Map` 加入 | 读多写少、键集合稳定时可用 |
| Go 1.13 | `errors.Is` / `errors.As` 让哨兵错误可跨包装判定 | 本讲错误模型依赖它 |
| Go 1.18 | `any` / 泛型加入，`sync.Map` 仍是具体类型 | 泛型版未进标准库 |
| Go 1.22 | 循环变量每轮新建，并发 goroutine 传参更省心 | 与第 15 讲衔接 |
| 全版本 | map 并发写 fatal、`recover` 拦不住 throw | 结论与版本无关 |

适用边界还有一条：全局一把 `RWMutex` 是最简单也最正确的选择，别急着上锁分片；等压测数据说明锁是瓶颈，再按第 113 讲的思路拆，过早优化会先把正确性赔进去。

### 与 L3 底层、L10 实战的呼应

[第 29 讲](/core/map-internals) 会展开 `hmap` 与 `hashWriting` 标志，[第 30 讲](/core/interface-internals) 讲 itab 动态派发，[第 45 讲](/concurrency/sync-primitives) 与 [第 49 讲](/concurrency/race) 分别给出锁原语与竞态判据；[第 52 讲](/engineering/unit-test) 用表驱动加 fake 替身测命令解析。实战里，[第 111 讲](/projects/kv-1-memory) 是本讲的直接延续，[第 113 讲](/projects/kv-3-async) 追加 WAL 与锁分片，[第 114 讲](/projects/kv-4-distributed) 演进到分片、副本与 Raft——今天留的每个接缝都在那里长出新东西。

## 动手练一练

<Exercise title="给 MemStore 加 Exists" task="在第 1 段的 MemStore 上补一个 Exists(key string) bool 方法：存在返回 true，否则 false。要求用读锁，并用 defer 解锁。" solution="func (s *MemStore) Exists(key string) bool {\n    s.mu.RLock()\n    defer s.mu.RUnlock()\n    _, ok := s.data[key]\n    return ok\n}" :hints='["只读不写，用 RLock 配 RUnlock。","用 comma-ok 判断键是否存在，别先 Get 再判错。"]' explain="读锁允许多个 Exists 并行；defer 保证任何返回路径都解锁；comma-ok 直接给出存在性，不会与 ErrNotFound 的判定缠在一起。" />

## 随堂检测

<Quiz no="25-1" q="关于本讲的设计，下面哪项说法正确？" :options='["Storage 接口应把所有可能的方法都列全，方便以后扩展","把变量声明为 Storage 后，仍能直接访问 MemStore 的 data 字段","Put 与 Delete 用写锁，Get 与 Keys 用读锁","错误判定应比较错误文本，因为哨兵无法跨包使用"]' answer="2" explain="写操作互斥用 Lock，纯读操作用 RLock 提高并行度；接口只暴露必要方法，声明成接口后看不到具体字段，哨兵正好供跨包 errors.Is 判定。" />

<Predict no="25-2" q="第 1 段 main 的最后一行 len(store.Keys()) 会输出什么？" :options='["1","0","2","panic"]' answer="1" output="0" explain="put 后 get 成功，随后 del 把 lang 删掉了，Keys 返回空切片，长度为 0。" />

## 一页纸速记

<KnowledgeCard :points='["用最小 Storage 接口隔离上层与具体实现","错误用包级哨兵，判定用 errors.Is","map 并发不安全，读写分别用 Lock 与 RLock","Command 把输入解析与存储执行解耦，天然可重放"]' :pitfalls='["声明成接口后又想访问具体字段，说明抽象没划对","持有锁时调用外部回调，容易死锁","Get 不判 err 就直接用 value，拿到空串还以为成功","没压测就上锁分片，牺牲正确性换不存在的性能"]' summary="先让接口、错误与锁把边界定稳，后续网络、持久化与分布式才有地方可长。" />

## 小结与下一讲

- `Storage` 接口 + `MemStore` 实现撑起 CRUD，哨兵错误与 `errors.Is` 让失败可判定，`RWMutex` 兜住并发。
- `Command` 与 `Apply` 把输入解析和存储执行分开，是后面协议帧与 WAL 的天然接缝。
- 今天留的六个扩展点，对应第 111 到 114 讲的网络层、持久化、锁分片与 Raft 演进。

L2 语法核心到此收官。下一阶段进入核心机制，从「会用」转向「知道为什么」：先看赋值、传参与拷贝成本，把值语义彻底讲透。

[下一讲：值语义深潜：赋值、传参、拷贝成本](/core/)
