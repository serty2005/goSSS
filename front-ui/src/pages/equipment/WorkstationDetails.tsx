import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { UpdateWorkstationPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';

const { Title, Text } = Typography;

const WorkstationDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);

  const { data: wsRes, isLoading } = useQuery({
    queryKey: ['workstation', id],
    queryFn: () => equipmentApi.getWorkstation(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateWorkstationPayload) => equipmentApi.updateWorkstation(id!, values),
    onSuccess: () => {
      message.success('Данные обновлены');
      queryClient.invalidateQueries({ queryKey: ['workstation', id] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!wsRes?.data) return <div>Рабочая станция не найдена</div>;

  const ws = wsRes.data;

  const saveField = (field: keyof UpdateWorkstationPayload, value: string) => {
    setActiveField(field);
    updateMutation.mutate({ [field]: value } as UpdateWorkstationPayload);
  };

  const handleBack = () => {
    const backTo = (location.state as { backTo?: string } | null)?.backTo;
    if (backTo) {
      navigate(backTo);
      return;
    }
    navigate(-1);
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={handleBack} />
          <Space>
            <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('Workstation')}</div>
            <div>
              <Title level={4} style={{ margin: 0 }}>{ws.DeviceName || 'Рабочая станция'}</Title>
              <Text type="secondary">{ws.ID}</Text>
            </div>
          </Space>
          <Badge status={getStatusColor(ws.HealthStatus)} text={ws.HealthStatus} />
        </Space>

        <Button danger icon={<DeleteOutlined />}>Удалить</Button>
      </div>

      <Card title="Детали рабочей станции" className="glass-panel" size="small">
        <Descriptions bordered column={1} className="compact-descriptions">
          <Descriptions.Item label="Название устройства">
            <InlineFieldEditor
              value={ws.DeviceName}
              onSave={(value) => saveField('device_name', value)}
              saving={updateMutation.isPending && activeField === 'device_name'}
            />
          </Descriptions.Item>
          <Descriptions.Item label="Описание">
            <InlineFieldEditor
              value={ws.Description}
              multiline
              onSave={(value) => saveField('description', value)}
              saving={updateMutation.isPending && activeField === 'description'}
            />
          </Descriptions.Item>
          <Descriptions.Item label="AnyDesk">
            <InlineFieldEditor
              value={ws.Anydesk}
              onSave={(value) => saveField('anydesk', value)}
              saving={updateMutation.isPending && activeField === 'anydesk'}
            />
          </Descriptions.Item>
          <Descriptions.Item label="TeamViewer">
            <InlineFieldEditor
              value={ws.Teamviewer}
              onSave={(value) => saveField('teamviewer', value)}
              saving={updateMutation.isPending && activeField === 'teamviewer'}
            />
          </Descriptions.Item>
          <Descriptions.Item label="LiteManager">
            <InlineFieldEditor
              value={ws.Litemanager}
              onSave={(value) => saveField('litemanager', value)}
              saving={updateMutation.isPending && activeField === 'litemanager'}
            />
          </Descriptions.Item>
        </Descriptions>
      </Card>
    </div>
  );
};

export default WorkstationDetails;
