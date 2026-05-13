// Package freedompay реализует подпись и сериализацию запросов/ответов Freedom Pay.
//
// Подпись 1-в-1 повторяет алгоритм PG SDK (pkg/freedompay/freedompay.go::GetSignature):
//
//	md5( scriptName + ";" + values_in_sorted_key_order + ";" + secretKey )
//
// scriptName может быть пустой строкой — тогда не включается в подпись (так у hold/holdinit ответов).
// Для вложенных карт ([]OrdMap) рекурсивно собираем значения по отсортированным ключам.
package freedompay

import (
	"crypto/md5" //nolint:gosec // freedompay использует md5 в подписи
	"crypto/rand"
	"fmt"
	"math/big"
	"sort"
	"strings"
)

const saltAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// SaltLength — длина соли по умолчанию.
const SaltLength = 8

// OrdMap — упорядоченный список пар. Значения: string или []OrdMap.
type OrdMap []OrdPair

// OrdPair — одна пара ключ/значение.
type OrdPair struct {
	Key   string
	Value any
}

// Set добавляет пару в конец, либо обновляет значение существующего ключа.
func (m OrdMap) Set(key string, value any) OrdMap {
	for i := range m {
		if m[i].Key == key {
			m[i].Value = value
			return m
		}
	}
	return append(m, OrdPair{Key: key, Value: value})
}

// Get возвращает значение по ключу.
func (m OrdMap) Get(key string) (any, bool) {
	for _, p := range m {
		if p.Key == key {
			return p.Value, true
		}
	}
	return nil, false
}

// Delete удаляет первое вхождение ключа. Возвращает новый OrdMap.
func (m OrdMap) Delete(key string) OrdMap {
	for i := range m {
		if m[i].Key == key {
			return append(m[:i], m[i+1:]...)
		}
	}
	return m
}

// SortedKeys возвращает ключи в алфавитном порядке.
func (m OrdMap) SortedKeys() []string {
	keys := make([]string, 0, len(m))
	for _, p := range m {
		keys = append(keys, p.Key)
	}
	sort.Strings(keys)
	return keys
}

// WithoutKey возвращает копию без указанного ключа.
func (m OrdMap) WithoutKey(key string) OrdMap {
	out := make(OrdMap, 0, len(m))
	for _, p := range m {
		if p.Key == key {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Sign возвращает MD5-подпись по тем же правилам, что и FreedomPay SDK на стороне PG.
func Sign(scriptName string, m OrdMap, secretKey string) string {
	parts := []string{}
	if scriptName != "" {
		parts = append(parts, scriptName)
	}
	parts = append(parts, collectValues(m)...)
	parts = append(parts, secretKey)

	sum := md5.Sum([]byte(strings.Join(parts, ";"))) //nolint:gosec
	return fmt.Sprintf("%x", sum[:])
}

// Verify проверяет подпись запроса. Возвращает (expected, ok).
func Verify(scriptName string, m OrdMap, secretKey, signature string) (string, bool) {
	expected := Sign(scriptName, m.WithoutKey("pg_sig"), secretKey)
	return expected, expected == signature
}

func collectValues(v any) []string {
	switch val := v.(type) {
	case OrdMap:
		var out []string
		for _, k := range val.SortedKeys() {
			child, _ := val.Get(k)
			out = append(out, collectValues(child)...)
		}
		return out
	case []OrdMap:
		var out []string
		for _, child := range val {
			out = append(out, collectValues(child)...)
		}
		return out
	case string:
		return []string{val}
	case []byte:
		return []string{string(val)}
	default:
		// несовместимый тип — приводим к строке через %v.
		return []string{fmt.Sprintf("%v", val)}
	}
}

// GenerateSalt — `length`-символьная соль из [a-zA-Z]. По умолчанию 8 (как RandomStrGenerator в SDK).
func GenerateSalt(length int) string {
	if length <= 0 {
		length = SaltLength
	}
	out := make([]byte, length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(saltAlphabet))))
		if err != nil {
			out[i] = saltAlphabet[i%len(saltAlphabet)]
			continue
		}
		out[i] = saltAlphabet[n.Int64()]
	}
	return string(out)
}
