package epay

import "testing"

func TestTokenStore_IssueAndLookup(t *testing.T) {
	s := NewTokenStore()
	t1 := s.Issue("client", "000123", "5000", "uuid-1")
	if t1.AccessToken == "" {
		t.Fatal("AccessToken should not be empty")
	}
	got := s.Lookup(t1.AccessToken)
	if got == nil || got.AccessToken != t1.AccessToken {
		t.Errorf("Lookup should return issued token, got %+v", got)
	}
	if got.ClientID != "client" {
		t.Errorf("ClientID = %s, want %s", got.ClientID, "client")
	}
}

func TestTokenStore_IssueUniqueTokens(t *testing.T) {
	s := NewTokenStore()
	a := s.Issue("c", "i", "1", "t")
	b := s.Issue("c", "i", "1", "t")
	if a.AccessToken == b.AccessToken {
		t.Error("each Issue should produce a unique token")
	}
}

func TestTokenStore_LookupPermissive(t *testing.T) {
	s := NewTokenStore()
	// Unknown token → permissive stub (not nil).
	got := s.Lookup("unknown-token")
	if got == nil {
		t.Error("Lookup for unknown should return permissive stub, not nil")
	}
}

func TestTokenStore_LookupEmpty(t *testing.T) {
	s := NewTokenStore()
	if got := s.Lookup(""); got != nil {
		t.Error("empty token should return nil (no permissive stub)")
	}
}
