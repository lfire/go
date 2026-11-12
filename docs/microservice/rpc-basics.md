---
title: 第 72 讲 · RPC 原理与自研最小 RPC
description: 从一次 HTTP 请求出发，亲手推导出桩、编解码、协议、超时与错误传递，得到一个能跑的最小 RPC
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★★☆" time="50 分钟" goal="从 HTTP 一步步推导出 RPC 要解决什么，并写出可运行的最小实现" prereq="第 71 讲 文件、对象存储与断点续传" />

# 第 72 讲 · RPC 原理与自研最小 RPC

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

## 动手练一练
<Exercise title="给最小 RPC 补上方法不存在与参数错误的处理" task="基于本讲的 Server，扩展 ServeHTTP：Method 未注册时返回明确错误，Params 无法解码时也返回错误而不是 panic。再用客户端桩分别触发这两种情况并打印错误。" :hints='["查方法表用 fn, ok := s.methods[req.Method]，!ok 时回写 method not found","json.Unmarshal 会返回 error，把它转成结构化错误回传","客户端 Call 已把非空 Error 还原成 error，直接判定即可"]' solution="要点一：解码 Request 失败时不要 panic，构造 Response 并写入 Error 回传。\n要点二：查表用 fn, ok := s.methods[req.Method]，!ok 时回写 method not found。\n要点三：客户端 Call 已把非空 Error 还原成 error，业务侧统一用 errors.Is 或错误码判定，而不是比对字符串。" explain="把所有失败都收敛成结构化 Error，调用方才能用统一方式判定与重试；panic 一旦越过网络边界，对端只会看到连接断开，问题反而更难查。" />

## 随堂检测
<Quiz no="72-1" q="在最小 RPC 里，把「调用哪个方法」从 URL 路径改到消息体的 Method 字段，带来的最直接好处是什么？" :options='["所有方法共用一个端点，新增方法不必再写路由与样板代码","报文体积一定比 JSON 更小","不再需要序列化参数","HTTP 状态码会自动变成业务错误码"]' answer="0" explain="Method 让寻址从路径变成数据，服务端用一张注册表即可分发任意方法，这正是桩能收敛样板代码的前提。" />

<Predict no="72-2" q="代码 4 中服务端固定 sleep 3 秒才返回，客户端用 context.WithTimeout 设了 200 毫秒。运行结果会是什么？" :options='["正常打印 结果: 7, 错误: nil","打印 结果: nil 并带一个 rpc 传输失败 的超时错误","客户端一直阻塞到 3 秒后才返回","服务端 panic 导致进程退出"]' answer="1" output="结果: nil, 错误: rpc 传输失败: context deadline exceeded（由 ctx 与 http.Client 共同触发）" explain="deadline 通过 context 一路传到 http.Client.Do，2 秒客户端超时与 200 毫秒 ctx 中更短的先触发，请求被提前取消。这正是 RPC 必须把超时当一等公民、而非靠调用方自觉的原因。" />

<FillBlank no="72-3" tpl="客户端把远程调用伪装成本地函数的那层代码叫 ___ ；服务端负责解码并查表分发的那层叫 ___ 。" :answers='["桩~stub","骨架~skeleton"]' hint="一个在调用方，一个在服务方" explain="两者合起来才把编码、传输、解码、执行、回传五段包成一次函数调用的样子。" />

## 一页纸速记
<KnowledgeCard :points='["RPC 不是黑科技：把函数调用拆成编码、传输、解码、执行、回传五段，再用桩包回函数形态","从 HTTP 推导的四步：手工 REST 到定义协议报文与编解码，再到注册表加反射分发，最后客户端桩加超时","Method 字段让所有方法共用端点，ID 让并发请求与响应一一对应","context 的 deadline 必须随调用链传播，错误要结构化回传而不是 panic"]' :pitfalls='["把远程调用当本地调用，忽略超时、重试与重复到达","协议里不带请求 ID，多路复用时响应会串包","用 panic 传递业务错误，越过网络后只剩连接断开"]' summary="一句话记住：RPC = 协议 + 编解码 + 桩与骨架 + 超时与错误契约，其余都是工程增强。" />

## 小结与下一讲

- **推导链**：HTTP 的样板代码逼出了协议报文、编解码器、注册表分发与客户端桩，四步得到最小可用 RPC。
- **关键设计**：Method 统一端点、ID 对应请求、context 传播 deadline、结构化 Error 回传，这四点决定了实现是否可靠。
- **工程红线**：跨进程调用天然有延迟与失败，必须显式处理超时、幂等与重试，不能照搬本地调用的心智。

[下一讲：Protobuf 与 gRPC 四种流](/microservice/protobuf)
