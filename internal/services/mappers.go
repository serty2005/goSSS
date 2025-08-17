package services

import (
	"context"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
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

	ownerUUID := ""
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if oUUID, oOk := owner["UUID"].(string); oOk {
			ownerUUID = oUUID
		}
	}
	if ownerUUID == "" {
		return nil, fmt.Errorf("server with uuid %s has no owner, skipping", uuid)
	}

	server := &models.Server{}
	server.ServiceDeskUUID = &uuid
	server.OwnerServiceDeskUUID = &ownerUUID
	server.MetaClass = "objectBase$Server"

	if uniqueID, ok := data["UniqueID"].(string); ok {
		server.UniqueID = validators.ValidateUniqueID(uniqueID)
	}
	if tv, ok := data["Teamviewer"].(string); ok {
		server.Teamviewer = validators.ValidateRemoteAccessID(tv)
	}
	if rdp, ok := data["RDP"].(string); ok {
		server.RDP = validators.ValidateRemoteAccessID(rdp)
	}
	if ad, ok := data["AnyDesk"].(string); ok {
		server.Anydesk = validators.ValidateRemoteAccessID(ad)
	}
	if ip, ok := data["IP"].(string); ok {
		server.IP = validators.ValidateIPAddress(ip)
	}
	if dn, ok := data["DeviceName"].(string); ok {
		server.DeviceName = &dn
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		server.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}
	if iikoVer, ok := data["iikoVersion"].(string); ok {
		server.IikoVersion = &iikoVer
	}

	nameForClient, _ := data["nameforclient"].(string)
	description, _ := data["description"].(string)
	fullDesc := strings.TrimSpace(nameForClient + " " + description)
	server.Description = &fullDesc

	server.Litemanager = validators.ExtractLiteManagerID(data, fullDesc)

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
		fr.RNKKT = &val
	}
	if val, ok := data["LegalName"].(string); ok {
		fr.LegalName = &val
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
