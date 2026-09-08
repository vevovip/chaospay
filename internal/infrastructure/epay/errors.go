package epay

import "strconv"

// ErrorClass — категория ошибки Halyk Epay.
// Соответствует common.Err* на стороне PG (см. internal/infrastructure/clients/payments/epay_2/error_mapping.go).
type ErrorClass string

// Категории ошибок (1-в-1 с PG-маппингом).
const (
	ErrCardDataInput           ErrorClass = "card_data_input"         // неверные данные карты
	ErrDeclinedByIssuer        ErrorClass = "declined_by_issuer"      // платёж отклонён эмитентом
	ErrUnknown                 ErrorClass = "unknown"                 // сбой на стороне эквайера
	ErrNotEnoughMoney          ErrorClass = "insufficient_funds"      // недостаточно средств
	ErrCardExpired             ErrorClass = "card_expired"            // срок действия карты истёк
	ErrCardLimitationsExceeded ErrorClass = "card_limits_exceeded"    // превышены лимиты карты
	ErrTransactionAmountIsZero ErrorClass = "transaction_amount_zero" // сумма операции равна нулю
	ErrEmitter                 ErrorClass = "emitter_error"           // эмитент недоступен
	ErrSecure3DFailed          ErrorClass = "secure3d_failed"         // проверка 3D-Secure не пройдена
	ErrFraudSuspected          ErrorClass = "fraud_suspected"         // подозрение на мошенничество
	ErrAttemptLimitExceeded    ErrorClass = "attempt_limit_exceeded"  // исчерпаны попытки оплаты
	ErrCountryNotSupported     ErrorClass = "country_not_supported"   // карта страны не принимается
	ErrAcquirerTimeout         ErrorClass = "acquirer_timeout"        // платёжная система не ответила
	ErrDefault                 ErrorClass = "default"                 // нераспознанный код
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
	// неверные данные карты
	457: ErrCardDataInput, 459: ErrCardDataInput, 469: ErrCardDataInput,
	471: ErrCardDataInput, 472: ErrCardDataInput, 501: ErrCardDataInput, 3339: ErrCardDataInput,

	// проверка 3D-Secure не пройдена
	455: ErrSecure3DFailed, 473: ErrSecure3DFailed, 499: ErrSecure3DFailed,
	500: ErrSecure3DFailed, 503: ErrSecure3DFailed, 877: ErrSecure3DFailed,

	// отказ эмитента
	462: ErrDeclinedByIssuer, 463: ErrDeclinedByIssuer, 464: ErrDeclinedByIssuer,
	465: ErrDeclinedByIssuer, 466: ErrDeclinedByIssuer, 467: ErrDeclinedByIssuer,
	468: ErrDeclinedByIssuer, 479: ErrDeclinedByIssuer, 480: ErrDeclinedByIssuer,
	481: ErrDeclinedByIssuer, 482: ErrDeclinedByIssuer, 487: ErrDeclinedByIssuer,
	489: ErrDeclinedByIssuer, 490: ErrDeclinedByIssuer, 495: ErrDeclinedByIssuer,
	498: ErrDeclinedByIssuer, 521: ErrDeclinedByIssuer, 523: ErrDeclinedByIssuer,
	524: ErrDeclinedByIssuer, 525: ErrDeclinedByIssuer, 527: ErrDeclinedByIssuer,

	// подозрение на мошенничество
	483: ErrFraudSuspected,

	// недостаточно средств
	484: ErrNotEnoughMoney,

	// срок действия карты истёк
	478: ErrCardExpired, 485: ErrCardExpired,

	// превышены лимиты карты
	486: ErrCardLimitationsExceeded, 488: ErrCardLimitationsExceeded,
	491: ErrCardLimitationsExceeded, 528: ErrCardLimitationsExceeded,
	529: ErrCardLimitationsExceeded, 2678: ErrCardLimitationsExceeded,

	// исчерпаны попытки оплаты
	476: ErrAttemptLimitExceeded, 492: ErrAttemptLimitExceeded, 2740: ErrAttemptLimitExceeded,

	// карта выпущена в стране, оплата из которой запрещена
	1612: ErrCountryNotSupported,

	// сумма операции равна нулю
	470: ErrTransactionAmountIsZero,

	// эмитент недоступен
	493: ErrEmitter, 494: ErrEmitter, 1267: ErrEmitter, 1563: ErrEmitter, 1611: ErrEmitter,

	// платёжная система не ответила вовремя
	458: ErrAcquirerTimeout, 497: ErrAcquirerTimeout, 502: ErrAcquirerTimeout,

	// сбой на стороне эквайера
	456: ErrUnknown, 460: ErrUnknown, 461: ErrUnknown, 477: ErrUnknown,
	496: ErrUnknown, 522: ErrUnknown, 526: ErrUnknown,
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
	case ErrSecure3DFailed:
		return "3D Secure verification failed"
	case ErrFraudSuspected:
		return "Declined by security service"
	case ErrAttemptLimitExceeded:
		return "Payment attempt limit exceeded"
	case ErrCountryNotSupported:
		return "Card country is not supported"
	case ErrAcquirerTimeout:
		return "Payment system timeout"
	case ErrUnknown:
		return "Unknown bank error"
	}
	return "code=" + strconv.Itoa(reasonCode)
}
