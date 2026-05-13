package pgclient

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
)

// CardClient отправляет XML-webhook на /api/v1/payment-gateway/webhook/freedompay/card.
type CardClient struct {
	url    string
	secret string
	client *http.Client
}

// NewCardClient конструктор.
func NewCardClient(webhookURL, secret string) *CardClient {
	return &CardClient{
		url:    webhookURL,
		secret: secret,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send отправляет card-bind webhook (scriptName="card" для подписи).
func (c *CardClient) Send(rec *pay.Record) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	fields := freedompay.OrdMap{}
	fields = fields.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	if rec.OrderID != 0 {
		fields = fields.Set("pg_order_id", strconv.FormatUint(uint64(rec.OrderID), 10))
	}
	fields = fields.Set("pg_card_hash", rec.CardPAN)
	fields = fields.Set("pg_card_id", "1")
	fields = fields.Set("pg_card_token", rec.CardToken)
	fields = fields.Set("pg_user_id", strconv.FormatUint(uint64(rec.UserID), 10))
	fields = fields.Set("pg_status", "ok")
	fields = fields.Set("pg_card_month", defaultStr(rec.CardMonth, "12"))
	fields = fields.Set("pg_card_year", defaultStr(rec.CardYear, "26"))
	fields = fields.Set("pg_bank", "FreedomBank")
	fields = fields.Set("pg_country", "KZ")
	salt := freedompay.GenerateSalt(freedompay.SaltLength)
	fields = fields.Set("pg_salt", salt)
	sig := freedompay.Sign("card", fields, c.secret)
	fields = fields.Set("pg_sig", sig)

	xmlStr := freedompay.RenderResponse("response", fields)

	form := url.Values{}
	form.Set("pg_xml", xmlStr)
	body := form.Encode()

	req, err := http.NewRequest(http.MethodPost, c.url, strings.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, errDo := c.client.Do(req)
	if errDo != nil {
		log.Printf("[WEBHOOK card] failed for payment %d: %v", rec.PaymentID, errDo)
		return 0, errDo
	}
	defer resp.Body.Close()

	log.Printf("[WEBHOOK card] sent payment %d → HTTP %d", rec.PaymentID, resp.StatusCode)
	return resp.StatusCode, nil
}
