package pgclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
)

const (
	payOldCabinet = 554415
	payNewCabinet = 587055
	payOldSecret  = "secret-old"
	payNewSecret  = "secret-new"
)

func testSecrets() SecretResolver {
	secrets := map[uint]string{payOldCabinet: payOldSecret, payNewCabinet: payNewSecret}

	return func(merchantID uint) string { return secrets[merchantID] }
}

// verifyForm повторяет проверку подписи постлинка на принимающей стороне.
func verifyForm(form url.Values, secret string) bool {
	fields := freedompay.OrdMap{}
	for k, vs := range form {
		if k == "pg_sig" || len(vs) == 0 {
			continue
		}
		fields = fields.Set(k, vs[0])
	}

	return freedompay.Sign("freedompay", fields, secret) == form.Get("pg_sig")
}

func capturePayWebhook(t *testing.T, rec *pay.Record) url.Values {
	t.Helper()

	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		parsed, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("не удалось разобрать тело постлинка: %v", err)
		}
		got = parsed
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := NewPayClient(srv.URL, testSecrets()).Send(rec, true, true); err != nil {
		t.Fatalf("отправка постлинка: %v", err)
	}

	return got
}

func TestPayWebhookSignedWithCabinetSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		merchantID uint
		secret     string
		otherKey   string
	}{
		{name: "старый кабинет", merchantID: payOldCabinet, secret: payOldSecret, otherKey: payNewSecret},
		{name: "новый кабинет", merchantID: payNewCabinet, secret: payNewSecret, otherKey: payOldSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			form := capturePayWebhook(t, &pay.Record{
				PaymentID:  1,
				OrderID:    1001,
				MerchantID: tt.merchantID,
				CabinetID:  int(tt.merchantID),
				TerminalID: 13,
				Amount:     500,
			})

			if !verifyForm(form, tt.secret) {
				t.Error("подпись постлинка не сходится с ключом кабинета платежа")
			}
			if verifyForm(form, tt.otherKey) {
				t.Error("подпись постлинка сошлась с ключом соседнего кабинета")
			}
		})
	}
}

// cabinet_id нужен принимающей стороне, чтобы выбрать ключ кабинета до того, как
// она нашла платеж: подпись проверяется раньше, чем читается транзакция.
func TestPayWebhookCarriesCabinetID(t *testing.T) {
	t.Parallel()

	form := capturePayWebhook(t, &pay.Record{
		PaymentID:  1,
		OrderID:    1001,
		MerchantID: payNewCabinet,
		CabinetID:  payNewCabinet,
		TerminalID: 13,
		Amount:     500,
	})

	if got := form.Get("cabinet_id"); got != strconv.Itoa(payNewCabinet) {
		t.Errorf("cabinet_id = %q, ожидался %d", got, payNewCabinet)
	}
	if got := form.Get("terminal_id"); got != "13" {
		t.Errorf("terminal_id = %q, ожидался 13", got)
	}
}

// Платеж, начатый до появления кабинетов, не должен получать cabinet_id — иначе
// принимающая сторона выберет несуществующий кабинет вместо fallback-а по терминалу.
func TestPayWebhookOmitsEmptyCabinetID(t *testing.T) {
	t.Parallel()

	form := capturePayWebhook(t, &pay.Record{
		PaymentID:  2,
		OrderID:    1002,
		MerchantID: payOldCabinet,
		TerminalID: 13,
		Amount:     500,
	})

	if _, ok := form["cabinet_id"]; ok {
		t.Errorf("cabinet_id присутствует, ожидалось отсутствие: %q", form.Get("cabinet_id"))
	}
}
