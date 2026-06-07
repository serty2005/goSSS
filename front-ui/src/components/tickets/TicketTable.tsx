import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { DatePicker, Space, Spin, Table, Tag, Typography, message } from 'antd';
import type { Dayjs } from 'dayjs';
import dayjs from 'dayjs';
import type { ColumnsType } from 'antd/es/table';
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
};

const { Text } = Typography;
const { RangePicker } = DatePicker;
const TABLE_LAYOUT_KEY = 'company_ticket_table';
const DEFAULT_MIN_COLUMN_WIDTH = 90;

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
    case 'assignee':
      return row.assignee?.full_name || '';
    case 'reporter_name':
      return row.reporter_name || 'Сотрудник';
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
    const excludedID = excludedTicketId === undefined || excludedTicketId === null ? '' : String(excludedTicketId);
    if (!excludedID) {
      return rawRows;
    }
    return rawRows.filter((item) => String(item.id) !== excludedID);
  }, [excludedTicketId, rawRows]);
  const sortedRows = useMemo(() => {
    if (!tableSort) {
      return rows;
    }

    return [...rows].sort((left, right) => {
      const leftValue = resolveSortValue(left, tableSort.key);
      const rightValue = resolveSortValue(right, tableSort.key);
      const result = typeof leftValue === 'number' && typeof rightValue === 'number'
        ? leftValue - rightValue
        : compareText(String(leftValue), String(rightValue));
      return tableSort.order === 'asc' ? result : -result;
    });
  }, [rows, tableSort]);
  const total = data?.pages?.[0]?.meta?.total || 0;
  const visibleTotal = Math.max(0, total - (rawRows.length - rows.length));

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

  const ticketLinkTarget = rowOpenMode === 'new_tab' ? '_blank' : undefined;
  const ticketLinkRel = rowOpenMode === 'new_tab' ? 'noreferrer' : undefined;

  const columnsBase = useMemo<TicketTableColumn[]>(() => [
    {
      title: 'Компания',
      dataIndex: 'company_name',
      key: 'company_name',
      width: 220,
      minWidth: createColumnMinWidth('Компания'),
      maxWidth: 360,
      render: (_value: string | undefined, record: TicketListItemDTO) => (
        <div className="company-ticket-table__cell-ellipsis" title={record.company_name || record.company_id || '-'}>
          <Link to={`/companies/${record.company_id}`} onClick={(event) => event.stopPropagation()}>
            {record.company_name || record.company_id || '-'}
          </Link>
        </div>
      ),
    },
    {
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
    },
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
  ], [ticketLinkRel, ticketLinkTarget]);

  const [columnsState, setColumnsState] = useState<TicketTableColumn[]>(columnsBase);

  useEffect(() => {
    const storedColumns = (user?.profile_config?.interface as any)?.[TABLE_LAYOUT_KEY]?.columns;
    const nextColumns = applyStoredColumnLayout(columnsBase, storedColumns);
    columnsStateRef.current = nextColumns;
    setColumnsState(nextColumns);
  }, [columnsBase, user?.id, user?.profile_config]);

  const saveColumnsLayout = useCallback((nextColumns: TicketTableColumn[]) => {
    if (!user || nextColumns.length === 0) {
      return;
    }

    const nextLayoutColumns = nextColumns.map((column) => ({
      key: column.key,
      width: column.width,
    }));
    const currentLayoutColumns = (user.profile_config?.interface as any)?.[TABLE_LAYOUT_KEY]?.columns;
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
        [TABLE_LAYOUT_KEY]: {
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
  }, [setUser, updateProfileConfigMutation, user]);

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

    setTableSort((currentSort) => {
      if (currentSort?.key !== columnKey) {
        return { key: columnKey, order: 'asc' };
      }
      if (currentSort.order === 'asc') {
        return { key: columnKey, order: 'desc' };
      }
      return null;
    });
  }, [isResizingColumn]);

  const columns = useMemo<ColumnsType<TicketListItemDTO>>(() => (
    columnsState
      .filter((column) => showCompanyColumn || column.key !== 'company_name')
      .map((column) => {
        const stateIndex = columnsState.findIndex((item) => item.key === column.key);
        return {
          ...column,
          onHeaderCell: () => ({
            id: column.key,
            width: column.width,
            minWidth: column.minWidth,
            isSortable: true,
            sortOrder: tableSort?.key === column.key ? tableSort.order : null,
            onSort: () => toggleSort(column.key),
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
    handleResize,
    handleResizeStop,
    isResizingColumn,
    showCompanyColumn,
    tableSort,
    toggleSort,
  ]);

  const tableScrollX = useMemo(() => {
    return columns.reduce((sum, column) => {
      const width = Number(column.width ?? (column as { minWidth?: number }).minWidth ?? DEFAULT_MIN_COLUMN_WIDTH);
      return sum + (Number.isFinite(width) ? width : DEFAULT_MIN_COLUMN_WIDTH);
    }, 0);
  }, [columns]);

  const openTicket = useCallback((ticketID: string) => {
    const url = `/tickets/${ticketID}`;
    if (rowOpenMode === 'new_tab') {
      window.open(url, '_blank', 'noopener,noreferrer');
      return;
    }
    navigate(url);
  }, [navigate, rowOpenMode]);

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

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
        onDragCancel={handleDragCancel}
      >
        <SortableContext
          items={columns.map((column) => column.key as string)}
          strategy={horizontalListSortingStrategy}
        >
          <Table
            dataSource={sortedRows}
            columns={columns}
            rowKey="id"
            loading={isLoading}
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
            onRow={(record) => ({
              onClick: () => {
                openTicket(String(record.id));
              },
              style: { cursor: 'pointer' },
            })}
          />
        </SortableContext>
      </DndContext>

      <div ref={loadMoreRef} style={{ marginTop: 4, display: 'flex', justifyContent: 'center', minHeight: 28 }}>
        {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
        {!hasNextPage && rows.length > 0 && (
          <Text type="secondary">Показано: {rows.length} из {visibleTotal}</Text>
        )}
        {!isLoading && rows.length === 0 && (
          <Text type="secondary">Тикеты не найдены</Text>
        )}
      </div>
    </Space>
  );
};

export default TicketTable;
