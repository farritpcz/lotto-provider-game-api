// Package middleware — hmac_auth.go + launch_token_auth.go
// Authentication middleware สำหรับ provider-game-api (#7)
//
// 2 แบบ:
// 1. HMACAuth — สำหรับ operator API (server-to-server)
// 2. LaunchTokenAuth — สำหรับ game client API (player ใน iframe)
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// =============================================================================
// HMAC Auth — Operator API
// =============================================================================

// Operator model (minimal สำหรับ middleware — ไม่ import model package เพื่อหลีกเลี่ยง circular)
type operatorInfo struct {
	ID          int64
	APIKey      string
	SecretKey   string
	IPWhitelist string
	Status      string
}

// HMACAuthWithDB middleware ตรวจสอบ HMAC signature + IP whitelist
//
// Headers ที่ operator ต้องส่ง:
//   - X-API-Key:   API Key ของ operator
//   - X-Signature: HMAC-SHA256(body + timestamp, secret_key)
//   - X-Timestamp: Unix timestamp (ต้องไม่เก่ากว่า 5 นาที)
//
// ⭐ ต่างจาก standalone (#3) ที่ใช้ JWT
func HMACAuthWithDB(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		signature := c.GetHeader("X-Signature")
		timestamp := c.GetHeader("X-Timestamp")

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "missing X-API-Key"})
			c.Abort()
			return
		}

		// 1. ดึง operator จาก API Key
		var op operatorInfo
		err := db.Table("operators").
			Select("id, api_key, secret_key, ip_whitelist, status").
			Where("api_key = ?", apiKey).First(&op).Error
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "invalid API key"})
			c.Abort()
			return
		}

		// 2. เช็ค operator status
		if op.Status != "active" {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "operator suspended"})
			c.Abort()
			return
		}

		// 3. เช็ค IP whitelist (ถ้ามี)
		if op.IPWhitelist != "" {
			clientIP := c.ClientIP()
			allowed := false
			for _, ip := range strings.Split(op.IPWhitelist, ",") {
				if strings.TrimSpace(ip) == clientIP {
					allowed = true
					break
				}
			}
			if !allowed {
				c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "IP not whitelisted: " + clientIP})
				c.Abort()
				return
			}
		}

		// 4. ตรวจ timestamp (ไม่เก่ากว่า 5 นาที — ป้องกัน replay attack)
		if timestamp != "" && signature != "" {
			ts, err := strconv.ParseInt(timestamp, 10, 64)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid timestamp"})
				c.Abort()
				return
			}
			diff := math.Abs(float64(time.Now().Unix() - ts))
			if diff > 300 { // 5 นาที
				c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "timestamp expired"})
				c.Abort()
				return
			}

			// 5. ตรวจ HMAC signature
			body, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(strings.NewReader(string(body))) // put body back

			signData := string(body) + timestamp
			mac := hmac.New(sha256.New, []byte(op.SecretKey))
			mac.Write([]byte(signData))
			expectedSig := hex.EncodeToString(mac.Sum(nil))

			if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
				c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "invalid signature"})
				c.Abort()
				return
			}

			// ⭐ 6. Replay protection — ถ้าเห็น signature นี้แล้วใน 5 นาที → reject
			//    (นอกเหนือจาก timestamp window — ป้องกัน sniff + replay ภายใน window)
			if !CheckNonce(apiKey, signature, timestamp) {
				c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "replay detected (signature already used)"})
				c.Abort()
				return
			}
		}

		// Set operator info ใน context ให้ handler ใช้
		c.Set("operator_id", op.ID)
		c.Set("api_key", apiKey)
		c.Next()
	}
}

// GetOperatorID ดึง operator ID จาก context (ใช้ใน handler)
func GetOperatorID(c *gin.Context) int64 {
	if id, exists := c.Get("operator_id"); exists {
		return id.(int64)
	}
	return 0
}

// =============================================================================
// Launch Token Auth — Game Client API
// =============================================================================

// LaunchTokenAuthWithSecret middleware ตรวจ launch token จริง (JWT)
//
// Token มาจาก: query ?token=xxx หรือ header X-Launch-Token
// Parse ด้วย: ParseLaunchToken() จาก launch_token.go
func LaunchTokenAuthWithSecret(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ดึง token จาก query param หรือ header
		token := c.Query("token")
		if token == "" {
			token = c.GetHeader("X-Launch-Token")
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "missing launch token"})
			c.Abort()
			return
		}

		// Parse + verify token
		claims, err := ParseLaunchToken(token, secret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": fmt.Sprintf("invalid token: %v", err)})
			c.Abort()
			return
		}

		// Set claims ใน context ให้ handler ใช้
		c.Set("member_id", claims.MemberID)
		c.Set("operator_id", claims.OperatorID)
		c.Set("external_player_id", claims.ExternalPlayerID)
		c.Next()
	}
}

// GetMemberID ดึง member ID จาก context (ใช้ใน game client handler)
func GetMemberID(c *gin.Context) int64 {
	if id, exists := c.Get("member_id"); exists {
		return id.(int64)
	}
	return 0
}
