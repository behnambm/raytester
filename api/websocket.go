package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSClient struct {
	conn *websocket.Conn
	send chan []byte
}

type WSHub struct {
	clients    map[*WSClient]bool
	broadcast  chan []byte
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
}

func NewWSHub() *WSHub {
	log.Printf("[api:ws] NewWSHub: creating hub")
	return &WSHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

func (h *WSHub) Run() {
	log.Printf("[api:ws] hub: Run loop started")
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			count := len(h.clients)
			h.mu.Unlock()
			log.Printf("[api:ws] hub: client registered (total: %d)", count)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			count := len(h.clients)
			h.mu.Unlock()
			log.Printf("[api:ws] hub: client unregistered (total: %d)", count)

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *WSHub) Broadcast(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[api:ws] Broadcast: marshal FAILED: %v", err)
		return
	}
	log.Printf("[api:ws] Broadcast: sending type=%s (%d bytes)", msg.Type, len(data))
	h.broadcast <- data
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	log.Printf("[api:ws] handleWebSocket: request for session=%s", id)

	if id == "" {
		log.Printf("[api:ws] handleWebSocket: missing session id")
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	session := s.getSession(id)
	if session == nil {
		log.Printf("[api:ws] handleWebSocket: session %s not found", id)
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[api:ws] handleWebSocket: upgrade FAILED: %v", err)
		return
	}

	log.Printf("[api:ws] handleWebSocket: upgraded to WebSocket for session %s", id)

	client := &WSClient{
		conn: conn,
		send: make(chan []byte, 256),
	}

	session.WsHub.register <- client

	go client.writePump()
	go client.readPump(session.WsHub)
}

func (c *WSClient) writePump() {
	log.Printf("[api:ws] writePump: started")
	defer func() {
		c.conn.Close()
		log.Printf("[api:ws] writePump: closed")
	}()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("[api:ws] writePump: write error: %v", err)
			return
		}
	}
}

func (c *WSClient) readPump(hub *WSHub) {
	log.Printf("[api:ws] readPump: started")
	defer func() {
		hub.unregister <- c
		c.conn.Close()
		log.Printf("[api:ws] readPump: closed and unregistered")
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			log.Printf("[api:ws] readPump: read error: %v", err)
			return
		}
	}
}
