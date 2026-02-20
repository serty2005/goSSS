package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
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
	cfg        *config.Config
}

func NewUserHandler(userRepo user.Repository, bitrixRepo bitrix.Repository, cfg *config.Config) *UserHandler {
	return &UserHandler{userRepo: userRepo, bitrixRepo: bitrixRepo, cfg: cfg}
}

func (h *UserHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.GetUsers)
	r.Post("/", h.CreateUser)
	r.Put("/{id}", h.UpdateUser)
	r.Post("/{id}/bitrix/sync-suggestion", h.ApplyUserBitrixSuggestion)
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

	lockedByKey := make(map[string]user.Integration, len(u.Integrations))
	for _, existing := range u.Integrations {
		if !existing.IsLocked {
			continue
		}
		key := buildIntegrationKey(existing.IntegrationType, existing.ExternalID)
		lockedByKey[key] = existing
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

		if lockedExisting, isLocked := lockedByKey[key]; isLocked {
			normalized = append(normalized, lockedExisting)
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

	if err := h.userRepo.ReplaceIntegrations(r.Context(), currentUserID, normalized); err != nil {
		log.Error("Не удалось обновить интеграции профиля", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить интеграции")
		return
	}

	var legacyType *string
	var legacyID *string
	if len(normalized) > 0 {
		first := normalized[0]
		legacyType = &first.IntegrationType
		legacyID = &first.ExternalID
	}
	if err := h.userRepo.UpdateExternalFields(r.Context(), currentUserID, legacyType, legacyID); err != nil {
		log.Error("Не удалось обновить legacy-поля интеграций", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить интеграции")
		return
	}

	updated, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil || updated == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *updated))
}

func (h *UserHandler) GetMyProfileConfig(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		log.Error("Не удалось получить профиль пользователя", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить профиль")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
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
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	var dto api.ProfileConfigUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}
	if dto.ProfileConfig == nil {
		response.RespondWithError(w, http.StatusBadRequest, "Поле profile_config обязательно")
		return
	}

	payload, err := json.Marshal(dto.ProfileConfig)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный profile_config")
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

	u.ProfileConfig = payload
	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось обновить profile_config", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить profile_config")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *u))
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	users, err := h.userRepo.GetAll(r.Context())
	if err != nil {
		log.Error("Не удалось получить список пользователей", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить пользователей")
		return
	}

	cacheItems, _ := h.getBitrixCache(r.Context())
	userDTOs := make([]api.UserDTO, len(users))
	for i, u := range users {
		userDTOs[i] = h.toUserDTOWithCache(u, cacheItems)
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
		integration := user.Integration{
			IntegrationType: *externalType,
			ExternalID:      *externalID,
		}
		if integration.IntegrationType == user.ExternalTypeBitrix24 {
			integration.IsVerified, integration.VerifiedName = h.verifyBitrixIntegration(r.Context(), newUser, integration.ExternalID)
			if integration.IsVerified {
				integration.IsLocked = true
			}
		}
		newUser.Integrations = []user.Integration{
			integration,
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

	if len(newUser.Integrations) > 0 &&
		newUser.Integrations[0].IntegrationType == user.ExternalTypeBitrix24 &&
		newUser.Integrations[0].IsVerified &&
		h.bitrixRepo != nil {
		b24UserID, parseErr := strconv.ParseInt(strings.TrimSpace(newUser.Integrations[0].ExternalID), 10, 64)
		if parseErr == nil && b24UserID > 0 {
			_ = h.bitrixRepo.UpsertUserMap(r.Context(), &bitrix.UserMap{
				EtalonUserID: newUser.ID,
				B24UserID:    b24UserID,
			})
		}
	}

	response.RespondWithJSON(w, http.StatusCreated, h.toUserDTO(r.Context(), *newUser))
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
				if u.Integrations[i].IsLocked && strings.TrimSpace(u.Integrations[i].ExternalID) != strings.TrimSpace(*normalizedID) {
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

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *u))
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

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *u))
}

func (h *UserHandler) toUserDTO(ctx context.Context, u user.User) api.UserDTO {
	cacheItems, _ := h.getBitrixCache(ctx)
	return h.toUserDTOWithCache(u, cacheItems)
}

func (h *UserHandler) toUserDTOWithCache(u user.User, cacheItems []bitrix.UserCache) api.UserDTO {
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
		BitrixEnabled:    h.cfg != nil && h.cfg.EnableBitrixGateway,
		ExternalSystemID: u.ExternalID,
		ExternalType:     u.ExternalType,
		ScheduleType:     u.ScheduleType,
		IsActive:         u.IsActive,
		HasLoggedIn:      u.HasLoggedIn,
		Integrations:     integrations,
		BitrixSuggestion: findBitrixSuggestionFromCache(&u, cacheItems),
		ProfileConfig:    parseProfileConfig(u.ProfileConfig),
	}
}

func (h *UserHandler) getBitrixCache(ctx context.Context) ([]bitrix.UserCache, error) {
	if h.bitrixRepo == nil {
		return nil, nil
	}
	return h.bitrixRepo.ListUserCache(ctx)
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

	userFirst := normalizePersonToken(u.FirstName)
	userLast := normalizePersonToken(u.LastName)
	cacheFirst := normalizePersonToken(matched.FirstName)
	cacheLast := normalizePersonToken(matched.LastName)
	if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
		return true, strings.TrimSpace(strings.Join([]string{matched.LastName, matched.FirstName}, " "))
	}

	userFull := normalizePersonToken(u.FullName)
	cacheFull := normalizePersonToken(strings.Join([]string{matched.LastName, matched.FirstName, matched.SecondName}, " "))
	if userFull != "" && cacheFull != "" && userFull == cacheFull {
		return true, strings.TrimSpace(strings.Join([]string{matched.LastName, matched.FirstName, matched.SecondName}, " "))
	}
	return false, ""
}

func findBitrixSuggestionFromCache(u *user.User, cacheItems []bitrix.UserCache) *api.BitrixUserSuggestionDTO {
	if u == nil || len(cacheItems) == 0 {
		return nil
	}

	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			continue
		}
		if integration.IsLocked && integration.IsVerified {
			return nil
		}
	}

	userFirst := normalizePersonToken(u.FirstName)
	userLast := normalizePersonToken(u.LastName)
	userFull := normalizePersonToken(u.FullName)
	if userFirst == "" || userLast == "" {
		if userFull == "" {
			return nil
		}
	}

	matches := make([]bitrix.UserCache, 0, 1)
	for i := range cacheItems {
		cache := cacheItems[i]
		if !cache.Active {
			continue
		}
		cacheFirst := normalizePersonToken(cache.FirstName)
		cacheLast := normalizePersonToken(cache.LastName)
		cacheFull := normalizePersonToken(strings.Join([]string{cache.LastName, cache.FirstName, cache.SecondName}, " "))

		if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
			matches = append(matches, cache)
			continue
		}
		if userFull != "" && userFull == cacheFull {
			matches = append(matches, cache)
		}
	}
	if len(matches) == 0 {
		return nil
	}

	found := matches[0]
	foundID := strconv.FormatInt(found.B24UserID, 10)
	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			continue
		}
		if strings.TrimSpace(integration.ExternalID) == foundID {
			return nil
		}
	}

	name := strings.TrimSpace(strings.Join([]string{found.LastName, found.FirstName, found.SecondName}, " "))
	return &api.BitrixUserSuggestionDTO{
		B24UserID: found.B24UserID,
		Name:      name,
	}
}

func (h *UserHandler) GetMyProfile(w http.ResponseWriter, r *http.Request) {
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), currentUserID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить профиль")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *u))
}

func (h *UserHandler) ApplyMyBitrixSuggestion(w http.ResponseWriter, r *http.Request) {
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	updated, err := h.applyBitrixSuggestion(r.Context(), currentUserID)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *updated))
}

func (h *UserHandler) ApplyUserBitrixSuggestion(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}

	updated, applyErr := h.applyBitrixSuggestion(r.Context(), uint(id))
	if applyErr != nil {
		response.RespondWithError(w, http.StatusBadRequest, applyErr.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *updated))
}

func (h *UserHandler) applyBitrixSuggestion(ctx context.Context, userID uint) (*user.User, error) {
	u, err := h.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить пользователя")
	}
	if u == nil {
		return nil, fmt.Errorf("пользователь не найден")
	}

	cacheItems, err := h.getBitrixCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить кэш пользователей Bitrix24")
	}
	suggestion := findBitrixSuggestionFromCache(u, cacheItems)
	if suggestion == nil {
		return nil, fmt.Errorf("не найдено однозначное предложение синхронизации")
	}

	targetID := strconv.FormatInt(suggestion.B24UserID, 10)
	updatedIntegrations := make([]user.Integration, 0, len(u.Integrations)+1)
	foundExact := false
	for i := range u.Integrations {
		integration := u.Integrations[i]
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			updatedIntegrations = append(updatedIntegrations, integration)
			continue
		}

		if integration.IsLocked && strings.TrimSpace(integration.ExternalID) != targetID {
			return nil, fmt.Errorf("у пользователя уже есть другая автоматическая интеграция Bitrix24")
		}

		if strings.TrimSpace(integration.ExternalID) == targetID {
			integration.IsVerified = true
			integration.IsLocked = true
			integration.VerifiedName = suggestion.Name
			foundExact = true
		}
		updatedIntegrations = append(updatedIntegrations, integration)
	}

	if !foundExact {
		updatedIntegrations = append(updatedIntegrations, user.Integration{
			UserID:          u.ID,
			IntegrationType: user.ExternalTypeBitrix24,
			ExternalID:      targetID,
			IsVerified:      true,
			IsLocked:        true,
			VerifiedName:    suggestion.Name,
		})
	}

	if err := h.userRepo.ReplaceIntegrations(ctx, u.ID, updatedIntegrations); err != nil {
		return nil, fmt.Errorf("не удалось сохранить интеграции пользователя")
	}

	if h.bitrixRepo != nil {
		if err := h.bitrixRepo.UpsertUserMap(ctx, &bitrix.UserMap{
			EtalonUserID: u.ID,
			B24UserID:    suggestion.B24UserID,
		}); err != nil {
			return nil, fmt.Errorf("не удалось сохранить связку с Bitrix24")
		}
	}

	extType := user.ExternalTypeBitrix24
	u.ExternalType = &extType
	u.ExternalID = &targetID
	if err := h.userRepo.Update(ctx, u); err != nil {
		return nil, fmt.Errorf("не удалось обновить legacy-поля пользователя")
	}

	return h.userRepo.GetByID(ctx, u.ID)
}

func buildFullName(firstName, lastName string) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s", strings.TrimSpace(firstName), strings.TrimSpace(lastName)))
}

func normalizePersonToken(v string) string {
	out := strings.ToLower(strings.TrimSpace(v))
	out = strings.ReplaceAll(out, "ё", "е")
	out = strings.Join(strings.Fields(out), " ")
	return out
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
