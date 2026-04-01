-- =============================================================================
-- Lotto Provider — Initial Database Schema
-- =============================================================================
-- DB Name: lotto_provider
-- ใช้ร่วมโดย: provider-game-api (#7) + provider-backoffice-api (#9)
--
-- ⭐ ต่างจาก standalone (lotto_standalone):
--   1. มี operators table — จัดการ operator ที่มาเชื่อมต่อ
--   2. members มี operator_id — รู้ว่า player มาจาก operator ไหน
--   3. bets มี operator_id — แยก bet ตาม operator
--   4. number_bans มี operator_id — อั้นเลขแยก per operator
--   5. pay_rates มี operator_id — rate แยก per operator
--   6. เพิ่ม wallet_transactions — สำหรับ transfer wallet mode
--
-- Tables ที่เหมือนกับ standalone:
--   lottery_types, bet_types, lottery_rounds, yeekee_rounds,
--   yeekee_shoots, transactions, settings
--   (โครงสร้างเหมือนกัน ข้อมูลแยกกัน)
-- =============================================================================

SET NAMES utf8mb4;
SET CHARACTER SET utf8mb4;

-- =============================================================================
-- OPERATORS — operator ที่มาเชื่อมต่อ API (⭐ ไม่มีใน standalone)
-- =============================================================================
-- จัดการโดย: backoffice-api (#9) admin endpoints
-- อ่านโดย: game-api (#7) ตอน HMAC auth + wallet integration
CREATE TABLE IF NOT EXISTS `operators` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(100) NOT NULL COMMENT 'ชื่อ operator',
  `code` VARCHAR(30) NOT NULL UNIQUE COMMENT 'code สั้นๆ เช่น "WINNER88"',
  `api_key` VARCHAR(64) NOT NULL UNIQUE COMMENT 'API Key สำหรับ HMAC auth',
  `secret_key` VARCHAR(128) NOT NULL COMMENT 'Secret Key สำหรับ HMAC signing',
  `callback_url` VARCHAR(500) DEFAULT NULL COMMENT 'URL ที่ provider จะ callback ไป (wallet, bet-result)',
  `wallet_type` VARCHAR(20) NOT NULL DEFAULT 'seamless' COMMENT 'seamless หรือ transfer — ตรงกับ lotto-core WalletType',
  `ip_whitelist` TEXT DEFAULT NULL COMMENT 'comma-separated IP list ที่อนุญาต',
  `username` VARCHAR(50) DEFAULT NULL COMMENT 'username สำหรับ login เข้า operator dashboard (#11)',
  `password_hash` VARCHAR(255) DEFAULT NULL COMMENT 'bcrypt hash สำหรับ operator dashboard login',
  `status` VARCHAR(20) NOT NULL DEFAULT 'active' COMMENT 'active, suspended',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX `idx_operators_api_key` (`api_key`),
  INDEX `idx_operators_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Operators ที่เชื่อมต่อ API — ⭐ ไม่มีใน standalone';

-- =============================================================================
-- ADMINS — ผู้ดูแลระบบ provider
-- =============================================================================
CREATE TABLE IF NOT EXISTS `admins` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `username` VARCHAR(50) NOT NULL UNIQUE,
  `password_hash` VARCHAR(255) NOT NULL,
  `name` VARCHAR(100) DEFAULT NULL,
  `role` VARCHAR(20) NOT NULL DEFAULT 'admin',
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `last_login_at` DATETIME DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- MEMBERS — สมาชิก (player จาก operator)
-- =============================================================================
-- ⭐ ต่างจาก standalone: มี operator_id + external_player_id
CREATE TABLE IF NOT EXISTS `members` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `operator_id` BIGINT NOT NULL COMMENT 'FK → operators — player มาจาก operator ไหน',
  `external_player_id` VARCHAR(100) NOT NULL COMMENT 'ID ผู้เล่นฝั่ง operator (ใช้สำหรับ seamless wallet)',
  `username` VARCHAR(100) DEFAULT NULL COMMENT 'ชื่อแสดงผล',
  `balance` DECIMAL(15,2) NOT NULL DEFAULT 0.00 COMMENT 'balance ใน provider (ใช้สำหรับ transfer wallet)',
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  UNIQUE KEY `uk_member_operator` (`operator_id`, `external_player_id`),
  INDEX `idx_members_operator` (`operator_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Players จาก operators — มี external_player_id สำหรับ wallet API';

-- =============================================================================
-- LOTTERY_TYPES — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `lottery_types` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(100) NOT NULL,
  `code` VARCHAR(30) NOT NULL UNIQUE,
  `category` VARCHAR(30) NOT NULL DEFAULT 'government',
  `description` TEXT DEFAULT NULL,
  `icon` VARCHAR(50) DEFAULT NULL,
  `is_auto_result` TINYINT(1) NOT NULL DEFAULT 0,
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- BET_TYPES — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `bet_types` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `name` VARCHAR(50) NOT NULL,
  `code` VARCHAR(20) NOT NULL UNIQUE,
  `digit_count` INT NOT NULL,
  `description` TEXT DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- LOTTERY_ROUNDS — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `lottery_rounds` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `lottery_type_id` BIGINT NOT NULL,
  `round_number` VARCHAR(50) NOT NULL,
  `round_date` DATE NOT NULL,
  `open_time` DATETIME NOT NULL,
  `close_time` DATETIME NOT NULL,
  `status` VARCHAR(20) NOT NULL DEFAULT 'upcoming',
  `result_top3` VARCHAR(3) DEFAULT NULL,
  `result_top2` VARCHAR(2) DEFAULT NULL,
  `result_bottom2` VARCHAR(2) DEFAULT NULL,
  `resulted_at` DATETIME DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (`lottery_type_id`) REFERENCES `lottery_types`(`id`),
  INDEX `idx_rounds_type_status` (`lottery_type_id`, `status`),
  UNIQUE KEY `uk_rounds_type_number` (`lottery_type_id`, `round_number`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- PAY_RATES — ⭐ มี operator_id เพิ่มจาก standalone
-- =============================================================================
-- operator_id = NULL → rate กลาง (admin ตั้ง)
-- operator_id = X → rate ของ operator X (override rate กลาง)
CREATE TABLE IF NOT EXISTS `pay_rates` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `lottery_type_id` BIGINT NOT NULL,
  `bet_type_id` BIGINT NOT NULL,
  `operator_id` BIGINT DEFAULT NULL COMMENT 'NULL = rate กลาง, มีค่า = rate per operator',
  `rate` DECIMAL(10,2) NOT NULL,
  `max_bet_per_number` DECIMAL(15,2) NOT NULL DEFAULT 0.00,
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (`lottery_type_id`) REFERENCES `lottery_types`(`id`),
  FOREIGN KEY (`bet_type_id`) REFERENCES `bet_types`(`id`),
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  UNIQUE KEY `uk_pay_rates` (`lottery_type_id`, `bet_type_id`, `operator_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='อัตราจ่าย — operator_id NULL=global, มีค่า=per operator';

-- =============================================================================
-- BETS — ⭐ มี operator_id เพิ่มจาก standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `bets` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `member_id` BIGINT NOT NULL,
  `operator_id` BIGINT NOT NULL COMMENT 'FK → operators — bet มาจาก operator ไหน',
  `lottery_round_id` BIGINT NOT NULL,
  `bet_type_id` BIGINT NOT NULL,
  `number` VARCHAR(10) NOT NULL,
  `amount` DECIMAL(15,2) NOT NULL,
  `rate` DECIMAL(10,2) NOT NULL,
  `status` VARCHAR(20) NOT NULL DEFAULT 'pending',
  `win_amount` DECIMAL(15,2) NOT NULL DEFAULT 0.00,
  `settled_at` DATETIME DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`member_id`) REFERENCES `members`(`id`),
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  FOREIGN KEY (`lottery_round_id`) REFERENCES `lottery_rounds`(`id`),
  FOREIGN KEY (`bet_type_id`) REFERENCES `bet_types`(`id`),
  INDEX `idx_bets_member` (`member_id`, `created_at` DESC),
  INDEX `idx_bets_operator` (`operator_id`, `created_at` DESC),
  INDEX `idx_bets_round` (`lottery_round_id`, `status`),
  INDEX `idx_bets_number` (`lottery_round_id`, `bet_type_id`, `number`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- NUMBER_BANS — ⭐ มี operator_id เพิ่มจาก standalone
-- =============================================================================
-- operator_id = NULL → อั้น global (admin ตั้ง → มีผลทุก operator)
-- operator_id = X → อั้นเฉพาะ operator X (operator ตั้งเอง)
-- ตรงกับ lotto-core/numberban.FilterBansForOperator()
CREATE TABLE IF NOT EXISTS `number_bans` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `lottery_type_id` BIGINT NOT NULL,
  `lottery_round_id` BIGINT DEFAULT NULL,
  `operator_id` BIGINT DEFAULT NULL COMMENT 'NULL = global ban, มีค่า = per operator',
  `bet_type_id` BIGINT NOT NULL,
  `number` VARCHAR(10) NOT NULL,
  `ban_type` VARCHAR(20) NOT NULL DEFAULT 'full_ban',
  `reduced_rate` DECIMAL(10,2) NOT NULL DEFAULT 0.00,
  `max_amount` DECIMAL(15,2) NOT NULL DEFAULT 0.00,
  `status` VARCHAR(20) NOT NULL DEFAULT 'active',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`lottery_type_id`) REFERENCES `lottery_types`(`id`),
  FOREIGN KEY (`lottery_round_id`) REFERENCES `lottery_rounds`(`id`),
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  FOREIGN KEY (`bet_type_id`) REFERENCES `bet_types`(`id`),
  INDEX `idx_bans_lookup` (`lottery_type_id`, `bet_type_id`, `number`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='เลขอั้น — operator_id NULL=global, มีค่า=per operator';

-- =============================================================================
-- YEEKEE_ROUNDS + YEEKEE_SHOOTS — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `yeekee_rounds` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `lottery_round_id` BIGINT NOT NULL,
  `round_no` INT NOT NULL,
  `start_time` DATETIME NOT NULL,
  `end_time` DATETIME NOT NULL,
  `status` VARCHAR(20) NOT NULL DEFAULT 'waiting',
  `result_number` VARCHAR(5) DEFAULT NULL,
  `total_shoots` INT NOT NULL DEFAULT 0,
  `total_sum` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`lottery_round_id`) REFERENCES `lottery_rounds`(`id`),
  INDEX `idx_yeekee_status` (`status`, `start_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `yeekee_shoots` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `yeekee_round_id` BIGINT NOT NULL,
  `member_id` BIGINT NOT NULL,
  `number` VARCHAR(5) NOT NULL,
  `shot_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`yeekee_round_id`) REFERENCES `yeekee_rounds`(`id`),
  FOREIGN KEY (`member_id`) REFERENCES `members`(`id`),
  INDEX `idx_shoots_round` (`yeekee_round_id`, `shot_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- TRANSACTIONS — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `transactions` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `member_id` BIGINT NOT NULL,
  `operator_id` BIGINT NOT NULL,
  `type` VARCHAR(20) NOT NULL,
  `amount` DECIMAL(15,2) NOT NULL,
  `balance_before` DECIMAL(15,2) NOT NULL,
  `balance_after` DECIMAL(15,2) NOT NULL,
  `reference_id` BIGINT DEFAULT NULL,
  `reference_type` VARCHAR(30) DEFAULT NULL,
  `note` TEXT DEFAULT NULL,
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`member_id`) REFERENCES `members`(`id`),
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  INDEX `idx_tx_member` (`member_id`, `created_at` DESC),
  INDEX `idx_tx_operator` (`operator_id`, `created_at` DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- WALLET_TRANSACTIONS — ⭐ เฉพาะ provider (transfer wallet mode)
-- =============================================================================
-- บันทึกการโอนเงินเข้า/ออก provider ของ operator
CREATE TABLE IF NOT EXISTS `wallet_transactions` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `operator_id` BIGINT NOT NULL,
  `member_external_id` VARCHAR(100) NOT NULL COMMENT 'ID ผู้เล่นฝั่ง operator',
  `type` VARCHAR(20) NOT NULL COMMENT 'deposit, withdraw',
  `amount` DECIMAL(15,2) NOT NULL,
  `reference` VARCHAR(100) DEFAULT NULL COMMENT 'txn reference จาก operator',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  INDEX `idx_wtx_operator` (`operator_id`, `created_at` DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Transfer wallet transactions — ⭐ ไม่มีใน standalone';

-- =============================================================================
-- OPERATOR_GAMES — operator เปิด/ปิดเกมไหนบ้าง
-- =============================================================================
CREATE TABLE IF NOT EXISTS `operator_games` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `operator_id` BIGINT NOT NULL,
  `lottery_type_id` BIGINT NOT NULL,
  `enabled` TINYINT(1) NOT NULL DEFAULT 1 COMMENT '1=เปิด, 0=ปิด',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (`operator_id`) REFERENCES `operators`(`id`),
  FOREIGN KEY (`lottery_type_id`) REFERENCES `lottery_types`(`id`),
  UNIQUE KEY `uk_operator_game` (`operator_id`, `lottery_type_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='operator เลือกเปิด/ปิดเกมไหน — ⭐ ไม่มีใน standalone';

-- =============================================================================
-- SETTINGS — เหมือน standalone
-- =============================================================================
CREATE TABLE IF NOT EXISTS `settings` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `key` VARCHAR(50) NOT NULL UNIQUE,
  `value` TEXT NOT NULL,
  `description` TEXT DEFAULT NULL,
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- =============================================================================
-- SEED DATA
-- =============================================================================

-- Admin default
INSERT INTO `admins` (`username`, `password_hash`, `name`, `role`) VALUES
('admin', '$2a$10$example_hash_change_this', 'Provider Admin', 'superadmin');

-- Bet types (เหมือน standalone)
INSERT INTO `bet_types` (`name`, `code`, `digit_count`, `description`) VALUES
('3 ตัวบน',    '3TOP',    3, 'ตรงตำแหน่ง 3 ตัวบน'),
('3 ตัวล่าง',  '3BOTTOM', 3, 'ตรงตำแหน่ง 3 ตัวล่าง'),
('3 ตัวโต๊ด',  '3TOD',    3, 'สลับตำแหน่งได้ 3 ตัว'),
('2 ตัวบน',    '2TOP',    2, 'ตรง 2 ตัวท้ายของ 3 ตัวบน'),
('2 ตัวล่าง',  '2BOTTOM', 2, 'ตรง 2 ตัวล่าง'),
('วิ่งบน',     'RUN_TOP', 1, 'เลข 1 ตัว อยู่ใน 3 ตัวบน'),
('วิ่งล่าง',   'RUN_BOT', 1, 'เลข 1 ตัว อยู่ใน 2 ตัวล่าง');

-- Lottery types (เหมือน standalone)
INSERT INTO `lottery_types` (`name`, `code`, `category`, `description`, `icon`, `is_auto_result`) VALUES
('หวยไทย (ใต้ดิน)',     'THAI',          'government', 'ออกผลวันที่ 1 และ 16 ของทุกเดือน', '🇹🇭', 0),
('หวยลาว',             'LAO',           'government', 'ออกผลตามรอบหวยลาว',              '🇱🇦', 0),
('หวยหุ้นไทย',         'STOCK_TH',      'stock',      'ออกผลตามตลาดหุ้นไทย จ-ศ',        '📈', 0),
('หวยหุ้นต่างประเทศ',   'STOCK_FOREIGN', 'stock',      'ออกผลตามตลาดหุ้นต่างประเทศ',     '🌍', 0),
('หวยยี่กี',            'YEEKEE',        'yeekee',     'ออกผลทุก 15 นาที (88 รอบ/วัน)',   '🎯', 1);

-- Pay rates กลาง (operator_id = NULL)
INSERT INTO `pay_rates` (`lottery_type_id`, `bet_type_id`, `operator_id`, `rate`, `max_bet_per_number`) VALUES
(1, 1, NULL, 900.00, 0), (1, 3, NULL, 150.00, 0), (1, 4, NULL, 90.00, 0),
(1, 5, NULL, 90.00, 0), (1, 6, NULL, 3.20, 0), (1, 7, NULL, 4.20, 0),
(2, 1, NULL, 900.00, 0), (2, 3, NULL, 150.00, 0), (2, 4, NULL, 90.00, 0),
(2, 5, NULL, 90.00, 0), (2, 6, NULL, 3.20, 0), (2, 7, NULL, 4.20, 0),
(3, 1, NULL, 850.00, 0), (3, 3, NULL, 120.00, 0), (3, 4, NULL, 90.00, 0),
(3, 5, NULL, 90.00, 0), (3, 6, NULL, 3.20, 0), (3, 7, NULL, 4.20, 0),
(4, 1, NULL, 850.00, 0), (4, 3, NULL, 120.00, 0), (4, 4, NULL, 90.00, 0),
(4, 5, NULL, 90.00, 0), (4, 6, NULL, 3.20, 0), (4, 7, NULL, 4.20, 0),
(5, 1, NULL, 850.00, 0), (5, 3, NULL, 120.00, 0), (5, 4, NULL, 90.00, 0),
(5, 5, NULL, 90.00, 0), (5, 6, NULL, 3.20, 0), (5, 7, NULL, 4.20, 0);

-- Settings
INSERT INTO `settings` (`key`, `value`, `description`) VALUES
('min_bet', '1', 'จำนวนเงินขั้นต่ำต่อ bet'),
('max_bet', '0', 'จำนวนเงินสูงสุดต่อ bet (0=ไม่จำกัด)'),
('yeekee_interval_minutes', '15', 'ระยะเวลาแต่ละรอบยี่กี'),
('yeekee_rounds_per_day', '88', 'จำนวนรอบยี่กีต่อวัน');
