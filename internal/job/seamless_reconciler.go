// Package job — seamless_reconciler.go
//
// Reconciliation job — ลอง SeamlessCredit ซ้ำ สำหรับ txn ที่ fail ตอน settle
//
// Flow:
//  1. ทุก 30 วินาที — ดึง seamless_credit_retries ที่ status=pending AND next_retry_at <= NOW()
//  2. ดึง operator (api/callback URL)
//  3. Call SeamlessCredit ผ่าน WalletService (มี idempotency store protection)
//  4. ถ้า success → status=success + completed_at
//  5. ถ้า fail → retry_count++, next_retry_at = exponential backoff
//  6. ถ้า retry_count >= max_retries → status=escalated (admin ต้องจัดการ)
//
// ⭐ ระบบ idempotent: WalletService.SeamlessCredit เช็ค seamless_txn_log ก่อน
//    → ไม่มีการจ่ายซ้ำแม้ retry หลายครั้ง
package job

import (
	"log"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/farritpcz/lotto-provider-game-api/internal/model"
	"github.com/farritpcz/lotto-provider-game-api/internal/service"
)

const (
	// Base backoff — doubled each retry (30s, 60s, 120s, 240s, ..., cap 1 hour)
	reconcileBaseBackoff = 30 * time.Second
	reconcileMaxBackoff  = 1 * time.Hour
)

// StartSeamlessReconciler รัน background goroutine ทุก 30 วินาที
// → retry SeamlessCredit ที่ fail ตอน settle
func StartSeamlessReconciler(db *gorm.DB) {
	log.Println("🔁 Seamless reconciler started (every 30s)")

	wallet := service.NewWalletServiceWithDB(db) // ⭐ ต้องมี db → เปิด idempotency

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		// รันครั้งแรกทันที (ไม่ต้องรอ 30s)
		processReconcileQueue(db, wallet)

		for range ticker.C {
			processReconcileQueue(db, wallet)
		}
	}()
}

// processReconcileQueue ดึง pending retries → ลอง credit ใหม่
func processReconcileQueue(db *gorm.DB, wallet *service.WalletService) {
	now := time.Now()

	var retries []model.SeamlessCreditRetry
	db.Where("status = ? AND next_retry_at <= ?", "pending", now).
		Order("next_retry_at ASC").
		Limit(50).
		Find(&retries)

	if len(retries) == 0 {
		return
	}

	log.Printf("🔁 [reconciler] processing %d pending retries", len(retries))

	for _, r := range retries {
		tryReconcile(db, wallet, r)
	}
}

// tryReconcile ลอง SeamlessCredit ครั้งนึง + update queue row
func tryReconcile(db *gorm.DB, wallet *service.WalletService, r model.SeamlessCreditRetry) {
	// 1. ดึง operator
	var op model.Operator
	if err := db.First(&op, r.OperatorID).Error; err != nil {
		log.Printf("⚠️ [reconciler] operator %d not found for retry txn=%s", r.OperatorID, r.TxnID)
		markRetryFailed(db, r, "operator not found", true)
		return
	}
	if op.Status != "active" {
		log.Printf("⚠️ [reconciler] operator %d suspended — escalating txn=%s", r.OperatorID, r.TxnID)
		markRetryFailed(db, r, "operator suspended", true)
		return
	}

	// 2. Call SeamlessCredit (idempotent — จะ return cached ถ้าเคย success)
	req := service.SeamlessCreditRequest{
		PlayerID:    r.PlayerID,
		Amount:      r.Amount,
		Currency:    "THB",
		TxnID:       r.TxnID,
		RoundID:     r.RoundID,
		Description: r.Description,
	}
	_, err := wallet.SeamlessCredit(op.CallbackURL, op.SecretKey, req, r.OperatorID)
	if err == nil {
		// Success
		now := time.Now()
		db.Model(&r).Updates(map[string]interface{}{
			"status":       "success",
			"completed_at": &now,
			"retry_count":  gorm.Expr("retry_count + 1"),
		})
		log.Printf("✅ [reconciler] txn=%s credited after %d retries", r.TxnID, r.RetryCount+1)
		return
	}

	// Fail
	markRetryFailed(db, r, err.Error(), false)
}

// markRetryFailed อัพเดท row: เพิ่ม retry_count + backoff, หรือ escalate ถ้าเกิน
func markRetryFailed(db *gorm.DB, r model.SeamlessCreditRetry, errMsg string, forceEscalate bool) {
	newCount := r.RetryCount + 1

	if forceEscalate || newCount >= r.MaxRetries {
		db.Model(&r).Updates(map[string]interface{}{
			"status":      "escalated",
			"last_error":  errMsg,
			"retry_count": newCount,
		})
		log.Printf("🚨 [reconciler] txn=%s ESCALATED after %d retries: %s", r.TxnID, newCount, errMsg)
		return
	}

	// Exponential backoff: 30s, 60s, 120s, 240s, ..., cap 1 hour
	backoff := time.Duration(math.Pow(2, float64(newCount))) * reconcileBaseBackoff
	if backoff > reconcileMaxBackoff {
		backoff = reconcileMaxBackoff
	}
	db.Model(&r).Updates(map[string]interface{}{
		"retry_count":   newCount,
		"last_error":    errMsg,
		"next_retry_at": time.Now().Add(backoff),
	})
	log.Printf("🔄 [reconciler] txn=%s retry %d/%d failed, next in %v: %s",
		r.TxnID, newCount, r.MaxRetries, backoff, errMsg)
}
