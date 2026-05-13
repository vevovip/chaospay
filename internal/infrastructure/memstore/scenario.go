package memstore

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vevovip/chaospay/internal/domain/scenario"
)

// ScenarioStore — потокобезопасный список сценариев.
// Порядок добавления = порядок матчинга (первое совпадение выигрывает).
type ScenarioStore struct {
	mu        sync.RWMutex
	scenarios []*scenario.Scenario
	nextID    atomic.Uint64
}

// NewScenarioStore конструктор.
func NewScenarioStore() *ScenarioStore {
	return &ScenarioStore{}
}

// Add добавляет сценарий в конец списка.
func (s *ScenarioStore) Add(sc *scenario.Scenario) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc.ID == "" {
		sc.ID = fmt.Sprintf("sc-%d", s.nextID.Add(1))
	}
	if sc.CreatedAt.IsZero() {
		sc.CreatedAt = time.Now()
	}
	s.scenarios = append(s.scenarios, sc)
}

// Remove удаляет сценарий по ID.
func (s *ScenarioStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.scenarios[:0]
	for _, sc := range s.scenarios {
		if sc.ID != id {
			out = append(out, sc)
		}
	}
	s.scenarios = out
}

// Reset очищает store.
func (s *ScenarioStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scenarios = nil
}

// List возвращает копии (отсортированы по дате создания).
func (s *ScenarioStore) List() []*scenario.Scenario {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*scenario.Scenario, len(s.scenarios))
	for i, sc := range s.scenarios {
		cp := *sc
		out[i] = &cp
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Match возвращает первый подходящий сценарий, инкрементит HitCount, удаляет если ConsumeOnce.
func (s *ScenarioStore) Match(in scenario.MatchInput) *scenario.Scenario {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sc := range s.scenarios {
		if !sc.Matches(in) {
			continue
		}
		sc.HitCount++
		matched := *sc
		if sc.ConsumeOnce {
			s.scenarios = append(s.scenarios[:i], s.scenarios[i+1:]...)
		}
		return &matched
	}
	return nil
}
