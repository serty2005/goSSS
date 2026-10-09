package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/transport/http/response"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// materialMaxFilesPerRequest ограничивает число файлов в одном запросе загрузки.
	materialMaxFilesPerRequest = 10
	// materialMultipartMemory - сколько multipart-данных держится в памяти до сброса на диск.
	materialMultipartMemory  = 8 << 20
	materialMaxFileNameRunes = 150
)

var errMaterialFilesDisabled = errors.New("хранилище файлов материалов не настроено")

// MaterialFileStore - S3-хранилище файлов материалов.
type MaterialFileStore interface {
	PutFile(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string, contentDisposition string) error
	DeleteFile(ctx context.Context, key string) error
}

type materialFileDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
	CreatedAt   string `json:"created_at"`
}

type materialFilesConfigDTO struct {
	Enabled      bool  `json:"enabled"`
	MaxFileBytes int64 `json:"max_file_bytes"`
	MaxFiles     int   `json:"max_files"`
}

// WithFileStorage подключает S3-хранилище файлов. Ссылки строятся как publicBaseURL/<ключ объекта>.
func (h *MaterialHandler) WithFileStorage(store MaterialFileStore, publicBaseURL string, maxFileBytes int64) *MaterialHandler {
	h.fileStore = store
	h.filePublicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	h.fileMaxBytes = maxFileBytes
	return h
}

func (h *MaterialHandler) filesEnabled() bool {
	return h.fileStore != nil && h.filePublicBaseURL != "" && h.fileMaxBytes > 0
}

// FilesConfig сообщает клиенту, доступна ли загрузка файлов и какие у нее ограничения.
func (h *MaterialHandler) FilesConfig(w http.ResponseWriter, _ *http.Request) {
	response.RespondWithJSON(w, http.StatusOK, materialFilesConfigDTO{
		Enabled:      h.filesEnabled(),
		MaxFileBytes: h.fileMaxBytes,
		MaxFiles:     materialMaxFilesPerRequest,
	})
}

// UploadFiles добавляет файлы к существующему материалу.
func (h *MaterialHandler) UploadFiles(w http.ResponseWriter, r *http.Request) {
	if !h.filesEnabled() {
		response.RespondWithError(w, http.StatusServiceUnavailable, errMaterialFilesDisabled.Error())
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	headers, status, err := h.readMultipartFiles(w, r)
	if err != nil {
		response.RespondWithError(w, status, err.Error())
		return
	}
	if len(headers) == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "Не выбраны файлы")
		return
	}

	var count int64
	if err := h.db.WithContext(r.Context()).Model(&models.Material{}).Where("id = ?", id).Count(&count).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить материал: "+err.Error())
		return
	}
	if count == 0 {
		response.RespondWithError(w, http.StatusNotFound, "Материал не найден")
		return
	}

	authorID, _ := h.resolveAuthor(r.Context())
	files, err := h.storeFiles(r.Context(), id, headers, authorID)
	if err != nil {
		response.RespondWithError(w, http.StatusBadGateway, "Не удалось сохранить файлы: "+err.Error())
		return
	}
	if err := h.db.WithContext(r.Context()).Create(&files).Error; err != nil {
		h.deleteStoredFiles(files)
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось записать файлы материала: "+err.Error())
		return
	}
	h.db.WithContext(r.Context()).Model(&models.Material{}).Where("id = ?", id).Update("updated_at", time.Now())

	var item models.Material
	if err := h.preloadMaterial(h.db.WithContext(r.Context())).Where("id = ?", id).First(&item).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить материал: "+err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, h.toMaterialDTO(item))
}

// DeleteFile удаляет один файл материала вместе с объектом в S3.
func (h *MaterialHandler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	materialID := strings.TrimSpace(chi.URLParam(r, "id"))
	fileID := strings.TrimSpace(chi.URLParam(r, "fileID"))
	ctx := r.Context()

	var file models.MaterialFile
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var material models.Material
		if err := tx.Where("id = ?", materialID).First(&material).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND material_id = ?", fileID, materialID).First(&file).Error; err != nil {
			return err
		}
		var remaining int64
		if err := tx.Model(&models.MaterialFile{}).
			Where("material_id = ? AND id <> ?", materialID, fileID).
			Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 && strings.TrimSpace(material.Content) == "" {
			return errMaterialWithoutBody
		}
		return tx.Where("id = ?", fileID).Delete(&models.MaterialFile{}).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.RespondWithError(w, http.StatusNotFound, "Файл не найден")
		return
	case errors.Is(err, errMaterialWithoutBody):
		response.RespondWithError(w, http.StatusConflict, errMaterialWithoutBody.Error())
		return
	case err != nil:
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось удалить файл: "+err.Error())
		return
	}
	h.deleteStoredFiles([]models.MaterialFile{file})
	h.db.WithContext(ctx).Model(&models.Material{}).Where("id = ?", materialID).Update("updated_at", time.Now())
	w.WriteHeader(http.StatusNoContent)
}

var errMaterialWithoutBody = errors.New("у материала должно быть содержание или хотя бы один файл")

// readMultipartFiles разбирает multipart-запрос и проверяет число и размер файлов.
func (h *MaterialHandler) readMultipartFiles(w http.ResponseWriter, r *http.Request) ([]*multipart.FileHeader, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, h.fileMaxBytes*materialMaxFilesPerRequest+(1<<20))
	if err := r.ParseMultipartForm(materialMultipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("превышен допустимый размер запроса")
		}
		return nil, http.StatusBadRequest, fmt.Errorf("некорректный multipart запрос")
	}
	var headers []*multipart.FileHeader
	if r.MultipartForm != nil {
		headers = append(headers, r.MultipartForm.File["files"]...)
		headers = append(headers, r.MultipartForm.File["file"]...)
	}
	if len(headers) > materialMaxFilesPerRequest {
		return nil, http.StatusBadRequest, fmt.Errorf("за один раз можно загрузить не более %d файлов", materialMaxFilesPerRequest)
	}
	for _, header := range headers {
		if header.Size > h.fileMaxBytes {
			return nil, http.StatusRequestEntityTooLarge, fmt.Errorf("файл %q больше допустимого размера %d МБ", header.Filename, h.fileMaxBytes>>20)
		}
	}
	return headers, http.StatusOK, nil
}

// storeFiles загружает файлы в S3. При ошибке уже загруженные объекты удаляются.
func (h *MaterialHandler) storeFiles(ctx context.Context, materialID string, headers []*multipart.FileHeader, uploaderID *uint) ([]models.MaterialFile, error) {
	files := make([]models.MaterialFile, 0, len(headers))
	for _, header := range headers {
		file, err := h.storeFile(ctx, materialID, header, uploaderID)
		if err != nil {
			h.deleteStoredFiles(files)
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func (h *MaterialHandler) storeFile(ctx context.Context, materialID string, header *multipart.FileHeader, uploaderID *uint) (models.MaterialFile, error) {
	source, err := header.Open()
	if err != nil {
		return models.MaterialFile{}, fmt.Errorf("не удалось прочитать файл %q: %w", header.Filename, err)
	}
	defer source.Close()

	name := sanitizeMaterialFileName(header.Filename)
	fileID := uuid.New().String()
	key := path.Join(materialID, fileID, name)
	contentType := resolveMaterialContentType(header, name)

	if err := h.fileStore.PutFile(ctx, key, source, header.Size, contentType, materialContentDisposition(name)); err != nil {
		return models.MaterialFile{}, fmt.Errorf("не удалось загрузить файл %q в хранилище: %w", name, err)
	}
	return models.MaterialFile{
		ID:           fileID,
		MaterialID:   materialID,
		FileName:     name,
		ContentType:  contentType,
		Size:         header.Size,
		StorageKey:   key,
		UploadedByID: uploaderID,
	}, nil
}

// deleteStoredFiles удаляет объекты S3 по принципу наилучшего усилия: ошибка не блокирует основную операцию.
func (h *MaterialHandler) deleteStoredFiles(files []models.MaterialFile) {
	if h.fileStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, file := range files {
		_ = h.fileStore.DeleteFile(ctx, file.StorageKey)
	}
}

func (h *MaterialHandler) materialFileURL(file models.MaterialFile) string {
	if h.filePublicBaseURL == "" {
		return ""
	}
	segments := strings.Split(file.StorageKey, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return h.filePublicBaseURL + "/" + strings.Join(segments, "/")
}

func (h *MaterialHandler) toMaterialFileDTOs(files []models.MaterialFile) []materialFileDTO {
	result := make([]materialFileDTO, 0, len(files))
	for _, file := range files {
		result = append(result, materialFileDTO{
			ID:          file.ID,
			Name:        file.FileName,
			ContentType: file.ContentType,
			Size:        file.Size,
			URL:         h.materialFileURL(file),
			CreatedAt:   file.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return result
}

// parseMaterialMultipartPayload читает поля материала из multipart-формы.
func parseMaterialMultipartPayload(r *http.Request) (materialPayload, error) {
	payload := materialPayload{
		Subject: r.FormValue("subject"),
		Content: r.FormValue("content"),
	}
	if rawRefs := strings.TrimSpace(r.FormValue("entity_refs")); rawRefs != "" {
		if err := json.Unmarshal([]byte(rawRefs), &payload.EntityRefs); err != nil {
			return payload, fmt.Errorf("некорректное поле entity_refs")
		}
	}
	return payload, nil
}

// sanitizeMaterialFileName оставляет только безопасное имя файла без пути и управляющих символов.
func sanitizeMaterialFileName(raw string) string {
	name := strings.ReplaceAll(raw, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	name = strings.TrimLeft(name, ".")
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if utf8.RuneCountInString(name) > materialMaxFileNameRunes {
		ext := filepath.Ext(name)
		if utf8.RuneCountInString(ext) > 16 {
			ext = ""
		}
		stem := []rune(strings.TrimSuffix(name, ext))
		limit := materialMaxFileNameRunes - utf8.RuneCountInString(ext)
		if limit < 1 {
			limit = 1
		}
		if len(stem) > limit {
			stem = stem[:limit]
		}
		name = string(stem) + ext
	}
	return name
}

func resolveMaterialContentType(header *multipart.FileHeader, name string) string {
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if parsed, _, err := mime.ParseMediaType(contentType); err == nil && parsed != "" {
		return parsed
	}
	if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); byExt != "" {
		if parsed, _, err := mime.ParseMediaType(byExt); err == nil {
			return parsed
		}
	}
	return "application/octet-stream"
}

// materialContentDisposition всегда включает скачивание, чтобы файл не исполнялся в origin приложения.
func materialContentDisposition(name string) string {
	if value := mime.FormatMediaType("attachment", map[string]string{"filename": name}); value != "" {
		return value
	}
	return "attachment"
}
