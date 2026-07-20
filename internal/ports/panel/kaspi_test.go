package panel

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	appkaspi "github.com/vevovip/chaospay/internal/application/kaspi"
	domainkaspi "github.com/vevovip/chaospay/internal/domain/kaspi"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

func newKaspiController() (*Controller, *appkaspi.Service) {
	svc := appkaspi.NewService(memstore.NewKaspiRepo(), appkaspi.BehaviorOptions{
		StatusPollingInterval: 1, LinkActivationWaitTimeout: 60, PaymentConfirmationTimeout: 120,
	})

	return &Controller{kaspi: svc}, svc
}

func TestKaspiBadge(t *testing.T) {
	t.Parallel()

	cases := map[domainkaspi.Status]string{
		domainkaspi.StatusWait:      "NEW",
		domainkaspi.StatusProcessed: "AUTHORIZED",
		domainkaspi.StatusError:     "FAILED",
	}
	for status, want := range cases {
		if got := kaspiBadge(status); got != want {
			t.Fatalf("kaspiBadge(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestRenderKaspiTab_Empty(t *testing.T) {
	t.Parallel()

	c, _ := newKaspiController()
	rec := httptest.NewRecorder()
	c.renderKaspiTab(rec)

	if !strings.Contains(rec.Body.String(), "Total: 0") {
		t.Fatalf("empty tab missing 'Total: 0': %s", rec.Body.String())
	}
}

func TestRenderKaspiTab_WithPayment(t *testing.T) {
	t.Parallel()

	c, svc := newKaspiController()
	p := svc.CreateLink("order-42", 2590)

	rec := httptest.NewRecorder()
	c.renderKaspiTab(rec)
	body := rec.Body.String()

	for _, want := range []string{"order-42", strconv.Itoa(p.PaymentID), "Confirm", "Decline", "badge-NEW"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered tab missing %q", want)
		}
	}
}

func TestRenderKaspiTab_TerminalNoActions(t *testing.T) {
	t.Parallel()

	c, svc := newKaspiController()
	p := svc.CreateLink("order-done", 100)
	if _, err := svc.SetStatus(p.PaymentID, domainkaspi.StatusProcessed); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	rec := httptest.NewRecorder()
	c.renderKaspiTab(rec)
	body := rec.Body.String()

	// У терминального платежа не должно быть форм-действий (слово "Confirm"
	// присутствует в тексте-подсказке, поэтому проверяем именно action-форму).
	if strings.Contains(body, `action="/panel/kaspi/action"`) {
		t.Fatal("terminal payment must not show action buttons")
	}
	if !strings.Contains(body, "badge-AUTHORIZED") {
		t.Fatal("processed payment must show AUTHORIZED badge")
	}
}

func TestHandleKaspiAction_Confirm(t *testing.T) {
	t.Parallel()

	c, svc := newKaspiController()
	p := svc.CreateLink("order-7", 300)

	form := url.Values{
		"payment_id": {strconv.Itoa(p.PaymentID)},
		"action":     {string(domainkaspi.StatusProcessed)},
	}
	req := httptest.NewRequest(http.MethodPost, "/panel/kaspi/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	c.handleKaspiAction(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	got, _ := svc.GetStatus(p.PaymentID)
	if got.Status != domainkaspi.StatusProcessed {
		t.Fatalf("status after confirm = %q, want Processed", got.Status)
	}
}

func TestHandleKaspiAction_BadRequest(t *testing.T) {
	t.Parallel()

	c, _ := newKaspiController()

	form := url.Values{"action": {"Processed"}} // no payment_id
	req := httptest.NewRequest(http.MethodPost, "/panel/kaspi/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	c.handleKaspiAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
