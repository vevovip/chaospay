package pay

import (
	"fmt"
	"net/http"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// applyScenarioBefore — для сценариев, не нуждающихся в нормальном handler-выполнении.
// Возвращает true, если ответ уже отправлен.
func (c *Controller) applyScenarioBefore(w http.ResponseWriter, sc *scenario.Scenario, responseScriptName string, entry *requestlog.Entry, started time.Time) bool {
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
	}
	return false
}

// applyScenarioAfter — модификации поверх валидного ответа.
func applyScenarioAfter(sc *scenario.Scenario, fields freedompay.OrdMap) freedompay.OrdMap {
	switch sc.Action {
	case scenario.ActionForceStatus:
		if v := scenario.Param(sc, "payment_status", ""); v != "" {
			fields = fields.Set("pg_payment_status", v)
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
