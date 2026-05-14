// Package panel — HTML панель управления моком.
//
// URL-схема:
//
//	/panel?bank=<freedom|epay|qr|loyalty>&tab=<cards|scenarios|log|qr|loyalty>
//	/panel?tab=settings
//
// Без параметров — редирект на bank=freedom&tab=cards.
package panel

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appqr "github.com/vevovip/chaospay/internal/application/qr"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/config"
	"github.com/vevovip/chaospay/internal/domain/bank"
	domainpay "github.com/vevovip/chaospay/internal/domain/pay"
	domainqr "github.com/vevovip/chaospay/internal/domain/qr"
	domainscenario "github.com/vevovip/chaospay/internal/domain/scenario"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
)

// Controller — HTTP-контроллер панели.
type Controller struct {
	pay       *apppay.Service
	qr        *appqr.Service
	scenarios *appscenario.Service
	log       *memstore.RequestLog
	cfg       config.Config
}

// NewController конструктор.
func NewController(pay *apppay.Service, qr *appqr.Service, scenarios *appscenario.Service, log *memstore.RequestLog, cfg config.Config) *Controller {
	return &Controller{pay: pay, qr: qr, scenarios: scenarios, log: log, cfg: cfg}
}

// Register регистрирует все panel routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /panel", c.handlePanel)
	mux.HandleFunc("GET /qr-panel", c.handleQRPanelAlias)

	// Cards actions
	mux.HandleFunc("POST /panel/cards/action", c.handleCardsAction)
	mux.HandleFunc("POST /panel/cards/webhook", c.handleCardsWebhook)
	mux.HandleFunc("POST /panel/cards/reset", c.handleCardsReset)

	// Scenarios actions
	mux.HandleFunc("POST /panel/scenarios/add", c.handleScenarioAdd)
	mux.HandleFunc("POST /panel/scenarios/delete", c.handleScenarioDelete)
	mux.HandleFunc("POST /panel/scenarios/preset", c.handleScenarioPreset)
	mux.HandleFunc("POST /panel/scenarios/reset", c.handleScenarioReset)

	// Log actions
	mux.HandleFunc("POST /panel/log/reset", c.handleLogReset)
	mux.HandleFunc("GET /panel/log/{id}", c.handleLogDetail)

	// QR actions (legacy /qr-panel/* paths сохранены)
	mux.HandleFunc("POST /qr-panel/action", c.handleQRAction)
	mux.HandleFunc("POST /qr-panel/webhook", c.handleQRWebhook)
	mux.HandleFunc("POST /panel/qr/reset", c.handleQRReset)
}

func (c *Controller) handlePanel(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	b := parseBank(r.URL.Query().Get("bank"))

	// Settings — глобальная вкладка, не относится к конкретному банку.
	if tab == "settings" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		c.renderHeader(w, bank.Any, "settings")
		c.renderSettingsTab(w)
		c.renderFooter(w)
		return
	}

	// Без bank — редирект на default.
	if b == bank.Any {
		http.Redirect(w, r, "/panel?bank=freedom&tab=cards", http.StatusSeeOther)
		return
	}
	if tab == "" {
		tab = defaultTabFor(b)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.renderHeader(w, b, tab)
	switch {
	case tab == "qr" && b == bank.QR:
		c.renderQRTab(w)
	case tab == "loyalty" && b == bank.Loyalty:
		c.renderLoyaltyTab(w)
	case tab == "cards" && (b == bank.Freedom || b == bank.Epay):
		c.renderCardsTab(w, b)
	case tab == "scenarios":
		c.renderScenariosTab(w, b)
	case tab == "log":
		c.renderLogTab(w, b)
	default:
		fmt.Fprintf(w, `<div class="empty">Unknown bank/tab: %s/%s</div>`, b, tab)
	}
	c.renderFooter(w)
}

// parseBank нормализует ?bank= параметр. Неизвестное значение → bank.Any.
func parseBank(s string) bank.Bank {
	b := bank.Bank(s)
	if !bank.Valid(b) {
		return bank.Any
	}
	return b
}

func (c *Controller) handleQRPanelAlias(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, qrRedirectURL(), http.StatusSeeOther)
}

// ----- Cards actions -----

func (c *Controller) handleCardsAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, "parse form: "+err.Error())
		return
	}
	action := r.FormValue("action")
	switch action {
	case "create_synthetic":
		c.createSynthetic(r)
	default:
		paymentID, err := strconv.ParseUint(r.FormValue("payment_id"), 10, 64)
		if err != nil {
			renderError(w, "invalid payment_id")
			return
		}
		c.applyCardAction(uint(paymentID), action)
	}
	http.Redirect(w, r, cardsRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) createSynthetic(r *http.Request) {
	orderID, _ := strconv.ParseUint(r.FormValue("order_id"), 10, 64)
	amount, _ := strconv.ParseUint(r.FormValue("amount"), 10, 64)
	userID, _ := strconv.ParseUint(r.FormValue("user_id"), 10, 64)
	status := domainpay.Status(r.FormValue("status"))
	if status == "" {
		status = domainpay.StatusNew
	}
	b := parseBank(r.FormValue("bank"))

	var pid uint
	switch b {
	case bank.Epay:
		// Минимальный Epay-record: Cryptopay через application/pay.
		rec, err := c.pay.EpayAuthorize(apppay.EpayAuthorizeInput{
			OrderID:       uint(orderID),
			Amount:        int(amount), //nolint:gosec
			Currency:      "KZT",
			InvoiceID:     fmt.Sprintf("%06d", orderID),
			AccountID:     fmt.Sprintf("synthetic-account-%d", userID),
			TerminalID:    c.cfg.EpayTerminalUUID,
			ClientID:      c.cfg.EpayClientID,
			HasCryptogram: true,
		})
		if err != nil {
			log.Printf("[panel] epay synthetic failed: %v", err)
			return
		}
		pid = rec.PaymentID
	default:
		// Freedom (default).
		rec, err := c.pay.HoldInit(apppay.HoldInitInput{
			OrderID:    uint(orderID),
			MerchantID: c.cfg.MerchantID,
			TerminalID: c.cfg.TerminalID,
			UserID:     uint(userID),
			Amount:     uint(amount),
			CardToken:  fmt.Sprintf("synthetic-token-%d", orderID),
		})
		if err != nil || rec == nil {
			return
		}
		pid = rec.PaymentID
	}

	if status != domainpay.StatusNew {
		_, _ = c.pay.ApplyForce(pid, status)
	}
}

func (c *Controller) applyCardAction(paymentID uint, action string) {
	var target domainpay.Status
	switch action {
	case "force_authorized":
		target = domainpay.StatusAuthorized
	case "force_captured":
		target = domainpay.StatusCaptured
	case "force_cancelled":
		target = domainpay.StatusCancelled
	case "force_refunded":
		target = domainpay.StatusRefunded
	case "force_failed":
		target = domainpay.StatusFailed
	case "send_card_webhook":
		if err := c.pay.SendCardWebhook(paymentID); err != nil {
			log.Printf("[panel] send card webhook failed: %v", err)
		}
		return
	default:
		log.Printf("[panel] unknown action: %s", action)
		return
	}
	if _, err := c.pay.ApplyForce(paymentID, target); err != nil {
		log.Printf("[panel] force %s on payment %d failed: %v", target, paymentID, err)
	}
}

func (c *Controller) handleCardsWebhook(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	paymentID, err := strconv.ParseUint(r.FormValue("payment_id"), 10, 64)
	if err != nil {
		renderError(w, "invalid payment_id")
		return
	}
	success := !strings.EqualFold(r.FormValue("result"), "fail")
	b := parseBank(r.FormValue("bank"))

	switch b {
	case bank.Epay:
		// Для Halyk Epay используем отдельный webhook-сервис (см. epay.Controller),
		// но он доступен только через мок-handler-ы. Здесь поддерживаем явные кнопки
		// из Cards-tab — отправляем soft postlink в PG-targets из config.
		if errSend := c.sendEpayWebhook(uint(paymentID), success, r.FormValue("variant")); errSend != nil {
			log.Printf("[panel] send epay webhook failed: %v", errSend)
		}
	default:
		if errSend := c.pay.SendWebhook(uint(paymentID), success); errSend != nil {
			log.Printf("[panel] send webhook failed: %v", errSend)
		}
	}
	http.Redirect(w, r, cardsRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) handleCardsReset(w http.ResponseWriter, r *http.Request) {
	c.pay.Repo().Reset()
	http.Redirect(w, r, cardsRedirectURL(r), http.StatusSeeOther)
}

// cardsRedirectURL возвращает URL вкладки cards с сохранением ?bank=.
func cardsRedirectURL(r *http.Request) string {
	b := r.FormValue("bank")
	if b == "" {
		b = "freedom"
	}
	return "/panel?bank=" + b + "&tab=cards"
}

// scenariosRedirectURL — то же, для scenarios tab.
func scenariosRedirectURL(r *http.Request) string {
	b := r.FormValue("bank")
	if b == "" {
		b = "freedom"
	}
	return "/panel?bank=" + b + "&tab=scenarios"
}

// logRedirectURL — то же, для log tab.
func logRedirectURL(r *http.Request) string {
	b := r.FormValue("bank")
	if b == "" {
		b = "freedom"
	}
	return "/panel?bank=" + b + "&tab=log"
}

// qrRedirectURL — для qr tab (всегда bank=qr).
func qrRedirectURL() string {
	return "/panel?bank=qr&tab=qr"
}

// sendEpayWebhook отправляет нужный вариант postlink для Halyk-платежа.
//
// variant принимает:
//   - "" / "success" / "fail"           — постлинк операции (postlink / failure_postlink);
//   - "bind-success" / "bind-fail"      — bind-postlink;
//   - "lost-order" / "missing-fields"   — успешный postlink с модифицированным телом
//     (см. application/pay/service.go::SendEpayPostlink).
func (c *Controller) sendEpayWebhook(paymentID uint, success bool, variant string) error {
	switch variant {
	case "bind-success":
		return c.pay.SendEpayBindPostlink(paymentID, true)
	case "bind-fail":
		return c.pay.SendEpayBindPostlink(paymentID, false)
	}
	return c.pay.SendEpayPostlink(paymentID, success, variant)
}

// renderLoyaltyTab — заглушка вкладки Loyalty (используется только для request log).
func (c *Controller) renderLoyaltyTab(w http.ResponseWriter) {
	fmt.Fprint(w, `<div class="section-header"><div class="section-title">
<h2>Loyalty</h2>
<p>Мок-обработчики <code>/authservice/api/auth/v1/security/getToken</code> и <code>/loyaltyservice/loyalty/frhcCompanyTransaction</code>.
Логика — фиксированный cashback от настроек (см. Settings). Управляется только через сценарии и Request Log.</p>
</div></div>
<div class="callout">Перейди в <a href="/panel?bank=loyalty&tab=log">Request Log</a> чтобы увидеть входящие запросы loyalty или в <a href="/panel?tab=settings">Settings</a> чтобы изменить процент кэшбека.</div>`)
}

// ----- Scenario actions -----

func (c *Controller) handleScenarioAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	consume, _ := strconv.ParseBool(r.FormValue("consume_once"))
	sc := &domainscenario.Scenario{
		Bank:        parseBank(r.FormValue("bank")),
		Endpoint:    valueOr(r.FormValue("endpoint"), domainscenario.Wildcard),
		PaymentID:   valueOr(r.FormValue("payment_id"), domainscenario.Wildcard),
		OrderID:     valueOr(r.FormValue("order_id"), domainscenario.Wildcard),
		MerchantID:  valueOr(r.FormValue("merchant_id"), domainscenario.Wildcard),
		Action:      domainscenario.Action(r.FormValue("action")),
		ConsumeOnce: consume,
		Params:      map[string]string{},
	}
	for _, k := range []string{"seconds", "http_status", "error_code", "message", "payment_status", "amount", "field", "body", "content_type", "chunk_delay_ms", "count"} {
		if v := r.FormValue(k); v != "" {
			sc.Params[k] = v
		}
	}
	// payment_id_param — отдельное имя в форме, чтобы не конфликтовать с matcher "payment_id".
	if v := r.FormValue("payment_id_param"); v != "" {
		sc.Params["payment_id"] = v
	}
	c.scenarios.Add(sc)
	http.Redirect(w, r, scenariosRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) handleScenarioDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	c.scenarios.Remove(r.FormValue("id"))
	http.Redirect(w, r, scenariosRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) handleScenarioReset(w http.ResponseWriter, r *http.Request) {
	c.scenarios.Reset()
	http.Redirect(w, r, scenariosRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) handleScenarioPreset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	c.scenarios.ApplyPreset(r.FormValue("preset"))
	http.Redirect(w, r, scenariosRedirectURL(r), http.StatusSeeOther)
}

// ----- Log actions -----

func (c *Controller) handleLogReset(w http.ResponseWriter, r *http.Request) {
	c.log.Reset()
	http.Redirect(w, r, logRedirectURL(r), http.StatusSeeOther)
}

func (c *Controller) handleLogDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	e, ok := c.log.Get(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Request #%d</title><style>%s</style></head><body><main>
<div class="section-header">
<div class="section-title"><h2>Request #%d</h2><p>Полные тела запроса и ответа.</p></div>
<a class="btn btn-ghost" href="javascript:history.back()">Back to log</a>
</div>
<div class="panel-card"><h2>Request</h2><pre class="body">%s</pre></div>
<div class="panel-card"><h2>Response</h2><pre class="body">%s</pre></div>
</main></body></html>`,
		e.ID, panelCSS,
		e.ID, escapeHTML(e.RequestBody), escapeHTML(e.ResponseBody))
}

// ----- QR actions (legacy) -----

func (c *Controller) handleQRAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	uuid := r.FormValue("uuid")
	action := r.FormValue("action")
	if uuid == "" || action == "" {
		http.Error(w, "Missing uuid or action", http.StatusBadRequest)
		return
	}
	if _, err := c.qr.ChangeStatus(uuid, domainqr.Status(action)); err != nil {
		log.Printf("[panel-qr] failed to update %s -> %s: %v", uuid, action, err)
	} else {
		log.Printf("[panel-qr] updated %s -> %s", uuid, action)
	}
	http.Redirect(w, r, qrRedirectURL(), http.StatusSeeOther)
}

func (c *Controller) handleQRWebhook(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	uuid := r.FormValue("uuid")
	if uuid == "" {
		http.Error(w, "Missing uuid", http.StatusBadRequest)
		return
	}
	if _, err := c.qr.SendWebhook(uuid); err != nil {
		log.Printf("[panel-qr] cannot send webhook for %s: %v", uuid, err)
	}
	http.Redirect(w, r, qrRedirectURL(), http.StatusSeeOther)
}

func (c *Controller) handleQRReset(w http.ResponseWriter, r *http.Request) {
	c.qr.Repo().Reset()
	http.Redirect(w, r, qrRedirectURL(), http.StatusSeeOther)
}

// ----- helpers -----

func valueOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func renderError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html><body><pre>%s</pre><a href="/panel">← back</a></body></html>`, escapeHTML(msg))
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;")
	return r.Replace(s)
}
