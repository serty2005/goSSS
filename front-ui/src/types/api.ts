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

  id?: string;

  title?: string;

  address?: string;

  additional_name?: string;

  active_contract?: boolean;

  parent_id?: string;

  parent_title?: string;

  contract_id?: string;

  contract_type?: string;

  last_modified_date?: string;
}

export interface CompanyBitrixMappingRowDTO {
  company_id: string;
  company_title: string;
  company_parent_title?: string;
  company_additional_name?: string;
  company_address?: string;
  bitrix_service_point_id?: number;
  bitrix_service_point_name?: string;
  bitrix_service_point_code?: string;
  bitrix_service_point_enabled?: boolean;
}

export type AntBadgeStatus = 'success' | 'processing' | 'error' | 'default' | 'warning';

// --- CMDB Entities (Rich DTOs) ---

export interface ServerEntity {
  uuid: string;
  external_uuid?: string;
  unique_id?: string;
  last_updated_by?: string;
  last_modified_date?: string;
  updated_at?: string;
  
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
  is_new?: boolean;
  last_updated_by?: string;
  last_modified_date?: string;
  updated_at?: string;
  
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
  last_updated_by?: string;
  last_modified_date?: string;
  updated_at?: string;
  
  model_kkt?: string;
  serial_number?: string;
  rn_kkt?: string;
  
  fn_number?: string;
  fn_registration_date?: string;
  fn_expire_date?: string;
  fn_execution?: string;
  fnExecution?: string;
  
  driver_version?: string;
  fr_firmware?: string;
  fr_downloader?: string;
  
  organization_name?: string;
  legal_name?: string;
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
export type TicketStatus =
  | 'new'
  | 'in_progress'
  | 'pending'
  | 'deferred'
  | 'onsite'
  | 'to_manager'
  | 'resolved'
  | 'spam'
  | 'execution'
  | 'closed';

export interface TicketListItemDTO {
  id: string;
  number: number;
  subject: string;
  company_name?: string;
  description?: string;
  reporter_name?: string;
  created_source?: 'ui' | 'bitrix' | 'servicedesk' | 'system' | string;
  result?: string;
  status: TicketStatus;
  last_comment?: string;
  last_comment_author?: string;
  last_comment_is_private?: boolean;
  last_activity: string;
  created_at?: string;
  company_id: string;
  contract_id?: string;
  is_common_contract?: boolean;
  sync_with_bitrix?: boolean;
  is_archived?: boolean;
  archived_at?: string;
  bitrix_service_point_id?: number;
  bitrix_deal_title?: string;
  bitrix_deal_id?: number;
  bitrix_deal_url?: string;
  assignee?: {
    id: number;
    full_name: string;
  };
}

// --- Tickets DTO ---
export interface TicketDTO {
  id: string;
  number: number;
  subject: string;
  description?: string;
  reporter_id?: number;
  reporter_name?: string;
  service_desk_uuid?: string;
  result?: string;
  company_name?: string;
  status: TicketStatus;
  priority?: string;
  created_at: string;
  updated_at: string;
  assignee?: {
    id: number;
    full_name: string;
  };
  company_id: string;
  contract_id?: string;
  is_common_contract?: boolean;
  sync_with_bitrix?: boolean;
  is_archived?: boolean;
  archived_at?: string;
  bitrix_service_point_id?: number;
  bitrix_deal_title?: string;
  bitrix_deal_id?: number;
  bitrix_deal_url?: string;
}

export interface TicketCreatePayload {
  subject: string;
  description: string;
  type: string;
  company_id: string;
  assignee_id: number;
  priority?: string;
  sync_with_bitrix?: boolean;
  bitrix_service_point_id?: number;
  bitrix_deal_title?: string;
}

export interface BitrixServicePointDTO {
  b24_element_id: number;
  name: string;
  address?: string;
  one_c_code?: string;
  contract_on?: boolean | null;
  raw_json?: string;
}

export interface ServicePointImportColumnDTO {
  key: string;
  name: string;
}

export interface ServicePointImportPreviewDTO {
  header_row: number;
  columns: ServicePointImportColumnDTO[];
  sample_rows: Record<string, string>[];
  total_rows: number;
}

export interface ServicePointImportResultDTO {
  processed_rows: number;
  updated: number;
  unchanged: number;
  skipped: number;
  not_found: number;
  ambiguous: number;
  not_found_names?: string[];
  ambiguous_names?: string[];
}

export type ServicePointSyncAction = 'create' | 'update' | 'unchanged' | 'skipped' | 'ambiguous';

export interface ServicePointSyncPlanItemDTO {
  row: number;
  name: string;
  one_c_code: string;
  contract_label?: string;
  action: ServicePointSyncAction;
  reason?: string;
  b24_element_id?: number;
  current_code?: string;
  current_contract?: string;
}

export interface ServicePointSyncPreviewDTO {
  processed_rows: number;
  to_create: number;
  to_update: number;
  unchanged: number;
  skipped: number;
  ambiguous: number;
  items: ServicePointSyncPlanItemDTO[];
}

export interface ServicePointSyncApplyResultDTO {
  processed_rows: number;
  created: number;
  updated: number;
  unchanged: number;
  skipped: number;
  ambiguous: number;
  applied_rows?: number[];
  errors?: string[];
}

export interface TicketHistoryDTO {
  id: number;
  ticket_id: string;
  user_id?: number;
  action: string;
  field: string;
  source?: 'ui' | 'bitrix' | 'servicedesk' | 'system' | string;
  old_value: string;
  new_value: string;
  meta?: Record<string, unknown>;
  created_at: string;
}

export interface TicketCommentDTO {
  uuid: string;
  text: string;
  author_name: string;
  creation_date: string;
  is_internal: boolean;
  is_private?: boolean;
}

export interface TicketDetailsDTO {
  metadata: TicketDTO;
  company_name?: string;
  comments: TicketCommentDTO[];
  history?: TicketHistoryDTO[];
  attachments?: unknown[];
}

export interface TicketAttachmentDTO {
  id: string;
  file_name: string;
  file_path: string;
  mime_type: string;
  size?: number;
}

export interface DashboardResolvedByAssigneeDTO {
  user_id: number;
  user_name: string;
  count: number;
}

export interface DashboardStatsDTO {
  resolved_by_assignee: DashboardResolvedByAssigneeDTO[];
  total_tickets: number;
  polled_servers_24h: number;
}

export interface TicketListParams {
  company_id?: string;
  limit?: number;
  offset?: number;
  status?: string | string[];
  search?: string;
  archive_mode?: 'active' | 'archive' | 'all';
  period_from?: string;
  period_to?: string;
  assignee_ids?: string | string[];
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

export type UserPosition = 'admin' | 'support_specialist' | 'intern';
export type UserSchedule = '2/2' | '3/3' | '5/2';

export interface UserAdminDTO {
  id: number;
  username: string;
  full_name: string;
  first_name: string;
  last_name: string;
  position: UserPosition;
  roles: string[];
  external_system_id?: string;
  external_type?: string;
  schedule_type: UserSchedule;
  is_active: boolean;
  has_logged_in: boolean;
  integrations?: UserIntegrationDTO[];
  bitrix_suggestion?: BitrixUserSuggestionDTO | null;
}

export interface UserIntegrationDTO {
  id: number;
  integration_type: string;
  external_id: string;
  is_verified: boolean;
  is_locked?: boolean;
  verified_name?: string;
}

export interface BitrixUserSuggestionDTO {
  b24_user_id: number;
  name: string;
}

export interface ThemePaletteConfigDTO {
  primary?: string;
  bg_layout?: string;
  bg_container?: string;
  border_color?: string;
}

export interface UserInterfaceConfigDTO {
  theme_mode?: 'light' | 'dark';
  theme_palettes?: {
    light?: ThemePaletteConfigDTO;
    dark?: ThemePaletteConfigDTO;
  };
  search?: {
    cards_columns?: number;
  };
}

export interface UserProfileConfigDTO {
  interface?: UserInterfaceConfigDTO;
  [key: string]: unknown;
}

export interface UserCreatePayload {
  username: string;
  password: string;
  first_name: string;
  last_name: string;
  position: UserPosition;
  external_system_id?: string;
  external_type?: string;
  schedule_type: UserSchedule;
}

export interface UserUpdatePayload {
  username?: string;
  password?: string;
  first_name?: string;
  last_name?: string;
  position?: UserPosition;
  external_system_id?: string;
  external_type?: string;
  schedule_type?: UserSchedule;
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
    date_from: string;
    date_until: string;
  };
}

export interface FiscalDetailDTO {
  id: string;
  created_at?: string;
  deleted_at?: string;
  updated_at?: string;
  last_updated_by?: string;
  last_modified_date?: string;
  model_kkt?: string;
  rn_kkt?: string;
  legal_name?: string;
  inn?: string;
  fr_serial_number?: string;
  fn_number?: string;
  fn_execution?: string;
  
  // Внимание: смешанный регистр в JSON
  kkt_reg_date?: string;
  fn_expire_date?: string;
  
  fr_firmware?: string;
  fr_downloader?: string;
  driver_version?: string;
  ffd?: string;
  fr_serial_normalized?: string;
  workstation_id?: string;
  health_status_before_lock?: string;
  
  health_status?: 'ok' | 'attention_required' | 'locked';
  
  // Внимание: lowercase в JSON
  address?: string;
  description?: string;
  
  licenses?: LicensesDict | string;
  attribute_excise?: boolean | null;
  attribute_marked?: boolean | null;
  ofd_name?: string;
  owner_id?: string;
  owner_binding_mode?: 'auto' | 'manual';
}

export interface WorkstationDetailDTO {
  id: string;
  updated_at?: string;
  last_updated_by?: string;
  last_modified_date?: string;
  device_name?: string;
  teamviewer?: string;
  anydesk?: string;
  litemanager?: string;
  description?: string;
  health_status?: 'ok' | 'attention_required' | 'locked';
  owner_id?: string;
  owner_binding_mode?: 'auto' | 'manual';
}

export interface ServerDetailDTO {
  id: string;
  updated_at?: string;
  last_updated_by?: string;
  last_modified_date?: string;
  unique_id?: string;
  ip?: string;
  device_name?: string;
  server_name?: string;
  server_version?: string;
  server_edition?: string;
  
  last_polled_at?: string;
  status?: 'active' | 'offline' | 'unknown'; // Operational status
  health_status?: 'ok' | 'attention_required' | 'locked';
  
  cabinet_link?: string;
  partners_link?: string;
  crm_id?: string;
  
  rdp?: string;
  teamviewer?: string;
  anydesk?: string;
  litemanager?: string;
  description?: string;
  owner_id?: string;
  owner_binding_mode?: 'auto' | 'manual';
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
  unique_id?: string;
  crm_id?: string;
  device_name?: string;
  server_name?: string;
  server_version?: string;
  server_edition?: string;
  ip?: string;
  anydesk?: string;
  teamviewer?: string;
  rdp?: string;
  litemanager?: string;
  cabinet_link?: string;
  description?: string;
  owner_id?: string;
}

export interface UpdateWorkstationPayload {
  device_name?: string;
  anydesk?: string;
  teamviewer?: string;
  litemanager?: string;
  description?: string;
  owner_id?: string;
}

export interface UpdateFiscalPayload {
  model_kkt?: string;
  rn_kkt?: string;
  fr_serial_number?: string;
  inn?: string;
  legal_name?: string;
  fn_number?: string;
  kkt_reg_date?: string;
  fn_expire_date?: string;
  fr_firmware?: string;
  fr_downloader?: string;
  driver_version?: string;
  address?: string;
  ofd_name?: string;
  description?: string;
  owner_id?: string;
}

export interface ContractDetailDTO {
  id?: string;
  state?: string;
  services?: string[] | Record<string, unknown>;
  recipients?: string[] | Record<string, unknown>;
  service_level?: number;
  state_start_time?: string;
}

// --- Candidate Acceptance DTO ---
export type CandidateStatus = 'NEW' | 'IN_REVIEW' | 'APPROVED' | 'REJECTED' | 'CANCELLED';

export interface CandidateWorkstationStagingDTO {
  id: number;
  candidate_id: number;
  observation_id: number;
  observed_at: string;
  hostname?: string;
  agent_uuid?: string;
  workstation_uuid?: string;
  teamviewer_id?: string;
  litemanager_id?: string;
  anydesk_id?: string;
  url_rms?: string;
  name?: string;
}

export interface CandidateFiscalStagingDTO {
  id: number;
  candidate_id: number;
  observation_id: number;
  observed_at: string;
  serial_number?: string;
  serial_normalized?: string;
  rn_kkt?: string;
  model_name?: string;
  inn?: string;
  fn_number?: string;
  fn_expire_date?: string;
  organization_name?: string;
  address?: string;
}

export interface CandidateDTO {
  id: number;
  server_key?: string;
  server_crm_id?: string;
  server_url?: string;
  existing_server_id?: string;
  status: CandidateStatus;
  ticket_id?: number;
  approved_company_id?: string;
  approved_server_id?: string;
  created_at: string;
  updated_at: string;
  staged_workstations?: CandidateWorkstationStagingDTO[];
  staged_fiscals?: CandidateFiscalStagingDTO[];
}

export interface CandidateObservationDTO {
  observation_id: number;
  observed_at: string;
  payload_json: Record<string, unknown>;
}

export interface CandidateApprovePayload {
	company_id?: string;
	company?: {
		title: string;
		address?: string;
		additional_name?: string;
		parent_id?: string;
		contract_mode?: 'inherit_parent' | 'new';
		contract_type?: string;
	};
	server?: {
		mode: 'existing' | 'new';
		server_id?: string;
		crm_id?: string;
		url_rms?: string;
		unique_id?: string;
		cabinet_link?: string;
		device_name?: string;
		description?: string;
	};
	workstations?: Array<{
		staging_id?: number;
		name: string;
		workstation_uuid?: string;
	}>;
	comment?: string;
	bitrix_service_point_id?: number;
	// Ручной ввод remote IDs (опционально).
	// Используется когда агент не собрал TeamViewer/LiteManager/AnyDesk.
	teamviewer_id?: string;
	litemanager_id?: string;
	anydesk_id?: string;
}

export type CompanyMode = 'existing' | 'new';
export type ServerMode = 'existing' | 'new';
export type ContractMode = 'inherit_parent' | 'new';

export type NetworkCandidateStatus = 'NEW' | 'IN_REVIEW' | 'APPROVED' | 'REJECTED' | 'CANCELLED';

export interface NetworkCandidateDTO {
  id: number;
  status: NetworkCandidateStatus;
  hub_company_id: string;
  server_id: string;
  server_key?: string;
  server_crm_id?: string;
  server_url?: string;
  // Информация о конфликте владельцев
  conflict_info?: string;
  ws_owner_candidate?: string;
  fr_owner_candidate?: string;
  created_at: string;
  updated_at: string;
}

export interface NetworkCandidateWSStagingDTO {
  id: number;
  group_id: number;
  observed_at: string;
  hostname?: string;
  agent_uuid?: string;
  workstation_uuid?: string;
  teamviewer_id?: string;
  litemanager_id?: string;
  anydesk_id?: string;
  url_rms?: string;
}

export interface NetworkCandidateFRStagingDTO {
  id: number;
  group_id: number;
  observed_at: string;
  serial_number?: string;
  serial_normalized?: string;
  rn_kkt?: string;
  model_name?: string;
  inn?: string;
  fn_number?: string;
  fn_expire_date?: string;
  organization_name?: string;
  address?: string;
}

export interface NetworkCandidateGroupDTO {
  group: {
    id: number;
    candidate_id: number;
    observation_id: number;
    status: string;
    created_at: string;
    updated_at: string;
  };
  ws?: NetworkCandidateWSStagingDTO;
  frs: NetworkCandidateFRStagingDTO[];
}

export interface NetworkCandidateDetailsDTO {
  candidate: NetworkCandidateDTO;
  groups: NetworkCandidateGroupDTO[];
}

export interface NetworkCandidateApprovePayload {
  child_company_id?: string;
  child_company?: {
    title: string;
    address?: string;
  };
  comment?: string;
}

export interface EntityOwnerHistoryItemDTO {
  id: number;
  entity_type: string;
  entity_id: string;
  from_owner_id?: string;
  to_owner_id: string;
  change_source: string;
  comment?: string;
  changed_by_user_id?: string;
  actor_type?: 'user' | 'agent' | 'system';
  agent_uuid?: string;
  observation_id?: number;
  created_at: string;
  is_agent_update?: boolean;
}

export interface AgentObservationFeedRowDTO {
  observation_id: number;
  agent_uuid?: string;
  vc?: string;
  workstation_id?: string;
  workstation_name?: string;
  fr_id?: string;
  fr_name?: string;
  owner_match?: boolean;
  observed_at: string;
  current_time?: string;
  v_time?: string;
  current_time_parsed?: string;
  v_time_parsed?: string;
  server_url?: string;
}

export interface AgentListItemDTO {
  uuid: string;
  hostname?: string;
  type?: string;
  status?: string;
  owner_id?: string;
  workstation_id?: string;
  last_observed_at?: string;
  last_heartbeat?: string;
}

export interface AgentObservationDetailsDTO {
  id: number;
  source?: string;
  status?: string;
  observed_at?: string;
  workstation_id?: string;
  fr_id?: string;
  payload_json?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

