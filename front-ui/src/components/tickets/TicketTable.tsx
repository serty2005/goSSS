import React from 'react';
import { Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { ticketsApi } from '@/api/tickets';
import dayjs from 'dayjs';

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

  const getStatusTag = (status: string) => {
    switch (status) {
      case 'registered': return <Tag color="blue">Новая</Tag>;
      case 'inprogress': return <Tag color="processing">В работе</Tag>;
      case 'wait': return <Tag color="orange">Ожидание</Tag>;
      case 'closed': return <Tag color="green">Закрыта</Tag>;
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
      render: (text: string) => <Text style={{ color: '#1890ff', cursor: 'pointer' }}>{text}</Text>,
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (status: string) => getStatusTag(status),
    },
    {
      title: 'Дата',
      dataIndex: 'updated_at',
      key: 'updated_at',
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
        style: { cursor: 'pointer' } 
      })}
    />
  );
};

export default TicketTable;