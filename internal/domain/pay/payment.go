// Package pay содержит доменные типы карточного платежа Freedom Pay.
package pay

import "time"

// Status — статус карточного платежа в моке.
type Status string

// Все статусы карточного платежа.
const (
	StatusNew             Status = "NEW"
	StatusHoldPending     Status = "HOLD_PENDING"
	StatusAuthorized      Status = "AUTHORIZED"
	StatusCaptured        Status = "CAPTURED"
	StatusCancelled       Status = "CANCELLED"
	StatusRefunded        Status = "REFUNDED"
	StatusPartialRefunded Status = "PARTIAL_REFUNDED"
	StatusFailed          Status = "FAILED"
)

// IsTerminal возвращает true для финальных статусов.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCaptured, StatusCancelled, StatusRefunded, StatusFailed:
		return true
	}
	return false
}

// Kind различает потоки оплаты.
type Kind string

// Все типы платежа.
const (
	KindCard      Kind = "card"       // /v1/merchant/{id}/card/{init,direct} — saved-card
	KindHosted    Kind = "hosted"     // /init_payment.php — PayPage
	KindApplePay  Kind = "apple_pay"  // /pay/{id}/pay JSON
	KindGooglePay Kind = "google_pay" // /pay/{id}/pay form
	KindBind      Kind = "bind"       // /cardstorage/add2
)

// HistoryEntry — запись об изменении состояния (для UI журнала).
type HistoryEntry struct {
	At     time.Time
	From   Status
	To     Status
	Reason string
}

// Record — состояние карточного платежа в моке.
type Record struct {
	PaymentID  uint
	OrderID    uint
	MerchantID uint
	TerminalID int
	UserID     uint
	Kind       Kind

	Amount   uint
	Currency string
	Captured uint
	Refunded uint

	Reference      uint
	Description    string
	IdempotencyKey string

	CardToken string
	CardPAN   string
	CardOwner string
	CardBrand string
	CardExp   string
	CardMonth string
	CardYear  string

	UserPhone string
	UserEmail string

	HostedRedirectURL string
	BindRedirectURL   string

	Status      Status
	LastError   string
	History     []HistoryEntry
	WebhookSent bool

	CreatedAt    time.Time
	AuthorizedAt time.Time
	CapturedAt   time.Time
}

// Clone возвращает глубокую копию записи (история — отдельный slice).
func (r *Record) Clone() *Record {
	cp := *r
	cp.History = append([]HistoryEntry(nil), r.History...)
	return &cp
}

// AllowedTransitions описывает, из каких статусов разрешён переход в указанный target.
// Возврат nil = переход допустим из любого состояния (panel force-action).
func AllowedTransitions(target Status) map[Status]bool {
	switch target {
	case StatusAuthorized:
		return map[Status]bool{StatusNew: true, StatusHoldPending: true}
	case StatusCaptured:
		return map[Status]bool{StatusAuthorized: true}
	case StatusCancelled:
		return map[Status]bool{StatusAuthorized: true}
	case StatusRefunded:
		return map[Status]bool{StatusCaptured: true, StatusPartialRefunded: true}
	case StatusPartialRefunded:
		return map[Status]bool{StatusCaptured: true, StatusPartialRefunded: true}
	}
	return nil
}
