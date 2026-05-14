package epay_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
)

// authorize создаёт Halyk-платёж через cryptopay и возвращает его ID.
// Использует уникальный invoiceId, чтобы тесты не конфликтовали.
func authorize(t *testing.T, baseURL, invoiceID string) string {
	t.Helper()
	body := `{"amount":5000,"invoiceId":"` + invoiceID + `","currency":"KZT","cryptogram":"x"}`
	resp := mustPost(t, baseURL+"/api/payment/cryptopay", "application/json", body)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize failed: %d %s", resp.StatusCode, string(raw))
	}
	var auth infraepay.AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&auth); err != nil {
		t.Fatal(err)
	}
	if auth.ID == "" {
		t.Fatal("authorize: empty id")
	}
	return auth.ID
}

// mustPost — http.Post с t.Fatal на ошибке (vet-friendly).
func mustPost(t *testing.T, url, contentType, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// mustGet — http.Get с t.Fatal на ошибке.
func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPreset_InsufficientFunds(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_insufficient_funds")

	resp, err := http.Post(st.Server.URL+"/api/payment/cryptopay", "application/json",
		strings.NewReader(`{"amount":1000,"invoiceId":"000111","currency":"KZT","cryptogram":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var er infraepay.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&er)
	if er.Code != 484 {
		t.Errorf("code = %d, want 484", er.Code)
	}
}

func TestPreset_CardExpired(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_card_expired")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000222","currency":"KZT","cryptogram":"x"}`)
	defer resp.Body.Close()
	var er infraepay.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&er)
	if resp.StatusCode != 400 || er.Code != 478 {
		t.Errorf("status=%d code=%d, want 400/478", resp.StatusCode, er.Code)
	}
}

func TestPreset_Unauthorized401(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_unauthorized_401")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000333","cryptogram":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestPreset_Forbidden403(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_forbidden_403")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000444","cryptogram":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestPreset_OAuthUnauthorized(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_oauth_unauthorized")

	resp := mustPost(t, st.Server.URL+"/oauth2/token", "application/x-www-form-urlencoded", "grant_type=client_credentials&client_id=bad")
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("oauth status = %d, want 401", resp.StatusCode)
	}
}

func TestPreset_TransientFailure(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	// Authorize первым (без сценария).
	id := authorize(t, st.Server.URL, "000555")

	// Поставим preset.
	st.Scenarios.ApplyPreset("epay_transient_500_then_ok")

	// Первый charge → 500.
	resp1 := mustPost(t, st.Server.URL+"/api/operation/"+id+"/charge", "application/json", `{"amount":5000}`)
	resp1.Body.Close()
	if resp1.StatusCode != 500 {
		t.Errorf("first charge status = %d, want 500", resp1.StatusCode)
	}

	// Второй charge — должен пройти. Authorize → Captured.
	resp2 := mustPost(t, st.Server.URL+"/api/operation/"+id+"/charge", "application/json", `{"amount":5000}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("second charge status = %d, want 200", resp2.StatusCode)
	}
}

func TestPreset_DoubleChargeRejected(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	id := authorize(t, st.Server.URL, "000666")
	st.Scenarios.ApplyPreset("epay_double_charge_rejected")

	resp := mustPost(t, st.Server.URL+"/api/operation/"+id+"/charge", "application/json", `{"amount":5000}`)
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	var er infraepay.ErrorResponse
	_ = json.NewDecoder(resp.Body).Decode(&er)
	if er.Code != 477 {
		t.Errorf("code = %d, want 477", er.Code)
	}
}

func TestPreset_AmbiguousChargeRecovery(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	id := authorize(t, st.Server.URL, "000777")
	st.Scenarios.ApplyPreset("epay_ambiguous_charge_recovery")

	// 1) charge → 400 (ambiguous).
	chResp := mustPost(t, st.Server.URL+"/api/operation/"+id+"/charge", "application/json", `{"amount":5000}`)
	chResp.Body.Close()
	if chResp.StatusCode != 400 {
		t.Errorf("charge status = %d, want 400", chResp.StatusCode)
	}

	// 2) Status-check показывает что операция в AUTH (списания не было).
	statusResp, err := http.Get(st.Server.URL + "/check-status/payment/transactionId/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	if statusResp.StatusCode != 200 {
		t.Fatalf("status check = %d, want 200", statusResp.StatusCode)
	}
	var sr infraepay.StatusResponse
	_ = json.NewDecoder(statusResp.Body).Decode(&sr)
	if sr.Status != "AUTH" {
		t.Errorf("status field = %s, want AUTH (charge не выполнился)", sr.Status)
	}
}

func TestPreset_3DSRequired(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_3ds_required")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000888","cryptogram":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ar infraepay.AuthorizeResponse
	_ = json.NewDecoder(resp.Body).Decode(&ar)
	if ar.Secure3D == nil {
		t.Fatal("secure3D block should be present")
	}
	if ar.Secure3D.Action == "" {
		t.Error("secure3D.action should be set")
	}
}

func TestPreset_3DSMissingActionURL(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_3ds_missing_action_url")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000889","cryptogram":"x"}`)
	defer resp.Body.Close()
	var ar infraepay.AuthorizeResponse
	_ = json.NewDecoder(resp.Body).Decode(&ar)
	if ar.Secure3D == nil {
		t.Fatal("secure3D should be present")
	}
	if ar.Secure3D.Action != "" {
		t.Errorf("secure3D.action = %q, want empty (edge case)", ar.Secure3D.Action)
	}
}

func TestPreset_WrongInvoiceID(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_wrong_invoice_id")

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json", `{"amount":1000,"invoiceId":"000990","cryptogram":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var ar infraepay.AuthorizeResponse
	_ = json.NewDecoder(resp.Body).Decode(&ar)
	if ar.InvoiceID != "" {
		t.Errorf("invoiceId = %q, want empty (missing_field action)", ar.InvoiceID)
	}
}

func TestStatusCheck_OnFreshAuthorize(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	id := authorize(t, st.Server.URL, "111111")

	resp, err := http.Get(st.Server.URL + "/check-status/payment/transactionId/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var sr infraepay.StatusResponse
	_ = json.NewDecoder(resp.Body).Decode(&sr)
	if sr.Status != "AUTH" {
		t.Errorf("status = %s, want AUTH", sr.Status)
	}
	if sr.InvoiceID != "111111" {
		t.Errorf("invoiceId = %s, want 111111", sr.InvoiceID)
	}
}

func TestStatusCheck_AfterCharge_ReturnsCharge(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	id := authorize(t, st.Server.URL, "222222")

	chResp := mustPost(t, st.Server.URL+"/api/operation/"+id+"/charge", "application/json", `{"amount":5000}`)
	chResp.Body.Close()

	resp := mustGet(t, st.Server.URL+"/check-status/payment/transactionId/"+id)
	defer resp.Body.Close()
	var sr infraepay.StatusResponse
	_ = json.NewDecoder(resp.Body).Decode(&sr)
	if sr.Status != "CHARGE" {
		t.Errorf("status = %s, want CHARGE", sr.Status)
	}
}

func TestStatusCheck_NotFound(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	resp := mustGet(t, st.Server.URL+"/check-status/payment/transactionId/non-existent-uuid")
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400 (operation not found)", resp.StatusCode)
	}
}

func TestPreset_OAuthTimeout_AffectsOnlyTokenEndpoint(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_oauth_timeout")

	// Cryptopay должен пройти — preset стоит только на token endpoint.
	resp, err := http.Post(st.Server.URL+"/api/payment/cryptopay", "application/json",
		strings.NewReader(`{"amount":1000,"invoiceId":"333333","cryptogram":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("cryptopay status = %d, want 200 (preset не должен затрагивать)", resp.StatusCode)
	}
}
