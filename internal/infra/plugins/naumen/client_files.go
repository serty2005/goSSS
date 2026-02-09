package naumen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchFilesBySource получает список файлов по source UUID.
func (s *naumenClientImpl) FetchFilesBySource(ctx context.Context, sourceUUID string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/find/file", s.baseURL)
	filter := map[string]string{"source": sourceUUID}

	bodyBytes, err := json.Marshal(filter)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации фильтра файлов: %w", err)
	}

	var responseList []map[string]interface{}
	err = s.doWithRetry(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes), &responseList)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения файлов по source %s: %w", sourceUUID, err)
	}
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("не удалось создать запрос download file: %w", err)
		}
		q := req.URL.Query()
		q.Add("accessKey", s.apiKey)
		req.URL.RawQuery = q.Encode()

		resp, err := s.client.Do(req)
		if err != nil {
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
			lastErr = fmt.Errorf("ошибка скачивания файла %s: HTTP %d, body=%s", fileUUID, resp.StatusCode, string(bodyBytes))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return bodyBytes, contentType, nil
	}

	return nil, "", fmt.Errorf("не удалось скачать файл %s после %d попыток: %w", fileUUID, s.maxRetries, lastErr)
}
