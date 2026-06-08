import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Checkbox, DatePicker, Popover, Space, Spin, Table, Tag, Tooltip, Typography, message } from 'antd';
import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';
import { FilterOutlined, LinkOutlined } from '@ant-design/icons';
import type { ColumnsType, TableProps } from 'antd/es/table';
import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Resizable } from 'react-resizable';
import { useInfiniteQuery, useMutation } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { profileApi } from '@/api/profile';
import { ticketsApi } from '@/api/tickets';
import { TicketListItemDTO, TicketStatus, UserProfileConfigDTO } from '@/types/api';
import { getTicketStatusMeta, isClosedLikeTicketStatus } from '@/constants/ticketStatus';
import { useAuthStore } from '@/store/authStore';
import { normalizeTicketPreview } from '@/utils/ticketText';

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
type TicketTableColumn = ColumnsType<TicketListItemDTO>[number] & {
  key: string;
  width: number;
  minWidth: number;
  maxWidth?: number;
};
type TicketTableLayoutColumn = {
  key: string;
  width?: number;
};
type TableSortOrder = 'asc' | 'desc';
type TableSortState = {
  key: string;
  order: TableSortOrder;
} | null;
type HeaderCellProps = React.HTMLAttributes<HTMLTableCellElement> & {
  id?: string;
  width?: number;
  minWidth?: number;
  isDragDisabled?: boolean;
  isSortable?: boolean;
  sortOrder?: TableSortOrder | null;
  onSort?: () => void;
  onResize?: (
    event: React.SyntheticEvent,
    data: { size: { width: number; height: number } },
  ) => void;
  onResizeStart?: () => void;
  onResizeStop?: () => void;
  isResizing?: boolean;
  filterContent?: React.ReactNode;
  isFilterActive?: boolean;
};

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
const DEFAULT_MIN_COLUMN_WIDTH = 90;
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

const estimateHeaderMinWidth = (title: string) => Math.max(80, title.length * 8 + 44);
const createColumnMinWidth = (title: string, fallback = DEFAULT_MIN_COLUMN_WIDTH) =>
  Math.max(fallback, estimateHeaderMinWidth(title));

const clampColumnWidth = (column: TicketTableColumn, width?: number) => {
  const currentWidth = Number(width ?? column.width ?? column.minWidth);
  const minWidth = column.minWidth || DEFAULT_MIN_COLUMN_WIDTH;
  const maxWidth = column.maxWidth;
  const bounded = Math.max(Number.isFinite(currentWidth) ? currentWidth : minWidth, minWidth);
  return typeof maxWidth === 'number' ? Math.min(bounded, maxWidth) : bounded;
};

const applyStoredColumnLayout = (
  baseColumns: TicketTableColumn[],
  storedColumns?: TicketTableLayoutColumn[],
) => {
  if (!Array.isArray(storedColumns) || storedColumns.length === 0) {
    return baseColumns;
  }

  const baseByKey = new Map(baseColumns.map((column) => [column.key, column]));
  const next: TicketTableColumn[] = [];
  const seen = new Set<string>();

  storedColumns.forEach((storedColumn) => {
    const baseColumn = baseByKey.get(String(storedColumn.key || ''));
    if (!baseColumn) {
      return;
    }
    next.push({
      ...baseColumn,
      width: clampColumnWidth(baseColumn, storedColumn.width),
    });
    seen.add(baseColumn.key);
  });

  baseColumns.forEach((column) => {
    if (!seen.has(column.key)) {
      next.push(column);
    }
  });

  return next.length ? next : baseColumns;
};

const serializeColumnsLayout = (columns?: TicketTableLayoutColumn[]) =>
  JSON.stringify((columns || []).map((column) => ({
    key: String(column.key || ''),
    width: typeof column.width === 'number' ? column.width : undefined,
  })));

const compareText = (left?: string, right?: string) =>
  String(left || '').localeCompare(String(right || ''), 'ru', { numeric: true, sensitivity: 'base' });

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

const ResizableHeaderCell = React.forwardRef<HTMLTableCellElement, HeaderCellProps>(
  (props, ref) => {
    const {
      onResize,
      onResizeStart,
      onResizeStop,
      width,
      minWidth,
      children,
      ...rest
    } = props;

    if (!width || !onResize) {
      return (
        <th ref={ref} {...rest}>
          {children}
        </th>
      );
    }

    return (
      <Resizable
        width={width}
        height={0}
        handle={
          <span
            className="resize-handle"
            onMouseDown={(event) => event.stopPropagation()}
            onTouchStart={(event) => event.stopPropagation()}
          />
        }
        onResize={onResize}
        onResizeStart={onResizeStart}
        onResizeStop={onResizeStop}
        minConstraints={[minWidth || DEFAULT_MIN_COLUMN_WIDTH, 0]}
        draggableOpts={{ enableUserSelectHack: false }}
      >
        <th ref={ref} {...rest}>
          {children}
        </th>
      </Resizable>
    );
  },
);

ResizableHeaderCell.displayName = 'ResizableHeaderCell';

const DraggableHeaderCell: React.FC<HeaderCellProps> = ({
  id,
  style,
  isResizing,
  isDragDisabled,
  isSortable,
  sortOrder,
  onSort,
  filterContent,
  isFilterActive,
  children,
  className,
  ...rest
}) => {
  const sortableDisabled = Boolean(isResizing || isDragDisabled || !id);
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: id || '', disabled: sortableDisabled });

  const mergedStyle: React.CSSProperties = {
    ...style,
    transform: CSS.Transform.toString(transform),
    transition,
    ...(isDragging ? { position: 'relative', zIndex: 2 } : {}),
  };
  const mergedClassName = [
    className,
    isSortable ? 'company-ticket-table__header-cell--sortable' : '',
    sortOrder ? 'company-ticket-table__header-cell--sorted' : '',
  ].filter(Boolean).join(' ');

  return (
    <ResizableHeaderCell
      ref={setNodeRef}
      style={mergedStyle}
      className={mergedClassName}
      aria-sort={
        sortOrder === 'asc' ? 'ascending' : sortOrder === 'desc' ? 'descending' : undefined
      }
      {...attributes}
      {...rest}
    >
      <div
        ref={setActivatorNodeRef}
        className="tickets-table-header company-ticket-table__header"
        title={isSortable ? 'Клик - сортировка, потянуть - переместить столбец' : undefined}
        onClick={(event) => {
          event.stopPropagation();
          onSort?.();
        }}
        onMouseDown={(event) => event.stopPropagation()}
        onTouchStart={(event) => event.stopPropagation()}
        {...(!sortableDisabled ? listeners : {})}
      >
        <span className="tickets-table-header-title company-ticket-table__header-title">
          {children}
        </span>
        {sortOrder && (
          <span className="company-ticket-table__sort-marker" aria-hidden="true">
            {sortOrder === 'asc' ? '↑' : '↓'}
          </span>
        )}
        {filterContent && (
          <Popover
            trigger="click"
            placement="bottomRight"
            content={filterContent}
          >
            <Button
              type={isFilterActive ? 'primary' : 'text'}
              size="small"
              icon={<FilterOutlined />}
              className="company-ticket-table__filter-button"
              aria-label="Фильтр столбца"
              onClick={(event) => event.stopPropagation()}
              onMouseDown={(event) => event.stopPropagation()}
              onTouchStart={(event) => event.stopPropagation()}
            />
          </Popover>
        )}
      </div>
    </ResizableHeaderCell>
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
  const columnsStateRef = useRef<TicketTableColumn[]>([]);
  const isResizeActiveRef = useRef(false);
  const suppressHeaderClickRef = useRef(false);
  const lastSubmittedLayoutSignatureRef = useRef('');
  const [createdRange, setCreatedRange] = useState<DateRangeValue>(null);
  const [closedRange, setClosedRange] = useState<DateRangeValue>(null);
  const [isResizingColumn, setIsResizingColumn] = useState(false);
  const [tableSort, setTableSort] = useState<TableSortState>(null);
  const [columnsMenu, setColumnsMenu] = useState<{ open: boolean; x: number; y: number }>({
    open: false,
    x: 0,
    y: 0,
  });
  const isControlledData = dataSource !== undefined;
  const resolvedLayoutKey = layoutKey || (variant === 'workspace' ? `tickets_workspace_table_${user?.id || 'guest'}` : TABLE_LAYOUT_KEY);
  const shouldShowPeriodFilters = showPeriodFilters ?? !isControlledData;
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 6 },
    }),
  );

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
  const sortedRows = useMemo(() => {
    if (!effectiveSort) {
      return rows;
    }

    return [...rows].sort((left, right) => {
      const leftValue = resolveSortValue(left, effectiveSort.key);
      const rightValue = resolveSortValue(right, effectiveSort.key);
      const result = typeof leftValue === 'number' && typeof rightValue === 'number'
        ? leftValue - rightValue
        : compareText(String(leftValue), String(rightValue));
      return effectiveSort.order === 'asc' ? result : -result;
    });
  }, [effectiveSort, rows]);
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
          render: (assignee?: { full_name: string }) => (
            <div className="company-ticket-table__cell-ellipsis" title={assignee?.full_name || 'Не назначен'}>
              {assignee?.full_name || 'Не назначен'}
            </div>
          ),
        },
        {
          title: 'Автор',
          dataIndex: 'reporter_name',
          key: 'reporter_display',
          width: 180,
          minWidth: createColumnMinWidth('Автор'),
          maxWidth: 280,
          render: (value?: string) => (
            <div className="company-ticket-table__cell-ellipsis" title={value || 'Сотрудник'}>
              {value || 'Сотрудник'}
            </div>
          ),
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
          render: (value?: string) => (
            <div className="company-ticket-table__cell-ellipsis" title={value || '-'}>
              {value || '-'}
            </div>
          ),
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
          render: (date?: string) => <TicketTableDateStamp value={date} />,
        },
        {
          title: 'Обновлено',
          dataIndex: 'last_activity',
          key: 'last_activity',
          width: 120,
          minWidth: estimateHeaderMinWidth('Обновлено'),
          maxWidth: 180,
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
        render: (assignee?: { full_name: string }) => (
          <div className="company-ticket-table__cell-ellipsis" title={assignee?.full_name || '-'}>
            {assignee?.full_name || '-'}
          </div>
        ),
      },
      {
        title: 'Автор',
        dataIndex: 'reporter_name',
        key: 'reporter_name',
        width: 130,
        minWidth: createColumnMinWidth('Автор'),
        maxWidth: 240,
        render: (value?: string) => (
          <div className="company-ticket-table__cell-ellipsis" title={value || 'Сотрудник'}>
            {value || 'Сотрудник'}
          </div>
        ),
      },
    ];
  }, [ticketLinkRel, ticketLinkTarget, variant]);

  const [columnsState, setColumnsState] = useState<TicketTableColumn[]>(columnsBase);

  useEffect(() => {
    if (layoutStorage === 'none') {
      columnsStateRef.current = columnsBase;
      setColumnsState(columnsBase);
      return;
    }

    let storedColumns: TicketTableLayoutColumn[] | undefined = layoutColumns;
    if (layoutStorage === 'local') {
      if (!storedColumns) {
        try {
          const raw = window.localStorage.getItem(resolvedLayoutKey);
          storedColumns = raw ? JSON.parse(raw) : undefined;
        } catch {
          storedColumns = undefined;
        }
      }
    } else if (!storedColumns) {
      storedColumns = (user?.profile_config?.interface as any)?.[resolvedLayoutKey]?.columns;
    }
    const nextColumns = applyStoredColumnLayout(columnsBase, storedColumns);
    columnsStateRef.current = nextColumns;
    setColumnsState(nextColumns);
  }, [columnsBase, layoutColumns, layoutStorage, resolvedLayoutKey, user?.id, user?.profile_config]);

  const saveColumnsLayout = useCallback((nextColumns: TicketTableColumn[]) => {
    if (layoutStorage === 'none' || nextColumns.length === 0) {
      return;
    }

    if (layoutStorage === 'local') {
      const nextLayoutColumns = nextColumns.map((column) => ({
        key: column.key,
        width: column.width,
      }));
      window.localStorage.setItem(
        resolvedLayoutKey,
        serializeColumnsLayout(nextLayoutColumns),
      );
      onLayoutChange?.(nextLayoutColumns);
      return;
    }

    if (!user) {
      return;
    }

    const nextLayoutColumns = nextColumns.map((column) => ({
      key: column.key,
      width: column.width,
    }));
    onLayoutChange?.(nextLayoutColumns);
    const currentLayoutColumns = (user.profile_config?.interface as any)?.[resolvedLayoutKey]?.columns;
    const nextLayoutSignature = serializeColumnsLayout(nextLayoutColumns);
    if (
      serializeColumnsLayout(currentLayoutColumns) === nextLayoutSignature ||
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
        lastSubmittedLayoutSignatureRef.current = serializeColumnsLayout(currentLayoutColumns);
        setUser(previousUser);
        message.error('Не удалось сохранить вид таблицы тикетов');
      },
    });
  }, [layoutStorage, onLayoutChange, resolvedLayoutKey, setUser, updateProfileConfigMutation, user]);

  const handleResize = useCallback(
    (stateIndex: number) =>
      (_event: React.SyntheticEvent, data: { size: { width: number } }) => {
        if (stateIndex < 0) {
          return;
        }
        setColumnsState((currentColumns) => {
          const nextColumns = [...currentColumns];
          const currentColumn = nextColumns[stateIndex];
          if (!currentColumn) {
            return currentColumns;
          }
          nextColumns[stateIndex] = {
            ...currentColumn,
            width: clampColumnWidth(currentColumn, data.size.width),
          };
          columnsStateRef.current = nextColumns;
          return nextColumns;
        });
      },
    [],
  );

  const handleResizeStop = useCallback(() => {
    setIsResizingColumn(false);
    if (!isResizeActiveRef.current) {
      return;
    }
    isResizeActiveRef.current = false;
    saveColumnsLayout(columnsStateRef.current);
  }, [saveColumnsLayout]);

  const handleDragEnd = useCallback(({ active, over }: DragEndEvent) => {
    window.setTimeout(() => {
      suppressHeaderClickRef.current = false;
    }, 0);

    if (!over || active.id === over.id) {
      return;
    }

    const activeID = String(active.id);
    const overID = String(over.id);
    setColumnsState((currentColumns) => {
      const oldIndex = currentColumns.findIndex((column) => column.key === activeID);
      const newIndex = currentColumns.findIndex((column) => column.key === overID);
      if (oldIndex === -1 || newIndex === -1) {
        return currentColumns;
      }
      const nextColumns = arrayMove(currentColumns, oldIndex, newIndex);
      columnsStateRef.current = nextColumns;
      saveColumnsLayout(nextColumns);
      return nextColumns;
    });
  }, [saveColumnsLayout]);

  const handleDragStart = useCallback(() => {
    suppressHeaderClickRef.current = true;
  }, []);

  const handleDragCancel = useCallback(() => {
    window.setTimeout(() => {
      suppressHeaderClickRef.current = false;
    }, 0);
  }, []);

  const toggleSort = useCallback((columnKey: string) => {
    if (isResizingColumn || suppressHeaderClickRef.current) {
      return;
    }

    if (onSortChange) {
      onSortChange(columnKey);
      return;
    }

    setTableSort((currentSort) => {
      if (currentSort?.key !== columnKey) {
        return { key: columnKey, order: 'asc' };
      }
      if (currentSort.order === 'asc') {
        return { key: columnKey, order: 'desc' };
      }
      return null;
    });
  }, [isResizingColumn, onSortChange]);

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
    if (columnKey === 'created_at') {
      return Boolean(columnFilters?.created?.value?.[0] || columnFilters?.created?.value?.[1]);
    }
    if (columnKey === 'last_activity') {
      return Boolean(columnFilters?.activity?.value?.[0] || columnFilters?.activity?.value?.[1]);
    }
    return false;
  }, [columnFilters]);

  const columns = useMemo<ColumnsType<TicketListItemDTO>>(() => (
    columnsState
      .filter((column) => {
        if (!showCompanyColumn && column.key === 'company_name') {
          return false;
        }
        if (!visibleColumnKeys) {
          return true;
        }
        return visibleColumnKeys.includes(column.key);
      })
      .map((column) => {
        const stateIndex = columnsState.findIndex((item) => item.key === column.key);
        const isSortable = effectiveSortableColumnKeys
          ? effectiveSortableColumnKeys.includes(column.key)
          : true;
        const filterContent = renderColumnFilter(column.key);
        return {
          ...column,
          onHeaderCell: () => ({
            id: column.key,
            width: column.width,
            minWidth: column.minWidth,
            isSortable,
            sortOrder: isSortable && effectiveSort?.key === column.key ? effectiveSort.order : null,
            onSort: isSortable ? () => toggleSort(column.key) : undefined,
            filterContent,
            isFilterActive: isColumnFilterActive(column.key),
            onResize: handleResize(stateIndex),
            onResizeStart: () => {
              isResizeActiveRef.current = true;
              setIsResizingColumn(true);
            },
            onResizeStop: handleResizeStop,
            isResizing: isResizingColumn,
          }),
        };
      })
  ), [
    columnsState,
    effectiveSort,
    effectiveSortableColumnKeys,
    handleResize,
    handleResizeStop,
    isColumnFilterActive,
    isResizingColumn,
    renderColumnFilter,
    showCompanyColumn,
    toggleSort,
    visibleColumnKeys,
  ]);

  const sortedRowIds = useMemo(() => sortedRows.map((item) => String(item.id)), [sortedRows]);
  const selectedVisibleCount = useMemo(
    () => sortedRowIds.filter((id) => selectedTicketIds.includes(id)).length,
    [selectedTicketIds, sortedRowIds],
  );
  const shouldShowSelectionColumn = Boolean(showSelectionColumn || visibleColumnKeys?.includes('selection'));
  const availableColumnSet = useMemo(
    () => new Set(availableColumnKeys || columnsState.map((column) => column.key)),
    [availableColumnKeys, columnsState],
  );
  const columnMenuRows = useMemo(
    () => {
      const rows = columnsState.filter((column) => availableColumnSet.has(column.key));
      if (availableColumnSet.has('selection') && !rows.some((column) => column.key === 'selection')) {
        return [
          {
            key: 'selection',
            title: 'Выбор',
            width: 44,
            minWidth: 44,
          } as TicketTableColumn,
          ...rows,
        ];
      }
      return rows;
    },
    [availableColumnSet, columnsState],
  );
  const currentVisibleColumnKeys = useMemo(
    () => visibleColumnKeys || columnMenuRows.map((column) => column.key),
    [columnMenuRows, visibleColumnKeys],
  );
  const closeColumnsMenu = useCallback(() => {
    setColumnsMenu((current) => ({ ...current, open: false }));
  }, []);
  useEffect(() => {
    if (!columnsMenu.open) {
      return;
    }
    const close = () => closeColumnsMenu();
    window.addEventListener('click', close);
    window.addEventListener('scroll', close, true);
    return () => {
      window.removeEventListener('click', close);
      window.removeEventListener('scroll', close, true);
    };
  }, [closeColumnsMenu, columnsMenu.open]);

  const toggleColumnVisibility = useCallback((columnKey: string, checked: boolean) => {
    if (!onVisibleColumnKeysChange) {
      return;
    }
    const next = checked
      ? [...currentVisibleColumnKeys, columnKey]
      : currentVisibleColumnKeys.filter((key) => key !== columnKey);
    const orderSource = columnMenuRows.map((column) => column.key);
    const ordered = orderSource
      .filter((key) => availableColumnSet.has(key) && next.includes(key));
    onVisibleColumnKeysChange(ordered);
  }, [
    availableColumnSet,
    columnMenuRows,
    currentVisibleColumnKeys,
    onVisibleColumnKeysChange,
  ]);

  const tableColumns = useMemo<ColumnsType<TicketListItemDTO>>(() => {
    if (!shouldShowSelectionColumn) {
      return columns;
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
        onHeaderCell: () => ({
          id: 'selection',
          width: 44,
          minWidth: 44,
          isDragDisabled: true,
          isSortable: false,
        }),
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
      ...columns,
    ];
  }, [
    columns,
    onSelectedTicketIdsChange,
    selectedTicketIds,
    selectedVisibleCount,
    shouldShowSelectionColumn,
    sortedRowIds,
  ]);

  const tableScrollX = useMemo(() => {
    return tableColumns.reduce((sum, column) => {
      const width = Number(column.width ?? (column as { minWidth?: number }).minWidth ?? DEFAULT_MIN_COLUMN_WIDTH);
      return sum + (Number.isFinite(width) ? width : DEFAULT_MIN_COLUMN_WIDTH);
    }, 0);
  }, [tableColumns]);

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

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={handleDragCancel}
      >
        <SortableContext
          items={tableColumns.map((column) => column.key as string)}
          strategy={horizontalListSortingStrategy}
        >
          <Table
            dataSource={sortedRows}
            columns={tableColumns}
            rowKey="id"
            loading={tableLoading}
            pagination={false}
            size="small"
            bordered
            className="tickets-table company-ticket-table"
            tableLayout="fixed"
            scroll={{ x: tableScrollX }}
            components={{
              header: {
                cell: DraggableHeaderCell,
              },
            }}
            rowClassName={rowClassName}
            onHeaderRow={() => ({
              onContextMenu: (event) => {
                if (!onVisibleColumnKeysChange) {
                  return;
                }
                event.preventDefault();
                event.stopPropagation();
                setColumnsMenu({
                  open: true,
                  x: event.clientX,
                  y: event.clientY,
                });
              },
            })}
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
          {columnsMenu.open && (
            <div
              className="company-ticket-table__columns-menu"
              style={{ left: columnsMenu.x, top: columnsMenu.y }}
              onClick={(event) => event.stopPropagation()}
              onContextMenu={(event) => event.preventDefault()}
            >
              <Text strong>Столбцы</Text>
              <Space direction="vertical" size={4} style={{ width: '100%', marginTop: 8 }}>
                {columnMenuRows.map((column) => (
                  <Checkbox
                    key={column.key}
                    checked={currentVisibleColumnKeys.includes(column.key)}
                    onChange={(event) => toggleColumnVisibility(column.key, event.target.checked)}
                  >
                    {column.title as React.ReactNode}
                  </Checkbox>
                ))}
              </Space>
            </div>
          )}
        </SortableContext>
      </DndContext>

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
