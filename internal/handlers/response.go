package handlers

import (
	"encoding/json"
	"etalon-server/internal/api"
	"net/http"
)

// RespondWithError отправляет стандартизированный JSON с ошибкой.
func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, api.ErrorResponseDTO{Error: message})
}

// RespondWithJSON отправляет JSON-ответ с указанным кодом и данными.
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		// В случае ошибки маршалинга, отправляем внутреннюю ошибку сервера
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
