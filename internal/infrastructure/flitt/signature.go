// Package flitt содержит DTO, signature-функции и таблицу тестовых карт Flitt.
//
// Контракт API совпадает с тем, что отправляет/ожидает Flitt SDK
// (см. pkg/flitt/* в payment-gateway-new): SHA1 от конкатенации secret и
// non-empty значений отсортированных по ключу.
package flitt

import (
	"crypto/sha1" //nolint:gosec // Flitt использует SHA1 по протоколу
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Sign возвращает подпись по правилам Flitt:
//
//	sha1( secret + "|" + value1 + "|" + value2 + ... )
//
// где value-ы — это все непустые значения карты, отсортированные по ключу
// (поле `signature` пропускается). Реализация 1-в-1 с
// `pkg/flitt/flitt.go::createSignature` в PG-SDK.
func Sign(secret string, params map[string]any) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	values := make([]string, 0, len(keys))
	for _, k := range keys {
		v, ok := params[k]
		if !ok {
			continue
		}
		s := valueToString(v)
		if s == "" {
			continue
		}
		values = append(values, s)
	}

	raw := secret + "|" + strings.Join(values, "|")
	sum := sha1.Sum([]byte(raw)) //nolint:gosec
	return fmt.Sprintf("%x", sum[:])
}

// valueToString приводит значение к строке тем же способом, что Flitt SDK.
// Поддерживает string / int / int64 / float64 / bool. Прочие типы → "".
func valueToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "Y"
		}
		return "N"
	}
	return ""
}
