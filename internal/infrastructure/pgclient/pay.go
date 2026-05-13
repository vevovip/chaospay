// Package pgclient отправляет webhook-запросы в Payment Gateway.
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

// PayClient отправляет form-encoded webhook на /api/v1/payment-gateway/webhook/freedompay.
type PayClient struct {
	url    string
	secret string
	client *http.Client
}

// NewPayClient конструктор.
func NewPayClient(webhookURL, secret string) *PayClient {
	return &PayClient{
		url:    webhookURL,
		secret: secret,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send отправляет webhook на PG. success → pg_result=1, иначе 0. captured → pg_captured=1.
// Возвращает HTTP-код ответа (или ошибку, если запрос не удалось отправить).
func (c *PayClient) Send(rec *pay.Record, success, captured bool) (int, error) {
	if rec == nil {
		return 0, fmt.Errorf("nil record")
	}
	form := buildPayForm(rec, success, captured)
	signPayForm(form, c.secret)

	body := form.Encode()
	req, err := http.NewRequest(http.MethodPost, c.url, strings.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, errDo := c.client.Do(req)
	if errDo != nil {
		log.Printf("[WEBHOOK pay] failed for payment %d: %v", rec.PaymentID, errDo)
		return 0, errDo
	}
	defer resp.Body.Close()

	log.Printf("[WEBHOOK pay] sent payment %d → HTTP %d", rec.PaymentID, resp.StatusCode)
	return resp.StatusCode, nil
}

func buildPayForm(rec *pay.Record, success, captured bool) url.Values {
	form := url.Values{}
	resultVal := "0"
	if success {
		resultVal = "1"
	}
	capturedVal := "0"
	if captured {
		capturedVal = "1"
	}

	pgAmount := strconv.FormatFloat(float64(rec.Amount), 'f', -1, 64)

	form.Set("pg_order_id", strconv.FormatUint(uint64(rec.OrderID), 10))
	form.Set("pg_payment_id", strconv.FormatUint(uint64(rec.PaymentID), 10))
	form.Set("pg_amount", pgAmount)
	form.Set("pg_currency", defaultStr(rec.Currency, "KZT"))
	form.Set("pg_net_amount", pgAmount)
	form.Set("pg_ps_amount", pgAmount)
	form.Set("pg_ps_full_amount", pgAmount)
	form.Set("pg_ps_currency", defaultStr(rec.Currency, "KZT"))
	form.Set("pg_description", defaultStr(rec.Description, fmt.Sprintf("оплата заказа № %d", rec.OrderID)))
	form.Set("pg_result", resultVal)
	form.Set("pg_payment_date", time.Now().Format("2006-01-02 15:04:05"))
	form.Set("pg_can_reject", "1")
	if rec.UserPhone != "" {
		form.Set("pg_user_phone", rec.UserPhone)
	}
	if rec.UserEmail != "" {
		form.Set("pg_user_contact_email", rec.UserEmail)
	}
	form.Set("pg_testing_mode", "1")
	form.Set("pg_captured", capturedVal)
	if rec.Reference != 0 {
		form.Set("pg_reference", strconv.FormatUint(uint64(rec.Reference), 10))
	}
	form.Set("pg_card_pan", defaultStr(rec.CardPAN, "5483-18XX-XXXX-0293"))
	if rec.CardToken != "" {
		form.Set("pg_card_token", rec.CardToken)
	}
	form.Set("pg_card_exp", defaultStr(rec.CardExp, "12/26"))
	form.Set("pg_card_owner", defaultStr(rec.CardOwner, "TEST USER"))
	form.Set("pg_card_brand", defaultStr(rec.CardBrand, "VISA"))
	form.Set("pg_payment_method", "bankcard")
	form.Set("terminal_id", strconv.Itoa(rec.TerminalID))
	return form
}

// signPayForm — подпись webhook'а: scriptName="freedompay" (последний сегмент URL).
func signPayForm(form url.Values, secret string) {
	signFields := freedompay.OrdMap{}
	for k, vs := range form {
		if len(vs) > 0 {
			signFields = signFields.Set(k, vs[0])
		}
	}
	salt := freedompay.GenerateSalt(freedompay.SaltLength)
	signFields = signFields.Set("pg_salt", salt)
	form.Set("pg_salt", salt)
	sig := freedompay.Sign("freedompay", signFields, secret)
	form.Set("pg_sig", sig)
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
