package events

import "etalon-server/pkg/eventbus"

// Этот файл регистрирует соответствие типов событий их payload-типам в
// реестре eventbus. Без регистрации NATS-шина не сможет восстановить
// конкретный тип payload при десериализации, и type-assertion у подписчиков
// (напр. event.Payload.(events.AgentObservationPayload)) провалится.
//
// Регистрация выполняется в init(), поэтому достаточно импортировать пакет
// events (что уже делается везде, где публикуются/читаются события).

func init() {
	eventbus.RegisterPayloadType(ContractsStatusRecalculated, ContractsStatusPayload{})
	eventbus.RegisterPayloadType(ServiceDeskEntityUpdated, ServiceDeskEntityPayload{})
	eventbus.RegisterPayloadType(ServiceDeskEntityDeleted, ServiceDeskEntityDeletePayload{})
	eventbus.RegisterPayloadType(AgentDataReceived, AgentDataPayload{})
	eventbus.RegisterPayloadType(AgentObservationRequested, AgentObservationPayload{})
	eventbus.RegisterPayloadType(DuplicatesFound, DuplicatesFoundPayload{})
	eventbus.RegisterPayloadType(ServerPollingSucceeded, ServerPollingSucceededPayload{})
	eventbus.RegisterPayloadType(ServerPollingFailed, ServerPollingFailedPayload{})
	eventbus.RegisterPayloadType(ServerPollingRequested, ServerPollingRequestedPayload{})
	eventbus.RegisterPayloadType(ServiceDeskCreateRequested, ServiceDeskModificationPayload{})
	eventbus.RegisterPayloadType(ServiceDeskUpdateRequested, ServiceDeskModificationPayload{})
	eventbus.RegisterPayloadType(FiscalRegisterDiscrepancyFound, FiscalRegisterDiscrepancyPayload{})
	eventbus.RegisterPayloadType(TicketUpdated, TicketUpdatedPayload{})
	eventbus.RegisterPayloadType(TelephonyLineUpdated, TelephonyLineUpdatedPayload{})
	eventbus.RegisterPayloadType(AgentObservationUpdated, AgentObservationUpdatedPayload{})
	eventbus.RegisterPayloadType(BitrixTicketSyncRequested, BitrixSyncEntityPayload{})
	eventbus.RegisterPayloadType(BitrixCommentSyncRequested, BitrixSyncEntityPayload{})
	eventbus.RegisterPayloadType(PyrusTicketSyncRequested, PyrusSyncEntityPayload{})
	eventbus.RegisterPayloadType(PyrusCommentSyncRequested, PyrusSyncEntityPayload{})
	eventbus.RegisterPayloadType(PyrusTicketStatusSyncRequested, PyrusSyncEntityPayload{})
	eventbus.RegisterPayloadType(PyrusTicketAssigneeSyncRequested, PyrusSyncEntityPayload{})
	eventbus.RegisterPayloadType(PyrusTicketExtIDSyncRequested, PyrusSyncEntityPayload{})
}
