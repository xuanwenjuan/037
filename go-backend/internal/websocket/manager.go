package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"thz-service/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Manager struct {
	upgrader websocket.Upgrader
	clients  map[string]map[*websocket.Conn]bool
	mu       sync.RWMutex
}

func NewManager(bufferSize int) *Manager {
	return &Manager{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  bufferSize,
			WriteBufferSize: bufferSize,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		clients: make(map[string]map[*websocket.Conn]bool),
	}
}

func (m *Manager) HandleConnection(c *gin.Context) {
	analysisID := c.Param("id")
	if analysisID == "" {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "analysis id is required"})
		return
	}

	conn, err := m.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	m.addClient(analysisID, conn)
	log.Printf("Client connected for analysis %s", analysisID)

	go m.readLoop(analysisID, conn)
}

func (m *Manager) addClient(analysisID string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[analysisID]; !exists {
		m.clients[analysisID] = make(map[*websocket.Conn]bool)
	}
	m.clients[analysisID][conn] = true
}

func (m *Manager) removeClient(analysisID string, conn *websocket.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if clients, exists := m.clients[analysisID]; exists {
		delete(clients, conn)
		if len(clients) == 0 {
			delete(m.clients, analysisID)
		}
	}
	conn.Close()
}

func (m *Manager) readLoop(analysisID string, conn *websocket.Conn) {
	defer m.removeClient(analysisID, conn)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error for %s: %v", analysisID, err)
			break
		}
	}
}

func (m *Manager) BroadcastProgress(analysisID string, msg *models.ProgressMessage) {
	m.mu.RLock()
	clients, exists := m.clients[analysisID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal progress message: %v", err)
		return
	}

	m.mu.RLock()
	for conn := range clients {
		err := conn.WriteMessage(websocket.TextMessage, body)
		if err != nil {
			log.Printf("WebSocket write error: %v", err)
			go m.removeClient(analysisID, conn)
		}
	}
	m.mu.RUnlock()
}

func (m *Manager) HasClients(analysisID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.clients[analysisID]
	return exists
}
