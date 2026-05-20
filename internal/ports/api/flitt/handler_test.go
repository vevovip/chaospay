package flitt_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	flittports "github.com/vevovip/chaospay/internal/ports/api/flitt"
)

type testStand struct {
	Server    *httptest.Server
	Svc       *apppay.Service
	Scenarios *appscenario.Service
}

func newTestStand(t *testing.T) *testStand {
	t.Helper()
	payRepo := memstore.NewPayRepo()
	scenarioStore := memstore.NewScenarioStore()
	requestLog := memstore.NewRequestLog(0)
	scenarios := appscenario.NewService(scenarioStore)
	// Flitt webhook без URL — не уходит наружу, но используется в SyncErrorAsyncWebhook.
	wh := pgclient.NewFlittClient("", "", "test")
	svc := apppay.NewService(payRepo, nil, nil, nil, wh, apppay.AutoWebhookConfig{})

	ctrl := flittports.NewController(svc, scenarios, requestLog, wh, flittports.Config{
		Secret:        "test",
		MerchantID:    1549901,
		HostedFormURL: "http://localhost:48532/panel?bank=flitt&tab=cards",
		AutoWebhook:   false,
	})
	mux := http.NewServeMux()
	ctrl.Register(mux)
	return &testStand{
		Server:    httptest.NewServer(mux),
		Svc:       svc,
		Scenarios: scenarios,
	}
}

// post — обёртка над POST {request: ...} JSON-вызовом.
func post[T any](t *testing.T, url string, body any) T {
	t.Helper()
	buf, err := json.Marshal(map[string]any{"request": body})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		t.Fatalf("unmarshal %s: %v (body=%s)", url, err, respBody)
	}
	return out
}

func TestCheckoutURL_ReturnsCheckoutURL(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	out := post[infraflitt.CheckoutEnvelope](t, st.Server.URL+"/api/checkout/url",
		map[string]any{
			"order_id":    "100500",
			"amount":      5000,
			"currency":    "GEL",
			"order_desc":  "test",
			"merchant_id": 1549901,
			"signature":   "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusSuccess {
		t.Fatalf("status: %s, err=%s", out.Response.ResponseStatus, out.Response.ErrorMessage)
	}
	if out.Response.CheckoutURL == "" {
		t.Fatalf("checkout_url empty")
	}
}

func TestDirect_ApprovedReturnsAuthorized(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	out := post[infraflitt.DirectEnvelope](t, st.Server.URL+"/api/3dsecure_step1",
		map[string]any{
			"order_id":            "1",
			"merchant_id":         1549901,
			"amount":              100,
			"currency":            "GEL",
			"order_desc":          "test",
			"container":           "encrypted-token",
			"server_callback_url": "http://pg/callback",
			"signature":           "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusSuccess {
		t.Fatalf("status: %s", out.Response.ResponseStatus)
	}
	if out.Response.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("order_status: %s", out.Response.OrderStatus)
	}
	if out.Response.Rectoken == "" {
		t.Fatalf("rectoken should be issued")
	}
}

func TestRecurring_RequiresToken(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// Без rectoken
	out := post[infraflitt.ErrorResponse](t, st.Server.URL+"/api/recurring",
		map[string]any{
			"order_id":    "1",
			"merchant_id": 1549901,
			"amount":      100,
			"currency":    "GEL",
			"order_desc":  "test",
			"signature":   "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("expected failure, got %s", out.Response.ResponseStatus)
	}
}

func TestRecurring_WithTokenApproved(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	out := post[infraflitt.RecurringEnvelope](t, st.Server.URL+"/api/recurring",
		map[string]any{
			"order_id":    "1",
			"merchant_id": 1549901,
			"amount":      100,
			"currency":    "GEL",
			"order_desc":  "test",
			"rectoken":    "stored-token-1",
			"signature":   "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusSuccess {
		t.Fatalf("status: %s", out.Response.ResponseStatus)
	}
	if out.Response.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("order_status: %s", out.Response.OrderStatus)
	}
}

func TestCaptureReverseStatus_FullFlow(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// 1) recurring → authorized
	rec := post[infraflitt.RecurringEnvelope](t, st.Server.URL+"/api/recurring",
		map[string]any{
			"order_id":    "9001",
			"merchant_id": 1549901,
			"amount":      5000,
			"currency":    "GEL",
			"order_desc":  "test",
			"rectoken":    "stored-token-1",
			"signature":   "x",
		},
	)
	if rec.Response.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("recurring status: %s", rec.Response.OrderStatus)
	}

	// 2) capture
	cap := post[infraflitt.CaptureEnvelope](t, st.Server.URL+"/api/capture/order_id",
		map[string]any{
			"order_id":    "9001",
			"amount":      5000,
			"currency":    "GEL",
			"merchant_id": 1549901,
			"version":     "1.0.1",
			"signature":   "x",
		},
	)
	if cap.Response.CaptureStatus != infraflitt.CaptureStatusCaptured {
		t.Fatalf("capture: %s err=%s", cap.Response.CaptureStatus, cap.Response.ErrorMessage)
	}

	// 3) status
	st2 := post[infraflitt.StatusEnvelope](t, st.Server.URL+"/api/status/order_id",
		map[string]any{
			"order_id":    "9001",
			"merchant_id": 1549901,
			"version":     "1.0.1",
			"signature":   "x",
		},
	)
	if st2.Response.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("status order: %s", st2.Response.OrderStatus)
	}

	// 4) reverse (full refund)
	rev := post[infraflitt.ReverseEnvelope](t, st.Server.URL+"/api/reverse/order_id",
		map[string]any{
			"order_id":    "9001",
			"amount":      5000,
			"currency":    "GEL",
			"merchant_id": 1549901,
			"version":     "1.0.1",
			"signature":   "x",
		},
	)
	if rev.Response.ReverseStatus != infraflitt.ReverseStatusApproved {
		t.Fatalf("reverse: %s err=%s", rev.Response.ReverseStatus, rev.Response.ErrorMessage)
	}
}

func TestStatus_NotFound(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	out := post[infraflitt.ErrorResponse](t, st.Server.URL+"/api/status/order_id",
		map[string]any{
			"order_id":    "777777",
			"merchant_id": 1549901,
			"version":     "1.0.1",
			"signature":   "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("expected failure, got %s", out.Response.ResponseStatus)
	}
}

func TestStep2_TransitionsAuthorized(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	// 1) direct с 3DS-исходом — нужен сценарий force_3ds
	st.Scenarios.ApplyPreset("flitt_3ds_decline") // включает Force3DS для direct/recurring + decline на step2
	defer st.Scenarios.Reset()

	rec := post[infraflitt.DirectEnvelope](t, st.Server.URL+"/api/3dsecure_step1",
		map[string]any{
			"order_id":            "1",
			"merchant_id":         1549901,
			"amount":              100,
			"currency":            "GEL",
			"order_desc":          "test",
			"container":           "encrypted",
			"server_callback_url": "http://pg/callback",
			"signature":           "x",
		},
	)
	if rec.Response.ACSURL == "" {
		t.Fatalf("acs_url must be set when 3DS scenario active")
	}

	// 2) step2 — у нас flitt_3ds_decline ставит ActionForceFailure → отдаёт failure
	out := post[infraflitt.ErrorResponse](t, st.Server.URL+"/api/3dsecure_step2",
		map[string]any{
			"merchant_id": "1549901",
			"order_id":    "1",
			"pares":       "PARES",
			"md":          "MD",
			"signature":   "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("expected failure (3DS decline preset), got %s", out.Response.ResponseStatus)
	}
}

func TestScenario_InsufficientFunds(t *testing.T) {
	t.Parallel()
	st := newTestStand(t)
	defer st.Server.Close()

	st.Scenarios.ApplyPreset("flitt_insufficient_funds")
	defer st.Scenarios.Reset()

	out := post[infraflitt.DirectEnvelope](t, st.Server.URL+"/api/3dsecure_step1",
		map[string]any{
			"order_id":            "1",
			"merchant_id":         1549901,
			"amount":              100,
			"currency":            "GEL",
			"order_desc":          "test",
			"container":           "x",
			"server_callback_url": "http://pg/callback",
			"signature":           "x",
		},
	)
	if out.Response.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("expected failure, got %s", out.Response.ResponseStatus)
	}
	if out.Response.OrderStatus != infraflitt.OrderStatusDeclined {
		t.Fatalf("order_status: %s", out.Response.OrderStatus)
	}
}
