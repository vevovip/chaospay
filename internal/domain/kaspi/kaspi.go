// Package kaspi содержит доменные типы мока KaspiPay (polling-based).
package kaspi

import "time"

// Status — статус платежа KaspiPay, ровно как банк отдаёт в /payment/status.
type Status string

// Статусы платежа Kaspi.
const (
	StatusWait      Status = "Wait"
	StatusProcessed Status = "Processed"
	StatusError     Status = "Error"
)

// AllStatuses — для UI/итерации.
var AllStatuses = []Status{StatusWait, StatusProcessed, StatusError}

// IsTerminal — финальный статус, после которого изменения не имеют смысла.
func (s Status) IsTerminal() bool {
	return s == StatusProcessed || s == StatusError
}

// Payment — состояние Kaspi-платежа в моке.
type Payment struct {
	PaymentID  int
	ExternalID string
	Amount     float64
	Status     Status
	CreatedAt  time.Time
}
