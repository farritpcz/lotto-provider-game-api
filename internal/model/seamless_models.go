// Package model — seamless_models.go
// Models สำหรับ seamless wallet idempotency + reconciliation
// DB: migration 002_seamless_idempotency.sql
package model

import "time"

// SeamlessTxnLog — idempotency store สำหรับ SeamlessDebit/Credit
//
// ⭐ ทุก txn_id ต้อง INSERT row นี้ก่อน call HTTP operator
// ถ้า INSERT fail (txn_id ซ้ำ) → return cached response (ไม่เรียก operator อีก)
//
// Ref: docs/rules/seamless_wallet.md §6
type SeamlessTxnLog struct {
	TxnID        string     `gorm:"column:txn_id;primaryKey;size:128" json:"txn_id"`
	OperatorID   int64      `gorm:"column:operator_id;not null;index" json:"operator_id"`
	Direction    string     `gorm:"column:direction;size:10;not null" json:"direction"` // debit / credit
	PlayerID     string     `gorm:"column:player_id;size:64;not null" json:"player_id"`
	Amount       float64    `gorm:"type:decimal(18,4);not null" json:"amount"`
	RoundID      string     `gorm:"column:round_id;size:64" json:"round_id"`
	Status       string     `gorm:"size:20;not null;default:pending" json:"status"` // pending / success / failed
	ResponseBody string     `gorm:"column:response_body;type:text" json:"response_body"`
	ErrorMessage string     `gorm:"column:error_message;size:500" json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

func (SeamlessTxnLog) TableName() string { return "seamless_txn_log" }

// SeamlessCreditRetry — queue สำหรับ credit ที่ fail (เข้า settlement แล้วแต่ยังไม่ push ไป operator)
//
// ⭐ cron job ดึงรายการ status=pending ที่ next_retry_at <= NOW() มา retry
// ถ้า retry_count >= max_retries → mark 'escalated' ให้แอดมินจัดการ
type SeamlessCreditRetry struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	TxnID        string     `gorm:"column:txn_id;size:128;uniqueIndex;not null" json:"txn_id"`
	OperatorID   int64      `gorm:"column:operator_id;not null" json:"operator_id"`
	PlayerID     string     `gorm:"column:player_id;size:64;not null" json:"player_id"`
	Amount       float64    `gorm:"type:decimal(18,4);not null" json:"amount"`
	RoundID      string     `gorm:"column:round_id;size:64" json:"round_id"`
	Description  string     `gorm:"size:255" json:"description"`
	RetryCount   int        `gorm:"column:retry_count;not null;default:0" json:"retry_count"`
	MaxRetries   int        `gorm:"column:max_retries;not null;default:10" json:"max_retries"`
	Status       string     `gorm:"size:20;not null;default:pending" json:"status"` // pending / success / escalated
	LastError    string     `gorm:"column:last_error;size:500" json:"last_error"`
	NextRetryAt  time.Time  `gorm:"column:next_retry_at;not null" json:"next_retry_at"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

func (SeamlessCreditRetry) TableName() string { return "seamless_credit_retries" }
