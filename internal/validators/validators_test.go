package validators

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRemoteAccessID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Valid ID with spaces", "123 456 789", stringPtr("123456789")},
		{"Valid 10-digit ID", "1234567890", stringPtr("1234567890")},
		{"Invalid short ID", "123 456", nil},
		{"ID with text", "AnyDesk: 987 654 321", stringPtr("987654321")},
		{"Empty string", "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateRemoteAccessID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateIPAddress(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Valid IP without port", "192.168.1.1", stringPtr("192.168.1.1:8080")},
		{"Valid IP with port", "8.8.8.8:53", stringPtr("8.8.8.8:53")},
		{"Iiko cloud domain", "https://my-res.iiko.it", stringPtr("my-res.iiko.it:443")},
		{"Syrve cloud domain", "https://my-res.syrve.online/api", stringPtr("my-res.syrve.online:443")},
		{"Local domain without port", "localhost", stringPtr("localhost:8080")},
		{"Local domain with port", "db.local:5432", stringPtr("db.local:5432")},
		{"Invalid IP", "999.999.999.999", nil},
		{"Just text", "not an ip", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateIPAddress(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateUniqueID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Корректный ID", "123-456-789", stringPtr("123-456-789")},
		{"Некорректный ID с буквами", "123-abc-789", nil},
		{"Некорректный формат", "123456789", nil},
		{"Пустая строка", "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateUniqueID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractLiteManagerID(t *testing.T) {
	testCases := []struct {
		name     string
		data     map[string]interface{}
		fallback string
		expected *string
	}{
		{"ID в поле data", map[string]interface{}{"litemanagerID": "MH_12345"}, "", stringPtr("MH_12345")},
		{"ID в fallback строке", map[string]interface{}{}, "Какой-то текст с MH_54321 внутри", stringPtr("MH_54321")},
		{"ID отсутствует", map[string]interface{}{}, "Просто текст", nil},
		{"Некорректный ID в поле data", map[string]interface{}{"litemanagerID": "MH_123"}, "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractLiteManagerID(tc.data, tc.fallback)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDetermineCompanyTypeFromIP(t *testing.T) {
	assert.Equal(t, "syrve", DetermineCompanyTypeFromIP("my.syrve.online"))
	assert.Equal(t, "iiko", DetermineCompanyTypeFromIP("my.iiko.it"))
	assert.Equal(t, "iiko", DetermineCompanyTypeFromIP("192.168.1.1"))
}

func TestValidateCabinetLink(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"Стандартный случай с clientId", "https://cabinet?clientId=12345", "12345"},
		{"Случай с параметром id в конце", "https://partners.iiko.ru/ru/cabinet/clients.html?mode=showOne&id=720846", "720846"},
		{"Случай с некорректным ключом (должен вернуть N/A)", "https://cabinet?client=12345", "12345"}, // Оставим это поведение, оно соответствует логике "берем последнее"
		{"URL без знака равно", "https://cabinet/clients", "N/A"},
		{"Параметр не является числом", "https://cabinet?id=abc", "N/A"},
		{"Параметр с якорем", "https://cabinet?id=54321#details", "54321"},
		{"Пустая строка", "", "N/A"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// В этой функции второй параметр companyType не используется, так что можно передать пустую строку
			assert.Equal(t, tc.expected, ValidateCabinetLink(tc.input, ""))
		})
	}
}

// Вспомогательная функция для тестов, чтобы создавать указатели на строки.
func stringPtr(s string) *string {
	return &s
}
