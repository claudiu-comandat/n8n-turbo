package push

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/n8n-io/n8n-turbo/internal/auth"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

type Client struct {
	ID            string
	UserID        string
	SessionID     string
	hub           *Hub
	conn          *websocket.Conn
	send          chan []byte
	executionSubs map[string]bool
}

type BroadcastTarget struct {
	UserID      string
	SessionID   string
	ExecutionID string
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 16 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	sessionID := r.URL.Query().Get("sessionId")
	pushRef := r.URL.Query().Get("pushRef")
	if sessionID == "" {
		sessionID = pushRef
	}
	if userID == "" {
		if user, ok := auth.UserFromContext(r.Context()); ok {
			userID = user.ID
		}
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	clientID := r.URL.Query().Get("clientId")
	if clientID == "" {
		clientID = pushRef
	}
	if clientID == "" && (userID != "" || sessionID != "") {
		clientID = userID + "-" + sessionID
	}
	if clientID == "" || clientID == "-" {
		clientID = generateClientID()
	}
	client := &Client{ID: clientID, UserID: userID, SessionID: sessionID, hub: h, conn: conn, send: make(chan []byte, 256), executionSubs: make(map[string]bool)}
	h.add(client)
	go client.writePump()
	go client.readPump()
}

func (h *Hub) Publish(message Message) {
	h.publish(message, BroadcastTarget{})
}

func (h *Hub) Broadcast(message Message) {
	h.Publish(message)
}

func (h *Hub) BroadcastToUser(userID string, message Message) {
	h.publish(message, BroadcastTarget{UserID: userID})
}

func (h *Hub) BroadcastToSession(sessionID string, message Message) {
	h.publish(message, BroadcastTarget{SessionID: sessionID})
}

func (h *Hub) BroadcastToExecution(executionID string, message Message) {
	h.publish(message, BroadcastTarget{ExecutionID: executionID})
}

func (h *Hub) SubscribeToExecution(clientID string, executionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if client, ok := h.clients[clientID]; ok {
		client.executionSubs[executionID] = true
	}
}

func (h *Hub) UnsubscribeFromExecution(clientID string, executionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if client, ok := h.clients[clientID]; ok {
		delete(client.executionSubs, executionID)
	}
}

func (h *Hub) publish(message Message, target BroadcastTarget) {
	payload, err := json.Marshal(message)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		if target.UserID != "" && client.UserID != target.UserID {
			continue
		}
		if target.SessionID != "" && client.SessionID != target.SessionID {
			continue
		}
		if target.ExecutionID != "" && !client.executionSubs[target.ExecutionID] {
			continue
		}
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		select {
		case client.send <- payload:
		default:
			h.remove(client)
		}
	}
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) add(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if previous, ok := h.clients[client.ID]; ok {
		delete(h.clients, previous.ID)
		close(previous.send)
		if previous.conn != nil {
			_ = previous.conn.Close()
		}
	}
	h.clients[client.ID] = client
}

func (h *Hub) remove(client *Client) {
	h.mu.Lock()
	if current, ok := h.clients[client.ID]; ok && current == client {
		delete(h.clients, client.ID)
		close(client.send)
	}
	h.mu.Unlock()
	if client.conn != nil {
		_ = client.conn.Close()
	}
}

func (c *Client) readPump() {
	defer c.hub.remove(c)
	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var message struct {
			Type string `json:"type"`
			Data struct {
				ExecutionID string `json:"executionId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		switch message.Type {
		case "subscribeToExecution":
			if message.Data.ExecutionID != "" {
				c.hub.SubscribeToExecution(c.ID, message.Data.ExecutionID)
			}
		case "unsubscribeFromExecution":
			if message.Data.ExecutionID != "" {
				c.hub.UnsubscribeFromExecution(c.ID, message.Data.ExecutionID)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.hub.remove(c)
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func generateClientID() string {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buffer[:])
}
