package epay

import "testing"

func TestClassify_KnownCodes(t *testing.T) {
	cases := map[int]ErrorClass{
		484: ErrNotEnoughMoney,
		457: ErrCardDataInput,
		455: ErrDeclinedByIssuer,
		478: ErrCardExpired,
		486: ErrCardLimitationsExceeded,
		470: ErrTransactionAmountIsZero,
		493: ErrEmitter,
		477: ErrUnknown,
	}
	for code, want := range cases {
		if got := Classify(code); got != want {
			t.Errorf("Classify(%d) = %s, want %s", code, got, want)
		}
	}
}

func TestClassify_UnknownFallsToDefault(t *testing.T) {
	if got := Classify(99999); got != ErrDefault {
		t.Errorf("unknown code should fall back to ErrDefault, got %s", got)
	}
}

func TestDefaultMessage_KnownReturnsNonEmpty(t *testing.T) {
	for code := range reasonCodeMap {
		if msg := DefaultMessage(code); msg == "" {
			t.Errorf("DefaultMessage(%d) returned empty string", code)
		}
	}
}
