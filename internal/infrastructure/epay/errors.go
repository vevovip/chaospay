package epay

import "strconv"

// ErrorClass — категория ошибки Halyk Epay.
// Соответствует common.Err* на стороне PG (см. internal/infrastructure/clients/payments/epay_2/error_mapping.go).
type ErrorClass string

// Категории ошибок (1-в-1 с PG-маппингом).
const (
	ErrCardDataInput           ErrorClass = "card_data_input"            // "неверные данные карты"
	ErrDeclinedByIssuer        ErrorClass = "declined_by_issuer"         // "платёж отклонён эмитентом"
	ErrUnknown                 ErrorClass = "unknown"                    // "не ожидаемая ошибка"
	ErrNotEnoughMoney          ErrorClass = "not_enough_money"           // "недостаточно средств"
	ErrCardExpired             ErrorClass = "card_expired"               // "карта истекла"
	ErrCardLimitationsExceeded ErrorClass = "card_limitations_exceeded"  // "превышены лимиты карты"
	ErrTransactionAmountIsZero ErrorClass = "transaction_amount_is_zero" // "сумма платежа должна быть больше 0"
	ErrEmitter                 ErrorClass = "emitter"                    // "ошибка на стороне банка-эмитента"
	ErrDefault                 ErrorClass = "default"                    // системная / неизвестная
)

// ErrorInfo — описание ошибки для тестов и UI пресетов.
type ErrorInfo struct {
	Code    int        // reasonCode из real Halyk
	Class   ErrorClass // в какой common.Err* PG её замаппит
	Message string     // что выдаётся в .reason / .message
}

// reasonCodeMap — выборка реальных reason-кодов Halyk Epay. Полный список
// уходит за пределы мока — добавляем коды по мере появления тестовых кейсов.
//
// Источник: internal/infrastructure/clients/payments/epay_2/error_mapping.go в PG.
var reasonCodeMap = map[int]ErrorClass{
	// Card data input
	457: ErrCardDataInput, 492: ErrCardDataInput, 473: ErrCardDataInput, 499: ErrCardDataInput,
	469: ErrCardDataInput, 471: ErrCardDataInput, 472: ErrCardDataInput, 501: ErrCardDataInput,

	// Declined by issuer
	455: ErrDeclinedByIssuer, 456: ErrDeclinedByIssuer, 462: ErrDeclinedByIssuer,
	463: ErrDeclinedByIssuer, 466: ErrDeclinedByIssuer, 468: ErrDeclinedByIssuer,
	487: ErrDeclinedByIssuer, 490: ErrDeclinedByIssuer, 521: ErrDeclinedByIssuer,
	523: ErrDeclinedByIssuer, 527: ErrDeclinedByIssuer,

	// Not enough money
	484: ErrNotEnoughMoney,

	// Card expired
	478: ErrCardExpired, 485: ErrCardExpired,

	// Card limitations exceeded
	486: ErrCardLimitationsExceeded, 488: ErrCardLimitationsExceeded,
	491: ErrCardLimitationsExceeded, 528: ErrCardLimitationsExceeded,
	529: ErrCardLimitationsExceeded,

	// Transaction amount is zero
	470: ErrTransactionAmountIsZero,

	// Emitter
	493: ErrEmitter, 494: ErrEmitter,

	// Unknown
	477: ErrUnknown, 522: ErrUnknown,
}

// Classify возвращает класс ошибки по reasonCode. Неизвестные → ErrDefault.
func Classify(reasonCode int) ErrorClass {
	if cls, ok := reasonCodeMap[reasonCode]; ok {
		return cls
	}
	return ErrDefault
}

// DefaultMessage — текст по умолчанию, который выдаёт мок при reasonCode.
// Реальные сообщения от Halyk на русском, но для нас стабильность строки
// важнее точности — PG-классификация идёт по reasonCode, не по message.
func DefaultMessage(reasonCode int) string {
	switch Classify(reasonCode) {
	case ErrCardDataInput:
		return "Invalid card data"
	case ErrDeclinedByIssuer:
		return "Declined by issuer"
	case ErrNotEnoughMoney:
		return "Insufficient funds"
	case ErrCardExpired:
		return "Card expired"
	case ErrCardLimitationsExceeded:
		return "Card limitations exceeded"
	case ErrTransactionAmountIsZero:
		return "Transaction amount is zero"
	case ErrEmitter:
		return "Emitter bank connection error"
	case ErrUnknown:
		return "Unknown bank error"
	}
	return "code=" + strconv.Itoa(reasonCode)
}
