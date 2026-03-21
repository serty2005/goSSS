package libfptr

import "testing"

func TestVariantFromVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		want    Variant
		wantErr bool
	}{
		{name: "ветка 10.8", version: "10.8.3.0", want: Variant108},
		{name: "ветка 10.9", version: "10.9.0.0", want: Variant109},
		{name: "ветка 10.10", version: "10.10.8.0", want: Variant109},
		{name: "ошибка формата", version: "abc", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := VariantFromVersion(tc.version)
			if tc.wantErr {
				if err == nil {
					t.Fatal("ожидалась ошибка разбора версии")
				}
				return
			}
			if err != nil {
				t.Fatalf("VariantFromVersion вернул ошибку: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ожидалась ветка %q, получено %q", tc.want, got)
			}
		})
	}
}
