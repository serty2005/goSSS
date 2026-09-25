package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/logger"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

const checklistTitleMaxLength = 1000

var (
	ErrChecklistItemNotFound     = errors.New("пункт чеклиста не найден")
	ErrChecklistTemplateNotFound = errors.New("шаблон чеклиста не найден")
	ErrChecklistValidation       = errors.New("некорректные данные чеклиста")
)

// ChecklistUserRef описывает пользователя в пункте чеклиста.
type ChecklistUserRef struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// TicketChecklistItemView описывает пункт чеклиста для отображения.
type TicketChecklistItemView struct {
	ID          string             `json:"id"`
	TicketID    string             `json:"ticket_id"`
	ParentID    *string            `json:"parent_id"`
	Title       string             `json:"title"`
	Position    int                `json:"position"`
	IsDone      bool               `json:"is_done"`
	DoneAt      *time.Time         `json:"done_at,omitempty"`
	DoneBy      *ChecklistUserRef  `json:"done_by,omitempty"`
	CreatedByID *uint              `json:"created_by_id,omitempty"`
	Assignees   []ChecklistUserRef `json:"assignees"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// ChecklistItemCreateInput описывает создание пунктов. Каждая непустая строка Title становится отдельным пунктом.
type ChecklistItemCreateInput struct {
	Title       string
	ParentID    *string
	AssigneeIDs []uint
}

// ChecklistItemUpdateInput описывает частичное изменение пункта.
type ChecklistItemUpdateInput struct {
	Title       *string
	IsDone      *bool
	AssigneeIDs *[]uint
}

// ChecklistTemplateInput описывает создание или изменение шаблона чеклиста.
type ChecklistTemplateInput struct {
	Title       string
	Description string
	Items       []tickets.ChecklistTemplateNode
	IsActive    bool
}

// TicketChecklistService управляет локальными чеклистами тикетов и их шаблонами.
type TicketChecklistService interface {
	List(ctx context.Context, ticketID string) ([]TicketChecklistItemView, error)
	CreateItems(ctx context.Context, ticketID string, input ChecklistItemCreateInput, actorID uint) ([]TicketChecklistItemView, error)
	UpdateItem(ctx context.Context, ticketID string, itemID string, input ChecklistItemUpdateInput, actorID uint) (*TicketChecklistItemView, error)
	DeleteItem(ctx context.Context, ticketID string, itemID string, actorID uint) error
	Reorder(ctx context.Context, ticketID string, parentID *string, orderedIDs []string) error
	ApplyTemplate(ctx context.Context, ticketID string, templateID string, actorID uint) ([]TicketChecklistItemView, error)

	ListTemplates(ctx context.Context, activeOnly bool) ([]tickets.ChecklistTemplate, error)
	CreateTemplate(ctx context.Context, input ChecklistTemplateInput, actorID uint) (*tickets.ChecklistTemplate, error)
	UpdateTemplate(ctx context.Context, id string, input ChecklistTemplateInput) (*tickets.ChecklistTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error
}

type ticketChecklistService struct {
	repo          tickets.ChecklistRepository
	ticketRepo    tickets.TicketRepository
	userRepo      user.Repository
	historyWriter TicketHistoryWriter
	logger        logger.LoggerInterface
}

// NewTicketChecklistService создает сервис чеклистов тикетов.
func NewTicketChecklistService(
	repo tickets.ChecklistRepository,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
	log logger.LoggerInterface,
) TicketChecklistService {
	return &ticketChecklistService{
		repo:          repo,
		ticketRepo:    ticketRepo,
		userRepo:      userRepo,
		historyWriter: NewTicketHistoryWriter(ticketRepo, log),
		logger:        log,
	}
}

func (s *ticketChecklistService) List(ctx context.Context, ticketID string) ([]TicketChecklistItemView, error) {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListItems(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	return s.toViews(ctx, items), nil
}

func (s *ticketChecklistService) CreateItems(ctx context.Context, ticketID string, input ChecklistItemCreateInput, actorID uint) ([]TicketChecklistItemView, error) {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	titles := splitChecklistTitles(input.Title)
	if len(titles) == 0 {
		return nil, fmt.Errorf("%w: текст пункта пустой", ErrChecklistValidation)
	}
	for _, title := range titles {
		if utf8.RuneCountInString(title) > checklistTitleMaxLength {
			return nil, fmt.Errorf("%w: текст пункта длиннее %d символов", ErrChecklistValidation, checklistTitleMaxLength)
		}
	}

	parentID := normalizeOptionalID(input.ParentID)
	if parentID != nil {
		existing, err := s.repo.ListItems(ctx, ticketID)
		if err != nil {
			return nil, err
		}
		depth, ok := checklistItemDepth(existing, *parentID)
		if !ok {
			return nil, ErrChecklistItemNotFound
		}
		if depth >= tickets.ChecklistMaxDepth {
			return nil, fmt.Errorf("%w: допускается не более %d уровней вложенности", ErrChecklistValidation, tickets.ChecklistMaxDepth)
		}
	}

	assigneeIDs, err := s.normalizeAssignees(ctx, input.AssigneeIDs)
	if err != nil {
		return nil, err
	}

	position, err := s.repo.NextPosition(ctx, ticketID, parentID)
	if err != nil {
		return nil, err
	}
	actor := optionalUserID(actorID)
	items := make([]tickets.TicketChecklistItem, 0, len(titles))
	for index, title := range titles {
		item := tickets.TicketChecklistItem{
			TicketID:    ticketID,
			ParentID:    parentID,
			Title:       title,
			Position:    position + index,
			CreatedByID: actor,
		}
		for _, userID := range assigneeIDs {
			item.Assignees = append(item.Assignees, tickets.TicketChecklistItemAssignee{UserID: userID})
		}
		items = append(items, item)
	}
	if err := s.repo.CreateItems(ctx, items); err != nil {
		return nil, err
	}

	for _, item := range items {
		s.writeHistory(ctx, ticketID, actor, "Добавлен пункт «"+item.Title+"»", map[string]interface{}{"item_id": item.ID})
	}
	return s.toViews(ctx, items), nil
}

func (s *ticketChecklistService) UpdateItem(ctx context.Context, ticketID string, itemID string, input ChecklistItemUpdateInput, actorID uint) (*TicketChecklistItemView, error) {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	item, err := s.repo.GetItem(ctx, ticketID, itemID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrChecklistItemNotFound
	}

	updates := map[string]interface{}{}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: текст пункта пустой", ErrChecklistValidation)
		}
		if utf8.RuneCountInString(title) > checklistTitleMaxLength {
			return nil, fmt.Errorf("%w: текст пункта длиннее %d символов", ErrChecklistValidation, checklistTitleMaxLength)
		}
		if title != item.Title {
			updates["title"] = title
		}
	}
	actor := optionalUserID(actorID)
	doneChanged := false
	if input.IsDone != nil && *input.IsDone != item.IsDone {
		doneChanged = true
		updates["is_done"] = *input.IsDone
		if *input.IsDone {
			now := time.Now()
			updates["done_at"] = now
			updates["done_by_id"] = actor
		} else {
			updates["done_at"] = nil
			updates["done_by_id"] = nil
		}
	}
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		if err := s.repo.UpdateItem(ctx, ticketID, itemID, updates); err != nil {
			return nil, err
		}
	}
	if input.AssigneeIDs != nil {
		assigneeIDs, err := s.normalizeAssignees(ctx, *input.AssigneeIDs)
		if err != nil {
			return nil, err
		}
		if err := s.repo.ReplaceAssignees(ctx, itemID, assigneeIDs); err != nil {
			return nil, err
		}
	}

	updated, err := s.repo.GetItem(ctx, ticketID, itemID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrChecklistItemNotFound
	}
	if doneChanged {
		text := "Выполнен пункт «" + updated.Title + "»"
		if !updated.IsDone {
			text = "Снята отметка с пункта «" + updated.Title + "»"
		}
		s.writeHistory(ctx, ticketID, actor, text, map[string]interface{}{"item_id": itemID})
	}
	views := s.toViews(ctx, []tickets.TicketChecklistItem{*updated})
	return &views[0], nil
}

func (s *ticketChecklistService) DeleteItem(ctx context.Context, ticketID string, itemID string, actorID uint) error {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return err
	}
	items, err := s.repo.ListItems(ctx, ticketID)
	if err != nil {
		return err
	}
	var target *tickets.TicketChecklistItem
	for i := range items {
		if items[i].ID == itemID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return ErrChecklistItemNotFound
	}
	ids := collectChecklistSubtree(items, itemID)
	if err := s.repo.DeleteItems(ctx, ticketID, ids); err != nil {
		return err
	}
	text := "Удалён пункт «" + target.Title + "»"
	if len(ids) > 1 {
		text += fmt.Sprintf(" вместе с подпунктами (%d)", len(ids)-1)
	}
	s.writeHistory(ctx, ticketID, optionalUserID(actorID), text, map[string]interface{}{"item_id": itemID})
	return nil
}

func (s *ticketChecklistService) Reorder(ctx context.Context, ticketID string, parentID *string, orderedIDs []string) error {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return err
	}
	items, err := s.repo.ListItems(ctx, ticketID)
	if err != nil {
		return err
	}
	byID := make(map[string]tickets.TicketChecklistItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	parentID = normalizeOptionalID(parentID)
	parentDepth := 0
	if parentID != nil {
		depth, ok := checklistItemDepth(items, *parentID)
		if !ok {
			return ErrChecklistItemNotFound
		}
		parentDepth = depth
	}

	seen := make(map[string]struct{}, len(orderedIDs))
	normalized := make([]string, 0, len(orderedIDs))
	for _, rawID := range orderedIDs {
		itemID := strings.TrimSpace(rawID)
		if itemID == "" {
			continue
		}
		if _, dup := seen[itemID]; dup {
			continue
		}
		seen[itemID] = struct{}{}
		if _, ok := byID[itemID]; !ok {
			return ErrChecklistItemNotFound
		}
		if parentID != nil {
			for _, descendantID := range collectChecklistSubtree(items, itemID) {
				if descendantID == *parentID {
					return fmt.Errorf("%w: нельзя переместить пункт внутрь собственного подпункта", ErrChecklistValidation)
				}
			}
		}
		if parentDepth+checklistSubtreeHeight(items, itemID) > tickets.ChecklistMaxDepth {
			return fmt.Errorf("%w: допускается не более %d уровней вложенности", ErrChecklistValidation, tickets.ChecklistMaxDepth)
		}
		normalized = append(normalized, itemID)
	}
	if len(normalized) == 0 {
		return nil
	}
	return s.repo.UpdatePositions(ctx, ticketID, parentID, normalized)
}

func (s *ticketChecklistService) ApplyTemplate(ctx context.Context, ticketID string, templateID string, actorID uint) ([]TicketChecklistItemView, error) {
	if err := s.ensureTicket(ctx, ticketID); err != nil {
		return nil, err
	}
	template, err := s.repo.GetTemplate(ctx, strings.TrimSpace(templateID))
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, ErrChecklistTemplateNotFound
	}
	nodes := template.Items.Data()
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: шаблон не содержит пунктов", ErrChecklistValidation)
	}
	position, err := s.repo.NextPosition(ctx, ticketID, nil)
	if err != nil {
		return nil, err
	}

	actor := optionalUserID(actorID)
	items := make([]tickets.TicketChecklistItem, 0)
	var build func(nodes []tickets.ChecklistTemplateNode, parentID *string, startPosition int)
	build = func(nodes []tickets.ChecklistTemplateNode, parentID *string, startPosition int) {
		index := 0
		for _, node := range nodes {
			title := strings.TrimSpace(node.Title)
			if title == "" {
				continue
			}
			item := tickets.TicketChecklistItem{
				ID:          newChecklistID(),
				TicketID:    ticketID,
				ParentID:    parentID,
				Title:       title,
				Position:    startPosition + index,
				CreatedByID: actor,
			}
			items = append(items, item)
			index++
			if len(node.Children) > 0 {
				itemID := item.ID
				build(node.Children, &itemID, 0)
			}
		}
	}
	build(nodes, nil, position)
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: шаблон не содержит пунктов", ErrChecklistValidation)
	}
	if err := s.repo.CreateItems(ctx, items); err != nil {
		return nil, err
	}
	s.writeHistory(ctx, ticketID, actor, fmt.Sprintf("Применён шаблон «%s» (%d пунктов)", template.Title, len(items)), map[string]interface{}{"template_id": template.ID})
	return s.toViews(ctx, items), nil
}

func (s *ticketChecklistService) ListTemplates(ctx context.Context, activeOnly bool) ([]tickets.ChecklistTemplate, error) {
	return s.repo.ListTemplates(ctx, activeOnly)
}

func (s *ticketChecklistService) CreateTemplate(ctx context.Context, input ChecklistTemplateInput, actorID uint) (*tickets.ChecklistTemplate, error) {
	template, err := buildChecklistTemplate(input)
	if err != nil {
		return nil, err
	}
	template.CreatedByID = optionalUserID(actorID)
	if err := s.repo.CreateTemplate(ctx, template); err != nil {
		return nil, err
	}
	return s.repo.GetTemplate(ctx, template.ID)
}

func (s *ticketChecklistService) UpdateTemplate(ctx context.Context, id string, input ChecklistTemplateInput) (*tickets.ChecklistTemplate, error) {
	existing, err := s.repo.GetTemplate(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrChecklistTemplateNotFound
	}
	template, err := buildChecklistTemplate(input)
	if err != nil {
		return nil, err
	}
	template.ID = existing.ID
	if err := s.repo.UpdateTemplate(ctx, template); err != nil {
		return nil, err
	}
	return s.repo.GetTemplate(ctx, existing.ID)
}

func (s *ticketChecklistService) DeleteTemplate(ctx context.Context, id string) error {
	existing, err := s.repo.GetTemplate(ctx, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrChecklistTemplateNotFound
	}
	return s.repo.DeleteTemplate(ctx, existing.ID)
}

func (s *ticketChecklistService) ensureTicket(ctx context.Context, ticketID string) error {
	if strings.TrimSpace(ticketID) == "" {
		return ErrTicketNotFound
	}
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return ErrTicketNotFound
	}
	return nil
}

func (s *ticketChecklistService) normalizeAssignees(ctx context.Context, ids []uint) ([]uint, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	result := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		u, err := s.userRepo.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("не удалось проверить исполнителя %d: %w", id, err)
		}
		if u == nil {
			return nil, fmt.Errorf("%w: исполнитель %d не найден", ErrChecklistValidation, id)
		}
		result = append(result, id)
	}
	return result, nil
}

func (s *ticketChecklistService) writeHistory(ctx context.Context, ticketID string, actor *uint, text string, meta map[string]interface{}) {
	if s.historyWriter == nil {
		return
	}
	s.historyWriter.Write(ctx, TicketHistoryWriteRequest{
		TicketID: ticketID,
		UserID:   actor,
		Action:   tickets.HistoryActionChecklistChanged,
		Field:    tickets.HistoryFieldChecklist,
		Source:   tickets.HistorySourceUI,
		NewValue: text,
		Meta:     meta,
	})
}

func (s *ticketChecklistService) toViews(ctx context.Context, items []tickets.TicketChecklistItem) []TicketChecklistItemView {
	names := s.loadUserNames(ctx)
	userRef := func(id uint) ChecklistUserRef {
		name := names[id]
		if name == "" {
			name = fmt.Sprintf("Пользователь #%d", id)
		}
		return ChecklistUserRef{ID: id, Name: name}
	}
	result := make([]TicketChecklistItemView, 0, len(items))
	for _, item := range items {
		view := TicketChecklistItemView{
			ID:          item.ID,
			TicketID:    item.TicketID,
			ParentID:    item.ParentID,
			Title:       item.Title,
			Position:    item.Position,
			IsDone:      item.IsDone,
			DoneAt:      item.DoneAt,
			CreatedByID: item.CreatedByID,
			Assignees:   make([]ChecklistUserRef, 0, len(item.Assignees)),
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
		}
		if item.DoneByID != nil && *item.DoneByID > 0 {
			ref := userRef(*item.DoneByID)
			view.DoneBy = &ref
		}
		for _, assignee := range item.Assignees {
			view.Assignees = append(view.Assignees, userRef(assignee.UserID))
		}
		sort.SliceStable(view.Assignees, func(i, j int) bool { return view.Assignees[i].Name < view.Assignees[j].Name })
		result = append(result, view)
	}
	return result
}

func (s *ticketChecklistService) loadUserNames(ctx context.Context) map[uint]string {
	names := map[uint]string{}
	if s.userRepo == nil {
		return names
	}
	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		s.logger.Warn("не удалось загрузить пользователей для чеклиста", "error", err)
		return names
	}
	for _, u := range users {
		name := strings.TrimSpace(u.FullName)
		if name == "" {
			name = strings.TrimSpace(u.Username)
		}
		names[u.ID] = name
	}
	return names
}

func buildChecklistTemplate(input ChecklistTemplateInput) (*tickets.ChecklistTemplate, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: название шаблона обязательно", ErrChecklistValidation)
	}
	nodes, count, err := normalizeTemplateNodes(input.Items, 1)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: шаблон должен содержать хотя бы один пункт", ErrChecklistValidation)
	}
	return &tickets.ChecklistTemplate{
		Title:       title,
		Description: strings.TrimSpace(input.Description),
		Items:       datatypes.NewJSONType(nodes),
		IsActive:    input.IsActive,
	}, nil
}

func normalizeTemplateNodes(nodes []tickets.ChecklistTemplateNode, depth int) ([]tickets.ChecklistTemplateNode, int, error) {
	result := make([]tickets.ChecklistTemplateNode, 0, len(nodes))
	count := 0
	for _, node := range nodes {
		title := strings.TrimSpace(node.Title)
		if title == "" {
			continue
		}
		if utf8.RuneCountInString(title) > checklistTitleMaxLength {
			return nil, 0, fmt.Errorf("%w: текст пункта длиннее %d символов", ErrChecklistValidation, checklistTitleMaxLength)
		}
		if len(node.Children) > 0 && depth >= tickets.ChecklistMaxDepth {
			return nil, 0, fmt.Errorf("%w: допускается не более %d уровней вложенности", ErrChecklistValidation, tickets.ChecklistMaxDepth)
		}
		children, childCount, err := normalizeTemplateNodes(node.Children, depth+1)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, tickets.ChecklistTemplateNode{Title: title, Children: children})
		count += 1 + childCount
	}
	return result, count, nil
}

func splitChecklistTitles(value string) []string {
	lines := strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if title := strings.TrimSpace(line); title != "" {
			result = append(result, title)
		}
	}
	return result
}

// checklistItemDepth возвращает уровень пункта (1 - корневой) и признак его наличия.
func checklistItemDepth(items []tickets.TicketChecklistItem, itemID string) (int, bool) {
	parents := make(map[string]*string, len(items))
	for _, item := range items {
		parents[item.ID] = item.ParentID
	}
	if _, ok := parents[itemID]; !ok {
		return 0, false
	}
	depth := 1
	current := parents[itemID]
	for current != nil && depth <= len(items) {
		next, ok := parents[*current]
		if !ok {
			break
		}
		depth++
		current = next
	}
	return depth, true
}

// collectChecklistSubtree возвращает ID пункта и всех его потомков.
func collectChecklistSubtree(items []tickets.TicketChecklistItem, rootID string) []string {
	children := make(map[string][]string, len(items))
	for _, item := range items {
		if item.ParentID != nil {
			children[*item.ParentID] = append(children[*item.ParentID], item.ID)
		}
	}
	result := []string{rootID}
	seen := map[string]struct{}{rootID: {}}
	for i := 0; i < len(result); i++ {
		for _, childID := range children[result[i]] {
			if _, ok := seen[childID]; ok {
				continue
			}
			seen[childID] = struct{}{}
			result = append(result, childID)
		}
	}
	return result
}

// checklistSubtreeHeight возвращает высоту поддерева пункта (1 - пункт без подпунктов).
func checklistSubtreeHeight(items []tickets.TicketChecklistItem, rootID string) int {
	children := make(map[string][]string, len(items))
	for _, item := range items {
		if item.ParentID != nil {
			children[*item.ParentID] = append(children[*item.ParentID], item.ID)
		}
	}
	var height func(id string, guard int) int
	height = func(id string, guard int) int {
		if guard > len(items) {
			return 1
		}
		maxChild := 0
		for _, childID := range children[id] {
			if h := height(childID, guard+1); h > maxChild {
				maxChild = h
			}
		}
		return 1 + maxChild
	}
	return height(rootID, 0)
}

func newChecklistID() string {
	return uuid.New().String()
}

func normalizeOptionalID(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalUserID(id uint) *uint {
	if id == 0 {
		return nil
	}
	value := id
	return &value
}
