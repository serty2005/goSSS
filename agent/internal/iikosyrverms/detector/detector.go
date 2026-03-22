package detector

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/registry"
)

func Detect(ctx context.Context, roots []registry.AppDataRoot, products []registry.ProductDefinition) ([]domain.KnownPathStatus, []domain.Candidate, error) {
	knownPaths := make([]domain.KnownPathStatus, 0, len(roots)*len(products)*4)
	candidates := make([]domain.Candidate, 0, len(roots)*len(products))

	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		for _, product := range products {
			candidate, statuses := detectProduct(root, product)
			knownPaths = append(knownPaths, statuses...)
			if candidate == nil {
				continue
			}
			candidates = append(candidates, *candidate)
		}
	}

	slices.SortFunc(knownPaths, func(a, b domain.KnownPathStatus) int {
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})

	return knownPaths, candidates, nil
}

func detectProduct(root registry.AppDataRoot, product registry.ProductDefinition) (*domain.Candidate, []domain.KnownPathStatus) {
	statuses := make([]domain.KnownPathStatus, 0, len(product.KnownPaths))
	signals := make([]domain.ActivitySignal, 0, len(product.KnownPaths))
	configFiles := make([]string, 0, 2)
	seenStatuses := make(map[string]struct{}, len(product.KnownPaths))
	seenSignals := make(map[string]struct{}, len(product.KnownPaths))
	seenConfigs := make(map[string]struct{}, 2)

	for _, knownPath := range product.KnownPaths {
		fullPath := filepath.Clean(filepath.Join(root.Path, knownPath.RelativePath))
		normalizedPath := strings.ToLower(fullPath)
		info, err := os.Stat(fullPath)
		exists := err == nil
		if _, seen := seenStatuses[normalizedPath]; !seen {
			statuses = append(statuses, domain.KnownPathStatus{
				SoftwareType: product.SoftwareType,
				Path:         fullPath,
				Kind:         knownPath.Kind,
				Exists:       exists,
			})
			seenStatuses[normalizedPath] = struct{}{}
		}
		if !exists {
			continue
		}

		if knownPath.UseForActivity {
			if _, seen := seenSignals[normalizedPath]; !seen {
				signals = append(signals, domain.ActivitySignal{
					Path:      fullPath,
					Kind:      knownPath.Kind,
					UpdatedAt: info.ModTime(),
				})
				seenSignals[normalizedPath] = struct{}{}
			}
		}
		if knownPath.UseForConfig && !info.IsDir() {
			if _, seen := seenConfigs[normalizedPath]; !seen {
				configFiles = append(configFiles, fullPath)
				seenConfigs[normalizedPath] = struct{}{}
			}
		}
	}

	if len(signals) == 0 {
		return nil, statuses
	}

	slices.SortFunc(signals, func(a, b domain.ActivitySignal) int {
		switch {
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		default:
			return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
		}
	})

	return &domain.Candidate{
		SoftwareType:    product.SoftwareType,
		AppDataRoot:     root.Path,
		AppDataPriority: root.Priority,
		RootPath:        filepath.Clean(filepath.Join(root.Path, product.RootRelativePath)),
		ActivityPath:    signals[0].Path,
		ActivityAt:      signals[0].UpdatedAt,
		ConfigFiles:     slices.Clip(configFiles),
		ActivitySignals: slices.Clip(signals),
	}, statuses
}
