---
title: 第 112 讲 · KV 贯穿线 ② TCP 协议帧与网络层
description: 设计定长协议帧解决粘包，管理连接与并发，并让客户端 SDK 实现同一个 Storage 接口
---

<StageHeader stage="L10 · 工程实战" level="★★★☆☆" time="60 分钟" goal="把单机 KV 变成网络服务" prereq="第 111 讲 · 内存版与接口抽象" />

# 第 112 讲 · KV 贯穿线 ② TCP 协议帧与网络层

> 上一版的 KV 已经有一份干净的 `Storage` 契约，但它只能在自己进程里被调用——同一个包里 `NewMemory()` 出来的对象，别的机器、别的语言都碰不到。这一讲给它加一层网络：定义协议帧、解决粘包、管理连接，并且让**客户端 SDK 也实现同一个 `Storage` 接口**。于是调用方拿到的还是那个 `Apply(Command)`，底层是本地内存还是远程 TCP，它完全不用关心。这就是第 111 讲把接口定为「窄腰」换来的红利。

## 这一讲你会得到什么

<CheckList id="L112-goal" title="本讲目标" :items='["设计带魔数、版本、长度上限的二进制协议帧","用固定头加 io.ReadFull 彻底解决 TCP 粘包与半包","为每个连接开 goroutine，并实现可优雅退出的服务端","让 Client 实现 Storage，做到本地与远程调用零差异"]' />

## 项目目标与约束

「上一版遇到了什么问题，所以这一版加了什么」：第 111 讲的 `Memory` 是进程内对象，没有跨进程能力；而 `Command`/`Storage` 契约是纯内存结构，天然可以序列化。所以这一版只加「传输层」，不动契约。

- **目标**：把任意一个 `Storage` 暴露成 TCP 服务；提供 `Client` SDK；编解码、连接管理、并发处理三块各司其职。
- **约束**：帧格式自研、只用标准库；单连接上请求串行（用互斥锁保证帧不交错）；`Op` 数值沿用第 111 讲，保证跨版本兼容。
- **明确不做**：认证、TLS、加密、连接池、超时控制——这些是网关与运维层的事，本讲只把链路打通并在「失败路径」里点出代价。

<Term word="粘包" text="TCP 是字节流，没有消息边界；一次 Read 可能读到半帧，也可能一次读到好几帧，必须靠协议自己切分" />

## 架构蓝图

<ConceptDiagram type="architecture-layer" title="网络版 KV 的分层：SDK → 帧编解码 → 连接管理 → Storage 实现" caption="Client 与 Memory 实现同一个 Storage 接口，Router 式的替换对调用方透明" />

<FlowTimeline direction="horizontal" :steps='[
  {"title":"调用方 Apply","desc":"构造 Command，感知不到底层在哪","tone":"blue"},
  {"title":"客户端编码","desc":"writeFrame 写定长头与 body","tone":"green"},
  {"title":"TCP 传输","desc":"字节流，可能半包或粘包","tone":"orange"},
  {"title":"服务端解码","desc":"ReadFull 读满头再读满 body","tone":"red"},
  {"title":"落地 Storage","desc":"交给 Memory 或后续任意实现","tone":"blue"}
]' />

<Callout type="tip" title="这一版最漂亮的一处设计" text="Client.Apply 与 Memory.Apply 签名完全相同，所以编排层、测试代码可以原样复用。后面第 113 讲的 WALStorage、第 114 讲的 Router 也都实现 Storage，四讲始终是「换实现、不换调用」。" />

## 关键实现

以下代码同属 `package kv`，分别存为 `contract.go`、`frame.go`、`server.go`、`client.go`、`frame_test.go`，同一目录 `go test ./...` 即可编译运行。`contract.go` 是第 111 讲契约的**快照**，字段与数值一律不改，重复列出只是为让本讲代码可独立编译。

### 1. 契约快照（与第 111 讲完全一致）

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

### 2. 协议帧与编解码

```go
// 文件：frame.go
package kv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	magic     byte = 0x4B // 'K'
	version   byte = 1
	reqStatus byte = 0
	okStatus  byte = 1
	errStatus byte = 2
	maxFrame       = 16 << 20 // 单帧上限 16 MiB，防止恶意超大长度打爆内存
)

var ErrBadFrame = errors.New("kv: bad frame")

// Frame 是网络上传输的一个协议帧。请求与响应共用同一结构，
// 靠 Status 区分：0 请求、1 成功、2 失败（此时 Value 放错误文本）。
type Frame struct {
	Op     Op
	Status byte
	Key    string
	Value  []byte
	Found  bool
}

// writeFrame 用「8 字节定长头 + 定长 body」写帧。
// head 布局：magic(1) version(1) op(1) status(1) bodyLen(4)。
// body 布局：keyLen(2) key found(1) value。
// 接收方按长度读取，天然解决粘包与半包。
func writeFrame(w io.Writer, f Frame) error {
	if len(f.Key) > 0xFFFF {
		return fmt.Errorf("%w: key too long", ErrBadFrame)
	}
	kl := make([]byte, 2)
	binary.BigEndian.PutUint16(kl, uint16(len(f.Key)))
	body := make([]byte, 0, 2+len(f.Key)+1+len(f.Value))
	body = append(body, kl...)
	body = append(body, f.Key...)
	if f.Found {
		body = append(body, 1)
	} else {
		body = append(body, 0)
	}
	body = append(body, f.Value...)

	head := make([]byte, 8)
	head[0], head[1], head[2], head[3] = magic, version, byte(f.Op), f.Status
	binary.BigEndian.PutUint32(head[4:8], uint32(len(body)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

// readFrame 先读满 8 字节头，校验魔数与版本，再按 bodyLen 读满 body。
// 用 io.ReadFull 而不是单次 Read，保证不会拿到半帧。
func readFrame(r io.Reader) (Frame, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return Frame{}, err
	}
	if head[0] != magic || head[1] != version {
		return Frame{}, ErrBadFrame
	}
	n := binary.BigEndian.Uint32(head[4:8])
	if n > maxFrame {
		return Frame{}, fmt.Errorf("%w: size %d", ErrBadFrame, n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return Frame{}, err
	}
	if len(body) < 3 {
		return Frame{}, ErrBadFrame
	}
	kl := int(binary.BigEndian.Uint16(body[:2]))
	if 2+kl+1 > len(body) {
		return Frame{}, ErrBadFrame
	}
	return Frame{
		Op:     Op(head[2]),
		Status: head[3],
		Key:    string(body[2 : 2+kl]),
		Found:  body[2+kl] == 1,
		Value:  append([]byte(nil), body[2+kl+1:]...),
	}, nil
}
```

### 3. 服务端：连接管理与并发

```go
// 文件：server.go
package kv

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"
)

// Server 把任意一个 Storage 暴露成 TCP 服务；它完全不关心底层是内存、WAL 还是 Raft。
type Server struct {
	ln   net.Listener
	st   Storage
	wg   sync.WaitGroup
	quit chan struct{}
	once sync.Once
}

func NewServer(addr string, st Storage) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{ln: ln, st: st, quit: make(chan struct{})}, nil
}

// Serve 循环接收连接，每个连接一个 goroutine；quit 用于优雅退出。
func (s *Server) Serve() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.wg.Wait()
				return nil
			default:
				return err
			}
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			s.handleConn(c)
		}(conn)
	}
}

func (s *Server) handleConn(c net.Conn) {
	for {
		fr, err := readFrame(c)
		if errors.Is(err, io.EOF) {
			return // 客户端正常关闭
		}
		if err != nil {
			log.Printf("read frame: %v", err)
			return
		}
		res, applyErr := s.st.Apply(Command{Op: fr.Op, Key: fr.Key, Value: fr.Value})
		out := Frame{Op: fr.Op, Status: okStatus, Key: fr.Key}
		if applyErr != nil {
			out.Status = errStatus
			out.Value = []byte(applyErr.Error())
		} else {
			out.Value = res.Value
			out.Found = res.Found
		}
		if err := writeFrame(c, out); err != nil {
			return
		}
	}
}

func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		close(s.quit)
		err = s.ln.Close()
	})
	return err
}
```

### 4. 客户端 SDK：同样实现 Storage

```go
// 文件：client.go
package kv

import (
	"errors"
	"net"
	"sync"
)

// 编译期断言：Client 必须满足与 Memory 完全相同的契约。
var _ Storage = (*Client)(nil)

// Client 是一个远程 Storage，调用方可以无缝把本地实现换成网络客户端。
type Client struct {
	mu   sync.Mutex
	conn net.Conn
}

func Dial(addr string) (*Client, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c}, nil
}

func (c *Client) Apply(cmd Command) (Result, error) {
	// 单连接复用必须串行化，否则并发写会让两个帧的字节交错，接收方解不出完整帧。
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return Result{}, ErrClosed
	}
	in := Frame{Op: cmd.Op, Status: reqStatus, Key: cmd.Key, Value: cmd.Value}
	if err := writeFrame(c.conn, in); err != nil {
		return Result{}, err
	}
	out, err := readFrame(c.conn)
	if err != nil {
		return Result{}, err
	}
	if out.Status == errStatus {
		return Result{}, errors.New(string(out.Value))
	}
	return Result{Value: out.Value, Found: out.Found}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}
```

### 5. 帧往返测试（用 net.Pipe，不开真实端口）

```go
// 文件：frame_test.go
package kv

import (
	"net"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	want := Frame{Op: OpSet, Status: reqStatus, Key: "user:7", Value: []byte("benji")}
	errCh := make(chan error, 1)
	go func() { errCh <- writeFrame(c1, want) }()
	got, err := readFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got.Op != want.Op || got.Key != want.Key || string(got.Value) != "benji" {
		t.Fatalf("got %+v", got)
	}
}
```

## 失败路径与边界条件

| 失败场景 | 现象 | 处理 / 代价 |
| --- | --- | --- |
| 半包与粘包 | 单次 `Read` 拿到 3 字节或两帧拼一起 | 8 字节定长头 + `io.ReadFull`，按 `bodyLen` 读满 |
| 超大 `bodyLen` | 攻击者声明 4 GiB 导致 OOM | `maxFrame` 上限，超限立即断连 |
| 魔数/版本不符 | 端口被别的协议误连 | 校验 `magic` 与 `version`，返回 `ErrBadFrame` |
| 并发写同一连接 | 两帧字节交错，解码错乱 | `Client.mu` 串行化；多连接要靠连接池提升吞吐 |
| 错误回传 | 响应里只放了错误文本 | 错误链与哨兵语义丢失，调用方无法 `errors.Is` |
| 慢客户端 | 长时间不发完整帧占用 goroutine | 需读超时（本讲未做，见扩展方向） |
| 优雅退出 | 长连接不关闭时 `wg.Wait` 一直阻塞 | 需连接注册表或 `SetDeadline` 强制收尾 |

<Callout type="warn" title="两个最容易被忽略的点" text="第一，错误帧里塞的是字符串，第 111 讲精心设计的 ErrNotFound 到客户端就退化成了普通 error，第 113 讲会把错误码写进帧状态位来修。第二，Server.Close 只关监听器，已建立的连接仍在跑，生产必须给每个连接设读超时或在 Close 时统一 Deadline。" />

## 深入一步：帧格式为什么这么排

<Compare title="拆包的两种思路" :panels='[
  {"label":"按分隔符拆包","code":"line, _ := reader.ReadString(0x0A)","note":"value 含换行就出错，且无法承载二进制数据"},
  {"label":"定长头加 ReadFull","code":"io.ReadFull(r, head); io.ReadFull(r, body)","note":"先读满 8 字节头再按长度读满 body，二进制安全"}
]' />

把 `magic` 与 `version` 放在头两个字节，是为了让服务端在解析任何长度字段之前就能拒绝垃圾连接；`Op` 单独占一个字节，是为了它能与第 111 讲的 `Op` 常量一一对应——网络帧、WAL、Raft 日志从此共用同一套操作编号。

<Term word="大端序" text="多字节整数把高位字节放在低地址，网络协议的事实标准，encoding/binary 的 BigEndian 即为此" />

## 测试与压测思路

- **编解码**：`TestFrameRoundTrip` 用 `net.Pipe` 在内存里做全双工往返，无需监听端口，适合放进 CI。
- **畸形输入**：构造魔数错误、`bodyLen` 超上限、`keyLen` 超出 body 的字节流，逐个断言返回 `ErrBadFrame` 且不 panic。
- **并发**：`go test -race` 下用多个 goroutine 共用一个 `Client`，验证 `mu` 确实防止了帧交错。
- **压测**：单连接串行是本版吞吐天花板。用 `net.Listener` 起服务端，逐步增加客户端连接数，观察 QPS 曲线；当单连接 RT 主导时，就说明该上连接池了。同时对比第 111 讲的 `BenchmarkMemorySet`，量化一次网络往返带来的延迟与分配开销。

## 工程实践 / 真实场景

真实网关不会让一条连接独占到底：常见的做法是每客户端维护一个小连接池，`Apply` 时借还连接；给每个 socket 设 `SetDeadline` 防慢客户端；用 TLS 与 token 做认证；把 `status` 扩展成结构化错误码而非文本。这一层还常承担背压——当服务端处理不过来时，用有界队列拒绝新请求，而不是让 goroutine 无限堆积。

## 可继续扩展的方向

- 连接池 + 请求 ID，支持单连接上的流水线并发（当前帧没有 ID 字段）。
- 结构化错误码替代错误文本，恢复 `errors.Is` 的可判定性。
- 超时、重试与熔断（第 79 讲），以及断线自动重连。
- 最重要的下一步：进程一重启数据就没了，且所有写都挤在一把锁上——第 113 讲用 WAL 和锁分片解决。

## 动手练一练

<Exercise title="给帧加上请求 ID 支持流水线" task="在 Frame 里增加一个 4 字节 RequestID，使服务端可以乱序回包、客户端能把响应匹配回原请求。写出改动要点与一个并发调用测试。" :hints='["请求 ID 放在 8 字节头里最省事，头会变成 12 字节","客户端需要一张 map[uint32]chan Frame 做等待与唤醒","注意响应到来时先加锁再取 chan，取完立刻删除"]' solution="head 扩到 12 字节，第 8 到 11 字节放大端 uint32 的 RequestID；writeFrame/readFrame 都多带一个字段。Client 维护 pending map，Apply 先分配自增 ID 并注册 channel，再写帧并等待该 channel；读循环单独 goroutine 按 ID 分发到对应 channel 后 close 它。测试起 N 个 goroutine 并发 Apply，断言每个返回值与自己的 key 一致。" explain="没有请求 ID 时，一问一答必须串行；加了 ID 才能在同一连接上并行在途请求，这是把单连接吞吐提上去的关键一步。" />

## 随堂检测

<Quiz no="112-1" q="关于 TCP 粘包，下面哪种说法是对的？" :options='["TCP 会保证每次 Write 对应一次 Read","粘包是 TCP 的 bug，换 UDP 就没有了","TCP 是字节流没有消息边界，需要协议自己用长度或分隔符切分","只要把 socket 缓冲区调大就不会粘包"]' answer="2" explain="TCP 只保证字节有序可靠到达，不保留写边界。定长头加 ReadFull 是应用层自己重建消息边界的标准做法，UDP 虽然保留数据报边界却失去了可靠与有序。" />

<Predict no="112-2" q="客户端连续并发调用同一个 Client.Apply 且没有加互斥锁，最可能发生什么？" :options='["服务端 panic","两个帧的字节交错，readFrame 解出错误数据","数据自动排队，一切正常","连接被操作系统复位"]' answer="1" output="两个帧的字节交错，readFrame 解出错误数据" explain="writeFrame 分两次 Write（head 与 body），并发时四个 Write 可能交叉；接收方按长度读到的 body 是拼接错乱的字节，校验魔数或 keyLen 时就崩了。" />

<StepBox title="一次 SET 请求走过的完整链路" :steps='[{"title":"第 1 步","body":"调用方构造 Command{Op: OpSet, Key, Value} 并调用 Client.Apply","tip":"签名与本地 Memory 完全一致"},{"title":"第 2 步","body":"Client 加锁，writeFrame 写 8 字节头与 body","tip":"头里的 Op 就是 Command.Op"},{"title":"第 3 步","body":"TCP 把字节流送过去，可能分多次到达","tip":"接收方只认长度，不认到达次数"},{"title":"第 4 步","body":"readFrame 用 ReadFull 读满头与 body，还原 Frame","tip":"校验 magic 与 version"},{"title":"第 5 步","body":"Server 调 Storage.Apply，把结果封装成响应帧回写","tip":"Storage 后面可以换成任意实现"}]' startLabel="开始追踪" />

## 一页纸速记

<KnowledgeCard :points='["定长头加 io.ReadFull 是处理粘包与半包的标准答案","Client 与 Memory 实现同一个 Storage，本地与远程对调用方透明","帧头带 magic 与 version，解析前就能拒绝垃圾连接并保留演进空间","bodyLen 必须有上限，否则一个伪造长度就能打爆内存","单连接上写帧必须串行，或用请求 ID 做流水线"]' :pitfalls='["用单次 Read 代替 io.ReadFull，半包时解出脏数据","错误帧只传字符串，丢掉了哨兵错误与错误链","Server.Close 只关监听器，长连接会让优雅退出卡住"]' summary="网络层只负责把 Command 可靠地搬来搬去，契约与存储实现一行都不用改。" />

## 小结与下一讲

1. 用「8 字节定长头 + 定长 body」定义帧格式，`io.ReadFull` 一次读满，彻底解决粘包与半包，并用长度上限与魔数校验守住恶意输入。
2. 服务端每连接一个 goroutine、客户端用互斥锁串行化写帧，两边都建立在第 111 讲的 `Storage` 之上。
3. 因为 `Client` 也实现了 `Storage`，调用方从本地切到远程零改动；同时我们看清了错误退化为字符串、单连接串行、退出阻塞三处待修的边界。

但这一版仍然把数据只放在内存里，进程一重启就全丢，而且写操作全挤在一把锁上。下一讲用 WAL 追加写与崩溃恢复解决持久性，用锁分片提升并发。

[下一讲：KV 贯穿线 ③ WAL、持久化与并发控制](/projects/kv-3-async)
