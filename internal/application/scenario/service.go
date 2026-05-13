// Package scenario — сервис над хранилищем сценариев.
package scenario

import (
	"time"

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
	Title       string
	Description string
	// Sample — пример ответа банка в этом сценарии. Раскрывается в UI через <details>,
	// чтобы можно было сразу понять, что увидит PG и его клиенты.
	// Формат свободный: XML для Freedom Pay, JSON для wallet, текст лога для transport-уровневых.
	Sample string
}

// AllPresets — список всех доступных preset-ов для рендера в panel.
// Бизнес-пресеты используют РЕАЛЬНЫЕ Freedom error codes из PG-маппинга
// ([internal/infrastructure/clients/payments/freedom/error_mapping.go]).
// Каждый код триггерит конкретную domain-ошибку PG (common.Err*).
var AllPresets = []PresetInfo{
	{
		Name: "ex1001", Title: "⚡ EX-1001",
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
		Name: "hold_pending_recovery", Title: "🔄 Hold pending → recovery",
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
		Name: "capture_failed_status_approved", Title: "💰 Capture failed → Status approved",
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
		Name: "cancel_failed_status_revoked", Title: "🚫 Cancel failed → Status revoked",
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
		Name: "revoke_failed_status_revoked", Title: "↩ Revoke failed → Status revoked",
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
		Name: "hold_timeout", Title: "Hold Timeout",
		Description: "Таймаут на direct (20s)",
		Sample: `# Соединение зависает на 20 секунд, потом мок закрывает TCP без ответа.
# PG увидит:
Post "https://api.freedompay.kz/v1/merchant/.../card/direct": net/http: request canceled
(Client.Timeout exceeded while awaiting headers)`,
	},
	{
		Name: "desync", Title: "Desync",
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
		Name: "init_retry_exhausted", Title: "🌐 Init retry-exhausted",
		Description: "3× timeout на init_payment.php (PG: giving up after 3 attempt(s))",
		Sample: `# Каждая попытка PG: 15s ожидания → TCP close без ответа.
# После 3 retry hashicorp/go-retryablehttp сдаётся.
# PG-лог:
: Post "https://api.freedompay.kz/init_payment.php": POST https://api.freedompay.kz/init_payment.php
giving up after 3 attempt(s): Post "...": net/http: request canceled while waiting for connection
(Client.Timeout exceeded while awaiting headers)`,
	},
	{
		Name: "hold_init_retry_exhausted", Title: "🌐 HoldInit retry-exhausted",
		Description: "3× timeout на /v1/merchant/.../card/init",
		Sample: `# То же что init_retry_exhausted, но на эндпоинте создания Hold.
# PG-лог:
: не удалось сделать инициализацию платежа: Post "https://api.freedompay.kz/v1/merchant/554415/card/init":
POST https://api.freedompay.kz/v1/merchant/554415/card/init giving up after 3 attempt(s)`,
	},
	{
		Name: "wallet_retry_exhausted", Title: "🌐 Wallet retry-exhausted",
		Description: "3× timeout на applepay/googlepay",
		Sample: `# PG-клиент wallet (Apple/Google Pay) исчерпал retry.
# PG-лог:
: Post "https://customer.freedompay.kz/pay/019e0186-.../pay": POST https://customer.freedompay.kz/pay/.../pay
giving up after 3 attempt(s): Post "...": context deadline exceeded`,
	},
	{
		Name: "context_deadline", Title: "⏱ Context deadline",
		Description: "60s timeout — клиент PG отвалится по context.deadline",
		Sample: `# Мок засыпает на 60s — гарантированно дольше любого PG context.WithTimeout.
# PG-лог:
giving up after 3 attempt(s): Post "...": context deadline exceeded`,
	},

	// Бизнес-ошибки — коды соответствуют PG error_mapping.go (один-в-один с тем,
	// что real Freedom возвращает в проде). Каждый триггерит свой common.Err*.
	{
		Name: "insufficient_funds", Title: "💸 Insufficient funds",
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
		Name: "card_declined", Title: "🚫 Declined by issuer",
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
		Name: "card_data_input", Title: "🪪 Wrong card data",
		Description: "Freedom code=10005 → ErrCardDataInput → 'введены неверные данные'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10005</pg_error_code>
  <pg_error_description>Wrong card data input</pg_error_description>
</response>

# Альтернативы: 10008, 10031, 10032, 11004, 11050, 11062, 11063, 11065, 110501, 100056`,
	},
	{
		Name: "expired_card", Title: "📅 Expired card",
		Description: "Freedom code=10017 → ErrCardExpired → 'срок действия карты истек'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10017</pg_error_code>
  <pg_error_description>Card expired</pg_error_description>
</response>

# Альтернативы: 9901, 100171`,
	},
	{
		Name: "3ds_failed", Title: "🔐 3DS failed",
		Description: "Freedom code=10004 → Err3DSFail → '3DS проверка не пройдена'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10004</pg_error_code>
  <pg_error_description>3D Secure authentication failed</pg_error_description>
</response>

# Альтернативы: 11037, 11053, 8889, 10042, 110100`,
	},
	{
		Name: "limit_exceeded", Title: "📈 Card limit exceeded",
		Description: "Freedom code=10006 → ErrCardLimitationsExceeded → 'превышен лимит по карте'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10006</pg_error_code>
  <pg_error_description>Card limitations exceeded</pg_error_description>
</response>

# Альтернативы: 11027, 11051, 100066, 1000661, 100228, 1000669`,
	},
	{
		Name: "code_limit_exceeded", Title: "🔢 PIN attempts exceeded",
		Description: "Freedom code=10003 → ErrCodeLimit → 'превышен лимит попыток ввода кода'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10003</pg_error_code>
  <pg_error_description>PIN code attempts exceeded</pg_error_description>
</response>

# Альтернативы: 11002, 11007, 11008, 11010`,
	},
	{
		Name: "emitter_error", Title: "🏦 Emitter error",
		Description: "Freedom code=10001 → ErrEmitter → 'ошибка на стороне эмитента'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10001</pg_error_code>
  <pg_error_description>Emitter bank connection error</pg_error_description>
</response>

# Альтернативы: 101156, 999993`,
	},
	{
		Name: "country_not_supported", Title: "🌍 Country not supported",
		Description: "Freedom code=10013 → ErrCountryNotSupported → 'карта данной страны не разрешена'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>10013</pg_error_code>
  <pg_error_description>Card country not supported</pg_error_description>
</response>

# Альтернатива: 11055`,
	},
	{
		Name: "transaction_amount_zero", Title: "0️⃣ Zero amount",
		Description: "Freedom code=11016 → ErrTransactionAmountIsZero → 'сумма транзакции равна нулю'",
		Sample: `<response>
  <pg_status>error</pg_status>
  <pg_error_code>11016</pg_error_code>
  <pg_error_description>Transaction amount is zero</pg_error_description>
</response>`,
	},
	{
		Name: "unknown_bank_error", Title: "❓ Unknown bank error",
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
		Name: "default_bank_error", Title: "🤷 Unmapped code",
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
		Name: "wallet_empty_response", Title: "📭 Wallet empty body",
		Description: "Applepay/Googlepay вернул 200 OK с пустым body",
		Sample: `HTTP/1.1 200 OK
Content-Length: 0

(пустое тело — клиент PG получит unexpected EOF при парсинге JSON)`,
	},
	{
		Name: "wallet_malformed", Title: "💥 Wallet malformed",
		Description: "Applepay/Googlepay вернул битый JSON",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/json

{"data":{

# json.Decoder PG упадёт с "unexpected end of JSON input"`,
	},
	{
		Name: "init_malformed_xml", Title: "💥 Init malformed XML",
		Description: "init_payment.php вернул мусор вместо XML",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

<<<NOT_XML>>>

# encoding/xml PG упадёт с syntax error`,
	},
	{
		Name: "slow_body_capture", Title: "🐌 Slow body capture",
		Description: "do_capture.php — байт-в-секунду (Read timeout)",
		Sample: `HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

# Headers ушли сразу (200 OK).
# Тело: <response><pg_status>ok</pg_status></response>
# Отдаётся по одному байту в секунду → Read timeout у клиента
# (не connect timeout, как в retry_exhausted). Другой класс ошибок.`,
	},

	{
		Name: "wrong_payment_id", Title: "🔀 Wrong payment_id",
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
		Name: "missing_signature", Title: "🚧 Missing pg_sig",
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
	{
		Name: "wrong_amount", Title: "💱 Wrong amount",
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
func (s *Service) ApplyPreset(name string) { //nolint:gocyclo
	wild := scenario.Wildcard
	add := func(endpoint string, action scenario.Action, params map[string]string, consumeOnce bool) {
		s.store.Add(&scenario.Scenario{
			Endpoint: endpoint, PaymentID: wild, OrderID: wild, MerchantID: wild,
			Action: action, Params: params, ConsumeOnce: consumeOnce, CreatedAt: time.Now(),
		})
	}

	switch name {
	case "ex1001":
		add("direct", scenario.ActionAmbiguousError, map[string]string{"message": "Неверный статус платежа", "error_code": "120"}, true)
		add("get_status3.php", scenario.ActionForceStatus, map[string]string{"payment_status": "success"}, true)
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
		add(wild, scenario.ActionTimeout, map[string]string{"seconds": "60"}, false)

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
	}
}
