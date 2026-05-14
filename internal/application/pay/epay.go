package pay

import (
	"errors"
	"fmt"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
)

// EpayAuthorizeInput — параметры авторизации платежа (Cryptopay для новой карты
// или Card Auth для сохранённой).
type EpayAuthorizeInput struct {
	OrderID         uint
	Amount          int // в тенге (Halyk не использует копейки для KZT в этих эндпоинтах)
	Currency        string
	InvoiceID       string
	TerminalID      string
	AccountID       string
	CardID          string // если задан → платёж сохранённой картой (Kind=KindEpayPay)
	PaymentType     string // "cardId" / "applePay" / ""
	Description     string
	Email           string
	Phone           string
	Postlink        string
	FailurePostlink string
	CardSave        bool
	ClientID        string // OAuth client_id (для трассировки)
	HasCryptogram   bool   // true если запрос пришёл с cryptogram (новая карта или Apple Pay)
}

// EpayAuthorize создаёт запись Halyk-платежа в статусе Authorized (без 3DS-челленджа).
// 3DS-челлендж эмулируется сценарием Force3DS на стороне порта (не меняет state).
func (s *Service) EpayAuthorize(in EpayAuthorizeInput) (*pay.Record, error) {
	if in.Amount <= 0 {
		return nil, errors.New("amount must be positive")
	}
	kind := pay.KindEpayPay
	if in.HasCryptogram {
		if in.PaymentType == "applePay" {
			kind = pay.KindEpayApplePay
		} else if in.CardSave {
			kind = pay.KindEpayBind
		} else {
			kind = pay.KindEpayCard
		}
	}

	currency := defaultStr(in.Currency, "KZT")
	invoiceID := in.InvoiceID

	rec := &pay.Record{
		Bank:                   bank.Epay,
		PaymentID:              s.repo.NextPaymentID(),
		OrderID:                in.OrderID,
		Kind:                   kind,
		Amount:                 uint(in.Amount), //nolint:gosec // amount > 0 проверено выше
		Currency:               currency,
		Description:            in.Description,
		UserEmail:              in.Email,
		UserPhone:              in.Phone,
		EpayInvoiceID:          invoiceID,
		EpayCardID:             in.CardID,
		EpayAccountID:          in.AccountID,
		EpayClientID:           in.ClientID,
		EpayTerminalID:         in.TerminalID,
		EpayCallbackURL:        in.Postlink,
		EpayFailureCallbackURL: in.FailurePostlink,
		EpayPaymentType:        in.PaymentType,
		CardPAN:                defaultStr("", "440043...2221"),
		CardBrand:              "VISA",
		CardOwner:              "STANDARD CARDHOLDER",
		Status:                 pay.StatusNew,
	}
	rec.EpayID = fmt.Sprintf("mock-epay-%d", rec.PaymentID)
	s.repo.Create(rec)

	// Halyk возвращает успешный AuthorizeResponse сразу — это эквивалент Freedom-Hold.
	// Переводим в Authorized, чтобы потом можно было сделать Charge/Cancel/Refund.
	updated, err := s.repo.Transition(rec.PaymentID, pay.AllowedTransitions(pay.StatusAuthorized), pay.StatusAuthorized, "epay authorize")
	if err != nil {
		return nil, fmt.Errorf("authorize transition: %w", err)
	}
	return updated, nil
}

// EpayCharge — POST /api/operation/{id}/charge — списание захолдированной суммы.
func (s *Service) EpayCharge(paymentID uint, amount int) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Epay {
		return nil, ErrInvalidState
	}
	if rec.Status != pay.StatusAuthorized {
		return nil, ErrInvalidState
	}
	target := uint(amount)
	if amount <= 0 {
		target = rec.Amount
	}
	updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		r.Captured = target
		return pay.StatusCaptured, "epay charge", nil
	})
	if errU != nil {
		return nil, errU
	}
	return updated, nil
}

// EpayCancel — POST /api/operation/{id}/cancel — отмена авторизации (только до charge).
func (s *Service) EpayCancel(paymentID uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Epay {
		return nil, ErrInvalidState
	}
	if rec.Status != pay.StatusAuthorized {
		return nil, ErrInvalidState
	}
	return s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusCancelled), pay.StatusCancelled, "epay cancel")
}

// EpayRefund — POST /api/operation/{id}/refund?amount=… — возврат после charge.
// Если amount=0 → полный возврат.
func (s *Service) EpayRefund(paymentID uint, amount int) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Epay {
		return nil, ErrInvalidState
	}
	if rec.Status != pay.StatusCaptured && rec.Status != pay.StatusPartialRefunded {
		return nil, ErrInvalidState
	}
	delta := uint(amount)
	if amount <= 0 {
		delta = rec.Captured - rec.Refunded
	}
	updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		r.Refunded += delta
		if r.Refunded >= r.Captured {
			return pay.StatusRefunded, "epay refund full", nil
		}
		return pay.StatusPartialRefunded, "epay refund partial", nil
	})
	if errU != nil {
		return nil, errU
	}
	return updated, nil
}
