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
  ID: string;
  Title?: string;
  Address?: string;
  AdditionalName?: string;
  ActiveContract?: boolean;
  ParentID?: string;
  LastModifiedDate?: string;
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
  
  // Добавлено поле address
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

  // Добавлено поле description
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

  // Добавлено поле address
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