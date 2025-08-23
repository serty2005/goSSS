// internal/services/mappers.go
package services

import (
	"context"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// DataToCompany преобразует мапу от ServiceDesk в модель Company.
func DataToCompany(ctx context.Context, data map[string]interface{}, sdClient ServiceDeskClient, logger *zap.Logger) (*models.Company, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("company data missing UUID")
	}

	company := &models.Company{}
	company.ServiceDeskUUID = &uuid
	company.MetaClass = "ou$company"

	if title, ok := data["title"].(string); ok {
		company.Title = &title
	}
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		company.AdditionalName = &addName
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Обработка parent
	if parent, ok := data["parent"].(map[string]interface{}); ok {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.ParentServiceDeskUUID = &parentUUID
		}
	}

	// Обработка active_contract
	active := false
	if agreements, ok := data["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			if agrMap, agrOk := agr.(map[string]interface{}); agrOk {
				if agrUUID, uuidOk := agrMap["UUID"].(string); uuidOk {
					isActive, err := sdClient.CheckAgreementActive(ctx, agrUUID)
					if err != nil {
						logger.Warn("Failed to check agreement status", zap.String("agreementUUID", agrUUID), zap.Error(err))
						continue
					}
					if isActive {
						active = true
						break
					}
				}
			}
		}
	}
	company.ActiveContract = &active

	return company, nil
}

// DataToServer преобразует мапу от ServiceDesk в модель Server.
func DataToServer(data map[string]interface{}) (*models.Server, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("server data missing UUID")
	}

	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("server with uuid %s has no owner, skipping", uuid)
	}

	server := &models.Server{}
	server.ServiceDeskUUID = &uuid
	server.OwnerServiceDeskUUID = &ownerUUID
	server.MetaClass = "objectBase$Server"

	// 1. Извлекаем все "сырые" строковые значения из данных ServiceDesk.
	rawUniqueID, _ := data["UniqueID"].(string)
	rawTeamviewer, _ := data["Teamviewer"].(string)
	rawRDP, _ := data["RDP"].(string)
	rawAnydesk, _ := data["AnyDesk"].(string)
	rawIP, _ := data["IP"].(string)
	rawDeviceName, _ := data["DeviceName"].(string)
	rawIikoVersion, _ := data["iikoVersion"].(string)
	rawDescription, _ := data["description"].(string)
	rawNameForClient, _ := data["nameforclient"].(string)
	rawLitemanager, _ := data["litemanagerID"].(string)

	// 2. Валидируем и заполняем основные поля модели.
	server.UniqueID = validators.ValidateUniqueID(rawUniqueID)
	server.Teamviewer = validators.ValidateRemoteAccessID(rawTeamviewer)
	server.Anydesk = validators.ValidateRemoteAccessID(rawAnydesk)
	server.IP = validators.ValidateIPAddress(rawIP)

	// Поле RDP сохраняется "как есть", без валидации.
	if rawRDP != "" {
		server.RDP = &rawRDP
	}

	if rawDeviceName != "" {
		server.DeviceName = &rawDeviceName
	}
	if rawIikoVersion != "" {
		server.ServerVersion = &rawIikoVersion
	}

	// 3. Собираем все извлеченные "сырые" данные в единое поле Description.
	var descriptionParts []string
	if rawNameForClient != "" {
		descriptionParts = append(descriptionParts, "Имя для клиента: "+rawNameForClient)
	}
	if rawDescription != "" {
		descriptionParts = append(descriptionParts, "Описание: "+rawDescription)
	}
	if rawUniqueID != "" {
		descriptionParts = append(descriptionParts, "UniqueID: "+rawUniqueID)
	}
	if rawTeamviewer != "" {
		descriptionParts = append(descriptionParts, "Teamviewer: "+rawTeamviewer)
	}
	if rawAnydesk != "" {
		descriptionParts = append(descriptionParts, "AnyDesk: "+rawAnydesk)
	}
	if rawRDP != "" {
		descriptionParts = append(descriptionParts, "RDP: "+rawRDP)
	}
	if rawLitemanager != "" {
		descriptionParts = append(descriptionParts, "Litemanager: "+rawLitemanager)
	}
	if rawIP != "" {
		descriptionParts = append(descriptionParts, "IP/URL: "+rawIP)
	}

	fullDescription := strings.Join(descriptionParts, " | ")
	if fullDescription != "" {
		server.Description = &fullDescription
	}

	// 4. Заполняем остальные поля.
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		server.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Litemanager заполняется либо из прямого поля, либо извлекается из описания (как fallback)
	if rawLitemanager != "" && validators.LiteManagerIDRegex.MatchString(rawLitemanager) {
		server.Litemanager = &rawLitemanager
	} else {
		// Если прямого поля нет, ищем в старых полях
		server.Litemanager = validators.ExtractLiteManagerID(data, fullDescription)
	}

	if cl, ok := data["CabinetLink"].(string); ok && server.IP != nil {
		companyType := validators.DetermineCompanyTypeFromIP(*server.IP)
		link := validators.ValidateCabinetLink(cl, companyType)
		server.CabinetLink = &link
	}

	return server, nil
}

// DataToWorkstation преобразует мапу в модель Workstation.
func DataToWorkstation(data map[string]interface{}) (*models.Workstation, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("workstation data missing UUID")
	}
	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("workstation with uuid %s has no owner, skipping", uuid)
	}

	ws := &models.Workstation{}
	ws.ServiceDeskUUID = &uuid
	ws.OwnerServiceDeskUUID = &ownerUUID
	ws.MetaClass = "objectBase$Workstation"

	if tv, ok := data["Teamviewer"].(string); ok {
		ws.Teamviewer = validators.ValidateRemoteAccessID(tv)
	}
	if ad, ok := data["AnyDesk"].(string); ok {
		ws.Anydesk = validators.ValidateRemoteAccessID(ad)
	}
	if dn, ok := data["DeviceName"].(string); ok {
		ws.DeviceName = &dn
	}
	if desc, ok := data["Commentariy"].(string); ok {
		ws.Description = &desc
		ws.Litemanager = validators.ExtractLiteManagerID(data, desc)
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		ws.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	return ws, nil
}

// Regex для поиска ИНН в строке.
var innRegex = regexp.MustCompile(`ИНН:\s*(\d{10,12})`)

// DataToFiscalRegister преобразует мапу в модель FiscalRegister.
func DataToFiscalRegister(data map[string]interface{}) (*models.FiscalRegister, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("FR data missing UUID")
	}
	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("FR with uuid %s has no owner, skipping", uuid)
	}

	fr := &models.FiscalRegister{}
	fr.ServiceDeskUUID = &uuid
	fr.OwnerServiceDeskUUID = &ownerUUID
	fr.MetaClass = "objectBase$FR"

	if val, ok := data["ModelKKT"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.ModelKKT = &title
		}
	} else if val, ok := data["ModelKKT"].(string); ok {
		fr.ModelKKT = &val
	}

	if val, ok := data["FFD"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.FFD = &title
		}
	} else if val, ok := data["FFD"].(string); ok {
		fr.FFD = &val
	}

	if val, ok := data["FRDownloader"].(string); ok {
		fr.FRDownloader = &val
	}
	if val, ok := data["RNKKT"].(string); ok {
		// Нормализуем РН ККТ перед сохранением
		normalizedRNKKT := utils.NormalizeRNKKT(val)
		fr.RNKKT = &normalizedRNKKT
	}
	if val, ok := data["LegalName"].(string); ok {
		// ИЗВЛЕЧЕНИЕ ИНН: Ищем ИНН в юридическом имени
		matches := innRegex.FindStringSubmatch(val)
		if len(matches) > 1 {
			inn := matches[1]
			fr.INN = &inn
			// Очищаем LegalName от найденного ИНН
			cleanName := strings.TrimSpace(innRegex.ReplaceAllString(val, ""))
			fr.LegalName = &cleanName
		} else {
			fr.LegalName = &val
		}
	}
	if val, ok := data["FRSerialNumber"].(string); ok {
		fr.FRSerialNumber = &val
	}
	if val, ok := data["FNNumber"].(string); ok {
		fr.FNNumber = &val
	}
	if val, ok := data["KKTRegDate"].(string); ok {
		fr.KKTRegDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["FNExpireDate"].(string); ok {
		fr.FNExpireDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["lastModifiedDate"].(string); ok {
		fr.LastModifiedDate = utils.ParseServiceDeskTime(val)
	}

	return fr, nil
}

// getOwnerUUID извлекает UUID владельца из данных.
func getOwnerUUID(data map[string]interface{}) string {
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if oUUID, oOk := owner["UUID"].(string); oOk {
			return oUUID
		}
	}
	return ""
}
