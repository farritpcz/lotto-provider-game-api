# Launch Flow (Game Client) — game-api

> Last updated: 2026-04-20 (v1 initial — starter rule)
> Related code: `internal/handler/router.go`, `internal/handler/stubs.go`

## 🎯 Purpose
Operator ส่ง player มาเล่นเกมผ่าน launch token — player ไม่ login บน provider, ใช้ single-use token แทน

## 📋 Rules
1. **Launch token**: operator gen มาจาก API (HMAC signed) — payload: `player_id, operator_id, currency, expires_at, nonce`
2. **One-time use**: redeem 1 ครั้งเท่านั้น → gen session token แทน (short-lived JWT) — ป้องกัน replay
3. **Session token** เก็บเป็น cookie httpOnly + ส่ง body (ต่างจาก standalone member JWT — ที่นี่ scope ต่อ operator)
4. **Session expiry**: กำหนดจาก operator config (default 2 ชม.) — หมดแล้ว player ต้อง launch ใหม่ผ่าน operator
5. **Player ไม่เห็น operator อื่น**: ทุก API ที่ call ต้องเช็ค `operator_id` จาก session claim match กับ resource
6. **Ban + rate**: ใช้ ban/rate ของ operator (+ admin global ban) — ดู `seamless_wallet.md`

## 🌐 Endpoints (typical)
- GET  `/launch?token=<launch_token>` → redirect ไป game-web `/launch?session=<short_session_token>`
- POST `/api/v1/game/bets` (session token required)
- GET  `/api/v1/game/rounds`, `/results`, `/history`

## ⚠️ Edge Cases
- Launch token หมดอายุ → 401 "launch token expired"
- Launch token ใช้ซ้ำ → 403 "already redeemed"
- Operator suspended ระหว่าง session → reject next API call (401) + แจ้งกลับผ่าน callback
- Currency mismatch: operator แจ้ง currency USD แต่ระบบ support เฉพาะ THB → 400 ตอน launch

## 🔗 Related
- Seamless wallet (operator callback): `seamless_wallet.md`
- Game-web launch page: `lotto-provider-game-web/src/app/launch/page.tsx`
- Yeekee WS: `yeekee_websocket.md`

## 📝 Change Log
- 2026-04-20: v1 initial skeleton
