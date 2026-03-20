import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Empty, Input, Row, Space, Statistic, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/ru';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { agentObservationsApi } from '@/api/agentObservations';
import AgentObservationRawModal, { type AgentObservationRawSummary } from '@/components/agents/AgentObservationRawModal';
import type { AgentListItemDTO, AgentObservationFeedRowDTO } from '@/types/api';

dayjs.extend(relativeTime);
dayjs.locale('ru');

const { Title, Text } = Typography;

const FRESH_HEARTBEAT_MINUTES = 5;
const WARN_HEARTBEAT_MINUTES = 30;

type LatestObservationModalState = {
  open: boolean;
  agent: AgentListItemDTO | null;
  observationID?: number;
  summary?: AgentObservationRawSummary;
  lookupLoading: boolean;
  lookupError?: string;
  emptyDescription?: string;
};

const createInitialModalState = (): LatestObservationModalState => ({
  open: false,
  agent: null,
  observationID: undefined,
  summary: undefined,
  lookupLoading: false,
  lookupError: undefined,
  emptyDescription: undefined,
});

const formatDateTime = (value?: string) => {
  if (!value) {
    return '-';
  }
  const parsed = dayjs(value);
  if (!parsed.isValid()) {
    return value;
  }
  return parsed.format('DD.MM.YYYY HH:mm:ss');
};

const formatRelativeTime = (value?: string, now = Date.now()) => {
  if (!value) {
    return '';
  }
  const parsed = dayjs(value);
  if (!parsed.isValid()) {
    return '';
  }
  return parsed.from(dayjs(now));
};

const getHeartbeatAgeMinutes = (value?: string, now = Date.now()) => {
  if (!value) {
    return null;
  }
  const parsed = dayjs(value);
  if (!parsed.isValid()) {
    return null;
  }
  return Math.max(0, dayjs(now).diff(parsed, 'minute', true));
};

const getHeartbeatFreshness = (value?: string, now = Date.now()) => {
  const ageMinutes = getHeartbeatAgeMinutes(value, now);
  if (ageMinutes === null) {
    return {
      color: 'error' as const,
      label: 'Нет heartbeat',
    };
  }
  if (ageMinutes <= FRESH_HEARTBEAT_MINUTES) {
    return {
      color: 'success' as const,
      label: 'Свежий',
    };
  }
  if (ageMinutes <= WARN_HEARTBEAT_MINUTES) {
    return {
      color: 'warning' as const,
      label: 'Устаревает',
    };
  }
  return {
    color: 'error' as const,
    label: 'Просрочен',
  };
};

const getAgentStatusColor = (status?: string) => {
  const normalized = String(status || '').trim().toLowerCase();
  if (!normalized) {
    return 'default';
  }
  if (['active', 'online', 'running', 'ok', 'healthy', 'connected'].includes(normalized)) {
    return 'success';
  }
  if (['new', 'pending', 'processing', 'starting', 'unknown'].includes(normalized)) {
    return 'processing';
  }
  if (['warning', 'degraded', 'stale'].includes(normalized)) {
    return 'warning';
  }
  if (['offline', 'error', 'failed', 'disconnected', 'stopped'].includes(normalized)) {
    return 'error';
  }
  return 'default';
};

const getAgentTypeColor = (type?: string) => {
  const normalized = String(type || '').trim().toLowerCase();
  if (normalized.includes('server')) {
    return 'geekblue';
  }
  if (normalized.includes('workstation') || normalized.includes('station') || normalized.includes('ws')) {
    return 'cyan';
  }
  if (normalized.includes('fr') || normalized.includes('fiscal') || normalized.includes('kkt')) {
    return 'gold';
  }
  return 'default';
};

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
  return 'Не удалось загрузить данные агентов';
};

const buildObservationSummary = (
  agent: AgentListItemDTO,
  latestObservation?: AgentObservationFeedRowDTO | null,
): AgentObservationRawSummary => ({
  agentUUID: latestObservation?.agent_uuid || agent.uuid || undefined,
  serverURL: latestObservation?.server_url || undefined,
  currentTime: latestObservation?.current_time || undefined,
  vTime: latestObservation?.v_time || undefined,
  workstation: latestObservation?.workstation_name || latestObservation?.workstation_id || agent.workstation_id || undefined,
  fr: latestObservation?.fr_name || latestObservation?.fr_id || undefined,
});

const AgentsPage: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();
  const [searchValue, setSearchValue] = useState(term);
  const [now, setNow] = useState(() => Date.now());
  const [latestModalState, setLatestModalState] = useState<LatestObservationModalState>(createInitialModalState);
  const lookupRequestRef = useRef(0);

  useEffect(() => {
    setSearchValue(term);
  }, [term]);

  useEffect(() => {
    const intervalID = window.setInterval(() => {
      setNow(Date.now());
    }, 60_000);
    return () => window.clearInterval(intervalID);
  }, []);

  const updateSearch = useCallback((value: string) => {
    const params = new URLSearchParams(searchParams);
    const nextValue = value.trim();
    if (nextValue) {
      params.set('q', nextValue);
    } else {
      params.delete('q');
    }
    setSearchParams(params);
  }, [searchParams, setSearchParams]);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['agents-list', term],
    queryFn: async () => {
      const response = await agentObservationsApi.listAgents({ term: term || undefined, limit: 500 });
      return response.data || [];
    },
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  });

  const rows = useMemo(() => {
    return (data || []).slice().sort((left, right) => {
      const leftHeartbeat = dayjs(left.last_heartbeat).isValid() ? dayjs(left.last_heartbeat).valueOf() : 0;
      const rightHeartbeat = dayjs(right.last_heartbeat).isValid() ? dayjs(right.last_heartbeat).valueOf() : 0;
      if (leftHeartbeat !== rightHeartbeat) {
        return rightHeartbeat - leftHeartbeat;
      }

      const leftObserved = dayjs(left.last_observed_at).isValid() ? dayjs(left.last_observed_at).valueOf() : 0;
      const rightObserved = dayjs(right.last_observed_at).isValid() ? dayjs(right.last_observed_at).valueOf() : 0;
      if (leftObserved !== rightObserved) {
        return rightObserved - leftObserved;
      }

      return String(left.hostname || left.uuid || '').localeCompare(String(right.hostname || right.uuid || ''), 'ru');
    });
  }, [data]);

  const summary = useMemo(() => {
    const fresh = rows.filter((item) => {
      const heartbeatAge = getHeartbeatAgeMinutes(item.last_heartbeat, now);
      return heartbeatAge !== null && heartbeatAge <= FRESH_HEARTBEAT_MINUTES;
    }).length;

    return {
      total: rows.length,
      fresh,
      stale: rows.length - fresh,
    };
  }, [now, rows]);

  const closeLatestModal = useCallback(() => {
    lookupRequestRef.current += 1;
    setLatestModalState(createInitialModalState());
  }, []);

  const openLatestObservation = useCallback(async (agent: AgentListItemDTO) => {
    const requestID = lookupRequestRef.current + 1;
    lookupRequestRef.current = requestID;

    setLatestModalState({
      open: true,
      agent,
      observationID: undefined,
      summary: buildObservationSummary(agent),
      lookupLoading: true,
      lookupError: undefined,
      emptyDescription: undefined,
    });

    try {
      const latestObservation = await queryClient.fetchQuery({
        queryKey: ['agent-observations-latest', agent.uuid],
        queryFn: async () => {
          const response = await agentObservationsApi.listFeed({
            agent_uuid: agent.uuid,
            sort_by: 'latest',
            order: 'desc',
            limit: 1,
          });
          return response.data?.[0] || null;
        },
        staleTime: 30_000,
      });

      if (lookupRequestRef.current !== requestID) {
        return;
      }

      setLatestModalState({
        open: true,
        agent,
        observationID: latestObservation?.observation_id,
        summary: buildObservationSummary(agent, latestObservation),
        lookupLoading: false,
        lookupError: undefined,
        emptyDescription: latestObservation ? undefined : 'У этого агента пока нет наблюдений.',
      });
    } catch (requestError) {
      if (lookupRequestRef.current !== requestID) {
        return;
      }

      setLatestModalState({
        open: true,
        agent,
        observationID: undefined,
        summary: buildObservationSummary(agent),
        lookupLoading: false,
        lookupError: getErrorMessage(requestError),
        emptyDescription: undefined,
      });
    }
  }, [queryClient]);

  const columns: ColumnsType<AgentListItemDTO> = [
    {
      title: 'Hostname',
      dataIndex: 'hostname',
      key: 'hostname',
      width: 220,
      render: (value: string | undefined, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{value || record.uuid}</Text>
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
        <Link to={`/agent-observations?agent_uuid=${encodeURIComponent(value)}`}>
          {value}
        </Link>
      ),
    },
    {
      title: 'Тип',
      dataIndex: 'type',
      key: 'type',
      width: 130,
      render: (value?: string) => (
        <Tag color={getAgentTypeColor(value)} style={{ marginInlineEnd: 0 }}>
          {value || 'Не указан'}
        </Tag>
      ),
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 150,
      render: (value?: string) => (
        <Tag color={getAgentStatusColor(value)} style={{ marginInlineEnd: 0 }}>
          {value || 'Не указан'}
        </Tag>
      ),
    },
    {
      title: 'Последний heartbeat',
      dataIndex: 'last_heartbeat',
      key: 'last_heartbeat',
      width: 220,
      render: (value?: string) => {
        const freshness = getHeartbeatFreshness(value, now);
        const relativeTime = formatRelativeTime(value, now);

        return (
          <Space direction="vertical" size={0}>
            <Text>{formatDateTime(value)}</Text>
            <Space size={8}>
              <Tag color={freshness.color} style={{ marginInlineEnd: 0 }}>
                {freshness.label}
              </Tag>
              {relativeTime ? <Text type="secondary">{relativeTime}</Text> : null}
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
      render: (value?: string) => {
        const relativeTime = formatRelativeTime(value, now);
        if (!value) {
          return <Text type="secondary">Нет наблюдений</Text>;
        }
        return (
          <Space direction="vertical" size={0}>
            <Text>{formatDateTime(value)}</Text>
            {relativeTime ? <Text type="secondary">{relativeTime}</Text> : null}
          </Space>
        );
      },
    },
    {
      title: 'Владелец',
      dataIndex: 'owner_id',
      key: 'owner_id',
      width: 220,
      render: (value?: string) => (
        value ? <Link to={`/companies/${value}`}>{value}</Link> : <Text type="secondary">Не указан</Text>
      ),
    },
    {
      title: 'Рабочая станция',
      dataIndex: 'workstation_id',
      key: 'workstation_id',
      width: 220,
      render: (value?: string) => (
        value ? <Link to={`/workstations/${value}`}>{value}</Link> : <Text type="secondary">Не привязана</Text>
      ),
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 280,
      fixed: 'right',
      render: (_value: unknown, record) => {
        const isRowLookupLoading = latestModalState.lookupLoading && latestModalState.agent?.uuid === record.uuid;

        return (
          <Space wrap size={4}>
            <Button
              type="link"
              style={{ paddingInline: 0 }}
              loading={isRowLookupLoading}
              onClick={() => void openLatestObservation(record)}
            >
              Последние данные
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
              Открыть оборудование
            </Button>
          </Space>
        );
      },
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <div>
        <Title level={4} style={{ margin: 0 }}>Агенты</Title>
        <Text type="secondary">
          Список агентских инстансов в разделе оборудования Etalon.
        </Text>
      </div>

      <Row gutter={[16, 16]}>
        <Row gutter={[16, 16]} style={{ width: '100%' }}>
          <div style={{ width: '100%', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 16 }}>
            <Card className="glass-panel">
              <Statistic title="Всего агентов" value={summary.total} />
            </Card>
            <Card className="glass-panel">
              <Statistic title="Heartbeat <= 5 минут" value={summary.fresh} valueStyle={{ color: '#389e0d' }} />
            </Card>
            <Card className="glass-panel">
              <Statistic title="Без свежего heartbeat" value={summary.stale} valueStyle={{ color: '#cf1322' }} />
            </Card>
          </div>
        </Row>
      </Row>

      <Card className="glass-panel">
        <Space wrap style={{ width: '100%', justifyContent: 'space-between' }}>
          <Input.Search
            allowClear
            placeholder="Поиск по hostname, uuid или владельцу"
            value={searchValue}
            onChange={(event) => setSearchValue(event.target.value)}
            onSearch={updateSearch}
            style={{ width: 420, maxWidth: '100%' }}
          />
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
          <Table<AgentListItemDTO>
            rowKey="uuid"
            loading
            dataSource={[]}
            columns={columns}
            pagination={false}
          />
        ) : rows.length === 0 ? (
          <Empty description={term ? `По запросу "${term}" агенты не найдены` : 'Агенты пока не найдены'} />
        ) : (
          <Table<AgentListItemDTO>
            rowKey="uuid"
            dataSource={rows}
            columns={columns}
            pagination={{ pageSize: 20, showSizeChanger: true }}
            scroll={{ x: 1650 }}
          />
        )}
      </Card>

      <AgentObservationRawModal
        open={latestModalState.open}
        observationID={latestModalState.observationID}
        title={latestModalState.agent?.hostname
          ? `Последние данные агента ${latestModalState.agent.hostname}`
          : latestModalState.agent?.uuid
            ? `Последние данные агента ${latestModalState.agent.uuid}`
            : 'Последние данные агента'}
        summary={latestModalState.summary}
        lookupLoading={latestModalState.lookupLoading}
        lookupError={latestModalState.lookupError}
        emptyDescription={latestModalState.emptyDescription}
        onClose={closeLatestModal}
      />
    </Space>
  );
};

export default AgentsPage;
