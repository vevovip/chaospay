// Package pay — оркестрация карточных платежей.
package pay

import (
	"errors"
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
)

// Repository — контракт хранилища (реализуется memstore.PayRepo).
type Repository interface {
	NextPaymentID() uint
	Create(rec *pay.Record)
	Get(paymentID uint) (*pay.Record, error)
	List() []*pay.Record
	Update(paymentID uint, fn func(rec *pay.Record) (pay.Status, string, error)) (*pay.Record, error)
	Transition(paymentID uint, allowedFrom map[pay.Status]bool, to pay.Status, reason string) (*pay.Record, error)
	MarkWebhookSent(paymentID uint)
	Reset()
}

// Webhook — отправляет outbound webhook на PG.
type Webhook interface {
	Send(rec *pay.Record, success, captured bool) (int, error)
}

// CardWebhook — отправляет card-bind webhook.
type CardWebhook interface {
	Send(rec *pay.Record) (int, error)
}

// EpayWebhook — отправляет postlink-ы Halyk Epay.
type EpayWebhook interface {
	SendSuccess(rec *pay.Record) (int, error)
	SendFailure(rec *pay.Record, reasonCode int, reason string) (int, error)
	SendBind(rec *pay.Record, success bool, reasonCode int, reason string) (int, error)
}

// AutoWebhookConfig — per-bank флаги авто-webhook'ов.
// true для банка = после каждого перехода в Authorized/Captured/... мок сам шлёт
// webhook на PG (panel-кнопки + внутренний flow сервиса). false = только вручную
// через кнопку "Webhook" в panel.
type AutoWebhookConfig struct {
	Freedom bool
	Epay    bool
	Flitt   bool
}

// Service — application-сервис карточных платежей.
type Service struct {
	repo         Repository
	webhook      Webhook
	cardWebhook  CardWebhook
	epayWebhook  EpayWebhook
	flittWebhook FlittWebhook
	autoWebhook  AutoWebhookConfig
}

// NewService конструктор. Все зависимости — интерфейсы (DI через конструктор).
// flittWebhook опционально; передавай nil, если Flitt не используется.
func NewService(repo Repository, wh Webhook, cw CardWebhook, ew EpayWebhook, fw FlittWebhook, autoWebhook AutoWebhookConfig) *Service {
	return &Service{
		repo:         repo,
		webhook:      wh,
		cardWebhook:  cw,
		epayWebhook:  ew,
		flittWebhook: fw,
		autoWebhook:  autoWebhook,
	}
}

// SendEpayPostlink отправляет success/failure postlink в PG для Epay-платежа.
// variant: "success" (по умолчанию), "fail", "lost-order" (подмена invoiceId),
// "missing-fields" (минимальный payload).
func (s *Service) SendEpayPostlink(paymentID uint, success bool, variant string) error {
	if s.epayWebhook == nil {
		return ErrEpayWebhookNotConfigured
	}
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	switch variant {
	case "lost-order":
		// Подмена invoiceId/EpayInvoiceID → у PG в БД нет такого заказа.
		clone := *rec
		clone.EpayInvoiceID = "999999999"
		rec = &clone
	case "missing-fields":
		clone := *rec
		clone.CardPAN = ""
		clone.CardBrand = ""
		clone.CardOwner = ""
		clone.Reference = 0
		rec = &clone
	}
	if success {
		_, errSend := s.epayWebhook.SendSuccess(rec)
		return errSend
	}
	_, errSend := s.epayWebhook.SendFailure(rec, 484, "Insufficient funds")
	return errSend
}

// SendEpayBindPostlink — bind-postlink (привязка карты).
func (s *Service) SendEpayBindPostlink(paymentID uint, success bool) error {
	if s.epayWebhook == nil {
		return ErrEpayWebhookNotConfigured
	}
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	if success {
		_, errSend := s.epayWebhook.SendBind(rec, true, 0, "success")
		return errSend
	}
	_, errSend := s.epayWebhook.SendBind(rec, false, -444, "Card binding failed")
	return errSend
}

// Repo возвращает репо для прямого доступа из ports (panel/list/raw get).
func (s *Service) Repo() Repository { return s.repo }

// HoldInit создаёт новый платёж по сохранённой карте.
func (s *Service) HoldInit(in HoldInitInput) (*pay.Record, error) {
	if in.CardToken == "" {
		return nil, errors.New("card token is required")
	}
	if in.Amount == 0 {
		return nil, errors.New("amount cannot be zero")
	}
	if in.UserID == 0 {
		return nil, errors.New("user id is required")
	}

	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		PaymentID:      paymentID,
		Reference:      referenceBase + paymentID,
		OrderID:        in.OrderID,
		MerchantID:     in.MerchantID,
		TerminalID:     in.TerminalID,
		UserID:         in.UserID,
		Kind:           pay.KindCard,
		Amount:         in.Amount,
		Currency:       defaultStr(in.Currency, "KZT"),
		Description:    in.Description,
		IdempotencyKey: in.IdempotencyKey,
		CardToken:      in.CardToken,
		CardPAN:        defaultCardPAN(),
		CardOwner:      "TEST USER",
		CardBrand:      "VISA",
		CardExp:        "12/26",
		CardMonth:      "12",
		CardYear:       "26",
		UserPhone:      in.UserPhone,
		UserEmail:      in.UserEmail,
		Status:         pay.StatusNew,
	}
	s.repo.Create(rec)
	return rec, nil
}

// Hold — авторизация по созданному платежу. Если статус уже Authorized/Captured — возвращает ErrAlreadyAuthorized
// (это и есть инцидент EX-1001 — повторный Hold на принятом платеже).
func (s *Service) Hold(paymentID uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Status == pay.StatusAuthorized || rec.Status == pay.StatusCaptured {
		return rec, ErrAlreadyAuthorized
	}
	updated, errT := s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusAuthorized), pay.StatusAuthorized, "hold")
	if errT != nil {
		return nil, errT
	}
	updated = s.ensureReference(paymentID, updated)
	s.maybeWebhook(updated, true, false)
	return updated, nil
}

// Capture — списание захолда.
func (s *Service) Capture(paymentID, clearingAmount uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Status != pay.StatusAuthorized {
		return nil, ErrInvalidState
	}
	target := clearingAmount
	if target == 0 {
		target = rec.Amount
	}
	updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		r.Captured = target
		return pay.StatusCaptured, "capture", nil
	})
	if errU != nil {
		return nil, errU
	}
	s.maybeWebhook(updated, true, true)
	return updated, nil
}

// Cancel — отмена холда (до списания).
func (s *Service) Cancel(paymentID uint) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Status != pay.StatusAuthorized {
		return nil, ErrInvalidState
	}
	updated, errT := s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusCancelled), pay.StatusCancelled, "cancel")
	if errT != nil {
		return nil, errT
	}
	s.maybeWebhook(updated, false, false)
	return updated, nil
}

// Revoke — возврат после списания (полный или частичный) с успешным исходом.
func (s *Service) Revoke(paymentID, refundAmount uint) (*pay.Record, error) {
	return s.RevokeWithOutcome(paymentID, refundAmount, pay.RefundStatusSuccess)
}

// RevokeWithOutcome — возврат с заданным исходом операции. Freedom заводит отдельный
// refund-платёж в любом случае: неуспешный остаётся в pg_refund_payments со статусом
// error и в возвращённую сумму не идёт.
func (s *Service) RevokeWithOutcome(paymentID, refundAmount uint, outcome string) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	if rec.Status != pay.StatusCaptured && rec.Status != pay.StatusPartialRefunded {
		return nil, ErrInvalidState
	}
	delta := refundAmount
	if delta == 0 {
		delta = rec.Captured - rec.Refunded
	}

	refundID := s.repo.NextPaymentID()
	updated, errU := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		r.Refunds = append(r.Refunds, pay.RefundOp{
			PaymentID: refundID,
			Reference: referenceBase + refundID,
			Amount:    -int(delta), //nolint:gosec // сумма возврата ограничена суммой платежа
			Status:    outcome,
			Date:      time.Now(),
		})

		if outcome != pay.RefundStatusSuccess {
			return r.Status, "revoke " + outcome, nil
		}

		r.Refunded += delta
		if r.Refunded >= r.Captured {
			return pay.StatusRefunded, "revoke full", nil
		}

		return pay.StatusPartialRefunded, "revoke partial", nil
	})
	if errU != nil {
		return nil, errU
	}
	s.maybeWebhook(updated, false, false)

	return updated, nil
}

// CreateHosted создаёт hosted-форму (init_payment.php).
func (s *Service) CreateHosted(in HostedInput) (*pay.Record, error) {
	if in.OrderID == 0 || in.Amount == 0 {
		return nil, errors.New("order_id and amount required")
	}
	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		PaymentID:         paymentID,
		Reference:         referenceBase + paymentID,
		OrderID:           in.OrderID,
		MerchantID:        in.MerchantID,
		TerminalID:        in.TerminalID,
		UserID:            in.UserID,
		Kind:              pay.KindHosted,
		Amount:            in.Amount,
		Currency:          defaultStr(in.Currency, "KZT"),
		Description:       in.Description,
		HostedRedirectURL: in.RedirectURL,
		Status:            pay.StatusNew,
		UserPhone:         in.UserPhone,
		UserEmail:         in.UserEmail,
		// Синтетическая карта: после авторизации hosted-формы pay-webhook несёт
		// токен, и PG сохраняет новую карту (флоу «оплата новой картой»).
		CardToken: hostedCardToken(paymentID),
		CardPAN:   hostedCardPAN(paymentID),
		CardOwner: "TEST USER",
		CardBrand: "VISA",
		CardExp:   "12/26",
		CardMonth: "12",
		CardYear:  "26",
	}
	s.repo.Create(rec)
	return rec, nil
}

// CreateBind создаёт bind-flow (cardstorage/add2).
func (s *Service) CreateBind(in BindInput) (*pay.Record, error) {
	if in.UserID == 0 {
		return nil, errors.New("user id is required")
	}
	paymentID := s.repo.NextPaymentID()
	rec := &pay.Record{
		PaymentID:       paymentID,
		Reference:       referenceBase + paymentID,
		OrderID:         in.OrderID,
		MerchantID:      in.MerchantID,
		TerminalID:      in.TerminalID,
		UserID:          in.UserID,
		Kind:            pay.KindBind,
		Status:          pay.StatusNew,
		BindRedirectURL: in.RedirectURL,
		CardPAN:         defaultCardPAN(),
		CardMonth:       "12",
		CardYear:        "26",
		CardOwner:       "TEST USER",
		CardBrand:       "VISA",
		Description:     in.PostLink,
	}
	rec.CardToken = generateBindToken(rec.PaymentID)
	s.repo.Create(rec)
	return rec, nil
}

// AuthorizeWallet используется wallet-эндпоинтом (apple/google) — переводит в Authorized.
func (s *Service) AuthorizeWallet(paymentID uint, kind pay.Kind) (*pay.Record, error) {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return nil, err
	}
	updated, errT := s.repo.Transition(paymentID, pay.AllowedTransitions(pay.StatusAuthorized), pay.StatusAuthorized, "wallet pay")
	if errT != nil {
		return nil, errT
	}
	if kind != "" {
		updated, _ = s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
			r.Kind = kind
			return r.Status, "", nil
		})
	}
	updated = s.ensureReference(paymentID, updated)
	_ = rec
	s.maybeWebhook(updated, true, false)
	return updated, nil
}

// SendWebhook вручную отправляет webhook (например из panel).
func (s *Service) SendWebhook(paymentID uint, success bool) error {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	if _, sendErr := s.webhook.Send(rec, success, rec.Status == pay.StatusCaptured); sendErr != nil {
		return sendErr
	}
	s.repo.MarkWebhookSent(paymentID)
	return nil
}

// SendCardWebhook вручную отправляет card-bind webhook.
func (s *Service) SendCardWebhook(paymentID uint) error {
	rec, err := s.repo.Get(paymentID)
	if err != nil {
		return err
	}
	if _, sendErr := s.cardWebhook.Send(rec); sendErr != nil {
		return sendErr
	}
	s.repo.MarkWebhookSent(paymentID)
	return nil
}

// ApplyForce — panel force-action: переводит в любой target без чека allowedFrom.
func (s *Service) ApplyForce(paymentID uint, target pay.Status) (*pay.Record, error) {
	updated, err := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		if target == pay.StatusCaptured && r.Captured == 0 {
			r.Captured = r.Amount
		}
		if target == pay.StatusRefunded {
			r.Refunded = r.Captured
		}
		return target, "panel force " + string(target), nil
	})
	if err != nil {
		return nil, err
	}
	if target == pay.StatusAuthorized || target == pay.StatusCaptured {
		updated = s.ensureReference(paymentID, updated)
	}
	// Auto-webhook решается per-bank в sendAutoWebhookForRecord по rec.Bank.
	success := target == pay.StatusAuthorized || target == pay.StatusCaptured
	captured := target == pay.StatusCaptured
	s.sendAutoWebhookForRecord(updated, success, captured)
	return updated, nil
}

// sendAutoWebhookForRecord роутит auto-webhook по банку записи.
// Каждый банк имеет свой автоматический флаг (AutoWebhookConfig): включён —
// шлём через соответствующего pgclient'а, выключен — ничего не делаем,
// оператор сам нажмёт кнопку "Webhook" в panel.
func (s *Service) sendAutoWebhookForRecord(rec *pay.Record, success, captured bool) {
	if rec == nil {
		return
	}
	switch rec.Bank {
	case bank.Epay:
		if !s.autoWebhook.Epay || s.epayWebhook == nil {
			return
		}
		go func() {
			if success {
				_, _ = s.epayWebhook.SendSuccess(rec)
			} else {
				_, _ = s.epayWebhook.SendFailure(rec, 477, "panel force fail")
			}
		}()
	case bank.Flitt:
		if !s.autoWebhook.Flitt || s.flittWebhook == nil {
			return
		}
		go func() { _, _ = s.flittWebhook.SendCallback(rec, success) }()
	default:
		// Freedom (включая bank.Any для legacy записей).
		if !s.autoWebhook.Freedom || s.webhook == nil {
			return
		}
		go func() { _, _ = s.webhook.Send(rec, success, captured) }()
	}
}

// referenceBase — стартовое 12-значное число для генерации auth-code,
// похожего на pg_reference от реального Freedom (например, "613554121756").
const referenceBase uint = 613554000000

// ensureReference выставляет Reference у платежа при переходе в Authorized/Captured.
// В реальном Freedom auth-code (pg_reference) появляется в момент авторизации
// и возвращается в get_status3.php. Мок этим занимается тут, чтобы PG-сторона
// (включая reconciliation recovery) видела непустой reference.
func (s *Service) ensureReference(paymentID uint, rec *pay.Record) *pay.Record {
	if rec == nil || rec.Reference != 0 {
		return rec
	}
	updated, err := s.repo.Update(paymentID, func(r *pay.Record) (pay.Status, string, error) {
		if r.Reference == 0 {
			r.Reference = referenceBase + r.PaymentID
		}
		return r.Status, "", nil
	})
	if err != nil {
		return rec
	}

	return updated
}

func (s *Service) maybeWebhook(rec *pay.Record, success, captured bool) {
	// maybeWebhook вызывается только из Freedom-flow (Hold/Capture/...). Управляется
	// AutoWebhookConfig.Freedom.
	if !s.autoWebhook.Freedom || s.webhook == nil {
		return
	}
	go func() {
		if _, err := s.webhook.Send(rec, success, captured); err == nil {
			s.repo.MarkWebhookSent(rec.PaymentID)
		}
	}()
}
