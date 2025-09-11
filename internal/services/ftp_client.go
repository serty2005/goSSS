package services

import (
	"bytes"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"time"

	"github.com/jlaffaye/ftp"
)

// FTPClient определяет интерфейс для работы с FTP-сервером.
type FTPClient interface {
	ListFiles(path string) ([]*ftp.Entry, error)
	DownloadFile(path string) ([]byte, error)
}

type ftpClientImpl struct {
	cfg    *config.Config
	logger logger.LoggerInterface
}

// NewFTPClient создает новый клиент для FTP.
func NewFTPClient(cfg *config.Config, logger logger.LoggerInterface) FTPClient {
	return &ftpClientImpl{
		cfg:    cfg,
		logger: logger,
	}
}

// getConn устанавливает соединение с FTP сервером.
func (f *ftpClientImpl) getConn() (*ftp.ServerConn, error) {
	addr := fmt.Sprintf("%s:%s", f.cfg.FTPHost, f.cfg.FTPPort)
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(15*time.Second))
	if err != nil {
		f.logger.Error("Не удалось подключиться к FTP", "addr", addr, "error", err)
		return nil, err
	}

	err = c.Login(f.cfg.FTPUser, f.cfg.FTPPassword)
	if err != nil {
		c.Quit()
		f.logger.Error("Не удалось авторизоваться на FTP", "user", f.cfg.FTPUser, "error", err)
		return nil, err
	}

	return c, nil
}

// ListFiles получает список файлов и их атрибутов из указанной директории.
func (f *ftpClientImpl) ListFiles(path string) ([]*ftp.Entry, error) {
	c, err := f.getConn()
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	entries, err := c.List(path)
	if err != nil {
		f.logger.Error("Не удалось получить список файлов с FTP", "path", path, "error", err)
		return nil, err
	}

	return entries, nil
}

// DownloadFile скачивает файл с FTP и возвращает его содержимое в виде []byte.
func (f *ftpClientImpl) DownloadFile(path string) ([]byte, error) {
	c, err := f.getConn()
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	r, err := c.Retr(path)
	if err != nil {
		f.logger.Error("Не удалось начать скачивание файла", "path", path, "error", err)
		return nil, err
	}
	defer r.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		f.logger.Error("Ошибка во время чтения скачиваемого файла", "path", path, "error", err)
		return nil, err
	}

	// Для отладки можно использовать bytes.NewReader(buf) если нужно будет передать io.Reader
	_ = bytes.NewReader(buf)

	return buf, nil
}
