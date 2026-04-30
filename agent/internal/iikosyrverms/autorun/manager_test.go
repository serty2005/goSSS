package autorun

import (
	"testing"

	"etalon-agent/internal/iikosyrverms/domain"
)

func TestMatchFrontTargetDetectsIikoAndSyrve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path         string
		matches      bool
		softwareType domain.SoftwareType
	}{
		{path: `C:\Program Files\iiko\iikoRMS\Front.Net\iikoFront.Net.exe`, matches: true, softwareType: domain.SoftwareTypeIiko},
		{path: `C:\Program Files\Syrve\Front.Net\Front.Net.exe`, matches: true, softwareType: domain.SoftwareTypeSyrve},
		{path: `C:\Windows\System32\notepad.exe`, matches: false, softwareType: domain.SoftwareTypeUnknown},
	}

	for _, tc := range cases {
		matches, softwareType := matchFrontTarget(tc.path)
		if matches != tc.matches {
			t.Fatalf("для пути %q ожидался matches_front=%v, получено %v", tc.path, tc.matches, matches)
		}
		if softwareType != tc.softwareType {
			t.Fatalf("для пути %q ожидался software_type=%q, получено %q", tc.path, tc.softwareType, softwareType)
		}
	}
}
