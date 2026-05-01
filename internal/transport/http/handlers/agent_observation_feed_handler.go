package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AgentObservationFeedHandler struct {
	db *gorm.DB
}

func NewAgentObservationFeedHandler(db *gorm.DB) *AgentObservationFeedHandler {
	return &AgentObservationFeedHandler{db: db}
}

func (h *AgentObservationFeedHandler) RegisterRoutes(r chi.Router) {
	r.Get("/agent-observations", h.ListLatestByAgent)
	r.Get("/agent-observations/{id}", h.GetObservationByID)
	r.Get("/agents-list", h.ListAgents)
}

type observationFeedDBRow struct {
	ID            uint           `gorm:"column:id"`
	ObservedAt    time.Time      `gorm:"column:observed_at"`
	Source        string         `gorm:"column:source"`
	AgentUUID     *string        `gorm:"column:agent_uuid"`
	WorkstationID *string        `gorm:"column:workstation_id"`
	FRID          *string        `gorm:"column:fr_id"`
	PayloadJSON   datatypes.JSON `gorm:"column:payload_json"`
}

type observationPayload struct {
	AgentUUID      string `json:"uuid"`
	AgentUUIDAlias string `json:"agent_uuid"`
	URLRms         string `json:"url_rms"`
	Current        string `json:"current_time"`
	VTime          string `json:"v_time"`
	VC             string `json:"vc"`
}

type observationFeedRow struct {
	ObservationID   uint       `json:"observation_id"`
	AgentUUID       *string    `json:"agent_uuid"`
	AgentVC         *string    `json:"vc"`
	WorkstationID   *string    `json:"workstation_id"`
	WorkstationName *string    `json:"workstation_name"`
	FRID            *string    `json:"fr_id"`
	FRName          *string    `json:"fr_name"`
	OwnerMatch      *bool      `json:"owner_match"`
	ObservedAt      time.Time  `json:"observed_at"`
	CurrentTimeRaw  *string    `json:"current_time"`
	VTimeRaw        *string    `json:"v_time"`
	CurrentTime     *time.Time `json:"current_time_parsed"`
	VTime           *time.Time `json:"v_time_parsed"`
	ServerURL       *string    `json:"server_url"`
}

func (h *AgentObservationFeedHandler) GetObservationByID(w http.ResponseWriter, r *http.Request) {
	idRaw := strings.TrimSpace(chi.URLParam(r, "id"))
	id, err := strconv.ParseUint(idRaw, 10, 64)
	if err != nil || id == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный id наблюдения")
		return
	}

	var item models.AgentObservation
	if err := h.db.WithContext(r.Context()).Where("id = ?", uint(id)).First(&item).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.RespondWithError(w, http.StatusNotFound, "Наблюдение не найдено")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить наблюдение")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":             item.ID,
		"source":         item.Source,
		"status":         item.Status,
		"observed_at":    item.ObservedAt,
		"workstation_id": item.WorkstationID,
		"fr_id":          item.FRID,
		"payload_json":   item.PayloadJSON,
		"created_at":     item.CreatedAt,
		"updated_at":     item.UpdatedAt,
	})
}

func (h *AgentObservationFeedHandler) ListLatestByAgent(w http.ResponseWriter, r *http.Request) {
	limit := 5000
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 10000 {
			limit = parsed
		}
	}

	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_by")))
	if sortBy == "" {
		sortBy = "latest"
	}
	order := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order")))
	if order != "asc" {
		order = "desc"
	}

	filterAgent := strings.TrimSpace(r.URL.Query().Get("agent_uuid"))
	filterWS := strings.TrimSpace(r.URL.Query().Get("workstation_id"))
	filterFR := strings.TrimSpace(r.URL.Query().Get("fr_id"))

	var rawRows []observationFeedDBRow
	if err := h.db.WithContext(r.Context()).
		Model(&models.AgentObservation{}).
		Select("id, observed_at, source, agent_uuid, workstation_id, fr_id, payload_json").
		Order("observed_at DESC, id DESC").
		Limit(limit).
		Find(&rawRows).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить наблюдения")
		return
	}

	latestByAgent := make(map[string]observationFeedRow, len(rawRows))
	for i := range rawRows {
		raw := rawRows[i]
		row := parseObservationFeedRow(raw)

		key := strings.TrimSpace(trimPtrValue(row.AgentUUID))
		if key == "" {
			key = strings.TrimSpace(raw.Source)
		}
		if key == "" {
			key = "observation:" + strconv.FormatUint(uint64(raw.ID), 10)
		}
		if _, exists := latestByAgent[key]; exists {
			continue
		}
		latestByAgent[key] = row
	}

	result := make([]observationFeedRow, 0, len(latestByAgent))
	wsIDs := make([]string, 0, len(latestByAgent))
	frIDs := make([]string, 0, len(latestByAgent))
	for _, row := range latestByAgent {
		if filterAgent != "" && trimPtrValue(row.AgentUUID) != filterAgent {
			continue
		}
		if filterWS != "" && trimPtrValue(row.WorkstationID) != filterWS {
			continue
		}
		if filterFR != "" && trimPtrValue(row.FRID) != filterFR {
			continue
		}
		result = append(result, row)
		if row.WorkstationID != nil && strings.TrimSpace(*row.WorkstationID) != "" {
			wsIDs = append(wsIDs, strings.TrimSpace(*row.WorkstationID))
		}
		if row.FRID != nil && strings.TrimSpace(*row.FRID) != "" {
			frIDs = append(frIDs, strings.TrimSpace(*row.FRID))
		}
	}

	wsMeta := h.loadWorkstationMeta(r, wsIDs)
	frMeta := h.loadFRMeta(r, frIDs)
	for i := range result {
		wsInfo, wsExists := wsMeta[trimPtrValue(result[i].WorkstationID)]
		frInfo, frExists := frMeta[trimPtrValue(result[i].FRID)]
		if wsExists && wsInfo.Name != "" {
			result[i].WorkstationName = stringPtrOrNil(wsInfo.Name)
		}
		if frExists && frInfo.Name != "" {
			result[i].FRName = stringPtrOrNil(frInfo.Name)
		}
		if wsExists && frExists {
			value := wsInfo.OwnerID == frInfo.OwnerID && wsInfo.OwnerID != ""
			result[i].OwnerMatch = &value
		}
	}

	sort.Slice(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		switch sortBy {
		case "v_time":
			return compareTimes(left.VTime, right.VTime, order == "asc")
		case "current_time":
			return compareTimes(left.CurrentTime, right.CurrentTime, order == "asc")
		default:
			return compareTimeValues(left.ObservedAt, right.ObservedAt, order == "asc")
		}
	})

	response.RespondWithJSON(w, http.StatusOK, result)
}

func (h *AgentObservationFeedHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	term := strings.TrimSpace(r.URL.Query().Get("term"))

	query := h.db.WithContext(r.Context()).Model(&models.Agent{})
	if term != "" {
		query = query.Where("uuid ILIKE ? OR hostname ILIKE ?", "%"+term+"%", "%"+term+"%")
	}
	var items []models.Agent
	if err := query.Order("last_observed_at DESC NULLS LAST, updated_at DESC").Limit(limit).Find(&items).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить список агентов")
		return
	}

	out := make([]map[string]interface{}, 0, len(items))
	for i := range items {
		item := items[i]
		out = append(out, map[string]interface{}{
			"uuid":             item.UUID,
			"hostname":         item.Hostname,
			"type":             item.Type,
			"status":           item.Status,
			"owner_id":         item.OwnerID,
			"workstation_id":   item.WorkstationID,
			"last_observed_at": item.LastObservedAt,
			"last_heartbeat":   item.LastHeartbeat,
		})
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func parseObservationFeedRow(raw observationFeedDBRow) observationFeedRow {
	row := observationFeedRow{
		ObservationID: raw.ID,
		ObservedAt:    raw.ObservedAt,
		WorkstationID: normalizePtr(raw.WorkstationID),
		FRID:          normalizePtr(raw.FRID),
	}
	var payload observationPayload
	if len(raw.PayloadJSON) > 0 {
		_ = json.Unmarshal(raw.PayloadJSON, &payload)
	}

	agentUUID := strings.TrimSpace(trimPtrValue(raw.AgentUUID))
	if agentUUID == "" {
		agentUUID = strings.TrimSpace(payload.AgentUUID)
	}
	if agentUUID == "" {
		agentUUID = strings.TrimSpace(payload.AgentUUIDAlias)
	}
	if agentUUID == "" {
		agentUUID = strings.TrimSpace(raw.Source)
	}
	row.AgentUUID = stringPtrOrNil(agentUUID)
	row.AgentVC = stringPtrOrNil(strings.TrimSpace(payload.VC))
	row.ServerURL = stringPtrOrNil(strings.TrimSpace(payload.URLRms))
	row.CurrentTimeRaw = stringPtrOrNil(strings.TrimSpace(payload.Current))
	row.VTimeRaw = stringPtrOrNil(strings.TrimSpace(payload.VTime))
	row.CurrentTime = parseFlexibleTime(trimPtrValue(row.CurrentTimeRaw))
	row.VTime = parseFlexibleTime(trimPtrValue(row.VTimeRaw))
	return row
}

type observationEntityMeta struct {
	OwnerID string
	Name    string
}

func (h *AgentObservationFeedHandler) loadWorkstationMeta(r *http.Request, ids []string) map[string]observationEntityMeta {
	out := map[string]observationEntityMeta{}
	if len(ids) == 0 {
		return out
	}
	type row struct {
		ID         string  `gorm:"column:id"`
		OwnerID    *string `gorm:"column:owner_id"`
		DeviceName *string `gorm:"column:device_name"`
	}
	var rows []row
	_ = h.db.WithContext(r.Context()).
		Table("workstations").
		Select("id, owner_id, device_name").
		Where("id IN ?", uniqueStrings(ids)).
		Find(&rows).Error
	for i := range rows {
		out[strings.TrimSpace(rows[i].ID)] = observationEntityMeta{
			OwnerID: strings.TrimSpace(trimPtrValue(rows[i].OwnerID)),
			Name:    strings.TrimSpace(trimPtrValue(rows[i].DeviceName)),
		}
	}
	return out
}

func (h *AgentObservationFeedHandler) loadFRMeta(r *http.Request, ids []string) map[string]observationEntityMeta {
	out := map[string]observationEntityMeta{}
	if len(ids) == 0 {
		return out
	}
	type row struct {
		ID             string  `gorm:"column:id"`
		OwnerID        *string `gorm:"column:owner_id"`
		ModelKKT       *string `gorm:"column:model_kkt"`
		RNKKT          *string `gorm:"column:rn_kkt"`
		FRSerialNumber *string `gorm:"column:fr_serial_number"`
	}
	var rows []row
	_ = h.db.WithContext(r.Context()).
		Table("fiscal_registers").
		Select("id, owner_id, model_kkt, rn_kkt, fr_serial_number").
		Where("id IN ?", uniqueStrings(ids)).
		Find(&rows).Error
	for i := range rows {
		name := strings.TrimSpace(trimPtrValue(rows[i].ModelKKT))
		if name == "" {
			name = strings.TrimSpace(trimPtrValue(rows[i].RNKKT))
		}
		if name == "" {
			name = strings.TrimSpace(trimPtrValue(rows[i].FRSerialNumber))
		}
		out[strings.TrimSpace(rows[i].ID)] = observationEntityMeta{
			OwnerID: strings.TrimSpace(trimPtrValue(rows[i].OwnerID)),
			Name:    name,
		}
	}
	return out
}

func compareTimes(left, right *time.Time, asc bool) bool {
	if left == nil && right == nil {
		return false
	}
	if left == nil {
		return !asc
	}
	if right == nil {
		return asc
	}
	return compareTimeValues(*left, *right, asc)
}

func compareTimeValues(left, right time.Time, asc bool) bool {
	if asc {
		return left.Before(right)
	}
	return left.After(right)
}

func parseFlexibleTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"02.01.2006 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func isUUIDLike(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') || (ch >= '0' && ch <= '9') {
			continue
		}
		if ch == '-' {
			continue
		}
		return false
	}
	return true
}

func trimPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func normalizePtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
