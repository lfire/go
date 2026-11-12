---
title: 第 16 讲 · map：哈希、遍历与 Set 技巧
description: 掌握 map 的 CRUD、comma-ok、遍历无序、Set 技巧与并发不安全的边界
---

<StageHeader stage="L2 · 语法核心" level="★★☆☆☆" time="30 分钟" goal="会用 map 并知道它的边界" prereq="第 15 讲 · for range 与闭包捕获陷阱" />

# 第 16 讲 · map：哈希、遍历与 Set 技巧

> 切片靠下标找元素，下标必须是连续整数。可现实里更多是「按名字查年龄」「按订单号查状态」这类需求：键是字符串，且不知道第几个。map 就是为这种查找而生——用哈希把键折算成桶位置，平均一次命中。学完本讲，你能写对增删改查、分辨「零值」与「不存在」、用 Set 去重，并知道它在并发下的红线。

## 这一讲你会得到什么
<CheckList id="l16-goals" title="本讲目标" :items='["写出 map 的创建与 CRUD","用 comma-ok 分辨零值与缺失","接受遍历无序并做稳定输出","用空结构体实现 Set，说清并发边界"]' />

## 1. 先建立直觉

把 map 想成带索引的档案柜：键经过哈希函数算出一个桶号，柜子里按桶分格存放键值对，取数据时再算一次桶号就能直接定位，不必从头翻。这带来两个必然结果：查找很快，但格子之间没有先后顺序，所以遍历顺序无法依赖；同一个键只能对应一个位置，重复赋值就是覆盖。

<ConceptDiagram type="map-hmap" title="map 是哈希表" caption="定位桶、比 tophash、再比 key；格子之间没有顺序" />

为什么不让 map 保持插入顺序？维护顺序会拖慢每次写入，把哈希表的均摊常数成本拉高。Go 把顺序交给调用方：真需要就自己取键排序，只做查找的场景不必为用不到的能力付费。

<Term word="comma-ok" text="取值时额外接一个布尔值，用来区分「键存在但值为零」与「键根本不存在」。" />

## 2. 语法与规则

创建 map 有三种写法，但只有初始化过的 map 才能写入。`var m map[string]int` 得到的是 nil map，可读、可 `len`、可 `delete`，一赋值就 panic，提示 `assignment to entry in nil map`。

<CodeTabs title="三种创建方式" :tabs='[{"label":"字面量","code":"m := map[string]int{ \"a\": 1, \"b\": 2 }","note":"适合已知初始键值"},{"label":"make","code":"m := make(map[string]int, 8)","note":"容量是提示，减少扩容"},{"label":"nil map","code":"var m map[string]int","note":"只能读，不能写"}]' />

CRUD 与 `comma-ok` 是日常最高频的部分。注意直接取值时，键不存在会返回值类型的零值，单凭结果无法判断它是否真的存在。

```go
package main

import "fmt"

func main() {
	age := map[string]int{"alice": 18, "bob": 20}
	score := make(map[string]int, 8)

	age["carol"] = 22 // 增改都是赋值
	score["alice"] = 90

	fmt.Println("age[bob] =", age["bob"])
	fmt.Println("age[nobody] =", age["nobody"]) // 缺失时给零值

	if v, ok := age["bob"]; ok {
		fmt.Println("找到", v)
	}
	if _, ok := age["nobody"]; !ok {
		fmt.Println("键不存在")
	}

	delete(age, "alice") // 删不存在的键也安全
	delete(age, "alice")
	fmt.Println("删除后 len =", len(age), "score len =", len(score))
}
```

<OutputBox title="运行结果" output="age[bob] = 20\nage[nobody] = 0\n找到 20\n键不存在\n删除后 len = 2 score len = 1" />

读这段输出要抓住一处：`age[nobody]` 打印的是 0，但 0 也可能是某个真实存在的键的值。所以判定存在只能用 `ok`，不能用值是否为零来推断。这一点在配置表、缓存、计数字典里尤其容易写错。

<LineByLine code="v, ok := m[k]\nif ok {\n    fmt.Println(v)\n}" :notes='["取值同时拿到是否存在标志","ok 为真才进入分支","打印确实存在的值，零值不能用来判定","结束判定块"]' title="comma-ok 逐行拆解" />

## 3. 深入一步

遍历 map 的顺序不保证，语言有意把起点随机化，就是为了不让代码暗中依赖顺序。要稳定输出，先取键排序再访问；只关心数量时可以写 `for range m`。Go 1.21 起标准库提供 `slices.Sort`，配合它就能得到确定的结果。另外，一个经典技巧是用 `map[string]struct{}` 当集合：空结构体不占内存，比 `map[string]bool` 更省。

```go
package main

import (
	"fmt"
	"slices"
)

func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	// range 顺序不保证，先取键排序
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Printf("%s=%d ", k, m[k])
	}
	fmt.Println()

	// 只关心次数时可以不接循环变量
	cnt := 0
	for range m {
		cnt++
	}
	fmt.Println("键个数 =", cnt)

	// Set：空结构体作值，零额外开销
	seen := map[string]struct{}{}
	for _, s := range []string{"go", "java", "go", "rust"} {
		seen[s] = struct{}{}
	}
	_, ok := seen["go"]
	fmt.Println("go 存在 =", ok, "去重个数 =", len(seen))
}
```

<OutputBox title="运行结果" output="a=1 b=2 c=3 \n键个数 = 3\ngo 存在 = true 去重个数 = 3" />

| 操作 | 写法 | 说明 |
| --- | --- | --- |
| 增改 | `m[k] = v` | 键存在即覆盖 |
| 查 | `v := m[k]` | 缺失返回零值 |
| 判定存在 | `v, ok := m[k]` | `ok` 才是依据 |
| 删 | `delete(m, k)` | 键不存在也安全 |
| 数量 | `len(m)` | nil map 返回 0 |
| 遍历 | `for k, v := range m` | 顺序不保证 |

还有两个边界值得记住：map 的键必须是可比较类型，切片、map、函数不能当键；值可以是任意类型，但写入后不能对 `m[k]` 取地址，因为扩容会搬动元素位置。想改结构体字段，得先取出、改完再整体赋值回去。

<Callout type="warn" title="易错点" text="nil map 只能读不能写；map 的 value 不能取地址，因为扩容时元素位置会变；也别用 m[k] 是否为零来判断键存在。" />

<Compare title="遍历 map 的两种写法" :panels='[{"label":"写法 A","code":"for k, v := range m {\n    fmt.Println(k, v)\n}","note":"顺序不保证，输出每次可能不同"},{"label":"写法 B","code":"keys := make([]string, 0, len(m))\nfor k := range m {\n    keys = append(keys, k)\n}\nslices.Sort(keys)","note":"先排序再访问，结果稳定"}]' />

## 4. 工程实践

map 不是并发安全的：多个 goroutine 同时写会触发运行时的 `fatal error: concurrent map writes`，而且这是 fatal error，`recover` 拦不住。读多写少的场景可以用 `sync.RWMutex`，或者直接用标准库的 `sync.Map`；下面用互斥锁保护累加，属于最常见的兜底写法。

```go
package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	m := make(map[string]int)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			m["hits"]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	mu.Lock()
	fmt.Println("hits =", m["hits"])
	mu.Unlock()
}
```

<OutputBox title="运行结果" output="hits = 100" />

<Callout type="tip" title="容量提示不是限制" text="make 的第二个参数只是初始容量提示，map 会自动扩容；它不会限制能存多少键，只是减少前期搬桶的次数。" />

先补一条最容易忽略的原理：map 变量本身是个轻量的描述符，赋值或传参拷的是这个描述符，底层那张哈希表仍是同一份。所以在函数里 `delete(m, k)`，调用方也能看到键没了——这和切片改元素会互相看见是同一类「共享」问题，却比切片更隐蔽，因为 map 连「子切片」这种明显的截取动作都不需要。想确认直接写个小例子 `go run`，或者更干脆：给一个结构体做纯读快照时，别把内部 map 直接返回出去，先 `make` 一份再逐键拷贝。真正存储键值对的 `hmap` 结构在 `runtime/map.go`，桶怎么分裂、怎么渐进搬迁是 L3 的内容；本讲只要建立「map 是引用、不是值」的直觉。

由这个结构还能推出两个编译期边界。`&m[k]` 会直接编译失败，报 `cannot take address of m[k]`：因为扩容会搬动元素，地址根本不稳定，语言干脆禁止取址，逼你「取出—改字段—整体赋值回去」。用 `go build` 就能复现这条错误，比背规则牢。另一条是版本差异：Go 1.21 才加入内置 `clear(m)`，一行清空所有键且对 nil map 是空操作；在此之前只能 `for k := range m { delete(m, k) }` 或者干脆重新 `make`。读老代码看到手写清空循环，先查 `go.mod` 的 `go` 指令再决定要不要改。

并发场景最值得跑的一条命令是 `go test -race`。多个 goroutine 同时 `m[k]++` 即便不触发 `fatal error: concurrent map writes`，也会丢失更新——读改写不是原子的，`-race` 会明确报出 `DATA RACE`，`sync.RWMutex` 或 `sync.Map` 才是正解。真实踩坑里有一类特别安静：把 map 当长期缓存用却从不清理，键只增不减，`runtime.ReadMemStats` 里 `HeapInuse` 一路涨，最后 OOM；定期 `clear` 或加 TTL 才收得住。还有人把 `sync.Map` 当普通 map 写 `m[k] = v`，直接编译失败——它只有 `Store`/`Load` 方法，没有下标赋值。

<FlowTimeline title="一次取值该不该信 ok" :steps='[{"title":"算哈希","desc":"定位到桶","tone":"blue"},{"title":"比键","desc":"找到则返回 v, true","tone":"orange"},{"title":"未命中","desc":"返回零值, false","tone":"green"},{"title":"判定","desc":"只看 ok，不看 v 是否为零","tone":"purple"}]' direction="horizontal" />

下面两段代码把「版本差异」与「并发安全」落成能跑的形式。第一段用 Go 1.21 的 `clear` 清空 map，并验证 nil map 传进去也不会 panic、清空后依然可写。

```go
package main

import "fmt"

func main() {
	m := map[string]int{"a": 1, "b": 2}
	clear(m) // Go 1.21 起的内置函数，一行清空所有键
	fmt.Println("清空后 len =", len(m))
	m["c"] = 3 // 清空后依然可写
	fmt.Println("重新写入后 len =", len(m))

	var nilMap map[string]int
	clear(nilMap) // 对 nil map 是空操作，不会 panic
	fmt.Println("nil map 清空后 len =", len(nilMap))
}
```

<OutputBox title="运行结果" output="清空后 len = 0\n重新写入后 len = 1\nnil map 清空后 len = 0" />

第二段用 `sync.RWMutex` 把读写都保护起来：读多写少时多个读可并行，查不到时返回明确错误而不是把零值误当成命中，最后用 50 个 goroutine 并发写入验证安全性。

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

// SafeIndex 用读写锁保护 map：读多写少时多个读可并行。
type SafeIndex struct {
	mu sync.RWMutex
	m  map[string]int
}

func NewSafeIndex() *SafeIndex {
	return &SafeIndex{m: make(map[string]int)}
}

func (s *SafeIndex) Set(k string, v int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
}

// Get 查不到时返回明确错误，不把零值误当成命中
func (s *SafeIndex) Get(k string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.m[k]; ok {
		return v, nil
	}
	return 0, errors.New("键不存在")
}

func main() {
	idx := NewSafeIndex()
	idx.Set("a", 0) // 值恰好为 0 但键真实存在
	v, err := idx.Get("a")
	fmt.Println("命中零值 =", v, "出错 =", err != nil)

	_, err = idx.Get("missing")
	fmt.Println("缺失时出错 =", err != nil, "提示 =", err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) { // 把循环变量作参数传入，避开闭包捕获
			defer wg.Done()
			idx.Set(fmt.Sprintf("k%d", n), n)
		}(i)
	}
	wg.Wait()
	v, err = idx.Get("k7")
	fmt.Println("并发写后取 k7 =", v, "出错 =", err != nil)
}
```

<OutputBox title="运行结果" output="命中零值 = 0 出错 = false\n缺失时出错 = true 提示 = 键不存在\n并发写后取 k7 = 7 出错 = false" />

| 方案 | 适用场合 | 代价 | 注意点 |
| --- | --- | --- | --- |
| `sync.Mutex` | 读写都频繁、逻辑简单 | 读也串行 | 临界区尽量短，别忘解锁 |
| `sync.RWMutex` | 读多写少 | 写会阻塞所有读 | 读锁内不要再做写操作 |
| `sync.Map` | 键集合稳定、读远多于写 | 内存占用更高 | 只有 Store / Load，没有下标赋值 |

<Predict q="var m map[string]int 是 nil map，对它调用 Go 1.21 的 clear(m) 后再 len(m)，结果是？" :options='["panic: assignment to entry in nil map","编译失败","0，且不会 panic","1"]' answer="2" output="0，不会 panic" explain="clear 对 nil map 是空操作，len 仍为 0；这和「nil map 写入会 panic」并不矛盾，clear 只清已有键，没有就什么都不做。" />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l2-basics/lesson-016-maps`](https://github.com/lfire/go/tree/main/examples/l2-basics/lesson-016-maps)。clone 仓库后进入 `examples/l2-basics`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

如果还想把上面几条结论追到语言规范与运行时源码，下面给出可自己复现的推导、两条真实误解的对照，以及带逐字段解释的实验。

### 再推一层：不可取址、键可比较与随机顺序的规范依据

`&m[k]` 编译失败，根本依据不是「扩容会搬元素」这条运行时现象，而是规范层面的可寻址性：Go 语言规范「Address operators」规定取址操作数必须是 addressable，而「Index expressions」明确 map 的 index 表达式**不是** addressable。所以无论 `make` 给多大容量、是否真的会扩容，编译器在类型检查阶段就拒绝 `&m[k]`；「扩容会搬动元素、地址不稳定」是设计动机，不是判定条件。同理 `m[k].Field = v` 也失败：map 返回的是 value 的一份拷贝，且这份拷贝不可寻址，无法给它的字段赋值；正确姿势是「取出 — 改字段 — 整体赋值回去」，或把 value 存成指针。键必须可比较也写在规范「Map types」里：`==` 与 `!=` 必须对键类型完全有定义，否则编译器报 `invalid map key type`，因此数组、可比较的结构体、指针可以作键，切片、map、函数不行。遍历顺序不保证则来自 `runtime/map.go` 的 `mapiterinit`：`fastrand()` 随机选起始桶与桶内偏移，`mapiternext` 顺序扫桶，所以顺序既不稳定也不该被依赖；规范同时写明「迭代中删除当前键安全，新增键可能被扫到也可能不被扫到」，这正是桶扫描的直接推论。

### 两条真实误解：错误写法与正确写法

**误解一：`&m[k]` 只是因为扩容才会失败，预分配足够容量、保证不扩容就能取地址。** 可寻址性是编译期的类型属性，不随运行时容量变化。

<Compare title="取址失败与容量无关" :panels='[{"label":"错误：以为容量够就合法","code":"m := make(map[string]int, 16)\np := &m[\"a\"] // 容量足够也编译失败\n_ = p","note":"报 cannot take address of m[k]"},{"label":"正确：取出改完写回","code":"v := m[\"a\"]\nv++\nm[\"a\"] = v","note":"或把 value 存成指针类型"}]' />

换成 `map[string]*User` 后 `m[k].Field = x` 能写，是因为指针值本身可解引用，而非 `m[k]` 变得可寻址——别把两件事混为一谈。

**误解二：把 map 传进函数，函数内 `m = make(...)` 重新赋值会清空或替换调用方那张表。** 传参拷的是描述符，重新赋值只改副本的指向。

<Compare title="描述符的重新赋值与删键" :panels='[{"label":"错误：以为换掉了调用方的表","code":"func reset(m map[string]int) {\n    m = make(map[string]int)\n}\n// 调用方那张表不变","note":"副本改指向，不影响原 hmap"},{"label":"正确：改共享的 hmap","code":"func clearAll(m map[string]int) {\n    for k := range m {\n        delete(m, k)\n    }\n}","note":"删键作用于同一张表，调用方看得见"}]' />

这与「切片传参改元素互相看见、但 `append` 换数组不回传」是同一套规则：描述符按值传，共享的是它指向的数据结构。

### 可验证实验：命令、输出与逐字段解释

```bash
go build ./...
go doc builtin.delete
```

```text
# example.com/demo
./main.go:5:7: invalid operation: cannot take address of m["a"] (map index expression of type int)
./main.go:8:10: invalid map key type []byte
func delete(m map[Type]Type1, key Type)
    If m is nil or there is no such element, delete is a no-op.
```

| 输出片段 | 含义 |
| --- | --- |
| `./main.go:5:7` | 报错位置：文件、行、列 |
| `cannot take address of m["a"]` | 取址被拒，map index 表达式不可寻址 |
| `(map index expression of type int)` | 编译器点明操作数类别与元素类型 |
| `invalid map key type []byte` | 键类型的 `==` 未定义，编译期拒绝 |
| `func delete(m ..., key Type)` | delete 的签名，key 类型必须与键类型一致 |
| `If m is nil ... delete is a no-op` | 规范承诺：nil map 与缺失键都安全 |

下面这段把两条误解、数组作键与 `clear` 对 nil map 的行为跑成确定输出：

```go
package main

import "fmt"

type User struct{ Age int }

func reset(m map[string]int) {
	m = make(map[string]int)
}

func clearAll(m map[string]int) {
	for k := range m {
		delete(m, k)
	}
}

func main() {
	users := make(map[string]User, 16)
	users["a"] = User{Age: 18}
	u := users["a"]
	u.Age = 20
	users["a"] = u
	fmt.Println("取出改回写 =", users["a"].Age)

	ptrs := map[string]*User{"a": &User{Age: 18}}
	ptrs["a"].Age = 30
	fmt.Println("指针 value 直接改 =", ptrs["a"].Age)

	m := map[string]int{"x": 1}
	reset(m)
	fmt.Println("重新赋值后调用方 =", m)
	clearAll(m)
	fmt.Println("删键后调用方 =", m)

	byArray := map[[3]int]string{}
	byArray[[3]int{1, 2, 3}] = "ok"
	fmt.Println("数组键 =", byArray[[3]int{1, 2, 3}])

	var nilMap map[string]int
	clear(nilMap)
	fmt.Println("nil map 清空后 len =", len(nilMap))
}
```

<OutputBox title="运行结果" output="取出改回写 = 20\n指针 value 直接改 = 30\n重新赋值后调用方 = map[x:1]\n删键后调用方 = map[]\n数组键 = ok\nnil map 清空后 len = 0" />

### 版本差异与适用边界

| 版本 | 变化 | 适用边界 |
| --- | --- | --- |
| Go 1.18 | `any` 成为 `interface{}` 别名，可作 map 的 value 类型 | 与 `interface{}` 等价 |
| Go 1.20 | `comparable` 约束放宽，普通接口值可满足 | 影响泛型 map 键，见第 22 讲 |
| Go 1.21 | 内建 `clear(m)` 一行清空所有键 | 对 nil map 是空操作 |
| Go 1.21 | 标准库 `maps` 包提供 `Clone`/`Keys`/`Values` | 手写拷贝有了官方替代 |
| Go 1.23 | `maps.Keys`/`Values` 返回 `iter.Seq` 迭代器 | 需配合 `for range` 使用 |
| 全版本 | `&m[k]` 不可取址、键须可比较、并发写 fatal | 结论与版本无关 |

适用边界还有两条：`clear(m)` 只删键、不缩桶，长期膨胀的缓存要定期重建而非只清键；键类型若有不可比较字段（如含切片的结构体）同样编译失败，别等运行时才发现。

### 与 L3 底层、L10 实战的呼应

[第 29 讲](/core/map-internals) 会展开 `hmap`/`bmap`、桶分裂与渐进扩容，把「为什么地址不稳定」落到结构里；[第 30 讲](/core/interface-internals) 解释 map 里装 `any` 时的动态类型；[第 45 讲](/concurrency/sync-primitives) 与 [第 49 讲](/concurrency/race) 分别给出 `RWMutex` 与竞态的判据。实战里，[第 111 讲](/projects/kv-1-memory) 的 `MemStore` 就是「一张 map 加一把锁」，[第 116 讲](/projects/micro-seckill) 的库存热点 key 则把锁粒度与 `sync.Map` 的取舍摆到台面上。

## 动手练一练

<Exercise title="用 Set 保序去重" task="对含重复元素的字符串切片 src 去重并保持首次出现的顺序，打印结果切片。" :hints='["用 map[string]struct{} 记录已出现","结果可以复用 src 的前半段"]' solution="seen := map[string]struct{}{}\nout := src[:0]\nfor _, s := range src {\n    if _, ok := seen[s]; ok {\n        continue\n    }\n    seen[s] = struct{}{}\n    out = append(out, s)\n}\nfmt.Println(out)" explain="seen 判重，out 只追加首次出现的元素，输出 [go java rust]。" />

## 随堂检测

<Quiz no="16-1" q="关于 map，正确的是？" :options='["nil map 可以直接写入","m[k] 返回零值说明键一定不存在","map 的 value 可以安全取地址","delete 对不存在的键是安全的"]' answer="3" explain="nil map 写入会 panic；零值也可能是键存在且值恰为零；value 因扩容不能取址；delete 对缺失键无害。" />

<Predict no="16-2" q="m 是空的 map[string]int，执行 v, ok := m[x] 后 v 与 ok 是？" :options='["0 与 false","0 与 true","nil 与 false","编译失败"]' answer="0" output="0 false" explain="键不存在时返回值类型零值，ok 为 false；这正是 comma-ok 存在的意义。" />

## 一页纸速记

<KnowledgeCard :points='["只有初始化后的 map 能写入","comma-ok 才能判定键存在","遍历顺序不保证，稳定输出先排序","空结构体作值实现 Set"]' :pitfalls='["nil map 直接赋值","依赖 range 顺序","并发写没有加锁"]' summary="map 用哈希换查找速度，代价是无顺序、值不可取址、并发不安全。" />

## 小结与下一讲

- `make` 或字面量初始化后才有可写的 map；`comma-ok` 是区分零值与缺失的唯一依据。
- 遍历顺序随机，稳定输出要先取键排序；`map[string]struct{}` 是零开销 Set。
- 并发写必须加锁或换 `sync.Map`，fatal error 无法用 `recover` 兜住。

map 的键与值都是零散数据，接下来把相关数据打包成一个整体。

[下一讲：结构体、标签与内存对齐](/basics/structs)
