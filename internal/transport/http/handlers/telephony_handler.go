package handlers

import (
	"encoding/json"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type TelephonyHandler struct {
	service services.TelephonyService
}

func NewTelephonyHandler(service services.TelephonyService) *TelephonyHandler {
	return &TelephonyHandler{service: service}
}

func (h *TelephonyHandler) GetPendingContextMe(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}

	item, err := h.service.GetPendingContextForUser(r.Context(), getUserIDFromContext(r))
	if err != nil || item == nil || item.PendingContext == nil {
		if err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]any{
		"data": mapPendingContextDTO(item),
	})
}

func (h *TelephonyHandler) BindPendingContext(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "сервис телефонии недоступен")
		return
	}

	var dto api.TelephonyBindPendingContextDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный payload")
		return
	}
	pendingID := strings.TrimSpace(chi.URLParam(r, "id"))
	if pendingID == "" || strings.TrimSpace(dto.TicketID) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "id и ticket_id обязательны")
		return
	}

	err := h.service.BindPendingContextToTicket(
		r.Context(),
		pendingID,
		dto.TicketID,
		dto.ContactName,
		getUserIDFromContext(r),
		getUserRolesFromContext(r),
	)
	switch {
	case err == nil:
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case err == services.ErrTelephonyForbidden:
		response.RespondWithError(w, http.StatusForbidden, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "не найден"):
		response.RespondWithError(w, http.StatusNotFound, err.Error())
	default:
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *TelephonyHandler) BindCallToTicket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "сервис телефонии недоступен")
		return
	}

	var dto api.TelephonyBindCallDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный payload")
		return
	}
	callID := strings.TrimSpace(chi.URLParam(r, "id"))
	if callID == "" || strings.TrimSpace(dto.TicketID) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "id и ticket_id обязательны")
		return
	}

	err := h.service.BindCallToTicket(
		r.Context(),
		callID,
		dto.TicketID,
		dto.ContactName,
		getUserIDFromContext(r),
		getUserRolesFromContext(r),
	)
	switch {
	case err == nil:
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case err == services.ErrTelephonyForbidden:
		response.RespondWithError(w, http.StatusForbidden, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "не найден"):
		response.RespondWithError(w, http.StatusNotFound, err.Error())
	default:
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *TelephonyHandler) UnbindCallFromTicket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "сервис телефонии недоступен")
		return
	}

	var dto api.TelephonyBindCallDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный payload")
		return
	}
	callID := strings.TrimSpace(chi.URLParam(r, "id"))
	if callID == "" || strings.TrimSpace(dto.TicketID) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "id и ticket_id обязательны")
		return
	}

	err := h.service.UnbindCallFromTicket(
		r.Context(),
		callID,
		dto.TicketID,
		getUserIDFromContext(r),
		getUserRolesFromContext(r),
	)
	switch {
	case err == nil:
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case err == services.ErrTelephonyForbidden:
		response.RespondWithError(w, http.StatusForbidden, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "не найден"):
		response.RespondWithError(w, http.StatusNotFound, err.Error())
	default:
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *TelephonyHandler) SetTicketContact(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "сервис телефонии недоступен")
		return
	}

	var dto api.TelephonySetTicketContactDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный payload")
		return
	}
	ticketID := strings.TrimSpace(chi.URLParam(r, "ticketId"))
	if ticketID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ticket_id обязателен")
		return
	}

	err := h.service.SetTicketContact(
		r.Context(),
		ticketID,
		services.TicketContactUpdateInput{
			ContactType:     dto.ContactType,
			Phone:           dto.Phone,
			Telegram:        dto.Telegram,
			ContactName:     dto.ContactName,
			TicketContactID: dto.TicketContactID,
			IsPrimary:       dto.IsPrimary,
			Clear:           dto.Clear,
		},
		getUserIDFromContext(r),
		getUserRolesFromContext(r),
	)
	switch {
	case err == nil:
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case err == services.ErrTelephonyForbidden:
		response.RespondWithError(w, http.StatusForbidden, err.Error())
	case strings.Contains(strings.ToLower(err.Error()), "не найден"):
		response.RespondWithError(w, http.StatusNotFound, err.Error())
	default:
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
	}
}

func (h *TelephonyHandler) ListContactCompanies(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"items": []api.TelephonyContactCompanyDTO{}})
		return
	}

	contactID, err := parseTelephonyUintParam(chi.URLParam(r, "contactId"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный contactId")
		return
	}
	items, err := h.service.ListContactCompanies(r.Context(), contactID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	payload := make([]api.TelephonyContactCompanyDTO, 0, len(items))
	for _, item := range items {
		payload = append(payload, api.TelephonyContactCompanyDTO{
			CompanyID:      item.CompanyID,
			Title:          item.Title,
			ParentTitle:    item.ParentTitle,
			LastSeenAt:     item.LastSeenAt,
			ActiveContract: item.ActiveContact,
		})
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]any{"items": payload})
}

func (h *TelephonyHandler) ListUserCalls(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithJSON(w, http.StatusOK, api.TelephonyCallListResponseDTO{Items: []api.TelephonyCallDTO{}, Total: 0})
		return
	}

	userID, err := parseTelephonyUintParam(chi.URLParam(r, "id"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный user id")
		return
	}

	items, total, err := h.service.ListUserCalls(
		r.Context(),
		userID,
		buildTelephonyCallFilter(r),
		getUserIDFromContext(r),
		getUserRolesFromContext(r),
	)
	if err == services.ErrTelephonyForbidden {
		response.RespondWithError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, api.TelephonyCallListResponseDTO{
		Items: mapTelephonyCallsDTO(items),
		Total: total,
	})
}

func (h *TelephonyHandler) ListCalls(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithJSON(w, http.StatusOK, api.TelephonyCallListResponseDTO{Items: []api.TelephonyCallDTO{}, Total: 0})
		return
	}

	items, total, err := h.service.ListCalls(r.Context(), buildTelephonyCallFilter(r), getUserIDFromContext(r), getUserRolesFromContext(r))
	if err == services.ErrTelephonyForbidden {
		response.RespondWithError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, api.TelephonyCallListResponseDTO{
		Items: mapTelephonyCallsDTO(items),
		Total: total,
	})
}

func (h *TelephonyHandler) GetLine(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithJSON(w, http.StatusOK, api.TelephonyLineDTO{Employees: []api.TelephonyLineEmployeeDTO{}})
		return
	}

	item, err := h.service.GetLineView(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		response.RespondWithJSON(w, http.StatusOK, api.TelephonyLineDTO{Employees: []api.TelephonyLineEmployeeDTO{}})
		return
	}

	payload := api.TelephonyLineDTO{
		Color:           item.Color,
		OnLineCount:     item.OnLineCount,
		MissedOpenCount: item.MissedOpenCount,
		Employees:       make([]api.TelephonyLineEmployeeDTO, 0, len(item.Employees)),
	}
	for _, employee := range item.Employees {
		payload.Employees = append(payload.Employees, api.TelephonyLineEmployeeDTO{
			UserID:       employee.UserID,
			Login:        employee.Login,
			Name:         employee.Name,
			Status:       employee.Status,
			Provider:     employee.Provider,
			ProviderExt:  employee.ProviderExt,
			ProviderLine: employee.ProviderLine,
		})
	}
	response.RespondWithJSON(w, http.StatusOK, payload)
}

func buildTelephonyCallFilter(r *http.Request) services.TelephonyCallFilter {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	var employeeUserID *uint
	if rawEmployeeUserID := strings.TrimSpace(r.URL.Query().Get("employee_user_id")); rawEmployeeUserID != "" {
		if parsedEmployeeUserID, err := parseTelephonyUintParam(rawEmployeeUserID); err == nil && parsedEmployeeUserID > 0 {
			employeeUserID = &parsedEmployeeUserID
		}
	}
	return services.TelephonyCallFilter{
		EmployeeUserID:    employeeUserID,
		ClientPhone:       strings.TrimSpace(r.URL.Query().Get("client_phone")),
		Statuses:          parseStringCSV(r.URL.Query().Get("status")),
		GroupNames:        parseStringCSV(r.URL.Query().Get("group_name")),
		StartedFrom:       parseDateTimeParam(r.URL.Query().Get("started_from"), false),
		StartedTo:         parseDateTimeParam(r.URL.Query().Get("started_to"), true),
		OnlyMissed:        strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("only_missed")), "true"),
		OnlyWithoutTicket: strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("only_without_ticket")), "true"),
		Limit:             limit,
		Offset:            offset,
	}
}

func mapPendingContextDTO(item *services.TelephonyPendingContextView) *api.TelephonyPendingContextDTO {
	if item == nil || item.PendingContext == nil {
		return nil
	}
	dto := &api.TelephonyPendingContextDTO{
		ID:             item.PendingContext.ID,
		ExternalCallID: item.PendingContext.ExternalCallID,
		ClientPhone:    item.PendingContext.ClientPhone,
		ExpiresAt:      item.PendingContext.ExpiresAt,
	}
	if item.Contact != nil {
		dto.Contact = &api.TelephonyContactDTO{
			ID:              item.Contact.ID,
			PhoneNormalized: item.Contact.PhoneNormalized,
			PhoneDisplay:    item.Contact.PhoneDisplay,
			Name:            item.Contact.Name,
			BitrixContactID: item.Contact.BitrixContactID,
		}
	}
	if item.Call != nil {
		call := services.TelephonyCallView{Call: *item.Call, Contact: item.Contact}
		callDTO := mapTelephonyCallDTO(call)
		dto.Call = &callDTO
	}
	return dto
}

func mapTelephonyCallsDTO(items []services.TelephonyCallView) []api.TelephonyCallDTO {
	payload := make([]api.TelephonyCallDTO, 0, len(items))
	for _, item := range items {
		payload = append(payload, mapTelephonyCallDTO(item))
	}
	return payload
}

func mapTelephonyCallDTO(item services.TelephonyCallView) api.TelephonyCallDTO {
	var contact *api.TelephonyContactDTO
	if item.Contact != nil {
		contact = &api.TelephonyContactDTO{
			ID:              item.Contact.ID,
			PhoneNormalized: item.Contact.PhoneNormalized,
			PhoneDisplay:    item.Contact.PhoneDisplay,
			Name:            item.Contact.Name,
			BitrixContactID: item.Contact.BitrixContactID,
		}
	}
	return api.TelephonyCallDTO{
		ID:              item.Call.ID,
		ExternalCallID:  item.Call.ExternalCallID,
		Direction:       item.Call.Direction,
		Status:          item.Call.Status,
		MissedStatus:    item.Call.MissedStatus,
		ClientPhone:     item.Call.ClientPhone,
		VATNumber:       item.Call.VATNumber,
		EmployeeLogin:   item.Call.EmployeeLogin,
		EmployeeUserID:  item.Call.EmployeeUserID,
		EmployeeName:    item.EmployeeName,
		EmployeeState:   item.EmployeeState,
		GroupName:       item.Call.GroupName,
		StartedAt:       item.Call.StartedAt,
		AnsweredAt:      item.Call.AnsweredAt,
		CompletedAt:     item.Call.CompletedAt,
		WaitSeconds:     item.Call.WaitSeconds,
		DurationSeconds: item.Call.DurationSeconds,
		RecordingURL:    item.Call.RecordingURL,
		HasRecording:    item.Call.HasRecording,
		TicketID:        item.TicketID,
		Contact:         contact,
	}
}

func parseTelephonyUintParam(raw string) (uint, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(value), nil
}
