package services

import "context"

type TelephonyRecordingObjectStore interface {
	PutObject(ctx context.Context, key string, body []byte, contentType string) error
	DeleteObject(ctx context.Context, key string) error
}

type MegafonVATSRecordingService interface {
	SyncCallRecording(ctx context.Context, callID string) error
	CleanupExpiredRecordings(ctx context.Context) (int, error)
}
