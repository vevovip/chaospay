// Package qr — HTTP handlers Single QR (JSON + Basic Auth).
package qr

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	appqr "github.com/vevovip/chaospay/internal/application/qr"
	domainqr "github.com/vevovip/chaospay/internal/domain/qr"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// Controller — HTTP-контроллер QR.
type Controller struct {
	svc                *appqr.Service
	globalDelaySeconds int
}

// NewController конструктор.
func NewController(svc *appqr.Service, globalDelaySeconds int) *Controller {
	return &Controller{svc: svc, globalDelaySeconds: globalDelaySeconds}
}

// Register регистрирует все QR routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /qr-code/generate", c.handleGenerate)
	mux.HandleFunc("GET /qr-code/get-status/{uuid}", c.handleGetStatus)
	mux.HandleFunc("POST /qr-code/change-status", c.handleChangeStatus)
	mux.HandleFunc("GET /qr-code/get-status-refund/{uuid}", c.handleGetRefundStatus)
	mux.HandleFunc("POST /qr-code/confirm-refund", c.handleConfirmRefund)
}

type generateRequest struct {
	Beneficiary struct {
		BIN string `json:"bin"`
		TID string `json:"tid"`
		MID string `json:"mid"`
	} `json:"beneficiary"`
	Payment struct {
		Amount     float64 `json:"amount"`
		DataType   string  `json:"dataType"`
		DeviceType string  `json:"deviceType"`
	} `json:"payment"`
}

type generateResponse struct {
	UUID string `json:"uuid"`
	QR   string `json:"qr"`
}

func (c *Controller) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if !checkBasicAuth(w, r) {
		return
	}
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", r.URL.Path)
		return
	}
	c.delay()
	code, err := c.svc.Generate(appqr.GenerateInput{
		BIN: req.Beneficiary.BIN, TID: req.Beneficiary.TID, MID: req.Beneficiary.MID,
		Amount: req.Payment.Amount, DataType: req.Payment.DataType,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), r.URL.Path)
		return
	}
	log.Printf("[qr] generated %s amount=%.2f bin=%s/%s/%s refund=%v",
		code.UUID, code.Amount, code.BIN, code.TID, code.MID, code.IsRefund)
	writeJSON(w, generateResponse{UUID: code.UUID, QR: code.QRBase64})
}

type getStatusResponse struct {
	UUID    string `json:"uuid"`
	Status  string `json:"status"`
	TrnID   int64  `json:"trnId,omitempty"`
	TrnDate string `json:"trnDate,omitempty"`
}

func (c *Controller) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if !checkBasicAuth(w, r) {
		return
	}
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "UUID is missing", r.URL.Path)
		return
	}
	c.delay()
	code, err := c.svc.GetStatus(uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, "No data found for UUID: "+uuid, r.URL.Path)
		return
	}
	resp := getStatusResponse{UUID: code.UUID, Status: string(code.Status)}
	if code.Status == domainqr.StatusSuccess {
		resp.TrnID = code.TrnID
		resp.TrnDate = code.TrnDate
	}
	writeJSON(w, resp)
}

type changeStatusRequest struct {
	UUID   string `json:"uuid"`
	Status string `json:"status"`
}

type changeStatusResponse struct {
	UUID   string `json:"uuid"`
	Status string `json:"status"`
}

func (c *Controller) handleChangeStatus(w http.ResponseWriter, r *http.Request) {
	if !checkBasicAuth(w, r) {
		return
	}
	var req changeStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", r.URL.Path)
		return
	}
	if req.UUID == "" {
		writeError(w, http.StatusBadRequest, "UUID is missing", r.URL.Path)
		return
	}
	if req.Status == "" {
		writeError(w, http.StatusBadRequest, "Status is missing", r.URL.Path)
		return
	}
	c.delay()
	updated, err := c.svc.ChangeStatus(req.UUID, domainqr.Status(req.Status))
	if err != nil {
		switch {
		case errors.Is(err, memstore.ErrQRNotFound):
			writeError(w, http.StatusNotFound, "QR info not found for UUID: "+req.UUID, r.URL.Path)
		case errors.Is(err, memstore.ErrQRTerminal):
			writeError(w, http.StatusGone, err.Error(), r.URL.Path)
		default:
			writeError(w, http.StatusBadRequest, err.Error(), r.URL.Path)
		}
		return
	}
	log.Printf("[qr] %s → %s", req.UUID, updated.Status)
	writeJSON(w, changeStatusResponse{UUID: updated.UUID, Status: string(updated.Status)})
}

type getRefundStatusResponse struct {
	UUID         string                       `json:"uuid"`
	Status       string                       `json:"status"`
	Transactions []domainqr.RefundTransaction `json:"transactions,omitempty"`
}

func (c *Controller) handleGetRefundStatus(w http.ResponseWriter, r *http.Request) {
	uuid := r.PathValue("uuid")
	if !checkBasicAuth(w, r) {
		return
	}
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "UUID is missing", r.URL.Path)
		return
	}
	c.delay()
	code, err := c.svc.GetStatus(uuid)
	if err != nil {
		writeError(w, http.StatusNotFound, "No data found for UUID: "+uuid, r.URL.Path)
		return
	}
	if !code.IsRefund {
		writeError(w, http.StatusBadRequest, "UUID is not a refund QR: "+uuid, r.URL.Path)
		return
	}
	resp := getRefundStatusResponse{UUID: code.UUID, Status: string(code.Status)}
	if code.Status == domainqr.StatusScanned {
		resp.Transactions = c.svc.ListRefundTransactions(code.BIN, code.TID, code.MID)
	}
	writeJSON(w, resp)
}

type confirmRefundRequest struct {
	UUID        string  `json:"uuid"`
	Amount      float64 `json:"amount"`
	Reference   string  `json:"reference"`
	ParentTrnID string  `json:"parentTrnId"`
}

type confirmRefundResponse struct {
	UUID   string `json:"uuid"`
	Status string `json:"status"`
	TrnID  string `json:"trnId,omitempty"`
	ErrMsg string `json:"errMsg"`
}

func (c *Controller) handleConfirmRefund(w http.ResponseWriter, r *http.Request) {
	if !checkBasicAuth(w, r) {
		return
	}
	var req confirmRefundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body", r.URL.Path)
		return
	}
	if req.UUID == "" {
		writeError(w, http.StatusBadRequest, "UUID is missing", r.URL.Path)
		return
	}
	if req.Reference == "" {
		writeError(w, http.StatusBadRequest, "reference is missing", r.URL.Path)
		return
	}
	if req.ParentTrnID == "" {
		writeError(w, http.StatusBadRequest, "parentTrnId is missing", r.URL.Path)
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be greater than 0", r.URL.Path)
		return
	}
	c.delay()
	updated, err := c.svc.ConfirmRefund(req.UUID, req.Reference, req.ParentTrnID, req.Amount)
	if err != nil {
		switch {
		case errors.Is(err, memstore.ErrRefundAlreadyConfirmed):
			writeError(w, http.StatusGone, "Refund already confirmed for UUID: "+req.UUID, r.URL.Path)
		case errors.Is(err, memstore.ErrQRNotFound):
			writeError(w, http.StatusNotFound, "No data found for UUID: "+req.UUID, r.URL.Path)
		default:
			writeError(w, http.StatusGone, err.Error(), r.URL.Path)
		}
		return
	}
	log.Printf("[qr-refund] confirmed %s amount=%.2f → trnID=%d", req.UUID, req.Amount, updated.TrnID)
	writeJSON(w, confirmRefundResponse{
		UUID:   updated.UUID,
		Status: string(updated.Status),
		TrnID:  strconv.FormatInt(updated.TrnID, 10),
		ErrMsg: "OK",
	})
}

func (c *Controller) delay() {
	if c.globalDelaySeconds > 0 {
		time.Sleep(time.Duration(c.globalDelaySeconds) * time.Second)
	}
}

// ----- helpers -----

func checkBasicAuth(_ http.ResponseWriter, _ *http.Request) bool {
	// Любой логин/пароль валиден (как у старого мока).
	return true
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

type errorResponse struct {
	Timestamp string `json:"timestamp"`
	Status    int    `json:"status"`
	Error     string `json:"error"`
	Message   string `json:"message"`
	Path      string `json:"path"`
}

func writeError(w http.ResponseWriter, code int, message, path string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Status:    code,
		Error:     http.StatusText(code),
		Message:   message,
		Path:      path,
	})
}
