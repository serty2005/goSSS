import React, { useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message, Table } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { UpdateFiscalPayload } from '@/types/api';
import dayjs from 'dayjs';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';

const { Title, Text } = Typography;

const FiscalDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

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
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!fiscalRes?.data) return <div>Фискальный регистратор не найден</div>;

  const fiscal = fiscalRes.data;

  const saveField = (field: keyof UpdateFiscalPayload, value: string) => {
    if (!canEdit) {
      return;
    }
    setActiveField(field);
    updateMutation.mutate({ [field]: value } as UpdateFiscalPayload);
  };

  const getFnDateColor = (dateStr?: string) => {
    if (!dateStr) return undefined;
    const diff = dayjs(dateStr).diff(dayjs(), 'day');
    if (diff < 0) return 'red';
    if (diff < 30) return 'orange';
    return 'green';
  };

  const licensesData = fiscal.Licenses
    ? Object.entries(fiscal.Licenses).map(([licenseID, data]) => ({ licenseID, ...data }))
    : [];

  const licenseColumns = [
    { title: 'ID', dataIndex: 'licenseID', width: 60 },
    { title: 'Название', dataIndex: 'name' },
    { title: 'До', dataIndex: 'dateUntil', render: (value: string) => value ? value.split(' ')[0] : '-' },
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
              <Text type="secondary">{fiscal.FRSerialNumber || fiscal.ID}</Text>
            </div>
          </Space>
          <Badge status={getStatusColor(fiscal.HealthStatus)} text={fiscal.HealthStatus} />
        </Space>

        {canEdit && <Button danger icon={<DeleteOutlined />}>Удалить</Button>}
      </div>

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Card title="Информация о ККТ" className="glass-panel" size="small">
          <Descriptions bordered column={2} className="compact-descriptions">
            <Descriptions.Item label="РНМ">
              <InlineFieldEditor value={fiscal.RNKKT} editable={canEdit} onSave={(v) => saveField('rn_kkt', v)} saving={updateMutation.isPending && activeField === 'rn_kkt'} />
              <div><Text type="secondary">Формат: {formatRnm(fiscal.RNKKT)}</Text></div>
            </Descriptions.Item>
            <Descriptions.Item label="Заводской номер">
              <InlineFieldEditor value={fiscal.FRSerialNumber} editable={canEdit} onSave={(v) => saveField('fr_serial_number', v)} saving={updateMutation.isPending && activeField === 'fr_serial_number'} />
            </Descriptions.Item>
            <Descriptions.Item label="Модель">
              <InlineFieldEditor value={fiscal.ModelKKT} editable={canEdit} onSave={(v) => saveField('model_kkt', v)} saving={updateMutation.isPending && activeField === 'model_kkt'} />
            </Descriptions.Item>
            <Descriptions.Item label="Описание">
              <InlineFieldEditor value={fiscal.Description} editable={canEdit} multiline onSave={(v) => saveField('description', v)} saving={updateMutation.isPending && activeField === 'description'} />
            </Descriptions.Item>
          </Descriptions>
        </Card>

        <Card title="Фискальный накопитель" className="glass-panel" size="small">
          <Descriptions bordered column={2} className="compact-descriptions">
            <Descriptions.Item label="Номер ФН">
              <InlineFieldEditor value={fiscal.FNNumber} editable={canEdit} onSave={(v) => saveField('fn_number', v)} saving={updateMutation.isPending && activeField === 'fn_number'} />
            </Descriptions.Item>
            <Descriptions.Item label="Дата регистрации">
              <InlineFieldEditor value={fiscal.kkt_reg_date ? dayjs(fiscal.kkt_reg_date).format('YYYY-MM-DD') : ''} editable={canEdit} placeholder="YYYY-MM-DD" onSave={(v) => saveField('kkt_reg_date', v)} saving={updateMutation.isPending && activeField === 'kkt_reg_date'} />
            </Descriptions.Item>
            <Descriptions.Item label="Дата окончания">
              <Space direction="vertical" size={4}>
                <InlineFieldEditor value={fiscal.fn_expire_date ? dayjs(fiscal.fn_expire_date).format('YYYY-MM-DD') : ''} editable={canEdit} placeholder="YYYY-MM-DD" onSave={(v) => saveField('fn_expire_date', v)} saving={updateMutation.isPending && activeField === 'fn_expire_date'} />
                <Text strong style={{ color: getFnDateColor(fiscal.fn_expire_date) }}>
                  {fiscal.fn_expire_date ? dayjs(fiscal.fn_expire_date).format('DD.MM.YYYY') : '-'}
                </Text>
              </Space>
            </Descriptions.Item>
          </Descriptions>
        </Card>

        <Card title="Прошивки и ПО" className="glass-panel" size="small">
          <Descriptions bordered column={3} className="compact-descriptions">
            <Descriptions.Item label="Прошивка ФР">
              <InlineFieldEditor value={fiscal.FRFirmware} editable={canEdit} onSave={(v) => saveField('fr_firmware', v)} saving={updateMutation.isPending && activeField === 'fr_firmware'} />
            </Descriptions.Item>
            <Descriptions.Item label="Загрузчик">
              <InlineFieldEditor value={fiscal.FRDownloader} editable={canEdit} onSave={(v) => saveField('fr_downloader', v)} saving={updateMutation.isPending && activeField === 'fr_downloader'} />
            </Descriptions.Item>
            <Descriptions.Item label="Драйвер">
              <InlineFieldEditor value={fiscal.DriverVersion} editable={canEdit} onSave={(v) => saveField('driver_version', v)} saving={updateMutation.isPending && activeField === 'driver_version'} />
            </Descriptions.Item>
          </Descriptions>
        </Card>

        <Card title="Юридическое лицо" className="glass-panel" size="small">
          <Descriptions bordered column={1} className="compact-descriptions">
            <Descriptions.Item label="Организация">
              <InlineFieldEditor value={fiscal.LegalName} editable={canEdit} onSave={(v) => saveField('legal_name', v)} saving={updateMutation.isPending && activeField === 'legal_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="ИНН">
              <InlineFieldEditor value={fiscal.INN} editable={canEdit} onSave={(v) => saveField('inn', v)} saving={updateMutation.isPending && activeField === 'inn'} />
            </Descriptions.Item>
            <Descriptions.Item label="Адрес установки">
              <InlineFieldEditor value={fiscal.address} editable={canEdit} onSave={(v) => saveField('address', v)} saving={updateMutation.isPending && activeField === 'address'} />
            </Descriptions.Item>
          </Descriptions>
        </Card>

        {licensesData.length > 0 && (
          <Card title="Лицензии ККТ" className="glass-panel" size="small">
            <Table dataSource={licensesData} columns={licenseColumns} rowKey="licenseID" pagination={false} size="small" />
          </Card>
        )}
      </Space>
    </div>
  );
};

export default FiscalDetails;
