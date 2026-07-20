package panel

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	domainkaspi "github.com/vevovip/chaospay/internal/domain/kaspi"
)

// renderKaspiTab рендерит список Kaspi-платежей с кнопками confirm/decline.
func (c *Controller) renderKaspiTab(w http.ResponseWriter) {
	payments := c.kaspi.List()

	fmt.Fprintf(w, `<div class="stats"><span class="stat stat-total">Total: %d</span></div>`, len(payments))

	fmt.Fprint(w, `<p class="hint">KaspiPay — polling-мок. PG создаёт платёж через create-link (статус Wait) и опрашивает статус.
Нажми <b>Confirm</b>, чтобы имитировать оплату пользователем (Wait → Processed), или <b>Decline</b> (Wait → Error).</p>`)

	if len(payments) == 0 {
		fmt.Fprint(w, `<div class="empty">Пока нет Kaspi-платежей. Создай заказ Kaspi через PG (checkout).</div>`)
		return
	}

	fmt.Fprint(w, `<div class="table-wrap"><table>
<thead><tr><th>PaymentId</th><th>ExternalId (orderId)</th><th>Amount</th><th>Status</th><th>Created</th><th>Actions</th></tr></thead><tbody>`)

	for _, p := range payments {
		fmt.Fprint(w, `<tr>`)
		fmt.Fprintf(w, `<td><code>%d</code></td>`, p.PaymentID)
		fmt.Fprintf(w, `<td>%s</td>`, p.ExternalID)
		fmt.Fprintf(w, `<td>%.2f</td>`, p.Amount)
		fmt.Fprintf(w, `<td><span class="badge badge-%s">%s</span></td>`, kaspiBadge(p.Status), p.Status)
		fmt.Fprintf(w, `<td>%s</td>`, p.CreatedAt.Format("2006-01-02 15:04:05"))

		fmt.Fprint(w, `<td><div class="actions">`)
		if !p.Status.IsTerminal() {
			kaspiActionButton(w, p.PaymentID, string(domainkaspi.StatusProcessed), "btn-success", "Confirm")
			kaspiActionButton(w, p.PaymentID, string(domainkaspi.StatusError), "btn-danger", "Decline")
		} else {
			fmt.Fprint(w, `<span class="muted">—</span>`)
		}
		fmt.Fprint(w, `</div></td>`)
		fmt.Fprint(w, `</tr>`)
	}

	fmt.Fprint(w, `</tbody></table></div>`)
}

// kaspiBadge маппит Kaspi-статус на CSS-класс бейджа (переиспользуем классы карт).
func kaspiBadge(s domainkaspi.Status) string {
	switch s {
	case domainkaspi.StatusProcessed:
		return "AUTHORIZED"
	case domainkaspi.StatusError:
		return "FAILED"
	default:
		return "NEW"
	}
}

func kaspiActionButton(w http.ResponseWriter, paymentID int, action, cls, label string) {
	fmt.Fprintf(w, `<form method="POST" action="/panel/kaspi/action" style="display:inline">
<input type="hidden" name="payment_id" value="%d">
<input type="hidden" name="action" value="%s">
<button type="submit" class="btn %s">%s</button>
</form>`, paymentID, action, cls, label)
}

// handleKaspiAction переводит платёж в Processed/Error по кнопке из панели.
func (c *Controller) handleKaspiAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	paymentID, errID := strconv.Atoi(r.FormValue("payment_id"))
	action := r.FormValue("action")
	if errID != nil || action == "" {
		http.Error(w, "Missing payment_id or action", http.StatusBadRequest)
		return
	}

	if _, err := c.kaspi.SetStatus(paymentID, domainkaspi.Status(action)); err != nil {
		log.Printf("[panel-kaspi] failed to update %d -> %s: %v", paymentID, action, err)
	} else {
		log.Printf("[panel-kaspi] updated %d -> %s", paymentID, action)
	}

	http.Redirect(w, r, kaspiRedirectURL(), http.StatusSeeOther)
}

func kaspiRedirectURL() string {
	return "/panel?bank=kaspi&tab=kaspi"
}
