package gateways

import (
	"testing"

	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/stretchr/testify/assert"
)

func TestIsFileNameNumeric(t *testing.T) {
	tests := []struct {
		fileName string
		expected bool
	}{
		{"123.json", true},
		{"123456.json", true},
		{"0.json", true},
		{"abc.json", false},
		{"123abc.json", false},
		{"abc123.json", false},
		{"file.json", false},
		{"123.txt", false}, // без расширения .json
		{"", false},
		{".json", false},
	}

	for _, test := range tests {
		t.Run(test.fileName, func(t *testing.T) {
			result := isFileNameNumeric(test.fileName)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestGetFileTypeDescription(t *testing.T) {
	tests := []struct {
		fileName string
		expected string
	}{
		{"123.json", "данные ФР"},
		{"123456.json", "данные ФР"},
		{"abc.json", "данные по id/url сервера"},
		{"file.json", "данные по id/url сервера"},
	}

	for _, test := range tests {
		t.Run(test.fileName, func(t *testing.T) {
			result := getFileTypeDescription(test.fileName)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b     int
		expected int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			result := min(test.a, test.b)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestValidateAgentData(t *testing.T) {
	tests := []struct {
		name        string
		data        api.AgentDataDTO
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid_fr_data",
			data: api.AgentDataDTO{
				Hostname:     "test-host",
				URLRms:       "http://test-server",
				SerialNumber: "123456789",
			},
			expectError: false,
		},
		{
			name: "valid_server_data",
			data: api.AgentDataDTO{
				Hostname: "test-host",
				URLRms:   "http://test-server",
				CRMID:    "CRM001",
			},
			expectError: false,
		},
		{
			name: "valid_remote_access_data",
			data: api.AgentDataDTO{
				Hostname:     "test-host",
				URLRms:       "http://test-server",
				TeamviewerID: "123456789",
			},
			expectError: false,
		},
		{
			name: "missing_hostname",
			data: api.AgentDataDTO{
				URLRms:       "http://test-server",
				SerialNumber: "123456789",
			},
			expectError: true,
			errorMsg:    "отсутствует обязательное поле hostname",
		},
		{
			name: "missing_url",
			data: api.AgentDataDTO{
				Hostname:     "test-host",
				SerialNumber: "123456789",
			},
			expectError: true,
			errorMsg:    "отсутствует обязательное поле url_rms",
		},
		{
			name: "no_useful_data",
			data: api.AgentDataDTO{
				Hostname: "test-host",
				URLRms:   "http://test-server",
			},
			expectError: true,
			errorMsg:    "файл не содержит полезных данных для идентификации",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Создаем простой логгер для тестов
			logger := logger.NewSlogLogger("", "test", "info", true)

			err := validateAgentData(&test.data, logger)

			if test.expectError {
				assert.Error(t, err)
				if test.errorMsg != "" {
					assert.Contains(t, err.Error(), test.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
