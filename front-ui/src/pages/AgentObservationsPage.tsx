import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Empty, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeftOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { agentObservationsApi } from '@/api/agentObservations';
import { AgentObservationFeedRowDTO } from '@/types/api';
import { useSSE } from '@/features/realtime/useSSE';
import AgentObservationRawModal from '@/components/agents/AgentObservationRawModal';

const { Title, Text } = Typography;

type SortField = 'latest' | 'v_time' | 'current_time';
type SortOrder = 'asc' | 'desc';

type LocalRow = AgentObservationFeedRowDTO & {
  rowKey: string;
  highlightedUntil?: number;
};

const parseDate = (value?: string) => {
  if (!value) {
    return null;
  }
  const parsed = dayjs(value);
  if (!parsed.isValid()) {
    return null;
  }
  return parsed;
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
  return 'Не удалось загрузить наблюдения агентов';
};

const AgentObservationsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const agentFilter = (searchParams.get('agent_uuid') || searchParams.get('agent') || '').trim();
  const workstationFilter = (searchParams.get('workstation_id') || '').trim();
  const frFilter = (searchParams.get('fr_id') || '').trim();
  const paused = searchParams.get('paused') === '1';
  const refreshNonce = (searchParams.get('refresh') || '').trim();
  const [sortField, setSortField] = useState<SortField>('latest');
  const [sortOrder, setSortOrder] = useState<SortOrder>('desc');
  const [snapshotRows, setSnapshotRows] = useState<LocalRow[]>([]);
  const [activeObservationID, setActiveObservationID] = useState<number | undefined>(undefined);
  const pendingRef = useRef<Record<string, LocalRow>>({});
  const { subscribe } = useSSE();

  const matchesFilters = useCallback((row: AgentObservationFeedRowDTO) => {
    if (agentFilter && (row.agent_uuid || '').trim() !== agentFilter) {
      return false;
    }
    if (workstationFilter && (row.workstation_id || '').trim() !== workstationFilter) {
      return false;
    }
    if (frFilter && (row.fr_id || '').trim() !== frFilter) {
      return false;
    }
    return true;
  }, [agentFilter, frFilter, workstationFilter]);

  const sortRows = useCallback((items: LocalRow[]) => {
    const getSortValue = (row: LocalRow) => {
      if (sortField === 'latest') {
        if (typeof row.observation_id === 'number') {
          return row.observation_id;
        }
        return parseDate(row.observed_at)?.valueOf() || 0;
      }
      if (sortField === 'v_time') {
        return parseDate(row.v_time_parsed || row.v_time)?.valueOf() || 0;
      }
      if (sortField === 'current_time') {
        return parseDate(row.current_time_parsed || row.current_time)?.valueOf() || 0;
      }
      return 0;
    };

    return items.slice().sort((left, right) => {
      const leftValue = getSortValue(left);
      const rightValue = getSortValue(right);
      if (sortOrder === 'asc') {
        return leftValue - rightValue;
      }
      return rightValue - leftValue;
    });
  }, [sortField, sortOrder]);

  const updateFilters = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, value]) => {
      if (!value) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    setSearchParams(params);
  };

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ['agent-observations', sortField, sortOrder, agentFilter, workstationFilter, frFilter, refreshNonce],
    queryFn: async () => {
      const response = await agentObservationsApi.listFeed({
        sort_by: sortField,
        order: sortOrder,
        agent_uuid: agentFilter || undefined,
        workstation_id: workstationFilter || undefined,
        fr_id: frFilter || undefined,
      });
      return response.data || [];
    },
    refetchOnWindowFocus: false,
    refetchInterval: paused ? false : 5000,
    refetchIntervalInBackground: true,
  });

  useEffect(() => {
    if (!data) {
      return;
    }
    const nextRows = data.map((row) => ({
      ...row,
      rowKey: String(row.agent_uuid || row.observation_id),
    }));
    pendingRef.current = Object.fromEntries(nextRows.map((item) => [item.rowKey, item]));
    if (!paused) {
      setSnapshotRows(sortRows(nextRows.filter(matchesFilters)));
    }
  }, [data, matchesFilters, paused, sortRows]);

  const onRealtimeMessage = useCallback((eventType: string, rawData: string) => {
    if (eventType !== 'agent.observation.updated') {
      return;
    }

    let payload: AgentObservationFeedRowDTO | null = null;
    try {
      payload = JSON.parse(rawData) as AgentObservationFeedRowDTO;
    } catch {
      return;
    }
    if (!payload) {
      return;
    }

    const rowKey = String(payload.agent_uuid || payload.observation_id);
    const localRow: LocalRow = {
      ...payload,
      rowKey,
      highlightedUntil: Date.now() + 2000,
    };

    pendingRef.current[rowKey] = localRow;
    if (paused) {
      return;
    }

    setSnapshotRows((prev) => {
      const map = new Map(prev.map((item) => [item.rowKey, item]));
      if (matchesFilters(localRow)) {
        map.set(rowKey, localRow);
      } else {
        map.delete(rowKey);
      }
      return sortRows(Array.from(map.values()));
    });

    window.setTimeout(() => {
      setSnapshotRows((prev) => prev.map((item) => (
        item.rowKey === rowKey ? { ...item, highlightedUntil: undefined } : item
      )));
    }, 2200);
  }, [matchesFilters, paused, sortRows]);

  useEffect(() => subscribe('agent.observation.updated', onRealtimeMessage), [onRealtimeMessage, subscribe]);

  useEffect(() => {
    if (!paused) {
      setSnapshotRows(sortRows(Object.values(pendingRef.current).filter(matchesFilters)));
    }
  }, [matchesFilters, paused, sortRows]);

  const rows = useMemo(() => snapshotRows, [snapshotRows]);

  const columns: ColumnsType<LocalRow> = [
    {
      title: '№ наблюдения',
      dataIndex: 'observation_id',
      key: 'observation_id',
      width: 120,
      sorter: true,
      render: (value: number) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => setActiveObservationID(value)}>
          #{value}
        </Button>
      ),
    },
    {
      title: 'UUID агента',
      dataIndex: 'agent_uuid',
      key: 'agent_uuid',
      render: (value?: string) => (
        value ? (
          <Space direction="vertical" size={0}>
            <Link to={`/agent-diagnostics/${encodeURIComponent(value)}`}>
              {value}
            </Link>
            <Button
              type="link"
              size="small"
              style={{ paddingInline: 0, textAlign: 'left' }}
              onClick={() => updateFilters({ agent_uuid: value, agent: undefined })}
            >
              Фильтр в ленте
            </Button>
          </Space>
        ) : '-'
      ),
    },
    {
      title: 'Версия агента',
      dataIndex: 'vc',
      key: 'vc',
      width: 140,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Рабочая станция',
      dataIndex: 'workstation_id',
      key: 'workstation_id',
      render: (_value: string | undefined, record) => {
        if (!record.workstation_id) {
          return '-';
        }
        const linkText = (record.workstation_name || '').trim() || record.workstation_id;
        return (
          <div onClick={() => updateFilters({ workstation_id: record.workstation_id })}>
            <Link
              to={`/workstations/${record.workstation_id}`}
              onClick={(event) => event.stopPropagation()}
            >
              {linkText}
            </Link>
          </div>
        );
      },
    },
    {
      title: 'ФР',
      dataIndex: 'fr_id',
      key: 'fr_id',
      render: (_value: string | undefined, record) => {
        if (!record.fr_id) {
          return '-';
        }
        const linkText = (record.fr_name || '').trim() || record.fr_id;
        return (
          <div onClick={() => updateFilters({ fr_id: record.fr_id })}>
            <Link
              to={`/fiscals/${record.fr_id}`}
              onClick={(event) => event.stopPropagation()}
            >
              {linkText}
            </Link>
          </div>
        );
      },
    },
    {
      title: 'Владелец РС=ФР',
      dataIndex: 'owner_match',
      key: 'owner_match',
      width: 130,
      render: (value?: boolean) => {
        if (typeof value !== 'boolean') {
          return '-';
        }
        return value ? <Tag color="success">✓</Tag> : <Tag color="error">✗</Tag>;
      },
    },
    {
      title: 'v_time',
      dataIndex: 'v_time',
      key: 'v_time',
      sorter: true,
      render: (value?: string) => value || '-',
    },
    {
      title: 'current_time',
      dataIndex: 'current_time',
      key: 'current_time',
      sorter: true,
      render: (value?: string) => value || '-',
    },
    {
      title: 'URL сервера',
      dataIndex: 'server_url',
      key: 'server_url',
      render: (value?: string) => value || '-',
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space direction="vertical" size={4} style={{ width: '100%' }}>
        <Space wrap style={{ justifyContent: 'space-between', width: '100%' }}>
          <div>
            <Title level={4} style={{ margin: 0 }}>Наблюдения агентов</Title>
            <Text type="secondary">
              Drill-down страница по сырому потоку наблюдений и диагностике агентов.
            </Text>
          </div>
          {agentFilter ? (
            <Space wrap>
              <Button onClick={() => navigate(`/agent-diagnostics/${encodeURIComponent(agentFilter)}`)}>
                К диагностике агента
              </Button>
              <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/agents')}>
                К списку агентов
              </Button>
            </Space>
          ) : null}
        </Space>
      </Space>

      <Card className="glass-panel">
        <Space wrap style={{ marginBottom: 12 }}>
          {workstationFilter ? <Tag color="blue">РС: {workstationFilter}</Tag> : null}
          {frFilter ? <Tag color="blue">ФР: {frFilter}</Tag> : null}
          {agentFilter ? <Tag color="blue">Агент: {agentFilter}</Tag> : null}
        </Space>

        {paused ? (
          <Alert
            type="info"
            showIcon
            message="Список на паузе, входящие обновления продолжают накапливаться."
            style={{ marginBottom: 12 }}
          />
        ) : null}

        {isError ? (
          <Alert
            type="error"
            showIcon
            message="Не удалось загрузить наблюдения агентов"
            description={getErrorMessage(error)}
            action={(
              <Button size="small" onClick={() => void refetch()}>
                Повторить
              </Button>
            )}
          />
        ) : isLoading ? (
          <Table<LocalRow>
            rowKey="rowKey"
            loading
            columns={columns}
            dataSource={[]}
            pagination={false}
          />
        ) : rows.length === 0 ? (
          <Empty description={agentFilter ? 'Для выбранного агента наблюдений пока нет' : 'Наблюдения агентов пока не найдены'} />
        ) : (
          <Table<LocalRow>
            rowKey="rowKey"
            columns={columns}
            dataSource={rows}
            onChange={(_pagination, _filters, sorter) => {
              const sortConfig = Array.isArray(sorter) ? sorter[0] : sorter;
              if (!sortConfig?.columnKey) {
                setSortField('latest');
                setSortOrder('desc');
                return;
              }
              if (sortConfig.columnKey === 'observation_id') {
                setSortField('latest');
              } else if (sortConfig.columnKey === 'v_time') {
                setSortField('v_time');
              } else if (sortConfig.columnKey === 'current_time') {
                setSortField('current_time');
              } else {
                setSortField('latest');
              }
              setSortOrder(sortConfig.order === 'ascend' ? 'asc' : 'desc');
            }}
            rowClassName={(record) => (record.highlightedUntil ? 'agent-observation-row-highlight' : '')}
            pagination={{ pageSize: 50 }}
            scroll={{ x: 1280 }}
          />
        )}
      </Card>

      <AgentObservationRawModal
        open={Boolean(activeObservationID)}
        observationID={activeObservationID}
        onClose={() => setActiveObservationID(undefined)}
      />
    </Space>
  );
};

export default AgentObservationsPage;
