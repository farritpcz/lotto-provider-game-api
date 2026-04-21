-- =============================================================================
-- Migration 002 — Seamless Wallet: Idempotency + Reconciliation
-- =============================================================================
-- Purpose:
--   1. seamless_txn_log     → idempotency store (txn_id PRIMARY KEY) ป้องกัน double-debit/credit
--   2. seamless_credit_retries → queue สำหรับ credit ที่ fail + cron retry
--
-- อ้างอิง: docs/rules/seamless_wallet.md §6
-- =============================================================================

SET NAMES utf8mb4;
SET CHARACTER SET utf8mb4;

-- =============================================================================
-- SEAMLESS_TXN_LOG — idempotency store
-- =============================================================================
-- ทุกการเรียก SeamlessDebit/Credit ต้อง INSERT row นี้ก่อน call HTTP
-- ถ้า INSERT fail เพราะ txn_id ซ้ำ → return cached result (ไม่เรียก operator อีก)
--
-- ใช้โดย: provider-game-api service/wallet_service.go
CREATE TABLE IF NOT EXISTS `seamless_txn_log` (
  `txn_id` VARCHAR(128) NOT NULL PRIMARY KEY COMMENT 'unique transaction ID (ซ้ำไม่ได้)',
  `operator_id` BIGINT NOT NULL COMMENT 'FK → operators.id',
  `direction` VARCHAR(10) NOT NULL COMMENT 'debit หรือ credit',
  `player_id` VARCHAR(64) NOT NULL COMMENT 'player_id ฝั่ง operator',
  `amount` DECIMAL(18,4) NOT NULL COMMENT 'จำนวนเงิน (+)',
  `round_id` VARCHAR(64) DEFAULT NULL COMMENT 'lottery round ID ถ้าเกี่ยว',
  `status` VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT 'pending, success, failed',
  `response_body` TEXT DEFAULT NULL COMMENT 'cached response จาก operator (สำหรับ return ตอน replay)',
  `error_message` VARCHAR(500) DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `completed_at` DATETIME DEFAULT NULL,
  INDEX `idx_txn_operator` (`operator_id`, `created_at`),
  INDEX `idx_txn_status` (`status`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Idempotency store สำหรับ seamless wallet txn (ซ้ำกันไม่ได้)';

-- =============================================================================
-- SEAMLESS_CREDIT_RETRIES — queue สำหรับ credit ที่ fail
-- =============================================================================
-- ⭐ ตอน settle → ถ้า SeamlessCredit() fail → INSERT queue + cron ลอง retry
-- ถ้า retry เกิน N ครั้ง → mark 'escalated' ให้แอดมินจัดการเอง
--
-- ใช้โดย: provider-game-api job/seamless_reconciler.go
CREATE TABLE IF NOT EXISTS `seamless_credit_retries` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `txn_id` VARCHAR(128) NOT NULL UNIQUE COMMENT 'unique transaction ID ต่อเนื่องจาก seamless_txn_log',
  `operator_id` BIGINT NOT NULL,
  `player_id` VARCHAR(64) NOT NULL,
  `amount` DECIMAL(18,4) NOT NULL,
  `round_id` VARCHAR(64) DEFAULT NULL,
  `description` VARCHAR(255) DEFAULT NULL,
  `retry_count` INT NOT NULL DEFAULT 0,
  `max_retries` INT NOT NULL DEFAULT 10 COMMENT 'เกินนี้ → escalate ให้แอดมินจัดการ',
  `status` VARCHAR(20) NOT NULL DEFAULT 'pending' COMMENT 'pending, success, escalated',
  `last_error` VARCHAR(500) DEFAULT NULL,
  `next_retry_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `completed_at` DATETIME DEFAULT NULL,
  INDEX `idx_retry_next` (`status`, `next_retry_at`),
  INDEX `idx_retry_operator` (`operator_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Queue สำหรับ SeamlessCredit ที่ fail → cron retry จนสำเร็จหรือ escalate';
