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
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

// FlittClient отправляет callback-webhooks на PG (Flitt-flow).
//
// Используются два URL:
//   - successURL → /api/v1/payment-gateway/webhook/flitt       (платежи)
//   - bindURL    → /api/v1/payment-gateway/webhook/flitt/bind  (привязка карт)
type FlittClient struct {
	successURL string
	bindURL    string
	secret     string // для подписи callback-а (SHA1, как в Flitt)
	client     *http.Client
}

// NewFlittClient конструктор.
func NewFlittClient(successURL, bindURL, secret string) *FlittClient {
	return &FlittClient{
		successURL: successURL,
		bindURL:    bindURL,
		secret:     secret,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// SendCallback — основной callback на /api/v1/payment-gateway/webhook/flitt.
// success определяет order_status (approved/declined) и response_status.
func (c *FlittClient) SendCallback(rec *pay.Record, success bool) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	payload := buildFlittCallback(rec, success)
	signFlittCallback(&payload, c.secret)
	return c.postJSON(c.successURL, payload, "flitt callback")
}

// SendBindCallback — bind callback на /api/v1/payment-gateway/webhook/flitt/bind.
// success=true → verification_status=verified, иначе declined.
func (c *FlittClient) SendBindCallback(rec *pay.Record, success bool) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	payload := buildFlittCallback(rec, success)
	if success {
		payload.VerificationStatus = "verified"
	} else {
		payload.VerificationStatus = "declined"
	}
	signFlittCallback(&payload, c.secret)
	return c.postJSON(c.bindURL, payload, "flitt bind callback")
}

// buildFlittCallback собирает callback-payload из pay.Record.
func buildFlittCallback(rec *pay.Record, success bool) infraflitt.CallbackPayload {
	orderStatus := infraflitt.OrderStatusApproved
	responseStatus := infraflitt.ResponseStatusSuccess
	if !success {
		orderStatus = infraflitt.OrderStatusDeclined
		responseStatus = infraflitt.ResponseStatusFailure
	}
	amountStr := strconv.FormatUint(uint64(rec.Amount), 10)
	return infraflitt.CallbackPayload{
		OrderID:            strconv.FormatUint(uint64(rec.OrderID), 10),
		PaymentID:          int(rec.FlittPaymentID), //nolint:gosec
		MerchantID:         int(rec.MerchantID),     //nolint:gosec
		Amount:             amountStr,
		ActualAmount:       amountStr,
		SettlementAmount:   amountStr,
		Currency:           defaultStr(rec.Currency, "GEL"),
		ActualCurrency:     defaultStr(rec.Currency, "GEL"),
		SettlementCurrency: defaultStr(rec.Currency, "GEL"),
		OrderStatus:        orderStatus,
		ResponseStatus:     responseStatus,
		ApprovalCode:       rec.FlittApprovalCode,
		RRN:                rec.FlittRRN,
		MaskedCard:         rec.CardPAN,
		CardType:           defaultStr(rec.CardBrand, "VISA"),
		CardBin:            0,
		Fee:                "0",
		FeeOplata:          "0",
		ReversalAmount:     "0",
		SenderEmail:        rec.UserEmail,
		SenderCellPhone:    rec.UserPhone,
		OrderTime:          rec.CreatedAt.Format("02.01.2006 15:04:05"),
		SettlementDate:     rec.CreatedAt.Format("2006-01-02"),
		PaymentSystem:      "card",
		TranType:           "purchase",
		ECI:                "5",
		ProductID:          "",
		MerchantData:       rec.FlittMerchantData,
		RecToken:           rec.FlittRectoken,
		RecTokenLifeTime:   rec.CreatedAt.AddDate(1, 0, 0).Format("02.01.2006 15:04:05"),
	}
}

// signFlittCallback кладёт SHA1-подпись в payload.Signature.
// Подпись по правилам Flitt: SHA1(secret + "|" + non-empty-values-asc-key).
func signFlittCallback(p *infraflitt.CallbackPayload, secret string) {
	if secret == "" {
		return
	}
	params := map[string]any{
		"order_id":            p.OrderID,
		"payment_id":          p.PaymentID,
		"merchant_id":         p.MerchantID,
		"amount":              p.Amount,
		"actual_amount":       p.ActualAmount,
		"currency":            p.Currency,
		"actual_currency":     p.ActualCurrency,
		"order_status":        p.OrderStatus,
		"response_status":     p.ResponseStatus,
		"approval_code":       p.ApprovalCode,
		"rrn":                 p.RRN,
		"masked_card":         p.MaskedCard,
		"card_type":           p.CardType,
		"sender_email":        p.SenderEmail,
		"rectoken":            p.RecToken,
		"verification_status": p.VerificationStatus,
		"merchant_data":       p.MerchantData,
	}
	p.Signature = infraflitt.Sign(secret, params)
}

func (c *FlittClient) postJSON(url string, payload any, label string) (int, error) {
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
