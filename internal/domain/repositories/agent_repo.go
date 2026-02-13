// Package repositories определяет интерфейсы для доступа к данным.
// Интерфейсы реализуются в слое инфраструктуры (internal/infra/repositories).
// Следует принципу Dependency Inversion: бизнес-логика зависит от абстракций, а не от конкретных реализаций.
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

// AgentRepo определяет интерфейс для работы с хранилищем агентов.
// Реализации: AgentRepository в internal/infra/repositories.
//
// Используется:
//   - Orchestrator — для создания и обновления агентов при обработке данных
//   - Engine — для привязки агентов к владельцам
//   - HTTP-хендлеры — для получения информации об агентах
type AgentRepo interface {
	// GetByUUID возвращает агента по его уникальному идентификатору.
	// Возвращает nil, если агент не найден (ошибки нет, просто nil).
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - uuid: уникальный идентификатор агента
	//
	// Возвращает:
	//   - *models.Agent: найденный агент или nil
	//   - error: ошибка БД (не "не найдено")
	GetByUUID(ctx context.Context, uuid string) (*models.Agent, error)

	// Create создаёт новую запись агента в БД.
	// UUID должен быть предварительно сгенерирован.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - agent: модель агента для сохранения (UUID обязателен)
	//
	// Возвращает:
	//   - error: ошибка при создании (например, дублирование UUID)
	Create(ctx context.Context, agent *models.Agent) error

	// Update обновляет существующую запись агента.
	// Обновляются все поля, включая UpdatedAt.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - agent: модель агента с обновлёнными данными
	//
	// Возвращает:
	//   - error: ошибка при обновлении (например, агент не найден)
	Update(ctx context.Context, agent *models.Agent) error

	// CountByOwnerUUID возвращает количество агентов, привязанных к компании.
	// Используется для статистики и валидации перед удалением компании.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - ownerUUID: UUID компании-владельца
	//
	// Возвращает:
	//   - int64: количество агентов
	//   - error: ошибка БД
	CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error)

	// GetPendingCommands возвращает список команд, ожидающих выполнения агентом.
	// Команды со статусом "new" выбираются в порядке создания.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - agentUUID: UUID агента-получателя
	//
	// Возвращает:
	//   - []models.AgentCommand: список команд (может быть пустым)
	//   - error: ошибка БД
	GetPendingCommands(ctx context.Context, agentUUID string) ([]models.AgentCommand, error)

	// MarkCommandsAsSent помечает команды как отправленные агенту.
	// Меняет статус с "new" на "sent" и устанавливает SentAt.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - commandIDs: список ID команд для обновления
	//
	// Возвращает:
	//   - error: ошибка БД
	MarkCommandsAsSent(ctx context.Context, commandIDs []uint) error
}
