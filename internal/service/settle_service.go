// Package service — settle_service.go
//
// Settlement (เทียบผลการแทง + จ่ายเงินรางวัล) สำหรับ provider
//
// ⭐ เรียกจาก 2 ที่:
//   - cron ปิดรอบยี่กีอัตโนมัติ (internal/job/yeekee_cron.go)
//   - admin backoffice กรอกผล manual (ไฟล์อยู่ backoffice-api — logic เหมือนกัน)
//
// Flow:
//  1. ดึง pending bets สำหรับ lottery_round_id
//  2. แปลง → lotto-core coreTypes.Bet
//  3. payout.SettleRound() → ได้ BetResult[] + summary
//  4. อัพเดท bet status/win_amount/settled_at
//  5. จ่ายเงินผู้ชนะ:
//       - transfer mode (operator.wallet_type='transfer' หรือ default):
//           update member.balance + insert transaction row
//       - seamless mode (operator.wallet_type='seamless'):
//           SeamlessCredit → operator callback (best-effort, fallback log warning)
//  6. NotifyBetResult (callback แจ้ง operator ทุก bet — ทั้งแพ้และชนะ, best-effort)
package service

import (
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/farritpcz/lotto-core/payout"
	coreTypes "github.com/farritpcz/lotto-core/types"

	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

// SettleService เทียบผล + จ่ายเงิน (per lottery_round)
type SettleService struct {
	db       *gorm.DB
	wallet   *WalletService
	callback *CallbackService
}

// NewSettleService สร้าง settlement service
func NewSettleService(db *gorm.DB) *SettleService {
	return &SettleService{
		db:       db,
		wallet:   NewWalletService(),
		callback: NewCallbackService(),
	}
}

// SettleSummary สรุปผล settle (สำหรับ log / return)
type SettleSummary struct {
	LotteryRoundID int64
	TotalBets      int
	TotalWinners   int
	TotalWinAmount float64
	TotalBetAmount float64
	Profit         float64 // TotalBetAmount - TotalWinAmount (มุมมอง house)
}

// SettleRound เทียบ bets กับ roundResult + จ่ายเงินผู้ชนะ
//
// ไม่ return error — log แล้วเดินต่อ (partial settle ดีกว่า rollback ทั้งหมด)
func (s *SettleService) SettleRound(lotteryRoundID int64, roundResult coreTypes.RoundResult) SettleSummary {
	summary := SettleSummary{LotteryRoundID: lotteryRoundID}

	// 1. ดึง pending bets + preload bet_type + operator (สำหรับ callback)
	var bets []model.Bet
	if err := s.db.Where("lottery_round_id = ? AND status = ?", lotteryRoundID, "pending").
		Preload("BetType").
		Preload("Operator").
		Find(&bets).Error; err != nil {
		log.Printf("❌ [settle] failed to load bets for round %d: %v", lotteryRoundID, err)
		return summary
	}

	if len(bets) == 0 {
		log.Printf("ℹ️ [settle] no pending bets for round %d", lotteryRoundID)
		return summary
	}

	// 2. แปลง → lotto-core bets
	coreBets := make([]coreTypes.Bet, 0, len(bets))
	for _, b := range bets {
		betTypeCode := ""
		if b.BetType != nil {
			betTypeCode = b.BetType.Code
		}
		coreBets = append(coreBets, coreTypes.Bet{
			ID:       b.ID,
			MemberID: b.MemberID,
			RoundID:  b.LotteryRoundID,
			BetType:  coreTypes.BetType(betTypeCode),
			Number:   b.Number,
			Amount:   b.Amount,
			Rate:     b.Rate,
			Status:   coreTypes.BetStatusPending,
		})
	}

	// 3. lotto-core: คำนวณผลทั้งหมด
	out := payout.SettleRound(payout.SettleRoundInput{
		RoundID: lotteryRoundID,
		Result:  roundResult,
		Bets:    coreBets,
	})

	summary.TotalBets = len(bets)
	summary.TotalWinners = out.TotalWinners
	summary.TotalWinAmount = out.TotalWinAmount
	summary.TotalBetAmount = out.TotalBetAmount
	summary.Profit = out.Profit

	log.Printf("💰 [settle] round=%d bets=%d winners=%d win=%.2f bet=%.2f profit=%.2f",
		lotteryRoundID, summary.TotalBets, summary.TotalWinners,
		summary.TotalWinAmount, summary.TotalBetAmount, summary.Profit)

	// 4. อัพเดท bet status + win_amount ใน transaction
	betResultMap := make(map[int64]coreTypes.BetResult, len(out.BetResults))
	for _, br := range out.BetResults {
		betResultMap[br.BetID] = br
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ [settle] panic during payout: %v", r)
		}
	}()

	now := time.Now()
	for _, b := range bets {
		br, found := betResultMap[b.ID]
		if !found {
			continue
		}
		newStatus := "lost"
		var winAmount float64
		if br.IsWin {
			newStatus = "won"
			winAmount = br.WinAmount
		}
		tx.Model(&model.Bet{}).Where("id = ?", b.ID).Updates(map[string]interface{}{
			"status":     newStatus,
			"win_amount": winAmount,
			"settled_at": &now,
		})
	}

	// 5. จ่ายเงินผู้ชนะ — แยกตาม operator.wallet_type
	s.payoutWinners(tx, bets, coreBets, out.BetResults, lotteryRoundID, now)

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		log.Printf("❌ [settle] failed to commit round %d: %v", lotteryRoundID, err)
		return summary
	}

	// 6. Fire-and-forget: callback แจ้งผลรายตัว (หลัง commit เพื่อให้ DB เป็น source of truth)
	go s.fireBetResultCallbacks(bets, betResultMap, lotteryRoundID)

	log.Printf("✅ [settle] round %d complete — %d winners credited", lotteryRoundID, summary.TotalWinners)
	return summary
}

// payoutWinners จ่ายเงินผู้ชนะตาม wallet_type ของแต่ละ operator
//
// - transfer: UPDATE member.balance + INSERT transaction (atomic)
// - seamless: call SeamlessCredit ไปหา operator (best-effort; ถ้า fail → log warn แต่ไม่ rollback)
//
// ⚠️ ใช้ tx (gorm.DB transaction) ที่ caller เปิดไว้ — รับผิดชอบ commit/rollback เอง
func (s *SettleService) payoutWinners(
	tx *gorm.DB,
	bets []model.Bet,
	coreBets []coreTypes.Bet,
	betResults []coreTypes.BetResult,
	lotteryRoundID int64,
	now time.Time,
) {
	// รวม win amount ต่อ member (ลด SQL calls)
	memberPayouts := payout.GroupWinnersByMember(coreBets, betResults)
	if len(memberPayouts) == 0 {
		return
	}

	// preload member + operator — single query
	memberIDs := make([]int64, 0, len(memberPayouts))
	for mid := range memberPayouts {
		memberIDs = append(memberIDs, mid)
	}

	var members []model.Member
	tx.Preload("Operator").Where("id IN ?", memberIDs).Find(&members)
	memberMap := make(map[int64]model.Member, len(members))
	for _, m := range members {
		memberMap[m.ID] = m
	}

	for memberID, totalWin := range memberPayouts {
		if totalWin <= 0 {
			continue
		}
		m, ok := memberMap[memberID]
		if !ok {
			log.Printf("⚠️ [settle] member %d not found — skip payout %.2f", memberID, totalWin)
			continue
		}

		mode := "transfer"
		if m.Operator != nil && m.Operator.WalletType == "seamless" {
			mode = "seamless"
		}

		switch mode {
		case "seamless":
			// ⭐ seamless: ส่ง credit ไปหา operator — best-effort, ถ้า fail → log แต่ไม่ rollback
			s.creditSeamless(m, totalWin, lotteryRoundID)
		default:
			// ⭐ transfer: update balance + insert transaction ภายใน tx เดียวกัน
			s.creditTransfer(tx, m, totalWin, lotteryRoundID, now)
		}
	}
}

// creditTransfer จ่ายเงินแบบ transfer — update member.balance + insert Transaction
func (s *SettleService) creditTransfer(
	tx *gorm.DB,
	m model.Member,
	amount float64,
	lotteryRoundID int64,
	now time.Time,
) {
	before := m.Balance
	after := before + amount

	if err := tx.Model(&model.Member{}).Where("id = ?", m.ID).
		Update("balance", gorm.Expr("balance + ?", amount)).Error; err != nil {
		log.Printf("⚠️ [settle/transfer] failed credit member %d: %v", m.ID, err)
		return
	}

	roundID := lotteryRoundID
	winTx := model.Transaction{
		MemberID:      m.ID,
		OperatorID:    m.OperatorID,
		Type:          "win",
		Amount:        amount,
		BalanceBefore: before,
		BalanceAfter:  after,
		ReferenceID:   &roundID,
		ReferenceType: "lottery_round",
		Note:          "ชนะรางวัลหวย",
		CreatedAt:     now,
	}
	if err := tx.Create(&winTx).Error; err != nil {
		log.Printf("⚠️ [settle/transfer] failed log transaction member %d: %v", m.ID, err)
	}
}

// creditSeamless ส่ง credit ไปหา operator (seamless wallet)
//
// ❗ best-effort: ถ้า operator API ล้ม → log warning ไม่ retry (ภายใน settle tx)
//    ควรมี reconciliation job แยกดึง failed credit มา retry — ยังไม่ implement
func (s *SettleService) creditSeamless(m model.Member, amount float64, lotteryRoundID int64) {
	if m.Operator == nil || m.Operator.CallbackURL == "" {
		log.Printf("⚠️ [settle/seamless] member %d operator missing callback_url — skip credit", m.ID)
		return
	}

	req := SeamlessCreditRequest{
		PlayerID:    m.ExternalPlayerID,
		Amount:      amount,
		Currency:    "THB",
		TxnID:       fmt.Sprintf("win-%d-%d", lotteryRoundID, m.ID),
		RoundID:     fmt.Sprintf("%d", lotteryRoundID),
		Description: "ชนะรางวัลหวย",
	}
	if _, err := s.wallet.SeamlessCredit(m.Operator.CallbackURL, m.Operator.SecretKey, req); err != nil {
		log.Printf("⚠️ [settle/seamless] credit failed member=%d op=%d amount=%.2f: %v",
			m.ID, m.OperatorID, amount, err)
	}
}

// fireBetResultCallbacks แจ้งผล bet ทุกตัวไปหา operator (best-effort, async)
//
// ไม่ block ทำ settle หลัก — errors แค่ log
func (s *SettleService) fireBetResultCallbacks(
	bets []model.Bet,
	betResultMap map[int64]coreTypes.BetResult,
	lotteryRoundID int64,
) {
	// cache operator config ต่อ id (ลด DB call)
	opCache := make(map[int64]*model.Operator)
	getOp := func(opID int64) *model.Operator {
		if op, ok := opCache[opID]; ok {
			return op
		}
		var op model.Operator
		if err := s.db.First(&op, opID).Error; err != nil {
			opCache[opID] = nil
			return nil
		}
		opCache[opID] = &op
		return &op
	}

	for _, b := range bets {
		br, ok := betResultMap[b.ID]
		if !ok {
			continue
		}
		op := getOp(b.OperatorID)
		if op == nil || op.CallbackURL == "" {
			continue // operator ไม่มี callback — ข้าม
		}

		status := "lost"
		if br.IsWin {
			status = "won"
		}
		betTypeCode := ""
		if b.BetType != nil {
			betTypeCode = b.BetType.Code
		}
		data := BetResultCallback{
			PlayerID:  "", // populate ด้วย member external id (query ต่อ)
			RoundID:   fmt.Sprintf("%d", lotteryRoundID),
			BetID:     fmt.Sprintf("%d", b.ID),
			Number:    b.Number,
			BetType:   betTypeCode,
			Amount:    b.Amount,
			Status:    status,
			WinAmount: br.WinAmount,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		// lookup external_player_id (ถ้า member record ยัง load — ทั่วไปไม่ preload จาก caller)
		var ep string
		s.db.Model(&model.Member{}).Where("id = ?", b.MemberID).
			Select("external_player_id").Scan(&ep)
		data.PlayerID = ep

		if err := s.callback.NotifyBetResult(op.CallbackURL, op.SecretKey, data); err != nil {
			log.Printf("⚠️ [settle/callback] bet_result failed bet=%d op=%d: %v", b.ID, op.ID, err)
		}
	}
}
