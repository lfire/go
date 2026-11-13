---
title: 第 072 讲 · RPC 原理与自研最小 RPC
description: 从一次 HTTP 请求出发，亲手推导出桩、编解码、协议、超时与错误传递，得到一个能跑的最小 RPC
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="从 HTTP 一步步推导出 RPC 要解决什么，并写出可运行的最小实现" prereq="第 071 讲 · 文件、对象存储与断点续传" />

# 第 072 讲 · RPC 原理与自研最小 RPC

> 我们已经会用 net/http 写服务，也知道怎么把数据序列化成 JSON 发来发去。可一旦方法变多、调用变频繁，你会发现自己在反复做同一堆体力活：拼 URL、选 HTTP 方法、塞 body、判状态码、解响应。RPC（Remote Procedure Call，远程过程调用）要消灭的正是这些样板，它让「调用另一台机器上的函数」看起来像调用本地函数一样自然。本讲不直接搬出 gRPC，而是从一段最普通的 HTTP 代码出发，一层层加上协议、编解码、桩与超时，最终得到一个你自己写得出来、也拆得开的最小 RPC。理解了这个推导过程，后面所有框架都只是同一套骨架的不同实现。

## 这一讲你会得到什么
<CheckList id="l72-goals" title="本讲目标" :items='["说清远程调用与本地调用的本质差别，以及为什么它绕不开序列化与网络","从 HTTP 请求一步步推导出协议报文、编解码器、服务端分发与客户端桩","动手写出带 context 超时、结构化错误传递的最小可用 RPC","建立后续 gRPC 与自研框架学习的统一心智模型"]' />

## 1. 先建立直觉

把本地函数调用想成在自己工位上递一张便签给同事：写清楚函数名和参数，对方算完把结果写回来，一瞬间完成。RPC 则是给异地分公司打电话：便签必须先变成能在电话线里传的「话术」（序列化），由接线员拨号（寻址与传输），对方接线员听写还原成便签（反序列化），交给人算完再沿原路送回。中间任何一环——号码错了、线路断了、对方加班不接——都会让这次「调用」失败，而本地调用根本不会有这些问题。

所以 RPC 的本质不是什么黑科技，而是把一次函数调用拆成「编码 → 传输 → 解码 → 执行 → 回传」五段，再用一层「桩」把这几段重新包成函数的样子。REST 之所以让人累，是因为它把这五段摊开给每个业务开发者自己写；RPC 的价值，是把它们收进同一个可复用的通道。先看一次调用从头到尾经过哪些关口，后面每一段代码都是在填其中一格。

<ConceptDiagram type="rpc-flow" title="一次 RPC 调用的完整路径" caption="客户端桩把参数编码成报文，经网络送到服务端骨架，反射分发到真实函数，结果原路返回" />

<FlowTimeline :steps='[{"title":"调用桩","desc":"业务代码像调本地函数一样传入方法名与参数","tone":"blue"},{"title":"编码","desc":"桩把请求序列化成约定报文","tone":"blue"},{"title":"传输","desc":"经统一端点发往服务端","tone":"blue"},{"title":"解码","desc":"服务端骨架还原出 Request","tone":"green"},{"title":"分发","desc":"按 Method 在注册表里找到函数","tone":"green"},{"title":"执行","desc":"反射调用真实业务函数","tone":"green"},{"title":"回传","desc":"结果或错误编码成 Response 送回","tone":"orange"},{"title":"解码返回","desc":"桩把响应还原成业务拿到的值","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 起点：一切都能用一段 HTTP 写

先看最朴素的做法。把「调用 Add 并传 3 和 4」翻译成一次 HTTP：服务端开一个 `/add` 路径，客户端手工拼 JSON、手工 POST、手工解响应。这里用标准库 `net/http/httptest` 起一个临时服务，整段代码不依赖外部端口，`go run` 就能跑。

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// 阶段一：最朴素的 HTTP 调用——把「调用哪个方法、传什么参数」全塞进请求里。
// 调用方必须自己知道 URL、自己拼 body、自己解析响应，这就是 RPC 要消灭的样板代码。
func main() {
	// 服务端：一个 URL 对应一个方法
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/add" {
			http.NotFound(w, r)
			return
		}
		var p struct{ A, B int }
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &p)
		_ = json.NewEncoder(w).Encode(map[string]int{"result": p.A + p.B})
	}))
	defer srv.Close()

	// 客户端：手工拼 URL、手工序列化、手工反序列化
	payload := `{"A":3,"B":4}`
	resp, err := http.Post(srv.URL+"/add", "application/json", strings.NewReader(payload))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var out map[string]int
	_ = json.NewDecoder(resp.Body).Decode(&out)
	fmt.Println("HTTP 调用结果:", out["result"])
}
```

<OutputBox title="运行结果" output="HTTP 调用结果: 7" note="功能没问题，但每加一个方法，客户端都要再写一遍拼 URL、序列化、解码、判错的四件套。" />

痛点是结构性的：方法名绑死在 URL 上，参数怎么编码、错误怎么表示全靠口头约定。当方法有几十个、还要给每个都加上超时和错误码时，这种写法必然崩塌。第一步抽象，就是把「调哪个方法」从路径搬进消息体。

### 2.2 把「调哪个方法」搬进消息体

我们定义一份双方都认的线上协议：`Request` 里带 `ID`、`Method`、`Params`，`Response` 里带 `ID`、`Result`、`Error`。有了 `Method` 字段，所有方法就能共用一个端点；有了 `ID`，并发请求和响应才不会串包。编码层收敛成函数，换序列化方式时上层完全不用改。

```go
package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// RPCRequest / RPCResponse 是我们自己定的线上协议。
// Method 把「调哪个函数」从 URL 搬进了消息体，于是所有方法共用一个端点。
type RPCRequest struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type RPCResponse struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error,omitempty"`
}

// 编码器的价值：把结构体与字节流的互转收敛到一个函数。
// 换 json / gob / protobuf 时，调用层一行都不用动，这就是协议与编码解耦。
func encodeJSON(v any) ([]byte, error) { return json.Marshal(v) }

func encodeGob(v any) ([]byte, error) {
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(v)
	return buf.Bytes(), err
}

func main() {
	req := RPCRequest{ID: 1, Method: "Add", Params: json.RawMessage(`{"A":3,"B":4}`)}
	b1, _ := encodeJSON(req)
	b2, _ := encodeGob(req)
	fmt.Println("JSON 报文:", string(b1))
	fmt.Printf("同样一条请求：JSON %d 字节，gob %d 字节\n", len(b1), len(b2))
}
```

<LineByLine code="type Request struct {\n    ID     uint64\n    Method string\n    Params []byte\n}" :notes='["定义线上协议报文，所有方法共用一个结构，这是统一端点的前提","ID 把并发请求与响应一一对应，避免响应串包","Method 把调哪个函数从 URL 搬进消息体，服务端据此查表分发","Params 用字节切片承接序列化后的参数，协议与具体编码方式解耦","右括号收尾；响应侧再补 Result 与 Error 即可传结果与错误"]' title="协议三要素逐行拆解" />

## 3. 深入一步

### 3.1 服务端：注册表 + 反射分发

有了统一报文，服务端只需要一张「方法名 → 函数值」的表。用 `reflect.ValueOf` 把普通函数注册进去，收到请求后按 `Method` 查表、用 `Call` 触发，就不必为每个方法手写一段 switch。这张表加这段分发逻辑，就是所谓的「骨架 skeleton」。标准库 `net/rpc` 也是同一副骨架，只是把编码换成了 gob、把协议做成了内置格式；自己写一遍，才能真正替换其中任何一环。

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
)

type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type Response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Server 用一张方法表把名字映射到可调用的函数值，reflect 让我们免于为每个方法写 switch。
type Server struct {
	mu      sync.RWMutex
	methods map[string]reflect.Value
}

func NewServer() *Server { return &Server{methods: make(map[string]reflect.Value)} }

func (s *Server) Register(name string, fn any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods[name] = reflect.ValueOf(fn)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		_ = json.NewEncoder(w).Encode(Response{Error: "bad request"})
		return
	}
	resp := Response{ID: req.ID}
	s.mu.RLock()
	fn, ok := s.methods[req.Method]
	s.mu.RUnlock()
	if !ok {
		resp.Error = "method not found: " + req.Method
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	// 为求最小，约定注册的函数都是 func(int, int) (int, error) 形态，
	// 真实框架会用 reflect 解析入参类型并按 Params 逐个填充。
	var args struct{ A, B int }
	_ = json.Unmarshal(req.Params, &args)
	out := fn.Call([]reflect.Value{reflect.ValueOf(args.A), reflect.ValueOf(args.B)})
	if e := out[1].Interface(); e != nil {
		resp.Error = fmt.Sprint(e)
	} else {
		resp.Result = out[0].Interface()
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func Add(a, b int) (int, error) { return a + b, nil }

func main() {
	s := NewServer()
	s.Register("Add", Add)
	ts := httptest.NewServer(s)
	defer ts.Close()
	fmt.Println("RPC 服务端已就绪:", ts.URL)
}
```

把这条路和起点对照，抽象到底买到了什么一目了然：

| 对比维度 | 手写 HTTP / REST | 自研最小 RPC |
| --- | --- | --- |
| 寻址 | 靠 URL 路径区分方法 | 统一端点，靠 Method 字段 |
| 参数 | 手工拼 query 与 body | 桩自动编码 |
| 调用形态 | 每次写 http.Post 与解码 | 像普通函数一样 Call |
| 超时 | 各处零散设置 | context 沿调用链统一传播 |
| 错误 | HTTP 状态码夹在 body 里 | 结构化 Error 字段 |
| 新增方法 | 复制一整套样板 | 注册进表即可 |

<Term word="桩 stub" text="客户端侧的替身，把远程调用伪装成本地函数，负责编码、发送、解码与抛错" />
<Term word="骨架 skeleton" text="服务端侧的分发器，负责收报文、解码、查方法表、调用真实函数并回写结果" />
<Term word="序列化" text="把内存里的结构体转成可跨网络传输的字节流，反方向叫反序列化" />

<Callout type="warn" title="别把 RPC 当成没有延迟的本地调用" text="跨进程调用可能超时、可能重复到达、可能乱序，网络故障和本地 panic 完全是两回事。任何把远程调用写得和本地函数一样想当然的代码，都会在弱网和下游抖动时出问题。" />

### 3.2 五段链路为什么一段都不能省

把「本地调用」升级成「远程调用」，消失的其实是三样东西：共享的地址空间、共享的类型定义、共享的控制流。本地调用之所以几乎不会失败，是因为调用方和被调用方活在同一个进程里——参数按同一套 ABI 放在栈或寄存器上，函数返回就是一条跳转指令，栈帧天然保证「请求与结果一一对应」。一旦把被调用方搬到另一台机器，这三点全部失效，于是每一次调用都必须人为补回五段，而每一段都精确对应一类本地调用根本不存在的故障。

- **编码**：对端不认识本端的内存布局。`int` 在两端可能是 32 位或 64 位，多字节整数的字节序可能不同，指针更是完全无意义。必须约定一种与语言、与机器无关的线上表示，这就是序列化。对应故障域是「类型系统不共享」。
- **传输**：本地是控制流跳转，远程要经过网络，于是引入分布式系统最麻烦的**部分失败**：请求可能发出去了但响应永远没回来，调用方无法区分「对端根本没收到」「收到了但算到一半崩了」「算完了但回包丢了」这三种情况。对应故障域是「控制流断裂」。
- **解码**：对端按约定把字节还原成参数，还要能识别并拒绝不认识的报文，否则双方发版不同步时会静默算错而不报错。对应故障域是「版本错配」。
- **执行**：真实函数在远端跑，它自己会 panic、会超时、可能被重复触发。写操作尤其要面对「超时不等于未执行」——你以为没成功而重试，对端其实已经扣过一次库存。对应故障域是「副作用不确定」。
- **回传**：结果或错误要能送回，并在高并发下与正确的请求对上号。这正是协议里 `ID` 字段存在的唯一理由：没有它，多个并发请求的响应会串包。对应故障域是「响应乱序与丢失」。

把本地调用与远程调用逐项对照，能看清「桩」到底替业务扛下了什么。

| 能力 | 本地函数调用 | 远程过程调用 | 缺失时补回的手段 |
| --- | --- | --- | --- |
| 传参 | 同一内存布局直接读 | 两端布局可能不同 | 序列化成约定字节流 |
| 寻址 | 编译期确定函数地址 | 需知道对端在哪、调哪个方法 | 端点加 Method 字段 |
| 失败模型 | 要么返回要么 panic，无中间态 | 存在请求发出但结果未知的中间态 | 超时加结构化错误加幂等键 |
| 请求与结果配对 | 栈帧天然一一对应 | 多请求共用一条连接，响应可能乱序 | 协议里的请求 ID |
| 耗时 | 纳秒到微秒，可忽略 | 毫秒到秒，且抖动大 | deadline 沿调用链传播 |

<SequenceDiagram title="一次 RPC 的五段链路与失败点" caption="编码、传输、解码、执行、回传逐段对应一类本地调用不存在的故障" :actors='["Client","Stub","Network","Skeleton","Service"]' :steps='[{"from":"Client","to":"Stub","label":"Call Add(3,4)","note":"业务像调本地函数","kind":"call"},{"from":"Stub","to":"Stub","label":"编码并分配 ID","note":"序列化与配对","kind":"self"},{"from":"Stub","to":"Network","label":"发送报文","note":"进入不可靠网络","kind":"call"},{"from":"Network","to":"Skeleton","label":"投递到骨架","note":"可能丢包或延迟","kind":"call"},{"from":"Skeleton","to":"Skeleton","label":"解码并查表","note":"按 Method 分发","kind":"self"},{"from":"Skeleton","to":"Service","label":"调用真实 Add","note":"执行业务函数","kind":"call"},{"from":"Service","to":"Skeleton","label":"返回结果 7","note":"或抛业务错误","kind":"return"},{"from":"Skeleton","to":"Network","label":"回写响应","note":"带同一请求 ID","kind":"call"},{"from":"Network","to":"Stub","label":"送达客户端","note":"可能超时未达","kind":"call"},{"from":"Stub","to":"Client","label":"解码并还原","note":"结果或 error","kind":"return"}]' />

### 3.3 序列化与传输协议：两种以上替代方案的取舍

选序列化格式，本质是选「可读性、性能、跨语言、是否有 schema」这四者的组合，没有通吃项。自研最小 RPC 用 JSON 是因为标准库自带、肉眼可调试；换成 gob 能省带宽却把双方锁死在 Go；换成 Protobuf 才真正获得跨语言与字段级兼容，代价是引入 IDL 与代码生成。传输层同理：HTTP/1.1 文本协议调试方便但头部冗余、无法多路复用；自定义 TCP 二进制帧最省却要从零处理粘包与连接复用；HTTP/2 二进制帧居中，是 gRPC 的选择。

| 方案 | 可读性 | 跨语言 | schema 约束 | 相对体积 | 主要代价 | 适用边界 |
| --- | --- | --- | --- | --- | --- | --- |
| JSON over HTTP/1.1 | 高，可直接 curl | 是 | 弱，靠口头约定 | 大 | 文本解析慢、无字段号兼容 | 内部小工具、调试期 |
| gob over TCP | 低 | 否，仅 Go | 中，随结构体走 | 中 | 语言锁死、类型变更易不兼容 | 纯 Go 内部服务 |
| Protobuf over HTTP/2 | 低，需解码 | 是 | 强，proto 契约 | 小 | 需 protoc 与 IDL 评审 | 跨语言、长期演进的服务 |

「二进制一定更小」是常见误解，用一段不依赖第三方库的代码就能量出反例：

```go
package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
)

// Order 是本章最小 RPC 里的一条业务消息。
type Order struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

func main() {
	o := Order{ID: 150, Name: "go"}
	j, _ := json.Marshal(o)

	var buf bytes.Buffer
	_ = gob.NewEncoder(&buf).Encode(o)

	fmt.Printf("JSON: %d 字节 内容=%s\n", len(j), j)
	fmt.Printf("gob : %d 字节（含类型描述，仅 Go 可读）\n", buf.Len())
}
```

```text
JSON: 22 字节 内容={"id":150,"name":"go"}
gob : 46 字节（含类型描述，仅 Go 可读）
```

逐字段解释：`22 字节` 是 `{"id":150,"name":"go"}` 的实际长度，字段名以文本形式重复出现，这是可读性的代价；`gob 46 字节` 反而更大，因为 gob 是 Go 专属的自描述编码，首条报文要携带类型元信息。gob 的优势在批量、同构、且只在 Go 之间传递的数据，而不是这一条小报文。这也说明选型必须看真实报文特征，而不是背结论。

### 3.4 可验证实验：用 -race 抓出桩里的自增竞态

原文 4.1 的客户端桩里有一行 `c.seq++`，用来给每个请求分配递增 ID。单 goroutine 下没问题，但桩天生要被并发调用——多个 goroutine 同时做「读改写」是典型的数据竞争：可能两个请求拿到同一个 ID，回包就会串到错误的调用方。把这段逻辑抽出来做成测试，用竞态检测器复现：

```go
package main

import (
	"sync"
	"testing"
)

type Client struct{ seq uint64 }

func (c *Client) next() uint64 {
	c.seq++ // 与 4.1 相同的写法：读改写不是原子的
	return c.seq
}

func TestSeqUnderRace(t *testing.T) {
	c := &Client{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.next()
		}()
	}
	wg.Wait()
}
```

```bash
go test -race -run TestSeqUnderRace ./...
```

```text
WARNING: DATA RACE
Write at 0x00c0000120a0 by goroutine 8:
  main.(*Client).next()
      race_test.go:11 +0x44
Previous write at 0x00c0000120a0 by goroutine 7:
  main.(*Client).next()
      race_test.go:11 +0x44
==================
FAIL
```

逐字段解释：`WARNING: DATA RACE` 说明检测器发现同一内存地址被两个 goroutine 无同步地写；`Write at 0x... by goroutine 8` 指出冲突写发生在第 11 行的 `c.seq++`；`Previous write ... by goroutine 7` 给出与它冲突的上一次访问；具体地址与 goroutine 编号每次运行都不同，属于正常现象；`FAIL` 表示竞态让测试以失败收场。注意 `-race` 只报告「确实跑到并被采样到」的冲突时序，所以循环起 100 个 goroutine 是为了提高复现概率——这也呼应第 049 讲的结论：抓不到竞态不等于代码正确。

修复方向是把「读改写」换成一次原子操作，Go 1.19 起提供的 `atomic.Uint64` 最直白：

```go
package main

import (
	"fmt"
	"sync/atomic"
)

type SafeClient struct{ seq atomic.Uint64 }

// Next 用原子自增分配 ID，多 goroutine 并发也保证唯一且递增。
func (c *SafeClient) Next() uint64 { return c.seq.Add(1) }

func main() {
	c := &SafeClient{}
	fmt.Println(c.Next(), c.Next(), c.Next())
}
```

<OutputBox title="运行结果" output="1 2 3" note="atomic 的 Add 把读改写合并为一条指令，ID 不再重复，响应串包的根因被消除。" />

## 4. 工程实践

### 4.1 客户端桩与 deadline 传播

最后补上桩，并顺手把超时做成一等公民。关键点有三个：用 `context.WithTimeout` 从父 ctx 派生带 deadline 的 ctx，把 deadline 一路传到 `http.NewRequestWithContext`；`defer cancel` 保证提前返回也释放定时器；把服务端的 `Error` 字段还原成 Go 的 `error`。下面故意让服务端睡 3 秒、客户端只等 200 毫秒，用来验证超时确实生效。

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"
)

type request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}
type response struct {
	ID     uint64 `json:"id"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// Client 就是桩：把远程调用伪装成本地方法，拼报文、发请求、解响应都被藏起来。
type Client struct {
	endpoint string
	httpc    *http.Client
	seq      uint64
}

func NewClient(endpoint string) *Client {
	return &Client{endpoint: endpoint, httpc: &http.Client{Timeout: 2 * time.Second}}
}

func (c *Client) Call(ctx context.Context, method string, args any) (any, error) {
	c.seq++
	params, _ := json.Marshal(args)
	body, _ := json.Marshal(request{ID: c.seq, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc 传输失败: %w", err)
	}
	defer res.Body.Close()
	var out response
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("rpc 解码失败: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("rpc 业务错误: %s", out.Error)
	}
	return out.Result, nil
}

func main() {
	// 慢服务：3 秒才回；客户端只等 200 毫秒，用来演示 deadline 生效。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		_ = json.NewEncoder(w).Encode(response{Result: 7})
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result, err := c.Call(ctx, "Add", struct{ A, B int }{3, 4})
	fmt.Printf("结果: %v, 错误: %v\n", result, err)
}
```

<Compare :panels='[{"label":"手写 HTTP 调用","code":"body := encode(args)\nres := post(endpoint, body)\nout := decode(res)","note":"URL、序列化、状态码、反解码全散落在业务里，每加一个方法就复制一遍"},{"label":"RPC 桩调用","code":"out, err := client.Call(ctx, method, args)\nif err != nil { return err }\nreturn out, nil","note":"方法名、编解码、超时、错误统一收敛进桩，业务只面对一次普通函数调用"}]' title="同一件事的两种写法" />

真实落地时还有几条硬经验。协议上，`ID` 保证多路复用不乱序，版本号字段给将来兼容留后路，错误要用稳定的错误码而不是给人看的字符串。可靠性上，超时必须由调用链顶层层层下传，绝不能靠每个 `Call` 自觉设置；写操作要考虑「超时不等于未执行」，因此必须配幂等键。连接层面，复用 `http.Client` 与连接池，别每次调用新建客户端。可观测上，桩是打点的黄金位置——在这里统一埋耗时、错误率与调用量，正好是后面 RED 指标与链路追踪的入口。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-072-rpc-basics`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-072-rpc-basics)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 一次 RPC 的失败矩阵

把五段链路逐一问「这里会怎么坏」，就得到一张能直接用于评审的清单。每一行都是本地调用根本不会遇到、而远程调用必须显式设防的情况：

| 故障现象 | 发生在哪一段 | 本地调用为何没有 | 最小防线 |
| --- | --- | --- | --- |
| 请求发出但结果未知 | 传输 | 控制流不断裂，没有中间态 | 超时加幂等键，承认部分失败 |
| 超时被误判为未执行，重复扣减 | 执行与回传 | 不存在已执行却没返回的状态 | 幂等键加状态机 |
| 并发响应串包 | 回传 | 栈帧天然把请求与结果配对 | 协议里的请求 ID |
| 新旧版本字段含义错配 | 解码 | 双方共用同一份类型定义 | 协议版本号加结构校验 |
| 下游抖动时每次调用新建连接 | 传输 | 没有连接这个概念 | 复用 http.Client 与连接池 |
| 弱网下重试把下游打垮 | 传输与执行 | 无网络放大效应 | 重试预算加退避 |

### 4.3 两条真实工程误解

**误解一：RPC 让远程调用长得像本地函数，所以可以像本地一样不处理超时与重试。** 桩把编码、传输、解码都藏起来，很容易让人连「这是跨进程调用」也一起忘了。最常见的写法是在中间层顺手 `context.Background()` 起一个新 ctx，或者干脆不设 deadline——弱网或下游卡死时，这次调用会永久挂住 goroutine 与连接。

<Compare :panels='[{"label":"错误：重置 ctx","code":"ctx := context.Background()\nout := call(ctx)","note":"剪断 deadline、取消信号与 metadata 三条链，慢调用无人叫停"},{"label":"正确：派生 ctx","code":"ctx, cancel := context.WithTimeout(parent, d)\ndefer cancel()\nout := call(ctx)","note":"沿用父预算，父子取更早者，提前返回也释放定时器"}]' title="中间层该怎么处理传入的 ctx" />

**误解二：用 HTTP 200 把错误塞进 body 就够了，不需要区分传输错误与业务错误。** 这种写法把所有失败都返回 200 加一段错误字符串，调用方靠比对字符串判定。它有两个致命缺口：连接断开、超时这类传输失败根本没有 HTTP 响应，200 永远盖不住；而业务错误用给人看的字符串表达，既无法稳定判定，也无法跨语言映射成状态码或决定是否重试。

<Callout type="warn" title="200 盖不住传输层失败" text="正确做法是把失败分三层：连接与超时等传输错误走 error 返回，框架与协议错误走稳定状态码，业务错误走结构化 Error 字段或错误码；调用方按层判定，绝不比对错误文本。" />

### 4.4 与后续阶段的呼应

本讲埋下的三颗种子会在后面反复发芽。「请求 ID」对应「响应与请求配对」，到 L8 会升级成幂等键与去重表，见 [/distributed/distributed-lock](/distributed/distributed-lock) 的 SETNX 租约与幂等键设计；「超时不等于未执行」正是 [/distributed/distributed-tx](/distributed/distributed-tx) 里消息最终一致要处理的老问题；而「部分失败」在理论上的名字就叫分区，[/distributed/cap-base](/distributed/cap-base) 会把它放进 CAP 框架里重新审视。到了 L10，[/projects/micro-seckill](/projects/micro-seckill) 的下单接口靠本讲的幂等键挡住重复请求，[/projects/micro-ecommerce](/projects/micro-ecommerce) 的订单链路则把五段放大成跨服务调用，超时预算交给第 074 讲的 deadline 传播接手。

<Quiz q="RPC 协议里之所以必须引入请求 ID 字段，最直接对应五段链路里哪一段的什么问题？" :options='["编码段的类型不共享","传输段的队头阻塞","回传段多请求共用连接时响应乱序","解码段的版本错配"]' answer="2" explain="多个并发请求共用一条连接时，响应回来先后不定；ID 把每个响应与发出它的请求一一对应，避免串包。这是回传段独有的需求，本地调用靠栈帧天然配对。" />

<Predict q="把原文 4.1 客户端桩的 c.seq++ 原样放到 100 个并发 goroutine 里调用，并用 go test -race 运行，最可能的结果是什么？" :options='["输出 100 个连续且唯一的 ID，测试通过","竞态检测器报 DATA RACE，可能出现重复 ID，测试失败","直接编译报错","所有 ID 都是 0"]' answer="1" output="WARNING: DATA RACE ... FAIL" explain="c.seq++ 是读改写，多 goroutine 无同步并发时必然冲突；-race 会报告 DATA RACE 并让测试失败。改用 atomic.Uint64 的 Add 把读改写合成一条指令才能消除。" />

## 动手练一练
<Exercise title="给最小 RPC 补上方法不存在与参数错误的处理" task="基于本讲的 Server，扩展 ServeHTTP：Method 未注册时返回明确错误，Params 无法解码时也返回错误而不是 panic。再用客户端桩分别触发这两种情况并打印错误。" :hints='["查方法表用 fn, ok := s.methods[req.Method]，!ok 时回写 method not found","json.Unmarshal 会返回 error，把它转成结构化错误回传","客户端 Call 已把非空 Error 还原成 error，直接判定即可"]' solution="要点一：解码 Request 失败时不要 panic，构造 Response 并写入 Error 回传。\n要点二：查表用 fn, ok := s.methods[req.Method]，!ok 时回写 method not found。\n要点三：客户端 Call 已把非空 Error 还原成 error，业务侧统一用 errors.Is 或错误码判定，而不是比对字符串。" explain="把所有失败都收敛成结构化 Error，调用方才能用统一方式判定与重试；panic 一旦越过网络边界，对端只会看到连接断开，问题反而更难查。" />

## 随堂检测
<Quiz no="072-1" q="在最小 RPC 里，把「调用哪个方法」从 URL 路径改到消息体的 Method 字段，带来的最直接好处是什么？" :options='["所有方法共用一个端点，新增方法不必再写路由与样板代码","报文体积一定比 JSON 更小","不再需要序列化参数","HTTP 状态码会自动变成业务错误码"]' answer="0" explain="Method 让寻址从路径变成数据，服务端用一张注册表即可分发任意方法，这正是桩能收敛样板代码的前提。" />

<Predict no="072-2" q="代码 4 中服务端固定 sleep 3 秒才返回，客户端用 context.WithTimeout 设了 200 毫秒。运行结果会是什么？" :options='["正常打印 结果: 7, 错误: &lt;nil&gt;","打印 结果: nil 并带一个 rpc 传输失败 的超时错误","客户端一直阻塞到 3 秒后才返回","服务端 panic 导致进程退出"]' answer="1" output="结果: nil, 错误: rpc 传输失败: context deadline exceeded（由 ctx 与 http.Client 共同触发）" explain="deadline 通过 context 一路传到 http.Client.Do，2 秒客户端超时与 200 毫秒 ctx 中更短的先触发，请求被提前取消。这正是 RPC 必须把超时当一等公民、而非靠调用方自觉的原因。" />

<FillBlank no="072-3" tpl="客户端把远程调用伪装成本地函数的那层代码叫 ___ ；服务端负责解码并查表分发的那层叫 ___ 。" :answers='["桩~stub","骨架~skeleton"]' hint="一个在调用方，一个在服务方" explain="两者合起来才把编码、传输、解码、执行、回传五段包成一次函数调用的样子。" :explains='["调用方需要一段与本地函数同签名的替身，把入参编码、发请求、收解回复都包进去，屏蔽网络细节，这一层习惯称桩。","服务端要有对称的一层负责按协议解码、查表定位方法、调用后回写结果，它撑起整个处理框架，故与桩相对称为骨架。"]' />

## 一页纸速记
<KnowledgeCard :points='["RPC 不是黑科技：把函数调用拆成编码、传输、解码、执行、回传五段，再用桩包回函数形态","从 HTTP 推导的四步：手工 REST 到定义协议报文与编解码，再到注册表加反射分发，最后客户端桩加超时","Method 字段让所有方法共用端点，ID 让并发请求与响应一一对应","context 的 deadline 必须随调用链传播，错误要结构化回传而不是 panic"]' :pitfalls='["把远程调用当本地调用，忽略超时、重试与重复到达","协议里不带请求 ID，多路复用时响应会串包","用 panic 传递业务错误，越过网络后只剩连接断开"]' summary="一句话记住：RPC = 协议 + 编解码 + 桩与骨架 + 超时与错误契约，其余都是工程增强。" />

## 小结与下一讲

- **推导链**：HTTP 的样板代码逼出了协议报文、编解码器、注册表分发与客户端桩，四步得到最小可用 RPC。
- **关键设计**：Method 统一端点、ID 对应请求、context 传播 deadline、结构化 Error 回传，这四点决定了实现是否可靠。
- **工程红线**：跨进程调用天然有延迟与失败，必须显式处理超时、幂等与重试，不能照搬本地调用的心智。

[下一讲：Protobuf 与 gRPC 四种流](/microservice/protobuf)
