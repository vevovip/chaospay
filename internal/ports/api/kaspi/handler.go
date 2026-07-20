// Package kaspi — HTTP-хендлеры мока KaspiPay (JSON, polling-based).
//
// PG обращается к Kaspi по base URI с префиксом /r3 (см. KASPI_BASE_URI),
// поэтому роуты регистрируются и на /r3/v01/..., и на /v01/... .
package kaspi

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	appkaspi "github.com/vevovip/chaospay/internal/application/kaspi"
	domainkaspi "github.com/vevovip/chaospay/internal/domain/kaspi"
)

const (
	statusCodeOK       = 0
	statusCodeNotFound = -1601 // "Покупка не найдена" (из PG statusCodeMapError)

	linkTTL     = 4 * time.Minute
	payLinkBase = "https://pay.kaspi.kz/pay/"
)

// paymentMethods — методы оплаты, которые Kaspi обычно возвращает в create-link.
var paymentMethods = []string{"Gold", "Red", "Loan"}

// Controller — HTTP-контроллер мока KaspiPay.
type Controller struct {
	svc                *appkaspi.Service
	globalDelaySeconds int
}

// NewController конструктор.
func NewController(svc *appkaspi.Service, globalDelaySeconds int) *Controller {
	return &Controller{svc: svc, globalDelaySeconds: globalDelaySeconds}
}

// Register регистрирует Kaspi routes на префиксах /r3/v01 и /v01.
func (c *Controller) Register(mux *http.ServeMux) {
	for _, p := range []string{"/r3", ""} {
		mux.HandleFunc("POST "+p+"/v01/qr/create-link", c.handleCreateLink)
		mux.HandleFunc("GET "+p+"/v01/payment/status/{ref}", c.handleStatus)
		mux.HandleFunc("POST "+p+"/v01/test/payment/confirm", c.handleTestConfirm)
		mux.HandleFunc("POST "+p+"/v01/test/payment/scanerror", c.handleTestDecline)
	}
}

type createLinkRequest struct {
	OrganizationBin string  `json:"OrganizationBin"`
	DeviceToken     string  `json:"DeviceToken"`
	Amount          float64 `json:"Amount"`
	ExternalId      string  `json:"ExternalId"`
}

type behaviorOptions struct {
	StatusPollingInterval      int `json:"StatusPollingInterval"`
	LinkActivationWaitTimeout  int `json:"LinkActivationWaitTimeout"`
	PaymentConfirmationTimeout int `json:"PaymentConfirmationTimeout"`
}

type createLinkData struct {
	PaymentLink            string          `json:"PaymentLink"`
	ExpireDate             time.Time       `json:"ExpireDate"`
	PaymentId              int             `json:"PaymentId"`
	PaymentMethods         []string        `json:"PaymentMethods"`
	PaymentBehaviorOptions behaviorOptions `json:"PaymentBehaviorOptions"`
}

type createLinkResponse struct {
	StatusCode int            `json:"StatusCode"`
	Message    string         `json:"Message"`
	Data       createLinkData `json:"Data"`
}

func (c *Controller) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var req createLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	c.delay()

	payment := c.svc.CreateLink(req.ExternalId, req.Amount)
	behavior := c.svc.Behavior()

	log.Printf("[kaspi] create-link external=%s amount=%.2f -> paymentId=%d",
		req.ExternalId, req.Amount, payment.PaymentID)

	writeJSON(w, createLinkResponse{
		StatusCode: statusCodeOK,
		Data: createLinkData{
			PaymentLink:    payLinkBase + strconv.Itoa(payment.PaymentID),
			ExpireDate:     payment.CreatedAt.Add(linkTTL),
			PaymentId:      payment.PaymentID,
			PaymentMethods: paymentMethods,
			PaymentBehaviorOptions: behaviorOptions{
				StatusPollingInterval:      behavior.StatusPollingInterval,
				LinkActivationWaitTimeout:  behavior.LinkActivationWaitTimeout,
				PaymentConfirmationTimeout: behavior.PaymentConfirmationTimeout,
			},
		},
	})
}

type statusResponse struct {
	StatusCode int        `json:"StatusCode"`
	Message    string     `json:"Message"`
	Data       statusData `json:"Data"`
}

type statusData struct {
	Status string `json:"Status"`
}

func (c *Controller) handleStatus(w http.ResponseWriter, r *http.Request) {
	paymentID, ok := parseRef(w, r)
	if !ok {
		return
	}

	c.delay()

	payment, err := c.svc.GetStatus(paymentID)
	if err != nil {
		log.Printf("[kaspi] status paymentId=%d not found", paymentID)
		writeJSON(w, statusResponse{StatusCode: statusCodeNotFound, Message: "Покупка не найдена"})

		return
	}

	writeJSON(w, statusResponse{
		StatusCode: statusCodeOK,
		Data:       statusData{Status: string(payment.Status)},
	})
}

type testPaymentRequest struct {
	QrPaymentID int `json:"qrPaymentId"`
}

// handleTestConfirm переводит платёж в Processed (пользователь подтвердил оплату).
func (c *Controller) handleTestConfirm(w http.ResponseWriter, r *http.Request) {
	c.setTestStatus(w, r, domainkaspi.StatusProcessed)
}

// handleTestDecline переводит платёж в Error (пользователь ошибся/отклонил).
func (c *Controller) handleTestDecline(w http.ResponseWriter, r *http.Request) {
	c.setTestStatus(w, r, domainkaspi.StatusError)
}

func (c *Controller) setTestStatus(w http.ResponseWriter, r *http.Request, status domainkaspi.Status) {
	var req testPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")

		return
	}

	payment, err := c.svc.SetStatus(req.QrPaymentID, status)
	if err != nil {
		writeError(w, http.StatusNotFound, "payment not found")

		return
	}

	log.Printf("[kaspi] test set paymentId=%d -> %s", payment.PaymentID, status)
	writeJSON(w, statusResponse{StatusCode: statusCodeOK, Data: statusData{Status: string(payment.Status)}})
}

func parseRef(w http.ResponseWriter, r *http.Request) (int, bool) {
	ref := r.PathValue("ref")

	paymentID, err := strconv.Atoi(ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid payment reference")

		return 0, false
	}

	return paymentID, true
}

func (c *Controller) delay() {
	if c.globalDelaySeconds > 0 {
		time.Sleep(time.Duration(c.globalDelaySeconds) * time.Second)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"StatusCode": code,
		"Message":    message,
	})
}
