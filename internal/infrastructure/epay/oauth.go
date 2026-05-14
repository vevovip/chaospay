package epay

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TokenInfo — информация о выпущенном токене (для отладки/UI).
type TokenInfo struct {
	AccessToken  string
	RefreshToken string
	ClientID     string
	InvoiceID    string
	Amount       string
	Terminal     string
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// TokenStore — потокобезопасный mock-стор OAuth-токенов.
//
// В реальном Halyk токен short-lived (1 час) и привязан к invoice+amount+terminal.
// В моке мы:
//   - Выдаём новый токен на каждый /oauth2/token (даже если invoiceID повторяется).
//   - Принимаем любой непустой Bearer в последующих запросах (без сверки invoice).
//   - Сохраняем последние N токенов для UI/Logging.
//
// Это сознательное упрощение — мы тестируем PG, а не строгий контракт Halyk OAuth.
type TokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*TokenInfo
}

// NewTokenStore конструктор.
func NewTokenStore() *TokenStore {
	return &TokenStore{tokens: make(map[string]*TokenInfo)}
}

// Issue выпускает новый токен и сохраняет его.
func (s *TokenStore) Issue(clientID, invoiceID, amount, terminal string) *TokenInfo {
	now := time.Now()
	info := &TokenInfo{
		AccessToken:  randomToken(32),
		RefreshToken: randomToken(32),
		ClientID:     clientID,
		InvoiceID:    invoiceID,
		Amount:       amount,
		Terminal:     terminal,
		IssuedAt:     now,
		ExpiresAt:    now.Add(time.Hour),
	}
	s.mu.Lock()
	s.tokens[info.AccessToken] = info
	s.mu.Unlock()
	return info
}

// Lookup возвращает info по access_token. nil если не найден.
// Мок-режим: если в стор пусто, мы возвращаем "permissive" stub —
// чтобы PG-клиент с захардкоженным dev-токеном тоже работал.
func (s *TokenStore) Lookup(token string) *TokenInfo {
	if token == "" {
		return nil
	}
	s.mu.RLock()
	info := s.tokens[token]
	s.mu.RUnlock()
	if info != nil {
		return info
	}
	return &TokenInfo{
		AccessToken: token,
		ClientID:    "mock",
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
}

// Reset очищает store.
func (s *TokenStore) Reset() {
	s.mu.Lock()
	s.tokens = make(map[string]*TokenInfo)
	s.mu.Unlock()
}

// List возвращает копии всех выданных токенов (для UI Settings).
func (s *TokenStore) List() []*TokenInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*TokenInfo, 0, len(s.tokens))
	for _, t := range s.tokens {
		cp := *t
		out = append(out, &cp)
	}
	return out
}

func randomToken(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand на любой современной системе ошибок не даёт. fallback на timestamp.
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}
	return hex.EncodeToString(buf)
}
