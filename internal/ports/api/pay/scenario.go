package pay

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// applyScenarioBefore — для сценариев, не нуждающихся в нормальном handler-выполнении.
// Возвращает true, если ответ уже отправлен.
func (c *Controller) applyScenarioBefore(w http.ResponseWriter, sc *scenario.Scenario, req *freedompay.ParsedRequest, responseScriptName string, entry *requestlog.Entry, started time.Time) bool {
	// Transport-level: применимо ко всем endpoint-ам.
	if scenarioapply.Transport(w, sc, entry, started, c.log) {
		return true
	}

	switch sc.Action {
	case scenario.ActionDelay:
		secs := scenario.ParamInt(sc, "seconds", 5)
		time.Sleep(time.Duration(secs) * time.Second)
		return false

	case scenario.ActionAmbiguousError:
		errMsg := scenario.Param(sc, "message", "Неверный статус платежа")
		errCode := scenario.Param(sc, "error_code", "120")
		fields := freedompay.OrdMap{}
		fields = fields.Set("pg_status", "error")
		fields = fields.Set("pg_error_code", errCode)
		fields = fields.Set("pg_failure_description", errMsg)
		body := signedXML("response", responseScriptName, fields, c.cfg.Secret)
		entry.StatusCode = http.StatusOK
		entry.ResponseBody = requestlog.Truncate(body, 4000)
		entry.DurationMS = time.Since(started).Milliseconds()
		c.log.Add(entry)
		writeXML(w, body)
		return true

	case scenario.ActionForceFailure:
		errMsg := scenario.Param(sc, "message", "forced failure")
		errCode := scenario.Param(sc, "error_code", "100")
		fields := freedompay.OrdMap{}
		fields = fields.Set("pg_status", "error")
		fields = fields.Set("pg_error_code", errCode)
		fields = fields.Set("pg_error_description", errMsg)
		body := signedXML("response", responseScriptName, fields, c.cfg.Secret)
		entry.StatusCode = http.StatusOK
		entry.ResponseBody = requestlog.Truncate(body, 4000)
		entry.DurationMS = time.Since(started).Milliseconds()
		c.log.Add(entry)
		writeXML(w, body)
		return true

	case scenario.ActionSyncErrorAsyncWebhook:
		errMsg := scenario.Param(sc, "message", "Неверный статус платежа")
		errCode := scenario.Param(sc, "error_code", "120")
		fields := freedompay.OrdMap{}
		fields = fields.Set("pg_status", "error")
		fields = fields.Set("pg_error_code", errCode)
		fields = fields.Set("pg_error_description", errMsg)
		body := signedXML("response", responseScriptName, fields, c.cfg.Secret)
		entry.StatusCode = http.StatusOK
		entry.ResponseBody = requestlog.Truncate(body, 4000)
		entry.DurationMS = time.Since(started).Milliseconds()
		c.log.Add(entry)
		writeXML(w, body)

		// Параллельно холдируем платёж и шлём success-webhook (EX-1001: банк "молча списал").
		paymentIDStr := req.Get("pg_payment_id", "")
		if paymentIDStr != "" {
			if id, err := strconv.ParseUint(paymentIDStr, 10, 64); err == nil && id > 0 {
				go func(pid uint) {
					if _, holdErr := c.svc.Hold(pid); holdErr != nil {
						log.Printf("[PAY sync_error_async_webhook] hold(%d) failed: %v", pid, holdErr)
					}
				}(uint(id))
			}
		}
		return true
	}
	return false
}

// applyScenarioAfter — модификации поверх валидного ответа.
func applyScenarioAfter(sc *scenario.Scenario, fields freedompay.OrdMap) freedompay.OrdMap {
	switch sc.Action {
	case scenario.ActionForceStatus:
		if v := scenario.Param(sc, "payment_status", ""); v != "" {
			fields = fields.Set("pg_payment_status", v)
			fields = stripTerminalFieldsIfNonTerminal(fields, v)
		}
	case scenario.ActionPartialAmount:
		if v := scenario.Param(sc, "amount", ""); v != "" {
			fields = fields.Set("pg_amount", v)
			fields = fields.Set("pg_clearing_amount", v)
		}
	case scenario.ActionWrongPaymentID:
		v := scenario.Param(sc, "payment_id", "9999999999")
		fields = fields.Set("pg_payment_id", v)
	case scenario.ActionWrongAmount:
		if v := scenario.Param(sc, "amount", ""); v != "" {
			fields = fields.Set("pg_amount", v)
			fields = fields.Set("pg_clearing_amount", v)
		}
	case scenario.ActionMissingField:
		if v := scenario.Param(sc, "field", ""); v != "" {
			fields = fields.Delete(v)
		}
	case scenario.ActionExtraGarbage:
		n := scenario.ParamInt(sc, "count", 5)
		for i := 0; i < n; i++ {
			fields = fields.Set(fmt.Sprintf("pg_garbage_%d", i), fmt.Sprintf("noise-%d", i))
		}
	}
	return fields
}

// stripTerminalFieldsIfNonTerminal приводит ответ к виду реального FFB:
// для non-terminal статусов (waiting/process/new) terminal-поля очищаются,
// потому что у FFB их там быть не может — платёж ещё не финализирован.
func stripTerminalFieldsIfNonTerminal(fields freedompay.OrdMap, paymentStatus string) freedompay.OrdMap {
	switch paymentStatus {
	case "waiting", "process", "new":
	default:
		return fields
	}

	fields = fields.Set("pg_payment_method", "none")
	fields = fields.Delete("pg_order_id")
	fields = fields.Delete("pg_auth_code")
	fields = fields.Delete("pg_reference")
	fields = fields.Delete("pg_card_pan")
	fields = fields.Delete("pg_card_token")
	fields = fields.Delete("pg_captured")
	fields = fields.Delete("pg_payment_date")

	return fields
}
