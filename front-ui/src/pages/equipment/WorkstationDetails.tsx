import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon } from '@/utils/mappers';
import { UpdateWorkstationPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';
import dayjs from 'dayjs';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';

const { Title, Text } = Typography;

const WorkstationDetails: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

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
  const agentUpdate = getAgentUpdateMeta(ws);

  const saveField = (field: keyof UpdateWorkstationPayload, value: string) => {
    if (!canEdit) {
      return;
    }
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
            <div style={{ fontSize: 24, color: token.colorPrimary }}>{getEntityIcon('Workstation')}</div>
            <div>
              <Title level={4} style={{ margin: 0 }}>{ws.device_name || 'Рабочая станция'}</Title>
              <Text type="secondary">{ws.id}</Text>
            </div>
          </Space>
          {agentUpdate ? (
            <Badge
              color="#1677ff"
              text={`Агент ${agentUpdate.updater}${agentUpdate.updatedAt ? ` • ${dayjs(agentUpdate.updatedAt).format('DD.MM.YYYY HH:mm')}` : ''}`}
            />
          ) : null}
        </Space>

        {canEdit && <Button danger icon={<DeleteOutlined />}>Удалить</Button>}
      </div>

      <Card title="Детали рабочей станции" className="glass-panel" size="small">
        <Descriptions bordered column={1} className="compact-descriptions">
          <Descriptions.Item label="Название устройства">
            <InlineFieldEditor value={ws.device_name} editable={canEdit} onSave={(v) => saveField('device_name', v)} saving={updateMutation.isPending && activeField === 'device_name'} />
          </Descriptions.Item>
          <Descriptions.Item label="Описание">
            <InlineFieldEditor value={ws.description} editable={canEdit} multiline onSave={(v) => saveField('description', v)} saving={updateMutation.isPending && activeField === 'description'} />
          </Descriptions.Item>
          <Descriptions.Item label="AnyDesk">
            <InlineFieldEditor value={ws.anydesk} editable={canEdit} onSave={(v) => saveField('anydesk', v)} saving={updateMutation.isPending && activeField === 'anydesk'} />
          </Descriptions.Item>
          <Descriptions.Item label="TeamViewer">
            <InlineFieldEditor value={ws.teamviewer} editable={canEdit} onSave={(v) => saveField('teamviewer', v)} saving={updateMutation.isPending && activeField === 'teamviewer'} />
          </Descriptions.Item>
          <Descriptions.Item label="LiteManager">
            <InlineFieldEditor value={ws.litemanager} editable={canEdit} onSave={(v) => saveField('litemanager', v)} saving={updateMutation.isPending && activeField === 'litemanager'} />
          </Descriptions.Item>
        </Descriptions>
      </Card>
    </div>
  );
};

export default WorkstationDetails;

