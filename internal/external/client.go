package external

import (
	"context"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ContractState представляет собой простой статус контракта для компании.
type ContractState struct {
	IsActive bool
	// В будущем можно добавить сюда уровень сервиса, дату окончания и т.д.
	// ServiceLevel int
	// ExpiresAt    *time.Time
}

// MapperContext содержит зависимости, необходимые для маппинга.
type MapperContext struct {
	DB       *gorm.DB
	LinkRepo repositories.LinkRepo
	Logger   *zap.Logger
}

// Mapper определяет интерфейс для преобразования данных из формата внешней системы в наши внутренние модели.
// Вся логика, специфичная для конкретной внешней системы, должна быть инкапсулирована здесь.
type Mapper interface {
	// DataToCompany преобразует сырые данные в модель Company.
	DataToCompany(ctx context.Context, mc *MapperContext, data map[string]interface{}) (*models.Company, error)

	// DataToServer преобразует сырые данные в модель Server.
	DataToServer(ctx context.Context, mc *MapperContext, data map[string]interface{}) (*models.Server, error)

	// DataToWorkstation преобразует сырые данные в модель Workstation.
	DataToWorkstation(ctx context.Context, mc *MapperContext, data map[string]interface{}) (*models.Workstation, error)

	// DataToFiscalRegister преобразует сырые данные в модель FiscalRegister.
	DataToFiscalRegister(ctx context.Context, mc *MapperContext, data map[string]interface{}) (*models.FiscalRegister, error)

	// DataToContract преобразует сырые данные в модель Contract.
	DataToContract(ctx context.Context, mc *MapperContext, data map[string]interface{}) (*models.Contract, error)

	// GetCompanyUUIDsFromContract извлекает UUID компаний из данных контракта.
	GetCompanyUUIDsFromContract(data map[string]interface{}) []string
}

// ExternalSystemClient определяет абстрактный интерфейс для взаимодействия с внешней системой (например, ServiceDesk).
// Интерфейс полностью очищен от деталей реализации конкретной системы.
type ExternalSystemClient interface {

	// Mapper возвращает реализацию маппера, специфичную для данной внешней системы.
	Mapper() Mapper

	// FetchEntityList получает список всех сущностей определенного типа из внешней системы.
	FetchEntityList(ctx context.Context, entityType string) ([]map[string]interface{}, error)

	// FetchEntitySummaries получает КРАТКИЙ список сущностей с минимальным набором атрибутов
	// (обычно UUID, lastModifiedDate, owner) для быстрой сверки изменений.
	FetchEntitySummaries(ctx context.Context, entityType string) ([]map[string]interface{}, error)

	// FetchEntityDetails получает полную информацию о конкретной сущности по ее внешнему ID.
	FetchEntityDetails(ctx context.Context, externalID string, entityType string) (map[string]interface{}, error)

	// FetchCompanyContractStates получает статусы контрактов для всех компаний.
	// Возвращает map[externalCompanyID]ContractState.
	FetchCompanyContractStates(ctx context.Context) (map[string]ContractState, error)

	// UpdateEntity обновляет сущность во внешней системе.
	UpdateEntity(ctx context.Context, externalID string, entityType string, data map[string]interface{}) error

	// CreateEntity создает сущность во внешней системе.
	CreateEntity(ctx context.Context, entityType string, data map[string]interface{}) (map[string]interface{}, error)

	// FindReferenceID ищет ID в справочнике внешней системы.
	FindReferenceID(ctx context.Context, referenceType, title string, useSubstringSearch bool) (string, error)
}
