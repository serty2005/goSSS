import React, { useMemo, useState } from 'react';
import { Alert, Button, Card, Empty, Segmented, Select, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';

import AgentAdapterRunResultDrawer from '@/components/agents/AgentAdapterRunResultDrawer';
import { formatDateTime, formatRelativeTime } from '@/components/agents/agentDiagnosticsUtils';
import { AgentAdapterRunDTO } from '@/types/api';

const { Text } = Typography;

type Props = {
  runs?: AgentAdapterRunDTO[] | null;
  knownAdapterIDs?: string[];
};

type RunStatusFilter = 'all' | 'pending' | 'completed' | 'failed';

const emptyRuns: AgentAdapterRunDTO[] = [];
const emptyAdapterIDs: string[] = [];

const getRunStatusMeta = (status?: string | null) => {
  const normalized = String(status || '').trim().toLowerCase();
  if (normalized === 'completed') {
    return { color: 'success', label: 'Завершена' };
  }
  if (normalized === 'failed') {
    return { color: 'error', label: 'Ошибка' };
  }
  if (normalized === 'sent') {
    return { color: 'processing', label: 'Отправлена' };
  }
  if (normalized === 'new' || normalized === 'pending') {
    return { color: 'warning', label: 'В очереди' };
  }
  return { color: 'default', label: status || 'Неизвестно' };
};

const formatDuration = (durationMS?: number) => {
  if (typeof durationMS !== 'number' || !Number.isFinite(durationMS) || durationMS <= 0) {
    return '-';
  }
  if (durationMS < 1000) {
    return `${durationMS} мс`;
  }
  return `${(durationMS / 1000).toFixed(durationMS >= 10_000 ? 0 : 1)} сек`;
};

const isPendingRun = (status?: string | null) => {
  const normalized = String(status || '').trim().toLowerCase();
  return normalized === 'new' || normalized === 'pending' || normalized === 'sent';
};

const matchesStatusFilter = (run: AgentAdapterRunDTO, filter: RunStatusFilter) => {
  const normalized = String(run.status || '').trim().toLowerCase();

  if (filter === 'pending') {
    return isPendingRun(run.status);
  }
  if (filter === 'completed') {
    return normalized === 'completed';
  }
  if (filter === 'failed') {
    return normalized === 'failed';
  }
  return true;
};

const AgentAdapterRunsCard: React.FC<Props> = ({
  runs = emptyRuns,
  knownAdapterIDs = emptyAdapterIDs,
}) => {
  const safeRuns = runs ?? emptyRuns;
  const [statusFilter, setStatusFilter] = useState<RunStatusFilter>('all');
  const [adapterFilter, setAdapterFilter] = useState<string>('all');
  const [activeRun, setActiveRun] = useState<AgentAdapterRunDTO | null>(null);

  const adapterOptions = useMemo(() => {
    const values = Array.from(new Set([
      ...knownAdapterIDs,
      ...safeRuns.map((item) => String(item.adapter_id || '').trim()).filter(Boolean),
    ])).sort((left, right) => left.localeCompare(right));

    return [
      { label: 'Все адаптеры', value: 'all' },
      ...values.map((value) => ({ label: value, value })),
    ];
  }, [knownAdapterIDs, runs]);

  const filteredRuns = useMemo(() => (
    safeRuns.filter((run) => {
      if (!matchesStatusFilter(run, statusFilter)) {
        return false;
      }
      if (adapterFilter !== 'all' && run.adapter_id !== adapterFilter) {
        return false;
      }
      return true;
    })
  ), [adapterFilter, safeRuns, statusFilter]);

  const useSegmentedAdapterFilter = adapterOptions.length <= 6;

  const columns = useMemo<ColumnsType<AgentAdapterRunDTO>>(() => [
    {
      title: 'ID',
      dataIndex: 'id',
      key: 'id',
      width: 90,
      render: (value?: number) => value ?? '-',
    },
    {
      title: 'Адаптер',
      dataIndex: 'adapter_id',
      key: 'adapter_id',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Тип',
      dataIndex: 'type',
      key: 'type',
      width: 140,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 140,
      render: (value?: string) => {
        const meta = getRunStatusMeta(value);
        return (
          <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>
            {meta.label}
          </Tag>
        );
      },
    },
    {
      title: 'Создана',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 190,
      render: (value?: string) => (
        <Space direction="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          <Text type="secondary">{formatRelativeTime(value) || '-'}</Text>
        </Space>
      ),
    },
    {
      title: 'Отправлена',
      dataIndex: 'sent_at',
      key: 'sent_at',
      width: 180,
      render: (value?: string | null) => formatDateTime(value),
    },
    {
      title: 'Завершена',
      dataIndex: 'completed_at',
      key: 'completed_at',
      width: 180,
      render: (value?: string | null) => formatDateTime(value),
    },
    {
      title: 'Длительность',
      dataIndex: 'duration_ms',
      key: 'duration_ms',
      width: 120,
      render: (value?: number) => formatDuration(value),
    },
    {
      title: 'Exit code',
      dataIndex: 'exit_code',
      key: 'exit_code',
      width: 110,
      render: (value?: number | null) => (typeof value === 'number' ? value : '-'),
    },
    {
      title: 'Ошибка',
      dataIndex: 'error_text',
      key: 'error_text',
      render: (value?: string) => (
        value ? <Text ellipsis={{ tooltip: value }}>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 170,
      render: (_value, record) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => setActiveRun(record)}>
          Открыть результат
        </Button>
      ),
    },
  ], []);

  const emptyDescription = knownAdapterIDs.length > 0
    ? 'История запусков пока пуста. Сначала поставьте run_adapter в очередь или дождитесь результата от агента.'
    : 'Для этого агента сервер пока не зафиксировал run_adapter / adapter_run команды. Для legacy submit_json-агента это штатно.';

  return (
    <>
      <Card className="glass-panel" title="История запусков адаптеров" size="small">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert
            type="info"
            showIcon
            message="Здесь видно, почему адаптер не отработал"
            description="Таблица показывает очередь и результаты run_adapter / adapter_run. По кнопке можно открыть payload команды, result payload, stdout, stderr и structured JSON."
          />

          <Space wrap size="middle" style={{ width: '100%', justifyContent: 'space-between' }}>
            <Segmented<RunStatusFilter>
              value={statusFilter}
              onChange={(value) => setStatusFilter(value)}
              options={[
                { label: 'Все', value: 'all' },
                { label: 'В очереди', value: 'pending' },
                { label: 'Завершены', value: 'completed' },
                { label: 'С ошибкой', value: 'failed' },
              ]}
            />

            {useSegmentedAdapterFilter ? (
              <Segmented<string>
                value={adapterFilter}
                onChange={(value) => setAdapterFilter(String(value))}
                options={adapterOptions}
              />
            ) : (
              <Select
                style={{ minWidth: 240 }}
                value={adapterFilter}
                onChange={setAdapterFilter}
                options={adapterOptions}
              />
            )}
          </Space>

          {safeRuns.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyDescription} />
          ) : filteredRuns.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="По выбранным фильтрам запусков не найдено" />
          ) : (
            <Table
              size="small"
              rowKey="id"
              columns={columns}
              dataSource={filteredRuns}
              pagination={{ pageSize: 10, hideOnSinglePage: true }}
              scroll={{ x: 1500 }}
            />
          )}
        </Space>
      </Card>

      <AgentAdapterRunResultDrawer
        open={Boolean(activeRun)}
        run={activeRun}
        onClose={() => setActiveRun(null)}
      />
    </>
  );
};

export default AgentAdapterRunsCard;
