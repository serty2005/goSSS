package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Downloader interface {
	DownloadFile(ctx context.Context, url string) ([]byte, error)
}

type Service struct {
	dataDir    string
	downloader Downloader
}

func NewService(dataDir string, downloader Downloader) *Service {
	return &Service{dataDir: dataDir, downloader: downloader}
}

func (s *Service) Download(ctx context.Context, url, fileName string) (string, error) {
	content, err := s.downloader.DownloadFile(ctx, url)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return "", fmt.Errorf("не удалось создать каталог данных: %w", err)
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = "agent-update.bin"
	}
	target := filepath.Join(s.dataDir, fileName)
	if err := os.WriteFile(target, content, 0o755); err != nil {
		return "", fmt.Errorf("не удалось сохранить файл обновления: %w", err)
	}
	return target, nil
}

func VerifySHA256(filePath, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать файл для проверки sha256: %w", err)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("sha256 не совпадает: expected=%s actual=%s", expected, actual)
	}
	return nil
}

func ApplyAndRestart(newBinaryPath string, args []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не удалось определить путь текущего exe: %w", err)
	}
	if runtime.GOOS == "windows" {
		return applyWindows(exePath, newBinaryPath, args)
	}
	return applyPosix(exePath, newBinaryPath, args)
}

func applyPosix(exePath, newBinaryPath string, args []string) error {
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		return fmt.Errorf("не удалось заменить бинарник агента: %w", err)
	}
	cmd := exec.Command(exePath, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось перезапустить агент: %w", err)
	}
	return nil
}

func applyWindows(exePath, newBinaryPath string, args []string) error {
	scriptPath := filepath.Join(filepath.Dir(exePath), "agent-update.cmd")
	backupPath := exePath + ".bak"
	argLine := ""
	if len(args) > 0 {
		argLine = " " + strings.Join(quoteArgs(args), " ")
	}

	script := strings.Join([]string{
		"@echo off",
		"setlocal",
		"timeout /t 2 /nobreak >nul",
		fmt.Sprintf("if exist %s del /f /q %s", quoteWin(backupPath), quoteWin(backupPath)),
		fmt.Sprintf("if exist %s move /y %s %s >nul", quoteWin(exePath), quoteWin(exePath), quoteWin(backupPath)),
		fmt.Sprintf("move /y %s %s >nul", quoteWin(newBinaryPath), quoteWin(exePath)),
		fmt.Sprintf("start \"\" %s%s", quoteWin(exePath), argLine),
		"(goto) 2>nul & del \"%~f0\"",
	}, "\r\n")

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("не удалось создать скрипт обновления: %w", err)
	}
	cmd := exec.Command("cmd.exe", "/C", "start", "", scriptPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить скрипт обновления: %w", err)
	}
	log.Printf("Самообновление запланировано через %s", scriptPath)
	time.Sleep(300 * time.Millisecond)
	return nil
}

func quoteWin(v string) string {
	return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
}

func quoteArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, quoteWin(a))
	}
	return out
}
