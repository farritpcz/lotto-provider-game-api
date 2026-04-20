// Package job — yeekee_cron.go
// Cron job สำหรับจัดการรอบยี่กีอัตโนมัติ
//
// ทำ 3 อย่าง:
//  1. สร้างรอบยี่กีประจำวัน (88 รอบ/วัน ทุก 15 นาที เริ่ม 06:00)
//  2. เปิดรอบ (upcoming → shooting) เมื่อถึงเวลา
//  3. ปิดรอบ + คำนวณผล (shooting → calculating → resulted) เมื่อหมดเวลา
//
// ความสัมพันธ์:
// - ใช้ lotto-core: lottery.GenerateYeekeeSchedule() สร้างตาราง
// - ใช้ lotto-core: yeekee.CalculateResult() คำนวณผลจากเลขยิง
// - ใช้ lotto-core: payout.SettleRound() ตัดสินผล + จ่ายเงิน
// - provider-game-api (#7) ใช้ cron เหมือนกันเป๊ะ
//
// รัน: เรียกจาก main.go → go job.StartYeekeeCron(db)
package job

import (
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/farritpcz/lotto-core/lottery"
	"github.com/farritpcz/lotto-core/yeekee"
	coreTypes "github.com/farritpcz/lotto-core/types"

	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

// StartYeekeeCron เริ่ม cron job สำหรับยี่กี
// รัน background goroutine ที่ตรวจสอบทุก 30 วินาที
func StartYeekeeCron(db *gorm.DB) {
	log.Println("🎯 Yeekee cron job started (check every 30s)")

	go func() {
		// สร้างรอบวันนี้ตอนเริ่ม
		createDailyRounds(db, time.Now())

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		lastDate := time.Now().Format("2006-01-02")

		for range ticker.C {
			now := time.Now()

			// ถ้าข้ามวัน → สร้างรอบใหม่
			today := now.Format("2006-01-02")
			if today != lastDate {
				createDailyRounds(db, now)
				lastDate = today
			}

			// เปิดรอบที่ถึงเวลา (upcoming → shooting)
			openReadyRounds(db, now)

			// ปิดรอบที่หมดเวลา + คำนวณผล
			closeAndSettleExpiredRounds(db, now)
		}
	}()
}

// createDailyRounds สร้างรอบยี่กี 88 รอบสำหรับวันที่กำหนด
//
// Flow:
//  1. lotto-core lottery.GenerateYeekeeSchedule() → ได้ schedule 88 รอบ
//  2. สร้าง lottery_round + yeekee_round สำหรับแต่ละรอบ
func createDailyRounds(db *gorm.DB, date time.Time) {
	// หา yeekee lottery type
	var yeekeeType model.LotteryType
	if err := db.Where("code = ?", "YEEKEE").First(&yeekeeType).Error; err != nil {
		log.Println("⚠️ YEEKEE lottery type not found — skipping cron")
		return
	}

	// เช็คว่าวันนี้สร้างแล้วหรือยัง
	today := date.Format("2006-01-02")
	var count int64
	db.Model(&model.LotteryRound{}).
		Where("lottery_type_id = ? AND round_date = ?", yeekeeType.ID, today).
		Count(&count)
	if count > 0 {
		log.Printf("🎯 Yeekee rounds for %s already exist (%d rounds)", today, count)
		return
	}

	// สร้าง schedule จาก lotto-core
	schedule := lottery.GenerateYeekeeSchedule(date, lottery.DefaultYeekeeConfig)

	log.Printf("🎯 Creating %d yeekee rounds for %s...", len(schedule), today)

	for _, s := range schedule {
		// สร้าง lottery_round
		roundNumber := lottery.GenerateRoundNumber(coreTypes.LotteryTypeYeekee, date, s.RoundNo)
		lotteryRound := model.LotteryRound{
			LotteryTypeID: yeekeeType.ID,
			RoundNumber:   roundNumber,
			RoundDate:     date,
			OpenTime:      s.StartTime,
			CloseTime:     s.EndTime,
			Status:        "upcoming",
		}
		if err := db.Create(&lotteryRound).Error; err != nil {
			log.Printf("⚠️ Failed to create lottery round %s: %v", roundNumber, err)
			continue
		}

		// สร้าง yeekee_round
		yeekeeRound := model.YeekeeRound{
			LotteryRoundID: lotteryRound.ID,
			RoundNo:        s.RoundNo,
			StartTime:      s.StartTime,
			EndTime:        s.EndTime,
			Status:         "waiting",
		}
		db.Create(&yeekeeRound)
	}

	log.Printf("✅ Created %d yeekee rounds for %s", len(schedule), today)
}

// openReadyRounds เปิดรอบที่ถึงเวลาเริ่ม
// upcoming → open (lottery_round) + waiting → shooting (yeekee_round)
func openReadyRounds(db *gorm.DB, now time.Time) {
	// อัพเดท lottery_rounds: upcoming → open (ถ้าถึง open_time แล้ว)
	result := db.Model(&model.LotteryRound{}).
		Where("status = ? AND open_time <= ?", "upcoming", now).
		Update("status", "open")
	if result.RowsAffected > 0 {
		log.Printf("🟢 Opened %d lottery rounds", result.RowsAffected)
	}

	// อัพเดท yeekee_rounds: waiting → shooting
	result = db.Model(&model.YeekeeRound{}).
		Where("status = ? AND start_time <= ? AND end_time > ?", "waiting", now, now).
		Update("status", "shooting")
	if result.RowsAffected > 0 {
		log.Printf("🔫 Yeekee rounds now shooting: %d", result.RowsAffected)
	}
}

// closeAndSettleExpiredRounds ปิดรอบที่หมดเวลา + คำนวณผล
func closeAndSettleExpiredRounds(db *gorm.DB, now time.Time) {
	// หารอบยี่กีที่หมดเวลาแล้ว (status = shooting, end_time <= now)
	var expiredRounds []model.YeekeeRound
	db.Where("status = ? AND end_time <= ?", "shooting", now).Find(&expiredRounds)

	for _, yr := range expiredRounds {
		settleYeekeeRound(db, yr)
	}
}

// settleYeekeeRound คำนวณผลยี่กีรอบเดียว
//
// Flow:
//  1. ดึงเลขที่ยิงทั้งหมด
//  2. lotto-core yeekee.CalculateResult() → ได้ผล
//  3. บันทึกผลใน yeekee_round + lottery_round
//  4. AIDEV-TODO(farri, 2026-04-21): trigger payout — pattern ตาม standalone yeekee_settle_handler.go
func settleYeekeeRound(db *gorm.DB, yr model.YeekeeRound) {
	log.Printf("🔄 Settling yeekee round %d (round_no: %d)...", yr.ID, yr.RoundNo)

	// 1. อัพเดทสถานะ → calculating
	db.Model(&yr).Update("status", "calculating")

	// 2. ดึงเลขที่ยิงทั้งหมด
	var shoots []model.YeekeeShoot
	db.Where("yeekee_round_id = ?", yr.ID).Find(&shoots)

	if len(shoots) == 0 {
		log.Printf("⚠️ No shoots in yeekee round %d — marking as resulted with no result", yr.ID)
		db.Model(&yr).Updates(map[string]interface{}{"status": "resulted", "total_shoots": 0})
		// ปิด lottery_round ด้วย
		db.Model(&model.LotteryRound{}).Where("id = ?", yr.LotteryRoundID).
			Update("status", "closed")
		return
	}

	// 3. แปลง model.YeekeeShoot → lotto-core types.YeekeeShoot
	coreShots := make([]coreTypes.YeekeeShoot, 0, len(shoots))
	for _, s := range shoots {
		coreShots = append(coreShots, coreTypes.YeekeeShoot{
			ID:       s.ID,
			RoundID:  s.YeekeeRoundID,
			MemberID: s.MemberID,
			Number:   s.Number,
			ShotAt:   s.ShotAt,
		})
	}

	// 4. ⭐ lotto-core: คำนวณผลยี่กี
	resultNumber, roundResult, err := yeekee.CalculateResult(coreShots)
	if err != nil {
		log.Printf("❌ Failed to calculate yeekee result: %v", err)
		return
	}

	log.Printf("🎯 Yeekee round %d result: %s (top3: %s, top2: %s, bottom2: %s, shoots: %d)",
		yr.RoundNo, resultNumber, roundResult.Top3, roundResult.Top2, roundResult.Bottom2, len(shoots))

	// 5. บันทึกผลใน yeekee_round
	db.Model(&yr).Updates(map[string]interface{}{
		"status":        "resulted",
		"result_number": resultNumber,
		"total_shoots":  len(shoots),
		"total_sum":     yeekee.GetShootSum(coreShots),
	})

	// 6. บันทึกผลใน lottery_round
	now := time.Now()
	db.Model(&model.LotteryRound{}).Where("id = ?", yr.LotteryRoundID).Updates(map[string]interface{}{
		"status":         "resulted",
		"result_top3":    roundResult.Top3,
		"result_top2":    roundResult.Top2,
		"result_bottom2": roundResult.Bottom2,
		"resulted_at":    &now,
	})

	// 7. AIDEV-TODO(farri, 2026-04-21): trigger payout — เทียบ bets + จ่ายเงิน
	// เหมือน admin กรอกผลใน standalone-admin-api (ดู yeekee_settle_handler.go: settleYeekeeBets)
	// ใช้ payout.SettleRound() + GroupWinnersByMember() + operator seamless callback

	log.Printf("✅ Yeekee round %d settled!", yr.RoundNo)
}
