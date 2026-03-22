package selector

import (
	"testing"
	"time"

	"etalon-agent/internal/iikosyrverms/domain"
)

func TestSelectReturnsFreshestCandidate(t *testing.T) {
	t.Parallel()

	older := domain.Candidate{
		SoftwareType: domain.SoftwareTypeIiko,
		ActivityPath: `C:\Users\demo\AppData\Roaming\iiko\cashserver\config.xml`,
		RootPath:     `C:\Users\demo\AppData\Roaming\iiko`,
		ActivityAt:   time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
	}
	newer := domain.Candidate{
		SoftwareType: domain.SoftwareTypeSyrve,
		ActivityPath: `C:\Users\demo\AppData\Roaming\syrve\CashServer\config.xml`,
		RootPath:     `C:\Users\demo\AppData\Roaming\syrve`,
		ActivityAt:   time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC),
	}

	selected, _, ok := Select([]domain.Candidate{older, newer})
	if !ok {
		t.Fatal("ожидался выбранный кандидат")
	}
	if selected.SoftwareType != domain.SoftwareTypeSyrve {
		t.Fatalf("ожидался software_type=syrve, получено %q", selected.SoftwareType)
	}
}
