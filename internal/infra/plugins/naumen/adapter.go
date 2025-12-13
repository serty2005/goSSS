package naumen

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"fmt"
	"regexp"
	"strings"
)

var srokFnRegex = regexp.MustCompile(`(13|15|36)`)

// NaumenAdapter реализует интерфейсы провайдеров данных для Naumen ServiceDesk.
type NaumenAdapter struct {
	client    external.ExternalSystemClient
	logger    logger.LoggerInterface
	mapperCtx *external.MapperContext
}

// NewNaumenAdapter создает новый адаптер.
func NewNaumenAdapter(client external.ExternalSystemClient, logger logger.LoggerInterface, mapperCtx *external.MapperContext) *NaumenAdapter {
	return &NaumenAdapter{
		client:    client,
		logger:    logger,
		mapperCtx: mapperCtx,
	}
}

// SystemName возвращает имя системы.
func (a *NaumenAdapter) SystemName() string {
	return string(domain.SystemNaumen)
}

// --- Helper methods ---

func (a *NaumenAdapter) fetchSummaries(ctx context.Context, entityType string) ([]integration.EntitySummary, error) {
	rawList, err := a.client.FetchEntitySummaries(ctx, entityType)
	if err != nil {
		return nil, err
	}

	result := make([]integration.EntitySummary, 0, len(rawList))
	for _, item := range rawList {
		uuid, _ := item["UUID"].(string)
		lmdStr, _ := item["lastModifiedDate"].(string)

		if uuid == "" {
			continue
		}

		summary := integration.EntitySummary{
			ExternalID: uuid,
		}
		if lmd := utils.ParseServiceDeskTime(lmdStr); lmd != nil {
			summary.UpdatedAt = *lmd
		}
		result = append(result, summary)
	}
	return result, nil
}

// --- InventoryProvider Implementation ---

func (a *NaumenAdapter) GetCompanySummaries(ctx context.Context) ([]integration.EntitySummary, error) {
	return a.fetchSummaries(ctx, "Company")
}

func (a *NaumenAdapter) GetCompany(ctx context.Context, externalID string) (*company.Company, error) {
	data, err := a.client.FetchEntityDetails(ctx, externalID, "Company")
	if err != nil {
		return nil, err
	}
	return a.client.Mapper().DataToCompany(ctx, a.mapperCtx, data)
}

func (a *NaumenAdapter) GetServerSummaries(ctx context.Context) ([]integration.EntitySummary, error) {
	return a.fetchSummaries(ctx, "Server")
}

func (a *NaumenAdapter) GetServer(ctx context.Context, externalID string) (*server.Server, error) {
	data, err := a.client.FetchEntityDetails(ctx, externalID, "Server")
	if err != nil {
		return nil, err
	}
	return a.client.Mapper().DataToServer(ctx, a.mapperCtx, data)
}

func (a *NaumenAdapter) GetWorkstationSummaries(ctx context.Context) ([]integration.EntitySummary, error) {
	return a.fetchSummaries(ctx, "Workstation")
}

func (a *NaumenAdapter) GetWorkstation(ctx context.Context, externalID string) (*workstation.Workstation, error) {
	data, err := a.client.FetchEntityDetails(ctx, externalID, "Workstation")
	if err != nil {
		return nil, err
	}
	return a.client.Mapper().DataToWorkstation(ctx, a.mapperCtx, data)
}

func (a *NaumenAdapter) GetFiscalRegisterSummaries(ctx context.Context) ([]integration.EntitySummary, error) {
	return a.fetchSummaries(ctx, "FiscalRegister")
}

func (a *NaumenAdapter) GetFiscalRegister(ctx context.Context, externalID string) (*fiscal.FiscalRegister, error) {
	data, err := a.client.FetchEntityDetails(ctx, externalID, "FiscalRegister")
	if err != nil {
		return nil, err
	}
	return a.client.Mapper().DataToFiscalRegister(ctx, a.mapperCtx, data)
}

func (a *NaumenAdapter) GetAllFiscalRegisters(ctx context.Context) (map[string]*fiscal.FiscalRegister, error) {
	rawData, err := a.client.FetchEntityList(ctx, "FiscalRegister")
	if err != nil {
		return nil, err
	}
	result := make(map[string]*fiscal.FiscalRegister)
	for _, item := range rawData {
		uuid, _ := item["UUID"].(string)
		if uuid == "" {
			continue
		}
		fr, err := a.client.Mapper().DataToFiscalRegister(ctx, a.mapperCtx, item)
		if err == nil {
			result[uuid] = fr
		}
	}
	return result, nil
}

// CreateFiscalRegister конвертирует модель ФР в специфичный JSON Naumen и создает сущность.
func (a *NaumenAdapter) CreateFiscalRegister(ctx context.Context, fr *fiscal.FiscalRegister) (string, error) {
	payload := make(map[string]interface{})

	// Владелец. В модели у нас Internal ID, но для создания в Naumen нам нужен External UUID владельца.
	// Адаптер получает уже "обогащенную" или "подготовленную" модель?
	// В `ExternalSystemLink` мы храним соответствия.
	// ПРОБЛЕМА: Модель `FiscalRegister` содержит `OwnerID` (внутренний).
	// Адаптер должен уметь резолвить его во внешний, либо он должен быть передан.
	// Правильный путь: Адаптер использует `linkRepo` (через mapperCtx), чтобы найти внешний ID владельца.

	if fr.OwnerID != nil && *fr.OwnerID != "" {
		link, err := a.mapperCtx.LinkRepo.GetByInternalID(ctx, a.mapperCtx.DB, "naumen", *fr.OwnerID)
		if err == nil && link != nil {
			payload["owner"] = link.ServiceDeskUUID
		} else {
			a.logger.Warn("Не найден внешний ID владельца для создания ФР", "internal_owner_id", *fr.OwnerID)
			// Naumen может не принять без владельца, но попробуем
		}
	}

	// Простые поля
	if fr.RNKKT != nil {
		payload["RNKKT"] = utils.FormatRNKKT(*fr.RNKKT)
	}
	if fr.FRSerialNumber != nil {
		payload["FRSerialNumber"] = strings.TrimSpace(*fr.FRSerialNumber)
	}
	if fr.FNNumber != nil {
		payload["FNNumber"] = strings.TrimSpace(*fr.FNNumber)
	}
	if fr.FRDownloader != nil {
		payload["FRDownloader"] = strings.TrimSpace(*fr.FRDownloader)
	}
	if fr.FRFirmware != nil {
		payload["FRFirmware"] = strings.TrimSpace(*fr.FRFirmware)
	}

	// LegalName
	var legalName string
	if fr.LegalName != nil {
		legalName = strings.TrimSpace(*fr.LegalName)
	}
	if fr.INN != nil && *fr.INN != "" {
		if legalName != "" {
			legalName = fmt.Sprintf("%s ИНН:%s", legalName, strings.TrimSpace(*fr.INN))
		} else {
			legalName = fmt.Sprintf("ИНН:%s", strings.TrimSpace(*fr.INN))
		}
	}
	if legalName != "" {
		payload["LegalName"] = legalName
	}

	// Даты
	if fr.KKTRegDate != nil {
		payload["KKTRegDate"] = fr.KKTRegDate.Format(utils.TimeLayoutServiceDesk)
	}
	if fr.FNExpireDate != nil {
		payload["FNExpireDate"] = fr.FNExpireDate.Format(utils.TimeLayoutServiceDesk)
	}

	// Справочники (Reference Fields)
	if fr.ModelKKT != nil {
		uuid, err := a.client.FindReferenceID(ctx, "ModeliFR", strings.TrimSpace(*fr.ModelKKT), false)
		if err == nil {
			payload["ModelKKT"] = uuid
		} else {
			a.logger.Warn("Модель ККТ не найдена в справочнике", "model", *fr.ModelKKT)
		}
	}

	if fr.FFD != nil {
		formattedFFD := utils.FormatFFDVersion(*fr.FFD)
		uuid, err := a.client.FindReferenceID(ctx, "FFD", formattedFFD, false)
		if err == nil {
			payload["FFD"] = uuid
		}
	}

	// Специфичное поле SrokiFN (извлекаем из Attributes, если есть)
	// В `Attributes` мы ожидаем сырые данные от агента, если они были переданы
	var fnExecution string
	if fr.Attributes != nil {
		var attrs map[string]interface{}
		if err := json.Unmarshal(fr.Attributes, &attrs); err == nil {
			if val, ok := attrs["fn_execution"].(string); ok {
				fnExecution = val
			}
		}
	}
	if fnExecution != "" {
		matches := srokFnRegex.FindStringSubmatch(fnExecution)
		if len(matches) >= 2 {
			uuid, err := a.client.FindReferenceID(ctx, "SrokiFN", matches[1], true)
			if err == nil {
				payload["SrokFN"] = uuid
			}
		}
	}

	resp, err := a.client.CreateEntity(ctx, "FiscalRegister", payload)
	if err != nil {
		return "", err
	}
	newUUID, _ := resp["UUID"].(string)
	return newUUID, nil
}

// UpdateFiscalRegister обновляет ФР в Naumen.
func (a *NaumenAdapter) UpdateFiscalRegister(ctx context.Context, externalID string, fr *fiscal.FiscalRegister) error {
	payload := make(map[string]interface{})

	if fr.RNKKT != nil {
		payload["RNKKT"] = utils.FormatRNKKT(*fr.RNKKT)
	}
	if fr.FNNumber != nil {
		payload["FNNumber"] = *fr.FNNumber
	}
	if fr.FRDownloader != nil {
		payload["FRDownloader"] = *fr.FRDownloader
	}
	if fr.FRFirmware != nil {
		payload["FRFirmware"] = *fr.FRFirmware
	}

	// LegalName update logic
	var legalName string
	if fr.LegalName != nil {
		legalName = *fr.LegalName
		if fr.INN != nil && *fr.INN != "" {
			legalName = fmt.Sprintf("%s ИНН:%s", legalName, *fr.INN)
		}
		payload["LegalName"] = legalName
	}

	if fr.FNExpireDate != nil {
		payload["FNExpireDate"] = fr.FNExpireDate.Format(utils.TimeLayoutServiceDesk)
	}
	if fr.KKTRegDate != nil {
		payload["KKTRegDate"] = fr.KKTRegDate.Format(utils.TimeLayoutServiceDesk)
	}

	// Очистка пустых строк
	for k, v := range payload {
		if strVal, ok := v.(string); ok && strVal == "" {
			delete(payload, k)
		}
	}

	return a.client.UpdateEntity(ctx, externalID, "FiscalRegister", payload)
}

// --- ContractProvider Implementation ---

func (a *NaumenAdapter) GetContracts(ctx context.Context) (map[string]*contract.Contract, error) {
	rawData, err := a.client.FetchEntityList(ctx, "Contract")
	if err != nil {
		return nil, err
	}

	result := make(map[string]*contract.Contract)
	for _, item := range rawData {
		uuid, _ := item["UUID"].(string)
		if uuid == "" {
			continue
		}
		c, err := a.client.Mapper().DataToContract(ctx, a.mapperCtx, item)
		if err != nil {
			a.logger.Warn("Ошибка маппинга контракта", "uuid", uuid, "error", err)
			continue
		}
		result[uuid] = c
	}
	return result, nil
}

func (a *NaumenAdapter) GetCompanyContractStates(ctx context.Context) (map[string]bool, error) {
	states, err := a.client.FetchCompanyContractStates(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for k, v := range states {
		result[k] = v.IsActive
	}
	return result, nil
}

// --- TicketProvider Implementation ---

func (a *NaumenAdapter) GetTickets(ctx context.Context, statuses []string) (map[string]*tickets.Ticket, error) {
	rawData, err := a.client.FetchTickets(ctx, statuses)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*tickets.Ticket)
	for _, item := range rawData {
		uuid, _ := item["UUID"].(string)
		if uuid == "" {
			continue
		}
		t, err := a.client.Mapper().DataToTicket(ctx, a.mapperCtx, item)
		if err != nil {
			continue
		}
		// Убеждаемся, что ServiceDeskUUID заполнен (хотя маппер должен это делать)
		if t.ServiceDeskUUID == "" {
			t.ServiceDeskUUID = uuid
		}
		result[uuid] = t
	}
	return result, nil
}

func (a *NaumenAdapter) CreateTicket(ctx context.Context, ticket *tickets.Ticket) (string, error) {
	// TODO: Реализовать обратный маппинг (Model -> Map) для создания тикета
	return "", fmt.Errorf("CreateTicket not implemented in adapter yet")
}

func (a *NaumenAdapter) UpdateTicket(ctx context.Context, externalID string, data map[string]interface{}) error {
	return a.client.UpdateEntity(ctx, externalID, "Ticket", data)
}
