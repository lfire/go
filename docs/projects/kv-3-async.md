---
title: 第 113 讲 · KV 贯穿线 ③ WAL、持久化与并发控制
description: 用 WAL 追加写与 CRC 校验做崩溃恢复，用锁分片提升并发，用快照遏制日志膨胀
---

<StageHeader stage="L10 · 工程实战" level="★★★★☆" time="70 分钟" goal="让数据落盘且并发安全" prereq="第 112 讲 · TCP 协议帧与网络层" />

# 第 113 讲 · KV 贯穿线 ③ WAL、持久化与并发控制

> 上一版把 KV 变成了网络服务，调用方甚至感知不到本地与远程的区别。但只要进程一重启，内存里的数据就全没了；而且 `Memory` 用一把全局 `RWMutex` 护住整个 map，写密集时所有 key 都挤在同一个锁上排队。这一讲解决这两件事：**写前日志（WAL）**让已确认的写在崩溃后能重放回来，**锁分片**把一把大锁拆成 32 把小锁，再用**快照**防止日志无限膨胀。整套改动依旧只在 `Storage` 背后换实现，`Apply(Command)` 一个字不改。

## 这一讲你会得到什么

<CheckList id="L113-goal" title="本讲目标" :items='["用长度加 CRC32 的记录格式实现 WAL 追加写","实现崩溃恢复：顺序重放并在损坏或半条记录处安全停止","用按 key 哈希的锁分片把写竞争摊到多个锁上","理解 Flush 与 fsync 的差别，以及快照与日志截断的边界"]' />

## 项目目标与约束

「上一版遇到了什么问题，所以这一版加了什么」：第 112 讲的数据只存在于 `Memory` 的 map 中，断电即失；读多写少尚可用，一旦写并发上来，单把锁就成了吞吐瓶颈。所以这一版加持久化与并发控制。

- **目标**：写命令先落 WAL 再改内存；启动时重放恢复；按 key 分片降低锁粒度；定期快照以截断日志。
- **约束**：日志只追加、不改写；每条记录带长度与 CRC32；重放必须是幂等的；`Apply` 语义仍是「执行一次命令」。
- **诚实边界**：示例对每条写做 `Flush` 以保证用户态缓冲被写出，但**没有调用 `File.Sync`**，因此机器掉电仍可能丢最后几条；生产必须明确 fsync 策略。

<Term word="WAL" text="Write-Ahead Log，写前日志：先把操作追加到日志，再改内存状态，崩溃后靠重放把状态追回来" />

## 架构蓝图

<ConceptDiagram type="architecture-layer" title="持久化版 KV：Apply → WAL 追加 → 分片内存表；启动时 Replay 回填" caption="读路径不经过 WAL，写路径先落日志再生效，快照定期压缩历史" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"写命令到达","desc":"Apply 收到 OpSet 或 OpDel","tone":"blue"},
  {"title":"追加 WAL","desc":"编码命令，写长度、CRC、载荷并 Flush","tone":"green"},
  {"title":"改内存","desc":"定位分片，在分片锁内更新 map","tone":"orange"},
  {"title":"崩溃重启","desc":"OpenWALStorage 先打开旧文件重放","tone":"red"},
  {"title":"对外服务","desc":"状态与崩溃前一致，继续追加日志","tone":"blue"}
]' />

## 关键实现

以下代码同属 `package kv`，分别为 `contract.go`（第 111 讲契约快照）、`wal.go`、`wal_storage.go`、`snapshot.go`、`wal_storage_test.go`，同一目录 `go test ./...` 可编译运行。

### 1. 契约快照（字段与 Op 数值沿用第 111 讲）

```go
// 文件：contract.go
package kv

import "errors"

type Op byte

const (
	OpGet Op = 1
	OpSet Op = 2
	OpDel Op = 3
)

var (
	ErrNotFound = errors.New("kv: key not found")
	ErrClosed   = errors.New("kv: storage closed")
	ErrEmptyKey = errors.New("kv: empty key")
	ErrBadOp    = errors.New("kv: unknown op")
)

type Command struct {
	Op    Op
	Key   string
	Value []byte
}

type Result struct {
	Value []byte
	Found bool
}

type Storage interface {
	Apply(cmd Command) (Result, error)
	Close() error
}
```

### 2. WAL：记录编码、追加写与重放

```go
// 文件：wal.go
package kv

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// WAL 用「长度 + CRC32 + Command 编码」追加写每条记录，
// 崩溃后顺序重放即可恢复；半条或损坏记录到末尾即停止重放。
type WAL struct {
	w *bufio.Writer
	f io.WriteCloser
}

func OpenWAL(f io.WriteCloser) *WAL {
	return &WAL{w: bufio.NewWriter(f), f: f}
}

// encodeCommand 把 Command 编成二进制：op(1) keyLen(2) valLen(4) key value。
func encodeCommand(cmd Command) []byte {
	buf := make([]byte, 0, 7+len(cmd.Key)+len(cmd.Value))
	buf = append(buf, byte(cmd.Op))
	kl := make([]byte, 2)
	binary.BigEndian.PutUint16(kl, uint16(len(cmd.Key)))
	buf = append(buf, kl...)
	vl := make([]byte, 4)
	binary.BigEndian.PutUint32(vl, uint32(len(cmd.Value)))
	buf = append(buf, vl...)
	buf = append(buf, cmd.Key...)
	buf = append(buf, cmd.Value...)
	return buf
}

func decodeCommand(b []byte) (Command, bool) {
	if len(b) < 7 {
		return Command{}, false
	}
	kl := int(binary.BigEndian.Uint16(b[1:3]))
	vl := int(binary.BigEndian.Uint32(b[3:7]))
	if 7+kl+vl != len(b) {
		return Command{}, false
	}
	return Command{
		Op:    Op(b[0]),
		Key:   string(b[7 : 7+kl]),
		Value: append([]byte(nil), b[7+kl:]...),
	}, true
}

// Append 追加一条写命令：先算 CRC，再写长度与校验，最后 Flush。
// 每条都刷盘换取「已返回给调用方的写不会因进程退出而丢失」。
func (w *WAL) Append(cmd Command) error {
	payload := encodeCommand(cmd)
	rec := make([]byte, 4, 4+4+len(payload))
	binary.BigEndian.PutUint32(rec, uint32(len(payload)))
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(payload))
	rec = append(rec, crc...)
	rec = append(rec, payload...)
	if _, err := w.w.Write(rec); err != nil {
		return err
	}
	return w.w.Flush()
}

// Replay 顺序读取所有完整且校验通过的记录，遇到损坏或半条即停。
// apply 回调同样只处理写命令，天然幂等（OpSet 与 OpDel 重复执行结果一致）。
func Replay(r io.Reader, apply func(Command) error) error {
	for {
		head := make([]byte, 8)
		if _, err := io.ReadFull(r, head); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil // 末尾半条记录，视为上次崩溃点
			}
			return err
		}
		n := binary.BigEndian.Uint32(head[:4])
		wantCRC := binary.BigEndian.Uint32(head[4:8])
		if n > 16<<20 {
			return fmt.Errorf("wal: record too large: %d", n)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil // 崩溃时最后一条未写完整，丢弃
		}
		if crc32.ChecksumIEEE(payload) != wantCRC {
			return nil // 校验失败，停止重放，不把脏数据带进内存
		}
		cmd, ok := decodeCommand(payload)
		if !ok {
			return nil
		}
		if err := apply(cmd); err != nil {
			return err
		}
	}
}

func (w *WAL) Close() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.f.Close()
}
```

### 3. 锁分片的 WALStorage

```go
// 文件：wal_storage.go
package kv

import (
	"os"
	"sync"
)

const shardCount = 32

type shard struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// 编译期断言：新实现同样满足最初的契约。
var _ Storage = (*WALStorage)(nil)

// WALStorage 在 Memory 之上加了两件事：写前落 WAL、按 key 哈希分 32 片。
type WALStorage struct {
	shards [shardCount]shard
	wal    *WAL
	mu     sync.Mutex // 只串行化「写日志 + 生效」，保证日志顺序与 Apply 顺序一致
	closed bool
}

// shardOf 用 FNV-1a 把 key 摊到固定数量的分片，分布均匀且无依赖。
func shardOf(key string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h % shardCount
}

// OpenWALStorage 先重放历史记录恢复状态，再以追加模式打开日志对外服务。
func OpenWALStorage(path string) (*WALStorage, error) {
	s := &WALStorage{}
	for i := range s.shards {
		s.shards[i].data = make(map[string][]byte)
	}
	if old, err := os.Open(path); err == nil {
		err = Replay(old, func(cmd Command) error { return s.applyLocked(cmd) })
		old.Close()
		if err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s.wal = OpenWAL(f)
	return s, nil
}

// applyLocked 只改内存、不写日志，供重放与写路径复用。
func (s *WALStorage) applyLocked(cmd Command) error {
	sh := &s.shards[shardOf(cmd.Key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	switch cmd.Op {
	case OpSet:
		sh.data[cmd.Key] = append([]byte(nil), cmd.Value...)
	case OpDel:
		delete(sh.data, cmd.Key)
	}
	return nil
}

func (s *WALStorage) Apply(cmd Command) (Result, error) {
	if cmd.Key == "" {
		return Result{}, ErrEmptyKey
	}
	// 读路径不取全局锁，直接落在对应分片，读吞吐可随分片数扩展。
	if cmd.Op == OpGet {
		sh := &s.shards[shardOf(cmd.Key)]
		sh.mu.RLock()
		defer sh.mu.RUnlock()
		v, ok := sh.data[cmd.Key]
		if !ok {
			return Result{}, ErrNotFound
		}
		return Result{Value: append([]byte(nil), v...), Found: true}, nil
	}
	if cmd.Op != OpSet && cmd.Op != OpDel {
		return Result{}, ErrBadOp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Result{}, ErrClosed
	}
	// 顺序极其重要：日志写成功才允许改内存，否则崩溃后会丢已确认的写。
	if err := s.wal.Append(cmd); err != nil {
		return Result{}, err
	}
	if err := s.applyLocked(cmd); err != nil {
		return Result{}, err
	}
	return Result{Found: true}, nil
}

func (s *WALStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.wal.Close()
}
```

### 4. 快照：给日志设一个终点

```go
// 文件：snapshot.go
package kv

import (
	"encoding/gob"
	"os"
)

// Snapshot 把当前全量数据写成一个 gob 文件，之后可截断 WAL。
// 真实系统用「快照 + 快照之后的 WAL」两段恢复，避免日志无限膨胀。
// 注意锁顺序与写路径一致：先拿全局 mu，再拿各分片读锁，避免死锁。
func (s *WALStorage) Snapshot(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	all := make(map[string][]byte)
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for k, v := range sh.data {
			all[k] = append([]byte(nil), v...)
		}
		sh.mu.RUnlock()
	}
	return gob.NewEncoder(f).Encode(all)
}
```

### 5. 崩溃恢复测试与写入基准

```go
// 文件：wal_storage_test.go
package kv

import (
	"path/filepath"
	"testing"
)

func TestRecoverAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.wal")
	s, err := OpenWALStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(Command{Op: OpSet, Key: "user:1", Value: []byte("benji")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// 模拟进程重启：同一文件重新打开，应重放出关闭前的写。
	s2, err := OpenWALStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.Apply(Command{Op: OpGet, Key: "user:1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Value) != "benji" {
		t.Fatalf("got %q", got.Value)
	}
}

func BenchmarkWALSet(b *testing.B) {
	path := filepath.Join(b.TempDir(), "bench.wal")
	s, err := OpenWALStorage(path)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Apply(Command{Op: OpSet, Key: "k", Value: []byte("v")}); err != nil {
			b.Fatal(err)
		}
	}
}
```

## 失败路径与边界条件

| 失败场景 | 现象 | 本讲的处理 |
| --- | --- | --- |
| 写日志后、改内存前崩溃 | 日志有一条，内存没有 | 重放会补上，`OpSet`/`OpDel` 幂等 |
| 日志只写了一半 | 末尾出现半条记录 | `io.ReadFull` 读不满即停止，丢弃尾巴 |
| 记录被位翻转 | 载荷与 CRC 不符 | 校验失败停止重放，绝不带入脏数据 |
| 日志无限增长 | 恢复越来越慢、磁盘涨满 | 定期 `Snapshot` + 截断（截断需原子替换） |
| 掉电 | `Flush` 只清了用户态缓冲 | 需 `File.Sync` 才真正落盘，见下方警告 |
| 快照写到一半崩溃 | 半个 gob 文件 | 应写临时文件后再 `os.Rename` 原子替换 |
| 快照与写入并发 | 锁顺序不一致会死锁 | 统一「先全局 mu、再分片锁」的顺序 |

<Callout type="warn" title="Flush 不等于 fsync" text="bufio.Writer.Flush 只是把数据交给操作系统，机器掉电时页缓存里的最后几条仍可能丢失。要真正保证持久性，需要在 Append 后调用底层 *os.File 的 Sync，或明确接受「最多丢 N 毫秒」的组提交策略——这是吞吐与持久性最经典的一处取舍。" />

## 深入一步：为什么日志顺序不能乱

`Apply` 的写路径用一把全局 `mu` 把「追加日志」和「改内存」串成原子步骤，目的不是加锁方便，而是保证**日志顺序与状态变更顺序完全一致**。假设两个写并发：若 A 先改内存、B 先写日志，崩溃重放后 B 会覆盖 A，凭空多出一个写。串行化之后，重放得到的序列与崩溃前执行的序列严格相同。

<Compare title="两种刷盘策略" :panels='[
  {"label":"每条 Flush","code":"w.w.Write(rec); w.w.Flush()","note":"本讲做法：进程退出不丢，但每条一次系统调用，写吞吐低"},
  {"label":"组提交 Group Commit","code":"攒够一批再 Flush 并 Sync","note":"吞吐高数倍，代价是崩溃可能丢最后一批，需业务可接受"}
]' />

<Term word="幂等" text="同一操作执行多次与执行一次的结果相同，是日志重放、消息重试、补偿事务共同的前提" />

读路径刻意不取全局 `mu`：GET 只拿目标分片的读锁，因此读吞吐可随分片数近似线性扩展；而写仍受全局 `mu` 串行，这是因为 WAL 是单一追加流。若要多写并行，需要把 WAL 也按分片拆成多条流——这正是第 114 讲分片复制的伏笔。

## 测试与压测思路

- **恢复正确性**：`TestRecoverAfterRestart` 用同一个临时文件两次 `OpenWALStorage`，验证重放把状态追回。
- **损坏注入**：往日志末尾追加半条记录、翻转一个字节制造 CRC 失败，断言恢复能安全停止且不 panic、不污染已有数据。
- **并发**：`go test -race` 下多 goroutine 混合读写，确认读路径与写路径的锁没有数据竞争；再构造一个「不同 key 的写」场景，对比锁分片前后的吞吐。
- **基线对照**：`BenchmarkWALSet` 与第 111 讲的 `BenchmarkMemorySet` 放一起跑 `benchstat`，量化一次 WAL 追加带来的延迟与分配增长；再对比「每条 Flush」与「攒批 Flush」的差距。

## 工程实践 / 真实场景

生产存储几乎都采用「快照 + 增量日志」两段式恢复：启动时先加载最近一份快照，再重放快照之后的日志，恢复时间从此与数据总量、而不是与历史写次数相关。快照文件必须先写 `tmp` 再原子 `Rename`，否则崩溃后留下半份快照比没有快照更糟。日志通常还会分段（segment）轮转，并在元数据里记录快照位点，才能安全地删除旧日志。

## 可继续扩展的方向

- 给 `Append` 增加 `Sync` 选项与攒批组提交，把持久性做成可配置档位。
- 快照记录日志位点，实现「加载快照 + 重放尾部」并安全截断旧日志。
- 把 WAL 按分片拆成多条流，让写也能并行。
- 最重要的下一步：单机仍有单点故障、容量受一台机器限制——第 114 讲引入分片、副本与 Raft。

## 动手练一练

<Exercise title="让恢复支持「快照 + 尾部日志」" task="在 Snapshot 里记录当前 WAL 的字节偏移，恢复时先加载快照再只重放偏移之后的记录；写出关键步骤与一个一致性测试。" :hints='["WAL 需要暴露当前已写入字节数，可在 Append 里累加","快照文件用 gob 同时编码位点与 map","截断旧日志前要确保新快照已 Rename 成功"]' solution="给 WAL 增加 offset 字段，Append 成功后累加 len(rec)；Snapshot 用 gob 把 offset 与 all 一起写进临时文件再 Rename 成正式快照；OpenWALStorage 先读快照恢复 map 与位点，再用 os.Open 加 Seek(offset) 从该位置开始 Replay；测试里写入若干条、打快照、再写几条、重启，断言最终状态等于全部写命令的结果，且重放条数等于快照之后的条数。" explain="难点在于位点与快照必须是同一次写入的原子视图：位点算早了会漏放，算晚了会重放已包含的写（好在幂等），所以位点要在持有全局 mu 的临界区内读取。" />

## 随堂检测

<Quiz no="113-1" q="写命令的正确顺序是哪一种？" :options='["先改内存，再写 WAL，减少写延迟","先写 WAL 并 Flush，再改内存","内存与 WAL 并发写，谁先完成算谁的","只写 WAL，读时再从日志重放"]' answer="1" explain="先写日志再改内存，才能保证「返回给调用方的写一定可重放」。反序会在日志落盘前崩溃时丢数据，且破坏日志顺序与状态顺序的一致性。" />

<Predict no="113-2" q="WAL 末尾有一条只写了一半的记录，Replay 会怎样？" :options='["报严重错误并拒绝启动","跳过它并继续解析后面的记录","在 io.ReadFull 读不满时停止，返回 nil 完成恢复","把半条记录按 0 填充后应用"]' answer="2" output="在 io.ReadFull 读不满时停止，返回 nil 完成恢复" explain="末尾半条是崩溃的正常痕迹：读不满即视为崩溃点，前面的完整记录已经全部重放，恢复到此结束是安全的；继续读或补零都会引入脏状态。" />

<FillBlank no="113-3" tpl="bufio.Writer 的 ___ 只清空用户态缓冲，真正落盘要调用文件的 ___；判断记录是否损坏用 ___ 校验。" :answers='["Flush","Sync~File.Sync","CRC32~CRC~校验和"]' hint="三个空分别来自 bufio、os.File、hash/crc32" explain="Flush 与 Sync 是两层不同的持久性保证，CRC 则负责把崩溃留下的半条或位翻转记录挡在状态机之外。" />

## 一页纸速记

<KnowledgeCard :points='["写顺序固定为：追加 WAL 并 Flush，再改内存，保证日志顺序等于状态顺序","记录格式用长度加 CRC32，重放遇到半条或校验失败就安全停止","OpSet 与 OpDel 幂等，是重放与重试不会出错的前提","锁分片让读路径绕开全局锁，写路径仍因单条 WAL 而串行","快照是日志的终点，必须用临时文件加 Rename 原子替换"]' :pitfalls='["把 Flush 当成 fsync，掉电后仍会丢最后几条写","快照与写路径锁顺序不一致导致死锁","在半条记录之后继续重放，把脏数据带进内存"]' summary="持久性靠的是「先记日志再生效 + 幂等重放」，并发靠的是把一把锁拆成很多把。" />

## 小结与下一讲

1. 用「长度 + CRC32 + 编码命令」实现 WAL 追加写，崩溃后顺序重放，遇到半条或损坏记录即安全停止。
2. 写路径以全局 `mu` 保证日志顺序与状态顺序一致，读路径绕开它走分片读锁，用 32 个分片摊开竞争。
3. 用快照给日志设终点，并诚实指出 `Flush` 与 `Sync` 的差别、快照原子替换、锁顺序三处真实边界。

单机版至此已经能落盘、能扛并发，但它仍是一台机器：机器宕机服务就停，容量也顶死在一台机器的内存与磁盘上。下一讲把 KV 演进成分布式存储，引入分片路由、多副本与 Raft 共识。

[下一讲：KV 贯穿线 ④ 分片、复制与 Raft](/projects/kv-4-distributed)
