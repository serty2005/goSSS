package drvfr

import "testing"

func TestDecodeLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantNil bool
	}{
		{
			name:  "распознанная лицензия",
			value: "0000000000000000FFFFFFFF",
			want:  "Подписка до 4 квартала 2027 года",
		},
		{
			name:    "слишком короткая строка",
			value:   "FFFF",
			wantNil: true,
		},
		{
			name:    "неизвестный код лицензии",
			value:   "0000000000000000ABCDEF",
			wantNil: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := decodeLicense(tc.value)
			if tc.wantNil {
				if got != "" {
					t.Fatalf("ожидалась пустая строка, получено %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("ожидалось %q, получено %q", tc.want, got)
			}
		})
	}
}
