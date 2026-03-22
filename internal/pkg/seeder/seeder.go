package seeder

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	infraDB "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const batchSize = 100

type Seeder struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	contractRepo    contract.Repository
}

func NewSeeder(
	logger logger.LoggerInterface, db *gorm.DB, companyRepo company.Repository,
	serverRepo server.Repository, workstationRepo workstation.Repository,
	frRepo fiscal.Repository, contractRepo contract.Repository,
) *Seeder {
	return &Seeder{
		logger, db, companyRepo, serverRepo, workstationRepo, frRepo, contractRepo,
	}
}

// relationsMap хранит временные связи: InternalID -> ExternalUUID родителя/владельца
type relationsMap map[string]string

func (s *Seeder) SeedDatabase(sdClient external.ExternalSystemClient) error {
	s.logger.Info("Начало процесса наполнения базы данных...")

	if err := s.clearDatabase(); err != nil {
		return err
	}

	s.logger.Info("Создание схемы базы данных через общую миграцию...")
	if err := infraDB.Migrate(&config.Config{}, s.db); err != nil {
		s.logger.Error("Не удалось выполнить миграцию схемы БД", "error", err)
		return err
	}
	s.logger.Info("Схема базы данных успешно создана.")

	ctx := context.Background()
	mapperCtx := (*external.MapperContext)(nil) // Для сидера контекст маппера не нужен, так как связи строим вручную

	// Получаем сырые данные
	companyData, _ := sdClient.FetchEntityList(ctx, "Company")
	serverData, _ := sdClient.FetchEntityList(ctx, "Server")
	wsData, _ := sdClient.FetchEntityList(ctx, "Workstation")
	frData, _ := sdClient.FetchEntityList(ctx, "FiscalRegister")
	contractData, _ := sdClient.FetchEntityList(ctx, "Contract")

	// Карты для связывания
	extToIntID := make(map[string]string) // ExternalUUID -> InternalID
	parentRelations := make(relationsMap) // CompanyInternalID -> ParentExternalUUID
	ownerRelations := make(relationsMap)  // EntityInternalID -> OwnerExternalUUID

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. Создание Компаний
		s.logger.Info("Создание Компаний...", "count", len(companyData))
		for _, data := range companyData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			comp, _ := sdClient.Mapper().DataToCompany(ctx, mapperCtx, data)
			if err := tx.Create(comp).Error; err != nil {
				continue
			}
			extToIntID[extID] = comp.ID
			tx.Create(&models.ExternalSystemLink{InternalID: comp.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Company", LastSyncedAt: time.Now()})

			// Сохраняем связь с родителем, если есть
			if parentData, ok := data["parent"].(map[string]interface{}); ok {
				if parentExtID, ok := parentData["UUID"].(string); ok && parentExtID != "" {
					parentRelations[comp.ID] = parentExtID
				}
			}
		}

		// 2. Создание Серверов (с отложенным связыванием владельца)
		s.logger.Info("Создание Серверов...", "count", len(serverData))
		for _, data := range serverData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			srv, _ := sdClient.Mapper().DataToServer(ctx, mapperCtx, data)
			srv.OwnerID = nil // Владельца проставим позже

			if err := tx.Create(srv).Error; err != nil {
				continue
			}

			extToIntID[extID] = srv.ID
			tx.Create(&models.ExternalSystemLink{InternalID: srv.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Server", LastSyncedAt: time.Now()})

			// Запоминаем владельца
			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[srv.ID] = ownerExtID
				}
			}
		}

		// 3. Создание Рабочих станций
		s.logger.Info("Создание Рабочих станций...", "count", len(wsData))
		for _, data := range wsData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			ws, _ := sdClient.Mapper().DataToWorkstation(ctx, mapperCtx, data)
			ws.OwnerID = nil

			if err := tx.Create(ws).Error; err != nil {
				continue
			}

			extToIntID[extID] = ws.ID
			tx.Create(&models.ExternalSystemLink{InternalID: ws.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Workstation", LastSyncedAt: time.Now()})

			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[ws.ID] = ownerExtID
				}
			}
		}

		// 4. Создание ФР
		s.logger.Info("Создание ФР...", "count", len(frData))
		for _, data := range frData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			fr, _ := sdClient.Mapper().DataToFiscalRegister(ctx, mapperCtx, data)
			fr.OwnerID = nil

			if err := tx.Create(fr).Error; err != nil {
				continue
			}

			extToIntID[extID] = fr.ID
			tx.Create(&models.ExternalSystemLink{InternalID: fr.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "FiscalRegister", LastSyncedAt: time.Now()})

			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[fr.ID] = ownerExtID
				}
			}
		}

		// --- LINKING PHASE ---
		s.logger.Info("Выполнение связывания сущностей (Linking Phase)...")

		// Связывание Родительских компаний
		for childID, parentExtID := range parentRelations {
			if parentIntID, ok := extToIntID[parentExtID]; ok {
				tx.Model(&company.Company{}).Where("id = ?", childID).Update("parent_id", parentIntID)
			}
		}

		// Связывание Оборудования с Владельцами
		// Можно оптимизировать батчами, но для сидера сойдет поштучно
		for entityID, ownerExtID := range ownerRelations {
			if ownerIntID, ok := extToIntID[ownerExtID]; ok {
				// Пытаемся обновить во всех таблицах (ID уникальны UUID, так что это безопасно, но не оптимально)
				// Лучше было бы разделить ownerRelations по типам, но для упрощения сделаем так:
				tx.Model(&server.Server{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
				tx.Model(&workstation.Workstation{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
				tx.Model(&fiscal.FiscalRegister{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
			}
		}

		// 5. Создание Контрактов
		s.logger.Info("Создание Контрактов и связей...", "count", len(contractData))
		var linksToCreate []models.CompanyContract

		for _, data := range contractData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			c, err := sdClient.Mapper().DataToContract(ctx, mapperCtx, data)
			if err != nil {
				continue
			}

			if err := tx.Create(c).Error; err != nil {
				continue
			}
			extToIntID[extID] = c.ID
			tx.Create(&models.ExternalSystemLink{InternalID: c.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Contract", LastSyncedAt: time.Now()})

			// Связи с компаниями
			companyExtIDs := sdClient.Mapper().GetCompanyUUIDsFromContract(data)
			for _, compExtID := range companyExtIDs {
				if compIntID, ok := extToIntID[compExtID]; ok {
					linksToCreate = append(linksToCreate, models.CompanyContract{CompanyID: compIntID, ContractID: c.ID})
				}
			}
		}

		if len(linksToCreate) > 0 {
			if err := tx.Table("company_contracts").CreateInBatches(linksToCreate, batchSize).Error; err != nil {
				return err
			}
		}

		// 6. Пересчет статусов (упрощенный, без сложных проверок сервиса)
		// Просто смотрим: если у компании есть active контракт -> active_contract = true
		s.logger.Info("Финализация статусов...")
		tx.Exec(`
			UPDATE companies 
			SET active_contract = true 
			WHERE id IN (
				SELECT cc.company_id 
				FROM company_contracts cc 
				JOIN contracts c ON c.id = cc.contract_id 
				WHERE c.state = 'active'
			)
		`)

		// 7. Загрузка тикетов и комментариев (если файл присутствует)
		ticketSeedRes, err := s.seedTicketsWithComments(tx, extToIntID)
		if err != nil {
			return err
		}

		// 8. Загрузка файлов тикетов из мок-данных (если манифест присутствует)
		if err := s.seedTicketFilesFromMock(tx, ticketSeedRes); err != nil {
			return err
		}

		return nil
	})
}

// clearDatabase удаляет все таблицы для полного пересоздания базы.
func (s *Seeder) clearDatabase() error {
	var tableNames []string
	if err := s.db.Raw(`
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
	`).Scan(&tableNames).Error; err != nil {
		return fmt.Errorf("не удалось получить список таблиц: %w", err)
	}

	if len(tableNames) == 0 {
		s.logger.Info("В схеме public нет таблиц для удаления.")
		return nil
	}

	sort.Strings(tableNames)
	quoted := make([]string, 0, len(tableNames))
	for _, table := range tableNames {
		quoted = append(quoted, quoteIdentifier(table))
	}

	s.logger.Info("Удаление всех таблиц в схеме public...", "count", len(quoted))
	if err := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", strings.Join(quoted, ", "))).Error; err != nil {
		return fmt.Errorf("не удалось удалить таблицы: %w", err)
	}

	return nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

type ticketExport struct {
	UUID             string          `json:"UUID"`
	Number           json.Number     `json:"number"`
	Agreement        exportRef       `json:"agreement"`
	ClientOU         exportRef       `json:"clientOU"`
	DescriptionRTF   string          `json:"descriptionRTF"`
	ResultDescr      string          `json:"resultDescr"`
	RequestDate      string          `json:"requestDate"`
	LastModifiedDate string          `json:"lastModifiedDate"`
	State            string          `json:"state"`
	Comments         []commentExport `json:"comments_list"`
}

type commentExport struct {
	UUID         string       `json:"UUID"`
	Text         string       `json:"text"`
	CreationDate string       `json:"creationDate"`
	Author       exportAuthor `json:"author"`
	Private      bool         `json:"private"`
	Files        []any        `json:"files"`
}

type exportRef struct {
	UUID string `json:"UUID"`
}

type exportAuthor struct {
	Title string `json:"title"`
}

type ticketSeedResult struct {
	ticketIDByExternal  map[string]string
	commentIDByExternal map[string]string
	commentTicketByExt  map[string]string
}

func (s *Seeder) seedTicketsWithComments(tx *gorm.DB, extToIntID map[string]string) (*ticketSeedResult, error) {
	filePath := filepath.Join("tools", "seeder", "mock_data", "2_full_export_with_comments.json")
	if _, err := os.Stat(filePath); err != nil {
		s.logger.Warn("Файл тикетов не найден, пропускаем сидинг тикетов", "file", filePath)
		return nil, nil
	}

	s.logger.Info("Загрузка тикетов и комментариев из файла...", "file", filePath)

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("ожидался JSON-массив в файле %s", filePath)
	}

	var (
		ticketBatch   []tickets.Ticket
		commentBatch  []tickets.TicketComment
		linkBatch     []models.ExternalSystemLink
		ticketsCount  int
		commentsCount int
		seedResult    = &ticketSeedResult{
			ticketIDByExternal:  make(map[string]string),
			commentIDByExternal: make(map[string]string),
			commentTicketByExt:  make(map[string]string),
		}
	)

	flushTickets := func() error {
		if len(ticketBatch) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(ticketBatch, batchSize).Error; err != nil {
			return err
		}
		ticketsCount += len(ticketBatch)
		ticketBatch = ticketBatch[:0]
		return nil
	}

	flushComments := func() error {
		if len(commentBatch) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(commentBatch, 500).Error; err != nil {
			return err
		}
		commentsCount += len(commentBatch)
		commentBatch = commentBatch[:0]
		return nil
	}

	flushLinks := func() error {
		if len(linkBatch) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(linkBatch, 500).Error; err != nil {
			return err
		}
		linkBatch = linkBatch[:0]
		return nil
	}

	for dec.More() {
		var raw ticketExport
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if raw.UUID == "" {
			continue
		}

		number := 0
		if raw.Number != "" {
			if n, err := raw.Number.Int64(); err == nil {
				number = int(n)
			}
		}
		if number == 0 {
			s.logger.Warn("Пропуск тикета без номера", "uuid", raw.UUID)
			continue
		}

		ticketID := uuid.New().String()
		ticket := tickets.Ticket{
			Base:            common.Base{ID: ticketID},
			Number:          number,
			Subject:         utils.StripHTML(raw.DescriptionRTF),
			Description:     raw.DescriptionRTF,
			Result:          raw.ResultDescr,
			Status:          raw.State,
			Priority:        tickets.PriorityMedium,
			Type:            tickets.TypeIncident,
			ServiceDeskUUID: raw.UUID,
			IsArchived:      true,
			ArchivedAt:      utils.ParseServiceDeskTime(raw.LastModifiedDate),
			SyncWithBitrix:  false,
		}

		if raw.RequestDate != "" {
			if t := utils.ParseServiceDeskTime(raw.RequestDate); t != nil {
				ticket.CreatedAt = *t
			}
		}

		if raw.LastModifiedDate != "" {
			if t := utils.ParseServiceDeskTime(raw.LastModifiedDate); t != nil {
				ticket.UpdatedAt = *t
			}
		}

		if ticket.CreatedAt.IsZero() && !ticket.UpdatedAt.IsZero() {
			ticket.CreatedAt = ticket.UpdatedAt
		}

		if raw.ClientOU.UUID != "" {
			if compID, ok := extToIntID[raw.ClientOU.UUID]; ok {
				ticket.CompanyID = compID
			}
		}
		if raw.Agreement.UUID != "" {
			if contractID, ok := extToIntID[raw.Agreement.UUID]; ok {
				ticket.ContractID = &contractID
			}
		}

		ticketBatch = append(ticketBatch, ticket)
		seedResult.ticketIDByExternal[raw.UUID] = ticketID
		linkBatch = append(linkBatch, models.ExternalSystemLink{
			InternalID:      ticketID,
			SystemName:      "naumen",
			ServiceDeskUUID: raw.UUID,
			EntityType:      "Ticket",
			LastSyncedAt:    time.Now(),
		})
		if len(ticketBatch) >= batchSize {
			if err := flushTickets(); err != nil {
				return nil, err
			}
		}
		if len(linkBatch) >= 500 {
			if err := flushLinks(); err != nil {
				return nil, err
			}
		}

		for _, c := range raw.Comments {
			comment := tickets.TicketComment{
				TicketID:        ticketID,
				ServiceDeskUUID: c.UUID,
				Text:            c.Text,
				AuthorName:      c.Author.Title,
				IsInternal:      c.Private,
			}
			comment.ID = uuid.New().String()
			if c.CreationDate != "" {
				if t := utils.ParseServiceDeskTime(c.CreationDate); t != nil {
					comment.CreationDate = *t
				}
			}
			if comment.CreationDate.IsZero() {
				comment.CreationDate = ticket.CreatedAt
			}
			if comment.CreationDate.IsZero() {
				comment.CreationDate = time.Now()
			}
			commentBatch = append(commentBatch, comment)
			if c.UUID != "" {
				seedResult.commentIDByExternal[c.UUID] = comment.ID
				seedResult.commentTicketByExt[c.UUID] = ticketID
				linkBatch = append(linkBatch, models.ExternalSystemLink{
					InternalID:      comment.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: c.UUID,
					EntityType:      "TicketComment",
					LastSyncedAt:    time.Now(),
				})
			}
			if len(commentBatch) >= 500 {
				if err := flushComments(); err != nil {
					return nil, err
				}
			}
			if len(linkBatch) >= 500 {
				if err := flushLinks(); err != nil {
					return nil, err
				}
			}
		}
	}

	if _, err := dec.Token(); err != nil {
		return nil, err
	}

	if err := flushTickets(); err != nil {
		return nil, err
	}
	if err := flushComments(); err != nil {
		return nil, err
	}
	if err := flushLinks(); err != nil {
		return nil, err
	}

	s.logger.Info("Сидинг тикетов завершен", "tickets", ticketsCount, "comments", commentsCount)
	return seedResult, nil
}
