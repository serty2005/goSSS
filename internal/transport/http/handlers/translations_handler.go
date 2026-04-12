package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"slices"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const globalTranslationsStorageKey = "global_translations"

type TranslationsHandler struct {
	db *gorm.DB
}

func NewTranslationsHandler(db *gorm.DB) *TranslationsHandler {
	return &TranslationsHandler{db: db}
}

func (h *TranslationsHandler) GetCatalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.loadCatalog(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить каталог переводов")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, catalog)
}

func (h *TranslationsHandler) UpdateCatalog(w http.ResponseWriter, r *http.Request) {
	var dto api.GlobalTranslationsDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	normalized := normalizeGlobalTranslationsDTO(dto)
	payload, err := json.Marshal(normalized)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный каталог переводов")
		return
	}

	record := models.AppLocalization{}
	err = h.db.WithContext(r.Context()).
		Where("key = ?", globalTranslationsStorageKey).
		First(&record).
		Error
	if err != nil && err != gorm.ErrRecordNotFound {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить каталог переводов")
		return
	}

	record.Key = globalTranslationsStorageKey
	record.Payload = datatypes.JSON(payload)

	if record.ID == 0 {
		if err := h.db.WithContext(r.Context()).Create(&record).Error; err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось сохранить каталог переводов")
			return
		}
	} else if err := h.db.WithContext(r.Context()).Save(&record).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось сохранить каталог переводов")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, normalized)
}

func (h *TranslationsHandler) loadCatalog(ctx context.Context) (api.GlobalTranslationsDTO, error) {
	record := models.AppLocalization{}
	err := h.db.WithContext(ctx).
		Where("key = ?", globalTranslationsStorageKey).
		First(&record).
		Error
	if err == gorm.ErrRecordNotFound {
		return defaultGlobalTranslationsDTO(), nil
	}
	if err != nil {
		return api.GlobalTranslationsDTO{}, err
	}

	var dto api.GlobalTranslationsDTO
	if err := json.Unmarshal(record.Payload, &dto); err != nil {
		return defaultGlobalTranslationsDTO(), nil
	}

	return normalizeGlobalTranslationsDTO(dto), nil
}

func defaultGlobalTranslationsDTO() api.GlobalTranslationsDTO {
	return api.GlobalTranslationsDTO{
		Locales: []api.GlobalTranslationLocaleDTO{
			{
				Code:       "en",
				Label:      "English",
				NativeLabel: "English",
				IsBuiltin:  true,
			},
			{
				Code:       "ru",
				Label:      "Russian",
				NativeLabel: "Русский",
				IsBuiltin:  true,
			},
		},
		Overrides: map[string]map[string]map[string]string{},
	}
}

func normalizeGlobalTranslationsDTO(dto api.GlobalTranslationsDTO) api.GlobalTranslationsDTO {
	builtinLocales := map[string]api.GlobalTranslationLocaleDTO{
		"en": {
			Code:       "en",
			Label:      "English",
			NativeLabel: "English",
			IsBuiltin:  true,
		},
		"ru": {
			Code:       "ru",
			Label:      "Russian",
			NativeLabel: "Русский",
			IsBuiltin:  true,
		},
	}

	localesByCode := map[string]api.GlobalTranslationLocaleDTO{
		"en": builtinLocales["en"],
		"ru": builtinLocales["ru"],
	}

	for _, locale := range dto.Locales {
		code := normalizeLocaleCode(locale.Code)
		if code == "" {
			continue
		}
		if builtin, ok := builtinLocales[code]; ok {
			localesByCode[code] = builtin
			continue
		}

		label := strings.TrimSpace(locale.Label)
		if label == "" {
			label = strings.ToUpper(code)
		}
		nativeLabel := strings.TrimSpace(locale.NativeLabel)
		if nativeLabel == "" {
			nativeLabel = label
		}

		localesByCode[code] = api.GlobalTranslationLocaleDTO{
			Code:        code,
			Label:       label,
			NativeLabel: nativeLabel,
			IsBuiltin:   false,
		}
	}

	overrides := map[string]map[string]map[string]string{}
	for localeCode, namespaceMap := range dto.Overrides {
		code := normalizeLocaleCode(localeCode)
		if code == "" {
			continue
		}

		normalizedNamespaces := map[string]map[string]string{}
		for namespace, entries := range namespaceMap {
			normalizedNamespace := strings.TrimSpace(namespace)
			if normalizedNamespace == "" {
				continue
			}

			normalizedEntries := map[string]string{}
			for key, value := range entries {
				normalizedKey := strings.TrimSpace(key)
				normalizedValue := strings.TrimSpace(value)
				if normalizedKey == "" || normalizedValue == "" {
					continue
				}
				normalizedEntries[normalizedKey] = normalizedValue
			}

			if len(normalizedEntries) > 0 {
				normalizedNamespaces[normalizedNamespace] = normalizedEntries
			}
		}

		if len(normalizedNamespaces) == 0 {
			continue
		}

		if _, ok := localesByCode[code]; !ok {
			label := strings.ToUpper(code)
			localesByCode[code] = api.GlobalTranslationLocaleDTO{
				Code:        code,
				Label:       label,
				NativeLabel: label,
				IsBuiltin:   false,
			}
		}

		overrides[code] = normalizedNamespaces
	}

	customCodes := make([]string, 0, len(localesByCode))
	for code, locale := range localesByCode {
		if locale.IsBuiltin {
			continue
		}
		customCodes = append(customCodes, code)
	}
	slices.Sort(customCodes)

	locales := []api.GlobalTranslationLocaleDTO{
		builtinLocales["en"],
		builtinLocales["ru"],
	}
	for _, code := range customCodes {
		locales = append(locales, localesByCode[code])
	}

	return api.GlobalTranslationsDTO{
		Locales:   locales,
		Overrides: overrides,
	}
}

func normalizeLocaleCode(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	return normalized
}
