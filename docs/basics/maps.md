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
