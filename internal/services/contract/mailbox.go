package contract

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const contractMailboxLookbackDays = 45

type ContractMailboxClient interface {
	FetchReports(ctx context.Context) ([]ContractMailReport, error)
}

type contractMailboxClient struct {
	cfg *config.Config
	log logger.LoggerInterface
}

// NewContractMailboxClient создает IMAP-клиент для чтения ежедневной контрактной рассылки.
func NewContractMailboxClient(cfg *config.Config, log logger.LoggerInterface) ContractMailboxClient {
	return &contractMailboxClient{cfg: cfg, log: log}
}

// FetchReports подключается к IMAP, читает письма из Inbox и извлекает из них отчеты.
func (c *contractMailboxClient) FetchReports(ctx context.Context) ([]ContractMailReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || c.cfg == nil {
		return nil, errors.New("imap-клиент контрактов не инициализирован")
	}
	if strings.TrimSpace(c.cfg.ContractIMAPHost) == "" || strings.TrimSpace(c.cfg.ContractIMAPUsername) == "" || strings.TrimSpace(c.cfg.ContractIMAPPassword) == "" {
		return nil, errors.New("не настроено подключение к контрактному IMAP-ящику")
	}

	addr := net.JoinHostPort(strings.TrimSpace(c.cfg.ContractIMAPHost), strconv.Itoa(c.cfg.ContractIMAPPort))
	conn, err := client.DialTLS(addr, &tls.Config{
		ServerName: strings.TrimSpace(c.cfg.ContractIMAPHost),
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось подключиться к IMAP-серверу %s: %w", addr, err)
	}
	defer func() { _ = conn.Logout() }()

	if err := conn.Login(strings.TrimSpace(c.cfg.ContractIMAPUsername), c.cfg.ContractIMAPPassword); err != nil {
		return nil, fmt.Errorf("ошибка авторизации в IMAP: %w", err)
	}

	mailbox := strings.TrimSpace(c.cfg.ContractIMAPMailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if _, err := conn.Select(mailbox, true); err != nil {
		return nil, fmt.Errorf("не удалось открыть папку %q: %w", mailbox, err)
	}
	c.log.Info("IMAP: папка открыта", "mailbox", mailbox)

	criteria := imap.NewSearchCriteria()
	criteria.Since = time.Now().AddDate(0, 0, -contractMailboxLookbackDays)

	seqNums, err := conn.Search(criteria)
	if err != nil {
		return nil, fmt.Errorf("не удалось выполнить поиск писем в IMAP: %w", err)
	}
	c.log.Info("IMAP: выполнен поиск писем для контрактов", "mailbox", mailbox, "messages_found", len(seqNums), "lookback_days", contractMailboxLookbackDays)
	if len(seqNums) == 0 {
		return []ContractMailReport{}, nil
	}

	seqSet := new(imap.SeqSet)
	seqSet.AddNum(seqNums...)

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchInternalDate, section.FetchItem()}
	messages := make(chan *imap.Message, min(len(seqNums), 32))

	var fetchErr error
	go func() {
		fetchErr = conn.Fetch(seqSet, items, messages)
	}()

	reports := make([]ContractMailReport, 0, len(seqNums))
	attachmentsFound := 0
	for message := range messages {
		if err := ctx.Err(); err != nil {
			return reports, err
		}
		if message == nil {
			continue
		}
		body := message.GetBody(section)
		if body == nil {
			continue
		}
		rawMessage, err := io.ReadAll(body)
		if err != nil {
			c.log.Warn("не удалось прочитать тело письма с контрактами", "seq_num", message.SeqNum, "error", err)
			continue
		}

		extracted, err := c.extractReportsFromMessage(rawMessage, message)
		if err != nil {
			c.log.Warn("не удалось извлечь отчёт по контрактам из письма", "seq_num", message.SeqNum, "error", err)
			continue
		}
		attachmentsFound += len(extracted)
		reports = append(reports, extracted...)
	}

	if fetchErr != nil {
		return reports, fmt.Errorf("ошибка чтения писем из IMAP: %w", fetchErr)
	}
	c.log.Info("IMAP: чтение контрактной рассылки завершено", "messages_processed", len(seqNums), "attachments_found", attachmentsFound, "reports_extracted", len(reports))

	return reports, nil
}

// extractReportsFromMessage разбирает MIME-письмо и преобразует вложения в набор отчетов.
func (c *contractMailboxClient) extractReportsFromMessage(raw []byte, message *imap.Message) ([]ContractMailReport, error) {
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать MIME-письмо: %w", err)
	}

	messageID := messageIDFromMail(parsed, envelopeMessageID(message))
	subject := decodeMIMEHeaderValue(cmpOr(parsed.Header.Get("Subject"), envelopeSubject(message)))
	receivedAt := envelopeDate(message)

	attachments, err := extractReportAttachments(c.log, parsed.Header, mustReadAll(parsed.Body))
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	c.log.Info(
		"IMAP: в письме найдены вложения с контрактами",
		"message_id", messageID,
		"subject", subject,
		"attachments_count", len(attachments),
	)

	reports := make([]ContractMailReport, 0, len(attachments))
	parseErrors := make([]error, 0, len(attachments))
	now := time.Now().UTC()
	for _, attachment := range attachments {
		if len(attachment.Content) > c.cfg.ContractZipMaxBytes {
			parseErrors = append(parseErrors, fmt.Errorf("вложение %q превышает допустимый размер %d байт", attachment.FileName, c.cfg.ContractZipMaxBytes))
			continue
		}

		c.log.Debug(
			"IMAP: начинаем разбор вложения с контрактами",
			"message_id", messageID,
			"attachment_name", attachment.FileName,
			"attachment_size", len(attachment.Content),
		)

		rows, attachmentHash, err := parseContractReportArchive(c.log, attachment.FileName, attachment.Content, now)
		if err != nil {
			c.log.Debug(
				"IMAP: вложение не удалось разобрать, продолжаем обработку остальных",
				"message_id", messageID,
				"attachment_name", attachment.FileName,
				"error", err,
			)
			parseErrors = append(parseErrors, fmt.Errorf("не удалось разобрать вложение %q: %w", attachment.FileName, err))
			continue
		}
		c.log.Info(
			"IMAP: вложение с контрактами разобрано",
			"message_id", messageID,
			"attachment_name", attachment.FileName,
			"attachment_hash", attachmentHash,
			"rows_extracted", len(rows),
		)
		reports = append(reports, ContractMailReport{
			MessageID:      messageID,
			Subject:        subject,
			ReceivedAt:     receivedAt,
			AttachmentName: attachment.FileName,
			AttachmentHash: attachmentHash,
			Rows:           rows,
		})
	}

	if len(reports) == 0 && len(parseErrors) > 0 {
		return nil, errors.Join(parseErrors...)
	}

	return reports, nil
}

type reportAttachment struct {
	FileName string
	Content  []byte
}

// extractReportAttachments запускает рекурсивный поиск поддерживаемых вложений в MIME-письме.
func extractReportAttachments(log logger.LoggerInterface, header mail.Header, body []byte) ([]reportAttachment, error) {
	return collectReportAttachments(log, textproto.MIMEHeader(header), body, 0)
}

// collectReportAttachments обходит MIME-части письма и собирает поддерживаемые вложения.
func collectReportAttachments(log logger.LoggerInterface, header textproto.MIMEHeader, body []byte, depth int) ([]reportAttachment, error) {
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	dispositionType := attachmentDispositionType(header.Get("Content-Disposition"))
	decodedBody, err := decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return nil, err
	}
	if log != nil {
		log.Debug(
			"IMAP: анализ MIME-части письма",
			"mime_depth", depth,
			"content_type", strings.TrimSpace(header.Get("Content-Type")),
			"content_disposition", strings.TrimSpace(header.Get("Content-Disposition")),
			"disposition_type", dispositionType,
			"media_type", mediaType,
			"decoded_size", len(decodedBody),
		)
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := strings.TrimSpace(params["boundary"])
		if boundary == "" {
			return nil, errors.New("multipart-письмо не содержит boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(decodedBody), boundary)
		collected := make([]reportAttachment, 0, 2)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("не удалось прочитать часть MIME-сообщения: %w", err)
			}
			partBody, readErr := io.ReadAll(part)
			_ = part.Close()
			if readErr != nil {
				return nil, fmt.Errorf("не удалось прочитать MIME-часть: %w", readErr)
			}
			nested, nestedErr := collectReportAttachments(log, part.Header, partBody, depth+1)
			if nestedErr != nil {
				return nil, nestedErr
			}
			collected = append(collected, nested...)
		}
		return collected, nil
	}

	fileName := attachmentFileName(header)
	isReport := isReportAttachment(mediaType, fileName)
	skipAsBody := shouldSkipMessageBodyPart(mediaType, dispositionType, fileName)
	if log != nil {
		log.Debug(
			"IMAP: MIME-часть проверена на отчёт",
			"mime_depth", depth,
			"media_type", mediaType,
			"disposition_type", dispositionType,
			"detected_file_name", fileName,
			"is_report_attachment", isReport,
			"skipped_as_message_body", skipAsBody,
		)
	}
	if !isReport || skipAsBody {
		return nil, nil
	}
	if fileName == "" {
		fileName = defaultReportAttachmentName(mediaType)
	}
	if log != nil {
		log.Debug(
			"IMAP: найдено вложение отчёта",
			"mime_depth", depth,
			"attachment_name", fileName,
			"media_type", mediaType,
			"attachment_size", len(decodedBody),
		)
	}

	return []reportAttachment{{
		FileName: fileName,
		Content:  decodedBody,
	}}, nil
}

// decodeTransferEncoding декодирует MIME-часть согласно указанному Content-Transfer-Encoding.
func decodeTransferEncoding(body []byte, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return body, nil
	case "base64":
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(body)))
		if err != nil {
			return nil, fmt.Errorf("не удалось декодировать base64 MIME-часть: %w", err)
		}
		return decoded, nil
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			return nil, fmt.Errorf("не удалось декодировать quoted-printable MIME-часть: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("неподдерживаемое Content-Transfer-Encoding: %s", encoding)
	}
}

// attachmentFileName извлекает имя вложения из MIME-заголовков части письма.
func attachmentFileName(header textproto.MIMEHeader) string {
	for _, raw := range []string{header.Get("Content-Disposition"), header.Get("Content-Type")} {
		if fileName := extractAttachmentNameFromHeader(raw); fileName != "" {
			return fileName
		}
	}
	return ""
}

// attachmentDispositionType извлекает тип disposition из заголовка Content-Disposition.
func attachmentDispositionType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if mediaType, _, err := mime.ParseMediaType(trimmed); err == nil {
		return strings.ToLower(strings.TrimSpace(mediaType))
	}
	mediaType, _, _ := strings.Cut(trimmed, ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// shouldSkipMessageBodyPart отбрасывает html-тело письма, ошибочно похожее на вложение отчёта.
func shouldSkipMessageBodyPart(mediaType, dispositionType, fileName string) bool {
	if strings.TrimSpace(fileName) != "" || dispositionType == "attachment" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "text/html", "application/html":
		return true
	default:
		return false
	}
}

// defaultReportAttachmentName возвращает fallback-имя вложения с расширением по MIME-типу.
func defaultReportAttachmentName(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "application/zip", "application/x-zip", "application/x-zip-compressed":
		return "contract-report.zip"
	case "application/vnd.ms-excel", "application/msexcel", "application/x-msexcel":
		return "contract-report.xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "contract-report.xlsx"
	case "text/html", "application/html":
		return "contract-report.html"
	default:
		return "contract-report"
	}
}

// isReportAttachment проверяет MIME-часть на принадлежность к поддерживаемому вложению отчета.
func isReportAttachment(mediaType string, fileName string) bool {
	normalizedType := strings.ToLower(strings.TrimSpace(mediaType))
	switch normalizedType {
	case "application/zip", "application/x-zip", "application/x-zip-compressed":
		return true
	case "application/vnd.ms-excel", "application/msexcel", "application/x-msexcel":
		return true
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return true
	case "text/html", "application/html":
		return true
	}

	switch strings.ToLower(filepathExt(fileName)) {
	case ".zip", ".xls", ".xlsx", ".html", ".htm":
		return true
	default:
		return false
	}
}

// extractAttachmentNameFromHeader извлекает имя вложения даже из нестандартных MIME-заголовков.
func extractAttachmentNameFromHeader(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if _, params, err := mime.ParseMediaType(trimmed); err == nil {
		for _, key := range []string{"filename*", "filename", "name*", "name"} {
			if value := decodeAttachmentHeaderValue(params[key], strings.HasSuffix(key, "*")); value != "" {
				return value
			}
		}
	}

	for _, key := range []string{"filename*", "filename", "name*", "name"} {
		if value := extractRawHeaderParam(trimmed, key); value != "" {
			return decodeAttachmentHeaderValue(value, strings.HasSuffix(key, "*"))
		}
	}

	return ""
}

// extractRawHeaderParam извлекает значение параметра из сырого MIME-заголовка без строгого парсинга.
func extractRawHeaderParam(raw string, key string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "\r", " "), "\n", " ")
	lower := strings.ToLower(normalized)
	target := strings.ToLower(key) + "="
	start := strings.Index(lower, target)
	if start < 0 {
		return ""
	}

	value := normalized[start+len(target):]
	value = strings.TrimLeft(value, " \t")
	if value == "" {
		return ""
	}
	if value[0] == '"' {
		value = value[1:]
		end := strings.IndexByte(value, '"')
		if end < 0 {
			return strings.TrimSpace(value)
		}
		return strings.TrimSpace(value[:end])
	}

	end := strings.IndexByte(value, ';')
	if end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

// decodeAttachmentHeaderValue декодирует MIME-имя файла из обычного или RFC 2231 формата.
func decodeAttachmentHeaderValue(raw string, isRFC2231 bool) string {
	value := strings.Trim(strings.TrimSpace(raw), "\"")
	if value == "" {
		return ""
	}
	if isRFC2231 {
		if decoded, err := decodeRFC2231Value(value); err == nil && decoded != "" {
			value = decoded
		}
	}

	return decodeMIMEHeaderValue(value)
}

// decodeRFC2231Value декодирует значение параметра filename*=charset”value.
func decodeRFC2231Value(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if strings.Count(value, "'") < 2 {
		decoded, err := url.QueryUnescape(value)
		if err != nil {
			return value, err
		}
		return decoded, nil
	}

	first := strings.IndexByte(value, '\'')
	second := strings.IndexByte(value[first+1:], '\'')
	if first < 0 || second < 0 {
		return value, nil
	}
	second += first + 1
	encodedValue := value[second+1:]
	decoded, err := url.QueryUnescape(encodedValue)
	if err != nil {
		return value, err
	}
	return decoded, nil
}

// decodeMIMEHeaderValue декодирует RFC 2047 encoded-word и возвращает исходное значение при неудаче.
func decodeMIMEHeaderValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	decoder := new(mime.WordDecoder)
	if decoded, err := decoder.DecodeHeader(value); err == nil && strings.TrimSpace(decoded) != "" {
		return strings.TrimSpace(decoded)
	}

	return value
}

// envelopeMessageID читает Message-ID из IMAP envelope.
func envelopeMessageID(message *imap.Message) string {
	if message == nil || message.Envelope == nil {
		return ""
	}
	return strings.TrimSpace(message.Envelope.MessageId)
}

// envelopeSubject читает тему письма из IMAP envelope.
func envelopeSubject(message *imap.Message) string {
	if message == nil || message.Envelope == nil {
		return ""
	}
	return strings.TrimSpace(message.Envelope.Subject)
}

// envelopeDate определяет дату письма по envelope или internal date.
func envelopeDate(message *imap.Message) *time.Time {
	if message == nil {
		return nil
	}
	if message.Envelope != nil && !message.Envelope.Date.IsZero() {
		date := message.Envelope.Date.UTC()
		return &date
	}
	if !message.InternalDate.IsZero() {
		date := message.InternalDate.UTC()
		return &date
	}
	return nil
}

// cmpOr возвращает первую непустую строку из списка значений.
func cmpOr(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// mustReadAll считывает поток целиком и подавляет ошибку чтения для best-effort парсинга письма.
func mustReadAll(reader io.Reader) []byte {
	if reader == nil {
		return nil
	}
	content, _ := io.ReadAll(reader)
	return content
}

// filepathExt возвращает расширение файла без обращения к файловой системе.
func filepathExt(name string) string {
	idx := strings.LastIndexByte(name, '.')
	if idx < 0 {
		return ""
	}
	return name[idx:]
}
