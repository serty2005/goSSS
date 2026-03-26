package services

import (
	"context"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
)

type pyrusAPIClient interface {
	IsConfigured() bool
	GetTask(ctx context.Context, taskID int64) (*pyrusplugin.Task, error)
	AddComment(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error)
	ListMembers(ctx context.Context) ([]pyrusplugin.Member, error)
	UpdateTaskExtID(ctx context.Context, taskID int64, extID string) (*pyrusplugin.Task, error)
	DownloadFile(ctx context.Context, fileID int64) (*pyrusplugin.DownloadedFile, error)
	UploadFile(ctx context.Context, fileName string, mimeType string, content []byte) (string, error)
}
