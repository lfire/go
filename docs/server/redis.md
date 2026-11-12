---
title: 第 67 讲 · Redis 缓存：穿透 / 击穿 / 雪崩与一致性
description: 讲清 Cache-Aside 模式、穿透击穿雪崩三大问题的成因与对策、TTL 抖动、双写一致与分布式锁初探，把缓存用对而不是用多
---

<StageHeader stage="L6 · 服务端与数据" level="★★★★☆" time="50 分钟" goal="把缓存用对而不是用多" prereq="第 66 讲 · ORM 与迁移" />

# 第 67 讲 · Redis 缓存：穿透 / 击穿 / 雪崩与一致性

> 加缓存看起来是稳赚的优化：内存比磁盘快几个数量级。但缓存是**副本**，只要引入副本，就必然引入「副本与主数据不一致」以及「副本失效时流量全部砸回主库」两个新问题。线上很多缓存事故不是没加缓存，而是加错了：给写多读少的接口加、给不能容忍任何不一致的余额加、所有 key 设同一个 TTL 导致整点集体失效。这一讲把三大经典问题和一致性方案逐个拆开，并给出可运行的对策代码。

## 这一讲你会得到什么

<CheckList id="67-goal" title="本讲目标" :items='["能写对 Cache-Aside 读路径：先查缓存、未命中回源、回填并设置 TTL","能说清穿透、击穿、雪崩各自的成因与对应解法","理解 TTL 抖动、单飞重建、空值缓存为什么能挡住流量","能分析双写一致的几种方案，并写出带 token 的最小分布式锁"]' />

## 1. 先建立直觉

把数据库想成仓库，Redis 想成店面前台的小货架。顾客要货，先看货架（缓存），有就直接拿；没有才去仓库取一件，顺手放一份到货架上（回填），并贴一个「几小时后下架」的标签（TTL）。这就是 **Cache-Aside**：缓存不主动同步，谁读谁负责回填。

三个经典故障都发生在货架失效的瞬间。**穿透**是顾客一直问一件根本不存在的货，货架永远没有、仓库也永远没有，每次都白跑一趟仓库；**击穿**是一件爆款货刚好下架，同一秒几百个顾客全部涌去仓库取同一件；**雪崩**是货架上一大批货贴了同一时刻的标签，整点到点一起下架，仓库瞬间被全量流量压垮。三者成因不同，解法也不同，混着谈只会记不住。

<ConceptDiagram type="cache-aside" title="Cache-Aside：读时回填，写时删除而不是更新" caption="命中直接返回；未命中才回源并回填；写操作删除缓存，让下一次读自然重建" />

## 2. 语法与规则

第一段代码是完整、可运行的 HTTP 服务：Cache-Aside 读路径、空值缓存防穿透、TTL 抖动防雪崩，以及 Redis 故障时降级回源。

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

// 模拟源数据库；生产里换成 db.QueryRowContext。
var db = map[string]string{"1": "gopher", "2": "alice"}

const (
	baseTTL   = 5 * time.Minute
	emptyTTL  = 60 * time.Second // 空值缓存只挡一小段时间
	emptyMark = "__NULL__"        // 约定一个不会与业务值冲突的哨兵
)

func queryDB(id string) (string, bool) {
	v, ok := db[id]
	return v, ok
}

// ttlWithJitter 在基础 TTL 上加 0~20% 随机量，让 key 分散过期。
func ttlWithJitter(base time.Duration) time.Duration {
	jitter := time.Duration(rand.Int63n(int64(base / 5)))
	return base + jitter
}

func getUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	key := "user:" + id
	c := r.Context()

	val, err := rdb.Get(c, key).Result()
	if err == nil { // 命中
		if val == emptyMark {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK,
			map[string]string{"id": id, "name": val, "from": "cache"})
		return
	}
	// 区分「没命中」与「缓存故障」：后者必须降级回源，不能让整个接口挂掉。
	if !errors.Is(err, redis.Nil) {
		log.Printf("redis get: %v", err)
	}

	name, ok := queryDB(id)
	if !ok {
		// 查不到也缓存一小段空值，挡住对不存在 id 的反复穿透。
		_ = rdb.Set(c, key, emptyMark, emptyTTL).Err()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = rdb.Set(c, key, name, ttlWithJitter(baseTTL)).Err()
	writeJSON(w, http.StatusOK,
		map[string]string{"id": id, "name": name, "from": "db"})
}

func main() {
	rdb = redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 0})
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", getUser)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
```

<OutputBox title="两次请求的差异" output="第 1 次 GET /users/1 返回 from=db，并回填缓存\n第 2 次 GET /users/1 返回 from=cache\nGET /users/999 返回 404，并写入短 TTL 的空值标记\n此后连续请求 999 都在缓存层被挡住" note="缓存故障时接口依然可用，只是回源比例升高；这是降级设计的目标" />

<LineByLine code="val, err := rdb.Get(ctx, key).Result()\nif err == nil {\nreturn val\n}\nrow := queryDB(id)\nrdb.Set(ctx, key, row, ttl)\nreturn row" :notes='["先查缓存，Get 在键不存在时返回 redis.Nil","err 为 nil 才是真正命中","命中直接返回，不碰数据库","未命中才回源；这一步最慢，应加超时与限流","回填时设置带抖动的 TTL，别用永不过期","把结果返回给调用方，下次同类读即命中"]' title="Cache-Aside 读路径逐行看" />

第二段代码用零依赖的纯 Go 演示**单飞重建**：热点 key 失效瞬间，只放一个请求回源，其余等待后直接读缓存；同时加上 TTL 抖动。生产里应当用第 48 讲介绍的 `singleflight.Group` 按 key 分组，这里用一把互斥锁把原理展示清楚。

```go
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type entry struct {
	value   string
	expires time.Time
}

type cache struct {
	mu    sync.Mutex
	data  map[string]entry
	loads sync.Mutex // 简化版单飞：同一时刻只允许一个协程回源
}

func (c *cache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.data[key]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.value, true
}

func (c *cache) set(key, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = entry{value: value, expires: time.Now().Add(ttl)}
}

func ttlWithJitter(base time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(base/5)))
}

// loadWithSingleFlight 让并发的未命中请求只回源一次。
func (c *cache) loadWithSingleFlight(key string, load func() string) string {
	if v, ok := c.get(key); ok {
		return v
	}
	c.loads.Lock()
	defer c.loads.Unlock()
	// 双检：等在锁上的请求进来时，可能已被前一个协程重建好了。
	if v, ok := c.get(key); ok {
		return v
	}
	v := load()
	c.set(key, v, ttlWithJitter(3*time.Second))
	return v
}

func main() {
	c := &cache{data: make(map[string]entry)}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.loadWithSingleFlight("hot", func() string {
				fmt.Println("回源一次（模拟慢查询）")
				time.Sleep(20 * time.Millisecond)
				return "value"
			})
		}()
	}
	wg.Wait()
	fmt.Println("20 个并发请求，回源只发生 1 次")
}
```

`loads` 是全局一把锁，意味着不同 key 也会互相等待，只适合演示；真实实现按 key 分组，让不相干的热点互不阻塞。双检不能省：等到锁的请求拿到的可能是刚被前一个协程填好的值，不检查就会重复回源。

## 3. 深入一步

| 问题 | 成因 | 现象 | 对策 |
| --- | --- | --- | --- |
| 穿透 | 查询**根本不存在**的数据 | 缓存与库都没有，每次都回源 | 空值缓存、布隆过滤器、参数校验 |
| 击穿 | 单个**热点 key** 过期 | 大量并发同时回源同一条数据 | 单飞重建、互斥锁、热点逻辑不过期 |
| 雪崩 | 大量 key **同一时刻**失效或 Redis 宕机 | 全量流量砸向数据库 | TTL 抖动、多级缓存、高可用与限流降级 |

三者的分界线很清晰：穿透是「数据不存在」，击穿是「一个 key」，雪崩是「一批 key 或整个缓存层」。所以空值缓存治不了雪崩，TTL 抖动也治不了穿透。

**双写一致**才是缓存最难的部分。写数据库与写缓存是两个独立操作，没有跨系统事务，并发下必然存在乱序窗口。主流方案有三档：

| 方案 | 做法 | 一致性 | 代价 |
| --- | --- | --- | --- |
| 先更新库，再删缓存 | Cache-Aside 标准写路径 | 短暂不一致，最终一致 | 简单可靠，推荐默认 |
| 延迟双删 | 删缓存、更新库、延迟再删 | 降低旧值回填概率 | 多一次删、延迟时长难定 |
| 订阅 binlog 异步删 | 监听数据库变更事件删缓存 | 更接近最终一致 | 需要 CDC 链路，运维成本高 |

<Term word="热 key" text="被极高频率访问的单个缓存键；单节点 Redis 可能因它被打满网卡与 CPU，常用本地缓存、key 加随机后缀打散到多分片、以及提前探测来治理" />

<Callout type="warn" title="头号反模式：盲目加缓存" text="不是所有读都该缓存。写多读少的接口（缓存刚回填就被删）、要求强一致的数据（余额、库存扣减）、单次查询本身已经很快且结果集很大的场景，加了缓存只会多一跳网络、多一份不一致、多一个要运维的组件。先确认「读多写少、能容忍秒级不一致、回源确实慢」，再决定加。缓存不是加速器，是一致性换性能的交易。" />

<Callout type="idea" title="抖动与多级缓存" text="TTL 抖动只需在基础过期时间上叠加一个随机量（如 0~20%），就能把集体失效摊平到时间轴上。对抗雪崩的另一层是本地缓存（进程内 LRU）作为 Redis 之前的挡箭牌：Redis 宕机时本地缓存仍能顶住一部分流量，但要注意本地缓存的一致性与内存上限。" />

第三段代码演示生产里两件必备能力：带 token 的最小分布式锁，以及延迟双删。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// tryLock 用 SETNX 加唯一 token 与 TTL，实现最小可用的互斥与租约。
func tryLock(ctx context.Context, rdb *redis.Client, key, token string, ttl time.Duration) (bool, error) {
	return rdb.SetNX(ctx, key, token, ttl).Result()
}

// 释放锁必须「先比对 token 再删除」，两步用 Lua 保证原子，
// 否则可能把别人续期后持有的锁误删。
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`)

func releaseLock(ctx context.Context, rdb *redis.Client, key, token string) error {
	return releaseScript.Run(ctx, rdb, []string{key}, token).Err()
}

// updateUser 演示延迟双删：先删、更新库、延迟再删一次，
// 用来清掉「读请求夹在删除与更新之间把旧值回填」的脏数据。
func updateUser(ctx context.Context, rdb *redis.Client, id, name string) error {
	key := "user:" + id
	if err := rdb.Del(ctx, key).Err(); err != nil {
		return err
	}
	fmt.Println("数据库已更新:", id, name) // 生产里是 writeDB
	// 用独立 context：原请求的 ctx 在返回后可能已取消。
	time.AfterFunc(500*time.Millisecond, func() {
		_ = rdb.Del(context.Background(), key).Err()
	})
	return nil
}

func main() {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer rdb.Close()
	ctx := context.Background()
	key := "lock:rebuild:user:1"
	token := fmt.Sprintf("%d", time.Now().UnixNano())

	ok, err := tryLock(ctx, rdb, key, token, 10*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	if !ok {
		fmt.Println("已有实例在重建，本实例跳过")
		return
	}
	defer func() { _ = releaseLock(ctx, rdb, key, token) }()

	fmt.Println("拿到锁，执行缓存重建")
	if err := updateUser(ctx, rdb, "1", "gopher-v2"); err != nil {
		log.Fatal(err)
	}
}
```

锁的三要素缺一不可：**唯一 token**（防止 A 的锁被 B 释放）、**TTL 租约**（持有者崩溃也能自动解锁）、**Lua 原子比对删除**。这里只是初探，完整的看门狗续期、Redlock 争议会在第 84 讲展开。

### 协议与契约：过期、淘汰与一致性的推导

**过期是两种删除策略的组合，不是「到期即消失」。** 若给每个带 TTL 的 key 挂一个定时器，key 数量一大，定时器本身就成了 O(N) 的内存与 CPU 负担。Redis 用的是**惰性删除加定期抽样删除**：访问某个 key 时才检查并删除已过期的（惰性保证不会把过期值返回给客户端），再由后台 `activeExpireCycle` 每隔一小段时间随机抽样一批带 TTL 的 key 清理（定期保证冷 key 也能被回收）。由此推出几个可观测结论：`TTL key` 返回 `-2` 表示 key 已不存在、`-1` 表示存在但永不过期、正数表示剩余秒数；刚过期的 key 可能仍占着内存，但 `GET` 一定读不到它。这也解释了为什么「内存该降不降」时要查的是 `maxmemory` 与淘汰策略，而不是 TTL。

**淘汰与过期是两套独立机制。** 过期由时间与访问驱动；淘汰（eviction）只在内存达到 `maxmemory` 时触发，与 TTL 无关。策略里 `noeviction` 让写入直接报错，`allkeys-lru` 在全体 key 中挑最近最少用删除，`volatile-lru` 只在设了 TTL 的 key 里删，`allkeys-lfu` 按使用频率删，`volatile-ttl` 优先删剩余寿命短的。还有一层常被忽略的实现细节：Redis 的 LRU 与 LFU 都是**近似**算法——随机采样 `maxmemory-samples` 个 key，从中挑最差的淘汰，而不是维护精确的全局 LRU 链表，用一点淘汰精度换内存与 CPU。代价是 `allkeys-lru` 可能删掉没设 TTL 的重要 key，而 `volatile-lru` 在库里几乎没有带 TTL 的 key 时会退化成 `noeviction` 的行为。

**缓存与 DB 不一致的窗口可以画出来。** 写库与写缓存是两个独立操作，没有跨系统事务，并发下必然存在乱序。以「先删缓存、再更新库」为例：读请求 A 恰好在删除之后、更新之前回源，读到旧值并回填，随后库更新完成，缓存里就长期留着旧值，且没有任何人再删它。改成「先更新库、再删缓存」后窗口小得多，但仍存在「读在更新前拿到旧值、删缓存之后才回填」的极小概率，于是才有延迟双删与订阅 binlog 两档加强。三条路各有代价：

| 方案 | 做法 | 一致性 | 代价 |
| --- | --- | --- | --- |
| A 先删缓存再更库 | `DEL` 后 `UPDATE` | 最差，旧值易被回填 | 乱序窗口最大，不推荐 |
| B 先更库再删缓存 | Cache-Aside 标准写路径 | 短暂不一致，最终一致 | 简单可靠，默认推荐 |
| C 延迟双删 / CDC | 删、更、延迟再删，或订阅 binlog 异步删 | 更接近最终一致 | 多一次删或一套 CDC 链路 |

**分布式锁的三要素也是推导出来的。** 只有 `SETNX` 不带 token 时：A 拿锁后超时、锁自动过期，B 拿到锁，A 恢复后执行 `DEL` 会把 **B 的锁**删掉。所以必须有唯一 token（删之前比对）、TTL 租约（持有者崩溃也能自动解锁）、以及 Lua 脚本把「比对加删除」做成原子（否则比对与删除之间仍可能被插入）。三者缺一不可，这也是第 84 讲 Redlock 争议的起点。

### 设计取舍与替代方案

| 决策点 | 方案 A | 方案 B（推荐） | 方案 A 的代价 |
| --- | --- | --- | --- |
| 写后缓存 | 写后 `SET` 新值 | 写后 `DEL` | 并发写乱序留旧值，计算型缓存白白重算 |
| 一致性增强 | 依靠读路径自然重建 | 延迟双删或订阅 binlog | 前者仍有极小脏窗口，后者要运维 CDC |
| 淘汰策略 | `noeviction` | `allkeys-lru` 加监控 | 内存打满时写入直接失败，需容量兜底 |
| 热点 key | 设「永不过期」 | 逻辑过期加后台单飞重建 | 数据改了缓存永远不刷新 |
| 锁实现 | 裸 `SETNX` 加 `DEL` | `SET NX EX` 加 token 加 Lua 释放 | 会误删他人续期后的锁 |

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 缓存与 DB 不一致 | 先删缓存再更库且并发读 | 缓存长期留存旧值 | 缓存与库值抽样比对 | 改先更库再删，必要时延迟双删 |
| 重试放大 | 缓存挂了全量回源无限流 | 数据库被放大数倍流量打死 | 入口与 DB QPS 对比 | 降级回源加限流 |
| 连接池耗尽 | 大量 key 同时失效回源 | DB 等待队列堆积 | `WaitCount`、慢 SQL 日志 | TTL 抖动加单飞重建 |
| 雪崩第二种形态 | Redis 整体不可用 | 全量流量砸向 DB | 缓存健康度告警 | 多级缓存、高可用、限流 |
| 锁误删 | 释放时不比对 token | 删掉他人持有的锁 | 锁持有者日志 | Lua 原子比对删除 |
| 热 key | 单 key 被极高频率访问 | 单分片网卡与 CPU 打满 | 按 key 的 QPS 分布 | 本地缓存、key 加后缀打散 |
| 空值缓存过期长 | 空值 TTL 设太大 | 数据刚创建却长时间读不到 | 空值命中率 | 空值 TTL 远小于正常数据 |
| 消息重复 | CDC 重投导致重复删缓存 | 无业务损害但徒增负载 | 事件 ID 去重 | 删操作天然幂等，可忽略 |

<SequenceDiagram title="缓存不一致窗口与延迟双删" :actors='["Writer","Reader","Cache","DB","CDC"]' :steps='[{"from":"Writer","to":"Cache","label":"删缓存","note":"先删后更","kind":"call"},{"from":"Writer","to":"DB","label":"更新库","note":"尚未提交","kind":"call"},{"from":"Reader","to":"Cache","label":"未命中","note":"GET 返回 nil","kind":"call"},{"from":"Cache","to":"Reader","label":"返回 nil","note":"空结果","kind":"return"},{"from":"Reader","to":"DB","label":"回源旧值","note":"更新还没提交","kind":"call"},{"from":"DB","to":"Reader","label":"返回 old","note":"旧值","kind":"return"},{"from":"Reader","to":"Cache","label":"回填 old","note":"脏数据落地","kind":"call"},{"from":"Writer","to":"DB","label":"提交 new","note":"库已是新值","kind":"call"},{"from":"Writer","to":"Cache","label":"延迟再删","note":"清掉旧值","kind":"async"},{"from":"Reader","to":"Cache","label":"再次读取","note":"未命中回源","kind":"call"},{"from":"Cache","to":"Reader","label":"返回 new","note":"最终一致","kind":"return"},{"from":"CDC","to":"Cache","label":"订阅 binlog","note":"更稳妥的异步删","kind":"async"}]' />

## 4. 工程实践

### 可验证实验：把 TTL、淘汰策略与一致性窗口跑出来看

**实验一：redis-cli 观察过期返回值与淘汰策略。** 用 `--raw` 去掉输出的引号，便于逐字段对照：

```bash
redis-cli --raw SET user:1 gopher EX 2
redis-cli --raw TTL user:1
redis-cli --raw GET user:1
sleep 3
redis-cli --raw TTL user:1
redis-cli --raw SET user:2 alice
redis-cli --raw TTL user:2
redis-cli --raw CONFIG GET maxmemory-policy
redis-cli --raw CONFIG SET maxmemory-policy allkeys-lru
```

<OutputBox title="redis-cli --raw 逐行输出" output="OK\n2\ngopher\n-2\nOK\n-1\nmaxmemory-policy\nnoeviction\nOK" note="-2 是不存在，-1 是永不过期，两者含义完全不同" />

逐字段解释：`OK` 是 `SET ... EX 2` 的返回；`2` 是剩余 TTL 秒数；`gopher` 是命中值；等 3 秒后的 `-2` 表示 key 已因过期而不存在（**不是**还剩 -2 秒）；不带 `EX` 写入的 `user:2` 返回 `-1`，表示存在但永不过期——把 `-1` 误读成「已过期」是排查时的高频错误；`maxmemory-policy` 与 `noeviction` 是 `CONFIG GET` 返回的两行，说明当前不主动淘汰，内存打满时写入会报错；最后的 `OK` 是切到 `allkeys-lru` 成功。对照实验：把 `maxmemory` 设成一个很小的值、灌入上千个 key，再观察 `used_memory` 是否稳定在上限附近、`evicted_keys` 是否上涨，就能把「淘汰与过期是两套机制」验证出来。

**实验二：go test -race 复现缓存与 DB 不一致窗口。** 下面这段完整可编译的测试文件用 channel 精确构造出「删缓存 → 读回填旧值 → 更新库」的交错，把第 3 节那个乱序窗口从文字变成可复现的输出：

```go
package main

import (
	"fmt"
	"testing"
)

func inconsistentWindow() (db, cache string, consistent bool) {
	var dbv, cachev = "old", "old"
	deleted := make(chan struct{})
	updated := make(chan struct{})
	done := make(chan struct{})

	go func() { // 写线程：先删缓存，等读线程回填后再更新库
		cachev = ""
		close(deleted)
		<-updated
		dbv = "new"
		close(done)
	}()

	<-deleted
	if cachev == "" {
		cachev = dbv // 回源，此刻库里还是 old
	}
	close(updated)
	<-done
	return dbv, cachev, dbv == cachev
}

func TestDelayedDoubleDeleteWindow(t *testing.T) {
	db, cache, consistent := inconsistentWindow()
	fmt.Println("db =", db, "cache =", cache, "一致 =", consistent)
	if consistent {
		t.Fatal("该交错下应当出现不一致，实验才说明问题")
	}
}
```

```bash
go test -race -run TestDelayedDoubleDeleteWindow -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestDelayedDoubleDeleteWindow\ndb = new cache = old 一致 = false\n--- PASS: TestDelayedDoubleDeleteWindow (0.00s)\nPASS" note="库已是新值、缓存仍是旧值，且 race 检测无告警" />

逐字段解释：`db = new` 说明写事务确实提交了；`cache = old` 是读线程在「缓存已删、库未更」的窗口里回填的旧值，此后没人再删它，于是缓存长期与库不一致；`一致 = false` 把失败矩阵第一行变成可复现的事实；`-race` 无 `WARNING: DATA RACE`，因为三个 channel 建立了 happens-before，替代了裸共享变量。对照实验：把写线程改成「先 `dbv = "new"` 再 `cachev = ""`」重跑，`一致` 会变成 `true`——两组输出直接解释了 Cache-Aside 为什么选「先更库、再删缓存」。把这段改成 `_test.go` 跑 `go test -race`，也是第 49 讲内存模型的现成练习。

<Predict q="redis-cli 对某个 key 执行 TTL 返回 -1，说明什么？" :options='["key 已过期不存在","key 存在但永不过期","还剩 -1 秒过期","命令执行出错"]' answer="1" output="key 存在但没有设置过期时间，TTL 返回 -1；返回 -2 才是 key 不存在" explain="-2 与 -1 只差一个数字，含义却相反：前者是查无此键，后者是长驻键。排查「内存不降」时把 -1 误当成已过期，会完全找错方向。" />

### 两条真实工程误解

**误解一：设了 TTL，key 到期的瞬间就会从内存里消失。** 错。惰性加定期抽样删除意味着刚过期的 key 可能仍占着内存，只是不会被读取到；内存该降不降时，要查的是 `maxmemory`、`maxmemory-policy` 和 `evicted_keys`，而不是怀疑 TTL 没生效。这个误解还会让人误判 `INFO memory` 的曲线，把正常的惰性回收当成内存泄漏去排查。

**误解二：给热点 key 设「永不过期」就能根治击穿。** 错。永不过期只是把物理失效时刻挪走，却带来「数据改了缓存永远不刷新」的新问题，等于把一致性风险从秒级放大到无限。正解是**逻辑过期**：value 里带一个逻辑过期时间，命中后若发现逻辑已过期，就由单个后台请求拿锁重建，其余请求先返回旧值；这样热点 key 既不因物理过期被并发回源，又能在可控窗口内刷新。第 2 节的单飞重建正是这套机制的前半部分。

### 与后续阶段的呼应

- 本讲的降级回源与限流，在 /microservice/resilience 会被形式化为熔断器状态机与重试预算；命中率、淘汰数、 latency 在 /microservice/metrics 里变成 RED 指标与告警规则。
- 带 token 的最小锁只是初探，完整的看门狗续期与 Redlock 争议在 /distributed/distributed-lock；「短暂不一致、最终一致」的判断框架在 /distributed/cap-base，跨系统原子性在 /distributed/distributed-tx。
- L10 的 /projects/net-shortlink 是「读多写少、能容忍秒级不一致」的标准缓存受益者；/projects/micro-seckill 则相反——热点账户与预扣减要求强一致，本讲「盲目加缓存」的警告在那里是正确性底线。

缓存上线前先想清楚失败路径：Redis 全部不可用时，接口是「降级回源」还是「直接失败」？对大多数业务，降级回源换取可用性更合理，但必须配合限流，否则缓存一挂、数据库立刻被放大数倍的流量打死，这被称为缓存雪崩的第二种形态。

<Compare title="写操作之后，该更新缓存还是删除缓存" :panels='[{"label":"反模式 · 写后更新缓存","code":"writeDB(row)\nrdb.Set(key, newValue, ttl)","note":"两个并发写可能乱序，缓存里留下旧值；计算结果型缓存还可能白白重算"},{"label":"正解 · 写后删除缓存","code":"writeDB(row)\nrdb.Del(key)","note":"下次读自然回源重建，天然避开并发写乱序，且与 Cache-Aside 读路径一致"}]' />

<FlowTimeline title="一次带缓存的读请求" :steps='[{"title":"查缓存","desc":"命中且非空值标记则直接返回","tone":"blue"},{"title":"判断未命中","desc":"区分 redis.Nil 与 Redis 故障，后者降级","tone":"purple"},{"title":"单飞回源","desc":"同 key 只放一个请求查库","tone":"orange"},{"title":"回填","desc":"写缓存并叠加 TTL 抖动","tone":"green"},{"title":"返回","desc":"空结果写短 TTL 空值，防后续穿透","tone":"gray"}]' direction="horizontal" />

还有几个反模式：**给所有 key 设同一个固定 TTL**（等价于给自己安排一次整点雪崩）；**永不过期 + 不主动更新**（数据改了缓存永远不刷新）；**把缓存当数据库用**（Redis 重启或淘汰策略会丢数据，业务必须能回源重建）；**缓存大对象或大集合**（单 key 几 MB，网络与序列化成本抵消收益）；以及前面点名的**盲目加缓存**。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-067-redis`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-067-redis)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 Cache-Aside 补上空值缓存" task="现有读路径在查库为空时直接返回 404，导致攻击者用大量不存在的 id 反复穿透。请补全分支，要求挡住穿透但又不长期占用内存。" :hints='["写一个与业务值不冲突的哨兵字符串","空值缓存的 TTL 要比正常数据短得多，比如 60 秒","命中缓存时也要判断哨兵，返回 404 而不是把哨兵当名字"]' solution="name, ok := queryDB(id)\nif !ok {\nrdb.Set(ctx, key, emptyMark, emptyTTL)\nreturn notFound\n}\nrdb.Set(ctx, key, name, ttlWithJitter(baseTTL))\nreturn name" explain="空值缓存是穿透最经济的解法：短 TTL 避免内存浪费，也避免「刚查不到、随后数据被创建」时长时间读不到新值。高量场景可再加布隆过滤器。" />

## 随堂检测

<Quiz no="67-1" q="某热点 key 过期瞬间，上千并发请求同时回源查同一条数据，这属于哪个问题？" :options='["缓存穿透","缓存击穿","缓存雪崩","双写不一致"]' answer="1" explain="单个热点 key 失效导致并发回源是击穿；穿透是查不存在的数据，雪崩是一批 key 或整个 Redis 层失效。" />

<Predict no="67-2" q="所有缓存 key 都设置为「创建后 5 分钟过期」，服务在整批预热后运行，最可能发生什么？" :options='["命中率稳定，没有异常","约 5 分钟后大量 key 同时失效，数据库流量骤增","Redis 内存持续增长直至 OOM","部分 key 永不过期"]' answer="1" output="预热后约 5 分钟出现集中的失效尖峰，回源 QPS 瞬间抬升" explain="同一时刻批量写入 + 固定 TTL = 集体过期，这是最典型的雪崩成因；解法是给 TTL 加随机抖动。" />

## 一页纸速记

<KnowledgeCard :points='["Cache-Aside：读时回填、写时删除，缓存是副本不是数据源","穿透查不存在的数据，用空值缓存与布隆过滤器；击穿是单热点，用单飞重建","雪崩是一批 key 集体失效，用 TTL 抖动、多级缓存与高可用兜住","写后删缓存优于写后更新；分布式锁要有唯一 token、TTL 与 Lua 原子释放"]' :pitfalls='["盲目给写多读少或强一致的数据加缓存","所有 key 用同一个固定 TTL，自己制造整点雪崩","缓存故障时直接返回错误，回源流量把数据库打死","释放锁时不比对 token，误删他人的锁","把 Redis 当持久化数据库用"]' summary="加缓存是拿一致性换性能的交易；先确认业务能接受这笔交易，再设计失效与降级路径。" />

## 小结与下一讲

- 三大问题按「数据是否存在 / 一个 key 还是一批 key」区分，对策不能混用。
- Cache-Aside 的标准写路径是「先更新库、再删缓存」，一致性更强要靠 CDC 订阅 binlog。
- TTL 抖动、单飞重建、空值缓存、降级回源，是把缓存从「能跑」做到「能扛」的四块拼图。

缓存解决的是读的压力，而服务之间的解耦、削峰与异步化，需要另一个组件。下一讲进入消息队列与可靠投递。

[下一讲：消息队列：Kafka / NATS / 可靠投递](/server/mq)
