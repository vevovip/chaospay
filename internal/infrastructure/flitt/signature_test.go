package flitt

import (
	"crypto/sha1" //nolint:gosec
	"fmt"
	"strings"
	"testing"
)

// referenceSign — независимая реализация подписи 1-в-1 с
// pkg/flitt/flitt.go::createSignature. Используется как «золотой» эталон в тестах,
// чтобы любое случайное изменение в Sign() было замечено.
func referenceSign(secret string, params map[string]any) string {
	keys := []string{}
	for k := range params {
		if k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	// sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	values := []string{}
	for _, k := range keys {
		s := valueToString(params[k])
		if s != "" {
			values = append(values, s)
		}
	}
	raw := secret + "|" + strings.Join(values, "|")
	sum := sha1.Sum([]byte(raw)) //nolint:gosec
	return fmt.Sprintf("%x", sum[:])
}

func TestSign_VectorsMatchReference(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		secret string
		params map[string]any
	}{
		{
			name:   "empty params",
			secret: "test",
			params: map[string]any{},
		},
		{
			name:   "single field",
			secret: "test",
			params: map[string]any{"order_id": "12345"},
		},
		{
			name:   "checkout url request",
			secret: "test",
			params: map[string]any{
				"order_id":    "100500",
				"amount":      5000,
				"currency":    "KZT",
				"order_desc":  "оплата заказа № 100500",
				"merchant_id": 1549901,
			},
		},
		{
			name:   "skip empty values",
			secret: "test",
			params: map[string]any{
				"order_id":     "1",
				"amount":       100,
				"currency":     "KZT",
				"merchant_id":  1549901,
				"sender_email": "",
				"language":     "",
			},
		},
		{
			name:   "ignore signature field",
			secret: "test",
			params: map[string]any{
				"order_id":  "1",
				"amount":    100,
				"currency":  "KZT",
				"signature": "should-be-ignored",
			},
		},
		{
			name:   "bool Y/N",
			secret: "test",
			params: map[string]any{
				"order_id": "1",
				"preauth":  true,
				"amount":   200,
				"currency": "KZT",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Sign(tc.secret, tc.params)
			want := referenceSign(tc.secret, tc.params)
			if got != want {
				t.Fatalf("Sign(%q) = %s, want %s", tc.name, got, want)
			}
			if len(got) != 40 {
				t.Fatalf("expected 40 hex chars (SHA1), got %d", len(got))
			}
		})
	}
}

func TestSign_DeterministicAndOrderIndependent(t *testing.T) {
	t.Parallel()
	a := Sign("test", map[string]any{"order_id": "1", "amount": 100, "currency": "KZT"})
	b := Sign("test", map[string]any{"currency": "KZT", "amount": 100, "order_id": "1"})
	if a != b {
		t.Fatalf("key order changes signature: %s vs %s", a, b)
	}
}

func TestSign_SecretChangesOutput(t *testing.T) {
	t.Parallel()
	params := map[string]any{"order_id": "1", "amount": 100}
	a := Sign("test", params)
	b := Sign("other", params)
	if a == b {
		t.Fatalf("different secret should yield different signature")
	}
}

func TestValueToString_AllTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{42, "42"},
		{int64(9223372036854775807), "9223372036854775807"},
		{3.14, "3.14"},
		{true, "Y"},
		{false, "N"},
		{nil, ""},
		{[]byte("ignored"), ""},
	}
	for _, tc := range cases {
		if got := valueToString(tc.in); got != tc.want {
			t.Fatalf("valueToString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
