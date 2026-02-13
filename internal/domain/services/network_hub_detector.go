package services

import (
	"context"

	"etalon-server/internal/domain/server"
)

// NetworkHubDetector интерфейс для определения, является ли компания network-hub.
//
// Network-hub — это компания, которая:
//   - Владеет серверами, но не имеет собственных рабочих станций
//   - Имеет дочерние компании, которые не владеют серверами
//   - Распределяет наблюдения от агентов по дочерним компаниям
type NetworkHubDetector interface {
	// IsNetworkHub проверяет, является ли компания network-hub.
	//
	// Проверка выполняется по признакам:
	//  1. У компании нет рабочих станций
	//  2. Дочерние компании не имеют серверов
	//  3. У компании есть хотя бы один сервер
	//
	// Результат кэшируется на 5 минут для снижения нагрузки на БД.
	IsNetworkHub(ctx context.Context, companyID string) (bool, error)

	// IsNetworkHubServer проверяет, является ли сервер network-hub.
	// Сервер считается network-hub если его владелец — network-hub компания.
	IsNetworkHubServer(ctx context.Context, srv *server.Server) (bool, error)

	// ClearCache очищает кэш результатов.
	ClearCache()
}
