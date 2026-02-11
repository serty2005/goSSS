import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, Col, Empty, Row, Space, Spin, Statistic, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ticketsApi } from '@/api/tickets';
import { DashboardResolvedByAssigneeDTO } from '@/types/api';

const { Title, Text } = Typography;

const Dashboard: React.FC = () => {
  const { data, isLoading } = useQuery({
    queryKey: ['dashboard-stats'],
    queryFn: () => ticketsApi.getDashboardStats(),
    staleTime: 30_000,
  });

  if (isLoading) {
    return <div style={{ textAlign: 'center', padding: 50 }}><Spin size="large" /></div>;
  }

  const stats = data?.data;
  const resolved = stats?.resolved_by_assignee || [];

  const columns: ColumnsType<DashboardResolvedByAssigneeDTO> = [
    { title: 'Сотрудник', dataIndex: 'user_name', key: 'user_name' },
    { title: 'Решено заявок', dataIndex: 'count', key: 'count', width: 180 },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={2} style={{ margin: 0 }}>Обзор системы</Title>

      <Row gutter={16}>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="Всего тикетов" value={stats?.total_tickets || 0} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="Опросы серверов за 24 часа" value={stats?.polled_servers_24h || 0} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card>
            <Statistic title="Сотрудников в статистике" value={resolved.length} />
          </Card>
        </Col>
      </Row>

      <Card title="Решённые заявки по сотрудникам" className="glass-panel">
        {resolved.length === 0 ? (
          <Empty description="Пока нет данных по решённым заявкам" />
        ) : (
          <Table
            dataSource={resolved}
            columns={columns}
            rowKey={(row) => `${row.user_id}`}
            pagination={false}
            size="small"
          />
        )}
        <Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
          Обновление статистики выполняется в реальном времени из локальной БД.
        </Text>
      </Card>
    </Space>
  );
};

export default Dashboard;
