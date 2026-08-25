package epay_test

import (
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"testing"

	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
)

// TestACS_ReturnsFormToTermURL — страница проверки возвращает браузер обратно на TermUrl.
func TestACS_ReturnsFormToTermURL(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	form := url.Values{
		"PaReq":   {"mock-pareq"},
		"MD":      {"mock-epay-1"},
		"TermUrl": {"http://pg/api/v1/payment-gateway/epay/3ds/callback?order_id=1"},
	}

	resp := mustPost(t, st.Server.URL+"/epay/3ds/acs", "application/x-www-form-urlencoded", form.Encode())
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if !strings.Contains(page, form.Get("TermUrl")) {
		t.Error("страница должна сабмитить форму на TermUrl")
	}

	if !strings.Contains(page, `name="PaRes"`) {
		t.Error("страница должна возвращать PaRes")
	}

	if !strings.Contains(page, form.Get("MD")) {
		t.Error("страница должна возвращать MD без изменений")
	}
}

// TestACS_RequiresTermURL — без TermUrl возвращать пользователя некуда.
func TestACS_RequiresTermURL(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	resp := mustPost(t, st.Server.URL+"/epay/3ds/acs", "application/x-www-form-urlencoded", "PaReq=x&MD=y")
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestConfirm_AuthorizesOperationAfter3DS — до подтверждения операция не авторизована,
// иначе confirm упирается в недопустимый переход статуса.
func TestConfirm_AuthorizesOperationAfter3DS(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_3ds_required")

	epayID := cryptopayWith3DS(t, st, "000901")

	status := statusOf(t, st, epayID)
	if status != "NEW" {
		t.Fatalf("status до подтверждения = %s, want NEW", status)
	}

	resp := mustPost(t, st.Server.URL+"/api/payment/confirm", "application/json",
		`{"ID":"`+epayID+`","PaRes":"mock-pares-approved","MD":"`+epayID+`"}`)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("confirm status = %d, want 200", resp.StatusCode)
	}

	if status := statusOf(t, st, epayID); status != "AUTH" {
		t.Errorf("status после подтверждения = %s, want AUTH", status)
	}
}

// TestConfirm_DeclinedMovesOperationToFailed — исход отказа PG узнает из состояния операции,
// поэтому она обязана перейти в FAILED, а не остаться новой.
func TestConfirm_DeclinedMovesOperationToFailed(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()
	st.Scenarios.ApplyPreset("epay_3ds_confirm_declined")

	epayID := cryptopayWith3DS(t, st, "000902")

	resp := mustPost(t, st.Server.URL+"/api/payment/confirm", "application/json",
		`{"ID":"`+epayID+`","PaRes":"mock-pares-declined","MD":"`+epayID+`"}`)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Errorf("confirm status = %d, want 400", resp.StatusCode)
	}

	if status := statusOf(t, st, epayID); status != "FAILED" {
		t.Errorf("status после отказа = %s, want FAILED", status)
	}
}

// TestStatus_APIPrefixAlias — PG собирает путь из BaseURI, который оканчивается на /api.
func TestStatus_APIPrefixAlias(t *testing.T) {
	st := newTestStand(t)
	defer st.Server.Close()

	epayID := cryptopay(t, st, "000903")

	resp := mustGet(t, st.Server.URL+"/api/check-status/payment/transactionId/"+epayID)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// cryptopayWith3DS проводит cryptopay при активном 3DS-сценарии и возвращает идентификатор операции
func cryptopayWith3DS(t *testing.T, st *testStand, invoiceID string) string {
	t.Helper()

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json",
		`{"amount":1000,"invoiceId":"`+invoiceID+`","cryptogram":"x"}`)
	defer resp.Body.Close()

	var ar infraepay.AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		t.Fatal(err)
	}

	if ar.Secure3D == nil {
		t.Fatal("ожидался блок secure3D")
	}

	return ar.ID
}

// cryptopay проводит обычный cryptopay и возвращает идентификатор операции
func cryptopay(t *testing.T, st *testStand, invoiceID string) string {
	t.Helper()

	resp := mustPost(t, st.Server.URL+"/api/payment/cryptopay", "application/json",
		`{"amount":1000,"invoiceId":"`+invoiceID+`","cryptogram":"x"}`)
	defer resp.Body.Close()

	var ar infraepay.AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		t.Fatal(err)
	}

	return ar.ID
}

// statusOf возвращает состояние операции у мока
func statusOf(t *testing.T, st *testStand, epayID string) string {
	t.Helper()

	resp := mustGet(t, st.Server.URL+"/check-status/payment/transactionId/"+epayID)
	defer resp.Body.Close()

	var sr infraepay.StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatal(err)
	}

	return sr.Status
}
