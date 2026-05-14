package epay

import (
	"strconv"
	"strings"
)

// MinInvoiceLen — минимальная длина invoiceID, которую ожидает Halyk (паддинг нулями).
const MinInvoiceLen = 6

// FormatInvoice превращает OrderID в invoiceId Halyk-формата: zero-padded до 6 знаков.
//
//	FormatInvoice(123)       → "000123"
//	FormatInvoice(16050955)  → "16050955"
func FormatInvoice(orderID uint) string {
	if orderID == 0 {
		return ""
	}
	s := strconv.FormatUint(uint64(orderID), 10)
	if len(s) >= MinInvoiceLen {
		return s
	}
	return strings.Repeat("0", MinInvoiceLen-len(s)) + s
}

// ParseInvoice возвращает OrderID из invoiceId (strip ведущих нулей).
// 0 на пустой/невалидной строке.
func ParseInvoice(invoiceID string) uint {
	if invoiceID == "" {
		return 0
	}
	trimmed := strings.TrimLeft(invoiceID, "0")
	if trimmed == "" {
		return 0
	}
	n, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}

// BearerFromHeader извлекает access_token из "Authorization: Bearer <token>".
// Возвращает "" если не Bearer или пусто.
func BearerFromHeader(authHeader string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	return strings.TrimSpace(authHeader[len(prefix):])
}
