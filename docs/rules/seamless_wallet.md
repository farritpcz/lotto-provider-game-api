# Seamless Wallet — provider-game-api (#7)

> Last updated: 2026-04-21 (v2 — seamless debit + settle-time credit + callbacks wired)
> Related code:
>   - `internal/handler/router.go` (operator group)
>   - `internal/handler/stubs.go` — SeamlessBalance/Debit/Credit, TransferDeposit/Withdraw, GamePlaceBets (เพิ่ม branch seamless/transfer)
>   - `internal/service/wallet_service.go` — WalletService (SeamlessBalance/Debit/Credit) + CallbackService (NotifyBetResult/NotifyRoundEvent)
>   - `internal/service/settle_service.go` — SettleRound (payout per operator.wallet_type)
>   - `internal/middleware/hmac.go` — HMACAuthWithDB
>   - `internal/middleware/ratelimit.go`
> Status: ✅ seamless mode active — GamePlaceBets branches on operator.wallet_type; settle payout uses SeamlessCredit for seamless operators, transfer otherwise

## Purpose
Wallet API คล้าย PG/JILI — operator เรียกจาก server ของตัวเอง (HMAC signed) เพื่อเช็คยอด/หัก/เติม ให้กับ player (member ใน DB ของ provider)

## Rules
1. ทุก request ผ่าน `HMACAuthWithDB(h.DB)` middleware + `RateLimitMiddleware` (100 req/s per operator, burst 200)
2. Identify operator ด้วย `api_key` header → middleware load operator จาก DB + validate IP whitelist + HMAC signature
3. Member key = `(operator_id, external_player_id)` — ห้าม query ข้าม operator
4. `SeamlessBalance`: auto-register member ถ้ายังไม่มี (balance 0, status active)
5. `SeamlessDebit`: ต้องเป็น atomic — `UPDATE ... WHERE balance >= amount`, ถ้า `RowsAffected == 0` → 400 `"insufficient balance"`
6. `TxnID` ต้อง idempotent key — request เดิมไม่หักซ้ำ (TODO: ยังไม่ enforce ในโค้ด)
7. ⭐ Seamless mode จริง: provider ต้อง callback operator URL (`operator.CallbackURL`) ตอน bet settle — ไม่ใช่เก็บเงินเอง (ปัจจุบัน fallback ไป transfer mode)

## API / Endpoints
- `POST /api/v1/wallet/balance`  — body `{ player_id }`
- `POST /api/v1/wallet/debit`    — body `{ player_id, amount, txn_id }`
- `POST /api/v1/wallet/credit`   — body `{ player_id, amount, txn_id }`
- `POST /api/v1/wallet/deposit`  — transfer mode
- `POST /api/v1/wallet/withdraw` — transfer mode
- `POST /api/v1/games/launch`    — create launch token (ดูใน yeekee_websocket.md / game-web launch page)

## Edge Cases
- Duplicate `txn_id` → ต้อง return result เดิม ไม่หัก/เติมซ้ำ
- Amount <= 0 → 400
- Player ถูก ban (`status != active`) → 403
- Operator ถูก suspend → middleware ควร block ก่อนถึง handler
- Rate limit เกิน → 429

## Related
- Middleware: `internal/middleware/hmac.go`, `ratelimit.go`
- Config ของ key ถูกตั้งที่: `lotto-provider-backoffice-api` (#9) operator handlers
- Callback URL ตั้งที่ operator-web `/callbacks`

## Wallet mode resolution
- `operators.wallet_type` = `'seamless'` | `'transfer'` (default from column definition)
- **GamePlaceBets** (place bet):
  - seamless → `WalletService.SeamlessDebit(op.CallbackURL, op.SecretKey, ...)` — operator เป็นคนหัก. `member.Balance` ใช้ค่าที่ operator ส่งกลับ
  - transfer → UPDATE local `members.balance` (ต้องเช็ค `balance >= amount` + `RowsAffected > 0`)
- **settle (yeekee cron + admin manual)**:
  - seamless → `SeamlessCredit` (fire best-effort, log warn ถ้า HTTP fail — ไม่ rollback bet updates)
  - transfer → UPDATE local balance + INSERT Transaction
- **NotifyBetResult** — fire หลัง commit (async) ไปทุก bet รวม win และ lost (ให้ operator มีประวัติครบ)

## Idempotency
- `txn_id` — convention:
  - debit:  `bet-{round_id}-{number}-{external_player_id}`
  - credit: `win-{round_id}-{member_id}`
- operator ควร dedupe ด้วย txn_id นี้ (TODO: provider ยังไม่ enforce replay-protection ด้วย local txn_id log)

## Change Log
- 2026-04-20: v1 initial skeleton
- 2026-04-21: v2 — seamless wallet wired end-to-end (Tier B). `GamePlaceBets` branches on `operator.wallet_type`. Settle payout via `SettleService` (transfer: local balance; seamless: SeamlessCredit). Callbacks fire post-commit.
