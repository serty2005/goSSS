import React from 'react';
import { Descriptions, Modal, Space, Tag, Typography } from 'antd';
import { AgentRegistrationAttemptDTO } from '@/types/api';
import JsonDataViewer from '@/components/common/JsonDataViewer';
import { formatDateTime, getRegistrationStatusMeta } from '@/components/agents/agentDiagnosticsUtils';

const { Text } = Typography;

type Props = {
  attempt?: AgentRegistrationAttemptDTO | null;
  open: boolean;
  onClose: () => void;
};

const AgentRegistrationAttemptModal: React.FC<Props> = ({ attempt, open, onClose }) => {
  const statusMeta = getRegistrationStatusMeta(attempt?.status);

  return (
    <Modal
      title={attempt?.id ? `Попытка регистрации #${attempt.id}` : 'Попытка регистрации'}
      open={open}
      onCancel={onClose}
      onOk={onClose}
      width={960}
      destroyOnClose
      okText="Закрыть"
      cancelButtonProps={{ style: { display: 'none' } }}
    >
      {!attempt ? (
        <Text type="secondary">Данные попытки регистрации не выбраны.</Text>
      ) : (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Descriptions bordered size="small" column={2}>
            <Descriptions.Item label="Время">
              {formatDateTime(attempt.created_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Статус">
              <Tag color={statusMeta.color} style={{ marginInlineEnd: 0 }}>
                {statusMeta.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Адрес источника">
              {attempt.remote_addr || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Fingerprint">
              {attempt.machine_fingerprint || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="UUID агента" span={2}>
              {attempt.agent_uuid || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="Ошибка" span={2}>
              {attempt.error_text || '-'}
            </Descriptions.Item>
          </Descriptions>

          <JsonDataViewer
            title="System info"
            value={attempt.system_info}
            emptyDescription="System info для этой попытки регистрации не сохранён"
          />

          <JsonDataViewer
            title="Payload регистрации"
            value={attempt.payload}
            emptyDescription="Payload для этой попытки регистрации не сохранён"
          />
        </Space>
      )}
    </Modal>
  );
};

export default AgentRegistrationAttemptModal;
