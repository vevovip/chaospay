package scenario

import (
	"testing"
)

func TestMatches_Wildcard(t *testing.T) {
	sc := &Scenario{
		Endpoint:   Wildcard,
		PaymentID:  Wildcard,
		OrderID:    Wildcard,
		MerchantID: Wildcard,
	}
	if !sc.Matches(MatchInput{Endpoint: "direct", PaymentID: "1", OrderID: "2", MerchantID: "3"}) {
		t.Errorf("Wildcard should match anything")
	}
	if !sc.Matches(MatchInput{}) {
		t.Errorf("Wildcard should match empty MatchInput")
	}
}

func TestMatches_EmptyRuleEqualsWildcard(t *testing.T) {
	// Пустая строка в правиле должна работать как wildcard (см. matchValue в scenario.go).
	sc := &Scenario{Endpoint: "", PaymentID: "", OrderID: "", MerchantID: ""}
	if !sc.Matches(MatchInput{Endpoint: "anything"}) {
		t.Errorf("Empty rule should be treated as wildcard")
	}
}

func TestMatches_SpecificEndpoint(t *testing.T) {
	sc := &Scenario{Endpoint: "direct", PaymentID: Wildcard, OrderID: Wildcard, MerchantID: Wildcard}
	if !sc.Matches(MatchInput{Endpoint: "direct"}) {
		t.Errorf("Should match exact endpoint")
	}
	if sc.Matches(MatchInput{Endpoint: "init"}) {
		t.Errorf("Should NOT match different endpoint")
	}
}

func TestMatches_SpecificPaymentID(t *testing.T) {
	sc := &Scenario{Endpoint: Wildcard, PaymentID: "999", OrderID: Wildcard, MerchantID: Wildcard}
	if !sc.Matches(MatchInput{Endpoint: "x", PaymentID: "999"}) {
		t.Errorf("Should match exact paymentID")
	}
	if sc.Matches(MatchInput{Endpoint: "x", PaymentID: "1000"}) {
		t.Errorf("Should NOT match different paymentID")
	}
}

func TestMatches_CombinedFilters(t *testing.T) {
	sc := &Scenario{
		Endpoint:   "direct",
		PaymentID:  Wildcard,
		OrderID:    "42",
		MerchantID: "100001",
	}
	// All match
	if !sc.Matches(MatchInput{Endpoint: "direct", PaymentID: "anything", OrderID: "42", MerchantID: "100001"}) {
		t.Errorf("Should match when endpoint+order+merchant align")
	}
	// One mismatch → no match
	if sc.Matches(MatchInput{Endpoint: "direct", OrderID: "43", MerchantID: "100001"}) {
		t.Errorf("Should NOT match when OrderID differs")
	}
}

func TestAllActions_NoDuplicates(t *testing.T) {
	seen := map[Action]bool{}
	for _, a := range AllActions {
		if seen[a] {
			t.Errorf("Duplicate action in AllActions: %s", a)
		}
		seen[a] = true
	}
}

func TestAllEndpoints_HasWildcardFirst(t *testing.T) {
	if len(AllEndpoints) == 0 || AllEndpoints[0] != Wildcard {
		t.Errorf("AllEndpoints should start with Wildcard for UI dropdown default")
	}
}

func TestParam_DefaultWhenMissing(t *testing.T) {
	sc := &Scenario{Params: map[string]string{"key": "value"}}
	if got := Param(sc, "key", "def"); got != "value" {
		t.Errorf("Param(key) = %q, want value", got)
	}
	if got := Param(sc, "missing", "def"); got != "def" {
		t.Errorf("Param(missing) = %q, want def", got)
	}
}

func TestParam_NilScenario(t *testing.T) {
	if got := Param(nil, "key", "def"); got != "def" {
		t.Errorf("Param(nil scenario) should return default")
	}
}

func TestParam_EmptyValueReturnsDefault(t *testing.T) {
	sc := &Scenario{Params: map[string]string{"key": ""}}
	if got := Param(sc, "key", "def"); got != "def" {
		t.Errorf("Empty param value should fall back to default")
	}
}

func TestParamInt_Default(t *testing.T) {
	if got := ParamInt(nil, "k", 42); got != 42 {
		t.Errorf("ParamInt(nil) should return default")
	}
}

func TestParamInt_Parse(t *testing.T) {
	sc := &Scenario{Params: map[string]string{"n": "15"}}
	if got := ParamInt(sc, "n", 0); got != 15 {
		t.Errorf("ParamInt(n) = %d, want 15", got)
	}
}

func TestParamInt_InvalidFallsBackToDefault(t *testing.T) {
	sc := &Scenario{Params: map[string]string{"n": "not-a-number"}}
	if got := ParamInt(sc, "n", 99); got != 99 {
		t.Errorf("ParamInt with non-numeric should return default")
	}
}
