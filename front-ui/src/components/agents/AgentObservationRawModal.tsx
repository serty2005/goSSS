import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Modal, Spin, Typography } from 'antd';
import { agentObservationsApi } from '@/api/agentObservations';

const { Text } = Typography;

type Props = {
  observationID?: number;
  open: boolean;
  onClose: () => void;
};

const AgentObservationRawModal: React.FC<Props> = ({ observationID, open, onClose }) => {
  const { data, isLoading } = useQuery({
    queryKey: ['agent-observation-details', observationID],
    queryFn: async () => {
      if (!observationID) return null;
      const response = await agentObservationsApi.getByID(observationID);
      return response.data;
    },
    enabled: open && Boolean(observationID),
    refetchOnWindowFocus: false,
  });

  const payload = data?.payload_json ? JSON.stringify(data.payload_json, null, 2) : '';

  return (
    <Modal
      title={observationID ? `Событие агента #${observationID}` : 'Событие агента'}
      open={open}
      onCancel={onClose}
      onOk={onClose}
      width={900}
      destroyOnClose
      okText="Закрыть"
      cancelButtonProps={{ style: { display: 'none' } }}
    >
      {isLoading ? (
        <div style={{ textAlign: 'center', padding: 24 }}>
          <Spin />
        </div>
      ) : (
        <div>
          <div style={{ marginBottom: 12 }}>
            <Text type="secondary">Источник: {data?.source || '-'}</Text>
          </div>
          <pre
            style={{
              margin: 0,
              maxHeight: 500,
              overflow: 'auto',
              padding: 12,
              borderRadius: 8,
              background: 'rgba(0,0,0,0.04)',
              fontSize: 12,
              lineHeight: 1.45,
            }}
          >
            {payload || '{}'}
          </pre>
        </div>
      )}
    </Modal>
  );
};

export default AgentObservationRawModal;
