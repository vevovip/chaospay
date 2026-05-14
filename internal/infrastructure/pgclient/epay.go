package pgclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/infrastructure/epay"
)

// EpayClient отправляет postlink-webhooks Halyk Epay в PG.
//
// Используются три URL, соответствующих real Halyk callback-режимам:
//   - successURL   → /api/v1/payment-gateway/webhook/epay_v2/postlink
//   - failureURL   → /api/v1/payment-gateway/webhook/epay_v2/failure_postlink
//   - bindURL      → /api/v1/payment-gateway/webhook/epay/postlink/bind
type EpayClient struct {
	successURL string
	failureURL string
	bindURL    string
	client     *http.Client
}

// NewEpayClient конструктор.
func NewEpayClient(successURL, failureURL, bindURL string) *EpayClient {
	return &EpayClient{
		successURL: successURL,
		failureURL: failureURL,
		bindURL:    bindURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendSuccess отправляет успешный postlink на /webhook/epay_v2/postlink.
// Принимает pay.Record и сам строит payload — это позволяет application/pay
// дёргать webhook без знания структуры Halyk-payload-ов.
func (c *EpayClient) SendSuccess(rec *pay.Record) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	return c.postJSON(c.successURL, buildSuccessPayload(rec), "epay success")
}

// SendFailure отправляет failure_postlink с указанным reasonCode/reason.
func (c *EpayClient) SendFailure(rec *pay.Record, reasonCode int, reason string) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	p := buildSuccessPayload(rec)
	p.Code = "error"
	p.Reason = reason
	p.ReasonCode = reasonCode
	return c.postJSON(c.failureURL, p, "epay failure")
}

// SendBind отправляет bind-postlink. success=true → reasonCode=0 + code="success".
func (c *EpayClient) SendBind(rec *pay.Record, success bool, reasonCode int, reason string) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	p := &epay.BindPostlinkPayload{
		AccountID:  rec.EpayAccountID,
		CardID:     rec.EpayCardID,
		CardMask:   rec.CardPAN,
		CardType:   rec.CardBrand,
		Currency:   rec.Currency,
		DateTime:   rec.CreatedAt.Format(time.RFC3339Nano),
		ID:         rec.EpayID,
		InvoiceID:  rec.EpayInvoiceID,
		Name:       rec.CardOwner,
		Email:      rec.UserEmail,
		Phone:      rec.UserPhone,
		Reason:     reason,
		ReasonCode: reasonCode,
		Reference:  strconv.FormatUint(uint64(rec.Reference), 10),
		Terminal:   rec.EpayTerminalID,
	}
	if success {
		p.Code = "success"
	} else {
		p.Code = "error"
	}
	return c.postJSON(c.bindURL, p, "epay bind")
}

// buildSuccessPayload — формирует payload успешного postlink из pay.Record.
// Дублирует логику ports/api/epay/handlers.go::buildSuccessPayload, но без
// зависимости от port-слоя (pgclient — чистый infrastructure).
func buildSuccessPayload(rec *pay.Record) *epay.PostlinkPayload {
	return &epay.PostlinkPayload{
		ID:          rec.EpayID,
		AccountID:   rec.EpayAccountID,
		DateTime:    rec.CreatedAt.Format(time.RFC3339Nano),
		InvoiceID:   rec.EpayInvoiceID,
		Amount:      int(rec.Amount), //nolint:gosec
		Currency:    rec.Currency,
		Terminal:    rec.EpayTerminalID,
		Description: rec.Description,
		CardMask:    rec.CardPAN,
		CardType:    rec.CardBrand,
		CardID:      rec.EpayCardID,
		Reference:   strconv.FormatUint(uint64(rec.Reference), 10),
		Code:        "ok",
		Reason:      "success",
		ReasonCode:  0,
		Name:        rec.CardOwner,
		Email:       rec.UserEmail,
	}
}

func (c *EpayClient) postJSON(url string, payload any, label string) (int, error) {
	if url == "" {
		return 0, fmt.Errorf("%s webhook url not configured", label)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, errDo := c.client.Do(req)
	if errDo != nil {
		log.Printf("[WEBHOOK %s] failed: %v", label, errDo)
		return 0, errDo
	}
	defer resp.Body.Close()

	log.Printf("[WEBHOOK %s] %s → HTTP %d", label, url, resp.StatusCode)
	return resp.StatusCode, nil
}
