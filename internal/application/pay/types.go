package pay

import (
	"errors"
	"fmt"
)

// ErrAlreadyAuthorized — повторный Hold на принятом платеже (инцидент EX-1001).
var ErrAlreadyAuthorized = errors.New("payment already authorized")

// ErrInvalidState — невалидный статус для операции.
var ErrInvalidState = errors.New("invalid state for operation")

// ErrEpayWebhookNotConfigured — попытка отправить Epay-postlink без сконфигурированного клиента.
var ErrEpayWebhookNotConfigured = errors.New("epay webhook is not configured")

// HoldInitInput — параметры HoldInit.
type HoldInitInput struct {
	OrderID        uint
	MerchantID     uint
	TerminalID     int
	UserID         uint
	Amount         uint
	Currency       string
	CardToken      string
	Description    string
	IdempotencyKey string
	UserPhone      string
	UserEmail      string
}

// HostedInput — параметры init_payment.php.
type HostedInput struct {
	OrderID     uint
	MerchantID  uint
	TerminalID  int
	UserID      uint
	Amount      uint
	Currency    string
	Description string
	UserPhone   string
	UserEmail   string
	RedirectURL string
}

// BindInput — параметры cardstorage/add2.
type BindInput struct {
	OrderID     uint
	MerchantID  uint
	TerminalID  int
	UserID      uint
	PostLink    string
	RedirectURL string
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func defaultCardPAN() string {
	return "5483-18XX-XXXX-0293"
}

func generateBindToken(paymentID uint) string {
	return fmt.Sprintf("mock-token-%d", paymentID)
}
