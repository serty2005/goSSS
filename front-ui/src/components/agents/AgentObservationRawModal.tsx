import React, { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Descriptions, Empty, Modal, Spin, Tag, Typography } from 'antd';
import dayjs from 'dayjs';
import { agentObservationsApi } from '@/api/agentObservations';
import JsonDataViewer from '@/components/common/JsonDataViewer';

const { Text } = Typography;

export type AgentObservationRawSummary = {
  agentUUID?: string;
  serverURL?: string;
  currentTime?: string;
  vTime?: string;
  workstation?: string;
  fr?: string;
};

type Props = {
  observationID?: number;
  open: boolean;
  onClose: () => void;
  title?: string;
  lookupLoading?: boolean;
  lookupError?: string;
  emptyDescription?: string;
  summary?: AgentObservationRawSummary;
};

const formatDateTime = (value?: string) => {
  if (!value) {
    return '-';
  }
  const parsed = dayjs(value);
  if (!parsed.isValid()) {
    return value;
  }
  return parsed.format('DD.MM.YYYY HH:mm:ss');
};

const getObservationStatusColor = (status?: string) => {
  const normalized = String(status || '').trim().toLowerCase();
  if (!normalized) {
    return 'default';
  }
  if (['success', 'ok', 'done', 'processed', 'completed'].includes(normalized)) {
    return 'success';
  }
  if (['warning', 'partial', 'stale'].includes(normalized)) {
    return 'warning';
  }
  if (['error', 'failed', 'failure', 'invalid'].includes(normalized)) {
    return 'error';
  }
  if (['new', 'pending', 'processing', 'running'].includes(normalized)) {
    return 'processing';
  }
  return 'default';
};

const AgentObservationRawModal: React.FC<Props> = ({
  observationID,
  open,
  onClose,
  title,
  lookupLoading = false,
  lookupError,
  emptyDescription,
  summary,
}) => {
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['agent-observation-details', observationID],
    queryFn: async () => {
      if (!observationID) {
        return null;
      }
      const response = await agentObservationsApi.getByID(observationID);
      return response.data;
    },
    enabled: open && Boolean(observationID) && !lookupLoading && !lookupError,
    refetchOnWindowFocus: false,
  });

  const payload = useMemo(() => {
    if (!data?.payload_json) {
      return '{}';
    }
    return JSON.stringify(data.payload_json, null, 2);
  }, [data?.payload_json]);

  const detailsError = error instanceof Error ? error.message : 'Не удалось загрузить сырой payload';
  const hasSummary = Boolean(
    summary?.agentUUID
      || summary?.serverURL
      || summary?.currentTime
      || summary?.vTime
      || summary?.workstation
      || summary?.fr,
  );

  return (
    <Modal
      title={title || (observationID ? `Данные агента #${observationID}` : 'Данные агента')}
      open={open}
      onCancel={onClose}
      onOk={onClose}
      width={960}
      destroyOnClose
      okText="Закрыть"
      cancelButtonProps={{ style: { display: 'none' } }}
    >
      {lookupLoading ? (
        <div style={{ textAlign: 'center', padding: 24 }}>
          <Spin />
          <div style={{ marginTop: 12 }}>
            <Text type="secondary">Ищем последнее наблюдение агента...</Text>
          </div>
        </div>
      ) : lookupError ? (
        <Alert
          type="error"
          showIcon
          message="Не удалось получить последнее наблюдение"
          description={lookupError}
        />
      ) : !observationID ? (
        <Empty description={emptyDescription || 'Для агента пока нет наблюдений'} />
      ) : isLoading ? (
        <div style={{ textAlign: 'center', padding: 24 }}>
          <Spin />
        </div>
      ) : isError ? (
        <Alert
          type="error"
          showIcon
          message="Не удалось загрузить данные наблюдения"
          description={detailsError}
        />
      ) : (
        <div style={{ display: 'grid', gap: 16 }}>
          <Descriptions bordered size="small" column={2}>
            <Descriptions.Item label="ID наблюдения">
              #{data?.id || observationID}
            </Descriptions.Item>
            <Descriptions.Item label="Статус">
              <Tag color={getObservationStatusColor(data?.status)} style={{ marginInlineEnd: 0 }}>
                {data?.status || 'Не указан'}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Время наблюдения">
              {formatDateTime(data?.observed_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Источник">
              {data?.source || '-'}
            </Descriptions.Item>
          </Descriptions>

          {hasSummary ? (
            <Descriptions bordered size="small" column={2}>
              <Descriptions.Item label="Агент">
                {summary?.agentUUID || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Рабочая станция">
                {summary?.workstation || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="ФР">
                {summary?.fr || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="URL сервера">
                {summary?.serverURL || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="current_time">
                {summary?.currentTime || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="v_time">
                {summary?.vTime || '-'}
              </Descriptions.Item>
            </Descriptions>
          ) : null}

          <JsonDataViewer title="Payload JSON" value={payload} defaultExpanded />
        </div>
      )}
    </Modal>
  );
};

export default AgentObservationRawModal;
