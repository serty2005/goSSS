package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

// CandidateRepo определяет интерфейс для работы с кандидатами на принятие в систему.
// Кандидаты создаются при обнаружении новых агентов без привязки к владельцу.
// Реализации: CandidateRepository в internal/infra/repositories.
//
// Жизненный цикл кандидата:
//  1. Агент отправляет данные -> создаётся Candidate со статусом "new"
//  2. Оператор просматривает staging-данные (рабочие станции, ФР)
//  3. Оператор подтверждает кандидата -> создаётся оборудование и привязывается к компании
//
// Используется:
//   - HTTP-хендлеры AcceptancePage — для отображения списка кандидатов
//   - Engine — для создания оборудования при подтверждении
type CandidateRepo interface {
	// List возвращает список кандидатов с фильтрацией по статусу.
	// Используется для отображения в UI (AcceptancePage).
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - status: фильтр по статусу ("new", "accepted", "rejected") или пусто для всех
	//   - limit: максимальное количество записей (пагинация)
	//   - offset: смещение для пагинации
	//
	// Возвращает:
	//   - []models.Candidate: список кандидатов (может быть пустым)
	//   - error: ошибка БД
	List(ctx context.Context, status string, limit, offset int) ([]models.Candidate, error)

	// GetByID возвращает кандидата по его ID.
	// Используется для детального просмотра перед подтверждением.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - id: первичный ключ кандидата
	//
	// Возвращает:
	//   - *models.Candidate: найденный кандидат или nil
	//   - error: ошибка БД (не "не найдено")
	GetByID(ctx context.Context, id uint) (*models.Candidate, error)

	// ListWorkstationStaging возвращает staging-данные рабочих станций кандидата.
	// Staging содержит данные из наблюдений агента, готовые к созданию оборудования.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - candidateID: ID кандидата
	//
	// Возвращает:
	//   - []models.CandidateWorkstationStaging: список staging-записей
	//   - error: ошибка БД
	ListWorkstationStaging(ctx context.Context, candidateID uint) ([]models.CandidateWorkstationStaging, error)

	// ListFiscalStaging возвращает staging-данные фискальных регистраторов кандидата.
	// Staging содержит данные из наблюдений агента, готовые к созданию ФР.
	//
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - candidateID: ID кандидата
	//
	// Возвращает:
	//   - []models.CandidateFiscalStaging: список staging-записей
	//   - error: ошибка БД
	ListFiscalStaging(ctx context.Context, candidateID uint) ([]models.CandidateFiscalStaging, error)
}
