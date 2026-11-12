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
