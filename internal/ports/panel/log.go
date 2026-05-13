package panel

import (
	"fmt"
	"net/http"
)

func (c *Controller) renderLogTab(w http.ResponseWriter) {
	entries := c.log.List()

	fmt.Fprintf(w, `<div class="section-header">
<div class="section-title">
<h2>Request Log</h2>
<p>Последние %d запросов к моку. Нажми на строку, чтобы раскрыть request/response без ухода со страницы.</p>
</div>
<div class="toolbar">
<form method="POST" action="/panel/log/reset">
<button class="btn btn-ghost" type="submit" onclick="return confirm('Очистить журнал запросов?')">Reset log</button>
</form>
</div>
</div>`, len(entries))

	if len(entries) == 0 {
		fmt.Fprint(w, `<div class="empty">Журнал пуст. Сделай хотя бы один запрос на мок (или PG-flow), и записи появятся здесь.</div>`)
		return
	}

	fmt.Fprint(w, `<div class="table-wrap"><table>
<tr><th>ID</th><th>Time</th><th>Method</th><th>URL</th><th>Endpoint</th><th>PayID</th><th>Sig</th><th>Scenario</th><th>HTTP</th><th>Δms</th></tr>`)
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
<td><span class="%s">%s</span></td>
<td>%s</td>
<td><span class="%s">%d</span></td>
<td>%s</td>
</tr>
<tr id="log-detail-%d" class="detail" style="display:none;"><td colspan="10">
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
