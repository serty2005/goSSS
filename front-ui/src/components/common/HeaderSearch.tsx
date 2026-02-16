import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button, Checkbox, DatePicker, Grid, Input, Popover, Segmented, Select, Space, Switch, Typography, message } from 'antd';
import { PlusOutlined, SettingOutlined } from '@ant-design/icons';
import dayjs, { Dayjs } from 'dayjs';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/store/authStore';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';

const { useBreakpoint } = Grid;
const { Text } = Typography;

const TICKET_STATUS_OPTIONS = [
  { value: 'new', label: 'Новая' },
  { value: 'in_progress', label: 'В работе' },
  { value: 'pending', label: 'Ожидание' },
  { value: 'deferred', label: 'Отложено' },
  { value: 'onsite', label: 'На выезд' },
  { value: 'to_manager', label: 'Передать менеджеру' },
  { value: 'resolved', label: 'Решена' },
  { value: 'spam', label: 'Спам' },
  { value: 'execution', label: 'Реализация' },
  { value: 'closed', label: 'Закрыта' },
];
const ACTIVE_STATUS_VALUES = ['new', 'in_progress', 'pending', 'deferred', 'onsite', 'to_manager'];
const LONGEST_STATUS_LABEL_WIDTH = 260;
const VIEW_SELECT_WIDTH = LONGEST_STATUS_LABEL_WIDTH / 2;
const TABLE_COLUMN_OPTIONS = [
  { value: 'number', label: 'Номер' },
  { value: 'status', label: 'Статус' },
  { value: 'company_display', label: 'Компания' },
  { value: 'assignee_display', label: 'Исполнитель' },
  { value: 'subject', label: 'Тема' },
  { value: 'last_comment', label: 'Последний комментарий' },
  { value: 'created_at', label: 'Создано' },
  { value: 'last_activity', label: 'Обновлено' },
  { value: 'sync_with_bitrix', label: 'B24' },
];
const TABLE_COLUMN_KEYS = TABLE_COLUMN_OPTIONS.map((item) => item.value);

type TicketPreset = {
  id: string;
  name: string;
  values: {
    status?: string;
    company?: string;
    assignee_ids?: string;
    period_from?: string;
    period_to?: string;
    table_columns?: string;
  };
};

const HeaderSearch: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const currentTerm = searchParams.get('term') || '';
  const showInactive = ['1', 'true', 'yes', 'on'].includes((searchParams.get('show_inactive') || '').toLowerCase());
  const [searchTerm, setSearchTerm] = useState(currentTerm);
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);

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

  const [ticketParams, setTicketParams] = useSearchParams();
  const [ticketTerm, setTicketTerm] = useState(ticketParams.get('q') || '');
  const [presetName, setPresetName] = useState('');
  const appliedSearch = ticketParams.get('q') || '';
  const ticketStatus = ticketParams.get('status') || '';
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
    setTicketTerm(ticketParams.get('q') || '');
  }, [ticketParams]);

  const statusValues = useMemo(() => (ticketStatus ? ticketStatus.split(',').filter(Boolean) : []), [ticketStatus]);
  const effectiveStatusValues = useMemo(() => {
    if (archiveMode === 'archive') {
      return [];
    }
    if (!onlyActiveStatuses) {
      return statusValues;
    }
    const filtered = statusValues.filter((value) => ACTIVE_STATUS_VALUES.includes(value));
    return filtered.length ? filtered : ACTIVE_STATUS_VALUES;
  }, [archiveMode, onlyActiveStatuses, statusValues]);
  const assigneeValues = useMemo(() => (ticketAssigneeIDs ? ticketAssigneeIDs.split(',').filter(Boolean) : []), [ticketAssigneeIDs]);
  const selectedTableColumns = useMemo(() => {
    if (!ticketTableColumns) {
      return TABLE_COLUMN_KEYS;
    }
    const values = ticketTableColumns.split(',').filter((value) => TABLE_COLUMN_KEYS.includes(value));
    return values.length ? values : TABLE_COLUMN_KEYS;
  }, [ticketTableColumns]);

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

  const presets = useMemo<TicketPreset[]>(() => {
    const raw = (user?.profile_config as { tickets?: { filters?: { presets?: TicketPreset[] } } } | undefined)?.tickets?.filters?.presets;
    if (!Array.isArray(raw)) {
      return [];
    }
    return raw.filter((item) => item && typeof item.id === 'string' && typeof item.name === 'string');
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
    params.set('page', '1');
    setTicketParams(params);
  };

  const applyPreset = (presetID: string) => {
    const preset = presets.find((item) => item.id === presetID);
    if (!preset) {
      return;
    }
    updateTicketParams({
      status: preset.values.status || undefined,
      company: preset.values.company || undefined,
      assignee_ids: preset.values.assignee_ids || undefined,
      period_from: preset.values.period_from || undefined,
      period_to: preset.values.period_to || undefined,
      table_columns: preset.values.table_columns || undefined,
    });
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

    const nextPreset: TicketPreset = {
      id: `preset_${Date.now()}`,
      name,
      values: {
        status: ticketStatus || undefined,
        company: activeCompany || undefined,
        assignee_ids: ticketAssigneeIDs || undefined,
        period_from: activePeriodFrom || undefined,
        period_to: activePeriodTo || undefined,
        table_columns: ticketTableColumns || undefined,
      },
    };

    const currentConfig = (user.profile_config || {}) as Record<string, unknown>;
    const ticketsConfig = (currentConfig.tickets || {}) as Record<string, unknown>;
    const filtersConfig = (ticketsConfig.filters || {}) as Record<string, unknown>;
    const nextConfig: Record<string, unknown> = {
      ...currentConfig,
      tickets: {
        ...ticketsConfig,
        filters: {
          ...filtersConfig,
          presets: [...presets, nextPreset],
        },
      },
    };

    try {
      await updateProfileMutation.mutateAsync(nextConfig);
      setUser({ ...user, profile_config: nextConfig as any });
      setPresetName('');
      message.success('Фильтр сохранён');
    } catch {
      message.error('Не удалось сохранить фильтр');
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

  const buildAgentFilterTokens = () => {
    const tokens: string[] = [];
    if (agentUUIDFilter) tokens.push(`agent:${agentUUIDFilter}`);
    if (workstationFilter) tokens.push(`ws:${workstationFilter}`);
    if (frFilter) tokens.push(`fr:${frFilter}`);
    return tokens;
  };
  const [agentFilterTokens, setAgentFilterTokens] = useState<string[]>(buildAgentFilterTokens());

  useEffect(() => {
    if (!isAgentObservationsPage) return;
    setAgentFilterTokens(buildAgentFilterTokens());
  }, [isAgentObservationsPage, agentUUIDFilter, workstationFilter, frFilter]);

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
      <Space direction="vertical" size="small" style={{ width: 420 }}>
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
              onChange={(values) => updateTicketParams({
                table_columns: values.length && values.length < TABLE_COLUMN_KEYS.length ? values.join(',') : undefined,
              })}
              options={TABLE_COLUMN_OPTIONS}
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
          <>
            <Select
              allowClear
              placeholder="Выбрать сохранённый фильтр"
              options={presets.map((item) => ({ value: item.id, label: item.name }))}
              onChange={(value) => {
                if (!value) return;
                applyPreset(value);
              }}
            />
            <Space.Compact style={{ width: '100%' }}>
              <Input
                placeholder="Имя фильтра"
                value={presetName}
                onChange={(event) => setPresetName(event.target.value)}
              />
              <Button onClick={() => void saveCurrentPreset()} loading={updateProfileMutation.isPending}>
                Сохранить
              </Button>
            </Space.Compact>
          </>
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
      <Space size="small" wrap style={{ justifyContent: 'center' }}>
        <Segmented
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
        <Input.Search
          placeholder="Поиск по заявкам..."
          allowClear
          value={ticketTerm}
          onChange={(event) => setTicketTerm(event.target.value)}
          onSearch={(value) => updateTicketParams({ q: value.trim() || undefined })}
          style={{ width: isCompact ? 240 : 320 }}
        />
        <Popover trigger="click" placement="bottomRight" content={filterContent}>
          <Button shape="circle" icon={<SettingOutlined />} />
        </Popover>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            if (isTicketsListPage) {
              updateTicketParams({ create: '1' });
              return;
            }
            navigate('/tickets?create=1');
          }}
        >
          Новая заявка
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
