package loyalty

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGetToken(t *testing.T) {
	t.Parallel()

	ctrl := NewController(0, 10, 10000)
	mux := http.NewServeMux()
	ctrl.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/authservice/api/auth/v1/security/getToken", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["access_token"] != mockAccessToken {
		t.Errorf("access_token = %v, want %q", resp["access_token"], mockAccessToken)
	}
	if resp["token_type"] != mockTokenType {
		t.Errorf("token_type = %v, want %q", resp["token_type"], mockTokenType)
	}
}

func TestHandleCompanyTransaction_ComputesCashbackByPercent(t *testing.T) {
	t.Parallel()

	ctrl := NewController(0, 12.5, 9999)
	mux := http.NewServeMux()
	ctrl.Register(mux)

	body, _ := json.Marshal(companyTransactionRequest{
		Phone:         "77001234567",
		Amount:        50000,
		CompanyName:   "CHOCO",
		IsTransaction: 0,
	})

	req := httptest.NewRequest(http.MethodPost, "/loyaltyservice/loyalty/frhcCompanyTransaction", bytes.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", w.Code)
	}

	var resp companyTransactionResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Phone != "77001234567" {
		t.Errorf("phone = %q, want %q", resp.Phone, "77001234567")
	}
	if resp.CashbackPercent != 12.5 {
		t.Errorf("cashbackPercent = %v, want 12.5", resp.CashbackPercent)
	}
	// 50000 * 12.5 / 100 = 6250
	if resp.CashbackAmount != 6250 {
		t.Errorf("cashbackAmount = %v, want 6250", resp.CashbackAmount)
	}
	if resp.CashbackBalance != 9999 {
		t.Errorf("cashbackBalance = %v, want 9999", resp.CashbackBalance)
	}
	if resp.Comment != mockComment {
		t.Errorf("comment = %q, want %q", resp.Comment, mockComment)
	}
}

func TestHandleCompanyTransaction_ZeroAmount(t *testing.T) {
	t.Parallel()

	ctrl := NewController(0, 10, 10000)
	mux := http.NewServeMux()
	ctrl.Register(mux)

	body, _ := json.Marshal(companyTransactionRequest{
		Phone:  "77001234567",
		Amount: 0,
	})
	req := httptest.NewRequest(http.MethodPost, "/loyaltyservice/loyalty/frhcCompanyTransaction", bytes.NewReader(body))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	var resp companyTransactionResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.CashbackAmount != 0 {
		t.Errorf("cashbackAmount for zero amount = %v, want 0", resp.CashbackAmount)
	}
	if resp.CashbackBalance != 10000 {
		t.Errorf("cashbackBalance = %v, want 10000 (balance не зависит от amount)", resp.CashbackBalance)
	}
}

func TestHandleCompanyTransaction_InvalidBody(t *testing.T) {
	t.Parallel()

	ctrl := NewController(0, 10, 10000)
	mux := http.NewServeMux()
	ctrl.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/loyaltyservice/loyalty/frhcCompanyTransaction", bytes.NewReader([]byte("{not-json")))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", w.Code)
	}
}
