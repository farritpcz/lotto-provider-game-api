// Package handler — router.go
// Provider Game API routes (คล้าย PG/JILI API)
//
// ความสัมพันธ์:
// - repo #7 (provider game API) — Core API สำหรับ operator + game client
// - คู่กับ #8 (game client iframe)
// - share DB กับ #9 (backoffice API)
//
// มี 2 กลุ่ม routes:
// 1. Operator API (HMAC auth) — operator เรียกจาก server ของตัวเอง
// 2. Game Client API (launch token auth) — player เล่นผ่าน iframe (#8)
//
// Provider API Design (คล้าย PG/JILI):
//
//	=== Operator API (HMAC Auth) ===
//	POST   /api/v1/wallet/balance          → Seamless: ดึงยอด
//	POST   /api/v1/wallet/debit            → Seamless: หักเงิน
//	POST   /api/v1/wallet/credit           → Seamless: เติมเงิน
//	POST   /api/v1/wallet/deposit          → Transfer: โอนเงินเข้า
//	POST   /api/v1/wallet/withdraw         → Transfer: โอนเงินออก
//	GET    /api/v1/games                   → รายการหวยทั้งหมด
//	GET    /api/v1/games/:id/rounds        → รอบที่เปิดรับ
//	POST   /api/v1/games/launch            → สร้าง game URL (iframe)
//	GET    /api/v1/results                 → ดูผลรางวัล
//	GET    /api/v1/reports/bets            → รายงานการเดิมพัน
//
//	=== Game Client API (Launch Token Auth) ===
//	GET    /api/v1/game/lobby              → รายการหวย (สำหรับ game client)
//	GET    /api/v1/game/rounds/:typeId     → รอบที่เปิด
//	POST   /api/v1/game/bets              → วางเดิมพัน
//	GET    /api/v1/game/bets              → ดู bets ของฉัน
//	GET    /api/v1/game/results           → ตรวจผล
//	GET    /api/v1/game/history           → ประวัติ
//	GET    /api/v1/game/balance           → ดูยอด
//	WS     /api/v1/game/yeekee/ws/:roundId → WebSocket ยี่กี
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/farritpcz/lotto-provider-game-api/internal/middleware"
)

type Handler struct {
	LaunchTokenSecret string
	DB                *gorm.DB // inject จาก main.go
}


func NewHandler(launchTokenSecret string) *Handler {
	return &Handler{LaunchTokenSecret: launchTokenSecret}
}

func (h *Handler) SetupRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	{
		// === Operator API (HMAC Auth) ===
		// Operator เรียกจาก server ของตัวเอง
		// ⭐ Operator API security chain:
		// 1. HMACAuthWithDB: API key + signature + IP whitelist
		// 2. RateLimitMiddleware: 100 req/s per operator, burst 200
		limiter := middleware.NewRateLimiter(100, 200)
		operator := api.Group("")
		operator.Use(middleware.HMACAuthWithDB(h.DB))
		operator.Use(middleware.RateLimitMiddleware(limiter))
		{
			// Wallet — Seamless
			operator.POST("/wallet/balance", h.SeamlessBalance)
			operator.POST("/wallet/debit", h.SeamlessDebit)
			operator.POST("/wallet/credit", h.SeamlessCredit)

			// Wallet — Transfer
			operator.POST("/wallet/deposit", h.TransferDeposit)
			operator.POST("/wallet/withdraw", h.TransferWithdraw)

			// Games
			operator.GET("/games", h.ListGames)
			operator.GET("/games/:id/rounds", h.ListGameRounds)
			operator.POST("/games/launch", h.GameLaunch) // ⭐ สร้าง game URL + token

			// Results & Reports
			operator.GET("/results", h.GetResults)
			operator.GET("/reports/bets", h.GetBetReport)
		}

		// === Game Client API (Launch Token Auth) ===
		// Player เล่นผ่าน game client iframe (#8)
		// ⭐ ใช้ LaunchTokenAuthWithSecret — parse JWT token จริง → ได้ member_id + operator_id
		game := api.Group("/game")
		game.Use(middleware.LaunchTokenAuthWithSecret(h.LaunchTokenSecret))
		{
			game.GET("/lobby", h.GameLobby)
			game.GET("/rounds/:typeId", h.GameRounds)
			game.POST("/bets", h.GamePlaceBets)      // ⭐ แทงหวย (ใช้ lotto-core เหมือน standalone)
			game.GET("/bets", h.GameMyBets)
			game.GET("/results", h.GameResults)
			game.GET("/history", h.GameHistory)
			game.GET("/balance", h.GameBalance)
			game.GET("/yeekee/ws/:roundId", h.GameYeekeeWS) // WebSocket ยี่กี
		}
	}

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "lotto-provider-game-api"})
	})
}
