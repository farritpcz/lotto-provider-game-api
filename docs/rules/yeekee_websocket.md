# Yeekee WebSocket — provider-game-api (#7)

> Last updated: 2026-04-20 (v1 initial — starter rule, expand as feature matures)
> Related code: `internal/ws/yeekee_hub.go`, `internal/job/yeekee_cron.go`, `internal/handler/router.go` (`WS /api/v1/game/yeekee/ws/:roundId`)
> Status: WIP — hub มีแล้ว, integrate กับ lotto-core `betting.ValidateYeekeeShoot()`

## Purpose
Real-time broadcast ให้ player ใน iframe (#8) เห็นเลขยิง/ผลรอบยี่กี — hub จัดการ connection pool per roundId

## Rules
1. Endpoint: `WS /api/v1/game/yeekee/ws/:roundId` — auth ด้วย launch token (ไม่ใช่ HMAC — นี่คือ Game Client API)
2. ใช้ `lotto-core betting.ValidateYeekeeShoot()` ตรวจเลขยิงก่อน broadcast (ห้าม duplicate logic)
3. Hub scope per `roundId` — ต้องปิด/ล้าง connection เมื่อรอบปิด (cron `yeekee_cron.go`)
4. ทุก message ต้อง tag `operator_id` ของ player — ห้าม leak ข้ามระหว่าง operator (ถ้าต้องมี channel แยก operator ก็แยก)
5. launch token ต้อง verify ด้วย `LaunchTokenSecret` (inject ผ่าน `NewHandler(launchTokenSecret)`)
6. Reconnect: client ต้องรับ snapshot state ปัจจุบันของรอบ (เลขที่ยิงมาแล้ว) ก่อนรับ stream ต่อ

## API / Endpoints
- `WS   /api/v1/game/yeekee/ws/:roundId` — Game client
- `GET  /api/v1/game/rounds/:typeId` — list open rounds
- `POST /api/v1/game/bets`, `GET /api/v1/game/bets`
- `GET  /api/v1/game/results`, `/api/v1/game/history`, `/api/v1/game/balance`

## Edge Cases
- รอบปิดแล้วยังมี client connect → hub ต้องส่ง final result แล้ว close
- Token หมดอายุระหว่าง connection → server close WebSocket พร้อม code ที่สื่อ re-auth
- High concurrency: ใช้ channel + mutex (ดู pattern ใน yeekee_hub.go)
- ยิงเลขช่วง frozen window (ใกล้ปิดรอบ) — reject ด้วย validator

## Related
- Core logic: `lotto-core/betting/yeekee.go` (ValidateYeekeeShoot)
- Cron: `internal/job/yeekee_cron.go` — เปิด/ปิดรอบอัตโนมัติ
- Frontend: `lotto-provider-game-web/src/app/yeekee/room/` (และ `play/`)

## Change Log
- 2026-04-20: v1 initial skeleton
