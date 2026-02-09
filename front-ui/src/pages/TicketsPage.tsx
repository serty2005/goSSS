import React, { useEffect, useMemo, useState } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Col, Descriptions, Divider, Empty, Input, List, Modal, Pagination, Row, Segmented, Select, Space, Spin, Table, Tag, Typography, message } from 'antd';
import { MenuOutlined, SearchOutlined } from '@ant-design/icons';
import { DndContext, PointerSensor, closestCenter, useSensor, useSensors } from '@dnd-kit/core';
import { SortableContext, arrayMove, horizontalListSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Resizable } from 'react-resizable';
import { useAuthStore } from '@/store/authStore';
import type { ColumnsType, ColumnType } from 'antd/es/table';
import dayjs from 'dayjs';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import { InfrastructureItem, TicketListItemDTO } from '@/types/api';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import { formatRnm } from '@/utils/formatters';

const { Title, Text, Paragraph } = Typography;

const STATUS_META: Record<string, { color: string; label: string }> = {
  new: { color: 'blue', label: 'Новая' },
  in_progress: { color: 'processing', label: 'В работе' },
  pending: { color: 'orange', label: 'Ожидание' },
  resolved: { color: 'green', label: 'Решена' },
  closed: { color: 'default', label: 'Закрыта' },
  registered: { color: 'blue', label: 'Новая' },
  inprogress: { color: 'processing', label: 'В работе' },
  wait: { color: 'orange', label: 'Ожидание' },
};

const normalizeDescription = (value?: string) => {
  if (!value) return '';
  const withLineBreaks = value
    .replace(/<\s*br\s*\/?>/gi, '\n')
    .replace(/<\/p>\s*<p>/gi, '\n')
    .replace(/<\/?p[^>]*>/gi, '\n');
  const withoutTags = withLineBreaks.replace(/<[^>]*>/g, ' ');
  const decoded = withoutTags
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'");
  return decoded
    .split('\n')
    .map((line) => line.replace(/\s+/g, ' ').trim())
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
};

const compactDescription = (value?: string) => normalizeDescription(value).replace(/\s*\n\s*/g, ' ').trim();

const sanitizeRichHtml = (value?: string) => {
  if (!value) return '';
  return value
    .replace(/<script[\s\S]*?>[\s\S]*?<\/script>/gi, '')
    .replace(/\son\w+="[^"]*"/gi, '')
    .replace(/\son\w+='[^']*'/gi, '')
    .replace(/javascript:/gi, '')
    .replace(/(src|href)=["']\/static\//gi, '$1="/api/static/')
    .replace(/(src|href)=["']static\//gi, '$1="/api/static/')
    .replace(/<img\b/gi, '<img style="max-width:100%;height:auto;display:block;"');
};

const resolveStatusMeta = (status?: string) => {
  if (status && STATUS_META[status]) {
    return STATUS_META[status];
  }
  return { color: 'default', label: 'Неизвестно' };
};

const renderFnInfo = (dateStr?: string) => {
  if (!dateStr) return <Tag>Нет ФН</Tag>;
  const expireDate = dayjs(dateStr);
  const daysLeft = expireDate.diff(dayjs(), 'day');

  let color = 'green';
  let label = 'ФН OK';

  if (daysLeft < 0) {
    color = 'red';
    label = 'ФН истек';
  } else if (daysLeft < 30) {
    color = 'orange';
    label = `ФН: ${daysLeft} дн.`;
  }

  return (
    <Space size={4}>
      <Tag color={color} style={{ marginRight: 0 }}>{label}</Tag>
      <Text type="secondary" style={{ fontSize: 11 }}>
        (до {expireDate.format('DD.MM.YYYY')})
      </Text>
    </Space>
  );
};

type HeaderCellProps = React.HTMLAttributes<HTMLTableCellElement> & {
  id?: string;
  width?: number;
  onResize?: (event: React.SyntheticEvent, data: { size: { width: number; height: number } }) => void;
  onResizeStart?: () => void;
  onResizeStop?: () => void;
  isResizing?: boolean;
};

const ResizableHeaderCell = React.forwardRef<HTMLTableCellElement, HeaderCellProps>((props, ref) => {
  const { onResize, onResizeStart, onResizeStop, width, children, ...rest } = props;
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
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const user = useAuthStore((state) => state.user);
  const [viewMode, setViewMode] = useState<'cards' | 'list' | 'table'>('cards');
  const [commentText, setCommentText] = useState('');
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isEditingDescription, setIsEditingDescription] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState('');
  const [isStatusModalOpen, setIsStatusModalOpen] = useState(false);
  const [isAttachmentsModalOpen, setIsAttachmentsModalOpen] = useState(false);
  const [pendingStatus, setPendingStatus] = useState<string | null>(null);
  const [statusComment, setStatusComment] = useState('');
  const [isResizingColumn, setIsResizingColumn] = useState(false);
  const queryClient = useQueryClient();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 8 } }));

  const term = searchParams.get('q') || '';
  const statusParam = searchParams.get('status') || '';
  const statuses = statusParam ? statusParam.split(',').filter(Boolean) : [];
  const companyId = searchParams.get('company') || undefined;
  const ticketParam = searchParams.get('ticket');

  const pageParam = Number(searchParams.get('page') || 1);
  const page = Number.isFinite(pageParam) && pageParam > 0 ? pageParam : 1;
  const limit = 20;
  const offset = (page - 1) * limit;

  const { data, isLoading, isError } = useQuery({
    queryKey: ['tickets', { term, statuses, companyId, limit, offset }],
    queryFn: () =>
      ticketsApi.getTickets({
        search: term || undefined,
        status: statuses.length ? statuses : undefined,
        company_id: companyId,
        limit,
        offset,
      }),
    staleTime: 30_000,
  });

  const tickets = data?.data || [];
  const total = data?.meta?.total || 0;

  const cards = useMemo(() => tickets as TicketListItemDTO[], [tickets]);
  const selectedTicket = useMemo(() => cards.find((item) => item.id === ticketParam) || null, [cards, ticketParam]);
  const selectedTicketId = ticketParam || undefined;

  const { data: ticketDetailsData, isLoading: isTicketLoading } = useQuery({
    queryKey: ['ticket', selectedTicketId],
    queryFn: () => ticketsApi.getTicket(selectedTicketId ?? ''),
    enabled: Boolean(selectedTicketId),
    staleTime: 30_000,
  });

  const selectedCompanyId =
    selectedTicket?.company_id ||
    ticketDetailsData?.data?.metadata?.company_id ||
    ticketDetailsData?.data?.metadata?.CompanyID;

  const { data: equipmentData, isLoading: isEquipmentLoading } = useQuery({
    queryKey: ['company-infrastructure', selectedCompanyId],
    queryFn: () => companiesApi.getInfrastructure(selectedCompanyId ?? ''),
    enabled: Boolean(selectedCompanyId),
    staleTime: 30_000,
  });

  const onPageChange = (nextPage: number) => {
    const params = new URLSearchParams(searchParams);
    params.set('page', String(nextPage));
    setSearchParams(params);
  };

  const onOpenTicket = (ticket: TicketListItemDTO) => {
    const params = new URLSearchParams(searchParams);
    params.set('ticket', ticket.id);
    setSearchParams(params);
  };

  const onCloseTicket = () => {
    const params = new URLSearchParams(searchParams);
    params.delete('ticket');
    setSearchParams(params);
    setIsAttachmentsModalOpen(false);
  };

  const resolveEquipmentTitle = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    return (
      (data.device_name as string) ||
      (data.server_name as string) ||
      (data.model_kkt as string) ||
      (data.serial_number as string) ||
      (data.rn_kkt as string) ||
      (data.unique_id as string) ||
      (data.uuid as string) ||
      'Оборудование'
    );
  };

  const resolveEquipmentLink = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    const uuid = data.uuid as string | undefined;
    if (!uuid) return null;
    switch (item.entity_type) {
      case 'Server':
        return `/servers/${uuid}`;
      case 'Workstation':
        return `/workstations/${uuid}`;
      case 'FiscalRegister':
        return `/fiscals/${uuid}`;
      default:
        return null;
    }
  };

  const handleEquipmentClick = (item: InfrastructureItem) => {
    const link = resolveEquipmentLink(item);
    if (!link) return;
    navigate(link, { state: { backTo: `${location.pathname}${location.search}` } });
  };

  const resolveConnectionItems = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    if (item.entity_type !== 'Server' && item.entity_type !== 'Workstation') {
      return null;
    }

    const items = [
      ...(item.entity_type === 'Server' ? [{ label: 'IP', value: data.ip as string | undefined }] : []),
      { label: 'AnyDesk', value: data.anydesk as string | undefined },
      { label: 'TeamViewer', value: data.teamviewer as string | undefined },
      { label: 'RDP', value: data.rdp as string | undefined },
      { label: 'LM', value: data.litemanager as string | undefined },
    ];

    return items.filter((entry) => entry.value);
  };

  const ticketDetails = ticketDetailsData?.data;
  const equipment = equipmentData?.data || [];
  const fiscalItems = equipment.filter((item) => item.entity_type === 'FiscalRegister');
  const comments = (ticketDetails?.comments || []).map((comment) => ({
    id: comment.uuid,
    author: comment.author_name || 'Сотрудник',
    created_at: dayjs(comment.creation_date).format('DD.MM.YYYY HH:mm'),
    text: comment.text || '',
  }));
  const attachments = ((ticketDetails?.attachments || []) as Array<Record<string, unknown>>)
    .map((item) => {
      const fileName = String(item.file_name || item.FileName || 'Файл');
      const rawPath = String(item.file_path || item.FilePath || '');
      const filePath = rawPath
        .replace(/^\/static\//, '/api/static/')
        .replace(/^static\//, '/api/static/');
      const mimeType = String(item.mime_type || item.MimeType || '');
      return {
        fileName,
        filePath,
        mimeType,
      };
    })
    .filter((item) => item.filePath !== '');

  const connectionsGroups = useMemo(() => {
    return equipment
      .map((item) => {
        const connections = resolveConnectionItems(item);
        if (!connections || connections.length === 0) return null;
        return {
          key: (item.data as { uuid?: string })?.uuid,
          title: resolveEquipmentTitle(item),
          connections,
          item,
        };
      })
      .filter(Boolean) as Array<{
      key?: string;
      title: string;
      connections: Array<{ label: string; value?: string }>;
      item: InfrastructureItem;
    }>;
  }, [equipment]);

  const tableData = useMemo(() => cards.map((ticket) => ({
    ...ticket,
    company_display: ticket.company_name || ticket.company_id || 'Компания не указана',
    assignee_display: ticket.assignee?.fullName || '-',
    description_display: compactDescription(ticket.description),
    last_comment_display: compactDescription(ticket.last_comment),
  })), [cards]);

  type TableRow = typeof tableData[number];


  const getColumnSearchProps = React.useCallback((dataIndex: keyof TableRow, placeholder: string): ColumnType<TableRow> => ({
    filterDropdown: ({ setSelectedKeys, selectedKeys, confirm, clearFilters }: any) => (
      <div style={{ padding: 8 }}>
        <Input
          placeholder={placeholder}
          value={selectedKeys[0]}
          onChange={(event) => setSelectedKeys(event.target.value ? [event.target.value] : [])}
          onPressEnter={() => confirm()}
          style={{ marginBottom: 8, display: 'block' }}
        />
        <Space>
          <Button type="primary" onClick={() => confirm()} size="small">Поиск</Button>
          <Button onClick={() => { clearFilters?.(); confirm(); }} size="small">Сброс</Button>
        </Space>
      </div>
    ),
    filterIcon: (filtered: boolean) => <SearchOutlined style={{ color: filtered ? '#1677ff' : undefined }} />,
    onFilter: (value, record) =>
      String(record[dataIndex] ?? '').toLowerCase().includes(String(value).toLowerCase()),
  }), []);

  const tableColumnsBase: ColumnsType<TableRow> = useMemo(() => [
    {
      title: 'Номер',
      dataIndex: 'number',
      key: 'number',
      width: 90,
      sorter: (a, b) => a.number - b.number,
      ...getColumnSearchProps('number', 'Номер'),
      render: (val: number) => <Text strong>#{val}</Text>,
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 140,
      filters: Object.entries(STATUS_META).map(([value, meta]) => ({ text: meta.label, value })),
      onFilter: (value, record) => record.status === value,
      render: (status: string, record) => {
        const meta = resolveStatusMeta(status);
        return (
          <Space size={4} wrap>
            <Tag color={meta.color}>{meta.label}</Tag>
            {record.is_common_contract && <Tag color="gold">Платный</Tag>}
          </Space>
        );
      },
    },
    {
      title: 'Компания',
      dataIndex: 'company_display',
      key: 'company_display',
      width: 200,
      sorter: (a, b) => a.company_display.localeCompare(b.company_display),
      ...getColumnSearchProps('company_display', 'Компания'),
      ellipsis: true,
    },
    {
      title: 'Описание',
      dataIndex: 'description_display',
      key: 'description',
      width: 260,
      ellipsis: true,
      render: (value: string) => (
        <Text type="secondary" ellipsis style={{ width: '100%', display: 'block' }}>
          {value}
        </Text>
      ),
    },
    {
      title: 'Последний комментарий',
      dataIndex: 'last_comment_display',
      key: 'last_comment',
      width: 260,
      ellipsis: true,
      render: (value: string) => (
        <Text type="secondary" ellipsis style={{ width: '100%', display: 'block' }}>
          {value}
        </Text>
      ),
    },
    {
      title: 'Исполнитель',
      dataIndex: 'assignee_display',
      key: 'assignee_display',
      sorter: (a, b) => a.assignee_display.localeCompare(b.assignee_display),
      ...getColumnSearchProps('assignee_display', 'Исполнитель'),
    },
  ], [getColumnSearchProps]);

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
        if (!base) {
          continue;
        }
        next.push({
          ...base,
          width: entry.width ?? base.width,
        });
        seen.add(entry.key);
      }
      for (const col of tableColumnsBase) {
        const key = col.key as string;
        if (seen.has(key)) {
          continue;
        }
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

  const handleResize = (index: number) => (_event: React.SyntheticEvent, data: { size: { width: number } }) => {
    setTableColumnsState((columns) => {
      const nextColumns = [...columns];
      nextColumns[index] = {
        ...nextColumns[index],
        width: data.size.width,
      };
      return nextColumns;
    });
  };

  const handleDragEnd = ({ active, over }: { active: { id: string }; over?: { id: string } | null }) => {
    if (!over || active.id === over.id) return;
    setTableColumnsState((columns) => {
      const oldIndex = columns.findIndex((col) => col.key === active.id);
      const newIndex = columns.findIndex((col) => col.key === over.id);
      if (oldIndex === -1 || newIndex === -1) return columns;
      return arrayMove(columns, oldIndex, newIndex);
    });
  };

  const tableColumns = tableColumnsState.map((col, index) => ({
    ...col,
    onHeaderCell: () => ({
      id: col.key as string,
      width: col.width,
      onResize: handleResize(index),
      onResizeStart: () => setIsResizingColumn(true),
      onResizeStop: () => setIsResizingColumn(false),
      isResizing: isResizingColumn,
    }),
  }));

  const metadata = ticketDetails?.metadata as Record<string, unknown> | undefined;
  const metadataCreatedAt = (metadata?.created_at as string | undefined) || (metadata?.CreatedAt as string | undefined);
  const metadataUpdatedAt = (metadata?.updated_at as string | undefined) || (metadata?.UpdatedAt as string | undefined);
  const metadataCompany = (metadata?.company_name as string | undefined) || (metadata?.CompanyName as string | undefined);
  const metadataCompanyId = (metadata?.company_id as string | undefined) || (metadata?.CompanyID as string | undefined);
  const metadataStatus = (metadata?.status as string | undefined) || (metadata?.Status as string | undefined);
  const metadataNumber = (metadata?.number as number | undefined) || (metadata?.Number as number | undefined);
  const metadataContractId = (metadata?.contract_id as string | undefined) || (metadata?.ContractID as string | undefined);
  const metadataIsCommonContract =
    (metadata?.is_common_contract as boolean | undefined) || (metadata?.IsCommonContract as boolean | undefined);

  const modalNumber = selectedTicket?.number ?? metadataNumber;
  const modalCompany = selectedTicket?.company_name || metadataCompany || selectedTicket?.company_id || metadataCompanyId;
  const modalAssignee = selectedTicket?.assignee?.fullName || ticketDetails?.metadata?.assignee?.fullName || '-';
  const modalUpdated = selectedTicket?.last_activity || metadataUpdatedAt;
  const modalCreated = metadataCreatedAt;
  const modalStatus = selectedTicket?.status || metadataStatus;
  const modalContractId = selectedTicket?.contract_id || metadataContractId;
  const modalIsCommonContract = selectedTicket?.is_common_contract ?? metadataIsCommonContract ?? false;

  useEffect(() => {
    if (!ticketParam) return;
    const rawDescription =
      (metadata?.description as string | undefined) ||
      selectedTicket?.description ||
      '';
    setDescriptionDraft(normalizeDescription(rawDescription));
    setIsEditingDescription(false);
  }, [metadata?.description, selectedTicket?.description, ticketParam]);

  const changeStatusMutation = useMutation({
    mutationFn: async (payload: { id: string; status: string; comment: string }) =>
      ticketsApi.changeStatus(payload.id, payload.status, payload.comment),
    onSuccess: () => {
      message.success('Статус обновлен');
      setIsStatusModalOpen(false);
      setPendingStatus(null);
      setStatusComment('');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => {
      message.error('Не удалось обновить статус');
    },
  });

  const updateDescriptionMutation = useMutation({
    mutationFn: async (payload: { id: string; description: string }) =>
      ticketsApi.updateDescription(payload.id, payload.description),
    onSuccess: () => {
      message.success('Описание обновлено');
      setIsEditingDescription(false);
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => {
      message.error('Не удалось обновить описание');
    },
  });

  if (isLoading) {
    return (
      <div style={{ textAlign: 'center', padding: 60 }}>
        <Spin size="large" />
      </div>
    );
  }

  if (isError) {
    return <Text type="danger">Ошибка при загрузке заявок</Text>;
  }

  return (
    <div>
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
          <Title level={4} style={{ margin: 0 }}>Заявки</Title>
          <Space size="middle">
            <Button type="primary" onClick={() => setIsCreateOpen(true)}>Новая заявка</Button>
            <Segmented
              value={viewMode}
              onChange={(value) => setViewMode(value as 'cards' | 'list' | 'table')}
              options={[
                { value: 'cards', label: 'Карточки' },
                { value: 'list', label: 'Список' },
                { value: 'table', label: 'Таблица' },
              ]}
            />
            <Text type="secondary">Найдено: {total}</Text>
          </Space>
        </div>

        {cards.length === 0 ? (
          <Empty description="Заявки не найдены" />
        ) : viewMode === 'cards' ? (
          <Row gutter={[16, 16]}>
            {cards.map((ticket) => {
              const meta = resolveStatusMeta(ticket.status);
              const description = compactDescription(ticket.description);
              const lastComment = compactDescription(ticket.last_comment);
              const companyTitle = ticket.company_name || ticket.company_id || 'Компания не указана';
              const createdAt = ticket.created_at ? dayjs(ticket.created_at).format('DD.MM.YYYY HH:mm') : '-';
              return (
                <Col key={ticket.id} xs={24} sm={12} lg={8} xl={6}>
                  <Card
                    className="glass-panel"
                    hoverable
                    size="small"
                    style={{ width: 400, height: 200 }}
                    onClick={() => onOpenTicket(ticket)}
                    styles={{ body: { height: '100%' } }}
                  >
                    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
                      <Space orientation="vertical" size={4} style={{ width: '100%' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                          <Text strong>#{ticket.number}</Text>
                          <Space size={4}>
                            <Tag color={meta.color}>{meta.label}</Tag>
                            {ticket.is_common_contract && <Tag color="gold">Платный</Tag>}
                          </Space>
                        </div>
                        <Text strong>{companyTitle}</Text>
                        {description && (
                          <Paragraph type="secondary" ellipsis={{ rows: 2 }} style={{ marginBottom: 0 }}>
                            {description}
                          </Paragraph>
                        )}
                        {lastComment && (
                          <Paragraph type="secondary" ellipsis={{ rows: 1 }} style={{ marginBottom: 0 }}>
                            <Text strong>Комментарий:</Text> {lastComment}
                          </Paragraph>
                        )}
                      </Space>
                      <div style={{ marginTop: 'auto' }}>
                        <Text type="secondary" style={{ display: 'block' }}>
                          Обновлено: {dayjs(ticket.last_activity).format('DD.MM.YYYY HH:mm')}
                        </Text>
                        <Text type="secondary" style={{ display: 'block' }}>
                          Создано: {createdAt}
                        </Text>
                      </div>
                    </div>
                  </Card>
                </Col>
              );
            })}
          </Row>
        ) : viewMode === 'list' ? (
          <List
            dataSource={cards}
            split={false}
            size="small"
            renderItem={(ticket) => {
              const meta = resolveStatusMeta(ticket.status);
              const description = compactDescription(ticket.description);
              const lastComment = compactDescription(ticket.last_comment);
              const companyTitle = ticket.company_name || ticket.company_id || 'Компания не указана';
              const createdAt = ticket.created_at ? dayjs(ticket.created_at).format('DD.MM.YYYY HH:mm') : '-';
              return (
                <List.Item key={ticket.id}>
                  <Card
                    className="glass-panel"
                    hoverable
                    size="small"
                    onClick={() => onOpenTicket(ticket)}
                    style={{ width: '100%' }}
                  >
                    <Space orientation="vertical" size={4} style={{ width: '100%' }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <Space size="middle">
                          <Text strong>#{ticket.number}</Text>
                          <Text strong>{companyTitle}</Text>
                        </Space>
                        <Space size={4}>
                          <Tag color={meta.color}>{meta.label}</Tag>
                          {ticket.is_common_contract && <Tag color="gold">Платный</Tag>}
                        </Space>
                      </div>
                      {description && (
                        <Paragraph type="secondary" ellipsis={{ rows: 2 }} style={{ marginBottom: 0 }}>
                          {description}
                        </Paragraph>
                      )}
                      {lastComment && (
                        <Paragraph type="secondary" ellipsis={{ rows: 1 }} style={{ marginBottom: 0 }}>
                          <Text strong>Комментарий:</Text> {lastComment}
                        </Paragraph>
                      )}
                      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: '#8c8c8c' }}>
                        <Text type="secondary">Обновлено: {dayjs(ticket.last_activity).format('DD.MM.YYYY HH:mm')}</Text>
                        <Text type="secondary">Создано: {createdAt}</Text>
                      </div>
                    </Space>
                  </Card>
                </List.Item>
              );
            }}
          />
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={tableColumns.map((col) => col.key as string)}
              strategy={horizontalListSortingStrategy}
            >
              <Table
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
                  onClick: () => onOpenTicket(record),
                  style: { cursor: 'pointer' },
                })}
              />
            </SortableContext>
          </DndContext>
        )}

        <div style={{ display: 'flex', justifyContent: 'center', marginTop: 16 }}>
          <Pagination
            current={page}
            pageSize={limit}
            total={total}
            onChange={onPageChange}
            showSizeChanger={false}
          />
        </div>
      </Space>

      <Modal
        open={Boolean(ticketParam)}
        onCancel={onCloseTicket}
        footer={null}
        width={760}
        title={(
          <div style={{ display: 'grid', alignItems: 'center', gridTemplateColumns: '1fr auto 1fr' }}>
            <span>{modalNumber ? `Заявка #${modalNumber}` : 'Заявка'}</span>
            <Space>
              <Button
                size="small"
                onClick={() => setIsAttachmentsModalOpen(true)}
              >
                Вложения ({attachments.length})
              </Button>
              <Button
                size="small"
                onClick={() => {
                  if (!selectedTicketId) return;
                  setPendingStatus('resolved');
                  setStatusComment('');
                  setIsStatusModalOpen(true);
                }}
                disabled={modalStatus === 'resolved'}
              >
                Завершить
              </Button>
            </Space>
            <span />
          </div>
        )}
      >
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Row gutter={[16, 16]}>
            <Col xs={24} md={16}>
              <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                <Descriptions size="small" column={1} bordered>
                  <Descriptions.Item label="Статус">
                    <Space size="small" wrap>
                      <Select
                        value={modalStatus}
                        style={{ minWidth: 180 }}
                        options={[
                          { value: 'new', label: 'Новая' },
                          { value: 'in_progress', label: 'В работе' },
                          { value: 'pending', label: 'Ожидание' },
                          { value: 'resolved', label: 'Решена' },
                          { value: 'closed', label: 'Закрыта' },
                        ]}
                        optionRender={(option) => {
                          const meta = resolveStatusMeta(String(option.value));
                          return <Tag color={meta.color}>{meta.label}</Tag>;
                        }}
                        onChange={(nextStatus) => {
                          if (!selectedTicketId) return;
                          if (nextStatus === modalStatus) return;
                          if (nextStatus === 'resolved') {
                            setPendingStatus(nextStatus);
                            setStatusComment('');
                            setIsStatusModalOpen(true);
                            return;
                          }
                          changeStatusMutation.mutate({
                            id: selectedTicketId,
                            status: nextStatus,
                            comment: '',
                          });
                        }}
                      />
                      {modalIsCommonContract && <Tag color="gold">Платный</Tag>}
                    </Space>
                  </Descriptions.Item>
                  <Descriptions.Item label="Компания">
                    {metadataCompanyId ? (
                      <Link to={`/companies/${metadataCompanyId}`}>{modalCompany || metadataCompanyId}</Link>
                    ) : (
                      modalCompany || '-'
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label="Контракт">
                    {modalIsCommonContract ? (
                      <Tag color="gold">Общий контракт</Tag>
                    ) : modalContractId ? (
                      <Text>{modalContractId}</Text>
                    ) : (
                      '-'
                    )}
                  </Descriptions.Item>
                  <Descriptions.Item label="Исполнитель">
                    {modalAssignee}
                  </Descriptions.Item>
                </Descriptions>

                <div>
                  <Space size="small" style={{ marginBottom: 8 }}>
                    <Text strong>Описание</Text>
                    {!isEditingDescription && (
                      <Button size="small" onClick={() => setIsEditingDescription(true)}>
                        Редактировать
                      </Button>
                    )}
                  </Space>
                  {isEditingDescription ? (
                    <Space orientation="vertical" size="small" style={{ width: '100%' }}>
                      <Input.TextArea
                        rows={4}
                        value={descriptionDraft}
                        onChange={(event) => setDescriptionDraft(event.target.value)}
                      />
                      <Space>
                        <Button
                          type="primary"
                          loading={updateDescriptionMutation.isPending}
                          onClick={() => {
                            if (!selectedTicketId) return;
                            updateDescriptionMutation.mutate({
                              id: selectedTicketId,
                              description: descriptionDraft.trim(),
                            });
                          }}
                        >
                          Сохранить
                        </Button>
                        <Button onClick={() => setIsEditingDescription(false)}>Отмена</Button>
                      </Space>
                    </Space>
                  ) : (
                    <div
                      style={{ width: '100%', color: 'rgba(0,0,0,0.65)' }}
                      dangerouslySetInnerHTML={{
                        __html: sanitizeRichHtml((metadata?.description as string | undefined) || selectedTicket?.description || '<span>Нет описания</span>'),
                      }}
                    />
                  )}
                </div>

                <Divider style={{ margin: '8px 0' }} />

                <div>
                  <Text strong>Фискальные регистраторы</Text>
                  {isEquipmentLoading ? (
                    <div style={{ textAlign: 'center', padding: 16 }}>
                      <Spin />
                    </div>
                  ) : fiscalItems.length === 0 ? (
                    <Empty description="Фискальные регистраторы не найдены" />
                  ) : (
                    <Row gutter={[12, 12]}>
                      {fiscalItems.map((item) => {
                        const data = item.data as Record<string, unknown>;
                        return (
                          <Col key={(data.uuid as string) || (data.serial_number as string)} xs={24} md={12}>
                            <Card
                              size="small"
                              className="glass-panel"
                              hoverable
                              onClick={() => handleEquipmentClick(item)}
                              style={{ height: '100%' }}
                            >
                              <Space orientation="vertical" size={4} style={{ width: '100%' }}>
                                <Text strong>{(data.model_kkt as string) || 'ККТ'}</Text>
                                {data.organization_name && (
                                  <Text type="secondary" style={{ fontSize: 12 }}>{String(data.organization_name)}</Text>
                                )}
                                <Space orientation="vertical" size={2} style={{ width: '100%' }}>
                                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                                    <Text type="secondary">РНМ:</Text>
                                    <Paragraph copyable={{ text: data.rn_kkt as string | undefined }} style={{ margin: 0, fontFamily: 'monospace' }}>
                                      {formatRnm(data.rn_kkt as string | undefined)}
                                    </Paragraph>
                                  </div>
                                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                                    <Text type="secondary">SN:</Text>
                                    <Paragraph copyable={{ text: data.serial_number as string | undefined }} style={{ margin: 0, fontSize: 12 }}>
                                      {String(data.serial_number ?? '')}
                                    </Paragraph>
                                  </div>
                                  <div style={{ marginTop: 4 }}>
                                    {renderFnInfo(data.fn_expire_date as string | undefined)}
                                  </div>
                                  {(data.driver_version || data.fr_firmware) && (
                                    <div style={{ marginTop: 4, fontSize: 11, color: '#8c8c8c', borderTop: '1px solid rgba(0,0,0,0.06)', paddingTop: 4 }}>
                                      FW: {String(data.fr_firmware ?? '')} {data.driver_version ? `| Drv: ${String(data.driver_version)}` : ''}
                                    </div>
                                  )}
                                </Space>
                              </Space>
                            </Card>
                          </Col>
                        );
                      })}
                    </Row>
                  )}
                </div>
              </Space>
            </Col>

            <Col xs={24} md={8}>
              <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                <Text strong>Подключения</Text>
                {isEquipmentLoading ? (
                  <div style={{ textAlign: 'center', padding: 16 }}>
                    <Spin />
                  </div>
                ) : connectionsGroups.length === 0 ? (
                  <Empty description="Подключения не найдены" />
                ) : (
                  <Row gutter={[12, 12]}>
                    {connectionsGroups.map((group) => (
                      <Col key={group.key || group.title} xs={24}>
                        <Card
                          size="small"
                          className="glass-panel"
                          hoverable
                          style={{ height: '100%' }}
                          onClick={() => handleEquipmentClick(group.item)}
                        >
                          <Space orientation="vertical" size={4} style={{ width: '100%' }}>
                            <Text>{group.title}</Text>
                            <Space orientation="vertical" size={0} style={{ width: '100%' }}>
                              {group.connections.map((entry) => (
                                <Paragraph key={`${group.title}-${entry.label}-${entry.value}`} copyable={{ text: entry.value }} style={{ margin: 0 }}>
                                  <Text type="secondary">{entry.label}:</Text> {entry.value}
                                </Paragraph>
                              ))}
                            </Space>
                          </Space>
                        </Card>
                      </Col>
                    ))}
                  </Row>
                )}
              </Space>
            </Col>
          </Row>

          {isTicketLoading && (
            <div style={{ textAlign: 'center', padding: 12 }}>
              <Spin />
            </div>
          )}

          <Divider style={{ margin: '8px 0' }} />

          <div>
            <Text strong>Комментарии</Text>
            {comments.length === 0 ? (
              <Empty description="Комментариев нет" />
            ) : (
              <List
                dataSource={comments}
                renderItem={(item) => (
                  <List.Item key={item.id}>
                    <Space orientation="vertical" size={2} style={{ width: '100%' }}>
                      <Space size="small">
                        <Text type="secondary" underline style={{ cursor: 'pointer' }}>
                          {item.author}
                        </Text>
                        <Text type="secondary">{item.created_at}</Text>
                      </Space>
                      <div
                        style={{ width: '100%' }}
                        dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(item.text) }}
                      />
                    </Space>
                  </List.Item>
                )}
              />
            )}

            <Space orientation="vertical" size="small" style={{ width: '100%' }}>
              <Input.TextArea
                rows={3}
                placeholder="Введите комментарий"
                value={commentText}
                onChange={(event) => setCommentText(event.target.value)}
              />
              <Button
                type="primary"
                disabled={!commentText.trim()}
                onClick={() => setCommentText('')}
              >
                Отправить
              </Button>
            </Space>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12, color: '#8c8c8c' }}>
            <Text type="secondary">
              Создано: {modalCreated ? dayjs(modalCreated).format('DD.MM.YYYY HH:mm') : '-'}
            </Text>
            <Text type="secondary">
              Обновлено: {modalUpdated ? dayjs(modalUpdated).format('DD.MM.YYYY HH:mm') : '-'}
            </Text>
          </div>
        </Space>
      </Modal>

      <Modal
        open={isAttachmentsModalOpen}
        onCancel={() => setIsAttachmentsModalOpen(false)}
        footer={null}
        width={640}
        title={`Вложения (${attachments.length})`}
      >
        {attachments.length === 0 ? (
          <Empty description="Вложений нет" />
        ) : (
          <List
            dataSource={attachments}
            renderItem={(item) => {
              const isImage = item.mimeType.startsWith('image/');
              return (
                <List.Item>
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
                    <a href={item.filePath} target="_blank" rel="noreferrer">
                      {item.fileName}
                    </a>
                    {isImage && (
                      <a href={item.filePath} target="_blank" rel="noreferrer">
                        <img
                          src={item.filePath}
                          alt={item.fileName}
                          style={{ maxWidth: '100%', maxHeight: 280, objectFit: 'contain', border: '1px solid #f0f0f0', borderRadius: 8 }}
                        />
                      </a>
                    )}
                  </Space>
                </List.Item>
              );
            }}
          />
        )}
      </Modal>

      <Modal
        open={isStatusModalOpen}
        onCancel={() => {
          setIsStatusModalOpen(false);
          setPendingStatus(null);
          setStatusComment('');
        }}
        onOk={() => {
          if (!selectedTicketId || !pendingStatus) return;
          changeStatusMutation.mutate({
            id: selectedTicketId,
            status: pendingStatus,
            comment: statusComment.trim(),
          });
        }}
        okText="Ок"
        cancelText="Отмена"
        okButtonProps={{ disabled: !statusComment.trim() }}
        confirmLoading={changeStatusMutation.isPending}
        title="Итоговый результат"
      >
        <Input.TextArea
          rows={4}
          placeholder="Опишите результат"
          value={statusComment}
          onChange={(event) => setStatusComment(event.target.value)}
        />
      </Modal>

      <NewTicketModal
        open={isCreateOpen}
        onClose={() => setIsCreateOpen(false)}
        onCreated={() => {}}
      />
    </div>
  );
};

export default TicketsPage;

