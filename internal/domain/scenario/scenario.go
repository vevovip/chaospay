// Package scenario описывает правила имитации ответов банка.
package scenario

import "time"

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
	// Transport-level: моделируют сетевые/протокольные сбои до того, как PG распарсит ответ.
	ActionConnectionReset Action = "connection_reset" // мгновенный TCP-reset без ответа
	ActionEmptyResponse   Action = "empty_response"   // 200 OK, тело пустое
	ActionMalformedBody   Action = "malformed_body"   // 200 OK, тело — мусор (param: body)
	ActionSlowBody        Action = "slow_body"        // headers ушли, тело — по байту с задержкой (param: chunk_delay_ms)
	ActionWrongStatusCode Action = "wrong_status_code"
	// Content-level: валидный XML/JSON, но с искажёнными бизнес-полями.
	ActionWrongPaymentID Action = "wrong_payment_id" // подмена pg_payment_id (param: payment_id)
	ActionWrongAmount    Action = "wrong_amount"     // подмена pg_amount (param: amount)
	ActionMissingField   Action = "missing_field"    // удалить указанное поле (param: field)
	ActionExtraGarbage   Action = "extra_garbage"    // добавить мусорные поля (param: count)
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
	ActionConnectionReset,
	ActionEmptyResponse,
	ActionMalformedBody,
	ActionSlowBody,
	ActionWrongStatusCode,
	ActionWrongPaymentID,
	ActionWrongAmount,
	ActionMissingField,
	ActionExtraGarbage,
}

// Wildcard матчер — совпадает с любым значением.
const Wildcard = "*"

// AllEndpoints — для UI dropdown. Совпадают с scriptName входящих запросов.
var AllEndpoints = []string{
	Wildcard,
	"init",
	"direct",
	"get_status3.php",
	"do_capture.php",
	"cancel.php",
	"revoke.php",
	"init_payment.php",
	"add2",
	"remove",
	"applepay",
	"googlepay",
}

// Scenario — одно правило.
type Scenario struct {
	ID          string
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
	Endpoint   string
	PaymentID  string
	OrderID    string
	MerchantID string
}

// Matches возвращает true, если правило совпадает с запросом.
func (s *Scenario) Matches(in MatchInput) bool {
	return matchValue(s.Endpoint, in.Endpoint) &&
		matchValue(s.PaymentID, in.PaymentID) &&
		matchValue(s.OrderID, in.OrderID) &&
		matchValue(s.MerchantID, in.MerchantID)
}

func matchValue(rule, actual string) bool {
	if rule == "" || rule == Wildcard {
		return true
	}
	return rule == actual
}
