// Package middleware — nonce_store.go
//
// In-memory nonce cache สำหรับ HMAC replay protection
//
// ⭐ กลไก:
//  - นับทุก (api_key, signature, timestamp) เป็น nonce unique
//  - ถ้าเจอซ้ำใน TTL (5 นาที) → reject
//  - cleanup ทุก 1 นาที เพื่อไม่ให้ memory โต
//
// ⚠️ จำกัด:
//  - in-memory → ถ้ามี server หลายตัวต้องใช้ Redis
//  - เพียงพอสำหรับ single-server deploy ปัจจุบัน
package middleware

import (
	"sync"
	"time"
)

// nonceStore — singleton cache เก็บ nonce ที่เห็นไปแล้ว
type nonceStore struct {
	mu    sync.Mutex
	seen  map[string]time.Time // key = nonce, value = expires_at
	ttl   time.Duration
}

var globalNonceStore = newNonceStore(5 * time.Minute)

func newNonceStore(ttl time.Duration) *nonceStore {
	s := &nonceStore{
		seen: make(map[string]time.Time),
		ttl:  ttl,
	}
	// cleanup goroutine — ลบ nonce ที่หมดอายุทุก 1 นาที
	go s.cleanupLoop()
	return s
}

// checkAndStore — ถ้า nonce เคยเห็น + ยังไม่หมดอายุ → return false (replay!)
// ถ้ายังไม่เห็น → เก็บไว้ + return true
func (s *nonceStore) checkAndStore(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if expiry, exists := s.seen[nonce]; exists && now.Before(expiry) {
		return false // replay detected
	}
	s.seen[nonce] = now.Add(s.ttl)
	return true
}

func (s *nonceStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.cleanup()
	}
}

func (s *nonceStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.seen {
		if now.After(exp) {
			delete(s.seen, k)
		}
	}
}

// CheckNonce — public helper ใช้ใน hmac_auth.go
// คืน false ถ้า signature นี้เคยถูกใช้แล้ว (replay attack)
func CheckNonce(apiKey, signature, timestamp string) bool {
	nonce := apiKey + "|" + signature + "|" + timestamp
	return globalNonceStore.checkAndStore(nonce)
}
