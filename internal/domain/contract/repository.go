package contract

import (
	"context"
)

// Repository определяет интерфейс для работы с хранилищем контрактов.
type Repository interface {
	Create(ctx context.Context, contract *Contract) error
	Update(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, internalID string) (bool, error)

	GetByID(ctx context.Context, internalID string) (*Contract, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*Contract, error)
	GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*Contract, error)
	ListByLastUpdatedBy(ctx context.Context, lastUpdatedBy string) ([]Contract, error)
	Restore(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error)

	// ReplaceCompanyLinks обновляет связи Many-to-Many для контракта.
	// companies: список моделей компаний, которые должны быть привязаны к контракту.
	ReplaceCompanyLinks(ctx context.Context, contract *Contract, companies []string) error

	// GetActiveContractIDsForCompany возвращает ID активных контрактов компании.
	GetActiveContractIDsForCompany(ctx context.Context, companyID string) ([]string, error)

	GetMailImportByAttachmentHash(ctx context.Context, attachmentHash string) (*MailImport, error)
	ListMailImports(ctx context.Context, limit int) ([]MailImport, error)
	UpsertMailImport(ctx context.Context, item *MailImport) error
	CreateServicePointSyncRun(ctx context.Context, item *ServicePointSyncRun) error
	ListServicePointSyncRuns(ctx context.Context, limit int) ([]ServicePointSyncRun, error)
	GetServicePointSyncRunByID(ctx context.Context, id string) (*ServicePointSyncRun, error)
	ListServicePointSyncConflicts(ctx context.Context) ([]ServicePointSyncConflict, error)
	ReplaceServicePointSyncConflicts(ctx context.Context, conflicts []ServicePointSyncConflict) error
}
