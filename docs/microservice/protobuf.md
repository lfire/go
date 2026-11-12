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

### 3.2 字段号、wire type 与 varint：把一条消息拆到字节级

proto 在线上不传字段名，只传一个由「字段号 + wire type」合成的 **tag**，后面再跟按类型编码的值。理解 `protoc --decode_raw` 为什么能脱离 `.proto` 反解报文，关键就在这三个概念。

`tag = (field_number << 3) | wire_type`，tag 本身也是一个 varint。低 3 位是 wire type，决定「值该怎么读」；其余高位是字段号，决定「这个值属于哪个字段」。wire type 一共用到四种：

| wire type | 值 | 读法 | 承载的字段类型 |
| --- | --- | --- | --- |
| Varint | 0 | 变长整数，读到最高位为 0 为止 | int32、int64、uint、bool、enum |
| 64-bit | 1 | 固定 8 字节 | fixed64、sfixed64、double |
| Length-delimited | 2 | 先读一个 varint 长度，再读该长度的字节 | string、bytes、嵌套 message、repeated |
| 32-bit | 5 | 固定 4 字节 | fixed32、sfixed32、float |

字段号范围是 1 到 2 的 29 次方减 1，其中 19000 到 19999 被 protobuf 实现保留，不能分配给用户字段。**varint** 的规则是：每个字节的低 7 位存数据，最高位（MSB）是 continuation 标志，1 表示后面还有字节、0 表示结束，并且低位字节排在前（小端序）。于是小整数只占 1 字节，大整数按需扩展，这就是「字段号尽量用 1 到 15」能省一个字节的原因——tag 小于 128 时只需 1 字节。

用官方那个经典例子推一遍：字段号 1、wire type 0（varint）、值 150。
- tag = (1 << 3) | 0 = 8，编码为 `0x08`。
- 150 的二进制是 `10010110`。取低 7 位 `0010110`（0x16），因还有高位，把 MSB 置 1 得 `0x96`；剩余的高位 `1` 单独占一个字节 `0x01`。
- 合起来就是 `08 96 01`——这正是 protobuf 官方文档里那条消息的编码。

下面这段不依赖任何第三方库的代码手写了 varint 与 tag，可以直接验证上面的推导：

```go
package main

import (
	"encoding/hex"
	"fmt"
)

// putVarint 按 protobuf 规则编码：每字节低 7 位存数据，MSB 表示是否还有后续字节。
func putVarint(x uint64) []byte {
	var out []byte
	for x >= 0x80 {
		out = append(out, byte(x)|0x80)
		x >>= 7
	}
	return append(out, byte(x))
}

// tag 把字段号与 wire type 合成一个 varint。
func tag(field int, wire int) []byte {
	return putVarint(uint64(field)<<3 | uint64(wire))
}

func main() {
	// 字段号 1、wire type 0、值 150，官方示例编码应为 08 96 01
	msg := append(tag(1, 0), putVarint(150)...)
	fmt.Println("wire hex:", hex.EncodeToString(msg))

	// 字段号 2、wire type 2、字符串 go：tag 0x12，长度 0x02，内容 67 6f
	str := append(tag(2, 2), putVarint(2)...)
	str = append(str, []byte("go")...)
	fmt.Println("string hex:", hex.EncodeToString(str))

	// 值 300 需要两个字节，验证多字节 varint
	fmt.Println("300 varint:", hex.EncodeToString(putVarint(300)))
}
```

<OutputBox title="运行结果" output="wire hex: 089601\nstring hex: 1202676f\n300 varint: ac02" note="三段十六进制把 tag、长度前缀与变长整数三类编码一次展示清楚。" />

逐字段解释：`089601` = `08`(tag，字段号 1 加 wire type 0) + `96 01`(150 的 varint)；`1202676f` = `12`(字段号 2 加 wire type 2) + `02`(后续内容长度 2) + `676f`(字符串 go 的 ASCII)；`ac02` 是 300 的 varint，低 7 位 `0101100` 加 MSB 得 `ac`、高位 `2` 得 `02`。由此也能看出「字段号终身不变」的物质基础：改字段号等于换 tag 的第一个字节，老报文里的数据会被新程序当作另一个字段甚至未知字段丢弃。

### 3.3 HTTP/2 多路复用与四种流的帧

gRPC 选择 HTTP/2 不是偶然，而是四种流都依赖它的三个能力。**二进制分帧**：一次调用被拆成 HEADERS 帧与若干 DATA 帧，每个帧都带 stream id，多个 stream 能在同一条 TCP 连接上并发交错、互不阻塞——这就解决了 HTTP/1.1 的队头阻塞，一个慢响应不再挡住后面所有请求。**HPACK 头部压缩**：metadata（grpc-timeout、trace 头、自定义键值）作为 HTTP/2 头部传输，重复头不再每次全量发。**流控 flow control**：接收方用 WINDOW_UPDATE 帧通告自己还能收多少字节，发送方不得超发，这直接决定了流式调用的背压行为。

gRPC 放在 DATA 帧里的是 **Length-Prefixed-Message**：1 字节压缩标志 + 4 字节大端长度 + N 字节 protobuf 消息。理解了这一层，就能看清「四种流」其实是同一条 HTTP/2 连接上的多个 stream，差别只在一个 stream 里消息帧的收发方向与个数，以及谁先发 END_STREAM 半关闭：

| 流类型 | 请求消息帧 | 响应消息帧 | 谁先半关闭 | 流控风险点 |
| --- | --- | --- | --- | --- |
| Unary 一元 | 1 | 1 | 客户端发完即半关闭 | 几乎无 |
| 服务端流 | 1 | 多 | 客户端半关闭，服务端推完关流 | 客户端不 Recv 会耗尽接收窗口 |
| 客户端流 | 多 | 1 | 客户端写完半关闭 | 服务端不 Recv 会耗尽接收窗口 |
| 双向流 | 多 | 多 | 双方各自关闭 | 两个方向窗口独立，任一方向堵住都卡住 |

流控为什么必须有？假设客户端以 1 Gbps 流式上传、服务端只有 100 Mbps 的处理能力，没有流控时数据会一路堆进服务端内存直到 OOM。HTTP/2 的窗口机制让接收方掌握节奏：窗口归零后发送方必须停发，直到收到 WINDOW_UPDATE。这解释了一类真实故障——「双向流突然卡住却不报错」，往往是某一方应用迟迟不 Recv，接收窗口没有释放，发送方被流控静默挡住。

<SequenceDiagram title="一条 HTTP2 连接上的一次 Unary 调用" caption="HEADERS 与 DATA 帧靠 stream id 交错复用，WINDOW_UPDATE 负责释放接收窗口" :actors='["Client","Stream","Connection","Server"]' :steps='[{"from":"Client","to":"Stream","label":"发起 SayHello","note":"进入客户端桩","kind":"call"},{"from":"Stream","to":"Connection","label":"HEADERS 帧","note":"携带 metadata","kind":"call"},{"from":"Stream","to":"Connection","label":"DATA 帧消息","note":"长度前缀消息","kind":"call"},{"from":"Connection","to":"Server","label":"交错投递","note":"同一 TCP 连接","kind":"call"},{"from":"Server","to":"Connection","label":"DATA 帧响应","note":"服务端结果","kind":"call"},{"from":"Connection","to":"Stream","label":"按 stream 归并","note":"靠 stream id","kind":"call"},{"from":"Stream","to":"Stream","label":"累计接收窗口","note":"流控计数","kind":"self"},{"from":"Stream","to":"Connection","label":"WINDOW_UPDATE","note":"释放窗口额度","kind":"call"},{"from":"Server","to":"Stream","label":"END_STREAM 尾帧","note":"带 grpc-status","kind":"return"},{"from":"Stream","to":"Client","label":"返回结果","note":"解码消息","kind":"return"}]' />

### 3.4 替代方案取舍：序列化与 RPC 通道各有几种走法

契约优先解决的是「双方对消息长什么样达成一致」，但达成一致的手段不止一种。下面两组对照把「为什么是 Protobuf + gRPC」以及各自的失败边界摆出来——任何一行选错，都会在某个具体场景下付出代价。

| 序列化方案 | 编码 | schema 来源 | 跨语言 | 典型失败边界 |
| --- | --- | --- | --- | --- |
| JSON | 文本 | 无，或额外 OpenAPI | 是 | 数字精度、无字段号兼容、体积大 |
| Protobuf | 二进制 + varint | .proto 契约 | 是 | 字段号复用导致静默错读 |
| FlatBuffers | 二进制，零拷贝 | .fbs 契约 | 是 | 访问前不解析，schema 演进更僵硬 |
| MessagePack | 二进制 | 无强 schema | 是 | 缺字段号，兼容性靠约定 |

| RPC 通道 | 传输 | 流式 | 典型失败边界 |
| --- | --- | --- | --- |
| REST over HTTP/1.1 | 文本请求响应 | 弱 | 队头阻塞、无内建 deadline 传播 |
| gRPC over HTTP/2 | 二进制帧 | 四种流 | 需 HTTP/2 基础设施，代理要支持 |
| Thrift | 自带 TProtocol，多传输层 | 有限 | 生态与工具链不如 gRPC 统一 |

### 3.5 可验证实验：protoc、decode_raw 与 grpcurl

三条命令把「契约 → 字节 → 调用」整条链走一遍：

```bash
# 1) 生成 Go 代码并确认能编译
protoc --go_out=. --go-grpc_out=. --proto_path=. greeter.proto
go build ./...

# 2) 脱离 .proto，纯按 wire 格式反解一段字节
printf '\x08\x96\x01' | protoc --decode_raw

# 3) 服务端开启反射后，用 grpcurl 发起一元调用
grpcurl -plaintext -d '{"name":"go","age":150}' localhost:50051 demo.v1.Greeter/SayHello
```

`protoc --decode_raw` 的输出：

```text
1: 150
```

逐字段解释：开头的 `1` 是从 `0x08` 解出的字段号（0x08 右移 3 位得 1，低 3 位得 wire type 0），`150` 是按 varint 规则把 `96 01` 还原出来的值。`--decode_raw` 不需要 `.proto` 就能工作，正是「线上只认字段号不认字段名」的直接证据，也解释了为什么改字段号等于把老数据判了死刑。

`grpcurl` 的典型输出：

```text
{
  "message": "hello, go"
}
```

逐字段解释：`message` 对应 `HelloReply` 里字段号 1 的字段，grpcurl 借服务端反射拿到 `.proto` 才能把二进制响应格式化成 JSON；未开启反射时会报 `server does not support the reflection API`，这不是服务不可用，而是缺少查询契约的通道。命令里的 `-plaintext` 表示不走 TLS，`-d` 后面是 JSON 形式的请求体，字段名用的是 proto 里的字段名而非字段号，转换由 grpcurl 依据反射结果完成。

## 4. 工程实践

契约的生命周期远比一次上线长，版本演进必须按规则来。可安全做的：新增字段（用新号码）、新增方法、新增 service、给枚举追加值（留意零值必须是第一个）。有风险的：改字段类型（int32 到 string 一定出事）、把 `string` 与 `bytes` 互换、把单值字段改成 `repeated`。必须禁止的：复用字段号、删除字段后不 reserved、随意改包名导致 `go_package` 漂移。

<Compare :panels='[{"label":"危险的改法","code":"int32  age  = 1;  // 复用了已删除字段的号\nstring name = 2;","note":"老报文里字段 1 是 name，新程序会把它错读成 age，静默数据错乱"},{"label":"安全的改法","code":"string name = 1;\nreserved 2;\nstring email = 3;","note":"老字段号不动，删掉的号写进 reserved，新字段只用全新号码"}]' title="字段演进的两种做法" />

工程上还有几条经验。`go_package` 一旦定下就不要改，否则所有下游 import 路径跟着变；把 `.proto` 纳入代码评审，像对待数据库 schema 一样对待它；用 buf 或 protoc 的 lint 在 CI 里强制检查「字段号是否复用、命名是否规范」；生成物是否入库要团队统一，避免有人本地 protoc 版本不同导致 diff 噪音。流的选型也别贪多：绝大多数内部接口用 Unary 就够，只有确实需要「持续推送」或「批量上行」时才上流，因为流式调用的错误处理、重试与超时都要比一元复杂得多。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l7-microservice/lesson-073-protobuf`](https://github.com/lfire/go/tree/main/examples/l7-microservice/lesson-073-protobuf)。clone 仓库后进入 `examples/l7-microservice`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

### 4.2 契约与流的失败矩阵

兼容性和流控的坑几乎都「不报错、只算错」，所以更值得提前列清楚：

| 故障 | 触发点 | 现象 | 防线 |
| --- | --- | --- | --- |
| 字段类型静默错读 | int32 改 string，wire type 从 0 变 2 | 老报文被按新类型解读，值全错却不报错 | 只加新号，改类型视为新字段 |
| 复用已删字段号 | 老数据仍在消息或存储里 | 老字段被新程序错读 | 删号写 reserved，永不复用 |
| proto3 零值歧义 | 0 与未传在 wire 上都不出现 | 业务把「没传」当成「传了 0」 | optional 或包装类型 |
| 双向流卡住不报错 | 一方迟迟不 Recv | 接收窗口耗尽，发送方被流控静默挡住 | 及时 Recv 加两侧各自超时 |
| 生成物版本漂移 | 团队 protoc 与插件版本不一 | 同一 proto 生成不同代码，行为漂移 | 锁定版本，buf lint 进 CI |
| 流式调用滥用 | 普通查询也上双向流 | 超时、重试、错误处理复杂度翻倍 | 绝大多数内部接口用 Unary |

### 4.3 两条真实工程误解

**误解一：改字段名不影响兼容，字段号也能顺便调一调。** 字段名确实随便改（只影响生成代码里的 Go 标识符），但字段号是线上定位数据的唯一编号，动一下就等于换掉了 tag 的第一个字节。把 `string name = 1` 改成 `int32 age = 1` 这种复用号的写法，老报文里字段 1 的 name 字节会被新程序按 varint 读成一个莫名其妙的 age，静默出错且极难排查。

<Compare :panels='[{"label":"危险：复用字段号","code":"int32 age = 1;\nstring name = 2;","note":"老报文字段 1 是 name，新程序按 int32 读成 age，静默错乱"},{"label":"安全：只加新号","code":"string name = 1;\nreserved 2;\nstring email = 3;","note":"老号不动，删掉的号写进 reserved，新字段用全新号"}]' title="字段演进的两种做法" />

**误解二：四种流是四种不同协议，需要各自建连、各自配超时。** 从 wire 看，四种流都是同一条 HTTP/2 连接上带 stream id 的帧序列，共享连接与流控窗口；差别只在一个 stream 里消息帧的数量、方向以及谁先发 END_STREAM 半关闭。以为双向流要单独建连，会平白多写一套连接管理，还容易把两侧流控窗口当成彼此独立而漏配超时。

<Callout type="idea" title="四种流共享同一条连接" text="一元、服务端流、客户端流、双向流只是同一 HTTP2 连接上不同的收发节奏，共享连接、头部压缩与流控窗口；流式调用真正增加的是逐消息的 Recv 与 Send 处理，而不是连接数量。" />

### 4.4 与后续阶段的呼应

「字段号是终身锚点」和「扩容只迁移少量 key」是同一种稳定锚点思想，[/distributed/sharding](/distributed/sharding) 的一致性哈希把它用到了数据分片上；[/distributed/distributed-id](/distributed/distributed-id) 里趋势递增 ID 追求的紧凑编码，也和本讲的 varint 思路一脉相承。工具链方面，[/projects/cli-codegen](/projects/cli-codegen) 会把本讲的 protoc 生成流程做成 protoc 插件与代码生成器；而 [/projects/micro-ecommerce](/projects/micro-ecommerce) 的订单契约一旦要演进，就必须遵守本讲的字段号红线，否则新老服务混部时会静默写错数据。

<Quiz q="一条 protobuf 报文的首字节是 0x12（十进制 18），它表示什么？" :options='["字段号 1、wire type 2","字段号 2、wire type 2（长度前缀）","字段号 18、wire type 0","字段号 2、wire type 0"]' answer="1" explain="tag 等于字段号左移 3 位再或上 wire type。18 等于 2 左移 3 位或上 2，所以字段号是 2、wire type 是 2，即接下来先读一个 varint 长度再读该长度的字节。" />

<Predict q="用本讲 3.2 的 putVarint 编码无符号整数 300，得到的十六进制是？" :options='["2c 01","ac 02","01 2c","96 01"]' answer="1" output="300 varint: ac02" explain="300 的二进制是 100101100，低 7 位 0101100 把最高位置 1 得 0xac，剩余高位 2 得 0x02；varint 低位字节在前，故为 ac02。" />

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
