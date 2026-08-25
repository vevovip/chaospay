package epay

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
)

// handleCryptopay — POST /api/payment/cryptopay (новая карта или Apple Pay).
func (c *Controller) handleCryptopay(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var req infraepay.AuthorizeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	entry.OrderID = req.InvoiceID

	updated, err := c.svc.EpayAuthorize(apppay.EpayAuthorizeInput{
		OrderID:         infraepay.ParseInvoice(req.InvoiceID),
		Amount:          req.Amount,
		Currency:        req.Currency,
		InvoiceID:       req.InvoiceID,
		TerminalID:      defaultStr(req.TerminalID, c.cfg.TerminalUUID),
		AccountID:       req.AccountID,
		CardID:          cardIDValue(req.CardID),
		PaymentType:     req.PaymentType,
		Description:     req.Description,
		Email:           req.Email,
		Phone:           req.Phone,
		Postlink:        req.Postlink,
		FailurePostlink: req.FailurePostlink,
		CardSave:        req.CardSave,
		HasCryptogram:   req.Cryptogram != "" || req.CryptogramApplePay != "" || req.CryptogramGooglePay != "",
		Requires3DS:     sc != nil && sc.Action == scenario.ActionForce3DS,
	})
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(updated.PaymentID), 10)

	// Если это bind-flow (cardSave=true) — Halyk отправляет НЕ обычный postlink,
	// а отдельный bind-postlink на postLinkBind URL. Симулируем это поведение,
	// иначе PG не зафиксирует привязку карты и end-to-end тест не пройдёт.
	if updated.Kind == pay.KindEpayBind {
		c.scheduleBindPostlink(sc, updated, true)
	}
	return http.StatusOK, buildAuthorizeResponse(updated, sc, c.cfg.ACSURL), nil
}

// handleConfirm — POST /api/payment/confirm: PG прислал результат проверки 3DS.
//
// Real Halyk отвечает на этот запрос редиректом на success/error-страницу, поэтому
// исход платежа PG узнаёт отдельным запросом состояния операции. Мок повторяет это:
// переводит операцию в авторизованные (или отклоняет по сценарию) и отвечает 200.
func (c *Controller) handleConfirm(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var req infraepay.ConfirmRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return 0, nil, errInvalidJSON(err)
	}

	if req.ID == "" || req.PaRes == "" || req.MD == "" {
		return 0, nil, errors.New("ID, PaRes and MD are required")
	}

	paymentID, ok := c.paymentIDByEpayID(req.ID)
	if !ok {
		return 0, nil, errors.New("operation not found")
	}
	entry.PaymentID = strconv.FormatUint(uint64(paymentID), 10)

	rec, err := c.svc.Repo().Get(paymentID)
	if err != nil {
		return 0, nil, err
	}
	entry.OrderID = rec.EpayInvoiceID

	// отказ на проверке 3DS: пароль не подошёл либо эмитент не подтвердил.
	// Операцию переводим в FAILED до ответа: исход платежа PG узнаёт из check-status.
	if sc != nil && sc.Action == scenario.ActionForceFailure {
		if _, failErr := c.svc.ApplyForce(paymentID, pay.StatusFailed); failErr != nil {
			return 0, nil, failErr
		}

		code := scenario.ParamInt(sc, "error_code", 484)

		return http.StatusBadRequest, infraepay.ErrorResponse{
			Code:       code,
			Message:    scenario.Param(sc, "message", "3DS verification failed"),
			ResultCode: code,
		}, nil
	}

	updated, errAuth := c.svc.AuthorizeWallet(paymentID, rec.Kind)
	if errAuth != nil {
		return 0, nil, errAuth
	}

	c.scheduleSuccessPostlink(sc, updated)

	return http.StatusOK, infraepay.OperationResponse{Code: 0, Message: "success"}, nil
}

// paymentIDByEpayID находит операцию по идентификатору Halyk
func (c *Controller) paymentIDByEpayID(epayID string) (uint, bool) {
	for _, rec := range c.svc.Repo().List() {
		if rec.EpayID == epayID {
			return rec.PaymentID, true
		}
	}

	return 0, false
}

// handleCardAuth — POST /api/payments/cards/auth (сохранённая карта).
func (c *Controller) handleCardAuth(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var req infraepay.AuthorizeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	entry.OrderID = req.InvoiceID

	if req.CardID == nil || req.CardID.ID == "" {
		return 0, nil, errors.New("cardId is required for card auth")
	}

	updated, err := c.svc.EpayAuthorize(apppay.EpayAuthorizeInput{
		OrderID:         infraepay.ParseInvoice(req.InvoiceID),
		Amount:          req.Amount,
		Currency:        req.Currency,
		InvoiceID:       req.InvoiceID,
		TerminalID:      defaultStr(req.TerminalID, c.cfg.TerminalUUID),
		AccountID:       req.AccountID,
		CardID:          req.CardID.ID,
		PaymentType:     defaultStr(req.PaymentType, "cardId"),
		Description:     req.Description,
		Postlink:        req.Postlink,
		FailurePostlink: req.FailurePostlink,
		HasCryptogram:   false,
	})
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(updated.PaymentID), 10)
	return http.StatusOK, buildAuthorizeResponse(updated, sc, c.cfg.ACSURL), nil
}

// handleCharge — POST /api/operation/{id}/charge.
func (c *Controller) handleCharge(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	paymentID, ok := c.resolveOperation(r)
	if !ok {
		return 0, nil, errors.New("operation not found")
	}
	entry.PaymentID = strconv.FormatUint(uint64(paymentID), 10)

	var req infraepay.ChargeRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}

	updated, err := c.svc.EpayCharge(paymentID, req.Amount)
	if err != nil {
		return 0, nil, err
	}
	c.scheduleSuccessPostlink(sc, updated)
	return http.StatusOK, infraepay.OperationResponse{Code: 0, Message: "Operation completed successfully"}, nil
}

// handleCancel — POST /api/operation/{id}/cancel.
func (c *Controller) handleCancel(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	paymentID, ok := c.resolveOperation(r)
	if !ok {
		return 0, nil, errors.New("operation not found")
	}
	entry.PaymentID = strconv.FormatUint(uint64(paymentID), 10)

	updated, err := c.svc.EpayCancel(paymentID)
	if err != nil {
		return 0, nil, err
	}
	c.scheduleSuccessPostlink(sc, updated)
	return http.StatusOK, infraepay.OperationResponse{Code: 0, Message: "Operation cancelled"}, nil
}

// handleStatus — GET /check-status/payment/transactionId/{id}.
//
// Используется PG-reconciler-ом для проверки реального состояния операции —
// аналог Freedom get_status3.php. Возвращает текущий статус из репозитория.
func (c *Controller) handleStatus(r *http.Request, _ []byte, _ *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	paymentID, ok := c.resolveOperation(r)
	if !ok {
		return 0, nil, errors.New("operation not found")
	}
	entry.PaymentID = strconv.FormatUint(uint64(paymentID), 10)

	rec, err := c.svc.Repo().Get(paymentID)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, infraepay.StatusResponse{
		ID:           rec.EpayID,
		InvoiceID:    rec.EpayInvoiceID,
		Amount:       int(rec.Amount), //nolint:gosec
		Currency:     rec.Currency,
		Status:       payStatusToEpay(rec.Status),
		StatusName:   payStatusName(rec.Status),
		Reference:    strconv.FormatUint(uint64(rec.Reference), 10),
		IntReference: strconv.FormatUint(uint64(rec.PaymentID), 10),
		DateTime:     rec.CreatedAt.Format(time.RFC3339),
		CardMask:     rec.CardPAN,
	}, nil
}

// handleRefund — POST /api/operation/{id}/refund?amount=…
func (c *Controller) handleRefund(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	paymentID, ok := c.resolveOperation(r)
	if !ok {
		return 0, nil, errors.New("operation not found")
	}
	entry.PaymentID = strconv.FormatUint(uint64(paymentID), 10)

	amount := 0
	if v := r.URL.Query().Get("amount"); v != "" {
		n, _ := strconv.Atoi(v)
		amount = n
	}

	updated, err := c.svc.EpayRefund(paymentID, amount)
	if err != nil {
		return 0, nil, err
	}
	c.scheduleSuccessPostlink(sc, updated)
	return http.StatusOK, infraepay.OperationResponse{
		Code:       0,
		Message:    "Refund processed",
		ExternalID: updated.EpayID,
	}, nil
}

// buildAuthorizeResponse — формирует AuthorizeResponse из записи.
// Если сценарий force_3ds — добавляем блок secure3D.
func buildAuthorizeResponse(rec *pay.Record, sc *scenario.Scenario, acsURL string) infraepay.AuthorizeResponse {
	resp := infraepay.AuthorizeResponse{
		ID:           rec.EpayID,
		Amount:       int(rec.Amount), //nolint:gosec
		Currency:     rec.Currency,
		InvoiceID:    rec.EpayInvoiceID,
		AccountID:    rec.EpayAccountID,
		Description:  rec.Description,
		Reference:    strconv.FormatUint(uint64(rec.Reference), 10),
		IntReference: strconv.FormatUint(uint64(rec.PaymentID), 10),
		Language:     "ru",
		CardID:       rec.EpayCardID,
		Fee:          0,
		Terminal:     rec.EpayTerminalID,
		CardMask:     rec.CardPAN,
		CardType:     rec.CardBrand,
		Name:         rec.CardOwner,
	}
	if sc != nil && sc.Action == scenario.ActionForce3DS {
		// Используем прямой lookup, чтобы различить "не задан" и "пустая строка"
		// (для edge case epay_3ds_missing_action_url: action="").
		action := acsURL
		if action == "" {
			action = defaultACSURL
		}

		if v, ok := sc.Params["action"]; ok {
			action = v
		}
		resp.Secure3D = &infraepay.Secure3D{
			PaReq:  scenario.Param(sc, "pa_req", "mock-pareq"),
			MD:     rec.EpayID,
			Action: action,
		}
	}
	return resp
}

// scheduleSuccessPostlink отправляет postlink на PG, если AutoWebhook включён.
// Сценарии: postlink_lost — не шлём; postlink_double — шлём дважды; postlink_before_ack
// (применяется в applyScenarioBefore до возврата ответа).
func (c *Controller) scheduleSuccessPostlink(sc *scenario.Scenario, rec *pay.Record) {
	if c.webhook == nil || !c.cfg.AutoWebhook {
		return
	}
	if sc != nil && sc.Action == scenario.ActionPostlinkLost {
		return
	}
	send := func() { _, _ = c.webhook.SendSuccess(rec) }
	go send()
	if sc != nil && sc.Action == scenario.ActionPostlinkDouble {
		go func() {
			time.Sleep(500 * time.Millisecond)
			send()
		}()
	}
}

// scheduleBindPostlink отправляет bind-postlink на postLinkBind URL PG.
// В real Halyk после cryptopay с cardSave=true → асинхронный bind-callback.
// Без этого PG-сторона никогда не зафиксирует привязку карты.
func (c *Controller) scheduleBindPostlink(sc *scenario.Scenario, rec *pay.Record, success bool) {
	if c.webhook == nil || !c.cfg.AutoWebhook {
		return
	}
	if sc != nil && sc.Action == scenario.ActionPostlinkLost {
		return
	}
	go func() {
		if success {
			_, _ = c.webhook.SendBind(rec, true, 0, "success")
		} else {
			_, _ = c.webhook.SendBind(rec, false, -444, "Card binding failed")
		}
	}()
}

func errInvalidJSON(err error) error {
	return errors.New("invalid json: " + err.Error())
}

func cardIDValue(c *infraepay.CardID) string {
	if c == nil {
		return ""
	}
	return c.ID
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// payStatusToEpay переводит наш domain.Status в код Halyk Epay status (uppercase константы).
func payStatusToEpay(s pay.Status) string {
	switch s {
	case pay.StatusNew, pay.StatusHoldPending:
		return "NEW"
	case pay.StatusAuthorized:
		return "AUTH"
	case pay.StatusCaptured:
		return "CHARGE"
	case pay.StatusCancelled:
		return "CANCEL"
	case pay.StatusRefunded, pay.StatusPartialRefunded:
		return "REFUND"
	case pay.StatusFailed:
		return "FAILED"
	}
	return "NEW"
}

// payStatusName — human-readable label для UI/логов Halyk.
func payStatusName(s pay.Status) string {
	switch s {
	case pay.StatusAuthorized:
		return "Авторизован"
	case pay.StatusCaptured:
		return "Списан"
	case pay.StatusCancelled:
		return "Отменён"
	case pay.StatusRefunded:
		return "Возвращён"
	case pay.StatusPartialRefunded:
		return "Частичный возврат"
	case pay.StatusFailed:
		return "Отклонён"
	}
	return "Новый"
}
