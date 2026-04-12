package adapterstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/s3store"
	"etalon-server/internal/services"
)

type agentAdapterObjectStore struct {
	store *s3store.BucketStore
}

type telephonyRecordingObjectStore struct {
	store *s3store.BucketStore
}

func NewAgentAdapterObjectStore(ctx context.Context, cfg *config.Config) (services.AgentAdapterObjectStore, error) {
	if cfg == nil || !cfg.AgentAdapterCatalog.Enabled {
		return nil, nil
	}
	service, err := s3store.New(ctx, cfg.S3)
	if err != nil {
		return nil, err
	}

	store, err := service.Bucket(ctx, strings.TrimSpace(cfg.AgentAdapterCatalog.Bucket), false)
	if err != nil {
		return nil, err
	}
	return &agentAdapterObjectStore{store: store}, nil
}

func NewTelephonyRecordingObjectStore(ctx context.Context, cfg *config.Config) (services.TelephonyRecordingObjectStore, error) {
	if cfg == nil || !cfg.MegafonVATSRecordings.Enabled {
		return nil, nil
	}
	service, err := s3store.New(ctx, cfg.S3)
	if err != nil {
		return nil, err
	}

	store, err := service.Bucket(ctx, strings.TrimSpace(cfg.MegafonVATSRecordings.Bucket), true)
	if err != nil {
		return nil, err
	}
	return &telephonyRecordingObjectStore{store: store}, nil
}

func (s *agentAdapterObjectStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	body, err := s.store.GetObject(ctx, key)
	if err != nil {
		return nil, mapAgentAdapterStoreError(err)
	}
	return body, nil
}

func (s *agentAdapterObjectStore) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	return s.store.PutObject(ctx, key, body, contentType)
}

func (s *agentAdapterObjectStore) PutFile(ctx context.Context, key string, filePath string, contentType string) error {
	return s.store.PutFile(ctx, key, filePath, contentType)
}

func (s *agentAdapterObjectStore) StatObject(ctx context.Context, key string) (services.AgentAdapterObjectInfo, error) {
	info, err := s.store.StatObject(ctx, key)
	if err != nil {
		return services.AgentAdapterObjectInfo{}, mapAgentAdapterStoreError(err)
	}

	return services.AgentAdapterObjectInfo{
		Size:         info.Size,
		LastModified: info.LastModified,
	}, nil
}

func (s *telephonyRecordingObjectStore) PutObject(ctx context.Context, key string, body []byte, contentType string) error {
	return s.store.PutObject(ctx, key, body, contentType)
}

func (s *telephonyRecordingObjectStore) DeleteObject(ctx context.Context, key string) error {
	return s.store.DeleteObject(ctx, key)
}

func mapAgentAdapterStoreError(err error) error {
	if errors.Is(err, s3store.ErrObjectNotFound) {
		return fmt.Errorf("%w: %s", services.ErrAgentAdapterObjectNotFound, err)
	}
	return err
}
