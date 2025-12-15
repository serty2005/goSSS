import React, { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, Modal, Form, Input, message } from 'antd';
import { ArrowLeftOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { UpdateFiscalDTO } from '@/types/api';
import dayjs from 'dayjs';

const { Title, Text } = Typography;

const FiscalDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [form] = Form.useForm();

  const { data: fiscalRes, isLoading } = useQuery({
    queryKey: ['fiscal', id],
    queryFn: () => equipmentApi.getFiscal(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateFiscalDTO) => equipmentApi.updateFiscal(id!, values),
    onSuccess: () => {
      message.success('Данные обновлены');
      queryClient.invalidateQueries({ queryKey: ['fiscal', id] });
      setIsEditModalOpen(false);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!fiscalRes?.data) return <div>ФР не найден</div>;

  const fiscal = fiscalRes.data;

  // Логика цвета даты окончания ФН
  const getFnDateColor = (dateStr?: string) => {
     if (!dateStr) return undefined;
     const diff = dayjs(dateStr).diff(dayjs(), 'day');
     if (diff < 0) return 'red';
     if (diff < 30) return 'orange';
     return 'green';
  };

  const handleEdit = () => {
    form.setFieldsValue({
      description: fiscal.description,
    });
    setIsEditModalOpen(true);
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate(-1)} />
          <Space>
             <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('FiscalRegister')}</div>
             <div>
               <Title level={4} style={{ margin: 0 }}>{fiscal.model_kkt || 'ККТ'}</Title>
               <Text type="secondary">{fiscal.serial_number}</Text>
             </div>
          </Space>
          <Badge status={getStatusColor(fiscal.health_status)} text={fiscal.health_status} />
        </Space>
        
        <Space>
          <Button type="primary" icon={<EditOutlined />} onClick={handleEdit}>Редактировать</Button>
          <Button danger icon={<DeleteOutlined />}>Удалить</Button>
        </Space>
      </div>

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          
          {/* Main Info */}
          <Card title="Информация о ККТ" className="glass-panel" size="small">
            <Descriptions bordered column={2}>
              <Descriptions.Item label="РНМ">
                  <Text code>{formatRnm(fiscal.rn_kkt)}</Text>
              </Descriptions.Item>
              <Descriptions.Item label="Заводской номер">{fiscal.serial_number}</Descriptions.Item>
              <Descriptions.Item label="Модель">{fiscal.model_kkt}</Descriptions.Item>
              <Descriptions.Item label="Описание">{fiscal.description || '-'}</Descriptions.Item>
            </Descriptions>
          </Card>

          {/* FN Info */}
          <Card title="Фискальный Накопитель" className="glass-panel" size="small">
             <Descriptions bordered column={2}>
               <Descriptions.Item label="Номер ФН">{fiscal.fn_number || '-'}</Descriptions.Item>
               <Descriptions.Item label="Дата регистрации">
                  {fiscal.fn_registration_date ? dayjs(fiscal.fn_registration_date).format('DD.MM.YYYY') : '-'}
               </Descriptions.Item>
               <Descriptions.Item label="Дата окончания">
                  <Text strong style={{ color: getFnDateColor(fiscal.fn_expire_date) }}>
                     {fiscal.fn_expire_date ? dayjs(fiscal.fn_expire_date).format('DD.MM.YYYY') : '-'}
                  </Text>
               </Descriptions.Item>
             </Descriptions>
          </Card>

          {/* Firmware Info */}
          <Card title="Прошивки и ПО" className="glass-panel" size="small">
             <Descriptions bordered column={3}>
               <Descriptions.Item label="Прошивка ФР">{fiscal.fr_firmware || '-'}</Descriptions.Item>
               <Descriptions.Item label="Загрузчик">{fiscal.fr_downloader || '-'}</Descriptions.Item>
               <Descriptions.Item label="Драйвер">{fiscal.driver_version || '-'}</Descriptions.Item>
             </Descriptions>
          </Card>

           {/* Legal Info */}
           <Card title="Юридическое лицо" className="glass-panel" size="small">
             <Descriptions bordered column={1}>
               <Descriptions.Item label="Организация">{fiscal.organization_name || '-'}</Descriptions.Item>
               <Descriptions.Item label="ИНН">{fiscal.inn || '-'}</Descriptions.Item>
               <Descriptions.Item label="Адрес установки">{fiscal.address || '-'}</Descriptions.Item>
             </Descriptions>
          </Card>
      </Space>

      <Modal
        title="Редактирование ФР"
        open={isEditModalOpen}
        onCancel={() => setIsEditModalOpen(false)}
        onOk={() => form.submit()}
        confirmLoading={updateMutation.isPending}
      >
        <Form form={form} layout="vertical" onFinish={(values) => updateMutation.mutate(values)}>
          <Form.Item name="description" label="Описание / Заметки">
            <Input.TextArea rows={4} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default FiscalDetails;