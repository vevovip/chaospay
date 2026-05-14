// Package epay содержит HTTP-handlers Halyk Epay v2 (JSON + OAuth Bearer).
//
// Шаблон обработки — тот же, что и в Freedom pay/handler.go:
//
//	parse → scenario.Match → applyBefore (transport+content) → handle → applyAfter → render JSON.
//
// Отличия от Freedom:
//
//   - JSON вместо XML.
//   - OAuth Bearer вместо MD5-подписи.
//   - 3DS-челлендж приходит inline в ответе на cryptopay (поле secure3D), а не отдельным редиректом.
//   - charge/cancel/refund НЕ возвращают записи — только {code, message}, реальный статус
//     PG узнаёт через postlink-webhook.
package epay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	domainbank "github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// Config — настройки Epay-контроллера.
type Config struct {
	// Allowed client credentials (мок принимает любую пару из списка или любые валидные, если пусто).
	Creds map[string]string

	// TerminalUUID — UUID терминала по умолчанию, если запрос не передал свой.
	TerminalUUID string

	// AutoWebhook — если true, мок сам шлёт postlink после успешного charge/cancel/refund.
	AutoWebhook bool

	GlobalDelaySeconds int
}

// Controller — HTTP-контроллер Epay-операций.
type Controller struct {
	svc       *apppay.Service
	scenarios *appscenario.Service
	log       *memstore.RequestLog
	tokens    *infraepay.TokenStore
	webhook   *pgclient.EpayClient
	cfg       Config
}

// NewController конструктор.
func NewController(
	svc *apppay.Service,
	scenarios *appscenario.Service,
	log *memstore.RequestLog,
	tokens *infraepay.TokenStore,
	webhook *pgclient.EpayClient,
	cfg Config,
) *Controller {
	return &Controller{
		svc:       svc,
		scenarios: scenarios,
		log:       log,
		tokens:    tokens,
		webhook:   webhook,
		cfg:       cfg,
	}
}

// Register регистрирует все Halyk Epay v2 routes.
//
// Реальные пути Halyk:
//
//	POST /oauth2/token                       — выдача access_token
//	POST /api/payment/cryptopay              — авторизация новой картой / ApplePay (cryptogram)
//	POST /api/payments/cards/auth            — авторизация сохранённой картой (cardId+accountId)
//	POST /api/operation/{id}/charge          — списание захолда
//	POST /api/operation/{id}/cancel          — отмена авторизации
//	POST /api/operation/{id}/refund?amount=… — возврат после списания
//
// В моке мы добавляем префикс /epay для большинства URL, кроме /oauth2/token,
// который в real Halyk сидит на отдельном хосте (testoauth.homebank.kz/epay2).
// На стороне PG host для оплат и для OAuth конфигурируются отдельно
// (EPAY_2_BASE_URI и EPAY_2_BASE_OAUTH_URI), поэтому никаких коллизий.
func (c *Controller) Register(mux *http.ServeMux) {
	// OAuth — без bearer, без scenario.
	mux.HandleFunc("POST /oauth2/token", c.handleToken)
	mux.HandleFunc("POST /epay2/oauth2/token", c.handleToken) // alias (полный путь real Halyk)

	mux.HandleFunc("POST /epay/api/payment/cryptopay",
		c.jsonEndpoint(scenario.EndpointEpayCryptopay, c.handleCryptopay))
	mux.HandleFunc("POST /api/payment/cryptopay",
		c.jsonEndpoint(scenario.EndpointEpayCryptopay, c.handleCryptopay))

	mux.HandleFunc("POST /epay/api/payments/cards/auth",
		c.jsonEndpoint(scenario.EndpointEpayCardAuth, c.handleCardAuth))
	mux.HandleFunc("POST /api/payments/cards/auth",
		c.jsonEndpoint(scenario.EndpointEpayCardAuth, c.handleCardAuth))

	mux.HandleFunc("POST /epay/api/operation/{operationID}/charge",
		c.jsonEndpoint(scenario.EndpointEpayCharge, c.handleCharge))
	mux.HandleFunc("POST /api/operation/{operationID}/charge",
		c.jsonEndpoint(scenario.EndpointEpayCharge, c.handleCharge))

	mux.HandleFunc("POST /epay/api/operation/{operationID}/cancel",
		c.jsonEndpoint(scenario.EndpointEpayCancel, c.handleCancel))
	mux.HandleFunc("POST /api/operation/{operationID}/cancel",
		c.jsonEndpoint(scenario.EndpointEpayCancel, c.handleCancel))

	mux.HandleFunc("POST /epay/api/operation/{operationID}/refund",
		c.jsonEndpoint(scenario.EndpointEpayRefund, c.handleRefund))
	mux.HandleFunc("POST /api/operation/{operationID}/refund",
		c.jsonEndpoint(scenario.EndpointEpayRefund, c.handleRefund))

	// Status-check (reconciler PG: "потеряли ответ — проверь, в каком состоянии операция").
	// Метод GET — без тела; обёртка та же.
	mux.HandleFunc("GET /epay/check-status/payment/transactionId/{operationID}",
		c.jsonEndpoint(scenario.EndpointEpayStatus, c.handleStatus))
	mux.HandleFunc("GET /check-status/payment/transactionId/{operationID}",
		c.jsonEndpoint(scenario.EndpointEpayStatus, c.handleStatus))
}

// jsonHandler — обработчик, возвращает (статус-код, ответ, ошибка).
// Если ошибка != nil, она транслируется в 400 + {message}.
type jsonHandler func(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error)

// jsonEndpoint — обёртка для JSON-endpoint-ов: парсинг body, проверка bearer,
// сопоставление сценария, вызов handler-а, применение after-сценариев.
func (c *Controller) jsonEndpoint(endpoint string, fn jsonHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		entry := &requestlog.Entry{
			Method:   r.Method,
			URL:      r.URL.Path,
			Endpoint: endpoint,
			Bank:     domainbank.Epay,
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		entry.RequestBody = requestlog.Truncate(string(bodyBytes), 4000)

		// Bearer-проверка: в моке принимаем любой непустой token.
		// scenario invalid_signature можно использовать для имитации 401.
		bearer := infraepay.BearerFromHeader(r.Header.Get("Authorization"))
		entry.SignatureOK = bearer != ""

		if c.cfg.GlobalDelaySeconds > 0 {
			time.Sleep(time.Duration(c.cfg.GlobalDelaySeconds) * time.Second)
		}

		sc := c.scenarios.Match(scenario.MatchInput{
			Bank:      domainbank.Epay,
			Endpoint:  endpoint,
			PaymentID: entry.PaymentID,
			OrderID:   entry.OrderID,
		})
		if sc != nil {
			entry.ScenarioHit = sc.ID
			entry.ScenarioName = string(sc.Action)
			if scenarioapply.Transport(w, sc, entry, started, c.log) {
				return
			}
			if applied := c.applyScenarioBefore(w, sc, entry, started); applied {
				return
			}
		}

		statusCode, response, err := fn(r, bodyBytes, sc, entry)
		if err != nil {
			c.respondError(w, entry, started, http.StatusBadRequest, err.Error())
			return
		}
		if sc != nil {
			response = applyScenarioAfter(sc, response)
		}
		c.respondJSON(w, entry, started, statusCode, response)
	}
}

func (c *Controller) respondJSON(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, code int, body any) {
	buf, err := json.Marshal(body)
	if err != nil {
		c.respondError(w, entry, started, http.StatusInternalServerError, "marshal: "+err.Error())
		return
	}
	entry.StatusCode = code
	entry.ResponseBody = requestlog.Truncate(string(buf), 4000)
	entry.DurationMS = time.Since(started).Milliseconds()
	c.log.Add(entry)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(buf)
}

func (c *Controller) respondError(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, code int, msg string) {
	c.respondJSON(w, entry, started, code, infraepay.ErrorResponse{Message: msg})
}

// operationIDFromPath парсит {operationID} из URL.
// Возвращает (paymentID, raw-string-id). Если URL содержит "mock-epay-{N}" —
// извлекает paymentID; иначе ищем по полю EpayID через resolveOperation.
func operationIDFromPath(r *http.Request) (uint, string) {
	raw := r.PathValue("operationID")
	if raw == "" {
		return 0, ""
	}
	const prefix = "mock-epay-"
	if len(raw) > len(prefix) && raw[:len(prefix)] == prefix {
		if n, err := strconv.ParseUint(raw[len(prefix):], 10, 64); err == nil {
			return uint(n), raw
		}
	}
	return 0, raw
}

// resolveOperation возвращает PaymentID для запроса по {operationID} в URL.
// Сначала пробует mock-формат, потом ищет по полю EpayID в репозитории.
func (c *Controller) resolveOperation(r *http.Request) (uint, bool) {
	pid, raw := operationIDFromPath(r)
	if pid != 0 {
		return pid, true
	}
	if raw == "" {
		return 0, false
	}
	for _, rec := range c.svc.Repo().List() {
		if rec.EpayID == raw {
			return rec.PaymentID, true
		}
	}
	return 0, false
}
