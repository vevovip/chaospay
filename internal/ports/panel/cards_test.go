package panel

import (
	"strings"
	"testing"

	"github.com/vevovip/chaospay/internal/domain/bank"
)

func TestAmountFieldFor_Flitt(t *testing.T) {
	t.Parallel()
	label, selector := amountFieldFor(bank.Flitt)
	if label != "Amount" {
		t.Fatalf("label: got %q, want %q", label, "Amount")
	}
	if !strings.Contains(selector, `name="currency"`) {
		t.Fatalf("selector must contain currency field, got: %s", selector)
	}
	if !strings.Contains(selector, `value="GEL" selected`) {
		t.Fatalf("GEL must be default-selected: %s", selector)
	}
	if !strings.Contains(selector, `value="USD"`) {
		t.Fatalf("USD must be an option: %s", selector)
	}
}

func TestAmountFieldFor_Epay(t *testing.T) {
	t.Parallel()
	label, selector := amountFieldFor(bank.Epay)
	if label != "Amount (KZT)" {
		t.Fatalf("label: got %q", label)
	}
	if !strings.Contains(selector, `value="KZT"`) {
		t.Fatalf("KZT must be hidden value: %s", selector)
	}
	if strings.Contains(selector, `<select`) {
		t.Fatalf("Epay should not have currency selector: %s", selector)
	}
}

func TestAmountFieldFor_Freedom(t *testing.T) {
	t.Parallel()
	label, selector := amountFieldFor(bank.Freedom)
	if label != "Amount (KZT)" {
		t.Fatalf("label: got %q", label)
	}
	if !strings.Contains(selector, `value="KZT"`) {
		t.Fatalf("KZT must be hidden value: %s", selector)
	}
}
