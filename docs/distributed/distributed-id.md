---
title: 第 086 讲 · 分布式 ID、时钟与雪花算法
description: 生成全局唯一且趋势有序的 ID，并直面时钟回拨这一真实风险
---

<StageHeader stage="L8 · 分布式与云原生" level="★★★☆☆" time="40 分钟" goal="生成全局唯一且有序的 ID，能对时钟回拨给出兜底" prereq="分布式事务：2PC / TCC / Saga / 消息最终一致" />

# 第 086 讲 · 分布式 ID、时钟与雪花算法

> 订单号、消息 ID、 trace 标识、分库分表主键，全都需要一个「全局不重复」的编号。单库时代一个 `AUTO_INCREMENT` 就够了；数据一拆分，三台机器各发各的 1、2、3，立刻撞号。更隐蔽的是：这个编号还要尽量有序，否则拿它当数据库主键会把 B+ 树写得支离破碎。本讲比较自增、UUID、Snowflake 与号段四种思路，并重点追问——如果机器的时钟突然往回跳了，发号器会发出什么？

## 这一讲你会得到什么
<CheckList id="goal-86" title="本讲目标" :items='["说清自增 ID 与 UUID 在分库后的局限","拆开 Snowflake 的位布局并独立实现一个发号器","解释时钟回拨为什么会撞号以及怎么兜底","用号段模式在本地批量发号并说清数据库故障时的表现"]' />

## 1. 先建立直觉
<ConceptDiagram type="snowflake" title="Snowflake：时间戳、机器 ID 与序列号的位布局" />

给全国快递包裹编号，最省事的办法是设一个总台按顺序发号——可总台一瘫，全网停摆，这正是数据库自增的困境。另一种办法是让每个网点随便编一个绝不重复的长串，谁也不求谁，可这串号完全没顺序，仓库按号上架时只能满场乱跑，这正是 UUID 当主键的代价。

Snowflake 走的是中间路线：把 64 位整数切成三段——高位放毫秒级时间戳，中间放机器编号，低位放同一毫秒内的自增序列。时间在前，号就大体按时间递增；机器编号隔开不同节点，序列撑住单机每毫秒的并发。三件事拼起来，既不依赖单点，又保住了趋势有序。

但这份优雅建立在一个危险假设上：**本机时钟一直往前走**。NTP 校时、虚拟机热迁移、运维手工改时间，都可能让 `time.Now()` 突然变小。若发号器不察觉，就会用「过去的毫秒」配上同样的机器 ID 与序列，发出一个和几秒前完全相同的号——两条订单共用一个主键。这就是本讲最该记住的中途失败。

## 2. 语法与规则

先把四种方案的取舍摆在同一张表里，再落到可运行的实现。

| 方案 | 唯一性来源 | 有序性 | 中途失败会怎样 | 适合场景 |
| --- | --- | --- | --- | --- |
| 数据库自增 | 单库事务 | 严格递增 | 主库宕机即停发，分库后直接撞号 | 单库小系统 |
| UUID v4 | 128 位随机数 | 完全无序 | 无中心依赖，永不阻塞；但随机主键导致页分裂 | 日志、对象名、跨系统标识 |
| Snowflake | 时间戳加机器 ID 加序列 | 趋势递增 | 时钟回拨或机器 ID 重复都会撞号 | 高并发业务主键 |
| 号段 Leaf | 数据库批量分配 | 段内递增 | 数据库不可用时本地号段发完即停 | 中等吞吐、容灾要求高 |

先看 UUID 为什么不适合当聚簇索引主键：它纯粹随机，插入时每次都落在 B+ 树的随机页上，页分裂与随机 IO 都明显增加。

```go
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// UUIDv4 用 16 字节随机数生成一个全局唯一标识
func UUIDv4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // 版本号 4
	b[8] = (b[8] & 0x3f) | 0x80 // 变体位
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func main() {
	for i := 0; i < 3; i++ {
		fmt.Println("UUID:", UUIDv4())
	}
	// 自增 ID 天然有序，插入 B+ 树时总落在最右侧页
	for i := 1; i <= 3; i++ {
		fmt.Println("自增 ID:", i)
	}
}
```

真正的主角是 Snowflake。下面这段实现把时钟做成可注入的函数，好处是能直接在测试里模拟回拨，把「中途失败会怎样」跑出来看。

```go
package main

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Snowflake 布局：41 位毫秒时间戳 + 10 位机器 ID + 12 位序列号
type Snowflake struct {
	mu     sync.Mutex
	epoch  int64
	nodeID int64
	lastMS int64
	seq    int64
	now    func() int64 // 可注入的时钟，便于测试时钟回拨
}

const (
	nodeBits = 10
	seqBits  = 12
	seqMask  = -1 ^ (-1 << seqBits) // 4095
	nodeMask = -1 ^ (-1 << nodeBits)
)

func NewSnowflake(epoch, nodeID int64) (*Snowflake, error) {
	if nodeID < 0 || nodeID > nodeMask {
		return nil, errors.New("机器 ID 超出范围")
	}
	return &Snowflake{
		epoch: epoch, nodeID: nodeID, lastMS: -1,
		now: func() int64 { return time.Now().UnixMilli() - epoch },
	}, nil
}

// NextID 生成下一个 ID；发现时钟回拨立即拒绝，避免发出重复号
func (s *Snowflake) NextID() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now < s.lastMS {
		// 时钟回拨：继续沿用旧时间戳会与已经发出的 ID 撞号
		return 0, fmt.Errorf("时钟回拨 %d 毫秒，拒绝发号", s.lastMS-now)
	}
	if now == s.lastMS {
		s.seq = (s.seq + 1) & seqMask
		if s.seq == 0 {
			// 同一毫秒序列号用尽，自旋等到下一毫秒
			for now <= s.lastMS {
				now = s.now()
			}
		}
	} else {
		s.seq = 0
	}
	s.lastMS = now
	return (now << (nodeBits + seqBits)) | (s.nodeID << seqBits) | s.seq, nil
}

func main() {
	epoch := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	s, _ := NewSnowflake(epoch, 7)
	ids := make([]int64, 5)
	for i := range ids {
		ids[i], _ = s.NextID()
	}
	fmt.Println("连续 5 个 ID 趋势递增:", ids[0] < ids[1] && ids[1] < ids[2] && ids[2] < ids[3] && ids[3] < ids[4])

	// 注入一次时钟回拨：模拟 NTP 校时把系统时间往回调
	base := s.now()
	s.now = func() int64 { return base - 1000 }
	_, err := s.NextID()
	fmt.Println("时钟回拨后拒绝发号:", err != nil)
}
```

<OutputBox title="运行结果" output="连续 5 个 ID 趋势递增: true\n时钟回拨后拒绝发号: true" />

`epoch` 是自定义纪元，用「当前毫秒减去纪元」把 41 位时间戳的可用年限撑到几十年。注意 `<<` 位移把三段拼成一个整数：时间在高位，所以跨毫秒一定变大；同一毫秒内靠 `seq` 自增，序列到 4095 归零时就自旋等下一毫秒。

<LineByLine code="const (\nnodeBits = 10\nseqBits = 12\n)" :notes='["用常量集中定义位宽，改配置时不必翻遍代码","nodeBits 给机器 ID 留 10 位，最多容纳 1024 个节点","seqBits 给序列留 12 位，单机每毫秒最多发 4096 个号","常量块结束，位宽之和决定时间戳还剩多少位"]' />

把位布局记牢，就能自己推出三个边界：机器 ID 超出 1023 要报错、每毫秒超过 4096 个号要等待、时间戳溢出前要规划好纪元。

## 3. 深入一步

<Term word="时钟回拨" text="系统时间因 NTP 校时、虚拟机迁移或人工调整而倒退，导致基于时间戳的 ID 可能重复。" />
<Term word="趋势递增" text="整体按时间变大，但同一毫秒内跨机器并不保证严格有序，适合聚簇索引却不等于全局排序。" />
<Term word="号段" text="一次从数据库领取一批连续号，在本地内存逐个发放，用完再领，把数据库访问摊薄到每批一次。" />

**时钟回拨是最该主动防御的风险**。它不像宕机那么显眼：服务还活着，ID 还在发，只是悄悄和几秒前重复了。常见兜底有三档：小幅回拨（例如几毫秒）可自旋等待时间追平；超过阈值直接拒绝发号并告警，宁可短时不可用也不撞号；把机器 ID 的分配交给 etcd 这类协调服务，重启后换一个新 ID，从根本上避开「旧进程用旧时间戳」的重叠。无论哪一档，都不要 silently 沿用旧时间戳继续发号。

<Callout type="warn" title="易错点" text="别把 Snowflake 的 ID 当成严格全局有序：不同机器在同一毫秒内各发各的序列，谁大谁小并不确定。也别让机器 ID 靠配置文件手工填，两台机器填成同一个值就会持续撞号。" />

三种结构并排看更直观：UUID 是 16 个随机字节；Snowflake 是 `41 + 10 + 12` 的位拼接；号段是一个 `[max, max+step)` 的区间。三者取巧的地方，分别是「不依赖时钟」「依赖时钟但有序」「依赖数据库但批量」。

### 从 64 位布局推导 Snowflake 的三条硬边界

Snowflake 的 `41 + 10 + 12` 不是拍脑袋的分割，每一段都对应一条能算出来的上限：

- **可用年限**：41 位毫秒时间戳最大约 `2^41 - 1`，折合约 69.7 年。所以真正决定「能用多久」的不是位数，而是自定义纪元 `epoch`：从 `2024-01-01` 起算，约到 2093 年溢出；把 epoch 设成系统上线日、而不是 Unix 纪元 1970，才不浪费高位。这也解释了第 2 节为什么用「当前毫秒减去 epoch」。
- **节点规模**：10 位机器 ID 上限 `2^10 = 1024` 个节点，`nodeMask` 就是 1023。超过就必须改位宽或引入分层编号。
- **单机吞吐**：12 位序列每毫秒最多 4096 个 ID，即单机理论上限约 409.6 万 ID 每秒。`seq` 用位与归零后自旋等下一毫秒，正是把并发压到这个上限之下。

唯一性的形式化条件也很朴素：两个 ID 相等当且仅当「时间戳、机器 ID、序列」三元组完全相同。因此只要保证「同一节点在同一毫秒内序列不重复」且「机器 ID 全局唯一」，唯一性就成立。**注意这里没有依赖时间递增**，只依赖「不回退到已经用过的毫秒加序列组合」。所以时钟回拨之所以危险，不是因为「时间倒退」本身，而是因为倒退后重新进入了一个**已经发过号的毫秒窗口**，三元组可能整体重合。

由此能推出回拨兜底的三档各自在防什么：

| 兜底 | 防的是 | 前提与失效条件 |
| --- | --- | --- |
| 小幅回拨自旋等待 | 回拨幅度小于等待窗口，时间能自己追平 | 若回拨来自 NTP 一次性大幅校时或人工改表，自旋会长期阻塞甚至永远等不到；必须设最大等待阈值，超阈值升级为拒绝 |
| 超过阈值拒绝发号并告警 | 避免复用已用过的毫秒窗口 | 代价是发号服务短时不可用；要求上游能容忍失败或快速切到备用发号器 |
| 重启换机器 ID、借 etcd 分配 | 旧进程用旧时间戳继续发号造成跨进程重叠 | 要求机器 ID 的分配是原子的、有租约的；两个进程拿到同一 ID 时仍然会撞号 |
| 逻辑时钟（lastMS 单调） | 用当前毫秒与上次发号毫秒取较大值代替物理时间 | 能避免重复，但会让 ID 时间戳偏离真实时间，长时间回拨下 41 位可能被提前耗尽，且跨节点趋势递增失真 |

<Callout type="note" title="关键区分" text="时钟回拨检测比较的是当前读到的毫秒与本进程上次发号用的毫秒，不是与系统启动时间比较。所以服务重启后记录归零、而物理时钟已回拨时，单靠内存里的这个值是发现不了的，这正是要把最大时间戳或机器 ID 持久化到外部协调服务的原因。" />

<SequenceDiagram title="时钟回拨从发生到拒绝发号" caption="注入回拨后检测时间倒退，超阈值拒绝并切备用发号器" :actors='["Client","IDGen","Clock","Alert"]' :steps='[{"from":"Client","to":"IDGen","label":"请求 ID","kind":"call"},{"from":"IDGen","to":"Clock","label":"读当前毫秒","kind":"call"},{"from":"Clock","to":"IDGen","label":"返回 1000","kind":"return"},{"from":"IDGen","to":"IDGen","label":"发号 记录 lastMS","kind":"self"},{"from":"Client","to":"IDGen","label":"再请求 ID","kind":"call"},{"from":"IDGen","to":"Clock","label":"读当前毫秒","kind":"call"},{"from":"Clock","to":"IDGen","label":"NTP 回拨到 900","note":"时间倒退","kind":"return"},{"from":"IDGen","to":"IDGen","label":"检测时间倒退","kind":"self"},{"from":"IDGen","to":"Alert","label":"回拨超阈值 告警","kind":"call"},{"from":"IDGen","to":"Client","label":"拒绝发号","kind":"return"},{"from":"Client","to":"IDGen","label":"切备用后重试","kind":"call"},{"from":"IDGen","to":"Client","label":"返回新 ID","kind":"return"}]' />

### 适用前提与不在讨论范围

| 结论 | 前提 | 失效条件 |
| --- | --- | --- |
| Snowflake 全局唯一 | 机器 ID 全局唯一且同一毫秒序列不复用 | 机器 ID 靠配置文件手填、容器重启 IP 复用、时钟回拨到已用窗口，三者任一都会撞号 |
| 趋势递增适合聚簇索引 | 时间在高位，且各节点时钟大致同步 | 不同节点同一毫秒内无全局顺序；节点间时钟偏差大时，后发生的事拿到更小 ID，分页按 ID 排序会错序 |
| 号段数据库故障仍能发一段 | 本地已领取的号段未耗尽 | 号段发完即停；若数据库长期不可用且无双 buffer 预取，服务不可用 |
| 41 位够用几十年 | epoch 设为上线时间、且时间不回拨 | 用逻辑时钟长期垫高 lastMS 会提前耗尽高位 |

范围上，本讲讨论的都是**崩溃-停止与时间异常**（NTP 校时、虚拟机热迁移、运维改表），不包含拜占庭式的「发号器故意发重复号」；后者要靠存储层唯一索引兜底，而不是靠算法。也别把 ID 当安全能力：Snowflake 把时间戳暴露在 ID 里，能反推业务量级与创建时间，对外暴露时通常要再包一层。

### 三条真实误解

| 误解 | 为什么错 | 正确说法 |
| --- | --- | --- |
| Snowflake 的 ID 全局严格有序，可以直接按它分页或当版本号比较先后 | 同一毫秒内跨机器只有序列不同，没有全局顺序；节点间时钟偏差还会让物理上后发生的事拿到更小 ID | 只能叫趋势递增；跨节点排序、分页要改用专门的序号或时间字段 |
| 检测到回拨就自旋等待，等到了就安全了 | 只有小幅回拨会自己追平；NTP 一次性大幅校时或人工改表可能永远等不到，线程长期挂起 | 设最大等待阈值，超阈值拒绝发号并告警；或换机器 ID、切备用发号器 |
| 号段模式没了中心单点，数据库挂了也不影响 | 号段只是把数据库访问摊薄，本地段发完仍要回数据库取 | 数据库短时故障靠本地缓冲续命，长期故障仍不可用；双 buffer 只是抹掉切换停顿 |

## 4. 工程实践：号段模式与可用性兜底

Snowflake 把发号能力下放到每台机器，代价是每台都得管好时钟与机器 ID。号段（Leaf-segment）反其道而行：仍由数据库分配，但一次领一大段，本地内存发完再领，既保住趋势递增，又把数据库压力降到每批一次。

```go
package main

import (
	"errors"
	"fmt"
	"sync"
)

// Segment 表示一个号段区间，本地内存发号，用完再取
type Segment struct {
	mu   sync.Mutex
	cur  int64
	max  int64
	step int64
	// fetch 模拟从数据库原子取号段：UPDATE max_id = max_id + step
	fetch func(step int64) (int64, error)
}

func (s *Segment) load() error {
	max, err := s.fetch(s.step)
	if err != nil {
		return err // 数据库不可用，取号段失败
	}
	s.cur = max - s.step
	s.max = max
	return nil
}

// Next 发一个号；号段用尽时尝试加载下一段
func (s *Segment) Next() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur >= s.max {
		if err := s.load(); err != nil {
			return 0, errors.New("号段用尽且数据库不可用：" + err.Error())
		}
	}
	id := s.cur
	s.cur++
	return id, nil
}

func main() {
	dbMax := int64(1000)
	seg := &Segment{
		step: 5,
		fetch: func(step int64) (int64, error) {
			dbMax += step
			return dbMax, nil
		},
	}
	for i := 0; i < 7; i++ {
		id, err := seg.Next()
		if err != nil {
			fmt.Println("发号失败:", err)
			continue
		}
		fmt.Println("发号:", id)
	}

	// 注入故障：数据库不可用，首段都领不到
	seg2 := &Segment{step: 5,
		fetch: func(step int64) (int64, error) {
			return 0, errors.New("数据库连接超时")
		}}
	_, err := seg2.Next()
	fmt.Println("取号段失败:", err != nil)
}
```

<CodeTabs title="三种 ID 的结构对照" :tabs='[{"label":"UUID","code":"16 random bytes","note":"完全随机，无中心节点，但作主键会页分裂"},{"label":"Snowflake","code":"41 ts + 10 node + 12 seq","note":"趋势递增，依赖时钟与机器 ID 唯一"},{"label":"号段","code":"batch [max, max+step)","note":"批量领取本地发放，数据库压力摊薄"}]' />

<Compare title="检测到时钟回拨的两种处理" :panels='[{"label":"拒绝发号","code":"return errorOnClockRollback()","note":"回拨立刻报错并告警，宁可短时不可用也不撞号，适合资金类"},{"label":"等待追平","code":"waitUntilClockCatchesUp()","note":"小幅回拨自旋等待，超过阈值仍要报警介入，适合可容忍短暂延迟的场景"}]' />

<FlowTimeline direction="horizontal" :steps='[{"title":"毫秒内发号","desc":"序列号从 0 递增，单机扛住并发","tone":"blue"},{"title":"跨毫秒","desc":"时间戳前进，序列归零重新开始","tone":"blue"},{"title":"NTP 校时","desc":"系统时间突然往回跳，出现回拨","tone":"red"},{"title":"检测回拨","desc":"当前毫秒早于上次发号时刻","tone":"red"},{"title":"拒绝并告警","desc":"停止发号，等追平或人工介入","tone":"green"}]' />

号段的故障表现同样清晰：数据库短暂不可用时，本地已领到的号段还能继续发，发完才停，不会撞号也不会阻塞到每次请求；这就是用「一段本地缓冲」换来的可用性。工程上常配双 buffer 预取，在当前段用到一定比例时异步领好下一段，把切换时的停顿也抹掉。选型时按依赖取舍：不能接受时钟问题与机器 ID 治理，选号段或 Redis INCR；能管好时钟与节点编号，就选 Snowflake 换掉中心依赖。

### 可验证实验：注入回拨与机器 ID 冲突，用 go test 把撞号跑出来

第 2 节把时钟做成了可注入函数，正好能在测试里制造回拨并断言拒绝；再补一个用例让两个发号器共用同一个 nodeID，验证确实撞号：

```bash
go test -run 'TestSnowflake' -v ./...
go test -race -run TestSnowflakeConcurrent -v ./...
# 观察本机物理时钟从哪来、是否会跳变
timedatectl status | grep -E 'System clock|NTP service'
```

```text
$ go test -run 'TestSnowflake' -v ./...
=== RUN   TestSnowflake/TrendIncreasing
=== RUN   TestSnowflake/ClockRollbackRejected
=== RUN   TestSnowflake/DuplicateNodeIDCollides
=== RUN   TestSnowflake/SeqOverflowWaitsNextMillis
--- PASS: TestSnowflake (0.00s)
PASS
ok      example.com/idgen    0.183s
$ go test -race -run TestSnowflakeConcurrent -v ./...
--- PASS: TestSnowflakeConcurrent (0.00s)
ok      example.com/idgen    0.297s
$ timedatectl status | grep -E 'System clock|NTP service'
System clock synchronized: yes
NTP service: active
```

逐字段解读：`TrendIncreasing` 连续取 5 个 ID 断言严格递增，验证时间在高位；`ClockRollbackRejected` 把注入的 `now` 调回 `base - 1000` 后断言 `NextID()` 返回非 nil 的 error，对应第 2 节那句「时钟回拨，拒绝发号」；`DuplicateNodeIDCollides` 让两个 `Snowflake` 实例用相同 `nodeID` 与相同注入时钟，断言生成的 ID 相等——把「机器 ID 重复会撞号」从结论变成可复现事实；`SeqOverflowWaitsNextMillis` 在序列归零时推进注入时钟，验证自旋确实等到下一毫秒。`-race` 的 `PASS` 说明 `NextID` 里的互斥锁保护了 `lastMS` 与 `seq` 的并发读写，没有 `WARNING: DATA RACE`；同样地，**`-race` 证明不了 ID 不重复**，唯一性靠的是上面这些断言与三元组推导。

`timedatectl` 两行里，`System clock synchronized: yes` 表示本机已与 NTP 同步，正因为它会周期性校时，才可能出现毫秒级回拨；`NTP service: active` 说明校时进程在跑。生产里应把 NTP 配成 slew 模式（缓慢微调而非跳变），并把发号器的回拨阈值与告警联动，而不是假设「服务器时间不会倒退」。

<Quiz q="Snowflake 发生大幅时钟回拨时，最稳妥的处理是？" :options='["沿用旧时间戳继续发号","无限自旋等待时间追平","超过阈值拒绝发号并告警","把序列位清零后继续"]' answer="2" explain="沿用旧时间戳会与已发出的 ID 撞号，无限自旋在大幅校时下可能永远等不到；拒绝发号把故障转成短时不可用，是正确性优先的选择。" />

### 失败矩阵与演练手段

| 故障注入 | 现象 | 兜底 | 演练手段 | 回滚或恢复 |
| --- | --- | --- | --- | --- |
| NTP 校时回拨 5ms | 可能复用已发过号的毫秒窗口 | 自旋等待追平 | 用注入时钟模拟小幅回拨 | 超阈值升级为拒绝 |
| 人工改表回拨 2 分钟 | 自旋等不到，发号线程挂起 | 拒绝发号、告警、切备用 | `date -s` 或注入时钟 | 恢复时间后重新启用，核对最大 ID |
| 两个容器拿到同一 nodeID | 持续撞号，主键冲突 | etcd 租约分配加启动校验 | 用相同 nodeID 起两个实例 | 停掉一个并换 ID，已有重复主键需人工修复 |
| 单毫秒超过 4096 个号 | 序列归零、自旋等下一毫秒 | 自旋；若持续超限则扩容节点 | `vegeta` 灌入突发流量 | 增加节点而非调大序列位，后者会挤占时间戳位 |
| 数据库不可用且号段发完 | 发号失败，上游报错 | 双 buffer 预取加备用号段 | chaos-mesh 断数据库网络 | 切备用发号器，恢复后校验 max_id |

数据迁移场景要特别留意：从自增主键迁到 Snowflake 时，历史 ID 可能远小于新 ID，若业务按 ID 范围做分片，必须重新规划分片路由而不是直接沿用旧区间——这与第 087 讲的迁移窗口是同一类问题。

### 替代路线与取舍

| 路线 | 唯一性来源 | 有序性 | 故障时 | 代价 | 适合 |
| --- | --- | --- | --- | --- | --- |
| Snowflake | 时间戳加机器 ID 加序列 | 趋势递增 | 回拨或 ID 重复会撞号 | 要治理时钟与节点编号 | 高并发业务主键 |
| 号段 Leaf | 数据库批量分配 | 段内递增 | 本地段发完即停 | 依赖数据库，需双 buffer | 中等吞吐、容灾要求高 |
| Redis INCR | 单实例原子自增 | 严格递增 | Redis 故障或主从切换可能回退 | 中心依赖、需持久化配置 | 简单、吞吐不极端 |
| UUID v4 或 ULID | 随机数，或时间加随机 | UUID 无序；ULID 可按时间排序 | 无中心依赖，永不阻塞 | UUID 作聚簇索引主键会页分裂 | 日志、对象名、跨系统标识 |
| 数据库自增 | 单库事务 | 严格递增 | 主库宕即停发，分库即撞号 | 单点 | 单库小系统 |

取舍主线只有一条：**要严格有序就得接受中心节点，要去中心就得接受趋势有序加治理时钟与编号**。ULID 是个折中——把时间放高位获得可排序性，同时用随机数代替机器 ID 免去编号治理，代价是 128 位而非 64 位，存储与索引成本更高。

### 与后续实战的衔接

- [短链服务](/projects/net-shortlink) 的「发号与 Base62」正是本讲的直接下游：发号器给出的趋势递增 ID 经 Base62 转成短码，回拨兜底保证短码不重复。
- [KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed) 在「分片路由」里会把 ID 当作分片键的来源，此时本讲的「趋势递增不等于全局有序」会直接影响能否用 ID 范围做路由。
- [分布式任务调度系统](/projects/concurrent-scheduler) 给每个任务实例分配全局唯一 ID，靠的正是号段或 Snowflake 这类不依赖单点的方案，避免 Master 切换时任务号重复。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l8-distributed/lesson-086-distributed-id`](https://github.com/lfire/go/tree/main/examples/l8-distributed/lesson-086-distributed-id)。clone 仓库后进入 `examples/l8-distributed`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练
<Exercise title="给回拨加一道防线" task="Snowflake 的 NextID 检测到当前毫秒小于上次发号时刻时，应该怎么做？请写出处理思路。" :hints='["想想复用旧时间戳会发出什么","区分小幅回拨与大幅回拨"]' solution="在 NextID 开头比对当前毫秒与 lastMS：\n发现时间倒退就返回错误，或自旋等待时间追平；\n回拨超过阈值则告警并停止发号，绝不复用旧时间戳" explain="复用旧时间戳会与已发出的 ID 撞号；正确做法是拒绝、等待或换机器 ID，把故障转成短暂不可用而不是数据错误。" />

## 随堂检测
<Quiz no="086-1" q="Snowflake 发生时钟回拨而继续沿用旧时间戳发号，最可能导致的后果是？" :options='["ID 变得无序","发出与之前重复的 ID","发号速度变慢","机器 ID 被覆盖"]' answer="1" explain="回拨让时间戳、机器 ID、序列三元组可能与历史完全重合，于是生成重复主键。" />
<Predict no="086-2" q="同一毫秒内两台不同机器各自发号，这两个 ID 一定严格递增吗？" :options='["一定递增","一定相等","不保证，只是趋势递增","会报错"]' answer="2" output="不保证，只是趋势递增" explain="跨机器在同一毫秒内只靠序列号区分，没有全局顺序，所以 Snowflake 只能叫趋势递增。" />
<FillBlank no="086-3" tpl="Snowflake 的经典位布局是 ___ 位时间戳、___ 位机器 ID 与 12 位序列号；号段模式一次领取一个 ___。" :answers='["41","10","区间~号段"]' hint="时间、机器、序列三段与批量领取" explain="41 位毫秒时间戳撑年限，10 位机器 ID 区分节点，12 位序列撑单机每毫秒并发；号段按批领取。" :explains='["去掉符号位后，41 位毫秒级时间戳可支撑约 69 年不回绕，既让 ID 大致按时间递增，也给每毫秒并发留出排序基准。","10 位可编码 1024 个节点标识，保证不同机器在同一毫秒生成序列号时前缀不同，从结构上避免跨节点碰撞。","号段模式不去逐次向数据库要一个 ID，而是一次取出一段连续区间缓存在本地慢慢发，把写库频率从每 ID 一次降到每段一次。"]' />

## 一页纸速记
<KnowledgeCard :points='["自增依赖单点且分库撞号，UUID 无序不适合聚簇索引主键","Snowflake 用时间戳加机器 ID 加序列拼出趋势递增的 64 位 ID","时钟回拨与机器 ID 重复是两类最容易撞号的故障","号段模式批量领号，数据库故障时本地段发完才停"]' :pitfalls='["不做回拨检测，校时后静默发出重复 ID","把 Snowflake 当严格全局有序来排序或分页","机器 ID 靠手工配置，多节点填成同一个值"]' summary="好的发号器既要在正常时唯一有序，更要在时钟或数据库出故障时拒绝撞号。" />

## 小结与下一讲

- 自增怕单点与分库撞号，UUID 怕随机主键，Snowflake 与号段是工程主流。
- Snowflake 把时间放高位换趋势递增，代价是必须治理时钟回拨与机器 ID 唯一性。
- 号段用本地缓冲换可用性，数据库短时故障时已领号段仍能继续发放。

ID 解决了「一条数据叫什么」，下一讲解决「一条数据放在哪台机器上」——分片与一致性哈希。

[下一讲：一致性哈希、分片与路由](/distributed/sharding)
