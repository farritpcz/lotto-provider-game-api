// Package handler — IMPLEMENTED handlers for provider-game-api
//
// ⭐ ต่างจาก standalone-member-api (#3):
//   - Operator API: ใช้ HMAC auth, wallet ผ่าน operator
//   - Game Client API: ใช้ launch token, ไม่มี login/register/wallet
//   - มี operator_id ในทุก query
package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"success": false, "error": msg})
}

// =============================================================================
// Operator API — Seamless Wallet
// =============================================================================
// ⭐ Operator เรียก endpoints เหล่านี้เพื่อจัดการเงินของ player

func (h *Handler) SeamlessBalance(c *gin.Context) {
	var req struct {
		PlayerID string `json:"player_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	// หา member จาก external_player_id + operator
	apiKey := c.GetString("api_key")
	var operator model.Operator
	if err := h.DB.Where("api_key = ?", apiKey).First(&operator).Error; err != nil {
		fail(c, 401, "invalid operator"); return
	}

	var member model.Member
	if err := h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member).Error; err != nil {
		// สร้าง member ใหม่ถ้ายังไม่มี (auto-register)
		member = model.Member{
			OperatorID:       operator.ID,
			ExternalPlayerID: req.PlayerID,
			Balance:          0,
			Status:           "active",
		}
		h.DB.Create(&member)
	}

	ok(c, gin.H{"balance": member.Balance, "player_id": req.PlayerID})
}

func (h *Handler) SeamlessDebit(c *gin.Context) {
	var req struct {
		PlayerID string  `json:"player_id" binding:"required"`
		Amount   float64 `json:"amount" binding:"required"`
		TxnID    string  `json:"txn_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	// ⭐ Seamless: provider ไม่ได้เก็บเงินจริง — แค่ forward ให้ operator
	// ใน production จะเรียก operator API กลับ (ดู service/wallet_service.go)
	// ตอนนี้ทำแบบ transfer mode (หักจาก balance ใน provider DB)
	apiKey := c.GetString("api_key")
	var operator model.Operator
	h.DB.Where("api_key = ?", apiKey).First(&operator)

	result := h.DB.Model(&model.Member{}).
		Where("operator_id = ? AND external_player_id = ? AND balance >= ?", operator.ID, req.PlayerID, req.Amount).
		Update("balance", h.DB.Raw("balance - ?", req.Amount))
	if result.RowsAffected == 0 {
		fail(c, 400, "insufficient balance"); return
	}

	var member model.Member
	h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member)
	ok(c, gin.H{"balance": member.Balance, "txn_id": req.TxnID})
}

func (h *Handler) SeamlessCredit(c *gin.Context) {
	var req struct {
		PlayerID string  `json:"player_id" binding:"required"`
		Amount   float64 `json:"amount" binding:"required"`
		TxnID    string  `json:"txn_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	apiKey := c.GetString("api_key")
	var operator model.Operator
	h.DB.Where("api_key = ?", apiKey).First(&operator)

	h.DB.Model(&model.Member{}).
		Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).
		Update("balance", h.DB.Raw("balance + ?", req.Amount))

	var member model.Member
	h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member)
	ok(c, gin.H{"balance": member.Balance, "txn_id": req.TxnID})
}

// =============================================================================
// Operator API — Transfer Wallet
// =============================================================================

func (h *Handler) TransferDeposit(c *gin.Context) {
	var req struct {
		PlayerID string  `json:"player_id" binding:"required"`
		Amount   float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	apiKey := c.GetString("api_key")
	var operator model.Operator
	h.DB.Where("api_key = ?", apiKey).First(&operator)

	// Auto-create member if not exists
	var member model.Member
	if err := h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member).Error; err != nil {
		member = model.Member{OperatorID: operator.ID, ExternalPlayerID: req.PlayerID, Status: "active"}
		h.DB.Create(&member)
	}

	h.DB.Model(&member).Update("balance", h.DB.Raw("balance + ?", req.Amount))

	// บันทึก wallet transaction
	h.DB.Create(&model.WalletTransaction{
		OperatorID:       operator.ID,
		MemberExternalID: req.PlayerID,
		Type:             "deposit",
		Amount:           req.Amount,
		CreatedAt:        time.Now(),
	})

	h.DB.First(&member, member.ID)
	ok(c, gin.H{"balance": member.Balance})
}

func (h *Handler) TransferWithdraw(c *gin.Context) {
	var req struct {
		PlayerID string  `json:"player_id" binding:"required"`
		Amount   float64 `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	apiKey := c.GetString("api_key")
	var operator model.Operator
	h.DB.Where("api_key = ?", apiKey).First(&operator)

	result := h.DB.Model(&model.Member{}).
		Where("operator_id = ? AND external_player_id = ? AND balance >= ?", operator.ID, req.PlayerID, req.Amount).
		Update("balance", h.DB.Raw("balance - ?", req.Amount))
	if result.RowsAffected == 0 {
		fail(c, 400, "insufficient balance"); return
	}

	h.DB.Create(&model.WalletTransaction{
		OperatorID:       operator.ID,
		MemberExternalID: req.PlayerID,
		Type:             "withdraw",
		Amount:           req.Amount,
		CreatedAt:        time.Now(),
	})

	var member model.Member
	h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member)
	ok(c, gin.H{"balance": member.Balance})
}

// =============================================================================
// Operator API — Games
// =============================================================================

func (h *Handler) ListGames(c *gin.Context) {
	var types []model.LotteryType
	h.DB.Where("status = ?", "active").Find(&types)
	ok(c, types)
}

func (h *Handler) ListGameRounds(c *gin.Context) {
	gameID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var rounds []model.LotteryRound
	h.DB.Where("lottery_type_id = ? AND status = ?", gameID, "open").
		Preload("LotteryType").Order("close_time ASC").Find(&rounds)
	ok(c, rounds)
}

// GameLaunch สร้าง game URL + launch token
// ⭐ Operator เรียก API นี้ → ได้ URL → player เปิด URL ใน iframe → game client (#8)
func (h *Handler) GameLaunch(c *gin.Context) {
	var req struct {
		PlayerID string `json:"player_id" binding:"required"`
		GameCode string `json:"game_code"` // optional — เปิดหน้า lobby ถ้าไม่ระบุ
		Currency string `json:"currency"`
		Language string `json:"language"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }

	apiKey := c.GetString("api_key")
	var operator model.Operator
	if err := h.DB.Where("api_key = ?", apiKey).First(&operator).Error; err != nil {
		fail(c, 401, "invalid operator"); return
	}

	// Auto-create member
	var member model.Member
	if err := h.DB.Where("operator_id = ? AND external_player_id = ?", operator.ID, req.PlayerID).First(&member).Error; err != nil {
		member = model.Member{OperatorID: operator.ID, ExternalPlayerID: req.PlayerID, Status: "active"}
		h.DB.Create(&member)
	}

	// สร้าง launch token (simplified — production ใช้ JWT)
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	// TODO: เก็บ token ใน Redis กับ TTL + map กับ member_id + operator_id
	// ตอนนี้ return token ตรงๆ (development)

	gameURL := fmt.Sprintf("http://localhost:3002/launch?token=%s", token)
	if req.GameCode != "" {
		gameURL += "&game=" + req.GameCode
	}

	ok(c, gin.H{
		"url":       gameURL,
		"token":     token,
		"player_id": req.PlayerID,
		"expires":   time.Now().Add(time.Duration(60) * time.Minute).Unix(),
	})
}

// =============================================================================
// Operator API — Results & Reports
// =============================================================================

func (h *Handler) GetResults(c *gin.Context) {
	var rounds []model.LotteryRound
	h.DB.Where("status = ?", "resulted").Preload("LotteryType").
		Order("resulted_at DESC").Limit(50).Find(&rounds)
	ok(c, rounds)
}

func (h *Handler) GetBetReport(c *gin.Context) {
	apiKey := c.GetString("api_key")
	var operator model.Operator
	h.DB.Where("api_key = ?", apiKey).First(&operator)

	var result struct {
		TotalBets   int64   `json:"total_bets"`
		TotalAmount float64 `json:"total_amount"`
		TotalWin    float64 `json:"total_win"`
	}
	dateFrom := c.DefaultQuery("from", time.Now().AddDate(0, 0, -7).Format("2006-01-02"))
	dateTo := c.DefaultQuery("to", time.Now().Format("2006-01-02"))

	h.DB.Model(&model.Bet{}).Where("operator_id = ? AND DATE(created_at) BETWEEN ? AND ?", operator.ID, dateFrom, dateTo).Count(&result.TotalBets)
	h.DB.Model(&model.Bet{}).Where("operator_id = ? AND DATE(created_at) BETWEEN ? AND ?", operator.ID, dateFrom, dateTo).
		Select("COALESCE(SUM(amount), 0)").Scan(&result.TotalAmount)
	h.DB.Model(&model.Bet{}).Where("operator_id = ? AND DATE(created_at) BETWEEN ? AND ? AND status = ?", operator.ID, dateFrom, dateTo, "won").
		Select("COALESCE(SUM(win_amount), 0)").Scan(&result.TotalWin)

	ok(c, result)
}

// =============================================================================
// Game Client API — ⭐ player เล่นผ่าน iframe (provider-game-web #8)
// =============================================================================

func (h *Handler) GameLobby(c *gin.Context) {
	var types []model.LotteryType
	h.DB.Where("status = ?", "active").Find(&types)
	ok(c, types)
}

func (h *Handler) GameRounds(c *gin.Context) {
	typeID, _ := strconv.ParseInt(c.Param("typeId"), 10, 64)
	var rounds []model.LotteryRound
	h.DB.Where("lottery_type_id = ? AND status = ?", typeID, "open").
		Preload("LotteryType").Order("close_time ASC").Find(&rounds)
	ok(c, rounds)
}

// GamePlaceBets — ⭐ ใช้ logic เดียวกับ standalone (#3) แต่ wallet + scope ต่างกัน
func (h *Handler) GamePlaceBets(c *gin.Context) {
	// TODO: parse launch token → ได้ member_id + operator_id
	// จากนั้น logic เหมือน standalone-member-api (#3) BetService.PlaceBets()
	// ต่างกันที่:
	//   1. wallet: เรียก operator API (seamless) หรือหักจาก provider balance (transfer)
	//   2. bet.operator_id = operator_id (standalone ไม่มี)
	//   3. numberban: เช็คทั้ง global + per-operator bans
	//   4. rate: ใช้ operator rate ถ้ามี ไม่งั้นใช้ global rate
	ok(c, gin.H{"message": "place bets — lotto-core integration TODO (same logic as standalone #3)"})
}

func (h *Handler) GameMyBets(c *gin.Context) {
	// TODO: parse token → member_id → query bets
	ok(c, gin.H{"bets": []interface{}{}, "total": 0})
}

func (h *Handler) GameResults(c *gin.Context) {
	var rounds []model.LotteryRound
	h.DB.Where("status = ?", "resulted").Preload("LotteryType").
		Order("resulted_at DESC").Limit(20).Find(&rounds)
	ok(c, rounds)
}

func (h *Handler) GameHistory(c *gin.Context) {
	// TODO: parse token → member_id → query bet history
	ok(c, gin.H{"bets": []interface{}{}, "total": 0})
}

func (h *Handler) GameBalance(c *gin.Context) {
	// TODO: parse token → member_id → get balance
	ok(c, gin.H{"balance": 0})
}

// GameYeekeeWS — WebSocket endpoint
// ⭐ ใช้ Hub เดียวกับ standalone (#3) → ws/yeekee_hub.go
// ต่างแค่ auth: parse launch token แทน JWT
func (h *Handler) GameYeekeeWS(c *gin.Context) {
	ok(c, gin.H{"message": "yeekee websocket — use Hub from ws package"})
}
