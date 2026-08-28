package config

import "testing"

func TestParseMerchants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want map[uint]string
	}{
		{
			name: "два кабинета",
			raw:  "554415:secret-old,587055:secret-new",
			want: map[uint]string{554415: "secret-old", 587055: "secret-new"},
		},
		{
			name: "пробелы вокруг пар",
			raw:  " 554415 : secret-old , 587055 : secret-new ",
			want: map[uint]string{554415: "secret-old", 587055: "secret-new"},
		},
		{
			name: "пустая строка",
			raw:  "",
			want: map[uint]string{},
		},
		{
			name: "битые пары пропускаются",
			raw:  "no-colon,abc:secret,587055:secret-new",
			want: map[uint]string{587055: "secret-new"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseMerchants(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("получено %d кабинетов, ожидалось %d: %v", len(got), len(tt.want), got)
			}
			for id, secret := range tt.want {
				if got[id] != secret {
					t.Errorf("кабинет %d: секрет %q, ожидался %q", id, got[id], secret)
				}
			}
		})
	}
}

func TestSecretForUnknownCabinetFallsBackToDefault(t *testing.T) {
	t.Parallel()

	cfg := Config{
		MerchantID: 554415,
		Secret:     "default-secret",
		Merchants:  map[uint]string{554415: "default-secret", 587055: "new-secret"},
	}

	if got := cfg.SecretFor(587055); got != "new-secret" {
		t.Errorf("SecretFor(587055) = %q, ожидался new-secret", got)
	}
	if got := cfg.SecretFor(999999); got != "default-secret" {
		t.Errorf("SecretFor(999999) = %q, ожидался default-secret", got)
	}
	if cfg.KnownMerchant(999999) {
		t.Error("KnownMerchant(999999) = true, ожидалось false")
	}
}

func TestMerchantIDsSorted(t *testing.T) {
	t.Parallel()

	cfg := Config{Merchants: map[uint]string{587055: "b", 554415: "a", 554692: "c"}}

	got := cfg.MerchantIDs()
	want := []uint{554415, 554692, 587055}
	if len(got) != len(want) {
		t.Fatalf("получено %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("получено %v, ожидалось %v", got, want)
		}
	}
}
