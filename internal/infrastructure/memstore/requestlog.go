package memstore

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
)

// RequestLog — кольцевой буфер последних N запросов.
type RequestLog struct {
	mu      sync.RWMutex
	entries []*requestlog.Entry
	cap     int
	nextID  atomic.Uint64
}

// DefaultCapacity — размер ring-буфера по умолчанию.
const DefaultCapacity = 200

// NewRequestLog конструктор.
func NewRequestLog(capacity int) *RequestLog {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &RequestLog{
		entries: make([]*requestlog.Entry, 0, capacity),
		cap:     capacity,
	}
}

// Add добавляет запись. Если переполнен — выбрасывает самую старую.
func (l *RequestLog) Add(e *requestlog.Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.ID = l.nextID.Add(1)
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if len(l.entries) >= l.cap {
		l.entries = append(l.entries[1:], e)
		return
	}
	l.entries = append(l.entries, e)
}

// List возвращает копии записей в обратном порядке (новые первые).
func (l *RequestLog) List() []*requestlog.Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]*requestlog.Entry, 0, len(l.entries))
	for i := len(l.entries) - 1; i >= 0; i-- {
		cp := *l.entries[i]
		out = append(out, &cp)
	}
	return out
}

// Get возвращает запись по ID.
func (l *RequestLog) Get(id uint64) (*requestlog.Entry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, e := range l.entries {
		if e.ID == id {
			cp := *e
			return &cp, true
		}
	}
	return nil, false
}

// Reset очищает журнал.
func (l *RequestLog) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}
