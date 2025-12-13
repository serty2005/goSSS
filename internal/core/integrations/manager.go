package integrations

import (
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/infra/logger"
)

// Manager управляет списком подключенных интеграций (Spokes).
type Manager struct {
	logger             logger.LoggerInterface
	inventoryProviders []integration.InventoryProvider
	contractProviders  []integration.ContractProvider
	ticketProviders    []integration.TicketProvider
}

// NewManager создает новый экземпляр менеджера интеграций.
func NewManager(logger logger.LoggerInterface) *Manager {
	return &Manager{
		logger:             logger,
		inventoryProviders: make([]integration.InventoryProvider, 0),
		contractProviders:  make([]integration.ContractProvider, 0),
		ticketProviders:    make([]integration.TicketProvider, 0),
	}
}

// RegisterInventoryProvider регистрирует провайдер инвентаризации.
func (m *Manager) RegisterInventoryProvider(p integration.InventoryProvider) {
	m.logger.Info("Регистрация InventoryProvider", "system", p.SystemName())
	m.inventoryProviders = append(m.inventoryProviders, p)
}

// RegisterContractProvider регистрирует провайдер контрактов.
func (m *Manager) RegisterContractProvider(p integration.ContractProvider) {
	m.logger.Info("Регистрация ContractProvider", "system", p.SystemName())
	m.contractProviders = append(m.contractProviders, p)
}

// RegisterTicketProvider регистрирует провайдер тикетов.
func (m *Manager) RegisterTicketProvider(p integration.TicketProvider) {
	m.logger.Info("Регистрация TicketProvider", "system", p.SystemName())
	m.ticketProviders = append(m.ticketProviders, p)
}

// GetInventoryProviders возвращает список всех зарегистрированных провайдеров инвентаризации.
func (m *Manager) GetInventoryProviders() []integration.InventoryProvider {
	return m.inventoryProviders
}

// GetContractProviders возвращает список всех зарегистрированных провайдеров контрактов.
func (m *Manager) GetContractProviders() []integration.ContractProvider {
	return m.contractProviders
}

// GetTicketProviders возвращает список всех зарегистрированных провайдеров тикетов.
func (m *Manager) GetTicketProviders() []integration.TicketProvider {
	return m.ticketProviders
}
