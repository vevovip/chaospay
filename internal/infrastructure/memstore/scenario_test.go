package memstore

import (
	"sync"
	"testing"

	"github.com/vevovip/chaospay/internal/domain/scenario"
)

func newSc(endpoint string, action scenario.Action, consumeOnce bool) *scenario.Scenario {
	return &scenario.Scenario{
		Endpoint:    endpoint,
		PaymentID:   scenario.Wildcard,
		OrderID:     scenario.Wildcard,
		MerchantID:  scenario.Wildcard,
		Action:      action,
		ConsumeOnce: consumeOnce,
	}
}

func TestScenarioStore_AddAssignsID(t *testing.T) {
	s := NewScenarioStore()
	sc := newSc("direct", scenario.ActionTimeout, true)
	s.Add(sc)
	if sc.ID == "" {
		t.Errorf("Add should assign ID")
	}
	if sc.CreatedAt.IsZero() {
		t.Errorf("Add should set CreatedAt")
	}
}

func TestScenarioStore_AddPreservesExistingID(t *testing.T) {
	s := NewScenarioStore()
	sc := newSc("direct", scenario.ActionTimeout, false)
	sc.ID = "custom-id"
	s.Add(sc)
	if sc.ID != "custom-id" {
		t.Errorf("Add should preserve pre-set ID")
	}
}

func TestScenarioStore_Match_FirstWins(t *testing.T) {
	s := NewScenarioStore()
	first := newSc("direct", scenario.ActionTimeout, false)
	second := newSc("direct", scenario.ActionForceFailure, false)
	s.Add(first)
	s.Add(second)

	matched := s.Match(scenario.MatchInput{Endpoint: "direct"})
	if matched == nil || matched.Action != scenario.ActionTimeout {
		t.Errorf("Match should return first added scenario, got %+v", matched)
	}
}

func TestScenarioStore_Match_IncrementsHitCount(t *testing.T) {
	s := NewScenarioStore()
	sc := newSc("direct", scenario.ActionTimeout, false)
	s.Add(sc)

	s.Match(scenario.MatchInput{Endpoint: "direct"})
	s.Match(scenario.MatchInput{Endpoint: "direct"})

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("Persistent scenario should stay, got %d", len(list))
	}
	if list[0].HitCount != 2 {
		t.Errorf("HitCount = %d, want 2", list[0].HitCount)
	}
}

func TestScenarioStore_Match_ConsumeOnce(t *testing.T) {
	s := NewScenarioStore()
	sc := newSc("direct", scenario.ActionTimeout, true)
	s.Add(sc)

	if m := s.Match(scenario.MatchInput{Endpoint: "direct"}); m == nil {
		t.Errorf("First Match should succeed")
	}
	if m := s.Match(scenario.MatchInput{Endpoint: "direct"}); m != nil {
		t.Errorf("Second Match after consume_once must return nil, got %+v", m)
	}
	if len(s.List()) != 0 {
		t.Errorf("consume_once should remove scenario from store")
	}
}

func TestScenarioStore_Match_NoMatch(t *testing.T) {
	s := NewScenarioStore()
	s.Add(newSc("direct", scenario.ActionTimeout, false))
	if m := s.Match(scenario.MatchInput{Endpoint: "init"}); m != nil {
		t.Errorf("Match should return nil for non-matching endpoint")
	}
}

func TestScenarioStore_Remove(t *testing.T) {
	s := NewScenarioStore()
	sc1 := newSc("a", scenario.ActionTimeout, false)
	sc2 := newSc("b", scenario.ActionTimeout, false)
	s.Add(sc1)
	s.Add(sc2)
	s.Remove(sc1.ID)
	list := s.List()
	if len(list) != 1 || list[0].ID != sc2.ID {
		t.Errorf("Remove failed: %+v", list)
	}
}

func TestScenarioStore_Reset(t *testing.T) {
	s := NewScenarioStore()
	s.Add(newSc("a", scenario.ActionTimeout, false))
	s.Add(newSc("b", scenario.ActionTimeout, false))
	s.Reset()
	if len(s.List()) != 0 {
		t.Errorf("Reset should clear store")
	}
}

func TestScenarioStore_List_ReturnsCopies(t *testing.T) {
	s := NewScenarioStore()
	sc := newSc("a", scenario.ActionTimeout, false)
	s.Add(sc)

	list := s.List()
	list[0].HitCount = 999

	// Original must be unaffected
	list2 := s.List()
	if list2[0].HitCount == 999 {
		t.Errorf("List should return copies — caller mutation leaked into store")
	}
}

func TestScenarioStore_ConcurrentAccess(t *testing.T) {
	// Sanity check for race detector: множественные goroutine'ы Add/Match/List должны быть безопасны.
	s := NewScenarioStore()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); s.Add(newSc("x", scenario.ActionTimeout, false)) }()
		go func() { defer wg.Done(); _ = s.Match(scenario.MatchInput{Endpoint: "x"}) }()
		go func() { defer wg.Done(); _ = s.List() }()
	}
	wg.Wait()
}
