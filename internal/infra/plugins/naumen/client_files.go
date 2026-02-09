package naumen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchFilesBySource получает список файлов по source UUID.
func (s *naumenClientImpl) FetchFilesBySource(ctx context.Context, sourceUUID string) ([]map[string]interface{}, error) {
	s.logger.Info("Запрос файлов Naumen по одной заявке", "source_uuid", sourceUUID)
	responseList, err := s.FetchFilesBySources(ctx, []string{sourceUUID})
	if err != nil {
		return nil, fmt.Errorf("ошибка получения файлов по source %s: %w", sourceUUID, err)
	}
	s.logger.Info("Получены файлы Naumen по одной заявке", "source_uuid", sourceUUID, "count", len(responseList))
	return responseList, nil
}

// FetchFilesBySources получает список файлов по набору source UUID.
func (s *naumenClientImpl) FetchFilesBySources(ctx context.Context, sourceUUIDs []string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/find/file", s.baseURL)

	unique := make([]string, 0, len(sourceUUIDs))
	seen := make(map[string]struct{}, len(sourceUUIDs))
	for _, sourceUUID := range sourceUUIDs {
		sourceUUID = strings.TrimSpace(sourceUUID)
		if sourceUUID == "" {
			continue
		}
		if _, ok := seen[sourceUUID]; ok {
			continue
		}
		seen[sourceUUID] = struct{}{}
		unique = append(unique, sourceUUID)
	}
	if len(unique) == 0 {
		return []map[string]interface{}{}, nil
	}
	s.logger.Info("Запрос batch-файлов Naumen", "sources_count", len(unique))

	filter := map[string][]string{"source": unique}
	bodyBytes, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации фильтра файлов: %w", err)
	}

	var responseList []map[string]interface{}
	err = s.doWithRetry(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes), &responseList)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения файлов по source: %w", err)
	}
	s.logger.Info("Получены batch-файлы Naumen", "sources_count", len(unique), "files_count", len(responseList))
	return responseList, nil
}

// DownloadFile скачивает файл по UUID.
func (s *naumenClientImpl) DownloadFile(ctx context.Context, fileUUID string) ([]byte, string, error) {
	if fileUUID == "" {
		return nil, "", fmt.Errorf("fileUUID не может быть пустым")
	}

	var lastErr error
	for i := 0; i < s.maxRetries; i++ {
		if err := s.limiter.Wait(ctx); err != nil {
			return nil, "", err
		}

		url := fmt.Sprintf("%s/get-file/%s", s.baseURL, fileUUID)
		s.logger.Info("Запрос загрузки файла из Naumen", "file_uuid", fileUUID, "attempt", i+1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("не удалось создать запрос download file: %w", err)
		}
		q := req.URL.Query()
		q.Add("accessKey", s.apiKey)
		req.URL.RawQuery = q.Encode()

		resp, err := s.client.Do(req)
		if err != nil {
			s.logger.Warn("Ошибка выполнения запроса загрузки файла из Naumen", "file_uuid", fileUUID, "attempt", i+1, "error", err)
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		bodyBytes, readErr := io.ReadAll(resp.Body)
		contentType := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("ошибка чтения файла %s: %w", fileUUID, readErr)
		}

		if resp.StatusCode >= 400 {
			s.logger.Warn("Naumen вернул ошибку при загрузке файла", "file_uuid", fileUUID, "attempt", i+1, "status_code", resp.StatusCode)
			lastErr = fmt.Errorf("ошибка скачивания файла %s: HTTP %d, body=%s", fileUUID, resp.StatusCode, string(bodyBytes))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if contentType == "" {
			contentType = "application/octet-stream"
		}
		s.logger.Info("Файл успешно загружен из Naumen", "file_uuid", fileUUID, "bytes", len(bodyBytes), "content_type", contentType)
		return bodyBytes, contentType, nil
	}

	return nil, "", fmt.Errorf("не удалось скачать файл %s после %d попыток: %w", fileUUID, s.maxRetries, lastErr)
}
