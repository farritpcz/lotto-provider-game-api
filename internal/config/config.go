// Package config จัดการ configuration ของ provider-game-api
//
// ความสัมพันธ์:
// - repo นี้ (#7 lotto-provider-game-api) เป็น Core API สำหรับ operator + game client
// - คู่กับ: #8 lotto-provider-game-web (game client iframe)
// - share DB กับ: #9 lotto-provider-backoffice-api (admin + operator dashboard API)
// - import: lotto-core (#2) สำหรับ business logic
//
// ต่างจาก standalone (#3):
// - ใช้ HMAC + API Key auth (ไม่ใช่ JWT) สำหรับ operator
// - ใช้ launch token สำหรับ player ใน game client
// - wallet เรียก API ของ operator (seamless/transfer)
// - มี callback แจ้ง operator
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port string
	Env  string

	// Database (MySQL) — share กับ backoffice-api (#9)
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Redis
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Launch Token — สำหรับ player auth ใน game client (#8)
	LaunchTokenSecret    string
	LaunchTokenExpiryMin int // อายุ token (นาที)

	// WebSocket allowed origins (comma-separated env: ALLOWED_ORIGINS)
	AllowedOrigins []string
}

func Load() *Config {
	return &Config{
		Port: getEnv("PORT", "9080"),
		Env:  getEnv("ENV", "development"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", "password"),
		DBName:     getEnv("DB_NAME", "lotto_provider"), // DB แยกจาก standalone

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		LaunchTokenSecret:    getEnv("LAUNCH_TOKEN_SECRET", "launch-secret-change-in-production"),
		LaunchTokenExpiryMin: getEnvInt("LAUNCH_TOKEN_EXPIRY_MIN", 60),

		AllowedOrigins: splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:3002")),
	}
}

// splitCSV แยก comma-separated string + trim whitespace + ตัดค่าว่าง
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%s", c.RedisHost, c.RedisPort)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" { return val }
	return defaultVal
}
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil { return i }
	}
	return defaultVal
}
