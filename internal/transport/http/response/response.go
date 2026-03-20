package response

import (
	"encoding/json"
	"etalon-server/internal/transport/http/dtos"
	"net/http"
)

// Response - единый конверт для всех ответов API.
type Response struct {
	Status string      `json:"status"`          // "success" или "error"
	Data   interface{} `json:"data,omitempty"`  // Основные данные
	Meta   interface{} `json:"meta,omitempty"`  // Метаданные (пагинация, фильтры)
	Error  interface{} `json:"error,omitempty"` // Объект ошибки
}

// RespondWithError отправляет стандартизированный JSON с ошибкой.
func RespondWithError(w http.ResponseWriter, code int, message string) {
	resp := Response{
		Status: "error",
		Error: dtos.ErrorResponseDTO{
			Error: message,
		},
	}
	sendJSON(w, code, resp)
}

// RespondWithJSON отправляет успешный JSON-ответ.
// Автоматически разделяет пагинированные данные на Data и Meta.
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	resp := Response{
		Status: "success",
	}

	// Проверяем, является ли пейлоад пагинированным ответом
	if paginated, ok := payload.(dtos.PaginatedResponse); ok {
		resp.Data = paginated.Data
		resp.Meta = map[string]interface{}{
			"total":    paginated.Total,
			"limit":    paginated.Limit,
			"offset":   paginated.Offset,
			"has_next": paginated.HasNext,
			"has_prev": paginated.HasPrev,
		}
	} else {
		resp.Data = payload
	}

	sendJSON(w, code, resp)
}

// RespondWithRawJSON отправляет успешный JSON без API-конверта.
// Используется для машинных контрактов, где клиент ожидает DTO напрямую.
func RespondWithRawJSON(w http.ResponseWriter, code int, payload interface{}) {
	sendJSON(w, code, payload)
}

func sendJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","error":{"message":"Internal Server Error"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
