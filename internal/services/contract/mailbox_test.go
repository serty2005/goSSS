package contract

import (
	"encoding/base64"
	"net/textproto"
	"strings"
	"testing"
)

// TestExtractReportAttachmentsHandlesFoldedFilename проверяет разбор вложения с нестандартным заголовком REG.RU.
func TestExtractReportAttachmentsHandlesFoldedFilename(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("zip-content"))
	header := textproto.MIMEHeader{
		"Content-Type":              {`application/zip; name="=?utf-8?B?0YLQtdGB0YIuemlw?="`},
		"Content-Transfer-Encoding": {"base64"},
		"Content-Disposition": {strings.Join([]string{
			"attachment;",
			` filename="=?utf-8?B?0YDQsNGB0YHRi9C70LrQsCDQtNC70Y8g0LjQvdGC0LXQs9GA0LA=?=`,
			` =?utf-8?B?0YbQuNC4ICDRgtC+0YfQtdC6ICDRgSDQsdC40YLRgNC40LrRgdC+0Lwg0L7RgiA=?=`,
			` =?utf-8?B?MjAyNi0wMy0xMS56aXA=?="; filename=рассылка для интеграции  точек  с битриксом от 2026-03-11.zip`,
		}, "\r\n")},
	}

	attachments, err := collectReportAttachments(nil, header, []byte(body), 0)
	if err != nil {
		t.Fatalf("ожидалось успешное извлечение zip-вложения, получена ошибка: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("ожидалось одно вложение, получено: %d", len(attachments))
	}
	if attachments[0].FileName == "" {
		t.Fatal("ожидалось имя файла вложения")
	}
	if attachments[0].FileName != "рассылка для интеграции  точек  с битриксом от 2026-03-11.zip" {
		t.Fatalf("неожиданное имя файла: %q", attachments[0].FileName)
	}
	if string(attachments[0].Content) != "zip-content" {
		t.Fatalf("неожиданный контент вложения: %q", string(attachments[0].Content))
	}
}

// TestCollectReportAttachmentsByMediaType проверяет fallback-имя вложения по MIME-типу.
func TestCollectReportAttachmentsByMediaType(t *testing.T) {
	testCases := []struct {
		name        string
		mediaType   string
		disposition string
		expected    string
	}{
		{name: "zip", mediaType: "application/zip", expected: "contract-report.zip"},
		{name: "xls", mediaType: "application/vnd.ms-excel", expected: "contract-report.xls"},
		{name: "xlsx", mediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", expected: "contract-report.xlsx"},
		{name: "html", mediaType: "text/html", disposition: "attachment", expected: "contract-report.html"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			header := textproto.MIMEHeader{
				"Content-Type":              {tc.mediaType},
				"Content-Transfer-Encoding": {"base64"},
			}
			if tc.disposition != "" {
				header["Content-Disposition"] = []string{tc.disposition}
			}

			attachments, err := collectReportAttachments(nil, header, []byte(base64.StdEncoding.EncodeToString([]byte("report-content"))), 0)
			if err != nil {
				t.Fatalf("ожидалось успешное извлечение вложения %s, получена ошибка: %v", tc.mediaType, err)
			}
			if len(attachments) != 1 {
				t.Fatalf("ожидалось одно вложение, получено: %d", len(attachments))
			}
			if attachments[0].FileName != tc.expected {
				t.Fatalf("ожидалось fallback-имя %q, получено: %q", tc.expected, attachments[0].FileName)
			}
		})
	}
}

// TestCollectReportAttachmentsSkipsInlineHTMLBody проверяет, что html-тело письма не считается вложением отчёта.
func TestCollectReportAttachmentsSkipsInlineHTMLBody(t *testing.T) {
	header := textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=utf-8"},
	}

	attachments, err := collectReportAttachments(nil, header, []byte("<html><body>служебный текст</body></html>"), 0)
	if err != nil {
		t.Fatalf("не ожидалась ошибка при разборе html-тела письма: %v", err)
	}
	if len(attachments) != 0 {
		t.Fatalf("html-тело письма не должно считаться вложением отчёта, получено вложений: %d", len(attachments))
	}
}

// TestDecodeMIMEHeaderValue проверяет декодирование кириллицы в теме письма.
func TestDecodeMIMEHeaderValue(t *testing.T) {
	raw := "=?utf-8?B?0YDQsNGB0YHRi9C70LrQsCDQtNC70Y8g0LjQvdGC0LXQs9GA0LDRhtC4?= =?utf-8?B?0LggINGC0L7Rh9C10LogINGBINCx0LjRgtGA0LjQutGB0L7QvCDQvtGCIDEwLjA=?= =?utf-8?B?My4yMDI2?="
	expected := "рассылка для интеграции  точек  с битриксом от 10.03.2026"

	if decoded := decodeMIMEHeaderValue(raw); decoded != expected {
		t.Fatalf("ожидалась декодированная тема %q, получено: %q", expected, decoded)
	}
}
