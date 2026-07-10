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

// hostedCardPAN — синтетический masked-PAN для hosted new-card flow.
// Последние 4 цифры зависят от paymentID, чтобы каждая оплата новой картой
// давала отдельную карту (у PG Add создаст новую запись, а не Bind существующей).
func hostedCardPAN(paymentID uint) string {
	return fmt.Sprintf("5483-18XX-XXXX-%04d", paymentID%10000)
}

// hostedCardToken — детерминированный UUID-токен для hosted new-card flow.
// PG хранит freedompay_token в колонке типа uuid, поэтому токен обязан быть
// валидным UUID (иначе INSERT падает 22P02).
func hostedCardToken(paymentID uint) string {
	return fmt.Sprintf("f0000000-0000-4000-8000-%012x", paymentID)
}

func generateBindToken(paymentID uint) string {
	return fmt.Sprintf("mock-token-%d", paymentID)
}
