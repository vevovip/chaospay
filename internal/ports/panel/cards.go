package panel

import (
	"fmt"
	"net/http"
	"strconv"

	domainpay "github.com/vevovip/chaospay/internal/domain/pay"
)

func (c *Controller) renderCardsTab(w http.ResponseWriter) {
	records := c.pay.Repo().List()

	counts := map[domainpay.Status]int{}
	for _, r := range records {
		counts[r.Status]++
	}

	resetDisabled := ""
	if len(records) == 0 {
		resetDisabled = " disabled"
	}

	fmt.Fprintf(w, `<div class="section-header">
<div class="section-title">
<h2>Card Payments</h2>
<p>Управление saved-card, PayPage, Apple Pay, Google Pay и bind-card платежами.</p>
</div>
<div class="toolbar">
<form method="POST" action="/panel/cards/reset">
<button type="submit" class="btn btn-ghost"%s onclick="return confirm('Удалить все карточные платежи?')">Reset card payments</button>
</form>
</div>
</div>

<div class="panel-card">
<div class="kv">
<div class="key">Payment webhook</div><div class="val">%s</div>
<div class="key">Card-bind webhook</div><div class="val">%s</div>
<div class="key">Auto-webhook</div><div class="val">%v</div>
</div>
</div>`, resetDisabled, c.cfg.PayWebhookURL, c.cfg.CardWebhookURL, c.cfg.AutoWebhook)

	fmt.Fprintf(w, `<div class="stats"><span class="stat stat-total">Total: %d</span>`, len(records))
	for _, s := range []domainpay.Status{
		domainpay.StatusNew, domainpay.StatusHoldPending, domainpay.StatusAuthorized, domainpay.StatusCaptured,
		domainpay.StatusCancelled, domainpay.StatusRefunded, domainpay.StatusPartialRefunded, domainpay.StatusFailed,
	} {
		if cnt := counts[s]; cnt > 0 {
			fmt.Fprintf(w, `<span class="stat stat-%s">%s: %d</span>`, s, s, cnt)
		}
	}
	fmt.Fprint(w, `</div>`)

	if len(records) == 0 {
		fmt.Fprint(w, `<div class="empty">Карточных платежей пока нет. Запусти оплату из PG или создай тестовый платеж ниже.</div>`)
	} else {
		fmt.Fprint(w, `<div class="table-wrap"><table>
<tr>
<th>Payment ID</th><th>Order ID</th><th>Kind</th><th>Term</th><th>Status</th><th>Amount</th><th>Captured / Refunded</th><th>Card</th><th>Created / Auth</th><th>Next</th><th>Actions</th>
</tr>`)
		for _, rec := range records {
			renderCardRow(w, rec)
		}
		fmt.Fprint(w, `</table></div>`)
	}

	fmt.Fprint(w, `<div class="panel-card" style="margin-top:24px;">
<div class="section-header">
<div class="section-title">
<h2>Add Test Payment</h2>
<p>Быстро создать запись без похода через PG-flow, чтобы проверить webhooks и переходы статусов.</p>
</div>
</div>
<form method="POST" action="/panel/cards/action">
<input type="hidden" name="action" value="create_synthetic">
<div class="scenario-form" style="margin-bottom:0;">
<div class="row">
<label>Order ID<input type="number" name="order_id" value="" required></label>
<label>Amount (KZT)<input type="number" name="amount" value="1500" required></label>
<label>User ID<input type="number" name="user_id" value="1"></label>
<label>Status<select name="status">
<option value="NEW">NEW</option>
<option value="AUTHORIZED">AUTHORIZED</option>
<option value="CAPTURED">CAPTURED</option>
</select></label>
</div>
<button class="btn btn-primary" type="submit">Create test payment</button>
</div>
</form></div>`)
}

func renderCardRow(w http.ResponseWriter, rec *domainpay.Record) {
	fmt.Fprint(w, `<tr>`)
	fmt.Fprintf(w, `<td class="uuid-cell" onclick="copyText(this, '%d')">%d<span class="copy-hint">click to copy</span></td>`,
		rec.PaymentID, rec.PaymentID)
	fmt.Fprintf(w, `<td>%d</td>`, rec.OrderID)
	fmt.Fprintf(w, `<td><span class="badge badge-NEW">%s</span></td>`, rec.Kind)
	fmt.Fprintf(w, `<td>%d</td>`, rec.TerminalID)
	fmt.Fprintf(w, `<td><span class="badge badge-%s">%s</span></td>`, rec.Status, rec.Status)
	fmt.Fprintf(w, `<td><span class="money">%d %s</span></td>`, rec.Amount, rec.Currency)
	fmt.Fprintf(w, `<td><span class="nowrap">%d / %d</span></td>`, rec.Captured, rec.Refunded)
	fmt.Fprintf(w, `<td><span class="muted">%s</span><br>%s</td>`, rec.CardBrand, rec.CardPAN)

	authStr := "-"
	if !rec.AuthorizedAt.IsZero() {
		authStr = rec.AuthorizedAt.Format("15:04:05")
	}
	fmt.Fprintf(w, `<td>%s<br><span class="muted">%s</span></td>`, rec.CreatedAt.Format("15:04:05"), authStr)
	title, hint := cardNextStep(rec)
	fmt.Fprintf(w, `<td><div class="next-step"><strong>%s</strong>%s</div></td>`, title, hint)

	fmt.Fprint(w, `<td><div class="actions">`)
	pid := strconv.FormatUint(uint64(rec.PaymentID), 10)
	if rec.Kind == domainpay.KindBind {
		actionButton(w, pid, "send_card_webhook", "btn-purple", "Send Card-Bind Webhook")
	} else {
		switch rec.Status {
		case domainpay.StatusNew, domainpay.StatusHoldPending:
			actionButton(w, pid, "force_authorized", "btn-primary", "Authorize")
			actionButton(w, pid, "force_failed", "btn-danger", "Fail")
		case domainpay.StatusAuthorized:
			actionButton(w, pid, "force_captured", "btn-success", "Capture")
			actionButton(w, pid, "force_cancelled", "btn-warning", "Cancel")
			actionButton(w, pid, "force_failed", "btn-danger", "Fail")
		case domainpay.StatusCaptured, domainpay.StatusPartialRefunded:
			actionButton(w, pid, "force_refunded", "btn-purple", "Refund")
		}
		webhookBtn := "Webhook"
		webhookCls := "btn-purple"
		if rec.WebhookSent {
			webhookBtn = "Re-send webhook"
			webhookCls = "btn-secondary"
		}
		fmt.Fprintf(w, `
<form method="POST" action="/panel/cards/webhook">
<input type="hidden" name="payment_id" value="%s">
<input type="hidden" name="result" value="ok">
<button type="submit" class="btn %s">%s</button>
</form>`, pid, webhookCls, webhookBtn)
	}
	fmt.Fprint(w, `</div></td>`)
	fmt.Fprint(w, `</tr>`)
}

func cardNextStep(rec *domainpay.Record) (string, string) {
	if rec.Kind == domainpay.KindBind {
		if rec.WebhookSent {
			return "Bind notified", "Card-bind webhook уже отправлен."
		}
		return "Send bind webhook", "Завершает flow привязки карты в PG."
	}
	switch rec.Status {
	case domainpay.StatusNew, domainpay.StatusHoldPending:
		return "Authorize or fail", "Выбери исход банковской авторизации."
	case domainpay.StatusAuthorized:
		return "Capture / cancel", "Capture списывает деньги, cancel отменяет hold."
	case domainpay.StatusCaptured:
		if rec.WebhookSent {
			return "Captured", "PG уже был уведомлён webhook-ом."
		}
		return "Send webhook", "Уведомь PG о финальном статусе."
	case domainpay.StatusCancelled, domainpay.StatusRefunded, domainpay.StatusFailed:
		if rec.WebhookSent {
			return "Done", "Финальный статус уже отправлен."
		}
		return "Send webhook", "Финальный статус ещё не отправлен в PG."
	case domainpay.StatusPartialRefunded:
		return "Refund or notify", "Можно завершить refund или отправить webhook."
	default:
		return "Review", "Проверь статус и историю запроса в Log."
	}
}

func actionButton(w http.ResponseWriter, paymentID, action, cls, label string) {
	fmt.Fprintf(w, `
<form method="POST" action="/panel/cards/action">
<input type="hidden" name="payment_id" value="%s">
<input type="hidden" name="action" value="%s">
<button type="submit" class="btn %s">%s</button>
</form>`, paymentID, action, cls, label)
}
