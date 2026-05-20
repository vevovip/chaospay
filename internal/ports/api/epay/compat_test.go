// Package epay — cross-compat тесты Halyk Epay v2.
//
// Цель: подтвердить, что наши ответы парсятся структурами, идентичными PG-стороне
// (internal/infrastructure/clients/payments/epay_2/*.go в payment-gateway-new),
// и что наши webhook payload-ы подходят под формат, который ожидает PG
// (internal/ports/api/v1/webhook/epay/request.go и request_bind.go).
//
// Структуры здесь — копия 1-в-1 с PG-стороны (json-теги + типы полей).
// Если PG-сторона меняет контракт — тест ломается локально, а не на проде.
package epay_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	epayports "github.com/vevovip/chaospay/internal/ports/api/epay"
)

// ---- Копии PG SDK response-структур (1-в-1 с infrastructure/clients/payments/epay_2/) ----

// pgTokenResponse — копия response.go::tokenResponse.
type pgTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    string `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// pgPaymentResponse — копия response.go::paymentResponse.
type pgPaymentResponse struct {
	ID           string      `json:"id"`
	Amount       int         `json:"amount"`
	AmountBonus  int         `json:"amountBonus"`
	Currency     string      `json:"currency"`
	InvoiceID    string      `json:"invoiceId"`
	AccountID    string      `json:"accountId,omitempty"`
	Description  string      `json:"description"`
	Reference    string      `json:"reference"`
	IntReference string      `json:"intReference"`
	Language     string      `json:"language"`
	Secure3D     *pgSecure3D `json:"secure3D"`
	CardID       string      `json:"cardID,omitempty"`
	Fee          int         `json:"fee"`
}

type pgSecure3D struct {
	PaReq  string `json:"paReq,omitempty"`
	MD     string `json:"md,omitempty"`
	Action string `json:"action,omitempty"`
}

// pgApproveResponse — копия response.go::approveResponse (для charge/cancel).
type pgApproveResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// pgEpayError — копия response.go::epayError (400-ответ).
type pgEpayError struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	InvoiceID string `json:"invoiceId,omitempty"`
}

// pgWebhookRequest — копия webhook/epay/request.go::webhookRequest.
type pgWebhookRequest struct {
	ID          string `json:"id,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	DateTime    string `json:"dateTime,omitempty"`
	InvoiceID   string `json:"invoiceId,omitempty"`
	Amount      int    `json:"amount,omitempty"`
	Currency    string `json:"currency,omitempty"`
	Terminal    string `json:"terminal,omitempty"`
	Description string `json:"description,omitempty"`
	CardMask    string `json:"cardMask,omitempty"`
	CardType    string `json:"cardType,omitempty"`
	CardID      string `json:"cardID,omitempty"`
	Issuer      string `json:"issuer,omitempty"`
	Reference   string `json:"reference,omitempty"`
	Code        string `json:"code,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ReasonCode  int    `json:"reasonCode,omitempty"`
	Name        string `json:"name,omitempty"`
	Email       string `json:"email,omitempty"`
}

// pgBindCallbackRequest — копия webhook/epay/request_bind.go::bindCallbackRequest.
type pgBindCallbackRequest struct {
	AccountID  string `json:"accountId"`
	CardID     string `json:"cardId"`
	CardMask   string `json:"cardMask"`
	Code       string `json:"code"`
	InvoiceID  string `json:"invoiceId"`
	Reason     string `json:"reason"`
	ReasonCode int    `json:"reasonCode"`
	Name       string `json:"name"`
	Issuer     string `json:"issuer"`
}

// ---- Стенд ----

type compatStand struct {
	Server    *httptest.Server
	Svc       *apppay.Service
	Scenarios *appscenario.Service
	BindHits  *capturedHits
	OkHits    *capturedHits
	FailHits  *capturedHits
}

type capturedHits struct {
	mu      sync.Mutex
	payload [][]byte
}

func (h *capturedHits) push(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.payload = append(h.payload, b)
}

func (h *capturedHits) last() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.payload) == 0 {
		return nil
	}
	return h.payload[len(h.payload)-1]
}

func (h *capturedHits) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.payload)
}

// newCompatStand поднимает Epay-controller + 3 capture-сервера для postlink/failure/bind.
func newCompatStand(t *testing.T, autoWebhook bool) *compatStand {
	t.Helper()
	ok := &capturedHits{}
	fail := &capturedHits{}
	bind := &capturedHits{}
	okSrv := httptest.NewServer(captureHandler(ok))
	t.Cleanup(okSrv.Close)
	failSrv := httptest.NewServer(captureHandler(fail))
	t.Cleanup(failSrv.Close)
	bindSrv := httptest.NewServer(captureHandler(bind))
	t.Cleanup(bindSrv.Close)

	payRepo := memstore.NewPayRepo()
	scenarioStore := memstore.NewScenarioStore()
	requestLog := memstore.NewRequestLog(0)
	tokens := infraepay.NewTokenStore()

	webhook := pgclient.NewEpayClient(okSrv.URL, failSrv.URL, bindSrv.URL)
	svc := apppay.NewService(payRepo, nil, nil, webhook, nil, apppay.AutoWebhookConfig{Epay: autoWebhook})
	scenarios := appscenario.NewService(scenarioStore)

	ctrl := epayports.NewController(svc, scenarios, requestLog, tokens, webhook, epayports.Config{
		TerminalUUID: "test-terminal",
		AutoWebhook:  autoWebhook,
	})

	mux := http.NewServeMux()
	ctrl.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &compatStand{
		Server:    srv,
		Svc:       svc,
		Scenarios: scenarios,
		OkHits:    ok,
		FailHits:  fail,
		BindHits:  bind,
	}
}

func captureHandler(h *capturedHits) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.push(body)
		w.WriteHeader(http.StatusOK)
	}
}

// ---- Тесты ----

// TestPGCompat_OAuthToken_FormURLEncoded — PG отправляет form-urlencoded.
func TestPGCompat_OAuthToken_FormURLEncoded(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, false)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "webapi usermanagement email_send")
	form.Set("client_id", "test")
	form.Set("client_secret", "secret")
	form.Set("invoiceID", "000123")
	form.Set("amount", "5000")
	form.Set("currency", "KZT")
	form.Set("terminal", "67e34d63-102f-4bd1-898e-370781d0074d")
	form.Set("secret_hash", "ignored")

	req, _ := http.NewRequest("POST", st.Server.URL+"/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /oauth2/token: %v", err)
	}
	defer resp.Body.Close()

	var tok pgTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatalf("PG SDK tokenResponse failed to decode: %v", err)
	}
	if tok.AccessToken == "" {
		t.Fatalf("AccessToken empty — PG getToken().GetAuth() даст пустой Bearer")
	}
	if tok.TokenType == "" {
		t.Fatalf("TokenType empty — PG req.Header.Set('Authorization', tt+' '+at) даст ' token'")
	}
}

// TestPGCompat_Cryptopay_Approved — paymentResponse парсится PG SDK.
func TestPGCompat_Cryptopay_Approved(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, false)

	body, _ := json.Marshal(map[string]any{
		"amount":     5000,
		"terminalId": "67e34d63-102f-4bd1-898e-370781d0074d",
		"invoiceId":  "000123",
		"currency":   "KZT",
		"cryptogram": "encrypted-cryptogram-payload",
	})
	resp, err := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	if err != nil {
		t.Fatalf("cryptopay: %v", err)
	}
	defer resp.Body.Close()

	var pr pgPaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatalf("PG SDK paymentResponse failed to decode: %v", err)
	}
	if pr.ID == "" {
		t.Fatalf("ID empty — PG paymentResponse.getPaymentResponseData() даст пустой PaymentID")
	}
	if pr.Reference == "" {
		t.Fatalf("Reference empty — PG Acquirer.Reference будет \"\"")
	}
	if pr.Amount != 5000 {
		t.Fatalf("Amount mismatch: got %d, want 5000", pr.Amount)
	}
	if pr.InvoiceID != "000123" {
		t.Fatalf("InvoiceID mismatch: got %q", pr.InvoiceID)
	}
}

// TestPGCompat_Cryptopay_3DSChallenge — Secure3D блок парсится.
func TestPGCompat_Cryptopay_3DSChallenge(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, false)
	st.Scenarios.ApplyPreset("epay_3ds_required")
	defer st.Scenarios.Reset()

	body, _ := json.Marshal(map[string]any{
		"amount":     1000,
		"terminalId": "test-terminal",
		"invoiceId":  "000200",
		"currency":   "KZT",
		"cryptogram": "x",
	})
	resp, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	defer resp.Body.Close()

	var pr pgPaymentResponse
	_ = json.NewDecoder(resp.Body).Decode(&pr)
	if pr.Secure3D == nil {
		t.Fatalf("Secure3D nil — preset epay_3ds_required не сработал")
	}
	if pr.Secure3D.Action == "" {
		t.Fatalf("Secure3D.Action empty — PG ActionURL будет пуст, юзер не увидит 3DS")
	}
	if pr.Secure3D.MD == "" {
		t.Fatalf("Secure3D.MD empty — PG не сможет завершить 3DS")
	}
}

// TestPGCompat_BusinessDeclines_400Response — PG checkResponse парсит epayError.
func TestPGCompat_BusinessDeclines_400Response(t *testing.T) {
	t.Parallel()
	cases := []struct {
		preset   string
		wantCode int
	}{
		{"epay_insufficient_funds", 484},
		{"epay_card_expired", 478},
		{"epay_invalid_card", 457},
		{"epay_declined_by_issuer", 455},
		{"epay_limit_exceeded", 486},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.preset, func(t *testing.T) {
			t.Parallel()
			st := newCompatStand(t, false)
			st.Scenarios.ApplyPreset(tc.preset)
			defer st.Scenarios.Reset()

			body, _ := json.Marshal(map[string]any{
				"amount":     1000,
				"terminalId": "test-terminal",
				"invoiceId":  "000300",
				"currency":   "KZT",
				"cryptogram": "x",
			})
			resp, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 (PG checkResponse=BadRequest path)", resp.StatusCode)
			}
			var ee pgEpayError
			if err := json.NewDecoder(resp.Body).Decode(&ee); err != nil {
				t.Fatalf("PG SDK epayError failed to decode: %v", err)
			}
			if ee.Code != tc.wantCode {
				t.Fatalf("code: got %d, want %d (PG GetError() даст не ту ошибку)", ee.Code, tc.wantCode)
			}
		})
	}
}

// TestPGCompat_Charge — approveResponse парсится.
func TestPGCompat_Charge(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, false)

	// 1) cryptopay → создаём запись
	body, _ := json.Marshal(map[string]any{
		"amount":     1000,
		"terminalId": "test-terminal",
		"invoiceId":  "000400",
		"currency":   "KZT",
		"cryptogram": "x",
	})
	resp1, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	defer resp1.Body.Close()
	var pr pgPaymentResponse
	_ = json.NewDecoder(resp1.Body).Decode(&pr)

	// 2) charge → списание
	chargeBody, _ := json.Marshal(map[string]any{"amount": 1000})
	resp2, _ := authedPost(st.Server.URL+"/api/operation/"+pr.ID+"/charge", chargeBody)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("charge status: %d", resp2.StatusCode)
	}
	var ar pgApproveResponse
	if err := json.NewDecoder(resp2.Body).Decode(&ar); err != nil {
		t.Fatalf("PG SDK approveResponse failed to decode: %v", err)
	}
	if ar.Code != 0 {
		t.Fatalf("charge Code: got %d, want 0", ar.Code)
	}
}

// TestPGCompat_PostlinkPayload — PG webhookRequest парсит наш success postlink.
func TestPGCompat_PostlinkPayload(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, true) // AutoWebhook=true

	body, _ := json.Marshal(map[string]any{
		"amount":     2500,
		"terminalId": "test-terminal",
		"invoiceId":  "000500",
		"currency":   "KZT",
		"cryptogram": "x",
		"accountId":  "12345",
	})
	resp1, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	defer resp1.Body.Close()
	var pr pgPaymentResponse
	_ = json.NewDecoder(resp1.Body).Decode(&pr)

	// charge → triggers AutoWebhook
	chargeBody, _ := json.Marshal(map[string]any{"amount": 2500})
	resp2, _ := authedPost(st.Server.URL+"/api/operation/"+pr.ID+"/charge", chargeBody)
	_ = resp2.Body.Close()

	if !waitFor(2*time.Second, func() bool { return st.OkHits.count() > 0 }) {
		t.Fatalf("success postlink не пришёл в течение 2s")
	}

	var wh pgWebhookRequest
	if err := json.Unmarshal(st.OkHits.last(), &wh); err != nil {
		t.Fatalf("PG SDK webhookRequest failed to decode: %v\npayload: %s", err, st.OkHits.last())
	}
	if wh.InvoiceID == "" {
		t.Fatalf("invoiceId empty — PG getPaymentDTO.Atoi(invoiceID) упадёт")
	}
	if _, err := strconv.Atoi(wh.InvoiceID); err != nil {
		t.Fatalf("invoiceId %q не парсится в int: %v", wh.InvoiceID, err)
	}
	if wh.Reference == "" {
		t.Fatalf("reference empty — PG Acquirer.Reference будет пуст")
	}
	if wh.ID == "" {
		t.Fatalf("id empty — PG PaymentID будет пуст")
	}
	if wh.CardMask == "" {
		t.Fatalf("cardMask empty — vo.NewMask упадёт")
	}
	if wh.Code != "ok" {
		t.Fatalf("code: got %q, want ok", wh.Code)
	}
}

// TestPGCompat_BindWebhookPayload — после cardSave=true идёт bind-postlink,
// который парсится bindCallbackRequest.
func TestPGCompat_BindWebhookPayload(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, true)

	body, _ := json.Marshal(map[string]any{
		"amount":     100, // bind обычно 0.1 GEL/USD/KZT
		"terminalId": "test-terminal",
		"invoiceId":  "000600",
		"currency":   "KZT",
		"cryptogram": "x",
		"accountId":  "12345",
		"cardSave":   true,
	})
	resp, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	defer resp.Body.Close()

	// После Bug B fix: автоматически отправится bind-postlink.
	if !waitFor(2*time.Second, func() bool { return st.BindHits.count() > 0 }) {
		t.Fatalf("bind postlink не пришёл — Bug B (auto bind) не починен")
	}
	// Regular postlink НЕ должен прилететь для bind-flow.
	if st.OkHits.count() > 0 {
		t.Fatalf("regular postlink не должен слаться для bind: %s", st.OkHits.last())
	}

	var bc pgBindCallbackRequest
	if err := json.Unmarshal(st.BindHits.last(), &bc); err != nil {
		t.Fatalf("PG SDK bindCallbackRequest failed to decode: %v\npayload: %s", err, st.BindHits.last())
	}
	// PG-обязательные поля валидации.
	if bc.InvoiceID == "" {
		t.Fatalf("invoiceId empty → PG errInvalidInvoiceID")
	}
	if bc.CardID == "" {
		t.Fatalf("cardId empty → PG errInvalidCardID")
	}
	if bc.CardMask == "" {
		t.Fatalf("cardMask empty → PG errEmptyCardMask")
	}
	if bc.AccountID == "" {
		t.Fatalf("accountId empty → PG Atoi(accountId) упадёт")
	}
	if _, err := strconv.Atoi(bc.AccountID); err != nil {
		t.Fatalf("accountId %q не парсится в int (user_id): %v", bc.AccountID, err)
	}
	if bc.Code != "success" {
		t.Fatalf("code: got %q, want success — иначе PG IsFailed=true", bc.Code)
	}
	if bc.ReasonCode != 0 {
		t.Fatalf("reasonCode: got %d, want 0 — иначе PG IsFailed=true", bc.ReasonCode)
	}
}

// TestPGCompat_FailurePostlink — failure_postlink парсится тем же webhookRequest.
func TestPGCompat_FailurePostlink(t *testing.T) {
	t.Parallel()
	st := newCompatStand(t, false)

	// 1) cryptopay
	body, _ := json.Marshal(map[string]any{
		"amount":     1000,
		"terminalId": "test-terminal",
		"invoiceId":  "000700",
		"currency":   "KZT",
		"cryptogram": "x",
		"accountId":  "12345",
	})
	resp, _ := authedPost(st.Server.URL+"/api/payment/cryptopay", body)
	defer resp.Body.Close()
	var pr pgPaymentResponse
	_ = json.NewDecoder(resp.Body).Decode(&pr)

	// 2) Ручная отправка failure через service (как кнопка в panel)
	pidU64, _ := strconv.ParseUint(strings.TrimPrefix(pr.ID, "mock-epay-"), 10, 64)
	if err := st.Svc.SendEpayPostlink(uint(pidU64), false, ""); err != nil {
		t.Fatalf("SendEpayPostlink: %v", err)
	}

	if !waitFor(2*time.Second, func() bool { return st.FailHits.count() > 0 }) {
		t.Fatalf("failure postlink не пришёл")
	}
	var wh pgWebhookRequest
	if err := json.Unmarshal(st.FailHits.last(), &wh); err != nil {
		t.Fatalf("PG SDK failure webhook failed to decode: %v", err)
	}
	if wh.Code != "error" {
		t.Fatalf("code: got %q, want error — PG getFailDTO ждёт error", wh.Code)
	}
	if wh.ReasonCode == 0 {
		t.Fatalf("reasonCode 0 — PG GetError(0) даст ErrDefault, потеря классификации")
	}
}

// ---- helpers ----

func authedPost(url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer any-token-mock-accepts")
	return http.DefaultClient.Do(req)
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
