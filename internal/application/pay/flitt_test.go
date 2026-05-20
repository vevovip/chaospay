package pay

import (
	"errors"
	"sync"
	"testing"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// stubFlittWebhook — счётчик вызовов SendCallback / SendBindCallback.
type stubFlittWebhook struct {
	mu       sync.Mutex
	callback int
	bind     int
	success  bool
	failOn   string // "callback" / "bind" — вернуть ошибку
}

func (s *stubFlittWebhook) SendCallback(_ *pay.Record, success bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callback++
	s.success = success
	if s.failOn == "callback" {
		return 0, errors.New("forced callback failure")
	}
	return 200, nil
}

func (s *stubFlittWebhook) SendBindCallback(_ *pay.Record, success bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bind++
	s.success = success
	if s.failOn == "bind" {
		return 0, errors.New("forced bind failure")
	}
	return 200, nil
}

func (s *stubFlittWebhook) callbackCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callback
}

func (s *stubFlittWebhook) bindCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bind
}

func (s *stubFlittWebhook) wasSuccess() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.success
}

func newFlittService() (*Service, *stubFlittWebhook) {
	repo := memstore.NewPayRepo()
	wh := &stubFlittWebhook{}
	return NewService(repo, nil, nil, nil, wh, AutoWebhookConfig{}), wh
}

func TestFlittCheckout_CreatesRecord(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittCheckout(FlittCheckoutInput{
		OrderID:       100,
		MerchantID:    1549901,
		Amount:        5000,
		Currency:      "GEL",
		Description:   "test",
		Email:         "user@example.com",
		HostedFormURL: "http://localhost:48532/panel",
	})
	if err != nil {
		t.Fatalf("FlittCheckout: %v", err)
	}
	if rec.Bank != bank.Flitt {
		t.Fatalf("bank: got %s, want flitt", rec.Bank)
	}
	if rec.Kind != pay.KindFlittCheckout {
		t.Fatalf("kind: got %s, want %s", rec.Kind, pay.KindFlittCheckout)
	}
	if rec.FlittCheckoutURL == "" {
		t.Fatalf("checkout URL must be set")
	}
	if rec.Status != pay.StatusNew {
		t.Fatalf("status: got %s, want NEW", rec.Status)
	}
}

func TestFlittCheckout_ValidationErrors(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	if _, err := svc.FlittCheckout(FlittCheckoutInput{OrderID: 1, Amount: 0}); err == nil {
		t.Fatal("expected error for zero amount")
	}
	if _, err := svc.FlittCheckout(FlittCheckoutInput{OrderID: 0, Amount: 100}); err == nil {
		t.Fatal("expected error for zero order_id")
	}
}

func TestFlittCheckout_VerifySetsBindKind(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittCheckout(FlittCheckoutInput{
		OrderID: 1, MerchantID: 1, Amount: 10, IsVerify: true,
		HostedFormURL: "http://localhost",
	})
	if err != nil {
		t.Fatalf("FlittCheckout: %v", err)
	}
	if rec.Kind != pay.KindFlittBind {
		t.Fatalf("kind: got %s, want %s", rec.Kind, pay.KindFlittBind)
	}
	if !rec.FlittIsVerify {
		t.Fatalf("FlittIsVerify must be true")
	}
}

func TestFlittDirect_Approved(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 100, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	if err != nil {
		t.Fatalf("FlittDirect: %v", err)
	}
	if rec.Status != pay.StatusAuthorized {
		t.Fatalf("status: got %s, want AUTHORIZED", rec.Status)
	}
}

func TestFlittDirect_Declined(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 100, Currency: "GEL",
		Outcome: infraflitt.OutcomeDeclined,
	})
	if !errors.Is(err, ErrFlittDeclined) {
		t.Fatalf("expected ErrFlittDeclined, got %v", err)
	}
	if rec.Status != pay.StatusFailed {
		t.Fatalf("status: got %s, want FAILED", rec.Status)
	}
}

func TestFlittDirect_3DSKeeps3DSFields(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 100, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved3DS,
	})
	if err != nil {
		t.Fatalf("FlittDirect: %v", err)
	}
	if rec.Status != pay.StatusNew {
		t.Fatalf("status: got %s, want NEW (waiting step2)", rec.Status)
	}
	if rec.FlittACSURL == "" || rec.FlittPareq == "" || rec.FlittMD == "" {
		t.Fatalf("3DS fields must be set: acs=%q pareq=%q md=%q",
			rec.FlittACSURL, rec.FlittPareq, rec.FlittMD)
	}
}

func TestFlittRecurring_RequiresToken(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	if _, err := svc.FlittRecurring(FlittRecurringInput{OrderID: 1, Amount: 10}); err == nil {
		t.Fatal("expected error for missing rectoken")
	}
}

func TestFlittRecurring_Approved(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittRecurring(FlittRecurringInput{
		OrderID: 1, Amount: 100, Currency: "GEL",
		Rectoken: "tok-1",
		Outcome:  infraflitt.OutcomeApproved,
	})
	if err != nil {
		t.Fatalf("FlittRecurring: %v", err)
	}
	if rec.Status != pay.StatusAuthorized {
		t.Fatalf("status: got %s", rec.Status)
	}
	if rec.FlittRectoken != "tok-1" {
		t.Fatalf("rectoken not stored")
	}
}

func TestFlittCapture_AuthorizedToCaptured(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 5000, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	cap, err := svc.FlittCapture(rec.PaymentID, 5000)
	if err != nil {
		t.Fatalf("FlittCapture: %v", err)
	}
	if cap.Status != pay.StatusCaptured {
		t.Fatalf("status: got %s, want CAPTURED", cap.Status)
	}
	if cap.Captured != 5000 {
		t.Fatalf("captured: got %d, want 5000", cap.Captured)
	}
}

func TestFlittCapture_RejectsNonAuthorized(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittCheckout(FlittCheckoutInput{OrderID: 1, MerchantID: 1, Amount: 100, HostedFormURL: "x"})
	if _, err := svc.FlittCapture(rec.PaymentID, 0); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
}

func TestFlittReverse_Cancel(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 1000, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	rev, err := svc.FlittReverse(rec.PaymentID, 0)
	if err != nil {
		t.Fatalf("FlittReverse: %v", err)
	}
	if rev.Status != pay.StatusCancelled {
		t.Fatalf("status: got %s, want CANCELLED", rev.Status)
	}
}

func TestFlittReverse_FullRefund(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 1000, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	_, _ = svc.FlittCapture(rec.PaymentID, 0)
	ref, err := svc.FlittReverse(rec.PaymentID, 0)
	if err != nil {
		t.Fatalf("FlittReverse: %v", err)
	}
	if ref.Status != pay.StatusRefunded {
		t.Fatalf("status: got %s, want REFUNDED", ref.Status)
	}
}

func TestFlittReverse_PartialRefund(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 1000, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	_, _ = svc.FlittCapture(rec.PaymentID, 0)
	partial, err := svc.FlittReverse(rec.PaymentID, 300)
	if err != nil {
		t.Fatalf("FlittReverse: %v", err)
	}
	if partial.Status != pay.StatusPartialRefunded {
		t.Fatalf("status: got %s, want PARTIAL_REFUNDED", partial.Status)
	}
}

func TestFlittStatus_FindsByOrderID(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 100500, Amount: 1, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	found, err := svc.FlittStatus("100500")
	if err != nil {
		t.Fatalf("FlittStatus: %v", err)
	}
	if found.PaymentID != rec.PaymentID {
		t.Fatalf("payment_id: got %d, want %d", found.PaymentID, rec.PaymentID)
	}
}

func TestFlittStatus_NotFound(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	if _, err := svc.FlittStatus("999"); !errors.Is(err, ErrFlittNotFound) {
		t.Fatalf("expected ErrFlittNotFound, got %v", err)
	}
}

func TestFlittComplete3DS_TransitionsToAuthorized(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 1000, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved3DS,
	})
	updated, err := svc.FlittComplete3DS(rec.PaymentID)
	if err != nil {
		t.Fatalf("FlittComplete3DS: %v", err)
	}
	if updated.Status != pay.StatusAuthorized {
		t.Fatalf("status: got %s, want AUTHORIZED", updated.Status)
	}
}

func TestSendFlittCallback_CallsWebhook(t *testing.T) {
	t.Parallel()
	svc, wh := newFlittService()
	rec, _ := svc.FlittDirect(FlittDirectInput{
		OrderID: 1, Amount: 100, Currency: "GEL",
		Outcome: infraflitt.OutcomeApproved,
	})
	if err := svc.SendFlittCallback(rec.PaymentID, true); err != nil {
		t.Fatalf("SendFlittCallback: %v", err)
	}
	if wh.callback != 1 {
		t.Fatalf("expected 1 callback, got %d", wh.callback)
	}
	if !wh.success {
		t.Fatalf("expected success=true")
	}
}

func TestSendFlittBindCallback_CallsBindWebhook(t *testing.T) {
	t.Parallel()
	svc, wh := newFlittService()
	rec, _ := svc.FlittCheckout(FlittCheckoutInput{
		OrderID: 1, MerchantID: 1, Amount: 10, IsVerify: true,
		HostedFormURL: "x",
	})
	if err := svc.SendFlittBindCallback(rec.PaymentID, false); err != nil {
		t.Fatalf("SendFlittBindCallback: %v", err)
	}
	if wh.bind != 1 {
		t.Fatalf("expected 1 bind, got %d", wh.bind)
	}
	if wh.success {
		t.Fatalf("expected success=false")
	}
}

func TestFlitt_DefaultCurrencyIsGEL(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()

	checkout, err := svc.FlittCheckout(FlittCheckoutInput{
		OrderID:       1,
		MerchantID:    1,
		Amount:        10,
		HostedFormURL: "x",
		// Currency опущено → должна примениться GEL по умолчанию.
	})
	if err != nil {
		t.Fatalf("FlittCheckout: %v", err)
	}
	if checkout.Currency != "GEL" {
		t.Fatalf("checkout currency: got %q, want GEL", checkout.Currency)
	}

	direct, err := svc.FlittDirect(FlittDirectInput{
		OrderID: 2, Amount: 100,
		Outcome: infraflitt.OutcomeApproved,
	})
	if err != nil {
		t.Fatalf("FlittDirect: %v", err)
	}
	if direct.Currency != "GEL" {
		t.Fatalf("direct currency: got %q, want GEL", direct.Currency)
	}

	recurring, err := svc.FlittRecurring(FlittRecurringInput{
		OrderID: 3, Amount: 100, Rectoken: "t",
		Outcome: infraflitt.OutcomeApproved,
	})
	if err != nil {
		t.Fatalf("FlittRecurring: %v", err)
	}
	if recurring.Currency != "GEL" {
		t.Fatalf("recurring currency: got %q, want GEL", recurring.Currency)
	}
}

func TestFlitt_ExplicitCurrencyOverridesDefault(t *testing.T) {
	t.Parallel()
	svc, _ := newFlittService()
	rec, err := svc.FlittCheckout(FlittCheckoutInput{
		OrderID: 1, MerchantID: 1, Amount: 10, Currency: "USD",
		HostedFormURL: "x",
	})
	if err != nil {
		t.Fatalf("FlittCheckout: %v", err)
	}
	if rec.Currency != "USD" {
		t.Fatalf("currency override broke: got %q, want USD", rec.Currency)
	}
}

func TestSendFlittCallback_NilWebhook(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	svc := NewService(repo, nil, nil, nil, nil, AutoWebhookConfig{})
	if err := svc.SendFlittCallback(1, true); !errors.Is(err, ErrFlittWebhookNotConfigured) {
		t.Fatalf("expected ErrFlittWebhookNotConfigured, got %v", err)
	}
}
