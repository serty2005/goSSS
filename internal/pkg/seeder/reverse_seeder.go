package seeder

import (
	"encoding/json"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/pkg/utils"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type mockRef struct {
	UUID string `json:"UUID"`
}

type mockRefWithTitle struct {
	UUID  string `json:"UUID"`
	Title string `json:"title,omitempty"`
}

type companyExport struct {
	Parent              *mockRefWithTitle `json:"parent"`
	RecipientAgreements []mockRef         `json:"recipientAgreements"`
	LastModifiedDate    string            `json:"lastModifiedDate"`
	Adress              *string           `json:"adress"`
	UUID                string            `json:"UUID"`
	Title               *string           `json:"title"`
	AdditionalName      *string           `json:"additionalName"`
}

type serverExport struct {
	Owner            *mockRefWithTitle `json:"owner"`
	UniqueID         *string           `json:"UniqueID"`
	IikoVersion      *string           `json:"iikoVersion"`
	LastModifiedDate string            `json:"lastModifiedDate"`
	IP               *string           `json:"IP"`
	Description      *string           `json:"description"`
	Teamviewer       *string           `json:"Teamviewer"`
	AnyDesk          *string           `json:"AnyDesk"`
	CabinetLink      *string           `json:"CabinetLink"`
	LitemanagerID    *string           `json:"litemanagerID"`
	UUID             string            `json:"UUID"`
	RDP              *string           `json:"RDP"`
	DeviceName       *string           `json:"DeviceName"`
	NameForClient    *string           `json:"nameforclient"`
}

type workstationExport struct {
	Owner            *mockRefWithTitle `json:"owner"`
	Commentariy      *string           `json:"Commentariy"`
	LastModifiedDate string            `json:"lastModifiedDate"`
	AnyDesk          *string           `json:"AnyDesk"`
	LitemanagerID    *string           `json:"litemanagerID"`
	Teamviewer       *string           `json:"Teamviewer"`
	UUID             string            `json:"UUID"`
	DeviceName       *string           `json:"DeviceName"`
}

type fiscalExport struct {
	Owner            *mockRefWithTitle `json:"owner"`
	LegalName        *string           `json:"LegalName"`
	LastModifiedDate string            `json:"lastModifiedDate"`
	FFD              *string           `json:"FFD"`
	FRFirmware       *string           `json:"FRFirmware"`
	FNNumber         *string           `json:"FNNumber"`
	FRSerialNumber   *string           `json:"FRSerialNumber"`
	FRDownloader     *string           `json:"FRDownloader"`
	RNKKT            *string           `json:"RNKKT"`
	KKTRegDate       string            `json:"KKTRegDate"`
	UUID             string            `json:"UUID"`
	FNExpireDate     string            `json:"FNExpireDate"`
	ModelKKT         *string           `json:"ModelKKT"`
}

type contractExport struct {
	LastModifiedDate string             `json:"lastModifiedDate"`
	State            string             `json:"state"`
	Services         []serviceExport    `json:"services"`
	StateStartTime   string             `json:"stateStartTime"`
	UUID             string             `json:"UUID"`
	RecipientsOU     []mockRefWithTitle `json:"recipientsOU"`
}

type serviceExport struct {
	Title string `json:"title"`
}

type ticketExportItem struct {
	Number           int                 `json:"number"`
	Agreement        *mockRef            `json:"agreement"`
	DescriptionRTF   string              `json:"descriptionRTF"`
	ResultDescr      string              `json:"resultDescr,omitempty"`
	ClientOU         *mockRef            `json:"clientOU"`
	LastModifiedDate string              `json:"lastModifiedDate"`
	RequestDate      string              `json:"requestDate"`
	State            string              `json:"state"`
	UUID             string              `json:"UUID"`
	Comments         []ticketCommentItem `json:"comments_list"`
}

type ticketCommentItem struct {
	UUID    string              `json:"UUID"`
	Author  ticketCommentAuthor `json:"author"`
	Private bool                `json:"private"`
	Files   []any               `json:"files"`
	Text    string              `json:"text"`
}

type ticketCommentAuthor struct {
	Title string `json:"title"`
}

type fileManifestItem struct {
	UUID          string   `json:"uuid"`
	FileID        string   `json:"file_id"`
	StorageKey    string   `json:"storage_key"`
	OriginalName  string   `json:"original_name"`
	MimeType      string   `json:"mime_type"`
	Size          int64    `json:"size"`
	Checksum      string   `json:"checksum"`
	ExportPath    string   `json:"export_path"`
	TicketUUIDs   []string `json:"ticket_uuids"`
	CommentUUIDs  []string `json:"comment_uuids"`
	RelationTypes []string `json:"relation_types"`
	MissingSource bool     `json:"missing_source"`
}

func (s *Seeder) ExportDatabaseToMockData(outputDir string, storageRoot string) error {
	s.logger.Info("Запуск обратного сидера")

	if outputDir == "" {
		return fmt.Errorf("не задан путь для экспорта мок-данных")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("не удалось создать директорию мок-данных: %w", err)
	}

	linksByEntity, linksByInternal, err := s.loadNaumenLinks()
	if err != nil {
		return err
	}

	var companies []company.Company
	if err := s.db.Find(&companies).Error; err != nil {
		return fmt.Errorf("не удалось загрузить компании: %w", err)
	}

	var servers []server.Server
	if err := s.db.Find(&servers).Error; err != nil {
		return fmt.Errorf("не удалось загрузить серверы: %w", err)
	}

	var workstations []workstation.Workstation
	if err := s.db.Find(&workstations).Error; err != nil {
		return fmt.Errorf("не удалось загрузить рабочие станции: %w", err)
	}

	var fiscals []fiscal.FiscalRegister
	if err := s.db.Find(&fiscals).Error; err != nil {
		return fmt.Errorf("не удалось загрузить фискальные регистраторы: %w", err)
	}

	var contracts []contract.Contract
	if err := s.db.Find(&contracts).Error; err != nil {
		return fmt.Errorf("не удалось загрузить контракты: %w", err)
	}

	var companyContracts []models.CompanyContract
	if err := s.db.Table("company_contracts").Find(&companyContracts).Error; err != nil {
		return fmt.Errorf("не удалось загрузить связи company_contracts: %w", err)
	}

	var allTickets []tickets.Ticket
	if err := s.db.Order("number ASC").Find(&allTickets).Error; err != nil {
		return fmt.Errorf("не удалось загрузить тикеты: %w", err)
	}

	var allComments []tickets.TicketComment
	if err := s.db.Order("creation_date ASC").Find(&allComments).Error; err != nil {
		return fmt.Errorf("не удалось загрузить комментарии тикетов: %w", err)
	}

	var fileAssets []tickets.FileAsset
	if err := s.db.Find(&fileAssets).Error; err != nil {
		return fmt.Errorf("не удалось загрузить file_assets: %w", err)
	}

	var fileLinks []tickets.TicketFileLink
	if err := s.db.Find(&fileLinks).Error; err != nil {
		return fmt.Errorf("не удалось загрузить ticket_file_links: %w", err)
	}

	companyTitleByID := make(map[string]string, len(companies))
	for _, c := range companies {
		if c.Title != nil {
			companyTitleByID[c.ID] = *c.Title
		}
	}

	companyContractsByCompanyID := make(map[string][]string)
	recipientsByContractID := make(map[string][]string)
	for _, cc := range companyContracts {
		companyContractsByCompanyID[cc.CompanyID] = append(companyContractsByCompanyID[cc.CompanyID], cc.ContractID)
		recipientsByContractID[cc.ContractID] = append(recipientsByContractID[cc.ContractID], cc.CompanyID)
	}

	contractUUIDByID := make(map[string]string)
	for _, c := range contracts {
		if uuid := linksByInternal[c.ID]; uuid != "" {
			contractUUIDByID[c.ID] = uuid
		}
	}

	fileUUIDByStorageKey, commentFilesByUUID, fileManifest, err := s.exportFiles(
		outputDir,
		storageRoot,
		fileAssets,
		fileLinks,
		linksByEntity,
	)
	if err != nil {
		return err
	}

	exportCompanies := s.buildCompanyExport(companies, linksByInternal, companyContractsByCompanyID, contractUUIDByID, companyTitleByID)
	exportServers := s.buildServerExport(servers, linksByInternal, companyTitleByID)
	exportWorkstations := s.buildWorkstationExport(workstations, linksByInternal, companyTitleByID)
	exportFiscals := s.buildFiscalExport(fiscals, linksByInternal, companyTitleByID)
	exportContracts := s.buildContractExport(contracts, linksByInternal, recipientsByContractID, companyTitleByID, linksByInternal)
	exportTickets := s.buildTicketExport(allTickets, allComments, linksByEntity, linksByInternal, fileUUIDByStorageKey, commentFilesByUUID)

	if err := writeJSON(filepath.Join(outputDir, "companies.json"), exportCompanies); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "servers.json"), exportServers); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "workstations.json"), exportWorkstations); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "fiscal_registers.json"), exportFiscals); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "agreements.json"), exportContracts); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "2_full_export_with_comments.json"), exportTickets); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(outputDir, "files_manifest.json"), fileManifest); err != nil {
		return err
	}

	s.logger.Info("Обратный сидер завершен",
		"companies", len(exportCompanies),
		"servers", len(exportServers),
		"workstations", len(exportWorkstations),
		"fiscal_registers", len(exportFiscals),
		"agreements", len(exportContracts),
		"tickets", len(exportTickets),
		"files", len(fileManifest),
	)
	return nil
}

func (s *Seeder) loadNaumenLinks() (map[string]map[string]string, map[string]string, error) {
	var rows []models.ExternalSystemLink
	if err := s.db.Where("system_name = ?", "naumen").Find(&rows).Error; err != nil {
		return nil, nil, fmt.Errorf("не удалось загрузить external_system_links: %w", err)
	}

	byEntity := make(map[string]map[string]string)
	byInternal := make(map[string]string, len(rows))
	for _, row := range rows {
		byInternal[row.InternalID] = row.ServiceDeskUUID
		if _, ok := byEntity[row.EntityType]; !ok {
			byEntity[row.EntityType] = make(map[string]string)
		}
		byEntity[row.EntityType][row.InternalID] = row.ServiceDeskUUID
	}
	return byEntity, byInternal, nil
}

func (s *Seeder) buildCompanyExport(
	companies []company.Company,
	linksByInternal map[string]string,
	companyContracts map[string][]string,
	contractUUIDByID map[string]string,
	companyTitleByID map[string]string,
) []companyExport {
	out := make([]companyExport, 0, len(companies))
	for _, c := range companies {
		extUUID := linksByInternal[c.ID]
		if extUUID == "" {
			continue
		}

		var parent *mockRefWithTitle
		if c.ParentID != nil {
			if parentUUID := linksByInternal[*c.ParentID]; parentUUID != "" {
				parent = &mockRefWithTitle{
					UUID:  parentUUID,
					Title: companyTitleByID[*c.ParentID],
				}
			}
		}

		agreements := make([]mockRef, 0)
		for _, contractID := range companyContracts[c.ID] {
			if agreementUUID := contractUUIDByID[contractID]; agreementUUID != "" {
				agreements = append(agreements, mockRef{UUID: agreementUUID})
			}
		}
		sort.Slice(agreements, func(i, j int) bool { return agreements[i].UUID < agreements[j].UUID })

		out = append(out, companyExport{
			Parent:              parent,
			RecipientAgreements: agreements,
			LastModifiedDate:    formatTime(c.LastModifiedDate),
			Adress:              c.Address,
			UUID:                extUUID,
			Title:               c.Title,
			AdditionalName:      c.AdditionalName,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func (s *Seeder) buildServerExport(
	servers []server.Server,
	linksByInternal map[string]string,
	companyTitleByID map[string]string,
) []serverExport {
	out := make([]serverExport, 0, len(servers))
	for _, entity := range servers {
		extUUID := linksByInternal[entity.ID]
		if extUUID == "" {
			continue
		}

		out = append(out, serverExport{
			Owner:            makeOwnerRef(entity.OwnerID, linksByInternal, companyTitleByID),
			UniqueID:         entity.UniqueID,
			IikoVersion:      entity.ServerVersion,
			LastModifiedDate: formatTime(entity.LastModifiedDate),
			IP:               entity.IP,
			Description:      entity.Description,
			Teamviewer:       entity.Teamviewer,
			AnyDesk:          entity.Anydesk,
			CabinetLink:      entity.CabinetLink,
			LitemanagerID:    entity.Litemanager,
			UUID:             extUUID,
			RDP:              entity.RDP,
			DeviceName:       entity.DeviceName,
			NameForClient:    entity.ServerName,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func (s *Seeder) buildWorkstationExport(
	workstations []workstation.Workstation,
	linksByInternal map[string]string,
	companyTitleByID map[string]string,
) []workstationExport {
	out := make([]workstationExport, 0, len(workstations))
	for _, entity := range workstations {
		extUUID := linksByInternal[entity.ID]
		if extUUID == "" {
			continue
		}

		out = append(out, workstationExport{
			Owner:            makeOwnerRef(entity.OwnerID, linksByInternal, companyTitleByID),
			Commentariy:      entity.Description,
			LastModifiedDate: formatTime(entity.LastModifiedDate),
			AnyDesk:          entity.Anydesk,
			LitemanagerID:    entity.Litemanager,
			Teamviewer:       entity.Teamviewer,
			UUID:             extUUID,
			DeviceName:       entity.DeviceName,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func (s *Seeder) buildFiscalExport(
	fiscals []fiscal.FiscalRegister,
	linksByInternal map[string]string,
	companyTitleByID map[string]string,
) []fiscalExport {
	out := make([]fiscalExport, 0, len(fiscals))
	for _, entity := range fiscals {
		extUUID := linksByInternal[entity.ID]
		if extUUID == "" {
			continue
		}

		legalName := strings.TrimSpace(utils.SafeStringDereference(entity.LegalName))
		if inn := strings.TrimSpace(utils.SafeStringDereference(entity.INN)); inn != "" {
			if legalName == "" {
				legalName = "ИНН:" + inn
			} else {
				legalName = legalName + " ИНН:" + inn
			}
		}
		legalNamePtr := utils.StringPtr(legalName)

		out = append(out, fiscalExport{
			Owner:            makeOwnerRef(entity.OwnerID, linksByInternal, companyTitleByID),
			LegalName:        legalNamePtr,
			LastModifiedDate: formatTime(entity.LastModifiedDate),
			FFD:              entity.FFD,
			FRFirmware:       entity.FRFirmware,
			FNNumber:         entity.FNNumber,
			FRSerialNumber:   entity.FRSerialNumber,
			FRDownloader:     entity.FRDownloader,
			RNKKT:            formatRNKKTForExport(entity.RNKKT),
			KKTRegDate:       formatTime(entity.KKTRegDate),
			UUID:             extUUID,
			FNExpireDate:     formatTime(entity.FNExpireDate),
			ModelKKT:         entity.ModelKKT,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func (s *Seeder) buildContractExport(
	contracts []contract.Contract,
	linksByInternal map[string]string,
	recipientsByContractID map[string][]string,
	companyTitleByID map[string]string,
	companyLinks map[string]string,
) []contractExport {
	out := make([]contractExport, 0, len(contracts))
	for _, c := range contracts {
		extUUID := linksByInternal[c.ID]
		if extUUID == "" {
			continue
		}

		services := parseServiceTitles(c.Services)
		serviceExportItems := make([]serviceExport, 0, len(services))
		for _, title := range services {
			serviceExportItems = append(serviceExportItems, serviceExport{Title: title})
		}

		recipients := make([]mockRefWithTitle, 0)
		for _, companyID := range recipientsByContractID[c.ID] {
			if companyUUID := companyLinks[companyID]; companyUUID != "" {
				recipients = append(recipients, mockRefWithTitle{
					UUID:  companyUUID,
					Title: companyTitleByID[companyID],
				})
			}
		}
		sort.Slice(recipients, func(i, j int) bool { return recipients[i].UUID < recipients[j].UUID })

		out = append(out, contractExport{
			LastModifiedDate: formatTime(c.LastModifiedDate),
			State:            utils.SafeStringDereference(c.State),
			Services:         serviceExportItems,
			StateStartTime:   formatTime(c.StateStartTime),
			UUID:             extUUID,
			RecipientsOU:     recipients,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].UUID < out[j].UUID })
	return out
}

func (s *Seeder) buildTicketExport(
	allTickets []tickets.Ticket,
	allComments []tickets.TicketComment,
	linksByEntity map[string]map[string]string,
	linksByInternal map[string]string,
	fileUUIDByStorageKey map[string]string,
	commentFilesByUUID map[string][]string,
) []ticketExportItem {
	linkReplacer := buildTicketLinkReplacer(fileUUIDByStorageKey)
	commentsByTicketID := make(map[string][]tickets.TicketComment)
	for _, comment := range allComments {
		commentsByTicketID[comment.TicketID] = append(commentsByTicketID[comment.TicketID], comment)
	}

	ticketLinks := linksByEntity["Ticket"]
	commentLinks := linksByEntity["TicketComment"]

	out := make([]ticketExportItem, 0, len(allTickets))
	for _, t := range allTickets {
		ticketUUID := strings.TrimSpace(t.ServiceDeskUUID)
		if ticketUUID == "" && ticketLinks != nil {
			ticketUUID = ticketLinks[t.ID]
		}
		if ticketUUID == "" {
			continue
		}

		var agreement *mockRef
		if t.ContractID != nil {
			if contractUUID := linksByInternal[*t.ContractID]; contractUUID != "" {
				agreement = &mockRef{UUID: contractUUID}
			}
		}

		var clientOU *mockRef
		if companyUUID := linksByInternal[t.CompanyID]; companyUUID != "" {
			clientOU = &mockRef{UUID: companyUUID}
		}

		comments := commentsByTicketID[t.ID]
		exportComments := make([]ticketCommentItem, 0, len(comments))
		for _, c := range comments {
			commentUUID := strings.TrimSpace(c.ServiceDeskUUID)
			if commentUUID == "" && commentLinks != nil {
				commentUUID = commentLinks[c.ID]
			}

			files := make([]any, 0)
			if commentUUID != "" {
				for _, fileUUID := range commentFilesByUUID[commentUUID] {
					files = append(files, map[string]any{"UUID": fileUUID})
				}
			}

			exportComments = append(exportComments, ticketCommentItem{
				UUID: commentUUID,
				Author: ticketCommentAuthor{
					Title: c.AuthorName,
				},
				Private: c.IsInternal,
				Files:   files,
				Text:    rewriteTicketTextLinks(c.Text, linkReplacer),
			})
		}

		out = append(out, ticketExportItem{
			Number:           t.Number,
			Agreement:        agreement,
			DescriptionRTF:   rewriteTicketTextLinks(t.Description, linkReplacer),
			ResultDescr:      rewriteTicketTextLinks(t.Result, linkReplacer),
			ClientOU:         clientOU,
			LastModifiedDate: formatTime(&t.UpdatedAt),
			RequestDate:      formatTime(&t.CreatedAt),
			State:            t.Status,
			UUID:             ticketUUID,
			Comments:         exportComments,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Number == out[j].Number {
			return out[i].UUID < out[j].UUID
		}
		return out[i].Number < out[j].Number
	})
	return out
}

func (s *Seeder) exportFiles(
	outputDir string,
	storageRoot string,
	fileAssets []tickets.FileAsset,
	fileLinks []tickets.TicketFileLink,
	linksByEntity map[string]map[string]string,
) (map[string]string, map[string][]string, []fileManifestItem, error) {
	if strings.TrimSpace(storageRoot) == "" {
		storageRoot = filepath.Join("storage", "tickets")
	}

	filesDir := filepath.Join(outputDir, "files")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("не удалось создать директорию файлов: %w", err)
	}

	fileUUIDByID := linksByEntity["File"]
	ticketUUIDByID := linksByEntity["Ticket"]
	if fileUUIDByID == nil {
		fileUUIDByID = map[string]string{}
	}
	if ticketUUIDByID == nil {
		ticketUUIDByID = map[string]string{}
	}

	type relationInfo struct {
		ticketUUID   string
		commentUUID  string
		relationType string
	}
	relationsByFileID := make(map[string][]relationInfo)
	for _, l := range fileLinks {
		relationsByFileID[l.FileID] = append(relationsByFileID[l.FileID], relationInfo{
			ticketUUID:   ticketUUIDByID[l.TicketID],
			commentUUID:  strings.TrimSpace(utils.SafeStringDereference(l.CommentUUID)),
			relationType: l.RelationType,
		})
	}

	fileUUIDByStorageKey := make(map[string]string)
	commentFilesByUUID := make(map[string][]string)
	manifest := make([]fileManifestItem, 0)

	for _, asset := range fileAssets {
		fileUUID := strings.TrimSpace(fileUUIDByID[asset.ID])
		if fileUUID == "" {
			continue
		}

		storageKey := filepath.ToSlash(asset.StorageKey)
		if storageKey != "" {
			fileUUIDByStorageKey[storageKey] = fileUUID
		}

		ext := strings.ToLower(filepath.Ext(asset.OriginalName))
		if ext == "" {
			ext = strings.ToLower(filepath.Ext(storageKey))
		}
		exportName := fileUUID
		if ext != "" {
			exportName += ext
		}
		exportRelPath := filepath.ToSlash(filepath.Join("files", exportName))
		exportAbsPath := filepath.Join(outputDir, filepath.FromSlash(exportRelPath))

		srcAbsPath := filepath.Join(storageRoot, filepath.FromSlash(storageKey))
		missingSource := false
		if err := copyFileIfExists(srcAbsPath, exportAbsPath); err != nil {
			if os.IsNotExist(err) {
				missingSource = true
			} else {
				return nil, nil, nil, fmt.Errorf("не удалось скопировать файл %s: %w", srcAbsPath, err)
			}
		}

		info := fileManifestItem{
			UUID:          fileUUID,
			FileID:        asset.ID,
			StorageKey:    storageKey,
			OriginalName:  asset.OriginalName,
			MimeType:      asset.MimeType,
			Size:          asset.Size,
			Checksum:      asset.Checksum,
			ExportPath:    exportRelPath,
			MissingSource: missingSource,
		}

		ticketSet := make(map[string]struct{})
		commentSet := make(map[string]struct{})
		relationSet := make(map[string]struct{})
		for _, rel := range relationsByFileID[asset.ID] {
			if rel.ticketUUID != "" {
				ticketSet[rel.ticketUUID] = struct{}{}
			}
			if rel.commentUUID != "" {
				commentSet[rel.commentUUID] = struct{}{}
				commentFilesByUUID[rel.commentUUID] = appendUnique(commentFilesByUUID[rel.commentUUID], fileUUID)
			}
			if rel.relationType != "" {
				relationSet[rel.relationType] = struct{}{}
			}
		}

		info.TicketUUIDs = setToSortedSlice(ticketSet)
		info.CommentUUIDs = setToSortedSlice(commentSet)
		info.RelationTypes = setToSortedSlice(relationSet)
		manifest = append(manifest, info)
	}

	sort.Slice(manifest, func(i, j int) bool { return manifest[i].UUID < manifest[j].UUID })
	return fileUUIDByStorageKey, commentFilesByUUID, manifest, nil
}

func buildTicketLinkReplacer(fileUUIDByStorageKey map[string]string) *strings.Replacer {
	if len(fileUUIDByStorageKey) == 0 {
		return nil
	}
	pairs := make([]string, 0, len(fileUUIDByStorageKey)*4)
	for storageKey, fileUUID := range fileUUIDByStorageKey {
		target := "./download?uuid=" + fileUUID
		pairs = append(pairs, "/api/static/tickets/"+storageKey, target)
		pairs = append(pairs, "/static/tickets/"+storageKey, target)
	}
	return strings.NewReplacer(pairs...)
}

func rewriteTicketTextLinks(text string, replacer *strings.Replacer) string {
	if replacer == nil || text == "" {
		return text
	}
	return replacer.Replace(text)
}

func copyFileIfExists(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func setToSortedSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func appendUnique(slice []string, value string) []string {
	for _, v := range slice {
		if v == value {
			return slice
		}
	}
	return append(slice, value)
}

func parseServiceTitles(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var titles []string
	if err := json.Unmarshal(raw, &titles); err != nil {
		return nil
	}
	filtered := make([]string, 0, len(titles))
	for _, title := range titles {
		if strings.TrimSpace(title) != "" {
			filtered = append(filtered, title)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func makeOwnerRef(ownerID *string, linksByInternal map[string]string, companyTitleByID map[string]string) *mockRefWithTitle {
	if ownerID == nil || *ownerID == "" {
		return nil
	}
	ownerUUID := linksByInternal[*ownerID]
	if ownerUUID == "" {
		return nil
	}
	return &mockRefWithTitle{
		UUID:  ownerUUID,
		Title: companyTitleByID[*ownerID],
	}
}

func formatRNKKTForExport(value *string) *string {
	if value == nil {
		return nil
	}
	formatted := utils.FormatRNKKT(*value)
	return &formatted
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(utils.TimeLayoutServiceDesk)
}

func writeJSON(path string, payload any) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("не удалось создать файл %s: %w", path, err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("не удалось записать JSON в %s: %w", path, err)
	}
	return nil
}
