import React from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Empty, Space, Spin, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { equipmentApi } from '@/api/equipment';

const { Title, Text } = Typography;

type Row = {
  id: string;
  name: string;
  anydesk: string;
  teamviewer: string;
  ownerId: string;
};

const WorkstationsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();

  const { data, isLoading } = useQuery({
    queryKey: ['equipment', 'workstations', term],
    queryFn: () => equipmentApi.listWorkstations(term, 200, 0),
    staleTime: 30_000,
  });

  const rows: Row[] = (data?.data || []).map((item) => {
    const row = item as Record<string, unknown>;
    const id = String(row.ID || row.id || '');
    return {
      id,
      name: String(row.DeviceName || row.device_name || id || 'Рабочая станция'),
      anydesk: String(row.Anydesk || row.anydesk || '-'),
      teamviewer: String(row.Teamviewer || row.teamviewer || '-'),
      ownerId: String(row.OwnerID || row.owner_id || ''),
    };
  });

  const columns: ColumnsType<Row> = [
    { title: 'Название', dataIndex: 'name', key: 'name' },
    { title: 'AnyDesk', dataIndex: 'anydesk', key: 'anydesk', width: 180 },
    { title: 'TeamViewer', dataIndex: 'teamviewer', key: 'teamviewer', width: 180 },
    {
      title: 'Владелец',
      dataIndex: 'ownerId',
      key: 'ownerId',
      width: 260,
      render: (ownerId: string) => (
        ownerId ? <Link to={`/companies/${ownerId}`} onClick={(e) => e.stopPropagation()}>{ownerId}</Link> : '-'
      ),
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Рабочие станции {term ? `по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Рабочие станции не найдены" />
        ) : (
          <Table<Row>
            rowKey="id"
            dataSource={rows}
            columns={columns}
            pagination={{ pageSize: 20 }}
            onRow={(record) => ({
              onClick: () => record.id && navigate(`/workstations/${record.id}`),
              style: { cursor: 'pointer' },
            })}
          />
        )}
        {!isLoading && rows.length > 0 && <Text type="secondary">Найдено: {rows.length}</Text>}
      </Card>
    </Space>
  );
};

export default WorkstationsPage;
