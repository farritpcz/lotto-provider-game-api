// Package handler — IMPLEMENTED handlers for provider-game-api
//
// ⭐ ต่างจาก standalone-member-api (#3):
//   - Operator API: ใช้ HMAC auth, wallet ผ่าน operator
//   - Game Client API: ใช้ launch token, ไม่มี login/register/wallet
//   - มี operator_id ในทุก query
package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/farritpcz/lotto-core/httpx"
	"github.com/farritpcz/lotto-provider-game-api/internal/middleware"
	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

// Response helpers — thin wrappers over lotto-core/httpx (source of truth).
func ok(c *gin.Context, data interface{})         { httpx.OK(c, data) }
func fail(c *gin.Context, status int, msg string) { httpx.Fail(c, status, msg) }

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
	operatorID := middleware.GetOperatorID(c)
	var operator model.Operator
	h.DB.First(&operator, operatorID)

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

	operatorID := middleware.GetOperatorID(c)
	var operator model.Operator
	h.DB.First(&operator, operatorID)

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

	operatorID := middleware.GetOperatorID(c)
	var operator model.Operator
	h.DB.First(&operator, operatorID)

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

	operatorID := middleware.GetOperatorID(c)
	var operator model.Operator
	h.DB.First(&operator, operatorID)

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

	// ⭐ สร้าง launch token จริง (JWT)
	token, err := middleware.GenerateLaunchToken(member.ID, operator.ID, req.PlayerID, h.LaunchTokenSecret, 60)
	if err != nil {
		fail(c, 500, "failed to generate token"); return
	}

	gameURL := fmt.Sprintf("http://localhost:3002/launch?token=%s", token)
	if req.GameCode != "" {
		gameURL += "&game=" + req.GameCode
	}

	ok(c, gin.H{
		"url":       gameURL,
		"token":     token,
		"player_id": req.PlayerID,
		"expires":   time.Now().Add(60 * time.Minute).Unix(),
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
	operatorID := middleware.GetOperatorID(c)
	var operator model.Operator
	h.DB.First(&operator, operatorID)

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

// GamePlaceBets — ⭐ วางเดิมพันจาก game client
// ใช้ logic คล้าย standalone (#3) แต่:
//   - wallet: หักจาก provider balance (transfer) — AIDEV-TODO(farri, 2026-04-21): seamless API call (ดู docs/rules/seamless_wallet.md)
//   - scope: per operator_id
//   - bans: เช็คทั้ง global + per-operator
func (h *Handler) GamePlaceBets(c *gin.Context) {
	memberID := middleware.GetMemberID(c)
	operatorID := middleware.GetOperatorID(c)

	var req struct {
		Bets []struct {
			LotteryRoundID int64   `json:"lottery_round_id"`
			BetTypeCode    string  `json:"bet_type_code"`
			Number         string  `json:"number"`
			Amount         float64 `json:"amount"`
		} `json:"bets" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, 400, err.Error()); return }
	if len(req.Bets) == 0 { fail(c, 400, "no bets"); return }

	var member model.Member
	if err := h.DB.First(&member, memberID).Error; err != nil { fail(c, 404, "member not found"); return }

	successCount := 0
	totalAmount := 0.0
	var errors []gin.H

	tx := h.DB.Begin()

	for _, b := range req.Bets {
		// ดึง round
		var round model.LotteryRound
		if err := h.DB.First(&round, b.LotteryRoundID).Error; err != nil {
			errors = append(errors, gin.H{"number": b.Number, "reason": "round not found"})
			continue
		}
		if round.Status != "open" {
			errors = append(errors, gin.H{"number": b.Number, "reason": "round not open"})
			continue
		}

		// ดึง bet type + rate (operator rate ก่อน, fallback global)
		var betType model.BetType
		if err := h.DB.Where("code = ?", b.BetTypeCode).First(&betType).Error; err != nil {
			errors = append(errors, gin.H{"number": b.Number, "reason": "invalid bet type"})
			continue
		}
		var rate model.PayRate
		// ลอง operator rate ก่อน
		err := h.DB.Where("lottery_type_id = ? AND bet_type_id = ? AND operator_id = ? AND status = ?",
			round.LotteryTypeID, betType.ID, operatorID, "active").First(&rate).Error
		if err != nil {
			// fallback global rate
			h.DB.Where("lottery_type_id = ? AND bet_type_id = ? AND operator_id IS NULL AND status = ?",
				round.LotteryTypeID, betType.ID, "active").First(&rate)
		}

		// ⭐ เช็ค auto-ban rules — ขั้นบันได (จำกัดยอด/ลดเรท/อั้นเต็ม)
		effectiveRate := rate.Rate
		var autoBanRules []model.AutoBanRule
		h.DB.Where("lottery_type_id = ? AND bet_type = ? AND status = ?",
			round.LotteryTypeID, b.BetTypeCode, "active").
			Order("threshold_amount ASC").
			Find(&autoBanRules)

		if len(autoBanRules) > 0 {
			var totalForNumber float64
			h.DB.Model(&model.Bet{}).
				Where("lottery_round_id = ? AND bet_type_id = ? AND number = ? AND status != ?",
					round.ID, betType.ID, b.Number, "cancelled").
				Select("COALESCE(SUM(amount), 0)").Scan(&totalForNumber)

			totalAfterBet := totalForNumber + b.Amount
			autoBanned := false
			for _, rule := range autoBanRules {
				if totalAfterBet > rule.ThresholdAmount {
					switch rule.Action {
					case "full_ban":
						errors = append(errors, gin.H{"number": b.Number, "reason": "เลขอั้น"})
						autoBanned = true
					case "reduce_rate":
						if rule.ReducedRate > 0 { effectiveRate = rule.ReducedRate }
					case "max_amount":
						var personalTotal float64
						h.DB.Model(&model.Bet{}).
							Where("lottery_round_id = ? AND bet_type_id = ? AND number = ? AND member_id = ? AND status != ?",
								round.ID, betType.ID, b.Number, memberID, "cancelled").
							Select("COALESCE(SUM(amount), 0)").Scan(&personalTotal)
						if personalTotal+b.Amount > rule.ThresholdAmount {
							errors = append(errors, gin.H{"number": b.Number, "reason": "จำกัดยอด"})
							autoBanned = true
						}
					}
				}
			}
			if autoBanned { continue }
		}

		// เช็ค balance
		if member.Balance < b.Amount {
			errors = append(errors, gin.H{"number": b.Number, "reason": "insufficient balance"})
			break
		}

		// หักเงิน
		result := tx.Model(&model.Member{}).Where("id = ? AND balance >= ?", memberID, b.Amount).
			Update("balance", h.DB.Raw("balance - ?", b.Amount))
		if result.RowsAffected == 0 {
			errors = append(errors, gin.H{"number": b.Number, "reason": "debit failed"})
			break
		}
		member.Balance -= b.Amount

		// สร้าง bet (ใช้ effectiveRate ที่อาจถูกลดจาก auto-ban)
		bet := model.Bet{
			MemberID:       memberID,
			OperatorID:     operatorID,
			LotteryRoundID: b.LotteryRoundID,
			BetTypeID:      betType.ID,
			Number:         b.Number,
			Amount:         b.Amount,
			Rate:           effectiveRate,
			Status:         "pending",
			CreatedAt:      time.Now(),
		}
		tx.Create(&bet)

		successCount++
		totalAmount += b.Amount
	}

	tx.Commit()

	ok(c, gin.H{
		"success_count": successCount,
		"total_amount":  totalAmount,
		"balance_after": member.Balance,
		"errors":        errors,
	})
}

func (h *Handler) GameMyBets(c *gin.Context) {
	memberID := middleware.GetMemberID(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	var bets []model.Bet
	var total int64
	h.DB.Model(&model.Bet{}).Where("member_id = ?", memberID).Count(&total)
	h.DB.Where("member_id = ?", memberID).Preload("BetType").Preload("LotteryRound").
		Order("created_at DESC").Offset((page-1)*perPage).Limit(perPage).Find(&bets)

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": bets, "total": total, "page": page, "per_page": perPage}})
}

func (h *Handler) GameResults(c *gin.Context) {
	var rounds []model.LotteryRound
	h.DB.Where("status = ?", "resulted").Preload("LotteryType").
		Order("resulted_at DESC").Limit(20).Find(&rounds)
	ok(c, rounds)
}

func (h *Handler) GameHistory(c *gin.Context) {
	memberID := middleware.GetMemberID(c)
	var bets []model.Bet
	h.DB.Where("member_id = ? AND status IN ?", memberID, []string{"won", "lost"}).
		Preload("BetType").Preload("LotteryRound").
		Order("created_at DESC").Limit(50).Find(&bets)
	ok(c, bets)
}

func (h *Handler) GameBalance(c *gin.Context) {
	memberID := middleware.GetMemberID(c)
	var member model.Member
	if err := h.DB.First(&member, memberID).Error; err != nil { fail(c, 404, "member not found"); return }
	ok(c, gin.H{"balance": member.Balance})
}

// GameYeekeeWS — WebSocket endpoint สำหรับยี่กี
// ⭐ ใช้ Hub เดียวกับ standalone (#3) → ws/yeekee_hub.go
// ต่างแค่ auth: launch token แทน JWT
func (h *Handler) GameYeekeeWS(c *gin.Context) {
	// AIDEV-TODO(farri, 2026-04-21): integrate WebSocket Hub (ใช้ของ standalone #3)
	// 1. Parse launch token → member_id
	// 2. Upgrade to WebSocket
	// 3. Create/get Hub for this round
	// 4. Register client
	ok(c, gin.H{"message": "yeekee websocket — not yet integrated"})
}
