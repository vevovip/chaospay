package memstore

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vevovip/chaospay/internal/domain/qr"
)

// ErrQRNotFound — QR не найден.
var ErrQRNotFound = errors.New("qr not found")

// ErrQRTerminal — попытка изменить уже-терминальный QR.
var ErrQRTerminal = errors.New("qr already in terminal state")

// ErrRefundAlreadyConfirmed — повторное подтверждение возврата (HTTP 410 на стороне банка).
var ErrRefundAlreadyConfirmed = errors.New("refund already confirmed")

// ErrRefundNotScanned — confirm-refund при не-SCANNED статусе.
var ErrRefundNotScanned = errors.New("refund confirm requires SCANNED status")

// ErrNotRefundQR — UUID не относится к refund-QR.
var ErrNotRefundQR = errors.New("uuid is not a refund QR")

// QRRepo — in-memory хранилище QR.
type QRRepo struct {
	mu         sync.RWMutex
	store      map[string]*qr.Code
	trnCounter atomic.Int64
}

// NewQRRepo конструктор.
func NewQRRepo() *QRRepo {
	return &QRRepo{store: make(map[string]*qr.Code)}
}

// Create сохраняет QR.
func (r *QRRepo) Create(code *qr.Code) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if code.CreatedAt.IsZero() {
		code.CreatedAt = time.Now()
	}
	if code.Status == "" {
		code.Status = qr.StatusNew
	}
	r.store[code.UUID] = code
}

// Get возвращает копию QR по UUID.
func (r *QRRepo) Get(uuid string) (*qr.Code, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.store[uuid]
	if !ok {
		return nil, ErrQRNotFound
	}
	cp := *c
	return &cp, nil
}

// UpdateStatus меняет статус, генерирует TrnID/TrnDate при SUCCESS.
func (r *QRRepo) UpdateStatus(uuid string, status qr.Status) (*qr.Code, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.store[uuid]
	if !ok {
		return nil, ErrQRNotFound
	}
	if c.Status.IsTerminal() {
		return nil, ErrQRTerminal
	}
	c.Status = status
	if status == qr.StatusSuccess {
		c.TrnID = time.Now().UnixMilli() + r.trnCounter.Add(1)
		c.TrnDate = time.Now().Format("2006-01-02T15:04:05")
	}
	cp := *c
	return &cp, nil
}

// MarkWebhookSent помечает QR как отправленный webhook (только для терминальных).
func (r *QRRepo) MarkWebhookSent(uuid string) (*qr.Code, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.store[uuid]
	if !ok {
		return nil, ErrQRNotFound
	}
	if !c.Status.IsTerminal() {
		return nil, ErrQRTerminal
	}
	c.WebhookSent = true
	cp := *c
	return &cp, nil
}

// List возвращает все QR-коды (новые первые).
func (r *QRRepo) List() []*qr.Code {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*qr.Code, 0, len(r.store))
	for _, c := range r.store {
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Reset очищает store.
func (r *QRRepo) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = make(map[string]*qr.Code)
}

// ListSuccessfulPayments возвращает успешные оплатные QR того же мерчанта.
// Используется для transactions[] при SCANNED refund-QR.
func (r *QRRepo) ListSuccessfulPayments(bin, tid, mid string) []*qr.Code {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*qr.Code, 0)
	for _, c := range r.store {
		if c.IsRefund || c.Status != qr.StatusSuccess {
			continue
		}
		if c.BIN != bin || c.TID != tid || c.MID != mid {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// ApplyRefundConfirmation фиксирует данные оплаты в refund-QR и переводит в SUCCESS.
func (r *QRRepo) ApplyRefundConfirmation(uuid, reference, parentTrnID string, amount float64) (*qr.Code, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.store[uuid]
	if !ok {
		return nil, ErrQRNotFound
	}
	if !c.IsRefund {
		return nil, ErrNotRefundQR
	}
	if c.Status == qr.StatusSuccess {
		return nil, ErrRefundAlreadyConfirmed
	}
	if c.Status != qr.StatusScanned {
		return nil, ErrRefundNotScanned
	}
	c.Status = qr.StatusSuccess
	c.TrnID = time.Now().UnixMilli() + r.trnCounter.Add(1)
	c.TrnDate = time.Now().Format("2006-01-02T15:04:05")
	c.Amount = amount
	c.RefundedReference = reference
	c.RefundedParentTrnID = parentTrnID
	c.RefundedAmount = amount
	cp := *c
	return &cp, nil
}
