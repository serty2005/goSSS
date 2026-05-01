import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Alert, Button, Card, Empty, Input, Select, Skeleton, Space, Statistic, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { agentDiagnosticsApi } from '@/api/agentDiagnostics';
import { AgentDiagnosticsListItemDTO } from '@/types/api';
import {
  FRESH_HEARTBEAT_MINUTES,
  formatDateTime,
  formatRelativeTime,
  getAgentStatusColor,
  getHeartbeatAgeMinutes,
  getHeartbeatFreshness,
  getRegistrationStatusMeta,
  isMeaningfulDate,
} from '@/components/agents/agentDiagnosticsUtils';
import { TEXT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

const { Title, Text } = Typography;

type HeartbeatFilterValue = 'all' | 'present' | 'missing';
type InventoryFilterValue = 'all' | 'with' | 'without';

const getErrorMessage = (error: unknown) => {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const response = (error as { response?: { data?: { error?: { error?: string } } } }).response;
    const apiMessage = response?.data?.error?.error;
    if (apiMessage) {
      return apiMessage;
    }
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return 'Не удалось загрузить список агентов';
};

const toSortTimestamp = (value?: string) => {
  if (!isMeaningfulDate(value)) {
    return 0;
  }
  return value ? new Date(value).getTime() : 0;
};

const AgentsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();
  const registrationFilter = (searchParams.get('registration_status') || '').trim();
  const heartbeatFilter = ((searchParams.get('heartbeat') || 'all').trim() || 'all') as HeartbeatFilterValue;
  const inventoryFilter = ((searchParams.get('inventory') || 'all').trim() || 'all') as InventoryFilterValue;
  const [searchValue, setSearchValue] = useState(term);
  const debouncedSearchValue = useDebouncedValue(searchValue, TEXT_SEARCH_DEBOUNCE_MS);
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    setSearchValue(term);
  }, [term]);

  useEffect(() => {
    const intervalID = window.setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => window.clearInterval(intervalID);
  }, []);

  const updateFilters = useCallback((next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, value]) => {
      const normalized = String(value || '').trim();
      if (!normalized || normalized === 'all') {
        params.delete(key);
      } else {
        params.set(key, normalized);
      }
    });
    setSearchParams(params);
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    if (debouncedSearchValue.trim() === term) {
      return;
    }
    updateFilters({ q: debouncedSearchValue });
  }, [debouncedSearchValue, term, updateFilters]);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['agent-diagnostics-list', term, registrationFilter],
    queryFn: async () => {
      const response = await agentDiagnosticsApi.list({
        term: term || undefined,
        registration_status: registrationFilter && registrationFilter !== '__empty__' ? registrationFilter : undefined,
        limit: 500,
      });
      return response.data || [];
    },
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  });

  const rows = useMemo(() => {
    return (data || [])
      .filter((item) => {
        if (registrationFilter === '__empty__' && item.last_registration_status) {
          return false;
        }
        if (heartbeatFilter === 'present' && !isMeaningfulDate(item.last_heartbeat)) {
          return false;
        }
        if (heartbeatFilter === 'missing' && isMeaningfulDate(item.last_heartbeat)) {
          return false;
        }
        if (inventoryFilter === 'with' && !item.has_latest_inventory) {
          return false;
        }
        if (inventoryFilter === 'without' && item.has_latest_inventory) {
          return false;
        }
        return true;
      })
      .slice()
      .sort((left, right) => {
        const rightRegistration = toSortTimestamp(right.last_registration_at);
        const leftRegistration = toSortTimestamp(left.last_registration_at);
        if (rightRegistration !== leftRegistration) {
          return rightRegistration - leftRegistration;
        }

        const rightHeartbeat = toSortTimestamp(right.last_heartbeat);
        const leftHeartbeat = toSortTimestamp(left.last_heartbeat);
        if (rightHeartbeat !== leftHeartbeat) {
          return rightHeartbeat - leftHeartbeat;
        }

        const rightObserved = toSortTimestamp(right.last_observed_at);
        const leftObserved = toSortTimestamp(left.last_observed_at);
        if (rightObserved !== leftObserved) {
          return rightObserved - leftObserved;
        }

        return String(left.hostname || left.uuid || '').localeCompare(String(right.hostname || right.uuid || ''), 'ru');
      });
  }, [data, heartbeatFilter, inventoryFilter, registrationFilter]);

  const summary = useMemo(() => {
    const registered = rows.filter((item) => item.last_registration_status === 'success').length;
    const registrationIssues = rows.filter((item) => item.last_registration_status && item.last_registration_status !== 'success').length;
    const freshHeartbeat = rows.filter((item) => {
      const heartbeatAge = getHeartbeatAgeMinutes(item.last_heartbeat, now);
      return heartbeatAge !== null && heartbeatAge <= FRESH_HEARTBEAT_MINUTES;
    }).length;
    const withInventory = rows.filter((item) => item.has_latest_inventory).length;

    return {
      total: rows.length,
      registered,
      registrationIssues,
      freshHeartbeat,
      withInventory,
    };
  }, [now, rows]);

  const columns: ColumnsType<AgentDiagnosticsListItemDTO> = [
    {
      title: 'Hostname',
      dataIndex: 'hostname',
      key: 'hostname',
      width: 220,
      render: (value: string | undefined, record) => (
        <Space direction="vertical" size={0}>
          <Link to={`/agent-diagnostics/${encodeURIComponent(record.uuid)}`}>
            {value || record.uuid}
          </Link>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {record.type || 'Тип не указан'}
          </Text>
        </Space>
      ),
    },
    {
      title: 'UUID',
      dataIndex: 'uuid',
      key: 'uuid',
      width: 260,
      render: (value: string) => (
        <Text copyable={{ text: value }} code>{value}</Text>
      ),
    },
    {
      title: 'Статус агента',
      dataIndex: 'status',
      key: 'status',
      width: 160,
      render: (value?: string) => (
        <Tag color={getAgentStatusColor(value)} style={{ marginInlineEnd: 0 }}>
          {value || 'Не указан'}
        </Tag>
      ),
    },
    {
      title: 'Последняя регистрация',
      dataIndex: 'last_registration_at',
      key: 'last_registration_at',
      width: 220,
      render: (value?: string) => (
        <Space direction="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          <Text type="secondary">{formatRelativeTime(value, now) || '-'}</Text>
        </Space>
      ),
    },
    {
      title: 'Статус регистрации',
      dataIndex: 'last_registration_status',
      key: 'last_registration_status',
      width: 180,
      render: (value?: string) => {
        const meta = getRegistrationStatusMeta(value);
        return (
          <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>
            {meta.label}
          </Tag>
        );
      },
    },
    {
      title: 'Ошибка регистрации',
      dataIndex: 'last_registration_error',
      key: 'last_registration_error',
      width: 260,
      render: (value?: string) => (
        value ? <Text ellipsis={{ tooltip: value }}>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'Последний heartbeat',
      dataIndex: 'last_heartbeat',
      key: 'last_heartbeat',
      width: 220,
      render: (value?: string) => {
        const freshness = getHeartbeatFreshness(value, now);
        return (
          <Space direction="vertical" size={0}>
            <Text>{formatDateTime(value)}</Text>
            <Space size={8}>
              <Tag color={freshness.color} style={{ marginInlineEnd: 0 }}>
                {freshness.label}
              </Tag>
              <Text type="secondary">{formatRelativeTime(value, now) || '-'}</Text>
            </Space>
          </Space>
        );
      },
    },
    {
      title: 'Последнее наблюдение',
      dataIndex: 'last_observed_at',
      key: 'last_observed_at',
      width: 220,
      render: (value?: string) => (
        <Space direction="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          <Text type="secondary">{formatRelativeTime(value, now) || '-'}</Text>
        </Space>
      ),
    },
    {
      title: 'Inventory',
      dataIndex: 'has_latest_inventory',
      key: 'has_latest_inventory',
      width: 130,
      render: (value?: boolean) => (
        <Tag color={value ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>
          {value ? 'Есть' : 'Нет'}
        </Tag>
      ),
    },
    {
      title: 'Adapter statuses',
      dataIndex: 'has_adapter_statuses',
      key: 'has_adapter_statuses',
      width: 160,
      render: (value?: boolean) => (
        <Tag color={value ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>
          {value ? 'Есть' : 'Нет'}
        </Tag>
      ),
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 260,
      fixed: 'right',
      render: (_value: unknown, record) => (
        <Space wrap size={4}>
          <Button
            type="link"
            style={{ paddingInline: 0 }}
            onClick={() => navigate(`/agent-diagnostics/${encodeURIComponent(record.uuid)}`)}
          >
            Диагностика
          </Button>
          <Button
            type="link"
            style={{ paddingInline: 0 }}
            onClick={() => navigate(`/agent-observations?agent_uuid=${encodeURIComponent(record.uuid)}`)}
          >
            Наблюдения
          </Button>
          <Button
            type="link"
            style={{ paddingInline: 0 }}
            disabled={!record.workstation_id}
            onClick={() => {
              if (!record.workstation_id) {
                return;
              }
              navigate(`/workstations/${record.workstation_id}`);
            }}
          >
            Оборудование
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <div>
        <Title level={4} style={{ margin: 0 }}>Агенты</Title>
        <Text type="secondary">
          Диагностика bootstrap-регистрации, heartbeat snapshot и наличия inventory/adapters для нового goSSSagent.
        </Text>
      </div>

      <div style={{ width: '100%', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 16 }}>
        <Card className="glass-panel">
          <Statistic title="Всего агентов" value={summary.total} />
        </Card>
        <Card className="glass-panel">
          <Statistic title="Регистрация успешна" value={summary.registered} valueStyle={{ color: '#389e0d' }} />
        </Card>
        <Card className="glass-panel">
          <Statistic title="Ошибки регистрации" value={summary.registrationIssues} valueStyle={{ color: '#cf1322' }} />
        </Card>
        <Card className="glass-panel">
          <Statistic title="Свежий heartbeat" value={summary.freshHeartbeat} valueStyle={{ color: '#1677ff' }} />
        </Card>
        <Card className="glass-panel">
          <Statistic title="Есть inventory" value={summary.withInventory} />
        </Card>
      </div>

      <Card className="glass-panel">
        <Space wrap style={{ width: '100%', justifyContent: 'space-between' }}>
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="Поиск по hostname, uuid, fingerprint или owner_id"
              value={searchValue}
              onChange={(event) => setSearchValue(event.target.value)}
              onSearch={(value) => updateFilters({ q: value })}
              style={{ width: 420, maxWidth: '100%' }}
            />
            <Select
              value={registrationFilter || 'all'}
              style={{ width: 220 }}
              onChange={(value) => updateFilters({ registration_status: value === 'all' ? undefined : value })}
               options={[
                 { value: 'all', label: 'Все регистрации' },
                 { value: 'success', label: 'Успешные' },
                 { value: 'pending_approval', label: 'Ожидают подтверждения' },
                 { value: 'unauthorized', label: 'Ошибка авторизации' },
                 { value: 'invalid_request', label: 'Неверный payload' },
                 { value: 'failed', label: 'Серверная ошибка' },
                { value: '__empty__', label: 'Без попытки регистрации' },
              ]}
            />
            <Select
              value={heartbeatFilter}
              style={{ width: 180 }}
              onChange={(value: HeartbeatFilterValue) => updateFilters({ heartbeat: value })}
              options={[
                { value: 'all', label: 'Любой heartbeat' },
                { value: 'present', label: 'Heartbeat есть' },
                { value: 'missing', label: 'Heartbeat нет' },
              ]}
            />
            <Select
              value={inventoryFilter}
              style={{ width: 180 }}
              onChange={(value: InventoryFilterValue) => updateFilters({ inventory: value })}
              options={[
                { value: 'all', label: 'Любой inventory' },
                { value: 'with', label: 'Inventory есть' },
                { value: 'without', label: 'Inventory нет' },
              ]}
            />
          </Space>

          <Space wrap>
            <Button onClick={() => void refetch()} loading={isFetching}>
              Обновить
            </Button>
            <Button onClick={() => navigate('/agent-observations')}>
              Лента наблюдений
            </Button>
          </Space>
        </Space>
      </Card>

      <Card className="glass-panel">
        {isError ? (
          <Alert
            type="error"
            showIcon
            message="Не удалось загрузить список агентов"
            description={getErrorMessage(error)}
            action={(
              <Button size="small" onClick={() => void refetch()}>
                Повторить
              </Button>
            )}
          />
        ) : isLoading ? (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Skeleton active paragraph={{ rows: 2 }} />
            <Table<AgentDiagnosticsListItemDTO>
              rowKey="uuid"
              loading
              columns={columns}
              dataSource={[]}
              pagination={false}
            />
          </Space>
        ) : rows.length === 0 ? (
          <Empty description={term ? `По запросу "${term}" агенты не найдены` : 'Агенты пока не найдены'} />
        ) : (
          <Table<AgentDiagnosticsListItemDTO>
            rowKey="uuid"
            dataSource={rows}
            columns={columns}
            pagination={{ pageSize: 20, showSizeChanger: true }}
            scroll={{ x: 2050 }}
          />
        )}
      </Card>
    </Space>
  );
};

export default AgentsPage;
