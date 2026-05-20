package flitt

// CardOutcome — fallback-исход платежа без активного сценария.
//
// Применяется, когда мок принимает прямой платёж (direct/recurring) и нужно
// решить, что вернуть. Активный сценарий из panel перебивает эту таблицу.
type CardOutcome string

// Допустимые исходы.
const (
	OutcomeApproved          CardOutcome = "approved"           // без 3DS
	OutcomeApproved3DS       CardOutcome = "approved_3ds"       // 3DS-челлендж, после step2 → approved
	OutcomeDeclined          CardOutcome = "declined"           // отказ без 3DS
	OutcomeDeclined3DS       CardOutcome = "declined_3ds"       // 3DS-челлендж, после step2 → declined
	OutcomeInsufficientFunds CardOutcome = "insufficient_funds" // декларативная причина отказа
	OutcomeUnknownCard       CardOutcome = "unknown"            // неизвестная карта — fallback на approved для совместимости тестов
)

// TestCard описывает поведение одной тестовой карты.
type TestCard struct {
	PAN         string
	Outcome     CardOutcome
	Description string
}

// TestCards — таблица из docs.flitt.com/api/testing/.
// Любая тестовая карта = одобрение без 3DS, если в Outcome не указано иное.
var TestCards = []TestCard{
	// Базовые карты (3DS + одобрение/отказ)
	{PAN: "4444555566661111", Outcome: OutcomeApproved3DS, Description: "Visa с 3DS — одобрение"},
	{PAN: "4444111166665555", Outcome: OutcomeDeclined3DS, Description: "Visa с 3DS — отказ"},
	{PAN: "4444555511116666", Outcome: OutcomeApproved, Description: "Visa без 3DS — одобрение"},
	{PAN: "5555666644441111", Outcome: OutcomeApproved3DS, Description: "MasterCard с 3DS — одобрение"},

	// 3DS-сценарии (frictionless / challenge)
	{PAN: "4444555566669999", Outcome: OutcomeApproved, Description: "Frictionless 3DS — одобрение"},
	{PAN: "4444666655559999", Outcome: OutcomeApproved3DS, Description: "Challenge 3DS — одобрение"},
	{PAN: "4444999966665555", Outcome: OutcomeDeclined, Description: "Frictionless 3DS — отказ"},
	{PAN: "4444666699995555", Outcome: OutcomeDeclined3DS, Description: "Challenge 3DS — отказ"},
}

// CardBehavior возвращает (Outcome, needs3DS) по тестовому PAN-у.
// Если карта не из таблицы → OutcomeUnknownCard, без 3DS.
func CardBehavior(pan string) (CardOutcome, bool) {
	for _, c := range TestCards {
		if c.PAN == pan {
			switch c.Outcome {
			case OutcomeApproved3DS, OutcomeDeclined3DS:
				return c.Outcome, true
			default:
				return c.Outcome, false
			}
		}
	}
	return OutcomeUnknownCard, false
}

// DefaultTestMerchantID — merchant_id тестового мерчанта Flitt (из доки).
const DefaultTestMerchantID = 1549901

// DefaultTestSecret — secret_key тестового мерчанта Flitt (платежи).
const DefaultTestSecret = "test"
