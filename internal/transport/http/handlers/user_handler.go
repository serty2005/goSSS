package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserHandler обрабатывает запросы для управления пользователями.
type UserHandler struct {
	db       *gorm.DB
	userSvc  services.AuthService
	userRepo repositories.UserRepo
}

// NewUserHandler создает новый экземпляр обработчика пользователей.
func NewUserHandler(db *gorm.DB, userSvc services.AuthService, userRepo repositories.UserRepo) *UserHandler {
	return &UserHandler{db, userSvc, userRepo}
}

// RegisterRoutes регистрирует роуты для пользователей.
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
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}

	userDTOs := make([]api.UserDTO, len(users))
	for i, user := range users {
		var roles []string
		if err := json.Unmarshal(user.Roles, &roles); err != nil {
			log.Warn("Failed to unmarshal user roles", "userID", user.ID, "error", err)
			roles = []string{}
		}

		userDTOs[i] = api.UserDTO{
			ID:       user.ID,
			Username: user.Username,
			FullName: user.FullName,
			Roles:    roles,
		}
	}

	RespondWithJSON(w, http.StatusOK, userDTOs)
}

func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.UserCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Конвертируем []string в datatypes.JSON
	rolesJSON, _ := json.Marshal(dto.Roles)

	user := &models.User{
		Username: dto.Username,
		FullName: dto.FullName,
		Roles:    datatypes.JSON(rolesJSON),
	}

	if err := user.HashPassword(dto.Password); err != nil {
		log.Error("Failed to hash password", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	err := h.userRepo.Create(r.Context(), user)
	if err != nil {
		log.Error("Failed to create user", "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	userDTO := api.UserDTO{
		ID:       user.ID,
		Username: user.Username,
		FullName: user.FullName,
		Roles:    dto.Roles, // Возвращаем исходный []string
	}

	RespondWithJSON(w, http.StatusCreated, userDTO)
}

func (h *UserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	var dto api.UserUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Получаем пользователя
	user, err := h.userRepo.GetByID(r.Context(), uint(id))
	if err != nil {
		log.Error("Failed to get user", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to get user")
		return
	}
	if user == nil {
		RespondWithError(w, http.StatusNotFound, "User not found")
		return
	}

	// Обновляем поля
	if dto.FullName != nil {
		user.FullName = *dto.FullName
	}
	if dto.Roles != nil {
		rolesJSON, _ := json.Marshal(dto.Roles)
		user.Roles = datatypes.JSON(rolesJSON)
	}
	if dto.Password != nil {
		if err := user.HashPassword(*dto.Password); err != nil {
			log.Error("Failed to hash password", "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Failed to update user")
			return
		}
	}

	err = h.userRepo.Update(r.Context(), user)
	if err != nil {
		log.Error("Failed to update user", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to update user")
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "user updated successfully"})
}

func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	err = h.userRepo.Delete(r.Context(), uint(id))
	if err != nil {
		log.Error("Failed to delete user", "id", id, "error", err)
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
