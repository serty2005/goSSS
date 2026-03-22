import React from 'react';
import { Descriptions, Drawer, Empty, Space, Tag, Typography } from 'antd';

import JsonDataViewer from '@/components/common/JsonDataViewer';
import { formatDateTime } from '@/components/agents/agentDiagnosticsUtils';
import { AgentAdapterRunDTO } from '@/types/api';

const { Text } = Typography;

type Props = {
  run?: AgentAdapterRunDTO | null;
  open: boolean;
  onClose: () => void;
};

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

const TextBlock: React.FC<{
  title: string;
  value?: string | null;
  emptyDescription: string;
}> = ({ title, value, emptyDescription }) => {
  const normalized = String(value || '').trim();

  if (!normalized) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyDescription} />;
  }

  return (
    <div style={{ display: 'grid', gap: 10 }}>
      <Text strong>{title}</Text>
      <pre
        style={{
          margin: 0,
          maxHeight: 260,
          overflow: 'auto',
          padding: 12,
          borderRadius: 8,
          background: 'rgba(0, 0, 0, 0.04)',
          fontSize: 12,
          lineHeight: 1.45,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {normalized}
      </pre>
    </div>
  );
};

const AgentAdapterRunResultDrawer: React.FC<Props> = ({ run, open, onClose }) => {
  const statusMeta = getRunStatusMeta(run?.status);

  return (
    <Drawer
      title={run?.id ? `Запуск адаптера #${run.id}` : 'Результат запуска адаптера'}
      placement="right"
      width={960}
      open={open}
      onClose={onClose}
      destroyOnClose
    >
      {!run ? (
        <Text type="secondary">Запуск адаптера не выбран.</Text>
      ) : (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Descriptions bordered size="small" column={2}>
            <Descriptions.Item label="ID команды">
              {run.id}
            </Descriptions.Item>
            <Descriptions.Item label="Адаптер">
              {run.adapter_id || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Тип команды">
              {run.type || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Статус">
              <Tag color={statusMeta.color} style={{ marginInlineEnd: 0 }}>
                {statusMeta.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Команда">
              {run.command || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Операция">
              {run.operation || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Создана">
              {formatDateTime(run.created_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Отправлена">
              {formatDateTime(run.sent_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Завершена">
              {formatDateTime(run.completed_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Длительность">
              {formatDuration(run.duration_ms)}
            </Descriptions.Item>
            <Descriptions.Item label="Exit code">
              {typeof run.exit_code === 'number' ? run.exit_code : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="UUID агента">
              {run.agent_uuid || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Краткая ошибка" span={2}>
              {run.error_text || <Text type="secondary">Нет</Text>}
            </Descriptions.Item>
          </Descriptions>

          <JsonDataViewer
            title="Payload команды"
            value={run.payload}
            emptyDescription="Payload команды для этого запуска не сохранён"
          />

          <JsonDataViewer
            title="Result payload"
            value={run.result_payload}
            emptyDescription="Result payload для этого запуска не сохранён"
          />

          <JsonDataViewer
            title="Structured result JSON"
            value={run.structured_result}
            emptyDescription="Структурированный JSON-результат отсутствует"
          />

          <TextBlock
            title="stdout"
            value={run.stdout}
            emptyDescription="stdout для этого запуска пуст"
          />

          <TextBlock
            title="stderr"
            value={run.stderr}
            emptyDescription="stderr для этого запуска пуст"
          />
        </Space>
      )}
    </Drawer>
  );
};

export default AgentAdapterRunResultDrawer;
