// Package bank — типы и константы банков, эмулируемых моком.
//
// Bank — это namespace для группировки эндпоинтов, сценариев, платежей и записей лога.
// Используется в UI (вкладки), в Scenario-matcher (фильтрация) и в RequestLog (классификация).
//
// Добавление нового банка:
//
//  1. Завести константу здесь (Bank = "<slug>").
//  2. Добавить в All / Titles / RoutePrefixes.
//  3. Создать ports/api/<bank>/ с handler-ом, регистрирующим routes под этим префиксом.
//  4. Создать пресеты в application/scenario/service.go с Bank = <slug>.
package bank

import "strings"

// Bank — идентификатор эмулируемого банка.
type Bank string

// Все поддерживаемые банки.
const (
	// Any — wildcard. Сценарий с Bank=Any срабатывает в любом банк-контексте.
	// MatchInput.Bank=Any означает "не фильтровать".
	Any Bank = ""

	Freedom Bank = "freedom" // Freedom Pay XML + MD5
	Epay    Bank = "epay"    // Halyk Epay v2 JSON + OAuth2
	QR      Bank = "qr"      // FreedomQR
	Loyalty Bank = "loyalty" // Freedom Loyalty (cashback)
)

// All — порядок отображения в panel header.
var All = []Bank{Freedom, Epay, QR, Loyalty}

// Titles — человекочитаемые названия для UI.
var Titles = map[Bank]string{
	Freedom: "Freedom Pay",
	Epay:    "Halyk Epay",
	QR:      "FreedomQR",
	Loyalty: "Loyalty",
}

// RoutePrefixes — какие HTTP-пути относятся к какому банку (для авто-классификации
// в RequestLog). Первый матч выигрывает, поэтому более специфичные префиксы — выше.
var RoutePrefixes = []struct {
	Prefix string
	Bank   Bank
}{
	// Loyalty
	{"/authservice/", Loyalty},
	{"/loyaltyservice/", Loyalty},
	// QR-PAY
	{"/qr-code/", QR},
	// Halyk Epay v2 (см. ports/api/epay/handler.go)
	{"/oauth2/token", Epay},
	{"/epay/", Epay},
	// Freedom Pay XML — широкие префиксы, должны быть последними
	{"/v1/merchant/", Freedom},
	{"/v2/", Freedom},
	{"/customer/", Freedom},
	{"/pay/", Freedom}, // /pay/{paymentID}/pay — Apple/Google Pay через Freedom wallet
	{"/get_status3.php", Freedom},
	{"/do_capture.php", Freedom},
	{"/cancel.php", Freedom},
	{"/revoke.php", Freedom},
	{"/init_payment.php", Freedom},
}

// FromPath возвращает банк, к которому относится HTTP-путь.
// Если ничего не подошло — Any (для /health, /panel и т.п.).
func FromPath(path string) Bank {
	for _, rp := range RoutePrefixes {
		if strings.HasPrefix(path, rp.Prefix) {
			return rp.Bank
		}
	}
	return Any
}

// Valid проверяет, что строка соответствует одному из известных банков (включая Any).
func Valid(b Bank) bool {
	if b == Any {
		return true
	}
	for _, kb := range All {
		if b == kb {
			return true
		}
	}
	return false
}
