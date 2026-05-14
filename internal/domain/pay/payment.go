// Package pay содержит доменные типы карточного платежа.
//
// Структура нейтральна к банку: один Record описывает платёж независимо от того,
// Freedom это или Halyk Epay. Банк-специфичные поля помечены в комментариях.
package pay

import (
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
)

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
	// Freedom Pay
	KindCard      Kind = "card"       // /v1/merchant/{id}/card/{init,direct} — saved-card
	KindHosted    Kind = "hosted"     // /init_payment.php — PayPage
	KindApplePay  Kind = "apple_pay"  // /pay/{id}/pay JSON
	KindGooglePay Kind = "google_pay" // /pay/{id}/pay form
	KindBind      Kind = "bind"       // /cardstorage/add2
	// Halyk Epay v2
	KindEpayCard     Kind = "epay_card"  // /api/payment/cryptopay — новая карта (с cryptogram)
	KindEpayPay      Kind = "epay_pay"   // /api/payments/cards/auth — сохранённая карта (cardId+accountId)
	KindEpayApplePay Kind = "epay_apple" // /api/payment/cryptopay с paymentType=applePay
	KindEpayBind     Kind = "epay_bind"  // привязка карты через cryptopay (cardSave=true)
)

// HistoryEntry — запись об изменении состояния (для UI журнала).
type HistoryEntry struct {
	At     time.Time
	From   Status
	To     Status
	Reason string
}

// Record — состояние карточного платежа в моке.
//
// Bank определяет, какой набор полей актуален:
//   - bank.Freedom: PaymentID (числовой), CardToken, Reference (auth code).
//   - bank.Epay:    EpayID (UUID), EpayCardID, EpayAccountID, Reference как RRN.
type Record struct {
	Bank       bank.Bank
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

	// Epay v2 specific. Для Freedom — пусты.
	EpayID                 string // UUID операции в Halyk Epay (например be127a53-8570-4584-b313-9d210888b91a)
	EpayInvoiceID          string // invoiceId — обычно = strconv(OrderID) с паддингом нулями (≥6 символов)
	EpayCardID             string // cardId — UUID карты в Halyk
	EpayAccountID          string // accountId — внешний идентификатор пользователя на стороне Halyk
	EpayClientID           string // OAuth client_id, под которым выпущен токен
	EpayTerminalID         string // terminalId (UUID, отличается от Freedom TerminalID int)
	EpayCallbackURL        string // postlink — успех
	EpayFailureCallbackURL string // failurePostlink — ошибка
	EpayPaymentType        string // "cardId" / "applePay" — из cryptopay/auth запроса

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
