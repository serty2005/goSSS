import React from 'react';
import { Space, Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import dayjs from 'dayjs';
import { TicketListItemDTO, TicketStatus } from '@/types/api';

interface Props {
  companyId?: string;
  limit?: number;
  showPagination?: boolean;
}

const { Text } = Typography;

const TicketTable: React.FC<Props> = ({ companyId, limit = 10, showPagination = true }) => {
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({
    queryKey: ['tickets', 'company-table', companyId, limit],
    queryFn: () => ticketsApi.getTickets({ company_id: companyId, limit }),
  });

  const getStatusTag = (status: TicketStatus, isCommonContract?: boolean) => {
    switch (status) {
      case 'new':
        return (
          <Space size={4}>
            <Tag color="blue">Новая</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'in_progress':
        return (
          <Space size={4}>
            <Tag color="processing">В работе</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'pending':
        return (
          <Space size={4}>
            <Tag color="orange">Ожидание</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'resolved':
        return (
          <Space size={4}>
            <Tag color="green">Решена</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'closed':
        return (
          <Space size={4}>
            <Tag color="default">Закрыта</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      default:
        return (
          <Space size={4}>
            <Tag>{status}</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
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
      render: (status: TicketStatus, record: TicketListItemDTO) => getStatusTag(status, record.is_common_contract),
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
      onRow={(record) => ({
        onClick: () => {
          navigate(`/tickets/${record.id}`);
        },
        style: { cursor: 'pointer' },
      })}
    />
  );
};

export default TicketTable;

