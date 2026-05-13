package scenario

import (
	"strconv"
	"testing"

	dscenario "github.com/vevovip/chaospay/internal/domain/scenario"
)

// fakeStore — простая реализация Store для unit-тестов сервиса (без зависимости на memstore).
type fakeStore struct {
	items []*dscenario.Scenario
}

func (f *fakeStore) Add(sc *dscenario.Scenario) {
	if sc.ID == "" {
		sc.ID = "sc-" + strconv.Itoa(len(f.items)+1)
	}
	f.items = append(f.items, sc)
}

func (f *fakeStore) Remove(id string) {
	out := f.items[:0]
	for _, sc := range f.items {
		if sc.ID != id {
			out = append(out, sc)
		}
	}
	f.items = out
}

func (f *fakeStore) Reset() { f.items = nil }

func (f *fakeStore) List() []*dscenario.Scenario {
	out := make([]*dscenario.Scenario, len(f.items))
	copy(out, f.items)
	return out
}

func (f *fakeStore) Match(in dscenario.MatchInput) *dscenario.Scenario {
	for _, sc := range f.items {
		if sc.Matches(in) {
			return sc
		}
	}
	return nil
}

// TestAllPresets_ApplyDoesNotPanic — гарантирует, что ApplyPreset не падает ни на одном PresetInfo.
// Каждый preset должен производить хотя бы один scenario.
func TestAllPresets_ApplyProducesScenarios(t *testing.T) {
	for _, info := range AllPresets {
		info := info
		t.Run(info.Name, func(t *testing.T) {
			store := &fakeStore{}
			s := NewService(store)
			s.ApplyPreset(info.Name)
			if len(store.items) == 0 {
				t.Errorf("preset %q did not add any scenarios — ApplyPreset case missing?", info.Name)
			}
		})
	}
}

func TestApplyPreset_Unknown_NoOp(t *testing.T) {
	store := &fakeStore{}
	s := NewService(store)
	s.ApplyPreset("nonexistent_preset")
	if len(store.items) != 0 {
		t.Errorf("Unknown preset should be no-op, got %d scenarios", len(store.items))
	}
}

func TestApplyPreset_EX1001_AddsTwoConsumeOnce(t *testing.T) {
	store := &fakeStore{}
	s := NewService(store)
	s.ApplyPreset("ex1001")

	if len(store.items) != 2 {
		t.Fatalf("ex1001 should add 2 scenarios, got %d", len(store.items))
	}
	first, second := store.items[0], store.items[1]
	if first.Endpoint != "direct" || first.Action != dscenario.ActionAmbiguousError {
		t.Errorf("first scenario wrong: %+v", first)
	}
	if second.Endpoint != "get_status3.php" || second.Action != dscenario.ActionForceStatus {
		t.Errorf("second scenario wrong: %+v", second)
	}
	for _, sc := range store.items {
		if !sc.ConsumeOnce {
			t.Errorf("ex1001 scenarios should be consume_once")
		}
	}
}

func TestApplyPreset_RetryExhausted_NotConsumeOnce(t *testing.T) {
	// retry-exhausted пресеты должны сработать на КАЖДОЙ из 3 retry-попыток PG.
	cases := []string{"init_retry_exhausted", "hold_init_retry_exhausted", "wallet_retry_exhausted", "context_deadline"}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			s := NewService(store)
			s.ApplyPreset(name)
			for _, sc := range store.items {
				if sc.ConsumeOnce {
					t.Errorf("retry preset %q must NOT be consume_once (matches every retry attempt)", name)
				}
			}
		})
	}
}

func TestApplyPreset_BusinessErrors_RealFreedomCodes(t *testing.T) {
	// Коды должны соответствовать PG error_mapping.go — иначе PG свалит всё в ErrDefault.
	cases := map[string]string{
		"insufficient_funds":      "10009",
		"card_declined":           "10007",
		"card_data_input":         "10005",
		"expired_card":            "10017",
		"3ds_failed":              "10004",
		"limit_exceeded":          "10006",
		"code_limit_exceeded":     "10003",
		"emitter_error":           "10001",
		"country_not_supported":   "10013",
		"transaction_amount_zero": "11016",
		"unknown_bank_error":      "9992",
		"default_bank_error":      "99999",
	}
	for name, wantCode := range cases {
		name := name
		wantCode := wantCode
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			s := NewService(store)
			s.ApplyPreset(name)
			if len(store.items) != 1 {
				t.Fatalf("business preset should produce 1 scenario, got %d", len(store.items))
			}
			sc := store.items[0]
			if sc.Action != dscenario.ActionForceFailure {
				t.Errorf("action = %s, want force_failure", sc.Action)
			}
			if sc.Params["error_code"] != wantCode {
				t.Errorf("error_code = %s, want %s", sc.Params["error_code"], wantCode)
			}
		})
	}
}

func TestApplyPreset_RecoveryFlows_TwoPhase(t *testing.T) {
	// Recovery flows: фаза 1 (на endpoint X) + фаза 2 (на get_status3.php).
	cases := []struct {
		name, phase1Endpoint, phase2Endpoint string
		phase2Status                         string
	}{
		{"hold_pending_recovery", "direct", "get_status3.php", "success"},
		{"capture_failed_status_approved", "do_capture.php", "get_status3.php", "success"},
		{"cancel_failed_status_revoked", "cancel.php", "get_status3.php", "revoked"},
		{"revoke_failed_status_revoked", "revoke.php", "get_status3.php", "revoked"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			s := NewService(store)
			s.ApplyPreset(tc.name)
			if len(store.items) != 2 {
				t.Fatalf("%s should add 2 scenarios, got %d", tc.name, len(store.items))
			}
			if store.items[0].Endpoint != tc.phase1Endpoint {
				t.Errorf("phase1 endpoint = %s, want %s", store.items[0].Endpoint, tc.phase1Endpoint)
			}
			if store.items[1].Endpoint != tc.phase2Endpoint {
				t.Errorf("phase2 endpoint = %s, want %s", store.items[1].Endpoint, tc.phase2Endpoint)
			}
			if store.items[1].Params["payment_status"] != tc.phase2Status {
				t.Errorf("phase2 payment_status = %s, want %s", store.items[1].Params["payment_status"], tc.phase2Status)
			}
		})
	}
}

func TestAllPresets_NoDuplicateNames(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range AllPresets {
		if seen[p.Name] {
			t.Errorf("Duplicate preset name: %q", p.Name)
		}
		seen[p.Name] = true
	}
}

func TestAllPresets_HaveSamples(t *testing.T) {
	// Все пресеты должны иметь Sample для UI ❔.
	for _, p := range AllPresets {
		if p.Sample == "" {
			t.Errorf("preset %q is missing Sample (UI ❔ won't show)", p.Name)
		}
	}
}
