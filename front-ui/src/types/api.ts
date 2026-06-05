// Общий конверт ответа API
export interface ApiResponse<T> {
  status: "success" | "error";
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

export interface CompanyContractReportRowDTO {
  company_id: string;
  company_title: string;
  company_parent_title?: string;
  company_contract_status: string;
  contract_id?: string;
  contract_type?: string;
  contract_state?: string;
}

export type AntBadgeStatus =
  | "success"
  | "processing"
  | "error"
  | "default"
  | "warning";

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
  iiko_web_link?: string;
  partners_link?: string;

  operational_status?: "active" | "offline" | "unknown";
  health_status: "ok" | "attention_required" | "locked";
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
  rustdesk?: string;

  health_status: "ok" | "attention_required" | "locked";
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

  health_status: "ok" | "attention_required" | "locked";
  status_details?: unknown;

  address?: string;
  description?: string;

  [key: string]: unknown;
}

export type EntityData = ServerEntity | WorkstationEntity | FiscalEntity;

export interface InfrastructureItem {
  entity_type: "Server" | "Workstation" | "FiscalRegister";
  data: EntityData;
}

// --- Search DTO ---
export interface SearchFoundEntity {
  entity_type: "Server" | "Workstation" | "FiscalRegister";
  data: EntityData;
}

export interface TicketSearchDTO {
  id: string;
  number: number;
  subject: string;
  description?: string;
  status: TicketStatus;
  company_id?: string;
  company_name?: string;
  assignee_name?: string;
  reporter_name?: string;
  last_comment?: string;
  last_activity: string;
  created_at: string;
  updated_at: string;
  is_archived: boolean;
  created_source?: string;
}

export interface SearchResultGroup {
  owner: CompanyOwner;
  found_entities: SearchFoundEntity[];
  matched_tickets?: TicketSearchDTO[];
  active_tickets?: TicketSearchDTO[];
}

export interface SearchResponseData {
  search_results: SearchResultGroup[];
  ticket_results_without_company?: TicketSearchDTO[];
}

// --- Tasks DTO ---
export type TaskStatus =
  | "new"
  | "resolved"
  | "rejected"
  | "pending_sd_action"
  | "sd_error";
export type TaskType = "add_equipment" | "conflict" | "offline_alert";

export interface TaskDTO {
  id: number;
  task_type: TaskType;
  entity_type: string;
  status: TaskStatus;
  created_at: string;
  details: Record<string, unknown>;
}

export interface TaskResolutionPayload {
  status: "resolved" | "rejected";
  comment?: string;
  resolution_payload?: {
    action: string;
    new_owner_id?: string;
    [key: string]: unknown;
  };
}

// --- Tickets DTO ---
export type TicketStatus =
  | "new"
  | "in_progress"
  | "pending"
  | "deferred"
  | "onsite"
  | "to_manager"
  | "resolved"
  | "spam"
  | "execution"
  | "closed";

export type ManagerTransferTarget = "sales" | "payment_review";
export type ManagerTransferContactType = "phone" | "telegram";

export interface TicketListItemDTO {
  id: string;
  number: number;
  subject: string;
  company_name?: string;
  description?: string;
  reporter_name?: string;
  created_source?: "ui" | "bitrix" | "servicedesk" | "system" | string;
  result?: string;
  status: TicketStatus;
  last_comment?: string;
  last_comment_author?: string;
  last_comment_is_private?: boolean;
  last_activity: string;
  created_at?: string;
  company_id: string;
  contact_id?: number;
  contract_id?: string;
  is_common_contract?: boolean;
  sync_with_bitrix?: boolean;
  is_archived?: boolean;
  archived_at?: string;
  bitrix_service_point_id?: number;
  bitrix_deal_title?: string;
  manager_transfer_target?: ManagerTransferTarget | string;
  bitrix_deal_id?: number;
  bitrix_deal_url?: string;
  pyrus_task_id?: number;
  pyrus_task_url?: string;
  deferred_until?: string;
  deferred_by_id?: number;
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
  contact_id?: number;
  contract_id?: string;
  is_common_contract?: boolean;
  sync_with_bitrix?: boolean;
  is_archived?: boolean;
  archived_at?: string;
  bitrix_service_point_id?: number;
  bitrix_deal_title?: string;
  manager_transfer_target?: ManagerTransferTarget | string;
  bitrix_deal_id?: number;
  bitrix_deal_url?: string;
  pyrus_task_id?: number;
  pyrus_task_url?: string;
  deferred_until?: string;
  deferred_by_id?: number;
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

export interface TelephonyContactDTO {
  id: number;
  phone_normalized: string;
  phone_display: string;
  name?: string;
  bitrix_contact_id?: string;
}

export interface TelephonyCallDTO {
  id: string;
  external_call_id: string;
  direction: string;
  status: string;
  missed_status?: string;
  client_phone?: string;
  vat_number?: string;
  employee_login?: string;
  employee_user_id?: number;
  employee_name?: string;
  employee_state?: string;
  group_name?: string;
  started_at?: string;
  answered_at?: string;
  completed_at?: string;
  wait_seconds?: number;
  duration_seconds?: number;
  recording_url?: string;
  has_recording: boolean;
  ticket_id?: string;
  contact?: TelephonyContactDTO;
}

export interface TelephonyPendingContextDTO {
  id: string;
  external_call_id: string;
  client_phone: string;
  expires_at: string;
  contact?: TelephonyContactDTO;
  call?: TelephonyCallDTO;
}

export interface TelephonyContactCompanyDTO {
  company_id: string;
  title: string;
  parent_title?: string;
  last_seen_at: string;
  active_contract?: boolean;
  contract_type?: string;
}

export interface TelephonyCallListResponseDTO {
  items: TelephonyCallDTO[];
  total: number;
}

export interface TelephonyCallListParams {
  employee_user_id?: number;
  client_phone?: string;
  status?: string | string[];
  group_name?: string | string[];
  started_from?: string;
  started_to?: string;
  only_missed?: boolean;
  only_without_ticket?: boolean;
  limit?: number;
  offset?: number;
}

export interface TelephonyLineEmployeeDTO {
  user_id?: number;
  login: string;
  name: string;
  status: "online" | "offline" | "in_call" | string;
  provider: string;
  provider_ext?: string;
  provider_line?: string;
}

export interface TelephonyLineDTO {
  color: "blue" | "yellow" | "green" | "red" | string;
  on_line_count: number;
  missed_open_count: number;
  employees: TelephonyLineEmployeeDTO[];
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

export type ServicePointSyncAction =
  | "create"
  | "update"
  | "delete"
  | "unchanged"
  | "skipped"
  | "ambiguous";

export interface ServicePointSyncPlanItemDTO {
  key: string;
  row: number;
  name: string;
  one_c_code: string;
  contract_label?: string;
  action: ServicePointSyncAction;
  reason?: string;
  b24_element_id?: number;
  current_code?: string;
  current_contract?: string;
  matched_point_ids?: number[];
  auto_apply?: boolean;
}

export interface ServicePointSyncPreviewDTO {
  processed_rows: number;
  to_create: number;
  to_update: number;
  to_delete: number;
  unchanged: number;
  skipped: number;
  ambiguous: number;
  items: ServicePointSyncPlanItemDTO[];
}

export interface ServicePointSyncApplyResultDTO {
  processed_rows: number;
  created: number;
  updated: number;
  deleted: number;
  unchanged: number;
  skipped: number;
  ambiguous: number;
  applied_keys?: string[];
  errors?: string[];
}

export interface ContractMailImportDTO {
  id: string;
  source?: string;
  message_id: string;
  attachment_name: string;
  attachment_hash: string;
  received_at?: string;
  status: string;
  error_text?: string;
  processed_at?: string;
  rows_count: number;
  created_at: string;
  updated_at: string;
}

export interface ContractSyncQueueItemDTO {
  key: string;
  action: "create" | "update" | "delete";
  service_point_name: string;
  service_point_code: string;
  contractor_id?: string;
  contractor_name?: string;
  contract_type?: string;
  b24_element_id?: number;
  current_name?: string;
  current_code?: string;
  current_contract_type?: string;
  change_set?: ContractSyncFieldDiffDTO[];
  matched_point_ids?: number[];
  filled_fields?: number;
  is_mapped?: boolean;
  reason?: string;
}

export interface ContractSyncFieldDiffDTO {
  field: string;
  label: string;
  current_value?: string;
  next_value?: string;
}

export interface ContractSyncBlockedItemDTO {
  key: string;
  service_point_name?: string;
  service_point_code?: string;
  contractor_id?: string;
  contractor_name?: string;
  reason: string;
  resolution_hint?: string;
  matched_point_ids?: number[];
}

export interface ContractSyncRunSummaryDTO {
  id: string;
  status: string;
  mode: string;
  actor_type: string;
  actor_user_id?: number;
  actor_name?: string;
  note?: string;
  started_at: string;
  completed_at?: string;
  report_rows: number;
  to_create: number;
  to_update: number;
  to_delete: number;
  blocked_rows: number;
  processed: number;
  created: number;
  updated: number;
  deleted: number;
  active_imports?: ContractMailImportDTO[];
}

export interface ContractSyncRunDetailsDTO extends ContractSyncRunSummaryDTO {
  queue_items?: ContractSyncQueueItemDTO[];
  errors?: string[];
  error_details?: ContractSyncExecuteErrorDTO[];
}

export interface ContractSyncAutoExecutionDTO {
  enabled: boolean;
  interval_minutes: number;
  applies_creates: boolean;
  applies_updates: boolean;
  applies_deletes: boolean;
  trigger_label?: string;
  safety_description?: string;
}

export interface ContractSyncStateDTO {
  latest_import?: ContractMailImportDTO;
  active_report_import?: ContractMailImportDTO;
  active_report_imports?: ContractMailImportDTO[];
  recent_imports: ContractMailImportDTO[];
  recent_runs?: ContractSyncRunSummaryDTO[];
  auto_sync?: ContractSyncAutoExecutionDTO;
  report_rows: number;
  to_create: number;
  to_update: number;
  to_delete: number;
  blocked_rows: number;
  blocked_items?: ContractSyncBlockedItemDTO[];
  upsert_items: ContractSyncQueueItemDTO[];
  delete_items: ContractSyncQueueItemDTO[];
}

export interface ContractSyncExecuteResultDTO {
  processed: number;
  created: number;
  updated: number;
  deleted: number;
  applied_keys?: string[];
  errors?: string[];
  error_details?: ContractSyncExecuteErrorDTO[];
}

export interface ContractSyncExecuteErrorDTO {
  key: string;
  action: "create" | "update" | "delete";
  service_point_name?: string;
  service_point_code?: string;
  b24_element_id?: number;
  message: string;
}

export interface TicketHistoryDTO {
  id: number;
  ticket_id: string;
  user_id?: number;
  user_name?: string;
  action: string;
  field: string;
  source?: "ui" | "bitrix" | "servicedesk" | "system" | string;
  old_value: string;
  new_value: string;
  meta?: Record<string, unknown>;
  created_at: string;
}

export interface ConnectionCopyStatDTO {
  entity_type: "Server" | "Workstation" | string;
  entity_id: string;
  copy_count: number;
  last_copied_at?: string;
}

export interface TicketCommentDTO {
  uuid: string;
  text: string;
  author_name: string;
  author_user_id?: number;
  creation_date: string;
  is_internal: boolean;
  is_private?: boolean;
  reply_to_client?: boolean;
}

export interface TicketDetailsDTO {
  metadata: TicketDTO;
  company_name?: string;
  contact?: TelephonyContactDTO;
  calls?: TelephonyCallDTO[];
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
  today_count: number;
  days_7_count: number;
  days_30_count: number;
}

export interface DashboardAcceptedCallsByEmployeeDTO {
  user_id: number;
  user_name: string;
  count: number;
}

export interface DashboardServerStatusDTO {
  status: string;
  count: number;
}

export interface DashboardStatsDTO {
  resolved_by_assignee: DashboardResolvedByAssigneeDTO[];
  accepted_calls_by_employee: DashboardAcceptedCallsByEmployeeDTO[];
  server_statuses: DashboardServerStatusDTO[];
  total_tickets: number;
  polled_servers_24h: number;
  accepted_calls_24h: number;
}

export interface TicketListParams {
  company_id?: string;
  company_ids?: string | string[];
  limit?: number;
  offset?: number;
  status?: string | string[];
  search?: string;
  archive_mode?: "active" | "archive" | "all";
  period_from?: string;
  period_to?: string;
  created_from?: string;
  created_to?: string;
  closed_from?: string;
  closed_to?: string;
  assignee_ids?: string | string[];
}

export type ArticleType =
  | "wiki"
  | "release_note"
  | "company_news"
  | "incident_note"
  | "internal_doc";

export type ArticleStatus = "draft" | "published" | "archived";
export type ArticleContentFormat = "markdown" | "tiptap_json";

export interface ArticleLinkDTO {
  entity_type: "Company" | "Server" | "Workstation" | "FiscalRegister" | "Ticket";
  entity_id: string;
}

export interface ArticleDTO {
  id: string;
  slug: string;
  title: string;
  summary: string;
  content: string;
  content_format: ArticleContentFormat;
  type: ArticleType;
  status: ArticleStatus;
  project_key?: string;
  version?: string;
  tags: string[];
  is_pinned: boolean;
  show_on_home: boolean;
  published_at?: string;
  author_id?: number;
  author_name: string;
  links?: ArticleLinkDTO[];
  created_at: string;
  updated_at: string;
}

export interface ArticlePayload {
  slug?: string;
  title: string;
  summary: string;
  content: string;
  content_format: ArticleContentFormat;
  type: ArticleType;
  status?: ArticleStatus;
  project_key?: string;
  version?: string;
  tags: string[];
  is_pinned: boolean;
  show_on_home: boolean;
  links?: ArticleLinkDTO[];
}

export interface ArticleListParams {
  term?: string;
  type?: ArticleType;
  status?: ArticleStatus;
  tag?: string;
  project_key?: string;
  limit?: number;
  offset?: number;
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

export type UserPosition = "admin" | "support_specialist" | "intern";
export type UserSchedule = "2/2" | "3/3" | "5/2";

export interface UserAdminDTO {
  id: number;
  username: string;
  full_name: string;
  first_name: string;
  last_name: string;
  position: UserPosition;
  email?: string;
  roles: string[];
  bitrix_enabled?: boolean;
  pyrus_enabled?: boolean;
  external_system_id?: string;
  external_type?: string;
  schedule_type: UserSchedule;
  is_active: boolean;
  has_logged_in: boolean;
  integrations?: UserIntegrationDTO[];
  bitrix_suggestion?: BitrixUserSuggestionDTO | null;
  pyrus_suggestion?: PyrusUserSuggestionDTO | null;
}

export interface UserIntegrationDTO {
  id: number;
  integration_type: string;
  external_id: string;
  is_enabled: boolean;
  is_verified: boolean;
  is_locked?: boolean;
  verified_name?: string;
}

export interface BitrixUserSuggestionDTO {
  b24_user_id: number;
  name: string;
}

export interface PyrusUserSuggestionDTO {
  pyrus_user_id: number;
  name: string;
  email?: string;
}

export interface MegafonUserSuggestionDTO {
  login: string;
  name: string;
}

export interface ThemePaletteConfigDTO {
  primary?: string;
  bg_layout?: string;
  bg_container?: string;
  border_color?: string;
}

export interface UserInterfaceConfigDTO {
  theme_mode?: "light" | "dark";
  locale?: "en" | "ru" | string;
  theme_palettes?: {
    light?: ThemePaletteConfigDTO;
    dark?: ThemePaletteConfigDTO;
  };
  search?: {
    cards_columns?: number;
  };
}

export interface GlobalTranslationLocaleDTO {
  code: string;
  label: string;
  native_label: string;
  is_builtin: boolean;
}

export interface GlobalTranslationsDTO {
  locales: GlobalTranslationLocaleDTO[];
  overrides: Record<string, Record<string, Record<string, string>>>;
}

export interface UserProfileConfigDTO {
  interface?: UserInterfaceConfigDTO;
  notifications?: {
    personal_enabled?: boolean;
    common_enabled?: boolean;
    common_ticket_updates?: boolean;
    common_equipment_updates?: boolean;
    common_comments?: boolean;
    common_deferred_due?: boolean;
    ticket_subscriptions_only?: boolean;
  };
  tickets?: {
    comments_new_first?: boolean;
    parse_phone_from_description?: boolean;
    subscriptions?: string[];
    filters?: {
      presets?: Array<{
        id: string;
        name: string;
        values: Record<string, string>;
      }>;
      last_state?: Record<string, string>;
    };
  };
  [key: string]: unknown;
}

export interface UserCreatePayload {
  username: string;
  password: string;
  first_name: string;
  last_name: string;
  position: UserPosition;
  email?: string;
  external_system_id?: string;
  external_type?: string;
  integrations?: Array<{
    integration_type: string;
    external_id: string;
    is_enabled?: boolean;
  }>;
  schedule_type: UserSchedule;
}

export interface UserUpdatePayload {
  username?: string;
  password?: string;
  first_name?: string;
  last_name?: string;
  position?: UserPosition;
  email?: string;
  external_system_id?: string;
  external_type?: string;
  integrations?: Array<{
    integration_type: string;
    external_id: string;
    is_enabled?: boolean;
  }>;
  schedule_type?: UserSchedule;
}

export interface DeletedUserRestoreCandidateDTO {
  id: number;
  username: string;
  full_name: string;
  first_name: string;
  last_name: string;
  position: UserPosition;
  email?: string;
  schedule_type: UserSchedule;
  deleted_at?: string;
  integrations?: UserIntegrationDTO[];
}

export interface BitrixDirectoryUserDTO {
  b24_user_id: number;
  name: string;
  active: boolean;
  last_name?: string;
  first_name?: string;
  second_name?: string;
  email?: string;
  phone?: string;
  last_seen_at?: string;
  updated_at: string;
}

export interface BitrixUsersRefreshDTO {
  status: string;
  count: number;
  users: BitrixDirectoryUserDTO[];
}

export interface PyrusDirectoryUserDTO {
  pyrus_user_id: number;
  name: string;
  first_name?: string;
  last_name?: string;
  email?: string;
  position?: string;
  type?: string;
  status?: string;
  banned: boolean;
  fired: boolean;
  mobile_phone?: string;
  phone?: string;
  location?: string;
  personnel_number?: string;
}

export interface PyrusUsersRefreshDTO {
  status: string;
  count: number;
  users: PyrusDirectoryUserDTO[];
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

  health_status?: "ok" | "attention_required" | "locked";

  // Внимание: lowercase в JSON
  address?: string;
  description?: string;

  licenses?: LicensesDict | string;
  attribute_excise?: boolean | null;
  attribute_marked?: boolean | null;
  ofd_name?: string;
  owner_id?: string;
  owner_binding_mode?: "auto" | "manual";
}

export interface WorkstationDetailDTO {
  id: string;
  updated_at?: string;
  last_updated_by?: string;
  last_modified_date?: string;
  device_name?: string;
  server_id?: string;
  teamviewer?: string;
  anydesk?: string;
  litemanager?: string;
  rustdesk?: string;
  description?: string;
  health_status?: "ok" | "attention_required" | "locked";
  owner_id?: string;
  owner_binding_mode?: "auto" | "manual";
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
  status?: "active" | "offline" | "unknown"; // Operational status
  health_status?: "ok" | "attention_required" | "locked";

  cabinet_link?: string;
  iiko_web_link?: string;
  partners_link?: string;
  crm_id?: string;

  rdp?: string;
  teamviewer?: string;
  anydesk?: string;
  litemanager?: string;
  description?: string;
  owner_id?: string;
  owner_binding_mode?: "auto" | "manual";
}

// ... (предыдущие TaskDTO и прочее остаются)
export interface ApiResponse<T> {
  status: "success" | "error";
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
  iiko_web_link?: string;
  description?: string;
  owner_id?: string;
}

export interface InstallServerLicensePayload {
  login: string;
  password: string;
  fallback_password?: string;
  unique_id: string;
}

export interface InstallServerLicenseResult {
  server_id: string;
  status: string;
  server_name?: string;
  server_version?: string;
  server_edition?: string;
  crm_id?: string;
  last_polled_at: string;
}

export interface InstallServerLicenseResponse {
  message: string;
  result?: InstallServerLicenseResult;
}

export interface UpdateWorkstationPayload {
  device_name?: string;
  anydesk?: string;
  teamviewer?: string;
  litemanager?: string;
  rustdesk?: string;
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
  companies?: Array<{ id: string; title: string }>;
  service_level?: number;
  state_start_time?: string;
}

export interface MaterialEntityRefDTO {
  entity_type: "Company" | "Server" | "Workstation" | "FiscalRegister";
  entity_id: string;
}

export interface MaterialDTO {
  id: string;
  author_id?: number;
  author_name: string;
  subject: string;
  content: string;
  entity_refs: MaterialEntityRefDTO[];
  created_at: string;
  updated_at: string;
}

export interface MaterialPayload {
  subject: string;
  content: string;
  entity_refs: MaterialEntityRefDTO[];
}

// --- Candidate Acceptance DTO ---
export type CandidateStatus =
  | "NEW"
  | "IN_REVIEW"
  | "APPROVED"
  | "REJECTED"
  | "CANCELLED"
  | "SUPERSEDED";

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
  rustdesk_id?: string;
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
  deactivation_reason?: string;
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
  observation_uid?: string;
  observed_at: string;
  payload_json: Record<string, unknown>;
}

export interface CandidateRecalculationResultDTO {
  candidates_total: number;
  observations_total: number;
  reprocessed: number;
  applied: number;
  staged: number;
  ignored: number;
  ignored_stale: number;
  errors: number;
  candidates_closed: number;
}

export interface CandidateApprovePayload {
  company_id?: string;
  company?: {
    title: string;
    address?: string;
    additional_name?: string;
    parent_id?: string;
    contract_mode?: "inherit_parent" | "new";
    contract_type?: string;
  };
  server?: {
    mode: "existing" | "new";
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
  rustdesk_id?: string;
  anydesk_id?: string;
}

export interface CandidateRejectPayload {
  comment?: string;
}

export type CompanyMode = "existing" | "new";
export type ServerMode = "existing" | "new";
export type ContractMode = "inherit_parent" | "new";

export type NetworkCandidateStatus =
  | "NEW"
  | "IN_REVIEW"
  | "APPROVED"
  | "REJECTED"
  | "CANCELLED"
  | "SUPERSEDED";

export interface NetworkCandidateDTO {
  id: number;
  status: NetworkCandidateStatus;
  hub_company_id: string;
  server_id: string;
  server_key?: string;
  server_crm_id?: string;
  server_url?: string;
  deactivation_reason?: string;
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
  rustdesk_id?: string;
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
    deactivation_reason?: string;
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
  actor_type?: "user" | "agent" | "system";
  agent_uuid?: string;
  observation_id?: number;
  created_at: string;
  is_agent_update?: boolean;
}

export type EntityDeletionCandidateStatus =
  | "PENDING"
  | "CONFIRMED"
  | "CANCELLED";

export interface EntityDeletionCandidateDTO {
  id: number;
  entity_type: "Server" | "Workstation" | "FiscalRegister" | string;
  entity_id: string;
  entity_display_name?: string;
  status: EntityDeletionCandidateStatus;
  reason?: string;
  source: "manual" | "duplicate_worker" | string;
  comment?: string;
  requested_by_user_id?: string;
  requested_at: string;
  confirmed_by_user_id?: string;
  confirmed_at?: string;
  duplicate_of_entity_id?: string;
  duplicate_field?: string;
  duplicate_value?: string;
  meta?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface EntityDeletionReplayRequestDTO {
  keep_entity_id: string;
  delete_entity_id: string;
}

export interface EntityDeletionCandidateAgentDataDTO {
  observation_id: number;
  observed_at?: string;
  payload_json?: Record<string, unknown>;
}

export interface EntityDeletionCandidateEntityDetailsDTO {
  entity_type: "Server" | "Workstation" | "FiscalRegister" | string;
  entity_id: string;
  display_name: string;
  owner_id: string;
  last_updated_by: string;
  updated_at: string;
  last_modified_date?: string;
  deleted: boolean;
  is_more_actual: boolean;
  raw: Record<string, unknown>;
  latest_agent_data?: EntityDeletionCandidateAgentDataDTO | null;
}

export interface EntityDeletionCandidateDetailsDTO {
  candidate: EntityDeletionCandidateDTO;
  reason_text: string;
  keep_entity?: EntityDeletionCandidateEntityDetailsDTO | null;
  delete_entity?: EntityDeletionCandidateEntityDetailsDTO | null;
  entities: EntityDeletionCandidateEntityDetailsDTO[];
  cascade_entities?: EntityDeletionCandidateEntityDetailsDTO[];
  more_actual_entity_id: string;
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

export interface AgentDiagnosticsListItemDTO {
  uuid: string;
  hostname?: string;
  type?: string;
  status?: string;
  owner_id?: string;
  workstation_id?: string;
  last_observed_at?: string;
  last_heartbeat?: string;
  last_registration_at?: string;
  last_registration_status?: string;
  last_registration_error?: string;
  registration_approved_at?: string;
  registration_approved_by?: string;
  machine_fingerprint?: string;
  has_latest_inventory?: boolean;
  has_adapter_statuses?: boolean;
}

export interface AgentInventoryNetworkInterfaceDTO {
  name?: string;
  index?: number;
  mtu?: number;
  hardware_addr?: string;
  addresses?: string[];
  flags?: string[];
}

export interface AgentInventoryHostInfoDTO {
  roaming_app_data_path?: string;
  cash_server_product?: string;
  cash_server_config?: string;
  cash_server_url?: string;
  teamviewer_id?: string;
  anydesk_id?: string;
  litemanager_id?: string;
  rustdesk_id?: string;
}

export interface AgentInventoryCOMPortClassificationDTO {
  device_type?: string;
  label?: string;
  confidence?: string;
  source?: string;
  matched_signature?: string;
  suggested_adapter?: string;
}

export interface AgentInventoryCOMPortDTO {
  name?: string;
  device?: string;
  source?: string;
  enumerator?: string;
  instance_id?: string;
  friendly_name?: string;
  description?: string;
  manufacturer?: string;
  service?: string;
  class?: string;
  location?: string;
  hardware_ids?: string[];
  compatible_ids?: string[];
  vendor_id?: string;
  product_id?: string;
  signature_key?: string;
  classification?: AgentInventoryCOMPortClassificationDTO | null;
}

export interface AgentInventoryInstalledSoftwareDTO {
  name?: string;
  version?: string;
  publisher?: string;
  install_location?: string;
  uninstall_string?: string;
  source?: string;
}

export interface AgentInventoryComponentEvidenceDTO {
  type?: string;
  source?: string;
  value?: string;
}

export interface AgentInventoryKnownComponentDTO {
  key?: string;
  name?: string;
  category?: string;
  detected?: boolean;
  version?: string;
  evidence?: AgentInventoryComponentEvidenceDTO[];
}

export interface AgentInventorySnapshotDTO {
  collected_at?: string;
  hostname?: string;
  os?: string;
  arch?: string;
  executable_path?: string;
  host_info?: AgentInventoryHostInfoDTO | null;
  network_interfaces?: AgentInventoryNetworkInterfaceDTO[];
  com_ports?: AgentInventoryCOMPortDTO[];
  installed_software?: AgentInventoryInstalledSoftwareDTO[];
  known_components?: AgentInventoryKnownComponentDTO[];
  [key: string]: unknown;
}

export interface AgentAdapterStatusDTO {
  adapter_id?: string;
  adapter_type?: string;
  version?: string;
  target_os?: string;
  target_arch?: string;
  protocol_version?: string;
  status?: string;
  run_status?: string;
  local_path?: string;
  file_size?: number;
  sha256?: string;
  last_error?: string;
  last_exit_code?: number | null;
  last_run_at?: string;
  installed_at?: string;
  updated_at?: string;
  [key: string]: unknown;
}

export interface AgentAdapterManifestDTO {
  adapter_id?: string;
  adapter_type?: string;
  version?: string;
  target_os?: string;
  target_arch?: string;
  protocol_version?: string;
  download_url?: string;
  sha256?: string;
  file_name?: string;
}

export interface AgentRegistrationAttemptDTO {
  id: number;
  agent_uuid?: string;
  status?: string;
  error_text?: string;
  machine_fingerprint?: string;
  system_info?: unknown;
  payload?: unknown;
  remote_addr?: string;
  created_at?: string;
}

export interface AgentAdapterRunDTO {
  id: number;
  agent_uuid: string;
  adapter_id?: string;
  type?: string;
  status?: string;
  command?: string;
  operation?: string;
  created_at?: string;
  sent_at?: string | null;
  completed_at?: string | null;
  duration_ms?: number;
  exit_code?: number | null;
  error_text?: string;
  stdout?: string;
  stderr?: string;
  structured_result?: unknown;
  payload?: unknown;
  result_payload?: unknown;
}

export interface AgentHeartbeatMeaningfulStateDTO {
  fingerprint?: string;
  last_meaningful_heartbeat_at?: string;
  last_meaningful_observed_at?: string;
  last_meaningful_state?: unknown;
}

export interface AgentMachineProfileDTO {
  key?: string;
  title?: string;
  summary?: string;
  source?: string;
  confirmed_at?: string;
  confirmed_by?: string;
}

export interface AgentCOMSignatureRuleDTO {
  id: number;
  signature_key?: string;
  device_type?: string;
  label?: string;
  confidence?: string;
  profile_hint?: string;
  suggested_adapter?: string;
  source?: string;
  notes?: string;
  updated_at?: string;
  updated_by?: string;
}

export interface AgentCOMSignatureCandidateDTO {
  port_name?: string;
  friendly_name?: string;
  signature_key?: string;
  vendor_id?: string;
  product_id?: string;
  classification_label?: string;
  classification_source?: string;
  device_type?: string;
  suggested_adapter?: string;
  existing_rule?: AgentCOMSignatureRuleDTO | null;
}

export interface AgentAdapterRuntimeDeviceDTO {
  label?: string;
  connection_type?: "tcp" | "com" | string;
  transport?: string;
  address?: string;
  ip?: string;
  port?: number;
  com_port?: string;
  baudrate?: string;
  model?: string;
  driver_hints?: Record<string, unknown>;
  extra_params?: Record<string, unknown>;
}

export interface AgentAdapterRuntimeScheduleDTO {
  enabled?: boolean;
  interval_seconds?: number;
  last_run_at?: string | null;
  next_run_at?: string | null;
}

export interface AgentAdapterRuntimeProfileDTO {
  adapter_id: string;
  command?: string;
  operation?: string;
  timeout_seconds?: number;
  devices?: AgentAdapterRuntimeDeviceDTO[];
  schedule?: AgentAdapterRuntimeScheduleDTO;
}

export interface PublishedAgentAdapterOptionDTO {
  adapter_id: string;
  title: string;
  description?: string;
  published: boolean;
  selectable: boolean;
  status_text: string;
  disabled_reason?: string;
  version?: string;
  stable_version?: string;
  latest_version?: string;
  adapter_type?: string;
  target_os?: string;
  target_arch?: string;
}

export interface AgentOperatorFlowDTO {
  meaningful_heartbeat?: AgentHeartbeatMeaningfulStateDTO | null;
  available_adapters?: PublishedAgentAdapterOptionDTO[];
  selected_adapter_ids?: string[];
  recommended_adapter_ids?: string[];
  recommended_profile?: AgentMachineProfileDTO | null;
  recommended_reasons?: string[];
  recommended_adapter_manifests?: AgentAdapterManifestDTO[];
  saved_profile?: AgentMachineProfileDTO | null;
  saved_reasons?: string[];
  saved_adapter_manifests?: AgentAdapterManifestDTO[];
  effective_adapter_manifests?: AgentAdapterManifestDTO[];
  saved_adapter_runtime_profiles?: AgentAdapterRuntimeProfileDTO[];
  signature_candidates?: AgentCOMSignatureCandidateDTO[];
  warnings?: string[];
}

export interface AgentDiagnosticsDetailsDTO {
  agent: AgentDiagnosticsListItemDTO;
  registration_payload?: unknown;
  registration_system_info?: unknown;
  latest_inventory?: AgentInventorySnapshotDTO | null;
  latest_adapter_statuses?: AgentAdapterStatusDTO[] | null;
  recent_registrations: AgentRegistrationAttemptDTO[];
  recent_adapter_runs?: AgentAdapterRunDTO[];
  operator_flow?: AgentOperatorFlowDTO | null;
}

export interface SaveAgentAdapterSelectionPayload {
  selected_adapter_ids?: string[];
  runtime_profiles?: AgentAdapterRuntimeProfileDTO[];
}

export interface UpsertAgentCOMSignatureRulePayload {
  signature_key: string;
  device_type?: string;
  label?: string;
  confidence?: string;
  profile_hint?: string;
  suggested_adapter?: string;
  notes?: string;
}

export interface EnqueueAgentAdapterRunPayload {
  adapter_id: string;
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
