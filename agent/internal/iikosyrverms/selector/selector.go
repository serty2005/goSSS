package selector

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"etalon-agent/internal/iikosyrverms/domain"
)

func Select(candidates []domain.Candidate) (domain.Candidate, string, bool) {
	if len(candidates) == 0 {
		return domain.Candidate{}, "", false
	}

	ordered := slices.Clone(candidates)
	slices.SortFunc(ordered, func(a, b domain.Candidate) int {
		switch {
		case a.ActivityAt.After(b.ActivityAt):
			return -1
		case a.ActivityAt.Before(b.ActivityAt):
			return 1
		}
		if a.AppDataPriority != b.AppDataPriority {
			if a.AppDataPriority < b.AppDataPriority {
				return -1
			}
			return 1
		}
		if cmp := strings.Compare(strings.ToLower(a.ActivityPath), strings.ToLower(b.ActivityPath)); cmp != 0 {
			return cmp
		}
		return strings.Compare(strings.ToLower(a.RootPath), strings.ToLower(b.RootPath))
	})

	selected := ordered[0]
	reason := fmt.Sprintf(
		"Выбран самый свежий путь активности %q со временем %s",
		selected.ActivityPath,
		selected.ActivityAt.UTC().Format(time.RFC3339),
	)
	return selected, reason, true
}
