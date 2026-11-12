---
title: 第 73 讲 · Protobuf 与 gRPC 四种流
description: 用 proto 定义契约，理解字段号与默认值，掌握一元与三种流，写出可演进的服务接口
---

<StageHeader stage="L7 · RPC 与微服务" level="★★★☆☆" time="45 分钟" goal="写出契约优先的服务接口" prereq="第 72 讲 RPC 原理与自研最小 RPC" />

# 第 73 讲 · Protobuf 与 gRPC 四种流

> 上一讲我们自己造了一份协议：Method 字段、ID 字段、JSON 报文，都能跑，但全靠口头约定。只要客户端和服务端不是同一天发布，字段含义、类型、增删就会立刻失控。工业界的解法是「契约优先」：把消息长什么样、服务有哪些方法，先写进一份中立的接口描述语言（IDL），再由工具生成各语言代码。Protobuf 就是这份 IDL 加它的二进制编码，gRPC 则是跑在这份契约上的 RPC 通道。本讲把契约、字段号、默认值、代码生成和四种调用流一次讲透，并说明为什么「字段号」是整条兼容性设计里最不能动的一根钉子。

## 这一讲你会得到什么
<CheckList id="l73-goals" title="本讲目标" :items='["读懂并写出一份 proto3 契约，理解字段号、默认值与标量类型","说清 protoc 代码生成产出的客户端桩与服务端接口分别是什么","区分数出一元、服务端流、客户端流、双向流四种调用形态及各自适用场景","掌握字段号不可复用、reserved、新增字段等版本演进红线"]' />

## 1. 先建立直觉

把上一讲的手写协议想成两个人用便签约定「第一格写名字、第二格写年龄」。便签能用的前提是双方记住格子的顺序——一旦哪天把「年龄」挪到第一格，对端读到的就是错的，而且不会报任何错，只会静默地算错。Protobuf 把这件事正式化：每个字段绑一个永不改变的**字段号**，线上传输只认号码不认名字；字段名随便改，号码绝不能动。这便是它比 JSON 更适合做跨服务契约的根本原因。

契约优先还带来一个额外好处：一份 `.proto` 文件是唯一事实来源，protoc 从它生成 Go 的结构体、客户端桩、服务端接口，客户端和服务端共享同一份类型，编译期就能挡住「方法名拼错」「参数类型不对」这类低级错误。再加上二进制编码省带宽、跨语言统一，微服务里的接口就从「口头约定」升级成了「可校验的资产」。

<ConceptDiagram type="rpc-flow" title="契约优先的 gRPC 调用" caption="一份 proto 生成双方共享的桩与消息类型，调用经由二进制编码在同一条 RPC 通道上完成" />

<FlowTimeline :steps='[{"title":"写 proto","desc":"在 IDL 里定义消息与 service，作为唯一事实来源","tone":"blue"},{"title":"生成代码","desc":"protoc 产出消息类型与客户端服务端桩","tone":"blue"},{"title":"实现服务","desc":"业务实现生成的接口，嵌入 Unimplemented 保持向前兼容","tone":"green"},{"title":"发起调用","desc":"客户端持有同一份桩，按四种流之一收发","tone":"orange"}]' direction="horizontal" />

## 2. 语法与规则

### 2.1 先写契约，再写代码

下面这份 proto 把一元调用和三种流都放进同一个 service。注意 `message` 里每个字段后面的 `= 1`、`= 2`：那是字段号，不是默认值。proto3 的标量字段没有「是否设置」的概念，缺省一律取类型零值（string 是空串、int32 是 0、bool 是 false），因此「没传」和「传了零值」在 wire 上无法区分，需要可表达「缺省」时用 `optional` 或包装类型。

```proto
syntax = "proto3";

package demo.v1;

option go_package = "example.com/demo/pb;pb";

// 一元与三种流的契约都写在同一个 service 里
service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply);                 // Unary 一元
  rpc ListHello (HelloRequest) returns (stream HelloReply);         // 服务端流
  rpc CollectHello (stream HelloRequest) returns (HelloReply);      // 客户端流
  rpc Chat (stream HelloRequest) returns (stream HelloReply);       // 双向流
}

message HelloRequest {
  string name = 1;
  int32  age  = 2;   // 字段号是兼容性的锚点，绝不复用
}

message HelloReply {
  string message = 1;
}
```

生成代码只需要一条命令，`--go_out` 产出消息类型，`--go-grpc_out` 产出客户端桩与服务端接口：

```bash
protoc --go_out=. --go-grpc_out=. --proto_path=. greeter.proto
```

<LineByLine code="message HelloRequest {\n  string name = 1;\n  int32  age  = 2;\n}" :notes='["message 定义一条消息，对应生成 Go 里的一个结构体","name 的字段号是 1；线上只认号码，字段名可改但号不能改","age 的字段号是 2；proto3 标量未传时取类型零值，没有 has 语义","右括号收尾；升级时新字段只能用没用过的号，删掉的号要写进 reserved"]' title="proto 消息的字段号逐行拆解" />

### 2.2 用可编译代码验证兼容性

字段号为什么是锚点，用一段标准库代码就能验证：把 tag 里的数字当作字段号，让「老报文」与「新报文」互相解析。新程序读老报文时，新增字段缺失自动取零值；老程序读新报文时，不认识的字段被忽略而不报错。这正是 Protobuf 双向兼容的直觉来源。

```go
package main

import (
	"encoding/json"
	"fmt"
)

// UserV1 模拟 proto 里的消息：json tag 里的数字类比 protobuf 的字段号。
// protobuf 靠字段号而非字段名定位数据，这是跨版本兼容的根基。
type UserV1 struct {
	ID   uint64 `json:"1"`
	Name string `json:"2"`
}

// UserV2 新增了 Email（字段号 3），老字段号 1、2 一动不动。
type UserV2 struct {
	ID    uint64 `json:"1"`
	Name  string `json:"2"`
	Email string `json:"3"`
}

func main() {
	// 老程序发出的报文，只有字段 1、2
	oldWire := `{"1":1001,"2":"alice"}`

	// 新程序解码：新增字段 3 缺失，自动取零值——这就是默认值语义
	var v2 UserV2
	_ = json.Unmarshal([]byte(oldWire), &v2)
	fmt.Printf("新程序读老报文: ID=%d Name=%s Email=%q（零值）\n", v2.ID, v2.Name, v2.Email)

	// 新程序发出的报文，老程序解码：未知字段 3 被忽略，不报错
	newWire := `{"1":1001,"2":"alice","3":"a@example.com"}`
	var v1 UserV1
	_ = json.Unmarshal([]byte(newWire), &v1)
	fmt.Printf("老程序读新报文: ID=%d Name=%s（未知字段被忽略）\n", v1.ID, v1.Name)
}
```

### 2.3 四种流到底差在哪

四种流的差别不在协议，而在「请求有几个、响应有几个、谁先关流」。用 channel 就能把语义完整复刻出来，且不依赖任何第三方库。

```go
package main

import "fmt"

// 用 channel 模拟 gRPC 四种流：核心差别是请求与响应的个数以及谁先关流。
func main() {
	// 1. Unary：一个请求，一个响应
	fmt.Println("Unary:", unary(3, 4))

	// 2. Server streaming：一个请求，服务端推一串响应后关流
	fmt.Print("server-stream: ")
	for v := range serverStream("go") {
		fmt.Print(v, " ")
	}
	fmt.Println()

	// 3. Client streaming：客户端推一串请求，服务端在流结束时汇总成一个响应
	fmt.Println("client-stream sum:", clientStream([]int{1, 2, 3, 4}))

	// 4. Bidi streaming：两边各推各的，边收边发，互不等待
	fmt.Print("bidi: ")
	bidi()
}

func unary(a, b int) int { return a + b }

func serverStream(prefix string) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch) // 服务端推完即关流，客户端用 range 感知结束
		for _, s := range []string{"hello", "world"} {
			ch <- prefix + "-" + s
		}
	}()
	return ch
}

func clientStream(nums []int) int {
	sum := 0
	for _, n := range nums { // 客户端逐个写入，流结束后才拿到汇总响应
		sum += n
	}
	return sum
}

func bidi() {
	requests := make(chan int)
	responses := make(chan int)
	go func() { // 服务端：来一个算一个，边收边回
		defer close(responses)
		for n := range requests {
			responses <- n * n
		}
	}()
	go func() { // 客户端：边发边收，不必等对方全部结束
		for _, n := range []int{1, 2, 3} {
			requests <- n
		}
		close(requests)
	}()
	for r := range responses {
		fmt.Print(r, " ")
	}
	fmt.Println()
}
```

<OutputBox title="运行结果" output="Unary: 7\nserver-stream: go-hello go-world \nclient-stream sum: 10\nbidi: 1 4 9 " note="close 是流的结束信号：谁关流、何时关，决定了这次调用属于哪一种流。" />

## 3. 深入一步

### 3.1 生成代码长什么样

protoc 生成的产物里，客户端拿到的是一个「看起来像本地方法」的桩，服务端拿到的是一个必须实现的接口。更重要的是生成器会额外产出 `UnimplementedXxxServer`：业务实现嵌入它，将来 proto 新增方法时老实现仍能通过编译，这是 gRPC 官方推荐的向前兼容姿势。下面用标准库复刻这套结构。

```go
package main

import (
	"context"
	"fmt"
)

// 模拟 protoc 生成的桩接口：契约由 .proto 唯一确定，客户端与服务端共享同一份接口。
type GreeterServer interface {
	SayHello(ctx context.Context, name string) (string, error)
}

// 生成器还会产出 Unimplemented，业务嵌入它即可在 proto 加方法时不被编译打断。
type UnimplementedGreeterServer struct{}

func (UnimplementedGreeterServer) SayHello(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("method SayHello not implemented")
}

type service struct{ UnimplementedGreeterServer }

func (service) SayHello(_ context.Context, name string) (string, error) {
	return "hello, " + name, nil
}

// dispatcher 模拟 grpc.RegisterGreeterServer：把实现挂到按方法名分发的位置。
type dispatcher struct{ srv GreeterServer }

func (d *dispatcher) handle(ctx context.Context, method, name string) (string, error) {
	if method != "SayHello" {
		return "", fmt.Errorf("unknown method")
	}
	return d.srv.SayHello(ctx, name)
}

func main() {
	d := &dispatcher{srv: service{}}
	out, err := d.handle(context.Background(), "SayHello", "gopher")
	fmt.Printf("生成桩调用结果: out=%s err=%v\n", out, err)
}
```

| 流类型 | 请求数 | 响应数 | 谁先关流 | 典型场景 |
| --- | --- | --- | --- | --- |
| Unary 一元 | 1 | 1 | 服务端回完即结束 | 查询、下单、鉴权 |
| 服务端流 | 1 | 多 | 服务端推完关流 | 列表分页、日志订阅、进度推送 |
| 客户端流 | 多 | 1 | 客户端写完关流 | 批量上传、指标聚合、文件分块 |
| 双向流 | 多 | 多 | 双方各自关闭 | 聊天、实时协同、游戏帧同步 |

<Term word="IDL" text="接口描述语言，用中立的语法描述消息与服务，是契约优先开发的事实来源" />
<Term word="字段号" text="protobuf 线上定位字段的唯一编号，字段名可改、字段号终身不可变" />
<Term word="proto3 默认值" text="标量字段未传时取类型零值，且无法区分「没传」与「传了零值」" />

<Callout type="warn" title="字段号是终身契约" text="永远不要复用或改变已用过的字段号，删除字段时把号码写进 reserved，新字段只用全新号码。一旦复用，老数据会被新程序按新类型解读，静默出错且极难排查。" />

## 4. 工程实践

契约的生命周期远比一次上线长，版本演进必须按规则来。可安全做的：新增字段（用新号码）、新增方法、新增 service、给枚举追加值（留意零值必须是第一个）。有风险的：改字段类型（int32 到 string 一定出事）、把 `string` 与 `bytes` 互换、把单值字段改成 `repeated`。必须禁止的：复用字段号、删除字段后不 reserved、随意改包名导致 `go_package` 漂移。

<Compare :panels='[{"label":"危险的改法","code":"int32  age  = 1;  // 复用了已删除字段的号\nstring name = 2;","note":"老报文里字段 1 是 name，新程序会把它错读成 age，静默数据错乱"},{"label":"安全的改法","code":"string name = 1;\nreserved 2;\nstring email = 3;","note":"老字段号不动，删掉的号写进 reserved，新字段只用全新号码"}]' title="字段演进的两种做法" />

工程上还有几条经验。`go_package` 一旦定下就不要改，否则所有下游 import 路径跟着变；把 `.proto` 纳入代码评审，像对待数据库 schema 一样对待它；用 buf 或 protoc 的 lint 在 CI 里强制检查「字段号是否复用、命名是否规范」；生成物是否入库要团队统一，避免有人本地 protoc 版本不同导致 diff 噪音。流的选型也别贪多：绝大多数内部接口用 Unary 就够，只有确实需要「持续推送」或「批量上行」时才上流，因为流式调用的错误处理、重试与超时都要比一元复杂得多。

## 动手练一练
<Exercise title="给 Greeter 增加一个服务端流方法并保持兼容" task="在 proto 里新增 rpc StreamHello(HelloRequest) returns (stream HelloReply)，说明要改哪几处；再用本讲 channel 版 serverStream 的思路，写出它对客户端 range 的结束语义。" :hints='["新增 rpc 不改变任何已有字段号与已有方法签名","生成的接口会多一个以 Server 结尾的流式入参，业务嵌入 Unimplemented 即可兼容","服务端用 Send 连续写，写完由框架关闭流，客户端 range 或 Recv 读直到 io.EOF"]' solution="要点一：只在 service 里追加一行 rpc，HelloRequest 与 HelloReply 的字段号保持原样。\n要点二：重新 protoc 生成，客户端得到返回 stream 的方法，服务端接口多一个 StreamHello(req, stream) 方法；业务实现嵌入 UnimplementedGreeterServer 就不会被新增方法打断。\n要点三：服务端循环中多次 Send，返回 nil 后框架关流；客户端 for 循环 Recv 直到 io.EOF 结束，等价于 channel 被 close。" explain="新增方法与新增字段是 proto 里最安全的两类变更，前提是绝不碰已有字段号，并用 Unimplemented 兜住生成接口的演进。" />

## 随堂检测
<Quiz no="73-1" q="proto3 中把某字段的字段号从 2 改成 3，但保持字段名不变，会发生什么？" :options='["完全兼容，字段名没变就行","线上只认字段号，老数据里的该字段会被当成未知字段丢弃","protoc 会自动做数据迁移","只影响 gRPC，不影响 protobuf 编码"]' answer="1" explain="wire 上定位字段靠号码不靠名字。改号等于把老字段判了死刑：老报文里号码 2 的数据新程序不认识，号码 3 又读不到值，于是静默丢失。" />

<Predict no="73-2" q="代码 2.3 里 serverStream 推了 go-hello 与 go-world 两条消息后 defer close(ch)。客户端用 for range 读这个 channel，会打印几条？" :options='["只打印第 1 条，range 遇到 close 立即退出","打印 2 条后随 channel 关闭自然结束","一直阻塞，因为没人关闭 channel","打印 3 条，多出一条零值"]' answer="1" output="server-stream: go-hello go-world " explain="服务端推完 defer close 关流，range 会把在途消息全部读完，再随 channel 关闭自然退出；这正是服务端流「一个请求、一串响应、服务端关流」的结束语义。" />

## 一页纸速记
<KnowledgeCard :points='["契约优先：一份 proto 是唯一事实来源，protoc 生成消息、客户端桩与服务端接口","字段号终身不变，线上只认号码；proto3 标量缺省取零值且无法区分未传与零值","四种流按请求数、响应数、谁先关流区分：Unary 一对一，服务端流一对多，客户端流多对一，双向流多对多","业务实现嵌入 UnimplementedXxxServer，proto 新增方法时不被编译打断"]' :pitfalls='["复用或改动已用过的字段号，导致老数据静默错乱","删除字段不写 reserved，日后被误复用","为了炫技滥用双向流，让超时、重试与错误处理复杂度翻倍"]' summary="一句话记住：proto 是契约，字段号是钉子，四种流只是同一通道上不同的收发节奏。" />

## 小结与下一讲

- **契约优先**：用 proto 把消息与服务写成唯一事实来源，protoc 生成双方共享的类型与桩，编译期挡住低级错误。
- **兼容性根基**：字段号终身不变，新增字段用新号，删除字段写 reserved，proto3 默认值语义要心里有数。
- **四种流**：一元、服务端流、客户端流、双向流按收发个数与关流方区分，绝大多数内部接口用一元即可。

[下一讲：gRPC 拦截器、metadata、错误与超时](/microservice/grpc-advanced)
