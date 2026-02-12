import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Tag, Space, Typography, Spin, Badge, message } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined, SyncOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatDate } from '@/utils/formatters';
import { UpdateServerPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';

const { Title, Text } = Typography;

const ServerDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

  const { data: serverRes, isLoading } = useQuery({
    queryKey: ['server', id],
    queryFn: () => equipmentApi.getServer(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateServerPayload) => equipmentApi.updateServer(id!, values),
    onSuccess: () => {
      message.success('Данные сервера обновлены');
      queryClient.invalidateQueries({ queryKey: ['server', id] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(id!),
    onSuccess: () => message.success('Запрос на опрос отправлен'),
    onError: () => message.error('Не удалось отправить запрос на опрос'),
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!serverRes?.data) return <div>Сервер не найден</div>;

  const server = serverRes.data;

  const saveField = (field: keyof UpdateServerPayload, value: string) => {
    if (!canEdit) {
      return;
    }
    setActiveField(field);
    updateMutation.mutate({ [field]: value } as UpdateServerPayload);
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
            <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('Server')}</div>
            <div>
              <Title level={4} style={{ margin: 0 }}>{server.device_name || server.server_name || 'Сервер'}</Title>
              <Text type="secondary">{server.id}</Text>
            </div>
          </Space>
          <Tag color={getStatusColor(server.status) === 'success' ? 'green' : 'red'}>{(server.status || 'unknown').toUpperCase()}</Tag>
        </Space>

        {canEdit && (
          <Space>
            <Button icon={<SyncOutlined spin={pollMutation.isPending} />} onClick={() => pollMutation.mutate()}>
              Опросить
            </Button>
            <Button danger icon={<DeleteOutlined />}>Удалить</Button>
          </Space>
        )}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 24 }}>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Card title="Основная информация" className="glass-panel" size="small">
            <Descriptions bordered column={2} className="compact-descriptions">
              <Descriptions.Item label="Название устройства">
                <InlineFieldEditor value={server.device_name} editable={canEdit} onSave={(v) => saveField('device_name', v)} saving={updateMutation.isPending && activeField === 'device_name'} />
              </Descriptions.Item>
              <Descriptions.Item label="Имя сервера">
                <InlineFieldEditor value={server.server_name} editable={canEdit} onSave={(v) => saveField('server_name', v)} saving={updateMutation.isPending && activeField === 'server_name'} />
              </Descriptions.Item>
              <Descriptions.Item label="IP адрес">
                <InlineFieldEditor value={server.ip} editable={canEdit} onSave={(v) => saveField('ip', v)} saving={updateMutation.isPending && activeField === 'ip'} />
              </Descriptions.Item>
              <Descriptions.Item label="Health Status">
                <Badge status={getStatusColor(server.health_status)} text={server.health_status} />
              </Descriptions.Item>
              <Descriptions.Item label="Unique ID">
                <InlineFieldEditor value={server.unique_id} editable={canEdit} onSave={(v) => saveField('unique_id', v)} saving={updateMutation.isPending && activeField === 'unique_id'} />
              </Descriptions.Item>
              <Descriptions.Item label="CRM ID">
                <InlineFieldEditor value={server.crm_id} editable={canEdit} onSave={(v) => saveField('crm_id', v)} saving={updateMutation.isPending && activeField === 'crm_id'} />
              </Descriptions.Item>
              <Descriptions.Item label="Описание" span={2}>
                <InlineFieldEditor value={server.description} editable={canEdit} multiline onSave={(v) => saveField('description', v)} saving={updateMutation.isPending && activeField === 'description'} />
              </Descriptions.Item>
            </Descriptions>
          </Card>

          <Card title="Программное обеспечение" className="glass-panel" size="small">
            <Descriptions bordered column={2} className="compact-descriptions">
              <Descriptions.Item label="Версия сервера">
                <InlineFieldEditor value={server.server_version} editable={canEdit} onSave={(v) => saveField('server_version', v)} saving={updateMutation.isPending && activeField === 'server_version'} />
              </Descriptions.Item>
              <Descriptions.Item label="Редакция">
                <InlineFieldEditor value={server.server_edition} editable={canEdit} onSave={(v) => saveField('server_edition', v)} saving={updateMutation.isPending && activeField === 'server_edition'} />
              </Descriptions.Item>
              <Descriptions.Item label="Посл. опрос">{formatDate(server.last_polled_at)}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Space>

        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Card title="Удаленный доступ" className="glass-panel" size="small">
            <Descriptions column={1} layout="vertical" className="compact-descriptions">
              <Descriptions.Item label="AnyDesk">
                <InlineFieldEditor value={server.anydesk} editable={canEdit} onSave={(v) => saveField('anydesk', v)} saving={updateMutation.isPending && activeField === 'anydesk'} />
              </Descriptions.Item>
              <Descriptions.Item label="TeamViewer">
                <InlineFieldEditor value={server.teamviewer} editable={canEdit} onSave={(v) => saveField('teamviewer', v)} saving={updateMutation.isPending && activeField === 'teamviewer'} />
              </Descriptions.Item>
              <Descriptions.Item label="rdp">
                <InlineFieldEditor value={server.rdp} editable={canEdit} onSave={(v) => saveField('rdp', v)} saving={updateMutation.isPending && activeField === 'rdp'} />
              </Descriptions.Item>
              <Descriptions.Item label="LiteManager">
                <InlineFieldEditor value={server.litemanager} editable={canEdit} onSave={(v) => saveField('litemanager', v)} saving={updateMutation.isPending && activeField === 'litemanager'} />
              </Descriptions.Item>
              <Descriptions.Item label="Кабинет дилера">
                {server.partners_link ? (
                  <a href={server.partners_link} target="_blank" rel="noopener noreferrer">Partners Portal</a>
                ) : '-'}
              </Descriptions.Item>
            </Descriptions>
          </Card>
        </Space>
      </div>
    </div>
  );
};

export default ServerDetails;

