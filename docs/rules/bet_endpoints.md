# Bet Endpoints (Game Client) — game-api

> Last updated: 2026-04-20 (v1 initial — starter rule)
> Related code: `internal/handler/stubs.go`, `internal/handler/router.go`

## 🎯 Purpose
Player (ผ่าน game-web) วางเดิมพัน/เช็คเลข/ดูประวัติ — ผ่าน session token (ไม่ใช่ launch token โดยตรง)

## 📋 Rules
1. **Auth**: session JWT (จาก launch redeem, ดู `launch_flow.md`) — middleware ตรวจ `operator_id + player_id`
2. **Scope**: ทุก query filter `operator_id` + `player_id` ของ session
3. **Place bet flow**:
   - Validate: round open, bet_type ถูก, number format ตาม lottery_type
   - Check ban/rate: admin global + operator-specific
   - Deduct wallet via operator callback (seamless — ดู `seamless_wallet.md`)
   - ถ้า wallet deduct fail → reject 402 "insufficient balance"
   - INSERT bet + return bet_id
4. **Cancel**: player **ห้าม** cancel เอง (ต่างจาก standalone) — ต้อง operator เป็นคน cancel ผ่าน admin API
5. **History**: pagination + filter (status/round/date) — scope ด้วย player_id

## 🌐 Endpoints (ตามที่ใช้จริง — ดู `stubs.go`)
- POST `/api/v1/game/bets`            → place bet (single หรือ batch)
- POST `/api/v1/game/bets/check`      → pre-check ban/rate/amount
- GET  `/api/v1/game/bets`            → my history (paginated)
- GET  `/api/v1/game/rounds/open`     → current open rounds
- GET  `/api/v1/game/results`         → รูปการออกผลของรอบที่ settle แล้ว

## ⚠️ Edge Cases
- Wallet sync ล่าช้า (seamless) → retry ฝั่ง backend; UI โชว์ "กำลังดำเนินการ"
- Round cutoff ระหว่าง submit → reject; client retry round ใหม่
- Rate ลดระหว่าง submit (rare) → rate lock ณ place time (snapshot ลง bet row)

## 🔗 Related
- Launch: `launch_flow.md`
- Seamless wallet: `seamless_wallet.md`
- Yeekee: `yeekee_websocket.md`
- Game-web bet UI: `lotto-provider-game-web/src/app/lottery/[type]/page.tsx`

## 📝 Change Log
- 2026-04-20: v1 initial skeleton
