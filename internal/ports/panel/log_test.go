package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

func newLogController() *Controller {
	log := memstore.NewRequestLog(10)
	log.Add(&requestlog.Entry{Bank: bank.Freedom, Endpoint: "init", PaymentID: "1", OrderID: "100", MerchantID: "555312", SignatureOK: true})
	log.Add(&requestlog.Entry{Bank: bank.Freedom, Endpoint: "revoke", PaymentID: "1", OrderID: "100", MerchantID: "555312", SignatureOK: true})
	log.Add(&requestlog.Entry{Bank: bank.Freedom, Endpoint: "init", PaymentID: "2", OrderID: "200", MerchantID: "587055", SignatureOK: true})
	log.Add(&requestlog.Entry{Bank: bank.Epay, Endpoint: "cryptopay", OrderID: "100"})

	return &Controller{log: log}
}

func TestHandleLogJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		query         string
		wantMerchants []string
	}{
		{name: "без фильтров", query: "", wantMerchants: []string{"", "587055", "555312", "555312"}},
		{name: "банк", query: "?bank=freedom", wantMerchants: []string{"587055", "555312", "555312"}},
		{name: "платёж и операция", query: "?bank=freedom&payment_id=1&endpoint=revoke", wantMerchants: []string{"555312"}},
		{name: "заказ", query: "?bank=freedom&order_id=200", wantMerchants: []string{"587055"}},
		{name: "ничего не найдено", query: "?payment_id=404", wantMerchants: []string{}},
		{name: "пустой фильтр игнорируется", query: "?bank=freedom&order_id=", wantMerchants: []string{"587055", "555312", "555312"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			newLogController().handleLogJSON(rec, httptest.NewRequest(http.MethodGet, "/panel/log.json"+tt.query, nil))

			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}

			var got []logEntryJSON
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(got) != len(tt.wantMerchants) {
				t.Fatalf("entries = %d, want %d: %+v", len(got), len(tt.wantMerchants), got)
			}
			for i, e := range got {
				if e.MerchantID != tt.wantMerchants[i] {
					t.Fatalf("entry %d merchant = %q, want %q", i, e.MerchantID, tt.wantMerchants[i])
				}
			}
		})
	}
}
