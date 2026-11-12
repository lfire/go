---
title: 第 084 讲 · 分布式锁、幂等与去重
description: 用租约、令牌与幂等键守住并发下的数据正确性，并直面 Redlock 争议
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★★☆" time="50 分钟" goal="在并发与故障下守住数据正确性，能说清每种方案中途失败会怎样" prereq="CAP / BASE 与一致性模型" />

# 第 084 讲 · 分布式锁、幂等与去重

> 同一笔订单被扣款两次、同一张优惠券被两个人领走，往往不是逻辑写错了，而是「超时重试」撞上了「没有防护」。分布式锁解决的是同一时刻只允许一个执行者进入临界区，幂等解决的是同一操作被重放多次仍只产生一份结果。两者都绕不开一个问题：如果持锁者加到一半崩了、如果业务还没跑完租约就到期、如果客户端超时后重发，系统会变成什么样？本讲把这些中途失败逐一拆开。

## 这一讲你会得到什么
<CheckList id="goal-84" title="本讲目标" :items='["用 SETNX 加租约实现互斥，并用唯一令牌防误删","说清看门狗续期解决了哪种中途失败","复述 Redlock 争议的核心分歧与适用边界","设计幂等键与去重表状态机，让重试不再重复扣款"]' />

## 1. 先建立直觉
<ConceptDiagram type="lock" title="分布式锁：租约、令牌与续期三件套" />

把锁想成健身房更衣室的手环：柜子只有一把钥匙，谁拿着令牌谁用。但手环丢了（客户端进程崩溃）不能让柜子永久锁死，于是给手环加「有效 30 分钟」的租约；用超时的人从外面回来，也不能拿旧手环去开别人正在用的柜子，所以开柜前必须核对令牌是不是自己的。

对应到 Redis，一次正确的加锁要同时做三件事：**仅当键不存在时写入**（互斥）、**写入一个唯一值**（令牌，证明锁是谁的）、**给键设置过期时间**（租约，保证持有者崩溃后能自愈）。三者缺一，都会在中途失败时出事：只加互斥不加租约，持锁者崩了锁就永远不释放；加了租约不带令牌，A 超时后 B 拿到锁，A 恢复时会把 B 的锁删掉；带令牌但释放时不比对，等于白带。

顺着「面向故障设计」先把四种失败摆出来：进程崩溃、业务耗时超过租约、旧持有者从卡顿中恢复、客户端超时重发。后面每个机制，都是为其中一种失败准备的。

## 2. 语法与规则

先看 Redis 侧的标准写法，再落到一段可独立运行的 Go 模拟实现。

```bash
# 加锁：仅当键不存在时写入令牌，并设置 30 秒租约
SET lock:order:1001 $token NX EX 30
# 看门狗在租约过半时续期，前提是令牌仍属于自己
EXPIRE lock:order:1001 30
```

释放不能用简单的 `DEL`，而要用一段原子脚本先比对令牌、一致才删除：

```text
-- 释放锁：先比对 token，一致才删除，避免误删他人的锁
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
```

下面用内存 map 完整模拟 `SET key value NX EX` 的语义，重点看「中途失败会怎样」：

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// leaseLock 用内存 map 模拟 Redis 的 SET key value NX EX 语义
type leaseLock struct {
	mu     sync.Mutex
	holder map[string]holder
}

// holder 是锁记录：令牌证明归属，过期时间实现租约
type holder struct {
	token  string
	expire time.Time
}

func newToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Acquire 仅在键不存在或已过期时写入，返回本次持有的令牌
func (l *leaseLock) Acquire(key string, ttl time.Duration) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if h, ok := l.holder[key]; ok && time.Now().Before(h.expire) {
		return "", false // 锁仍被别人持有
	}
	t := newToken()
	l.holder[key] = holder{token: t, expire: time.Now().Add(ttl)}
	return t, true
}

// Release 先比对令牌再删除，模拟上面那段 Lua 的原子语义
func (l *leaseLock) Release(key, token string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.holder[key]
	if !ok || h.token != token {
		return false // 令牌不匹配，说明锁已易主，绝不能删
	}
	delete(l.holder, key)
	return true
}

func main() {
	l := &leaseLock{holder: make(map[string]holder)}
	key := "order:1001"

	tokA, ok := l.Acquire(key, 200*time.Millisecond)
	fmt.Println("A 加锁:", ok)

	_, ok = l.Acquire(key, 200*time.Millisecond)
	fmt.Println("B 并发加锁:", ok, "(被拒绝)")

	// 模拟 A 业务卡顿超过租约：锁到期后 B 能拿到锁
	time.Sleep(250 * time.Millisecond)
	tokB, ok := l.Acquire(key, 200*time.Millisecond)
	fmt.Println("租约过期后 B 加锁:", ok)

	// A 恢复后试图释放，令牌不匹配，不会误删 B 的锁
	fmt.Println("A 用旧令牌释放:", l.Release(key, tokA), "(被拒绝)")
	fmt.Println("B 用自己的令牌释放:", l.Release(key, tokB))
}
```

<OutputBox title="运行结果" output="A 加锁: true\nB 并发加锁: false (被拒绝)\n租约过期后 B 加锁: true\nA 用旧令牌释放: false (被拒绝)\nB 用自己的令牌释放: true" />

关键在最后两行：A 拿着旧令牌回来，Release 返回 false，B 的锁安然无恙。这就是令牌（也常被称作 fencing 的雏形）把「锁易主」这一失败挡在门外的过程。

<LineByLine code="type holder struct {\ntoken string\nexpire time.Time\n}" :notes='["锁记录用结构体表达，一条记录对应一个资源键","token 保存持有者的唯一值，是释放时比对的凭据","expire 是租约到期时刻，让崩溃的持有者能自动让出锁","结构体结束，Acquire 与 Release 都围绕这两个字段判断"]' />

可租约还有一个绕不开的问题：业务到底要跑多久，事先往往估不准。估短了，锁会在临界区中途过期，第二个执行者照样进来。看门狗就是为此存在的——只要业务还活着，就周期性把租约续长。

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// keeper 模拟看门狗：业务未结束就持续把租约续长
type keeper struct {
	mu      sync.Mutex
	expires time.Time
	ttl     time.Duration
}

// renew 续期；若锁已经丢失，续期无意义，必须让业务中止
func (k *keeper) renew() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if time.Now().After(k.expires) {
		return false
	}
	k.expires = time.Now().Add(k.ttl)
	return true
}

func (k *keeper) remaining() time.Duration {
	k.mu.Lock()
	defer k.mu.Unlock()
	return time.Until(k.expires)
}

// watchdog 每 ttl/3 续一次，业务结束或续期失败即退出
func (k *keeper) watchdog(ctx context.Context, done chan<- string) {
	ticker := time.NewTicker(k.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- "业务结束，停止续期"
			return
		case <-ticker.C:
			if !k.renew() {
				done <- "续期失败：锁已丢失，立即中止业务"
				return
			}
		}
	}
}

func main() {
	k := &keeper{ttl: 300 * time.Millisecond, expires: time.Now().Add(300 * time.Millisecond)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go k.watchdog(ctx, done)

	// 模拟一个耗时远超初始租约的业务
	time.Sleep(700 * time.Millisecond)
	fmt.Println("业务执行 700ms 后锁仍有效:", k.remaining() > 0)
	cancel()
	fmt.Println(<-done)
}
```

看门狗最容易被漏掉的一点是：续期失败时不能假装无事发生。一旦续不上，说明锁可能已经易主，正确做法是立刻中止业务并回滚，而不是继续写数据。`remaining() > 0` 这个断言在业务跑满 700 毫秒后依然成立，正是续期在起作用。

## 3. 深入一步

把前面所有机制按「中途失败」重新排一遍，就能看懂它们各自的位置：

| 故障场景 | 若没有防护会怎样 | 需要的机制 |
| --- | --- | --- |
| 持锁者进程崩溃 | 锁永不释放，后续请求全部阻塞 | 租约 TTL |
| 业务耗时超过 TTL | 锁提前过期，第二个执行者进入临界区 | 看门狗续期 |
| 旧持有者从卡顿中恢复 | 误删新持有者的锁，连锁破坏 | 令牌原子比对 |
| 客户端超时后重发 | 同一操作被执行两次 | 幂等键与去重表 |
| 长 GC 停顿或时钟跳跃 | 误以为仍持锁，出现双主 | fencing token |

<Term word="fencing token" text="单调递增的围栏令牌，每次加锁加一；下游存储拒绝更小令牌的写入，从根上挡住过期持有者。" />
<Term word="去重表" text="以幂等键为唯一索引记录处理结果的表，用状态机区分处理中、已完成与失败，重复请求直接回放。" />

关于 **Redlock 争议**，必须讲清分歧而不是站队。Redlock 的做法是：部署多个彼此独立的 Redis 实例，客户端依次向它们申请锁，若在租约有效期内拿到多数派且总耗时小于 TTL，就认定加锁成功。Martin Kleppmann 的批评集中在两点：它依赖各节点的本地时钟与「耗时小于 TTL」这一假设，而进程长 GC 停顿、时钟跳跃、网络延迟都可能让客户端误以为自己仍持有锁；同时它没有 fencing token，即便出现双主，下游存储也无法识别并拒绝旧持有者的写入。antirez 的回应是这些场景可以通过规整时钟、控制停顿、附加围栏来缓解。

<Callout type="warn" title="Redlock 的真实边界" text="对效率型场景（避免重复计算、减少重复发通知），Redlock 够用；对正确性不容有失的场景（资金、库存、选主），不要把它当强一致锁。更稳的是带租约与顺序令牌的系统，或让业务本身幂等，把兜底放在存储层。" />

换言之，Redlock 争论的本质仍是「中途失败会怎样」：当客户端与锁服务之间出现一段谁也说不清的停顿，方案有没有能力让系统保持互斥。没有 fencing token，答案就是不确定的。

### 从「进程暂停」推导租约、看门狗与 fencing token

锁的三件套不是并列的最佳实践，而是被同一条时间线逼出来的。设加锁时刻 `t0`、租约 `TTL`、临界区执行耗时 `T`：

- 若 `T` 有上界且 `T` 小于 `TTL`，一次加锁就够了，不需要看门狗。
- 现实中 `T` 没有上界（GC 停顿、网络阻塞、下游慢），于是出现 `T` 大于 `TTL`：客户端 A 在租约到期之后仍在临界区里干活，而锁服务已经把锁判给 B。此时 A 和 B 同时自认为持锁——**互斥被时间打破，而不是被并发打破**。
- 看门狗把这条不等式改成「只要业务活着就持续把到期时刻往后推」，使 `T` 始终小于当前租约到期时刻。但看门狗本身跑在客户端进程里：一旦进程整体停顿（GC、虚拟机挂起），看门狗也停了，续期失败与业务暂停是同一个原因。所以第 2 节强调「续不上就立刻中止业务」，因为此时无法区分「我还在临界区」和「锁已经易主」。
- 令牌比对只解决「别误删别人的锁」，解决不了「旧持有者仍在往下游写」。A 恢复后，只要不调用 `DEL`，它的写请求照样能到达存储层。fencing token 补的就是这一刀：锁服务在每次加锁时发一个单调递增的序号，A 拿到 33、B 拿到 34，下游存储记住见过最大序号，凡是带着比 34 更小的令牌的写入一律拒绝。

<SequenceDiagram title="租约过期：令牌与 fencing token 各挡住什么" caption="令牌挡住误删锁，fencing token 挡住过期持有者仍向下游写入" :actors='["ClientA","Redis","ClientB","Storage"]' :steps='[{"from":"ClientA","to":"Redis","label":"加锁 取 NX EX","kind":"call"},{"from":"Redis","to":"ClientA","label":"返回 fencing 33","kind":"return"},{"from":"ClientA","to":"Storage","label":"写 带令牌 33","kind":"call"},{"from":"ClientA","to":"ClientA","label":"GC 停顿 30 秒","note":"看门狗同时停摆","kind":"self"},{"from":"ClientB","to":"Redis","label":"租约到期 抢锁","kind":"call"},{"from":"Redis","to":"ClientB","label":"返回 fencing 34","kind":"return"},{"from":"ClientB","to":"Storage","label":"写 带令牌 34","kind":"call"},{"from":"Storage","to":"Storage","label":"记录最大 34","kind":"self"},{"from":"ClientA","to":"Storage","label":"恢复后写 33","kind":"call"},{"from":"Storage","to":"ClientA","label":"拒绝 令牌过期","note":"fencing 生效","kind":"return"},{"from":"ClientA","to":"Redis","label":"旧令牌释放被拒","kind":"call"},{"from":"Redis","to":"ClientA","label":"令牌不匹配","kind":"return"}]' />

### 结论成立的前提与失效边界

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| 租约让崩溃的持有者自动让出锁 | 锁服务与客户端的时钟推进速率大致一致 | 客户端本地时钟跳跃会让 TTL 计算失准；Redis 的 EX 用服务端时间，比客户端自算稳健，但客户端「我还有多久」的判断仍不可靠 |
| 令牌比对能防误删 | 释放动作是读令牌、比对、删除的原子操作 | 拆成 GET 再 DEL 两条命令就不是原子：A 读到自己的令牌、B 恰好抢到锁、A 再 DEL 会删掉 B 的锁，所以必须用 Lua 脚本或事务 |
| 看门狗能让临界区不中断 | 续期线程与业务线程共享同一进程生命周期 | 进程整体停顿（长 GC、虚拟机挂起、宿主机冻结）时二者一起停，看门狗无法自救 |
| Redlock 的多数派足够安全 | 各节点时钟可信、进程停顿有界、总耗时小于 TTL | 这三条正是 Kleppmann 质疑的假设；只要出现长停顿，客户端就会误以为仍持锁 |
| fencing token 能挡住旧持有者 | 下游存储愿意校验并记住最大令牌 | 若下游只是普通 MySQL 且没有令牌列，或客户端根本不带令牌，围栏就形同虚设 |

还要明确一条范围：**本讲讨论的是崩溃-停止故障（节点可能卡住、崩溃、重启），不包含拜占庭故障**——不讨论锁服务故意返回错误的令牌、或客户端伪造序号。若把恶意节点纳入模型，需要的就不是租约与围栏，而是带签名的共识协议了。同理，Redis 单实例的锁只提供「效率型互斥」，主从异步复制下的故障转移会丢掉已写入的锁键，这一点与第 088 讲 Raft 的「多数派才提交」形成对照。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| 带了唯一令牌，锁就绝对安全了 | 令牌只防误删，不防旧持有者继续写；A 恢复后不调用释放、直接写下游，令牌比对根本没被触发 | 令牌是释放校验，fencing token 才是写入拦截，二者解决不同失败点 |
| 看门狗续期成功就说明锁还在自己手里 | 续期成功只说明那一刻 Redis 里的键还匹配；紧接着网络分区或主从切换，锁可能已经易主而客户端还不知道 | 续期失败必须中止业务；续期成功也不能作为放心写下游的充分条件，正确性最终要落在存储层的幂等或围栏上 |
| Redlock 有争议，所以凡是多实例锁都不能用 | 争议针对的是把它当强一致正确性锁，不是否定它在效率场景的价值 | 避免重复计算、减少重复通知这类效率场景 Redlock 够用；资金、库存、选主要用带租约与顺序令牌的系统，或让业务幂等兜底 |

## 4. 工程实践：幂等键与去重表

锁只能约束「同时」，约束不了「先后重放」。客户端超时后重发、消息队列重复投递、上游重试，都会让同一个业务意图到达两次。工程上的通用解法是给每次操作一个幂等键，配一张带唯一索引的去重表。

幂等键一般由「业务域 + 业务唯一标识 + 操作类型或请求号」拼成，例如 `pay:order-2026:req-9527`。要区分两种粒度：请求幂等（同一请求重放返回同一结果）与业务幂等（不同请求但同一业务约束，例如一张订单只允许扣款一次，靠数据库唯一索引兜底）。

<CodeTabs title="Redis 侧一把锁的三个动作" :tabs='[{"label":"加锁","code":"SET lock:order token NX EX 30","note":"NX 保证互斥，EX 给锁一个可自愈的租约"},{"label":"续期","code":"EXPIRE lock:order 30","note":"看门狗在过期前重置 TTL，前提是令牌仍匹配"},{"label":"释放","code":"EVAL lua 1 lock:order token","note":"用脚本原子比对令牌再删除，杜绝误删"}]' />

下面用状态机把去重表跑一遍，注意「处理中」这一状态如何承接中途失败：

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

type status int

const (
	statusInit status = iota
	statusDoing
	statusDone
)

// record 是去重表里的一条记录，state 构成状态机
type record struct {
	state  status
	result string
}

type idempotent struct {
	mu     sync.Mutex
	table  map[string]*record
	charge func(key string) string // 真实扣款，只允许执行一次
	called int
}

// Handle 用幂等键去重：已完成直接回放，处理中拒绝并发，未开始才执行
func (s *idempotent) Handle(key string) (string, error) {
	s.mu.Lock()
	r, ok := s.table[key]
	if ok {
		switch r.state {
		case statusDone:
			s.mu.Unlock()
			return r.result, nil // 重复请求回放上次结果
		case statusDoing:
			s.mu.Unlock()
			return "", errors.New("处理中，请稍后重试")
		}
	} else {
		r = &record{state: statusDoing}
		s.table[key] = r
	}
	s.mu.Unlock()

	// 临界区外执行业务：这里可能中途失败，状态机要能承接
	res := s.charge(key)
	s.called++

	s.mu.Lock()
	r.state = statusDone
	r.result = res
	s.mu.Unlock()
	return res, nil
}

func main() {
	s := &idempotent{
		table:  make(map[string]*record),
		charge: func(key string) string { return "扣款成功:" + key },
	}
	key := "pay:order-2026:req-9527"
	r1, _ := s.Handle(key)
	r2, _ := s.Handle(key) // 超时后的重试
	r3, _ := s.Handle(key) // 客户端再次重放
	fmt.Println("首次:", r1)
	fmt.Println("重试:", r2)
	fmt.Println("重放:", r3)
	fmt.Println("真实扣款执行次数:", s.called)
}
```

三次调用，真实扣款只执行一次，其余两次回放同一结果。若业务在 `statusDoing` 阶段崩了，记录会卡在处理中，工程上要么靠定时任务回查真实支付结果后推进到完成，要么给处理中记录也设超时重置，绝不能让请求永远卡死。

<Compare title="两种加锁方式的故障表现" :panels='[{"label":"朴素 SETNX","code":"SETNX key 1","note":"无过期时间，持锁者崩溃后锁永远不释放，后续请求全阻塞"},{"label":"租约加令牌","code":"SET key token NX EX 30","note":"带租约可自愈、带令牌可校验，崩溃与误删都有兜底"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"加锁成功","desc":"拿到令牌与 30 秒租约，进入临界区","tone":"blue"},{"title":"业务未完","desc":"租约将到期，看门狗在后台续期","tone":"blue"},{"title":"客户端超时","desc":"上游判定失败并重发同一请求","tone":"red"},{"title":"幂等命中","desc":"去重表按幂等键回放结果，不重复扣款","tone":"green"},{"title":"释放锁","desc":"比对令牌后原子删除，锁正常归还","tone":"green"}]' />

真实系统里，锁与幂等是两层防线：锁挡并发，幂等挡重放，数据库唯一索引是最后一道兜底。三者叠加，才能在超时、崩溃、重试同时发生时仍守住「只执行一次」。

### 可验证实验：redis-cli 复现误删，go test -race 验证看门狗

先用真实 Redis 把「不带令牌会误删」复现出来，再用竞态检测器确认看门狗并发续期没有数据竞争：

```bash
# 终端 A：加一把 3 秒过期的锁
redis-cli SET lock:order:1001 token_A NX EX 3
# 终端 B：等锁过期后抢到锁
sleep 4 && redis-cli SET lock:order:1001 token_B NX EX 30
# 终端 A：用朴素 DEL 释放，没有比对令牌
redis-cli DEL lock:order:1001
# 查看结果：B 的锁已被 A 删掉
redis-cli GET lock:order:1001
# 用竞态检测器跑看门狗续期测试
go test -race -run TestWatchdogRenew -v ./...
```

```text
$ redis-cli SET lock:order:1001 token_A NX EX 3
OK
$ redis-cli SET lock:order:1001 token_B NX EX 30
OK
$ redis-cli DEL lock:order:1001
(integer) 1
$ redis-cli GET lock:order:1001
(nil)
$ go test -race -run TestWatchdogRenew -v ./...
=== RUN   TestWatchdogRenew
--- PASS: TestWatchdogRenew (0.00s)
PASS
ok      example.com/lock    0.284s
```

逐字段解读：第一行 `OK` 是 `SET ... NX` 在键不存在时写入成功；第二行 `OK` 说明 3 秒租约已到期、`NX` 再次成立，B 合法抢到锁；第三行 `(integer) 1` 是 `DEL` 删除了 1 个键——问题恰恰在这里，它删的是 **B 的锁**，因为朴素 `DEL` 不比对令牌；第四行 `(nil)` 证实锁已消失，此时第三个客户端 C 又能抢到锁，互斥彻底被打破。改用第 2 节那段 Lua 脚本后，这一步会返回 `0` 且键仍是 `token_B`。`go test -race` 的 `PASS` 说明 `keeper.renew()` 与 `remaining()` 对 `expires` 的读写都由 `sync.Mutex` 保护，没有 `WARNING: DATA RACE`；同样要注意 **`-race` 只能证明内存访问无竞争，证明不了「锁没易主」**，后者要靠令牌语义与下游围栏保证。

<Quiz q="fencing token 主要用来挡住哪一种失败？" :options='["防止客户端误删他人的锁","阻止过期持有者恢复后继续向下游写入","缩短锁的租约时间","加快加锁速度"]' answer="1" explain="令牌比对只防误删，fencing token 靠单调递增序号让下游存储拒绝旧持有者的写入，解决的是进程停顿后仍向下游写这一失败点。" />

### 失败矩阵与演练手段

| 故障注入 | 没有防护的后果 | 防护机制 | 演练命令或手段 | 回滚动作 |
| --- | --- | --- | --- | --- |
| `kill -9` 持锁进程 | 锁永不释放，后续全阻塞 | 租约 TTL | `docker kill` 后观察 `TTL` | 等到期自动恢复，无需人工 |
| 注入 200ms STW 或 `SIGSTOP` | 业务超过 TTL，第二执行者进入 | 看门狗加 fencing | `kill -STOP` 冻结进程数秒 | 旧持有者恢复后由存储层拒绝其写入 |
| 客户端超时重发同一请求 | 扣款两次 | 幂等键加去重表 | 用 `vegeta` 重放同一幂等键 | 回放已完成结果 |
| Redis 主从故障转移 | 异步复制丢锁键，出现双持锁 | 不用单实例锁做正确性，改 etcd 或唯一索引 | chaos-mesh 杀主节点 | 业务幂等兜底，人工对账 |
| 释放命令拆成 GET 再 DEL | A 误删 B 的锁 | Lua 原子脚本 | 并发压两个释放者 | 重新抢锁，检查令牌 |

演练要点：先在测试环境用 `SIGSTOP` 制造「进程还活着但不推进」的停顿，这是唯一能同时骗过租约、看门狗与客户端超时的故障；再叠加 `kill -9` 与主从切换，确认每一层兜底都真的被触发。回滚的核心不是「把锁删掉」，而是让业务重放时仍只产生一份结果——也就是把正确性放在存储层而不是锁服务上。

### 替代路线与取舍

| 路线 | 互斥依据 | 故障时 | 代价 | 适合 |
| --- | --- | --- | --- | --- |
| Redis 单实例加租约加令牌 | 单键 NX | 主从切换可能丢键 | 实现最轻、延迟低 | 效率型去重、缓存重建 |
| etcd 租约加 CAS 加顺序令牌 | Raft 多数派 | 少数派不可用，但不会双持锁 | 每次加锁一次共识往返，吞吐低 | 选主、强正确互斥 |
| 数据库唯一索引或乐观锁 | 存储层约束 | 数据库不可用即不可用 | 无额外组件，但锁粒度受表结构限制 | 扣款、库存等本就要落库的业务 |
| 不做锁，只做幂等 | 业务键唯一 | 并发都执行，但结果只有一份 | 要容忍白跑几次，不能挡副作用 | 通知、计数、可重放任务 |

取舍的关键判断：如果临界区里的动作**本身无法回滚**（发短信、调外部支付），就不能只靠「锁加重试」，必须让存储层具备拒绝重复的能力。反过来，如果业务天然可重放，去掉锁、只留幂等往往比维护一套锁服务更稳。

### 与后续实战的衔接

- [分布式任务调度系统](/projects/concurrent-scheduler) 的「分片与幂等」直接复用本讲：Master 用 etcd 租约选主，Worker 用幂等键保证任务重放不重复执行，正是「锁挡并发、幂等挡重放」两层防线的组合。
- [KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async) 的「锁分片」会把单把全局锁拆成按 key 分片的细粒度锁，思路源自本讲对互斥粒度的讨论；其「一致性校验」对应 fencing 在存储层的落地。
- [秒杀与高并发库存](/projects/micro-seckill) 在高并发下会放弃分布式锁、改用预扣减加唯一索引兜底，正好是上表「不做锁，只做幂等」那条路线的极端化。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-084-distributed-lock`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-084-distributed-lock)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="给锁补上过期判断" task="为什么 SETNX 必须同时设置过期时间？请写出锁记录里用于判断过期的字段，并说明持锁者进程崩溃时会怎样。" :hints='["崩溃后没有任何人能主动调用释放","锁记录需要保存一个到期时刻"]' solution="给每条锁记录加 expire 字段：\ntype holder struct {\ntoken string\nexpire time.Time\n}\n再在 Acquire 里用 time.Now().Before(h.expire) 判断锁是否仍有效" explain="没有租约，持锁者崩溃后键永远存在，所有后续请求都被拒之门外；有了过期时刻，Acquire 发现记录已过期即可让新请求抢占，故障自动愈合。" />

## 随堂检测
<Quiz no="084-1" q="A 加锁后业务卡顿超过 TTL，B 拿到锁；A 恢复后用旧令牌释放，正确实现应返回什么？" :options='["true，锁被删除","false，令牌不匹配拒绝删除","报错并让 B 的锁续期","直接覆盖 B 的令牌"]' answer="1" explain="释放前必须原子比对令牌，旧持有者的令牌与当前锁不一致，应拒绝删除，否则会误删 B 的锁。" />
<Predict no="084-2" q="Redlock 最主要的争议点是什么？" :options='["加锁太慢","依赖时钟且缺少 fencing token，长停顿下可能出现双主","只支持单实例","不支持设置过期时间"]' answer="1" output="依赖时钟且缺少 fencing token，长停顿下可能出现双主" explain="GC 停顿或时钟跳跃会让客户端误以为仍持锁，而没有围栏令牌时下游无法拒绝旧持有者的写入。" />
<FillBlank no="084-3" tpl="释放分布式锁必须先比对 ___ 再删除；给锁设置租约是为了防止持锁者崩溃后 ___。" :answers='["令牌~token","死锁"]' hint="唯一值与过期时间各自解决一种故障" explain="令牌防误删、租约防死锁，两者都是对中途失败的兜底。" :explains='["加锁时写入随机唯一值，释放前先比对该值再删，可避免 A 的锁超时被 B 持有后，A 醒来误删掉 B 的锁。","锁带上过期时间，持锁者崩溃或网络分区时由租约自动释放；否则这把锁永不归还，其他等待者会一直阻塞。"]' />

## 一页纸速记
<KnowledgeCard :points='["加锁三件套：仅不存在时写入、唯一令牌、过期租约","释放必须先比对令牌再删除，用脚本保证原子","看门狗续期应对业务超时，续不上就中止业务","幂等键加去重表状态机，让重试只产生一份结果"]' :pitfalls='["用 SETNX 却不设过期时间，持锁者崩溃即死锁","Redlock 用在资金与选主场景，长停顿下无法保证互斥","去重表缺少处理中状态，崩溃后请求永久卡住"]' summary="锁挡并发、幂等挡重放，每层都要回答中途失败会怎样。" />

## 小结与下一讲

- 正确的分布式锁 = `SET NX EX` + 唯一令牌 + 原子比对释放，租约让崩溃自愈。
- 看门狗解决业务耗时超过租约；Redlock 因依赖时钟且缺少 fencing token，不宜用于强正确场景。
- 幂等键配合去重表状态机与唯一索引，是超时重试下「只执行一次」的最后保障。

锁与幂等解决的是单个资源上的互斥，可一旦一次业务要跨多个服务、多个库同时改数据，就需要事务层面的方案了。

[下一讲：分布式事务：2PC / TCC / Saga / 消息最终一致](/distributed/distributed-tx)
