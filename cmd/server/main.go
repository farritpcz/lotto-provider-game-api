// Package main — entry point ของ lotto-provider-game-api
//
// Repo: #7 lotto-provider-game-api
// คู่กับ: #8 lotto-provider-game-web (game client iframe)
// Share DB กับ: #9 lotto-provider-backoffice-api (DB: lotto_provider)
// Import: #2 lotto-core
//
// Port: 9080
package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/farritpcz/lotto-provider-game-api/internal/config"
	"github.com/farritpcz/lotto-provider-game-api/internal/handler"
	"github.com/farritpcz/lotto-provider-game-api/internal/job"
	"github.com/farritpcz/lotto-provider-game-api/internal/service"
	"github.com/farritpcz/lotto-provider-game-api/internal/ws"
)

func main() {
	cfg := config.Load()
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// =================================================================
	// เชื่อมต่อ MySQL — ⭐ share DB "lotto_provider" กับ backoffice-api (#9)
	// =================================================================
	gormConfig := &gorm.Config{}
	if cfg.Env != "production" {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN()), gormConfig)
	if err != nil {
		log.Fatal("❌ Failed to connect to MySQL:", err)
	}
	log.Println("✅ Connected to MySQL:", cfg.DBName)

	// =================================================================
	// สร้าง Router + Handler + dependencies
	// =================================================================
	r := gin.Default()

	hubManager := ws.NewHubManager()
	yeekeeService := service.NewYeekeeService(db)

	h := handler.NewHandler(cfg.LaunchTokenSecret)
	h.DB = db
	h.HubManager = hubManager
	h.YeekeeService = yeekeeService
	h.AllowedOrigins = cfg.AllowedOrigins
	h.SetupRoutes(r)

	// =================================================================
	// Background jobs — yeekee cron (สร้างรอบ + ปิดรอบ + settle)
	// =================================================================
	job.StartYeekeeCron(db)

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🎰 lotto-provider-game-api starting on %s (env: %s)", addr, cfg.Env)
	log.Printf("📡 Operator API: http://localhost:%s/api/v1", cfg.Port)
	log.Printf("🎮 Game Client API: http://localhost:%s/api/v1/game", cfg.Port)

	if err := r.Run(addr); err != nil {
		log.Fatal("failed to start server:", err)
	}
}
