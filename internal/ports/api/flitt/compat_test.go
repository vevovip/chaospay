// Package flitt — cross-compat тесты.
//
// Цель: подтвердить, что наши ответы парсятся структурами, идентичными PG SDK
// (pkg/flitt/commands/*/response.go в payment-gateway-new), и что наш webhook
// payload подходит под формат, который ожидает PG (request.go в webhook/flitt).
//
// Структуры здесь — копия 1-в-1 с PG-стороны (json-теги + типы полей).
// Если PG-сторона меняет контракт — этот тест должен сломаться сразу,
// а не на проде.
package flitt_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// ---- Копии PG SDK response-структур (1-в-1 с pkg/flitt в payment-gateway-new) ----

// pgPaymentResponse — копия pkg/flitt/commands/payment/response.go::Response.
type pgPaymentResponse struct {
	ResponseStatus string      `json:"response_status"`
	CheckoutURL    string      `json:"checkout_url,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	ErrorCode      interface{} `json:"error_code,omitempty"`
}

type pgPaymentWrapper struct {
	Response pgPaymentResponse `json:"response"`
}

// pgDirectResponse — копия pkg/flitt/commands/direct/response.go::Response.
type pgDirectResponse struct {
	ResponseStatus string      `json:"response_status"`
	ACSURL         string      `json:"acs_url"`
	Pareq          string      `json:"pareq"`
	MD             string      `json:"md"`
	OrderID        string      `json:"order_id"`
	Rectoken       string      `json:"rectoken"`
	ResponseCode   string      `json:"response_code"`
	RRN            string      `json:"rrn"`
	MaskedCard     string      `json:"masked_card"`
	ApprovalCode   string      `json:"approval_code"`
	OrderStatus    string      `json:"order_status"`
	PaymentID      interface{} `json:"payment_id"`
	ErrorMessage   string      `json:"error_message"`
	ErrorCode      int         `json:"error_code"`
}

type pgDirectWrapper struct {
	Response pgDirectResponse `json:"response"`
}

// pgRecurringResponse — копия pkg/flitt/commands/recurring/response.go::Response.
type pgRecurringResponse struct {
	ResponseStatus string      `json:"response_status"`
	OrderStatus    string      `json:"order_status"`
	PaymentID      interface{} `json:"payment_id,omitempty"`
	MaskedCard     string      `json:"masked_card,omitempty"`
	OrderID        string      `json:"order_id,omitempty"`
	Amount         string      `json:"amount,omitempty"`
	Currency       string      `json:"currency,omitempty"`
	ApprovalCode   string      `json:"approval_code,omitempty"`
	RRN            string      `json:"rrn,omitempty"`
	CardType       string      `json:"card_type,omitempty"`
	Rectoken       string      `json:"rectoken,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	ErrorCode      interface{} `json:"error_code,omitempty"`
}

type pgRecurringWrapper struct {
	Response pgRecurringResponse `json:"response"`
}

// pgStatusResponse — копия pkg/flitt/commands/status/response.go::Response.
type pgStatusResponse struct {
	OrderID        string `json:"order_id"`
	OrderStatus    string `json:"order_status"`
	ResponseStatus string `json:"response_status"`
	MerchantID     int    `json:"merchant_id"`
	PaymentID      int64  `json:"payment_id"` // ⚠️ ВНИМАНИЕ: тут int64, не interface{}
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	ApprovalCode   string `json:"approval_code"`
	RRN            string `json:"rrn"`
	MaskedCard     string `json:"masked_card"`
	Rectoken       string `json:"rectoken"`
}

type pgStatusEnvelope struct {
	Response pgStatusResponse `json:"response"`
}

// pgWebhookRequest — копия PG webhook request.go (internal/ports/api/v1/webhook/flitt/request.go).
type pgWebhookRequest struct {
	OrderID            string `json:"order_id"`
	PaymentID          int    `json:"payment_id"` // ⚠️ int, не int64
	OrderStatus        string `json:"order_status"`
	ResponseStatus     string `json:"response_status"`
	Amount             string `json:"amount"`
	Currency           string `json:"currency"`
	ActualAmount       string `json:"actual_amount"`
	ActualCurrency     string `json:"actual_currency"`
	SettlementAmount   string `json:"settlement_amount"`
	SettlementCurrency string `json:"settlement_currency"`
	MaskedCard         string `json:"masked_card"`
	CardType           string `json:"card_type"`
	ApprovalCode       string `json:"approval_code"`
	RRN                string `json:"rrn"`
	SenderEmail        string `json:"sender_email"`
	MerchantID         int    `json:"merchant_id"`
	Signature          string `json:"signature"`
	RecToken           string `json:"rectoken"`
	RecTokenLifeTime   string `json:"rectoken_lifetime"`
	VerificationStatus string `json:"verification_status"`
	MerchantData       string `json:"merchant_data"`
}

// pgBindRequest — копия PG bind_card_request.go.
type pgBindRequest struct {
	OrderID          string `json:"order_id"`
	PaymentID        int    `json:"payment_id"`
	ResponseStatus   string `json:"response_status"`
	Amount           string `json:"amount"`
	Currency         string `json:"currency"`
	RecToken         string `json:"rectoken"`
	MaskedCard       string `json:"masked_card"`
	SenderEmail      string `json:"sender_email"`
	RecTokenLifetime string `json:"rectoken_lifetime"`
	MerchantData     string `json:"merchant_data"`
	MerchantID       int    `json:"merchant_id"`
}

// pgBindAdditionalInfo — PG распарсивает merchant_data как JSON {user_id, merchant_id}.
type pgBindAdditionalInfo struct {
	UserID     string `json:"user_id"`
	MerchantID int    `json:"merchant_id"`
}

// ---- Тесты ----

// TestPGCompat_CheckoutResponse подтверждает, что наш ответ /api/checkout/url
// успешно парсится в pkg/flitt/commands/payment/Wrapper.
func TestPGCompat_CheckoutResponse(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	body, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "100500", "amount": 5000, "currency": "GEL",
		"order_desc": "test", "merchant_id": 1549901, "signature": "x",
	}})
	resp, err := http.Post(st.Server.URL+"/api/checkout/url", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var w pgPaymentWrapper
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("PG SDK structure failed to decode our response: %v", err)
	}
	if w.Response.ResponseStatus != "success" {
		t.Fatalf("ResponseStatus: %s", w.Response.ResponseStatus)
	}
	if w.Response.CheckoutURL == "" {
		t.Fatalf("CheckoutURL empty — PG SDK would think payment URL is missing")
	}
}

// TestPGCompat_DirectResponse_Approved.
// PG SDK direct.Response.PaymentID is interface{} — должен принять int64.
func TestPGCompat_DirectResponse_Approved(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	body, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "1", "merchant_id": 1549901, "amount": 100,
		"currency": "GEL", "order_desc": "test", "container": "x",
		"server_callback_url": "http://pg/cb", "signature": "x",
	}})
	resp, err := http.Post(st.Server.URL+"/api/3dsecure_step1", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var w pgDirectWrapper
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("PG SDK direct.Wrapper failed to decode: %v", err)
	}
	if w.Response.ResponseStatus != "success" {
		t.Fatalf("ResponseStatus: %s", w.Response.ResponseStatus)
	}
	// PG SDK: getReference в test-mode = fmt.Sprintf("%v", PaymentID).
	// Это должен быть не nil и не "" после форматирования.
	if w.Response.PaymentID == nil {
		t.Fatalf("PaymentID nil — PG getReference вернёт <nil>")
	}
	switch w.Response.PaymentID.(type) {
	case float64:
		// JSON-числа decode'ятся в float64 (default Go behavior) — OK
	case json.Number:
		// если decoder использует UseNumber() — OK
	default:
		t.Fatalf("unexpected PaymentID type %T", w.Response.PaymentID)
	}
	if w.Response.Rectoken == "" {
		t.Fatalf("Rectoken empty — PG не сохранит токен карты")
	}
	if w.Response.ApprovalCode == "" {
		t.Fatalf("ApprovalCode empty — PG getReference в prod-mode вернёт пустую строку")
	}
	if w.Response.MaskedCard == "" {
		t.Fatalf("MaskedCard empty — PG cardvo.Mask(\"\") упадёт")
	}
}

// TestPGCompat_DirectResponse_Declined_NoLeakage — после fix Bug A.
// PG SDK не использует поля при failure, но семантически они должны быть пустыми.
func TestPGCompat_DirectResponse_Declined_NoLeakage(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("flitt_card_declined")
	defer st.Scenarios.Reset()

	body, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "1", "merchant_id": 1549901, "amount": 100,
		"currency": "GEL", "order_desc": "test", "container": "x",
		"server_callback_url": "http://pg/cb", "signature": "x",
	}})
	resp, _ := http.Post(st.Server.URL+"/api/3dsecure_step1", "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	var w pgDirectWrapper
	_ = json.NewDecoder(resp.Body).Decode(&w)
	if w.Response.ResponseStatus != "failure" {
		t.Fatalf("expected failure, got %s", w.Response.ResponseStatus)
	}
	// Поля sensitive-данных должны быть пустыми в failure.
	if w.Response.Rectoken != "" {
		t.Fatalf("declined response leaks rectoken=%q", w.Response.Rectoken)
	}
	if w.Response.ApprovalCode != "" {
		t.Fatalf("declined response leaks approval_code=%q", w.Response.ApprovalCode)
	}
	if w.Response.RRN != "" {
		t.Fatalf("declined response leaks rrn=%q", w.Response.RRN)
	}
}

// TestPGCompat_RecurringResponse_Declined_NoLeakage.
func TestPGCompat_RecurringResponse_Declined_NoLeakage(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("flitt_insufficient_funds")
	defer st.Scenarios.Reset()

	body, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "1", "merchant_id": 1549901, "amount": 100,
		"currency": "GEL", "order_desc": "test", "rectoken": "stored-t",
		"signature": "x",
	}})
	resp, _ := http.Post(st.Server.URL+"/api/recurring", "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	var w pgRecurringWrapper
	_ = json.NewDecoder(resp.Body).Decode(&w)
	if w.Response.ResponseStatus != "failure" {
		t.Fatalf("expected failure, got %s", w.Response.ResponseStatus)
	}
	if w.Response.Rectoken != "" {
		t.Fatalf("declined recurring leaks rectoken=%q", w.Response.Rectoken)
	}
	if w.Response.ApprovalCode != "" {
		t.Fatalf("declined recurring leaks approval_code=%q", w.Response.ApprovalCode)
	}
	if w.Response.RRN != "" {
		t.Fatalf("declined recurring leaks rrn=%q", w.Response.RRN)
	}
}

// TestPGCompat_StatusResponse_PaymentIDInt64.
// PG SDK Status.PaymentID is **int64** (не interface{}). Если мы вернём
// число больше MaxInt32 — оно должно безопасно влезть. Если строкой —
// json.Unmarshal в int64 упадёт.
func TestPGCompat_StatusResponse_PaymentIDInt64(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// Создаём платёж
	createBody, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "999", "merchant_id": 1549901, "amount": 100,
		"currency": "GEL", "order_desc": "test", "rectoken": "t",
		"signature": "x",
	}})
	_, _ = http.Post(st.Server.URL+"/api/recurring", "application/json", bytes.NewReader(createBody))

	// Status query
	statusBody, _ := json.Marshal(map[string]any{"request": map[string]any{
		"version": "1.0.1", "order_id": "999", "merchant_id": 1549901, "signature": "x",
	}})
	resp, _ := http.Post(st.Server.URL+"/api/status/order_id", "application/json", bytes.NewReader(statusBody))
	defer resp.Body.Close()

	var env pgStatusEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("PG SDK status.Envelope failed to decode (likely PaymentID type mismatch): %v", err)
	}
	if env.Response.PaymentID < int64(1_000_000) {
		t.Fatalf("PaymentID too small (%d) — должен быть похож на real Flitt (1e9+)", env.Response.PaymentID)
	}
	if env.Response.OrderStatus != "approved" {
		t.Fatalf("OrderStatus: %s", env.Response.OrderStatus)
	}
}

// TestPGCompat_WebhookPayload — проверка что payload подходит под PG webhook request.
func TestPGCompat_WebhookPayload(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// Создаём direct платёж → mock не шлёт webhook автоматически (AutoWebhook=false в test).
	createBody, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "555", "merchant_id": 1549901, "amount": 1000,
		"currency": "GEL", "order_desc": "test", "container": "x",
		"server_callback_url": "http://pg/cb", "signature": "x",
	}})
	_, _ = http.Post(st.Server.URL+"/api/3dsecure_step1", "application/json", bytes.NewReader(createBody))

	// Получаем последнюю запись и шлём callback напрямую через pgclient.
	// Используем FlittStatus как способ найти запись по orderID.
	rec, err := st.Svc.FlittStatus("555")
	if err != nil {
		t.Fatalf("FlittStatus: %v", err)
	}

	// Локальный capture-сервер для приёма webhook.
	captured := make(chan []byte, 1)
	pgSrv := httpTestServerCapturing(t, captured)
	defer pgSrv.Close()

	// Подменяем pgclient на тот, что шлёт на наш capture-server.
	// Используем тот же FlittClient через подмену URL — для теста собираем напрямую.
	client := pgClientWithSecret(pgSrv.URL, "test")
	if _, err := client.SendCallback(rec, true); err != nil {
		t.Fatalf("SendCallback: %v", err)
	}

	select {
	case raw := <-captured:
		var wh pgWebhookRequest
		if err := json.Unmarshal(raw, &wh); err != nil {
			t.Fatalf("PG SDK webhook struct failed to decode our payload: %v\npayload: %s", err, raw)
		}
		// Проверки обязательных полей для Finalizer.
		if wh.OrderID == "" {
			t.Fatalf("order_id empty — Finalizer strconv.Atoi упадёт")
		}
		if _, err := strconv.Atoi(wh.OrderID); err != nil {
			t.Fatalf("order_id %q не парсится Atoi: %v", wh.OrderID, err)
		}
		if wh.PaymentID == 0 {
			t.Fatalf("payment_id 0 — getReference вернёт \"0\"")
		}
		if wh.OrderStatus != "approved" {
			t.Fatalf("order_status: %s", wh.OrderStatus)
		}
		if wh.ResponseStatus != "success" {
			t.Fatalf("response_status: %s", wh.ResponseStatus)
		}
		if wh.MaskedCard == "" {
			t.Fatalf("masked_card empty — cardvo.NewMask(\"\") упадёт")
		}
		if wh.Currency != "GEL" {
			t.Fatalf("currency: got %q, want GEL", wh.Currency)
		}
		if wh.Signature == "" {
			t.Fatalf("signature empty")
		}
	case <-timeout(2):
		t.Fatalf("webhook didn't arrive in 2s")
	}
}

// TestPGCompat_BindWebhookPayload — проверка bind payload (с MerchantData).
func TestPGCompat_BindWebhookPayload(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// Создаём bind-record с MerchantData содержащим user_id+merchant_id.
	// FlittCheckout с IsVerify=true создаёт KindFlittBind.
	createBody, _ := json.Marshal(map[string]any{"request": map[string]any{
		"order_id": "777", "merchant_id": 1549901, "amount": 10,
		"currency": "GEL", "order_desc": "bind test",
		"verification": "Y",
		"merchant_data": `{"user_id":"42","merchant_id":1549901}`,
		"signature":     "x",
	}})
	_, _ = http.Post(st.Server.URL+"/api/checkout/url", "application/json", bytes.NewReader(createBody))

	rec, err := st.Svc.FlittStatus("777")
	if err != nil {
		t.Fatalf("FlittStatus: %v", err)
	}
	if !rec.FlittIsVerify {
		t.Fatalf("FlittIsVerify must be true for verification=Y")
	}

	captured := make(chan []byte, 1)
	pgSrv := httpTestServerCapturing(t, captured)
	defer pgSrv.Close()

	client := pgClientWithBindURL(pgSrv.URL, "test")
	if _, err := client.SendBindCallback(rec, true); err != nil {
		t.Fatalf("SendBindCallback: %v", err)
	}

	select {
	case raw := <-captured:
		var bind pgBindRequest
		if err := json.Unmarshal(raw, &bind); err != nil {
			t.Fatalf("PG bind struct failed to decode: %v\npayload: %s", err, raw)
		}
		if bind.ResponseStatus != "success" {
			t.Fatalf("response_status: %s (PG validate() bind отклонит)", bind.ResponseStatus)
		}
		if bind.MerchantData == "" {
			t.Fatalf("merchant_data empty — PG bind validate упадёт с 'поле merchant_data не может быть пустым'")
		}
		// Финальная проверка: PG json.Unmarshal в additionalInfo проходит.
		var info pgBindAdditionalInfo
		if err := json.Unmarshal([]byte(bind.MerchantData), &info); err != nil {
			t.Fatalf("PG не сможет распарсить merchant_data: %v (value=%q)", err, bind.MerchantData)
		}
		if info.UserID != "42" {
			t.Fatalf("user_id lost in round-trip: got %q", info.UserID)
		}
		// rectoken_lifetime парсится PG через time.Parse("02.01.2006 15:04:05", ...)
		if !strings.Contains(bind.RecTokenLifetime, ".") {
			t.Fatalf("rectoken_lifetime в неверном формате: %q (ожидается 02.01.2006 15:04:05)", bind.RecTokenLifetime)
		}
		if bind.RecToken == "" {
			t.Fatalf("rectoken empty — карта не привяжется")
		}
		if bind.MaskedCard == "" {
			t.Fatalf("masked_card empty — vo.NewMask упадёт")
		}
	case <-timeout(2):
		t.Fatalf("bind webhook didn't arrive in 2s")
	}
}
