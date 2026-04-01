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
	"net/http"
	"time"
)

// WalletService จัดการ wallet กับ operator
type WalletService struct {
	httpClient *http.Client
}

// NewWalletService สร้าง WalletService instance
func NewWalletService() *WalletService {
	return &WalletService{
		httpClient: &http.Client{Timeout: 10 * time.Second},
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
func (s *WalletService) SeamlessDebit(callbackURL string, secretKey string, req SeamlessDebitRequest) (*WalletResponse, error) {
	return s.callOperator(callbackURL+"/wallet/debit", secretKey, req)
}

// SeamlessCredit เติมเงินกลับ operator (ตอนชนะรางวัล)
//
// Flow: ออกผล → ลูกค้าชนะ → provider credit → operator เพิ่มเงินให้ player
func (s *WalletService) SeamlessCredit(callbackURL string, secretKey string, req SeamlessCreditRequest) (*WalletResponse, error) {
	return s.callOperator(callbackURL+"/wallet/credit", secretKey, req)
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
