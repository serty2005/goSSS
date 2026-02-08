import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Tag, Space, Typography, Spin, Badge, Modal, Form, Input, message } from 'antd';
import { ArrowLeftOutlined, EditOutlined, SyncOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatDate } from '@/utils/formatters';
import { UpdateServerPayload } from '@/types/api';

const { Title, Text, Paragraph } = Typography;

const ServerDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [form] = Form.useForm();

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
      setIsEditModalOpen(false);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(id!),
    onSuccess: () => {
      message.success('Запрос на опрос отправлен');
    }
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!serverRes?.data) return <div>Сервер не найден</div>;

  const server = serverRes.data;

  const handleEdit = () => {
    // Маппинг для формы (PascalCase -> snake_case payload)
    form.setFieldsValue({
      device_name: server.DeviceName,
      ip: server.IP,
      anydesk: server.Anydesk,
      teamviewer: server.Teamviewer,
      description: server.Description,
    });
    setIsEditModalOpen(true);
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
      {/* Header */}
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={handleBack} />
          <Space>
             <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('Server')}</div>
             <div>
               <Title level={4} style={{ margin: 0 }}>{server.DeviceName || server.ServerName || 'Server'}</Title>
               <Text type="secondary">{server.ID}</Text>
             </div>
          </Space>
          <Tag color={getStatusColor(server.Status) === 'success' ? 'green' : 'red'}>
            {server.Status?.toUpperCase()}
          </Tag>
        </Space>
        
        <Space>
          <Button 
            icon={<SyncOutlined spin={pollMutation.isPending} />} 
            onClick={() => pollMutation.mutate()}
          >
            Опросить
          </Button>
          <Button type="primary" icon={<EditOutlined />} onClick={handleEdit}>Редактировать</Button>
          <Button danger icon={<DeleteOutlined />}>Удалить</Button>
        </Space>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr', gap: 24 }}>
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          
          {/* Main Info */}
          <Card title="Основная информация" className="glass-panel" size="small">
            <Descriptions bordered column={2}>
              <Descriptions.Item label="IP Адрес">
                 <Paragraph copyable={{ text: server.IP }} style={{ margin: 0 }}>{server.IP || '-'}</Paragraph>
              </Descriptions.Item>
              <Descriptions.Item label="Health Status">
                 <Badge status={getStatusColor(server.HealthStatus)} text={server.HealthStatus} />
              </Descriptions.Item>
              <Descriptions.Item label="Unique ID">{server.UniqueID || '-'}</Descriptions.Item>
              <Descriptions.Item label="CRM ID">{server.CRMid || '-'}</Descriptions.Item>
              <Descriptions.Item label="Описание" span={2}>{server.Description || '-'}</Descriptions.Item>
            </Descriptions>
          </Card>

          {/* Software Info */}
          <Card title="Программное обеспечение" className="glass-panel" size="small">
             <Descriptions bordered column={2}>
               <Descriptions.Item label="Версия сервера">{server.ServerVersion || '-'}</Descriptions.Item>
               <Descriptions.Item label="Редакция">{server.ServerEdition || '-'}</Descriptions.Item>
               <Descriptions.Item label="Посл. опрос">{formatDate(server.LastPolledAt)}</Descriptions.Item>
             </Descriptions>
          </Card>
        </Space>

        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
           {/* Access Info */}
           <Card title="Удаленный доступ" className="glass-panel" size="small">
              <Descriptions column={1} layout="vertical">
                 <Descriptions.Item label="AnyDesk">
                    {server.Anydesk ? <Paragraph copyable>{server.Anydesk}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 <Descriptions.Item label="TeamViewer">
                    {server.Teamviewer ? <Paragraph copyable>{server.Teamviewer}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 <Descriptions.Item label="RDP">
                    {server.RDP ? <Paragraph copyable>{server.RDP}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 {server.CabinetLink && (
                   <Descriptions.Item label="Кабинет">
                      <Button type="link" href={server.CabinetLink} target="_blank" style={{ padding: 0 }}>
                        Перейти в кабинет дилера
                      </Button>
                   </Descriptions.Item>
                 )}
              </Descriptions>
           </Card>
        </Space>
      </div>

      {/* Edit Modal */}
      <Modal
        title="Редактирование сервера"
        open={isEditModalOpen}
        onCancel={() => setIsEditModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={updateMutation.isPending}
      >
        <Form form={form} layout="vertical" onFinish={(values) => updateMutation.mutate(values)}>
          <Form.Item name="device_name" label="Имя устройства">
            <Input />
          </Form.Item>
          <Form.Item name="ip" label="IP адрес">
            <Input />
          </Form.Item>
          <Form.Item name="anydesk" label="AnyDesk">
            <Input />
          </Form.Item>
          <Form.Item name="teamviewer" label="TeamViewer">
            <Input />
          </Form.Item>
          <Form.Item name="description" label="Описание">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default ServerDetails;
