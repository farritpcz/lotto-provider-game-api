// Package ws — hub_manager.go
// จัดการ lifecycle ของ Hub (1 Hub ต่อ 1 รอบยี่กี)
//
// ทำหน้าที่:
// - สร้าง/ดึง Hub สำหรับแต่ละรอบ (thread-safe ด้วย sync.Map)
// - ลบ Hub เมื่อรอบจบ (cleanup memory)
// - Broadcast result + countdown ให้ทุก client ในรอบ
//
// ⭐ port จาก lotto-standalone-member-api/internal/ws/hub_manager.go
//   API compatible — ต่างเฉพาะ package path ของ import (repo)
//
// ความสัมพันธ์:
// - ถูกสร้างใน main.go → inject ให้ handler + cron job
// - handler.GameYeekeeWS → GetOrCreateHub เมื่อ client connect WebSocket
// - job/yeekee_cron.go → BroadcastResult เมื่อรอบจบ
package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// HubManager จัดการ Hub instances ทั้งหมด (1 Hub = 1 รอบยี่กี)
type HubManager struct {
	hubs sync.Map // map[int64]*Hub — key = yeekee_round_id
}

// NewHubManager สร้าง HubManager instance ใหม่
func NewHubManager() *HubManager {
	return &HubManager{}
}

// GetOrCreateHub ดึง Hub ที่มีอยู่แล้ว หรือสร้างใหม่ถ้ายังไม่มี
//
// thread-safe — ใช้ sync.Map.LoadOrStore เพื่อป้องกัน race condition
// ถ้าสร้างใหม่ → เริ่ม Hub.Run() goroutine ทันที
func (m *HubManager) GetOrCreateHub(roundID int64, roundNo int, handler ShootHandler) *Hub {
	if val, ok := m.hubs.Load(roundID); ok {
		return val.(*Hub)
	}

	hub := NewHub(roundID, roundNo, handler)
	actual, loaded := m.hubs.LoadOrStore(roundID, hub)
	if loaded {
		return actual.(*Hub)
	}

	go hub.Run()
	log.Printf("[HubManager] Created new Hub for yeekee round %d (no: %d)", roundID, roundNo)
	return hub
}

// GetHub ดึง Hub ที่มีอยู่ (ไม่สร้างใหม่); คืน nil ถ้ายังไม่มี
func (m *HubManager) GetHub(roundID int64) *Hub {
	if val, ok := m.hubs.Load(roundID); ok {
		return val.(*Hub)
	}
	return nil
}

// RemoveHub ลบ Hub ออกจาก manager
func (m *HubManager) RemoveHub(roundID int64) {
	m.hubs.Delete(roundID)
	log.Printf("[HubManager] Removed Hub for yeekee round %d", roundID)
}

// BroadcastResult ส่งผลยี่กีให้ทุก client ในรอบ
// เรียกจาก cron หลังคำนวณผลเสร็จ
func (m *HubManager) BroadcastResult(roundID int64, resultNumber, top3, top2, bottom2 string, totalShoots int) {
	hub := m.GetHub(roundID)
	if hub == nil {
		return
	}
	hub.BroadcastResult(resultNumber, top3, top2, bottom2, totalShoots)
}

// BroadcastCountdown ส่ง countdown ให้ทุก client ในรอบ
func (m *HubManager) BroadcastCountdown(roundID int64, secondsRemaining int, status string) {
	hub := m.GetHub(roundID)
	if hub == nil {
		return
	}
	hub.BroadcastCountdown(secondsRemaining, status)
}

// BroadcastBotShoot ส่ง bot shoot broadcast (ชื่อแสดงเป็นเบอร์โทรสุ่ม)
func (m *HubManager) BroadcastBotShoot(roundID int64, number string, totalSum int64, shootCount int) {
	hub := m.GetHub(roundID)
	if hub == nil {
		return
	}
	msg, _ := json.Marshal(WSMessage{
		Type: "shoot_broadcast",
		Data: ShootBroadcast{
			MemberID:       0,
			MemberUsername: generateFakePhone(),
			Number:         number,
			TotalSum:       totalSum,
			ShootCount:     shootCount,
			ShotAt:         time.Now().Format(time.RFC3339),
		},
	})
	hub.Broadcast <- msg
}

// generateFakePhone สร้างเบอร์โทรปลอมเช่น "09x-xxx-4821"
func generateFakePhone() string {
	prefixes := []string{"06", "08", "09"}
	prefix := prefixes[time.Now().UnixNano()%3]
	last4 := fmt.Sprintf("%04d", time.Now().UnixNano()%10000)
	return prefix + "x-xxx-" + last4
}

// ActiveHubCount จำนวน Hub ที่ active (monitoring)
func (m *HubManager) ActiveHubCount() int {
	count := 0
	m.hubs.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}
