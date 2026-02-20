package handlers

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/pkg/xlsx"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

type ReportHandler struct {
	db *gorm.DB
}

func NewReportHandler(db *gorm.DB) *ReportHandler {
	return &ReportHandler{db: db}
}

func (h *ReportHandler) RegisterRoutes(r chi.Router) {
	r.Get("/reports/companies/contracts", h.CompaniesContracts)
	r.Get("/reports/companies/contracts/export", h.ExportCompaniesContracts)
}

type companiesContractsReportRow struct {
	CompanyID             string  `json:"company_id"`
	CompanyTitle          string  `json:"company_title"`
	CompanyParentTitle    *string `json:"company_parent_title,omitempty"`
	CompanyContractStatus string  `json:"company_contract_status"`
	ContractID            *string `json:"contract_id,omitempty"`
	ContractType          *string `json:"contract_type,omitempty"`
	ContractState         *string `json:"contract_state,omitempty"`
}

type contractSelectionRow struct {
	ContractID    *string `gorm:"column:contract_id"`
	ContractType  *string `gorm:"column:contract_type"`
	ContractState *string `gorm:"column:contract_state"`
}

type reportFilters struct {
	Statuses      []string
	ContractTypes []string
	CompanyIDs    []string
	SearchTerms   []string
}

func (h *ReportHandler) CompaniesContracts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.getCompaniesContractsReport(r.URL.Query())
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось сформировать отчет по компаниям и контрактам", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось сформировать отчет")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, rows)
}

func (h *ReportHandler) ExportCompaniesContracts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.getCompaniesContractsReport(r.URL.Query())
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось сформировать xlsx отчета по компаниям и контрактам", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось сформировать отчет")
		return
	}

	headers := []string{
		"ID компании",
		"Компания",
		"Родительская компания",
		"Статус компании",
		"ID контракта",
		"Тип контракта",
		"Статус контракта",
	}

	dataRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		dataRows = append(dataRows, []string{
			row.CompanyID,
			row.CompanyTitle,
			safeString(row.CompanyParentTitle),
			row.CompanyContractStatus,
			safeString(row.ContractID),
			safeString(row.ContractType),
			safeString(row.ContractState),
		})
	}

	fileBytes, err := xlsx.BuildWorkbook("Компании и контракты", headers, dataRows)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось упаковать отчет в xlsx", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось сформировать xlsx")
		return
	}

	fileName := "report-companies-contracts-" + time.Now().Format("20060102-150405") + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(fileBytes)
}

func (h *ReportHandler) getCompaniesContractsReport(params url.Values) ([]companiesContractsReportRow, error) {
	filters := parseReportFilters(params)

	contractQuery := h.db.Table("contracts AS c").
		Select(`c.id AS contract_id, NULLIF(BTRIM(c.services->>0), '') AS contract_type, c.state AS contract_state`).
		Joins("JOIN company_contracts cc ON cc.contract_id = c.id").
		Where("cc.company_id = companies.id").
		Order("(c.state = 'active') DESC").
		Order("c.updated_at DESC").
		Limit(1)

	query := h.db.Table("companies").
		Select(`
			companies.id AS company_id,
			COALESCE(NULLIF(BTRIM(companies.title), ''), companies.id) AS company_title,
			parent.title AS company_parent_title,
			CASE WHEN companies.active_contract = true THEN 'active' ELSE 'inactive' END AS company_contract_status,
			latest.contract_id,
			latest.contract_type,
			latest.contract_state
		`).
		Joins("LEFT JOIN companies AS parent ON parent.id = companies.parent_id").
		Joins("LEFT JOIN LATERAL (?) AS latest ON true", contractQuery)

	if len(filters.CompanyIDs) > 0 {
		query = query.Where("companies.id IN ?", filters.CompanyIDs)
	}

	if len(filters.ContractTypes) > 0 {
		query = query.Where("COALESCE(latest.contract_type, '') IN ?", filters.ContractTypes)
	}

	if len(filters.Statuses) > 0 {
		hasWithoutContract := slices.Contains(filters.Statuses, "without_contract")
		statuses := make([]string, 0, len(filters.Statuses))
		for _, status := range filters.Statuses {
			if status != "without_contract" {
				statuses = append(statuses, status)
			}
		}

		switch {
		case hasWithoutContract && len(statuses) > 0:
			query = query.Where("latest.contract_id IS NULL OR latest.contract_state IN ?", statuses)
		case hasWithoutContract:
			query = query.Where("latest.contract_id IS NULL")
		default:
			query = query.Where("latest.contract_state IN ?", statuses)
		}
	}

	for _, rawTerm := range filters.SearchTerms {
		term := strings.TrimSpace(rawTerm)
		if term == "" {
			continue
		}

		companyLike := "%" + strings.ToLower(term) + "%"
		query = query.Where(`
			(
				LOWER(COALESCE(latest.contract_id, '')) = LOWER(?)
				OR LOWER(COALESCE(NULLIF(BTRIM(companies.title), ''), companies.id)) LIKE ?
				OR LOWER(COALESCE(companies.additional_name, '')) LIKE ?
			)
		`, term, companyLike, companyLike)
	}

	query = query.Order("company_title ASC")

	var rows []companiesContractsReportRow
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	return rows, nil
}

func parseReportFilters(params url.Values) reportFilters {
	return reportFilters{
		Statuses:      parseCSVParams(params, "statuses"),
		ContractTypes: parseCSVParams(params, "contract_types"),
		CompanyIDs:    parseCSVParams(params, "company_ids"),
		SearchTerms:   parseCSVParams(params, "search_terms"),
	}
}

func parseCSVParams(params url.Values, key string) []string {
	rawValues := params[key]
	result := make([]string, 0, len(rawValues))

	for _, value := range rawValues {
		for _, part := range strings.Split(value, ",") {
			normalized := strings.TrimSpace(part)
			if normalized == "" {
				continue
			}
			result = append(result, normalized)
		}
	}

	return result
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
