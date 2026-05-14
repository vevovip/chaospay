package epay

import "testing"

func TestFormatInvoice_Padding(t *testing.T) {
	cases := []struct {
		in   uint
		want string
	}{
		{0, ""},
		{1, "000001"},
		{123, "000123"},
		{999999, "999999"},
		{1000000, "1000000"},
		{16050955, "16050955"},
	}
	for _, c := range cases {
		if got := FormatInvoice(c.in); got != c.want {
			t.Errorf("FormatInvoice(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseInvoice_Roundtrip(t *testing.T) {
	cases := []uint{1, 123, 999999, 16050955}
	for _, n := range cases {
		s := FormatInvoice(n)
		got := ParseInvoice(s)
		if got != n {
			t.Errorf("ParseInvoice(%q) = %d, want %d", s, got, n)
		}
	}
}

func TestParseInvoice_Edge(t *testing.T) {
	if got := ParseInvoice(""); got != 0 {
		t.Errorf("empty → 0, got %d", got)
	}
	if got := ParseInvoice("000000"); got != 0 {
		t.Errorf("all zeros → 0, got %d", got)
	}
	if got := ParseInvoice("abc"); got != 0 {
		t.Errorf("non-numeric → 0, got %d", got)
	}
}

func TestBearerFromHeader(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"Basic xxx":        "",
		"Bearer abc123":    "abc123",
		"Bearer  spaced  ": "spaced",
		"Bearer ":          "",
	}
	for in, want := range cases {
		if got := BearerFromHeader(in); got != want {
			t.Errorf("BearerFromHeader(%q) = %q, want %q", in, got, want)
		}
	}
}
