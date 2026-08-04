package flitt

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

// requestWrapper парсит общий конверт {"request": ...}.
type requestWrapper[T any] struct {
	Request T `json:"request"`
}

// ---- /api/checkout/url ----

func (c *Controller) handleCheckout(_ *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.CheckoutRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	orderID, _ := strconv.ParseUint(req.OrderID, 10, 64)
	entry.OrderID = req.OrderID

	rec, err := c.svc.FlittCheckout(apppay.FlittCheckoutInput{
		OrderID:       uint(orderID),
		MerchantID:    uint(req.MerchantID), //nolint:gosec
		Amount:        uintFromInt(req.Amount),
		Currency:      req.Currency,
		Description:   req.OrderDesc,
		Email:         req.SenderEmail,
		CallbackURL:   req.ServerCallbackURL,
		ResponseURL:   req.ResponseURL,
		MerchantData:  req.MerchantData,
		IsVerify:      req.Verification == "Y",
		NeedRectoken:  req.RequiredRectoken == "Y",
		HostedFormURL: c.cfg.HostedFormURL,
	})
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)
	_ = sc
	return http.StatusOK, infraflitt.CheckoutEnvelope{
		Response: infraflitt.CheckoutResponse{
			ResponseStatus: infraflitt.ResponseStatusSuccess,
			CheckoutURL:    rec.FlittCheckoutURL,
		},
	}, nil
}

// ---- /api/3dsecure_step1 (direct: Apple/Google Pay) ----

func (c *Controller) handleDirect(_ *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.DirectRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	orderID, _ := strconv.ParseUint(req.OrderID, 10, 64)
	entry.OrderID = req.OrderID

	outcome := flittOutcomeFromScenario(sc)

	rec, err := c.svc.FlittDirect(apppay.FlittDirectInput{
		OrderID:     uint(orderID),
		MerchantID:  uint(req.MerchantID), //nolint:gosec
		Amount:      uintFromInt(req.Amount),
		Currency:    req.Currency,
		Description: req.OrderDesc,
		Container:   req.Container,
		IsApplePay:  true, // в реальности направление wallet PG не различает — пишем applepay
		IsGooglePay: false,
		CallbackURL: req.ServerCallbackURL,
		Outcome:     outcome,
	})
	if err != nil && !errors.Is(err, apppay.ErrFlittDeclined) {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)

	resp := buildDirectResponse(rec)
	if errors.Is(err, apppay.ErrFlittDeclined) {
		resp = infraflitt.DirectResponse{
			ResponseStatus: infraflitt.ResponseStatusFailure,
			OrderStatus:    infraflitt.OrderStatusDeclined,
			OrderID:        strconv.FormatUint(uint64(rec.OrderID), 10),
			ErrorMessage:   "payment declined",
			ResponseCode:   flittErrorCode(sc),
		}
	}
	// Авто-callback после approved-исхода.
	if c.cfg.AutoWebhook && rec.Status == pay.StatusAuthorized {
		c.svc.MaybeFlittCallback(rec, true)
	}
	return http.StatusOK, infraflitt.DirectEnvelope{Response: resp}, nil
}

// ---- /api/recurring (saved card) ----

func (c *Controller) handleRecurring(_ *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.RecurringRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	orderID, _ := strconv.ParseUint(req.OrderID, 10, 64)
	entry.OrderID = req.OrderID

	outcome := flittOutcomeFromScenario(sc)

	rec, err := c.svc.FlittRecurring(apppay.FlittRecurringInput{
		OrderID:     uint(orderID),
		MerchantID:  uint(req.MerchantID), //nolint:gosec
		Amount:      uintFromInt(req.Amount),
		Currency:    req.Currency,
		Description: req.OrderDesc,
		Rectoken:    req.Rectoken,
		CallbackURL: req.ServerCallbackURL,
		Outcome:     outcome,
	})
	if err != nil && !errors.Is(err, apppay.ErrFlittDeclined) {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)

	var resp infraflitt.RecurringResponse
	if errors.Is(err, apppay.ErrFlittDeclined) {
		// failure: real Flitt не возвращает rectoken/approval_code/rrn — оставляем только обязательные.
		resp = infraflitt.RecurringResponse{
			ResponseStatus: infraflitt.ResponseStatusFailure,
			OrderStatus:    infraflitt.OrderStatusDeclined,
			OrderID:        req.OrderID,
			Amount:         strconv.Itoa(req.Amount),
			Currency:       req.Currency,
			ErrorMessage:   "payment declined",
			ResponseCode:   flittErrorCode(sc),
		}
	} else {
		resp = infraflitt.RecurringResponse{
			ResponseStatus: infraflitt.ResponseStatusSuccess,
			OrderStatus:    infraflitt.OrderStatusApproved,
			OrderID:        req.OrderID,
			Amount:         strconv.Itoa(req.Amount),
			Currency:       req.Currency,
			PaymentID:      rec.FlittPaymentID,
			MaskedCard:     rec.CardPAN,
			ApprovalCode:   rec.FlittApprovalCode,
			RRN:            rec.FlittRRN,
			CardType:       rec.CardBrand,
			Rectoken:       rec.FlittRectoken,
		}
		if c.cfg.AutoWebhook && rec.Status == pay.StatusAuthorized {
			c.svc.MaybeFlittCallback(rec, true)
		}
	}
	return http.StatusOK, infraflitt.RecurringEnvelope{Response: resp}, nil
}

// ---- /api/capture/order_id ----

func (c *Controller) handleCapture(_ *http.Request, body []byte, _ *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.CaptureRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	entry.OrderID = req.OrderID

	rec, err := c.findFlittRecord(req.OrderID)
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)

	updated, errCap := c.svc.FlittCapture(rec.PaymentID, uintFromInt(req.Amount))
	if errCap != nil {
		return http.StatusOK, infraflitt.CaptureEnvelope{
			Response: infraflitt.CaptureResponse{
				CaptureStatus:  infraflitt.CaptureStatusDeclined,
				OrderID:        req.OrderID,
				ResponseStatus: infraflitt.ResponseStatusFailure,
				ErrorCode:      1002,
				ErrorMessage:   errCap.Error(),
			},
		}, nil
	}
	if c.cfg.AutoWebhook {
		c.svc.MaybeFlittCallback(updated, true)
	}
	return http.StatusOK, infraflitt.CaptureEnvelope{
		Response: infraflitt.CaptureResponse{
			CaptureStatus:       infraflitt.CaptureStatusCaptured,
			OrderID:             req.OrderID,
			ResponseDescription: "captured",
			ResponseCode:        "1000",
			MerchantID:          req.MerchantID,
			ResponseStatus:      infraflitt.ResponseStatusSuccess,
		},
	}, nil
}

// ---- /api/reverse/order_id ----

func (c *Controller) handleReverse(_ *http.Request, body []byte, _ *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.ReverseRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	entry.OrderID = req.OrderID

	rec, err := c.findFlittRecord(req.OrderID)
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)

	updated, errRev := c.svc.FlittReverse(rec.PaymentID, uintFromInt(req.Amount))
	if errRev != nil {
		return http.StatusOK, infraflitt.ReverseEnvelope{
			Response: infraflitt.ReverseResponse{
				ReverseStatus:  infraflitt.ReverseStatusDeclined,
				OrderID:        req.OrderID,
				ResponseStatus: infraflitt.ResponseStatusFailure,
				ErrorCode:      1003,
				ErrorMessage:   errRev.Error(),
			},
		}, nil
	}
	if c.cfg.AutoWebhook {
		c.svc.MaybeFlittCallback(updated, true)
	}
	reverseID := req.ReverseID
	if reverseID == "" {
		reverseID = "mock-rev-" + req.OrderID
	}
	return http.StatusOK, infraflitt.ReverseEnvelope{
		Response: infraflitt.ReverseResponse{
			ReverseStatus:       infraflitt.ReverseStatusApproved,
			OrderID:             req.OrderID,
			ResponseDescription: "reversed",
			ResponseCode:        "1000",
			MerchantID:          req.MerchantID,
			ResponseStatus:      infraflitt.ResponseStatusSuccess,
			ReverseID:           reverseID,
			ReversalAmount:      strconv.Itoa(req.Amount),
			TransactionID:       strconv.FormatInt(updated.FlittPaymentID, 10),
		},
	}, nil
}

// ---- /api/status/order_id ----

func (c *Controller) handleStatus(_ *http.Request, body []byte, _ *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.StatusRequest]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	entry.OrderID = req.OrderID

	rec, err := c.findFlittRecord(req.OrderID)
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)
	return http.StatusOK, infraflitt.StatusEnvelope{Response: buildStatusResponse(rec)}, nil
}

// ---- /api/3dsecure_step2 ----

func (c *Controller) handle3DSStep2(_ *http.Request, body []byte, _ *scenario.Scenario, entry *requestlog.Entry) (int, any, error) {
	var w requestWrapper[infraflitt.Step2Request]
	if err := json.Unmarshal(body, &w); err != nil {
		return 0, nil, errInvalidJSON(err)
	}
	req := w.Request
	entry.OrderID = req.OrderID

	rec, err := c.findFlittRecord(req.OrderID)
	if err != nil {
		return 0, nil, err
	}
	entry.PaymentID = strconv.FormatUint(uint64(rec.PaymentID), 10)

	updated, errStep := c.svc.FlittComplete3DS(rec.PaymentID)
	if errStep != nil {
		return 0, nil, errStep
	}
	if c.cfg.AutoWebhook && updated.Status == pay.StatusAuthorized {
		c.svc.MaybeFlittCallback(updated, true)
	}
	return http.StatusOK, infraflitt.Step2Envelope{
		Response: infraflitt.Step2Response{
			ResponseStatus: infraflitt.ResponseStatusSuccess,
			OrderID:        req.OrderID,
			OrderStatus:    infraflitt.OrderStatusApproved,
			ApprovalCode:   updated.FlittApprovalCode,
			RRN:            updated.FlittRRN,
			MaskedCard:     updated.CardPAN,
			PaymentID:      updated.FlittPaymentID,
			Rectoken:       updated.FlittRectoken,
		},
	}, nil
}

// ---- helpers ----

func (c *Controller) findFlittRecord(orderID string) (*pay.Record, error) {
	rec, err := c.svc.FlittStatus(orderID)
	if err != nil {
		return nil, errors.New("order not found: " + orderID)
	}
	return rec, nil
}

func buildDirectResponse(rec *pay.Record) infraflitt.DirectResponse {
	resp := infraflitt.DirectResponse{
		ResponseStatus:     infraflitt.ResponseStatusSuccess,
		OrderID:            strconv.FormatUint(uint64(rec.OrderID), 10),
		MaskedCard:         rec.CardPAN,
		ApprovalCode:       rec.FlittApprovalCode,
		RRN:                rec.FlittRRN,
		ResponseCode:       "1000",
		PaymentID:          rec.FlittPaymentID,
		Rectoken:           rec.FlittRectoken,
		SettlementAmount:   strconv.FormatUint(uint64(rec.Amount), 10),
		SettlementCurrency: defaultStr(rec.Currency, "GEL"),
	}
	// Если 3DS-челлендж требуется — отдаём acs_url + pareq + md, OrderStatus="processing".
	if rec.FlittACSURL != "" {
		resp.ACSURL = rec.FlittACSURL
		resp.Pareq = rec.FlittPareq
		resp.MD = rec.FlittMD
		resp.OrderStatus = infraflitt.OrderStatusProcessing
	} else {
		resp.OrderStatus = infraflitt.OrderStatusApproved
	}
	return resp
}

func buildStatusResponse(rec *pay.Record) infraflitt.StatusResponse {
	orderStatus := orderStatusFromPay(rec.Status)
	respStatus := infraflitt.ResponseStatusSuccess
	if rec.Status == pay.StatusFailed {
		respStatus = infraflitt.ResponseStatusFailure
	}
	return infraflitt.StatusResponse{
		OrderID:            strconv.FormatUint(uint64(rec.OrderID), 10),
		OrderStatus:        orderStatus,
		ResponseStatus:     respStatus,
		PaymentID:          rec.FlittPaymentID,
		Amount:             strconv.FormatUint(uint64(rec.Amount), 10),
		ActualAmount:       strconv.FormatUint(uint64(rec.Amount), 10),
		SettlementAmount:   strconv.FormatUint(uint64(rec.Amount), 10),
		Currency:           defaultStr(rec.Currency, "GEL"),
		ActualCurrency:     defaultStr(rec.Currency, "GEL"),
		SettlementCurrency: defaultStr(rec.Currency, "GEL"),
		ApprovalCode:       rec.FlittApprovalCode,
		RRN:                rec.FlittRRN,
		MaskedCard:         rec.CardPAN,
		CardType:           defaultStr(rec.CardBrand, "VISA"),
		OrderTime:          rec.CreatedAt.Format("02.01.2006 15:04:05"),
		SettlementDate:     rec.CreatedAt.Format("2006-01-02"),
		SenderEmail:        rec.UserEmail,
		MerchantID:         int(rec.MerchantID), //nolint:gosec
		MerchantData:       rec.FlittMerchantData,
		PaymentSystem:      "card",
		TranType:           "purchase",
		ECI:                "5",
		Rectoken:           rec.FlittRectoken,
		Fee:                "0",
		FeeOplata:          "0",
		ReversalAmount:     strconv.FormatUint(uint64(rec.Refunded), 10),
		VerificationStatus: verificationStatusFor(rec),
		ResponseCode:       "1000",
	}
}

func verificationStatusFor(rec *pay.Record) string {
	if !rec.FlittIsVerify {
		return ""
	}
	if rec.Status == pay.StatusAuthorized || rec.Status == pay.StatusCaptured {
		return "verified"
	}
	return "pending"
}

func orderStatusFromPay(s pay.Status) string {
	switch s {
	case pay.StatusAuthorized, pay.StatusCaptured:
		return infraflitt.OrderStatusApproved
	case pay.StatusCancelled, pay.StatusRefunded, pay.StatusPartialRefunded:
		return infraflitt.OrderStatusReversed
	case pay.StatusFailed:
		return infraflitt.OrderStatusDeclined
	}
	return infraflitt.OrderStatusCreated
}

// flittOutcomeFromScenario вытаскивает Outcome из сценария (по action force_failure/force_status и параметру outcome).
func flittOutcomeFromScenario(sc *scenario.Scenario) infraflitt.CardOutcome {
	if sc == nil {
		return infraflitt.OutcomeApproved
	}
	switch sc.Action {
	case scenario.ActionForceFailure:
		// Конкретный outcome можно задать параметром outcome=...; по умолчанию — declined.
		v := scenario.Param(sc, "outcome", string(infraflitt.OutcomeDeclined))
		return infraflitt.CardOutcome(v)
	case scenario.ActionForce3DS:
		// Сценарий запрашивает 3DS-челлендж.
		v := scenario.Param(sc, "outcome", string(infraflitt.OutcomeApproved3DS))
		return infraflitt.CardOutcome(v)
	}
	return infraflitt.OutcomeApproved
}

// flittErrorCode возвращает код отказа из сценария (param error_code), дефолт 1001.
func flittErrorCode(sc *scenario.Scenario) string {
	return scenario.Param(sc, "error_code", "1001")
}

func uintFromInt(v int) uint {
	if v < 0 {
		return 0
	}
	return uint(v)
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func errInvalidJSON(err error) error {
	return errors.New("invalid json: " + err.Error())
}
