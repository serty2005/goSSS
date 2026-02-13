import React from 'react';
import { Card, Empty, Space, Typography } from 'antd';
import { CandidateFiscalStagingDTO } from '@/types/api';

interface StagedFiscalsProps {
  fiscals: CandidateFiscalStagingDTO[];
}

const { Text } = Typography;

export const StagedFiscals: React.FC<StagedFiscalsProps> = ({ fiscals }) => {
  return (
    <Card size="small" title={`ФР из наблюдений (${fiscals.length})`}>
      {fiscals.length === 0 ? (
        <Empty description="Нет staged ФР" />
      ) : (
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          {fiscals.map((fiscal) => (
            <Card key={fiscal.id} size="small" bodyStyle={{ padding: 10 }}>
              <Space direction="vertical" size={2}>
                <Text strong>{fiscal.serial_number || fiscal.serial_normalized || `ФР #${fiscal.id}`}</Text>
                <Text type="secondary">РН ККТ: {fiscal.rn_kkt || '-'}</Text>
                <Text type="secondary">Модель: {fiscal.model_name || '-'}</Text>
                <Text type="secondary">ИНН: {fiscal.inn || '-'}</Text>
                {fiscal.address ? <Text type="secondary">Адрес регистрации: {fiscal.address}</Text> : null}
              </Space>
            </Card>
          ))}
        </Space>
      )}
    </Card>
  );
};
