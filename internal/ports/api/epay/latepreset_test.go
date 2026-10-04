package epay_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	domainbank "github.com/vevovip/chaospay/internal/domain/bank"
	domainscenario "github.com/vevovip/chaospay/internal/domain/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	epayports "github.com/vevovip/chaospay/internal/ports/api/epay"
)

// latePay шлёт cryptopay Google Pay и возвращает ответ мока целиком.
func latePay(t *testing.T, baseURL, invoiceID string) (int, string) {
	t.Helper()
	body := `{"amount":5000,"invoiceId":"` + invoiceID + `","currency":"KZT","paymentType":"googlePay","cryptogram":"x"}`
	resp := mustPost(t, baseURL+"/api/payment/cryptopay", "application/json", body)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(raw)
}

// statusByInvoice спрашивает состояние операции по инвойсу.
func statusByInvoice(t *testing.T, baseURL, invoiceID string) (int, infraepay.StatusResponse, string) {
	t.Helper()
	resp := mustGet(t, baseURL+"/check-status/payment/transaction/"+invoiceID)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var sr infraepay.StatusResponse
	_ = json.Unmarshal(raw, &sr)

	return resp.StatusCode, sr, string(raw)
}

func TestPreset_LateOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		preset       string
		visibleAfter string
		wantStatus   string
		wantName     string
		moneyBack    string // cancel или refund
		wantFinal    string
	}{
		{"холд виден сразу", "epay_late_auth_postlink_lost", "", "AUTH", "Авторизован", "cancel", "CANCEL"},
		{"списание видно сразу", "epay_late_charge_postlink_lost", "", "CHARGE", "Списан", "refund", "REFUND"},
		{"холд скрыт до reveal", "epay_late_auth_postlink_lost", "600", "AUTH", "Авторизован", "cancel", "CANCEL"},
		{"списание скрыто до reveal", "epay_late_charge_postlink_lost", "600", "CHARGE", "Списан", "refund", "REFUND"},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newTestStand(t)
			defer st.Server.Close()

			if err := st.Scenarios.ApplyPresetWithParams(tt.preset, map[string]string{"visible_after": tt.visibleAfter}); err != nil {
				t.Fatalf("ApplyPresetWithParams: %v", err)
			}
			invoiceID := "00080" + string(rune('0'+i))

			code, body := latePay(t, st.Server.URL, invoiceID)
			if code != http.StatusInternalServerError || strings.TrimSpace(body) != "Internal Server Error" {
				t.Fatalf("cryptopay = %d %q, want 500 как у epay_cryptopay_500", code, body)
			}

			if tt.visibleAfter != "" {
				code, _, raw := statusByInvoice(t, st.Server.URL, invoiceID)
				if code != http.StatusBadRequest || !strings.Contains(raw, "operation not found") {
					t.Fatalf("hidden status = %d %s, want 400 operation not found", code, raw)
				}

				if n := st.Svc.EpayRevealInvoice(invoiceID); n != 1 {
					t.Fatalf("revealed = %d, want 1", n)
				}
			}

			code, sr, raw := statusByInvoice(t, st.Server.URL, invoiceID)
			if code != http.StatusOK || sr.Status != tt.wantStatus || sr.StatusName != tt.wantName {
				t.Fatalf("status = %d %s, want %s/%s", code, raw, tt.wantStatus, tt.wantName)
			}

			back := mustPost(t, st.Server.URL+"/api/operation/"+sr.ID+"/"+tt.moneyBack, "application/json", `{}`)
			back.Body.Close()
			if back.StatusCode != http.StatusOK {
				t.Fatalf("%s = %d, want 200", tt.moneyBack, back.StatusCode)
			}

			if _, sr, raw := statusByInvoice(t, st.Server.URL, invoiceID); sr.Status != tt.wantFinal {
				t.Fatalf("status after %s = %s, want %s", tt.moneyBack, raw, tt.wantFinal)
			}
		})
	}
}

func TestPreset_LateOperation_HiddenByTransactionID(t *testing.T) {
	t.Parallel()

	st := newTestStand(t)
	defer st.Server.Close()

	if err := st.Scenarios.ApplyPresetWithParams("epay_late_auth_postlink_lost", map[string]string{"visible_after": "600"}); err != nil {
		t.Fatalf("ApplyPresetWithParams: %v", err)
	}
	latePay(t, st.Server.URL, "000901")

	var epayID string
	for _, rec := range st.Svc.Repo().List() {
		epayID = rec.EpayID
	}

	resp := mustGet(t, st.Server.URL+"/check-status/payment/transactionId/"+epayID)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status by id = %d, want 400 пока операция скрыта", resp.StatusCode)
	}
}

func TestPreset_LateOperation_ConsumedOnce(t *testing.T) {
	t.Parallel()

	st := newTestStand(t)
	defer st.Server.Close()

	st.Scenarios.ApplyPreset("epay_late_auth_postlink_lost")
	latePay(t, st.Server.URL, "000902")

	if code, body := latePay(t, st.Server.URL, "000903"); code != http.StatusOK {
		t.Fatalf("second cryptopay = %d %s, want 200: сценарий одноразовый", code, body)
	}
}

func TestPreset_LateOperation_NoPostlink(t *testing.T) {
	t.Parallel()

	postlinks := make(chan string, 8)
	pg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p infraepay.PostlinkPayload
		_ = json.NewDecoder(r.Body).Decode(&p)
		postlinks <- p.InvoiceID
	}))
	defer pg.Close()

	svc := apppay.NewService(memstore.NewPayRepo(), nil, nil, nil, nil, apppay.AutoWebhookConfig{})
	scenarios := appscenario.NewService(memstore.NewScenarioStore())
	ctrl := epayports.NewController(svc, scenarios, memstore.NewRequestLog(0), infraepay.NewTokenStore(),
		pgclient.NewEpayClient(pg.URL, pg.URL, pg.URL), epayports.Config{TerminalUUID: "test-terminal", AutoWebhook: true})
	mux := http.NewServeMux()
	ctrl.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	scenarios.ApplyPreset("epay_late_charge_postlink_lost")
	latePay(t, srv.URL, "000904")
	// обычная оплата шлёт постлинк: дождавшись его, убеждаемся, что поздней операции постлинк не ушёл
	latePay(t, srv.URL, "000905")

	select {
	case got := <-postlinks:
		if got != "000905" {
			t.Fatalf("postlink for %s, want only 000905", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("postlink обычной оплаты не пришёл")
	}

	select {
	case got := <-postlinks:
		t.Fatalf("unexpected postlink for %s", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPreset_LateOperation_VisibleAfterExpires(t *testing.T) {
	t.Parallel()

	st := newTestStand(t)
	defer st.Server.Close()

	if err := st.Scenarios.ApplyPresetWithParams("epay_late_charge_postlink_lost", map[string]string{"visible_after": "1"}); err != nil {
		t.Fatalf("ApplyPresetWithParams: %v", err)
	}
	latePay(t, st.Server.URL, "000906")

	if code, _, raw := statusByInvoice(t, st.Server.URL, "000906"); code != http.StatusBadRequest {
		t.Fatalf("status до срока = %d %s, want 400", code, raw)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		code, sr, raw := statusByInvoice(t, st.Server.URL, "000906")
		if code == http.StatusOK {
			if sr.Status != "CHARGE" {
				t.Fatalf("status после срока = %s, want CHARGE", raw)
			}

			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("операция не стала видна после visible_after без reveal: %d %s", code, raw)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPreset_LateOperation_InvalidVisibleAfterInScenario(t *testing.T) {
	t.Parallel()

	st := newTestStand(t)
	defer st.Server.Close()

	st.Scenarios.Add(&domainscenario.Scenario{
		Bank: domainbank.Epay, Endpoint: domainscenario.EndpointEpayCryptopay,
		PaymentID: domainscenario.Wildcard, OrderID: domainscenario.Wildcard, MerchantID: domainscenario.Wildcard,
		Action: domainscenario.ActionEpayLateOperation, ConsumeOnce: true,
		Params: map[string]string{"status": "AUTH", "visible_after": "abc"},
	})

	if code, body := latePay(t, st.Server.URL, "000907"); code != http.StatusBadRequest || !strings.Contains(body, "visible_after") {
		t.Fatalf("cryptopay = %d %s, want 400 про visible_after", code, body)
	}
	if n := len(st.Svc.Repo().List()); n != 0 {
		t.Fatalf("операция заведена при битом visible_after: %d", n)
	}
}
