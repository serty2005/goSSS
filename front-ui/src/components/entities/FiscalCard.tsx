import React from 'react';
import { Card, Space, Typography, Tag, Tooltip } from 'antd';
import { useNavigate } from 'react-router-dom';
import dayjs from 'dayjs';
import { FiscalEntity } from '@/types/api';
import { getEntityIcon } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';

interface Props {
  data: FiscalEntity;
}

const { Text, Paragraph } = Typography;

const FiscalCard: React.FC<Props> = ({ data }) => {
  const navigate = useNavigate();
  const agentUpdate = getAgentUpdateMeta(data);
  const organizationName = data.legal_name || data.organization_name;
  const fnExecution = typeof data.fn_execution === 'string'
    ? data.fn_execution
    : (typeof data.fnExecution === 'string' ? data.fnExecution : undefined);

  const renderFnInfo = (dateStr?: string) => {
    if (!dateStr) return <Tag>Нет ФН</Tag>;
    const expireDate = dayjs(dateStr);
    const daysLeft = expireDate.diff(dayjs(), 'day');

    let color = 'green';
    let label = 'ФН OK';

    if (daysLeft < 0) {
      color = 'red';
      label = 'ФН истёк';
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

  const handleCardClick = () => {
    navigate(`/fiscals/${data.uuid}`);
  };

  return (
    <Card
      size="small"
      className="glass-panel"
      hoverable
      style={{ minWidth: 300, minHeight: 200 }}
      onClick={handleCardClick}
      title={(
        <Space>
          {getEntityIcon('FiscalRegister')}
          <Text strong>{data.model_kkt || 'ККТ'}</Text>
        </Space>
      )}
      extra={agentUpdate ? (
        <Tooltip
          title={agentUpdate.updatedAt
            ? `Агент ${agentUpdate.updater}, ${dayjs(agentUpdate.updatedAt).format('DD.MM.YYYY HH:mm:ss')}`
            : `Агент ${agentUpdate.updater}`}
        >
          <Tag color="blue" style={{ fontSize: 12, lineHeight: '22px', paddingInline: 10, marginRight: 0 }}>
            Агент
          </Tag>
        </Tooltip>
      ) : null}
    >
      <div style={{ marginBottom: 12 }}>
        {organizationName && <Text strong style={{ display: 'block' }}>{organizationName}</Text>}
        {data.inn && <Text type="secondary" style={{ fontSize: 12, marginRight: 8 }}>ИНН: {data.inn}</Text>}
        {data.address && <Text type="secondary" style={{ fontSize: 12 }}>| {String(data.address)}</Text>}
      </div>

      <Space direction="vertical" size={4} style={{ width: '100%' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <Text type="secondary">РНМ:</Text>
          <Paragraph copyable={{ text: data.rn_kkt }} style={{ margin: 0, fontFamily: 'monospace' }}>
            {formatRnm(data.rn_kkt)}
          </Paragraph>
        </div>

        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <Text type="secondary">SN:</Text>
          <Paragraph copyable={{ text: data.serial_number }} style={{ margin: 0, fontSize: 12 }}>
            {data.serial_number}
          </Paragraph>
        </div>

        <div style={{ marginTop: 4 }}>
          {renderFnInfo(data.fn_expire_date)}
        </div>

        {(data.driver_version || data.fr_firmware || data.fr_downloader) && (
          <div style={{ marginTop: 4, fontSize: 11, color: '#8c8c8c', borderTop: '1px solid rgba(0,0,0,0.06)', paddingTop: 4 }}>
            FW: {data.fr_firmware || '-'} {data.fr_downloader ? `| FWDownloader: ${data.fr_downloader}` : ''}{data.driver_version ? `| Drv: ${data.driver_version}` : ''}
          </div>
        )}
      </Space>
    </Card>
  );
};

export default FiscalCard;
