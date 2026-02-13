package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/user"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type UserHandler struct {
	userRepo   user.Repository
	bitrixRepo bitrix.Repository
}

func NewUserHandler(userRepo user.Repository, bitrixRepo bitrix.Repository) *UserHandler {
	return &UserHandler{userRepo: userRepo, bitrixRepo: bitrixRepo}
}

func (h *UserHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.GetUsers)
	r.Post("/", h.CreateUser)
	r.Put("/{id}", h.UpdateUser)
	r.Patch("/{id}/status", h.UpdateUserStatus)
	r.Delete("/{id}", h.DeleteUser)
}

func (h *UserHandler) ListAssignees(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	users, err := h.userRepo.GetAll(r.Context())
	if err != nil {
		log.Error("Не удалось получить список исполнителей", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить исполнителей")
		return
	}

	type assigneeDTO struct {
		ID       uint   `json:"id"`
		FullName string `json:"full_name"`
		Username string `json:"username"`
		IsActive bool   `json:"is_active"`
	}

	result := make([]assigneeDTO, 0, len(users))
	for _, u := range users {
		if !u.IsActive {
			continue
		}
		result = append(result, assigneeDTO{
			ID:       u.ID,
			FullName: strings.TrimSpace(u.FullName),
			Username: strings.TrimSpace(u.Username),
			IsActive: u.IsActive,
		})
	}

	response.RespondWithJSON(w, http.StatusOK, result)
}

func (h *UserHandler) UpdateMyCredentials(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	var dto api.ProfileCredentialsUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		log.Error("Не удалось получить пользователя", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователя")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	if dto.Username != nil {
		username := strings.TrimSpace(*dto.Username)
		if username == "" {
			response.RespondWithError(w, http.StatusBadRequest, "Логин не может быть пустым")
			return
		}
		u.Username = username
	}

	if dto.Password != nil {
		password := strings.TrimSpace(*dto.Password)
		if len(password) < 6 {
			response.RespondWithError(w, http.StatusBadRequest, "Пароль должен быть не менее 6 символов")
			return
		}
		if err := u.HashPassword(password); err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пароль")
			return
		}
	}

	if dto.Username == nil && dto.Password == nil {
		response.RespondWithError(w, http.StatusBadRequest, "Нет данных для обновления")
		return
	}

	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось обновить учетные данные пользователя", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить учетные данные")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *UserHandler) UpdateMyIntegrations(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	var dto api.ProfileIntegrationsUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		log.Error("Не удалось получить пользователя", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователя")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	lockedExisting := make([]user.Integration, 0, len(u.Integrations))
	lockedKeys := make(map[string]struct{}, len(u.Integrations))
	for _, existing := range u.Integrations {
		if !existing.IsLocked {
			continue
		}
		key := buildIntegrationKey(existing.IntegrationType, existing.ExternalID)
		lockedKeys[key] = struct{}{}
		lockedExisting = append(lockedExisting, existing)
	}

	normalized := make([]user.Integration, 0, len(dto.Integrations))
	seen := make(map[string]struct{}, len(dto.Integrations))
	for _, item := range dto.Integrations {
		typeVal := strings.TrimSpace(strings.ToLower(item.IntegrationType))
		idVal := strings.TrimSpace(item.ExternalID)
		typePtr, idPtr, validateErr := validateExternalFields(&typeVal, &idVal)
		if validateErr != nil {
			response.RespondWithError(w, http.StatusBadRequest, validateErr.Error())
			return
		}
		if typePtr == nil || idPtr == nil {
			continue
		}
		key := *typePtr + ":" + *idPtr
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, locked := lockedKeys[key]; locked {
			continue
		}

		integration := user.Integration{
			IntegrationType: *typePtr,
			ExternalID:      *idPtr,
		}
		if integration.IntegrationType == user.ExternalTypeBitrix24 {
			integration.IsVerified, integration.VerifiedName = h.verifyBitrixIntegration(r.Context(), u, integration.ExternalID)
		}
		normalized = append(normalized, integration)
	}

	for key := range lockedKeys {
		if _, exists := seen[key]; !exists {
			response.RespondWithError(w, http.StatusBadRequest, "автоматически привязанную интеграцию Bitrix24 нельзя изменять или удалять")
			return
		}
	}
	normalized = append(normalized, lockedExisting...)

	if err := h.userRepo.ReplaceIntegrations(r.Context(), currentUserID, normalized); err != nil {
		log.Error("Не удалось обновить интеграции профиля", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить интеграции")
		return
	}

	// Поддержка legacy-полей для совместимости.
	if len(normalized) > 0 {
		u.ExternalType = &normalized[0].IntegrationType
		u.ExternalID = &normalized[0].ExternalID
	} else {
		u.ExternalType = nil
		u.ExternalID = nil
	}
	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось сохранить legacy-поля интеграций", "id", currentUserID, "error", err)
	}

	updated, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil || updated == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toUserDTO(*updated))
}

func (h *UserHandler) GetMyProfileConfig(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "РџРѕР»СЊР·РѕРІР°С‚РµР»СЊ РЅРµ РѕРїСЂРµРґРµР»С‘РЅ")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ РїСЂРѕС„РёР»СЊ РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "РќРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ РїСЂРѕС„РёР»СЊ")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "РџРѕР»СЊР·РѕРІР°С‚РµР»СЊ РЅРµ РЅР°Р№РґРµРЅ")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"profile_config": parseProfileConfig(u.ProfileConfig),
	})
}

func (h *UserHandler) UpdateMyProfileConfig(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "РџРѕР»СЊР·РѕРІР°С‚РµР»СЊ РЅРµ РѕРїСЂРµРґРµР»С‘РЅ")
		return
	}

	var dto api.ProfileConfigUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "РќРµРєРѕСЂСЂРµРєС‚РЅРѕРµ С‚РµР»Рѕ Р·Р°РїСЂРѕСЃР°")
		return
	}
	if dto.ProfileConfig == nil {
		response.RespondWithError(w, http.StatusBadRequest, "РџРѕР»Рµ profile_config РѕР±СЏР·Р°С‚РµР»СЊРЅРѕ")
		return
	}

	payload, err := json.Marshal(dto.ProfileConfig)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "РќРµРєРѕСЂСЂРµРєС‚РЅС‹Р№ profile_config")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "РќРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "РџРѕР»СЊР·РѕРІР°С‚РµР»СЊ РЅРµ РЅР°Р№РґРµРЅ")
		return
	}

	u.ProfileConfig = payload
	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ РѕР±РЅРѕРІРёС‚СЊ profile_config", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "РќРµ СѓРґР°Р»РѕСЃСЊ РѕР±РЅРѕРІРёС‚СЊ profile_config")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, toUserDTO(*u))
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	users, err := h.userRepo.GetAll(r.Context())
	if err != nil {
		log.Error("Не удалось получить список пользователей", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователей")
		return
	}

	userDTOs := make([]api.UserDTO, len(users))
	for i, u := range users {
		userDTOs[i] = toUserDTO(u)
	}

	response.RespondWithJSON(w, http.StatusOK, userDTOs)
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.UserCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	position := strings.TrimSpace(dto.Position)
	if !user.IsValidRole(position) {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректная должность")
		return
	}

	schedule := strings.TrimSpace(dto.ScheduleType)
	if !user.IsValidSchedule(schedule) {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный тип графика")
		return
	}

	externalType, externalID, err := validateExternalFields(dto.ExternalType, dto.ExternalSystemID)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	role, err := h.userRepo.EnsureRoleExists(r.Context(), position, roleDescription(position))
	if err != nil {
		log.Error("Не удалось подготовить роль пользователя", "role", position, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка обработки должности")
		return
	}

	newUser := &user.User{
		Username:     strings.TrimSpace(dto.Username),
		FirstName:    strings.TrimSpace(dto.FirstName),
		LastName:     strings.TrimSpace(dto.LastName),
		FullName:     buildFullName(dto.FirstName, dto.LastName),
		Position:     position,
		Roles:        []user.Role{*role},
		ExternalID:   externalID,
		ExternalType: externalType,
		ScheduleType: schedule,
		IsActive:     true,
		HasLoggedIn:  false,
	}
	if externalType != nil && externalID != nil {
		newUser.Integrations = []user.Integration{
			{
				IntegrationType: *externalType,
				ExternalID:      *externalID,
			},
		}
	}

	if err := newUser.HashPassword(dto.Password); err != nil {
		log.Error("Не удалось захэшировать пароль", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось создать пользователя")
		return
	}

	if err := h.userRepo.Create(r.Context(), newUser); err != nil {
		log.Error("Не удалось создать пользователя", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось создать пользователя")
		return
	}

	response.RespondWithJSON(w, http.StatusCreated, toUserDTO(*newUser))
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}

	var dto api.UserUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), uint(id))
	if err != nil {
		log.Error("Не удалось получить пользователя", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователя")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	if u.HasLoggedIn && (dto.Username != nil || dto.Password != nil) {
		response.RespondWithError(w, http.StatusBadRequest, "Логин и пароль можно менять только до первого входа сотрудника")
		return
	}

	if dto.Username != nil {
		username := strings.TrimSpace(*dto.Username)
		if username == "" {
			response.RespondWithError(w, http.StatusBadRequest, "Логин не может быть пустым")
			return
		}
		u.Username = username
	}

	if dto.Password != nil {
		if len(strings.TrimSpace(*dto.Password)) < 6 {
			response.RespondWithError(w, http.StatusBadRequest, "Пароль должен быть не менее 6 символов")
			return
		}
		if err := u.HashPassword(*dto.Password); err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пароль")
			return
		}
	}

	if dto.FirstName != nil {
		u.FirstName = strings.TrimSpace(*dto.FirstName)
	}
	if dto.LastName != nil {
		u.LastName = strings.TrimSpace(*dto.LastName)
	}
	if dto.Position != nil {
		position := strings.TrimSpace(*dto.Position)
		if !user.IsValidRole(position) {
			response.RespondWithError(w, http.StatusBadRequest, "Некорректная должность")
			return
		}
		u.Position = position
		role, err := h.userRepo.EnsureRoleExists(r.Context(), position, roleDescription(position))
		if err != nil {
			log.Error("Не удалось подготовить роль пользователя", "role", position, "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Ошибка обработки должности")
			return
		}
		u.Roles = []user.Role{*role}
	}
	if dto.ScheduleType != nil {
		schedule := strings.TrimSpace(*dto.ScheduleType)
		if !user.IsValidSchedule(schedule) {
			response.RespondWithError(w, http.StatusBadRequest, "Некорректный тип графика")
			return
		}
		u.ScheduleType = schedule
	}

	nextExternalType := u.ExternalType
	nextExternalID := u.ExternalID
	if dto.ExternalType != nil {
		nextExternalType = dto.ExternalType
	}
	if dto.ExternalSystemID != nil {
		nextExternalID = dto.ExternalSystemID
	}

	normalizedType, normalizedID, err := validateExternalFields(nextExternalType, nextExternalID)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	u.ExternalType = normalizedType
	u.ExternalID = normalizedID
	if normalizedType != nil && normalizedID != nil {
		found := false
		for i := range u.Integrations {
			if strings.TrimSpace(strings.ToLower(u.Integrations[i].IntegrationType)) == strings.TrimSpace(strings.ToLower(*normalizedType)) {
				if u.Integrations[i].IsLocked {
					response.RespondWithError(w, http.StatusBadRequest, "автоматически привязанную интеграцию нельзя редактировать")
					return
				}
				u.Integrations[i].ExternalID = *normalizedID
				found = true
				break
			}
		}
		if !found {
			u.Integrations = append(u.Integrations, user.Integration{
				IntegrationType: *normalizedType,
				ExternalID:      *normalizedID,
			})
		}
	}

	u.FullName = buildFullName(u.FirstName, u.LastName)

	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось обновить пользователя", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пользователя")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, toUserDTO(*u))
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}
	h.setUserActiveStatus(w, r, uint(id), false)
}

func (h *UserHandler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}

	var dto api.UserStatusUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	h.setUserActiveStatus(w, r, uint(id), dto.IsActive)
}

func (h *UserHandler) setUserActiveStatus(w http.ResponseWriter, r *http.Request, targetUserID uint, isActive bool) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == targetUserID && !isActive {
		response.RespondWithError(w, http.StatusBadRequest, "Нельзя заблокировать текущего пользователя")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), targetUserID)
	if err != nil {
		log.Error("Не удалось получить пользователя", "id", targetUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователя")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	u.IsActive = isActive
	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось обновить статус пользователя", "id", targetUserID, "is_active", isActive, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить статус пользователя")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, toUserDTO(*u))
}

func toUserDTO(u user.User) api.UserDTO {
	roles := make([]string, 0, len(u.Roles))
	for _, role := range u.Roles {
		roles = append(roles, role.Name)
	}
	integrations := make([]api.UserIntegrationDTO, 0, len(u.Integrations))
	for _, item := range u.Integrations {
		integrations = append(integrations, api.UserIntegrationDTO{
			ID:              item.ID,
			IntegrationType: item.IntegrationType,
			ExternalID:      item.ExternalID,
			IsVerified:      item.IsVerified,
			IsLocked:        item.IsLocked,
			VerifiedName:    item.VerifiedName,
		})
	}

	return api.UserDTO{
		ID:               u.ID,
		Username:         u.Username,
		FullName:         u.FullName,
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		Position:         u.Position,
		Roles:            roles,
		ExternalSystemID: u.ExternalID,
		ExternalType:     u.ExternalType,
		ScheduleType:     u.ScheduleType,
		IsActive:         u.IsActive,
		HasLoggedIn:      u.HasLoggedIn,
		Integrations:     integrations,
		ProfileConfig:    parseProfileConfig(u.ProfileConfig),
	}
}

func parseProfileConfig(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return map[string]interface{}{}
	}
	return result
}

func (h *UserHandler) verifyBitrixIntegration(ctx context.Context, u *user.User, externalID string) (bool, string) {
	if h.bitrixRepo == nil || u == nil {
		return false, ""
	}
	targetID, err := strconv.ParseInt(strings.TrimSpace(externalID), 10, 64)
	if err != nil || targetID <= 0 {
		return false, ""
	}
	cache, err := h.bitrixRepo.ListUserCache(ctx)
	if err != nil {
		return false, ""
	}
	var matched *bitrix.UserCache
	for i := range cache {
		if cache[i].B24UserID == targetID {
			matched = &cache[i]
			break
		}
	}
	if matched == nil {
		return false, ""
	}

	userFirst := strings.ToLower(strings.TrimSpace(u.FirstName))
	userLast := strings.ToLower(strings.TrimSpace(u.LastName))
	cacheFirst := strings.ToLower(strings.TrimSpace(matched.FirstName))
	cacheLast := strings.ToLower(strings.TrimSpace(matched.LastName))
	if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
		return true, strings.TrimSpace(strings.Join([]string{matched.LastName, matched.FirstName}, " "))
	}

	userFull := strings.ToLower(strings.TrimSpace(u.FullName))
	cacheFull := strings.ToLower(strings.TrimSpace(strings.Join([]string{matched.LastName, matched.FirstName, matched.SecondName}, " ")))
	if userFull != "" && cacheFull != "" && userFull == cacheFull {
		return true, strings.TrimSpace(strings.Join([]string{matched.LastName, matched.FirstName, matched.SecondName}, " "))
	}
	return false, ""
}

func buildFullName(firstName, lastName string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s", strings.TrimSpace(firstName), strings.TrimSpace(lastName)))
}

func buildIntegrationKey(integrationType, externalID string) string {
	return strings.TrimSpace(strings.ToLower(integrationType)) + ":" + strings.TrimSpace(externalID)
}

func roleDescription(role string) string {
	switch role {
	case user.RoleAdmin:
		return "Администратор системы"
	case user.RoleSupportSpecialist:
		return "Специалист техподдержки"
	case user.RoleIntern:
		return "Стажёр"
	default:
		return ""
	}
}

func getCurrentUserID(r *http.Request) uint {
	val := r.Context().Value(contextkeys.UserIDContextKey)
	userIDStr, ok := val.(string)
	if !ok || userIDStr == "" {
		return 0
	}

	userID, err := strconv.ParseUint(userIDStr, 10, 32)
	if err != nil {
		return 0
	}

	return uint(userID)
}

func validateExternalFields(rawType, rawID *string) (*string, *string, error) {
	typeStr := ""
	if rawType != nil {
		typeStr = strings.ToLower(strings.TrimSpace(*rawType))
	}

	idStr := ""
	if rawID != nil {
		idStr = strings.TrimSpace(*rawID)
	}

	if typeStr == "" && idStr == "" {
		return nil, nil, nil
	}

	if typeStr == "" || idStr == "" {
		return nil, nil, fmt.Errorf("для внешней системы нужно заполнить и тип, и ID")
	}

	if !user.IsValidExternalType(typeStr) {
		return nil, nil, fmt.Errorf("некорректный тип внешней системы")
	}

	if !user.IsValidExternalID(typeStr, idStr) {
		switch typeStr {
		case user.ExternalTypeTelegram:
			return nil, nil, fmt.Errorf("для Telegram ID должен начинаться с @")
		case user.ExternalTypeNaumen:
			return nil, nil, fmt.Errorf("для Naumen ID должен начинаться с $")
		case user.ExternalTypeBitrix24:
			return nil, nil, fmt.Errorf("для Bitrix24 ID должен быть числом")
		default:
			return nil, nil, fmt.Errorf("некорректный ID внешней системы")
		}
	}

	return &typeStr, &idStr, nil
}
