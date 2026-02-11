import React from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Empty, Space, Spin, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { equipmentApi } from '@/api/equipment';

const { Title, Text } = Typography;

type Row = {
  id: string;
  model: string;
  rnm: string;
  serial: string;
  ownerId: string;
};

const FiscalsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();

  const { data, isLoading } = useQuery({
    queryKey: ['equipment', 'fiscals', term],
    queryFn: () => equipmentApi.listFiscals(term, 200, 0),
    staleTime: 30_000,
  });

  const rows: Row[] = (data?.data || []).map((item) => {
    const row = item as Record<string, unknown>;
    const id = String(row.ID || row.id || '');
    return {
      id,
      model: String(row.ModelKKT || row.model_kkt || 'ККТ'),
      rnm: String(row.RNKKT || row.rn_kkt || '-'),
      serial: String(row.FRSerialNumber || row.fr_serial_number || '-'),
      ownerId: String(row.OwnerID || row.owner_id || ''),
    };
  });

  const columns: ColumnsType<Row> = [
    { title: 'Модель', dataIndex: 'model', key: 'model' },
    { title: 'РНМ', dataIndex: 'rnm', key: 'rnm', width: 180 },
    { title: 'Серийный номер', dataIndex: 'serial', key: 'serial', width: 200 },
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
        Фискальные регистраторы {term ? `по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Фискальные регистраторы не найдены" />
        ) : (
          <Table<Row>
            rowKey="id"
            dataSource={rows}
            columns={columns}
            pagination={{ pageSize: 20 }}
            onRow={(record) => ({
              onClick: () => record.id && navigate(`/fiscals/${record.id}`),
              style: { cursor: 'pointer' },
            })}
          />
        )}
        {!isLoading && rows.length > 0 && <Text type="secondary">Найдено: {rows.length}</Text>}
      </Card>
    </Space>
  );
};

export default FiscalsPage;
