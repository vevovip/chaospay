// Package flitt содержит HTTP-handlers для Flitt JSON API.
//
// Шаблон обработки повторяет epay/handler.go: parse → scenario.Match →
// applyBefore (transport+content) → handle → applyAfter → render JSON.
//
// Отличия от Epay:
//
//   - Подпись SHA1 (а не Bearer-токен).
//   - Все эндпоинты POST + JSON.
//   - Hosted-формы (checkout) — возвращают checkout_url, фактическое списание
//     произойдёт после кнопки в panel либо через прямой direct-вызов.
package flitt

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	domainbank "github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraflitt "github.com/vevovip/chaospay/internal/infrastructure/flitt"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// Config — настройки Flitt-контроллера.
type Config struct {
	// Secret — secret_key мерчанта. Используется для верификации подписи входящих
	// запросов. Мок принимает любую подпись (для проще тестирования), но если
	// указано — попадает в RequestLog (SignatureOK).
	Secret string
	// MerchantID — merchant_id мерчанта (для подстановки в ответы).
	MerchantID int
	// HostedFormURL — URL hosted-формы, который возвращается в checkout_url.
	HostedFormURL string
	// AutoWebhook — если true, мок сам шлёт callback после approved-сценариев.
	AutoWebhook bool
	// GlobalDelaySeconds — глобальная задержка перед каждым ответом (для дебага).
	GlobalDelaySeconds int
}

// Controller — HTTP-контроллер Flitt.
type Controller struct {
	svc       *apppay.Service
	scenarios *appscenario.Service
	log       *memstore.RequestLog
	webhook   *pgclient.FlittClient
	cfg       Config
}

// NewController конструктор.
func NewController(
	svc *apppay.Service,
	scenarios *appscenario.Service,
	log *memstore.RequestLog,
	webhook *pgclient.FlittClient,
	cfg Config,
) *Controller {
	return &Controller{
		svc:       svc,
		scenarios: scenarios,
		log:       log,
		webhook:   webhook,
		cfg:       cfg,
	}
}

// Register регистрирует все Flitt routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/checkout/url",
		c.jsonEndpoint(scenario.EndpointFlittCheckout, c.handleCheckout))
	mux.HandleFunc("POST /api/3dsecure_step1",
		c.jsonEndpoint(scenario.EndpointFlittDirect, c.handleDirect))
	mux.HandleFunc("POST /api/recurring",
		c.jsonEndpoint(scenario.EndpointFlittRecurring, c.handleRecurring))
	mux.HandleFunc("POST /api/capture/order_id",
		c.jsonEndpoint(scenario.EndpointFlittCapture, c.handleCapture))
	mux.HandleFunc("POST /api/reverse/order_id",
		c.jsonEndpoint(scenario.EndpointFlittReverse, c.handleReverse))
	mux.HandleFunc("POST /api/status/order_id",
		c.jsonEndpoint(scenario.EndpointFlittStatus, c.handleStatus))
	mux.HandleFunc("POST /api/3dsecure_step2",
		c.jsonEndpoint(scenario.EndpointFlittStep2, c.handle3DSStep2))
}

// jsonHandler — обработчик, возвращает (статус-код, ответ, ошибка).
type jsonHandler func(r *http.Request, body []byte, sc *scenario.Scenario, entry *requestlog.Entry) (int, any, error)

// jsonEndpoint — обёртка для JSON-endpoint-ов: парсинг body, проверка подписи,
// сопоставление сценария, вызов handler-а, применение after-сценариев.
func (c *Controller) jsonEndpoint(endpoint string, fn jsonHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		entry := &requestlog.Entry{
			Method:   r.Method,
			URL:      r.URL.Path,
			Endpoint: endpoint,
			Bank:     domainbank.Flitt,
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		entry.RequestBody = requestlog.Truncate(string(bodyBytes), 4000)

		// orderID извлекаем из тела (используется matcher-ом сценариев).
		entry.OrderID = extractOrderID(bodyBytes)

		// Подпись принимаем любую (упрощает тестирование), но отмечаем в логе.
		entry.SignatureOK = extractSignature(bodyBytes) != ""

		if c.cfg.GlobalDelaySeconds > 0 {
			time.Sleep(time.Duration(c.cfg.GlobalDelaySeconds) * time.Second)
		}

		sc := c.scenarios.Match(scenario.MatchInput{
			Bank:     domainbank.Flitt,
			Endpoint: endpoint,
			OrderID:  entry.OrderID,
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
			c.respondFailure(w, entry, started, http.StatusOK, 1000, err.Error())
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
		c.respondFailure(w, entry, started, http.StatusInternalServerError, 1000, "marshal: "+err.Error())
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

func (c *Controller) respondFailure(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, httpCode, errCode int, msg string) {
	c.respondJSON(w, entry, started, httpCode, infraflitt.NewFailure(errCode, msg))
}

// extractOrderID парсит order_id из {"request": {"order_id": "..."}}.
// Возвращает пустую строку, если тело не похоже на формат Flitt.
func extractOrderID(body []byte) string {
	var w struct {
		Request struct {
			OrderID string `json:"order_id"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return ""
	}
	return strings.TrimSpace(w.Request.OrderID)
}

// extractSignature парсит signature из {"request": {"signature": "..."}}.
func extractSignature(body []byte) string {
	var w struct {
		Request struct {
			Signature string `json:"signature"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		return ""
	}
	return strings.TrimSpace(w.Request.Signature)
}
