// Package model — GORM models สำหรับ provider-game-api
//
// ⭐ ต่างจาก standalone models:
//   - Operator struct — ไม่มีใน standalone
//   - Member มี OperatorID + ExternalPlayerID
//   - Bet มี OperatorID
//   - NumberBan มี OperatorID (nullable)
//   - PayRate มี OperatorID (nullable)
//   - WalletTransaction — ไม่มีใน standalone
//   - OperatorGame — ไม่มีใน standalone
//
// ความสัมพันธ์:
// - ใช้โดย: repository layer → GORM queries
// - map ไป lotto-core types ก่อนส่งเข้า business logic
// - share DB กับ backoffice-api (#9) — models เหมือนกัน
package model

import "time"

// Operator — operator ที่มาเชื่อมต่อ API
type Operator struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:100;not null" json:"name"`
	Code         string    `gorm:"size:30;uniqueIndex;not null" json:"code"`
	APIKey       string    `gorm:"column:api_key;size:64;uniqueIndex;not null" json:"api_key"`
	SecretKey    string    `gorm:"column:secret_key;size:128;not null" json:"-"` // ไม่ return ใน JSON
	CallbackURL  string    `gorm:"column:callback_url;size:500" json:"callback_url"`
	WalletType   string    `gorm:"column:wallet_type;size:20;not null;default:seamless" json:"wallet_type"` // seamless / transfer
	IPWhitelist  string    `gorm:"column:ip_whitelist;type:text" json:"ip_whitelist"`
	Username     string    `gorm:"size:50" json:"username"`
	PasswordHash string    `gorm:"column:password_hash;size:255" json:"-"`
	Status       string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Admin — provider admin user
type Admin struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"size:50;uniqueIndex;not null" json:"username"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`
	Name         string     `gorm:"size:100" json:"name"`
	Role         string     `gorm:"size:20;not null;default:admin" json:"role"`
	Status       string     `gorm:"size:20;not null;default:active" json:"status"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// Member — player จาก operator
type Member struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	OperatorID       int64     `gorm:"not null;index" json:"operator_id"`
	ExternalPlayerID string    `gorm:"column:external_player_id;size:100;not null" json:"external_player_id"`
	Username         string    `gorm:"size:100" json:"username"`
	Balance          float64   `gorm:"type:decimal(15,2);not null;default:0" json:"balance"`
	Status           string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// Relations
	Operator *Operator `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`
}

// LotteryType — ประเภทหวย
type LotteryType struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:100;not null" json:"name"`
	Code         string    `gorm:"size:30;uniqueIndex;not null" json:"code"`
	Category     string    `gorm:"size:30;not null;default:government" json:"category"`
	Description  string    `gorm:"type:text" json:"description"`
	Icon         string    `gorm:"size:50" json:"icon"`
	IsAutoResult bool      `gorm:"column:is_auto_result;not null;default:false" json:"is_auto_result"`
	Status       string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// BetType — ประเภทการแทง
type BetType struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:50;not null" json:"name"`
	Code        string    `gorm:"size:20;uniqueIndex;not null" json:"code"`
	DigitCount  int       `gorm:"not null" json:"digit_count"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// LotteryRound — รอบหวย
type LotteryRound struct {
	ID            int64      `gorm:"primaryKey" json:"id"`
	LotteryTypeID int64      `gorm:"not null;index" json:"lottery_type_id"`
	RoundNumber   string     `gorm:"size:50;not null" json:"round_number"`
	RoundDate     time.Time  `gorm:"type:date;not null" json:"round_date"`
	OpenTime      time.Time  `gorm:"not null" json:"open_time"`
	CloseTime     time.Time  `gorm:"not null" json:"close_time"`
	Status        string     `gorm:"size:20;not null;default:upcoming" json:"status"`
	ResultTop3    *string    `gorm:"column:result_top3;size:3" json:"result_top3"`
	ResultTop2    *string    `gorm:"column:result_top2;size:2" json:"result_top2"`
	ResultBottom2 *string    `gorm:"column:result_bottom2;size:2" json:"result_bottom2"`
	ResultedAt    *time.Time `json:"resulted_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	// Relations
	LotteryType *LotteryType `gorm:"foreignKey:LotteryTypeID" json:"lottery_type,omitempty"`
}

// PayRate — อัตราจ่าย (⭐ มี OperatorID)
type PayRate struct {
	ID              int64     `gorm:"primaryKey" json:"id"`
	LotteryTypeID   int64     `gorm:"not null" json:"lottery_type_id"`
	BetTypeID       int64     `gorm:"not null" json:"bet_type_id"`
	OperatorID      *int64    `json:"operator_id"` // NULL = global rate
	Rate            float64   `gorm:"type:decimal(10,2);not null" json:"rate"`
	MaxBetPerNumber float64   `gorm:"type:decimal(15,2);not null;default:0" json:"max_bet_per_number"`
	Status          string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Relations
	BetType     *BetType     `gorm:"foreignKey:BetTypeID" json:"bet_type,omitempty"`
	LotteryType *LotteryType `gorm:"foreignKey:LotteryTypeID" json:"lottery_type,omitempty"`
}

// Bet — การเดิมพัน (⭐ มี OperatorID)
type Bet struct {
	ID             int64      `gorm:"primaryKey" json:"id"`
	MemberID       int64      `gorm:"not null;index" json:"member_id"`
	OperatorID     int64      `gorm:"not null;index" json:"operator_id"`
	LotteryRoundID int64      `gorm:"not null;index" json:"lottery_round_id"`
	BetTypeID      int64      `gorm:"not null" json:"bet_type_id"`
	Number         string     `gorm:"size:10;not null" json:"number"`
	Amount         float64    `gorm:"type:decimal(15,2);not null" json:"amount"`
	Rate           float64    `gorm:"type:decimal(10,2);not null" json:"rate"`
	Status         string     `gorm:"size:20;not null;default:pending" json:"status"`
	WinAmount      float64    `gorm:"type:decimal(15,2);not null;default:0" json:"win_amount"`
	SettledAt      *time.Time `json:"settled_at"`
	CreatedAt      time.Time  `json:"created_at"`
	// Relations
	Member       *Member       `gorm:"foreignKey:MemberID" json:"member,omitempty"`
	Operator     *Operator     `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`
	LotteryRound *LotteryRound `gorm:"foreignKey:LotteryRoundID" json:"lottery_round,omitempty"`
	BetType      *BetType      `gorm:"foreignKey:BetTypeID" json:"bet_type,omitempty"`
}

// NumberBan — เลขอั้น (⭐ มี OperatorID)
type NumberBan struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	LotteryTypeID  int64     `gorm:"not null" json:"lottery_type_id"`
	LotteryRoundID *int64    `json:"lottery_round_id"`
	OperatorID     *int64    `json:"operator_id"` // NULL = global
	BetTypeID      int64     `gorm:"not null" json:"bet_type_id"`
	Number         string    `gorm:"size:10;not null" json:"number"`
	BanType        string    `gorm:"size:20;not null;default:full_ban" json:"ban_type"`
	ReducedRate    float64   `gorm:"type:decimal(10,2);not null;default:0" json:"reduced_rate"`
	MaxAmount      float64   `gorm:"type:decimal(15,2);not null;default:0" json:"max_amount"`
	Status         string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// YeekeeRound — รอบยี่กี
type YeekeeRound struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	LotteryRoundID int64     `gorm:"not null" json:"lottery_round_id"`
	RoundNo        int       `gorm:"not null" json:"round_no"`
	StartTime      time.Time `gorm:"not null" json:"start_time"`
	EndTime        time.Time `gorm:"not null" json:"end_time"`
	Status         string    `gorm:"size:20;not null;default:waiting" json:"status"`
	ResultNumber   *string   `gorm:"size:5" json:"result_number"`
	TotalShoots    int       `gorm:"not null;default:0" json:"total_shoots"`
	TotalSum       int64     `gorm:"not null;default:0" json:"total_sum"`
	CreatedAt      time.Time `json:"created_at"`
}

// YeekeeShoot — เลขที่ยิง
type YeekeeShoot struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	YeekeeRoundID int64     `gorm:"not null;index" json:"yeekee_round_id"`
	MemberID      int64     `gorm:"not null" json:"member_id"`
	Number        string    `gorm:"size:5;not null" json:"number"`
	ShotAt        time.Time `gorm:"not null" json:"shot_at"`
	// Relations
	Member *Member `gorm:"foreignKey:MemberID" json:"member,omitempty"`
}

// Transaction — ธุรกรรม
type Transaction struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	MemberID      int64     `gorm:"not null;index" json:"member_id"`
	OperatorID    int64     `gorm:"not null;index" json:"operator_id"`
	Type          string    `gorm:"size:20;not null" json:"type"`
	Amount        float64   `gorm:"type:decimal(15,2);not null" json:"amount"`
	BalanceBefore float64   `gorm:"type:decimal(15,2);not null" json:"balance_before"`
	BalanceAfter  float64   `gorm:"type:decimal(15,2);not null" json:"balance_after"`
	ReferenceID   *int64    `json:"reference_id"`
	ReferenceType string    `gorm:"size:30" json:"reference_type"`
	Note          string    `gorm:"type:text" json:"note"`
	CreatedAt     time.Time `json:"created_at"`
}

// WalletTransaction — transfer wallet (⭐ เฉพาะ provider)
type WalletTransaction struct {
	ID               int64     `gorm:"primaryKey" json:"id"`
	OperatorID       int64     `gorm:"not null;index" json:"operator_id"`
	MemberExternalID string    `gorm:"column:member_external_id;size:100;not null" json:"member_external_id"`
	Type             string    `gorm:"size:20;not null" json:"type"`
	Amount           float64   `gorm:"type:decimal(15,2);not null" json:"amount"`
	Reference        string    `gorm:"size:100" json:"reference"`
	CreatedAt        time.Time `json:"created_at"`
}

// OperatorGame — operator เปิด/ปิดเกมไหน (⭐ เฉพาะ provider)
type OperatorGame struct {
	ID            int64     `gorm:"primaryKey" json:"id"`
	OperatorID    int64     `gorm:"not null" json:"operator_id"`
	LotteryTypeID int64     `gorm:"not null" json:"lottery_type_id"`
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	// Relations
	LotteryType *LotteryType `gorm:"foreignKey:LotteryTypeID" json:"lottery_type,omitempty"`
}

// Setting — ตั้งค่าระบบ
type Setting struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Key         string    `gorm:"size:50;uniqueIndex;not null" json:"key"`
	Value       string    `gorm:"type:text;not null" json:"value"`
	Description string    `gorm:"type:text" json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AutoBanRule กฎอั้นเลขอัตโนมัติ — share table กับ standalone
type AutoBanRule struct {
	ID              int64     `gorm:"primaryKey" json:"id"`
	AgentID         int64     `gorm:"not null;default:1;index" json:"agent_id"`
	LotteryTypeID   int64     `gorm:"not null;index" json:"lottery_type_id"`
	BetType         string    `gorm:"size:30;not null" json:"bet_type"`
	ThresholdAmount float64   `gorm:"type:decimal(15,2);not null" json:"threshold_amount"`
	Action          string    `gorm:"size:20;not null;default:full_ban" json:"action"`
	ReducedRate     float64   `gorm:"type:decimal(10,2);default:0" json:"reduced_rate"`
	Capital         float64   `gorm:"type:decimal(15,2);default:0" json:"capital"`
	MaxLoss         float64   `gorm:"type:decimal(15,2);default:0" json:"max_loss"`
	Rate            float64   `gorm:"type:decimal(10,2);default:0" json:"rate"`
	Status          string    `gorm:"size:20;not null;default:active" json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (AutoBanRule) TableName() string { return "auto_ban_rules" }
