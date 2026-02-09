// Общий конверт ответа API
export interface ApiResponse<T> {
  status: 'success' | 'error';
  data: T;
  meta?: PaginationMeta;
  error?: {
    error: string;
  };
}

export interface PaginationMeta {
  total: number;
  limit: number;
  offset: number;
  has_next: boolean;
  has_prev: boolean;
}

// --- Common Entities ---
export interface CompanyOwner {
  uuid: string;
  external_uuid: string | null;
  name: string;
  address: string;
  active_contract: boolean;
  parent_info?: { uuid: string; name: string };
}

export interface CompanyModel {
  ID?: string;
  id?: string;
  Title?: string;
  title?: string;
  Address?: string;
  address?: string;
  AdditionalName?: string;
  additional_name?: string;
  ActiveContract?: boolean;
  active_contract?: boolean;
  ParentID?: string;
  parent_id?: string;
  ParentTitle?: string;
  parent_title?: string;
  LastModifiedDate?: string;
  last_modified_date?: string;
}

export type AntBadgeStatus = 'success' | 'processing' | 'error' | 'default' | 'warning';

// --- CMDB Entities (Rich DTOs) ---

export interface ServerEntity {
  uuid: string;
  external_uuid?: string;
  unique_id?: string;
  
  device_name?: string;
  server_name?: string;
  ip?: string;
  
  rdp?: string;
  anydesk?: string;
  teamviewer?: string;
  litemanager?: string;
  partners_link?: string;
  
  operational_status?: 'active' | 'offline' | 'unknown';
  health_status: 'ok' | 'attention_required' | 'locked';
  status_details?: unknown;
  last_polled_at?: string;
  
  server_version?: string;
  server_edition?: string;
  
  crm_id?: string;
  
  address?: string;
  description?: string;

  [key: string]: unknown;
}

export interface WorkstationEntity {
  uuid: string;
  external_uuid?: string;
  device_name?: string;
  
  anydesk?: string;
  teamviewer?: string;
  litemanager?: string;
  
  health_status: 'ok' | 'attention_required' | 'locked';
  status_details?: unknown;

  description?: string;
  address?: string;

  [key: string]: unknown;
}

export interface FiscalEntity {
  uuid: string;
  external_uuid?: string;
  
  model_kkt?: string;
  serial_number?: string;
  rn_kkt?: string;
  
  fn_number?: string;
  fn_registration_date?: string;
  fn_expire_date?: string;
  
  driver_version?: string;
  fr_firmware?: string;
  fr_downloader?: string;
  
  organization_name?: string;
  inn?: string;
  
  health_status: 'ok' | 'attention_required' | 'locked';
  status_details?: unknown;

  address?: string;
  description?: string;

  [key: string]: unknown;
}

export type EntityData = ServerEntity | WorkstationEntity | FiscalEntity;

export interface InfrastructureItem {
  entity_type: 'Server' | 'Workstation' | 'FiscalRegister';
  data: EntityData;
}

// --- Search DTO ---
export interface SearchFoundEntity {
  entity_type: 'Server' | 'Workstation' | 'FiscalRegister';
  data: EntityData;
}

export interface SearchResultGroup {
  owner: CompanyOwner;
  found_entities: SearchFoundEntity[];
}

export interface SearchResponseData {
  search_results: SearchResultGroup[];
}

// --- Tasks DTO ---
export type TaskStatus = 'new' | 'resolved' | 'rejected' | 'pending_sd_action' | 'sd_error';
export type TaskType = 'add_equipment' | 'conflict' | 'offline_alert';

export interface TaskDTO {
  id: number;
  task_type: TaskType;
  entity_type: string;
  status: TaskStatus;
  created_at: string;
  details: Record<string, unknown>; 
}

export interface TaskResolutionPayload {
  status: 'resolved' | 'rejected';
  comment?: string;
  resolution_payload?: {
    action: string;
    new_owner_id?: string;
    [key: string]: unknown;
  };
}

// --- Tickets DTO ---
export type TicketStatus = 'new' | 'in_progress' | 'pending' | 'resolved' | 'closed';

export interface TicketListItemDTO {
  id: string;
  number: number;
  subject: string;
  company_name?: string;
  description?: string;
  result?: string;
  status: TicketStatus;
  last_comment?: string;
  last_comment_author?: string;
  last_activity: string;
  created_at?: string;
  company_id: string;
  contract_id?: string;
  is_common_contract?: boolean;
  assignee?: {
    id: number;
    fullName: string;
  };
}

// --- Tickets DTO ---
export interface TicketDTO {
  id: string;
  number: number;
  subject: string;
  description?: string;
  result?: string;
  company_name?: string;
  status: TicketStatus;
  priority?: string;
  created_at: string;
  updated_at: string;
  assignee?: {
    id: number;
    fullName: string;
  };
  company_id: string;
  contract_id?: string;
  is_common_contract?: boolean;
}

export interface TicketCreatePayload {
  subject: string;
  description: string;
  type: string;
  company_id: string;
  priority?: string;
}

export interface TicketHistoryDTO {
  id: number;
  ticket_id: string;
  user_id?: number;
  action: string;
  field: string;
  old_value: string;
  new_value: string;
  created_at: string;
}

export interface TicketCommentDTO {
  uuid: string;
  text: string;
  author_name: string;
  creation_date: string;
  is_internal: boolean;
}

export interface TicketDetailsDTO {
  metadata: TicketDTO;
  company_name?: string;
  comments: TicketCommentDTO[];
  history?: TicketHistoryDTO[];
  attachments?: unknown[];
}

export interface TicketListParams {
  company_id?: string;
  limit?: number;
  offset?: number;
  status?: string | string[];
  search?: string;
}

export interface TicketCompanyFilterItem {
  id: string;
  name: string;
  parent_name?: string;
  count: number;
}

export interface TicketFiltersResponse {
  companies: TicketCompanyFilterItem[];
}

// DTO для обновления оборудования
export interface UpdateServerDTO {
  device_name?: string;
  ip?: string;
  anydesk?: string;
  teamviewer?: string;
  description?: string;
  // Добавляем другие поля по мере необходимости
}

export interface UpdateWorkstationDTO {
  device_name?: string;
  anydesk?: string;
  teamviewer?: string;
  description?: string;
}

export interface UpdateFiscalDTO {
  description?: string;
  // ККТ поля обычно read-only, т.к. приходят из железа, но description можно править
}

// --- Detail DTOs (Strictly matching Backend JSON) ---

export interface LicensesDict {
  [key: string]: {
    name: string;
    dateFrom: string;
    dateUntil: string;
  };
}

export interface FiscalDetailDTO {
  ID: string;
  ModelKKT?: string;
  RNKKT?: string;
  LegalName?: string;
  INN?: string;
  FRSerialNumber?: string;
  FNNumber?: string;
  
  // Внимание: смешанный регистр в JSON
  kkt_reg_date?: string;
  fn_expire_date?: string;
  
  FRFirmware?: string;
  FRDownloader?: string;
  DriverVersion?: string;
  
  HealthStatus?: 'ok' | 'attention_required' | 'locked';
  
  // Внимание: lowercase в JSON
  address?: string;
  Description?: string;
  
  Licenses?: LicensesDict;
}

export interface WorkstationDetailDTO {
  ID: string;
  DeviceName?: string;
  Teamviewer?: string;
  Anydesk?: string;
  Litemanager?: string;
  Description?: string;
  HealthStatus?: 'ok' | 'attention_required' | 'locked';
}

export interface ServerDetailDTO {
  ID: string;
  UniqueID?: string;
  IP?: string;
  DeviceName?: string;
  ServerName?: string;
  ServerVersion?: string;
  ServerEdition?: string;
  
  // PascalCase
  LastPolledAt?: string;
  Status?: 'active' | 'offline' | 'unknown'; // Operational Status
  HealthStatus?: 'ok' | 'attention_required' | 'locked';
  
  CabinetLink?: string;
  CRMid?: string;
  
  RDP?: string;
  Teamviewer?: string;
  Anydesk?: string;
  Litemanager?: string;
  Description?: string;
}

// ... (предыдущие TaskDTO и прочее остаются)
export interface ApiResponse<T> {
  status: 'success' | 'error';
  data: T;
  meta?: PaginationMeta;
  error?: {
    error: string;
  };
}
// Необходимо убедиться, что PaginationMeta и другие типы на месте
export interface PaginationMeta {
  total: number;
  limit: number;
  offset: number;
  has_next: boolean;
  has_prev: boolean;
}

export interface UpdateServerPayload {
  device_name?: string;
  ip?: string;
  anydesk?: string;
  teamviewer?: string;
  description?: string;
}

export interface UpdateWorkstationPayload {
  device_name?: string;
  anydesk?: string;
  teamviewer?: string;
  description?: string;
}

export interface UpdateFiscalPayload {
  description?: string;
}
