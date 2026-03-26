package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/internal/services"
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
	userRepo     user.Repository
	bitrixRepo   bitrix.Repository
	pyrusRepo    pyrus.Repository
	pyrusService services.PyrusSyncService
	cfg          *config.Config
}

func NewUserHandler(
	userRepo user.Repository,
	bitrixRepo bitrix.Repository,
	pyrusRepo pyrus.Repository,
	pyrusService services.PyrusSyncService,
	cfg *config.Config,
) *UserHandler {
	return &UserHandler{
		userRepo:     userRepo,
		bitrixRepo:   bitrixRepo,
		pyrusRepo:    pyrusRepo,
		pyrusService: pyrusService,
		cfg:          cfg,
	}
}

func (h *UserHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.GetUsers)
	r.Get("/restore-candidate", h.GetRestoreCandidate)
	r.Post("/", h.CreateUser)
	r.Post("/restore", h.RestoreDeletedUser)
	r.Put("/{id}", h.UpdateUser)
	r.Post("/{id}/bitrix/sync-suggestion", h.ApplyUserBitrixSuggestion)
	r.Post("/{id}/pyrus/sync-suggestion", h.ApplyUserPyrusSuggestion)
	r.Patch("/{id}/status", h.UpdateUserStatus)
	r.Delete("/{id}", h.DeleteUser)
}

func (h *UserHandler) GetRestoreCandidate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"candidate": nil})
		return
	}

	found, err := h.userRepo.GetDeletedByUsername(r.Context(), username)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось проверить удалённого пользователя")
		return
	}
	if found == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"candidate": nil})
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]any{
		"candidate": h.toDeletedUserRestoreCandidateDTO(*found),
	})
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
		existingUser, lookupErr := h.userRepo.GetByUsername(r.Context(), username)
		if lookupErr != nil {
			log.Error("Не удалось проверить логин пользователя", "username", username, "error", lookupErr)
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пользователя")
			return
		}
		if existingUser != nil && existingUser.ID != u.ID {
			response.RespondWithError(w, http.StatusBadRequest, "Пользователь с таким логином уже существует")
			return
		}
		deletedUser, lookupDeletedErr := h.userRepo.GetDeletedByUsername(r.Context(), username)
		if lookupDeletedErr != nil {
			log.Error("Не удалось проверить удалённого пользователя по логину", "username", username, "error", lookupDeletedErr)
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пользователя")
			return
		}
		if deletedUser != nil && deletedUser.ID != u.ID {
			response.RespondWithError(w, http.StatusBadRequest, "Этот логин уже принадлежит удалённому пользователю. Используйте восстановление.")
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

	normalized, err := h.normalizeRequestedIntegrations(r.Context(), u, dto.Integrations, true)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.userRepo.ReplaceIntegrations(r.Context(), currentUserID, normalized); err != nil {
		log.Error("Не удалось обновить интеграции профиля", "id", currentUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить интеграции")
		return
	}
	u.Integrations = normalized
	h.persistVerifiedExternalMaps(r.Context(), u)

	legacyType, legacyID := pickPrimaryIntegration(normalized)
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
	pyrusMembers, _ := h.getPyrusMembers(r.Context())
	userDTOs := make([]api.UserDTO, len(users))
	for i, u := range users {
		userDTOs[i] = h.toUserDTOWithLookups(u, cacheItems, pyrusMembers)
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
	if len(strings.TrimSpace(dto.Password)) < 6 {
		response.RespondWithError(w, http.StatusBadRequest, "Пароль должен быть не менее 6 символов")
		return
	}

	username := strings.TrimSpace(dto.Username)
	if username == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Логин не может быть пустым")
		return
	}

	existingUser, err := h.userRepo.GetByUsername(r.Context(), username)
	if err != nil {
		log.Error("Не удалось проверить логин пользователя", "username", username, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось создать пользователя")
		return
	}
	if existingUser != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Пользователь с таким логином уже существует")
		return
	}

	deletedCandidate, err := h.userRepo.GetDeletedByUsername(r.Context(), username)
	if err != nil {
		log.Error("Не удалось проверить удалённого пользователя", "username", username, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось создать пользователя")
		return
	}
	if deletedCandidate != nil {
		response.RespondWithError(w, http.StatusConflict, "Найден удалённый пользователь с таким логином. Используйте восстановление.")
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
		Username:     username,
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
	if dto.Email != nil {
		newUser.Email = normalizeOptionalString(dto.Email)
	}

	requestedIntegrations := mergeLegacyIntegrationItems(dto.Integrations, externalType, externalID)
	if len(requestedIntegrations) > 0 {
		normalizedIntegrations, normalizeErr := h.normalizeRequestedIntegrations(r.Context(), newUser, requestedIntegrations, false)
		if normalizeErr != nil {
			response.RespondWithError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		newUser.Integrations = normalizedIntegrations
		newUser.ExternalType, newUser.ExternalID = pickPrimaryIntegration(normalizedIntegrations)
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

	h.persistVerifiedExternalMaps(r.Context(), newUser)

	response.RespondWithJSON(w, http.StatusCreated, h.toUserDTO(r.Context(), *newUser))
}

func (h *UserHandler) RestoreDeletedUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.UserCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	username := strings.TrimSpace(dto.Username)
	if username == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Логин не может быть пустым")
		return
	}

	activeUser, err := h.userRepo.GetByUsername(r.Context(), username)
	if err != nil {
		log.Error("Не удалось проверить активного пользователя", "username", username, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось восстановить пользователя")
		return
	}
	if activeUser != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Пользователь с таким логином уже существует")
		return
	}

	deletedUser, err := h.userRepo.GetDeletedByUsername(r.Context(), username)
	if err != nil {
		log.Error("Не удалось получить удалённого пользователя", "username", username, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось восстановить пользователя")
		return
	}
	if deletedUser == nil {
		response.RespondWithError(w, http.StatusNotFound, "Удалённый пользователь с таким логином не найден")
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
	if len(strings.TrimSpace(dto.Password)) < 6 {
		response.RespondWithError(w, http.StatusBadRequest, "Пароль должен быть не менее 6 символов")
		return
	}

	externalType, externalID, err := validateExternalFields(dto.ExternalType, dto.ExternalSystemID)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	role, err := h.userRepo.EnsureRoleExists(r.Context(), position, roleDescription(position))
	if err != nil {
		log.Error("Не удалось подготовить роль для восстановления", "role", position, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось восстановить пользователя")
		return
	}

	deletedUser.Username = username
	deletedUser.FirstName = strings.TrimSpace(dto.FirstName)
	deletedUser.LastName = strings.TrimSpace(dto.LastName)
	deletedUser.FullName = buildFullName(dto.FirstName, dto.LastName)
	deletedUser.Position = position
	deletedUser.ScheduleType = schedule
	deletedUser.Roles = []user.Role{*role}
	deletedUser.IsActive = true
	deletedUser.HasLoggedIn = false
	if dto.Email != nil {
		deletedUser.Email = normalizeOptionalString(dto.Email)
	}
	deletedUser.DeletedAt.Valid = false
	deletedUser.DeletedAt.Time = deletedUser.UpdatedAt

	requestedIntegrations := mergeLegacyIntegrationItems(dto.Integrations, externalType, externalID)
	if len(requestedIntegrations) > 0 {
		normalizedIntegrations, normalizeErr := h.normalizeRequestedIntegrations(r.Context(), deletedUser, requestedIntegrations, false)
		if normalizeErr != nil {
			response.RespondWithError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		deletedUser.Integrations = normalizedIntegrations
	}
	deletedUser.ExternalType, deletedUser.ExternalID = pickPrimaryIntegration(deletedUser.Integrations)

	if err := deletedUser.HashPassword(dto.Password); err != nil {
		log.Error("Не удалось обновить пароль при восстановлении пользователя", "id", deletedUser.ID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось восстановить пользователя")
		return
	}

	if err := h.userRepo.Restore(r.Context(), deletedUser); err != nil {
		log.Error("Не удалось восстановить пользователя", "id", deletedUser.ID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось восстановить пользователя")
		return
	}
	h.persistVerifiedExternalMaps(r.Context(), deletedUser)

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *deletedUser))
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
	if dto.Email != nil {
		u.Email = normalizeOptionalString(dto.Email)
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
	u.FullName = buildFullName(u.FirstName, u.LastName)

	if dto.Integrations != nil {
		normalizedIntegrations, normalizeErr := h.normalizeRequestedIntegrations(r.Context(), u, dto.Integrations, false)
		if normalizeErr != nil {
			response.RespondWithError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		u.Integrations = normalizedIntegrations
		u.ExternalType, u.ExternalID = pickPrimaryIntegration(normalizedIntegrations)
	} else {
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
				if strings.TrimSpace(strings.ToLower(u.Integrations[i].IntegrationType)) != strings.TrimSpace(strings.ToLower(*normalizedType)) {
					continue
				}
				if u.Integrations[i].IsLocked && strings.TrimSpace(u.Integrations[i].ExternalID) != strings.TrimSpace(*normalizedID) {
					response.RespondWithError(w, http.StatusBadRequest, "Автоматически привязанную интеграцию нельзя редактировать")
					return
				}
				u.Integrations[i].ExternalID = *normalizedID
				u.Integrations[i].IsEnabled = true
				h.enrichIntegration(r.Context(), u, &u.Integrations[i])
				found = true
				break
			}
			if !found {
				integration := user.Integration{
					IntegrationType: *normalizedType,
					ExternalID:      *normalizedID,
					IsEnabled:       true,
				}
				h.enrichIntegration(r.Context(), u, &integration)
				u.Integrations = append(u.Integrations, integration)
			}
		}
	}

	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось обновить пользователя", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось обновить пользователя")
		return
	}
	h.persistVerifiedExternalMaps(r.Context(), u)

	response.RespondWithJSON(w, http.StatusOK, h.toUserDTO(r.Context(), *u))
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}
	targetUserID := uint(id)
	currentUserID := getCurrentUserID(r)
	if currentUserID == targetUserID {
		response.RespondWithError(w, http.StatusBadRequest, "Нельзя удалить текущего пользователя")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), targetUserID)
	if err != nil {
		log.Error("Не удалось получить пользователя для удаления", "id", targetUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось удалить пользователя")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}

	u.IsActive = false
	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Не удалось деактивировать пользователя перед удалением", "id", targetUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось удалить пользователя")
		return
	}
	if err := h.userRepo.Delete(r.Context(), targetUserID); err != nil {
		log.Error("Не удалось выполнить мягкое удаление пользователя", "id", targetUserID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось удалить пользователя")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]any{
		"status": "deleted",
		"id":     targetUserID,
	})
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
	pyrusMembers, _ := h.getPyrusMembers(ctx)
	return h.toUserDTOWithLookups(u, cacheItems, pyrusMembers)
}

func (h *UserHandler) toUserDTOWithLookups(u user.User, cacheItems []bitrix.UserCache, pyrusMembers []pyrusplugin.Member) api.UserDTO {
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
			IsEnabled:       item.IsEnabled,
			IsVerified:      item.IsVerified,
			IsLocked:        item.IsLocked,
			VerifiedName:    item.VerifiedName,
		})
	}
	var pyrusSuggestion *api.PyrusUserSuggestionDTO
	if suggestion := findPyrusSuggestionFromMembers(&u, pyrusMembers); suggestion != nil {
		pyrusSuggestion = suggestion
	}

	return api.UserDTO{
		ID:               u.ID,
		Username:         u.Username,
		FullName:         u.FullName,
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		Position:         u.Position,
		Email:            u.Email,
		Roles:            roles,
		BitrixEnabled:    h.cfg != nil && h.cfg.EnableBitrixGateway,
		PyrusEnabled:     h.cfg != nil && h.cfg.EnablePyrusGateway,
		ExternalSystemID: u.ExternalID,
		ExternalType:     u.ExternalType,
		ScheduleType:     u.ScheduleType,
		IsActive:         u.IsActive,
		HasLoggedIn:      u.HasLoggedIn,
		Integrations:     integrations,
		BitrixSuggestion: findBitrixSuggestionFromCache(&u, cacheItems),
		PyrusSuggestion:  pyrusSuggestion,
		ProfileConfig:    parseProfileConfig(u.ProfileConfig),
	}
}

func (h *UserHandler) toDeletedUserRestoreCandidateDTO(u user.User) api.DeletedUserRestoreCandidateDTO {
	candidate := api.DeletedUserRestoreCandidateDTO{
		ID:           u.ID,
		Username:     u.Username,
		FullName:     u.FullName,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		Position:     u.Position,
		Email:        u.Email,
		ScheduleType: u.ScheduleType,
		Integrations: make([]api.UserIntegrationDTO, 0, len(u.Integrations)),
	}
	if u.DeletedAt.Valid {
		candidate.DeletedAt = &u.DeletedAt.Time
	}
	for _, item := range u.Integrations {
		candidate.Integrations = append(candidate.Integrations, api.UserIntegrationDTO{
			ID:              item.ID,
			IntegrationType: item.IntegrationType,
			ExternalID:      item.ExternalID,
			IsEnabled:       item.IsEnabled,
			IsVerified:      item.IsVerified,
			IsLocked:        item.IsLocked,
			VerifiedName:    item.VerifiedName,
		})
	}
	return candidate
}

func (h *UserHandler) getBitrixCache(ctx context.Context) ([]bitrix.UserCache, error) {
	if h.bitrixRepo == nil {
		return nil, nil
	}
	return h.bitrixRepo.ListUserCache(ctx)
}

func (h *UserHandler) getPyrusMembers(ctx context.Context) ([]pyrusplugin.Member, error) {
	if h.pyrusService == nil || !h.pyrusService.IsEnabled() {
		return nil, nil
	}
	return h.pyrusService.ListMembers(ctx)
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

func (h *UserHandler) verifyPyrusIntegration(ctx context.Context, u *user.User, externalID string) (bool, string, string) {
	if h.pyrusService == nil || !h.pyrusService.IsEnabled() || u == nil {
		return false, "", ""
	}
	members, err := h.getPyrusMembers(ctx)
	if err != nil {
		return false, "", ""
	}
	return services.VerifyPyrusUserMatch(u, externalID, members)
}

func (h *UserHandler) enrichIntegration(ctx context.Context, u *user.User, integration *user.Integration) {
	if integration == nil {
		return
	}
	if !integration.IsEnabled {
		integration.IsVerified = false
		integration.IsLocked = false
		integration.VerifiedName = ""
		return
	}
	integration.IsVerified = false
	integration.VerifiedName = ""
	switch strings.TrimSpace(strings.ToLower(integration.IntegrationType)) {
	case user.ExternalTypeBitrix24:
		integration.IsVerified, integration.VerifiedName = h.verifyBitrixIntegration(ctx, u, integration.ExternalID)
		if integration.IsVerified {
			integration.IsLocked = true
		}
	case user.ExternalTypePyrus:
		var verifiedEmail string
		integration.IsVerified, integration.VerifiedName, verifiedEmail = h.verifyPyrusIntegration(ctx, u, integration.ExternalID)
		if integration.IsVerified {
			integration.IsLocked = true
		}
		if verifiedEmail != "" && (u.Email == nil || strings.TrimSpace(*u.Email) == "") {
			u.Email = normalizeOptionalString(&verifiedEmail)
		}
	default:
		integration.IsLocked = false
	}
}

func (h *UserHandler) persistVerifiedExternalMaps(ctx context.Context, u *user.User) {
	if u == nil {
		return
	}
	for i := range u.Integrations {
		integration := u.Integrations[i]
		if !integration.IsEnabled {
			continue
		}
		if !integration.IsVerified {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(integration.IntegrationType)) {
		case user.ExternalTypeBitrix24:
			if h.bitrixRepo == nil {
				continue
			}
			b24UserID, err := strconv.ParseInt(strings.TrimSpace(integration.ExternalID), 10, 64)
			if err != nil || b24UserID <= 0 {
				continue
			}
			_ = h.bitrixRepo.UpsertUserMap(ctx, &bitrix.UserMap{
				EtalonUserID: u.ID,
				B24UserID:    b24UserID,
			})
		case user.ExternalTypePyrus:
			if h.pyrusRepo == nil {
				continue
			}
			pyrusUserID, err := strconv.ParseInt(strings.TrimSpace(integration.ExternalID), 10, 64)
			if err != nil || pyrusUserID <= 0 {
				continue
			}
			_ = h.pyrusRepo.UpsertUserMap(ctx, &pyrus.UserMap{
				EtalonUserID: u.ID,
				PyrusUserID:  pyrusUserID,
			})
		}
	}
}

func findBitrixSuggestionFromCache(u *user.User, cacheItems []bitrix.UserCache) *api.BitrixUserSuggestionDTO {
	if u == nil || len(cacheItems) == 0 {
		return nil
	}

	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			continue
		}
		if integration.IsEnabled && integration.IsLocked && integration.IsVerified {
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
		if !integration.IsEnabled {
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

func findPyrusSuggestionFromMembers(u *user.User, members []pyrusplugin.Member) *api.PyrusUserSuggestionDTO {
	if u == nil || len(members) == 0 {
		return nil
	}
	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypePyrus {
			continue
		}
		if integration.IsEnabled && integration.IsLocked && integration.IsVerified {
			return nil
		}
	}
	suggestion := services.FindPyrusUserSuggestionForUser(u, members)
	if suggestion == nil {
		return nil
	}
	targetID := strconv.FormatInt(suggestion.PyrusUserID, 10)
	if u.ExternalType != nil && u.ExternalID != nil &&
		strings.TrimSpace(strings.ToLower(*u.ExternalType)) == user.ExternalTypePyrus &&
		strings.TrimSpace(*u.ExternalID) == targetID {
		return nil
	}
	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypePyrus {
			continue
		}
		if !integration.IsEnabled {
			continue
		}
		if strings.TrimSpace(integration.ExternalID) == targetID {
			return nil
		}
	}
	return &api.PyrusUserSuggestionDTO{
		PyrusUserID: suggestion.PyrusUserID,
		Name:        suggestion.Name,
		Email:       suggestion.Email,
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

func (h *UserHandler) ApplyMyPyrusSuggestion(w http.ResponseWriter, r *http.Request) {
	currentUserID := getCurrentUserID(r)
	if currentUserID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "Пользователь не определён")
		return
	}

	updated, err := h.applyPyrusSuggestion(r.Context(), currentUserID)
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

func (h *UserHandler) ApplyUserPyrusSuggestion(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID пользователя")
		return
	}

	updated, applyErr := h.applyPyrusSuggestion(r.Context(), uint(id))
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

		if integration.IsEnabled && integration.IsLocked && strings.TrimSpace(integration.ExternalID) != targetID {
			return nil, fmt.Errorf("у пользователя уже есть другая автоматическая интеграция Bitrix24")
		}

		if strings.TrimSpace(integration.ExternalID) == targetID {
			integration.IsEnabled = true
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
			IsEnabled:       true,
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

	u.Integrations = updatedIntegrations
	u.ExternalType, u.ExternalID = pickPrimaryIntegration(updatedIntegrations)
	if err := h.userRepo.Update(ctx, u); err != nil {
		return nil, fmt.Errorf("не удалось обновить legacy-поля пользователя")
	}

	return h.userRepo.GetByID(ctx, u.ID)
}

func (h *UserHandler) applyPyrusSuggestion(ctx context.Context, userID uint) (*user.User, error) {
	u, err := h.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить пользователя")
	}
	if u == nil {
		return nil, fmt.Errorf("пользователь не найден")
	}

	members, err := h.getPyrusMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список сотрудников Pyrus")
	}
	suggestion := services.FindPyrusUserSuggestionForUser(u, members)
	if suggestion == nil {
		return nil, fmt.Errorf("не найдено однозначное предложение синхронизации Pyrus")
	}

	targetID := strconv.FormatInt(suggestion.PyrusUserID, 10)
	updatedIntegrations := make([]user.Integration, 0, len(u.Integrations)+1)
	foundExact := false
	for i := range u.Integrations {
		integration := u.Integrations[i]
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypePyrus {
			updatedIntegrations = append(updatedIntegrations, integration)
			continue
		}

		if integration.IsEnabled && integration.IsLocked && strings.TrimSpace(integration.ExternalID) != targetID {
			return nil, fmt.Errorf("у пользователя уже есть другая автоматическая интеграция Pyrus")
		}

		if strings.TrimSpace(integration.ExternalID) == targetID {
			integration.IsEnabled = true
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
			IntegrationType: user.ExternalTypePyrus,
			ExternalID:      targetID,
			IsEnabled:       true,
			IsVerified:      true,
			IsLocked:        true,
			VerifiedName:    suggestion.Name,
		})
	}

	if err := h.userRepo.ReplaceIntegrations(ctx, u.ID, updatedIntegrations); err != nil {
		return nil, fmt.Errorf("не удалось сохранить интеграции пользователя")
	}

	if h.pyrusRepo != nil {
		if err := h.pyrusRepo.UpsertUserMap(ctx, &pyrus.UserMap{
			EtalonUserID: u.ID,
			PyrusUserID:  suggestion.PyrusUserID,
		}); err != nil {
			return nil, fmt.Errorf("не удалось сохранить связку с Pyrus")
		}
	}

	u.Integrations = updatedIntegrations
	u.ExternalType, u.ExternalID = pickPrimaryIntegration(updatedIntegrations)
	if u.Email == nil || strings.TrimSpace(*u.Email) == "" {
		u.Email = normalizeOptionalString(&suggestion.Email)
	}
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

func mergeLegacyIntegrationItems(
	items []api.ProfileIntegrationUpdateItemDTO,
	externalType *string,
	externalID *string,
) []api.ProfileIntegrationUpdateItemDTO {
	merged := make([]api.ProfileIntegrationUpdateItemDTO, 0, len(items)+1)
	merged = append(merged, items...)
	if externalType == nil && externalID == nil {
		return merged
	}
	merged = append(merged, api.ProfileIntegrationUpdateItemDTO{
		IntegrationType: strings.TrimSpace(strings.ToLower(derefString(externalType))),
		ExternalID:      strings.TrimSpace(derefString(externalID)),
	})
	return merged
}

func (h *UserHandler) normalizeRequestedIntegrations(
	ctx context.Context,
	u *user.User,
	items []api.ProfileIntegrationUpdateItemDTO,
	preserveLocked bool,
) ([]user.Integration, error) {
	existingByKey := make(map[string]user.Integration, len(u.Integrations))
	lockedByKey := make(map[string]user.Integration, len(u.Integrations))
	for _, existing := range u.Integrations {
		key := buildIntegrationKey(existing.IntegrationType, existing.ExternalID)
		existingByKey[key] = existing
		if preserveLocked && existing.IsEnabled && existing.IsLocked {
			lockedByKey[key] = existing
		}
	}

	normalized := make([]user.Integration, 0, len(items)+len(lockedByKey))
	seen := make(map[string]struct{}, len(items)+len(lockedByKey))
	for _, item := range items {
		typeVal := strings.TrimSpace(strings.ToLower(item.IntegrationType))
		idVal := strings.TrimSpace(item.ExternalID)
		typePtr, idPtr, err := validateExternalFields(&typeVal, &idVal)
		if err != nil {
			return nil, err
		}
		if typePtr == nil || idPtr == nil {
			continue
		}

		key := buildIntegrationKey(*typePtr, *idPtr)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		existing, hasExisting := existingByKey[key]
		integration := user.Integration{
			IntegrationType: *typePtr,
			ExternalID:      *idPtr,
			IsEnabled:       true,
		}
		if hasExisting {
			integration = existing
			integration.IntegrationType = *typePtr
			integration.ExternalID = *idPtr
		}

		if item.IsEnabled != nil {
			integration.IsEnabled = *item.IsEnabled
		} else if hasExisting {
			integration.IsEnabled = existing.IsEnabled
		} else {
			integration.IsEnabled = true
		}

		if preserveLocked {
			if lockedExisting, ok := lockedByKey[key]; ok {
				integration = lockedExisting
			}
		}

		h.enrichIntegration(ctx, u, &integration)
		normalized = append(normalized, integration)
	}

	if preserveLocked {
		for key, integration := range lockedByKey {
			if _, exists := seen[key]; exists {
				continue
			}
			normalized = append(normalized, integration)
		}
	}

	return normalized, nil
}

func pickPrimaryIntegration(items []user.Integration) (*string, *string) {
	for _, item := range items {
		if !item.IsEnabled {
			continue
		}
		integrationType := strings.TrimSpace(strings.ToLower(item.IntegrationType))
		externalID := strings.TrimSpace(item.ExternalID)
		if integrationType == "" || externalID == "" {
			continue
		}
		return &integrationType, &externalID
	}
	return nil, nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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
		case user.ExternalTypePyrus:
			return nil, nil, fmt.Errorf("для Pyrus ID должен быть числом")
		default:
			return nil, nil, fmt.Errorf("некорректный ID внешней системы")
		}
	}

	return &typeStr, &idStr, nil
}
