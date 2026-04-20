# Rules — lotto-provider-game-api (#7)

> Last updated: 2026-04-20
> Source of truth: ทุก feature ของ repo นี้ต้องมี rule file ใน folder นี้
> Cross-repo standards: `../../../lotto-system/docs/coding_standards.md`

## วิธีใช้
- ทุก feature ต้องมี `{feature}.md` เก็บ rules + edge cases
- แก้โค้ด feature ไหน → update rule file ในคอมมิตเดียวกัน (BLOCKING)
- Format: ดูตัวอย่าง `C:/project/lotto-standalone-admin-api/docs/rules/member_levels.md`

## Rules ปัจจุบัน
- [seamless_wallet.md](./seamless_wallet.md) — Operator API: balance / debit / credit / transfer deposit-withdraw (HMAC + rate limit)
- [yeekee_websocket.md](./yeekee_websocket.md) — WebSocket yeekee real-time hub

## Related repos
- Client iframe: `lotto-provider-game-web` (#8)
- Share DB: `lotto-provider-backoffice-api` (#9)
- Similar standalone: `lotto-standalone-member-api` (#3)

## Status
WIP — handlers หลายตัวยังเป็น stub, Seamless `debit` ยัง fallback เป็น transfer mode
