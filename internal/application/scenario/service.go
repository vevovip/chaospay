// Package scenario — сервис над хранилищем сценариев.
package scenario

import (
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/scenario"
)

// Store — контракт хранилища.
type Store interface {
	Add(sc *scenario.Scenario)
	Remove(id string)
	Reset()
	List() []*scenario.Scenario
	Match(in scenario.MatchInput) *scenario.Scenario
}

// Service оркестрирует сценарии.
type Service struct {
	store Store
}

// NewService конструктор.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Store возвращает store (для panel).
func (s *Service) Store() Store { return s.store }

// Add — добавить сценарий.
func (s *Service) Add(sc *scenario.Scenario) { s.store.Add(sc) }

// Remove — удалить.
func (s *Service) Remove(id string) { s.store.Remove(id) }

// Reset — очистить все.
func (s *Service) Reset() { s.store.Reset() }

// List — все активные.
func (s *Service) List() []*scenario.Scenario { return s.store.List() }

// Match — найти первое совпадение и удалить, если ConsumeOnce.
func (s *Service) Match(in scenario.MatchInput) *scenario.Scenario { return s.store.Match(in) }

// PresetInfo — описание одного preset-а для UI.
type PresetInfo struct {
	Name        string
	Bank        bank.Bank // к какому банку относится preset (для фильтра в UI).
	Title       string
	Description string
	// Sample — пример ответа банка в этом сценарии. Раскрывается в UI через <details>,
	// чтобы можно было сразу понять, что увидит PG и его клиенты.
	// Формат свободный: XML для Freedom Pay, JSON для wallet, текст лога для transport-уровневых.
	Sample string
}

// PresetsFor возвращает пресеты, относящиеся к выбранному банку.
// bank.Any → все пресеты.
func PresetsFor(b bank.Bank) []PresetInfo {
	if b == bank.Any {
		return AllPresets
	}
	out := make([]PresetInfo, 0, len(AllPresets))
	for _, p := range AllPresets {
		if p.Bank == b || p.Bank == bank.Any {
			out = append(out, p)
		}
	}
	return out
}

// AllPresets — список всех доступных preset-ов для рендера в panel.
// Бизнес-пресеты используют РЕАЛЬНЫЕ Freedom error codes из PG-маппинга
// ([internal/infrastructure/clients/payments/freedom/error_mapping.go]).
// Каждый код триггерит конкретную domain-ошибку PG (common.Err*).
var AllPresets = []PresetInfo{
	{
		Name: "ex1001", Bank: bank.Freedom, Title: "⚡ EX-1001",
		Description: "Ambiguous Hold → recovery success на next get_status3",
		Sample: `# Шаг 1: на direct мок отдаёт ambiguous-ошибку (Hold)
<response>
  <pg_status>error</pg_status>
  <pg_error_code>120</pg_error_code>
  <pg_failure_description>Неверный статус платежа</pg_failure_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Шаг 2: PG ReconcilingClient идёт в get_status3 → мок отдаёт success
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>success</pg_payment_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# PG-лог: "reconciliation: recovered payment from false fail"`,
	},
	{
		Name: "ex1001_silent_hold", Bank: bank.Freedom, Title: "🕳 EX-1001 silent hold",
		Description: "direct отвечает ошибкой, но банк параллельно холдирует деньги и шлёт success-webhook (pg_result=1)",
		Sample: `# Production-кейс EX-1001: банк синхронно сказал «ошибка»,
# но в реальности захолдировал платёж и асинхронно подтверждает через webhook.

# Шаг 1: direct → синхронно ошибка
<response>
  <pg_status>error</pg_status>
  <pg_error_code>120</pg_error_code>
  <pg_error_description>Неверный статус платежа</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Шаг 2 (асинхронно, ~миллисекунды позже):
# chaospay переводит запись в Authorized и шлёт webhook на PG:
#   POST /api/v1/payment-gateway/webhook/freedompay
#   pg_result=1, pg_payment_id=..., pg_amount=...

# PG-поведение сейчас (до фикса):
#   1) direct error → MarkAsFailed → AMQP order.failed
#   2) webhook success → Finalizer.MarkAsAuthorized → ErrOrderWrongStatus (из Failed нельзя)
#   Итог: у банка hold, у нас Failed.`,
	},
	{
		Name: "ex1001_wallet_silent_hold", Bank: bank.Freedom, Title: "🕳 EX-1001 wallet silent hold",
		Description: "applepay/googlepay отвечают ошибкой, но банк параллельно холдирует деньги и шлёт success-webhook",
		Sample: `# Аналог EX-1001, но для Apple/Google Pay (endpoint /pay/{id}/pay).
# Wallet синхронно отдаёт JSON-ошибку, мок параллельно холдирует и шлёт webhook.

# Шаг 1: /pay/{id}/pay → JSON-ошибка
{"data":{"status":"error","message":"Неверный статус платежа"}}

# Шаг 2 (асинхронно):
#   POST /api/v1/payment-gateway/webhook/freedompay
#   pg_result=1, pg_payment_id=..., pg_amount=...

# Ожидаемое поведение PG:
#   1) wallet error → MarkAsFailed (фраза "Неверный статус платежа" — ambiguous-маркер)
#   2) webhook success на Failed-заказе → Resolver → cancel у PSP
#   3) Заказ переходит в AutoRefunded, deньги отменены`,
	},
	{
		Name: "hold_pending_recovery", Bank: bank.Freedom, Title: "🔄 Hold pending → recovery",
		Description: "Hold вернул pg_payment_status=process → PG авто-Status → success (без ambiguous-marker)",
		Sample: `# Шаг 1: direct отдаёт ok-ответ, но pg_payment_status=process (pending)
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>process</pg_payment_status>   ← pending-индикатор
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# PG в merchant.go:251 видит pending → автоматически вызывает Status (без ReconcilingClient).

# Шаг 2: get_status3.php отдаёт success
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>success</pg_payment_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Транзакция уходит в Authorized.
# Отличие от EX-1001: тут НЕ ambiguous-error, а штатный pending → recovery flow.`,
	},
	{
		Name: "capture_failed_status_approved", Bank: bank.Freedom, Title: "💰 Capture failed → Status approved",
		Description: "do_capture.php отвечает ошибкой, но Status подтверждает: 'банк всё-таки списал'",
		Sample: `# Production-кейс: 'деньги списались, но PG думает Capture не прошёл'.
# PG в merchant.go:411-421 при ошибке Capture сам проверяет Status — если success, принимает.

# Шаг 1: do_capture.php → ошибка
<response>
  <pg_status>error</pg_status>
  <pg_error_code>100</pg_error_code>
  <pg_error_description>technical bank error</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Шаг 2: get_status3.php → success (банк подтвердил списание)
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>success</pg_payment_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Результат: PG принимает Capture, транзакция → Captured.`,
	},
	{
		Name: "cancel_failed_status_revoked", Bank: bank.Freedom, Title: "🚫 Cancel failed → Status revoked",
		Description: "cancel.php отвечает ошибкой, но Status: 'отмена прошла на банке'",
		Sample: `# PG в merchant.go:457-469 при ошибке Cancel проверяет Status.

# Шаг 1: cancel.php → ошибка
<response>
  <pg_status>error</pg_status>
  <pg_error_code>100</pg_error_code>
  <pg_error_description>technical bank error</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Шаг 2: get_status3.php → revoked
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>revoked</pg_payment_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Результат: PG принимает Cancel.`,
	},
	{
		Name: "revoke_failed_status_revoked", Bank: bank.Freedom, Title: "↩ Revoke failed → Status revoked",
		Description: "revoke.php (refund) отвечает ошибкой, но Status: 'возврат на банке прошёл'",
		Sample: `# PG в merchant.go:520-532 при ошибке Revoke проверяет Status.

# Шаг 1: revoke.php → ошибка
<response>
  <pg_status>error</pg_status>
  <pg_error_code>100</pg_error_code>
  <pg_error_description>technical bank error</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Шаг 2: get_status3.php → revoked (refund прошёл)
<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>revoked</pg_payment_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Результат: PG принимает Refund.`,
	},
	{
		Name: "hold_timeout", Bank: bank.Freedom, Title: "Hold Timeout",
		Description: "Таймаут на direct (20s)",
		Sample: `# Соединение зависает на 20 секунд, потом мок закрывает TCP без ответа.
# PG увидит:
Post "https://api.freedompay.kz/v1/merchant/.../card/direct": net/http: request canceled
(Client.Timeout exceeded while awaiting headers)`,
	},
	{
		Name: "desync", Bank: bank.Freedom, Title: "Desync",
		Description: "Fatal failure на direct без ambiguous-marker",
		Sample: `# Мок отдаёт фатальную ошибку (не подпадает под ambiguous-маркеры PG).
# PG-транзакция уходит в Failed, recovery не срабатывает.
<response>
  <pg_status>error</pg_status>
  <pg_error_code>100</pg_error_code>
  <pg_error_description>технический сбой банка</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# После этого вручную нажать Force Captured в Card Payments — банк якобы списал.
# Получишь рассинхрон: orders/pg=failed, bank=CAPTURED.`,
	},

	{
		Name: "init_retry_exhausted", Bank: bank.Freedom, Title: "🌐 Init retry-exhausted",
		Description: "3× timeout на init_payment.php (PG: giving up after 3 attempt(s))",
		Sample: `# Каждая попытка PG: 15s ожидания → TCP close без ответа.
# После 3 retry hashicorp/go-retryablehttp сдаётся.
# PG-лог:
: Post "https://api.freedompay.kz/init_payment.php": POST https://api.freedompay.kz/init_payment.php
giving up after 3 attempt(s): Post "...": net/http: request canceled while waiting for connection
(Client.Timeout exceeded while awaiting headers)`,
	},
	{
		Name: "hold_init_retry_exhausted", Bank: bank.Freedom, Title: "🌐 HoldInit retry-exhausted",
		Description: "3× timeout на /v1/merchant/.../card/init",
		Sample: `# То же что init_retry_exhausted, но на эндпоинте создания Hold.
# PG-лог:
: не удалось сделать инициализацию платежа: Post "https://api.freedompay.kz/v1/merchant/554415/card/init":
POST https://api.freedompay.kz/v1/merchant/554415/card/init giving up after 3 attempt(s)`,
	},
	{
		Name: "wallet_retry_exhausted", Bank: bank.Freedom, Title: "🌐 Wallet retry-exhausted",
		Description: "3× timeout на applepay/googlepay",
		Sample: `# PG-клиент wallet (Apple/Google Pay) исчерпал retry.
# PG-лог:
: Post "https://customer.freedompay.kz/pay/019e0186-.../pay": POST https://customer.freedompay.kz/pay/.../pay
giving up after 3 attempt(s): Post "...": context deadline exceeded`,
	},
	{
		Name: "context_deadline", Bank: bank.Freedom, Title: "⏱ Context deadline",
		Description: "60s timeout — клиент PG отвалится по context.deadline",
		Sample: `# Мок засыпает на 60s — гарантированно дольше любого PG context.WithTimeout.
# PG-лог:
giving up after 3 attempt(s): Post "...": context deadline exceeded`,
	},

	// Бизнес-ошибки — коды соответствуют PG error_mapping.go (один-в-один с тем,
	// что real Freedom возвращает в проде). Каждый триггерит свой common.Err*.
	{
		Name: "insufficient_funds", Bank: bank.Freedom, Title: "💸 Insufficient funds",
		Description: "Freedom code=10009 → ErrNotEnoughMoney → 'недостаточно средств на карте'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10009</pg_error_code>
  <pg_error_description>Insufficient funds on card</pg_error_description>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Также триггерится кодами: 11006, 8888, 100091, 100094`,
	},
	{
		Name: "card_declined", Bank: bank.Freedom, Title: "🚫 Declined by issuer",
		Description: "Freedom code=10007 → ErrDeclinedByIssuer → 'оплата отклонена банком'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10007</pg_error_code>
  <pg_error_description>Declined by issuer</pg_error_description>
</response>

# Альтернативные коды той же категории: 10010, 11011, 11012, 10038, 10039, 10024,
# 11024, 11028, 11036, 100101, 10045, 100384, 13713, 100403`,
	},
	{
		Name: "card_data_input", Bank: bank.Freedom, Title: "🪪 Wrong card data",
		Description: "Freedom code=10005 → ErrCardDataInput → 'введены неверные данные'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10005</pg_error_code>
  <pg_error_description>Wrong card data input</pg_error_description>
</response>

# Альтернативы: 10008, 10031, 10032, 11004, 11050, 11062, 11063, 11065, 110501, 100056`,
	},
	{
		Name: "expired_card", Bank: bank.Freedom, Title: "📅 Expired card",
		Description: "Freedom code=10017 → ErrCardExpired → 'срок действия карты истек'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10017</pg_error_code>
  <pg_error_description>Card expired</pg_error_description>
</response>

# Альтернативы: 9901, 100171`,
	},
	{
		Name: "3ds_failed", Bank: bank.Freedom, Title: "🔐 3DS failed",
		Description: "Freedom code=10004 → Err3DSFail → '3DS проверка не пройдена'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10004</pg_error_code>
  <pg_error_description>3D Secure authentication failed</pg_error_description>
</response>

# Альтернативы: 11037, 11053, 8889, 10042, 110100`,
	},
	{
		Name: "limit_exceeded", Bank: bank.Freedom, Title: "📈 Card limit exceeded",
		Description: "Freedom code=10006 → ErrCardLimitationsExceeded → 'превышен лимит по карте'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10006</pg_error_code>
  <pg_error_description>Card limitations exceeded</pg_error_description>
</response>

# Альтернативы: 11027, 11051, 100066, 1000661, 100228, 1000669`,
	},
	{
		Name: "code_limit_exceeded", Bank: bank.Freedom, Title: "🔢 PIN attempts exceeded",
		Description: "Freedom code=10003 → ErrCodeLimit → 'превышен лимит попыток ввода кода'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10003</pg_error_code>
  <pg_error_description>PIN code attempts exceeded</pg_error_description>
</response>

# Альтернативы: 11002, 11007, 11008, 11010`,
	},
	{
		Name: "emitter_error", Bank: bank.Freedom, Title: "🏦 Emitter error",
		Description: "Freedom code=10001 → ErrEmitter → 'ошибка на стороне эмитента'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10001</pg_error_code>
  <pg_error_description>Emitter bank connection error</pg_error_description>
</response>

# Альтернативы: 101156, 999993`,
	},
	{
		Name: "country_not_supported", Bank: bank.Freedom, Title: "🌍 Country not supported",
		Description: "Freedom code=10013 → ErrCountryNotSupported → 'карта данной страны не разрешена'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10013</pg_error_code>
  <pg_error_description>Card country not supported</pg_error_description>
</response>

# Альтернатива: 11055`,
	},
	{
		Name: "transaction_amount_zero", Bank: bank.Freedom, Title: "0️⃣ Zero amount",
		Description: "Freedom code=11016 → ErrTransactionAmountIsZero → 'сумма транзакции равна нулю'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>11016</pg_error_code>
  <pg_error_description>Transaction amount is zero</pg_error_description>
</response>`,
	},
	{
		Name: "unknown_bank_error", Bank: bank.Freedom, Title: "❓ Unknown bank error",
		Description: "Freedom code=9992 → ErrUnknown → 'не ожидаемая ошибка, поддержка'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>9992</pg_error_code>
  <pg_error_description>Unknown bank error</pg_error_description>
</response>

# Альтернативы: 9993, 9994, 9996, 9997, 9998, 10012, 10014, 10015, 10016, 10018,
# 10020, 10021, 10025, 10026, 10028, 10029, 10030, 11000, 11001, 11009`,
	},
	{
		Name: "default_bank_error", Bank: bank.Freedom, Title: "🤷 Unmapped code",
		Description: "Freedom code=99999 (отсутствует в маппинге) → ErrDefault → 'обратитесь в банк'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>99999</pg_error_code>
  <pg_error_description>Unmapped error code (PG fallback to ErrDefault)</pg_error_description>
</response>

# Полезно для теста PG-fallback: что будет, если Freedom вернёт код,
# которого нет в error_mapping.go.`,
	},

	{
		Name: "wallet_empty_response", Bank: bank.Freedom, Title: "📭 Wallet empty body",
		Description: "Applepay/Googlepay вернул 200 OK с пустым body",
		Sample: `HTTP/1.1 200 OK
Content-Length: 0

(пустое тело — клиент PG получит unexpected EOF при парсинге JSON)`,
	},
	{
		Name: "wallet_malformed", Bank: bank.Freedom, Title: "💥 Wallet malformed",
		Description: "Applepay/Googlepay вернул битый JSON",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/json

{"data":{

# json.Decoder PG упадёт с "unexpected end of JSON input"`,
	},
	{
		Name: "init_malformed_xml", Bank: bank.Freedom, Title: "💥 Init malformed XML",
		Description: "init_payment.php вернул мусор вместо XML",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

<<<NOT_XML>>>

# encoding/xml PG упадёт с syntax error`,
	},
	{
		Name: "slow_body_capture", Bank: bank.Freedom, Title: "🐌 Slow body capture",
		Description: "do_capture.php — байт-в-секунду (Read timeout)",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

# Headers ушли сразу (200 OK).
# Тело: <response><pg_status>ok</pg_status></response>
# Отдаётся по одному байту в секунду → Read timeout у клиента
# (не connect timeout, как в retry_exhausted). Другой класс ошибок.`,
	},

	{
		Name: "wrong_payment_id", Bank: bank.Freedom, Title: "🔀 Wrong payment_id",
		Description: "Hold ответ с чужим pg_payment_id",
		Sample: `<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>9999999999</pg_payment_id>   ← подменено, не тот, что PG отправил
  <pg_transaction_status>Authorized</pg_transaction_status>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Тест валидации соответствия запрос↔ответ на стороне PG.`,
	},
	{
		Name: "missing_signature", Bank: bank.Freedom, Title: "🚧 Missing pg_sig",
		Description: "Hold ответ без подписи",
		Sample: `<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_transaction_status>Authorized</pg_transaction_status>
  <pg_salt>...</pg_salt>
  <!-- pg_sig отсутствует -->
</response>

# PG должен отклонить ответ из-за невалидной подписи.`,
	},
	// ===== Halyk Epay v2 presets =====
	{
		Name: "epay_insufficient_funds", Bank: bank.Epay, Title: "💸 Epay: Insufficient funds",
		Description: "Halyk reasonCode=484 → PG ErrNotEnoughMoney",
		Sample: `HTTP/1.1 400 Bad Request
Content-Type: application/json

{"code":484,"message":"Insufficient funds","resultCode":484}

# PG-классификация: reasonCode 484 → ErrNotEnoughMoney → "недостаточно средств"`,
	},
	{
		Name: "epay_card_expired", Bank: bank.Epay, Title: "📅 Epay: Expired card",
		Description: "Halyk reasonCode=478 → PG ErrCardExpired",
		Sample: `{"code":478,"message":"Card expired","resultCode":478}

# Альтернатива: reasonCode=485 (та же категория).`,
	},
	{
		Name: "epay_invalid_card", Bank: bank.Epay, Title: "🪪 Epay: Invalid card data",
		Description: "Halyk reasonCode=457 → PG ErrCardDataInput",
		Sample: `{"code":457,"message":"Invalid card data","resultCode":457}

# Альтернативы: 492, 473, 499, 469, 471, 472, 501.`,
	},
	{
		Name: "epay_declined_by_issuer", Bank: bank.Epay, Title: "🚫 Epay: Declined by issuer",
		Description: "Halyk reasonCode=455 → PG ErrDeclinedByIssuer",
		Sample: `{"code":455,"message":"Declined by issuer","resultCode":455}

# Альтернативы: 456, 462, 463, 466, 468, 487, 490, 521, 523, 527.`,
	},
	{
		Name: "epay_limit_exceeded", Bank: bank.Epay, Title: "📈 Epay: Limit exceeded",
		Description: "Halyk reasonCode=486 → PG ErrCardLimitationsExceeded",
		Sample: `{"code":486,"message":"Card limitations exceeded","resultCode":486}

# Альтернативы: 488, 491, 528, 529.`,
	},
	{
		Name: "epay_3ds_required", Bank: bank.Epay, Title: "🔐 Epay: 3DS challenge",
		Description: "Cryptopay возвращает secure3D-блок → PG должен редиректить пользователя на ACS",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/json

{
  "id":"mock-epay-1700000123",
  "amount":5000,
  "currency":"KZT",
  "invoiceId":"000123",
  "secure3D":{
    "paReq":"mock-pareq",
    "md":"mock-epay-1700000123",
    "action":"https://test.bankffin.kz/3d_secure"
  },
  ...
}`,
	},
	{
		Name: "epay_oauth_timeout", Bank: bank.Epay, Title: "⏱ Epay: OAuth timeout",
		Description: "POST /oauth2/token — 15s timeout × 3 retry. PG не получит access_token.",
		Sample: `# Каждая попытка → 15s + TCP close.
# PG-лог:
giving up after 3 attempt(s): Post "https://testoauth.homebank.kz/epay2/oauth2/token":
context deadline exceeded`,
	},
	{
		Name: "epay_charge_timeout", Bank: bank.Epay, Title: "⏱ Epay: Charge timeout",
		Description: "POST /api/operation/{id}/charge — 20s timeout. Холд остаётся, статус неизвестен.",
		Sample: `# PG-лог: net/http: request canceled while awaiting headers
# PG-сценарий: при ошибке charge → reconciling запрос статуса.`,
	},
	{
		Name: "epay_cryptopay_500", Bank: bank.Epay, Title: "💥 Epay: Cryptopay 500",
		Description: "Cryptopay отвечает 500 (Halyk API упал)",
		Sample: `HTTP/1.1 500 Internal Server Error
# PG retryablehttp ретрайт 3×, потом giving up`,
	},
	{
		Name: "epay_postlink_lost", Bank: bank.Epay, Title: "📭 Epay: Postlink lost",
		Description: "Charge успешен, но мок НЕ шлёт postlink. PG ждёт callback вечно (reconciler найдёт).",
		Sample: `# Шаг 1: charge → 200 {code:0}
# Шаг 2: postlink НЕ отправляется
# PG: транзакция остаётся в pending до тайм-аута reconciler-а.`,
	},
	{
		Name: "epay_postlink_double", Bank: bank.Epay, Title: "🔁 Epay: Double postlink",
		Description: "После charge мок шлёт postlink дважды (race-кейс)",
		Sample: `# Шаг 1: charge → 200 {code:0}
# Шаг 2: postlink → /webhook/epay_v2/postlink (success)
# Шаг 3: ещё один postlink на тот же URL через 500мс
# Тест idempotency на стороне PG.`,
	},
	{
		Name: "epay_postlink_before_ack", Bank: bank.Epay, Title: "⚡ Epay: Postlink-before-ack",
		Description: "Postlink уходит до возврата ответа на charge (race с inflight-запросом)",
		Sample: `# Шаг 1: PG отправляет POST /api/operation/{id}/charge
# Шаг 2: мок начинает обработку — параллельно шлёт postlink
# Шаг 3: PG обрабатывает postlink ДО получения ответа на charge
# Воспроизводит inflight-race на стороне PG.`,
	},
	{
		Name: "epay_wrong_invoice_id", Bank: bank.Epay, Title: "🔀 Epay: Wrong invoiceId",
		Description: "Cryptopay ответ с подменённым invoiceId (проверка валидации соответствия запрос↔ответ)",
		Sample: `{
  "id":"mock-epay-1700000123",
  "invoiceId":"000000",  ← не тот, что PG отправил
  ...
}`,
	},
	{
		Name: "epay_unknown_error", Bank: bank.Epay, Title: "❓ Epay: Unknown bank error",
		Description: "Halyk reasonCode=477 → PG ErrUnknown",
		Sample:      `{"code":477,"message":"Unknown bank error","resultCode":477}`,
	},
	{
		Name: "epay_ambiguous_charge_recovery", Bank: bank.Epay, Title: "⚡ Epay-EX1001: Charge ambiguous → status recovery",
		Description: "Charge упал (code=477 'Operation already exists'), но check-status показывает CHARGE → PG должен принять",
		Sample: `# Шаг 1: POST /api/operation/{id}/charge → 400
{"code":477,"message":"Operation already exists","resultCode":477}

# Шаг 2: GET /check-status/payment/transactionId/{id} → 200
{"id":"...","status":"CHARGE","statusName":"Списан","amount":5000}

# PG-reconciler: видит CHARGE → транзакция → Captured (как Freedom EX-1001).`,
	},
	{
		Name: "epay_ambiguous_authorize_recovery", Bank: bank.Epay, Title: "⚡ Epay-EX1001: Authorize ambiguous → status recovery",
		Description: "Cryptopay упал (code=477), но check-status показывает AUTH → платёж создан",
		Sample: `# Шаг 1: POST /api/payment/cryptopay → 400
{"code":477,"message":"Operation already exists","resultCode":477}

# Шаг 2: GET /check-status/payment/transactionId/{id} → 200
{"status":"AUTH","statusName":"Авторизован","amount":5000,...}

# PG: принимает Authorized, продолжает flow charge.`,
	},
	{
		Name: "epay_unauthorized_401", Bank: bank.Epay, Title: "🔒 Epay: 401 Unauthorized",
		Description: "OAuth-токен принят, но платёжный запрос вернул 401 (токен истёк / отозван)",
		Sample: `HTTP/1.1 401 Unauthorized
{"message":"Unauthorized"}

# PG-клиент: "требуется авторизация" (epay_2/client.go:checkResponse).`,
	},
	{
		Name: "epay_forbidden_403", Bank: bank.Epay, Title: "🚫 Epay: 403 Forbidden (IP-whitelist)",
		Description: "Запрос с не-whitelisted IP. В проде — индикатор смены IP/proxy.",
		Sample: `HTTP/1.1 403 Forbidden
{"message":"Forbidden"}

# PG-клиент: "недостаточно прав для выполнения операции".`,
	},
	{
		Name: "epay_transient_500_then_ok", Bank: bank.Epay, Title: "🔄 Epay: Transient 500 → retry succeeds",
		Description: "Первая попытка charge → 500, retry → 200. Тест smarthttp retry-обёртки PG.",
		Sample: `# Запрос 1: POST /api/operation/{id}/charge → 500
{"message":"Service temporarily unavailable"}

# (smarthttp.WithRetryRequest)
# Запрос 2: POST /api/operation/{id}/charge → 200
{"code":0,"message":"Operation completed successfully"}`,
	},
	{
		Name: "epay_double_charge_rejected", Bank: bank.Epay, Title: "🔁 Epay: Double charge → 477",
		Description: "Повторный charge на ту же операцию (после успешного первого) — отклоняется",
		Sample: `# Запрос 1: charge → 200 (успех)
# Запрос 2: charge (retry после сетевого timeout) → 400
{"code":477,"message":"Operation already charged"}

# PG: ambiguous-marker, идёт в check-status — там CHARGE → принимает.`,
	},
	{
		Name: "epay_double_cancel_rejected", Bank: bank.Epay, Title: "🔁 Epay: Double cancel → error",
		Description: "Повторный cancel на уже отменённой операции",
		Sample: `# Запрос 1: cancel → 200
# Запрос 2: cancel → 400
{"code":477,"message":"Operation already cancelled"}`,
	},
	{
		Name: "epay_oauth_unauthorized", Bank: bank.Epay, Title: "🔒 Epay: OAuth credentials invalid",
		Description: "POST /oauth2/token → 401: client_id/secret не приняты",
		Sample: `HTTP/1.1 401 Unauthorized
{"message":"Invalid client credentials"}

# PG: getToken() fail → весь flow обрывается до первого платёжного вызова.`,
	},
	{
		Name: "epay_bind_failure", Bank: bank.Epay, Title: "🪪 Epay: Bind failure webhook",
		Description: "Cryptopay с cardSave=true прошёл, но bind-postlink приходит с reasonCode != 0",
		Sample: `POST /api/v1/payment-gateway/webhook/epay/postlink/bind
{
  "accountId":"12345",
  "cardId":"11f1...","cardMask":"440043...2221",
  "code":"error","reason":"Card binding failed","reasonCode":-444,
  "invoiceId":"191111111"
}

# PG: order.markAsFailed для bind-flow.`,
	},
	{
		Name: "epay_webhook_unknown_order", Bank: bank.Epay, Title: "📨 Epay: Webhook for non-existent order",
		Description: "Postlink приходит с invoiceId, которого нет в БД PG",
		Sample: `POST /api/v1/payment-gateway/webhook/epay_v2/postlink
{"id":"epay-uuid","invoiceId":"999999999","code":"ok",...}

# PG: 200 OK, лог-warn, ничего не делает (защита от мусорных webhook-ов).`,
	},
	{
		Name: "epay_webhook_missing_fields", Bank: bank.Epay, Title: "📨 Epay: Webhook with missing fields",
		Description: "Postlink с пропущенными опциональными полями (cardId, reference, …)",
		Sample: `POST /api/v1/payment-gateway/webhook/epay_v2/postlink
{"id":"epay-uuid","invoiceId":"123","code":"ok"}
# cardId, cardMask, reference, issuer — отсутствуют.
# PG: записывает orderID без payment-метаданных.`,
	},
	{
		Name: "epay_3ds_missing_action_url", Bank: bank.Epay, Title: "🔐 Epay: 3DS without action URL",
		Description: "Cryptopay возвращает secure3D, но action пустой (мусорный ответ Halyk)",
		Sample: `{
  "id":"epay-uuid",
  "secure3D":{"paReq":"...","md":"...","action":""}
}
# PG не сможет отрендерить редирект — клиент застрянет на форме.`,
	},

	{
		Name: "wrong_amount", Bank: bank.Freedom, Title: "💱 Wrong amount",
		Description: "Status вернул pg_amount=1",
		Sample: `<response>
  <pg_status>ok</pg_status>
  <pg_payment_id>123</pg_payment_id>
  <pg_payment_status>success</pg_payment_status>
  <pg_amount>1</pg_amount>             ← вместо реальной суммы
  <pg_clearing_amount>1</pg_clearing_amount>
  <pg_salt>...</pg_salt><pg_sig>...</pg_sig>
</response>

# Тест сверки сумм на стороне PG.`,
	},
}

// ApplyPreset — добавляет сценарии по имени preset-а. См. AllPresets для списка.
func (s *Service) ApplyPreset(name string) { //nolint:gocyclo,funlen
	wild := scenario.Wildcard
	addFor := func(b bank.Bank, endpoint string, action scenario.Action, params map[string]string, consumeOnce bool) {
		s.store.Add(&scenario.Scenario{
			Bank: b, Endpoint: endpoint, PaymentID: wild, OrderID: wild, MerchantID: wild,
			Action: action, Params: params, ConsumeOnce: consumeOnce, CreatedAt: time.Now(),
		})
	}
	add := func(endpoint string, action scenario.Action, params map[string]string, consumeOnce bool) {
		addFor(bank.Freedom, endpoint, action, params, consumeOnce)
	}
	addEpay := func(endpoint string, action scenario.Action, params map[string]string, consumeOnce bool) {
		addFor(bank.Epay, endpoint, action, params, consumeOnce)
	}

	switch name {
	case "ex1001":
		add("direct", scenario.ActionAmbiguousError, map[string]string{"message": "Неверный статус платежа", "error_code": "120"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "success"}, true)
	case "ex1001_silent_hold":
		// direct синхронно отвечает ошибкой, но мок параллельно холдирует и шлёт success-webhook.
		add("direct", scenario.ActionSyncErrorAsyncWebhook, map[string]string{"message": "Неверный статус платежа", "error_code": "120"}, true)
	case "ex1001_wallet_silent_hold":
		// applepay/googlepay синхронно отвечают JSON-ошибкой, но мок параллельно холдирует и шлёт success-webhook.
		add("applepay", scenario.ActionSyncErrorAsyncWebhook, map[string]string{"message": "Неверный статус платежа"}, true)
		add("googlepay", scenario.ActionSyncErrorAsyncWebhook, map[string]string{"message": "Неверный статус платежа"}, true)
	case "hold_pending_recovery":
		// Шаг 1: Hold отвечает ok с pg_payment_status=process (pending).
		// Шаг 2: Авто-Status (без ReconcilingClient) подтверждает success.
		add("direct", scenario.ActionForceStatus, map[string]string{"payment_status": "process"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "success"}, true)
	case "capture_failed_status_approved":
		// Capture упал, но Status говорит что деньги списаны → PG принимает.
		add("do_capture.php", scenario.ActionForceFailure, map[string]string{"message": "technical bank error", "error_code": "100"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "success"}, true)
	case "cancel_failed_status_revoked":
		// Cancel упал, но Status говорит revoked → PG принимает.
		add("cancel.php", scenario.ActionForceFailure, map[string]string{"message": "technical bank error", "error_code": "100"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "revoked"}, true)
	case "revoke_failed_status_revoked":
		// Revoke (refund) упал, но Status говорит revoked → PG принимает.
		add("revoke.php", scenario.ActionForceFailure, map[string]string{"message": "technical bank error", "error_code": "100"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "revoked"}, true)
	case "hold_timeout":
		add("direct", scenario.ActionTimeout, map[string]string{"seconds": "20"}, true)
	case "desync":
		add("direct", scenario.ActionForceFailure, map[string]string{"message": "технический сбой банка", "error_code": "100"}, true)

	// Transport: retry-exhausted. ConsumeOnce=false — каждая retry-попытка матчится снова.
	// seconds=15 > PG retryablehttp Client.Timeout → "giving up ... awaiting headers".
	case "init_retry_exhausted":
		add("init_payment.php", scenario.ActionTimeout, map[string]string{"seconds": "15"}, false)
	case "hold_init_retry_exhausted":
		add("init", scenario.ActionTimeout, map[string]string{"seconds": "15"}, false)
	case "wallet_retry_exhausted":
		add("applepay", scenario.ActionTimeout, map[string]string{"seconds": "15"}, false)
		add("googlepay", scenario.ActionTimeout, map[string]string{"seconds": "15"}, false)
	case "context_deadline":
		// Wildcard endpoint — действует на любой банк, поэтому Bank=Any.
		addFor(bank.Any, wild, scenario.ActionTimeout, map[string]string{"seconds": "60"}, false)

	// Бизнес-ошибки. Коды соответствуют PG error_mapping.go — один-в-один
	// с тем, что Freedom Pay возвращает в проде.
	case "insufficient_funds":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10009", "message": "Insufficient funds on card",
		}, true)
	case "card_declined":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10007", "message": "Declined by issuer",
		}, true)
	case "card_data_input":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10005", "message": "Wrong card data input",
		}, true)
	case "expired_card":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10017", "message": "Card expired",
		}, true)
	case "3ds_failed":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10004", "message": "3D Secure authentication failed",
		}, true)
	case "limit_exceeded":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10006", "message": "Card limitations exceeded",
		}, true)
	case "code_limit_exceeded":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10003", "message": "PIN code attempts exceeded",
		}, true)
	case "emitter_error":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10001", "message": "Emitter bank connection error",
		}, true)
	case "country_not_supported":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "10013", "message": "Card country not supported",
		}, true)
	case "transaction_amount_zero":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "11016", "message": "Transaction amount is zero",
		}, true)
	case "unknown_bank_error":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "9992", "message": "Unknown bank error",
		}, true)
	case "default_bank_error":
		add(wild, scenario.ActionForceFailure, map[string]string{
			"error_code": "99999", "message": "Unmapped error code (PG fallback to ErrDefault)",
		}, true)

	// Битые ответы (для тестов парсеров PG).
	case "wallet_empty_response":
		add("applepay", scenario.ActionEmptyResponse, nil, true)
		add("googlepay", scenario.ActionEmptyResponse, nil, true)
	case "wallet_malformed":
		p := map[string]string{"body": `{"data":{`, "content_type": "application/json"}
		add("applepay", scenario.ActionMalformedBody, p, true)
		add("googlepay", scenario.ActionMalformedBody, p, true)
	case "init_malformed_xml":
		add("init_payment.php", scenario.ActionMalformedBody, map[string]string{"body": `<<<NOT_XML>>>`}, true)
	case "slow_body_capture":
		add("do_capture.php", scenario.ActionSlowBody, map[string]string{"chunk_delay_ms": "1000"}, true)

	// Содержимое (data integrity).
	case "wrong_payment_id":
		add("direct", scenario.ActionWrongPaymentID, map[string]string{"payment_id": "9999999999"}, true)
	case "missing_signature":
		add("direct", scenario.ActionMissingField, map[string]string{"field": "pg_sig"}, true)
	case "wrong_amount":
		add("get_status3.php", scenario.ActionWrongAmount, map[string]string{"amount": "1"}, true)

	// ===== Halyk Epay v2 =====
	case "epay_insufficient_funds":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "484", "message": "Insufficient funds"}, true)
	case "epay_card_expired":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "478", "message": "Card expired"}, true)
	case "epay_invalid_card":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "457", "message": "Invalid card data"}, true)
	case "epay_declined_by_issuer":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "455", "message": "Declined by issuer"}, true)
	case "epay_limit_exceeded":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "486", "message": "Card limitations exceeded"}, true)
	case "epay_unknown_error":
		addEpay(wild, scenario.ActionForceFailure, map[string]string{"reason_code": "477", "message": "Unknown bank error"}, true)
	case "epay_3ds_required":
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionForce3DS, nil, true)
	case "epay_oauth_timeout":
		addEpay(scenario.EndpointEpayToken, scenario.ActionTimeout, map[string]string{"seconds": "15"}, false)
	case "epay_charge_timeout":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionTimeout, map[string]string{"seconds": "20"}, true)
	case "epay_cryptopay_500":
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionHTTPError, map[string]string{"http_status": "500"}, true)
	case "epay_postlink_lost":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionPostlinkLost, nil, true)
	case "epay_postlink_double":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionPostlinkDouble, nil, true)
	case "epay_postlink_before_ack":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionPostlinkBeforeAck, nil, true)
	case "epay_wrong_invoice_id":
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionMissingField, map[string]string{"field": "invoiceId"}, true)

	// Halyk EX-1001 аналог: charge/cryptopay fail, но status показывает реальный success.
	case "epay_ambiguous_charge_recovery":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionEpayAmbiguous,
			map[string]string{"reason_code": "477", "message": "Operation already exists"}, true)
		// На следующий status-check — handler без сценария отдаст реальное состояние из репо,
		// но т.к. сам charge не выполнился, статус остался AUTH → ровно то, что нужно для recovery.
	case "epay_ambiguous_authorize_recovery":
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionEpayAmbiguous,
			map[string]string{"reason_code": "477", "message": "Operation already exists"}, true)

	// HTTP-уровневые статусы
	case "epay_unauthorized_401":
		addEpay(wild, scenario.ActionForceUnauthorized, nil, true)
	case "epay_forbidden_403":
		addEpay(wild, scenario.ActionForceForbidden, nil, true)
	case "epay_oauth_unauthorized":
		addEpay(scenario.EndpointEpayToken, scenario.ActionForceUnauthorized,
			map[string]string{"message": "Invalid client credentials"}, true)

	// Transient retry-recovery: одна попытка 500, следующая — успешна.
	case "epay_transient_500_then_ok":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionTransientFailure,
			map[string]string{"http_status": "500", "message": "Service temporarily unavailable"}, false)

	// Double charge/cancel
	case "epay_double_charge_rejected":
		addEpay(scenario.EndpointEpayCharge, scenario.ActionForceFailure,
			map[string]string{"reason_code": "477", "message": "Operation already charged"}, true)
	case "epay_double_cancel_rejected":
		addEpay(scenario.EndpointEpayCancel, scenario.ActionForceFailure,
			map[string]string{"reason_code": "477", "message": "Operation already cancelled"}, true)

	// Bind / webhook edge cases
	case "epay_bind_failure":
		// На cryptopay (cardSave-flow) cancel transmission — ошибка bind webhook идёт отдельно
		// через panel-button "Send Card-Bind Webhook (Failure)". Пока — преcет работает как
		// маркер: сам по себе не создаёт сценарий, но добавляет правило on-bind cryptopay
		// success возвращать ошибку.
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionForceFailure,
			map[string]string{"reason_code": "457", "message": "Card binding failed"}, true)
	case "epay_webhook_unknown_order":
		// Webhook отправляется со стороны мока вручную; этот preset — заметка для UI,
		// что нужно нажать "Send postlink" на несуществующий orderID. Технически добавляем
		// no-op-сценарий, чтобы preset фигурировал в списке.
		addEpay(wild, scenario.ActionDelay, map[string]string{"seconds": "0"}, true)
	case "epay_webhook_missing_fields":
		// Аналогично — оператор отправляет webhook руками. Сценарий-маркер.
		addEpay(wild, scenario.ActionDelay, map[string]string{"seconds": "0"}, true)
	case "epay_3ds_missing_action_url":
		addEpay(scenario.EndpointEpayCryptopay, scenario.ActionForce3DS,
			map[string]string{"action": "", "pa_req": "mock-pareq"}, true)
	}
}
