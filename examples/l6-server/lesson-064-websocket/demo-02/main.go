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
