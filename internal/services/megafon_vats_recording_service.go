package services

import (
	"context"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type megafonVATSRecordingService struct {
	cfg        *config.Config
	log        logger.LoggerInterface
	repo       telephony.Repository
	store      TelephonyRecordingObjectStore
	httpClient *http.Client
	apiKey     string
}

func NewMegafonVATSRecordingService(
	cfg *config.Config,
	log logger.LoggerInterface,
	repo telephony.Repository,
	store TelephonyRecordingObjectStore,
) MegafonVATSRecordingService {
	timeout := 15 * time.Second
	apiKey := ""
	if cfg != nil {
		timeout = cfg.RequestTimeout
		apiKey = strings.TrimSpace(cfg.MegafonVATSAPIKey)
	}

	return &megafonVATSRecordingService{
		cfg:        cfg,
		log:        log,
		repo:       repo,
		store:      store,
		httpClient: &http.Client{Timeout: timeout},
		apiKey:     apiKey,
	}
}

func (s *megafonVATSRecordingService) SyncCallRecording(ctx context.Context, callID string) error {
	if !s.isEnabled() {
		return nil
	}
	call, err := s.repo.GetCallByID(ctx, callID)
	if err != nil || call == nil {
		return err
	}

	sourceURL := strings.TrimSpace(safeMegafonStringPointer(call.RecordingURL))
	if !call.HasRecording || sourceURL == "" {
		return nil
	}

	if s.isExpired(call) {
		return s.cleanupCallRecording(ctx, call.ID)
	}

	artifact, err := s.repo.GetCallArtifact(ctx, call.ID, telephony.CallArtifactTypeRecording)
	if err != nil {
		return err
	}

	if artifact != nil && strings.TrimSpace(safeMegafonStringPointer(artifact.URL)) == sourceURL {
		storageKey := strings.TrimSpace(safeMegafonStringPointer(artifact.StorageKey))
		if storageKey != "" {
			publicURL := s.publicURL(storageKey)
			call.RecordingURL = &publicURL
			call.HasRecording = true
			return s.repo.UpsertCall(ctx, call)
		}
	}

	body, contentType, err := s.downloadRecording(ctx, sourceURL)
	if err != nil {
		return err
	}

	storageKey := s.recordingStorageKey(call, sourceURL, contentType)
	if artifact != nil && strings.TrimSpace(safeMegafonStringPointer(artifact.StorageKey)) != "" {
		storageKey = strings.TrimSpace(*artifact.StorageKey)
	}

	if err = s.store.PutObject(ctx, storageKey, body, contentType); err != nil {
		return err
	}

	call.RecordingURL = stringPtr(s.publicURL(storageKey))
	call.HasRecording = true
	if err = s.repo.UpsertCall(ctx, call); err != nil {
		return err
	}

	return s.repo.UpsertCallArtifact(ctx, &telephony.CallArtifact{
		TelephonyCallID: call.ID,
		ArtifactType:    telephony.CallArtifactTypeRecording,
		URL:             stringPtr(sourceURL),
		StorageKey:      stringPtr(storageKey),
		MimeType:        stringPtr(contentType),
	})
}

func (s *megafonVATSRecordingService) CleanupExpiredRecordings(ctx context.Context) (int, error) {
	if !s.isEnabled() {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -s.retentionDays())
	total := 0
	for {
		items, err := s.repo.ListExpiredCallArtifacts(ctx, telephony.CallArtifactTypeRecording, cutoff, 100)
		if err != nil {
			return total, err
		}
		if len(items) == 0 {
			return total, nil
		}
		for i := range items {
			if err = s.cleanupArtifact(ctx, &items[i]); err != nil {
				return total, err
			}
			total++
		}
	}
}

func (s *megafonVATSRecordingService) cleanupArtifact(ctx context.Context, artifact *telephony.CallArtifact) error {
	if artifact == nil {
		return nil
	}
	storageKey := strings.TrimSpace(safeMegafonStringPointer(artifact.StorageKey))
	if storageKey != "" {
		if err := s.store.DeleteObject(ctx, storageKey); err != nil {
			return err
		}
	}
	if err := s.repo.ClearCallRecording(ctx, artifact.TelephonyCallID); err != nil {
		return err
	}
	return s.repo.DeleteCallArtifact(ctx, artifact.TelephonyCallID, artifact.ArtifactType)
}

func (s *megafonVATSRecordingService) cleanupCallRecording(ctx context.Context, callID string) error {
	artifact, err := s.repo.GetCallArtifact(ctx, callID, telephony.CallArtifactTypeRecording)
	if err != nil {
		return err
	}
	if artifact != nil {
		return s.cleanupArtifact(ctx, artifact)
	}
	return s.repo.ClearCallRecording(ctx, callID)
}

func (s *megafonVATSRecordingService) downloadRecording(ctx context.Context, sourceURL string) ([]byte, string, error) {
	body, contentType, statusCode, err := s.doDownload(ctx, sourceURL, false)
	if err == nil {
		return body, contentType, nil
	}
	if (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && s.apiKey != "" {
		body, contentType, _, err = s.doDownload(ctx, sourceURL, true)
		return body, contentType, err
	}
	return nil, "", err
}

func (s *megafonVATSRecordingService) doDownload(
	ctx context.Context,
	sourceURL string,
	withAPIKey bool,
) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, "", 0, err
	}
	if withAPIKey {
		req.Header.Set("X-API-KEY", s.apiKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("не удалось скачать запись звонка: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", resp.StatusCode, fmt.Errorf("не удалось скачать запись звонка: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("не удалось прочитать запись звонка: %w", err)
	}

	return body, normalizeRecordingContentType(resp.Header.Get("Content-Type"), sourceURL), resp.StatusCode, nil
}

func (s *megafonVATSRecordingService) recordingStorageKey(call *telephony.Call, sourceURL string, contentType string) string {
	eventTime := recordingEventTime(call).UTC()
	return path.Join(
		"megafon-vats",
		"recordings",
		eventTime.Format("2006"),
		eventTime.Format("01"),
		eventTime.Format("02"),
		call.ID+recordingExtension(sourceURL, contentType),
	)
}

func (s *megafonVATSRecordingService) publicURL(storageKey string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.MegafonVATSRecordings.PublicBaseURL), "/")
	return baseURL + "/" + strings.TrimLeft(storageKey, "/")
}

func (s *megafonVATSRecordingService) isEnabled() bool {
	return s != nil &&
		s.cfg != nil &&
		s.cfg.EnableMegafonVATS &&
		s.cfg.MegafonVATSRecordings.Enabled &&
		s.repo != nil &&
		s.store != nil &&
		strings.TrimSpace(s.cfg.MegafonVATSRecordings.PublicBaseURL) != ""
}

func (s *megafonVATSRecordingService) retentionDays() int {
	if s == nil || s.cfg == nil || s.cfg.MegafonVATSRecordings.RetentionDays <= 0 {
		return 7
	}
	return s.cfg.MegafonVATSRecordings.RetentionDays
}

func (s *megafonVATSRecordingService) isExpired(call *telephony.Call) bool {
	return recordingEventTime(call).Before(time.Now().AddDate(0, 0, -s.retentionDays()))
}

func recordingEventTime(call *telephony.Call) time.Time {
	switch {
	case call == nil:
		return time.Now()
	case call.CompletedAt != nil:
		return *call.CompletedAt
	case call.StartedAt != nil:
		return *call.StartedAt
	default:
		return call.CreatedAt
	}
}

func normalizeRecordingContentType(header string, sourceURL string) string {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if contentType != "" {
		return contentType
	}
	ext := recordingExtension(sourceURL, "")
	if ext == "" {
		return "audio/mpeg"
	}
	if detected := mime.TypeByExtension(ext); detected != "" {
		return detected
	}
	return "audio/mpeg"
}

func recordingExtension(sourceURL string, contentType string) string {
	if parsedURL, err := url.Parse(strings.TrimSpace(sourceURL)); err == nil {
		if ext := strings.ToLower(strings.TrimSpace(path.Ext(parsedURL.Path))); ext != "" {
			return ext
		}
	}
	if strings.TrimSpace(contentType) != "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			return exts[0]
		}
	}
	return ".mp3"
}
