import React from 'react';
import { Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { ticketsApi } from '@/api/tickets';
import dayjs from 'dayjs';
import { TicketStatus } from '@/types/api';

interface Props {
  companyId?: string;
  limit?: number;
  showPagination?: boolean;
}

const { Text } = Typography;

const TicketTable: React.FC<Props> = ({ companyId, limit = 10, showPagination = true }) => {
  const { data, isLoading } = useQuery({
    queryKey: ['tickets', companyId],
    queryFn: () => ticketsApi.getTickets({ company_id: companyId, limit }),
  });

  const getStatusTag = (status: TicketStatus) => {
    switch (status) {
      case 'new': return <Tag color="blue">Новая</Tag>;
      case 'in_progress': return <Tag color="processing">В работе</Tag>;
      case 'pending': return <Tag color="orange">Ожидание</Tag>;
      case 'resolved': return <Tag color="green">Решена</Tag>;
      case 'closed': return <Tag color="default">Закрыта</Tag>;
      default: return <Tag>{status}</Tag>;
    }
  };

  const columns = [
    {
      title: 'Номер',
      dataIndex: 'number',
      key: 'number',
      width: 100,
      render: (val: number) => <Text strong>#{val}</Text>,
    },
    {
      title: 'Тема',
      dataIndex: 'subject',
      key: 'subject',
      render: (textValue: string) => <Text style={{ color: '#1890ff', cursor: 'pointer' }}>{textValue}</Text>,
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (status: TicketStatus) => getStatusTag(status),
    },
    {
      title: 'Дата',
      dataIndex: 'last_activity',
      key: 'last_activity',
      width: 150,
      render: (date: string) => dayjs(date).format('DD.MM.YYYY HH:mm'),
    },
    {
      title: 'Исполнитель',
      dataIndex: 'assignee',
      key: 'assignee',
      render: (assignee?: { fullName: string }) => assignee?.fullName || '-',
    },
  ];

  return (
    <Table
      dataSource={data?.data}
      columns={columns}
      rowKey="id"
      loading={isLoading}
      pagination={showPagination ? { pageSize: limit } : false}
      size="small"
      onRow={() => ({
        onClick: () => {
          // Заглушка перехода, пока нет страницы тикета
          console.log('Go to ticket details');
        },
        style: { cursor: 'pointer' },
      })}
    />
  );
};

export default TicketTable;
