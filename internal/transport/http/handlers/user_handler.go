package handlers

import (
	"encoding/json"                      // Оставляем для старых моделей, если нужны
	"etalon-server/internal/domain/user" // Новый домен
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type UserHandler struct {
	userSvc  services.AuthService
	userRepo user.Repository
}

func NewUserHandler(userSvc services.AuthService, userRepo user.Repository) *UserHandler {
	return &UserHandler{userSvc, userRepo}
}

func (h *UserHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.GetUsers)
	r.Post("/", h.CreateUser)
	r.Put("/{id}", h.UpdateUser)
	r.Delete("/{id}", h.DeleteUser)
}

func (h *UserHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	users, err := h.userRepo.GetAll(r.Context())
	if err != nil {
		log.Error("Failed to get users", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}

	userDTOs := make([]api.UserDTO, len(users))
	for i, u := range users {
		var roles []string
		for _, role := range u.Roles {
			roles = append(roles, role.Name)
		}

		userDTOs[i] = api.UserDTO{
			ID:       u.ID,
			Username: u.Username,
			FullName: u.FullName,
			Roles:    roles,
		}
	}

	response.RespondWithJSON(w, http.StatusOK, userDTOs)
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.UserCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// 1. Конвертация имен ролей в объекты
	var roles []user.Role
	for _, roleName := range dto.Roles {
		// Используем EnsureRoleExists или GetRoleByName.
		// Для простоты, если роли должны быть предсозданы, используем Get.
		// Но чтобы не ломать создание, используем Ensure.
		role, err := h.userRepo.EnsureRoleExists(r.Context(), roleName, "")
		if err != nil {
			log.Error("Failed to find/create role", "role", roleName, "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Error processing roles")
			return
		}
		roles = append(roles, *role)
	}

	newUser := &user.User{
		Username: dto.Username,
		FullName: dto.FullName,
		Roles:    roles,
		IsActive: true,
	}

	if err := newUser.HashPassword(dto.Password); err != nil {
		log.Error("Failed to hash password", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	if err := h.userRepo.Create(r.Context(), newUser); err != nil {
		log.Error("Failed to create user", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	userDTO := api.UserDTO{
		ID:       newUser.ID,
		Username: newUser.Username,
		FullName: newUser.FullName,
		Roles:    dto.Roles,
	}

	response.RespondWithJSON(w, http.StatusCreated, userDTO)
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var dto api.UserUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	u, err := h.userRepo.GetByID(r.Context(), uint(id))
	if err != nil {
		log.Error("Failed to get user", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}
	if u == nil {
		response.RespondWithError(w, http.StatusNotFound, "User not found")
		return
	}

	// Обновляем поля
	if dto.FullName != nil {
		u.FullName = *dto.FullName
	}
	if dto.Roles != nil {
		// Очищаем старые роли и назначаем новые
		var newRoles []user.Role
		for _, roleName := range dto.Roles {
			role, err := h.userRepo.EnsureRoleExists(r.Context(), roleName, "")
			if err != nil {
				log.Error("Failed to ensure role", "role", roleName, "error", err)
				continue
			}
			newRoles = append(newRoles, *role)
		}
		// GORM: Замена связей Many2Many делается через Association().Replace
		// Но здесь мы просто обновим поле, а сохранение сделаем через Save,
		// однако для m2m лучше делать явный Replace в репо.
		// В нашем базовом репо Update делает Save(user), что обновит связи если они загружены.
		u.Roles = newRoles
	}
	if dto.Password != nil {
		if err := u.HashPassword(*dto.Password); err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, "Failed to update password")
			return
		}
	}

	if err := h.userRepo.Update(r.Context(), u); err != nil {
		log.Error("Failed to update user", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]string{"message": "user updated successfully"})
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	if err := h.userRepo.Delete(r.Context(), uint(id)); err != nil {
		log.Error("Failed to delete user", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
