// Package panel — HTML панель управления моком (5 вкладок).
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
	if tab == "" {
		tab = "cards"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.renderHeader(w, tab)
	switch tab {
	case "qr":
		c.renderQRTab(w)
	case "cards":
		c.renderCardsTab(w)
	case "scenarios":
		c.renderScenariosTab(w)
	case "log":
		c.renderLogTab(w)
	case "settings":
		c.renderSettingsTab(w)
	default:
		fmt.Fprintf(w, `<div class="empty">Unknown tab: %s</div>`, tab)
	}
	c.renderFooter(w)
}

func (c *Controller) handleQRPanelAlias(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/panel?tab=qr", http.StatusSeeOther)
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
	http.Redirect(w, r, "/panel?tab=cards", http.StatusSeeOther)
}

func (c *Controller) createSynthetic(r *http.Request) {
	orderID, _ := strconv.ParseUint(r.FormValue("order_id"), 10, 64)
	amount, _ := strconv.ParseUint(r.FormValue("amount"), 10, 64)
	userID, _ := strconv.ParseUint(r.FormValue("user_id"), 10, 64)
	status := domainpay.Status(r.FormValue("status"))
	if status == "" {
		status = domainpay.StatusNew
	}
	rec, _ := c.pay.HoldInit(apppay.HoldInitInput{
		OrderID:    uint(orderID),
		MerchantID: c.cfg.MerchantID,
		TerminalID: c.cfg.TerminalID,
		UserID:     uint(userID),
		Amount:     uint(amount),
		CardToken:  fmt.Sprintf("synthetic-token-%d", orderID),
	})
	if rec == nil {
		return
	}
	if status != domainpay.StatusNew {
		_, _ = c.pay.ApplyForce(rec.PaymentID, status)
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
	if errSend := c.pay.SendWebhook(uint(paymentID), success); errSend != nil {
		log.Printf("[panel] send webhook failed: %v", errSend)
	}
	http.Redirect(w, r, "/panel?tab=cards", http.StatusSeeOther)
}

func (c *Controller) handleCardsReset(w http.ResponseWriter, r *http.Request) {
	c.pay.Repo().Reset()
	http.Redirect(w, r, "/panel?tab=cards", http.StatusSeeOther)
}

// ----- Scenario actions -----

func (c *Controller) handleScenarioAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	consume, _ := strconv.ParseBool(r.FormValue("consume_once"))
	sc := &domainscenario.Scenario{
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
	http.Redirect(w, r, "/panel?tab=scenarios", http.StatusSeeOther)
}

func (c *Controller) handleScenarioDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	c.scenarios.Remove(r.FormValue("id"))
	http.Redirect(w, r, "/panel?tab=scenarios", http.StatusSeeOther)
}

func (c *Controller) handleScenarioReset(w http.ResponseWriter, r *http.Request) {
	c.scenarios.Reset()
	http.Redirect(w, r, "/panel?tab=scenarios", http.StatusSeeOther)
}

func (c *Controller) handleScenarioPreset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderError(w, err.Error())
		return
	}
	c.scenarios.ApplyPreset(r.FormValue("preset"))
	http.Redirect(w, r, "/panel?tab=scenarios", http.StatusSeeOther)
}

// ----- Log actions -----

func (c *Controller) handleLogReset(w http.ResponseWriter, r *http.Request) {
	c.log.Reset()
	http.Redirect(w, r, "/panel?tab=log", http.StatusSeeOther)
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
<a class="btn btn-ghost" href="/panel?tab=log">Back to log</a>
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
	http.Redirect(w, r, "/panel?tab=qr", http.StatusSeeOther)
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
	http.Redirect(w, r, "/panel?tab=qr", http.StatusSeeOther)
}

func (c *Controller) handleQRReset(w http.ResponseWriter, r *http.Request) {
	c.qr.Repo().Reset()
	http.Redirect(w, r, "/panel?tab=qr", http.StatusSeeOther)
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
