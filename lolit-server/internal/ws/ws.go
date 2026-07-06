package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub broadcasts lock events to connected clients.
type Hub struct {
	mu       sync.RWMutex
	clients  map[*conn]bool
	upgrader websocket.Upgrader
}

type conn struct {
	socket *websocket.Conn
	send   chan []byte
}

type Event struct {
	Type   string `json:"type"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	User   string `json:"user"`
	Locked bool   `json:"locked"`
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	socket, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("ws upgrade:", err)
		return
	}
	c := &conn{socket: socket, send: make(chan []byte, 256)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	go c.writePump()
	c.readPump(h)
}

func (c *conn) readPump(h *Hub) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		c.socket.Close()
	}()
	c.socket.SetReadLimit(512)
	for {
		_, _, err := c.socket.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *conn) writePump() {
	defer c.socket.Close()
	for msg := range c.send {
		if err := c.socket.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

func (h *Hub) Broadcast(ev Event) {
	b, _ := json.Marshal(ev)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- b:
		default:
			// channel full; drop
		}
	}
}
