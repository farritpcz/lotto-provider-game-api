// Package middleware — rate_limit.go
// Rate limiting per operator (in-memory token bucket)
//
// ⭐ จำกัดจำนวน request ต่อ operator ต่อวินาที
// ป้องกัน operator ส่ง request มากเกินไปจนกระทบ performance
//
// Production ควรใช้ Redis-based rate limiter แทน in-memory
// เพราะ in-memory ไม่ share ระหว่าง instances (load balancer)
package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// tokenBucket ใช้ token bucket algorithm
// แต่ละ operator มี bucket ของตัวเอง
type tokenBucket struct {
	tokens     float64   // จำนวน tokens ปัจจุบัน
	maxTokens  float64   // จำนวน tokens สูงสุด (burst size)
	refillRate float64   // จำนวน tokens ที่เติมต่อวินาที
	lastRefill time.Time // เวลาที่เติมล่าสุด
}

// allow ตรวจว่ามี token พอให้ใช้ไหม
func (b *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()

	// เติม tokens ตามเวลาที่ผ่านไป
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	// ใช้ 1 token
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimiter จัดการ rate limit per operator
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[int64]*tokenBucket // key: operator_id
	rate    float64                // requests per second
	burst   float64                // max burst
}

// NewRateLimiter สร้าง RateLimiter
//
// Parameters:
//   - ratePerSecond: จำนวน requests ที่อนุญาตต่อวินาที (เช่น 100)
//   - burstSize: จำนวน requests ที่อนุญาต burst (เช่น 200)
func NewRateLimiter(ratePerSecond float64, burstSize float64) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[int64]*tokenBucket),
		rate:    ratePerSecond,
		burst:   burstSize,
	}
}

// Allow ตรวจว่า operator นี้ยังส่ง request ได้ไหม
func (rl *RateLimiter) Allow(operatorID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[operatorID]
	if !exists {
		bucket = &tokenBucket{
			tokens:     rl.burst,
			maxTokens:  rl.burst,
			refillRate: rl.rate,
			lastRefill: time.Now(),
		}
		rl.buckets[operatorID] = bucket
	}

	return bucket.allow()
}

// RateLimitMiddleware Gin middleware สำหรับ rate limiting per operator
//
// ใช้หลัง HMACAuthWithDB — ต้องมี operator_id ใน context แล้ว
//
// ตัวอย่าง:
//
//	limiter := NewRateLimiter(100, 200) // 100 req/s, burst 200
//	operator.Use(HMACAuthWithDB(db))
//	operator.Use(RateLimitMiddleware(limiter))
func RateLimitMiddleware(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		operatorID := GetOperatorID(c)
		if operatorID == 0 {
			c.Next()
			return
		}

		if !limiter.Allow(operatorID) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error":   "rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
