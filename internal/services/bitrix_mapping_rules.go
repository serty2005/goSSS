package services

import (
	"context"
	"strings"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/infra/config"
)

const bitrixTemporaryServicePointName = "Тестовая временная"

func normalizeBitrixServicePointName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	return strings.Join(strings.Fields(normalized), " ")
}

func isBitrixTemporaryServicePointName(name string) bool {
	return normalizeBitrixServicePointName(name) == normalizeBitrixServicePointName(bitrixTemporaryServicePointName)
}

func isBitrixTemporaryServicePoint(ctx context.Context, repo bitrix.Repository, pointID int64) (bool, error) {
	if repo == nil || pointID <= 0 {
		return false, nil
	}
	point, err := repo.GetServicePointByID(ctx, pointID)
	if err != nil || point == nil {
		return false, err
	}
	return isBitrixTemporaryServicePointName(point.Name), nil
}

func isBitrixTestCompanyID(cfg *config.Config, companyID int64) bool {
	if cfg == nil || companyID <= 0 {
		return false
	}
	for _, candidate := range cfg.BitrixTestCompanyIDs {
		if candidate == companyID {
			return true
		}
	}
	return false
}
