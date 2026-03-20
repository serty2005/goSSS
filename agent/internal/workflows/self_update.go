package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
	"etalon-agent/internal/updater"
)

type SelfUpdater interface {
	Download(ctx context.Context, url, fileName string) (string, error)
}

type SelfUpdateWorkflow struct {
	sagaRunner     *saga.Runner
	updater        SelfUpdater
	currentVersion string
}

func NewSelfUpdateWorkflow(currentVersion string, u SelfUpdater) *SelfUpdateWorkflow {
	return &SelfUpdateWorkflow{
		sagaRunner:     saga.NewRunner(),
		updater:        u,
		currentVersion: currentVersion,
	}
}

func (w *SelfUpdateWorkflow) Type() string {
	return "self_update"
}

func (w *SelfUpdateWorkflow) Run(ctx context.Context, payload []byte) error {
	var req protocol.SelfUpdateTaskPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("невалидный payload self_update: %w", err)
	}
	if strings.TrimSpace(req.DownloadURL) == "" {
		return errors.New("в payload self_update отсутствует download_url")
	}
	if strings.TrimSpace(req.Version) != "" && strings.TrimSpace(req.Version) == strings.TrimSpace(w.currentVersion) {
		log.Printf("Self-update пропущен: версия %s уже запущена", req.Version)
		return nil
	}

	fileName := strings.TrimSpace(req.FileName)
	if fileName == "" {
		fileName = "agent-update.bin"
		if req.Version != "" {
			fileName = "agent-update-" + req.Version + ".bin"
		}
	}

	var downloadedPath string
	steps := []saga.Step{
		{
			Name: "download",
			Do: func(ctx context.Context) error {
				path, err := w.updater.Download(ctx, req.DownloadURL, filepath.Base(fileName))
				if err != nil {
					return err
				}
				downloadedPath = path
				return nil
			},
			Compensate: func(context.Context) {
				if downloadedPath != "" {
					_ = os.Remove(downloadedPath)
				}
			},
		},
		{
			Name: "verify_sha256",
			Do: func(context.Context) error {
				return updater.VerifySHA256(downloadedPath, req.SHA256)
			},
		},
		{
			Name: "apply_and_restart",
			Do: func(context.Context) error {
				return updater.ApplyAndRestart(downloadedPath, req.Args)
			},
		},
	}

	return w.sagaRunner.Run(ctx, "self_update", steps)
}
