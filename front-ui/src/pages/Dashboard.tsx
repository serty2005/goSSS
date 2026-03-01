import React from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Col, Empty, Row, Space, Spin, Statistic, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ticketsApi } from '@/api/tickets';
import { DashboardResolvedByAssigneeDTO, DashboardServerStatusDTO } from '@/types/api';
import { useTicketParamsStore } from '@/store/ticketParamsStore';

const { Title, Text } = Typography;

const Dashboard: React.FC = () => {
  const navigate = useNavigate();
  const setTicketParams = useTicketParamsStore((state) => state.setTicketParams);
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
  const serverStatuses = stats?.server_statuses || [];

  const openResolvedByAssignee = (row: DashboardResolvedByAssigneeDTO) => {
    const userID = Number(row.user_id || 0);
    if (!userID) return;
    const params = new URLSearchParams();
    params.set('assignee_ids', String(userID));
    params.set('status', 'resolved,closed');
    params.set('archive_mode', 'active');
    setTicketParams(params.toString());
    navigate('/tickets');
  };

  const openServersByStatus = (row: DashboardServerStatusDTO) => {
    const status = String(row.status || '').trim();
    if (!status) return;
    navigate(`/servers?status=${encodeURIComponent(status)}`);
  };

  const resolvedColumns: ColumnsType<DashboardResolvedByAssigneeDTO> = [
    { title: 'Сотрудник', dataIndex: 'user_name', key: 'user_name' },
    { title: 'Решено заявок', dataIndex: 'count', key: 'count', width: 180 },
  ];

  const serverColumns: ColumnsType<DashboardServerStatusDTO> = [
    { title: 'Статус сервера', dataIndex: 'status', key: 'status' },
    { title: 'Количество', dataIndex: 'count', key: 'count', width: 140 },
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

      <Row gutter={16} align="stretch">
        <Col xs={24} lg={12}>
          <Card title="Решённые заявки по сотрудникам" className="glass-panel">
            {resolved.length === 0 ? (
              <Empty description="Пока нет данных по решённым заявкам" />
            ) : (
              <Table
                dataSource={resolved}
                columns={resolvedColumns}
                rowKey={(row) => `${row.user_id}`}
                pagination={false}
                size="small"
                onRow={(record) => ({
                  onClick: () => openResolvedByAssignee(record),
                  style: { cursor: 'pointer' },
                })}
              />
            )}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card title="Статусы серверов" className="glass-panel">
            {serverStatuses.length === 0 ? (
              <Empty description="Пока нет данных по статусам серверов" />
            ) : (
              <Table
                dataSource={serverStatuses}
                columns={serverColumns}
                rowKey={(row) => `${row.status}`}
                pagination={false}
                size="small"
                onRow={(record) => ({
                  onClick: () => openServersByStatus(record),
                  style: { cursor: 'pointer' },
                })}
              />
            )}
          </Card>
        </Col>
      </Row>

      <Text type="secondary">
        Обновление статистики выполняется в реальном времени из локальной БД.
      </Text>
    </Space>
  );
};

export default Dashboard;
