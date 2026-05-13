package panel

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"

	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/domain/scenario"
)

func (c *Controller) renderScenariosTab(w http.ResponseWriter) {
	scs := c.scenarios.List()
	totalHits := 0
	onceCount := 0
	for _, sc := range scs {
		totalHits += sc.HitCount
		if sc.ConsumeOnce {
			onceCount++
		}
	}
	persistentCount := len(scs) - onceCount

	fmt.Fprint(w, `<div class="section-header">
<div class="section-title">
<h2>Scenarios</h2>
<p>Очередь правил, которые меняют следующий подходящий ответ банка. Верхний сценарий срабатывает первым.</p>
</div>
<div class="toolbar">
<form method="POST" action="/panel/scenarios/reset">
<button class="btn btn-ghost" type="submit" onclick="return confirm('Удалить все активные сценарии?')">Reset scenarios</button>
</form>
</div>
</div>

<div class="scenario-hero">
<div class="scenario-hero-main">
<strong>Быстрый сценарный стенд</strong>
<span>Выбери пресет, повтори нужный платёжный шаг в PG, затем проверь Card Payments и Request Log.</span>
</div>
<div class="scenario-metrics">
`)
	fmt.Fprintf(w, `<div class="scenario-metric"><span>Active</span><strong>%d</strong></div>`, len(scs))
	fmt.Fprintf(w, `<div class="scenario-metric"><span>One-shot</span><strong>%d</strong></div>`, onceCount)
	fmt.Fprintf(w, `<div class="scenario-metric"><span>Persistent</span><strong>%d</strong></div>`, persistentCount)
	fmt.Fprintf(w, `<div class="scenario-metric"><span>Hits</span><strong>%d</strong></div>`, totalHits)
	fmt.Fprint(w, `</div>
</div>

<div class="scenario-flow">
<div><strong>1. Arm</strong><span>Добавь preset или custom rule.</span></div>
<div><strong>2. Trigger</strong><span>Повтори запрос: Hold, Capture, Status, QR или Wallet.</span></div>
<div><strong>3. Inspect</strong><span>Смотри активные правила, платежи и request log.</span></div>
</div>

<div class="scenario-presets">`)
	for _, group := range []string{"Incidents", "Retry and timeouts", "Business declines", "Broken responses", "Data integrity"} {
		fmt.Fprintf(w, `<section class="preset-card"><div class="preset-card-head"><h3>%s</h3><span>%d</span></div>`, group, presetGroupCount(group))
		renderPresetGroup(w, group)
		fmt.Fprint(w, `</section>`)
	}
	fmt.Fprint(w, `</div>

<details class="panel-card advanced-card">
<summary>
<div class="section-title">
<h2>Custom Scenario</h2>
<p>Используй, когда пресета недостаточно: задай endpoint, matchers и параметры action-а.</p>
</div>
<span class="summary-action">Open form</span>
</summary>

<form method="POST" action="/panel/scenarios/add" class="scenario-form">
<div class="row">
<label>Endpoint
<select name="endpoint">
`)
	for _, ep := range scenario.AllEndpoints {
		fmt.Fprintf(w, `<option value="%s">%s</option>`, ep, ep)
	}
	fmt.Fprint(w, `</select></label>
<label>PaymentID matcher<input type="text" name="payment_id" value="*" placeholder="* или конкретный"></label>
<label>OrderID matcher<input type="text" name="order_id" value="*" placeholder="* или конкретный"></label>
<label>MerchantID matcher<input type="text" name="merchant_id" value="*" placeholder="* или конкретный"></label>
</div>
<div class="row">
<label>Action
<select name="action">
`)
	for _, a := range scenario.AllActions {
		fmt.Fprintf(w, `<option value="%s">%s</option>`, a, a)
	}
	fmt.Fprint(w, `</select></label>
<label>seconds<input type="number" name="seconds" placeholder="для timeout/delay"></label>
<label>http_status<input type="number" name="http_status" placeholder="500/502/..."></label>
<label>error_code<input type="text" name="error_code" placeholder="120"></label>
</div>
<div class="row">
<label>message<input type="text" name="message" placeholder="error description / failure_description"></label>
<label>payment_status<select name="payment_status">
<option value="">(force_status only)</option>
<option value="success">success</option>
<option value="process">process</option>
<option value="failed">failed</option>
<option value="new">new</option>
<option value="revoked">revoked</option>
</select></label>
<label>amount<input type="text" name="amount" placeholder="partial_amount only"></label>
<label>ConsumeOnce<select name="consume_once">
<option value="true" selected>true</option>
<option value="false">false</option>
</select></label>
</div>
<div class="row">
<label>field<input type="text" name="field" placeholder="missing_field: pg_sig / pg_payment_id ..."></label>
<label>body<input type="text" name="body" placeholder="malformed_body: тело ответа"></label>
<label>content_type<input type="text" name="content_type" placeholder="malformed_body: application/json"></label>
<label>chunk_delay_ms<input type="number" name="chunk_delay_ms" placeholder="slow_body: задержка между байтами"></label>
<label>count<input type="number" name="count" placeholder="extra_garbage: сколько полей"></label>
<label>payment_id<input type="text" name="payment_id_param" placeholder="wrong_payment_id: новое значение"></label>
</div>
<button type="submit" class="btn btn-primary">Add custom scenario</button>
</form>
</details>

<div class="panel-card scenario-note">
<div class="section-title">
<h2>Desync flow</h2>
<p>Пресет <code>Desync</code> отвечает фатальной ошибкой на <code>direct</code>, PG-транзакция уходит в <code>failed</code>. После триггера открой Card Payments и нажми <b>Capture</b> на записи, чтобы получить расхождение <code>orders/pg = failed</code>, <code>bank = CAPTURED</code>.</p>
</div>
</div>

<div class="section-header" style="margin-top:24px;">
<div class="section-title">
<h2>Active Scenarios</h2>
<p>Если несколько правил подходят под запрос, сработает первое в списке.</p>
</div>
</div>`)

	if len(scs) == 0 {
		fmt.Fprint(w, `<div class="empty">Активных сценариев нет. Мок отвечает штатно. Добавь preset выше, чтобы воспроизвести банковский сбой.</div>`)
		return
	}

	fmt.Fprint(w, `<div class="table-wrap"><table>
<tr><th>Priority</th><th>Rule</th><th>Match</th><th>Params</th><th>Mode</th><th>Hits</th><th>Created</th><th></th></tr>`)
	for _, sc := range scs {
		params := formatScenarioParams(sc.Params)
		mode := "Persistent"
		if sc.ConsumeOnce {
			mode = "One-shot"
		}
		fmt.Fprintf(w, `<tr>
<td><code>%s</code></td>
<td><div class="scenario-rule"><span class="endpoint-pill">%s</span><span class="badge %s">%s</span></div></td>
<td><div class="match-stack"><span>payment <code>%s</code></span><span>order <code>%s</code></span><span>merchant <code>%s</code></span></div></td>
<td><span class="muted">%s</span></td>
<td>%s</td>
<td><strong>%d</strong></td>
<td class="nowrap">%s</td>
<td>
<form method="POST" action="/panel/scenarios/delete">
<input type="hidden" name="id" value="%s">
<button class="btn btn-danger" type="submit">Delete</button>
</form>
</td>
</tr>`,
			html.EscapeString(sc.ID),
			html.EscapeString(sc.Endpoint),
			scenarioActionClass(sc.Action),
			html.EscapeString(string(sc.Action)),
			html.EscapeString(sc.PaymentID),
			html.EscapeString(sc.OrderID),
			html.EscapeString(sc.MerchantID),
			params,
			mode,
			sc.HitCount,
			sc.CreatedAt.Format("15:04:05"),
			html.EscapeString(sc.ID),
		)
	}
	fmt.Fprint(w, `</table></div>`)
}

func renderPresetGroup(w http.ResponseWriter, group string) {
	empty := true
	for _, p := range appscenario.AllPresets {
		if scenarioPresetGroup(p.Name) != group {
			continue
		}
		empty = false
		btnClass := presetButtonClass(p.Name)
		fmt.Fprintf(w, `<div class="preset-option">
<p class="preset-description">%s</p>
<div class="preset-row">
<form method="POST" action="/panel/scenarios/preset">
<input type="hidden" name="preset" value="%s">
<button class="btn %s" type="submit" title="%s">%s</button>
</form>`, html.EscapeString(p.Description), html.EscapeString(p.Name), btnClass, html.EscapeString(p.Description), html.EscapeString(p.Title))
		if p.Sample != "" {
			fmt.Fprintf(w, `<details class="preset-details"><summary title="Что отдаст банк / увидит PG" aria-label="Что отдаст банк / увидит PG"><span>i</span></summary><pre>%s</pre></details>`, html.EscapeString(p.Sample))
		}
		fmt.Fprint(w, `</div></div>`)
	}
	if empty {
		fmt.Fprint(w, `<p class="muted">Нет пресетов.</p>`)
	}
}

func presetGroupCount(group string) int {
	count := 0
	for _, p := range appscenario.AllPresets {
		if scenarioPresetGroup(p.Name) == group {
			count++
		}
	}
	return count
}

func scenarioPresetGroup(name string) string {
	switch {
	case name == "ex1001" || name == "desync" || name == "hold_timeout":
		return "Incidents"
	case strings.Contains(name, "recovery") || strings.Contains(name, "failed_status"):
		// hold_pending_recovery, capture_failed_status_*, cancel_failed_status_*, revoke_failed_status_*
		return "Incidents"
	case strings.Contains(name, "retry") || strings.Contains(name, "deadline"):
		return "Retry and timeouts"
	case strings.Contains(name, "funds") || strings.Contains(name, "declined") || strings.Contains(name, "fraud") ||
		strings.Contains(name, "expired") || strings.Contains(name, "3ds") || strings.Contains(name, "limit"):
		return "Business declines"
	case strings.Contains(name, "malformed") || strings.Contains(name, "empty") || strings.Contains(name, "slow"):
		return "Broken responses"
	default:
		return "Data integrity"
	}
}

func scenarioActionClass(action scenario.Action) string {
	switch action {
	case scenario.ActionForceStatus:
		return "badge-SUCCESS"
	case scenario.ActionAmbiguousError, scenario.ActionHTTPError, scenario.ActionForceFailure, scenario.ActionConnectionReset, scenario.ActionEmptyResponse:
		return "badge-FAILED"
	case scenario.ActionTimeout, scenario.ActionDelay, scenario.ActionSlowBody:
		return "badge-CANCELLED"
	case scenario.ActionMalformedBody, scenario.ActionInvalidSignature, scenario.ActionWrongStatusCode, scenario.ActionWrongPaymentID, scenario.ActionWrongAmount, scenario.ActionMissingField, scenario.ActionExtraGarbage:
		return "badge-REFUNDED"
	default:
		return "badge-NEW"
	}
}

func formatScenarioParams(params map[string]string) string {
	if len(params) == 0 {
		return "—"
	}
	out := make([]string, 0, len(params))
	for k, v := range params {
		if v != "" {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return "—"
	}
	for i, item := range out {
		out[i] = html.EscapeString(item)
	}
	return strings.Join(out, ", ")
}

func presetButtonClass(name string) string {
	switch scenarioPresetGroup(name) {
	case "Retry and timeouts", "Broken responses":
		return "btn-warning"
	case "Business declines":
		return "btn-danger"
	case "Data integrity":
		return "btn-purple"
	default:
		if name == "desync" {
			return "btn-danger"
		}
		return "btn-primary"
	}
}
