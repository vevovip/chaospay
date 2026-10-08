package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
)

const (
	logQueryBank      = "bank"
	logQueryPaymentID = "payment_id"
	logQueryOrderID   = "order_id"
	logQueryEndpoint  = "endpoint"
)

// logEntryJSON — запись журнала для автотестов: кто и в какой кабинет обращался, без тел запросов.
type logEntryJSON struct {
	ID          uint64    `json:"id"`
	At          time.Time `json:"at"`
	Bank        bank.Bank `json:"bank"`
	Method      string    `json:"method"`
	Endpoint    string    `json:"endpoint"`
	PaymentID   string    `json:"payment_id"`
	OrderID     string    `json:"order_id"`
	MerchantID  string    `json:"merchant_id"`
	SignatureOK bool      `json:"signature_ok"`
	StatusCode  int       `json:"status_code"`
}

// entriesByBank — записи журнала банка, новые первыми. Банк старых записей определяется по URL.
func (c *Controller) entriesByBank(b bank.Bank) []*requestlog.Entry {
	all := c.log.List()
	entries := make([]*requestlog.Entry, 0, len(all))
	for _, e := range all {
		eb := e.Bank
		if eb == bank.Any {
			eb = bank.FromPath(e.URL)
		}
		if b == bank.Any || eb == b {
			entries = append(entries, e)
		}
	}

	return entries
}

// handleLogJSON отдаёт журнал запросов JSON-ом с фильтрами bank, payment_id, order_id, endpoint.
func (c *Controller) handleLogJSON(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filters := map[string]func(*requestlog.Entry) string{
		logQueryPaymentID: func(e *requestlog.Entry) string { return e.PaymentID },
		logQueryOrderID:   func(e *requestlog.Entry) string { return e.OrderID },
		logQueryEndpoint:  func(e *requestlog.Entry) string { return e.Endpoint },
	}

	out := make([]logEntryJSON, 0)
	for _, e := range c.entriesByBank(parseBank(query.Get(logQueryBank))) {
		if !matchesFilters(e, query, filters) {
			continue
		}

		out = append(out, logEntryJSON{
			ID:          e.ID,
			At:          e.At,
			Bank:        e.Bank,
			Method:      e.Method,
			Endpoint:    e.Endpoint,
			PaymentID:   e.PaymentID,
			OrderID:     e.OrderID,
			MerchantID:  e.MerchantID,
			SignatureOK: e.SignatureOK,
			StatusCode:  e.StatusCode,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func matchesFilters(e *requestlog.Entry, query map[string][]string, filters map[string]func(*requestlog.Entry) string) bool {
	for name, field := range filters {
		values := query[name]
		if len(values) == 0 || values[0] == "" {
			continue
		}
		if field(e) != values[0] {
			return false
		}
	}

	return true
}

func (c *Controller) renderLogTab(w http.ResponseWriter, b bank.Bank) {
	entries := c.entriesByBank(b)

	bankTitle := bank.Titles[b]
	if bankTitle == "" {
		bankTitle = "All banks"
	}

	fmt.Fprintf(w, `<div class="section-header">
<div class="section-title">
<h2>%s — Request Log</h2>
<p>Последние %d запросов к моку. Нажми на строку, чтобы раскрыть request/response без ухода со страницы.</p>
</div>
<div class="toolbar">
<form method="POST" action="/panel/log/reset">
<input type="hidden" name="bank" value="%s">
<button class="btn btn-ghost" type="submit" onclick="return confirm('Очистить журнал запросов?')">Reset log</button>
</form>
</div>
</div>`, bankTitle, len(entries), b)

	if len(entries) == 0 {
		fmt.Fprint(w, `<div class="empty">Журнал пуст. Сделай хотя бы один запрос на мок (или PG-flow), и записи появятся здесь.</div>`)
		return
	}

	fmt.Fprint(w, `<div class="table-wrap"><table>
<tr><th>ID</th><th>Time</th><th>Method</th><th>URL</th><th>Endpoint</th><th>PayID</th><th>Merchant</th><th>Sig</th><th>Scenario</th><th>HTTP</th><th>Δms</th></tr>`)
	for _, e := range entries {
		sigCls := "signature-bad"
		sigText := "✗"
		if e.SignatureOK {
			sigCls = "signature-ok"
			sigText = "✓"
		}
		scenarioStr := "-"
		if e.ScenarioHit != "" {
			scenarioStr = fmt.Sprintf("%s (%s)", e.ScenarioHit, e.ScenarioName)
		}
		merchantStr := e.MerchantID
		if merchantStr == "" {
			merchantStr = "-"
		}
		httpCls := logHTTPClass(e.StatusCode)
		durationText := fmt.Sprintf("%d", e.DurationMS)
		if e.DurationMS >= 1000 {
			durationText = fmt.Sprintf("%d slow", e.DurationMS)
		}
		fmt.Fprintf(w, `<tr onclick="toggleRow(%d)" style="cursor:pointer;">
<td>%d</td>
<td>%s</td>
<td>%s</td>
<td><code>%s</code></td>
<td>%s</td>
<td>%s</td>
<td>%s</td>
<td><span class="%s">%s</span></td>
<td>%s</td>
<td><span class="%s">%d</span></td>
<td>%s</td>
</tr>
<tr id="log-detail-%d" class="detail" style="display:none;"><td colspan="11">
<div class="kv">
<div class="key">Full page</div><div class="val"><a href="/panel/log/%d">Open request #%d</a></div>
<div class="key">Request body</div><div class="val"><pre class="body">%s</pre></div>
<div class="key">Response body</div><div class="val"><pre class="body">%s</pre></div>
</div>
</td></tr>`,
			e.ID,
			e.ID,
			e.At.Format("15:04:05.000"),
			e.Method,
			e.URL,
			e.Endpoint,
			e.PaymentID,
			merchantStr,
			sigCls, sigText,
			scenarioStr,
			httpCls,
			e.StatusCode,
			durationText,
			e.ID,
			e.ID,
			e.ID,
			escapeHTML(e.RequestBody),
			escapeHTML(e.ResponseBody),
		)
	}
	fmt.Fprint(w, `</table></div>`)
}

func logHTTPClass(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "status-http-ok"
	case status >= 300 && status < 500:
		return "status-http-warn"
	default:
		return "status-http-bad"
	}
}
