// Package service — wallet_service.go
// จัดการ wallet integration กับ operator
//
// ⭐ นี่คือส่วนที่ต่างจาก standalone (#3) มากที่สุด
// standalone: หักเงินจาก internal wallet (UPDATE members SET balance -= amount)
// provider:   เรียก API ของ operator (HTTP call ไปหา operator server)
//
// รองรับ 2 แบบ:
// 1. Seamless Wallet — เรียก API operator ทุกครั้ง (balance/debit/credit)
// 2. Transfer Wallet — โอนเงินเข้า provider ก่อน แล้วเล่นจาก balance ใน provider
//
// ความสัมพันธ์:
// - ถูกเรียกโดย: BetService (ตอนแทง → debit) + ResultService (ตอนชนะ → credit)
// - ข้อมูล operator: operators table (api_key, secret_key, callback_url, wallet_type)
package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

// WalletService จัดการ wallet กับ operator
type WalletService struct {
	httpClient *http.Client
	db         *gorm.DB // ⭐ optional — ถ้า set จะเปิดใช้ idempotency store
}

// NewWalletService สร้าง WalletService instance (ไม่มี idempotency store)
func NewWalletService() *WalletService {
	return &WalletService{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewWalletServiceWithDB สร้าง WalletService พร้อม idempotency store
// ⭐ production ต้องใช้ตัวนี้ — ป้องกัน double-debit/credit จาก retry
func NewWalletServiceWithDB(db *gorm.DB) *WalletService {
	return &WalletService{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		db:         db,
	}
}

// =============================================================================
// Seamless Wallet API — เรียก operator ทุกครั้ง
// =============================================================================

// SeamlessBalanceRequest ขอดูยอดเงินจาก operator
type SeamlessBalanceRequest struct {
	PlayerID string `json:"player_id"` // ID ผู้เล่นฝั่ง operator
	Currency string `json:"currency"`
}

// SeamlessDebitRequest หักเงินจาก operator
type SeamlessDebitRequest struct {
	PlayerID    string  `json:"player_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	TxnID       string  `json:"txn_id"`       // unique transaction ID (ป้องกัน duplicate)
	RoundID     string  `json:"round_id"`      // รอบหวย
	Description string  `json:"description"`
}

// SeamlessCreditRequest เติมเงินกลับ operator (ตอนชนะ)
type SeamlessCreditRequest struct {
	PlayerID    string  `json:"player_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	TxnID       string  `json:"txn_id"`
	RoundID     string  `json:"round_id"`
	Description string  `json:"description"`
}

// WalletResponse response จาก operator
type WalletResponse struct {
	Success bool    `json:"success"`
	Balance float64 `json:"balance"`
	Message string  `json:"message,omitempty"`
}

// SeamlessBalance ดึงยอดเงินจาก operator
//
// Flow: provider → POST {operator_callback_url}/wallet/balance → operator
//
// ⭐ operator ต้อง implement endpoint นี้ตาม API spec ของเรา
func (s *WalletService) SeamlessBalance(callbackURL string, secretKey string, req SeamlessBalanceRequest) (*WalletResponse, error) {
	return s.callOperator(callbackURL+"/wallet/balance", secretKey, req)
}

// SeamlessDebit หักเงินจาก operator (ตอนวางเดิมพัน)
//
// Flow: ลูกค้าแทง → provider debit → operator หักเงินจาก player
// ถ้า operator return success=false → แทงไม่สำเร็จ (ยอดไม่พอ)
//
// ⭐ Idempotent: ถ้า txn_id เคย success แล้ว → return cached response ไม่ call operator อีก
func (s *WalletService) SeamlessDebit(callbackURL string, secretKey string, req SeamlessDebitRequest, operatorID int64) (*WalletResponse, error) {
	return s.seamlessCallIdempotent(callbackURL+"/wallet/debit", secretKey, req.TxnID, operatorID, "debit", req.PlayerID, req.Amount, req.RoundID, req)
}

// SeamlessCredit เติมเงินกลับ operator (ตอนชนะรางวัล)
//
// Flow: ออกผล → ลูกค้าชนะ → provider credit → operator เพิ่มเงินให้ player
//
// ⭐ Idempotent: ถ้า txn_id เคย success แล้ว → return cached response
func (s *WalletService) SeamlessCredit(callbackURL string, secretKey string, req SeamlessCreditRequest, operatorID int64) (*WalletResponse, error) {
	return s.seamlessCallIdempotent(callbackURL+"/wallet/credit", secretKey, req.TxnID, operatorID, "credit", req.PlayerID, req.Amount, req.RoundID, req)
}

// seamlessCallIdempotent — core wrapper ที่เช็ค seamless_txn_log ก่อน call operator
//
// Flow:
//  1. ถ้า db == nil → ผ่าน idempotency (legacy path) — call operator ตรงๆ
//  2. INSERT seamless_txn_log(txn_id, ...) status=pending
//  3. ถ้า INSERT fail (duplicate) → ดึง row เดิม:
//     - ถ้า status=success → return cached response (ไม่ call อีก)
//     - ถ้า status=pending → return error (concurrent request — operator อาจ process อยู่)
//     - ถ้า status=failed → update → retry call
//  4. Call operator
//  5. Update row status + response_body + completed_at
func (s *WalletService) seamlessCallIdempotent(url, secretKey, txnID string, operatorID int64, direction, playerID string, amount float64, roundID string, payload interface{}) (*WalletResponse, error) {
	// Legacy path: ไม่มี db → call ตรงๆ (unit tests / minimal setups)
	if s.db == nil {
		return callOperatorAPI(s.httpClient, url, secretKey, payload)
	}
	if txnID == "" {
		return nil, errors.New("seamless: txn_id required for idempotent call")
	}

	// Step 1+2: พยายาม INSERT row ใหม่
	logRow := model.SeamlessTxnLog{
		TxnID:      txnID,
		OperatorID: operatorID,
		Direction:  direction,
		PlayerID:   playerID,
		Amount:     amount,
		RoundID:    roundID,
		Status:     "pending",
	}
	err := s.db.Create(&logRow).Error
	if err != nil {
		// อาจซ้ำ — ลองดึง row เดิม
		var existing model.SeamlessTxnLog
		if findErr := s.db.Where("txn_id = ?", txnID).First(&existing).Error; findErr != nil {
			return nil, fmt.Errorf("seamless: insert failed and cannot lookup existing: %w", err)
		}
		if existing.Status == "success" {
			// ⭐ idempotent hit — ใช้ cached response
			log.Printf("🔁 [seamless] idempotent hit txn=%s (cached)", txnID)
			var cached WalletResponse
			if existing.ResponseBody != "" {
				_ = json.Unmarshal([]byte(existing.ResponseBody), &cached)
			}
			cached.Success = true
			return &cached, nil
		}
		if existing.Status == "pending" {
			return nil, fmt.Errorf("seamless: txn %s still pending (concurrent call?)", txnID)
		}
		// status=failed → reset to pending และ retry call
		s.db.Model(&existing).Updates(map[string]interface{}{
			"status":        "pending",
			"error_message": "",
		})
	}

	// Step 4: Call operator
	resp, callErr := callOperatorAPI(s.httpClient, url, secretKey, payload)

	// Step 5: อัพเดท log row
	now := time.Now()
	if callErr != nil {
		s.db.Model(&model.SeamlessTxnLog{}).Where("txn_id = ?", txnID).Updates(map[string]interface{}{
			"status":        "failed",
			"error_message": callErr.Error(),
			"completed_at":  &now,
		})
		return resp, callErr
	}

	// success — เก็บ response body สำหรับ replay
	respJSON, _ := json.Marshal(resp)
	s.db.Model(&model.SeamlessTxnLog{}).Where("txn_id = ?", txnID).Updates(map[string]interface{}{
		"status":        "success",
		"response_body": string(respJSON),
		"completed_at":  &now,
	})
	return resp, nil
}

// =============================================================================
// Callback — แจ้ง operator เมื่อมี event สำคัญ
// =============================================================================

// CallbackService แจ้ง event ไปยัง operator
type CallbackService struct {
	httpClient *http.Client
}

// NewCallbackService สร้าง CallbackService
func NewCallbackService() *CallbackService {
	return &CallbackService{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// BetResultCallback แจ้งผลแพ้ชนะไปยัง operator
type BetResultCallback struct {
	PlayerID    string  `json:"player_id"`
	RoundID     string  `json:"round_id"`
	BetID       string  `json:"bet_id"`
	Number      string  `json:"number"`
	BetType     string  `json:"bet_type"`
	Amount      float64 `json:"amount"`
	Status      string  `json:"status"` // won, lost
	WinAmount   float64 `json:"win_amount"`
	Timestamp   string  `json:"timestamp"`
}

// RoundEventCallback แจ้ง event รอบหวย (เปิด/ปิด)
type RoundEventCallback struct {
	Event     string `json:"event"` // round_start, round_end
	RoundID   string `json:"round_id"`
	GameCode  string `json:"game_code"`
	Timestamp string `json:"timestamp"`
}

// NotifyBetResult แจ้งผลแพ้ชนะไปยัง operator
//
// POST {operator_callback_url}/bet-result
func (s *CallbackService) NotifyBetResult(callbackURL string, secretKey string, data BetResultCallback) error {
	_, err := callOperatorAPI(s.httpClient, callbackURL+"/bet-result", secretKey, data)
	return err
}

// NotifyRoundEvent แจ้ง event รอบ (เปิด/ปิด)
//
// POST {operator_callback_url}/round-start  หรือ /round-end
func (s *CallbackService) NotifyRoundEvent(callbackURL string, secretKey string, data RoundEventCallback) error {
	endpoint := callbackURL + "/" + data.Event
	_, err := callOperatorAPI(s.httpClient, endpoint, secretKey, data)
	return err
}

// =============================================================================
// Internal — HTTP call + HMAC signing
// =============================================================================

// callOperator เรียก API ของ operator พร้อม HMAC signature
func (s *WalletService) callOperator(url string, secretKey string, payload interface{}) (*WalletResponse, error) {
	return callOperatorAPI(s.httpClient, url, secretKey, payload)
}

// callOperatorAPI เรียก API ของ operator พร้อม HMAC signature
//
// ทุก request ที่ส่งไป operator จะมี:
// - Header X-Signature: HMAC-SHA256(body, secret_key)
// - Header X-Timestamp: Unix timestamp
// - Body: JSON payload
func callOperatorAPI(client *http.Client, url string, secretKey string, payload interface{}) (*WalletResponse, error) {
	// Marshal body
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// สร้าง HMAC signature
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	signData := string(body) + timestamp
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(signData))
	signature := hex.EncodeToString(mac.Sum(nil))

	// สร้าง HTTP request
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", signature)
	req.Header.Set("X-Timestamp", timestamp)

	// ส่ง request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("operator API call failed: %w", err)
	}
	defer resp.Body.Close()

	// อ่าน response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("operator returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var walletResp WalletResponse
	if err := json.Unmarshal(respBody, &walletResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !walletResp.Success {
		return &walletResp, errors.New(walletResp.Message)
	}

	return &walletResp, nil
}
