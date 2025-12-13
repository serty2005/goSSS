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

// Тип для статуса бейджа Ant Design
export type AntBadgeStatus = 'success' | 'processing' | 'error' | 'default' | 'warning';

export interface EntityData {
  uuid: string;
  device_name?: string;
  ip?: string; // Для серверов
  rn_kkt?: string; // Для ФР
  serial_number?: string;
  operational_status?: 'active' | 'offline' | 'unknown';
  health_status?: 'ok' | 'attention_required' | 'locked';
  // Используем unknown вместо any для безопасной работы с динамическими полями
  [key: string]: unknown; 
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
  // Детали могут быть любой структуры, но мы знаем, что это объект
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