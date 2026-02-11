import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Card,
  Col,
  Drawer,
  Input,
  List,
  Pagination,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { MenuOutlined } from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { DndContext, DragEndEvent, PointerSensor, closestCenter, useSensor, useSensors } from '@dnd-kit/core';
import { SortableContext, arrayMove, horizontalListSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Resizable } from 'react-resizable';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { companiesApi } from '@/api/companies';
import { TicketDetailsDTO, TicketStatus } from '@/types/api';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import { useAuthStore } from '@/store/authStore';

const { Text, Paragraph } = Typography;

const STATUS_OPTIONS: Array<{ value: TicketStatus; label: string; color: string }> = [
  { value: 'new', label: 'Новая', color: 'blue' },
  { value: 'in_progress', label: 'В работе', color: 'processing' },
  { value: 'pending', label: 'Ожидание', color: 'orange' },
  { value: 'deferred', label: 'Отложено', color: 'orange' },
  { value: 'onsite', label: 'На выезд', color: 'cyan' },
  { value: 'to_manager', label: 'Передать менеджеру', color: 'purple' },
  { value: 'resolved', label: 'Решена', color: 'green' },
  { value: 'spam', label: 'Спам', color: 'red' },
  { value: 'execution', label: 'Реализация', color: 'magenta' },
  { value: 'closed', label: 'Закрыта', color: 'default' },
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

const sanitizeRichHtml = (value?: string) => {
  if (!value) return '';
  return value
    .replace(/<script[\s\S]*?>[\s\S]*?<\/script>/gi, '')
    .replace(/\son\w+="[^"]*"/gi, '')
    .replace(/\son\w+='[^']*'/gi, '')
    .replace(/javascript:/gi, '')
    .replace(/(src|href)=["']\/static\//gi, '$1="/api/static/')
    .replace(/(src|href)=["']static\//gi, '$1="/api/static/');
};

const statusMeta = (status?: string) => STATUS_OPTIONS.find((item) => item.value === status) || STATUS_OPTIONS[0];
const isClosedLikeStatus = (status?: string) => status === 'resolved' || status === 'closed' || status === 'spam' || status === 'execution';

const estimateHeaderMinWidth = (title: string) => {
  // Базовая оценка: ширина текста заголовка + отступы + иконка drag-handle.
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
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const [commentDraft, setCommentDraft] = useState('');
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [statusComment, setStatusComment] = useState('');
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [isResizingColumn, setIsResizingColumn] = useState(false);

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 8 } }));

  const q = searchParams.get('q') || '';
  const status = searchParams.get('status') || '';
  const company = searchParams.get('company') || '';
  const viewMode = (searchParams.get('view') as ViewMode) || 'list';
  const createParam = searchParams.get('create') || '';
  const pageParam = Number(searchParams.get('page') || 1);
  const page = Number.isFinite(pageParam) && pageParam > 0 ? pageParam : 1;
  const limit = 20;

  const { data, isLoading } = useQuery({
    queryKey: ['tickets', { q, status, company, page }],
    queryFn: () =>
      ticketsApi.getTickets({
        search: q || undefined,
        status: status || undefined,
        company_id: company || undefined,
        limit,
        offset: (page - 1) * limit,
      }),
    staleTime: 20_000,
  });

  const tickets = data?.data || [];
  const total = data?.meta?.total || 0;

  const { data: detailsResponse, isLoading: isDetailsLoading } = useQuery({
    queryKey: ['ticket', selectedTicketId],
    queryFn: () => ticketsApi.getTicket(selectedTicketId || ''),
    enabled: Boolean(selectedTicketId),
  });

  const details: TicketDetailsDTO | undefined = detailsResponse?.data;
  const metadata = details?.metadata;

  const { data: infraResponse, isLoading: isInfraLoading } = useQuery({
    queryKey: ['company-infra', metadata?.company_id],
    queryFn: () => companiesApi.getInfrastructure(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const infrastructure = infraResponse?.data || [];

  const { data: companyResponse } = useQuery({
    queryKey: ['company-profile', metadata?.company_id],
    queryFn: () => companiesApi.getCompany(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const companyTitle = useMemo(() => {
    const companyData = companyResponse?.data as { Title?: string; title?: string; AdditionalName?: string; additional_name?: string } | undefined;
    return (
      companyData?.Title ||
      companyData?.title ||
      companyData?.AdditionalName ||
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
        const title = dataRow.device_name || dataRow.server_name || dataRow.uuid || 'Оборудование';
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
        author: item.author_name || 'Сотрудник',
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
      message.success('Статус обновлён');
      setPendingStatus(null);
      setStatusComment('');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('Не удалось обновить статус'),
  });

  const addCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; comment: string; isPrivate: boolean }) =>
      ticketsApi.addComment(payload.id, payload.comment, payload.isPrivate),
    onSuccess: () => {
      message.success('Комментарий добавлен');
      setCommentDraft('');
      setCommentIsPrivate(false);
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['ticket', selectedTicketId] });
    },
    onError: () => message.error('Не удалось добавить комментарий'),
  });

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: { id: string; label: string; value: string }) =>
      ticketsApi.recordConnectionCopy(payload.id, payload.label, payload.value),
  });

  const closeQuickModal = () => {
    setSelectedTicketId(null);
    setCommentDraft('');
    setCommentIsPrivate(false);
    setPendingStatus(null);
    setStatusComment('');
  };

  useEffect(() => {
    if (createParam === '1') {
      setIsCreateOpen(true);
    }
  }, [createParam]);

  const tableData = useMemo(
    () =>
      tickets.map((ticket) => ({
        ...ticket,
        company_display: ticket.company_name || ticket.company_id || 'Компания не указана',
        last_comment_display: normalizeDescription(ticket.last_comment),
      })),
    [tickets],
  );

  type TableRow = (typeof tableData)[number];

  const tableColumnsBase: ColumnsType<TableRow> = useMemo(
    () => [
      {
        title: 'Номер',
        dataIndex: 'number',
        key: 'number',
        width: 90,
        minWidth: estimateHeaderMinWidth('Номер'),
        render: (val: number) => <Text strong>#{val}</Text>,
      },
      {
        title: 'Статус',
        dataIndex: 'status',
        key: 'status',
        width: 140,
        minWidth: estimateHeaderMinWidth('Статус'),
        render: (value: TicketStatus, row) => {
          const meta = statusMeta(value);
          return (
            <Space size={4}>
              <Tag color={meta.color}>{meta.label}</Tag>
              {row.is_common_contract && <Tag color="gold">Платный</Tag>}
            </Space>
          );
        },
      },
      {
        title: 'Компания',
        dataIndex: 'company_display',
        key: 'company_display',
        width: 220,
        minWidth: estimateHeaderMinWidth('Компания'),
        ellipsis: true,
        render: (value: string) => (
          <Text ellipsis style={{ width: '100%', display: 'block' }}>
            {value}
          </Text>
        ),
      },
      {
        title: 'Тема',
        dataIndex: 'subject',
        key: 'subject',
        width: 260,
        minWidth: estimateHeaderMinWidth('Тема'),
        ellipsis: true,
      },
      {
        title: 'Последний комментарий',
        dataIndex: 'last_comment_display',
        key: 'last_comment',
        width: 260,
        minWidth: estimateHeaderMinWidth('Последний комментарий'),
        ellipsis: true,
        render: (value: string) => (
          <Text type="secondary" ellipsis style={{ width: '100%', display: 'block' }}>
            {value || '-'}
          </Text>
        ),
      },
      {
        title: 'Обновлено',
        dataIndex: 'last_activity',
        key: 'last_activity',
        width: 170,
        minWidth: estimateHeaderMinWidth('Обновлено'),
        render: (value: string) => dayjs(value).format('DD.MM.YYYY HH:mm'),
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

  const tableColumns = tableColumnsState.map((col, index) => ({
    ...col,
    onHeaderCell: () => ({
      id: col.key as string,
      width: col.width,
      minWidth: (col as { minWidth?: number }).minWidth || 90,
      onResize: handleResize(index),
      onResizeStart: () => setIsResizingColumn(true),
      onResizeStop: () => setIsResizingColumn(false),
      isResizing: isResizingColumn,
    }),
  }));

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card>
        {viewMode === 'list' && (
          <List
            loading={isLoading}
            dataSource={tickets}
            renderItem={(item) => {
              const meta = statusMeta(item.status);
              return (
                <List.Item key={item.id} style={{ cursor: 'pointer' }} onClick={() => setSelectedTicketId(item.id)}>
                  <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                    <Space direction="vertical" size={0}>
                      <Space size={8}>
                        <Text strong>#{item.number}</Text>
                        <Tag color={meta.color}>{meta.label}</Tag>
                        {item.is_common_contract && <Tag color="gold">Платный</Tag>}
                      </Space>
                      <Text>{item.subject || 'Без темы'}</Text>
                      <Text type="secondary">{item.company_name || item.company_id}</Text>
                      {item.last_comment && <Text type="secondary">Комментарий: {normalizeDescription(item.last_comment)}</Text>}
                    </Space>
                    <Text type="secondary">{dayjs(item.last_activity).format('DD.MM.YYYY HH:mm')}</Text>
                  </Space>
                </List.Item>
              );
            }}
          />
        )}

        {viewMode === 'cards' && (
          <Row gutter={[12, 12]}>
            {tickets.map((item) => {
              const meta = statusMeta(item.status);
              return (
                <Col key={item.id} xs={24} md={12} xl={8}>
                  <Card hoverable className="glass-panel" onClick={() => setSelectedTicketId(item.id)}>
                    <Space direction="vertical" size={6} style={{ width: '100%' }}>
                      <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                        <Text strong>#{item.number}</Text>
                        <Space size={4}>
                          <Tag color={meta.color}>{meta.label}</Tag>
                          {item.is_common_contract && <Tag color="gold">Платный</Tag>}
                        </Space>
                      </Space>
                      <Text strong>{item.company_name || item.company_id}</Text>
                      <Paragraph style={{ marginBottom: 0 }} ellipsis={{ rows: 2 }}>
                        {item.subject || 'Без темы'}
                      </Paragraph>
                      {item.last_comment && (
                        <Paragraph type="secondary" style={{ marginBottom: 0 }} ellipsis={{ rows: 2 }}>
                          {normalizeDescription(item.last_comment)}
                        </Paragraph>
                      )}
                      <Text type="secondary">{dayjs(item.last_activity).format('DD.MM.YYYY HH:mm')}</Text>
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

        <div style={{ marginTop: 16, display: 'flex', justifyContent: 'center' }}>
          <Pagination
            current={page}
            pageSize={limit}
            total={total}
            showSizeChanger={false}
            onChange={(nextPage) => {
              const next = new URLSearchParams(searchParams);
              next.set('page', String(nextPage));
              setSearchParams(next);
            }}
          />
        </div>
      </Card>

      <Drawer
        open={Boolean(selectedTicketId)}
        onClose={closeQuickModal}
        width={656}
        title={
          metadata ? (
            <div style={{ display: 'grid', alignItems: 'center', gridTemplateColumns: '1fr auto 1fr', gap: 8 }}>
              <span>Быстрый просмотр #{metadata.number}</span>
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
            'Быстрый просмотр заявки'
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
              <Select
                value={metadata.status}
                options={STATUS_OPTIONS.filter((item) => item.value !== 'closed').map((item) => ({ value: item.value, label: item.label }))}
                style={{ width: 220 }}
                onChange={(nextStatus: TicketStatus) => {
                  if (!selectedTicketId || nextStatus === metadata.status) {
                    return;
                  }
                  if (nextStatus === 'resolved') {
                    setPendingStatus(nextStatus);
                    return;
                  }
                  changeStatusMutation.mutate({ id: selectedTicketId, status: nextStatus });
                }}
              />
              <Button
                onClick={() => {
                  if (!selectedTicketId) return;
                  navigate(`/tickets/${selectedTicketId}`);
                  closeQuickModal();
                }}
              >
                Открыть страницу
              </Button>
            </Space>

            <Card size="small" title="Описание">
              <div style={{ whiteSpace: 'pre-wrap' }} dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(metadata.description || '<span>Нет описания</span>') }} />
            </Card>

            {isClosedLikeStatus(metadata.status) && (
              <Card size="small" title="Результат">
                <div style={{ whiteSpace: 'pre-wrap' }} dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(metadata.result || '<span>Результат не заполнен</span>') }} />
              </Card>
            )}

            <Card size="small" title="Подключения">
              {isInfraLoading ? (
                <div style={{ textAlign: 'center', padding: 12 }}>
                  <Spin />
                </div>
              ) : connections.length === 0 ? (
                <Text type="secondary">Подключения не найдены</Text>
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
              title="Комментарии"
            >
              {comments.length > 0 && (
                <List
                  dataSource={comments}
                  renderItem={(item) => (
                    <List.Item key={item.id}>
                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                        <Space size={8}>
                          <Text type="secondary">
                            {item.author} • {item.date}
                          </Text>
                          {item.isPrivate && <Tag color="orange">Приватный</Tag>}
                        </Space>
                        <div style={{ whiteSpace: 'pre-wrap' }} dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(item.text) }} />
                      </Space>
                    </List.Item>
                  )}
                />
              )}

              <Space direction="vertical" size="small" style={{ width: '100%', marginTop: 12 }}>
                <Input.TextArea
                  rows={3}
                  placeholder="Добавьте комментарий"
                  value={commentDraft}
                  onChange={(event) => setCommentDraft(event.target.value)}
                />
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: '#595959' }}>
                  <input
                    type="checkbox"
                    checked={commentIsPrivate}
                    onChange={(event) => setCommentIsPrivate(event.target.checked)}
                  />
                  Приватный комментарий (не синхронизировать во внешние системы)
                </label>
                <Button
                  type="primary"
                  loading={addCommentMutation.isPending}
                  disabled={!commentDraft.trim() || !selectedTicketId}
                  onClick={() => {
                    if (!selectedTicketId) return;
                    addCommentMutation.mutate({ id: selectedTicketId, comment: commentDraft.trim(), isPrivate: commentIsPrivate });
                  }}
                >
                  Отправить
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
        title="Завершение заявки"
        placement="right"
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Input.TextArea
            rows={4}
            value={statusComment}
            onChange={(event) => setStatusComment(event.target.value)}
            placeholder="Опишите итог выполнения заявки"
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
            Завершить заявку
          </Button>
        </Space>
      </Drawer>

      <NewTicketModal
        open={isCreateOpen}
        onClose={() => {
          setIsCreateOpen(false);
          const next = new URLSearchParams(searchParams);
          next.delete('create');
          setSearchParams(next);
        }}
        onCreated={() => {
          const next = new URLSearchParams(searchParams);
          next.delete('create');
          setSearchParams(next);
          queryClient.invalidateQueries({ queryKey: ['tickets'] });
        }}
      />
    </Space>
  );
};

export default TicketsPage;
