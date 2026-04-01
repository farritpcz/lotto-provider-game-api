// Package handler — stubs.go
// Stub implementations สำหรับ provider-game-api
package handler

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

func todo(c *gin.Context, name string) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": name + " - TODO"})
}

// === Operator API — Seamless Wallet ===
func (h *Handler) SeamlessBalance(c *gin.Context) { todo(c, "seamless balance") }
func (h *Handler) SeamlessDebit(c *gin.Context)   { todo(c, "seamless debit") }
func (h *Handler) SeamlessCredit(c *gin.Context)  { todo(c, "seamless credit") }

// === Operator API — Transfer Wallet ===
func (h *Handler) TransferDeposit(c *gin.Context)  { todo(c, "transfer deposit") }
func (h *Handler) TransferWithdraw(c *gin.Context) { todo(c, "transfer withdraw") }

// === Operator API — Games ===
func (h *Handler) ListGames(c *gin.Context)      { todo(c, "list games") }
func (h *Handler) ListGameRounds(c *gin.Context) { todo(c, "list game rounds") }

// GameLaunch สร้าง game URL สำหรับ player เข้าเล่นผ่าน iframe
// ⭐ สำคัญ: Operator เรียก API นี้ → ได้ URL + token → ส่งให้ player เปิดใน iframe
//
// Request: { "player_id": "ext123", "game_code": "THAI", "currency": "THB", "language": "th" }
// Response: { "url": "https://game.lotto.com/launch?token=xxx", "token": "xxx" }
func (h *Handler) GameLaunch(c *gin.Context) { todo(c, "game launch") }

// === Operator API — Results & Reports ===
func (h *Handler) GetResults(c *gin.Context)   { todo(c, "get results") }
func (h *Handler) GetBetReport(c *gin.Context) { todo(c, "bet report") }

// === Game Client API ===
func (h *Handler) GameLobby(c *gin.Context)   { todo(c, "game lobby") }
func (h *Handler) GameRounds(c *gin.Context)  { todo(c, "game rounds") }

// GamePlaceBets วางเดิมพันจาก game client
// ⭐ ใช้ lotto-core เหมือน standalone-member-api (#3) เป๊ะ
// ต่างกันที่:
//   - wallet: เรียก operator API (seamless/transfer) แทน internal wallet
//   - callback: แจ้ง operator ว่ามี bet เข้า
//   - operatorID: ต้องมี (standalone ไม่มี)
func (h *Handler) GamePlaceBets(c *gin.Context) { todo(c, "game place bets") }

func (h *Handler) GameMyBets(c *gin.Context)  { todo(c, "game my bets") }
func (h *Handler) GameResults(c *gin.Context) { todo(c, "game results") }
func (h *Handler) GameHistory(c *gin.Context) { todo(c, "game history") }
func (h *Handler) GameBalance(c *gin.Context) { todo(c, "game balance") }

// GameYeekeeWS WebSocket endpoint สำหรับยี่กี
// ⭐ WebSocket protocol เหมือน standalone (#3) เป๊ะ
// ต่างแค่ auth (launch token vs JWT)
func (h *Handler) GameYeekeeWS(c *gin.Context) { todo(c, "yeekee websocket") }
