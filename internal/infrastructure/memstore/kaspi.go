package memstore

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vevovip/chaospay/internal/domain/kaspi"
)

// ErrKaspiNotFound — Kaspi-платёж не найден.
var ErrKaspiNotFound = errors.New("kaspi payment not found")

// kaspiIDBase — стартовое значение счётчика PaymentId, близко к реальным Kaspi id.
const kaspiIDBase = 16600000000

// KaspiRepo — in-memory хранилище Kaspi-платежей.
type KaspiRepo struct {
	mu        sync.RWMutex
	store     map[int]*kaspi.Payment
	idCounter atomic.Int64
}

// NewKaspiRepo конструктор.
func NewKaspiRepo() *KaspiRepo {
	r := &KaspiRepo{store: make(map[int]*kaspi.Payment)}
	r.idCounter.Store(kaspiIDBase)

	return r
}

// Create создаёт платёж в статусе Wait и возвращает его копию.
func (r *KaspiRepo) Create(externalID string, amount float64) *kaspi.Payment {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := int(r.idCounter.Add(1))
	p := &kaspi.Payment{
		PaymentID:  id,
		ExternalID: externalID,
		Amount:     amount,
		Status:     kaspi.StatusWait,
		CreatedAt:  time.Now(),
	}
	r.store[id] = p

	cp := *p

	return &cp
}

// Get возвращает копию платежа по PaymentId.
func (r *KaspiRepo) Get(paymentID int) (*kaspi.Payment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.store[paymentID]
	if !ok {
		return nil, ErrKaspiNotFound
	}

	cp := *p

	return &cp, nil
}

// List возвращает все платежи, новые сверху.
func (r *KaspiRepo) List() []*kaspi.Payment {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*kaspi.Payment, 0, len(r.store))
	for _, p := range r.store {
		cp := *p
		out = append(out, &cp)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].PaymentID > out[j].PaymentID
	})

	return out
}

// SetStatus меняет статус платежа и возвращает его копию.
func (r *KaspiRepo) SetStatus(paymentID int, status kaspi.Status) (*kaspi.Payment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.store[paymentID]
	if !ok {
		return nil, ErrKaspiNotFound
	}

	p.Status = status
	cp := *p

	return &cp, nil
}
