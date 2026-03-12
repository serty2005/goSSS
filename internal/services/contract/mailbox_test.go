package contract

import (
	"encoding/base64"
	"net/textproto"
	"strings"
	"testing"
)

// TestExtractZIPAttachmentsHandlesFoldedFilename проверяет разбор zip-вложения с нестандартным заголовком REG.RU.
func TestExtractZIPAttachmentsHandlesFoldedFilename(t *testing.T) {
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

	attachments, err := collectZIPAttachments(header, []byte(body))
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

// TestCollectZIPAttachmentsByMediaType проверяет извлечение zip-части даже без имени файла.
func TestCollectZIPAttachmentsByMediaType(t *testing.T) {
	header := textproto.MIMEHeader{
		"Content-Type":              {"application/zip"},
		"Content-Transfer-Encoding": {"base64"},
	}

	attachments, err := collectZIPAttachments(header, []byte(base64.StdEncoding.EncodeToString([]byte("zip-content"))))
	if err != nil {
		t.Fatalf("ожидалось успешное извлечение zip-вложения, получена ошибка: %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("ожидалось одно вложение, получено: %d", len(attachments))
	}
	if attachments[0].FileName != "contract-report.zip" {
		t.Fatalf("ожидалось fallback-имя contract-report.zip, получено: %q", attachments[0].FileName)
	}
}
