package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

func newEpayPanel(t *testing.T) (*http.ServeMux, *apppay.Service, *appscenario.Service) {
	t.Helper()

	svc := apppay.NewService(memstore.NewPayRepo(), nil, nil, nil, nil, apppay.AutoWebhookConfig{})
	scenarios := appscenario.NewService(memstore.NewScenarioStore())
	c := &Controller{pay: svc, scenarios: scenarios}
	mux := http.NewServeMux()
	c.Register(mux)

	return mux, svc, scenarios
}

func lateInput(invoiceID string) apppay.EpayAuthorizeInput {
	return apppay.EpayAuthorizeInput{Amount: 5000, InvoiceID: invoiceID, HasCryptogram: true, PaymentType: "googlePay"}
}

func TestHandleEpayReveal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		visibleAt time.Time
		wantCode  int
	}{
		{"скрытая операция раскрывается", time.Now().Add(10 * time.Minute), http.StatusSeeOther},
		{"уже видимую операцию раскрывать нечего", time.Time{}, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux, svc, _ := newEpayPanel(t)
			rec, err := svc.EpayLateOperation(lateInput("000700"), false, tt.visibleAt)
			if err != nil {
				t.Fatalf("EpayLateOperation: %v", err)
			}

			resp := httptest.NewRecorder()
			mux.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/panel/payments/000700/reveal", nil))

			if resp.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", resp.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusSeeOther && resp.Header().Get("Location") != "/panel?bank=epay&tab=cards" {
				t.Fatalf("Location = %q", resp.Header().Get("Location"))
			}

			got, _ := svc.Repo().Get(rec.PaymentID)
			if got.EpayHidden(time.Now()) {
				t.Fatal("операция осталась скрытой")
			}
		})
	}
}

func TestHandleEpayReveal_UnknownInvoice(t *testing.T) {
	t.Parallel()

	mux, _, _ := newEpayPanel(t)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/panel/payments/999999/reveal", nil))

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.Code)
	}
}

func TestHandleScenarioPreset_InvalidVisibleAfter(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"abc", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			mux, _, scenarios := newEpayPanel(t)
			form := url.Values{"preset": {"epay_late_auth_postlink_lost"}, "bank": {"epay"}, "visible_after": {value}}
			req := httptest.NewRequest(http.MethodPost, "/panel/scenarios/preset", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp := httptest.NewRecorder()

			mux.ServeHTTP(resp, req)

			if resp.Code == http.StatusSeeOther || !strings.Contains(resp.Body.String(), "visible_after") {
				t.Fatalf("status = %d body = %q, want страница ошибки про visible_after", resp.Code, resp.Body.String())
			}
			if n := len(scenarios.List()); n != 0 {
				t.Fatalf("scenarios = %d, want 0", n)
			}
		})
	}
}
