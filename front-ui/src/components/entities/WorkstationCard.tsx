import React from 'react';
import { Card, Badge, Space, Typography, Tooltip } from 'antd';
import { useNavigate } from 'react-router-dom';
import { WorkstationEntity } from '@/types/api';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';

interface Props {
  data: WorkstationEntity;
}

const { Text, Paragraph } = Typography;

const WorkstationCard: React.FC<Props> = ({ data }) => {
  const navigate = useNavigate();

  const handleCardClick = () => {
    navigate(`/workstations/${data.uuid}`);
  };

  return (
    <Card 
      size="small" 
      className="glass-panel"
      hoverable
      onClick={handleCardClick}
      title={
        <Space>
          {getEntityIcon('Workstation')}
          <Text strong>{data.device_name || 'Workstation'}</Text>
        </Space>
      }
      extra={
        <Tooltip title={`Health: ${data.health_status}`}>
           <Badge status={getStatusColor(data.health_status)} text={data.health_status} />
        </Tooltip>
      }
    >
       <Space direction="vertical" size={2} style={{ width: '100%' }}>
          {/* description теперь типизирован как string | undefined */}
          {data.description && (
            <Paragraph 
              ellipsis={{ rows: 2, expandable: false }} 
              type="secondary" 
              style={{ fontSize: 12, marginBottom: 8 }}
            >
              {data.description}
            </Paragraph>
          )}

          {data.anydesk && (
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
               <Text>AnyDesk:</Text>
               <Paragraph copyable={{ text: data.anydesk }} style={{ margin: 0 }}>{data.anydesk}</Paragraph>
            </div>
          )}
          
          {data.teamviewer && (
             <div style={{ display: 'flex', justifyContent: 'space-between' }}>
               <Text>TV:</Text>
               <Paragraph copyable={{ text: data.teamviewer }} style={{ margin: 0 }}>{data.teamviewer}</Paragraph>
             </div>
          )}
          
           {data.litemanager && (
             <div style={{ display: 'flex', justifyContent: 'space-between' }}>
               <Text>LM:</Text>
               <Paragraph copyable={{ text: data.litemanager }} style={{ margin: 0 }}>{data.litemanager}</Paragraph>
             </div>
          )}

          {!data.anydesk && !data.teamviewer && !data.litemanager && !data.description && (
             <Text type="secondary" italic>Нет доп. информации</Text>
          )}
       </Space>
    </Card>
  );
};

export default WorkstationCard;