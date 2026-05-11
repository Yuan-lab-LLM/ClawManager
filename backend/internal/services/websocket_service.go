package services

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"clawreef/internal/models"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Client represents a WebSocket client
type Client struct {
	ID     int
	UserID int
	Conn   *websocket.Conn
	Send   chan []byte
	hub    *Hub
}

// Hub maintains the set of active clients and broadcasts messages
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan *Message
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	stop       chan struct{}
}

// Message represents a WebSocket message
type Message struct {
	Type       string      `json:"type"`
	UserID     int         `json:"user_id,omitempty"`
	InstanceID int         `json:"instance_id,omitempty"`
	Data       interface{} `json:"data"`
	Timestamp  time.Time   `json:"timestamp"`
}

// InstanceStatusUpdate represents an instance status update
type InstanceStatusUpdate struct {
	InstanceID int    `json:"instance_id"`
	Status     string `json:"status"`
	PodName    string `json:"pod_name,omitempty"`
	PodIP      string `json:"pod_ip,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

var (
	hub     *Hub
	hubOnce sync.Once
)

// GetHub returns the global hub instance
func GetHub() *Hub {
	hubOnce.Do(func() {
		hub = NewHub()
		go hub.Run()
	})
	return hub
}

// NewHub creates a new Hub instance
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		stop:       make(chan struct{}),
	}
}

// Run starts the hub's main loop
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			h.mu.Lock()
			for client := range h.clients {
				close(client.Send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			log.Println("WebSocket hub stopped")
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("WebSocket client registered: user=%d", client.UserID)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("WebSocket client unregistered: user=%d", client.UserID)

		case message := <-h.broadcast:
			h.mu.RLock()
			clients := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				// Filter by user ID if specified
				if message.UserID == 0 || client.UserID == message.UserID {
					clients = append(clients, client)
				}
			}
			h.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.Send <- mustEncode(message):
				default:
					// Client's send channel is full, close it
					h.mu.Lock()
					close(client.Send)
					delete(h.clients, client)
					h.mu.Unlock()
				}
			}
		}
	}
}

// mustEncode encodes a message to JSON, panics on error
func mustEncode(msg *Message) []byte {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to encode message: %v", err)
		return []byte(`{"type":"error","data":"encoding error"}`)
	}
	return data
}

// BroadcastInstanceStatus broadcasts instance status update to relevant clients
func (h *Hub) BroadcastInstanceStatus(userID int, instance *models.Instance) {
	update := InstanceStatusUpdate{
		InstanceID: instance.ID,
		Status:     instance.Status,
		UpdatedAt:  instance.UpdatedAt.Format(time.RFC3339),
	}

	if instance.PodName != nil {
		update.PodName = *instance.PodName
	}
	if instance.PodIP != nil {
		update.PodIP = *instance.PodIP
	}

	msg := &Message{
		Type:      "instance_status",
		UserID:    userID,
		Data:      update,
		Timestamp: time.Now(),
	}
	h.broadcast <- msg
}

// BroadcastToAll broadcasts a message to all connected clients
func (h *Hub) BroadcastToAll(msgType string, data interface{}) {
	msg := &Message{
		Type:      msgType,
		Data:      data,
		Timestamp: time.Now(),
	}
	h.broadcast <- msg
}

// ServeWS handles WebSocket connections
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, userID int) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}

	client := &Client{
		UserID: userID,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		hub:    hub,
	}

	client.hub.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// readPump pumps messages from the websocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		// Process incoming messages if needed
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// GetClientCount returns the number of connected clients
func (h *Hub) GetClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}


// Stop gracefully shuts down the hub, closing all client connections.
func (h *Hub) Stop() {
	close(h.stop)
}
