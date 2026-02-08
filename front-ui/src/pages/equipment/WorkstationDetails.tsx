import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, Modal, Form, Input, message } from 'antd';
import { ArrowLeftOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { UpdateWorkstationPayload } from '@/types/api';

const { Title, Text, Paragraph } = Typography;

const WorkstationDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [form] = Form.useForm();

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
      setIsEditModalOpen(false);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!wsRes?.data) return <div>Рабочая станция не найдена</div>;

  const ws = wsRes.data;

  const handleEdit = () => {
    form.setFieldsValue({
      device_name: ws.DeviceName,
      anydesk: ws.Anydesk,
      teamviewer: ws.Teamviewer,
      description: ws.Description,
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
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={handleBack} />
          <Space>
             <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('Workstation')}</div>
             <div>
               <Title level={4} style={{ margin: 0 }}>{ws.DeviceName || 'Workstation'}</Title>
               <Text type="secondary">{ws.ID}</Text>
             </div>
          </Space>
          <Badge status={getStatusColor(ws.HealthStatus)} text={ws.HealthStatus} />
        </Space>
        
        <Space>
          <Button type="primary" icon={<EditOutlined />} onClick={handleEdit}>Редактировать</Button>
          <Button danger icon={<DeleteOutlined />}>Удалить</Button>
        </Space>
      </div>

      <Card title="Детали рабочей станции" className="glass-panel" size="small">
        <Descriptions bordered column={1}>
          <Descriptions.Item label="Описание">
             {ws.Description || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="AnyDesk">
             {ws.Anydesk ? <Paragraph copyable>{ws.Anydesk}</Paragraph> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="TeamViewer">
             {ws.Teamviewer ? <Paragraph copyable>{ws.Teamviewer}</Paragraph> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="LiteManager">
             {ws.Litemanager ? <Paragraph copyable>{ws.Litemanager}</Paragraph> : '-'}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Modal
        title="Редактирование РС"
        open={isEditModalOpen}
        onCancel={() => setIsEditModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={updateMutation.isPending}
      >
        <Form form={form} layout="vertical" onFinish={(values) => updateMutation.mutate(values)}>
          <Form.Item name="device_name" label="Имя устройства">
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

export default WorkstationDetails;
