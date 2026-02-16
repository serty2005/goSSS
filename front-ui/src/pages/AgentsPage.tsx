import React, { useMemo } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Space, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import dayjs from 'dayjs';
import { agentObservationsApi } from '@/api/agentObservations';
import { AgentListItemDTO } from '@/types/api';

const { Title } = Typography;

const AgentsPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();

  const { data, isLoading } = useQuery({
    queryKey: ['agents-list', term],
    queryFn: async () => {
      const response = await agentObservationsApi.listAgents({ term: term || undefined, limit: 500 });
      return response.data || [];
    },
    refetchOnWindowFocus: false,
  });

  const rows = useMemo(() => data || [], [data]);

  const columns: ColumnsType<AgentListItemDTO> = [
    {
      title: 'UUID',
      dataIndex: 'uuid',
      key: 'uuid',
      render: (value: string) => <Link to={`/agent-observations?agent=${encodeURIComponent(value)}`}>{value}</Link>,
    },
    { title: 'Hostname', dataIndex: 'hostname', key: 'hostname' },
    { title: 'Тип', dataIndex: 'type', key: 'type', width: 120 },
    { title: 'Статус', dataIndex: 'status', key: 'status', width: 160 },
    { title: 'Владелец', dataIndex: 'owner_id', key: 'owner_id', width: 220 },
    {
      title: 'Последнее наблюдение',
      dataIndex: 'last_observed_at',
      key: 'last_observed_at',
      width: 220,
      render: (value?: string) => value ? dayjs(value).format('DD.MM.YYYY HH:mm:ss') : '-',
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>Список агентов</Title>
      <Card className="glass-panel">
        <Table<AgentListItemDTO>
          rowKey="uuid"
          loading={isLoading}
          dataSource={rows}
          columns={columns}
          pagination={{ pageSize: 50 }}
        />
      </Card>
    </Space>
  );
};

export default AgentsPage;
