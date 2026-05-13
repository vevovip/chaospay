package freedompay

import (
	"strings"
	"testing"
)

func TestOrdMap_SetGet(t *testing.T) {
	m := OrdMap{}
	m = m.Set("a", "1").Set("b", "2").Set("c", "3")

	if got, _ := m.Get("a"); got != "1" {
		t.Errorf("Get(a) = %v, want 1", got)
	}
	if got, _ := m.Get("b"); got != "2" {
		t.Errorf("Get(b) = %v, want 2", got)
	}
	if _, ok := m.Get("missing"); ok {
		t.Errorf("Get(missing) should return ok=false")
	}
}

func TestOrdMap_SetUpdatesExisting(t *testing.T) {
	m := OrdMap{}
	m = m.Set("a", "1").Set("a", "2")

	if got, _ := m.Get("a"); got != "2" {
		t.Errorf("Get(a) after re-set = %v, want 2", got)
	}
	if len(m) != 1 {
		t.Errorf("len = %d, want 1 (Set should update, not append)", len(m))
	}
}

func TestOrdMap_Delete(t *testing.T) {
	m := OrdMap{}.Set("a", "1").Set("b", "2").Set("c", "3")
	m = m.Delete("b")

	if len(m) != 2 {
		t.Errorf("len after delete = %d, want 2", len(m))
	}
	if _, ok := m.Get("b"); ok {
		t.Errorf("Get(b) should be missing after Delete")
	}
	if got, _ := m.Get("a"); got != "1" {
		t.Errorf("a should be preserved")
	}
	if got, _ := m.Get("c"); got != "3" {
		t.Errorf("c should be preserved")
	}
}

func TestOrdMap_DeleteMissing(t *testing.T) {
	m := OrdMap{}.Set("a", "1")
	m2 := m.Delete("nonexistent")
	if len(m2) != 1 {
		t.Errorf("Delete of missing key should not change map")
	}
}

func TestOrdMap_WithoutKey(t *testing.T) {
	m := OrdMap{}.Set("a", "1").Set("b", "2").Set("a", "1") // a updated, not duplicated
	m2 := m.WithoutKey("a")

	if len(m2) != 1 {
		t.Errorf("WithoutKey len = %d, want 1", len(m2))
	}
	if _, ok := m2.Get("a"); ok {
		t.Errorf("a should be removed by WithoutKey")
	}
	// Original must remain intact
	if _, ok := m.Get("a"); !ok {
		t.Errorf("WithoutKey should not mutate original")
	}
}

func TestOrdMap_SortedKeys(t *testing.T) {
	m := OrdMap{}.Set("c", "3").Set("a", "1").Set("b", "2")
	keys := m.SortedKeys()
	expected := []string{"a", "b", "c"}
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Errorf("SortedKeys = %v, want %v", keys, expected)
	}
}

func TestSign_Deterministic(t *testing.T) {
	m := OrdMap{}.Set("pg_amount", "100").Set("pg_order_id", "abc")
	sig1 := Sign("init_payment.php", m, "secret")
	sig2 := Sign("init_payment.php", m, "secret")
	if sig1 != sig2 {
		t.Errorf("Sign not deterministic: %s vs %s", sig1, sig2)
	}
	if len(sig1) != 32 {
		t.Errorf("MD5 must be 32 hex chars, got %d", len(sig1))
	}
}

func TestSign_DifferentSecrets(t *testing.T) {
	m := OrdMap{}.Set("pg_amount", "100")
	a := Sign("script", m, "secret1")
	b := Sign("script", m, "secret2")
	if a == b {
		t.Errorf("Different secrets must yield different sigs")
	}
}

func TestSign_OrderIndependent(t *testing.T) {
	// Сортировка по ключам должна давать одинаковую подпись независимо от порядка добавления.
	m1 := OrdMap{}.Set("a", "1").Set("b", "2").Set("c", "3")
	m2 := OrdMap{}.Set("c", "3").Set("a", "1").Set("b", "2")
	if Sign("x", m1, "sec") != Sign("x", m2, "sec") {
		t.Errorf("Sign should be order-independent (sorts by key)")
	}
}

func TestVerify_RoundTrip(t *testing.T) {
	m := OrdMap{}.Set("pg_amount", "100").Set("pg_order_id", "abc")
	sig := Sign("init_payment.php", m, "secret")
	m = m.Set("pg_sig", sig)

	_, ok := Verify("init_payment.php", m, "secret", sig)
	if !ok {
		t.Errorf("Verify should accept valid signature")
	}
}

func TestVerify_WrongSig(t *testing.T) {
	m := OrdMap{}.Set("pg_amount", "100").Set("pg_sig", "wrong")
	_, ok := Verify("init_payment.php", m, "secret", "wrong")
	if ok {
		t.Errorf("Verify should reject wrong signature")
	}
}

func TestVerify_WrongScript(t *testing.T) {
	m := OrdMap{}.Set("pg_amount", "100")
	sig := Sign("init_payment.php", m, "secret")
	_, ok := Verify("different_script", m.Set("pg_sig", sig), "secret", sig)
	if ok {
		t.Errorf("Verify should reject when scriptName differs")
	}
}

func TestVerify_ExcludesPgSig(t *testing.T) {
	// Verify должен исключать pg_sig из расчёта; иначе самореферентно не сойдётся.
	m := OrdMap{}.Set("pg_amount", "100")
	expected := Sign("script", m, "secret")
	m = m.Set("pg_sig", expected)
	got, ok := Verify("script", m, "secret", expected)
	if !ok {
		t.Errorf("Verify failed: expected=%s got=%s", expected, got)
	}
}

func TestGenerateSalt_Length(t *testing.T) {
	if got := GenerateSalt(8); len(got) != 8 {
		t.Errorf("GenerateSalt(8) len = %d, want 8", len(got))
	}
	if got := GenerateSalt(0); len(got) != SaltLength {
		t.Errorf("GenerateSalt(0) must use default SaltLength=%d, got %d", SaltLength, len(got))
	}
}

func TestGenerateSalt_Alphabet(t *testing.T) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for i := 0; i < 20; i++ {
		s := GenerateSalt(8)
		for _, c := range s {
			if !strings.ContainsRune(alphabet, c) {
				t.Errorf("GenerateSalt produced char %q outside [a-zA-Z]: %q", c, s)
			}
		}
	}
}

func TestReplaceTagValue(t *testing.T) {
	body := `<response><pg_status>ok</pg_status><pg_sig>aaa</pg_sig></response>`
	got := ReplaceTagValue(body, "pg_sig", "00000")
	want := `<response><pg_status>ok</pg_status><pg_sig>00000</pg_sig></response>`
	if got != want {
		t.Errorf("ReplaceTagValue:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestReplaceTagValue_MissingTag(t *testing.T) {
	body := `<response><pg_status>ok</pg_status></response>`
	got := ReplaceTagValue(body, "missing", "X")
	if got != body {
		t.Errorf("ReplaceTagValue of missing tag should be no-op")
	}
}

func TestRemoveTag(t *testing.T) {
	body := `<response><pg_status>ok</pg_status><pg_sig>aaa</pg_sig><pg_salt>xx</pg_salt></response>`
	got := RemoveTag(body, "pg_sig")
	want := `<response><pg_status>ok</pg_status><pg_salt>xx</pg_salt></response>`
	if got != want {
		t.Errorf("RemoveTag:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestRemoveTag_MissingTag(t *testing.T) {
	body := `<response><pg_status>ok</pg_status></response>`
	got := RemoveTag(body, "missing")
	if got != body {
		t.Errorf("RemoveTag of missing tag should be no-op")
	}
}

func TestRenderResponse_Order(t *testing.T) {
	m := OrdMap{}.Set("pg_status", "ok").Set("pg_payment_id", "123").Set("pg_sig", "abc")
	got := RenderResponse("response", m)
	// Поля должны идти в порядке Set, не отсортированном
	if !strings.Contains(got, "<pg_status>ok</pg_status>") {
		t.Errorf("RenderResponse missing pg_status: %s", got)
	}
	if !strings.Contains(got, "<pg_payment_id>123</pg_payment_id>") {
		t.Errorf("RenderResponse missing pg_payment_id: %s", got)
	}
	idxStatus := strings.Index(got, "pg_status")
	idxPayID := strings.Index(got, "pg_payment_id")
	if idxStatus > idxPayID {
		t.Errorf("RenderResponse should preserve insertion order (pg_status before pg_payment_id)")
	}
}

func TestParseRequestXML_RoundTrip(t *testing.T) {
	xml := `<?xml version="1.0"?><request><pg_merchant_id>100</pg_merchant_id><pg_amount>500</pg_amount></request>`
	req, err := ParseRequestXML(xml)
	if err != nil {
		t.Fatalf("ParseRequestXML failed: %v", err)
	}
	if v := req.Get("pg_merchant_id", ""); v != "100" {
		t.Errorf("pg_merchant_id = %q, want 100", v)
	}
	if v := req.Get("pg_amount", ""); v != "500" {
		t.Errorf("pg_amount = %q, want 500", v)
	}
	if v := req.Get("pg_missing", "default"); v != "default" {
		t.Errorf("Get with default should return default for missing key")
	}
}

func TestParseRequestXML_MalformedReturnsError(t *testing.T) {
	_, err := ParseRequestXML(`<<<NOT_XML>>>`)
	if err == nil {
		t.Errorf("ParseRequestXML should reject malformed XML")
	}
}
