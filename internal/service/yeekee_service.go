// Package service — yeekee_service.go
//
// Business logic สำหรับยี่กี ฝั่ง provider (#7):
//   - HandleShoot: validate + save เลขยิง (callback ของ ws.Hub)
//   - ดึงรอบ + shoots (สำหรับ initial state ของ WebSocket)
//
// ⭐ ต่างจาก standalone:
//   - ใช้ raw gorm query (ยังไม่มี repository layer ใน provider)
//   - ตรวจ operator_id ของ member เพิ่มเติม (ในอนาคต — เช่น operator-ban / operator-scoped rounds)
package service

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/farritpcz/lotto-core/betting"

	"github.com/farritpcz/lotto-provider-game-api/internal/model"
)

// YeekeeService จัดการ logic ยี่กีของ provider
type YeekeeService struct {
	db *gorm.DB
}

// NewYeekeeService สร้าง YeekeeService
func NewYeekeeService(db *gorm.DB) *YeekeeService {
	return &YeekeeService{db: db}
}

// GetRoundWithShoots ดึงรอบ + shoots ที่มีอยู่ (สำหรับ WebSocket initial state)
func (s *YeekeeService) GetRoundWithShoots(yeekeeRoundID int64) (*model.YeekeeRound, []model.YeekeeShoot, error) {
	var round model.YeekeeRound
	if err := s.db.First(&round, yeekeeRoundID).Error; err != nil {
		return nil, nil, errors.New("round not found")
	}

	var shoots []model.YeekeeShoot
	s.db.Where("yeekee_round_id = ?", yeekeeRoundID).
		Order("shot_at ASC").
		Preload("Member").
		Find(&shoots)

	return &round, shoots, nil
}

// HandleShoot ประมวลผลเลขยิงจากสมาชิก (signature ตรงกับ ws.ShootHandler)
//
// Flow:
//  1. Validate เลข 5 หลักด้วย lotto-core/betting
//  2. ตรวจสอบว่ารอบยังเปิดอยู่ + ยังไม่หมดเวลา
//  3. บันทึก yeekee_shoot
//  4. คำนวณ totalSum + shootCount สำหรับ broadcast
func (s *YeekeeService) HandleShoot(roundID int64, memberID int64, number string) (int64, int, error) {
	// 1. Validate เลข 5 หลัก
	if err := betting.ValidateYeekeeShoot(number); err != nil {
		return 0, 0, err
	}

	// 2. ตรวจสอบรอบ
	var round model.YeekeeRound
	if err := s.db.First(&round, roundID).Error; err != nil {
		return 0, 0, errors.New("round not found")
	}
	if round.Status != "shooting" {
		return 0, 0, errors.New("round is not in shooting phase")
	}
	if time.Now().After(round.EndTime) {
		return 0, 0, errors.New("round has ended")
	}

	// 3. บันทึกเลขยิง
	shoot := &model.YeekeeShoot{
		YeekeeRoundID: roundID,
		MemberID:      memberID,
		Number:        number,
		ShotAt:        time.Now(),
	}
	if err := s.db.Create(shoot).Error; err != nil {
		return 0, 0, errors.New("failed to save shoot")
	}

	// 4. คำนวณ totalSum + shootCount
	var totalSum int64
	s.db.Model(&model.YeekeeShoot{}).
		Where("yeekee_round_id = ?", roundID).
		Select("COALESCE(SUM(CAST(number AS UNSIGNED)), 0)").
		Scan(&totalSum)

	var shootCount int64
	s.db.Model(&model.YeekeeShoot{}).
		Where("yeekee_round_id = ?", roundID).
		Count(&shootCount)

	return totalSum, int(shootCount), nil
}
