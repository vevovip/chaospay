// Package wallet — POST /pay/{paymentID}/pay (ApplePay JSON, GooglePay form).
package wallet

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/domain/bank"
	domainpay "github.com/vevovip/chaospay/internal/domain/pay"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// Controller — wallet HTTP-handler.
type Controller struct {
	svc       *apppay.Service
	scenarios *appscenario.Service
	log       *memstore.RequestLog
}

// NewController конструктор.
func NewController(svc *apppay.Service, scenarios *appscenario.Service, log *memstore.RequestLog) *Controller {
	return &Controller{svc: svc, scenarios: scenarios, log: log}
}

// Register регистрирует /pay/{paymentID}/pay.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /pay/{paymentID}/pay", c.handlePay)
}

func (c *Controller) handlePay(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	paymentIDStr := r.PathValue("paymentID")
	paymentID, parseErr := strconv.ParseUint(paymentIDStr, 10, 64)

	entry := &requestlog.Entry{Method: r.Method, URL: r.URL.Path, PaymentID: paymentIDStr, Bank: bank.Freedom}

	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	entry.RequestBody = requestlog.Truncate(string(bodyBytes), 4000)

	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	isJSON := strings.Contains(contentType, "application/json")
	if isJSON {
		entry.Endpoint = "applepay"
	} else {
		entry.Endpoint = "googlepay"
	}

	if parseErr != nil {
		c.respondError(w, entry, started, http.StatusBadRequest, "invalid payment id")
		return
	}

	rec, err := c.svc.Repo().Get(uint(paymentID))
	if err != nil {
		c.respondError(w, entry, started, http.StatusNotFound, "payment not found")
		return
	}
	entry.OrderID = strconv.FormatUint(uint64(rec.OrderID), 10)
	entry.MerchantID = strconv.FormatUint(uint64(rec.MerchantID), 10)

	sc := c.scenarios.Match(scenario.MatchInput{
		Bank:       bank.Freedom,
		Endpoint:   entry.Endpoint,
		PaymentID:  paymentIDStr,
		OrderID:    entry.OrderID,
		MerchantID: entry.MerchantID,
	})
	if sc != nil {
		entry.ScenarioHit = sc.ID
		entry.ScenarioName = string(sc.Action)
		// Transport-level (timeout/http_error/connection_reset/empty/malformed/slow/wrong_status).
		if scenarioapply.Transport(w, sc, entry, started, c.log) {
			return
		}
		switch sc.Action {
		case scenario.ActionDelay:
			time.Sleep(time.Duration(scenario.ParamInt(sc, "seconds", 5)) * time.Second)
		case scenario.ActionForceFailure:
			c.respondWalletError(w, scenario.Param(sc, "message", "forced failure"))
			c.finalize(entry, started, http.StatusOK, "")
			return
		case scenario.ActionAmbiguousError:
			// Wallet (JSON): отдаём ту же error-форму с ambiguous-маркером в message.
			// PG ловит фразу в error_classifier.go → идёт в ReconcilingClient.
			msg := scenario.Param(sc, "message", "Неверный статус платежа")
			c.respondWalletError(w, msg)
			c.finalize(entry, started, http.StatusOK, msg)
			return
		case scenario.ActionSyncErrorAsyncWebhook:
			// EX-1001 для wallet: синхронно отдаём ошибку, асинхронно холдируем платёж
			// (Hold сам пошлёт success-webhook на PG из pgclient).
			msg := scenario.Param(sc, "message", "Неверный статус платежа")
			c.respondWalletError(w, msg)
			c.finalize(entry, started, http.StatusOK, msg)

			go func(pid uint) {
				if _, holdErr := c.svc.Hold(pid); holdErr != nil {
					log.Printf("[wallet sync_error_async_webhook] hold(%d) failed: %v", pid, holdErr)
				}
			}(uint(paymentID))
			return
		}
	}

	kind := domainpay.KindGooglePay
	if isJSON {
		kind = domainpay.KindApplePay
	}
	updated, errA := c.svc.AuthorizeWallet(uint(paymentID), kind)
	if errA != nil {
		c.respondWalletError(w, errA.Error())
		c.finalize(entry, started, http.StatusOK, "")
		return
	}

	if isJSON {
		resp := map[string]any{
			"data": map[string]any{
				"status":  "ok",
				"message": "",
				"back_url": map[string]any{
					"url": fmt.Sprintf("https://rahmetapp.kz?pg_payment_id=%d&pg_order_id=%d", updated.PaymentID, updated.OrderID),
					"params": map[string]any{
						"pg_order_id":   updated.OrderID,
						"pg_payment_id": updated.PaymentID,
					},
				},
			},
		}
		writeJSON(w, resp)
		c.finalize(entry, started, http.StatusOK, jsonString(resp))
	} else {
		resp := map[string]any{
			"data": map[string]any{
				"status":  "ok",
				"message": "",
				"payment_info": map[string]any{
					"payment_id": updated.PaymentID,
				},
				"back_url":  map[string]any{"url": ""},
				"frame_url": "",
			},
		}
		writeJSON(w, resp)
		c.finalize(entry, started, http.StatusOK, jsonString(resp))
	}

	log.Printf("[wallet] paymentID=%d kind=%s authorized", updated.PaymentID, updated.Kind)
}

func (c *Controller) respondError(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, code int, msg string) {
	c.finalize(entry, started, code, msg)
	http.Error(w, msg, code)
}

func (c *Controller) respondWalletError(w http.ResponseWriter, msg string) {
	resp := map[string]any{"data": map[string]any{"status": "error", "message": msg}}
	writeJSON(w, resp)
}

func (c *Controller) finalize(entry *requestlog.Entry, started time.Time, code int, body string) {
	entry.StatusCode = code
	entry.ResponseBody = requestlog.Truncate(body, 4000)
	entry.DurationMS = time.Since(started).Milliseconds()
	c.log.Add(entry)
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return requestlog.Truncate(string(b), 4000)
}
