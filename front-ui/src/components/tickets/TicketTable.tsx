import React from 'react';
import { Space, Table, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import dayjs from 'dayjs';
import { TicketListItemDTO, TicketStatus } from '@/types/api';

interface Props {
  companyId?: string;
  limit?: number;
  showPagination?: boolean;
}

const { Text } = Typography;

const normalizeDescription = (value?: string) => {
  if (!value) return '';
  return value
    .replace(/<\s*br\s*\/?>/gi, '\n')
    .replace(/<\/p>\s*<p>/gi, '\n')
    .replace(/<\/?p[^>]*>/gi, '\n')
    .replace(/<[^>]*>/g, ' ')
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/\s+/g, ' ')
    .trim();
};

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
      case 'deferred':
        return (
          <Space size={4}>
            <Tag color="orange">Отложено</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'onsite':
        return (
          <Space size={4}>
            <Tag color="cyan">На выезд</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'to_manager':
        return (
          <Space size={4}>
            <Tag color="purple">Передать менеджеру</Tag>
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
      case 'spam':
        return (
          <Space size={4}>
            <Tag color="red">Спам</Tag>
            {isCommonContract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      case 'execution':
        return (
          <Space size={4}>
            <Tag color="magenta">Реализация</Tag>
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
      render: (val: number, record: TicketListItemDTO) => (
        <Link to={`/tickets/${record.id}`} onClick={(event) => event.stopPropagation()}>
          <Text strong>#{val}</Text>
        </Link>
      ),
    },
    {
      title: 'Описание',
      dataIndex: 'description',
      key: 'subject',
      render: (textValue?: string) => <Text>{normalizeDescription(textValue) || 'Без описания'}</Text>,
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
      render: (assignee?: { full_name: string }) => assignee?.full_name || '-',
    },
    {
      title: 'Автор',
      dataIndex: 'reporter_name',
      key: 'reporter_name',
      render: (value?: string) => value || 'Сотрудник',
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


