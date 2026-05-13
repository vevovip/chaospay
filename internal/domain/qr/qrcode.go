// Package qr содержит доменные типы QR-PAY (Single QR).
package qr

import "time"

// TTL — время жизни QR.
const TTL = 5 * time.Minute

// Status — статус QR.
type Status string

// Все статусы QR.
const (
	StatusNew       Status = "NEW"
	StatusScanned   Status = "SCANNED"
	StatusSuccess   Status = "SUCCESS"
	StatusExpired   Status = "EXPIRED"
	StatusCancelled Status = "CANCELLED"
	StatusError     Status = "ERROR"
)

// IsTerminal — финальный статус, после которого изменения не разрешены.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSuccess, StatusExpired, StatusCancelled, StatusError:
		return true
	}
	return false
}

// AllStatuses — для итерации в UI.
var AllStatuses = []Status{StatusNew, StatusScanned, StatusSuccess, StatusExpired, StatusCancelled, StatusError}

// Code — состояние QR.
type Code struct {
	UUID        string
	Status      Status
	Amount      float64
	BIN         string
	TID         string
	MID         string
	TrnID       int64
	TrnDate     string
	QRBase64    string
	PaymentURL  string
	WebhookSent bool
	IsRefund    bool

	RefundedReference   string
	RefundedParentTrnID string
	RefundedAmount      float64

	CreatedAt time.Time
}

// RefundTransaction — элемент списка transactions[] при SCANNED refund-QR.
type RefundTransaction struct {
	Reference     string  `json:"reference"`
	OperationDate string  `json:"operation_date"`
	Amount        float64 `json:"amount"`
	TrnID         int64   `json:"trnID"`
}
