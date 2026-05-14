package epay_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	epayports "github.com/vevovip/chaospay/internal/ports/api/epay"
)

// testStand — минимальный DI-стенд для httptest.
type testStand struct {
	Server    *httptest.Server
	Svc       *apppay.Service
	Scenarios *appscenario.Service
}

// newTestServer собирает минимальный стенд: in-memory репо + Epay-контроллер.
// Webhook-клиент с пустыми URL — postlink не уйдёт (AutoWebhook=false).
func newTestServer(t *testing.T) (*httptest.Server, *apppay.Service) {
	t.Helper()
	st := newTestStand(t)
	return st.Server, st.Svc
}

func newTestStand(t *testing.T) *testStand {
	t.Helper()
	payRepo := memstore.NewPayRepo()
	scenarioStore := memstore.NewScenarioStore()
	requestLog := memstore.NewRequestLog(0)
	tokens := infraepay.NewTokenStore()

	svc := apppay.NewService(payRepo, nil, nil, nil, false)
	scenarios := appscenario.NewService(scenarioStore)
	webhook := pgclient.NewEpayClient("", "", "")

	ctrl := epayports.NewController(svc, scenarios, requestLog, tokens, webhook, epayports.Config{
		TerminalUUID: "test-terminal",
	})

	mux := http.NewServeMux()
	ctrl.Register(mux)
	return &testStand{
		Server:    httptest.NewServer(mux),
		Svc:       svc,
		Scenarios: scenarios,
	}
}

func TestEpay_OAuthToken_FormURLEncoded(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	body := "grant_type=client_credentials&client_id=test&client_secret=secret&invoiceID=000123&amount=5000&currency=KZT&terminal=test-terminal"
	resp, err := http.Post(srv.URL+"/oauth2/token", "application/x-www-form-urlencoded", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out infraepay.TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.AccessToken == "" {
		t.Error("access_token should not be empty")
	}
	if out.TokenType != "Bearer" {
		t.Errorf("token_type = %s, want Bearer", out.TokenType)
	}
}

func TestEpay_CryptopayHappyPath(t *testing.T) {
	srv, svc := newTestServer(t)
	defer srv.Close()

	reqJSON := `{"amount":5000,"invoiceId":"000123","currency":"KZT","cryptogram":"base64data","description":"test"}`
	resp, err := http.Post(srv.URL+"/api/payment/cryptopay", "application/json", strings.NewReader(reqJSON))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out infraepay.AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.ID == "" {
		t.Error("response.id should not be empty")
	}
	if out.Secure3D != nil {
		t.Errorf("happy path should have secure3D=nil, got %+v", out.Secure3D)
	}
	if out.Amount != 5000 {
		t.Errorf("amount = %d, want 5000", out.Amount)
	}
	// Record должен быть создан в репозитории.
	if len(svc.Repo().List()) != 1 {
		t.Errorf("repo should have 1 record, got %d", len(svc.Repo().List()))
	}
}

func TestEpay_ChargeFlow(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	// 1. Cryptopay создаёт запись в Authorized.
	reqJSON := `{"amount":1000,"invoiceId":"000001","currency":"KZT","cryptogram":"x"}`
	resp := mustPost(t, srv.URL+"/api/payment/cryptopay", "application/json", reqJSON)
	defer resp.Body.Close()
	var auth infraepay.AuthorizeResponse
	_ = json.NewDecoder(resp.Body).Decode(&auth)
	if auth.ID == "" {
		t.Fatal("authorize did not return id")
	}

	// 2. Charge.
	chResp, err := http.Post(srv.URL+"/api/operation/"+auth.ID+"/charge", "application/json", strings.NewReader(`{"amount":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	defer chResp.Body.Close()
	if chResp.StatusCode != 200 {
		t.Fatalf("charge status = %d, want 200", chResp.StatusCode)
	}
	var op infraepay.OperationResponse
	_ = json.NewDecoder(chResp.Body).Decode(&op)
	if op.Code != 0 {
		t.Errorf("charge code = %d, want 0", op.Code)
	}
}

func TestEpay_RefundOnNonChargedFails(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	// Authorize → попытка Refund сразу должна провалиться.
	reqJSON := `{"amount":1000,"invoiceId":"000002","currency":"KZT","cryptogram":"x"}`
	resp := mustPost(t, srv.URL+"/api/payment/cryptopay", "application/json", reqJSON)
	defer resp.Body.Close()
	var auth infraepay.AuthorizeResponse
	_ = json.NewDecoder(resp.Body).Decode(&auth)

	rfResp, err := http.Post(srv.URL+"/api/operation/"+auth.ID+"/refund?amount=500", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	defer rfResp.Body.Close()
	if rfResp.StatusCode != 400 {
		t.Errorf("refund without charge should return 400, got %d", rfResp.StatusCode)
	}
}

func TestEpay_BankInvalidJSONReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/payment/cryptopay", "application/json", strings.NewReader(`{"invalid"`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("invalid JSON → 400, got %d", resp.StatusCode)
	}
}
