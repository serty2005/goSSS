import React, { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Tag, Space, Typography, Spin, Badge, Modal, Form, Input, message } from 'antd';
import { ArrowLeftOutlined, EditOutlined, SyncOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatDate } from '@/utils/formatters';
import { UpdateServerDTO } from '@/types/api';

const { Title, Text, Paragraph } = Typography;

const ServerDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [form] = Form.useForm();

  const { data: serverRes, isLoading } = useQuery({
    queryKey: ['server', id],
    queryFn: () => equipmentApi.getServer(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateServerDTO) => equipmentApi.updateServer(id!, values),
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
    form.setFieldsValue({
      device_name: server.device_name,
      ip: server.ip,
      anydesk: server.anydesk,
      teamviewer: server.teamviewer,
      description: server.description,
    });
    setIsEditModalOpen(true);
  };

  return (
    <div>
      {/* Header */}
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)} />
          <Space>
             <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('Server')}</div>
             <div>
               <Title level={4} style={{ margin: 0 }}>{server.device_name || server.server_name || 'Server'}</Title>
               <Text type="secondary">{server.uuid}</Text>
             </div>
          </Space>
          <Tag color={getStatusColor(server.operational_status) === 'success' ? 'green' : 'red'}>
            {server.operational_status?.toUpperCase()}
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
                 <Paragraph copyable={{ text: server.ip }} style={{ margin: 0 }}>{server.ip || '-'}</Paragraph>
              </Descriptions.Item>
              <Descriptions.Item label="Health Status">
                 <Badge status={getStatusColor(server.health_status)} text={server.health_status} />
              </Descriptions.Item>
              <Descriptions.Item label="Unique ID">{server.unique_id || '-'}</Descriptions.Item>
              <Descriptions.Item label="CRM ID">{server.crm_id || '-'}</Descriptions.Item>
              <Descriptions.Item label="Описание" span={2}>{server.description || '-'}</Descriptions.Item>
            </Descriptions>
          </Card>

          {/* Software Info */}
          <Card title="Программное обеспечение" className="glass-panel" size="small">
             <Descriptions bordered column={2}>
               <Descriptions.Item label="Версия сервера">{server.server_version || '-'}</Descriptions.Item>
               <Descriptions.Item label="Редакция">{server.server_edition || '-'}</Descriptions.Item>
               <Descriptions.Item label="Посл. опрос">{formatDate(server.last_polled_at)}</Descriptions.Item>
             </Descriptions>
          </Card>
        </Space>

        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
           {/* Access Info */}
           <Card title="Удаленный доступ" className="glass-panel" size="small">
              <Descriptions column={1} layout="vertical">
                 <Descriptions.Item label="AnyDesk">
                    {server.anydesk ? <Paragraph copyable>{server.anydesk}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 <Descriptions.Item label="TeamViewer">
                    {server.teamviewer ? <Paragraph copyable>{server.teamviewer}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 <Descriptions.Item label="LiteManager">
                    {server.litemanager ? <Paragraph copyable>{server.litemanager}</Paragraph> : <Text type="secondary">-</Text>}
                 </Descriptions.Item>
                 {server.partners_link && (
                   <Descriptions.Item label="Кабинет">
                      <Button type="link" href={server.partners_link} target="_blank" style={{ padding: 0 }}>
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