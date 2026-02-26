import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AutoComplete, Button, Checkbox, DatePicker, Grid, Input, Popover, Segmented, Select, Space, Switch, Typography, message } from 'antd';
import { PlusOutlined, SettingOutlined } from '@ant-design/icons';
import dayjs, { Dayjs } from 'dayjs';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/store/authStore';
import { useTicketParamsStore } from '@/store/ticketParamsStore';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';
import { TICKET_ACTIVE_STATUS_VALUES, TICKET_STATUS_OPTIONS } from '@/constants/ticketStatus';

const { useBreakpoint } = Grid;
const { Text } = Typography;

const LONGEST_STATUS_LABEL_WIDTH = 260;
const VIEW_SELECT_WIDTH = LONGEST_STATUS_LABEL_WIDTH / 2;
const TABLE_COLUMN_OPTIONS = [
  { value: 'selection', label: 'Выбор' },
  { value: 'number', label: 'Номер' },
  { value: 'status', label: 'Статус' },
  { value: 'company_display', label: 'Компания' },
  { value: 'assignee_display', label: 'Исполнитель' },
  { value: 'reporter_display', label: 'Автор' },
  { value: 'subject', label: 'Описание' },
  { value: 'bitrix_deal_title', label: 'Заголовок Bitrix24' },
  { value: 'last_comment', label: 'Последний комментарий' },
  { value: 'created_at', label: 'Создано' },
  { value: 'last_activity', label: 'Обновлено' },
  { value: 'sync_with_bitrix', label: 'B24' },
];
const TABLE_COLUMN_KEYS = TABLE_COLUMN_OPTIONS.map((item) => item.value);
const DEFAULT_TABLE_COLUMN_KEYS = TABLE_COLUMN_KEYS.filter((key) => key !== 'bitrix_deal_title' && key !== 'selection');
const TICKET_STATE_PARAM_KEYS = [
  'preset_id',
  'q',
  'view',
  'status',
  'only_active_statuses',
  'table_columns',
  'table_sort',
  'assignee_ids',
  'archive_mode',
  'company',
  'archive_company',
  'period_from',
  'period_to',
  'archive_period_from',
  'archive_period_to',
] as const;
type TicketStateParamKey = (typeof TICKET_STATE_PARAM_KEYS)[number];
const TICKET_PRESET_PARAM_KEYS = [
  'view',
  'archive_mode',
  'status',
  'only_active_statuses',
  'table_columns',
  'table_sort',
  'assignee_ids',
  'company',
  'archive_company',
  'period_from',
  'period_to',
  'archive_period_from',
  'archive_period_to',
] as const;
type TicketPresetParamKey = (typeof TICKET_PRESET_PARAM_KEYS)[number];

type TicketPreset = {
  id: string;
  name: string;
  values: Partial<Record<TicketPresetParamKey, string>>;
};

const HeaderSearch: React.FC = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const currentTerm = searchParams.get('term') || '';
  const showInactive = ['1', 'true', 'yes', 'on'].includes((searchParams.get('show_inactive') || '').toLowerCase());
  const [searchTerm, setSearchTerm] = useState(currentTerm);
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const isBitrixEnabled = user?.bitrix_enabled === true;

  useEffect(() => {
    setSearchTerm(currentTerm);
  }, [currentTerm]);

  const onGlobalSearch = (value: string) => {
    const trimmed = value.trim();
    if (!trimmed) return;
    const params = new URLSearchParams();
    params.set('term', trimmed);
    if (showInactive) {
      params.set('show_inactive', '1');
    }
    navigate(`/search?${params.toString()}`);
  };

  const onToggleShowInactive = (nextValue: boolean) => {
    const params = new URLSearchParams(searchParams);
    if (nextValue) {
      params.set('show_inactive', '1');
    } else {
      params.delete('show_inactive');
    }
    if (currentTerm || searchTerm) {
      params.set('term', (searchTerm || currentTerm).trim());
      navigate(`/search?${params.toString()}`);
    } else if (location.pathname.startsWith('/search')) {
      navigate(`/search?${params.toString()}`);
    }
  };

  const isTicketsPage = location.pathname.startsWith('/tickets');
  const isTicketsListPage = location.pathname === '/tickets';
  const isCompaniesPage = location.pathname.startsWith('/companies');
  const isServersPage = location.pathname === '/servers';
  const isWorkstationsPage = location.pathname === '/workstations';
  const isFiscalsPage = location.pathname === '/fiscals';
  const isAgentObservationsPage = location.pathname === '/agent-observations';
  const isSectionSearchPage = isCompaniesPage || isServersPage || isWorkstationsPage || isFiscalsPage;
  const screens = useBreakpoint();
  const isCompact = !screens.xl;
  const isHeaderNarrow = !screens.xxl;
  const isHeaderMobile = !screens.md;

  const ticketParamsRaw = useTicketParamsStore((state) => state.ticketParams);
  const setTicketParamsRaw = useTicketParamsStore((state) => state.setTicketParams);
  const requestCreateTicket = useTicketParamsStore((state) => state.requestCreateTicket);
  const selectedTicketIDs = useTicketParamsStore((state) => state.selectedTicketIDs);
  const clearSelectedTicketIDs = useTicketParamsStore((state) => state.clearSelectedTicketIDs);
  const ticketParams = useMemo(() => new URLSearchParams(ticketParamsRaw), [ticketParamsRaw]);
  const [ticketTerm, setTicketTerm] = useState(ticketParams.get('q') || '');
  const [presetName, setPresetName] = useState('');
  const appliedSearch = ticketParams.get('q') || '';
  const ticketStatus = ticketParams.get('status') || '';
  const selectedPresetID = ticketParams.get('preset_id') || undefined;
  const onlyActiveStatuses = ticketParams.get('only_active_statuses') === '1';
  const ticketView = ticketParams.get('view') || 'list';
  const ticketTableColumns = ticketParams.get('table_columns') || '';
  const ticketAssigneeIDs = ticketParams.get('assignee_ids') || '';
  const archiveMode = ticketParams.get('archive_mode') === 'archive' ? 'archive' : 'active';
  const activeCompany = ticketParams.get('company') || undefined;
  const archiveCompany = ticketParams.get('archive_company') || undefined;
  const ticketCompany = archiveMode === 'archive' ? archiveCompany : activeCompany;
  const activePeriodFrom = ticketParams.get('period_from') || '';
  const activePeriodTo = ticketParams.get('period_to') || '';
  const archivePeriodFrom = ticketParams.get('archive_period_from') || '';
  const archivePeriodTo = ticketParams.get('archive_period_to') || '';
  const periodFrom = archiveMode === 'archive' ? archivePeriodFrom : activePeriodFrom;
  const periodTo = archiveMode === 'archive' ? archivePeriodTo : activePeriodTo;
  const companyParamKey = archiveMode === 'archive' ? 'archive_company' : 'company';
  const periodFromParamKey = archiveMode === 'archive' ? 'archive_period_from' : 'period_from';
  const periodToParamKey = archiveMode === 'archive' ? 'archive_period_to' : 'period_to';

  useEffect(() => {
    if (!isTicketsPage || !location.search) {
      return;
    }
    navigate(location.pathname, { replace: true });
  }, [isTicketsPage, location.pathname, location.search, navigate]);

  useEffect(() => {
    if (!isTicketsPage && selectedTicketIDs.length > 0) {
      clearSelectedTicketIDs();
    }
  }, [clearSelectedTicketIDs, isTicketsPage, selectedTicketIDs.length]);

  useEffect(() => {
    setTicketTerm(new URLSearchParams(ticketParamsRaw).get('q') || '');
  }, [ticketParamsRaw]);

  const statusValues = useMemo(() => (ticketStatus ? ticketStatus.split(',').filter(Boolean) : []), [ticketStatus]);
  const effectiveStatusValues = useMemo(() => {
    if (archiveMode === 'archive') {
      return [];
    }
    if (!onlyActiveStatuses) {
      return statusValues;
    }
    const filtered = statusValues.filter(
      (value): value is (typeof TICKET_ACTIVE_STATUS_VALUES)[number] =>
        TICKET_ACTIVE_STATUS_VALUES.includes(value as (typeof TICKET_ACTIVE_STATUS_VALUES)[number]),
    );
    return filtered.length ? filtered : TICKET_ACTIVE_STATUS_VALUES;
  }, [archiveMode, onlyActiveStatuses, statusValues]);
  const assigneeValues = useMemo(() => (ticketAssigneeIDs ? ticketAssigneeIDs.split(',').filter(Boolean) : []), [ticketAssigneeIDs]);
  const ownAssigneeID = user?.id ? String(user.id) : '';
  const isMineOnly = Boolean(ownAssigneeID) && assigneeValues.length === 1 && assigneeValues[0] === ownAssigneeID;
  const selectedTableColumns = useMemo(() => {
    const availableTableColumnKeys = isBitrixEnabled
      ? TABLE_COLUMN_KEYS
      : TABLE_COLUMN_KEYS.filter((key) => key !== 'bitrix_deal_title' && key !== 'sync_with_bitrix');
    const defaultTableColumns = isBitrixEnabled
      ? DEFAULT_TABLE_COLUMN_KEYS
      : DEFAULT_TABLE_COLUMN_KEYS.filter((key) => key !== 'bitrix_deal_title' && key !== 'sync_with_bitrix');
    if (!ticketTableColumns) {
      return defaultTableColumns;
    }
    const values = ticketTableColumns.split(',').filter((value) => availableTableColumnKeys.includes(value));
    return values.length ? values : defaultTableColumns;
  }, [isBitrixEnabled, ticketTableColumns]);

  const tableColumnOptions = useMemo(
    () => (isBitrixEnabled ? TABLE_COLUMN_OPTIONS : TABLE_COLUMN_OPTIONS.filter((item) => item.value !== 'bitrix_deal_title' && item.value !== 'sync_with_bitrix')),
    [isBitrixEnabled],
  );
  const tableColumnOrder = useMemo(
    () => tableColumnOptions.map((item) => item.value),
    [tableColumnOptions],
  );

  const { data: filterRes, isFetching: isFiltersLoading } = useQuery({
    queryKey: ['ticket-filters', archiveMode, appliedSearch, effectiveStatusValues, periodFrom, periodTo, onlyActiveStatuses],
    queryFn: () =>
      ticketsApi.getTicketFilters({
        archive_mode: archiveMode,
        search: appliedSearch || undefined,
        status: archiveMode === 'archive' ? undefined : (effectiveStatusValues.length ? effectiveStatusValues : undefined),
        period_from: periodFrom || undefined,
        period_to: periodTo || undefined,
      }),
    staleTime: 30_000,
    enabled: isTicketsPage,
  });

  const { data: assigneesRes } = useQuery({
    queryKey: ['ticket-assignees'],
    queryFn: () => usersApi.getAssignees(),
    enabled: isTicketsPage && archiveMode !== 'archive',
    staleTime: 60_000,
  });

  const updateProfileMutation = useMutation({
    mutationFn: async (config: Record<string, unknown>) => profileApi.updateConfig({ profile_config: config }),
  });
  const persistTicketStateMutation = useMutation({
    mutationFn: async (config: Record<string, unknown>) => profileApi.updateConfig({ profile_config: config }),
  });

  const companyOptions = useMemo(() => {
    const list = filterRes?.data?.companies || [];
    const renderLabel = (title: string, parentTitle?: string) => {
      const parts = getCompanyHierarchyParts(title, parentTitle);
      if (!parts.hasParent) {
        return parts.child;
      }
      return (
        <Space direction="vertical" size={0} style={{ lineHeight: 1.2 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>{parts.parent}</Text>
          <Text style={{ paddingLeft: 14 }}>{parts.child}</Text>
        </Space>
      );
    };
    return list.map((company) => ({
      value: company.id,
      selectedLabel: company.name || company.id,
      label: (
        <Space style={{ width: '100%', justifyContent: 'space-between' }}>
          {renderLabel(company.name || company.id, company.parent_name)}
          <Text type="secondary">({company.count})</Text>
        </Space>
      ),
      searchText: `${company.parent_name || ''} ${company.name || company.id}`.trim().toLowerCase(),
    }));
  }, [filterRes]);

  const assigneeOptions = useMemo(
    () => (assigneesRes?.data || []).map((item) => ({
      value: String(item.id),
      label: item.full_name || item.username || `ID ${item.id}`,
    })),
    [assigneesRes],
  );

  const bulkAssignMutation = useMutation({
    mutationFn: async (payload: { ids: string[]; assigneeID: number }) => {
      await Promise.all(payload.ids.map((id) => ticketsApi.assign(id, payload.assigneeID)));
    },
    onSuccess: () => {
      message.success('Исполнитель назначен');
      clearSelectedTicketIDs();
      void queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось выполнить массовое назначение'),
  });

  const presets = useMemo<TicketPreset[]>(() => {
    const raw = (user?.profile_config as { tickets?: { filters?: { presets?: TicketPreset[] } } } | undefined)?.tickets?.filters?.presets;
    if (!Array.isArray(raw)) {
      return [];
    }
    return raw.filter((item) => item && typeof item.id === 'string' && typeof item.name === 'string');
  }, [user?.profile_config]);
  const presetNameOptions = useMemo(
    () => presets.map((item) => ({ value: item.name })),
    [presets],
  );
  const normalizedPresetName = useMemo(() => presetName.trim().toLocaleLowerCase('ru-RU'), [presetName]);
  const existingPresetByName = useMemo(
    () => presets.find((item) => item.name.trim().toLocaleLowerCase('ru-RU') === normalizedPresetName),
    [normalizedPresetName, presets],
  );
  const nextPresetID = useMemo(() => {
    const maxIndex = presets.reduce((maxValue, item) => {
      const match = item.id.match(/^preset_(\d+)$/);
      if (!match) return maxValue;
      const parsed = Number(match[1]);
      if (!Number.isFinite(parsed)) return maxValue;
      return Math.max(maxValue, parsed);
    }, 0);
    return `preset_${maxIndex + 1}`;
  }, [presets]);
  const ticketStateStorageKey = useMemo(() => {
    const userKey = user?.id ? String(user.id) : 'guest';
    return `tickets-last-state-${userKey}`;
  }, [user?.id]);
  const hasExplicitTicketState = TICKET_STATE_PARAM_KEYS.some((key) => ticketParams.has(key));
  const ticketStateRefReady = useRef(false);
  const lastSyncedTicketStateRef = useRef('');
  const ticketStateFromParams = useMemo(() => {
    const nextState: Record<string, string> = {};
    TICKET_STATE_PARAM_KEYS.forEach((key) => {
      const value = ticketParams.get(key);
      if (value) {
        nextState[key] = value;
      }
    });
    return nextState;
  }, [ticketParams]);
  const serializedTicketState = useMemo(() => JSON.stringify(ticketStateFromParams), [ticketStateFromParams]);
  const profileTicketState = useMemo(() => {
    const raw = (
      user?.profile_config as { tickets?: { filters?: { last_state?: Record<string, unknown> } } } | undefined
    )?.tickets?.filters?.last_state;
    if (!raw || typeof raw !== 'object') {
      return null;
    }
    const parsed: Record<string, string> = {};
    TICKET_STATE_PARAM_KEYS.forEach((key) => {
      const value = raw[key];
      if (typeof value === 'string' && value) {
        parsed[key] = value;
      }
    });
    return Object.keys(parsed).length ? parsed : null;
  }, [user?.profile_config]);

  const updateTicketParams = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(ticketParams);
    Object.entries(next).forEach(([key, value]) => {
      if (!value) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    const shouldResetPreset =
      !Object.prototype.hasOwnProperty.call(next, 'preset_id')
      && Object.keys(next).some((key) => (TICKET_PRESET_PARAM_KEYS as readonly string[]).includes(key) || key === 'q');
    if (shouldResetPreset) {
      params.delete('preset_id');
    }
    params.set('page', '1');
    setTicketParamsRaw(params.toString());
  };

  useEffect(() => {
    if (!isTicketsListPage) {
      ticketStateRefReady.current = false;
    }
  }, [isTicketsListPage]);

  useLayoutEffect(() => {
    if (!isTicketsListPage || ticketStateRefReady.current) {
      return;
    }
    ticketStateRefReady.current = true;
    if (hasExplicitTicketState) {
      return;
    }

    const parseState = (rawState: string | null): Record<string, string> | null => {
      if (!rawState) {
        return null;
      }
      try {
        const savedState = JSON.parse(rawState) as Record<string, unknown>;
        if (!savedState || typeof savedState !== 'object') {
          return null;
        }
        const parsed: Record<string, string> = {};
        TICKET_STATE_PARAM_KEYS.forEach((key) => {
          const value = savedState[key];
          if (typeof value === 'string' && value) {
            parsed[key] = value;
          }
        });
        return Object.keys(parsed).length ? parsed : null;
      } catch {
        return null;
      }
    };

    const localState = parseState(localStorage.getItem(ticketStateStorageKey));
    const savedState = localState || profileTicketState;
    if (!savedState) {
      return;
    }

    const nextParams = new URLSearchParams(ticketParams);
    let hasChanges = false;
    (Object.keys(savedState) as TicketStateParamKey[]).forEach((key) => {
      const savedValue = savedState[key];
      if (!savedValue || nextParams.has(key)) {
        return;
      }
      nextParams.set(key, savedValue);
      hasChanges = true;
    });
    if (!hasChanges) {
      return;
    }
    nextParams.set('page', '1');
    setTicketParamsRaw(nextParams.toString());
  }, [hasExplicitTicketState, isTicketsListPage, profileTicketState, setTicketParamsRaw, ticketParams, ticketStateStorageKey]);

  useEffect(() => {
    if (!isTicketsPage) {
      return;
    }
    if (isTicketsListPage && !ticketStateRefReady.current) {
      return;
    }
    localStorage.setItem(ticketStateStorageKey, serializedTicketState);
  }, [isTicketsPage, isTicketsListPage, serializedTicketState, ticketStateStorageKey]);

  useEffect(() => {
    if (!user || !isTicketsPage) {
      return;
    }
    if (isTicketsListPage && !ticketStateRefReady.current) {
      return;
    }
    if (lastSyncedTicketStateRef.current === serializedTicketState) {
      return;
    }
    const timeoutID = window.setTimeout(() => {
      const currentConfig = (user.profile_config || {}) as Record<string, unknown>;
      const ticketsConfig = (currentConfig.tickets || {}) as Record<string, unknown>;
      const filtersConfig = (ticketsConfig.filters || {}) as Record<string, unknown>;
      const nextConfig: Record<string, unknown> = {
        ...currentConfig,
        tickets: {
          ...ticketsConfig,
          filters: {
            ...filtersConfig,
            last_state: ticketStateFromParams,
          },
        },
      };
      lastSyncedTicketStateRef.current = serializedTicketState;
      setUser({ ...user, profile_config: nextConfig as any });
      persistTicketStateMutation.mutate(nextConfig, {
        onError: () => {
          lastSyncedTicketStateRef.current = '';
        },
      });
    }, 400);
    return () => window.clearTimeout(timeoutID);
  }, [
    isTicketsPage,
    isTicketsListPage,
    persistTicketStateMutation,
    serializedTicketState,
    setUser,
    ticketStateFromParams,
    user,
  ]);

  const applyPreset = (presetID: string) => {
    const preset = presets.find((item) => item.id === presetID);
    if (!preset) {
      return;
    }
    const nextParams: Record<string, string | undefined> = {};
    TICKET_PRESET_PARAM_KEYS.forEach((key) => {
      nextParams[key] = preset.values[key] || undefined;
    });
    nextParams.preset_id = preset.id;
    updateTicketParams(nextParams);
    setPresetName(preset.name);
  };

  const saveCurrentPreset = async () => {
    const name = presetName.trim();
    if (!name) {
      message.warning('Введите имя фильтра');
      return;
    }
    if (!user) {
      return;
    }

    const nextPresetValues: Partial<Record<TicketPresetParamKey, string>> = {};
    TICKET_PRESET_PARAM_KEYS.forEach((key) => {
      const value = ticketParams.get(key);
      if (value) {
        nextPresetValues[key] = value;
      }
    });
    const nextPreset: TicketPreset = {
      id: existingPresetByName?.id || nextPresetID,
      name: existingPresetByName?.name || name,
      values: nextPresetValues,
    };
    const nextPresets = existingPresetByName
      ? presets.map((item) => (item.id === existingPresetByName.id ? nextPreset : item))
      : [...presets, nextPreset];

    const currentConfig = (user.profile_config || {}) as Record<string, unknown>;
    const ticketsConfig = (currentConfig.tickets || {}) as Record<string, unknown>;
    const filtersConfig = (ticketsConfig.filters || {}) as Record<string, unknown>;
    const nextConfig: Record<string, unknown> = {
      ...currentConfig,
      tickets: {
        ...ticketsConfig,
        filters: {
          ...filtersConfig,
          presets: nextPresets,
        },
      },
    };

    try {
      await updateProfileMutation.mutateAsync(nextConfig);
      setUser({ ...user, profile_config: nextConfig as any });
      setPresetName('');
      updateTicketParams({ preset_id: nextPreset.id });
      message.success(existingPresetByName ? 'Фильтр обновлён' : 'Фильтр сохранён');
    } catch {
      message.error('Не удалось сохранить фильтр');
    }
  };

  const deleteCurrentPreset = async () => {
    if (!user || !existingPresetByName) {
      return;
    }
    const currentConfig = (user.profile_config || {}) as Record<string, unknown>;
    const ticketsConfig = (currentConfig.tickets || {}) as Record<string, unknown>;
    const filtersConfig = (ticketsConfig.filters || {}) as Record<string, unknown>;
    const nextPresets = presets.filter((item) => item.id !== existingPresetByName.id);
    const nextConfig: Record<string, unknown> = {
      ...currentConfig,
      tickets: {
        ...ticketsConfig,
        filters: {
          ...filtersConfig,
          presets: nextPresets,
        },
      },
    };

    try {
      await updateProfileMutation.mutateAsync(nextConfig);
      setUser({ ...user, profile_config: nextConfig as any });
      if (selectedPresetID === existingPresetByName.id) {
        updateTicketParams({ preset_id: undefined });
      }
      setPresetName('');
      message.success('Фильтр удалён');
    } catch {
      message.error('Не удалось удалить фильтр');
    }
  };

  const [sectionParams] = useSearchParams();
  const sectionTerm = sectionParams.get('q') || '';
  const [sectionSearchTerm, setSectionSearchTerm] = useState(sectionTerm);

  useEffect(() => {
    if (!isSectionSearchPage) return;
    setSectionSearchTerm(sectionTerm);
  }, [isSectionSearchPage, sectionTerm]);

  const sectionPlaceholder = (() => {
    if (isCompaniesPage) return 'Поиск компаний: название, адрес, юр. название';
    if (isServersPage) return 'Поиск серверов: id, ip, название';
    if (isWorkstationsPage) return 'Поиск станций: id, название';
    if (isFiscalsPage) return 'Поиск ФР: id, модель, РНМ';
    return 'Поиск...';
  })();

  const onSectionSearch = (value: string) => {
    const trimmed = value.trim();
    const params = new URLSearchParams(sectionParams);
    if (!trimmed) {
      params.delete('q');
    } else {
      params.set('q', trimmed);
    }
    params.delete('page');
    const query = params.toString();
    navigate(query ? `${location.pathname}?${query}` : location.pathname);
  };

  const [agentObservationParams] = useSearchParams();
  const agentUUIDFilter = (agentObservationParams.get('agent_uuid') || agentObservationParams.get('agent') || '').trim();
  const workstationFilter = (agentObservationParams.get('workstation_id') || '').trim();
  const frFilter = (agentObservationParams.get('fr_id') || '').trim();
  const pausedFilter = agentObservationParams.get('paused') === '1';

  const agentFilterTokensFromParams = useMemo(() => {
    const tokens: string[] = [];
    if (agentUUIDFilter) tokens.push(`agent:${agentUUIDFilter}`);
    if (workstationFilter) tokens.push(`ws:${workstationFilter}`);
    if (frFilter) tokens.push(`fr:${frFilter}`);
    return tokens;
  }, [agentUUIDFilter, frFilter, workstationFilter]);
  const [agentFilterTokens, setAgentFilterTokens] = useState<string[]>(agentFilterTokensFromParams);

  useEffect(() => {
    if (!isAgentObservationsPage) return;
    setAgentFilterTokens(agentFilterTokensFromParams);
  }, [agentFilterTokensFromParams, isAgentObservationsPage]);

  const updateAgentObservationParams = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(agentObservationParams);
    Object.entries(next).forEach(([key, value]) => {
      if (!value) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    const query = params.toString();
    navigate(query ? `/agent-observations?${query}` : '/agent-observations');
  };

  const parseAgentFilterTokens = (tokens: string[]) => {
    let nextAgent = '';
    let nextWS = '';
    let nextFR = '';
    tokens.forEach((raw) => {
      const token = String(raw || '').trim();
      if (!token) return;
      const [rawType, ...valueParts] = token.split(':');
      const type = rawType.trim().toLowerCase();
      const value = valueParts.join(':').trim();
      if (!value) return;
      if (type === 'agent') nextAgent = value;
      if (type === 'ws' || type === 'workstation') nextWS = value;
      if (type === 'fr' || type === 'fiscal') nextFR = value;
    });
    return { nextAgent, nextWS, nextFR };
  };

  if (isTicketsPage) {
    const periodValue: [Dayjs, Dayjs] | null = periodFrom && periodTo ? [dayjs(periodFrom), dayjs(periodTo)] : null;
    const filterContent = (
      <Space direction="vertical" size="small" style={{ width: 420, maxWidth: 'min(420px, calc(100vw - 40px))' }}>
        {isHeaderNarrow && (
          <div className="ticket-filter-popover-mobile-only">
            <Text type="secondary" style={{ fontSize: 12 }}>Режим списка</Text>
            <div style={{ marginTop: 6 }}>
              <Segmented
                block
                value={archiveMode}
                options={[
                  { value: 'active', label: 'В работе' },
                  { value: 'archive', label: 'Архив' },
                ]}
                onChange={(value) => {
                  const nextMode = value as 'active' | 'archive';
                  updateTicketParams({ archive_mode: nextMode });
                }}
              />
            </div>
          </div>
        )}

        {isHeaderNarrow && archiveMode !== 'archive' && (
          <div className="ticket-filter-popover-mobile-only">
            <Text type="secondary" style={{ fontSize: 12 }}>Сохранённый фильтр</Text>
            <Select
              allowClear
              placeholder="Сохранённый фильтр"
              value={selectedPresetID}
              options={presets.map((item) => ({ value: item.id, label: item.name }))}
              onChange={(value) => {
                if (!value) {
                  updateTicketParams({ preset_id: undefined });
                  return;
                }
                applyPreset(value);
              }}
              style={{ width: '100%', marginTop: 6 }}
            />
          </div>
        )}

        <Space style={{ width: '100%' }} align="start">
          <Select
            value={ticketView}
            onChange={(value) => updateTicketParams({ view: value })}
            options={[
              { value: 'list', label: 'Список' },
              { value: 'cards', label: 'Карточки' },
              { value: 'table', label: 'Таблица' },
            ]}
            style={{ width: VIEW_SELECT_WIDTH, flexShrink: 0 }}
          />
          {ticketView === 'table' && (
            <Select
              mode="multiple"
              value={selectedTableColumns}
              onChange={(values) => {
                const normalized = tableColumnOrder.filter((key) => values.includes(key));
                updateTicketParams({
                  table_columns: normalized.length ? normalized.join(',') : undefined,
                });
              }}
              options={tableColumnOptions}
              style={{ flex: 1, minWidth: 0 }}
            />
          )}
        </Space>


        {archiveMode !== 'archive' && (
          <>
            <Space style={{ width: LONGEST_STATUS_LABEL_WIDTH, justifyContent: 'space-between' }} align="start">
              <Select
                mode="multiple"
                placeholder="Статусы"
                value={statusValues}
                onChange={(values) => updateTicketParams({ status: values.length ? values.join(',') : undefined })}
                options={TICKET_STATUS_OPTIONS}
                style={{ width: 182 }}
              />
              <Checkbox
                checked={onlyActiveStatuses}
                onChange={(event) => updateTicketParams({ only_active_statuses: event.target.checked ? '1' : undefined })}
              >
                Активные
              </Checkbox>
            </Space>
            <Select
              mode="multiple"
              placeholder="Сотрудники"
              value={assigneeValues}
              onChange={(values) => updateTicketParams({ assignee_ids: values.length ? values.join(',') : undefined })}
              options={assigneeOptions}
              loading={!assigneesRes}
              style={{ width: LONGEST_STATUS_LABEL_WIDTH }}
            />
            <Checkbox
              checked={isMineOnly}
              disabled={!ownAssigneeID}
              onChange={(event) => updateTicketParams({ assignee_ids: event.target.checked ? ownAssigneeID : undefined })}
            >
              Мои
            </Checkbox>
          </>
        )}

        <DatePicker.RangePicker
          value={periodValue}
          onChange={(dates) => {
            const from = dates?.[0] ? dates[0].startOf('day').format('YYYY-MM-DD') : undefined;
            const to = dates?.[1] ? dates[1].endOf('day').format('YYYY-MM-DD') : undefined;
            updateTicketParams({ [periodFromParamKey]: from, [periodToParamKey]: to });
          }}
          style={{ width: LONGEST_STATUS_LABEL_WIDTH }}
          allowClear
          format="DD.MM.YYYY"
        />

        <Select
          showSearch
          allowClear
          placeholder="Компания"
          value={ticketCompany}
          onChange={(value) => updateTicketParams({ [companyParamKey]: value || undefined })}
          filterOption={(input, option) =>
            String((option as { searchText?: string } | undefined)?.searchText || '').includes(input.toLowerCase())
          }
          options={companyOptions}
          loading={isFiltersLoading}
          optionLabelProp="selectedLabel"
          style={{ width: LONGEST_STATUS_LABEL_WIDTH }}
        />

        {archiveMode !== 'archive' && (
          <Space.Compact style={{ width: '100%' }}>
            <AutoComplete
              options={presetNameOptions}
              placeholder="Имя фильтра"
              value={presetName}
              onChange={setPresetName}
              filterOption={(inputValue, option) =>
                String(option?.value || '').toLowerCase().includes(inputValue.toLowerCase())
              }
            />
            <Button onClick={() => void saveCurrentPreset()} loading={updateProfileMutation.isPending}>
              {existingPresetByName ? 'Обновить' : 'Сохранить'}
            </Button>
            {existingPresetByName && (
              <Button danger onClick={() => void deleteCurrentPreset()} loading={updateProfileMutation.isPending}>
                Удалить
              </Button>
            )}
          </Space.Compact>
        )}

        <Button
          onClick={() => updateTicketParams(
            archiveMode === 'archive'
              ? {
                  archive_company: undefined,
                  archive_period_from: undefined,
                  archive_period_to: undefined,
                }
              : {
                  status: undefined,
                  only_active_statuses: undefined,
                  table_columns: undefined,
                  assignee_ids: undefined,
                  company: undefined,
                  period_from: undefined,
                  period_to: undefined,
                },
          )}
        >
          Сбросить фильтры
        </Button>
      </Space>
    );

    return (
      <Space size="small" wrap={!isHeaderNarrow} style={{ justifyContent: 'center' }} className="ticket-header-search-controls">
        {selectedTicketIDs.length >= 1 && archiveMode !== 'archive' && (
          <Select
            placeholder={`Исполнитель (${selectedTicketIDs.length})`}
            options={assigneeOptions}
            loading={!assigneesRes || bulkAssignMutation.isPending}
            style={{ width: isCompact ? 190 : 230 }}
            onChange={(value) => {
              const next = Number(value);
              if (!next || selectedTicketIDs.length === 0) return;
              bulkAssignMutation.mutate({ ids: selectedTicketIDs, assigneeID: next });
            }}
          />
        )}
        {!isHeaderNarrow && (
          <Segmented
            className="ticket-header-inline-archive"
            value={archiveMode}
            options={[
              { value: 'active', label: 'В работе' },
              { value: 'archive', label: 'Архив' },
            ]}
            onChange={(value) => {
              const nextMode = value as 'active' | 'archive';
              updateTicketParams({ archive_mode: nextMode });
            }}
          />
        )}
        <Input.Search
          placeholder="Поиск по заявкам..."
          allowClear
          value={ticketTerm}
          onChange={(event) => setTicketTerm(event.target.value)}
          onSearch={(value) => updateTicketParams({ q: value.trim() || undefined })}
          style={{ width: isHeaderMobile ? 200 : (isCompact ? 240 : 320) }}
        />
        {!isHeaderNarrow && archiveMode !== 'archive' && (
          <Select
            className="ticket-header-inline-preset"
            allowClear
            placeholder="Сохранённый фильтр"
            value={selectedPresetID}
            options={presets.map((item) => ({ value: item.id, label: item.name }))}
            onChange={(value) => {
              if (!value) {
                updateTicketParams({ preset_id: undefined });
                return;
              }
              applyPreset(value);
            }}
            style={{ width: isCompact ? 180 : 220 }}
          />
        )}
        <Popover trigger="click" placement="bottomRight" content={filterContent}>
          <Button shape="circle" icon={<SettingOutlined />} />
        </Popover>
        <Button
          className="ticket-header-new-ticket"
          type="primary"
          icon={<PlusOutlined />}
          aria-label="Новая заявка"
          style={isHeaderNarrow ? { width: 40, minWidth: 40, paddingInline: 0 } : undefined}
          onClick={() => {
            if (isTicketsListPage) {
              requestCreateTicket();
              return;
            }
            if (isTicketsPage) {
              const params = new URLSearchParams(location.search);
              params.set('create', '1');
              navigate(`${location.pathname}?${params.toString()}`);
              return;
            }
            requestCreateTicket();
            navigate('/tickets');
          }}
        >
          {!isHeaderNarrow && <span className="ticket-header-new-ticket-label">Новая заявка</span>}
        </Button>
      </Space>
    );
  }

  if (isSectionSearchPage) {
    return (
      <Input.Search
        placeholder={sectionPlaceholder}
        allowClear
        value={sectionSearchTerm}
        onChange={(event) => setSectionSearchTerm(event.target.value)}
        onSearch={onSectionSearch}
        style={{ width: 440, maxWidth: '100%' }}
      />
    );
  }

  if (isAgentObservationsPage) {
    return (
      <Space size="small" wrap style={{ justifyContent: 'center' }}>
        <Select
          mode="tags"
          value={agentFilterTokens}
          onChange={(values) => {
            const normalized = values.map((item) => String(item || '').trim()).filter(Boolean);
            setAgentFilterTokens(normalized);
            const parsed = parseAgentFilterTokens(normalized);
            updateAgentObservationParams({
              agent_uuid: parsed.nextAgent || undefined,
              agent: undefined,
              workstation_id: parsed.nextWS || undefined,
              fr_id: parsed.nextFR || undefined,
            });
          }}
          tokenSeparators={[',']}
          style={{ width: 520, maxWidth: '100%' }}
          placeholder="Фильтры: agent:<uuid>, ws:<id>, fr:<id>"
        />
        <Space size={6}>
          <Switch checked={pausedFilter} onChange={(checked) => updateAgentObservationParams({ paused: checked ? '1' : undefined })} />
          <span style={{ fontSize: 12, color: '#8c8c8c' }}>Пауза списка</span>
        </Space>
        <Button onClick={() => updateAgentObservationParams({ refresh: String(Date.now()) })}>
          Обновить
        </Button>
        <Button
          onClick={() => {
            setAgentFilterTokens([]);
            updateAgentObservationParams({
              agent_uuid: undefined,
              agent: undefined,
              workstation_id: undefined,
              fr_id: undefined,
            });
          }}
        >
          Сброс фильтров
        </Button>
      </Space>
    );
  }


  return (
    <Space size="small">
      <Input.Search
        placeholder="Поиск по IP, Serial, Name..."
        allowClear
        value={searchTerm}
        onChange={(event) => setSearchTerm(event.target.value)}
        onSearch={onGlobalSearch}
        style={{ width: 360 }}
        className="header-search-input"
      />
      <Space size={6}>
        <Switch size="small" checked={showInactive} onChange={onToggleShowInactive} />
        <span style={{ fontSize: 12, color: '#8c8c8c' }}>Без контракта</span>
      </Space>
    </Space>
  );
};

export default HeaderSearch;
