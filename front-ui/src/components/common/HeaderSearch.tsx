import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AutoComplete, Button, Checkbox, DatePicker, Grid, Input, Popover, Segmented, Select, Space, Switch, Typography, message } from 'antd';
import { PlusOutlined, SettingOutlined } from '@ant-design/icons';
import dayjs, { Dayjs } from 'dayjs';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import { profileApi } from '@/api/profile';
import { getSupportedLocale } from '@/i18n/supportedLocales';
import { useAuthStore } from '@/store/authStore';
import { useTicketParamsStore } from '@/store/ticketParamsStore';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';
import { TICKET_ACTIVE_STATUS_VALUES, TICKET_STATUS_OPTIONS } from '@/constants/ticketStatus';

const { useBreakpoint } = Grid;
const { Text } = Typography;

const LONGEST_STATUS_LABEL_WIDTH = 260;
const VIEW_SELECT_WIDTH = LONGEST_STATUS_LABEL_WIDTH / 2;
const TABLE_COLUMN_OPTIONS = [
  { value: 'selection', labelKey: 'layout:headerSearch.ticket.tableColumns.selection' },
  { value: 'number', labelKey: 'layout:headerSearch.ticket.tableColumns.number' },
  { value: 'status', labelKey: 'layout:headerSearch.ticket.tableColumns.status' },
  { value: 'company_display', labelKey: 'layout:headerSearch.ticket.tableColumns.company_display' },
  { value: 'assignee_display', labelKey: 'layout:headerSearch.ticket.tableColumns.assignee_display' },
  { value: 'reporter_display', labelKey: 'layout:headerSearch.ticket.tableColumns.reporter_display' },
  { value: 'subject', labelKey: 'layout:headerSearch.ticket.tableColumns.subject' },
  { value: 'bitrix_deal_title', labelKey: 'layout:headerSearch.ticket.tableColumns.bitrix_deal_title' },
  { value: 'last_comment', labelKey: 'layout:headerSearch.ticket.tableColumns.last_comment' },
  { value: 'created_at', labelKey: 'layout:headerSearch.ticket.tableColumns.created_at' },
  { value: 'last_activity', labelKey: 'layout:headerSearch.ticket.tableColumns.last_activity' },
  { value: 'sync_with_bitrix', labelKey: 'layout:headerSearch.ticket.tableColumns.sync_with_bitrix' },
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
  const { t, i18n } = useTranslation(['layout', 'common']);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const localeDefinition = useMemo(() => getSupportedLocale(i18n.resolvedLanguage), [i18n.resolvedLanguage]);
  const ticketModeOptions = useMemo(
    () => [
      { value: 'active', label: t('layout:headerSearch.ticket.modes.active') },
      { value: 'archive', label: t('layout:headerSearch.ticket.modes.archive') },
    ],
    [t],
  );
  const ticketViewOptions = useMemo(
    () => [
      { value: 'list', label: t('layout:headerSearch.ticket.views.list') },
      { value: 'cards', label: t('layout:headerSearch.ticket.views.cards') },
      { value: 'table', label: t('layout:headerSearch.ticket.views.table') },
    ],
    [t],
  );
  const ticketStatusOptions = useMemo(
    () => TICKET_STATUS_OPTIONS.map((item) => ({
      ...item,
      label: t(`layout:headerSearch.ticket.statusOptions.${item.value}`),
    })),
    [t],
  );

  const isTicketsPage = location.pathname.startsWith('/tickets');
  const isTicketsListPage = location.pathname === '/tickets';
  const isCompaniesPage = location.pathname.startsWith('/companies');
  const isServersPage = location.pathname === '/servers';
  const isWorkstationsPage = location.pathname === '/workstations';
  const isFiscalsPage = location.pathname === '/fiscals';
  const isAgentsPage = location.pathname === '/agents';
  const isAgentObservationsPage = location.pathname === '/agent-observations';
  const isSectionSearchPage = isCompaniesPage || isServersPage || isWorkstationsPage || isFiscalsPage || isAgentsPage;
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
    () => {
      const localizedOptions = TABLE_COLUMN_OPTIONS.map((item) => ({
        value: item.value,
        label: t(item.labelKey),
      }));
      return isBitrixEnabled
        ? localizedOptions
        : localizedOptions.filter((item) => item.value !== 'bitrix_deal_title' && item.value !== 'sync_with_bitrix');
    },
    [isBitrixEnabled, t],
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
      label: item.full_name || item.username || t('layout:headerSearch.ticket.assigneeFallback', { id: item.id }),
    })),
    [assigneesRes, t],
  );

  const bulkAssignMutation = useMutation({
    mutationFn: async (payload: { ids: string[]; assigneeID: number }) => {
      await Promise.all(payload.ids.map((id) => ticketsApi.assign(id, payload.assigneeID)));
    },
    onSuccess: () => {
      message.success(t('layout:headerSearch.ticket.bulkAssignSuccess'));
      clearSelectedTicketIDs();
      void queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error(t('layout:headerSearch.ticket.bulkAssignError')),
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
  const normalizedPresetName = useMemo(
    () => presetName.trim().toLocaleLowerCase(localeDefinition.intlLocale),
    [localeDefinition.intlLocale, presetName],
  );
  const existingPresetByName = useMemo(
    () => presets.find((item) => item.name.trim().toLocaleLowerCase(localeDefinition.intlLocale) === normalizedPresetName),
    [localeDefinition.intlLocale, normalizedPresetName, presets],
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
      message.warning(t('layout:headerSearch.ticket.presetNameRequired'));
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
      message.success(t(existingPresetByName ? 'layout:headerSearch.ticket.presetUpdated' : 'layout:headerSearch.ticket.presetSaved'));
    } catch {
      message.error(t('layout:headerSearch.ticket.presetSaveError'));
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
      message.success(t('layout:headerSearch.ticket.presetDeleted'));
    } catch {
      message.error(t('layout:headerSearch.ticket.presetDeleteError'));
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
    if (isCompaniesPage) return t('layout:headerSearch.sectionPlaceholders.companies');
    if (isServersPage) return t('layout:headerSearch.sectionPlaceholders.servers');
    if (isWorkstationsPage) return t('layout:headerSearch.sectionPlaceholders.workstations');
    if (isFiscalsPage) return t('layout:headerSearch.sectionPlaceholders.fiscals');
    if (isAgentsPage) return t('layout:headerSearch.sectionPlaceholders.agents');
    return t('layout:headerSearch.sectionPlaceholders.default');
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
    const isBulkAssignMode =
      isTicketsListPage
      && archiveMode !== 'archive'
      && selectedTicketIDs.length > 0;
    const periodValue: [Dayjs, Dayjs] | null = periodFrom && periodTo ? [dayjs(periodFrom), dayjs(periodTo)] : null;
    const filterContent = (
      <Space direction="vertical" size="small" style={{ width: 420, maxWidth: 'min(420px, calc(100vw - 40px))' }}>
        {isHeaderNarrow && (
          <div className="ticket-filter-popover-mobile-only">
            <Text type="secondary" style={{ fontSize: 12 }}>{t('layout:headerSearch.ticket.listMode')}</Text>
            <div style={{ marginTop: 6 }}>
              <Segmented
                block
                value={archiveMode}
                options={ticketModeOptions}
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
            <Text type="secondary" style={{ fontSize: 12 }}>{t('layout:headerSearch.ticket.savedFilter')}</Text>
            <Select
              allowClear
              placeholder={t('layout:headerSearch.ticket.savedFilter')}
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
            options={ticketViewOptions}
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
                placeholder={t('layout:headerSearch.ticket.statuses')}
                value={statusValues}
                onChange={(values) => updateTicketParams({ status: values.length ? values.join(',') : undefined })}
                options={ticketStatusOptions}
                style={{ width: 182 }}
              />
              <Checkbox
                checked={onlyActiveStatuses}
                onChange={(event) => updateTicketParams({ only_active_statuses: event.target.checked ? '1' : undefined })}
              >
                {t('layout:headerSearch.ticket.activeOnly')}
              </Checkbox>
            </Space>
            <Select
              mode="multiple"
              placeholder={t('layout:headerSearch.ticket.assignees')}
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
              {t('layout:headerSearch.ticket.mineOnly')}
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
        />

        <Select
          showSearch
          allowClear
          placeholder={t('layout:headerSearch.ticket.company')}
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
              placeholder={t('layout:headerSearch.ticket.presetName')}
              value={presetName}
              onChange={setPresetName}
              filterOption={(inputValue, option) =>
                String(option?.value || '').toLowerCase().includes(inputValue.toLowerCase())
              }
            />
            <Button onClick={() => void saveCurrentPreset()} loading={updateProfileMutation.isPending}>
              {t(existingPresetByName ? 'common:actions.update' : 'common:actions.save')}
            </Button>
            {existingPresetByName && (
              <Button danger onClick={() => void deleteCurrentPreset()} loading={updateProfileMutation.isPending}>
                {t('common:actions.delete')}
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
          {t('layout:headerSearch.ticket.resetFilters')}
        </Button>
      </Space>
    );

    if (isBulkAssignMode) {
      return (
        <Select
          placeholder={t('layout:headerSearch.ticket.bulkAssign', {
            count: selectedTicketIDs.length,
          })}
          options={assigneeOptions}
          loading={!assigneesRes || bulkAssignMutation.isPending}
          style={{ width: isCompact ? 220 : 280, maxWidth: '100%' }}
          onChange={(value) => {
            const next = Number(value);
            if (!next || selectedTicketIDs.length === 0) {
              return;
            }
            bulkAssignMutation.mutate({
              ids: selectedTicketIDs,
              assigneeID: next,
            });
          }}
        />
      );
    }

    return (
      <Space size="small" wrap={!isHeaderNarrow} style={{ justifyContent: 'center' }} className="ticket-header-search-controls">
        {!isHeaderNarrow && (
          <Segmented
            className="ticket-header-inline-archive"
            value={archiveMode}
            options={ticketModeOptions}
            onChange={(value) => {
              const nextMode = value as 'active' | 'archive';
              updateTicketParams({ archive_mode: nextMode });
            }}
          />
        )}
        <Input.Search
          placeholder={t('layout:headerSearch.ticket.searchPlaceholder')}
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
            placeholder={t('layout:headerSearch.ticket.savedFilter')}
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
          <Button shape="circle" icon={<SettingOutlined />} aria-label={t('layout:headerSearch.ticket.openFilters')} />
        </Popover>
        <Button
          className="ticket-header-new-ticket"
          type="primary"
          icon={<PlusOutlined />}
          aria-label={t('layout:headerSearch.ticket.newTicket')}
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
          {!isHeaderNarrow && <span className="ticket-header-new-ticket-label">{t('layout:headerSearch.ticket.newTicket')}</span>}
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
          placeholder={t('layout:headerSearch.agentObservations.filtersPlaceholder')}
        />
        <Space size={6}>
          <Switch checked={pausedFilter} onChange={(checked) => updateAgentObservationParams({ paused: checked ? '1' : undefined })} />
          <span style={{ fontSize: 12, color: '#8c8c8c' }}>{t('layout:headerSearch.agentObservations.pauseList')}</span>
        </Space>
        <Button onClick={() => updateAgentObservationParams({ refresh: String(Date.now()) })}>
          {t('common:actions.refresh')}
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
          {t('layout:headerSearch.agentObservations.resetFilters')}
        </Button>
      </Space>
    );
  }


  return null;
};

export default HeaderSearch;
