import React from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Empty, Space, Spin, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { equipmentApi } from '@/api/equipment';

const { Title, Text } = Typography;

type Row = {
  id: string;
  name: string;
  ip: string;
  status: string;
  ownerId: string;
};

const ServersPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();

  const { data, isLoading } = useQuery({
    queryKey: ['equipment', 'servers', term],
    queryFn: () => equipmentApi.listServers(term, 200, 0),
    staleTime: 30_000,
  });

  const rows: Row[] = (data?.data || []).map((item) => {
    const row = item as Record<string, unknown>;
    const id = String(row.id || '');
    return {
      id,
      name: String(row.device_name || row.server_name || id || 'Сервер'),
      ip: String(row.ip || '-'),
      status: String(row.status || 'unknown'),
      ownerId: String(row.owner_id || ''),
    };
  });

  const columns: ColumnsType<Row> = [
    { title: 'Название', dataIndex: 'name', key: 'name' },
    { title: 'IP', dataIndex: 'ip', key: 'ip', width: 180 },
    {
      title: 'Владелец',
      dataIndex: 'ownerId',
      key: 'ownerId',
      width: 260,
      render: (ownerId: string) => (
        ownerId ? <Link to={`/companies/${ownerId}`} onClick={(e) => e.stopPropagation()}>{ownerId}</Link> : '-'
      ),
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 140,
      render: (status: string) => <Tag color={status === 'active' ? 'success' : 'default'}>{status}</Tag>,
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Серверы {term ? `по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Серверы не найдены" />
        ) : (
          <Table<Row>
            rowKey="id"
            dataSource={rows}
            columns={columns}
            pagination={{ pageSize: 20 }}
            onRow={(record) => ({
              onClick: () => record.id && navigate(`/servers/${record.id}`),
              style: { cursor: 'pointer' },
            })}
          />
        )}
        {!isLoading && rows.length > 0 && <Text type="secondary">Найдено: {rows.length}</Text>}
      </Card>
    </Space>
  );
};

export default ServersPage;

