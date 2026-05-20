package flitt

import "testing"

func TestCardBehavior_KnownCards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pan         string
		wantOutcome CardOutcome
		want3DS     bool
	}{
		{"4444555566661111", OutcomeApproved3DS, true},
		{"4444111166665555", OutcomeDeclined3DS, true},
		{"4444555511116666", OutcomeApproved, false},
		{"5555666644441111", OutcomeApproved3DS, true},
		{"4444555566669999", OutcomeApproved, false},
		{"4444666655559999", OutcomeApproved3DS, true},
		{"4444999966665555", OutcomeDeclined, false},
		{"4444666699995555", OutcomeDeclined3DS, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.pan, func(t *testing.T) {
			t.Parallel()
			gotOutcome, got3DS := CardBehavior(tc.pan)
			if gotOutcome != tc.wantOutcome {
				t.Fatalf("outcome: got %s, want %s", gotOutcome, tc.wantOutcome)
			}
			if got3DS != tc.want3DS {
				t.Fatalf("3DS: got %v, want %v", got3DS, tc.want3DS)
			}
		})
	}
}

func TestCardBehavior_UnknownCardFallsBack(t *testing.T) {
	t.Parallel()
	outcome, needs3DS := CardBehavior("0000000000000000")
	if outcome != OutcomeUnknownCard {
		t.Fatalf("expected OutcomeUnknownCard, got %s", outcome)
	}
	if needs3DS {
		t.Fatalf("unknown card should not require 3DS")
	}
}

func TestTestCards_AllUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for _, c := range TestCards {
		if seen[c.PAN] {
			t.Fatalf("duplicate PAN: %s", c.PAN)
		}
		seen[c.PAN] = true
	}
}

func TestDefaults(t *testing.T) {
	t.Parallel()
	if DefaultTestMerchantID != 1549901 {
		t.Fatalf("DefaultTestMerchantID mismatch: %d", DefaultTestMerchantID)
	}
	if DefaultTestSecret != "test" {
		t.Fatalf("DefaultTestSecret mismatch: %q", DefaultTestSecret)
	}
}
