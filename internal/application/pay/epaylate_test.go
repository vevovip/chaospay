package pay

import (
	"testing"
	"time"

	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// visibilityRepo после каждой записи проверяет, что check-status не увидел бы операцию
// раньше срока или в промежуточном статусе.
type visibilityRepo struct {
	*memstore.PayRepo
	t         *testing.T
	wantFinal pay.Status
	leaks     []string
}

func (r *visibilityRepo) check(paymentID uint, op string) {
	rec, err := r.PayRepo.Get(paymentID)
	if err != nil {
		return
	}
	if !rec.EpayHidden(time.Now()) && rec.Status != r.wantFinal {
		r.leaks = append(r.leaks, op+": visible as "+string(rec.Status))
	}
}

func (r *visibilityRepo) Create(rec *pay.Record) {
	r.PayRepo.Create(rec)
	r.check(rec.PaymentID, "create")
}

func (r *visibilityRepo) Update(paymentID uint, fn func(rec *pay.Record) (pay.Status, string, error)) (*pay.Record, error) {
	out, err := r.PayRepo.Update(paymentID, fn)
	r.check(paymentID, "update")

	return out, err
}

func (r *visibilityRepo) Transition(paymentID uint, allowedFrom map[pay.Status]bool, to pay.Status, reason string) (*pay.Record, error) {
	out, err := r.PayRepo.Transition(paymentID, allowedFrom, to, reason)
	r.check(paymentID, "transition")

	return out, err
}

func TestEpayLateOperation_NeverVisibleEarly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		captured  bool
		visibleAt time.Time
		wantFinal pay.Status
	}{
		{"холд скрыт с момента создания", false, time.Now().Add(10 * time.Minute), pay.StatusAuthorized},
		{"списание скрыто с момента создания", true, time.Now().Add(10 * time.Minute), pay.StatusCaptured},
		{"видимое сразу списание не видно холдом", true, time.Time{}, pay.StatusCaptured},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &visibilityRepo{PayRepo: memstore.NewPayRepo(), t: t, wantFinal: tt.wantFinal}
			svc := NewService(repo, nil, nil, nil, nil, AutoWebhookConfig{})

			rec, err := svc.EpayLateOperation(EpayAuthorizeInput{Amount: 5000, InvoiceID: "000123", HasCryptogram: true, PaymentType: "googlePay"}, tt.captured, tt.visibleAt)
			if err != nil {
				t.Fatalf("EpayLateOperation: %v", err)
			}
			if len(repo.leaks) > 0 {
				t.Fatalf("operation leaked into check-status: %v", repo.leaks)
			}
			if rec.Status != tt.wantFinal || !rec.EpayVisibleAt.Equal(tt.visibleAt) {
				t.Fatalf("record = %s visibleAt=%v, want %s visibleAt=%v", rec.Status, rec.EpayVisibleAt, tt.wantFinal, tt.visibleAt)
			}
			if tt.captured && rec.Captured != rec.Amount {
				t.Fatalf("captured = %d, want %d", rec.Captured, rec.Amount)
			}
		})
	}
}
