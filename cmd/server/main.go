// Package main — entry point ของ lotto-provider-game-api
//
// Repo: #7 lotto-provider-game-api
// คู่กับ: #8 lotto-provider-game-web (game client iframe)
// Share DB กับ: #9 lotto-provider-backoffice-api
// Import: #2 lotto-core
//
// Port: 9080
package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/farritpcz/lotto-provider-game-api/internal/config"
	"github.com/farritpcz/lotto-provider-game-api/internal/handler"
)

func main() {
	cfg := config.Load()
	if cfg.Env == "production" { gin.SetMode(gin.ReleaseMode) }

	r := gin.Default()
	h := handler.NewHandler(cfg.LaunchTokenSecret)
	h.SetupRoutes(r)

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🎰 lotto-provider-game-api starting on %s (env: %s)", addr, cfg.Env)
	log.Printf("📡 Operator API: http://localhost:%s/api/v1", cfg.Port)
	log.Printf("🎮 Game Client API: http://localhost:%s/api/v1/game", cfg.Port)

	if err := r.Run(addr); err != nil { log.Fatal("failed to start server:", err) }
}
