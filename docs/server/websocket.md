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

## 4. 工程实践

上生产前还要补齐几件事。第一，**鉴权前置**：WebSocket 握手仍是一次 HTTP 请求，Bearer Token 或 Cookie 校验要在 `Upgrade` 之前完成；升级成功后连接长期存活，令牌过期需要有踢下线机制（见第 70 讲的吊销话题）。第二，**连接数治理**：单机连接数受 fd 与内存限制，要设全局上限并按用户限流，广播队列要有容量与丢弃策略。第三，**水平扩展**：Hub 只持有本进程的连接，多实例部署时广播必须经由 Redis Pub/Sub 或消息队列做跨机扇出，否则用户连到 A 机器就收不到 B 机器发出的消息。

<Compare title="按方向选型，而不是按热度选型" :panels='[{"label":"场景 A · 双向交互","code":"聊天 / 协同编辑 / 游戏\n客户端需要持续往服务端发消息","note":"选 WebSocket：全双工、可传二进制；自己实现心跳与指数退避重连"},{"label":"场景 B · 单向推送","code":"通知 / 行情 / 进度条 / 日志流\n只需服务端往客户端推","note":"选 SSE：普通 HTTP，代理友好，浏览器自动重连，服务端代码少一半"}]' />

<FlowTimeline title="客户端断线重连的正确姿势" :steps='[{"title":"检测断开","desc":"onclose 或心跳超时触发","tone":"blue"},{"title":"指数退避","desc":"延迟按 1s、2s、4s 增长并加随机抖动","tone":"purple"},{"title":"携带断点","desc":"用 Last-Event-ID 或自增序号请求补发","tone":"orange"},{"title":"重建连接","desc":"重新走握手与鉴权","tone":"green"},{"title":"补齐消息","desc":"回放断线期间的增量，避免跳变","tone":"gray"}]' direction="horizontal" />

还有一个反模式值得点名：**在广播循环里同步写 conn**。它既违反单写约束，又让广播耗时等于最慢客户端的耗时。正确形态永远是「Hub 只往各连接的缓冲通道投递，写泵自己负责落笔」，把慢客户端的影响隔离在它自己的队列里。

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
