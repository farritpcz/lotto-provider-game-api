// Package middleware — launch_token.go
// สร้าง + ตรวจสอบ launch token สำหรับ game client
//
// ⭐ Launch token คือ JWT-like token ที่ operator ได้จาก GameLaunch API
// player เปิด game URL → game client (#8) ส่ง token มา → middleware ตรวจสอบ
//
// Token contains: player_id (member.ID), operator_id, exp
package middleware

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LaunchTokenClaims ข้อมูลใน launch token
type LaunchTokenClaims struct {
	MemberID           int64  `json:"member_id"`
	OperatorID         int64  `json:"operator_id"`
	ExternalPlayerID   string `json:"external_player_id"`
	jwt.RegisteredClaims
}

// GenerateLaunchToken สร้าง launch token
//
// เรียกจาก: handler/GameLaunch → สร้าง token → return URL + token ให้ operator
func GenerateLaunchToken(memberID int64, operatorID int64, externalPlayerID string, secret string, expiryMinutes int) (string, error) {
	claims := LaunchTokenClaims{
		MemberID:         memberID,
		OperatorID:       operatorID,
		ExternalPlayerID: externalPlayerID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expiryMinutes) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "lotto-provider-game-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseLaunchToken ตรวจสอบ + parse launch token
//
// เรียกจาก: middleware/LaunchTokenAuth → ได้ member_id + operator_id
func ParseLaunchToken(tokenString string, secret string) (*LaunchTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &LaunchTokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*LaunchTokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
