package pay

import (
	"testing"
	"time"
)

func TestRecord_EpayHidden(t *testing.T) {
	t.Parallel()

	visibleAt := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		visibleAt time.Time
		now       time.Time
		want      bool
	}{
		{"без срока видна всегда", time.Time{}, visibleAt, false},
		{"до срока скрыта", visibleAt, visibleAt.Add(-time.Second), true},
		{"в момент срока видна", visibleAt, visibleAt, false},
		{"после срока видна без reveal", visibleAt, visibleAt.Add(time.Second), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &Record{EpayVisibleAt: tt.visibleAt}
			if got := rec.EpayHidden(tt.now); got != tt.want {
				t.Fatalf("EpayHidden = %v, want %v", got, tt.want)
			}
		})
	}
}
