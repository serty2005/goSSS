import React from 'react';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Col, Empty, Row, Space, Spin, Statistic, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ticketsApi } from '@/api/tickets';
import {
  DashboardAcceptedCallsByEmployeeDTO,
  DashboardResolvedByAssigneeDTO,
  DashboardServerStatusDTO,
} from '@/types/api';
import { useTicketParamsStore } from '@/store/ticketParamsStore';

const { Text } = Typography;

type TicketPeriodKey = 'today' | 'days_7' | 'days_30';

const buildTicketPeriodRange = (period: TicketPeriodKey) => {
  const now = dayjs();
  switch (period) {
    case 'today':
      return { from: now.startOf('day'), to: now.endOf('day') };
    case 'days_7':
      return { from: now.subtract(6, 'day').startOf('day'), to: now.endOf('day') };
    default:
      return { from: now.subtract(29, 'day').startOf('day'), to: now.endOf('day') };
  }
};

const buildCallsPeriodRange = () => {
  const now = dayjs();
  return { from: now.subtract(24, 'hour'), to: now };
};

const DashboardStatsPanel: React.FC = () => {
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
  const acceptedCalls = stats?.accepted_calls_by_employee || [];
  const serverStatuses = stats?.server_statuses || [];

  const openResolvedByAssignee = (row: DashboardResolvedByAssigneeDTO, period: TicketPeriodKey) => {
    const userID = Number(row.user_id || 0);
    if (!userID) return;
    const periodRange = buildTicketPeriodRange(period);
    const params = new URLSearchParams();
    params.set('assignee_ids', String(userID));
    params.set('status', 'resolved,closed');
    params.set('archive_mode', 'all');
    params.set('closed_from', periodRange.from.toISOString());
    params.set('closed_to', periodRange.to.toISOString());
    setTicketParams(params.toString());
    navigate('/tickets');
  };

  const openCallsByEmployee = (row: DashboardAcceptedCallsByEmployeeDTO) => {
    const userID = Number(row.user_id || 0);
    if (!userID) return;
    const periodRange = buildCallsPeriodRange();
    const params = new URLSearchParams();
    params.set('started_from', periodRange.from.toISOString());
    params.set('started_to', periodRange.to.toISOString());
    navigate(`/telephony/users/${userID}/calls?${params.toString()}`);
  };

  const openServersByStatus = (row: DashboardServerStatusDTO) => {
    const status = String(row.status || '').trim();
    if (status) {
      navigate(`/servers?status=${encodeURIComponent(status)}`);
    }
  };

  const renderTicketCount = (value: number, row: DashboardResolvedByAssigneeDTO, period: TicketPeriodKey) =>
    value ? (
      <Button type="link" size="small" style={{ paddingInline: 0 }} onClick={() => openResolvedByAssignee(row, period)}>
        {value}
      </Button>
    ) : <Text type="secondary">0</Text>;

  const renderAcceptedCallsCount = (value: number, row: DashboardAcceptedCallsByEmployeeDTO) =>
    value ? (
      <Button type="link" size="small" style={{ paddingInline: 0 }} onClick={() => openCallsByEmployee(row)}>
        {value}
      </Button>
    ) : <Text type="secondary">0</Text>;

  const resolvedColumns: ColumnsType<DashboardResolvedByAssigneeDTO> = [
    { title: 'Сотрудник', dataIndex: 'user_name', key: 'user_name' },
    { title: 'Сегодня', dataIndex: 'today_count', key: 'today_count', width: 110, render: (value: number, row) => renderTicketCount(value, row, 'today') },
    { title: '7 дней', dataIndex: 'days_7_count', key: 'days_7_count', width: 110, render: (value: number, row) => renderTicketCount(value, row, 'days_7') },
    { title: '30 дней', dataIndex: 'days_30_count', key: 'days_30_count', width: 110, render: (value: number, row) => renderTicketCount(value, row, 'days_30') },
  ];

  const acceptedCallsColumns: ColumnsType<DashboardAcceptedCallsByEmployeeDTO> = [
    { title: 'Сотрудник', dataIndex: 'user_name', key: 'user_name' },
    { title: 'Принято за 24 часа', dataIndex: 'count', key: 'count', width: 160, render: (value: number, row) => renderAcceptedCallsCount(value, row) },
  ];

  const serverColumns: ColumnsType<DashboardServerStatusDTO> = [
    { title: 'Статус сервера', dataIndex: 'status', key: 'status' },
    { title: 'Количество', dataIndex: 'count', key: 'count', width: 120 },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Row gutter={[12, 12]}>
        <Col xs={24} md={8}>
          <Card><Statistic title="Всего тикетов" value={stats?.total_tickets || 0} /></Card>
        </Col>
        <Col xs={24} md={8}>
          <Card><Statistic title="Принято звонков за 24 часа" value={stats?.accepted_calls_24h || 0} /></Card>
        </Col>
        <Col xs={24} md={8}>
          <Card><Statistic title="Опросы серверов за 24 часа" value={stats?.polled_servers_24h || 0} /></Card>
        </Col>
      </Row>

      <Card title="Решённые заявки по сотрудникам" className="glass-panel">
        {resolved.length === 0 ? <Empty description="Пока нет данных по решённым заявкам" /> : (
          <Table dataSource={resolved} columns={resolvedColumns} rowKey={(row) => `${row.user_id}`} pagination={false} size="small" scroll={{ x: 'max-content' }} />
        )}
      </Card>

      <Card title="Принятые звонки по сотрудникам" className="glass-panel">
        {acceptedCalls.length === 0 ? <Empty description="Пока нет данных по принятым звонкам" /> : (
          <Table dataSource={acceptedCalls} columns={acceptedCallsColumns} rowKey={(row) => `${row.user_id}`} pagination={false} size="small" scroll={{ x: 'max-content' }} />
        )}
      </Card>

      <Card title="Статусы серверов" className="glass-panel">
        {serverStatuses.length === 0 ? <Empty description="Пока нет данных по статусам серверов" /> : (
          <Table
            dataSource={serverStatuses}
            columns={serverColumns}
            rowKey={(row) => `${row.status}`}
            pagination={false}
            size="small"
            scroll={{ x: 'max-content' }}
            onRow={(record) => ({ onClick: () => openServersByStatus(record), style: { cursor: 'pointer' } })}
          />
        )}
      </Card>
    </Space>
  );
};

export default DashboardStatsPanel;
