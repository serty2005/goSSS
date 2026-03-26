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
		{"ID во fallback строке", map[string]interface{}{}, "Какой-то текст с MH_54321 внутри", stringPtr("MH_54321")},
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
		{"Только числовой ID", "893403", "893403"},
		{"Стандартный случай с clientId", "https://cabinet?clientId=12345", "12345"},
		{"Случай с параметром id в конце", "https://partners.iiko.ru/ru/cabinet/clients.html?mode=showOne&id=720846", "720846"},
		{"Некорректный ключ параметра, но c ID", "https://cabinet?client=12345", "12345"},
		{"URL без '=' но с цифрами в пути", "https://cabinet/clients/8747265", "8747265"},
		{"Параметр не является числом", "https://cabinet?id=abc", "N/A"},
		{"Параметр с якорем", "https://cabinet?id=54321#details", "54321"},
		{"Пустая строка", "", "N/A"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ValidateCabinetLink(tc.input, ""))
		})
	}
}

func TestBuildPartnersPortalLink(t *testing.T) {
	syrveLink := BuildPartnersPortalLink("893403", "https://code.syrve.online/resto/")
	if assert.NotNil(t, syrveLink) {
		assert.Equal(t, "https://pp.syrve.com/en/cabinet/client-area/index.html?clientId=893403", *syrveLink)
	}

	iikoLink := BuildPartnersPortalLink("8747265", "10.10.10.10:8080")
	if assert.NotNil(t, iikoLink) {
		assert.Equal(t, "https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=8747265", *iikoLink)
	}

	assert.Nil(t, BuildPartnersPortalLink("N/A", "https://code.syrve.online/resto/"))
	assert.Nil(t, BuildPartnersPortalLink("", "10.10.10.10:8080"))
}

func TestValidateWorkstationRemoteID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Допустимый буквенно-цифровой ID", "LM-123_ABC", stringPtr("LM-123_ABC")},
		{"Пустой ID", "", nil},
		{"Некорректные символы", "LM 123 !", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateWorkstationRemoteID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateIikoWebLink(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{
			name:     "Корректная ссылка iikoWeb",
			input:    "https://809-613-203.iikoweb.ru",
			expected: stringPtr("https://809-613-203.iikoweb.ru/"),
		},
		{
			name:     "Корректная ссылка SyrveApp с query и регистром",
			input:    "HTTP://Restaurant-Margaret.SYRVE.APP/app?foo=bar",
			expected: stringPtr("https://restaurant-margaret.syrve.app/"),
		},
		{
			name:     "Извлечение домена из произвольного текста",
			input:    "смотри сюда: restaurant-margaret.syrve.app/login",
			expected: stringPtr("https://restaurant-margaret.syrve.app/"),
		},
		{
			name:     "Некорректный домен",
			input:    "https://example.com",
			expected: nil,
		},
		{
			name:     "Пустая строка",
			input:    "",
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateIikoWebLink(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Вспомогательная функция для тестов, чтобы создавать указатели на строки.
func stringPtr(s string) *string {
	return &s
}
