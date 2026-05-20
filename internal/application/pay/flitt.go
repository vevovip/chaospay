package pay

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

// Flitt fallback-маска (если в запросе нет PAN — direct/recurring без указанной карты).
const flittDefaultMaskedCard = "444455XXXXXX1111"

// Flitt fallback-approval-code (для тестов).
const flittDefaultApprovalCode = "123456"

// FlittWebhook — отправляет outbound callback на PG.
type FlittWebhook interface {
	SendCallback(rec *pay.Record, success bool) (int, error)
	SendBindCallback(rec *pay.Record, success bool) (int, error)
}

// FlittCheckoutInput — параметры hosted-формы Flitt (/api/checkout/url).
type FlittCheckoutInput struct {
	OrderID       uint
	MerchantID    uint
	TerminalID    int
	Amount        uint
	Currency      string
	Description   string
	Email         string
	CallbackURL   string
	ResponseURL   string
	MerchantData  string
	IsVerify      bool // verification=Y → bind-flow
	NeedRectoken  bool // required_rectoken=Y
	HostedFormURL string
}

// FlittCheckout создаёт запись hosted-формы и возвращает её checkout URL.
func (s *Service) FlittCheckout(in FlittCheckoutInput) (*pay.Record, error) {
	if in.Amount == 0 {
		return nil, errors.New("amount must be positive")
	}
	if in.OrderID == 0 {
		return nil, errors.New("order_id is required")
	}

	kind := pay.KindFlittCheckout
	if in.IsVerify {
		kind = pay.KindFlittBind
	}

	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		Bank:              bank.Flitt,
		PaymentID:         paymentID,
		Reference:         referenceBase + paymentID,
		OrderID:           in.OrderID,
		MerchantID:        in.MerchantID,
		TerminalID:        in.TerminalID,
		Kind:              kind,
		Amount:            in.Amount,
		Currency:          defaultStr(in.Currency, "GEL"),
		Description:       in.Description,
		UserEmail:         in.Email,
		CardPAN:           flittDefaultMaskedCard,
		CardBrand:         "VISA",
		CardOwner:         "TEST USER",
		FlittPaymentID:    int64(referenceBase + paymentID), //nolint:gosec
		FlittApprovalCode: flittDefaultApprovalCode,
		FlittRRN:          fmt.Sprintf("%012d", paymentID),
		FlittCallbackURL:  in.CallbackURL,
		FlittResponseURL:  in.ResponseURL,
		FlittMerchantData: in.MerchantData,
		FlittIsVerify:     in.IsVerify,
		Status:            pay.StatusNew,
		HostedRedirectURL: in.HostedFormURL,
	}
	// CheckoutURL — для тестов на PG достаточно вернуть путь к panel, чтобы
	// можно было оператором перевести запись в Authorized/Captured.
	rec.FlittCheckoutURL = in.HostedFormURL
	if rec.FlittCheckoutURL == "" {
		rec.FlittCheckoutURL = fmt.Sprintf("http://localhost:48532/panel?bank=flitt&tab=cards#payment-%d", paymentID)
	}
	rec.CardToken = generateFlittRectoken(paymentID)
	rec.FlittRectoken = rec.CardToken

	s.repo.Create(rec)
	return rec, nil
}

// FlittDirectInput — параметры прямого платежа (Apple/Google Pay) через /api/3dsecure_step1.
type FlittDirectInput struct {
	OrderID     uint
	MerchantID  uint
	TerminalID  int
	Amount      uint
	Currency    string
	Description string
	Container   string // base64-encoded encrypted token (Apple/Google Pay)
	IsApplePay  bool
	IsGooglePay bool
	CallbackURL string
	Outcome     infraflitt.CardOutcome // если не указан — OutcomeApproved
}

// FlittDirect создаёт direct-платёж (Apple/Google Pay).
//
// Поведение определяется Outcome (приходит из panel-сценария или из тестовой карты).
// Если Outcome требует 3DS — запись остаётся в StatusNew, FlittACSURL/Pareq/MD заполнены.
// Если approved — запись переходит в Authorized сразу.
func (s *Service) FlittDirect(in FlittDirectInput) (*pay.Record, error) {
	if in.Amount == 0 {
		return nil, errors.New("amount must be positive")
	}

	kind := pay.KindFlittApplePay
	if in.IsGooglePay {
		kind = pay.KindFlittGooglePay
	}
	outcome := in.Outcome
	if outcome == "" || outcome == infraflitt.OutcomeUnknownCard {
		outcome = infraflitt.OutcomeApproved
	}

	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		Bank:              bank.Flitt,
		PaymentID:         paymentID,
		Reference:         referenceBase + paymentID,
		OrderID:           in.OrderID,
		MerchantID:        in.MerchantID,
		TerminalID:        in.TerminalID,
		Kind:              kind,
		Amount:            in.Amount,
		Currency:          defaultStr(in.Currency, "GEL"),
		Description:       in.Description,
		CardPAN:           flittDefaultMaskedCard,
		CardBrand:         "VISA",
		CardOwner:         "TEST USER",
		CardToken:         generateFlittRectoken(paymentID),
		FlittPaymentID:    int64(referenceBase + paymentID), //nolint:gosec
		FlittApprovalCode: flittDefaultApprovalCode,
		FlittRRN:          fmt.Sprintf("%012d", paymentID),
		FlittCallbackURL:  in.CallbackURL,
		Status:            pay.StatusNew,
	}
	rec.FlittRectoken = rec.CardToken
	s.repo.Create(rec)

	return s.applyFlittOutcome(rec, outcome)
}

// FlittRecurringInput — параметры списания сохранённой картой (/api/recurring).
type FlittRecurringInput struct {
	OrderID     uint
	MerchantID  uint
	TerminalID  int
	Amount      uint
	Currency    string
	Description string
	Rectoken    string
	CallbackURL string
	Outcome     infraflitt.CardOutcome
}

// FlittRecurring создаёт rectoken-платёж.
func (s *Service) FlittRecurring(in FlittRecurringInput) (*pay.Record, error) {
	if in.Amount == 0 {
		return nil, errors.New("amount must be positive")
	}
	if in.Rectoken == "" {
		return nil, errors.New("rectoken is required")
	}
	outcome := in.Outcome
	if outcome == "" || outcome == infraflitt.OutcomeUnknownCard {
		outcome = infraflitt.OutcomeApproved
	}

	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		Bank:              bank.Flitt,
		PaymentID:         paymentID,
		Reference:         referenceBase + paymentID,
		OrderID:           in.OrderID,
		MerchantID:        in.MerchantID,
		TerminalID:        in.TerminalID,
		Kind:              pay.KindFlittRecurring,
		Amount:            in.Amount,
		Currency:          defaultStr(in.Currency, "GEL"),
		Description:       in.Description,
		CardPAN:           flittDefaultMaskedCard,
		CardBrand:         "VISA",
		CardOwner:         "TEST USER",
		CardToken:         in.Rectoken,
		FlittRectoken:     in.Rectoken,
		FlittPaymentID:    int64(referenceBase + paymentID), //nolint:gosec
		FlittApprovalCode: flittDefaultApprovalCode,
		FlittRRN:          fmt.Sprintf("%012d", paymentID),
		FlittCallbackURL:  in.CallbackURL,
		Status:            pay.StatusNew,
	}
	s.repo.Create(rec)

	return s.applyFlittOutcome(rec, outcome)
}

// applyFlittOutcome применяет исход к свежесозданной записи:
//
//   - approved → StatusAuthorized сразу.
//   - approved_3ds / declined_3ds → запись остаётся в StatusNew, готова к step2.
//   - declined / insufficient_funds → запись остаётся в StatusNew, вернуть FAILURE.
func (s *Service) applyFlittOutcome(rec *pay.Record, outcome infraflitt.CardOutcome) (*pay.Record, error) {
	switch outcome {
	case infraflitt.OutcomeApproved:
		// preauth=Y → Authorized (как в реальном Flitt для preauth=Y).
		updated, err := s.repo.Transition(rec.PaymentID, pay.AllowedTransitions(pay.StatusAuthorized), pay.StatusAuthorized, "flitt authorize")
		if err != nil {
			return nil, fmt.Errorf("flitt outcome authorize: %w", err)
		}
		return updated, nil
	case infraflitt.OutcomeApproved3DS, infraflitt.OutcomeDeclined3DS:
		// 3DS-челлендж: добавляем acs_url/pareq/md, статус не меняем.
		updated, err := s.repo.Update(rec.PaymentID, func(r *pay.Record) (pay.Status, string, error) {
			r.FlittACSURL = "https://chaospay.local/3ds/challenge"
			r.FlittPareq = fmt.Sprintf("mock-pareq-%d", r.PaymentID)
			r.FlittMD = fmt.Sprintf("mock-md-%d", r.PaymentID)
			return r.Status, "", nil
		})
		if err != nil {
			return nil, fmt.Errorf("flitt outcome 3ds: %w", err)
		}
		return updated, nil
	case infraflitt.OutcomeDeclined, infraflitt.OutcomeInsufficientFunds:
		// Сразу переводим в Failed; webhook не шлём.
		updated, err := s.repo.Update(rec.PaymentID, func(r *pay.Record) (pay.Status, string, error) {
			r.LastError = string(outcome)
			return pay.StatusFailed, "flitt declined", nil
		})
		if err != nil {
			return nil, fmt.Errorf("flitt outcome decline: %w", err)
		}
		return updated, ErrFlittDeclined
	}
	return rec, nil
}

// ErrFlittDeclined — карта/платёж отклонён по таблице testcards / сценарию.
var ErrFlittDeclined = errors.New("flitt: payment declined")

// FlittCapture — POST /api/capture/order_id. amount=0 → полная сумма.
func (s *Service) FlittCapture(paymentID uint, amount uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Flitt {
		return nil, ErrInvalidState
	}
	if rec.Status != pay.StatusAuthorized {
		return nil, ErrInvalidState
	}
	target := amount
	if target == 0 {
		target = rec.Amount
	}
	updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		r.Captured = target
		return pay.StatusCaptured, "flitt capture", nil
	})
	if errU != nil {
		return nil, errU
	}
	return updated, nil
}

// FlittReverse — POST /api/reverse/order_id. До списания — отмена холда,
// после списания — refund. amount=0 → полная сумма.
func (s *Service) FlittReverse(paymentID uint, amount uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Flitt {
		return nil, ErrInvalidState
	}
	switch rec.Status {
	case pay.StatusAuthorized:
		// До списания — Cancel.
		return s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusCancelled), pay.StatusCancelled, "flitt cancel")
	case pay.StatusCaptured, pay.StatusPartialRefunded:
		delta := amount
		if delta == 0 {
			delta = rec.Captured - rec.Refunded
		}
		updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
			r.Refunded += delta
			if r.Refunded >= r.Captured {
				return pay.StatusRefunded, "flitt refund full", nil
			}
			return pay.StatusPartialRefunded, "flitt refund partial", nil
		})
		if errU != nil {
			return nil, errU
		}
		return updated, nil
	}
	return nil, ErrInvalidState
}

// FlittStatus — read-only выборка записи по orderID или paymentID.
// Если по orderID не нашли — пытаемся paymentID.
func (s *Service) FlittStatus(orderIDOrPaymentID string) (*pay.Record, error) {
	// 1. Пытаемся orderID
	if oid, err := strconv.ParseUint(orderIDOrPaymentID, 10, 64); err == nil {
		for _, r := range s.repo.List() {
			if r.Bank == bank.Flitt && uint64(r.OrderID) == oid {
				return r, nil
			}
		}
		// fallback: PaymentID
		if rec, errGet := s.repo.Get(uint(oid)); errGet == nil && rec.Bank == bank.Flitt {
			return rec, nil
		}
	}
	return nil, ErrFlittNotFound
}

// ErrFlittNotFound — Flitt-запись с указанным order_id не найдена.
var ErrFlittNotFound = errors.New("flitt: order not found")

// FlittComplete3DS — POST /api/3dsecure_step2. По таблице testcards/сценарию
// переводим запись либо в Authorized (approved_3ds), либо в Failed (declined_3ds).
func (s *Service) FlittComplete3DS(paymentID uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Bank != bank.Flitt {
		return nil, ErrInvalidState
	}
	if rec.Status != pay.StatusNew {
		// уже обработан step2 или вне состояния
		return rec, nil
	}
	// По умолчанию step2 успешен — если другое поведение нужно, его задаёт сценарий.
	updated, errU := s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusAuthorized), pay.StatusAuthorized, "flitt 3ds step2")
	if errU != nil {
		return nil, errU
	}
	return updated, nil
}

// SendFlittCallback отправляет callback на PG (success/failure).
// Используется auto-webhook flow + кнопкой "Send webhook" в panel.
func (s *Service) SendFlittCallback(paymentID uint, success bool) error {
	if s.flittWebhook == nil {
		return ErrFlittWebhookNotConfigured
	}
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	if _, errSend := s.flittWebhook.SendCallback(rec, success); errSend != nil {
		return errSend
	}
	s.repo.MarkWebhookSent(paymentID)
	return nil
}

// SendFlittBindCallback — bind-callback (карта привязана / отказ).
func (s *Service) SendFlittBindCallback(paymentID uint, success bool) error {
	if s.flittWebhook == nil {
		return ErrFlittWebhookNotConfigured
	}
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	if _, errSend := s.flittWebhook.SendBindCallback(rec, success); errSend != nil {
		return errSend
	}
	s.repo.MarkWebhookSent(paymentID)
	return nil
}

// MaybeFlittCallback — авто-webhook после authorize/capture/reverse.
// Управляется AutoWebhookConfig.Flitt. Раньше тут была двойная проверка
// (svc.autoWebhook + port-cfg), которая ломала Flitt PG-flow когда Freedom-флаг
// был выключен — теперь единый per-bank флаг.
func (s *Service) MaybeFlittCallback(rec *pay.Record, success bool) {
	if !s.autoWebhook.Flitt || s.flittWebhook == nil {
		return
	}
	go func() {
		if _, err := s.flittWebhook.SendCallback(rec, success); err == nil {
			s.repo.MarkWebhookSent(rec.PaymentID)
		}
	}()
}

// ErrFlittWebhookNotConfigured — попытка отправить Flitt-webhook без сконфигурированного клиента.
var ErrFlittWebhookNotConfigured = errors.New("flitt webhook is not configured")

// generateFlittRectoken — детерминированный токен (для воспроизводимых тестов).
func generateFlittRectoken(paymentID uint) string {
	return fmt.Sprintf("flitt-mock-rectoken-%d", paymentID)
}
