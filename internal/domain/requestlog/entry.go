// Package requestlog описывает запись HTTP-запроса для UI журнала.
package requestlog

import "time"

// Entry — одна запись.
type Entry struct {
	ID           uint64
	At           time.Time
	Method       string
	URL          string
	Endpoint     string
	PaymentID    string
	OrderID      string
	MerchantID   string
	SignatureOK  bool
	ScenarioHit  string
	ScenarioName string
	DurationMS   int64
	StatusCode   int
	RequestBody  string
	ResponseBody string
}

// Truncate обрезает строку для UI.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}
