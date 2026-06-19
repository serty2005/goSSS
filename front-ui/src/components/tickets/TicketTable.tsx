import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Checkbox, DatePicker, Space, Spin, Tag, Tooltip, Typography, message } from 'antd';
import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';
import { LinkOutlined } from '@ant-design/icons';
import type { TableProps } from 'antd/es/table';
import { useInfiniteQuery, useMutation } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { profileApi } from '@/api/profile';
import { ticketsApi } from '@/api/tickets';
import { TicketListItemDTO, TicketStatus, UserProfileConfigDTO } from '@/types/api';
import { getTicketStatusMeta, isClosedLikeTicketStatus } from '@/constants/ticketStatus';
import { useAuthStore } from '@/store/authStore';
import { normalizeTicketPreview } from '@/utils/ticketText';
import DataTable, {
  DataTableColumn,
  DataTableLayoutColumn,
  DataTableTextCell,
  DataTableSortState,
} from '@/components/common/DataTable';
import {
  createDataTableColumnMinWidth,
  estimateDataTableHeaderMinWidth,
  serializeDataTableLayout,
} from '@/components/common/dataTableUtils';

interface Props {
  companyId?: string;
  companyIds?: string[];
  limit?: number;
  showCompanyColumn?: boolean;
  excludedTicketId?: string | number;
  rowOpenMode?: 'current' | 'new_tab';
  variant?: 'related' | 'workspace';
  dataSource?: TicketListItemDTO[];
  total?: number;
  loading?: TableProps<TicketListItemDTO>['loading'];
  visibleColumnKeys?: string[];
  showPeriodFilters?: boolean;
  showFooter?: boolean;
  layoutKey?: string;
  layoutStorage?: 'profile' | 'local' | 'none';
  sortState?: TableSortState;
  sortableColumnKeys?: string[];
  onSortChange?: (columnKey: string) => void;
  onRowClick?: (record: TicketListItemDTO, event: React.MouseEvent<HTMLElement>) => void;
  rowClassName?: TableProps<TicketListItemDTO>['rowClassName'];
  showSelectionColumn?: boolean;
  selectedTicketIds?: string[];
  onSelectedTicketIdsChange?: (ids: string[]) => void;
  emptyText?: string;
  availableColumnKeys?: string[];
  onVisibleColumnKeysChange?: (keys: string[]) => void;
  onLayoutChange?: (columns: TicketTableLayoutColumn[]) => void;
  layoutColumns?: TicketTableLayoutColumn[];
  columnFilters?: TicketTableColumnFilters;
}

type DateRangeValue = [Dayjs | null, Dayjs | null] | null;
type TicketTableColumn = DataTableColumn<TicketListItemDTO>;
type TicketTableLayoutColumn = DataTableLayoutColumn;
type TableSortState = DataTableSortState;

type TicketTableFilterOption = {
  value: string;
  label: React.ReactNode;
  count?: number;
};

type TicketTableColumnFilters = {
  status?: {
    values: string[];
    options: TicketTableFilterOption[];
    onChange: (values: string[]) => void;
  };
  company?: {
    values: string[];
    options: TicketTableFilterOption[];
    loading?: boolean;
    onChange: (values: string[]) => void;
  };
  assignee?: {
    values: string[];
    options: TicketTableFilterOption[];
    ownValue?: string;
    onChange: (values: string[]) => void;
  };
  created?: {
    value: DateRangeValue;
    onChange: (value: DateRangeValue) => void;
  };
  activity?: {
    value: DateRangeValue;
    onChange: (value: DateRangeValue) => void;
  };
};

const { Text } = Typography;
const { RangePicker } = DatePicker;
const TABLE_LAYOUT_KEY = 'company_ticket_table';
const WORKSPACE_SORTABLE_COLUMN_KEYS = ['number', 'assignee_display', 'created_at', 'last_activity'];

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

const createColumnMinWidth = createDataTableColumnMinWidth;
const estimateHeaderMinWidth = estimateDataTableHeaderMinWidth;

const resolveNewTicketAgeClass = (
  ticket: TicketListItemDTO,
  warningHours: number,
  criticalHours: number,
  now: number,
) => {
  if (ticket.status !== 'new' || !ticket.created_at) return '';
  const createdAt = dayjs(ticket.created_at);
  if (!createdAt.isValid()) return '';
  const ageHours = (now - createdAt.valueOf()) / 3_600_000;
  if (ageHours > criticalHours) return 'ticket-age-cell ticket-age-cell--critical';
  if (ageHours > warningHours) return 'ticket-age-cell ticket-age-cell--warning';
  return '';
};

const resolveSortValue = (row: TicketListItemDTO, key: string) => {
  switch (key) {
    case 'company_name':
    case 'company_display':
      return row.company_name || row.company_id || '';
    case 'number':
      return row.number || 0;
    case 'subject':
      return normalizeTicketPreview(row.description || row.subject || '');
    case 'status':
      return getTicketStatusMeta(row.status).label || row.status || '';
    case 'created_at':
      return resolveDateValue(row.created_at);
    case 'closed_at':
      return resolveDateValue(resolveClosedAt(row));
    case 'last_activity':
      return resolveDateValue(row.last_activity);
    case 'assignee':
    case 'assignee_display':
      return row.assignee?.full_name || '';
    case 'reporter_name':
    case 'reporter_display':
      return row.reporter_name || 'Сотрудник';
    case 'bitrix_deal_title':
      return row.bitrix_deal_title || '';
    case 'last_comment':
      return normalizeTicketPreview(row.last_comment || '');
    case 'sync_with_bitrix':
      return row.bitrix_deal_url || row.pyrus_task_url || '';
    default:
      return '';
  }
};

const getStatusTag = (status: TicketStatus, isCommonContract?: boolean) => {
  const meta = getTicketStatusMeta(status);
  return (
    <Space size={4}>
      <Tag color={meta.color}>{meta.label}</Tag>
      {isCommonContract && <Tag color="gold">Платный</Tag>}
    </Space>
  );
};

const formatDateParts = (value?: string) => {
  if (!value) return { date: '-', time: '--:--' };
  const parsed = dayjs(value);
  if (!parsed.isValid()) return { date: '-', time: '--:--' };
  return {
    date: parsed.format('DD.MM.YYYY'),
    time: parsed.format('HH:mm'),
  };
};

const formatDeferredDateTime = (value?: string) => {
  if (!value) return '';
  const parsed = dayjs(value);
  if (!parsed.isValid()) return '';
  return parsed.format('DD.MM.YYYY HH:mm');
};

const TicketTableDateStamp: React.FC<{ value?: string }> = ({ value }) => {
  const stamp = formatDateParts(value);
  return (
    <Space direction="vertical" size={0}>
      <Text className="company-ticket-table__date-part">{stamp.date}</Text>
      <Text type="secondary" className="company-ticket-table__time-part">{stamp.time}</Text>
    </Space>
  );
};

const TicketExternalLinks: React.FC<{ ticket: TicketListItemDTO }> = ({ ticket }) => {
  const links = [
    {
      label: 'B24',
      href: ticket.bitrix_deal_url,
      title: 'Открыть сделку Bitrix24',
      color: 'success',
    },
    {
      label: 'Pyrus',
      href: ticket.pyrus_task_url,
      title: 'Открыть задачу Pyrus',
      color: 'geekblue',
    },
  ].filter((item) => String(item.href || '').trim());

  if (links.length === 0) {
    return <Text type="secondary">-</Text>;
  }

  return (
    <Space size={4} wrap>
      {links.map((item) => (
        <Tooltip key={item.label} title={item.title}>
          <a
            href={item.href}
            target="_blank"
            rel="noreferrer"
            onClick={(event) => event.stopPropagation()}
            className="company-ticket-table__external-link"
          >
            <Tag color={item.color} style={{ marginInlineEnd: 0 }}>{item.label}</Tag>
            <LinkOutlined />
          </a>
        </Tooltip>
      ))}
    </Space>
  );
};

const TicketTable: React.FC<Props> = ({
  companyId,
  companyIds,
  limit = 20,
  showCompanyColumn = true,
  excludedTicketId,
  rowOpenMode = 'current',
  variant = 'related',
  dataSource,
  total,
  loading,
  visibleColumnKeys,
  showPeriodFilters,
  showFooter = true,
  layoutKey,
  layoutStorage = 'profile',
  sortState,
  sortableColumnKeys,
  onSortChange,
  onRowClick,
  rowClassName,
  showSelectionColumn,
  selectedTicketIds = [],
  onSelectedTicketIdsChange,
  emptyText = 'Тикеты не найдены',
  availableColumnKeys,
  onVisibleColumnKeysChange,
  onLayoutChange,
  layoutColumns,
  columnFilters,
}) => {
  const navigate = useNavigate();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const lastSubmittedLayoutSignatureRef = useRef('');
  const [createdRange, setCreatedRange] = useState<DateRangeValue>(null);
  const [closedRange, setClosedRange] = useState<DateRangeValue>(null);
  const [tableSort, setTableSort] = useState<TableSortState>(null);
  const [ageClock, setAgeClock] = useState(() => Date.now());
  const isControlledData = dataSource !== undefined;
  const resolvedLayoutKey = layoutKey || (variant === 'workspace' ? `tickets_workspace_table_${user?.id || 'guest'}` : TABLE_LAYOUT_KEY);
  const shouldShowPeriodFilters = showPeriodFilters ?? !isControlledData;
  const configuredWarningHours = Number(user?.profile_config?.tickets?.new_ticket_warning_hours ?? 1);
  const warningHours = Number.isFinite(configuredWarningHours) && configuredWarningHours > 0 ? configuredWarningHours : 1;
  const configuredCriticalHours = Number(user?.profile_config?.tickets?.new_ticket_critical_hours ?? 3);
  const criticalHours = Number.isFinite(configuredCriticalHours) && configuredCriticalHours > warningHours
    ? configuredCriticalHours
    : Math.max(3, warningHours + 2);

  useEffect(() => {
    const timer = window.setInterval(() => setAgeClock(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

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

  const updateProfileConfigMutation = useMutation({
    mutationFn: (config: UserProfileConfigDTO) =>
      profileApi.updateConfig({ profile_config: config }),
    onSuccess: (response) => {
      const dtoUser = (response as any)?.data;
      if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
        setUser(dtoUser as any);
      }
    },
  });

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
    enabled: !isControlledData,
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

  const rawRows = useMemo(
    () => (data?.pages || []).flatMap((pageData) => pageData.data || []),
    [data?.pages],
  );

  const rows = useMemo(() => {
    const sourceRows = isControlledData ? (dataSource || []) : rawRows;
    const excludedID = excludedTicketId === undefined || excludedTicketId === null ? '' : String(excludedTicketId);
    if (!excludedID) {
      return sourceRows;
    }
    return sourceRows.filter((item) => String(item.id) !== excludedID);
  }, [dataSource, excludedTicketId, isControlledData, rawRows]);
  const effectiveSort = sortState !== undefined ? sortState : tableSort;
  const fetchedTotal = data?.pages?.[0]?.meta?.total || 0;
  const visibleTotal = total ?? Math.max(0, fetchedTotal - (rawRows.length - rows.length));

  useEffect(() => {
    if (isControlledData) {
      return;
    }
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
  }, [fetchNextPage, hasNextPage, isControlledData, isFetchingNextPage, rows.length]);

  const ticketLinkTarget = rowOpenMode === 'new_tab' ? '_blank' : undefined;
  const ticketLinkRel = rowOpenMode === 'new_tab' ? 'noreferrer' : undefined;

  const columnsBase = useMemo<TicketTableColumn[]>(() => {
    const numberColumn: TicketTableColumn = {
      title: 'Номер',
      dataIndex: 'number',
      key: 'number',
      width: 105,
      minWidth: createColumnMinWidth('Номер'),
      maxWidth: 140,
      render: (val: number, record: TicketListItemDTO) => (
        <Link
          className="ticket-number-cell-link"
          to={`/tickets/${record.id}`}
          target={ticketLinkTarget}
          rel={ticketLinkRel}
          onClick={(event) => event.stopPropagation()}
        >
          <Text strong>#{val}</Text>
        </Link>
      ),
    };

    if (variant === 'workspace') {
      return [
        numberColumn,
        {
          title: 'Статус',
          dataIndex: 'status',
          key: 'status',
          width: 140,
          minWidth: createColumnMinWidth('Статус'),
          maxWidth: 190,
          onCell: (record) => ({ className: resolveNewTicketAgeClass(record, warningHours, criticalHours, ageClock) }),
          render: (status: TicketStatus, record: TicketListItemDTO) => {
            const tag = getStatusTag(status, record.is_common_contract);
            const deferredTitle = status === 'deferred'
              ? formatDeferredDateTime(record.deferred_until)
              : '';
            return deferredTitle ? <Tooltip title={`Отложено до ${deferredTitle}`}>{tag}</Tooltip> : tag;
          },
        },
        {
          title: 'Компания',
          dataIndex: 'company_name',
          key: 'company_display',
          width: 220,
          minWidth: createColumnMinWidth('Компания'),
          maxWidth: 360,
          render: (_value: string | undefined, record: TicketListItemDTO) => (
            <Link
              to={`/companies/${record.company_id}`}
              target="_blank"
              rel="noreferrer"
              className="company-ticket-table__company-link"
              title={record.company_name || record.company_id || 'Компания не указана'}
              onClick={(event) => event.stopPropagation()}
            >
              {record.company_name || record.company_id || 'Компания не указана'}
            </Link>
          ),
        },
        {
          title: 'Исполнитель',
          dataIndex: 'assignee',
          key: 'assignee_display',
          width: 170,
          minWidth: createColumnMinWidth('Исполнитель'),
          maxWidth: 320,
          render: (assignee?: { full_name: string }) => <DataTableTextCell value={assignee?.full_name} fallback="Не назначен" />,
        },
        {
          title: 'Автор',
          dataIndex: 'reporter_name',
          key: 'reporter_display',
          width: 180,
          minWidth: createColumnMinWidth('Автор'),
          maxWidth: 280,
          render: (value?: string) => <DataTableTextCell value={value} fallback="Сотрудник" />,
        },
        {
          title: 'Описание',
          dataIndex: 'description',
          key: 'subject',
          width: 260,
          minWidth: createColumnMinWidth('Описание'),
          maxWidth: 500,
          render: (textValue?: string) => {
            const preview = normalizeTicketPreview(textValue) || 'Без описания';
            return (
              <div className="tickets-table-multiline-cell" title={preview}>
                {preview}
              </div>
            );
          },
        },
        {
          title: 'Сделка Bitrix24',
          dataIndex: 'bitrix_deal_title',
          key: 'bitrix_deal_title',
          width: 240,
          minWidth: createColumnMinWidth('Сделка Bitrix24'),
          maxWidth: 420,
          render: (value?: string) => <DataTableTextCell value={value} />,
        },
        {
          title: 'Последний комментарий',
          dataIndex: 'last_comment',
          key: 'last_comment',
          width: 260,
          minWidth: createColumnMinWidth('Последний комментарий'),
          maxWidth: 500,
          render: (value?: string) => {
            const preview = normalizeTicketPreview(value) || '-';
            return (
              <div className="tickets-table-multiline-cell tickets-table-multiline-cell-secondary" title={preview}>
                {preview}
              </div>
            );
          },
        },
        {
          title: 'Создано',
          dataIndex: 'created_at',
          key: 'created_at',
          width: 120,
          minWidth: estimateHeaderMinWidth('Создано'),
          maxWidth: 180,
          onCell: (record) => ({ className: resolveNewTicketAgeClass(record, warningHours, criticalHours, ageClock) }),
          render: (date?: string) => <TicketTableDateStamp value={date} />,
        },
        {
          title: 'Обновлено',
          dataIndex: 'last_activity',
          key: 'last_activity',
          width: 120,
          minWidth: estimateHeaderMinWidth('Обновлено'),
          maxWidth: 180,
          onCell: (record) => ({ className: resolveNewTicketAgeClass(record, warningHours, criticalHours, ageClock) }),
          render: (date?: string) => <TicketTableDateStamp value={date} />,
        },
        {
          title: 'Внешние',
          dataIndex: 'sync_with_bitrix',
          key: 'sync_with_bitrix',
          width: 160,
          minWidth: createColumnMinWidth('Внешние'),
          maxWidth: 220,
          render: (_value: boolean | undefined, record: TicketListItemDTO) => <TicketExternalLinks ticket={record} />,
        },
      ];
    }

    return [
      {
        title: 'Компания',
        dataIndex: 'company_name',
        key: 'company_name',
        width: 220,
        minWidth: createColumnMinWidth('Компания'),
        maxWidth: 360,
        render: (_value: string | undefined, record: TicketListItemDTO) => (
          <Link
            to={`/companies/${record.company_id}`}
            target="_blank"
            rel="noreferrer"
            className="company-ticket-table__company-link"
            title={record.company_name || record.company_id || '-'}
            onClick={(event) => event.stopPropagation()}
          >
            {record.company_name || record.company_id || '-'}
          </Link>
        ),
      },
      numberColumn,
      {
        title: 'Описание',
        dataIndex: 'description',
        key: 'subject',
        width: 360,
        minWidth: createColumnMinWidth('Описание'),
        maxWidth: 640,
        render: (textValue?: string) => {
          const preview = normalizeTicketPreview(textValue) || 'Без описания';
          return (
            <Typography.Text
              className="company-ticket-table__description"
              ellipsis={{ tooltip: preview }}
            >
              {preview}
            </Typography.Text>
          );
        },
      },
      {
        title: 'Статус',
        dataIndex: 'status',
        key: 'status',
        width: 120,
        minWidth: createColumnMinWidth('Статус'),
        maxWidth: 180,
        render: (status: TicketStatus, record: TicketListItemDTO) => getStatusTag(status, record.is_common_contract),
      },
      {
        title: 'Дата создания',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 145,
        minWidth: estimateHeaderMinWidth('Дата создания'),
        maxWidth: 220,
        render: (date?: string) => <Text className="company-ticket-table__cell-nowrap">{formatDateTime(date)}</Text>,
      },
      {
        title: 'Дата закрытия',
        dataIndex: 'last_activity',
        key: 'closed_at',
        width: 145,
        minWidth: estimateHeaderMinWidth('Дата закрытия'),
        maxWidth: 220,
        render: (_date: string, record: TicketListItemDTO) => <Text className="company-ticket-table__cell-nowrap">{formatDateTime(resolveClosedAt(record))}</Text>,
      },
      {
        title: 'Исполнитель',
        dataIndex: 'assignee',
        key: 'assignee',
        width: 160,
        minWidth: createColumnMinWidth('Исполнитель'),
        maxWidth: 320,
        render: (assignee?: { full_name: string }) => <DataTableTextCell value={assignee?.full_name} />,
      },
      {
        title: 'Автор',
        dataIndex: 'reporter_name',
        key: 'reporter_name',
        width: 130,
        minWidth: createColumnMinWidth('Автор'),
        maxWidth: 240,
        render: (value?: string) => <DataTableTextCell value={value} fallback="Сотрудник" />,
      },
    ];
  }, [ageClock, criticalHours, ticketLinkRel, ticketLinkTarget, variant, warningHours]);

  const columnsBaseWithSort = useMemo<TicketTableColumn[]>(() => (
    columnsBase.map((column) => ({
      ...column,
      sortValue: (record) => resolveSortValue(record, column.key),
    }))
  ), [columnsBase]);

  const resolvedLayoutColumns = useMemo<TicketTableLayoutColumn[] | undefined>(() => {
    if (layoutColumns) {
      return layoutColumns;
    }
    if (layoutStorage !== 'profile') {
      return undefined;
    }
    return (user?.profile_config?.interface as any)?.[resolvedLayoutKey]?.columns;
  }, [layoutColumns, layoutStorage, resolvedLayoutKey, user?.profile_config]);

  const handleLayoutChange = useCallback((nextLayoutColumns: TicketTableLayoutColumn[]) => {
    if (layoutStorage === 'none' || nextLayoutColumns.length === 0) {
      return;
    }
    onLayoutChange?.(nextLayoutColumns);
    if (layoutStorage !== 'profile') {
      return;
    }

    if (!user) {
      return;
    }

    const currentLayoutColumns = (user.profile_config?.interface as any)?.[resolvedLayoutKey]?.columns;
    const nextLayoutSignature = serializeDataTableLayout(nextLayoutColumns);
    if (
      serializeDataTableLayout(currentLayoutColumns) === nextLayoutSignature ||
      lastSubmittedLayoutSignatureRef.current === nextLayoutSignature
    ) {
      return;
    }
    lastSubmittedLayoutSignatureRef.current = nextLayoutSignature;

    const previousUser = user;
    const nextConfig: UserProfileConfigDTO = {
      ...(user.profile_config || {}),
      interface: {
        ...((user.profile_config || {}).interface || {}),
        [resolvedLayoutKey]: {
          columns: nextLayoutColumns,
        },
      },
    };

    setUser({ ...user, profile_config: nextConfig });
    updateProfileConfigMutation.mutate(nextConfig, {
      onError: () => {
        lastSubmittedLayoutSignatureRef.current = serializeDataTableLayout(currentLayoutColumns);
        setUser(previousUser);
        message.error('Не удалось сохранить вид таблицы тикетов');
      },
    });
  }, [layoutStorage, onLayoutChange, resolvedLayoutKey, setUser, updateProfileConfigMutation, user]);

  const effectiveSortableColumnKeys = useMemo(() => {
    if (sortableColumnKeys) {
      return sortableColumnKeys;
    }
    return variant === 'workspace' ? WORKSPACE_SORTABLE_COLUMN_KEYS : undefined;
  }, [sortableColumnKeys, variant]);

  const renderColumnFilter = useCallback((columnKey: string) => {
    if (columnKey === 'status' && columnFilters?.status) {
      const filter = columnFilters.status;
      return (
        <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
          <Text strong>Статусы</Text>
          <Checkbox.Group
            value={filter.values}
            onChange={(values) => filter.onChange(values.map((value) => String(value)))}
          >
            <Space direction="vertical" size={4}>
              {filter.options.map((option) => (
                <Checkbox key={option.value} value={option.value}>
                  <Space size={6}>
                    <span>{option.label}</span>
                    {typeof option.count === 'number' && <Tag>{option.count}</Tag>}
                  </Space>
                </Checkbox>
              ))}
            </Space>
          </Checkbox.Group>
        </Space>
      );
    }

    if (columnKey === 'company_display' && columnFilters?.company) {
      const filter = columnFilters.company;
      return (
        <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
          <Text strong>Компании</Text>
          <Checkbox
            checked={filter.values.length === 0}
            onChange={() => filter.onChange([])}
          >
            Все компании
          </Checkbox>
          <Checkbox.Group
            value={filter.values}
            onChange={(values) => filter.onChange(values.map((value) => String(value)))}
          >
            <Space direction="vertical" size={4} style={{ marginTop: 2 }}>
              {filter.options.map((option) => (
                <Checkbox key={option.value} value={option.value}>
                  <Space size={6}>
                    <span>{option.label}</span>
                    {typeof option.count === 'number' && <Tag>{option.count}</Tag>}
                  </Space>
                </Checkbox>
              ))}
            </Space>
          </Checkbox.Group>
          {filter.loading && <Text type="secondary">Загрузка...</Text>}
        </Space>
      );
    }

    if (columnKey === 'assignee_display' && columnFilters?.assignee) {
      const filter = columnFilters.assignee;
      const isMine = Boolean(filter.ownValue) && filter.values.length === 1 && filter.values[0] === filter.ownValue;
      return (
        <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
          <Text strong>Сотрудники</Text>
          <Checkbox
            checked={isMine}
            disabled={!filter.ownValue}
            onChange={(event) => filter.onChange(event.target.checked && filter.ownValue ? [filter.ownValue] : [])}
          >
            Мои
          </Checkbox>
          <Checkbox.Group
            value={filter.values}
            onChange={(values) => filter.onChange(values.map((value) => String(value)))}
          >
            <Space direction="vertical" size={4}>
              {filter.options.map((option) => (
                <Checkbox key={option.value} value={option.value}>
                  {option.label}
                </Checkbox>
              ))}
            </Space>
          </Checkbox.Group>
        </Space>
      );
    }

    const dateFilter = columnKey === 'created_at'
      ? columnFilters?.created
      : columnKey === 'last_activity'
        ? columnFilters?.activity
        : undefined;
    if (dateFilter) {
      return (
        <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
          <Text strong>Период</Text>
          <RangePicker
            value={dateFilter.value}
            format="DD.MM.YYYY"
            allowClear
            onChange={(value) => dateFilter.onChange((value as DateRangeValue) || null)}
          />
        </Space>
      );
    }

    return null;
  }, [columnFilters]);

  const isColumnFilterActive = useCallback((columnKey: string) => {
    if (columnKey === 'status') {
      return Boolean(columnFilters?.status?.values.length);
    }
    if (columnKey === 'assignee_display') {
      return Boolean(columnFilters?.assignee?.values.length);
    }
    if (columnKey === 'company_display') {
      return Boolean(columnFilters?.company?.values.length);
    }
    if (columnKey === 'created_at') {
      return Boolean(columnFilters?.created?.value?.[0] || columnFilters?.created?.value?.[1]);
    }
    if (columnKey === 'last_activity') {
      return Boolean(columnFilters?.activity?.value?.[0] || columnFilters?.activity?.value?.[1]);
    }
    return false;
  }, [columnFilters]);

  const dataColumns = useMemo<TicketTableColumn[]>(() => (
    columnsBaseWithSort
      .filter((column) => {
        if (!showCompanyColumn && (column.key === 'company_name' || column.key === 'company_display')) {
          return false;
        }
        return true;
      })
      .map((column) => ({
        ...column,
        filterContent: renderColumnFilter(column.key),
        isFilterActive: isColumnFilterActive(column.key),
      }))
  ), [columnsBaseWithSort, isColumnFilterActive, renderColumnFilter, showCompanyColumn]);

  const sortedRowIds = useMemo(() => rows.map((item) => String(item.id)), [rows]);
  const selectedVisibleCount = useMemo(
    () => sortedRowIds.filter((id) => selectedTicketIds.includes(id)).length,
    [selectedTicketIds, sortedRowIds],
  );
  const shouldIncludeSelectionColumn = Boolean(
    showSelectionColumn ||
    visibleColumnKeys?.includes('selection') ||
    availableColumnKeys?.includes('selection'),
  );
  const tableColumns = useMemo<TicketTableColumn[]>(() => {
    if (!shouldIncludeSelectionColumn) {
      return dataColumns;
    }
    return [
      {
        key: 'selection',
        title: (
          <Checkbox
            checked={sortedRowIds.length > 0 && selectedVisibleCount === sortedRowIds.length}
            indeterminate={selectedVisibleCount > 0 && selectedVisibleCount < sortedRowIds.length}
            onChange={(event) => {
              onSelectedTicketIdsChange?.(event.target.checked ? sortedRowIds : []);
            }}
            onClick={(event) => event.stopPropagation()}
          />
        ),
        width: 44,
        minWidth: 44,
        menuTitle: 'Выбор',
        isSortable: false,
        autoFormatText: false,
        render: (_value: unknown, record: TicketListItemDTO) => (
          <Checkbox
            checked={selectedTicketIds.includes(String(record.id))}
            onChange={() => {
              const id = String(record.id);
              const next = selectedTicketIds.includes(id)
                ? selectedTicketIds.filter((item) => item !== id)
                : [...selectedTicketIds, id];
              onSelectedTicketIdsChange?.(next);
            }}
            onClick={(event) => event.stopPropagation()}
          />
        ),
      } as TicketTableColumn,
      ...dataColumns,
    ];
  }, [
    dataColumns,
    onSelectedTicketIdsChange,
    selectedTicketIds,
    selectedVisibleCount,
    shouldIncludeSelectionColumn,
    sortedRowIds,
  ]);

  const openTicket = useCallback((ticketID: string) => {
    const url = `/tickets/${ticketID}`;
    if (rowOpenMode === 'new_tab') {
      window.open(url, '_blank', 'noopener,noreferrer');
      return;
    }
    navigate(url);
  }, [navigate, rowOpenMode]);

  const tableLoading = loading ?? isLoading;

  return (
    <Space direction="vertical" size={10} style={{ width: '100%' }}>
      {shouldShowPeriodFilters && (
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
      )}

      <DataTable<TicketListItemDTO>
        dataSource={rows}
        columns={tableColumns}
        rowKey="id"
        loading={tableLoading}
        pagination={false}
        size="small"
        bordered
        tableLayout="fixed"
        visibleColumnKeys={visibleColumnKeys}
        availableColumnKeys={availableColumnKeys}
        onVisibleColumnKeysChange={onVisibleColumnKeysChange}
        layoutKey={resolvedLayoutKey}
        layoutStorage={layoutStorage === 'local' ? 'local' : 'none'}
        layoutColumns={resolvedLayoutColumns}
        onLayoutChange={handleLayoutChange}
        sortState={effectiveSort}
        onSortChange={onSortChange || ((columnKey) => {
          setTableSort((currentSort) => {
            if (currentSort?.key !== columnKey) {
              return { key: columnKey, order: 'asc' };
            }
            if (currentSort.order === 'asc') {
              return { key: columnKey, order: 'desc' };
            }
            return null;
          });
        })}
        sortableColumnKeys={effectiveSortableColumnKeys}
        emptyText={emptyText}
        rowClassName={rowClassName}
        onRow={(record) => ({
          onClick: (event) => {
            if (onRowClick) {
              onRowClick(record, event);
              return;
            }
            openTicket(String(record.id));
          },
          style: { cursor: 'pointer' },
        })}
      />

      {showFooter && (
      <div ref={loadMoreRef} style={{ marginTop: 4, display: 'flex', justifyContent: 'center', minHeight: 28 }}>
        {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
        {!hasNextPage && rows.length > 0 && (
          <Text type="secondary">Показано: {rows.length} из {visibleTotal}</Text>
        )}
        {!isLoading && rows.length === 0 && (
          <Text type="secondary">{emptyText}</Text>
        )}
      </div>
      )}
    </Space>
  );
};

export default TicketTable;
