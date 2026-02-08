import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, Modal, Form, Input, message, Table } from 'antd';
import { ArrowLeftOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { UpdateFiscalPayload } from '@/types/api';
import dayjs from 'dayjs';

const { Title, Text } = Typography;

const FiscalDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [isEditModalOpen, setIsEditModalOpen] = useState(false);
  const [form] = Form.useForm();

  const { data: fiscalRes, isLoading } = useQuery({
    queryKey: ['fiscal', id],
    queryFn: () => equipmentApi.getFiscal(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateFiscalPayload) => equipmentApi.updateFiscal(id!, values),
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

  const getFnDateColor = (dateStr?: string) => {
     if (!dateStr) return undefined;
     const diff = dayjs(dateStr).diff(dayjs(), 'day');
     if (diff < 0) return 'red';
     if (diff < 30) return 'orange';
     return 'green';
  };

  const handleEdit = () => {
    form.setFieldsValue({
      description: fiscal.Description,
    });
    setIsEditModalOpen(true);
  };

  // Преобразуем лицензии из объекта в массив для таблицы
  const licensesData = fiscal.Licenses 
    ? Object.entries(fiscal.Licenses).map(([id, data]) => ({ id, ...data }))
    : [];

  const licenseColumns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: 'Название', dataIndex: 'name' },
    { title: 'До', dataIndex: 'dateUntil', render: (d: string) => d ? d.split(' ')[0] : '-' },
  ];

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
             <div style={{ fontSize: 24, color: '#1890ff' }}>{getEntityIcon('FiscalRegister')}</div>
             <div>
               <Title level={4} style={{ margin: 0 }}>{fiscal.ModelKKT || 'ККТ'}</Title>
               <Text type="secondary">{fiscal.FRSerialNumber}</Text>
             </div>
          </Space>
          <Badge status={getStatusColor(fiscal.HealthStatus)} text={fiscal.HealthStatus} />
        </Space>
        
        <Space>
          <Button type="primary" icon={<EditOutlined />} onClick={handleEdit}>Редактировать</Button>
          <Button danger icon={<DeleteOutlined />}>Удалить</Button>
        </Space>
      </div>

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          
          <Card title="Информация о ККТ" className="glass-panel" size="small">
            <Descriptions bordered column={2}>
              <Descriptions.Item label="РНМ">
                  <Text code>{formatRnm(fiscal.RNKKT)}</Text>
              </Descriptions.Item>
              <Descriptions.Item label="Заводской номер">{fiscal.FRSerialNumber}</Descriptions.Item>
              <Descriptions.Item label="Модель">{fiscal.ModelKKT}</Descriptions.Item>
              <Descriptions.Item label="Описание">{fiscal.Description || '-'}</Descriptions.Item>
            </Descriptions>
          </Card>

          <Card title="Фискальный Накопитель" className="glass-panel" size="small">
             <Descriptions bordered column={2}>
               <Descriptions.Item label="Номер ФН">{fiscal.FNNumber || '-'}</Descriptions.Item>
               <Descriptions.Item label="Дата регистрации">
                  {fiscal.kkt_reg_date ? dayjs(fiscal.kkt_reg_date).format('DD.MM.YYYY') : '-'}
               </Descriptions.Item>
               <Descriptions.Item label="Дата окончания">
                  <Text strong style={{ color: getFnDateColor(fiscal.fn_expire_date) }}>
                     {fiscal.fn_expire_date ? dayjs(fiscal.fn_expire_date).format('DD.MM.YYYY') : '-'}
                  </Text>
               </Descriptions.Item>
             </Descriptions>
          </Card>

          <Card title="Прошивки и ПО" className="glass-panel" size="small">
             <Descriptions bordered column={3}>
               <Descriptions.Item label="Прошивка ФР">{fiscal.FRFirmware || '-'}</Descriptions.Item>
               <Descriptions.Item label="Загрузчик">{fiscal.FRDownloader || '-'}</Descriptions.Item>
               <Descriptions.Item label="Драйвер">{fiscal.DriverVersion || '-'}</Descriptions.Item>
             </Descriptions>
          </Card>

           <Card title="Юридическое лицо" className="glass-panel" size="small">
             <Descriptions bordered column={1}>
               <Descriptions.Item label="Организация">{fiscal.LegalName || '-'}</Descriptions.Item>
               <Descriptions.Item label="ИНН">{fiscal.INN || '-'}</Descriptions.Item>
               <Descriptions.Item label="Адрес установки">{fiscal.address || '-'}</Descriptions.Item>
             </Descriptions>
          </Card>
          
          {licensesData.length > 0 && (
            <Card title="Лицензии ККТ" className="glass-panel" size="small">
              <Table 
                 dataSource={licensesData} 
                 columns={licenseColumns} 
                 rowKey="id" 
                 pagination={false} 
                 size="small"
              />
            </Card>
          )}
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
