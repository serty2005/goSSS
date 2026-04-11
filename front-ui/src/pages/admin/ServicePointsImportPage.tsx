import React, { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Input,
  Popconfirm,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
  theme as antTheme,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  ArrowLeftOutlined,
  ClearOutlined,
  CloseOutlined,
  ExclamationCircleOutlined,
  PlayCircleOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  StopOutlined,
} from '@ant-design/icons';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import { useLayoutHeader } from '@/components/layout/LayoutHeaderContext';
import { useBackNavigation } from '@/hooks/useBackNavigation';
import { useAuthStore } from '@/store/authStore';
import type {
  ApiResponse,
  ContractMailImportDTO,
  ContractSyncAutoExecutionDTO,
  ContractSyncBlockedItemDTO,
  ContractSyncExecuteResultDTO,
  ContractSyncFieldDiffDTO,
  ContractSyncQueueItemDTO,
  ContractSyncRunDetailsDTO,
  ContractSyncRunSummaryDTO,
  ContractSyncStateDTO,
} from '@/types/api';

const { Text } = Typography;

type UpsertFilter = 'all' | 'create' | 'update';
type DeleteFilter = 'all' | 'mapped' | 'unmapped';
type QueueFilter = 'all' | 'create' | 'update' | 'delete' | 'errors';
type PageTabKey = 'sync' | 'history';
type QueueRow = ContractSyncQueueItemDTO & {
  queue_order: number;
  execution_errors: string[];
  has_execution_errors: boolean;
};

type HeaderHintButtonProps = {
  icon: React.ReactNode;
  title: string;
  content: React.ReactNode;
  danger?: boolean;
  active?: boolean;
};

const actionOrder: Record<ContractSyncQueueItemDTO['action'], number> = { create: 0, update: 1, delete: 2 };

const formatDateTime = (value?: string) => {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ru-RU');
};

const shortValue = (value?: string, size = 10) => (!value ? '—' : value.length <= size * 2 ? value : `${value.slice(0, size)}…${value.slice(-size)}`);
const displayValue = (value?: string | number | null) => String(value ?? '').trim() || '—';
const hasValue = (value?: string | number | null) => String(value ?? '').trim().length > 0;
const normalizeSearch = (value: string) => value.trim().toLowerCase().replace(/ё/g, 'е');
const isChangedValue = (left?: string | number | null, right?: string | number | null) => String(left ?? '').trim() !== String(right ?? '').trim();
const joinCompactParts = (...parts: Array<string | number | null | undefined>) => parts.map((part) => String(part ?? '').trim()).filter(Boolean).join(' • ');
const buildDiffSummary = (diff: ContractSyncFieldDiffDTO) => `${diff.label}: ${displayValue(diff.current_value)} → ${displayValue(diff.next_value)}`;
const buildDeleteDuplicateSummary = (item: ContractSyncQueueItemDTO) => joinCompactParts(
  hasValue(item.service_point_code) ? `Код ${displayValue(item.service_point_code)}` : '',
  hasValue(item.current_code) ? `Текущий ${displayValue(item.current_code)}` : '',
  hasValue(item.current_contract_type || item.contract_type) ? `Тип ${displayValue(item.current_contract_type || item.contract_type)}` : '',
  hasValue(item.contractor_name) ? `Контрагент ${item.contractor_name}` : '',
);
const buildQueueIdentitySummary = (item: ContractSyncQueueItemDTO) => joinCompactParts(
  hasValue(item.service_point_code) ? `Код ${displayValue(item.service_point_code)}` : '',
  hasValue(item.contract_type) ? `Тип ${displayValue(item.contract_type)}` : '',
  hasValue(item.contractor_name) ? `Контрагент ${item.contractor_name}` : '',
);
const buildDuplicateGroupSummary = (item: ContractSyncQueueItemDTO) => item.matched_point_ids?.length
  ? `Группа дублей: ${item.matched_point_ids.join(', ')}`
  : 'Группа дублей не определена';
const buildBlockedDuplicateSummary = (item: ContractSyncBlockedItemDTO) => item.matched_point_ids?.length
  ? `Конфликтующие B24 ID: ${item.matched_point_ids.join(', ')}`
  : 'B24 ID конфликтующих точек не определены';

const HeaderHintButton: React.FC<HeaderHintButtonProps> = ({ icon, title, content, danger = false, active = false }) => {
  const { token } = antTheme.useToken();
  const [isPinned, setIsPinned] = useState(false);
  const [isHovered, setIsHovered] = useState(false);
  const isOpen = isPinned || isHovered;

  return (
    <div
      style={{ position: 'relative', display: 'inline-flex' }}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => {
        if (!isPinned) {
          setIsHovered(false);
        }
      }}
    >
      <button
        type="button"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          if (isPinned) {
            setIsPinned(false);
            setIsHovered(false);
            return;
          }
          setIsPinned(true);
          setIsHovered(true);
        }}
        style={{
          width: 36,
          height: 36,
          borderRadius: 18,
          border: `1px solid ${danger && active ? token.colorErrorBorder : token.colorBorder}`,
          background: danger && active ? token.colorErrorBg : token.colorBgContainer,
          color: danger && active ? token.colorError : token.colorTextSecondary,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: 'pointer',
        }}
      >
        {icon}
      </button>
      {isOpen ? (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 8px)',
            right: 0,
            zIndex: 3000,
            width: 360,
            maxWidth: 'min(360px, calc(100vw - 32px))',
            padding: 12,
            borderRadius: 12,
            border: `1px solid ${token.colorBorderSecondary}`,
            background: token.colorBgElevated,
            boxShadow: token.boxShadowSecondary,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, marginBottom: 8 }}>
            <strong>{title}</strong>
            {isPinned ? (
              <button
                type="button"
                onClick={(event) => {
                  event.preventDefault();
                  event.stopPropagation();
                  setIsPinned(false);
                  setIsHovered(false);
                }}
                style={{
                  width: 28,
                  height: 28,
                  borderRadius: 14,
                  border: `1px solid ${token.colorBorder}`,
                  background: token.colorBgContainer,
                  cursor: 'pointer',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  color: token.colorTextSecondary,
                }}
              >
                <CloseOutlined />
              </button>
            ) : null}
          </div>
          <div>{content}</div>
        </div>
      ) : null}
    </div>
  );
};

const importStatusTag = (status: string) => {
  if (status === 'processed') return <Tag color="green">Обработан</Tag>;
  if (status === 'failed') return <Tag color="red">Ошибка</Tag>;
  return <Tag>{status || '—'}</Tag>;
};

const importSourceTag = (source?: string) => {
  if (source === 'id') return <Tag color="geekblue">ID</Tag>;
  if (source === 'ru') return <Tag color="cyan">RU</Tag>;
  return <Tag>Источник не определён</Tag>;
};

const syncRunStatusTag = (status: string) => {
  if (status === 'success') return <Tag color="green">Успешно</Tag>;
  if (status === 'partial') return <Tag color="gold">Частично</Tag>;
  if (status === 'failed') return <Tag color="red">Ошибка</Tag>;
  if (status === 'skipped') return <Tag>Пропущено</Tag>;
  return <Tag>{status || '—'}</Tag>;
};

const syncRunModeTag = (mode: string) => {
  if (mode === 'automatic') return <Tag color="blue">Автоматически</Tag>;
  if (mode === 'manual') return <Tag color="purple">Вручную</Tag>;
  return <Tag>{mode || '—'}</Tag>;
};

const buildSyncRunActorLabel = (item?: ContractSyncRunSummaryDTO | ContractSyncRunDetailsDTO | null) => {
  if (!item) return '—';
  if (item.actor_name) return item.actor_name;
  return item.actor_type === 'system' ? 'Система' : 'Пользователь';
};

const buildSyncRunOptionLabel = (item: ContractSyncRunSummaryDTO) =>
  joinCompactParts(
    formatDateTime(item.started_at),
    buildSyncRunActorLabel(item),
    item.mode === 'automatic' ? 'Авто' : 'Ручной',
    `Изменений ${item.processed}`,
  );

const buildAutoSyncModeLabel = (autoSync?: ContractSyncAutoExecutionDTO) => {
  if (!autoSync?.enabled) return 'Автоматическое применение отключено';
  if (autoSync.applies_deletes) return 'Автоматически применяются create, update и delete';
  return 'Автоматически применяются только create и update';
};

const actionTag = (action: ContractSyncQueueItemDTO['action']) => {
  if (action === 'create') return <Tag color="green">Создать</Tag>;
  if (action === 'update') return <Tag color="blue">Обновить</Tag>;
  if (action === 'delete') return <Tag color="volcano">Удалить</Tag>;
  return <Tag>{action}</Tag>;
};

const getQueryErrorText = (error: unknown) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || 'Не удалось загрузить состояние синхронизации';
};

const removeAppliedItemsFromState = (current: ApiResponse<ContractSyncStateDTO> | undefined, appliedKeys: Set<string>) => {
  if (!current?.data || appliedKeys.size === 0) return current;
  const nextUpsertItems = current.data.upsert_items.filter((item) => !appliedKeys.has(item.key));
  const nextDeleteItems = current.data.delete_items.filter((item) => !appliedKeys.has(item.key));
  return {
    ...current,
    data: {
      ...current.data,
      upsert_items: nextUpsertItems,
      delete_items: nextDeleteItems,
      to_create: nextUpsertItems.filter((item) => item.action === 'create').length,
      to_update: nextUpsertItems.filter((item) => item.action === 'update').length,
      to_delete: nextDeleteItems.length,
    },
  };
};

const countByAction = (items: ContractSyncQueueItemDTO[]) => ({
  create: items.filter((item) => item.action === 'create').length,
  update: items.filter((item) => item.action === 'update').length,
  delete: items.filter((item) => item.action === 'delete').length,
});

const itemSearchText = (item: ContractSyncQueueItemDTO) =>
  normalizeSearch(
    [
      item.service_point_name,
      item.service_point_code,
      item.contract_type,
      item.current_name,
      item.current_code,
      item.current_contract_type,
      item.contractor_name,
      item.contractor_id,
      item.reason,
      item.b24_element_id,
      item.matched_point_ids?.join(' '),
      ...(item.change_set || []).flatMap((diff) => [diff.label, diff.current_value, diff.next_value]),
    ].join(' '),
  );

const matchesSearch = (item: ContractSyncQueueItemDTO, searchValue: string) => !searchValue || itemSearchText(item).includes(searchValue);

const blockedItemSearchText = (item: ContractSyncBlockedItemDTO) =>
  normalizeSearch(
    [
      item.service_point_name,
      item.service_point_code,
      item.contractor_name,
      item.contractor_id,
      item.reason,
      item.resolution_hint,
      item.matched_point_ids?.join(' '),
    ].join(' '),
  );

const matchesBlockedSearch = (item: ContractSyncBlockedItemDTO, searchValue: string) => !searchValue || blockedItemSearchText(item).includes(searchValue);

const buildErrorMap = (result?: ContractSyncExecuteResultDTO | null) => (result?.error_details || []).reduce<Record<string, string[]>>((acc, item) => {
  const key = String(item.key || '').trim();
  const msg = String(item.message || '').trim();
  if (key && msg) {
    if (!acc[key]) acc[key] = [];
    acc[key].push(msg);
  }
  return acc;
}, {});

const fieldBlock = (label: string, value?: string | number | null, strong = false) => (
  <Space orientation="vertical" size={0}>
    <Text type="secondary">{label}</Text>
    <Text strong={strong}>{displayValue(value)}</Text>
  </Space>
);

const renderDeleteRisk = (item: ContractSyncQueueItemDTO) => (
  <Space orientation="vertical" size={6}>
    <Space wrap>
      <Tag color={item.is_mapped ? 'blue' : 'orange'}>{item.is_mapped ? 'Есть сопоставление' : 'Без сопоставления'}</Tag>
      <Tag color={item.filled_fields ? 'gold' : 'default'}>Заполнено полей: {item.filled_fields ?? 0}</Tag>
      <Tag color={(item.matched_point_ids?.length || 0) > 1 ? 'volcano' : 'default'}>Дублей: {item.matched_point_ids?.length || 0}</Tag>
    </Space>
    <Text type="secondary" className="contract-sync-inline-text" title={buildDeleteDuplicateSummary(item) || '—'}>
      {buildDeleteDuplicateSummary(item) || '—'}
    </Text>
    <Text type="secondary" className="contract-sync-inline-text" title={buildDuplicateGroupSummary(item)}>
      {buildDuplicateGroupSummary(item)}
    </Text>
  </Space>
);

const renderQueueChangeSummary = (item: ContractSyncQueueItemDTO) => {
  if (item.action === 'delete') {
    return (
      <Space orientation="vertical" size={4}>
        <Text>Удаление дубля из Bitrix24</Text>
        <Space wrap size={[4, 4]}>
          <Tag color={item.is_mapped ? 'blue' : 'orange'}>{item.is_mapped ? 'Есть сопоставление' : 'Без сопоставления'}</Tag>
          <Tag color={item.filled_fields ? 'gold' : 'default'}>Полей: {item.filled_fields ?? 0}</Tag>
          <Tag color={(item.matched_point_ids?.length || 0) > 1 ? 'volcano' : 'default'}>Дублей: {item.matched_point_ids?.length || 0}</Tag>
        </Space>
        <Text type="secondary" className="contract-sync-inline-text" title={buildDuplicateGroupSummary(item)}>
          {buildDuplicateGroupSummary(item)}
        </Text>
      </Space>
    );
  }

  if (item.action === 'create') {
    return (
      <Space orientation="vertical" size={4}>
        <Text>Создание новой точки по данным отчёта</Text>
        {item.contractor_name ? (
          <Text type="secondary" className="contract-sync-inline-text" title={`Контрагент ${item.contractor_name}`}>
            Контрагент {item.contractor_name}
          </Text>
        ) : null}
      </Space>
    );
  }

  if (item.change_set?.length) {
    return (
      <Space orientation="vertical" size={4}>
        <Space wrap size={[4, 4]}>
          {item.change_set.map((diff) => (
            <Tag key={`${item.key}-${diff.field}`} color="blue">{diff.label}</Tag>
          ))}
        </Space>
        {item.change_set.slice(0, 2).map((diff) => {
          const summary = buildDiffSummary(diff);
          return (
            <Text key={`${item.key}-${diff.field}-summary`} className="contract-sync-inline-text" title={summary}>
              {summary}
            </Text>
          );
        })}
        {item.change_set.length > 2 ? <Text type="secondary">И ещё полей: {item.change_set.length - 2}</Text> : null}
      </Space>
    );
  }

  return <Text type="secondary">Изменения не детализированы</Text>;
};

const rowClassName = (item: ContractSyncQueueItemDTO | QueueRow) => {
  if ('has_execution_errors' in item && item.has_execution_errors) return 'contract-sync-row-error';
  if (item.action === 'create') return 'contract-sync-row-create';
  if (item.action === 'update') return 'contract-sync-row-update';
  if (item.action === 'delete') return 'contract-sync-row-delete';
  return '';
};

const ServicePointsImportPage: React.FC = () => {
  const { token } = antTheme.useToken();
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const goBack = useBackNavigation('/admin/synchronizations');
  const queryClient = useQueryClient();
  const { setHeaderConfig } = useLayoutHeader();
  const abortRef = useRef<AbortController | null>(null);

  const [activeTab, setActiveTab] = useState<PageTabKey>('sync');
  const [selectedUpsertKeys, setSelectedUpsertKeys] = useState<React.Key[]>([]);
  const [selectedDeleteKeys, setSelectedDeleteKeys] = useState<React.Key[]>([]);
  const [selectedQueueKeys, setSelectedQueueKeys] = useState<React.Key[]>([]);
  const [queueItems, setQueueItems] = useState<ContractSyncQueueItemDTO[]>([]);
  const [isExecuting, setIsExecuting] = useState(false);
  const [searchValue, setSearchValue] = useState('');
  const [upsertFilter, setUpsertFilter] = useState<UpsertFilter>('all');
  const [deleteFilter, setDeleteFilter] = useState<DeleteFilter>('all');
  const [queueFilter, setQueueFilter] = useState<QueueFilter>('all');
  const [isRefreshingContractSync, setIsRefreshingContractSync] = useState(false);
  const [lastExecutionResult, setLastExecutionResult] = useState<ContractSyncExecuteResultDTO | null>(null);
  const [queueErrorMap, setQueueErrorMap] = useState<Record<string, string[]>>({});
  const [selectedRunID, setSelectedRunID] = useState<string>();

  const deferredSearch = useDeferredValue(searchValue);
  const search = useMemo(() => normalizeSearch(deferredSearch), [deferredSearch]);

  const contractSyncQuery = useQuery({
    queryKey: ['bitrix', 'contract-sync-state'],
    queryFn: () => bitrixAdminApi.getContractSyncState(),
    staleTime: 30_000,
  });

  const selectedRunQuery = useQuery({
    queryKey: ['bitrix', 'contract-sync-run', selectedRunID],
    queryFn: () => bitrixAdminApi.getContractSyncRun(selectedRunID as string),
    enabled: Boolean(selectedRunID),
    staleTime: 30_000,
  });

  const syncState = contractSyncQuery.data?.data;
  const latestImport = syncState?.latest_import;
  const activeReportImport = syncState?.active_report_import;
  const activeReportImports = useMemo(
    () => syncState?.active_report_imports || (activeReportImport ? [activeReportImport] : []),
    [activeReportImport, syncState?.active_report_imports],
  );
  const recentImports = useMemo(() => syncState?.recent_imports || [], [syncState?.recent_imports]);
  const recentRuns = useMemo(() => syncState?.recent_runs || [], [syncState?.recent_runs]);
  const autoSync = syncState?.auto_sync;
  const blockedItems = useMemo(() => syncState?.blocked_items || [], [syncState?.blocked_items]);
  const baseUpsertItems = useMemo(() => syncState?.upsert_items || [], [syncState?.upsert_items]);
  const baseDeleteItems = useMemo(() => syncState?.delete_items || [], [syncState?.delete_items]);
  const selectedRun = selectedRunQuery.data?.data;
  const activeImportHashKey = useMemo(() => activeReportImports.map((item) => item.attachment_hash || item.id).join('|'), [activeReportImports]);

  const resetSyncScreenState = () => {
    setQueueItems([]);
    setSelectedUpsertKeys([]);
    setSelectedDeleteKeys([]);
    setSelectedQueueKeys([]);
    setLastExecutionResult(null);
    setQueueErrorMap({});
  };

  useEffect(() => {
    setQueueItems([]);
    setSelectedUpsertKeys([]);
    setSelectedDeleteKeys([]);
    setSelectedQueueKeys([]);
    setLastExecutionResult(null);
    setQueueErrorMap({});
  }, [activeImportHashKey]);

  useEffect(() => {
    if (recentRuns.length === 0) {
      setSelectedRunID(undefined);
      return;
    }
    setSelectedRunID((current) => (current && recentRuns.some((item) => item.id === current) ? current : recentRuns[0].id));
  }, [recentRuns]);

  const queueKeySet = useMemo(() => new Set(queueItems.map((item) => item.key)), [queueItems]);
  const upsertItems = useMemo(() => baseUpsertItems.filter((item) => !queueKeySet.has(item.key)), [baseUpsertItems, queueKeySet]);
  const deleteItems = useMemo(() => baseDeleteItems.filter((item) => !queueKeySet.has(item.key)), [baseDeleteItems, queueKeySet]);
  const filteredUpsertItems = useMemo(
    () => upsertItems.filter((item) => (upsertFilter === 'all' || item.action === upsertFilter) && matchesSearch(item, search)),
    [search, upsertFilter, upsertItems],
  );
  const filteredDeleteItems = useMemo(
    () => deleteItems.filter((item) => (deleteFilter !== 'mapped' || item.is_mapped) && (deleteFilter !== 'unmapped' || !item.is_mapped) && matchesSearch(item, search)),
    [deleteFilter, deleteItems, search],
  );
  const queueRows = useMemo<QueueRow[]>(
    () => queueItems.map((item, index) => ({
      ...item,
      queue_order: index + 1,
      execution_errors: queueErrorMap[item.key] || [],
      has_execution_errors: Boolean(queueErrorMap[item.key]?.length),
    })),
    [queueErrorMap, queueItems],
  );
  const filteredQueueItems = useMemo(
    () => queueRows.filter((item) => (queueFilter === 'all' || (queueFilter === 'errors' ? item.has_execution_errors : item.action === queueFilter)) && matchesSearch(item, search)),
    [queueFilter, queueRows, search],
  );
  const filteredBlockedItems = useMemo(
    () => blockedItems.filter((item) => matchesBlockedSearch(item, search)),
    [blockedItems, search],
  );
  const upsertVisibleStats = useMemo(() => countByAction(filteredUpsertItems), [filteredUpsertItems]);
  const upsertStats = useMemo(() => countByAction(upsertItems), [upsertItems]);
  const deleteMappedCount = useMemo(() => deleteItems.filter((item) => item.is_mapped).length, [deleteItems]);
  const deleteUnmappedCount = useMemo(() => deleteItems.filter((item) => !item.is_mapped).length, [deleteItems]);
  const queueStats = useMemo(() => countByAction(queueItems), [queueItems]);
  const queueErrorCount = useMemo(() => Object.values(queueErrorMap).reduce((sum, values) => sum + values.length, 0), [queueErrorMap]);
  const hasHeaderWarnings = Boolean(syncState?.blocked_rows || queueStats.delete || latestImport?.status === 'failed');

  const clearQueueState = () => {
    setQueueItems([]);
    setSelectedQueueKeys([]);
    setQueueErrorMap({});
  };

  const refreshContractSyncState = useCallback(async () => {
    if (isRefreshingContractSync) return;
    setIsRefreshingContractSync(true);
    try {
      const nextState = await bitrixAdminApi.refreshContractSyncState();
      resetSyncScreenState();
      queryClient.setQueryData<ApiResponse<ContractSyncStateDTO>>(['bitrix', 'contract-sync-state'], nextState);
      const attachmentName = nextState.data?.active_report_import?.attachment_name;
      message.success(attachmentName ? `Состояние пересчитано по отчёту: ${attachmentName}` : 'Состояние синхронизации пересчитано по текущему ящику');
    } catch (error) {
      message.error(getQueryErrorText(error));
    } finally {
      setIsRefreshingContractSync(false);
    }
  }, [isRefreshingContractSync, queryClient]);

  const removeQueueItemsByKeys = (keys: string[]) => {
    const keySet = new Set(keys);
    if (!keySet.size) return;
    setQueueItems((current) => current.filter((item) => !keySet.has(item.key)));
    setSelectedQueueKeys((current) => current.filter((key) => !keySet.has(String(key))));
    setQueueErrorMap((current) => {
      const next = { ...current };
      for (const key of keySet) delete next[key];
      return next;
    });
  };

  const addItemsToQueue = (items: ContractSyncQueueItemDTO[], selectedKeys: React.Key[]) => {
    const selectedSet = new Set(selectedKeys.map(String));
    const picked = items.filter((item) => selectedSet.has(item.key));
    if (!picked.length) return;
    setQueueItems((current) => {
      const existing = new Set(current.map((item) => item.key));
      return [...current, ...picked.filter((item) => !existing.has(item.key))];
    });
  };

  const executeQueue = async () => {
    if (isExecuting) {
      abortRef.current?.abort();
      return;
    }
    if (!queueItems.length) return;

    const controller = new AbortController();
    abortRef.current = controller;
    setIsExecuting(true);
    try {
      const result = await bitrixAdminApi.executeContractSync(
        { selected_keys: queueItems.map((item) => item.key), queue_items: queueItems },
        controller.signal,
      );
      const payload = result.data;
      setLastExecutionResult(payload);
      setQueueErrorMap(buildErrorMap(payload));

      const parts = [
        payload.created ? `создано ${payload.created}` : '',
        payload.updated ? `обновлено ${payload.updated}` : '',
        payload.deleted ? `удалено ${payload.deleted}` : '',
      ].filter(Boolean);
      const errorCount = payload.error_details?.length || payload.errors?.length || 0;
      if (errorCount > 0) {
        message.warning(`${parts.length ? parts.join(', ') : 'Очередь выполнена'}. Строк с ошибками: ${errorCount}`);
      } else {
        message.success(parts.length ? parts.join(', ') : 'Очередь выполнена');
      }

      const appliedKeys = new Set(payload.applied_keys || []);
      if (appliedKeys.size > 0) {
        setQueueItems((current) => current.filter((item) => !appliedKeys.has(item.key)));
        setSelectedQueueKeys((current) => current.filter((key) => !appliedKeys.has(String(key))));
        setQueueErrorMap((current) => {
          const next = { ...current };
          for (const key of appliedKeys) delete next[key];
          return next;
        });
        queryClient.setQueryData<ApiResponse<ContractSyncStateDTO>>(
          ['bitrix', 'contract-sync-state'],
          (current) => removeAppliedItemsFromState(current, appliedKeys),
        );
        await queryClient.invalidateQueries({ queryKey: ['bitrix', 'contract-sync-state'] });
      }
    } catch (error) {
      const axiosError = error as { code?: string; name?: string };
      if (axiosError.code === 'ERR_CANCELED' || axiosError.name === 'CanceledError') {
        message.info('Выполнение очереди остановлено');
      } else {
        message.error(getQueryErrorText(error));
      }
    } finally {
      abortRef.current = null;
      setIsExecuting(false);
    }
  };

  const importColumns = useMemo<ColumnsType<ContractMailImportDTO>>(
    () => [
      { title: 'Источник', dataIndex: 'source', key: 'source', width: 110, render: (value?: string) => importSourceTag(value) },
      { title: 'Статус', dataIndex: 'status', key: 'status', width: 120, render: (value: string) => importStatusTag(value) },
      { title: 'Вложение', dataIndex: 'attachment_name', key: 'attachment_name', ellipsis: true },
      { title: 'Строк', dataIndex: 'rows_count', key: 'rows_count', width: 90 },
      { title: 'Получено', dataIndex: 'received_at', key: 'received_at', width: 180, render: (value?: string) => formatDateTime(value) },
      { title: 'Обработано', dataIndex: 'processed_at', key: 'processed_at', width: 180, render: (value?: string) => formatDateTime(value) },
      { title: 'Хэш', dataIndex: 'attachment_hash', key: 'attachment_hash', width: 180, render: (value: string) => shortValue(value, 8) },
      { title: 'Ошибка', dataIndex: 'error_text', key: 'error_text', ellipsis: true, render: (value?: string) => value || '—' },
    ],
    [],
  );

  const runColumns = useMemo<ColumnsType<ContractSyncRunSummaryDTO>>(
    () => [
      {
        title: 'Когда и кем',
        key: 'started_at',
        width: 280,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Text strong>{formatDateTime(item.started_at)}</Text>
            <Text type="secondary">{buildSyncRunActorLabel(item)}</Text>
          </Space>
        ),
      },
      {
        title: 'Режим',
        key: 'mode',
        width: 180,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            {syncRunModeTag(item.mode)}
            {syncRunStatusTag(item.status)}
          </Space>
        ),
      },
      {
        title: 'Документы',
        key: 'active_imports',
        width: 260,
        render: (_, item) => (
          <Space wrap size={[4, 4]}>
            {(item.active_imports || []).length > 0
              ? (item.active_imports || []).map((report) => (
                <Tag key={`${item.id}-${report.attachment_hash || report.source || report.attachment_name}`}>
                  {(report.source || '—').toUpperCase()} • {report.rows_count}
                </Tag>
              ))
              : <Text type="secondary">—</Text>}
          </Space>
        ),
      },
      {
        title: 'Итог',
        key: 'summary',
        width: 280,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Text>{joinCompactParts(`Обработано ${item.processed}`, `Создано ${item.created}`, `Обновлено ${item.updated}`, `Удалено ${item.deleted}`)}</Text>
            {item.note ? <Text type="secondary" className="contract-sync-inline-text" title={item.note}>{item.note}</Text> : null}
          </Space>
        ),
      },
    ],
    [],
  );

  const historyQueueColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Операция',
        dataIndex: 'action',
        key: 'action',
        width: 160,
        render: (value: ContractSyncQueueItemDTO['action'], item) => (
          <Space orientation="vertical" size={4}>
            {actionTag(value)}
            {item.b24_element_id ? <Text type="secondary">B24 #{item.b24_element_id}</Text> : null}
          </Space>
        ),
      },
      {
        title: 'Точка',
        key: 'service_point_name',
        width: 280,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Text strong className="contract-sync-inline-text" title={item.service_point_name || '—'}>
              {item.service_point_name || '—'}
            </Text>
            <Text type="secondary" className="contract-sync-inline-text" title={buildQueueIdentitySummary(item) || '—'}>
              {buildQueueIdentitySummary(item) || '—'}
            </Text>
          </Space>
        ),
      },
      {
        title: 'Изменение',
        key: 'changes',
        width: 360,
        render: (_, item) => renderQueueChangeSummary(item),
      },
      {
        title: 'Причина',
        dataIndex: 'reason',
        key: 'reason',
        width: 260,
        render: (value?: string) => (
          <Text className="contract-sync-inline-text" title={value || '—'}>
            {value || '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  const upsertColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Операция',
        dataIndex: 'action',
        key: 'action',
        width: 180,
        sorter: (a, b) => actionOrder[a.action] - actionOrder[b.action],
        render: (value: ContractSyncQueueItemDTO['action'], item) => (
          <Space orientation="vertical" size={2}>
            {actionTag(value)}
            {item.b24_element_id ? <Text type="secondary">B24 #{item.b24_element_id}</Text> : <Text type="secondary">Новый элемент</Text>}
          </Space>
        ),
      },
      {
        title: 'Данные из отчёта',
        key: 'report_state',
        width: 340,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            {fieldBlock('Название', item.service_point_name, isChangedValue(item.service_point_name, item.current_name))}
            {fieldBlock('Код', item.service_point_code, isChangedValue(item.service_point_code, item.current_code))}
            {fieldBlock('Тип контракта', item.contract_type, isChangedValue(item.contract_type, item.current_contract_type))}
            {item.contractor_name ? <Text type="secondary">Контрагент: {item.contractor_name}</Text> : null}
          </Space>
        ),
      },
      {
        title: 'Текущее состояние',
        key: 'current_state',
        width: 340,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            {fieldBlock('Название', item.current_name, isChangedValue(item.current_name, item.service_point_name))}
            {fieldBlock('Код', item.current_code, isChangedValue(item.current_code, item.service_point_code))}
            {fieldBlock('Тип контракта', item.current_contract_type, isChangedValue(item.current_contract_type, item.contract_type))}
          </Space>
        ),
      },
    ],
    [],
  );

  const deleteColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Кандидат на удаление',
        key: 'delete_target',
        width: 420,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Text strong className="contract-sync-inline-text" title={item.service_point_name || '—'}>
              {item.service_point_name || '—'}
            </Text>
            <Text type="secondary" className="contract-sync-inline-text" title={buildDeleteDuplicateSummary(item) || '—'}>
              {buildDeleteDuplicateSummary(item) || '—'}
            </Text>
          </Space>
        ),
      },
      { title: 'Контекст', key: 'risk', width: 320, render: (_, item) => renderDeleteRisk(item) },
      {
        title: 'Причина удаления',
        dataIndex: 'reason',
        key: 'reason',
        width: 240,
        render: (value?: string) => (
          <Text className="contract-sync-inline-text" title={value || '—'}>
            {value || '—'}
          </Text>
        ),
      },
      {
        title: 'Действие',
        key: 'delete_action',
        width: 150,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            {actionTag(item.action)}
            {item.b24_element_id ? <Tag color="processing">B24 #{item.b24_element_id}</Tag> : <Text type="secondary">Без B24 ID</Text>}
          </Space>
        ),
      },
    ],
    [],
  );

  const blockedColumns = useMemo<ColumnsType<ContractSyncBlockedItemDTO>>(
    () => [
      {
        title: 'Строка отчёта',
        key: 'report_row',
        width: 340,
        render: (_, item) => {
          const summary = joinCompactParts(
            hasValue(item.service_point_code) ? `Код ${displayValue(item.service_point_code)}` : '',
            hasValue(item.contractor_name) ? `Контрагент ${displayValue(item.contractor_name)}` : '',
            hasValue(item.contractor_id) ? `ID ${displayValue(item.contractor_id)}` : '',
          );
          return (
            <Space orientation="vertical" size={4}>
              <Text strong className="contract-sync-inline-text" title={item.service_point_name || '—'}>
                {item.service_point_name || '—'}
              </Text>
              <Text type="secondary" className="contract-sync-inline-text" title={summary || '—'}>
                {summary || '—'}
              </Text>
            </Space>
          );
        },
      },
      {
        title: 'Причина блокировки',
        key: 'blocked_reason',
        width: 360,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Tag color="red">Требует ручной разбор</Tag>
            <Text className="contract-sync-inline-text" title={item.reason}>
              {item.reason}
            </Text>
            {item.matched_point_ids?.length ? (
              <Text type="secondary" className="contract-sync-inline-text" title={buildBlockedDuplicateSummary(item)}>
                {buildBlockedDuplicateSummary(item)}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: 'Что сделать',
        dataIndex: 'resolution_hint',
        key: 'resolution_hint',
        width: 340,
        render: (value?: string) => (
          <Text className="contract-sync-inline-text" title={value || '—'}>
            {value || '—'}
          </Text>
        ),
      },
    ],
    [],
  );

  const queueColumns = useMemo<ColumnsType<QueueRow>>(
    () => [
      {
        title: 'Порядок',
        dataIndex: 'queue_order',
        key: 'queue_order',
        width: 72,
        sorter: (a, b) => a.queue_order - b.queue_order,
      },
      {
        title: 'Операция',
        dataIndex: 'action',
        key: 'action',
        width: 150,
        render: (value: ContractSyncQueueItemDTO['action'], item) => (
          <Space orientation="vertical" size={4}>
            {actionTag(value)}
            {item.b24_element_id ? <Tag color="processing">B24 #{item.b24_element_id}</Tag> : <Tag>Новый</Tag>}
          </Space>
        ),
      },
      {
        title: 'Точка',
        key: 'identity',
        width: 280,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            <Text strong className="contract-sync-inline-text" title={item.service_point_name || '—'}>
              {item.service_point_name || '—'}
            </Text>
            <Text type="secondary" className="contract-sync-inline-text" title={buildQueueIdentitySummary(item) || '—'}>
              {buildQueueIdentitySummary(item) || '—'}
            </Text>
          </Space>
        ),
      },
      {
        title: 'Что произойдёт',
        key: 'changes',
        width: 360,
        render: (_, item) => renderQueueChangeSummary(item),
      },
      {
        title: 'Статус',
        key: 'status',
        width: 280,
        render: (_, item) => (
          <Space orientation="vertical" size={4}>
            {item.has_execution_errors ? <Tag color="red">Ошибка выполнения</Tag> : null}
            {item.reason ? (
              <Text type="secondary" className="contract-sync-inline-text" title={item.reason}>
                {item.reason}
              </Text>
            ) : (
              <Text type="secondary">Без замечаний</Text>
            )}
            {item.execution_errors.map((errorText, index) => (
              <Text key={`${item.key}-error-${index}`} type="danger" className="contract-sync-inline-text" title={errorText}>
                {errorText}
              </Text>
            ))}
          </Space>
        ),
      },
      {
        title: 'Вернуть',
        key: 'queue_action',
        width: 120,
        fixed: 'right',
        render: (_, item) => (
          <Button size="small" disabled={isExecuting} onClick={() => removeQueueItemsByKeys([item.key])}>
            Вернуть
          </Button>
        ),
      },
    ],
    [isExecuting],
  );

  const infoPopoverContent = useMemo(
    () => (
      <div style={{ display: 'grid', gap: 4 }}>
        <Text style={{ lineHeight: '20px' }}>Для расчёта очереди используется последний отчёт из почты, сохранённый после автосинхронизации или кнопки `Обновить`.</Text>
        <Text style={{ lineHeight: '20px' }}>Кнопка `Обновить` перечитывает текущий почтовый ящик, заново применяет последний отчёт и пересобирает очередь для Bitrix24.</Text>
        <Text style={{ lineHeight: '20px' }}>Контракты компаний в ServiceDesk обновляются автоматически по нему, а изменения Bitrix24 выполняются только вручную через очередь.</Text>
        <Text style={{ lineHeight: '20px' }}>Очередь исполняется строго по снимку UI: `update` не может незаметно превратиться в `create`.</Text>
        <Text style={{ lineHeight: '20px' }}>Если строка попала в блокировку, ниже появится отдельная таблица с причиной и подсказкой, что исправить.</Text>
        <Text style={{ lineHeight: '20px' }}>После частичного прогона успешные строки уйдут с экрана, а проблемные останутся в очереди с ошибками.</Text>
      </div>
    ),
    [],
  );

  const warningPopoverContent = useMemo(
    () => (
      <div style={{ display: 'grid', gap: 4 }}>
        {syncState?.blocked_rows ? (
          <Text style={{ lineHeight: '20px' }}>Заблокировано строк: {syncState.blocked_rows}. Для них нельзя выбрать безопасное действие автоматически, детали показаны в таблице ниже.</Text>
        ) : null}
        {queueStats.delete ? (
          <Text style={{ lineHeight: '20px' }}>В очереди сейчас {queueStats.delete} операций удаления. Перед запуском проверьте `B24ElementID`, сопоставление и дубль-группу.</Text>
        ) : null}
        {latestImport?.status === 'failed' ? (
          <Text style={{ lineHeight: '20px' }}>Последний почтовый прогон завершился ошибкой: {latestImport.error_text || 'подробности отсутствуют в журнале.'}</Text>
        ) : null}
        {!hasHeaderWarnings ? <Text style={{ lineHeight: '20px' }}>Критичных предупреждений сейчас нет.</Text> : null}
      </div>
    ),
    [hasHeaderWarnings, latestImport?.error_text, latestImport?.status, queueStats.delete, syncState?.blocked_rows],
  );

  const headerControls = useMemo(() => {
    const commonControls = (
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
        <Button
          icon={<ReloadOutlined />}
          loading={isRefreshingContractSync}
          disabled={isExecuting}
          onClick={() => void refreshContractSyncState()}
        >
          Обновить
        </Button>
        <HeaderHintButton icon={<QuestionCircleOutlined />} title="Как устроен экран" content={infoPopoverContent} active />
        <HeaderHintButton
          icon={<ExclamationCircleOutlined />}
          title="Предупреждения"
          content={warningPopoverContent}
          active={hasHeaderWarnings}
          danger
        />
      </div>
    );

    if (activeTab !== 'sync') {
      return commonControls;
    }

    return (
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr) minmax(280px, 420px) minmax(0, 1fr)',
          alignItems: 'center',
          gap: 8,
          width: '100%',
          maxWidth: 1080,
          minWidth: 0,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: 8, minWidth: 0 }}>
          <Select
            value={upsertFilter}
            onChange={(value) => setUpsertFilter(value as UpsertFilter)}
            options={[
              { value: 'all', label: `Изменения: ${upsertItems.length}` },
              { value: 'create', label: `Create: ${upsertStats.create}` },
              { value: 'update', label: `Update: ${upsertStats.update}` },
            ]}
            style={{ minWidth: 180, flexShrink: 0 }}
            popupMatchSelectWidth={false}
          />
          <Select
            value={deleteFilter}
            onChange={(value) => setDeleteFilter(value as DeleteFilter)}
            options={[
              { value: 'all', label: `Удаления: ${deleteItems.length}` },
              { value: 'mapped', label: `С сопоставлением: ${deleteMappedCount}` },
              { value: 'unmapped', label: `Без сопоставления: ${deleteUnmappedCount}` },
            ]}
            style={{ minWidth: 210, flexShrink: 0 }}
            popupMatchSelectWidth={false}
          />
          <Select
            value={queueFilter}
            onChange={(value) => setQueueFilter(value as QueueFilter)}
            options={[
              { value: 'all', label: `Очередь: ${queueItems.length}` },
              { value: 'create', label: `Create: ${queueStats.create}` },
              { value: 'update', label: `Update: ${queueStats.update}` },
              { value: 'delete', label: `Delete: ${queueStats.delete}` },
              { value: 'errors', label: `Ошибки: ${queueErrorCount}` },
            ]}
            style={{ minWidth: 160, flexShrink: 0 }}
            popupMatchSelectWidth={false}
          />
        </div>
        <Input
          allowClear
          placeholder="Поиск по точке, коду, типу, компании или B24 ID"
          value={searchValue}
          onChange={(event) => setSearchValue(event.target.value)}
          style={{
            width: '100%',
            minWidth: 0,
          }}
        />
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-start', minWidth: 0 }}>
          {commonControls}
        </div>
      </div>
    );
  }, [
    activeTab,
    deleteMappedCount,
    deleteFilter,
    deleteItems.length,
    deleteUnmappedCount,
    hasHeaderWarnings,
    infoPopoverContent,
    isExecuting,
    isRefreshingContractSync,
    queueErrorCount,
    queueFilter,
    queueItems.length,
    refreshContractSyncState,
    queueStats.create,
    queueStats.delete,
    queueStats.update,
    searchValue,
    upsertFilter,
    upsertItems.length,
    upsertStats.create,
    upsertStats.update,
    warningPopoverContent,
  ]);

  useEffect(() => {
    if (!isBitrixEnabled) {
      setHeaderConfig(null);
      return undefined;
    }

    setHeaderConfig({
      mode: 'page-controls',
      title: 'Ручная синхронизация',
      controls: headerControls,
    });
    return () => setHeaderConfig(null);
  }, [headerControls, isBitrixEnabled, setHeaderConfig]);

  if (!isBitrixEnabled) {
      return (
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Button icon={<ArrowLeftOutlined />} onClick={goBack}>Назад</Button>
          <Alert
            type="warning"
            showIcon
          title="Интеграция Bitrix24 отключена"
          description="Экран ручной синхронизации недоступен, пока ENABLE_BITRIX_GATEWAY=false."
        />
      </Space>
    );
  }

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%', marginTop: -16 }}>
      <style>{`
        .contract-sync-row-create td{background:${token.colorSuccessBg}}
        .contract-sync-row-update td{background:${token.colorInfoBg}}
        .contract-sync-row-delete td{background:${token.colorWarningBg}}
        .contract-sync-row-error td{background:${token.colorErrorBg}!important}
        .contract-sync-table .ant-table-tbody > tr > td{vertical-align:top}
        .contract-sync-inline-text{display:block;max-width:100%;overflow:hidden;white-space:nowrap;text-overflow:ellipsis}
      `}</style>

      {contractSyncQuery.isError ? (
        <Alert
          type="error"
          showIcon
          title="Не удалось загрузить состояние синхронизации"
          description={getQueryErrorText(contractSyncQuery.error)}
        />
      ) : null}

      {lastExecutionResult ? (
        <Alert
          type={(lastExecutionResult.error_details?.length || lastExecutionResult.errors?.length || 0) > 0 ? 'warning' : 'success'}
          showIcon
          title="Результат последнего запуска"
          description={`Создано ${lastExecutionResult.created}, обновлено ${lastExecutionResult.updated}, удалено ${lastExecutionResult.deleted}, обработано ${lastExecutionResult.processed}. Ошибок по строкам: ${lastExecutionResult.error_details?.length || 0}.`}
        />
      ) : null}

      <Tabs
        activeKey={activeTab}
        destroyOnHidden
        onChange={(key) => setActiveTab(key as PageTabKey)}
        tabBarExtraContent={{
          left: (
            <div style={{ marginInlineEnd: 16 }}>
              <Button icon={<ArrowLeftOutlined />} onClick={goBack}>
                Назад
              </Button>
            </div>
          ),
        }}
        items={[
          {
            key: 'sync',
            label: 'Синхронизация',
            children: (
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Row gutter={[16, 16]}>
                  <Col xs={12} md={8} xl={4}><Card className="glass-panel"><Statistic title="К созданию" value={syncState?.to_create || 0} /></Card></Col>
                  <Col xs={12} md={8} xl={4}><Card className="glass-panel"><Statistic title="К обновлению" value={syncState?.to_update || 0} /></Card></Col>
                  <Col xs={12} md={8} xl={4}><Card className="glass-panel"><Statistic title="К удалению" value={syncState?.to_delete || 0} /></Card></Col>
                  <Col xs={12} md={8} xl={4}><Card className="glass-panel"><Statistic title="Заблокировано" value={syncState?.blocked_rows || 0} /></Card></Col>
                  <Col xs={24} md={8} xl={8}>
                    <Card className="glass-panel">
                      <Statistic
                        title="Сейчас в очереди"
                        value={queueItems.length}
                        suffix={<Text type="secondary">создать {queueStats.create} / обновить {queueStats.update} / удалить {queueStats.delete}</Text>}
                      />
                    </Card>
                  </Col>
                </Row>

                <Card className="glass-panel" title={`Заблокированные строки${blockedItems.length ? ` (${blockedItems.length})` : ''}`}>
                  {blockedItems.length === 0 ? (
                    <Alert
                      type="success"
                      showIcon
                      title="Заблокированных строк нет"
                      description="Все строки последнего отчёта либо попали в очередь изменений, либо уже актуальны."
                    />
                  ) : (
                    <Table<ContractSyncBlockedItemDTO>
                      className="contract-sync-table"
                      size="small"
                      rowKey="key"
                      dataSource={filteredBlockedItems}
                      columns={blockedColumns}
                      loading={contractSyncQuery.isLoading}
                      pagination={{ pageSize: 8, hideOnSinglePage: true }}
                      scroll={{ x: 980 }}
                      locale={{ emptyText: search ? 'По текущему поиску среди заблокированных строк совпадений нет' : 'Заблокированные строки отсутствуют' }}
                    />
                  )}
                </Card>

                <Card
                  className="glass-panel"
                  title="Обновления и создания точек обслуживания"
                  extra={(
                    <Button
                      type="primary"
                      disabled={isExecuting || selectedUpsertKeys.length === 0}
                      onClick={() => {
                        addItemsToQueue(filteredUpsertItems, selectedUpsertKeys);
                        const moved = new Set(filteredUpsertItems.filter((item) => selectedUpsertKeys.includes(item.key)).map((item) => item.key));
                        setSelectedUpsertKeys((current) => current.filter((key) => !moved.has(String(key))));
                      }}
                    >
                      Добавить выбранное
                    </Button>
                  )}
                >
                  {upsertItems.length === 0 ? (
                    <Alert type="info" showIcon title="Новых изменений нет" description="Последний отчёт не требует создания или обновления точек в Bitrix24." />
                  ) : (
                    <Table<ContractSyncQueueItemDTO>
                      className="contract-sync-table"
                      size="small"
                      rowKey="key"
                      rowClassName={rowClassName}
                      dataSource={filteredUpsertItems}
                      columns={upsertColumns}
                      loading={contractSyncQuery.isLoading}
                      rowSelection={{
                        selectedRowKeys: selectedUpsertKeys,
                        preserveSelectedRowKeys: true,
                        onChange: setSelectedUpsertKeys,
                        getCheckboxProps: () => ({ disabled: isExecuting }),
                      }}
                      pagination={{ pageSize: 8, hideOnSinglePage: true }}
                      scroll={{ x: 880 }}
                      locale={{ emptyText: search ? 'По текущему поиску и фильтрам совпадений нет' : 'Нет строк для выбранного режима' }}
                    />
                  )}
                </Card>

                <Card
                  className="glass-panel"
                  title="Удаление неактуальных дублей"
                  extra={(
                    <Button
                      type="primary"
                      danger
                      disabled={isExecuting || selectedDeleteKeys.length === 0}
                      onClick={() => {
                        addItemsToQueue(filteredDeleteItems, selectedDeleteKeys);
                        const moved = new Set(filteredDeleteItems.filter((item) => selectedDeleteKeys.includes(item.key)).map((item) => item.key));
                        setSelectedDeleteKeys((current) => current.filter((key) => !moved.has(String(key))));
                      }}
                    >
                      Добавить выбранное
                    </Button>
                  )}
                >
                  {deleteItems.length === 0 ? (
                    <Alert
                      type="success"
                      showIcon
                      title="Неактуальных дублей нет"
                      description="Сейчас в Bitrix24 нет дублей точек обслуживания, подпадающих под правила ручного удаления."
                    />
                  ) : (
                    <Table<ContractSyncQueueItemDTO>
                      className="contract-sync-table"
                      size="small"
                      rowKey="key"
                      rowClassName={rowClassName}
                      dataSource={filteredDeleteItems}
                      columns={deleteColumns}
                      loading={contractSyncQuery.isLoading}
                      rowSelection={{
                        selectedRowKeys: selectedDeleteKeys,
                        preserveSelectedRowKeys: true,
                        onChange: setSelectedDeleteKeys,
                        getCheckboxProps: () => ({ disabled: isExecuting }),
                      }}
                      pagination={{ pageSize: 8, hideOnSinglePage: true }}
                      scroll={{ x: 1130 }}
                      locale={{ emptyText: search ? 'По текущему поиску и фильтрам совпадений нет' : 'Нет строк для выбранного режима' }}
                    />
                  )}
                </Card>

                <Card
                  className="glass-panel"
                  title="Очередь выполнения"
                  extra={(
                    <Space wrap>
                      <Button
                        type="primary"
                        disabled={isExecuting || upsertVisibleStats.create === 0}
                        onClick={() => addItemsToQueue(
                          filteredUpsertItems.filter((item) => item.action === 'create'),
                          filteredUpsertItems.filter((item) => item.action === 'create').map((item) => item.key),
                        )}
                      >
                        Добавить все create ({upsertVisibleStats.create})
                      </Button>
                      <Button
                        type="primary"
                        disabled={isExecuting || upsertVisibleStats.update === 0}
                        onClick={() => addItemsToQueue(
                          filteredUpsertItems.filter((item) => item.action === 'update'),
                          filteredUpsertItems.filter((item) => item.action === 'update').map((item) => item.key),
                        )}
                      >
                        Добавить все update ({upsertVisibleStats.update})
                      </Button>
                      <Button
                        danger
                        disabled={isExecuting || filteredDeleteItems.length === 0}
                        onClick={() => addItemsToQueue(filteredDeleteItems, filteredDeleteItems.map((item) => item.key))}
                      >
                        Добавить все delete ({filteredDeleteItems.length})
                      </Button>
                      <Button icon={<ClearOutlined />} disabled={isExecuting || queueItems.length === 0} onClick={clearQueueState}>
                        Очистить очередь
                      </Button>
                      <Button
                        disabled={isExecuting || queueErrorCount === 0}
                        onClick={() => {
                          const failedKeys = new Set(Object.keys(queueErrorMap).filter((key) => queueErrorMap[key]?.length));
                          if (!failedKeys.size) {
                            message.info('В очереди нет строк с ошибками выполнения');
                            return;
                          }
                          setQueueItems((current) => current.filter((item) => failedKeys.has(item.key)));
                          setSelectedQueueKeys((current) => current.filter((key) => failedKeys.has(String(key))));
                        }}
                      >
                        Оставить только ошибки ({queueErrorCount})
                      </Button>
                      {isExecuting || queueStats.delete === 0 ? (
                        <Button
                          icon={isExecuting ? <StopOutlined /> : <PlayCircleOutlined />}
                          type="primary"
                          disabled={!isExecuting && queueItems.length === 0}
                          onClick={() => void executeQueue()}
                        >
                          {isExecuting ? 'Стоп' : 'Пуск'}
                        </Button>
                      ) : (
                        <Popconfirm
                          title="В очереди есть удаление"
                          description={`Будет удалено ${queueStats.delete} строк. Запустить очередь?`}
                          okText="Да, запустить"
                          cancelText="Отмена"
                          onConfirm={() => void executeQueue()}
                        >
                          <Button icon={<PlayCircleOutlined />} type="primary" disabled={queueItems.length === 0}>Пуск</Button>
                        </Popconfirm>
                      )}
                      <Button disabled={isExecuting || selectedQueueKeys.length === 0} onClick={() => removeQueueItemsByKeys(selectedQueueKeys.map(String))}>
                        Отменить выбранное
                      </Button>
                    </Space>
                  )}
                >
                  {queueItems.length === 0 ? (
                    <Empty description="Очередь выполнения пуста" />
                  ) : (
                    <Table<QueueRow>
                      className="contract-sync-table"
                      size="small"
                      rowKey="key"
                      rowClassName={rowClassName}
                      dataSource={filteredQueueItems}
                      columns={queueColumns}
                      rowSelection={{
                        selectedRowKeys: selectedQueueKeys,
                        preserveSelectedRowKeys: true,
                        onChange: setSelectedQueueKeys,
                        getCheckboxProps: () => ({ disabled: isExecuting }),
                      }}
                      pagination={{ pageSize: 8, hideOnSinglePage: true }}
                      scroll={{ x: 1140 }}
                      locale={{ emptyText: search ? 'По текущему поиску и фильтрам в очереди совпадений нет' : 'Очередь выполнения пуста' }}
                    />
                  )}
                </Card>
              </Space>
            ),
          },
          {
            key: 'history',
            label: 'История и отчёт',
            children: (
              <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                <Card className="glass-panel" title="Активные документы и автозапуск">
                  <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                    {latestImport?.status === 'failed' ? (
                      <Alert
                        type="error"
                        showIcon
                        title="Последний почтовый прогон завершился ошибкой"
                        description={latestImport.error_text || 'Подробности ошибки отсутствуют в журнале импорта.'}
                      />
                    ) : null}
                    <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
                      <Descriptions.Item label="Автоприменение">{autoSync?.enabled ? 'Включено' : 'Отключено'}</Descriptions.Item>
                      <Descriptions.Item label="Интервал">{autoSync?.interval_minutes ? `${autoSync.interval_minutes} мин` : '—'}</Descriptions.Item>
                      <Descriptions.Item label="Режим">{buildAutoSyncModeLabel(autoSync)}</Descriptions.Item>
                      <Descriptions.Item label="Триггер">{autoSync?.trigger_label || '—'}</Descriptions.Item>
                      <Descriptions.Item label="Безопасность" span={2}>{autoSync?.safety_description || '—'}</Descriptions.Item>
                    </Descriptions>
                    {activeReportImports.length > 0 ? (
                      <Row gutter={[16, 16]}>
                        {activeReportImports.map((report) => (
                          <Col key={`${report.attachment_hash || report.id}-${report.source || 'source'}`} xs={24} md={12}>
                            <Card size="small" title={<Space>{importSourceTag(report.source)}<span>{report.attachment_name || '—'}</span></Space>}>
                              <Descriptions column={1} size="small" bordered>
                                <Descriptions.Item label="Статус">{importStatusTag(report.status)}</Descriptions.Item>
                                <Descriptions.Item label="Получено">{formatDateTime(report.received_at)}</Descriptions.Item>
                                <Descriptions.Item label="Обработано">{formatDateTime(report.processed_at)}</Descriptions.Item>
                                <Descriptions.Item label="Точек">{report.rows_count}</Descriptions.Item>
                                <Descriptions.Item label="Хэш">{shortValue(report.attachment_hash, 8)}</Descriptions.Item>
                                <Descriptions.Item label="Message-ID">{report.message_id || '—'}</Descriptions.Item>
                              </Descriptions>
                            </Card>
                          </Col>
                        ))}
                      </Row>
                    ) : (
                      <Empty description="Нет успешно обработанных активных документов" />
                    )}
                  </Space>
                </Card>

                <Card
                  className="glass-panel"
                  title="Журнал применений"
                  extra={(
                    <Select
                      value={selectedRunID}
                      onChange={setSelectedRunID}
                      style={{ minWidth: 360 }}
                      placeholder="Выберите дату и время применения"
                      options={recentRuns.map((item) => ({ value: item.id, label: buildSyncRunOptionLabel(item) }))}
                    />
                  )}
                >
                  <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                    {selectedRun ? (
                      <Card size="small" title="Выбранный прогон">
                        <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
                          <Descriptions.Item label="Статус">{syncRunStatusTag(selectedRun.status)}</Descriptions.Item>
                          <Descriptions.Item label="Режим">{syncRunModeTag(selectedRun.mode)}</Descriptions.Item>
                          <Descriptions.Item label="Запущен">{formatDateTime(selectedRun.started_at)}</Descriptions.Item>
                          <Descriptions.Item label="Завершён">{formatDateTime(selectedRun.completed_at)}</Descriptions.Item>
                          <Descriptions.Item label="Инициатор">{buildSyncRunActorLabel(selectedRun)}</Descriptions.Item>
                          <Descriptions.Item label="Обработано">{selectedRun.processed}</Descriptions.Item>
                          <Descriptions.Item label="Создано">{selectedRun.created}</Descriptions.Item>
                          <Descriptions.Item label="Обновлено">{selectedRun.updated}</Descriptions.Item>
                          <Descriptions.Item label="Удалено">{selectedRun.deleted}</Descriptions.Item>
                          <Descriptions.Item label="Заблокировано">{selectedRun.blocked_rows}</Descriptions.Item>
                          <Descriptions.Item label="Документы" span={2}>
                            <Space wrap size={[4, 4]}>
                              {(selectedRun.active_imports || []).map((report) => (
                                <Tag key={`${selectedRun.id}-${report.attachment_hash || report.source || report.attachment_name}`}>
                                  {(report.source || '—').toUpperCase()} • {report.attachment_name}
                                </Tag>
                              ))}
                            </Space>
                          </Descriptions.Item>
                          <Descriptions.Item label="Комментарий" span={2}>{selectedRun.note || '—'}</Descriptions.Item>
                        </Descriptions>
                      </Card>
                    ) : selectedRunID && selectedRunQuery.isLoading ? (
                      <Alert type="info" showIcon title="Загружаю детали выбранного прогона" />
                    ) : (
                      <Empty description="История применений пока пуста" />
                    )}

                    {selectedRun?.queue_items?.length ? (
                      <Card size="small" title={`Изменения в выбранный прогон (${selectedRun.queue_items.length})`}>
                        <Table<ContractSyncQueueItemDTO>
                          rowKey="key"
                          dataSource={selectedRun.queue_items}
                          columns={historyQueueColumns}
                          pagination={{ pageSize: 8, hideOnSinglePage: true }}
                          scroll={{ x: 1080 }}
                          locale={{ emptyText: 'В выбранном прогоне не было изменений' }}
                        />
                      </Card>
                    ) : null}

                    {selectedRun?.errors?.length ? (
                      <Alert
                        type="warning"
                        showIcon
                        title="В выбранном прогоне были ошибки"
                        description={selectedRun.errors.join(' | ')}
                      />
                    ) : null}

                    <Table<ContractSyncRunSummaryDTO>
                      rowKey="id"
                      dataSource={recentRuns}
                      columns={runColumns}
                      loading={contractSyncQuery.isLoading}
                      pagination={{ pageSize: 8, hideOnSinglePage: true }}
                      scroll={{ x: 980 }}
                      locale={{ emptyText: 'История применений пуста' }}
                      onRow={(record) => ({
                        onClick: () => setSelectedRunID(record.id),
                        style: { cursor: 'pointer' },
                      })}
                    />
                  </Space>
                </Card>

                <Card className="glass-panel" title="История почтовых импортов" extra={<Text type="secondary">Показываются последние 20 прогонов</Text>}>
                  <Table<ContractMailImportDTO>
                    rowKey="id"
                    dataSource={recentImports}
                    columns={importColumns}
                    loading={contractSyncQuery.isLoading}
                    pagination={{ pageSize: 10, hideOnSinglePage: true }}
                    scroll={{ x: 1080 }}
                    locale={{ emptyText: 'История импортов пуста' }}
                  />
                </Card>
              </Space>
            ),
          },
        ]}
      />
    </Space>
  );
};

export default ServicePointsImportPage;
