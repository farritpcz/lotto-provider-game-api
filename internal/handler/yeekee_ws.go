// Package handler — yeekee_ws.go
//
// WebSocket endpoint สำหรับยี่กี (game client → provider)
//
// ⭐ เทียบกับ standalone (#3):
//   - auth: launch token (ผ่าน middleware.LaunchTokenAuthWithSecret) แทน JWT
//   - Hub lifecycle: HubManager จัดการเหมือน standalone
//   - ShootHandler: service.YeekeeService.HandleShoot
//
// Flow (หลัง launch token middleware):
//  1. parse round id จาก URL
//  2. load round + shoots (initial state)
//  3. upgrade HTTP → WebSocket
//  4. GetOrCreateHub + register client
//  5. ReadPump (blocking) — return เมื่อ client disconnect
package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/farritpcz/lotto-provider-game-api/internal/middleware"
	"github.com/farritpcz/lotto-provider-game-api/internal/model"
	"github.com/farritpcz/lotto-provider-game-api/internal/ws"
)

// jsonMarshalMsg marshal WSMessage wrapper — กระชับ call site
func jsonMarshalMsg(msgType string, data interface{}) ([]byte, error) {
	return json.Marshal(ws.WSMessage{Type: msgType, Data: data})
}

// parseInt64Safe parse int64 — ถ้า error return 0 (ไม่ crash) เพราะเลขควรเป็น 5 หลักเลขล้วนอยู่แล้ว
func parseInt64Safe(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// wsUpgrader สร้าง WebSocket upgrader ที่ตรวจ Origin header
//
// - allowed origins ถูก config ผ่าน h.AllowedOrigins (main.go inject)
// - ถ้าไม่มี Origin (non-browser client เช่น mobile app) → อนุญาต
// - development: localhost ทุก port
func (h *Handler) wsUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			for _, ao := range h.AllowedOrigins {
				if ao == origin {
					return true
				}
				// dev convenience: localhost ทุก port match
				if strings.HasPrefix(ao, "http://localhost") && strings.HasPrefix(origin, "http://localhost") {
					return true
				}
			}
			return false
		},
	}
}

// GameYeekeeWS — WebSocket endpoint สำหรับยี่กี (per round)
//
// URL: GET /api/v1/game/yeekee/ws/:roundId
// Auth: launch token middleware (middleware.LaunchTokenAuthWithSecret)
func (h *Handler) GameYeekeeWS(c *gin.Context) {
	// 1. parse round id
	roundID, err := strconv.ParseInt(c.Param("roundId"), 10, 64)
	if err != nil {
		fail(c, 400, "invalid round id")
		return
	}

	// 2. member info (จาก launch token middleware)
	memberID := middleware.GetMemberID(c)
	if memberID == 0 {
		fail(c, 401, "unauthorized")
		return
	}

	// 3. load round + shoots (initial state)
	round, shoots, err := h.YeekeeService.GetRoundWithShoots(roundID)
	if err != nil {
		fail(c, 404, "round not found")
		return
	}

	// 4. อนุญาตเฉพาะรอบที่กำลัง active (shooting/waiting)
	if round.Status != "shooting" && round.Status != "waiting" {
		fail(c, 400, "round is not active (status: "+round.Status+")")
		return
	}

	// 5. ดึง username (ไว้แสดงใน shoot_broadcast)
	var member model.Member
	h.DB.Select("external_player_id, username").First(&member, memberID)
	username := member.Username
	if username == "" {
		username = member.ExternalPlayerID
	}

	// 6. Upgrade HTTP → WebSocket
	up := h.wsUpgrader()
	conn, err := up.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[YeekeeWS] Failed to upgrade connection: %v", err)
		return
	}

	// 7. Get/create Hub + register client
	hub := h.HubManager.GetOrCreateHub(roundID, round.RoundNo, h.YeekeeService.HandleShoot)
	client := &ws.Client{
		Hub:      hub,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		MemberID: memberID,
		Username: username,
	}
	hub.Register <- client

	// 8. ส่ง initial state (countdown + existing shoots)
	sendInitialState(client, round, shoots)

	// 9. เริ่ม pumps — WritePump goroutine, ReadPump blocking
	go client.WritePump()
	client.ReadPump()
}

// sendInitialState ส่ง round info + existing shoots ให้ client ที่เพิ่ง connect
func sendInitialState(client *ws.Client, round *model.YeekeeRound, shoots []model.YeekeeShoot) {
	// countdown
	secondsRemaining := int(time.Until(round.EndTime).Seconds())
	if secondsRemaining < 0 {
		secondsRemaining = 0
	}
	status := round.Status

	// ส่ง countdown แยก message
	countdownMsg, _ := jsonMarshalMsg("countdown", ws.CountdownData{
		SecondsRemaining: secondsRemaining,
		RoundNo:          round.RoundNo,
		Status:           status,
	})
	client.Send <- countdownMsg

	// ส่ง existing shoots เป็น shoot_broadcast ทีละตัว
	var totalSum int64
	for i, s := range shoots {
		num := parseInt64Safe(s.Number)
		totalSum += num
		username := ""
		if s.Member != nil {
			username = s.Member.Username
			if username == "" {
				username = s.Member.ExternalPlayerID
			}
		}
		msg, _ := jsonMarshalMsg("shoot_broadcast", ws.ShootBroadcast{
			MemberID:       s.MemberID,
			MemberUsername: username,
			Number:         s.Number,
			TotalSum:       totalSum,
			ShootCount:     i + 1,
			ShotAt:         s.ShotAt.Format(time.RFC3339),
		})
		client.Send <- msg
	}
}
