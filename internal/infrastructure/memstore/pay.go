// Package memstore — in-memory репозитории для всех доменных сущностей мока.
package memstore

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vevovip/chaospay/internal/domain/pay"
)

// ErrPaymentNotFound возвращается когда платёж не найден.
var ErrPaymentNotFound = errors.New("payment not found")

// ErrInvalidTransition возвращается при попытке недопустимого перехода статуса.
var ErrInvalidTransition = errors.New("invalid status transition")

// PayRepo — in-memory хранилище карточных платежей.
type PayRepo struct {
	mu      sync.RWMutex
	byID    map[uint]*pay.Record
	byOrder map[uint]uint
	nextID  atomic.Uint64
	refSeed atomic.Uint64
}

// NewPayRepo конструктор.
func NewPayRepo() *PayRepo {
	r := &PayRepo{
		byID:    make(map[uint]*pay.Record),
		byOrder: make(map[uint]uint),
	}
	// стартуем с большого числа, чтобы paymentID выглядел реалистично (как у Freedom: 1.7e9)
	r.nextID.Store(1700000000)
	return r
}

// NextPaymentID возвращает новый paymentID.
func (r *PayRepo) NextPaymentID() uint {
	return uint(r.nextID.Add(1))
}

// NextReference генерирует короткий числовой reference (auth code).
func (r *PayRepo) NextReference() uint {
	r.refSeed.Add(1)
	return uint(time.Now().UnixMilli()%1_000_000_000) + uint(r.refSeed.Load()%1000) //nolint:gosec
}

// Create добавляет новый Record.
func (r *PayRepo) Create(rec *pay.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	if rec.Status == "" {
		rec.Status = pay.StatusNew
	}
	if rec.Currency == "" {
		rec.Currency = "KZT"
	}
	rec.History = append(rec.History, pay.HistoryEntry{At: rec.CreatedAt, To: rec.Status, Reason: "create"})
	r.byID[rec.PaymentID] = rec
	if rec.OrderID != 0 {
		r.byOrder[rec.OrderID] = rec.PaymentID
	}
}

// Get возвращает копию записи.
func (r *PayRepo) Get(paymentID uint) (*pay.Record, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.byID[paymentID]
	if !ok {
		return nil, ErrPaymentNotFound
	}
	return rec.Clone(), nil
}

// List возвращает копии всех записей в обратном хронологическом порядке.
func (r *PayRepo) List() []*pay.Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*pay.Record, 0, len(r.byID))
	for _, rec := range r.byID {
		out = append(out, rec.Clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Update применяет fn внутри блокировки. fn может менять поля и возвращать (newStatus, reason, err).
// Если newStatus совпадает с текущим — переход не записывается в историю.
func (r *PayRepo) Update(paymentID uint, fn func(rec *pay.Record) (pay.Status, string, error)) (*pay.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.byID[paymentID]
	if !ok {
		return nil, ErrPaymentNotFound
	}
	from := rec.Status
	to, reason, err := fn(rec)
	if err != nil {
		return nil, err
	}
	if to != "" && to != from {
		rec.Status = to
		rec.History = append(rec.History, pay.HistoryEntry{At: time.Now(), From: from, To: to, Reason: reason})
	}
	return rec.Clone(), nil
}

// Transition выполняет проверенный переход статуса.
// Если allowedFrom != nil и текущий статус не в map — возвращает ErrInvalidTransition.
func (r *PayRepo) Transition(paymentID uint, allowedFrom map[pay.Status]bool, to pay.Status, reason string) (*pay.Record, error) {
	return r.Update(paymentID, func(rec *pay.Record) (pay.Status, string, error) {
		if allowedFrom != nil && !allowedFrom[rec.Status] {
			return rec.Status, "", ErrInvalidTransition
		}
		switch to {
		case pay.StatusAuthorized:
			rec.AuthorizedAt = time.Now()
			if rec.Reference == 0 {
				rec.Reference = r.NextReference()
			}
		case pay.StatusCaptured:
			rec.CapturedAt = time.Now()
			if rec.Captured == 0 {
				rec.Captured = rec.Amount
			}
		}
		return to, reason, nil
	})
}

// MarkWebhookSent — помечает запись отправленным webhook.
func (r *PayRepo) MarkWebhookSent(paymentID uint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.byID[paymentID]; ok {
		rec.WebhookSent = true
	}
}

// Reset очищает store.
func (r *PayRepo) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID = make(map[uint]*pay.Record)
	r.byOrder = make(map[uint]uint)
}
