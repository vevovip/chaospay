// Package scenarioapply содержит общие сценарии transport-уровня, применимые
// ко всем endpoint-ам (Freedom Pay XML, ApplePay/GooglePay, QR-PAY). Эти actions
// не зависят от формата тела ответа — они либо обрывают соединение, либо
// отдают мусор/код ошибки до того, как клиент успеет распарсить.
package scenarioapply

import (
	"net/http"
	"time"

	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// Transport применяет transport-уровневый action из сценария к ответу.
// Возвращает true, если ответ уже отправлен (или соединение оборвано) и нужно прекратить обработку.
//
// Покрывает actions: timeout, http_error, connection_reset, empty_response,
// malformed_body, slow_body, wrong_status_code.
func Transport(w http.ResponseWriter, sc *scenario.Scenario, entry *requestlog.Entry, started time.Time, log *memstore.RequestLog) bool {
	if sc == nil {
		return false
	}
	switch sc.Action {
	case scenario.ActionTimeout:
		secs := scenario.ParamInt(sc, "seconds", 20)
		time.Sleep(time.Duration(secs) * time.Second)
		finalize(log, entry, started, 0, "(timeout)")
		hijackClose(w)
		return true

	case scenario.ActionConnectionReset:
		finalize(log, entry, started, 0, "(connection reset)")
		hijackClose(w)
		return true

	case scenario.ActionHTTPError:
		code := scenario.ParamInt(sc, "http_status", http.StatusInternalServerError)
		finalize(log, entry, started, code, "(http error scenario)")
		http.Error(w, http.StatusText(code), code)
		return true

	case scenario.ActionWrongStatusCode:
		code := scenario.ParamInt(sc, "http_status", http.StatusTeapot)
		finalize(log, entry, started, code, "(wrong status code scenario)")
		w.WriteHeader(code)
		return true

	case scenario.ActionEmptyResponse:
		finalize(log, entry, started, http.StatusOK, "(empty body)")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		return true

	case scenario.ActionMalformedBody:
		body := scenario.Param(sc, "body", "<<<NOT_VALID_XML_OR_JSON>>>")
		ct := scenario.Param(sc, "content_type", "application/xml; charset=utf-8")
		finalize(log, entry, started, http.StatusOK, body)
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
		return true

	case scenario.ActionSlowBody:
		// Headers ушли сразу — клиент думает, что ответ пошёл, и начинает читать body.
		// Тело пишем медленно по байту, ловим Client.Timeout на чтении / context deadline.
		delayMS := scenario.ParamInt(sc, "chunk_delay_ms", 500)
		body := scenario.Param(sc, "body", "<response><pg_status>ok</pg_status></response>")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, ch := range []byte(body) {
			_, werr := w.Write([]byte{ch})
			if werr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(time.Duration(delayMS) * time.Millisecond)
		}
		finalize(log, entry, started, http.StatusOK, "(slow body)")
		return true
	}
	return false
}

func finalize(log *memstore.RequestLog, entry *requestlog.Entry, started time.Time, code int, body string) {
	entry.StatusCode = code
	entry.ResponseBody = requestlog.Truncate(body, 4000)
	entry.DurationMS = time.Since(started).Milliseconds()
	log.Add(entry)
}

func hijackClose(w http.ResponseWriter) {
	if h, ok := w.(http.Hijacker); ok {
		if conn, _, err := h.Hijack(); err == nil {
			_ = conn.Close()
			return
		}
	}
	w.WriteHeader(http.StatusGatewayTimeout)
}
