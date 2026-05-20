package pay

import (
	"sync"
	"testing"
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// --- Stubs ---

type stubFreedomWebhook struct {
	mu    sync.Mutex
	calls int
}

func (s *stubFreedomWebhook) Send(_ *pay.Record, _ bool, _ bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return 200, nil
}

func (s *stubFreedomWebhook) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type stubEpayWebhook struct {
	mu        sync.Mutex
	successes int
	failures  int
}

func (s *stubEpayWebhook) SendSuccess(_ *pay.Record) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.successes++
	return 200, nil
}

func (s *stubEpayWebhook) SendFailure(_ *pay.Record, _ int, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	return 200, nil
}

func (s *stubEpayWebhook) SendBind(_ *pay.Record, _ bool, _ int, _ string) (int, error) {
	return 200, nil
}

func (s *stubEpayWebhook) successCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successes
}

func (s *stubEpayWebhook) failureCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failures
}

// --- Helpers ---

func createRecord(repo Repository, b bank.Bank, status pay.Status) *pay.Record {
	pid := repo.NextPaymentID()
	rec := &pay.Record{
		Bank:       b,
		PaymentID:  pid,
		OrderID:    pid,
		Amount:     1000,
		Currency:   "KZT",
		Kind:       pay.KindCard,
		Status:     status,
		MerchantID: 100001,
		TerminalID: 1,
		UserID:     1,
	}
	repo.Create(rec)
	return rec
}

func waitWebhook(check func() bool) bool {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return check()
}

// --- Tests ---

func TestApplyForce_RoutesToFreedomWebhook(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	fw := &stubFreedomWebhook{}
	ew := &stubEpayWebhook{}
	flw := &stubFlittWebhook{}
	svc := NewService(repo, fw, nil, ew, flw, AutoWebhookConfig{Freedom: true, Epay: true, Flitt: true})

	rec := createRecord(repo, bank.Freedom, pay.StatusAuthorized)
	_, err := svc.ApplyForce(rec.PaymentID, pay.StatusCaptured)
	if err != nil {
		t.Fatalf("ApplyForce: %v", err)
	}
	if !waitWebhook(func() bool { return fw.count() == 1 }) {
		t.Fatalf("freedom webhook not called: %d", fw.count())
	}
	if ew.successCount() != 0 {
		t.Fatalf("epay webhook leaked for Freedom record: %d", ew.successCount())
	}
	if flw.callbackCount() != 0 {
		t.Fatalf("flitt webhook leaked for Freedom record: %d", flw.callbackCount())
	}
}

func TestApplyForce_RoutesToEpayWebhook(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	fw := &stubFreedomWebhook{}
	ew := &stubEpayWebhook{}
	flw := &stubFlittWebhook{}
	svc := NewService(repo, fw, nil, ew, flw, AutoWebhookConfig{Freedom: true, Epay: true, Flitt: true})

	rec := createRecord(repo, bank.Epay, pay.StatusAuthorized)
	if _, err := svc.ApplyForce(rec.PaymentID, pay.StatusCaptured); err != nil {
		t.Fatalf("ApplyForce: %v", err)
	}
	if !waitWebhook(func() bool { return ew.successCount() == 1 }) {
		t.Fatalf("epay webhook not called: %d", ew.successCount())
	}
	if fw.count() != 0 {
		t.Fatalf("freedom webhook leaked for Epay: %d", fw.count())
	}
	if flw.callbackCount() != 0 {
		t.Fatalf("flitt webhook leaked for Epay: %d", flw.callbackCount())
	}
}

func TestApplyForce_RoutesToFlittWebhook(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	fw := &stubFreedomWebhook{}
	ew := &stubEpayWebhook{}
	flw := &stubFlittWebhook{}
	svc := NewService(repo, fw, nil, ew, flw, AutoWebhookConfig{Freedom: true, Epay: true, Flitt: true})

	rec := createRecord(repo, bank.Flitt, pay.StatusAuthorized)
	if _, err := svc.ApplyForce(rec.PaymentID, pay.StatusCaptured); err != nil {
		t.Fatalf("ApplyForce: %v", err)
	}
	if !waitWebhook(func() bool { return flw.callbackCount() == 1 }) {
		t.Fatalf("flitt webhook not called: %d", flw.callbackCount())
	}
	if !flw.wasSuccess() {
		t.Fatalf("flitt webhook should be success=true for Captured")
	}
	if fw.count() != 0 {
		t.Fatalf("freedom webhook leaked for Flitt: %d", fw.count())
	}
	if ew.successCount() != 0 {
		t.Fatalf("epay webhook leaked for Flitt: %d", ew.successCount())
	}
}

func TestApplyForce_FailedSendsFailureForEpay(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	ew := &stubEpayWebhook{}
	svc := NewService(repo, nil, nil, ew, nil, AutoWebhookConfig{Epay: true})

	rec := createRecord(repo, bank.Epay, pay.StatusAuthorized)
	if _, err := svc.ApplyForce(rec.PaymentID, pay.StatusFailed); err != nil {
		t.Fatalf("ApplyForce: %v", err)
	}
	if !waitWebhook(func() bool { return ew.failureCount() == 1 }) {
		t.Fatalf("epay failure webhook not called: %d", ew.failureCount())
	}
	if ew.successCount() != 0 {
		t.Fatalf("epay success webhook leaked: %d", ew.successCount())
	}
}

func TestApplyForce_NoAutoWebhookWhenDisabled(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	fw := &stubFreedomWebhook{}
	ew := &stubEpayWebhook{}
	flw := &stubFlittWebhook{}
	svc := NewService(repo, fw, nil, ew, flw, AutoWebhookConfig{}) // все флаги false

	for _, b := range []bank.Bank{bank.Freedom, bank.Epay, bank.Flitt} {
		rec := createRecord(repo, b, pay.StatusAuthorized)
		if _, err := svc.ApplyForce(rec.PaymentID, pay.StatusCaptured); err != nil {
			t.Fatalf("ApplyForce(%s): %v", b, err)
		}
	}
	// 50ms buffer чтобы убедиться что goroutines не успели/не должны были вызваться.
	time.Sleep(50 * time.Millisecond)
	if fw.count() != 0 || ew.successCount() != 0 || flw.callbackCount() != 0 {
		t.Fatalf("webhooks fired when autoWebhook=false: freedom=%d epay=%d flitt=%d",
			fw.count(), ew.successCount(), flw.callbackCount())
	}
}

func TestApplyForce_NilWebhookDoesntPanic(t *testing.T) {
	t.Parallel()
	repo := memstore.NewPayRepo()
	// Все webhooks nil, флаги включены — не должно паниковать.
	svc := NewService(repo, nil, nil, nil, nil, AutoWebhookConfig{Freedom: true, Epay: true, Flitt: true})

	for _, b := range []bank.Bank{bank.Freedom, bank.Epay, bank.Flitt} {
		rec := createRecord(repo, b, pay.StatusAuthorized)
		if _, err := svc.ApplyForce(rec.PaymentID, pay.StatusCaptured); err != nil {
			t.Fatalf("ApplyForce(%s): %v", b, err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	// если до сюда дошли — паники не было.
}
