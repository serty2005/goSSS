package services

import (
	"context"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/infra/config"
	infraRepos "etalon-server/internal/infra/repositories"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeTelephonyRecordingStore struct {
	objects map[string][]byte
	deleted []string
}

func (s *fakeTelephonyRecordingStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), body...)
	return nil
}

func (s *fakeTelephonyRecordingStore) DeleteObject(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

func TestMegafonVATSRecordingService_SyncCallRecordingStoresLocalCopy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:megafon-vats-recordings-sync?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err = db.AutoMigrate(&telephony.Call{}, &telephony.CallArtifact{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	repo := infraRepos.NewTelephonyRepo(db)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("mp3-body"))
	}))
	defer server.Close()

	completedAt := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	call := &telephony.Call{
		ID:             "call-local-copy",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "external-call-local-copy",
		HasRecording:   true,
		RecordingURL:   stringPtr(server.URL + "/record.mp3"),
		CompletedAt:    &completedAt,
	}
	if err = repo.UpsertCall(t.Context(), call); err != nil {
		t.Fatalf("не удалось сохранить звонок: %v", err)
	}

	store := &fakeTelephonyRecordingStore{}
	service := NewMegafonVATSRecordingService(
		&config.Config{
			EnableMegafonVATS: true,
			MegafonVATSRecordings: config.MegafonVATSRecordingsConfig{
				Enabled:       true,
				PublicBaseURL: "https://storage.local/records",
				RetentionDays: 7,
			},
			RequestTimeout: 2 * time.Second,
		},
		nil,
		repo,
		store,
	)

	if err = service.SyncCallRecording(t.Context(), call.ID); err != nil {
		t.Fatalf("SyncCallRecording вернул ошибку: %v", err)
	}

	persistedCall, err := repo.GetCallByID(t.Context(), call.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать звонок: %v", err)
	}
	if persistedCall == nil || persistedCall.RecordingURL == nil {
		t.Fatal("ожидали локальный URL записи звонка")
	}
	expectedURL := "https://storage.local/records/megafon-vats/recordings/2026/04/05/call-local-copy.mp3"
	if *persistedCall.RecordingURL != expectedURL {
		t.Fatalf("ожидали локальный URL %q, получили %q", expectedURL, *persistedCall.RecordingURL)
	}

	artifact, err := repo.GetCallArtifact(t.Context(), call.ID, telephony.CallArtifactTypeRecording)
	if err != nil {
		t.Fatalf("не удалось получить артефакт записи: %v", err)
	}
	if artifact == nil || artifact.StorageKey == nil {
		t.Fatal("ожидали сохранённый артефакт записи")
	}
	if _, ok := store.objects[*artifact.StorageKey]; !ok {
		t.Fatalf("ожидали файл в локальном S3 по ключу %q", *artifact.StorageKey)
	}
}

func TestMegafonVATSRecordingService_CleanupExpiredRecordingsRemovesLocalCopy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:megafon-vats-recordings-cleanup?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err = db.AutoMigrate(&telephony.Call{}, &telephony.CallArtifact{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	repo := infraRepos.NewTelephonyRepo(db)
	completedAt := time.Now().AddDate(0, 0, -10).UTC()
	call := &telephony.Call{
		ID:             "call-expired-recording",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "external-call-expired-recording",
		HasRecording:   true,
		RecordingURL:   stringPtr("https://storage.local/records/megafon-vats/recordings/2026/03/26/call-expired-recording.mp3"),
		CompletedAt:    &completedAt,
	}
	if err = repo.UpsertCall(t.Context(), call); err != nil {
		t.Fatalf("не удалось сохранить звонок: %v", err)
	}
	if err = repo.UpsertCallArtifact(t.Context(), &telephony.CallArtifact{
		TelephonyCallID: call.ID,
		ArtifactType:    telephony.CallArtifactTypeRecording,
		URL:             stringPtr("https://megafon.local/record.mp3"),
		StorageKey:      stringPtr("megafon-vats/recordings/2026/03/26/call-expired-recording.mp3"),
		MimeType:        stringPtr("audio/mpeg"),
	}); err != nil {
		t.Fatalf("не удалось сохранить артефакт: %v", err)
	}

	store := &fakeTelephonyRecordingStore{
		objects: map[string][]byte{
			"megafon-vats/recordings/2026/03/26/call-expired-recording.mp3": []byte("old"),
		},
	}
	service := NewMegafonVATSRecordingService(
		&config.Config{
			EnableMegafonVATS: true,
			MegafonVATSRecordings: config.MegafonVATSRecordingsConfig{
				Enabled:       true,
				PublicBaseURL: "https://storage.local/records",
				RetentionDays: 7,
			},
		},
		nil,
		repo,
		store,
	)

	count, err := service.CleanupExpiredRecordings(t.Context())
	if err != nil {
		t.Fatalf("CleanupExpiredRecordings вернул ошибку: %v", err)
	}
	if count != 1 {
		t.Fatalf("ожидали очищение 1 записи, получили %d", count)
	}

	persistedCall, err := repo.GetCallByID(t.Context(), call.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать звонок: %v", err)
	}
	if persistedCall == nil {
		t.Fatal("ожидали существующий звонок после очистки")
	}
	if persistedCall.HasRecording {
		t.Fatal("ожидали сброс has_recording после очистки")
	}
	if persistedCall.RecordingURL != nil {
		t.Fatalf("ожидали очистку recording_url, получили %+v", persistedCall.RecordingURL)
	}

	artifact, err := repo.GetCallArtifact(t.Context(), call.ID, telephony.CallArtifactTypeRecording)
	if err != nil {
		t.Fatalf("не удалось проверить удаление артефакта: %v", err)
	}
	if artifact != nil {
		t.Fatalf("ожидали удаление артефакта, получили %+v", artifact)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "megafon-vats/recordings/2026/03/26/call-expired-recording.mp3" {
		t.Fatalf("ожидали удаление объекта из S3, получили %+v", store.deleted)
	}
}
