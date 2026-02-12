// internal/services/sd_editor_service.go
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SDEditorService РѕРїСЂРµРґРµР»СЏРµС‚ РёРЅС‚РµСЂС„РµР№СЃ РґР»СЏ СЃРµСЂРІРёСЃР°, РєРѕС‚РѕСЂС‹Р№ Р°СЃРёРЅС…СЂРѕРЅРЅРѕ РёР·РјРµРЅСЏРµС‚ РґР°РЅРЅС‹Рµ РІ ServiceDesk.
type SDEditorService interface {
	Start(ctx context.Context)
}

// sdEditorServiceImpl СЂРµР°Р»РёР·СѓРµС‚ РёРЅС‚РµСЂС„РµР№СЃ SDEditorService.
type sdEditorServiceImpl struct {
	logger          logger.LoggerInterface
	tm              interfaces.Transactor
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

// NewSDEditorService СЃРѕР·РґР°РµС‚ РЅРѕРІС‹Р№ СЌРєР·РµРјРїР»СЏСЂ РІРѕСЂРєРµСЂР° SDEditorService.
func NewSDEditorService(
	logger logger.LoggerInterface,
	tm interfaces.Transactor,
	bus eventbus.EventBus,
	sdClient external.ExternalSystemClient,
	taskRepo repositories.TaskRepo,
	linkRepo repositories.LinkRepo,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) SDEditorService {
	return &sdEditorServiceImpl{
		logger:          logger,
		tm:              tm,
		bus:             bus,
		sdClient:        sdClient,
		taskRepo:        taskRepo,
		linkRepo:        linkRepo,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start Р·Р°РїСѓСЃРєР°РµС‚ РІРѕСЂРєРµСЂ Рё РїРѕРґРїРёСЃС‹РІР°РµС‚ РµРіРѕ РЅР° СЃРѕР±С‹С‚РёСЏ.
func (s *sdEditorServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Р—Р°РїСѓСЃРє РІРѕСЂРєРµСЂР° SDEditorService")
	s.bus.Subscribe(events.ServiceDeskCreateRequested, s.handleCreateRequest)
	s.bus.Subscribe(events.ServiceDeskUpdateRequested, s.handleUpdateRequest)
}

// handleUpdateRequest РѕР±СЂР°Р±Р°С‚С‹РІР°РµС‚ Р·Р°РїСЂРѕСЃ РЅР° РѕР±РЅРѕРІР»РµРЅРёРµ СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk.
func (s *sdEditorServiceImpl) handleUpdateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}

	log := s.logger.With("taskID", payload.TaskID, "internalUUID", payload.EntityUUID)
	log.Info("РџРѕР»СѓС‡РµРЅ Р·Р°РїСЂРѕСЃ РЅР° РѕР±РЅРѕРІР»РµРЅРёРµ СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk")

	// 1. РќР°С…РѕРґРёРј РІРЅРµС€РЅРёР№ ID РїРѕ РІРЅСѓС‚СЂРµРЅРЅРµРјСѓ ID.
	link, err := s.linkRepo.GetByInternalID(ctx, nil, "naumen", payload.EntityUUID)
	if err != nil || link == nil {
		msg := fmt.Sprintf("РќРµ РЅР°Р№РґРµРЅР° СЃРІСЏР·СЊ СЃ ServiceDesk РґР»СЏ СЃСѓС‰РЅРѕСЃС‚Рё СЃ РІРЅСѓС‚СЂРµРЅРЅРёРј ID %s", payload.EntityUUID)
		log.Error(msg, "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", msg)
		return
	}

	// 2. РЎРѕР±РёСЂР°РµРј payload РґР»СЏ ServiceDesk РЅР° РѕСЃРЅРѕРІРµ СЌС‚Р°Р»РѕРЅРЅС‹С… РґР°РЅРЅС‹С… РёР· РЅР°С€РµР№ Р‘Р”.
	payloadForSD, err := s.buildUpdatePayload(ctx, payload.EntityType, payload.EntityUUID)
	if err != nil {
		log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР±СЂР°С‚СЊ payload РґР»СЏ РѕР±РЅРѕРІР»РµРЅРёСЏ", "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("РћС€РёР±РєР° СЃР±РѕСЂРєРё РґР°РЅРЅС‹С… РґР»СЏ SD: %v", err))
		return
	}
	log.Info("РџРѕРґРіРѕС‚РѕРІР»РµРЅ payload РґР»СЏ РѕР±РЅРѕРІР»РµРЅРёСЏ СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk", "payload", payloadForSD)

	// 3. Р’С‹РїРѕР»РЅСЏРµРј РѕР±РЅРѕРІР»РµРЅРёРµ, РёСЃРїРѕР»СЊР·СѓСЏ РІРЅРµС€РЅРёР№ ID.
	err = s.sdClient.UpdateEntity(ctx, link.ServiceDeskUUID, payload.EntityType, payloadForSD)
	if err != nil {
		log.Error("РћС€РёР±РєР° РїСЂРё РѕР±РЅРѕРІР»РµРЅРёРё СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk", "error", err, "sent_payload", payloadForSD)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("РћС€РёР±РєР° API ServiceDesk: %v", err))
		return
	}

	// 4. Р’ СЃР»СѓС‡Р°Рµ СѓСЃРїРµС…Р°, РѕР±РЅРѕРІР»СЏРµРј СЃС‚Р°С‚СѓСЃ Р·Р°РґР°С‡Рё.
	log.Info("РЎСѓС‰РЅРѕСЃС‚СЊ РІ ServiceDesk СѓСЃРїРµС€РЅРѕ РѕР±РЅРѕРІР»РµРЅР° (РёР»Рё РІС‹РїРѕР»РЅРµРЅ dry run)")
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", "РЎСѓС‰РЅРѕСЃС‚СЊ СѓСЃРїРµС€РЅРѕ РѕР±РЅРѕРІР»РµРЅР° РІ ServiceDesk.")
}

// handleCreateRequest РѕР±СЂР°Р±Р°С‚С‹РІР°РµС‚ Р·Р°РїСЂРѕСЃ РЅР° СЃРѕР·РґР°РЅРёРµ СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk.
func (s *sdEditorServiceImpl) handleCreateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}
	log := s.logger.With("taskID", payload.TaskID)
	log.Info("РџРѕР»СѓС‡РµРЅ Р·Р°РїСЂРѕСЃ РЅР° СЃРѕР·РґР°РЅРёРµ СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk")

	var newExternalUUID string
	var err error

	switch payload.EntityType {
	case "FiscalRegister":
		newExternalUUID, err = s.createFiscalRegisterFromTask(ctx, payload.TaskID)
	default:
		err = fmt.Errorf("СЃРѕР·РґР°РЅРёРµ СЃСѓС‰РЅРѕСЃС‚Рё С‚РёРїР° '%s' РЅРµ РїРѕРґРґРµСЂР¶РёРІР°РµС‚СЃСЏ", payload.EntityType)
	}

	if err != nil {
		log.Error("РћС€РёР±РєР° РїСЂРё СЃРѕР·РґР°РЅРёРё СЃСѓС‰РЅРѕСЃС‚Рё РІ ServiceDesk", "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("РћС€РёР±РєР° API ServiceDesk: %v", err))
		return
	}

	// РџРѕСЃР»Рµ СѓСЃРїРµС€РЅРѕРіРѕ СЃРѕР·РґР°РЅРёСЏ РІ SD, РЅР°Рј РЅСѓР¶РЅРѕ СЃРІСЏР·Р°С‚СЊ РЅР°С€ РІРЅСѓС‚СЂРµРЅРЅРёР№ РѕР±СЉРµРєС‚ СЃ РЅРѕРІС‹Рј РІРЅРµС€РЅРёРј.
	task, _ := s.taskRepo.GetByID(ctx, payload.TaskID)
	if task != nil {
		// EntityUUID РІ Р·Р°РґР°С‡Рµ С‚РёРїР° add_equipment - СЌС‚Рѕ СѓРЅРёРєР°Р»СЊРЅС‹Р№ РёРґРµРЅС‚РёС„РёРєР°С‚РѕСЂ РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЏ (РЅР°РїСЂРёРјРµСЂ, СЃРµСЂРёР№РЅС‹Р№ РЅРѕРјРµСЂ).
		// РќР°Рј РЅСѓР¶РЅРѕ РЅР°Р№С‚Рё РІРЅСѓС‚СЂРµРЅРЅРёР№ РѕР±СЉРµРєС‚ РїРѕ СЌС‚РѕРјСѓ РёРґРµРЅС‚РёС„РёРєР°С‚РѕСЂСѓ.
		var internalID string
		switch task.EntityType {
		case "FiscalRegister":
			fr, _ := s.frRepo.FindBySerialNumber(ctx, task.EntityUUID)
			if fr != nil {
				internalID = fr.ID
			}
		}

		if internalID != "" {
			newLink := models.ExternalSystemLink{
				InternalID: internalID, SystemName: "naumen", ServiceDeskUUID: newExternalUUID,
				EntityType: task.EntityType, LastSyncedAt: time.Now(),
			}
			if err := s.linkRepo.Create(ctx, nil, &newLink); err != nil {
				log.Error("РљСЂРёС‚РёС‡РµСЃРєР°СЏ РѕС€РёР±РєР°: СЃСѓС‰РЅРѕСЃС‚СЊ РІ SD СЃРѕР·РґР°РЅР°, РЅРѕ РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ РґР»СЏ РЅРµРµ СЃРІСЏР·СЊ РІ Р»РѕРєР°Р»СЊРЅРѕР№ Р‘Р”", "error", err)
				// РЎС‚Р°С‚СѓСЃ Р·Р°РґР°С‡Рё РЅРµ РјРµРЅСЏРµРј РЅР° resolved, С‡С‚РѕР±С‹ РѕРїРµСЂР°С‚РѕСЂ СѓРІРёРґРµР» РїСЂРѕР±Р»РµРјСѓ
				s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("РЎРѕР·РґР°РЅРѕ РІ SD (extUUID: %s), РЅРѕ РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ СЃРІСЏР·СЊ РІ Р‘Р”!", newExternalUUID))
				return
			}
		}
	}

	log.Info("РЎСѓС‰РЅРѕСЃС‚СЊ РІ ServiceDesk СѓСЃРїРµС€РЅРѕ СЃРѕР·РґР°РЅР° Рё СЃРІСЏР·Р°РЅР°", "newExternalUUID", newExternalUUID)
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", fmt.Sprintf("РЎСѓС‰РЅРѕСЃС‚СЊ СѓСЃРїРµС€РЅРѕ СЃРѕР·РґР°РЅР° РІ ServiceDesk СЃ UUID: %s", newExternalUUID))
}

// buildUpdatePayload С„РѕСЂРјРёСЂСѓРµС‚ map[string]interface{} РґР»СЏ РѕР±РЅРѕРІР»РµРЅРёСЏ СЃСѓС‰РЅРѕСЃС‚Рё РІ SD.
func (s *sdEditorServiceImpl) buildUpdatePayload(ctx context.Context, entityType, internalID string) (map[string]interface{}, error) {
	payload := make(map[string]interface{})

	switch entityType {
	case "FiscalRegister":
		fr, err := s.frRepo.GetByID(ctx, internalID)
		if err != nil || fr == nil {
			return nil, fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р№С‚Рё Р¤Р  СЃ ID %s РІ Р»РѕРєР°Р»СЊРЅРѕР№ Р‘Р”: %w", internalID, err)
		}
		payload["RNKKT"] = utils.FormatRNKKT(utils.SafeStringDereference(fr.RNKKT))
		payload["FNNumber"] = utils.SafeStringDereference(fr.FNNumber)
		payload["FRDownloader"] = utils.SafeStringDereference(fr.FRDownloader)

		var legalName string
		if fr.LegalName != nil && *fr.LegalName != "" {
			legalName = *fr.LegalName
			if fr.INN != nil && *fr.INN != "" {
				legalName = fmt.Sprintf("%s РРќРќ:%s", *fr.LegalName, *fr.INN)
			}
		}
		payload["LegalName"] = legalName

		if fr.FNExpireDate != nil {
			payload["FNExpireDate"] = fr.FNExpireDate.Format(utils.TimeLayoutServiceDesk)
		}
		if fr.KKTRegDate != nil {
			payload["KKTRegDate"] = fr.KKTRegDate.Format(utils.TimeLayoutServiceDesk)
		}

		for k, v := range payload {
			if strVal, ok := v.(string); ok && strVal == "" {
				delete(payload, k)
			}
		}

	default:
		return nil, fmt.Errorf("РѕР±РЅРѕРІР»РµРЅРёРµ РґР»СЏ С‚РёРїР° СЃСѓС‰РЅРѕСЃС‚Рё '%s' РЅРµ РїРѕРґРґРµСЂР¶РёРІР°РµС‚СЃСЏ", entityType)
	}

	return payload, nil
}

// createFiscalRegisterFromTask РёР·РІР»РµРєР°РµС‚ РґР°РЅРЅС‹Рµ РёР· Р·Р°РґР°С‡Рё Рё СЃРѕР·РґР°РµС‚ Р¤Р  РІ ServiceDesk.
func (s *sdEditorServiceImpl) createFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return "", fmt.Errorf("РѕС€РёР±РєР° РїРѕР»СѓС‡РµРЅРёСЏ Р·Р°РґР°С‡Рё %d: %w", taskID, err)
	}

	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_uuid"` // Р—РґРµСЃСЊ РІСЃРµ РµС‰Рµ РІРЅРµС€РЅРёР№ ID РІР»Р°РґРµР»СЊС†Р°
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return "", err
	}

	agentData := details.AgentData
	log := s.logger.With("taskID", taskID)
	log.Debug("РќР°С‡Р°Р»Рѕ СЃР±РѕСЂРєРё payload РґР»СЏ СЃРѕР·РґР°РЅРёСЏ Р¤Р ")
	payload := make(map[string]interface{})

	addStringFieldToPayload(log, payload, "owner", details.EtalonOwnerUUID)

	// 1. РџСЂРѕСЃС‚С‹Рµ С‚РµРєСЃС‚РѕРІС‹Рµ Рё РІСЂРµРјРµРЅРЅС‹Рµ РїРѕР»СЏ
	addStringFieldToPayload(log, payload, "RNKKT", utils.FormatRNKKT(agentData.RNM))
	addStringFieldToPayload(log, payload, "FRSerialNumber", strings.TrimSpace(agentData.SerialNumber))
	addStringFieldToPayload(log, payload, "FNNumber", strings.TrimSpace(agentData.FNSerial))

	var legalName string
	if agentData.OrganizationName != "" && agentData.INN != "" {
		legalName = fmt.Sprintf("%s РРќРќ:%s", strings.TrimSpace(agentData.OrganizationName), strings.TrimSpace(agentData.INN))
	} else {
		legalName = strings.TrimSpace(agentData.OrganizationName)
	}
	addStringFieldToPayload(log, payload, "LegalName", legalName)

	if regDate := utils.ParseAgentTime(agentData.DateTimeReg); regDate != nil {
		addStringFieldToPayload(log, payload, "KKTRegDate", regDate.Format(utils.TimeLayoutServiceDesk))
	}
	if expDate := utils.ParseAgentTime(agentData.DateTimeEnd); expDate != nil {
		addStringFieldToPayload(log, payload, "FNExpireDate", expDate.Format(utils.TimeLayoutServiceDesk))
	}

	// 2. РќРѕРІС‹Рµ РїРѕР»СЏ РґР»СЏ РїСЂРѕС€РёРІРєРё Рё Р·Р°РіСЂСѓР·С‡РёРєР°
	addStringFieldToPayload(log, payload, "FRDownloader", strings.TrimSpace(agentData.BootVersion))
	frFirmwareValue := utils.CalculateFRFirmware(agentData.Licenses)
	addStringFieldToPayload(log, payload, "FRFirmware", frFirmwareValue)

	// 3. РџРѕР»СЏ, С‚СЂРµР±СѓСЋС‰РёРµ РїРѕРёСЃРєР° РІ СЃРїСЂР°РІРѕС‡РЅРёРєР°С… ServiceDesk
	modelName := strings.TrimSpace(agentData.ModelName)
	modelUUID, err := s.sdClient.FindReferenceID(ctx, "ModeliFR", modelName, false)
	if err != nil {
		return "", fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р№С‚Рё РјРѕРґРµР»СЊ РљРљРў '%s': %w", modelName, err)
	}
	addStringFieldToPayload(log, payload, "ModelKKT", modelUUID)

	ffdVersion := utils.FormatFFDVersion(agentData.FFDVersion)
	ffdUUID, err := s.sdClient.FindReferenceID(ctx, "FFD", ffdVersion, false)
	if err != nil {
		return "", fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р№С‚Рё РІРµСЂСЃРёСЋ Р¤Р¤Р” '%s': %w", ffdVersion, err)
	}
	addStringFieldToPayload(log, payload, "FFD", ffdUUID)

	srokMatches := srokFnRegex.FindStringSubmatch(agentData.FNExecution)
	if len(srokMatches) < 2 {
		return "", fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РёР·РІР»РµС‡СЊ СЃСЂРѕРє Р¤Рќ РёР· '%s'", agentData.FNExecution)
	}
	srokUUID, err := s.sdClient.FindReferenceID(ctx, "SrokiFN", srokMatches[1], true)
	if err != nil {
		return "", fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РЅР°Р№С‚Рё СЃСЂРѕРє Р¤Рќ '%s': %w", srokMatches[1], err)
	}
	addStringFieldToPayload(log, payload, "SrokFN", srokUUID)

	// 4. Р’Р»Р°РґРµР»РµС† СЃСѓС‰РЅРѕСЃС‚Рё
	addStringFieldToPayload(log, payload, "owner", details.EtalonOwnerUUID)

	log.Info("РџРѕРґРіРѕС‚РѕРІР»РµРЅ РёС‚РѕРіРѕРІС‹Р№ payload РґР»СЏ СЃРѕР·РґР°РЅРёСЏ Р¤Р  РІ ServiceDesk", "payload", payload)

	// 5. Р’С‹Р·РѕРІ РєР»РёРµРЅС‚Р° РґР»СЏ СЃРѕР·РґР°РЅРёСЏ СЃСѓС‰РЅРѕСЃС‚Рё
	response, err := s.sdClient.CreateEntity(ctx, "FiscalRegister", payload)
	if err != nil {
		return "", fmt.Errorf("РѕС€РёР±РєР° СЃРѕР·РґР°РЅРёСЏ Р¤Р  РІ ServiceDesk: %w", err)
	}

	newUUID, _ := response["UUID"].(string)
	return newUUID, nil
}

// updateTaskStatus РѕР±РЅРѕРІР»СЏРµС‚ СЃС‚Р°С‚СѓСЃ Рё РєРѕРјРјРµРЅС‚Р°СЂРёР№ Р·Р°РґР°С‡Рё. Р’С‹РїРѕР»РЅСЏРµС‚СЃСЏ РІ РѕС‚РґРµР»СЊРЅРѕР№ С‚СЂР°РЅР·Р°РєС†РёРё
// РґР»СЏ РѕР±РµСЃРїРµС‡РµРЅРёСЏ Р°С‚РѕРјР°СЂРЅРѕСЃС‚Рё Рё РЅРµР·Р°РІРёСЃРёРјРѕСЃС‚Рё РѕС‚ РѕСЃРЅРѕРІРЅРѕРіРѕ РєРѕРЅС‚РµРєСЃС‚Р° РѕРїРµСЂР°С†РёРё.
func (s *sdEditorServiceImpl) updateTaskStatus(ctx context.Context, taskID uint, newStatus, commentText string) {
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		task, err := s.taskRepo.GetByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return fmt.Errorf("Р·Р°РґР°С‡Р° СЃ ID %d РЅРµ РЅР°Р№РґРµРЅР° РґР»СЏ РѕР±РЅРѕРІР»РµРЅРёСЏ СЃС‚Р°С‚СѓСЃР°", taskID)
		}
		comment := fmt.Sprintf("%s\n[SD_WORKER] %s", task.Comment, commentText)
		ok, err := s.taskRepo.Update(txCtx, task.ID, map[string]interface{}{"status": newStatus, "comment": comment})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("Р В·Р В°Р Т‘Р В°РЎвЂЎР В° РЎРѓ ID %d Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р…Р В° Р Т‘Р В»РЎРЏ Р С•Р В±Р Р…Р С•Р Р†Р В»Р ВµР Р…Р С‘РЎРЏ РЎРѓРЎвЂљР В°РЎвЂљРЎС“РЎРѓР В°", taskID)
		}
		return nil
	})
	if err != nil {
		s.logger.Error("РљСЂРёС‚РёС‡РµСЃРєР°СЏ РѕС€РёР±РєР°: РЅРµ СѓРґР°Р»РѕСЃСЊ РѕР±РЅРѕРІРёС‚СЊ СЃС‚Р°С‚СѓСЃ Р·Р°РґР°С‡Рё РїРѕСЃР»Рµ РѕРїРµСЂР°С†РёРё СЃ SD",
			"taskID", taskID,
			"newStatus", newStatus,
			"error", err,
		)
	}
}

// srokFnRegex - СЂРµРіСѓР»СЏСЂРЅРѕРµ РІС‹СЂР°Р¶РµРЅРёРµ РґР»СЏ РёР·РІР»РµС‡РµРЅРёСЏ СЃСЂРѕРєР° РґРµР№СЃС‚РІРёСЏ Р¤Рќ (13, 15 РёР»Рё 36 РјРµСЃСЏС†РµРІ).
var srokFnRegex = regexp.MustCompile(`(13|15|36)`)

// addStringFieldToPayload - С…РµР»РїРµСЂ РґР»СЏ Р»РѕРіРёСЂРѕРІР°РЅРёСЏ Рё РґРѕР±Р°РІР»РµРЅРёСЏ РЅРµРїСѓСЃС‚С‹С… СЃС‚СЂРѕРєРѕРІС‹С… РїРѕР»РµР№ РІ payload.
func addStringFieldToPayload(log logger.LoggerInterface, payload map[string]interface{}, key, value string) {
	if value != "" {
		log.Debug("Р”РѕР±Р°РІР»РµРЅРёРµ РїРѕР»СЏ РІ payload", "РїРѕР»Рµ", key, "Р·РЅР°С‡РµРЅРёРµ", value)
		payload[key] = value
	} else {
		log.Debug("РџСЂРѕРїСѓСЃРє РїСѓСЃС‚РѕРіРѕ РїРѕР»СЏ", "РїРѕР»Рµ", key)
	}
}
