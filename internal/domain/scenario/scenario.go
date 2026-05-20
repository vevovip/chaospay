// Package scenario описывает правила имитации ответов банка.
package scenario

import (
	"time"

	"github.com/vevovip/chaospay/internal/domain/bank"
)

// Action — тип реакции мока на матчевый запрос.
type Action string

// Все типы действий.
const (
	ActionTimeout          Action = "timeout"
	ActionAmbiguousError   Action = "ambiguous_error"
	ActionForceStatus      Action = "force_status"
	ActionHTTPError        Action = "http_error"
	ActionInvalidSignature Action = "invalid_signature"
	ActionPartialAmount    Action = "partial_amount"
	ActionDelay            Action = "delay"
	ActionForceFailure     Action = "force_failure"
	// ActionSyncErrorAsyncWebhook — EX-1001: банк отвечает синхронно ошибкой (pg_status=error),
	// но при этом параллельно холдирует платёж и асинхронно шлёт success-webhook (pg_result=1).
	// Параметры: error_code (по умолч. "120"), message (по умолч. "Неверный статус платежа").
	ActionSyncErrorAsyncWebhook Action = "sync_error_async_webhook"
	// Transport-level: моделируют сетевые/протокольные сбои до того, как PG распарсит ответ.
	ActionConnectionReset Action = "connection_reset" // мгновенный TCP-reset без ответа
	ActionEmptyResponse   Action = "empty_response"   // 200 OK, тело пустое
	ActionMalformedBody   Action = "malformed_body"   // 200 OK, тело — мусор (param: body)
	ActionSlowBody        Action = "slow_body"        // headers ушли, тело — по байту с задержкой (param: chunk_delay_ms)
	ActionWrongStatusCode Action = "wrong_status_code"
	// Content-level: валидный XML/JSON, но с искажёнными бизнес-полями.
	ActionWrongPaymentID Action = "wrong_payment_id" // подмена pg_payment_id / id
	ActionWrongAmount    Action = "wrong_amount"     // подмена pg_amount / amount
	ActionMissingField   Action = "missing_field"    // удалить указанное поле (param: field)
	ActionExtraGarbage   Action = "extra_garbage"    // добавить мусорные поля
	// Epay-specific content-level. Реализуются в ports/api/epay/scenario.go.
	ActionForce3DS          Action = "force_3ds"           // на cryptopay вернуть secure3D-ответ (3DS-челлендж)
	ActionPostlinkBeforeAck Action = "postlink_before_ack" // отправить postlink ДО возврата ответа на charge
	ActionPostlinkDouble    Action = "postlink_double"     // отправить postlink дважды
	ActionPostlinkLost      Action = "postlink_lost"       // НЕ отправлять postlink (только ответ на charge)
	// Halyk Epay incident-кейсы для reconciliation/race-flow.
	ActionEpayAmbiguous     Action = "epay_ambiguous"     // 400 {code:477, message:"Operation already exists"}; следующий status-check вернёт настоящее состояние
	ActionTransientFailure  Action = "transient_failure"  // первый запрос — http_status (по умолч. 500), последующие — нормальный ответ
	ActionForceUnauthorized Action = "force_unauthorized" // 401 {message:"Unauthorized"} (как при истёкшем токене)
	ActionForceForbidden    Action = "force_forbidden"    // 403 {message:"Forbidden"} (IP-whitelist в продaкшне)
)

// AllActions — для UI dropdown.
var AllActions = []Action{
	ActionAmbiguousError,
	ActionForceStatus,
	ActionTimeout,
	ActionDelay,
	ActionHTTPError,
	ActionInvalidSignature,
	ActionPartialAmount,
	ActionForceFailure,
	ActionSyncErrorAsyncWebhook,
	ActionConnectionReset,
	ActionEmptyResponse,
	ActionMalformedBody,
	ActionSlowBody,
	ActionWrongStatusCode,
	ActionWrongPaymentID,
	ActionWrongAmount,
	ActionMissingField,
	ActionExtraGarbage,
	ActionForce3DS,
	ActionPostlinkBeforeAck,
	ActionPostlinkDouble,
	ActionPostlinkLost,
	ActionEpayAmbiguous,
	ActionTransientFailure,
	ActionForceUnauthorized,
	ActionForceForbidden,
}

// Wildcard матчер — совпадает с любым значением.
const Wildcard = "*"

// Freedom Pay endpoints (scriptName входящих запросов).
const (
	EndpointFreedomInit        = "init"
	EndpointFreedomDirect      = "direct"
	EndpointFreedomStatus      = "get_status3.php"
	EndpointFreedomCapture     = "do_capture.php"
	EndpointFreedomCancel      = "cancel.php"
	EndpointFreedomRevoke      = "revoke.php"
	EndpointFreedomInitPayment = "init_payment.php"
	EndpointFreedomCardAdd     = "add2"
	EndpointFreedomCardRemove  = "remove"
	EndpointFreedomApplePay    = "applepay"
	EndpointFreedomGooglePay   = "googlepay"
)

// Halyk Epay v2 endpoints.
const (
	EndpointEpayToken     = "epay_token"
	EndpointEpayCryptopay = "epay_cryptopay"
	EndpointEpayCardAuth  = "epay_card_auth"
	EndpointEpayCharge    = "epay_charge"
	EndpointEpayCancel    = "epay_cancel"
	EndpointEpayRefund    = "epay_refund"
	EndpointEpayStatus    = "epay_status" // GET /check-status/payment/transactionId/{id}
)

// Flitt endpoints (operation-name внутри JSON-handler-ов).
const (
	EndpointFlittCheckout  = "flitt_checkout"  // POST /api/checkout/url — hosted-форма
	EndpointFlittDirect    = "flitt_direct"    // POST /api/3dsecure_step1 — direct (Apple/Google Pay)
	EndpointFlittRecurring = "flitt_recurring" // POST /api/recurring — списание сохранённой картой
	EndpointFlittCapture   = "flitt_capture"   // POST /api/capture/order_id
	EndpointFlittReverse   = "flitt_reverse"   // POST /api/reverse/order_id
	EndpointFlittStatus    = "flitt_status"    // POST /api/status/order_id
	EndpointFlittStep2     = "flitt_3ds_step2" // POST /api/3dsecure_step2 — завершение 3DS
)

// AllEndpoints — для UI dropdown.
var AllEndpoints = []string{
	Wildcard,
	// Freedom
	EndpointFreedomInit, EndpointFreedomDirect,
	EndpointFreedomStatus, EndpointFreedomCapture, EndpointFreedomCancel, EndpointFreedomRevoke,
	EndpointFreedomInitPayment,
	EndpointFreedomCardAdd, EndpointFreedomCardRemove,
	EndpointFreedomApplePay, EndpointFreedomGooglePay,
	// Epay
	EndpointEpayToken, EndpointEpayCryptopay, EndpointEpayCardAuth,
	EndpointEpayCharge, EndpointEpayCancel, EndpointEpayRefund, EndpointEpayStatus,
	// Flitt
	EndpointFlittCheckout, EndpointFlittDirect, EndpointFlittRecurring,
	EndpointFlittCapture, EndpointFlittReverse, EndpointFlittStatus, EndpointFlittStep2,
}

// EndpointBank возвращает банк, к которому принадлежит endpoint-ключ.
// Для wildcard ("*") и неизвестных — bank.Any.
func EndpointBank(ep string) bank.Bank {
	switch ep {
	case EndpointFreedomInit, EndpointFreedomDirect,
		EndpointFreedomStatus, EndpointFreedomCapture, EndpointFreedomCancel, EndpointFreedomRevoke,
		EndpointFreedomInitPayment,
		EndpointFreedomCardAdd, EndpointFreedomCardRemove,
		EndpointFreedomApplePay, EndpointFreedomGooglePay:
		return bank.Freedom
	case EndpointEpayToken, EndpointEpayCryptopay, EndpointEpayCardAuth,
		EndpointEpayCharge, EndpointEpayCancel, EndpointEpayRefund, EndpointEpayStatus:
		return bank.Epay
	case EndpointFlittCheckout, EndpointFlittDirect, EndpointFlittRecurring,
		EndpointFlittCapture, EndpointFlittReverse, EndpointFlittStatus, EndpointFlittStep2:
		return bank.Flitt
	}
	return bank.Any
}

// EndpointsFor возвращает endpoint-ы выбранного банка (для UI dropdown с фильтрацией).
// Возвращает Wildcard первым элементом.
func EndpointsFor(b bank.Bank) []string {
	if b == bank.Any {
		return AllEndpoints
	}
	out := []string{Wildcard}
	for _, ep := range AllEndpoints {
		if ep == Wildcard {
			continue
		}
		if EndpointBank(ep) == b {
			out = append(out, ep)
		}
	}
	return out
}

// Scenario — одно правило.
type Scenario struct {
	ID          string
	Bank        bank.Bank // фильтр по банку. Пусто = любой.
	Endpoint    string
	PaymentID   string
	OrderID     string
	MerchantID  string
	Action      Action
	Params      map[string]string
	ConsumeOnce bool
	HitCount    int
	CreatedAt   time.Time
}

// MatchInput — параметры запроса для матчинга.
type MatchInput struct {
	Bank       bank.Bank
	Endpoint   string
	PaymentID  string
	OrderID    string
	MerchantID string
}

// Matches возвращает true, если правило совпадает с запросом.
func (s *Scenario) Matches(in MatchInput) bool {
	return matchBank(s.Bank, in.Bank) &&
		matchValue(s.Endpoint, in.Endpoint) &&
		matchValue(s.PaymentID, in.PaymentID) &&
		matchValue(s.OrderID, in.OrderID) &&
		matchValue(s.MerchantID, in.MerchantID)
}

func matchBank(rule, actual bank.Bank) bool {
	// bank.Any в правиле = wildcard. bank.Any в запросе = "не указано" → тоже совпадает.
	if rule == bank.Any || actual == bank.Any {
		return true
	}
	return rule == actual
}

func matchValue(rule, actual string) bool {
	if rule == "" || rule == Wildcard {
		return true
	}
	return rule == actual
}
