package pay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

const (
	oldCabinet   = 554415
	newCabinet   = 587055
	oldSecret    = "secret-old"
	newSecret    = "secret-new"
	holdInitPath = "/v1/merchant/%d/card/init"
)

func newTestController() (*Controller, *apppay.Service) {
	repo := memstore.NewPayRepo()
	svc := apppay.NewService(repo, nil, nil, nil, nil, apppay.AutoWebhookConfig{})
	scenarios := appscenario.NewService(memstore.NewScenarioStore())

	ctrl := NewController(svc, scenarios, memstore.NewRequestLog(0), Config{
		Secret:            oldSecret,
		Secrets:           map[uint]string{oldCabinet: oldSecret, newCabinet: newSecret},
		DefaultTerminalID: 1,
	})

	return ctrl, svc
}

// holdInitRequest собирает подписанный init по сохраненной карте.
func holdInitRequest(merchantID, cabinetID int, secret string) *http.Request {
	fields := freedompay.OrdMap{}
	fields = fields.Set("pg_order_id", "1001")
	fields = fields.Set("pg_merchant_id", fmt.Sprintf("%d", merchantID))
	fields = fields.Set("pg_amount", "500")
	fields = fields.Set("pg_user_id", "77")
	fields = fields.Set("pg_card_token", "f0000000-0000-4000-8000-000000000001")
	fields = fields.Set("pg_salt", "abcdefgh")
	fields = fields.Set("merchant_params", freedompay.OrdMap{}.
		Set("terminal_id", "13").
		Set("cabinet_id", fmt.Sprintf("%d", cabinetID)))
	fields = fields.Set("pg_sig", freedompay.Sign("init", fields, secret))

	form := url.Values{}
	form.Set("pg_xml", freedompay.RenderResponse("request", fields))

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf(holdInitPath, merchantID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return req
}

func TestHoldInitAcceptsSecretOfRequestedCabinet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		merchantID int
		secret     string
	}{
		{name: "старый кабинет своим ключом", merchantID: oldCabinet, secret: oldSecret},
		{name: "новый кабинет своим ключом", merchantID: newCabinet, secret: newSecret},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl, _ := newTestController()
			mux := http.NewServeMux()
			ctrl.Register(mux)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, holdInitRequest(tt.merchantID, tt.merchantID, tt.secret))

			if !strings.Contains(rec.Body.String(), "<pg_status>ok</pg_status>") {
				t.Fatalf("ожидался ok, получено: %s", rec.Body.String())
			}
		})
	}
}

// Ключ соседнего кабинета не должен подходить — иначе мок не отличит платеж,
// ушедший не в тот кабинет, от корректного.
func TestHoldInitRejectsSecretOfAnotherCabinet(t *testing.T) {
	t.Parallel()

	ctrl, _ := newTestController()
	mux := http.NewServeMux()
	ctrl.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, holdInitRequest(newCabinet, newCabinet, oldSecret))

	body := rec.Body.String()
	if !strings.Contains(body, "invalid signature") {
		t.Fatalf("ожидалась ошибка подписи, получено: %s", body)
	}
}

// Ответ подписывается тем же ключом, что и запрос: иначе вызывающая сторона
// не проверит подпись ответа кабинета, в который сама же и сходила.
func TestHoldInitResponseSignedWithCabinetSecret(t *testing.T) {
	t.Parallel()

	ctrl, _ := newTestController()
	mux := http.NewServeMux()
	ctrl.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, holdInitRequest(newCabinet, newCabinet, newSecret))

	parsed, err := freedompay.ParseRequestXML(rec.Body.String())
	if err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}

	sig := parsed.Get("pg_sig", "")
	if _, ok := freedompay.Verify("init", parsed.Fields, newSecret, sig); !ok {
		t.Error("подпись ответа не сходится с ключом нового кабинета")
	}
	if _, ok := freedompay.Verify("init", parsed.Fields, oldSecret, sig); ok {
		t.Error("подпись ответа сошлась с ключом старого кабинета")
	}
}

func TestHoldInitStoresCabinetID(t *testing.T) {
	t.Parallel()

	ctrl, svc := newTestController()
	mux := http.NewServeMux()
	ctrl.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, holdInitRequest(newCabinet, newCabinet, newSecret))

	records := svc.Repo().List()
	if len(records) != 1 {
		t.Fatalf("ожидалась одна запись платежа, получено %d", len(records))
	}
	if records[0].CabinetID != newCabinet {
		t.Errorf("CabinetID = %d, ожидался %d", records[0].CabinetID, newCabinet)
	}
	if records[0].TerminalID != 13 {
		t.Errorf("TerminalID = %d, ожидался 13", records[0].TerminalID)
	}
}
