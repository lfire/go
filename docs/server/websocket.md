---
title: 第 64 讲 · WebSocket / SSE 与实时推送
description: 讲清协议握手与升级、gorilla/websocket 读写泵与心跳、断线重连、SSE 取舍以及广播与连接管理，实现可用的长连接服务
---

<StageHeader stage="L6 · 服务端与数据" level="★★★★☆" time="45 分钟" goal="实现长连接场景与连接治理" prereq="第 63 讲 · REST API" />

# 第 64 讲 · WebSocket / SSE 与实时推送

> 前面所有接口都是「客户端问一句、服务端答一句」：连接用完即走，服务端永远不知道客户端还在不在。可聊天室、行情推送、任务进度、日志流这些场景，需要服务端**主动、持续地**把消息送出去。这一讲把短请求升级成长连接：先看 HTTP 如何握手升级成 WebSocket，再解决长连接真正的难点——心跳、僵尸连接、并发写与扇出广播，最后给出 SSE 这个更轻的替代方案。

## 这一讲你会得到什么

<CheckList id="64-goal" title="本讲目标" :items='["理解 Upgrade 握手做了什么，能写出安全的升级校验","会用 gorilla/websocket 搭读写泵、心跳与读写超时","理解 Hub 模式下连接注册、广播与慢客户端治理","能按业务在 WebSocket、SSE、长轮询之间做取舍"]' />

## 1. 先建立直觉

短请求像打电话问一句就挂；WebSocket 像把电话转成对讲机：先拨一次普通 HTTP 号码，说「我们换成对讲模式吧」（`Upgrade: websocket`），对方回 101 Switching Protocols，此后这条 TCP 连接就不再遵守请求-应答规则，双方随时都能说话。关键点在于：**握手仍然是一次标准 HTTP 请求**，所以鉴权、Cookie、反向代理都在这时生效，一旦升级成功，后面就是独立的帧协议了。

长连接和短请求最难的不是「连上」，而是「怎么知道对方还活着」。TCP 不会告诉你对端已经拔了网线，中间代理也可能静默掐掉空闲连接。于是必须自己发心跳（Ping/Pong），并用读超时兜底：一段时间没收到任何帧，就认定连接已死、主动回收。

<ConceptDiagram type="mq-flow" title="广播本质是一次扇出：一条消息复制给所有在线连接" caption="Producer 对应消息源，Broker 对应 Hub，Consumer 对应每个连接的写泵；慢消费者必须被隔离，否则会拖垮整场广播" />

## 2. 语法与规则

第一段代码给出完整、可运行的 WebSocket 服务：升级校验、读限制、心跳、读写超时，以及最重要的「单写 goroutine」约束。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 生产必须白名单校验 Origin，否则任意站点都能连上（CSWSH）。
		return r.Header.Get("Origin") == "http://localhost:5173"
	},
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10 // 必须小于 pongWait
	maxMsgSize = 1 << 20             // 单条消息上限 1 MiB
)

func serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(maxMsgSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		// 每收到一个 Pong 就把读超时往后推，形成心跳闭环。
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// 所有写操作收敛到写泵，杜绝并发写 conn 引发的 panic。
	out := make(chan []byte, 16)
	go writePump(conn, out)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("read: %v", err)
			}
			break
		}
		select {
		case out <- msg: // 回显交给写泵，读循环绝不直接写
		default:
			log.Println("client too slow, drop message")
		}
	}
	close(out)
}

func writePump(conn *websocket.Conn, out chan []byte) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-out:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", serveWS)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "use a WebSocket client to connect /ws")
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

<OutputBox title="握手阶段抓包要点" output="请求头 Upgrade: websocket\n请求头 Connection: Upgrade\n响应行 HTTP/1.1 101 Switching Protocols\n此后不再有新的 HTTP 请求，双方改传 WebSocket 帧" note="鉴权必须在 Upgrade 之前完成：升级成功后拿不到新的 HTTP 头，也没机会再 Set-Cookie" />

<LineByLine code="conn, err := upgrader.Upgrade(w, r, nil)\nif err != nil {\nreturn\n}\ndefer conn.Close()\nconn.SetReadLimit(maxSize)\nconn.SetReadDeadline(deadline)" :notes='["把普通 HTTP 连接升级成 WebSocket，失败时应直接返回","升级失败通常由 Origin 校验或非 WebSocket 请求引起","不要吞掉错误，日志里留一条便于排查","无论从哪条路径退出，都要关闭底层连接","限制单帧大小，防止恶意超大帧吃满内存","设置读超时，僵尸连接到期会被自动断开"]' title="握手与防护逐行看" />

这里有一条铁律：**一个连接只能有一个 goroutine 写**。gorilla/websocket 的文档明确说明并发调用 `WriteMessage` 是不安全的，轻则帧错乱，重则直接 panic。所以读循环收到消息只往 `out` 通道里投，由唯一的写泵统一落笔。这也解释了为什么广播架构里每个客户端都要带一个缓冲 `send` 通道。

第二段代码把单连接扩成聊天室：Hub 用通道串行化所有连接状态变更，广播时对慢客户端做非阻塞发送。

```go
package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type client struct {
	conn *websocket.Conn
	send chan []byte
}

type hub struct {
	clients    map[*client]struct{}
	broadcast  chan []byte
	register   chan *client
	unregister chan *client
}

func newHub() *hub {
	return &hub{
		clients:    make(map[*client]struct{}),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *client),
		unregister: make(chan *client),
	}
}

// run 是唯一修改 clients 的地方，状态变更被串行化，因此无需加锁。
func (h *hub) run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = struct{}{}
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
		case msg := <-h.broadcast:
			for c := range h.clients {
				select {
				case c.send <- msg:
				default: // 慢客户端：直接踢出，绝不让一条连接阻塞整场广播
					delete(h.clients, c)
					close(c.send)
				}
			}
		}
	}
}

func (c *client) writePump() {
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // 仅演示
}

func (h *hub) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 16)}
	h.register <- c
	go c.writePump()

	defer func() {
		h.unregister <- c
		_ = conn.Close()
	}()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		h.broadcast <- msg
	}
}

func main() {
	h := newHub()
	go h.run()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", h.serveWS)
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

`default` 分支是整套广播的保险丝：如果某个客户端的 `send` 队列满了，说明它的写泵跟不上（网络慢或对端不读了），此时立刻把它摘除，而不是让 `broadcast` 卡在这一条连接上。一个慢消费者拖垮所有在线用户，是长连接服务最典型的事故。

## 3. 深入一步

心跳参数之间有严格的大小关系：`pingPeriod` 必须小于 `pongWait`，`pongWait` 又决定了读超时；写超时 `writeWait` 单独限制每次落笔。若把读超时设得比心跳周期还短，正常连接会被误杀。

**断线重连**必须由客户端实现，且要带指数退避与随机抖动：断线后立刻疯狂重连，会在服务端恢复瞬间造成连接风暴。浏览器原生 `WebSocket` 不会自动重连，而 `EventSource` 会——这是 SSE 的一大隐性优势。重连后还要处理「断线期间漏掉的消息」：给每条消息编号，客户端带上 `Last-Event-ID` 或自增序号，服务端从断点补发。

| 维度 | WebSocket | SSE | 长轮询 |
| --- | --- | --- | --- |
| 方向 | 全双工 | 服务端到客户端单向 | 半双工，靠反复请求模拟 |
| 底层协议 | 独立帧协议（需 Upgrade） | 普通 HTTP 流 | 普通 HTTP 请求 |
| 自动重连 | 无，需自己写 | 浏览器原生支持 | 天然就是下一次请求 |
| 代理与防火墙 | 可能被拦截 | 兼容性最好 | 兼容性最好 |
| 数据格式 | 文本 / 二进制帧 | 仅 UTF-8 文本 | 任意 |
| 典型场景 | 聊天、协同、游戏 | 通知、行情、进度、日志 | 兜底方案 |

<Term word="101 Switching Protocols" text="HTTP 升级响应的状态码；服务端同意切换协议后，这条 TCP 连接不再按 HTTP 请求-应答工作，后续由新协议（这里是 WebSocket 帧）接管" />

<Callout type="warn" title="三个高频反模式" text="其一，多个 goroutine 直接对同一个 conn 调 WriteMessage，会帧错乱甚至 panic；其二，不设 SetReadDeadline 与 SetReadLimit，僵尸连接和大帧会慢慢耗尽内存与 fd；其三，CheckOrigin 无条件返回 true，等于允许任意站点带着用户 Cookie 发起跨站 WebSocket 劫持。" />

<Callout type="idea" title="SSE 的取舍" text="如果业务只需要服务端单向推送，优先选 SSE：它是普通 HTTP，天然穿过各类代理与网关，浏览器 EventSource 自带重连与 Last-Event-ID，服务端实现只要设置 text/event-stream 并记得 Flush。只有确实需要客户端高频回传、或要传二进制时，才值得付出 WebSocket 的连接治理成本。" />

第三段代码是零依赖、完整可运行的 SSE 服务，包含事件编号与心跳注释行。

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func sseHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// 心跳注释行：SSE 里以冒号开头的行是注释，用来防止代理掐空闲连接。
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for i := 0; ; i++ {
		select {
		case <-r.Context().Done(): // 客户端关闭 EventSource 时立刻退出
			return
		case <-ticker.C:
			fmt.Fprintf(w, "id: %d\nevent: tick\ndata: %s\n\n",
				i, time.Now().Format(time.RFC3339))
			flusher.Flush() // 不 Flush 数据会卡在缓冲区里
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", sseHandler)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "open /events with EventSource")
	})
	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

<OutputBox title="SSE 帧格式" output="id: 0\nevent: tick\ndata: 2026-11-11T10:00:00Z\n\n: ping" note="空行表示一个事件结束；id 供重连时通过 Last-Event-ID 续传，冒号开头的行是心跳注释" />

### 协议与契约：握手、帧与半开连接的推导

**握手为什么必须是一次标准 HTTP 请求。** RFC 6455 规定客户端发起带 `Upgrade: websocket`、`Connection: Upgrade`、`Sec-WebSocket-Key`（16 字节随机值的 base64）、`Sec-WebSocket-Version: 13` 的请求；服务端同意则回 `101 Switching Protocols`，并带 `Sec-WebSocket-Accept`。这个头不是回声，而是 `base64(SHA1(key + GUID))`，其中 GUID 是规范写死的 `258EAFA5-E914-47DA-95CA-C5AB0DC85B11`。推导：拼接固定 GUID 是为了让普通 HTTP 服务器或缓存代理不会「碰巧」返回一段被误认成升级响应的内容，Accept 值则让客户端确认对端真的理解 WebSocket 而非普通 200。101 之后这条 TCP 连接不再遵守请求-应答规则，双方改传帧——所以鉴权、Cookie、`Set-Cookie`、限流、灰度全都只能在握手阶段完成，升级成功后既没有新的 HTTP 头，也没机会再补一次校验。

**帧格式解释了「单写」铁律。** RFC 6455 第 5.2 节的一个帧由 `FIN(1) + RSV(3) + opcode(4) + MASK(1) + 载荷长度(7/16/64 位) + 掩码键(0 或 4 字节) + 载荷` 组成。一条应用消息可以拆成多帧、靠 FIN 位标识结束，opcode 区分 text(0x1)、binary(0x2)、close(0x8)、ping(0x9)、pong(0xA)，控制帧必须 FIN=1 且载荷不超过 125 字节。由此推出并发写为什么不是「加锁就好」：若两个 goroutine 各自写帧，A 写了未置 FIN 的续帧，B 又插入一个 text 帧，帧序列交错后消息边界与 opcode 语义都被破坏，对端解析出错乱报文，gorilla/websocket 文档因此明确不支持并发写。规范还要求**客户端到服务端**的帧必须掩码（MASK=1），目的是防止历史上一类缓存污染攻击（RFC 6455 第 10.3 节），服务端到客户端不掩码——这层由库透明处理，但知道它存在才明白升级后不能再往连接里裸写字节。

**心跳参数之间有严格的不等式。** `SetReadDeadline(now + pongWait)` 定义「多久没收到任何帧就判定连接已死」，服务端每 `pingPeriod` 发一次 Ping，对端回 Pong 时 `SetPongHandler` 把读超时往后推。推导：必须 `pingPeriod < pongWait`，否则心跳还没来得及刷新超时，正常连接已被误杀；再留一点余量，工程上取 `pongWait * 9 / 10`。`writeWait` 单独限制每次落笔，防止对端不读导致写阻塞。为什么不交给 TCP keepalive？因为系统默认探测间隔常以小时计，而 NAT、负载均衡器对空闲连接的静默清除往往是 60 到 350 秒；只有应用层心跳才能同时对抗「半开连接」与中间设备的空闲超时。这是第 61 讲四类超时在长连接上的变体，也说明 WebSocket 必须从短请求的 `WriteTimeout` 体系里豁免出来。

**SSE 帧是纯文本协议，不是库的魔法。** `text/event-stream` 里每个事件由 `id:`、`event:`、`data:`、`retry:` 等行组成，空行表示事件结束，冒号开头的行是注释（正好用来做心跳、防止代理掐空闲连接）；浏览器 `EventSource` 断线后自动重连并带上 `Last-Event-ID` 请求头，服务端据此从断点补发。它是普通 HTTP 流，天然穿过各类代理与网关，却只支持服务端到客户端的单向 UTF-8 文本。理解帧格式才能解释两个高频坑：不调用 `Flusher.Flush()` 数据会卡在缓冲区里根本没发出去；注释行不加，长连接会被代理按空闲超时掐掉。

### 设计取舍与替代方案

| 维度 | 方案 A：WebSocket | 方案 B：SSE | 方案 C：长轮询 |
| --- | --- | --- | --- |
| 方向 | 全双工 | 服务端到客户端单向 | 半双工，靠反复请求模拟 |
| 底层 | 独立帧协议，需 Upgrade | 普通 HTTP 流 | 普通 HTTP 请求 |
| 自动重连 | 无，需自己写指数退避 | 浏览器原生，带 Last-Event-ID | 天然就是下一次请求 |
| 代理与防火墙 | 可能被拦截或需特殊配置 | 兼容性最好 | 兼容性最好 |
| 数据 | 文本与二进制帧 | 仅 UTF-8 文本 | 任意 |
| 连接治理成本 | 心跳、并发写约束、扇出都要自己做 | 服务端代码少一半 | 请求头与握手开销重复 |
| 主要代价 | 每个连接常驻 goroutine 与 fd | 无法双向、不能传二进制 | 延迟高、无效请求多 |

第四种常被提起的替代是 HTTP/2 Server Push，但它面向资源预推送而非应用消息，且已被主流浏览器弃用，不作为实时推送方案。选型的判断只有一句：确实需要客户端高频回传或二进制才付 WebSocket 的连接治理成本，否则 SSE 更省。把「按方向选」换成「按热度选」，是这里最贵的错误。

### 失败矩阵

| 失败场景 | 触发条件 | 可观测现象 | 定位手段 | 对策 |
| --- | --- | --- | --- | --- |
| 半开连接 | 拔网线、NAT 静默断连 | 连接僵住，fd 与内存不释放 | 连接存活时长分布 | 读超时加 Ping/Pong 加读限制 |
| 心跳错配 | `pingPeriod >= pongWait` | 正常连接被周期性误杀、频繁重连 | 断线原因码统计 | 保证 pingPeriod 小于 pongWait |
| 慢消费者 | 广播循环里同步写 conn | 整场推送被最慢连接拖住 | 各连接 send 队列长度 | Hub 只投递缓冲通道，default 踢出 |
| 并发写 | 多 goroutine 直接 WriteMessage | 帧错乱、解析失败甚至 panic | `panic: concurrent write` | 单写 goroutine 加缓冲 channel |
| 断线风暴 | 断线后立刻无限重连 | 服务端恢复瞬间连接数打满 | 每秒新建连接数 | 指数退避加随机抖动 |
| 消息漏收 | 重连不带断点 | 断线期间增量丢失，状态跳变 | 客户端序号连续性 | 消息编号加 Last-Event-ID 补发 |
| 大帧 / 大内存 | 不设 SetReadLimit | 单帧吃满内存 | 进程 RSS 曲线 | 限制单条消息上限并拒绝 |
| 跨站劫持 | CheckOrigin 恒返回 true | 任意站点带用户 Cookie 连上 | Origin 头审计 | 白名单校验 Origin |
| 重试放大 | 断线即重连且无退避 | 重连请求把鉴权服务打满 | 入口与鉴权 QPS 对比 | 退避加抖动加连接数上限 |

<SequenceDiagram title="握手、心跳、广播与断线补发" :actors='["Client","Proxy","Server","Hub","Store"]' :steps='[{"from":"Client","to":"Proxy","label":"Upgrade 请求","note":"携带 Sec-WebSocket-Key","kind":"call"},{"from":"Proxy","to":"Server","label":"转发握手","note":"鉴权在升级前完成","kind":"call"},{"from":"Server","to":"Client","label":"101 切换","note":"带 Sec-WebSocket-Accept","kind":"return"},{"from":"Server","to":"Hub","label":"注册连接","note":"分配缓冲 send 通道","kind":"call"},{"from":"Server","to":"Client","label":"Ping","note":"周期小于 pongWait","kind":"call"},{"from":"Client","to":"Server","label":"Pong","note":"刷新读超时","kind":"call"},{"from":"Store","to":"Hub","label":"新消息","note":"业务事件到达","kind":"async"},{"from":"Hub","to":"Server","label":"扇出投递","note":"非阻塞写 send 队列","kind":"call"},{"from":"Server","to":"Client","label":"推送帧","note":"单写 goroutine 落笔","kind":"call"},{"from":"Client","to":"Client","label":"断线","note":"心跳超时","kind":"self"},{"from":"Client","to":"Server","label":"带断点重连","note":"Last-Event-ID 或序号","kind":"call"},{"from":"Store","to":"Server","label":"补发增量","note":"回放断线期间消息","kind":"return"}]' />

## 4. 工程实践

### 可验证实验：把握手、帧格式和 Flush 跑出来看

**实验一：curl 观察 101 升级与 SSE 帧。** 用 RFC 6455 第 1.3 节给出的固定样例 key 发起握手，再单独请求 SSE 端点看原始文本帧：

```bash
curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  http://127.0.0.1:8080/ws
curl -i -N http://127.0.0.1:8080/events
```

<OutputBox title="握手响应与 SSE 原始帧" output="HTTP/1.1 101 Switching Protocols\nUpgrade: websocket\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\n\nid: 0\nevent: tick\ndata: 2026-11-11T10:00:00Z\n\n: ping" note="101 之后不再有 HTTP 头；SSE 以空行分隔事件，冒号行是心跳注释" />

逐字段解释：`101 Switching Protocols` 表示协议切换成功，此后这条连接改传帧；`Upgrade: websocket` 确认协商结果；`Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=` 正是 `base64(SHA1(key + GUID))` 的结果——它是可计算的，不是随机数，下面的实验二会把它断言出来；`id: 0` 供重连时通过 `Last-Event-ID` 续传；`event: tick` 是自定义事件名；`data:` 后跟载荷；中间的**空行**是事件结束标志；`: ping` 是注释心跳，用来防止代理按空闲超时掐连接。把 SSE Handler 里的 `flusher.Flush()` 注掉再请求，curl 会一直等不到 `data:` 行，直接验证「不 Flush 数据卡在缓冲区」。

**实验二：用 Go 断言 Accept 计算与 SSE 帧，并加 go test -race。** 下面这段完整可编译的测试文件把「握手 Accept 可推导」和「SSE 帧必须带 Content-Type 且 Flush 后可读到完整事件」变成断言：

```go
package main

import (
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func acceptKey(clientKey string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(clientKey + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestSecWebSocketAccept(t *testing.T) {
	// RFC 6455 第 1.3 节的固定样例，用来验证推导链。
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := acceptKey(key); got != want {
		t.Fatalf("accept=%s, want %s", got, want)
	}
}

func TestSSEFrameAndFlush(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: 7\nevent: tick\ndata: hello\n\n"))
		w.(http.Flusher).Flush()
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%s", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data: hello\n\n") {
		t.Fatalf("未读到完整事件帧: %q", body)
	}
}
```

```bash
go test -race -run 'TestSecWebSocketAccept|TestSSEFrameAndFlush' -v ./...
```

<OutputBox title="go test -race 预期输出" output="=== RUN   TestSecWebSocketAccept\n--- PASS: TestSecWebSocketAccept (0.00s)\n=== RUN   TestSSEFrameAndFlush\n--- PASS: TestSSEFrameAndFlush (0.00s)\nPASS" note="Accept 值与规范样例一致，SSE 帧经 Flush 后客户端能读到完整事件" />

逐字段解释：第一个 `--- PASS` 证明 `Sec-WebSocket-Accept` 完全由 key 加常量 GUID 推导，任何人都能独立复算，握手校验不是黑盒；第二个 `--- PASS` 证明 `Content-Type` 正确、且 `Flush()` 之后 `data: hello` 加空行确实抵达客户端——若去掉 `Flush()` 这一行，`httptest` 的短连接虽会在 Handler 返回时冲刷，但在真实长连接里客户端会一直挂住，这正是生产事故与本地测试的差别；`-race` 无告警说明没有共享状态竞争。把 `wsGUID` 改一个字符再跑，第一个用例立即失败，握手契约的可验证性一目了然。

<Predict q="把 pingPeriod 设成大于 pongWait，最可能发生什么？" :options='["连接更省带宽","正常连接被读超时周期性误杀","心跳自动适配","广播变快"]' answer="1" output="Pong 还没来得及刷新读超时，连接就因超时被判定为死连接并断开" explain="读超时由 pongWait 决定，心跳周期必须小于它并留余量；否则心跳失去意义，正常连接反复重连。" />

### 两条真实工程误解

**误解一：升级成 WebSocket 之后连接还是 HTTP，可以继续用中间件设置响应头、补发 Set-Cookie 或再校验一次令牌。** 错。101 响应之后这条 TCP 连接只传帧，不再有请求-应答，也没有新的 HTTP 头；令牌过期只能靠应用层消息主动踢下线或直接发 Close 帧（与 /server/auth 的吊销话题衔接）。把所有鉴权逻辑压在 `Upgrade` 之前，是长连接安全的第一条边界。

**误解二：并发写 conn 只是偶发乱序，库内部加把锁就能解决，gorilla 没加锁是偷懒。** 错。根因是「一条消息可跨多帧、帧之间靠 FIN 与 opcode 维持边界」，即使库给每次 `WriteMessage` 加了锁，也无法保证一条**完整消息**的原子性，两个消息的各帧仍可能交错。库明确不支持并发写，正解是每个连接一个写 goroutine，其余 goroutine 只往带缓冲的 `send` 通道投递；再配合 Hub 的非阻塞 `default` 分支，把慢消费者隔离在它自己的队列里。

### 与后续阶段的呼应

- 本讲的 Hub 只持有本进程连接，多实例广播要经 Redis Pub/Sub 或消息队列跨机扇出，这直接接到 /server/redis 与 /server/mq；连接归属与实例变化则交给 /microservice/discovery 的健康检查与摘除。
- L7 的 /microservice/gateway 必须显式放行 `Upgrade` 并对长连接单独放宽超时，否则握手在网关层就被掐断；/microservice/resilience 的指数退避加抖动正是本讲「断线风暴」的通用解法。
- L10 的 /projects/ai-gateway 用 SSE 做 LLM 流式转发，几乎原封不动复用本讲的 `text/event-stream`、`Flush` 与心跳注释；/projects/micro-seckill 的库存与行情推送则需要 WebSocket 的全双工与连接治理。

上生产前还要补齐几件事。第一，**鉴权前置**：WebSocket 握手仍是一次 HTTP 请求，Bearer Token 或 Cookie 校验要在 `Upgrade` 之前完成；升级成功后连接长期存活，令牌过期需要有踢下线机制（见第 70 讲的吊销话题）。第二，**连接数治理**：单机连接数受 fd 与内存限制，要设全局上限并按用户限流，广播队列要有容量与丢弃策略。第三，**水平扩展**：Hub 只持有本进程的连接，多实例部署时广播必须经由 Redis Pub/Sub 或消息队列做跨机扇出，否则用户连到 A 机器就收不到 B 机器发出的消息。

<Compare title="按方向选型，而不是按热度选型" :panels='[{"label":"场景 A · 双向交互","code":"聊天 / 协同编辑 / 游戏\n客户端需要持续往服务端发消息","note":"选 WebSocket：全双工、可传二进制；自己实现心跳与指数退避重连"},{"label":"场景 B · 单向推送","code":"通知 / 行情 / 进度条 / 日志流\n只需服务端往客户端推","note":"选 SSE：普通 HTTP，代理友好，浏览器自动重连，服务端代码少一半"}]' />

<FlowTimeline title="客户端断线重连的正确姿势" :steps='[{"title":"检测断开","desc":"onclose 或心跳超时触发","tone":"blue"},{"title":"指数退避","desc":"延迟按 1s、2s、4s 增长并加随机抖动","tone":"purple"},{"title":"携带断点","desc":"用 Last-Event-ID 或自增序号请求补发","tone":"orange"},{"title":"重建连接","desc":"重新走握手与鉴权","tone":"green"},{"title":"补齐消息","desc":"回放断线期间的增量，避免跳变","tone":"gray"}]' direction="horizontal" />

还有一个反模式值得点名：**在广播循环里同步写 conn**。它既违反单写约束，又让广播耗时等于最慢客户端的耗时。正确形态永远是「Hub 只往各连接的缓冲通道投递，写泵自己负责落笔」，把慢客户端的影响隔离在它自己的队列里。

<Callout type="idea" title="配套示例工程">

本讲的全部示例代码已整理为可运行工程：[`examples/l6-server/lesson-064-websocket`](https://github.com/lfire/go/tree/main/examples/l6-server/lesson-064-websocket)。clone 仓库后进入 `examples/l6-server`，按该目录 `README.md` 里的命令 `go run` / `go test` 即可逐段验证。章节内的代码与讲解仍按教学需要完整保留，这里提供的是工程化、可直接运行的版本。

</Callout>

## 动手练一练

<Exercise title="给 SSE 加断点续传" task="改造 sseHandler：读取请求头 Last-Event-ID，从该编号之后开始推送事件；同时给每个事件带上递增的 id 字段。请写出关键骨架。" :hints='["浏览器重连时会自动带上 Last-Event-ID 请求头","用 strconv.Atoi 解析，解析失败就从 0 开始","事件帧格式是 id 行、event 行、data 行，最后必须有一个空行"]' solution="lastID := r.Header.Get(lastEventIDHeader)\nstart, err := strconv.Atoi(lastID)\nif err != nil {\nstart = 0\n}\nfor i := start; ; i++ {\nselect {\ncase ctxDone:\nreturn\ncase tick:\nfmt.Fprintf(w, eventFormat, i)\nflusher.Flush()\n}\n}" explain="其中 ctxDone 与 tick 分别代表请求取消通道与定时器通道。续传的语义是「从上次编号之后继续」，注意 start 的边界处理；生产里还应限制一次补发的最大条数，避免长时间断线后重放压垮连接。" />

## 随堂检测

<Quiz no="64-1" q="关于 WebSocket 并发写，下列说法正确的是？" :options='["多个 goroutine 可安全并发调用 WriteMessage","一个连接应由唯一 goroutine 负责写，其余通过 channel 投递","写操作由库内部加锁，无需关心","只要不开压缩就不会有问题"]' answer="1" explain="gorilla/websocket 明确不支持并发写，标准做法是每个连接一个写泵，业务 goroutine 只往它的 send 通道发消息。" />

<Predict no="64-2" q="广播时对慢客户端用了阻塞发送（没有 default 分支），会发生什么？" :options='["只有该客户端收不到消息","整场广播被这条连接卡住，其他客户端一起延迟","库自动丢弃该消息","服务端立即 panic"]' answer="1" output="所有在线客户端的推送都被阻塞，直到慢客户端的 send 队列腾出空间" explain="Hub 的广播循环是串行的，一个阻塞发送会卡住整轮扇出；用带 default 的非阻塞发送并踢出慢客户端，才能隔离故障。" />

## 一页纸速记

<KnowledgeCard :points='["握手是一次带 Upgrade 头的 HTTP 请求，鉴权必须在升级前完成","pingPeriod 小于 pongWait，读写超时与读限制缺一不可","单连接单写 goroutine，业务只往缓冲 channel 投递","Hub 串行化连接状态，广播用非阻塞发送踢出慢客户端","单向推送优先 SSE：普通 HTTP、原生重连、Last-Event-ID 续传"]' :pitfalls='["多 goroutine 直接写同一个 conn，帧错乱或 panic","不设 ReadDeadline，僵尸连接长期占资源","CheckOrigin 无条件返回 true，造成跨站劫持","在广播循环里同步写 conn，被最慢的连接拖垮","断线后立刻无限重连，恢复时引发连接风暴"]' summary="长连接的难点从来不是连上，而是心跳、并发写约束、慢消费者隔离与断线补发。" />

## 小结与下一讲

- WebSocket 通过 101 升级握手把 HTTP 连接变成全双工帧通道，握手前的鉴权是唯一机会。
- 读写泵分离 + 心跳超时 + 读限制，是连接不泄漏、不 panic 的最低配置。
- 广播靠 Hub 串行化与带缓冲的 `send` 通道，慢客户端必须被非阻塞地隔离出去；单向场景优先 SSE。

处理完网络层，接下来要面对后端的另一半：数据存哪里、怎么存。下一讲从 `database/sql` 与连接池开始。

[下一讲：database/sql、连接池与事务](/server/database-sql)
