package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
)

type BitrixHandler struct {
	service services.BitrixSyncService
}

func NewBitrixHandler(service services.BitrixSyncService) *BitrixHandler {
	return &BitrixHandler{service: service}
}

func (h *BitrixHandler) RegisterRoutes(r chi.Router) {
	r.Get("/service-points", h.ListServicePoints)
	r.Post("/service-points/refresh", h.RefreshServicePoints)
	r.Post("/service-points/import/preview", h.PreviewServicePointsImport)
	r.Post("/service-points/import/sync-preview", h.PreviewServicePointsSync)
	r.Post("/service-points/import/apply", h.ImportServicePoints)
	r.Post("/sync/pull", h.PullSync)
}

func (h *BitrixHandler) ListServicePoints(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListServicePoints(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *BitrixHandler) RefreshServicePoints(w http.ResponseWriter, r *http.Request) {
	count, err := h.service.RefreshServicePoints(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"count":  count,
	})
}

func (h *BitrixHandler) PullSync(w http.ResponseWriter, r *http.Request) {
	deals, comments, err := h.service.PullFromBitrix(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"deals_updated":     deals,
		"comments_imported": comments,
	})
}

func (h *BitrixHandler) PreviewServicePointsImport(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	preview, err := h.service.PreviewServicePointsImport(r.Context(), fileName, content)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, preview)
}

func (h *BitrixHandler) ImportServicePoints(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	mapping := services.ServicePointImportMapping{
		CodeColumn:     r.FormValue("code_column"),
		NameColumn:     r.FormValue("name_column"),
		ContractColumn: r.FormValue("contract_column"),
	}

	if mapping.CodeColumn == "" || mapping.NameColumn == "" || mapping.ContractColumn == "" {
		var payload services.ServicePointImportMapping
		if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr == nil {
			mapping = payload
		}
	}

	options := services.ServicePointSyncApplyOptions{
		SelectedRows: parseSelectedRows(r.FormValue("selected_rows")),
	}

	result, err := h.service.ImportServicePoints(r.Context(), fileName, content, mapping, options)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, result)
}

func (h *BitrixHandler) PreviewServicePointsSync(w http.ResponseWriter, r *http.Request) {
	fileName, content, err := extractImportFile(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	mapping := services.ServicePointImportMapping{
		CodeColumn:     r.FormValue("code_column"),
		NameColumn:     r.FormValue("name_column"),
		ContractColumn: r.FormValue("contract_column"),
	}

	preview, err := h.service.PreviewServicePointsSync(r.Context(), fileName, content, mapping)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, preview)
}

func extractImportFile(r *http.Request) (string, []byte, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return "", nil, fmt.Errorf("не удалось прочитать multipart-запрос: %w", err)
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		return "", nil, fmt.Errorf("не передан файл в поле file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", nil, fmt.Errorf("не удалось прочитать файл: %w", err)
	}
	if len(content) == 0 {
		return "", nil, fmt.Errorf("передан пустой файл")
	}

	return fileHeader.Filename, content, nil
}

func parseSelectedRows(raw string) []int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	var values []int
	if strings.HasPrefix(trimmed, "[") {
		var parsed []int
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			for _, row := range parsed {
				if row > 0 {
					values = append(values, row)
				}
			}
			return values
		}
	}

	parts := strings.Split(trimmed, ",")
	for _, part := range parts {
		row, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && row > 0 {
			values = append(values, row)
		}
	}
	if len(values) == 0 {
		return nil
	}
	return values
}
