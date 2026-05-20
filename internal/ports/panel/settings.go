package panel

import (
	"fmt"
	"net/http"

	"github.com/vevovip/chaospay/internal/config"
)

func (c *Controller) renderSettingsTab(w http.ResponseWriter) {
	paymentCount := len(c.pay.Repo().List())
	qrCount := len(c.qr.Repo().List())
	scenarioCount := len(c.scenarios.List())
	logCount := len(c.log.List())
	autoWebhookText := "Manual"
	autoWebhookHint := "Webhook отправляется кнопкой на Cards/QR. Это безопаснее для ручной проверки."
	if c.cfg.AutoWebhook {
		autoWebhookText = "Automatic"
		autoWebhookHint = "Мок сам отправляет webhook при переходах статусов."
	}

	fmt.Fprintf(w, `<div class="section-header">
<div class="section-title">
<h2>Settings</h2>
<p>Сводка окружения, endpoints и terminal credentials. Если PG не видит мок, начинай проверку отсюда.</p>
</div>
<div class="toolbar">
<a class="btn btn-ghost" href="/health">Health</a>
<a class="btn btn-primary" href="/panel?tab=scenarios">Open Scenarios</a>
</div>
</div>

<div class="settings-grid">
<div class="status-card">
<strong>Runtime state</strong>
<div class="value">%d / %d / %d</div>
<div class="hint">card payments / QR codes / active scenarios. Log entries: %d.</div>
</div>
<div class="status-card">
<strong>Webhook mode</strong>
<div class="value">%s</div>
<div class="hint">%s</div>
</div>
<div class="status-card">
<strong>Global delay</strong>
<div class="value">%ds</div>
<div class="hint">Применяется ко всем mock responses и помогает проверять таймауты клиента.</div>
</div>
</div>

<div class="settings-grid">
<div class="panel-card">
<div class="section-title" style="margin-bottom:12px;">
<h2>Freedom Pay Terminal</h2>
<p>Должно совпадать с terminal config в payment-gateway.</p>
</div>
<div class="kv">
<div class="key">MOCK_FREEDOM_MERCHANT_ID</div><div class="val">%d</div>
<div class="key">MOCK_FREEDOM_TERMINAL_ID</div><div class="val">%d</div>
<div class="key">MOCK_FREEDOM_SECRET</div><div class="val">%s</div>
</div>
</div>

<div class="panel-card">
<div class="section-title" style="margin-bottom:12px;">
<h2>Panel URLs</h2>
<p>В контейнере сервис слушает <code>:8532</code>, наружу compose публикует <code>48532</code>.</p>
</div>
<div class="endpoint-list">
<div class="endpoint-row"><div class="key">External panel</div><div class="val">http://localhost:48532/panel</div></div>
<div class="endpoint-row"><div class="key">Container service</div><div class="val">http://chaospay:8532</div></div>
<div class="endpoint-row"><div class="key">Hosted redirect</div><div class="val">%s</div></div>
</div>
</div>
</div>

<div class="panel-card">
<div class="section-title" style="margin-bottom:12px;">
<h2>Halyk Epay v2 Terminal</h2>
<p>Должно совпадать с EPAY_2_* конфигом в payment-gateway.</p>
</div>
<div class="kv">
<div class="key">EPAY_2_CLIENT_ID</div><div class="val">%s</div>
<div class="key">EPAY_2_CLIENT_SECRET</div><div class="val">%s</div>
<div class="key">EPAY_2_TERMINAL_UUID</div><div class="val">%s</div>
<div class="key">EPAY auto-webhook</div><div class="val">%v</div>
</div>
</div>

<div class="panel-card">
<div class="section-title" style="margin-bottom:12px;">
<h2>Flitt Terminal</h2>
<p>Должно совпадать с FLITT_* конфигом в payment-gateway. SDK хардкодит <code>pay.flitt.com</code>; используй <code>FLITT_API_URL=http://chaospay:8532</code> для подмены на мок.</p>
</div>
<div class="kv">
<div class="key">FLITT_MERCHANT_ID</div><div class="val">%d</div>
<div class="key">FLITT_SECRET</div><div class="val">%s</div>
<div class="key">FLITT auto-webhook</div><div class="val">%v</div>
</div>
</div>

<div class="panel-card">
<div class="section-title" style="margin-bottom:12px;">
<h2>Webhook Targets</h2>
<p>Эти URL должны быть доступны из Docker network <code>dockernet-local</code>, а не только с хоста.</p>
</div>
<div class="endpoint-list">
<div class="endpoint-row"><div class="key">Freedom: pay</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Freedom: card-bind</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">QR webhook</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Epay: postlink</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Epay: failure_postlink</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Epay: bind postlink</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Flitt: callback</div><div class="val">%s</div></div>
<div class="endpoint-row"><div class="key">Flitt: bind callback</div><div class="val">%s</div></div>
</div>
</div>

<div class="panel-card">
<div class="section-title">
<h2>Quick Checks</h2>
<p>Минимальный порядок проверки, когда локальный PG-flow ведёт себя не так, как ожидается.</p>
</div>
<ol>
<li>Открой <a href="/panel?tab=log">Request Log</a> и проверь, приходит ли запрос от PG в мок.</li>
<li>Если запроса нет, сверяй <code>FREEDOM_PAY_HOST=http://chaospay:8532</code> и Docker network.</li>
<li>Если запрос есть, но подпись красная, сверяй merchant secret и terminal config.</li>
<li>Если статус изменился, но PG не обновился, проверь webhook URL и режим отправки webhook.</li>
</ol>
</div>

<div class="section-header" style="margin-top:24px;">
<div class="section-title">
<h2>EX-1001 Playback</h2>
<p>Короткий операторский сценарий для проверки восстановления после ambiguous Hold.</p>
</div>
</div>
<div class="panel-card">
<ol>
<li>Перейди в <a href="/panel?tab=scenarios">Scenarios</a> и нажми <b>EX-1001</b>. Будут добавлены два consume-once сценария: на следующий <code>direct</code> — ambiguous error, на следующий <code>get_status3.php</code> — force success.</li>
<li>Триггерни оплату saved-card в PG (rahmet-app или curl-ом order/authorize).</li>
<li>Поток: PG <code>init</code> → мок создаёт PaymentRecord (NEW) → PG <code>direct</code> → срабатывает сценарий 1, ответ ambiguous, состояние записи остаётся NEW → PG ловит ambiguous-marker, <code>ReconcilingClient</code> идёт в <code>get_status3.php</code> → срабатывает сценарий 2, <code>pg_payment_status=success</code> → транзакция в Authorized.</li>
<li>В логах PG: <code>reconciliation: recovered payment from false fail</code>.</li>
</ol>
<p>Альтернативно: повторный <code>direct</code> на уже Authorized платёж автоматически возвращает <code>"Неверный статус платежа"</code> без необходимости сценария — это и есть автоматическая имитация retry-инцидента.</p>
</div>`,
		paymentCount, qrCount, scenarioCount, logCount,
		autoWebhookText, autoWebhookHint, c.cfg.GlobalDelaySeconds,
		c.cfg.MerchantID, c.cfg.TerminalID, config.MaskSecret(c.cfg.Secret),
		c.cfg.HostedFormURL,
		c.cfg.EpayClientID, config.MaskSecret(c.cfg.EpayClientSecret), c.cfg.EpayTerminalUUID, c.cfg.EpayAutoWebhook,
		c.cfg.FlittMerchantID, config.MaskSecret(c.cfg.FlittSecret), c.cfg.FlittAutoWebhook,
		c.cfg.PayWebhookURL, c.cfg.CardWebhookURL, c.cfg.QRWebhookURL,
		c.cfg.EpaySuccessWebhookURL, c.cfg.EpayFailureWebhookURL, c.cfg.EpayBindWebhookURL,
		c.cfg.FlittSuccessWebhookURL, c.cfg.FlittBindWebhookURL,
	)
}
