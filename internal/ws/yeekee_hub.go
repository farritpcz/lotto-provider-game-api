// Package ws จัดการ WebSocket connections สำหรับยี่กี real-time
//
// ความสัมพันธ์:
// - ถูกเรียกโดย: handler/yeekee.go → WebSocket endpoint
// - ใช้ lotto-core: betting.ValidateYeekeeShoot() ตรวจเลขยิง
// - ใช้ lotto-core: yeekee.CalculateResult() ตอนออกผล
// - provider-game-api (#7) จะมี Hub เหมือนกันเป๊ะ ต่างแค่ auth (token vs JWT)
//
// Architecture:
//
//	Hub (1 per round)
//	 ├── Client (player 1) ← WebSocket connection
//	 ├── Client (player 2)
//	 └── Client (player N)
//
// Message flow:
//   Player sends: {"type":"shoot","data":{"number":"12345"}}
//   Hub validates → saves to DB → broadcasts to all:
//     {"type":"shoot_broadcast","data":{"member":"user1","number":"12345","total_sum":91346}}
//   Timer ends → Hub calculates result → broadcasts:
//     {"type":"result","data":{"result_number":"91346","top3":"346","top2":"46","bottom2":"13"}}
package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// =============================================================================
// Message Types — ข้อความที่ส่งผ่าน WebSocket
// =============================================================================

// WSMessage ข้อความ WebSocket กลาง
type WSMessage struct {
	Type string      `json:"type"` // shoot, shoot_broadcast, countdown, result, error
	Data interface{} `json:"data"`
}

// ShootData ข้อมูลที่ player ส่งมาตอนยิงเลข
type ShootData struct {
	Number string `json:"number"` // เลข 5 หลัก
}

// ShootBroadcast ข้อมูลที่ broadcast กลับไปทุกคน
type ShootBroadcast struct {
	MemberID       int64  `json:"member_id"`
	MemberUsername string `json:"member_username"`
	Number         string `json:"number"`
	TotalSum       int64  `json:"total_sum"`   // ผลรวมเลขสะสม
	ShootCount     int    `json:"shoot_count"` // จำนวนเลขที่ยิงทั้งหมด
	ShotAt         string `json:"shot_at"`
}

// CountdownData ข้อมูล countdown
type CountdownData struct {
	SecondsRemaining int    `json:"seconds_remaining"`
	RoundNo          int    `json:"round_no"`
	Status           string `json:"status"` // shooting, calculating
}

// ResultData ผลยี่กี
type ResultData struct {
	ResultNumber string `json:"result_number"` // เลข 5 หลัก
	Top3         string `json:"top3"`
	Top2         string `json:"top2"`
	Bottom2      string `json:"bottom2"`
	TotalShoots  int    `json:"total_shoots"`
}

// =============================================================================
// Client — WebSocket connection ของ player 1 คน
// =============================================================================

// Client แทน player 1 คนที่เชื่อมต่อ WebSocket
type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan []byte // channel สำหรับส่งข้อความกลับ
	MemberID int64
	Username string
}

// ReadPump อ่านข้อความจาก WebSocket connection (goroutine)
// เมื่อ player ส่งข้อความมา → ส่งต่อให้ Hub ประมวลผล
func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	// ตั้ง read deadline + pong handler
	c.Conn.SetReadLimit(512)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		// ส่งต่อให้ Hub ประมวลผล
		c.Hub.ProcessMessage(c, message)
	}
}

// WritePump เขียนข้อความกลับไปยัง WebSocket connection (goroutine)
func (c *Client) WritePump() {
	ticker := time.NewTicker(30 * time.Second) // ping ทุก 30 วินาที
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			c.Conn.WriteMessage(websocket.TextMessage, message)

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// =============================================================================
// Hub — จัดการ clients ทั้งหมดของรอบยี่กี 1 รอบ
// =============================================================================

// ShootHandler callback function ที่ API layer ต้อง implement
// ใช้สำหรับ: save shoot to DB, get current total_sum
type ShootHandler func(roundID int64, memberID int64, number string) (totalSum int64, shootCount int, err error)

// Hub จัดการ WebSocket connections ของรอบยี่กี 1 รอบ
type Hub struct {
	RoundID int64
	RoundNo int

	// Channels
	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan []byte

	// State
	clients      map[*Client]bool
	mu           sync.RWMutex
	shootHandler ShootHandler // callback ไปยัง API layer เพื่อ save DB
}

// NewHub สร้าง Hub ใหม่สำหรับรอบยี่กี
func NewHub(roundID int64, roundNo int, handler ShootHandler) *Hub {
	return &Hub{
		RoundID:      roundID,
		RoundNo:      roundNo,
		Register:     make(chan *Client),
		Unregister:   make(chan *Client),
		Broadcast:    make(chan []byte, 256),
		clients:      make(map[*Client]bool),
		shootHandler: handler,
	}
}

// Run เริ่ม Hub loop (ต้องรันเป็น goroutine)
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[YeekeeHub] Client joined round %d (member: %d, total: %d)",
				h.RoundID, client.MemberID, len(h.clients))

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
			}
			h.mu.Unlock()

		case message := <-h.Broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// ProcessMessage ประมวลผลข้อความจาก player
func (h *Hub) ProcessMessage(client *Client, rawMessage []byte) {
	var msg WSMessage
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
		h.sendError(client, "invalid message format")
		return
	}

	switch msg.Type {
	case "shoot":
		h.handleShoot(client, msg.Data)
	default:
		h.sendError(client, "unknown message type: "+msg.Type)
	}
}

// handleShoot ประมวลผลการยิงเลข
func (h *Hub) handleShoot(client *Client, data interface{}) {
	// Parse shoot data
	dataBytes, _ := json.Marshal(data)
	var shootData ShootData
	if err := json.Unmarshal(dataBytes, &shootData); err != nil {
		h.sendError(client, "invalid shoot data")
		return
	}

	// Validate เลขยิง ด้วย lotto-core
	// ⭐ ใช้ lotto-core/betting.ValidateYeekeeShoot()
	// import อยู่ที่ API layer — hub เรียกผ่าน shootHandler callback
	if len(shootData.Number) != 5 {
		h.sendError(client, "number must be 5 digits")
		return
	}

	// Save to DB + get totals ผ่าน callback
	totalSum, shootCount, err := h.shootHandler(h.RoundID, client.MemberID, shootData.Number)
	if err != nil {
		h.sendError(client, err.Error())
		return
	}

	// Broadcast ให้ทุกคนเห็น
	broadcast := ShootBroadcast{
		MemberID:       client.MemberID,
		MemberUsername: client.Username,
		Number:         shootData.Number,
		TotalSum:       totalSum,
		ShootCount:     shootCount,
		ShotAt:         time.Now().Format(time.RFC3339),
	}

	msg, _ := json.Marshal(WSMessage{Type: "shoot_broadcast", Data: broadcast})
	h.Broadcast <- msg
}

// BroadcastCountdown ส่ง countdown ให้ทุกคน
func (h *Hub) BroadcastCountdown(secondsRemaining int, status string) {
	msg, _ := json.Marshal(WSMessage{
		Type: "countdown",
		Data: CountdownData{
			SecondsRemaining: secondsRemaining,
			RoundNo:          h.RoundNo,
			Status:           status,
		},
	})
	h.Broadcast <- msg
}

// BroadcastResult ส่งผลยี่กีให้ทุกคน
func (h *Hub) BroadcastResult(resultNumber, top3, top2, bottom2 string, totalShoots int) {
	msg, _ := json.Marshal(WSMessage{
		Type: "result",
		Data: ResultData{
			ResultNumber: resultNumber,
			Top3:         top3,
			Top2:         top2,
			Bottom2:      bottom2,
			TotalShoots:  totalShoots,
		},
	})
	h.Broadcast <- msg
}

// ClientCount จำนวน clients ที่เชื่อมต่ออยู่
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// sendError ส่ง error กลับไปให้ player คนเดียว
func (h *Hub) sendError(client *Client, message string) {
	msg, _ := json.Marshal(WSMessage{Type: "error", Data: map[string]string{"message": message}})
	select {
	case client.Send <- msg:
	default:
	}
}
