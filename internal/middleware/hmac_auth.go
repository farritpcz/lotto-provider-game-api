// Package middleware — hmac_auth.go
// HMAC authentication สำหรับ operator requests (คล้าย PG/JILI)
//
// ความสัมพันธ์:
// - ต่างจาก standalone (#3) ที่ใช้ JWT
// - operator ส่ง API Key + HMAC signature ทุก request
// - signature = HMAC-SHA256(request_body, secret_key)
//
// Flow:
//  1. Operator ส่ง header: X-API-Key + X-Signature + X-Timestamp
//  2. Middleware ดึง operator จาก API Key
//  3. ตรวจ IP whitelist
//  4. คำนวณ HMAC signature จาก body + timestamp + secret_key
//  5. เทียบ signature → ถ้าตรงก็ผ่าน
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HMACAuth middleware ตรวจสอบ HMAC signature จาก operator
//
// Headers ที่ต้องส่ง:
//   - X-API-Key:    API Key ของ operator
//   - X-Signature:  HMAC-SHA256 signature
//   - X-Timestamp:  Unix timestamp (ป้องกัน replay attack)
//
// TODO: implement จริง เมื่อมี operator repository
func HMACAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		// signature := c.GetHeader("X-Signature")
		// timestamp := c.GetHeader("X-Timestamp")

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "missing X-API-Key header"})
			c.Abort()
			return
		}

		// TODO: implement HMAC verification
		// 1. ดึง operator จาก API Key (operators table)
		// 2. ตรวจ IP whitelist
		// 3. ตรวจ timestamp (ไม่เก่ากว่า 5 นาที)
		// 4. คำนวณ expected_signature = HMAC-SHA256(body + timestamp, operator.secret_key)
		// 5. เทียบ expected_signature กับ signature ที่ส่งมา
		// 6. ถ้าตรง → c.Set("operator_id", operator.ID)

		c.Set("api_key", apiKey)
		c.Next()
	}
}

// LaunchTokenAuth middleware ตรวจสอบ launch token สำหรับ player ใน game client
//
// Flow:
//  1. Operator เรียก POST /api/v1/games/launch → ได้ game URL + token
//  2. Player เปิด URL → game client (#8) ส่ง token มา
//  3. middleware นี้ตรวจสอบ token → ได้ player_id + operator_id
//
// Token เก็บใน: query param ?token=xxx หรือ header Authorization: Bearer xxx
func LaunchTokenAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Query("token")
		if token == "" {
			token = c.GetHeader("X-Launch-Token")
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "missing launch token"})
			c.Abort()
			return
		}

		// TODO: parse + verify launch token (JWT-like)
		// claims: { player_id, operator_id, exp }
		// c.Set("player_id", claims.PlayerID)
		// c.Set("operator_id", claims.OperatorID)

		c.Next()
	}
}
