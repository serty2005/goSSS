package gateways

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/jlaffaye/ftp"
	"github.com/stretchr/testify/assert"
)

type startupObservationServiceStub struct {
	reconcileCalls int
	onReconcile    func()
}

func (s *startupObservationServiceStub) ApplyObservation(context.Context, string, *api.AgentDataDTO) (*models.AgentObservation, error) {
	return &models.AgentObservation{}, nil
}

func (s *startupObservationServiceStub) ApproveCandidate(context.Context, services.CandidateApproveInput) (*models.Candidate, error) {
	return nil, nil
}

func (s *startupObservationServiceStub) RejectCandidate(context.Context, services.CandidateRejectInput) (*models.Candidate, error) {
	return nil, nil
}

func (s *startupObservationServiceStub) RecalculateCandidates(context.Context) (*services.CandidateRecalculationResult, error) {
	return &services.CandidateRecalculationResult{}, nil
}

func (s *startupObservationServiceStub) ReconcileActualAgentObservations(context.Context) (*services.ActualObservationReconciliationResult, error) {
	s.reconcileCalls++
	if s.onReconcile != nil {
		s.onReconcile()
	}
	return &services.ActualObservationReconciliationResult{}, nil
}

type startupFTPClientStub struct{}

func (startupFTPClientStub) ListFiles(string) ([]*ftp.Entry, error) {
	return nil, nil
}

func (startupFTPClientStub) DownloadFile(string) ([]byte, error) {
	return nil, nil
}

func (startupFTPClientStub) GetModTime(string) (time.Time, error) {
	return time.Time{}, nil
}

func (startupFTPClientStub) IsTimePreciseInList() bool {
	return true
}

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

func TestComputePayloadHash(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string // Ожидаемый SHA256 хеш
	}{
		{
			name:     "empty_data",
			data:     []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple_string",
			data:     []byte("hello"),
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:     "json_data",
			data:     []byte(`{"hostname":"test","url_rms":"http://test"}`),
			expected: "9a6b6a9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b9b", // Будет вычислен
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := computePayloadHash(test.data)

			// Хеш всегда должен быть 64 символа (SHA256 в hex)
			assert.Len(t, result, 64)

			// Для известных значений проверяем точное совпадение
			if test.name != "json_data" {
				assert.Equal(t, test.expected, result)
			}

			// Одинаковые данные должны давать одинаковый хеш
			result2 := computePayloadHash(test.data)
			assert.Equal(t, result, result2)
		})
	}
}

func TestComputePayloadHashDeterminism(t *testing.T) {
	// Проверка идемпотентности: одинаковые данные должны всегда давать одинаковый хеш
	data := []byte(`{"hostname":"test-server","url_rms":"192.168.1.1","serial_number":"12345"}`)

	hash1 := computePayloadHash(data)
	hash2 := computePayloadHash(data)
	hash3 := computePayloadHash(data)

	assert.Equal(t, hash1, hash2, "хеш должен быть детерминированным")
	assert.Equal(t, hash2, hash3, "хеш должен быть детерминированным")

	// Разные данные должны давать разный хеш
	differentData := []byte(`{"hostname":"test-server","url_rms":"192.168.1.1","serial_number":"12346"}`)
	differentHash := computePayloadHash(differentData)

	assert.NotEqual(t, hash1, differentHash, "разные данные должны давать разный хеш")
}

func TestAgentFTPGatewayStart_ВыполняетСверкуАктуальныхНаблюденийДоПервогоЦикла(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	obsSvc := &startupObservationServiceStub{
		onReconcile: cancel,
	}
	gateway := NewAgentFTPGateway(
		&config.Config{
			AgentFTPInterval: time.Hour,
		},
		logger.New("", "test", "error", true),
		nil,
		startupFTPClientStub{},
		obsSvc,
	)

	gateway.Start(ctx)

	assert.Equal(t, 1, obsSvc.reconcileCalls)
}
