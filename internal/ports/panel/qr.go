package panel

import (
	"fmt"
	"math"
	"net/http"
	"time"

	domainqr "github.com/vevovip/chaospay/internal/domain/qr"
)

func (c *Controller) renderQRTab(w http.ResponseWriter) {
	codes := c.qr.Repo().List()

	counts := map[domainqr.Status]int{}
	for _, code := range codes {
		counts[code.Status]++
	}

	resetDisabled := ""
	if len(codes) == 0 {
		resetDisabled = " disabled"
	}

	fmt.Fprintf(w, `<div class="section-header">
<div class="section-title">
<h2>QR-PAY</h2>
<p>Состояния Single QR и refund QR. Нефинальные QR можно вручную перевести в нужный банковский статус.</p>
</div>
<div class="toolbar">
<form method="POST" action="/panel/qr/reset">
<button type="submit" class="btn btn-ghost"%s onclick="return confirm('Удалить все QR-коды?')">Reset QR</button>
</form>
</div>
</div>
<div class="panel-card"><div class="kv">
<div class="key">QR webhook</div><div class="val">%s</div>
<div class="key">TTL</div><div class="val">%s</div>
</div></div>
<div class="callout">Создать тестовый QR можно из PG-flow или напрямую: <code>curl -u test:test -X POST http://localhost:48532/qr-code/generate -H 'Content-Type: application/json' -d '{"beneficiary":{"bin":"123","tid":"T","mid":"M"},"payment":{"amount":1500,"dataType":"001"}}'</code></div>`, resetDisabled, c.cfg.QRWebhookURL, domainqr.TTL)
	fmt.Fprintf(w, `<div class="stats"><span class="stat stat-total">Total: %d</span>`, len(codes))
	for _, s := range domainqr.AllStatuses {
		if c := counts[s]; c > 0 {
			fmt.Fprintf(w, `<span class="stat stat-%s">%s: %d</span>`, s, s, c)
		}
	}
	fmt.Fprint(w, `</div>`)

	if len(codes) == 0 {
		fmt.Fprint(w, `<div class="empty">QR-кодов пока нет. Создай его через <code>POST /qr-code/generate</code>, и он появится здесь.</div>`)
		return
	}

	fmt.Fprint(w, `<div class="table-wrap"><table>
<tr><th>QR</th><th>UUID</th><th>Type</th><th>Status</th><th>TTL</th><th>Amount</th><th>BIN/TID/MID</th><th>Created</th><th>TrnID</th><th>Actions</th></tr>`)
	now := time.Now()
	for _, code := range codes {
		renderQRRow(w, code, now)
	}
	fmt.Fprint(w, `</table></div>`)
}

func renderQRRow(w http.ResponseWriter, code *domainqr.Code, now time.Time) {
	trnInfo := "-"
	if code.TrnID > 0 {
		trnInfo = fmt.Sprintf("%d", code.TrnID)
	}

	fmt.Fprint(w, `<tr><td>`)
	if code.QRBase64 != "" {
		fmt.Fprintf(w, `<img class="qr-img" src="data:image/png;base64,%s" alt="QR">`, code.QRBase64)
	} else {
		fmt.Fprint(w, `-`)
	}
	fmt.Fprint(w, `</td>`)

	fmt.Fprintf(w, `<td class="uuid-cell" onclick="copyText(this, '%s')">%s<span class="copy-hint">click to copy UUID</span>`, code.UUID, code.UUID)
	if code.PaymentURL != "" {
		fmt.Fprintf(w, `<div class="muted">url: <code>%s</code></div>`, escapeHTML(code.PaymentURL))
	}
	if code.IsRefund && code.RefundedParentTrnID != "" {
		fmt.Fprintf(w, `<div class="muted">refund of trnID <code>%s</code> • ref <code>%s</code></div>`, code.RefundedParentTrnID, code.RefundedReference)
	}
	fmt.Fprint(w, `</td>`)

	typeStr := "PAY"
	if code.IsRefund {
		typeStr = "REFUND"
	}
	fmt.Fprintf(w, `<td><span class="badge badge-NEW">%s</span></td>`, typeStr)
	fmt.Fprintf(w, `<td><span class="badge badge-%s">%s</span></td>`, code.Status, code.Status)

	fmt.Fprint(w, `<td>`)
	renderQRTTL(w, code, now)
	fmt.Fprint(w, `</td>`)

	fmt.Fprintf(w, `<td><span class="money">%.2f</span></td><td>%s / %s / %s</td><td>%s</td><td>%s</td><td>`,
		code.Amount, code.BIN, code.TID, code.MID, code.CreatedAt.Format("15:04:05"), trnInfo)

	fmt.Fprint(w, `<div class="actions">`)
	if !code.Status.IsTerminal() {
		if code.Status == domainqr.StatusNew {
			qrActionButton(w, code.UUID, "SCANNED", "btn-primary", "Scanned")
		}
		qrActionButton(w, code.UUID, "SUCCESS", "btn-success", "Success")
		qrActionButton(w, code.UUID, "EXPIRED", "btn-warning", "Expired")
		qrActionButton(w, code.UUID, "CANCELLED", "btn-danger", "Cancelled")
		qrActionButton(w, code.UUID, "ERROR", "btn-danger", "Error")
	} else {
		if code.WebhookSent {
			fmt.Fprint(w, `<button class="btn btn-secondary" disabled>Webhook Sent</button>`)
		} else {
			fmt.Fprintf(w, `<form method="POST" action="/qr-panel/webhook"><input type="hidden" name="uuid" value="%s"><button type="submit" class="btn btn-purple">Send Webhook</button></form>`, code.UUID)
		}
	}
	fmt.Fprint(w, `</div></td></tr>`)
}

func renderQRTTL(w http.ResponseWriter, code *domainqr.Code, now time.Time) {
	if code.Status.IsTerminal() {
		fmt.Fprint(w, `<span class="muted">-</span>`)
		return
	}
	elapsed := now.Sub(code.CreatedAt)
	remaining := domainqr.TTL - elapsed
	if remaining <= 0 {
		fmt.Fprint(w, `<span class="badge badge-FAILED">expired</span>`)
		return
	}
	secs := int(math.Ceil(remaining.Seconds()))
	mins := secs / 60
	secs = secs % 60
	fmt.Fprintf(w, `<span class="muted">%d:%02d</span>`, mins, secs)
}

func qrActionButton(w http.ResponseWriter, uuid, action, cls, label string) {
	fmt.Fprintf(w, `<form method="POST" action="/qr-panel/action"><input type="hidden" name="uuid" value="%s"><input type="hidden" name="action" value="%s"><button type="submit" class="btn %s">%s</button></form>`,
		uuid, action, cls, label)
}
