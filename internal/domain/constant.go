package domain

// EntityType представляет тип сущности в системе.
type EntityType string

const (
	Company        EntityType = "Company"
	Server         EntityType = "Server"
	Workstation    EntityType = "Workstation"
	FiscalRegister EntityType = "FiscalRegister"
	Contract       EntityType = "Contract"
	Agent          EntityType = "Agent"
	Ticket         EntityType = "Ticket"
)

// SystemName представляет имя внешней системы.
type SystemName string

const (
	SystemNaumen SystemName = "naumen"
)

// MetaClass представляет метакласс Naumen ServiceDesk.
type MetaClass string

const (
	MetaClassCompany     MetaClass = "ou$company"
	MetaClassServer      MetaClass = "objectBase$Server"
	MetaClassWorkstation MetaClass = "objectBase$Workstation"
	MetaClassFR          MetaClass = "objectBase$FR"
	MetaClassAgreement   MetaClass = "agreement$agreement"
	MetaClassServiceCall MetaClass = "serviceCall"
	MetaClassComment     MetaClass = "comment"
	MetaClassModeliFR    MetaClass = "ModeliFR"
	MetaClassFFD         MetaClass = "FFD"
	MetaClassSrokiFN     MetaClass = "SrokiFN"
)

// TaskType представляет тип задачи.
type TaskType string

const (
	TaskAddEquipment       TaskType = "add_equipment"
	TaskNeedUpdate         TaskType = "need_update"
	TaskDataConflict       TaskType = "data_conflict"
	TaskResolveDuplicate   TaskType = "resolve_duplicate"
	TaskAgentOwnerRequired TaskType = "agent_owner_required"
	TaskNewClient          TaskType = "new_client"
)

// TaskStatus представляет статус задачи.
type TaskStatus string

const (
	StatusNew             TaskStatus = "new"
	StatusResolved        TaskStatus = "resolved"
	StatusRejected        TaskStatus = "rejected"
	StatusPendingSDAction TaskStatus = "pending_sd_action"
	StatusSDError         TaskStatus = "sd_error"
)
