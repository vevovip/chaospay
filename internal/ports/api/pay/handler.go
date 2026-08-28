// Package pay содержит HTTP-handlers Freedom Pay (XML form + MD5 signature).
package pay

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/freedompay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

const (
	merchantParamsField = "merchant_params"
	terminalIDParam     = "terminal_id"
	cabinetIDParam      = "cabinet_id"
)

// XMLHandler возвращает поля ответа либо ошибку.
type xmlHandler func(req *freedompay.ParsedRequest, sc *scenario.Scenario) (freedompay.OrdMap, error)

// Controller — HTTP-контроллер карточных операций.
type Controller struct {
	svc       *apppay.Service
	scenarios *appscenario.Service
	log       *memstore.RequestLog
	cfg       Config
}

// Config — настройки контроллера.
type Config struct {
	// Secret — ключ кабинета по умолчанию: им подписаны запросы без pg_merchant_id
	// и кабинетов, которых нет в Secrets.
	Secret string
	// Secrets — ключи кабинетов: merchant_id → secret. PG может ходить в несколько
	// кабинетов одного банка, и подпись каждой команды проверяется ключом своего.
	Secrets            map[uint]string
	DefaultTerminalID  int
	HostedFormURL      string
	GlobalDelaySeconds int
}

// secretFor возвращает ключ кабинета из pg_merchant_id запроса.
func (c Config) secretFor(merchantID string) string {
	if merchantID == "" {
		return c.Secret
	}

	id, err := strconv.ParseUint(merchantID, 10, 64)
	if err != nil {
		return c.Secret
	}

	if secret, ok := c.Secrets[uint(id)]; ok {
		return secret
	}

	log.Printf("[PAY] кабинет %s не заведен в моке, пробуем ключ по умолчанию", merchantID)

	return c.Secret
}

// NewController конструктор.
func NewController(svc *apppay.Service, scenarios *appscenario.Service, log *memstore.RequestLog, cfg Config) *Controller {
	return &Controller{svc: svc, scenarios: scenarios, log: log, cfg: cfg}
}

// Register регистрирует все Freedom Pay XML routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/merchant/{merchantID}/card/init",
		c.xmlEndpoint("init", "init", c.handleHoldInit))
	mux.HandleFunc("POST /v1/merchant/{merchantID}/card/direct",
		c.xmlEndpoint("direct", "direct", c.handleHold))

	mux.HandleFunc("POST /get_status3.php",
		c.xmlEndpoint("get_status3.php", "get_status3.php", c.handleStatus))
	mux.HandleFunc("POST /v2/get_status3",
		c.xmlEndpoint("get_status3.php", "get_status3.php", c.handleStatus))
	mux.HandleFunc("POST /customer/get_status3.php",
		c.xmlEndpoint("get_status3.php", "get_status3.php", c.handleStatus))

	mux.HandleFunc("POST /do_capture.php",
		c.xmlEndpoint("do_capture.php", "do_capture.php", c.handleCapture))
	mux.HandleFunc("POST /v2/do_capture",
		c.xmlEndpoint("do_capture.php", "do_capture.php", c.handleCapture))

	mux.HandleFunc("POST /cancel.php",
		c.xmlEndpoint("cancel.php", "cancel.php", c.handleCancel))
	mux.HandleFunc("POST /v2/cancel",
		c.xmlEndpoint("cancel.php", "cancel.php", c.handleCancel))

	mux.HandleFunc("POST /revoke.php",
		c.xmlEndpoint("revoke.php", "revoke.php", c.handleRevoke))
	mux.HandleFunc("POST /v2/revoke3",
		c.xmlEndpoint("revoke.php", "revoke.php", c.handleRevoke))

	mux.HandleFunc("POST /init_payment.php",
		c.xmlEndpoint("init_payment.php", "init_payment.php", c.handleInitPayment))
	mux.HandleFunc("POST /v2/init_payment",
		c.xmlEndpoint("init_payment.php", "init_payment.php", c.handleInitPayment))

	mux.HandleFunc("POST /v1/merchant/{merchantID}/cardstorage/add2",
		c.xmlEndpoint("add2", "add2", c.handleAddCard))
	mux.HandleFunc("POST /v1/merchant/{merchantID}/cardstorage/remove",
		c.xmlEndpoint("remove", "remove", c.handleRemoveCard))
}

// xmlEndpoint — обёртка: парсинг pg_xml, верификация подписи, scenario match, вызов handler-а, подпись ответа.
//
//	endpoint           — короткий ключ (init/direct/...) — для scenarios match и журнала.
//	responseScriptName — scriptName для подписи ответа (см. *Response.GetScriptName() PG SDK).
func (c *Controller) xmlEndpoint(endpoint, responseScriptName string, fn xmlHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		entry := &requestlog.Entry{Method: r.Method, URL: r.URL.Path, Endpoint: endpoint, Bank: bank.Freedom}

		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		if err := r.ParseForm(); err != nil {
			c.respondError(w, entry, started, http.StatusBadRequest, "parse form: "+err.Error())
			return
		}

		xmlStr := r.PostFormValue("pg_xml")
		entry.RequestBody = requestlog.Truncate(xmlStr, 4000)
		if xmlStr == "" {
			xmlStr = string(bodyBytes)
			entry.RequestBody = requestlog.Truncate(xmlStr, 4000)
		}

		req, err := freedompay.ParseRequestXML(xmlStr)
		if err != nil {
			c.respondError(w, entry, started, http.StatusBadRequest, "invalid xml: "+err.Error())
			return
		}

		entry.PaymentID = req.Get("pg_payment_id", "")
		entry.OrderID = req.Get("pg_order_id", "")
		entry.MerchantID = req.Get("pg_merchant_id", "")
		secret := c.cfg.secretFor(entry.MerchantID)

		if c.cfg.GlobalDelaySeconds > 0 {
			time.Sleep(time.Duration(c.cfg.GlobalDelaySeconds) * time.Second)
		}

		// Подпись: scriptName входящего запроса = endpoint.
		incomingSig := req.Get("pg_sig", "")
		expected, sigOK := freedompay.Verify(endpoint, req.Fields, secret, incomingSig)
		entry.SignatureOK = sigOK
		if !sigOK {
			log.Printf("[PAY %s] invalid signature: merchant=%s got=%s expected=%s fields=%v",
				endpoint, entry.MerchantID, incomingSig, expected, req.Fields)
			c.respondFailure(w, entry, started, responseScriptName, secret, "2000", "invalid signature: got "+incomingSig)
			return
		}

		// Scenario match
		sc := c.scenarios.Match(scenario.MatchInput{
			Bank:       bank.Freedom,
			Endpoint:   endpoint,
			PaymentID:  entry.PaymentID,
			OrderID:    entry.OrderID,
			MerchantID: entry.MerchantID,
		})
		if sc != nil {
			entry.ScenarioHit = sc.ID
			entry.ScenarioName = string(sc.Action)
			if applied := c.applyScenarioBefore(w, sc, req, responseScriptName, secret, entry, started); applied {
				return
			}
		}

		// Вызов handler-а
		fields, fnErr := fn(req, sc)
		if fnErr != nil {
			c.respondFailure(w, entry, started, responseScriptName, secret, "100", fnErr.Error())
			return
		}

		if sc != nil {
			fields = applyScenarioAfter(sc, fields)
		}

		body := signedXML("response", responseScriptName, fields, secret)

		if sc != nil && sc.Action == scenario.ActionInvalidSignature {
			body = freedompay.ReplaceTagValue(body, "pg_sig", "00000000000000000000000000000000")
		}
		// missing_field применяется через applyScenarioAfter ДО signedXML, поэтому поля,
		// добавляемые подписью (pg_sig/pg_salt), сами не удалятся. Делаем post-pass.
		if sc != nil && sc.Action == scenario.ActionMissingField {
			if field := scenario.Param(sc, "field", ""); field != "" {
				body = freedompay.RemoveTag(body, field)
			}
		}

		entry.StatusCode = http.StatusOK
		entry.ResponseBody = requestlog.Truncate(body, 4000)
		entry.DurationMS = time.Since(started).Milliseconds()
		c.log.Add(entry)

		writeXML(w, body)
	}
}

// signedXML добавляет pg_salt + pg_sig и рендерит.
func signedXML(rootName, responseScriptName string, fields freedompay.OrdMap, secret string) string {
	salt := freedompay.GenerateSalt(freedompay.SaltLength)
	fields = fields.Set("pg_salt", salt)
	sig := freedompay.Sign(responseScriptName, fields, secret)
	fields = fields.Set("pg_sig", sig)
	return freedompay.RenderResponse(rootName, fields)
}

// writeXML отдаёт XML-ответ.
func writeXML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

func (c *Controller) respondError(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, code int, msg string) {
	entry.StatusCode = code
	entry.ResponseBody = msg
	entry.DurationMS = time.Since(started).Milliseconds()
	c.log.Add(entry)
	http.Error(w, msg, code)
}

func (c *Controller) respondFailure(w http.ResponseWriter, entry *requestlog.Entry, started time.Time, responseScriptName, secret, errCode, errDesc string) {
	fields := freedompay.OrdMap{}
	fields = fields.Set("pg_status", "error")
	if errCode != "" {
		fields = fields.Set("pg_error_code", errCode)
	}
	if errDesc != "" {
		fields = fields.Set("pg_error_description", errDesc)
	}
	body := signedXML("response", responseScriptName, fields, secret)
	entry.StatusCode = http.StatusOK
	entry.ResponseBody = requestlog.Truncate(body, 4000)
	entry.DurationMS = time.Since(started).Milliseconds()
	c.log.Add(entry)
	writeXML(w, body)
}

// uintFromReq — uint из строкового поля.
func uintFromReq(req *freedompay.ParsedRequest, key string, def uint) uint {
	v := req.Get(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return def
	}
	return uint(n)
}

// extractTerminalID читает terminal_id из merchant_params/верхнего поля.
func extractTerminalID(req *freedompay.ParsedRequest, defaultID int) int {
	return merchantParamInt(req, terminalIDParam, defaultID)
}

// extractCabinetID читает cabinet_id из merchant_params. Ноль означает, что вызывающая
// сторона еще не проставляет кабинет — тогда в постлинк его не кладем.
func extractCabinetID(req *freedompay.ParsedRequest) int {
	return merchantParamInt(req, cabinetIDParam, 0)
}

// merchantParamInt читает числовой параметр из merchant_params либо из поля верхнего уровня.
func merchantParamInt(req *freedompay.ParsedRequest, key string, defaultValue int) int {
	if v, ok := req.Fields.Get(merchantParamsField); ok {
		if mp, ok := v.(freedompay.OrdMap); ok {
			if raw, ok := mp.Get(key); ok {
				if s, ok := raw.(string); ok {
					if n, err := strconv.Atoi(s); err == nil {
						return n
					}
				}
			}
		}
	}

	if raw := req.Get(key, ""); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
	}

	return defaultValue
}
