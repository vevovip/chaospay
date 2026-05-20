package flitt

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
)

// applyScenarioBefore — content-level actions, не требующие штатного handler-а.
// Возвращает true, если ответ уже отправлен.
func (c *Controller) applyScenarioBefore(w http.ResponseWriter, sc *scenario.Scenario, entry *requestlog.Entry, started time.Time) bool {
	switch sc.Action {
	case scenario.ActionDelay:
		time.Sleep(time.Duration(scenario.ParamInt(sc, "seconds", 5)) * time.Second)
		return false

	case scenario.ActionForceFailure:
		// Если outcome задан — обрабатываем штатно в handler-е, чтобы создать запись
		// со статусом Failed. Если outcome не задан — отдаём FAILURE-конверт сразу.
		if scenario.Param(sc, "outcome", "") != "" {
			return false
		}
		code := scenario.ParamInt(sc, "error_code", 1001)
		msg := scenario.Param(sc, "message", "payment declined")
		c.respondFailure(w, entry, started, http.StatusOK, code, msg)
		return true

	case scenario.ActionAmbiguousError:
		// Аналог EX-1001: возвращаем FAILURE, но запись остаётся в Authorized.
		// На следующий status — реальный статус.
		code := scenario.ParamInt(sc, "error_code", 1001)
		msg := scenario.Param(sc, "message", "ambiguous error")
		c.respondFailure(w, entry, started, http.StatusOK, code, msg)
		return true

	case scenario.ActionForceUnauthorized:
		w.Header().Set("Content-Type", "application/json")
		c.respondJSON(w, entry, started, http.StatusUnauthorized, infraflitt.NewFailure(1006, "unauthorized"))
		return true

	case scenario.ActionForceForbidden:
		c.respondJSON(w, entry, started, http.StatusForbidden, infraflitt.NewFailure(1007, "forbidden"))
		return true

	case scenario.ActionTransientFailure:
		// Первая попытка — 5xx, далее — штатно.
		if sc.HitCount == 1 {
			status := scenario.ParamInt(sc, "http_status", http.StatusInternalServerError)
			msg := scenario.Param(sc, "message", "service temporarily unavailable")
			c.respondJSON(w, entry, started, status, infraflitt.NewFailure(status, msg))
			return true
		}
		return false

	case scenario.ActionSyncErrorAsyncWebhook:
		// Аналог EX-1001 silent hold: сначала шлём callback (через webhook),
		// потом возвращаем FAILURE.
		if c.webhook != nil && c.cfg.AutoWebhook {
			go c.sendAsyncCallback(entry, true)
		}
		code := scenario.ParamInt(sc, "error_code", 1001)
		msg := scenario.Param(sc, "message", "payment declined")
		c.respondFailure(w, entry, started, http.StatusOK, code, msg)
		return true
	}
	return false
}

// applyScenarioAfter — модификации поверх валидного ответа.
func applyScenarioAfter(sc *scenario.Scenario, resp any) any {
	switch sc.Action {
	case scenario.ActionWrongPaymentID:
		newID, _ := strconv.ParseInt(scenario.Param(sc, "payment_id", "9999999999"), 10, 64)
		switch r := resp.(type) {
		case infraflitt.DirectEnvelope:
			r.Response.PaymentID = newID
			return r
		case infraflitt.RecurringEnvelope:
			r.Response.PaymentID = newID
			return r
		case infraflitt.StatusEnvelope:
			r.Response.PaymentID = newID
			return r
		}

	case scenario.ActionWrongAmount:
		newAmount := scenario.Param(sc, "amount", "1")
		switch r := resp.(type) {
		case infraflitt.RecurringEnvelope:
			r.Response.Amount = newAmount
			return r
		case infraflitt.StatusEnvelope:
			r.Response.Amount = newAmount
			r.Response.ActualAmount = newAmount
			r.Response.SettlementAmount = newAmount
			return r
		}

	case scenario.ActionMissingField:
		field := scenario.Param(sc, "field", "")
		switch r := resp.(type) {
		case infraflitt.StatusEnvelope:
			switch field {
			case "approval_code":
				r.Response.ApprovalCode = ""
			case "rrn":
				r.Response.RRN = ""
			case "signature":
				r.Response.Signature = ""
			case "masked_card":
				r.Response.MaskedCard = ""
			}
			return r
		case infraflitt.DirectEnvelope:
			if field == "approval_code" {
				r.Response.ApprovalCode = ""
			}
			return r
		}

	case scenario.ActionForceStatus:
		// Подмена order_status в Status-ответе.
		newStatus := scenario.Param(sc, "order_status", infraflitt.OrderStatusApproved)
		if r, ok := resp.(infraflitt.StatusEnvelope); ok {
			r.Response.OrderStatus = newStatus
			if newStatus == infraflitt.OrderStatusApproved {
				r.Response.ResponseStatus = infraflitt.ResponseStatusSuccess
			} else {
				r.Response.ResponseStatus = infraflitt.ResponseStatusFailure
			}
			return r
		}
	}
	return resp
}

// sendAsyncCallback — отправка callback-а в режиме SyncErrorAsyncWebhook.
// Идёт по entry.PaymentID (если он есть). Если нет — пытаемся orderID.
func (c *Controller) sendAsyncCallback(entry *requestlog.Entry, success bool) {
	if entry.PaymentID != "" {
		if pid, err := strconv.ParseUint(entry.PaymentID, 10, 64); err == nil {
			_ = c.svc.SendFlittCallback(uint(pid), success)
			return
		}
	}
	if entry.OrderID == "" {
		return
	}
	rec, err := c.svc.FlittStatus(entry.OrderID)
	if err != nil {
		return
	}
	_ = c.svc.SendFlittCallback(rec.PaymentID, success)
}
