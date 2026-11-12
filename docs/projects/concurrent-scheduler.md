---
title: 第 110 讲 · 实战 08 · 分布式任务调度系统
description: 讲透 etcd 选主、租约续期、session 失效与故障转移，并用 fencing token、分片与幂等实现高可用调度
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="实现 Master-Worker 的高可用调度" prereq="第 109 讲 · 实战 07 · 并发爬虫：单机 → 并发 → 分布式" />

# 第 110 讲 · 实战 08 · 分布式任务调度系统

> 定时任务最怕执行两次：重复扣款、重复发券补都补不回来。可为了高可用又必须跑多个 Master，于是绕不开一个问题：凭什么只有一个节点能派发？

## 这一讲你会得到什么
<CheckList id="l110-goals" title="本讲目标" :items='["说清为什么定时任务必须先选主再派发","走通 Lease、Session、Election、Watch 完整链路","解释 TTL 长短对故障转移与误判失主的影响","用 fencing token 挡住旧 leader 复活造成的脑裂"]' />

## 1. 项目目标与约束

| 维度 | 目标 | 约束与取舍 |
| --- | --- | --- |
| 唯一性 | 任意时刻只有一个节点派发 | 宁可全员停摆，也不双主 |
| 高可用 | Master 挂了自动换主 | 换主窗口允许少跑，不允许重跑 |
| 恢复 | 换主后在途任务可重派 | 语义是至少一次，必须幂等 |
| 底座 | 依赖 etcd 线性一致 | 不自己实现 Raft，见第 88 讲 |

<Callout type="tip" title="fail-closed" text="etcd 整体不可用时全员停止派发：漏跑一次可人工补，重复扣款没法补。" />

Master 负责触发、分片派发、监听选主；Worker 无状态，只负责领取、执行、回报。

## 2. 架构蓝图

<ConceptDiagram type="raft" title="etcd 之上的 Master-Worker" caption="etcd 用 Raft 保证线性一致，选主只是它的一个应用；多个 Master 抢同一把租约锁" />

跟随者一直 watch，key 一删就被唤醒，故障转移延迟约等于剩余 TTL。

## 3. etcd 选主与租约故障转移

**三块能力。** Lease 带 TTL，用 cli.Grant 创建、cli.KeepAlive 续租，到期自动删 key，这就是进程死了锁自己放。Session 打包二者，sess.Done 在续租失败时关闭，是感知失主的唯一信号。Election 用事务创建带 lease 的 key，CreateRevision 最小者当选，其余 watch 更小的 key。

<CodeTabs :tabs='[{"label":"创建会话与竞选","code":"sess, _ := concurrency.NewSession(cli, concurrency.WithTTL(10))\ne := concurrency.NewElection(sess, \"/sched/leader\")\n_ = e.Campaign(ctx, \"master-1\")","note":"NewSession 已 Grant 租约并启动 KeepAlive"},{"label":"失主与优雅退位","code":"defer e.Resign(ctx)\ndone := sess.Done()\n// done 被关闭说明续租失败，立即停止派发","note":"Resign 主动释放，跟随者不必等 TTL"}]' title="生产版 etcd 选主关键调用" />

真实代码需 go get go.etcd.io/etcd/client/v3，用到 clientv3 与 concurrency 包。

**完整链路。** 申请租约并 KeepAlive；Campaign 抢带 lease 的 key，失败者阻塞在 watch 上；循环监听 sess.Done，续租一断立刻退出；旧 leader 的 key 到期自动删除，跟随者 watch 到事件、Campaign 返回当选；e.Resign 主动删 key 可免去等 TTL。

**TTL 两难。** 过短会因 GC 停顿误判失主、频繁切主；过长则真故障要等很久才换主；折中取五到十五秒，靠 KeepAlive 保活而非调大 TTL。

**脑裂与 fencing token。** 短暂分区后旧 leader 本地仍认为自己是主，恢复后会与新的同时派发；检查一次解决不了，因为检查与派发之间有窗口。做法是派发带单调递增 token（CreateRevision 天然单调，换主 epoch 加一），写入端只接受更大的 token。

## 4. 关键模块实现

### 4.1 任务模型与稳定分片

```go
package main

import (
	"fmt"
	"time"
)

// Job 用固定周期演示；生产里把 Every 换成 robfig/cron 解析出的下次触发时间。
type Job struct {
	ID    string
	Every time.Duration
	Shard int
}

// NextRun 把触发时间对齐到整秒，避免多次调度产生毫秒级抖动。
func NextRun(j Job, now time.Time) time.Time {
	return now.Add(j.Every).Truncate(time.Second)
}

// ShardOf 用任务 ID 的字节和取模决定分片，稳定且不需要中心分配。
func ShardOf(id string, n int) int {
	sum := 0
	for i := 0; i < len(id); i++ {
		sum += int(id[i])
	}
	return sum % n
}

func main() {
	jobs := []Job{
		{ID: "report-daily", Every: 24 * time.Hour},
		{ID: "cleanup", Every: time.Hour},
		{ID: "heartbeat", Every: 5 * time.Second},
	}
	now := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	for _, j := range jobs {
		j.Shard = ShardOf(j.ID, 3)
		fmt.Printf("%-14s 分片=%d 下次=%s\n", j.ID, j.Shard, NextRun(j, now).Format("15:04:05"))
	}
}
```

分片函数必须是纯函数，同一 job ID 在任何 Master 上算出同一分片，换主才不会分丢或分重。

### 4.2 选主与租约：把 etcd 语义跑一遍

```go
package main

import (
	"fmt"
	"sync"
	"time"
)

// LeaseCoordinator 模拟 etcd 选主的核心语义：
// 谁持有未过期的租约谁才是 leader；续租失败即失去资格。
type LeaseCoordinator struct {
	mu      sync.Mutex
	holder  string
	leaseID int64
	expiry  time.Time
	epoch   int64 // fencing token：每次换主递增，旧 leader 的 epoch 作废
	nextID  int64
}

func (c *LeaseCoordinator) Campaign(node string, ttl time.Duration) (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.holder != "" && now.Before(c.expiry) {
		return 0, false // 已有存活 leader，竞选失败
	}
	c.nextID++
	c.leaseID = c.nextID
	c.holder = node
	c.expiry = now.Add(ttl)
	c.epoch++
	fmt.Printf("  %s 当选，租约=%d epoch=%d\n", node, c.leaseID, c.epoch)
	return c.leaseID, true
}

// KeepAlive 续租；只有当前 holder 能续自己的租约。
func (c *LeaseCoordinator) KeepAlive(node string, id int64, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder != node || c.leaseID != id || time.Now().After(c.expiry) {
		return false
	}
	c.expiry = time.Now().Add(ttl)
	return true
}

// IsLeader 用 fencing token 判定派发资格，挡住旧 leader 复活造成的脑裂。
func (c *LeaseCoordinator) IsLeader(node string, epoch int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder == node && c.epoch == epoch && time.Now().Before(c.expiry)
}

func main() {
	c := &LeaseCoordinator{}
	ttl := 30 * time.Millisecond
	id, _ := c.Campaign("master-1", ttl)
	_, contested := c.Campaign("master-2", ttl)
	fmt.Println("master-2 抢主是否成功:", contested)
	fmt.Println("master-1 续租:", c.KeepAlive("master-1", id, ttl))
	fmt.Println("master-1 有派发权:", c.IsLeader("master-1", 1))

	// 模拟进程卡死：停止续租，租约自然过期。
	time.Sleep(ttl + 5*time.Millisecond)
	fmt.Println("过期后 master-1 仍有派发权:", c.IsLeader("master-1", 1))
	id2, ok := c.Campaign("master-2", ttl)
	fmt.Println("master-2 重新竞选:", ok, "新租约=", id2)
	fmt.Println("master-1 用旧 epoch 派发被拒:", !c.IsLeader("master-1", 1))
	fmt.Println("master-2 用新 epoch 派发:", c.IsLeader("master-2", 2))
}
```

对照链路：首次 Campaign 成功即持有租约，第二个节点抢主失败；停止续租后 IsLeader 变假，新节点当选且 epoch 变成 2，旧 leader 拿旧 epoch 回来会被拒。

### 4.3 故障转移后的幂等派发

```go
package main

import (
	"fmt"
	"sync"
)

// Executions 记录每个任务已完成的最大 epoch，用于幂等判定。
type Executions struct {
	mu   sync.Mutex
	done map[string]int64
}

// TryRun 只接受 epoch 更大的派发；同一 epoch 或旧 epoch 的重投直接拒绝。
func (e *Executions) TryRun(job string, epoch int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if prev, ok := e.done[job]; ok && prev >= epoch {
		return false
	}
	e.done[job] = epoch
	return true
}

func RunSharded(jobs []string, shards int, ex *Executions, epoch int64, workerID string) int {
	n := 0
	for _, j := range jobs {
		shard := 0
		for i := 0; i < len(j); i++ {
			shard += int(j[i])
		}
		if shard%shards != 0 {
			continue // 本 worker 只负责 0 号分片
		}
		if ex.TryRun(j, epoch) {
			fmt.Printf("  %s 执行 %s (epoch=%d)\n", workerID, j, epoch)
			n++
		}
	}
	return n
}

func main() {
	ex := &Executions{done: make(map[string]int64)}
	jobs := []string{"report", "cleanup", "heartbeat", "sync"}
	fmt.Println("旧 leader 派发 (epoch=1):")
	RunSharded(jobs, 3, ex, 1, "worker-1")
	fmt.Println("新 leader 重派 (epoch=2):")
	RunSharded(jobs, 3, ex, 2, "worker-2")
	fmt.Println("旧 leader 复活重派 (epoch=1):")
	RunSharded(jobs, 3, ex, 1, "worker-1")
}
```

三轮派发各有含义：epoch 1 正常执行；epoch 2 允许重跑在途任务；旧 leader 复活带 epoch 1 再来被 TryRun 挡掉。

## 5. 失败路径与边界条件

Master 崩溃靠租约到期、跟随者 watch 唤醒接管；双主靠 fencing token 在写入端校验 epoch；续租断但进程还活着必须监听 sess.Done 停派；etcd 不可用一律全员停派；任务超时靠租约重投加幂等去重。检查自己是不是 leader 永远不够。

## 6. 测试与压测思路

选主单测断言只有一个 Campaign 成功、epoch 递增；故障注入模拟旧 leader 复活并断言旧 epoch 被拒；混沌演练真的杀掉 leader，量出换主恢复时间。

压测盯重派率，持续升高说明任务超时或幂等键设计有问题。

## 7. 可继续扩展的方向

把本地模拟换成 clientv3/concurrency 用真实 Watch 驱动换主，见第 88 讲；引入 robfig/cron 并统一时间源。

## 随堂检测

<Quiz no="110-1" q="为什么监听 sess.Done 比派发前检查是不是 leader 更可靠？" :options='["Done 读得更快","续租一断就持续收到信号，检查与派发之间的时间窗被消除","etcd 强制要求"]' answer="1" explain="单次检查与派发之间仍有延迟；Done 被关闭是持续信号，能让调度循环立刻停止。" />

## 一页纸速记

<KnowledgeCard :points='["Lease 加 KeepAlive 让进程死了锁自己放","跟随者靠 Watch 唤醒，换主延迟约等于剩余 TTL","fencing token 必须在写入端校验"]' :pitfalls='["只检查本地 isLeader 会留下脑裂窗口","etcd 不可用时各自派发比全员停摆更危险"]' summary="租约、watch 换主与 fencing token 齐备才谈得上高可用调度。" />

## 小结与下一讲

- 选主用 etcd 的 Lease、Session、Election、Watch，不要自己拿 SETNX 重造。
- 故障转移链路是续租中断、租约到期 key 自动删除、跟随者唤醒、epoch 加一。
- 脑裂只能靠单调递增的 fencing token 在写入端拦截，幂等表消化至少一次语义。

[下一讲：KV 贯穿线 ① 内存版与接口抽象](/projects/kv-1-memory)
