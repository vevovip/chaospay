package kaspi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	appkaspi "github.com/vevovip/chaospay/internal/application/kaspi"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	kaspiports "github.com/vevovip/chaospay/internal/ports/api/kaspi"
)

func newServer() *httptest.Server {
	repo := memstore.NewKaspiRepo()
	svc := appkaspi.NewService(repo, appkaspi.BehaviorOptions{
		StatusPollingInterval:      1,
		LinkActivationWaitTimeout:  60,
		PaymentConfirmationTimeout: 120,
	})
	mux := http.NewServeMux()
	kaspiports.NewController(svc, 0).Register(mux)

	return httptest.NewServer(mux)
}

type createLinkResp struct {
	StatusCode int `json:"StatusCode"`
	Data       struct {
		PaymentLink            string `json:"PaymentLink"`
		PaymentId              int    `json:"PaymentId"`
		PaymentBehaviorOptions struct {
			StatusPollingInterval int `json:"StatusPollingInterval"`
		} `json:"PaymentBehaviorOptions"`
	} `json:"Data"`
}

type statusResp struct {
	StatusCode int `json:"StatusCode"`
	Data       struct {
		Status string `json:"Status"`
	} `json:"Data"`
}

func createLink(t *testing.T, base, externalID string, amount float64) createLinkResp {
	t.Helper()

	body, _ := json.Marshal(map[string]any{
		"OrganizationBin": "160640004075",
		"DeviceToken":     "token",
		"Amount":          amount,
		"ExternalId":      externalID,
	})

	resp, err := http.Post(base+"/r3/v01/qr/create-link", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create-link request: %v", err)
	}
	defer resp.Body.Close()

	var out createLinkResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode create-link: %v", err)
	}

	return out
}

func getStatus(t *testing.T, base string, paymentID int) statusResp {
	t.Helper()

	resp, err := http.Get(base + "/r3/v01/payment/status/" + strconv.Itoa(paymentID))
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer resp.Body.Close()

	var out statusResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	return out
}

func testAction(t *testing.T, base, action string, paymentID int) {
	t.Helper()

	body, _ := json.Marshal(map[string]any{"qrPaymentId": paymentID})
	resp, err := http.Post(base+"/r3/v01/test/payment/"+action, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s request: %v", action, err)
	}
	_ = resp.Body.Close()
}

func TestCreateLinkThenConfirm(t *testing.T) {
	t.Parallel()

	srv := newServer()
	defer srv.Close()

	link := createLink(t, srv.URL, "12345", 1790)
	if link.StatusCode != 0 {
		t.Fatalf("create-link StatusCode = %d, want 0", link.StatusCode)
	}
	if link.Data.PaymentId == 0 {
		t.Fatalf("create-link PaymentId is empty")
	}
	if link.Data.PaymentLink == "" {
		t.Fatalf("create-link PaymentLink is empty")
	}
	if link.Data.PaymentBehaviorOptions.StatusPollingInterval != 1 {
		t.Fatalf("StatusPollingInterval = %d, want 1", link.Data.PaymentBehaviorOptions.StatusPollingInterval)
	}

	if got := getStatus(t, srv.URL, link.Data.PaymentId); got.Data.Status != "Wait" {
		t.Fatalf("initial status = %q, want Wait", got.Data.Status)
	}

	testAction(t, srv.URL, "confirm", link.Data.PaymentId)

	if got := getStatus(t, srv.URL, link.Data.PaymentId); got.Data.Status != "Processed" {
		t.Fatalf("after confirm status = %q, want Processed", got.Data.Status)
	}
}

func TestCreateLinkThenDecline(t *testing.T) {
	t.Parallel()

	srv := newServer()
	defer srv.Close()

	link := createLink(t, srv.URL, "777", 500)

	testAction(t, srv.URL, "scanerror", link.Data.PaymentId)

	if got := getStatus(t, srv.URL, link.Data.PaymentId); got.Data.Status != "Error" {
		t.Fatalf("after decline status = %q, want Error", got.Data.Status)
	}
}

func TestStatusNotFound(t *testing.T) {
	t.Parallel()

	srv := newServer()
	defer srv.Close()

	got := getStatus(t, srv.URL, 999999)
	if got.StatusCode == 0 {
		t.Fatalf("not-found status returned StatusCode 0, want non-zero")
	}
}
