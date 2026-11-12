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
