package epay

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
)

// applyScenarioBefore — content-level actions, не требующие штатного handler-а.
// Возвращает true, если ответ уже отправлен.
func (c *Controller) applyScenarioBefore(w http.ResponseWriter, sc *scenario.Scenario, entry *requestlog.Entry, started time.Time) bool { //nolint:gocyclo
	switch sc.Action {
	case scenario.ActionDelay:
		time.Sleep(time.Duration(scenario.ParamInt(sc, "seconds", 5)) * time.Second)
		return false

	case scenario.ActionForceFailure:
		code := scenario.ParamInt(sc, "reason_code", 477)
		msg := scenario.Param(sc, "message", infraepay.DefaultMessage(code))
		// Halyk возвращает 400 + {code, message} при бизнес-ошибке.
		c.respondJSON(w, entry, started, http.StatusBadRequest, infraepay.ErrorResponse{
			Code:       code,
			Message:    msg,
			ResultCode: code,
		})
		return true

	case scenario.ActionAmbiguousError, scenario.ActionEpayAmbiguous:
		// Halyk-аналог Freedom EX-1001: charge упал с 400+477, но операция на самом
		// деле прошла. PG должен сходить в check-status → увидеть AUTH/CHARGE → принять.
		code := scenario.ParamInt(sc, "reason_code", 477)
		msg := scenario.Param(sc, "message", "Operation already exists")
		c.respondJSON(w, entry, started, http.StatusBadRequest, infraepay.ErrorResponse{
			Code:       code,
			Message:    msg,
			ResultCode: code,
		})
		return true

	case scenario.ActionForceUnauthorized:
		msg := scenario.Param(sc, "message", "Unauthorized")
		c.respondJSON(w, entry, started, http.StatusUnauthorized, infraepay.ErrorResponse{Message: msg})
		return true

	case scenario.ActionForceForbidden:
		msg := scenario.Param(sc, "message", "Forbidden")
		c.respondJSON(w, entry, started, http.StatusForbidden, infraepay.ErrorResponse{Message: msg})
		return true

	case scenario.ActionTransientFailure:
		// Первая попытка → 5xx, последующие → штатный ответ. HitCount считается
		// внутри store.Match: первое срабатывание = 1.
		if sc.HitCount == 1 {
			status := scenario.ParamInt(sc, "http_status", http.StatusInternalServerError)
			msg := scenario.Param(sc, "message", "Service temporarily unavailable")
			c.respondJSON(w, entry, started, status, infraepay.ErrorResponse{Message: msg})
			return true
		}
		return false

	case scenario.ActionPostlinkBeforeAck:
		// Сначала шлём postlink (если webhook доступен), потом возвращаем нормальный ответ.
		// Логика: handler штатно вызовется ПОСЛЕ нас (возвращаем false), но postlink уйдёт
		// прямо сейчас. Это воспроизводит race-кейсы PG, где postlink приходит до charge-ответа.
		if c.webhook != nil && c.cfg.AutoWebhook {
			go c.sendBeforeAckPostlink(entry)
		}
		return false
	}
	return false
}

// applyScenarioAfter — модификации поверх валидного ответа.
// Применяется ТОЛЬКО к структурированным response (AuthorizeResponse / OperationResponse).
func applyScenarioAfter(sc *scenario.Scenario, resp any) any {
	switch sc.Action {
	case scenario.ActionWrongPaymentID:
		if ar, ok := resp.(infraepay.AuthorizeResponse); ok {
			ar.ID = scenario.Param(sc, "payment_id", "00000000-0000-0000-0000-000000000000")
			return ar
		}
	case scenario.ActionWrongAmount:
		if ar, ok := resp.(infraepay.AuthorizeResponse); ok {
			if v := scenario.ParamInt(sc, "amount", 1); v > 0 {
				ar.Amount = v
			}
			return ar
		}
	case scenario.ActionMissingField:
		// Для JSON missing_field стирает значение поля (id/reference/invoiceId).
		if ar, ok := resp.(infraepay.AuthorizeResponse); ok {
			switch scenario.Param(sc, "field", "") {
			case "id":
				ar.ID = ""
			case "reference":
				ar.Reference = ""
			case "invoiceId":
				ar.InvoiceID = ""
			case "cardID":
				ar.CardID = ""
			}
			return ar
		}
	case scenario.ActionPartialAmount:
		if ar, ok := resp.(infraepay.AuthorizeResponse); ok {
			ar.Amount = scenario.ParamInt(sc, "amount", ar.Amount/2)
			return ar
		}
	}
	return resp
}

// sendBeforeAckPostlink отправляет ранний postlink (race-кейс).
// Использует данные из текущей записи, найденной по PaymentID/OrderID из entry.
func (c *Controller) sendBeforeAckPostlink(entry *requestlog.Entry) {
	if entry.PaymentID == "" {
		return
	}
	pid, err := strconv.ParseUint(entry.PaymentID, 10, 64)
	if err != nil {
		return
	}
	rec, err := c.svc.Repo().Get(uint(pid))
	if err != nil {
		return
	}
	_, _ = c.webhook.SendSuccess(rec)
}
