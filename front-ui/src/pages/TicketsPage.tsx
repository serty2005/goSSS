import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Col,
  Drawer,
  Input,
  List,
  Popconfirm,
  Popover,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
  theme as antTheme,
} from 'antd';
import { LinkOutlined, MenuOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { DndContext, DragEndEvent, PointerSensor, closestCenter, useSensor, useSensors } from '@dnd-kit/core';
import { SortableContext, arrayMove, horizontalListSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Resizable } from 'react-resizable';
import { Link, useNavigate } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { companiesApi } from '@/api/companies';
import { usersApi } from '@/api/users';
import { TicketDetailsDTO, TicketStatus } from '@/types/api';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import SmartTicketEditor from '@/features/tickets/editor/SmartTicketEditor';
import { hasEditorContent } from '@/features/tickets/editor/content';
import type { MentionOption } from '@/features/tickets/editor/mentions';
import { useAuthStore } from '@/store/authStore';
import { useTicketParamsStore } from '@/store/ticketParamsStore';
import { SafeHtmlContent } from '@/utils/safeHtml';

const { Text, Paragraph } = Typography;

const STATUS_OPTIONS: Array<{ value: TicketStatus; label: string; color: string }> = [
  { value: 'new', label: 'РќРѕРІР°СЏ', color: 'blue' },
  { value: 'in_progress', label: 'Р’ СЂР°Р±РѕС‚Рµ', color: 'processing' },
  { value: 'pending', label: 'РћР¶РёРґР°РЅРёРµ', color: 'orange' },
  { value: 'deferred', label: 'РћС‚Р»РѕР¶РµРЅРѕ', color: 'orange' },
  { value: 'onsite', label: 'РќР° РІС‹РµР·Рґ', color: 'cyan' },
  { value: 'to_manager', label: 'РџРµСЂРµРґР°С‚СЊ РјРµРЅРµРґР¶РµСЂСѓ', color: 'purple' },
  { value: 'resolved', label: 'Р РµС€РµРЅР°', color: 'green' },
  { value: 'spam', label: 'РЎРїР°Рј', color: 'red' },
  { value: 'execution', label: 'Р РµР°Р»РёР·Р°С†РёСЏ', color: 'magenta' },
  { value: 'closed', label: 'Р—Р°РєСЂС‹С‚Р°', color: 'default' },
];

type ViewMode = 'list' | 'cards' | 'table';

type HeaderCellProps = React.HTMLAttributes<HTMLTableCellElement> & {
  id?: string;
  width?: number;
  minWidth?: number;
  onResize?: (event: React.SyntheticEvent, data: { size: { width: number; height: number } }) => void;
  onResizeStart?: () => void;
  onResizeStop?: () => void;
  isResizing?: boolean;
};

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

const resolveTicketSubjectFromDescription = (value?: string) => {
  const normalized = normalizeDescription(value);
  return normalized || 'Р‘РµР· РѕРїРёСЃР°РЅРёСЏ';
};

const resolveTicketCreatedSourceLabel = (source?: string) => {
  if (source === 'ui') return 'UI';
  if (source === 'bitrix') return 'Bitrix24';
  if (source === 'servicedesk') return 'ServiceDesk';
  if (source === 'system') return 'System';
  return 'РќРµРёР·РІРµСЃС‚РЅРѕ';
};

const statusMeta = (status?: string) => STATUS_OPTIONS.find((item) => item.value === status) || STATUS_OPTIONS[0];
const isClosedLikeStatus = (status?: string) => status === 'resolved' || status === 'closed' || status === 'spam' || status === 'execution';
const ACTIVE_STATUS_VALUES: TicketStatus[] = ['new', 'in_progress', 'pending', 'deferred', 'onsite', 'to_manager'];
const DATE_STAMP_MIN_WIDTH = '10ch';
const TIME_STAMP_MIN_WIDTH = '5ch';
const TABLE_COLUMN_KEYS = ['number', 'status', 'company_display', 'assignee_display', 'reporter_display', 'subject', 'bitrix_deal_title', 'last_comment', 'created_at', 'last_activity', 'sync_with_bitrix'] as const;
const DEFAULT_TABLE_COLUMN_KEYS = TABLE_COLUMN_KEYS.filter((key) => key !== 'bitrix_deal_title');
type TableColumnKey = (typeof TABLE_COLUMN_KEYS)[number];
type TableSortKey = 'number' | 'assignee_display' | 'created_at' | 'last_activity';
type TableSortOrder = 'asc' | 'desc';

const formatDateStamp = (value?: string) => ({
  date: value ? dayjs(value).format('DD.MM.YYYY') : '-',
  time: value ? dayjs(value).format('HH:mm') : '--:--',
});

const TicketDateStamp: React.FC<{ label: string; value?: string }> = ({ label, value }) => {
  const stamp = formatDateStamp(value);
  return (
    <div className="ticket-date-stamp">
      <Text type="secondary" className="ticket-date-stamp-label">{label}</Text>
      <Text className="ticket-date-stamp-value">
        <span>{stamp.date}</span>
        <span>{stamp.time}</span>
      </Text>
    </div>
  );
};

const BitrixSyncIndicator: React.FC<{
  sync?: boolean;
  dealURL?: string;
  compact?: boolean;
  onClick?: (event: React.MouseEvent) => void;
}> = ({ sync, dealURL, compact, onClick }) => {
  if (!sync) {
    return null;
  }
  if (!dealURL) {
    return <Tag color="processing">B24</Tag>;
  }
  return (
    <Tooltip title="РћС‚РєСЂС‹С‚СЊ СЃРґРµР»РєСѓ РІ Bitrix24">
      <a href={dealURL} target="_blank" rel="noreferrer" onClick={onClick} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        <Tag color="success" style={{ marginInlineEnd: 0 }}>
          {compact ? 'B24' : 'РЎРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°РЅРѕ B24'}
        </Tag>
        <LinkOutlined />
      </a>
    </Tooltip>
  );
};

const estimateHeaderMinWidth = (title: string) => {
  // Р‘Р°Р·РѕРІР°СЏ РѕС†РµРЅРєР°: С€РёСЂРёРЅР° С‚РµРєСЃС‚Р° Р·Р°РіРѕР»РѕРІРєР° + РѕС‚СЃС‚СѓРїС‹ + РёРєРѕРЅРєР° drag-handle.
  return Math.max(70, title.length * 8 + 20);
};

const ResizableHeaderCell = React.forwardRef<HTMLTableCellElement, HeaderCellProps>((props, ref) => {
  const { onResize, onResizeStart, onResizeStop, width, minWidth, children, ...rest } = props;
  if (!width) {
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
      handle={(
        <span
          className="resize-handle"
          onMouseDown={(event) => event.stopPropagation()}
          onTouchStart={(event) => event.stopPropagation()}
        />
      )}
      onResize={onResize}
      onResizeStart={onResizeStart}
      onResizeStop={onResizeStop}
      minConstraints={[minWidth || 90, 0]}
      draggableOpts={{ enableUserSelectHack: false }}
    >
      <th ref={ref} {...rest}>
        {children}
      </th>
    </Resizable>
  );
});

ResizableHeaderCell.displayName = 'ResizableHeaderCell';

const DraggableHeaderCell: React.FC<HeaderCellProps> = ({ id, style, isResizing, children, ...rest }) => {
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: id || '', disabled: Boolean(isResizing) });

  const mergedStyle: React.CSSProperties = {
    ...style,
    transform: CSS.Transform.toString(transform),
    transition,
    cursor: 'move',
    ...(isDragging ? { position: 'relative', zIndex: 2 } : {}),
  };

  return (
    <ResizableHeaderCell
      ref={setNodeRef}
      style={mergedStyle}
      {...attributes}
      {...rest}
    >
      <div className="tickets-table-header">
        <span className="tickets-table-header-title">{children}</span>
        <span
          ref={setActivatorNodeRef}
          className={`tickets-table-drag-handle${isResizing ? ' is-disabled' : ''}`}
          {...listeners}
          onClick={(event) => event.stopPropagation()}
          onMouseDown={(event) => event.stopPropagation()}
          onTouchStart={(event) => event.stopPropagation()}
        >
          <MenuOutlined />
        </span>
      </div>
    </ResizableHeaderCell>
  );
};

const TicketsPage: React.FC = () => {
  const { token } = antTheme.useToken();
  const searchParamsRaw = useTicketParamsStore((state) => state.ticketParams);
  const setSearchParamsRaw = useTicketParamsStore((state) => state.setTicketParams);
  const createTicketRequestID = useTicketParamsStore((state) => state.createTicketRequestID);
  const clearCreateTicketRequest = useTicketParamsStore((state) => state.clearCreateTicketRequest);
  const searchParams = useMemo(() => new URLSearchParams(searchParamsRaw), [searchParamsRaw]);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const userRoles = user?.roles || [];
  const isAdminRole = userRoles.includes('admin');
  const isDeleteBlockedRole = userRoles.includes('support_specialist') || userRoles.includes('intern');
  const isCommentAuthor = (authorName?: string) => String(authorName || '').trim() === String(user?.full_name || '').trim();
  const canManageComment = (authorName?: string) => isAdminRole || isCommentAuthor(authorName);
  const canDeleteComment = (authorName?: string) => isAdminRole || (!isDeleteBlockedRole && isCommentAuthor(authorName));

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const [commentDraft, setCommentDraft] = useState('');
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [editingCommentID, setEditingCommentID] = useState('');
  const [editingCommentDraft, setEditingCommentDraft] = useState('');
  const [statusComment, setStatusComment] = useState('');
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [isResizingColumn, setIsResizingColumn] = useState(false);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 8 } }));

  const q = searchParams.get('q') || '';
  const status = searchParams.get('status') || '';
  const tableColumnsParam = searchParams.get('table_columns') || '';
  const tableSortParam = searchParams.get('table_sort') || '';
  const onlyActiveStatuses = searchParams.get('only_active_statuses') === '1';
  const assigneeIDs = searchParams.get('assignee_ids') || '';
  const archiveMode = searchParams.get('archive_mode') === 'archive' ? 'archive' : 'active';
  const activeCompany = searchParams.get('company') || '';
  const archiveCompany = searchParams.get('archive_company') || '';
  const company = archiveMode === 'archive' ? archiveCompany : activeCompany;
  const activePeriodFrom = searchParams.get('period_from') || '';
  const activePeriodTo = searchParams.get('period_to') || '';
  const archivePeriodFrom = searchParams.get('archive_period_from') || '';
  const archivePeriodTo = searchParams.get('archive_period_to') || '';
  const periodFrom = archiveMode === 'archive' ? archivePeriodFrom : activePeriodFrom;
  const periodTo = archiveMode === 'archive' ? archivePeriodTo : activePeriodTo;
  const viewMode = (searchParams.get('view') as ViewMode) || 'list';
  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const statusValues = useMemo(
    () => status.split(',').filter((value): value is TicketStatus => Boolean(value)),
    [status],
  );
  const effectiveStatusValues = useMemo(() => {
    if (archiveMode === 'archive') {
      return [];
    }
    if (!onlyActiveStatuses) {
      return statusValues;
    }
    const filtered = statusValues.filter((value) => ACTIVE_STATUS_VALUES.includes(value));
    return filtered.length ? filtered : ACTIVE_STATUS_VALUES;
  }, [archiveMode, onlyActiveStatuses, statusValues]);
  const effectiveStatus = effectiveStatusValues.join(',');
  const selectedTableColumnKeys = useMemo<TableColumnKey[]>(() => {
    if (!tableColumnsParam) {
      return [...DEFAULT_TABLE_COLUMN_KEYS];
    }
    const values = tableColumnsParam
      .split(',')
      .filter((value): value is TableColumnKey => (TABLE_COLUMN_KEYS as readonly string[]).includes(value));
    return values.length ? values : [...DEFAULT_TABLE_COLUMN_KEYS];
  }, [tableColumnsParam]);
  const tableSort = useMemo<{ key: TableSortKey; order: TableSortOrder } | null>(() => {
    if (!tableSortParam) {
      return null;
    }
    const [rawKey, rawOrder] = tableSortParam.split(':');
    if (
      (rawKey === 'number' || rawKey === 'assignee_display' || rawKey === 'created_at' || rawKey === 'last_activity')
      && (rawOrder === 'asc' || rawOrder === 'desc')
    ) {
      return { key: rawKey, order: rawOrder };
    }
    return null;
  }, [tableSortParam]);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: ['tickets', {
      q,
      status,
      onlyActiveStatuses,
      effectiveStatus,
      company,
      assigneeIDs,
      periodFrom,
      periodTo,
      archiveMode,
      activeCompany,
      archiveCompany,
      activePeriodFrom,
      activePeriodTo,
      archivePeriodFrom,
      archivePeriodTo,
    }],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      ticketsApi.getTickets({
        search: q || undefined,
        status: archiveMode === 'archive' ? undefined : (effectiveStatus || undefined),
        company_id: company || undefined,
        assignee_ids: assigneeIDs || undefined,
        period_from: periodFrom || undefined,
        period_to: periodTo || undefined,
        archive_mode: archiveMode,
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

  const tickets = useMemo(
    () => (data?.pages || []).flatMap((pageData) => pageData.data || []),
    [data?.pages],
  );
  const visibleTickets = tickets;
  const total = data?.pages?.[0]?.meta?.total || 0;

  const { data: detailsResponse, isLoading: isDetailsLoading } = useQuery({
    queryKey: ['ticket', selectedTicketId],
    queryFn: () => ticketsApi.getTicket(selectedTicketId || ''),
    enabled: Boolean(selectedTicketId),
  });

  const details: TicketDetailsDTO | undefined = detailsResponse?.data;
  const metadata = details?.metadata;

  const { data: usersResponse } = useQuery({
    queryKey: ['users-assignees'],
    queryFn: () => usersApi.getAssignees(),
    retry: false,
    staleTime: 60_000,
  });

  const mentionOptions = useMemo<MentionOption[]>(
    () =>
      (usersResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({ id: item.id, label: item.full_name || item.username })),
    [usersResponse?.data],
  );

  const { data: infraResponse, isLoading: isInfraLoading } = useQuery({
    queryKey: ['company-infra', metadata?.company_id],
    queryFn: () => companiesApi.getInfrastructure(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const infrastructure = useMemo(() => infraResponse?.data || [], [infraResponse?.data]);

  const { data: companyResponse } = useQuery({
    queryKey: ['company-profile', metadata?.company_id],
    queryFn: () => companiesApi.getCompany(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const companyTitle = useMemo(() => {
    const companyData = companyResponse?.data;
    return (
      companyData?.title ||
      companyData?.additional_name ||
      details?.company_name ||
      metadata?.company_name ||
      metadata?.company_id ||
      ''
    );
  }, [companyResponse?.data, details?.company_name, metadata?.company_id, metadata?.company_name]);

  const connections = useMemo(() => {
    return infrastructure
      .map((item) => {
        if (item.entity_type !== 'Server' && item.entity_type !== 'Workstation') {
          return null;
        }
        const dataRow = item.data as Record<string, string | undefined>;
        const title = dataRow.device_name || dataRow.server_name || dataRow.uuid || 'РћР±РѕСЂСѓРґРѕРІР°РЅРёРµ';
        const rows = [
          ...(item.entity_type === 'Server' ? [{ label: 'IP', value: dataRow.ip }] : []),
          { label: 'AnyDesk', value: dataRow.anydesk },
          { label: 'TeamViewer', value: dataRow.teamviewer },
          { label: 'RDP', value: dataRow.rdp },
          { label: 'LM', value: dataRow.litemanager },
        ].filter((entry) => entry.value);
        if (rows.length === 0) return null;
        return { key: `${item.entity_type}-${dataRow.uuid || title}`, title, rows };
      })
      .filter(Boolean) as Array<{ key: string; title: string; rows: Array<{ label: string; value?: string }> }>;
  }, [infrastructure]);

  const comments = useMemo(
    () =>
      (details?.comments || []).map((item) => ({
        id: item.uuid,
        author: item.author_name || 'РЎРѕС‚СЂСѓРґРЅРёРє',
        authorRaw: item.author_name || '',
        date: dayjs(item.creation_date).format('DD.MM.YYYY HH:mm'),
        text: item.text,
        isPrivate: item.is_private ?? false,
      })),
    [details?.comments],
  );

  const changeStatusMutation = useMutation({
    mutationFn: async (payload: { id: string; status: TicketStatus; comment?: string }) =>
      ticketsApi.changeStatus(payload.id, payload.status, payload.comment),
    onSuccess: () => {
      message.success('РЎС‚Р°С‚СѓСЃ РѕР±РЅРѕРІР»С‘РЅ');
      setPendingStatus(null);
      setStatusComment('');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('РќРµ СѓРґР°Р»РѕСЃСЊ РѕР±РЅРѕРІРёС‚СЊ СЃС‚Р°С‚СѓСЃ'),
  });

  const addCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; comment: string; isPrivate: boolean }) =>
      ticketsApi.addComment(payload.id, payload.comment, payload.isPrivate),
    onSuccess: () => {
      message.success('РљРѕРјРјРµРЅС‚Р°СЂРёР№ РґРѕР±Р°РІР»РµРЅ');
      setCommentDraft('');
      setCommentIsPrivate(false);
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('РќРµ СѓРґР°Р»РѕСЃСЊ РґРѕР±Р°РІРёС‚СЊ РєРѕРјРјРµРЅС‚Р°СЂРёР№'),
  });

  const updateCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; commentUUID: string; comment: string }) =>
      ticketsApi.updateComment(payload.id, payload.commentUUID, payload.comment),
    onSuccess: () => {
      message.success('Комментарий обновлён');
      setEditingCommentID('');
      setEditingCommentDraft('');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('Не удалось обновить комментарий'),
  });

  const deleteCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; commentUUID: string }) =>
      ticketsApi.deleteComment(payload.id, payload.commentUUID),
    onSuccess: () => {
      message.success('Комментарий удалён');
      setEditingCommentID('');
      setEditingCommentDraft('');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('Не удалось удалить комментарий'),
  });

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: { id: string; label: string; value: string }) =>
      ticketsApi.recordConnectionCopy(payload.id, payload.label, payload.value),
  });

  const closeQuickModal = () => {
    setSelectedTicketId(null);
    setCommentDraft('');
    setCommentIsPrivate(false);
    setEditingCommentID('');
    setEditingCommentDraft('');
    setPendingStatus(null);
    setStatusComment('');
  };

  useEffect(() => {
    if (createTicketRequestID === 0) {
      return;
    }
    setIsCreateOpen(true);
    clearCreateTicketRequest();
  }, [clearCreateTicketRequest, createTicketRequestID]);

  useEffect(() => {
    if (!editingCommentID) return;
    const exists = (details?.comments || []).some((item) => item.uuid === editingCommentID);
    if (!exists) {
      setEditingCommentID('');
      setEditingCommentDraft('');
    }
  }, [details?.comments, editingCommentID]);

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
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, tickets.length]);

  const tableDataBase = useMemo(
    () =>
      visibleTickets.map((ticket) => ({
        ...ticket,
        subject: resolveTicketSubjectFromDescription(ticket.description),
        company_display: ticket.company_name || ticket.company_id || 'РљРѕРјРїР°РЅРёСЏ РЅРµ СѓРєР°Р·Р°РЅР°',
        last_comment_display: normalizeDescription(ticket.last_comment),
        assignee_display: ticket.assignee?.full_name || 'РќРµ РЅР°Р·РЅР°С‡РµРЅ',
        reporter_display: ticket.reporter_name || 'РЎРѕС‚СЂСѓРґРЅРёРє',
      })),
    [visibleTickets],
  );
  const tableData = useMemo(() => {
    if (!tableSort) {
      return tableDataBase;
    }
    const factor = tableSort.order === 'asc' ? 1 : -1;
    return [...tableDataBase].sort((a, b) => {
      switch (tableSort.key) {
        case 'number':
          return ((a.number || 0) - (b.number || 0)) * factor;
        case 'assignee_display':
          return String(a.assignee_display || '').localeCompare(String(b.assignee_display || ''), 'ru') * factor;
        case 'created_at':
          return (dayjs(a.created_at).valueOf() - dayjs(b.created_at).valueOf()) * factor;
        case 'last_activity':
          return (dayjs(a.last_activity).valueOf() - dayjs(b.last_activity).valueOf()) * factor;
        default:
          return 0;
      }
    });
  }, [tableDataBase, tableSort]);

  type TableRow = (typeof tableData)[number];

  const tableColumnsBase: ColumnsType<TableRow> = useMemo(
    () => [
      {
        title: 'РќРѕРјРµСЂ',
        dataIndex: 'number',
        key: 'number',
        width: 90,
        minWidth: estimateHeaderMinWidth('РќРѕРјРµСЂ'),
        render: (val: number, row) => (
          <Link
            to={`/tickets/${row.id}`}
            onClick={(event) => event.stopPropagation()}
          >
            <Text strong>#{val}</Text>
          </Link>
        ),
      },
      {
        title: 'РЎС‚Р°С‚СѓСЃ',
        dataIndex: 'status',
        key: 'status',
        width: 140,
        minWidth: estimateHeaderMinWidth('РЎС‚Р°С‚СѓСЃ'),
        render: (value: TicketStatus, row) => {
          const meta = statusMeta(value);
          return (
            <Space size={4}>
              <Tag color={meta.color}>{meta.label}</Tag>
              {row.is_common_contract && <Tag color="gold">РџР»Р°С‚РЅС‹Р№</Tag>}
            </Space>
          );
        },
      },
      {
        title: 'РљРѕРјРїР°РЅРёСЏ',
        dataIndex: 'company_display',
        key: 'company_display',
        width: 220,
        minWidth: estimateHeaderMinWidth('РљРѕРјРїР°РЅРёСЏ'),
        ellipsis: true,
        render: (value: string) => (
          <Text ellipsis style={{ width: '100%', display: 'block' }}>
            {value}
          </Text>
        ),
      },
      {
        title: 'РСЃРїРѕР»РЅРёС‚РµР»СЊ',
        dataIndex: 'assignee_display',
        key: 'assignee_display',
        width: 170,
        minWidth: estimateHeaderMinWidth('РСЃРїРѕР»РЅРёС‚РµР»СЊ'),
        ellipsis: true,
      },
      {
        title: 'РђРІС‚РѕСЂ',
        dataIndex: 'reporter_display',
        key: 'reporter_display',
        width: 180,
        minWidth: estimateHeaderMinWidth('РђРІС‚РѕСЂ'),
        ellipsis: true,
      },
      {
        title: 'РћРїРёСЃР°РЅРёРµ',
        dataIndex: 'subject',
        key: 'subject',
        width: 260,
        minWidth: estimateHeaderMinWidth('РћРїРёСЃР°РЅРёРµ'),
        ellipsis: true,
      },
      {
        title: 'Р—Р°РіРѕР»РѕРІРѕРє Bitrix24',
        dataIndex: 'bitrix_deal_title',
        key: 'bitrix_deal_title',
        width: 240,
        minWidth: estimateHeaderMinWidth('Р—Р°РіРѕР»РѕРІРѕРє Bitrix24'),
        ellipsis: true,
        render: (value?: string) => value || '-',
      },
      {
        title: 'РџРѕСЃР»РµРґРЅРёР№ РєРѕРјРјРµРЅС‚Р°СЂРёР№',
        dataIndex: 'last_comment_display',
        key: 'last_comment',
        width: 260,
        minWidth: estimateHeaderMinWidth('РџРѕСЃР»РµРґРЅРёР№ РєРѕРјРјРµРЅС‚Р°СЂРёР№'),
        ellipsis: true,
        render: (value: string) => (
          <Text type="secondary" ellipsis style={{ width: '100%', display: 'block' }}>
            {value || '-'}
          </Text>
        ),
      },
      {
        title: 'РЎРѕР·РґР°РЅРѕ',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 110,
        minWidth: estimateHeaderMinWidth('РЎРѕР·РґР°РЅРѕ'),
        render: (value?: string) => {
          const stamp = formatDateStamp(value);
          return (
            <Space direction="vertical" size={0}>
              <Text style={{ minWidth: DATE_STAMP_MIN_WIDTH }}>{stamp.date}</Text>
              <Text type="secondary" style={{ minWidth: TIME_STAMP_MIN_WIDTH }}>{stamp.time}</Text>
            </Space>
          );
        },
      },
      {
        title: 'РћР±РЅРѕРІР»РµРЅРѕ',
        dataIndex: 'last_activity',
        key: 'last_activity',
        width: 110,
        minWidth: estimateHeaderMinWidth('РћР±РЅРѕРІР»РµРЅРѕ'),
        render: (value: string) => {
          const stamp = formatDateStamp(value);
          return (
            <Space direction="vertical" size={0}>
              <Text style={{ minWidth: DATE_STAMP_MIN_WIDTH }}>{stamp.date}</Text>
              <Text type="secondary" style={{ minWidth: TIME_STAMP_MIN_WIDTH }}>{stamp.time}</Text>
            </Space>
          );
        },
      },
      {
        title: 'B24',
        dataIndex: 'sync_with_bitrix',
        key: 'sync_with_bitrix',
        width: 120,
        minWidth: estimateHeaderMinWidth('B24'),
        render: (_value: boolean, row) => (
          <BitrixSyncIndicator
            sync={row.sync_with_bitrix}
            dealURL={row.bitrix_deal_url}
            compact
            onClick={(event) => event.stopPropagation()}
          />
        ),
      },
    ],
    [],
  );

  const [tableColumnsState, setTableColumnsState] = useState<ColumnsType<TableRow>>(tableColumnsBase);

  const tableLayoutStorageKey = useMemo(() => {
    const userKey = user?.id ? String(user.id) : 'guest';
    return `tickets-table-layout-${userKey}`;
  }, [user?.id]);

  useEffect(() => {
    const raw = localStorage.getItem(tableLayoutStorageKey);
    if (!raw) {
      setTableColumnsState(tableColumnsBase);
      return;
    }
    try {
      const parsed = JSON.parse(raw) as Array<{ key: string; width?: number }>;
      const baseByKey = new Map(tableColumnsBase.map((col) => [col.key, col]));
      const next: ColumnsType<TableRow> = [];
      const seen = new Set<string>();

      for (const entry of parsed) {
        const base = baseByKey.get(entry.key);
        if (!base) continue;
        next.push({
          ...base,
          width: Math.max(entry.width ?? (base.width as number), (base as { minWidth?: number }).minWidth || 90),
        });
        seen.add(entry.key);
      }
      for (const col of tableColumnsBase) {
        const key = col.key as string;
        if (seen.has(key)) continue;
        next.push(col);
      }
      setTableColumnsState(next.length ? next : tableColumnsBase);
    } catch {
      setTableColumnsState(tableColumnsBase);
    }
  }, [tableColumnsBase, tableLayoutStorageKey]);

  useEffect(() => {
    if (!tableColumnsState.length) return;
    const payload = tableColumnsState.map((col) => ({
      key: col.key as string,
      width: col.width as number | undefined,
    }));
    localStorage.setItem(tableLayoutStorageKey, JSON.stringify(payload));
  }, [tableColumnsState, tableLayoutStorageKey]);

  const handleResize =
    (index: number) => (_event: React.SyntheticEvent, data: { size: { width: number } }) => {
      setTableColumnsState((columns) => {
        const nextColumns = [...columns];
        const minWidth = (nextColumns[index] as { minWidth?: number }).minWidth || 90;
        nextColumns[index] = {
          ...nextColumns[index],
          width: Math.max(data.size.width, minWidth),
        };
        return nextColumns;
      });
    };

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const activeID = String(active.id);
    const overID = String(over.id);
    setTableColumnsState((columns) => {
      const oldIndex = columns.findIndex((col) => col.key === activeID);
      const newIndex = columns.findIndex((col) => col.key === overID);
      if (oldIndex === -1 || newIndex === -1) return columns;
      return arrayMove(columns, oldIndex, newIndex);
    });
  };

  function applyTableSort(key: TableSortKey) {
    const params = new URLSearchParams(searchParams);
    const nextOrder: TableSortOrder | null =
      tableSort?.key !== key ? 'asc' : tableSort.order === 'asc' ? 'desc' : null;
    if (!nextOrder) {
      params.delete('table_sort');
    } else {
      params.set('table_sort', `${key}:${nextOrder}`);
    }
    params.set('page', '1');
    setSearchParamsRaw(params.toString());
  }

  function renderSortableTitle(label: string, key: TableSortKey) {
    const order = tableSort?.key === key ? tableSort.order : null;
    return (
      <Space size={4}>
        <span>{label}</span>
        <Button
          size="small"
          type="text"
          onClick={(event) => {
            event.stopPropagation();
            applyTableSort(key);
          }}
        >
          {order === 'asc' ? 'в†‘' : order === 'desc' ? 'в†“' : 'в†•'}
        </Button>
      </Space>
    );
  }

  const tableColumnsVisibleState = tableColumnsState.filter((col) =>
    selectedTableColumnKeys.includes(String(col.key) as TableColumnKey),
  );
  const tableColumns = tableColumnsVisibleState.map((col) => {
    const columnKey = String(col.key);
    const stateIndex = tableColumnsState.findIndex((item) => item.key === col.key);
    const sortableLabel =
      columnKey === 'number'
        ? 'РќРѕРјРµСЂ'
        : columnKey === 'assignee_display'
          ? 'РСЃРїРѕР»РЅРёС‚РµР»СЊ'
          : columnKey === 'created_at'
            ? 'РЎРѕР·РґР°РЅРѕ'
            : columnKey === 'last_activity'
              ? 'РћР±РЅРѕРІР»РµРЅРѕ'
              : null;
    return {
      ...col,
      title: sortableLabel ? renderSortableTitle(sortableLabel, columnKey as TableSortKey) : col.title,
      onHeaderCell: () => ({
        id: col.key as string,
        width: col.width,
        minWidth: (col as { minWidth?: number }).minWidth || 90,
        onResize: handleResize(stateIndex),
        onResizeStart: () => setIsResizingColumn(true),
        onResizeStop: () => setIsResizingColumn(false),
        isResizing: isResizingColumn,
      }),
    };
  });

  const applyAssigneeFilter = (assigneeID?: number) => {
    if (!assigneeID) return;
    const params = new URLSearchParams(searchParams);
    params.set('assignee_ids', String(assigneeID));
    params.set('page', '1');
    setSearchParamsRaw(params.toString());
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card>
        {viewMode === 'list' && (
          <List
            loading={isLoading}
            dataSource={visibleTickets}
            renderItem={(item) => {
              const meta = statusMeta(item.status);
              return (
                <List.Item key={item.id} style={{ cursor: 'pointer' }} onClick={() => setSelectedTicketId(item.id)}>
                  <Space className="ticket-list-item-wrap">
                    <Space direction="vertical" size={0} className="ticket-list-main">
                      <Text className="ticket-company-centered" strong>{item.company_name || item.company_id}</Text>
                      <Space size={8}>
                        <Link
                          to={`/tickets/${item.id}`}
                          onClick={(event) => event.stopPropagation()}
                        >
                          <Text strong>#{item.number}</Text>
                        </Link>
                        <Tag color={meta.color}>{meta.label}</Tag>
                        {item.is_common_contract && <Tag color="gold">РџР»Р°С‚РЅС‹Р№</Tag>}
                        <BitrixSyncIndicator
                          sync={item.sync_with_bitrix}
                          dealURL={item.bitrix_deal_url}
                          compact
                          onClick={(event) => event.stopPropagation()}
                        />
                      </Space>
                      <Text>{resolveTicketSubjectFromDescription(item.description)}</Text>
                      {item.last_comment && (
                        <Paragraph className="ticket-description-paragraph" type="secondary" ellipsis={{ rows: 3 }}>
                          {normalizeDescription(item.last_comment)}
                        </Paragraph>
                      )}
                    </Space>
                    <Space direction="vertical" size={6} className="ticket-list-side">
                      <Text className="ticket-assignee-linklike">{item.assignee?.full_name || 'РќРµ РЅР°Р·РЅР°С‡РµРЅ'}</Text>
                      <Text type="secondary">
                        {item.reporter_name || 'РЎРѕС‚СЂСѓРґРЅРёРє'} вЂў {resolveTicketCreatedSourceLabel(item.created_source)}
                      </Text>
                      <TicketDateStamp label="РЎРѕР·РґР°РЅРѕ" value={item.created_at} />
                      <TicketDateStamp label="РћР±РЅРѕРІР»РµРЅРѕ" value={item.last_activity} />
                    </Space>
                  </Space>
                </List.Item>
              );
            }}
          />
        )}

        {viewMode === 'cards' && (
          <Row gutter={[12, 12]}>
            {visibleTickets.map((item) => {
              const meta = statusMeta(item.status);
              return (
                <Col key={item.id} xs={24} md={12} xl={8}>
                  <Card hoverable className="glass-panel" onClick={() => setSelectedTicketId(item.id)}>
                    <Space direction="vertical" size={6} style={{ width: '100%' }}>
                      <div className="ticket-card-top">
                        <div className="ticket-card-left">
                          <Link
                            to={`/tickets/${item.id}`}
                            onClick={(event) => event.stopPropagation()}
                          >
                            <Text strong className="ticket-card-number">#{item.number}</Text>
                          </Link>
                          <BitrixSyncIndicator
                            sync={item.sync_with_bitrix}
                            dealURL={item.bitrix_deal_url}
                            compact
                            onClick={(event) => event.stopPropagation()}
                          />
                        </div>
                        <div className="ticket-company-centered ticket-company-top">
                          {/* TODO: Р РµР°Р»РёР·РѕРІР°С‚СЊ СЃРѕРґРµСЂР¶РёРјРѕРµ popover РєРѕРјРїР°РЅРёРё РІРјРµСЃС‚Рµ СЃ popover РёСЃРїРѕР»РЅРёС‚РµР»СЏ. */}
                          <Popover trigger="hover" content={<div style={{ minWidth: 180, minHeight: 48 }} />}>
                            <a
                              className="ticket-assignee-linklike"
                              onClick={(event) => {
                                event.preventDefault();
                                event.stopPropagation();
                              }}
                            >
                              {item.company_name || item.company_id}
                            </a>
                          </Popover>
                        </div>
                        <div className="ticket-card-right">
                          <Space size={4} className="ticket-card-status-wrap">
                            <Tag color={meta.color}>{meta.label}</Tag>
                            {item.is_common_contract && <Tag color="gold">РџР»Р°С‚РЅС‹Р№</Tag>}
                          </Space>
                          <div className="ticket-card-assignee-right">
                            <Popover trigger="hover" content={<div style={{ minWidth: 180, minHeight: 48 }} />}>
                              <a
                                className="ticket-assignee-linklike"
                                onClick={(event) => {
                                  event.preventDefault();
                                  event.stopPropagation();
                                  applyAssigneeFilter(item.assignee?.id);
                                }}
                              >
                                {item.assignee?.full_name || 'РќРµ РЅР°Р·РЅР°С‡РµРЅ'}
                              </a>
                            </Popover>
                          </div>
                        </div>
                      </div>
                      <div className="ticket-company-centered ticket-company-mobile">
                        <Popover trigger="hover" content={<div style={{ minWidth: 180, minHeight: 48 }} />}>
                          <a
                            className="ticket-assignee-linklike"
                            onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                            }}
                          >
                            {item.company_name || item.company_id}
                          </a>
                        </Popover>
                      </div>
                      <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2 }}>
                        {resolveTicketSubjectFromDescription(item.description)}
                      </Paragraph>
                      <Text type="secondary">
                        {item.reporter_name || 'РЎРѕС‚СЂСѓРґРЅРёРє'} вЂў {resolveTicketCreatedSourceLabel(item.created_source)}
                      </Text>
                      {item.last_comment && (
                        <Paragraph className="ticket-description-paragraph" type="secondary" style={{ marginBottom: 0 }} ellipsis={{ rows: 3 }}>
                          {normalizeDescription(item.last_comment)}
                        </Paragraph>
                      )}
                      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                        <TicketDateStamp label="РЎРѕР·РґР°РЅРѕ" value={item.created_at} />
                        <TicketDateStamp label="РћР±РЅРѕРІР»РµРЅРѕ" value={item.last_activity} />
                      </Space>
                    </Space>
                  </Card>
                </Col>
              );
            })}
          </Row>
        )}
	        {viewMode === 'table' && (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={tableColumns.map((col) => col.key as string)}
              strategy={horizontalListSortingStrategy}
            >
              <Table<TableRow>
                dataSource={tableData}
                columns={tableColumns}
                rowKey="id"
                size="small"
                bordered
                className="tickets-table"
                pagination={false}
                components={{
                  header: {
                    cell: DraggableHeaderCell,
                  },
                }}
                onRow={(record) => ({
                  onClick: () => setSelectedTicketId(record.id),
                  style: { cursor: 'pointer' },
                })}
              />
            </SortableContext>
          </DndContext>
        )}

        <div ref={loadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40 }}>
          {(isFetchingNextPage || (hasNextPage && visibleTickets.length > 0)) && <Spin size="small" />}
          {!hasNextPage && visibleTickets.length > 0 && (
            <Text type="secondary">РџРѕРєР°Р·Р°РЅРѕ: {visibleTickets.length} РёР· {total}</Text>
          )}
        </div>
      </Card>

      <Drawer
        open={Boolean(selectedTicketId)}
        onClose={closeQuickModal}
        width={656}
        title={
          metadata ? (
            <div style={{ display: 'grid', alignItems: 'center', gridTemplateColumns: '1fr auto 1fr', gap: 8 }}>
              <span>Р‘С‹СЃС‚СЂС‹Р№ РїСЂРѕСЃРјРѕС‚СЂ #{metadata.number}</span>
              {metadata.company_id ? (
                <Link to={`/companies/${metadata.company_id}`} onClick={closeQuickModal}>
                  {companyTitle}
                </Link>
              ) : (
                <span />
              )}
              <span />
            </div>
          ) : (
            'Р‘С‹СЃС‚СЂС‹Р№ РїСЂРѕСЃРјРѕС‚СЂ Р·Р°СЏРІРєРё'
          )
        }
        placement="right"
        mask={false}
      >
        {isDetailsLoading || !details || !metadata ? (
          <div style={{ padding: 24, textAlign: 'center' }}>
            <Spin />
          </div>
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Space wrap>
              {metadata.is_archived ? (
                <Button
                  type="primary"
                  loading={changeStatusMutation.isPending}
                  onClick={() => {
                    if (!selectedTicketId) return;
                    changeStatusMutation.mutate({ id: selectedTicketId, status: 'in_progress' });
                  }}
                >
                  Р’РµСЂРЅСѓС‚СЊ РІ СЂР°Р±РѕС‚Сѓ
                </Button>
              ) : (
                <Select
                  value={metadata.status}
                  options={STATUS_OPTIONS.filter((item) => item.value !== 'closed').map((item) => ({ value: item.value, label: item.label }))}
                  style={{ width: 220 }}
                  onChange={(nextStatus: TicketStatus) => {
                    if (!selectedTicketId || nextStatus === metadata.status) {
                      return;
                    }
                    if (nextStatus === 'resolved') {
                      const hasComments = (details?.comments || []).length > 0;
                      if (!hasComments) {
                        setPendingStatus(nextStatus);
                        return;
                      }
                      changeStatusMutation.mutate({ id: selectedTicketId, status: nextStatus });
                      return;
                    }
                    changeStatusMutation.mutate({ id: selectedTicketId, status: nextStatus });
                  }}
                />
              )}
              <BitrixSyncIndicator sync={metadata.sync_with_bitrix} dealURL={metadata.bitrix_deal_url} />
              <Text type="secondary">РСЃРїРѕР»РЅРёС‚РµР»СЊ: {metadata.assignee?.full_name || 'РќРµ РЅР°Р·РЅР°С‡РµРЅ'}</Text>
              <Button
                onClick={() => {
                  if (!selectedTicketId) return;
                  navigate(`/tickets/${selectedTicketId}`);
                  closeQuickModal();
                }}
              >
                РћС‚РєСЂС‹С‚СЊ СЃС‚СЂР°РЅРёС†Сѓ
              </Button>
            </Space>

            <Card size="small" title="РћРїРёСЃР°РЅРёРµ">
              <SafeHtmlContent html={metadata.description || '<span>РќРµС‚ РѕРїРёСЃР°РЅРёСЏ</span>'} style={{ whiteSpace: 'pre-wrap' }} />
            </Card>

            {isClosedLikeStatus(metadata.status) && Boolean((metadata.result || '').trim()) && (
              <Card size="small" title="Р РµР·СѓР»СЊС‚Р°С‚">
                <SafeHtmlContent html={metadata.result || ''} style={{ whiteSpace: 'pre-wrap' }} />
              </Card>
            )}

            <Card size="small" title="РџРѕРґРєР»СЋС‡РµРЅРёСЏ">
              {isInfraLoading ? (
                <div style={{ textAlign: 'center', padding: 12 }}>
                  <Spin />
                </div>
              ) : connections.length === 0 ? (
                <Text type="secondary">РџРѕРґРєР»СЋС‡РµРЅРёСЏ РЅРµ РЅР°Р№РґРµРЅС‹</Text>
              ) : (
                <List
                  dataSource={connections}
                  renderItem={(group) => (
                    <List.Item key={group.key}>
                      <Space direction="vertical" size={0} style={{ width: '100%' }}>
                        <Text strong>{group.title}</Text>
                        {group.rows.map((row) => (
                          <Paragraph
                            key={`${group.key}-${row.label}-${row.value}`}
                            style={{ margin: 0 }}
                            copyable={
                              row.value
                                ? {
                                    text: row.value,
                                    onCopy: () => {
                                      if (!selectedTicketId || !row.value) return;
                                      copyConnectionMutation.mutate({ id: selectedTicketId, label: row.label, value: row.value });
                                    },
                                  }
                                : false
                            }
                          >
                            <Text type="secondary">{row.label}:</Text> {row.value}
                          </Paragraph>
                        ))}
                      </Space>
                    </List.Item>
                  )}
                />
              )}
            </Card>

            <Card
              size="small"
              title="РљРѕРјРјРµРЅС‚Р°СЂРёРё"
            >
              {comments.length > 0 && (
                <List
                  dataSource={comments}
                  renderItem={(item) => (
                    <List.Item key={item.id}>
                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                        <Space size={8} style={{ justifyContent: 'space-between', width: '100%' }} wrap>
                          <Text type="secondary">
                            {item.author} вЂў {item.date}
                          </Text>
                          <Space size={8}>
                            {item.isPrivate && <Tag color="orange">РџСЂРёРІР°С‚РЅС‹Р№</Tag>}
                            {canManageComment(item.authorRaw) && (
                              <Button
                                type="link"
                                size="small"
                                onClick={() => {
                                  setEditingCommentID(item.id);
                                  setEditingCommentDraft(item.text || '');
                                }}
                              >
                                Редактировать
                              </Button>
                            )}
                            {canDeleteComment(item.authorRaw) && (
                              <Popconfirm
                                title="Удалить комментарий?"
                                okText="Удалить"
                                cancelText="Отмена"
                                onConfirm={() => {
                                  if (!selectedTicketId) return;
                                  deleteCommentMutation.mutate({ id: selectedTicketId, commentUUID: item.id });
                                }}
                              >
                                <Button type="link" size="small" danger loading={deleteCommentMutation.isPending}>
                                  Удалить
                                </Button>
                              </Popconfirm>
                            )}
                          </Space>
                        </Space>
                        {editingCommentID === item.id ? (
                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
                            <SmartTicketEditor
                              value={editingCommentDraft}
                              onChange={setEditingCommentDraft}
                              placeholder="Измените комментарий"
                              mentions={mentionOptions}
                              minHeight={96}
                            />
                            <Space>
                              <Button
                                type="primary"
                                loading={updateCommentMutation.isPending}
                                disabled={!hasEditorContent(editingCommentDraft) || !selectedTicketId}
                                onClick={() => {
                                  if (!selectedTicketId) return;
                                  updateCommentMutation.mutate({
                                    id: selectedTicketId,
                                    commentUUID: item.id,
                                    comment: editingCommentDraft,
                                  });
                                }}
                              >
                                Сохранить
                              </Button>
                              <Button
                                onClick={() => {
                                  setEditingCommentID('');
                                  setEditingCommentDraft('');
                                }}
                              >
                                Отмена
                              </Button>
                            </Space>
                          </Space>
                        ) : (
                          <SafeHtmlContent html={item.text} style={{ whiteSpace: 'pre-wrap' }} />
                        )}
                      </Space>
                    </List.Item>
                  )}
                />
              )}

              <Space direction="vertical" size="small" style={{ width: '100%', marginTop: 12 }}>
                <SmartTicketEditor
                  value={commentDraft}
                  onChange={setCommentDraft}
                  placeholder="Р”РѕР±Р°РІСЊС‚Рµ РєРѕРјРјРµРЅС‚Р°СЂРёР№"
                  mentions={mentionOptions}
                  minHeight={96}
                />
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: token.colorTextSecondary }}>
                  <input
                    type="checkbox"
                    checked={commentIsPrivate}
                    onChange={(event) => setCommentIsPrivate(event.target.checked)}
                  />
                  РџСЂРёРІР°С‚РЅС‹Р№ РєРѕРјРјРµРЅС‚Р°СЂРёР№ (РЅРµ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ РІРѕ РІРЅРµС€РЅРёРµ СЃРёСЃС‚РµРјС‹)
                </label>
                <Button
                  type="primary"
                  loading={addCommentMutation.isPending}
                  disabled={!hasEditorContent(commentDraft) || !selectedTicketId}
                  onClick={() => {
                    if (!selectedTicketId) return;
                    addCommentMutation.mutate({ id: selectedTicketId, comment: commentDraft, isPrivate: commentIsPrivate });
                  }}
                >
                  РћС‚РїСЂР°РІРёС‚СЊ
                </Button>
              </Space>
            </Card>
          </Space>
        )}
      </Drawer>

      <Drawer
        open={Boolean(pendingStatus)}
        onClose={() => {
          setPendingStatus(null);
          setStatusComment('');
        }}
        width={420}
        title="Р—Р°РІРµСЂС€РµРЅРёРµ Р·Р°СЏРІРєРё"
        placement="right"
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Input.TextArea
            rows={4}
            value={statusComment}
            onChange={(event) => setStatusComment(event.target.value)}
            placeholder="РћРїРёС€РёС‚Рµ РёС‚РѕРі РІС‹РїРѕР»РЅРµРЅРёСЏ Р·Р°СЏРІРєРё"
          />
          <Button
            type="primary"
            loading={changeStatusMutation.isPending}
            disabled={!statusComment.trim()}
            onClick={() => {
              if (!selectedTicketId || !pendingStatus || !statusComment.trim()) return;
              changeStatusMutation.mutate({ id: selectedTicketId, status: pendingStatus, comment: statusComment.trim() });
            }}
          >
            Р—Р°РІРµСЂС€РёС‚СЊ Р·Р°СЏРІРєСѓ
          </Button>
        </Space>
      </Drawer>

      <NewTicketModal
        open={isCreateOpen}
        onClose={() => {
          setIsCreateOpen(false);
        }}
        onCreated={() => {
          queryClient.invalidateQueries({ queryKey: ['tickets'] });
        }}
      />
    </Space>
  );
};

export default TicketsPage;
