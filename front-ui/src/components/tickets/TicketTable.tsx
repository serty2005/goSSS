import React, { useEffect, useMemo, useRef, useState } from 'react';
import { DatePicker, Space, Spin, Table, Tag, Typography } from 'antd';
import type { Dayjs } from 'dayjs';
import type { ColumnsType } from 'antd/es/table';
import { useInfiniteQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { ticketsApi } from '@/api/tickets';
import dayjs from 'dayjs';
import { TicketListItemDTO, TicketStatus } from '@/types/api';
import { getTicketStatusMeta, isClosedLikeTicketStatus } from '@/constants/ticketStatus';

interface Props {
  companyId?: string;
  companyIds?: string[];
  limit?: number;
}

type DateRangeValue = [Dayjs | null, Dayjs | null] | null;

const { Text } = Typography;
const { RangePicker } = DatePicker;

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

const formatDateTime = (value?: string) => {
  if (!value) return '-';
  const parsed = dayjs(value);
  if (!parsed.isValid()) return '-';
  return parsed.format('DD.MM.YYYY HH:mm');
};

const resolveClosedAt = (ticket: TicketListItemDTO) => {
  if (!isClosedLikeTicketStatus(ticket.status)) {
    return '';
  }
  return ticket.last_activity || '';
};

const resolveDateValue = (value?: string) => {
  if (!value) return 0;
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.valueOf() : 0;
};

const resolveRangeBounds = (range: DateRangeValue) => {
  const from = range?.[0] ? range[0].startOf('day').toISOString() : '';
  const to = range?.[1] ? range[1].endOf('day').toISOString() : '';
  return { from, to };
};

const TicketTable: React.FC<Props> = ({ companyId, companyIds, limit = 20 }) => {
  const navigate = useNavigate();
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const [createdRange, setCreatedRange] = useState<DateRangeValue>(null);
  const [closedRange, setClosedRange] = useState<DateRangeValue>(null);

  const normalizedCompanyIds = useMemo(() => {
    const source = companyIds?.length ? companyIds : (companyId ? [companyId] : []);
    const unique = new Set<string>();
    source.forEach((item) => {
      const value = String(item || '').trim();
      if (value) {
        unique.add(value);
      }
    });
    return Array.from(unique);
  }, [companyId, companyIds]);

  const createdBounds = useMemo(() => resolveRangeBounds(createdRange), [createdRange]);
  const closedBounds = useMemo(() => resolveRangeBounds(closedRange), [closedRange]);

  const {
    data,
    isLoading,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: [
      'tickets',
      'company-table',
      normalizedCompanyIds.join(','),
      limit,
      createdBounds.from,
      createdBounds.to,
      closedBounds.from,
      closedBounds.to,
    ],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      ticketsApi.getTickets({
        company_ids: normalizedCompanyIds.length > 0 ? normalizedCompanyIds : undefined,
        archive_mode: 'all',
        created_from: createdBounds.from || undefined,
        created_to: createdBounds.to || undefined,
        closed_from: closedBounds.from || undefined,
        closed_to: closedBounds.to || undefined,
        limit,
        offset: Number(pageParam) || 0,
      }),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || limit);
    },
    staleTime: 20_000,
  });

  const rows = useMemo(
    () => (data?.pages || []).flatMap((pageData) => pageData.data || []),
    [data?.pages],
  );
  const total = data?.pages?.[0]?.meta?.total || 0;

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasNextPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextPage) {
          return;
        }
        void fetchNextPage();
      },
      { rootMargin: '220px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, rows.length]);

  const getStatusTag = (status: TicketStatus, isCommonContract?: boolean) => {
    const meta = getTicketStatusMeta(status);
    return (
      <Space size={4}>
        <Tag color={meta.color}>{meta.label}</Tag>
        {isCommonContract && <Tag color="gold">Платный</Tag>}
      </Space>
    );
  };

  const columns: ColumnsType<TicketListItemDTO> = [
    {
      title: 'Компания',
      dataIndex: 'company_name',
      key: 'company_name',
      width: 220,
      sorter: (a, b) => String(a.company_name || a.company_id || '').localeCompare(String(b.company_name || b.company_id || ''), 'ru'),
      render: (_value: string | undefined, record: TicketListItemDTO) => (
        <Link to={`/companies/${record.company_id}`} onClick={(event) => event.stopPropagation()}>
          {record.company_name || record.company_id || '-'}
        </Link>
      ),
    },
    {
      title: 'Номер',
      dataIndex: 'number',
      key: 'number',
      width: 100,
      sorter: (a, b) => (a.number || 0) - (b.number || 0),
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
      title: 'Дата создания',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 150,
      sorter: (a, b) => resolveDateValue(a.created_at) - resolveDateValue(b.created_at),
      render: (date?: string) => formatDateTime(date),
    },
    {
      title: 'Дата закрытия',
      dataIndex: 'last_activity',
      key: 'closed_at',
      width: 150,
      sorter: (a, b) => resolveDateValue(resolveClosedAt(a)) - resolveDateValue(resolveClosedAt(b)),
      render: (_date: string, record: TicketListItemDTO) => formatDateTime(resolveClosedAt(record)),
    },
    {
      title: 'Исполнитель',
      dataIndex: 'assignee',
      key: 'assignee',
      sorter: (a, b) => String(a.assignee?.full_name || '').localeCompare(String(b.assignee?.full_name || ''), 'ru'),
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
    <Space direction="vertical" size={10} style={{ width: '100%' }}>
      <Space wrap size={8}>
        <Space direction="vertical" size={2}>
          <Text type="secondary">Период создания</Text>
          <RangePicker
            value={createdRange}
            format="DD.MM.YYYY"
            onChange={(value) => setCreatedRange((value as DateRangeValue) || null)}
          />
        </Space>
        <Space direction="vertical" size={2}>
          <Text type="secondary">Период закрытия (Решено)</Text>
          <RangePicker
            value={closedRange}
            format="DD.MM.YYYY"
            onChange={(value) => setClosedRange((value as DateRangeValue) || null)}
          />
        </Space>
      </Space>

      <Table
        dataSource={rows}
        columns={columns}
        rowKey="id"
        loading={isLoading}
        pagination={false}
        size="small"
        onRow={(record) => ({
          onClick: () => {
            navigate(`/tickets/${record.id}`);
          },
          style: { cursor: 'pointer' },
        })}
      />

      <div ref={loadMoreRef} style={{ marginTop: 4, display: 'flex', justifyContent: 'center', minHeight: 28 }}>
        {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
        {!hasNextPage && rows.length > 0 && (
          <Text type="secondary">Показано: {rows.length} из {total}</Text>
        )}
        {!isLoading && rows.length === 0 && (
          <Text type="secondary">Тикеты не найдены</Text>
        )}
      </div>
    </Space>
  );
};

export default TicketTable;
