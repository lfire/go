---
title: 第 96 讲 · GC 源码与真实调优案例
description: 从 gcStart 到 gcMarkDone 看懂一次 GC，掌握后台标记、辅助标记、调步算法与 GOGC/MemoryLimit 实战
---

<StageHeader stage="L9 · 资深进阶与源码" level="★★★★★" time="60 分钟" goal="从 gcStart 到 gcMarkDone 看懂一次 GC" prereq="第 33 讲 · 垃圾回收：三色标记、屏障与 GOGC" />

# 第 96 讲 · GC 源码与真实调优案例

> 第 33 讲讲过三色标记与混合写屏障的算法，但「什么时候触发」「谁在后台标记」「为什么分配快了 GC 反而更忙」这些问题的答案都在源码里。本讲沿 gcStart 到 gcMarkDone 走一遍一次完整回收，再落到 GOGC 与 MemoryLimit 的真实调优：给出基线、改动、量化对比和验证手段。所有源码位置以 Go 1.23.8 为准，结论都能用 `GODEBUG=gctrace=1` 与 `ReadMemStats` 复现。

## 这一讲你会得到什么
<CheckList id="L9-96" title="本讲目标" :items='["说出一次 GC 的触发条件与五个阶段", "解释后台标记 worker、辅助标记与调步算法的协作关系", "用 gctrace 与量化对比完成一次 GOGC/MemoryLimit 调优"]' />

## 1. 先建立直觉
把堆想成一座不断进货的仓库，GC 是盘点员。盘点员不会等仓库爆满才动手，而是按「上次盘点后货量增长到某个比例」提前开工；开工时只短暂封门登记（STW），之后一边让仓库照常收货、一边在货架间贴标签（并发标记）。如果收货速度太快，盘点员会临时拉几个装卸工来帮忙贴标签（辅助标记），这反过来又拖慢了收货。调步算法就是那个决定「提前多久开工、派几个人帮忙」的调度室。
<ConceptDiagram type="gc-tricolor" title="三色标记与并发回收" caption="白色待考察、灰色待扫描、黑色已完成；写屏障保证业务并发修改不丢对象" />

## 2. 语法与规则
读 GC 源码先抓两处入口与一个状态结构，再验证行为。以下均来自 Go 1.23.8。

- `src/runtime/mgc.go:629`：`func gcStart(trigger gcTrigger)`，触发类型有堆增长、周期时间与显式调用三类。
- `src/runtime/mgc.go:858`：`func gcMarkDone()`；`1008` 行 `func gcMarkTermination(stw worldStop)`，收尾的第二次 STW。
- `src/runtime/mgc.go:1377`：`func gcBgMarkWorker(ready chan struct{})`，后台标记 worker 主体。
- `src/runtime/mgcpacer.go:90`：`type gcControllerState struct`，调步算法的状态；`731` 行 `findRunnableGCWorker` 决定本轮该唤醒多少 worker。
- `src/runtime/debug/garbage.go:93 / 234`：`SetGCPercent` 与 `SetMemoryLimit` 两个运行时开关。

三段程序分别读取调步结果、对比不同 GOGC 的量化差异、以及验证 MemoryLimit。

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// 观察一：读取 GC 前后的堆与 NextGC 目标，验证调步算法算出的触发点。
func main() {
	old := debug.SetGCPercent(100)
	fmt.Println("原 GOGC =", old)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("GC 次数 =", m.NumGC)
	fmt.Println("当前堆分配 =", m.HeapAlloc)
	fmt.Println("下次触发目标 =", m.NextGC)

	const blocks = 200
	hold := make([][]byte, 0, blocks)
	for i := 0; i < blocks; i++ {
		hold = append(hold, make([]byte, 1<<20)) // 每块 1 MiB
	}
	runtime.KeepAlive(hold)

	runtime.ReadMemStats(&m)
	fmt.Println("分配后 GC 次数 =", m.NumGC)
	fmt.Println("分配后堆 =", m.HeapAlloc)
	fmt.Println("新的下次目标 =", m.NextGC)
}
```

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"
)

// 观察二：同一份分配压力在 GOGC=100 与 GOGC=400 下的 GC 次数与耗时。
var sink [][]byte

func workload() {
	for i := 0; i < 120; i++ {
		sink = append(sink, make([]byte, 1<<20))
		if len(sink) > 40 {
			sink = sink[len(sink)-20:]
		}
	}
}

func run(percent int) (int, time.Duration) {
	debug.SetGCPercent(percent)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	workload()
	elapsed := time.Since(start)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return int(after.NumGC - before.NumGC), elapsed
}

func main() {
	for _, p := range []int{100, 400} {
		n, d := run(p)
		fmt.Printf("GOGC=%d 触发 %d 次 GC，耗时 %v\n", p, n, d)
	}
}
```

```go
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// 观察三：MemoryLimit 是软上限，与 GOGC 一起约束堆目标。
func main() {
	const limit = 64 << 20 // 64 MiB
	prev := debug.SetMemoryLimit(limit)
	fmt.Println("原内存上限 =", prev)
	fmt.Println("当前内存上限 =", debug.SetMemoryLimit(-1)) // 传 -1 只查询不修改

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println("初始 GC 次数 =", m.NumGC)
	fmt.Println("下次 GC 目标 =", m.NextGC)

	var keep [][]byte
	for i := 0; i < 100; i++ {
		keep = append(keep, make([]byte, 1<<20))
		if len(keep) > 30 {
			keep = keep[len(keep)-15:]
		}
	}
	runtime.KeepAlive(keep)
	runtime.ReadMemStats(&m)
	fmt.Println("施压后 GC 次数 =", m.NumGC)
	fmt.Println("施压后堆 =", m.HeapAlloc)
}
```

<OutputBox title="第一段程序的输出示例（数值因平台而异）" output="原 GOGC = 100\nGC 次数 = 0\n当前堆分配 = 401408\n下次触发目标 = 4194304\n分配后 GC 次数 = 45\n分配后堆 = 76218368\n新的下次目标 = 149946368" note="NextGC 随存活堆上升，是调步算法重算触发点的证据。" />

<LineByLine title="一次 GC 的关键状态迁移" code="gcStart(gcTriggerHeap)\ngcBgMarkWorker(ready)\ngcAssistAlloc(gp)\ngcMarkDone()\ngcSweep(mode)" :notes='["触发：短暂 STW，切到标记阶段并开启混合写屏障", "并发标记：后台 worker 三色遍历，与业务 goroutine 同时运行", "辅助标记：分配过快时让业务 G 分担标记，反过来抑制分配速率", "标记完成：第二次 STW，关闭写屏障、统计并进入清扫", "并发清扫：回收白色 span，按需把内存归还操作系统"]' />

## 3. 深入一步
一次 GC 能把停顿压到亚毫秒级，靠三件事配合：混合写屏障让并发标记期间改指针不漏标；`gcControllerState` 按堆增长率与 GOGC 动态算出目标堆 `NextGC` 和 worker 数；`gcAssistAlloc` 把「分配太快」转成「先帮忙标记再分配」的负反馈。触发条件三类：堆达到 `NextGC`、sysmon 发现距上次 GC 约两分钟、显式调用 `runtime.GC()`。

<Term word="调步算法（pacer）" text="mgcpacer.go 里的 gcControllerState：它预测堆增长速率，决定何时启动 GC、目标堆多大、需要多少后台标记 worker，并在内存与 CPU 之间做动态平衡。" />

| 阶段 | 关键函数（Go 1.23.8） | 是否 STW | 做了什么 |
| --- | --- | --- | --- |
| 触发 | `gcStart(trigger gcTrigger)` | 短暂 | 切换到标记阶段、启用写屏障、准备 worker |
| 并发标记 | `gcBgMarkWorker` | 否 | 三色遍历，与业务并发执行 |
| 辅助标记 | `gcAssistAlloc` | 否 | 分配过快的 G 先分担标记，形成负反馈 |
| 标记收尾 | `gcMarkDone` / `gcMarkTermination` | 短暂 | 关闭写屏障、完成统计、进入清扫 |
| 并发清扫 | `gcSweep` | 否 | 回收白色 span，按需归还 OS |

<Callout type="warn" title="MemoryLimit 不是硬上限" text="它是软目标：堆逼近上限时 GC 更卖力，但存活对象真超限时仍会增长，甚至因 GC 过频拖垮吞吐。应与 GOGC 一起调，别当 OOM 保险丝。" />

### 源码精读与可复现验证（以 Go 1.21~1.23 为准，版本可能变化）

一次 GC 的源码主线在 `src/runtime/mgc.go`。`func gcStart(trigger gcTrigger)` 是唯一入口，参数 `gcTrigger` 是个三字段结构：`kind gcTriggerKind` 取值 `gcTriggerHeap`、`gcTriggerTime`、`gcTriggerCycle`，分别对应堆增长到目标、sysmon 周期性强制与显式调用；`now` 与 `n` 供时间与轮次判断。函数先 `stopTheWorld` 做第一次 STW，切到 `_GCmark` 阶段、开启混合写屏障，再按 `gcControllerState` 算出的数量唤醒 `gcBgMarkWorker`。并发标记期间，全局 `work` 结构记录进度：`work.heap0`（标记开始时堆）、`work.heap1`（标记结束时堆）、`work.bytesMarked`（已标记字节）、`work.markrootDone`（根扫描是否完成）、`work.nproc` 与 `work.nwait`（参与与等待的 worker 数）。业务 goroutine 分配过快时，`newobject` 会走 `gcAssistAlloc`，按欠下的扫描量先帮忙标记再继续分配，这就是「分配反压」的源码落点。最后一个 worker 发现标记队列清空，调用 `gcMarkDone` 进入 `gcMarkTermination` 做第二次 STW，关闭写屏障、算出下一轮 `heapGoal`，再由 `gcSweep` 并发回收白色 span。

调步与屏障分在另两个文件。`src/runtime/mgcpacer.go` 的 `gcControllerState` 用 `scanWork`、`bgScanCredit`、`assistTime`、`dedicatedMarkWorkersNeeded` 等字段记录扫描负债与 worker 预算，`findRunnableGCWorker` 决定本轮该唤醒多少 dedicated、fractional 与 idle worker，`gcController.heapGoal()` 给出目标堆。`src/runtime/mbarrier.go` 的混合写屏障在并发标记期间同时把旧值与新值标灰，保证业务改指针不漏标。一个可复现的观察实验：对第 2 节的分配程序跑 `GODEBUG=gctrace=1`，记录 `goal` 与 `MemStats.NextGC` 是否只差一个单位换算；再用 `GOGC=off` 跑同样负载，确认 `gc N` 不再增长、堆持续上涨，从而坐实「触发条件由 gcTrigger 控制」这一点。

适用边界要写清：函数名与字段以 Go 1.21~1.23 为准，gctrace 的输出格式与分段数量每版都可能微调，判断时以本机实测为准；`goal` 是目标堆而非硬上限，存活对象真的超过它时堆仍会增长；`MemoryLimit` 与 GOGC 共同作用，不能单独当 OOM 保险丝；`gcTriggerTime` 的约两分钟阈值由 sysmon 检查，长时间空闲的程序 GC 次数本来就不该拿来和压测程序对比。

一条命令把每轮 GC 的完整账单打出来：

```bash
GODEBUG=gctrace=1 ./aggregator 2> gc.log
```

```text
gc 12 @3.214s 1%: 0.018+1.1+0.017 ms clock, 0.14+0.24/1.0/0+0.14 ms cpu, 4->5->3 MB, 5 MB goal, 0 MB stacks, 0 MB globals, 8 P
gc 13 @3.502s 1%: 0.021+1.3+0.019 ms clock, 0.16+0.28/1.1/0+0.15 ms cpu, 5->6->3 MB, 5 MB goal, 0 MB stacks, 0 MB globals, 8 P
```

一行里每个数字都有落点：`gc 12` 是第 12 次回收，对应 `MemStats.NumGC`；`@3.214s` 是程序启动到本轮开始的时刻；`1%` 是自启动以来 GC 占用的 CPU 时间比例；`0.018+1.1+0.017 ms clock` 是三段墙钟耗时，依次为第一次 STW（清扫终止与标记准备）、并发标记、第二次 STW（标记终止），两段 STW 之和才是用户能感知的停顿；`0.14+0.24/1.0/0+0.14 ms cpu` 是对应的 CPU 时间，中间三项分别是 dedicated、fractional、idle 三类后台 worker 的标记开销；`4->5->3 MB` 是标记开始堆、标记结束堆与标记后存活堆；`5 MB goal` 是本轮目标堆，与 `NextGC` 是同一个目标、只是单位不同；`0 MB stacks` 与 `0 MB globals` 是栈与全局数据扫描量；`8 P` 是参与本轮标记的 P 数。排查时看两处就够：`1%` 长期偏高说明 GC 在抢 CPU，`goal` 随 GOGC 调大而抬高，正好解释第 4 节那张表里的吞吐变化。

<SequenceDiagram title="一次 GC 的状态迁移与参与者" caption="从 gcStart 触发到 gcSweep 清扫，展示两次 STW 与并发标记的衔接" :actors='["mutator","gcStart","bgMarkWorker","gcAssistAlloc","gcMarkDone","gcSweep"]' :steps='[{"from":"mutator","to":"gcStart","label":"堆达到 NextGC","note":"gcTriggerHeap","kind":"call"},{"from":"gcStart","to":"gcStart","label":"STW 开写屏障","note":"切到 _GCmark","kind":"self"},{"from":"gcStart","to":"bgMarkWorker","label":"唤醒后台 worker","note":"按预算起 worker","kind":"call"},{"from":"bgMarkWorker","to":"mutator","label":"并发三色标记","note":"业务继续运行","kind":"async"},{"from":"mutator","to":"gcAssistAlloc","label":"分配过快帮标记","note":"先还扫描负债","kind":"call"},{"from":"gcAssistAlloc","to":"bgMarkWorker","label":"分担扫描 credit","note":"抑制分配速率","kind":"return"},{"from":"bgMarkWorker","to":"gcMarkDone","label":"标记队列清空","note":"最后一个 worker","kind":"call"},{"from":"gcMarkDone","to":"gcMarkDone","label":"二次 STW 收尾","note":"关屏障算 goal","kind":"self"},{"from":"gcMarkDone","to":"gcSweep","label":"进入并发清扫","note":"回收白色 span","kind":"call"},{"from":"gcSweep","to":"mutator","label":"归还内存恢复","note":"按需还给 OS","kind":"return"}]' />

图中两次 `self` 正是两次 STW：第一次开屏障、第二次关屏障并定下一轮目标，中间的所有标记都与业务并发，这就是亚毫秒停顿的结构性来源。

| gctrace 字段 | 示例 | 含义 | 调优时看什么 |
| --- | --- | --- | --- |
| `gc N` | `gc 12` | 第 N 次回收，等于 `NumGC` | 频率是否过高 |
| `@T s` | `@3.214s` | 程序启动到本轮开始 | 相邻间隔是否骤缩 |
| `1%` | `1%` | 累计 GC 占用 CPU 比例 | 是否长期偏高 |
| `ms clock` 三段 | `0.018+1.1+0.017` | 一次 STW、并发标记、二次 STW 的墙钟时间 | 两段 STW 之和 |
| `ms cpu` 中段 | `0.24/1.0/0` | dedicated、fractional、idle worker 的标记 CPU | 后台标记是否吃满 |
| 三个堆值 | `4、5、3 MB` | 标记开始堆、结束堆、存活堆 | 存活堆与增长幅度 |
| `goal` | `5 MB goal` | 本轮目标堆，等同 `NextGC` 的 MB 值 | GOGC 调大后是否抬高 |
| `N P` | `8 P` | 参与标记的 P 数 | 与 `GOMAXPROCS` 是否一致 |

表格把一行输出拆成速查项；真正调优时不要逐行盯，而是用 `benchstat` 比较改动前后多轮采样的 `1%` 与两段 STW 之和，才不会被单次抖动误导。

<Callout type="tip" title="源码阅读建议" text="先读 mgc.go 的 gcTrigger 与 gcStart 理清触发和两次 STW，再读 mgcpacer.go 的 findRunnableGCWorker 看 worker 预算，最后才看 mbarrier.go 的写屏障；work 全局结构里的 heap0、heap1、bytesMarked 是串起各阶段的主线字段，别一上来就钻汇编屏障。" />

### 常见误读与反例

GC 的误读几乎都把「目标堆」当成「硬阈值」、把「并发」当成「没有停顿」。下面三条用 `gctrace` 一行输出与第 2 节程序就能推翻。

| 误读 | 为什么站不住（源码推导） | 正确写法或反例 |
| --- | --- | --- |
| 「GOGC=100 表示堆每涨 100 字节就 GC 一次，堆到 goal 就必须立刻停下」 | `goal`（即 `NextGC`）约等于上一轮标记结束时的存活堆乘以 `(1 + GOGC/100)`，是**相对存活堆的增长目标**，不是绝对阈值；`gcTriggerHeap` 在 `mallocgc` 里按分配量检查，`goal` 是软目标，存活对象真超过它时堆仍会继续涨，只是调步算法更早开工、派更多 worker | 第 2 段程序里 `NextGC` 随存活堆上升而抬高；`GOGC=off` 时堆无界增长、`gc N` 不再增加，正好反证触发由 `gcTrigger` 控制而非某个固定字节数 |
| 「写屏障只在 GC 开始时开一次且全程 STW，有了混合写屏障就不需要第二次 STW」 | `gcStart` 第一次 STW 切到 `_GCmark` 并开启混合写屏障，`src/runtime/mbarrier.go` 在并发标记期间同时把旧值与新值标灰，屏障一直生效到收尾；`gcMarkDone` 进 `gcMarkTermination` 做第二次 STW 才关闭屏障、算下一轮 `heapGoal` | `gctrace` 的 `ms clock` 三段 `0.018+1.1+0.017` 里，中间的 1.1 是并发标记，前后两段才是 STW，两者之和才是用户能感知的停顿；两次 STW 缺一不可 |
| 「调大 GOGC 是免费提速，GC 次数越少越好」 | GOGC 调大抬高 `heapGoal`，GC 变少、标记 CPU 让给业务，但两次 GC 之间堆积的对象更多，峰值堆成倍上涨，还会抬高页表与缓存压力，容器环境里甚至先触发 OOM | 第 4 节表里 GOGC 从 100 到 400 吞吐只涨约 18%、峰值堆从 78 MiB 涨到 268 MiB；在线服务反而要配合 `MemoryLimit` 压堆，按 `gctrace` 建基线、只改一个变量、`benchstat` 验证的闭环决策 |

<Quiz q="gctrace 一行里的 0.018+1.1+0.017 ms clock，用户能感知的停顿主要来自哪几段？" :options='["中间那段 1.1 ms 并发标记","前后两段 STW 之和","三段相加的总和","与 clock 无关，只看 goal"]' answer="1" explain="三段依次是第一次 STW、并发标记、第二次 STW；并发标记与业务同时跑，只有前后两段 STW 会暂停所有 G，二者之和才是停顿。" />

### 观测手段的能证与不能证

第 3 节给了 `gctrace` 的字段速查表，这里补上证据边界，避免把周期账单当成请求级归因。

| 手段 | 能证明什么 | 不能证明什么 |
| --- | --- | --- |
| `GODEBUG=gctrace=1` | 每轮的 `goal`、两段 STW、标记 CPU 占比、存活堆变化与 GC 频率 | 单次请求延迟就是 GC 造成的（它是周期账单，不按请求归因），也不能证明 `goal` 是硬上限 |
| `runtime.ReadMemStats` | `NumGC`、`HeapAlloc`、`NextGC` 的瞬时值 | 看不出时间分布；`ReadMemStats` 本身会触发一次 STW，高频调用反而制造停顿 |
| `GOGC=off` 对照 | 触发条件确实由 `gcTrigger` 控制 | 不能用来评估真实负载的吞吐与 OOM 风险（堆无界增长） |
| `benchstat` 对比 | 多轮采样下 `1%` 与两段 STW 之和的变化是否显著 | 单次抖动得不出结论；实验室 delta 也不能直接当线上 QPS 收益 |

一句话记边界：`gctrace` 证明「这一轮 GC 花了多少、目标堆多大」，`MemStats` 证明「此刻堆是什么状态」，两者都不证明「某次 p99 尖刺一定来自 GC」——那要靠 `go tool trace` 把 GC 事件与请求时间线对齐。

### 与 L10 源码级实战的呼应

- [实战 10 · 秒杀与高并发库存](/projects/micro-seckill)：流量削峰与预扣减会在极短时间内产生海量短命对象，GC 的 `1%` 与两段 STW 直接决定 p99 尾延迟；本讲「gctrace 建基线、只改 GOGC、benchstat 验证」正是「压测与预案」里定 GOGC 与 MemoryLimit 的方法。
- [实战 03 · 短链服务](/projects/net-shortlink)：高并发读服务把热点映射常驻缓存时，调大 GOGC 可换吞吐；一旦容器内存受限，就要加 `MemoryLimit` 压堆，避免被 OOM kill——这就是「内存换 CPU」的边界判断，不能只看吞吐一列。
- KV 贯穿线 ④ [分片、复制与 Raft](/projects/kv-4-distributed)：Raft 快照与日志复制会产生大块存活堆，`heapGoal` 抬高后 `gcAssistAlloc` 的分配反压会拖慢写入；在「成员变更与压测」阶段要用 `gctrace` 确认 GC 没有成为吞吐拐点，而不是把单机版的 GOGC 结论直接搬过来。

## 4. 工程实践
一次在线服务调优这样闭环：先用 `GODEBUG=gctrace=1` 采集基线，记录每轮 GC 频率、标记占用 CPU 比例与 `goal`；再只改一个变量，用固定压测脚本复跑；最后用 `benchstat` 对比，确认收益不是噪声。下表是某 JSON 聚合服务在同样压测下的代表性数据。

```bash
GODEBUG=gctrace=1 ./aggregator 2> gc.log
```

| 配置 | GC 次数 | 平均停顿 | 峰值堆 | 相对吞吐 |
| --- | --- | --- | --- | --- |
| 基线 GOGC=100 | 46 次 | 0.42 ms | 78 MiB | 1.00x |
| GOGC=400 | 12 次 | 0.45 ms | 268 MiB | 1.18x |
| GOGC=100 + MemoryLimit 128 MiB | 51 次 | 0.40 ms | 71 MiB | 0.97x |

结论很直白：调大 GOGC 是内存换 CPU，吞吐涨约 18%，峰值堆涨三倍多；加 MemoryLimit 压住堆，代价是 GC 更频繁、吞吐小幅下降。批任务选前者，在线服务选后者。

<Compare title="两种调优取向" :panels='[{"label":"吞吐优先","code":"debug.SetGCPercent(400)\nserve()","note":"GC 更少、CPU 让给业务，适合离线批任务，代价是常驻内存更大"},{"label":"内存可控","code":"debug.SetGCPercent(100)\ndebug.SetMemoryLimit(536870912)\nserve()","note":"堆受软上限约束、延迟平稳，适合在线服务"}]' />

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l9-advanced/lesson-096-runtime-gc`](https://github.com/lfire/go/tree/main/examples/l9-advanced/lesson-096-runtime-gc)。clone 仓库后进入 `examples/l9-advanced`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="用 gctrace 读一轮 GC" task="给第二段程序加上 GODEBUG=gctrace=1 运行，找出 goal 字段，说明它与 NextGC 的关系。" :hints='["gctrace 每行以 gc 编号开头", "goal 单位 MB，NextGC 单位字节"]' solution="runtime.GC()\nvar m runtime.MemStats\nruntime.ReadMemStats(&m)\nfmt.Println(m.NumGC, m.NextGC)\nfmt.Println(debug.SetGCPercent(-1))" explain="gctrace 的 goal 与 MemStats.NextGC 是同一个目标堆，只是单位不同。SetGCPercent(-1) 只查询不修改，可确认调优是否生效。" />

## 随堂检测
<Quiz no="96-1" q="下列哪一项不是 Go 1.23 的 GC 触发条件？" :options='["堆增长到 NextGC 目标", "距上次约两分钟由 sysmon 强制触发", "显式调用 runtime.GC()", "每创建一个 goroutine 检查一次"]' answer="3" explain="gcTrigger 只区分堆增长、周期时间与显式三类，创建 goroutine 不触发 GC。" />
<Predict no="96-2" q="把 GOGC 从 100 调到 400，同样分配压力下 GC 次数和堆占用会怎么变？" :options='["次数变多、堆变小", "次数变少、堆变大", "两者都不变"]' answer="1" output="GC 次数下降，峰值堆明显上升" explain="GOGC 调大后 NextGC 变高，GC 变少，但两次 GC 之间堆积的对象更多。" />

## 一页纸速记
<KnowledgeCard :points='["一次 GC 走 gcStart、并发标记、辅助标记、gcMarkDone、gcSweep", "调步算法动态算 NextGC 与 worker 数量", "GOGC 调大是内存换 CPU"]' :pitfalls='["把 MemoryLimit 当硬上限", "只看平均停顿", "同时改多个变量"]' summary="GC 调优是在目标堆、标记 CPU 与停顿之间做可测量的权衡。" />

## 小结与下一讲
1. `gcStart` 短暂 STW 开启写屏障，后台 `gcBgMarkWorker` 与业务并发三色标记，`gcMarkDone` 收尾再清扫。
2. 调步算法按堆增长率与 GOGC 算 `NextGC`，`gcAssistAlloc` 用分配反压保证标记跟得上。
3. 调优必须闭环：gctrace 建基线、只改一个变量、benchstat 验证，GOGC 换吞吐、MemoryLimit 控内存。

[下一讲：channel / mutex 源码与锁竞争](/advanced/runtime-sync)
