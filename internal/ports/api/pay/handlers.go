package pay

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	domainpay "github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// handleHoldInit — POST /v1/merchant/{id}/card/init
func (c *Controller) handleHoldInit(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	rec, err := c.svc.HoldInit(apppay.HoldInitInput{
		OrderID:        uintFromReq(req, "pg_order_id", 0),
		MerchantID:     uintFromReq(req, "pg_merchant_id", 0),
		TerminalID:     extractTerminalID(req, c.cfg.DefaultTerminalID),
		CabinetID:      extractCabinetID(req),
		UserID:         uintFromReq(req, "pg_user_id", 0),
		Amount:         uintFromReq(req, "pg_amount", 0),
		Currency:       req.Get("pg_currency", ""),
		CardToken:      req.Get("pg_card_token", ""),
		Description:    req.Get("pg_description", ""),
		IdempotencyKey: req.Get("pg_idempotency_key", ""),
		UserPhone:      req.Get("pg_user_phone", ""),
		UserEmail:      req.Get("pg_user_email", ""),
	})
	if err != nil {
		return nil, err
	}
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	out = out.Set("pg_merchant_id", strconv.FormatUint(uint64(rec.MerchantID), 10))
	if rec.OrderID != 0 {
		out = out.Set("pg_order_id", strconv.FormatUint(uint64(rec.OrderID), 10))
	}
	return out, nil
}

// handleHold — POST /v1/merchant/{id}/card/direct
func (c *Controller) handleHold(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	paymentID := uintFromReq(req, "pg_payment_id", 0)
	if paymentID == 0 {
		return nil, errors.New("payment id is required")
	}
	rec, err := c.svc.Hold(paymentID)
	if errors.Is(err, apppay.ErrAlreadyAuthorized) {
		// Главный сигнал EX-1001 — возвращаем ambiguous error без переноса состояния.
		out := freedompay.OrdMap{}
		out = out.Set("pg_status", "error")
		out = out.Set("pg_error_code", "120")
		out = out.Set("pg_failure_description", "Неверный статус платежа")
		return out, nil
	}
	if errors.Is(err, memstore.ErrPaymentNotFound) {
		return nil, errors.New("payment not found")
	}
	if err != nil {
		return nil, err
	}

	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	out = out.Set("pg_transaction_status", "Authorized")
	out = out.Set("pg_payment_method", "bankcard")
	out = out.Set("pg_create_date", rec.CreatedAt.Format(time.RFC3339))
	out = out.Set("pg_can_reject", "1")
	out = out.Set("pg_captured", "0")
	out = out.Set("pg_card_pan", rec.CardPAN)
	out = out.Set("pg_card_id", "1")
	out = out.Set("pg_card_token", rec.CardToken)
	out = out.Set("pg_card_hash", rec.CardPAN)
	out = out.Set("pg_amount", strconv.FormatUint(uint64(rec.Amount), 10))
	out = out.Set("pg_currency", rec.Currency)
	return out, nil
}

// handleStatus — POST /get_status3.php
// Freedom находит платёж по pg_payment_id или по pg_order_id (если pg_payment_id=0).
func (c *Controller) handleStatus(req *freedompay.ParsedRequest, sc *scenario.Scenario) (freedompay.OrdMap, error) {
	paymentID := uintFromReq(req, "pg_payment_id", 0)
	orderID := uintFromReq(req, "pg_order_id", 0)
	if paymentID == 0 && orderID == 0 {
		return nil, errors.New("payment id or order id is required")
	}
	rec, err := findStatusRecord(c.svc.Repo(), paymentID, orderID)
	if err != nil {
		return nil, err
	}
	// Поиск по order_id отдаёт последний платёж заказа, а после возврата это он и есть:
	// прод-ответ по заказу 18508640 пришёл с pg_amount=-4140 и нулевым клирингом.
	if paymentID == 0 && !hideRefunds(sc) {
		if refund, ok := lastRefund(rec); ok {
			return refundStatusResponse(rec, refund), nil
		}
	}

	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	out = out.Set("pg_can_reject", "1")
	out = out.Set("pg_payment_method", "bankcard")
	if rec.OrderID != 0 {
		out = out.Set("pg_order_id", strconv.FormatUint(uint64(rec.OrderID), 10))
	}
	out = out.Set("pg_currency", rec.Currency)
	out = out.Set("pg_payment_status", payToFreedomStatus(rec.Status))
	out = out.Set("pg_amount", strconv.FormatUint(uint64(rec.Amount), 10))
	out = out.Set("pg_clearing_amount", strconv.FormatUint(uint64(rec.Captured), 10))
	out = out.Set("pg_create_date", rec.CreatedAt.Format(time.RFC3339))
	if !rec.AuthorizedAt.IsZero() {
		out = out.Set("pg_payment_date", rec.AuthorizedAt.Format(time.RFC3339))
	}
	if rec.Captured > 0 {
		// pg_captured у Freedom — флаг клиринга (0/1), сумма живёт в pg_clearing_amount.
		out = out.Set("pg_captured", "1")
	}
	if hideRefunds(sc) {
		rec = withoutRefunds(rec)
	}
	if rec.Refunded > 0 {
		out = out.Set("pg_revoked_amount", "-"+strconv.FormatUint(uint64(rec.Refunded), 10))
	}
	out = setRefundPayments(out, rec)
	if rec.Reference != 0 {
		out = out.Set("pg_reference", strconv.FormatUint(uint64(rec.Reference), 10))
		out = out.Set("pg_auth_code", strconv.FormatUint(uint64(rec.Reference), 10))
	}
	if rec.CardPAN != "" {
		out = out.Set("pg_card_pan", rec.CardPAN)
	}
	if rec.CardToken != "" {
		out = out.Set("pg_card_token", rec.CardToken)
	}
	if rec.UserEmail != "" {
		out = out.Set("pg_user_email", rec.UserEmail)
	}
	if rec.UserPhone != "" {
		out = out.Set("pg_user_phone", rec.UserPhone)
	}
	out = out.Set("pg_testing_mode", "1")
	if rec.Status == domainpay.StatusFailed && rec.LastError != "" {
		out = out.Set("pg_failure_description", rec.LastError)
		out = out.Set("pg_failure_code", "100")
	}
	return out, nil
}

// handleCapture — POST /do_capture.php
func (c *Controller) handleCapture(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	paymentID := uintFromReq(req, "pg_payment_id", 0)
	clearing := uintFromReq(req, "pg_clearing_amount", 0)
	if paymentID == 0 {
		return nil, errors.New("payment id is required")
	}
	updated, err := c.svc.Capture(paymentID, clearing)
	if err != nil {
		return nil, err
	}
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_amount", strconv.FormatUint(uint64(updated.Amount), 10))
	out = out.Set("pg_clearing_amount", strconv.FormatUint(uint64(updated.Captured), 10))
	return out, nil
}

// handleCancel — POST /cancel.php
func (c *Controller) handleCancel(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	paymentID := uintFromReq(req, "pg_payment_id", 0)
	if paymentID == 0 {
		return nil, errors.New("payment id is required")
	}
	if _, err := c.svc.Cancel(paymentID); err != nil {
		return nil, err
	}
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	return out, nil
}

// handleRevoke — POST /revoke.php
func (c *Controller) handleRevoke(req *freedompay.ParsedRequest, sc *scenario.Scenario) (freedompay.OrdMap, error) {
	paymentID := uintFromReq(req, "pg_payment_id", 0)
	refund := uintFromReq(req, "pg_refund_amount", 0)
	if paymentID == 0 {
		return nil, errors.New("payment id is required")
	}
	updated, err := c.svc.RevokeWithOutcome(paymentID, refund, refundOutcome(sc))
	if err != nil {
		return nil, err
	}
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_revoke_payment_id", strconv.FormatUint(uint64(updated.PaymentID), 10))
	return out, nil
}

// handleInitPayment — POST /init_payment.php
func (c *Controller) handleInitPayment(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	rec, err := c.svc.CreateHosted(apppay.HostedInput{
		OrderID:     uintFromReq(req, "pg_order_id", 0),
		MerchantID:  uintFromReq(req, "pg_merchant_id", 0),
		TerminalID:  extractTerminalID(req, c.cfg.DefaultTerminalID),
		CabinetID:   extractCabinetID(req),
		UserID:      uintFromReq(req, "pg_user_id", 0),
		Amount:      uintFromReq(req, "pg_amount", 0),
		Currency:    req.Get("pg_currency", ""),
		Description: req.Get("pg_description", ""),
		UserPhone:   req.Get("pg_user_phone", ""),
		UserEmail:   req.Get("pg_user_contact_email", ""),
	})
	if err != nil {
		return nil, err
	}
	rec.HostedRedirectURL = fmt.Sprintf("%s&customer=%d", c.cfg.HostedFormURL, rec.PaymentID)
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	out = out.Set("pg_redirect_url", rec.HostedRedirectURL)
	out = out.Set("pg_redirect_url_type", "auto")
	return out, nil
}

// handleAddCard — POST /v1/merchant/{id}/cardstorage/add2
func (c *Controller) handleAddCard(req *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	rec, err := c.svc.CreateBind(apppay.BindInput{
		OrderID:    uintFromReq(req, "pg_order_id", 0),
		MerchantID: uintFromReq(req, "pg_merchant_id", 0),
		TerminalID: extractTerminalID(req, c.cfg.DefaultTerminalID),
		CabinetID:  extractCabinetID(req),
		UserID:     uintFromReq(req, "pg_user_id", 0),
		PostLink:   req.Get("pg_post_link", ""),
	})
	if err != nil {
		return nil, err
	}
	rec.BindRedirectURL = fmt.Sprintf("%s&bind=%d", c.cfg.HostedFormURL, rec.PaymentID)
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	out = out.Set("pg_redirect_url", rec.BindRedirectURL)
	if rec.OrderID != 0 {
		out = out.Set("pg_order_id", strconv.FormatUint(uint64(rec.OrderID), 10))
	}
	return out, nil
}

// handleRemoveCard — POST /v1/merchant/{id}/cardstorage/remove
func (c *Controller) handleRemoveCard(_ *freedompay.ParsedRequest, _ *scenario.Scenario) (freedompay.OrdMap, error) {
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	return out, nil
}

// lastRefund — последняя операция возврата по платежу.
func lastRefund(rec *domainpay.Record) (domainpay.RefundOp, bool) {
	if len(rec.Refunds) == 0 {
		return domainpay.RefundOp{}, false
	}

	return rec.Refunds[len(rec.Refunds)-1], true
}

// refundStatusResponse — статус самого возврата: сумма минусом, свой payment_id, клиринга нет.
func refundStatusResponse(rec *domainpay.Record, refund domainpay.RefundOp) freedompay.OrdMap {
	out := freedompay.OrdMap{}
	out = out.Set("pg_status", "ok")
	out = out.Set("pg_payment_id", strconv.FormatUint(uint64(refund.PaymentID), 10))
	out = out.Set("pg_can_reject", "1")
	out = out.Set("pg_payment_method", "bankcard")
	out = out.Set("pg_currency", rec.Currency)
	out = out.Set("pg_payment_status", refund.Status)
	out = out.Set("pg_amount", strconv.Itoa(refund.Amount))
	out = out.Set("pg_clearing_amount", "0")
	out = out.Set("pg_create_date", refund.Date.Format(time.RFC3339))
	out = out.Set("pg_reference", strconv.FormatUint(uint64(refund.Reference), 10))
	if rec.CardPAN != "" {
		out = out.Set("pg_card_pan", rec.CardPAN)
	}
	out = out.Set("pg_testing_mode", "1")

	return out
}

// refundOutcome — с каким исходом сценарий велит завести refund-платёж.
func refundOutcome(sc *scenario.Scenario) string {
	if sc == nil {
		return domainpay.RefundStatusSuccess
	}
	switch sc.Action {
	case scenario.ActionRefundDeclined:
		return domainpay.RefundStatusError
	case scenario.ActionRefundPending:
		return domainpay.RefundStatusProcess
	}

	return domainpay.RefundStatusSuccess
}

// hideRefunds — сценарий просит отдать статус так, будто возврата ещё не видно.
func hideRefunds(sc *scenario.Scenario) bool {
	return sc != nil && sc.Action == scenario.ActionRefundInvisible
}

// withoutRefunds — копия записи без следов возврата, сам платёж при этом не меняется.
func withoutRefunds(rec *domainpay.Record) *domainpay.Record {
	clone := rec.Clone()
	clone.Refunds = nil
	clone.Refunded = 0
	if clone.Status == domainpay.StatusRefunded || clone.Status == domainpay.StatusPartialRefunded {
		clone.Status = domainpay.StatusCaptured
	}

	return clone
}

// setRefundPayments добавляет в ответ pg_refund_payments — по операции на каждый возврат,
// включая неуспешные: Freedom оставляет их в списке, но в pg_refund_amount не считает.
func setRefundPayments(out freedompay.OrdMap, rec *domainpay.Record) freedompay.OrdMap {
	if len(rec.Refunds) == 0 {
		return out
	}

	children := make([]freedompay.OrdMap, 0, len(rec.Refunds))
	for _, refund := range rec.Refunds {
		child := freedompay.OrdMap{}
		child = child.Set("pg_payment_id", strconv.FormatUint(uint64(refund.PaymentID), 10))
		child = child.Set("pg_payment_status", refund.Status)
		child = child.Set("pg_amount", strconv.Itoa(refund.Amount))
		child = child.Set("pg_payment_date", refund.Date.Format(time.RFC3339))
		child = child.Set("pg_reference", strconv.FormatUint(uint64(refund.Reference), 10))
		children = append(children, child)
	}

	group := freedompay.OrdMap{}
	group = group.Set("pg_refund_payment", children)

	return out.Set("pg_refund_payments", group)
}

// findStatusRecord ищет платёж по paymentID, либо (если он пустой) по orderID.
// Реальный Freedom get_status3.php допускает оба варианта поиска.
func findStatusRecord(repo apppay.Repository, paymentID, orderID uint) (*domainpay.Record, error) {
	if paymentID != 0 {
		rec, err := repo.Get(paymentID)
		if err != nil {
			return nil, errors.New("payment not found")
		}
		return rec, nil
	}
	for _, rec := range repo.List() {
		if rec.OrderID == orderID {
			return rec, nil
		}
	}
	return nil, errors.New("payment not found")
}

func payToFreedomStatus(s domainpay.Status) string {
	switch s {
	case domainpay.StatusNew, domainpay.StatusHoldPending:
		return "process"
	case domainpay.StatusAuthorized, domainpay.StatusCaptured:
		return "success"
	case domainpay.StatusFailed:
		return "failed"
	case domainpay.StatusCancelled:
		return "revoked"
	case domainpay.StatusRefunded, domainpay.StatusPartialRefunded:
		// Возвращённый платёж остаётся success: прод-статус заказа 18508638 с
		// pg_refund_amount=-2016 приходит именно так, факт возврата живёт в суммах.
		return "success"
	}
	return "process"
}
