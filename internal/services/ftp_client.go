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

// FTPFileInfo содержит метаданные файла с FTP-сервера.
// Используется для передачи информации о файле без необходимости дополнительных запросов.
type FTPFileInfo struct {
	Name    string    // Имя файла
	Size    int64     // Размер файла в байтах
	ModTime time.Time // Время последней модификации
	IsDir   bool      // Является ли директорией
}

// FTPClient определяет интерфейс для работы с FTP-сервером.
type FTPClient interface {
	ListFiles(path string) ([]*ftp.Entry, error)
	DownloadFile(path string) ([]byte, error)
	GetModTime(path string) (time.Time, error)
	// IsTimePreciseInList возвращает true, если сервер поддерживает MLSD
	// и время модификации в List() возвращается с точностью до секунды.
	// Это позволяет избежать лишних запросов MDTM для каждого файла.
	IsTimePreciseInList() bool
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

// GetModTime возвращает время последней модификации файла на FTP сервере.
// Использует команду MDTM для получения времени без скачивания файла.
// Это позволяет оптимизировать синхронизацию - проверять изменения до скачивания.
func (f *ftpClientImpl) GetModTime(path string) (time.Time, error) {
	c, err := f.getConn()
	if err != nil {
		return time.Time{}, err
	}
	defer c.Quit()

	modTime, err := c.GetTime(path)
	if err != nil {
		f.logger.Error("Не удалось получить время модификации файла", "path", path, "error", err)
		return time.Time{}, err
	}

	return modTime, nil
}

// IsTimePreciseInList проверяет, поддерживает ли сервер команду MLSD.
// Если сервер поддерживает MLSD, то метод List() возвращает точное время
// модификации файлов с точностью до секунды, и нет необходимости
// делать отдельные запросы MDTM для каждого файла.
func (f *ftpClientImpl) IsTimePreciseInList() bool {
	c, err := f.getConn()
	if err != nil {
		f.logger.Warn("Не удалось подключиться к FTP для проверки MLSD", "error", err)
		return false
	}
	defer c.Quit()

	result := c.IsTimePreciseInList()
	f.logger.Debug("Проверка поддержки MLSD на FTP сервере", "supported", result)
	return result
}
