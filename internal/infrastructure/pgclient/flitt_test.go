package pgclient

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

func newFlittRecord() *pay.Record {
	return &pay.Record{
		Bank:              bank.Flitt,
		PaymentID:         1779000001,
		OrderID:           100500,
		MerchantID:        1549901,
		Amount:            5000,
		Currency:          "GEL",
		CardPAN:           "444455XXXXXX1111",
		CardBrand:         "VISA",
		UserEmail:         "user@example.com",
		UserPhone:         "+995111222333",
		FlittPaymentID:    615333000001,
		FlittApprovalCode: "123456",
		FlittRRN:          "001779000001",
		FlittRectoken:     "tok-1",
		FlittMerchantData: `{"x":"y"}`,
		CreatedAt:         time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}
}

// captureServer ловит входящий POST и сохраняет тело.
type captureServer struct {
	srv   *httptest.Server
	hits  atomic.Int32
	last  atomic.Pointer[infraflitt.CallbackPayload]
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	cs := &captureServer{}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p infraflitt.CallbackPayload
		_ = json.Unmarshal(body, &p)
		cs.last.Store(&p)
		cs.hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	return cs
}

func TestFlittClient_SendCallback_Success(t *testing.T) {
	t.Parallel()
	cs := newCaptureServer(t)
	defer cs.srv.Close()

	client := NewFlittClient(cs.srv.URL, cs.srv.URL+"/bind", "test")
	code, err := client.SendCallback(newFlittRecord(), true)
	if err != nil {
		t.Fatalf("SendCallback: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("status: got %d", code)
	}
	got := cs.last.Load()
	if got == nil {
		t.Fatalf("server didn't receive payload")
	}
	if got.OrderStatus != infraflitt.OrderStatusApproved {
		t.Fatalf("order_status: got %s, want approved", got.OrderStatus)
	}
	if got.ResponseStatus != infraflitt.ResponseStatusSuccess {
		t.Fatalf("response_status: got %s", got.ResponseStatus)
	}
	if got.Amount != "5000" {
		t.Fatalf("amount: got %s", got.Amount)
	}
	if got.Currency != "GEL" {
		t.Fatalf("currency: got %s", got.Currency)
	}
	if got.PaymentID != 615333000001 {
		t.Fatalf("payment_id: got %d", got.PaymentID)
	}
	if got.Signature == "" {
		t.Fatalf("signature must be present")
	}
}

func TestFlittClient_SendCallback_Failure(t *testing.T) {
	t.Parallel()
	cs := newCaptureServer(t)
	defer cs.srv.Close()

	client := NewFlittClient(cs.srv.URL, "", "test")
	if _, err := client.SendCallback(newFlittRecord(), false); err != nil {
		t.Fatalf("SendCallback: %v", err)
	}
	got := cs.last.Load()
	if got.OrderStatus != infraflitt.OrderStatusDeclined {
		t.Fatalf("order_status: got %s, want declined", got.OrderStatus)
	}
	if got.ResponseStatus != infraflitt.ResponseStatusFailure {
		t.Fatalf("response_status: got %s", got.ResponseStatus)
	}
}

func TestFlittClient_SendBindCallback_HitsBindURL(t *testing.T) {
	t.Parallel()
	main := newCaptureServer(t)
	defer main.srv.Close()
	bindSrv := newCaptureServer(t)
	defer bindSrv.srv.Close()

	client := NewFlittClient(main.srv.URL, bindSrv.srv.URL, "test")
	if _, err := client.SendBindCallback(newFlittRecord(), true); err != nil {
		t.Fatalf("SendBindCallback: %v", err)
	}
	if main.hits.Load() != 0 {
		t.Fatalf("main webhook should not be hit, got %d", main.hits.Load())
	}
	if bindSrv.hits.Load() != 1 {
		t.Fatalf("bind webhook hits: got %d, want 1", bindSrv.hits.Load())
	}
	got := bindSrv.last.Load()
	if got.VerificationStatus != "verified" {
		t.Fatalf("verification_status: got %s", got.VerificationStatus)
	}
}

func TestFlittClient_SendBindCallback_Declined(t *testing.T) {
	t.Parallel()
	cs := newCaptureServer(t)
	defer cs.srv.Close()

	client := NewFlittClient("", cs.srv.URL, "test")
	if _, err := client.SendBindCallback(newFlittRecord(), false); err != nil {
		t.Fatalf("SendBindCallback: %v", err)
	}
	got := cs.last.Load()
	if got.VerificationStatus != "declined" {
		t.Fatalf("verification_status: got %s, want declined", got.VerificationStatus)
	}
}

func TestFlittClient_NilRecord(t *testing.T) {
	t.Parallel()
	client := NewFlittClient("", "", "")
	if _, err := client.SendCallback(nil, true); err == nil {
		t.Fatalf("expected error on nil record")
	}
	if _, err := client.SendBindCallback(nil, true); err == nil {
		t.Fatalf("expected error on nil bind record")
	}
}

func TestFlittClient_EmptyURLReturnsError(t *testing.T) {
	t.Parallel()
	client := NewFlittClient("", "", "test")
	if _, err := client.SendCallback(newFlittRecord(), true); err == nil {
		t.Fatalf("expected error when URL is empty")
	}
}

func TestFlittClient_SignatureIsDeterministic(t *testing.T) {
	t.Parallel()
	cs := newCaptureServer(t)
	defer cs.srv.Close()

	client := NewFlittClient(cs.srv.URL, "", "test")
	_, _ = client.SendCallback(newFlittRecord(), true)
	first := cs.last.Load().Signature

	_, _ = client.SendCallback(newFlittRecord(), true)
	second := cs.last.Load().Signature

	if first != second {
		t.Fatalf("signature must be deterministic, got %s vs %s", first, second)
	}
	if len(first) != 40 {
		t.Fatalf("signature must be SHA1 hex (40 chars), got %d", len(first))
	}
	if strings.ContainsAny(first, "ABCDEF") {
		t.Fatalf("signature must be lowercase hex")
	}
}

func TestFlittClient_EmptySecretSkipsSignature(t *testing.T) {
	t.Parallel()
	cs := newCaptureServer(t)
	defer cs.srv.Close()

	// Без secret подпись не вычисляется — поле остаётся пустым.
	client := NewFlittClient(cs.srv.URL, "", "")
	if _, err := client.SendCallback(newFlittRecord(), true); err != nil {
		t.Fatalf("SendCallback: %v", err)
	}
	if got := cs.last.Load().Signature; got != "" {
		t.Fatalf("signature must be empty when secret is empty, got %s", got)
	}
}
