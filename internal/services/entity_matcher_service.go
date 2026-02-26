package services

import (
	"context"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"net"
	"strings"
)

// MatchReport содержит детальный отчет о поиске сущностей по данным агента.
type MatchReport struct {
	PrimaryOwnerID   string                   // Владелец, определенный по приоритетам
	FoundServer      *server.Server           // Найденный сервер (Приоритет 1)
	FoundWorkstation *workstation.Workstation // Найденная РС (Приоритет 2)
	FoundFR          *fiscal.FiscalRegister   // Найденный ФР (Приоритет 3)

	// Duplicates содержит список сущностей, если по одному критерию найдено более одной записи.
	Duplicates []interface{}

	// Conflict указывает на несовпадение владельцев между найденными сущностями.
	Conflict bool
}

// EntityMatcherService определяет интерфейс для сервиса идентификации сущностей.
type EntityMatcherService interface {
	GetMatchReport(ctx context.Context, data *api.AgentDataDTO) (*MatchReport, error)
}

type entityMatcherServiceImpl struct {
	logger          logger.LoggerInterface
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

func NewEntityMatcherService(
	logger logger.LoggerInterface,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) EntityMatcherService {
	return &entityMatcherServiceImpl{logger, serverRepo, workstationRepo, frRepo}
}

func (s *entityMatcherServiceImpl) GetMatchReport(ctx context.Context, data *api.AgentDataDTO) (*MatchReport, error) {
	report := &MatchReport{
		Duplicates: make([]interface{}, 0),
	}

	// Извлекаем ID заранее для логгера
	tvID := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	lmID := utils.SafeStringDereference(validators.ExtractLiteManagerID(data.AdditionalProperties, data.LitemanagerID))
	rdID := strings.TrimSpace(data.RustdeskID)
	if strings.EqualFold(rdID, "none") {
		rdID = ""
	}

	// Формируем идентификатор для логов: TV_LM
	remoteIDs := fmt.Sprintf("TV:%s_LM:%s_RD:%s", tvID, lmID, rdID)
	log := s.logger.With("remote_ids", remoteIDs, "serial", data.SerialNumber)

	// --- Приоритет 1: Сервер (Server) ---
	normalizedIP := validators.ValidateIPAddress(data.URLRms)
	isLocal := true

	if normalizedIP != nil {
		hostOnly := utils.ExtractHostFromURL(*normalizedIP)
		// Если это валидный IP, проверяем на приватность.
		// Если это домен (ParseIP == nil), считаем его публичным (isLocal = false).
		if net.ParseIP(hostOnly) != nil {
			var err error
			isLocal, err = utils.IsPrivateIP(hostOnly)
			if err != nil {
				log.Warn("Ошибка проверки IP адреса", "ip", *normalizedIP, "error", err)
				isLocal = true // Считаем локальным при ошибке для безопасности
			}
		} else {
			// Это доменное имя (например, iiko.it), значит адрес не локальный (127.x, 192.168.x)
			isLocal = false
		}
	}

	if !isLocal && normalizedIP != nil {
		srv, err := s.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, *normalizedIP)
		if err == nil && srv != nil {
			report.FoundServer = srv
			log.Debug("Найден сервер (Приоритет 1)", "server_id", srv.ID, "owner", utils.SafeStringDereference(srv.OwnerID))

			if srv.OwnerID != nil && *srv.OwnerID != "" {
				report.PrimaryOwnerID = *srv.OwnerID
			}
		}
	} else {
		log.Debug("Пропуск поиска сервера: локальный или невалидный адрес", "url_rms", data.URLRms)
	}

	// --- Приоритет 2: Рабочая станция (Workstation) ---
	if tvID != "" || lmID != "" || rdID != "" {
		wsList, err := s.workstationRepo.FindAllByRemoteIDs(ctx, tvID, lmID, rdID)
		if err == nil && len(wsList) > 0 {
			if len(wsList) == 1 {
				report.FoundWorkstation = &wsList[0]
				log.Debug("Найдена рабочая станция (Приоритет 2)", "ws_id", wsList[0].ID, "owner", utils.SafeStringDereference(wsList[0].OwnerID))

				if report.PrimaryOwnerID == "" && wsList[0].OwnerID != nil && *wsList[0].OwnerID != "" {
					report.PrimaryOwnerID = *wsList[0].OwnerID
				}
			} else {
				log.Warn("Обнаружены дубликаты рабочих станций", "count", len(wsList))
				for _, ws := range wsList {
					report.Duplicates = append(report.Duplicates, ws)
				}
			}
		}
	}

	// --- Приоритет 3: Фискальный регистратор (FiscalRegister) ---
	if data.SerialNumber != "" {
		fr, err := s.frRepo.FindBySerialNumber(ctx, data.SerialNumber)
		if err == nil && fr != nil {
			report.FoundFR = fr
			log.Debug("Найден ФР (Приоритет 3)", "fr_id", fr.ID, "owner", utils.SafeStringDereference(fr.OwnerID))

			if report.PrimaryOwnerID == "" && fr.OwnerID != nil && *fr.OwnerID != "" {
				report.PrimaryOwnerID = *fr.OwnerID
			}
		}
	}

	// --- Финальная проверка на конфликт владельцев ---
	if report.FoundServer != nil && report.FoundWorkstation != nil {
		srvOwner := utils.SafeStringDereference(report.FoundServer.OwnerID)
		wsOwner := utils.SafeStringDereference(report.FoundWorkstation.OwnerID)

		if srvOwner != "" && wsOwner != "" && srvOwner != wsOwner {
			report.Conflict = true
			log.Warn("Обнаружено несовпадение владельцев Сервера и РС", "srv_owner", srvOwner, "ws_owner", wsOwner)
		}
	}

	return report, nil
}
