import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import {
  App as AntdApp,
  Button,
  Card,
  Empty,
  Popconfirm,
  Segmented,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined, RedoOutlined } from '@ant-design/icons';
import { pyrusAdminApi } from '@/api/pyrusAdmin';
import type { PyrusIncomingEventDTO, PyrusIncomingEventStatus, PyrusIncomingTaskDTO } from '@/types/api';

const { Text } = Typography;

type TasksScope = 'problem' | 'all';

const PAGE_SIZE = 20;

const PYRUS_EVENT_STATUS_LABELS: Record<PyrusIncomingEventStatus, string> = {
  new: 'Новое',
  queued: 'В очереди',
  processing: 'Обрабатывается',
  waiting: 'Ждёт данных',
  failed: 'Ошибка',
  done: 'Обработано',
  ignored: 'Пропущено',
};

const PYRUS_EVENT_STATUS_COLORS: Record<PyrusIncomingEventStatus, string> = {
  new: 'default',
  queued: 'processing',
  processing: 'processing',
  waiting: 'warning',
  failed: 'error',
  done: 'success',
  ignored: 'default',
};

const STATUS_ORDER: PyrusIncomingEventStatus[] = ['failed', 'waiting', 'processing', 'queued', 'new', 'done', 'ignored'];

const formatDateTime = (value?: string) => {
  if (!value) {
    return '—';
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ru-RU');
};

const extractErrorText = (error: unknown, fallback: string) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || fallback;
};

const canReplayEvent = (status: PyrusIncomingEventStatus) => status === 'failed' || status === 'waiting';

const TaskEventsTable: React.FC<{
  taskId: number;
  onReplayEvent: (eventId: string) => void;
  replayingEventId?: string;
}> = ({ taskId, onReplayEvent, replayingEventId }) => {
  const { data, isError } = useQuery({
    queryKey: ['pyrus-incoming-events', taskId],
    queryFn: async () => (await pyrusAdminApi.listTaskEvents(taskId)).data || [],
  });

  const columns: ColumnsType<PyrusIncomingEventDTO> = useMemo(() => [
    {
      title: 'Получено',
      dataIndex: 'received_at',
      key: 'received_at',
      width: 170,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 140,
      render: (value: PyrusIncomingEventStatus) => (
        <Tag color={PYRUS_EVENT_STATUS_COLORS[value]}>{PYRUS_EVENT_STATUS_LABELS[value] ?? value}</Tag>
      ),
    },
    {
      title: 'Попытки',
      key: 'attempts',
      width: 120,
      render: (_, record) => (
        <Text>
          {record.attempts}
          {record.replay_count ? <Text type="secondary"> · повторов вручную: {record.replay_count}</Text> : null}
        </Text>
      ),
    },
    {
      title: 'Причина',
      key: 'last_error',
      render: (_, record) => (
        <Space orientation="vertical" size={0}>
          <Text type={record.status === 'failed' ? 'danger' : 'secondary'}>{record.last_error || '—'}</Text>
          {record.status === 'waiting' && record.next_retry_at ? (
            <Text type="secondary">Следующая попытка: {formatDateTime(record.next_retry_at)}</Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '',
      key: 'actions',
      width: 130,
      render: (_, record) => (
        <Button
          size="small"
          icon={<RedoOutlined />}
          disabled={!canReplayEvent(record.status)}
          loading={replayingEventId === record.id}
          onClick={() => onReplayEvent(record.id)}
        >
          Повторить
        </Button>
      ),
    },
  ], [onReplayEvent, replayingEventId]);

  if (isError) {
    return <Text type="danger">Не удалось загрузить события задачи</Text>;
  }

  return (
    <Table<PyrusIncomingEventDTO>
      size="small"
      rowKey="id"
      columns={columns}
      dataSource={data ?? []}
      pagination={false}
    />
  );
};

// Сводка входящих событий Pyrus по задачам: показывает, какие задачи не превратились в тикет, и повторяет их целиком.
const PyrusIncomingTasksPanel: React.FC = () => {
  const { message } = AntdApp.useApp();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<TasksScope>('problem');
  const [page, setPage] = useState(1);
  const [expandedKeys, setExpandedKeys] = useState<number[]>([]);

  const tasksQuery = useQuery({
    queryKey: ['pyrus-incoming-tasks', scope, page],
    queryFn: () => pyrusAdminApi.listIncomingTasks({ scope, limit: PAGE_SIZE, offset: (page - 1) * PAGE_SIZE }),
  });

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['pyrus-incoming-tasks'] });
    void queryClient.invalidateQueries({ queryKey: ['pyrus-incoming-events'] });
  };

  const replayTaskMutation = useMutation({
    mutationFn: (taskId: number) => pyrusAdminApi.replayTask(taskId),
    onSuccess: (response, taskId) => {
      message.success(`Задача ${taskId}: повторно запущено событий — ${response.data?.events ?? 0}`);
      invalidate();
    },
    onError: (error) => message.error(extractErrorText(error, 'Не удалось повторить задачу')),
  });

  const replayEventMutation = useMutation({
    mutationFn: (eventId: string) => pyrusAdminApi.replayEvent(eventId),
    onSuccess: () => {
      message.success('Событие повторно запущено');
      invalidate();
    },
    onError: (error) => message.error(extractErrorText(error, 'Не удалось повторить событие')),
  });

  const replayProblemMutation = useMutation({
    mutationFn: () => pyrusAdminApi.replayProblemTasks(),
    onSuccess: (response) => {
      message.success(`Повторно запущено задач: ${response.data?.tasks ?? 0}, событий: ${response.data?.events ?? 0}`);
      invalidate();
    },
    onError: (error) => message.error(extractErrorText(error, 'Не удалось повторить проблемные задачи')),
  });

  const tasks = tasksQuery.data?.data ?? [];
  const total = tasksQuery.data?.meta?.total ?? 0;

  const columns: ColumnsType<PyrusIncomingTaskDTO> = useMemo(() => [
    {
      title: 'Задача Pyrus',
      key: 'task',
      width: 140,
      render: (_, record) => (
        <a href={`https://pyrus.com/t#id${record.task_id}`} target="_blank" rel="noreferrer">
          {record.task_id}
        </a>
      ),
    },
    {
      title: 'Обращение',
      key: 'subject',
      width: 300,
      render: (_, record) => (
        <Space orientation="vertical" size={0}>
          <Text strong>{record.subject || '—'}</Text>
          <Space size={8} wrap>
            {record.client_name ? <Text type="secondary">{record.client_name}</Text> : null}
            {record.crm_id ? <Tag>CRMID {record.crm_id}</Tag> : <Tag color="warning">CRMID не заполнен</Tag>}
          </Space>
        </Space>
      ),
    },
    {
      title: 'Тикет',
      key: 'ticket',
      width: 120,
      render: (_, record) => (
        record.ticket_id
          ? <Link to={`/tickets/${record.ticket_id}`}>Открыть</Link>
          : <Tag color="warning">Не создан</Tag>
      ),
    },
    {
      title: 'События',
      key: 'events',
      width: 240,
      render: (_, record) => (
        <Space size={[4, 4]} wrap>
          {STATUS_ORDER.filter((status) => (record.status_counts[status] ?? 0) > 0).map((status) => (
            <Tag key={status} color={PYRUS_EVENT_STATUS_COLORS[status]}>
              {PYRUS_EVENT_STATUS_LABELS[status]}: {record.status_counts[status]}
            </Tag>
          ))}
        </Space>
      ),
    },
    {
      title: 'Причина',
      key: 'last_error',
      render: (_, record) => (
        <Space orientation="vertical" size={0}>
          {record.last_error ? (
            <Tooltip title={record.last_error}>
              <Text type="danger" ellipsis style={{ maxWidth: 320 }}>{record.last_error}</Text>
            </Tooltip>
          ) : <Text type="secondary">—</Text>}
          {record.next_retry_at ? (
            <Text type="secondary">Следующая попытка: {formatDateTime(record.next_retry_at)}</Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: 'Последнее событие',
      dataIndex: 'last_received_at',
      key: 'last_received_at',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '',
      key: 'actions',
      width: 150,
      render: (_, record) => (
        <Button
          size="small"
          type={record.needs_attention ? 'primary' : 'default'}
          icon={<RedoOutlined />}
          disabled={!record.needs_attention}
          loading={replayTaskMutation.isPending && replayTaskMutation.variables === record.task_id}
          onClick={() => replayTaskMutation.mutate(record.task_id)}
        >
          Повторить задачу
        </Button>
      ),
    },
  ], [replayTaskMutation]);

  return (
    <Card
      className="glass-panel"
      title="Входящие события Pyrus"
      extra={(
        <Space wrap>
          <Segmented<TasksScope>
            value={scope}
            onChange={(value) => {
              setScope(value);
              setPage(1);
              setExpandedKeys([]);
            }}
            options={[
              { label: 'Требуют внимания', value: 'problem' },
              { label: 'Все задачи', value: 'all' },
            ]}
          />
          <Button icon={<ReloadOutlined />} onClick={invalidate}>
            Обновить
          </Button>
          <Popconfirm
            title="Повторить все проблемные задачи?"
            description="События каждой задачи обрабатываются вместе: тикет собирается по актуальной задаче Pyrus, дублей не будет."
            okText="Повторить"
            cancelText="Отмена"
            onConfirm={() => replayProblemMutation.mutate()}
          >
            <Button
              type="primary"
              icon={<RedoOutlined />}
              loading={replayProblemMutation.isPending}
              disabled={scope !== 'problem' || total === 0}
            >
              Повторить все
            </Button>
          </Popconfirm>
        </Space>
      )}
    >
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <Text type="secondary">
          Событие со статусом «Ждёт данных» повторяется автоматически, пока не появятся нужные данные
          (например, у сервера не проставлен CRMID). После исправления данных можно не ждать и повторить задачу вручную.
        </Text>
        <Table<PyrusIncomingTaskDTO>
          size="small"
          rowKey="task_id"
          columns={columns}
          dataSource={tasks}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={scope === 'problem' ? 'Задач, требующих внимания, нет' : 'Входящих событий нет'}
              />
            ),
          }}
          expandable={{
            expandedRowKeys: expandedKeys,
            onExpandedRowsChange: (keys) => setExpandedKeys(keys.map(Number)),
            expandedRowRender: (record) => (
              <TaskEventsTable
                taskId={record.task_id}
                onReplayEvent={(eventId) => replayEventMutation.mutate(eventId)}
                replayingEventId={replayEventMutation.isPending ? replayEventMutation.variables : undefined}
              />
            ),
          }}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total,
            showSizeChanger: false,
            hideOnSinglePage: true,
            onChange: (next) => {
              setPage(next);
              setExpandedKeys([]);
            },
          }}
        />
      </Space>
    </Card>
  );
};

export default PyrusIncomingTasksPanel;
