import React, { useMemo, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message, Table, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { companiesApi } from '@/api/companies';
import { getEntityIcon } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { EntityOwnerHistoryItemDTO, UpdateFiscalPayload } from '@/types/api';
import dayjs from 'dayjs';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';
import { CompanySearchSelect } from '@/components/companies/CompanySearchSelect';
import AgentObservationRawModal from '@/components/agents/AgentObservationRawModal';

const { Title, Text } = Typography;

const sourceLabelMap: Record<string, string> = {
  created: 'Создание',
  manual_update: 'Ручное изменение',
  agent_data_update: 'Обновление от агента',
  candidate_approve: 'Подтверждение кандидата',
  network_auto: 'Автоопределение сети',
  network_auto_ws: 'Автоопределение сети (РС)',
  network_auto_fr: 'Автоопределение сети (ФР)',
  network_auto_both: 'Автоопределение сети (РС+ФР)',
  network_conflict: 'Конфликт сети',
  manual_resolution: 'Ручное разрешение',
};

const FiscalDetails: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);
  const [companySearch, setCompanySearch] = useState<string>('');
  const [activeObservationID, setActiveObservationID] = useState<number | undefined>(undefined);
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

  const { data: fiscalRes, isLoading } = useQuery({
    queryKey: ['fiscal', id],
    queryFn: () => equipmentApi.getFiscal(id!),
    enabled: !!id,
  });

  const { data: ownerHistoryRes } = useQuery({
    queryKey: ['owner-history', 'FiscalRegister', id],
    queryFn: () => equipmentApi.getOwnerHistory('FiscalRegister', id!, 200),
    enabled: !!id,
  });

  const { data: companiesRes } = useQuery({
    queryKey: ['companies-search', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 10_000,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateFiscalPayload) => equipmentApi.updateFiscal(id!, values),
    onSuccess: () => {
      message.success('Данные обновлены');
      queryClient.invalidateQueries({ queryKey: ['fiscal', id] });
      queryClient.invalidateQueries({ queryKey: ['owner-history', 'FiscalRegister', id] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const fiscal = fiscalRes?.data;
  const companyOptions = useMemo(() => (companiesRes?.data || []).map((item) => ({
    value: String(item.id || ''),
    title: String(item.title || item.additional_name || item.id || ''),
    parentTitle: item.parent_title ? String(item.parent_title) : undefined,
  })).filter((item) => item.value && item.title), [companiesRes?.data]);
  const agentUpdate = useMemo(() => (fiscal ? getAgentUpdateMeta(fiscal) : null), [fiscal]);
  const licensesData = useMemo(() => (
    fiscal?.licenses
      ? Object.entries(fiscal.licenses).map(([licenseID, data]) => ({ licenseID, ...data }))
      : []
  ), [fiscal?.licenses]);

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!fiscal) return <div>Фискальный регистратор не найден</div>;

  const saveField = (field: keyof UpdateFiscalPayload, value: string) => {
    if (!canEdit) return;
    setActiveField(field);
    updateMutation.mutate({ [field]: value } as UpdateFiscalPayload);
  };

  const licenseColumns = [
    { title: 'ID', dataIndex: 'licenseID', width: 60 },
    { title: 'Название', dataIndex: 'name' },
    { title: 'До', dataIndex: 'date_until', render: (value: string) => value ? value.split(' ')[0] : '-' },
  ];

  const handleBack = () => {
    const backTo = (location.state as { backTo?: string } | null)?.backTo;
    if (backTo) {
      navigate(backTo);
      return;
    }
    navigate(-1);
  };

  const renderActor = (record: EntityOwnerHistoryItemDTO) => {
    if (record.changed_by_user_id) {
      return `Пользователь ${record.changed_by_user_id}`;
    }
    if (record.agent_uuid) {
      if (record.observation_id) {
        return (
          <Space size={4}>
            <span>{record.agent_uuid}</span>
            <a onClick={() => setActiveObservationID(record.observation_id)}>событие #{record.observation_id}</a>
          </Space>
        );
      }
      return record.agent_uuid;
    }
    return '-';
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={handleBack} />
          <Space>
            <div style={{ fontSize: 24, color: token.colorPrimary }}>{getEntityIcon('FiscalRegister')}</div>
            <div>
              <Title level={4} style={{ margin: 0 }}>{fiscal.model_kkt || 'ККТ'}</Title>
              <Text type="secondary">{fiscal.fr_serial_number || fiscal.id}</Text>
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

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Card title="Информация о ККТ" className="glass-panel" size="small">
          <Descriptions bordered column={2} className="compact-descriptions">
            <Descriptions.Item label="Владелец" span={2}>
              <Space direction="vertical" style={{ width: '100%' }}>
                <CompanySearchSelect
                  value={fiscal.owner_id}
                  options={companyOptions}
                  loading={updateMutation.isPending && activeField === 'owner_id'}
                  placeholder="Выберите компанию-владельца"
                  onSearch={setCompanySearch}
                  onChange={(value) => {
                    if (!canEdit || !value) return;
                    saveField('owner_id', value);
                  }}
                />
                <Text type="secondary">Режим привязки: {fiscal.owner_binding_mode || 'auto'}</Text>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="РНМ">
              <InlineFieldEditor value={fiscal.rn_kkt} editable={canEdit} onSave={(v) => saveField('rn_kkt', v)} saving={updateMutation.isPending && activeField === 'rn_kkt'} />
              <div><Text type="secondary">Формат: {formatRnm(fiscal.rn_kkt)}</Text></div>
            </Descriptions.Item>
            <Descriptions.Item label="Заводской номер">
              <InlineFieldEditor value={fiscal.fr_serial_number} editable={canEdit} onSave={(v) => saveField('fr_serial_number', v)} saving={updateMutation.isPending && activeField === 'fr_serial_number'} />
            </Descriptions.Item>
            <Descriptions.Item label="Модель">
              <InlineFieldEditor value={fiscal.model_kkt} editable={canEdit} onSave={(v) => saveField('model_kkt', v)} saving={updateMutation.isPending && activeField === 'model_kkt'} />
            </Descriptions.Item>
            <Descriptions.Item label="Описание">
              <InlineFieldEditor value={fiscal.description} editable={canEdit} multiline onSave={(v) => saveField('description', v)} saving={updateMutation.isPending && activeField === 'description'} />
            </Descriptions.Item>
          </Descriptions>
        </Card>

        {licensesData.length > 0 && (
          <Card title="Лицензии ККТ" className="glass-panel" size="small">
            <Table dataSource={licensesData} columns={licenseColumns} rowKey="licenseID" pagination={false} size="small" />
          </Card>
        )}

        <Card title="История изменений" className="glass-panel" size="small">
          <Table<EntityOwnerHistoryItemDTO>
            rowKey="id"
            pagination={{ pageSize: 10 }}
            dataSource={ownerHistoryRes?.data || []}
            columns={[
              {
                title: 'Время',
                dataIndex: 'created_at',
                key: 'created_at',
                render: (value: string) => dayjs(value).format('DD.MM.YYYY HH:mm:ss'),
                width: 200,
              },
              {
                title: 'Источник',
                dataIndex: 'change_source',
                key: 'change_source',
                width: 220,
                render: (value: string) => sourceLabelMap[value] || value || '-',
              },
              {
                title: 'Владелец',
                key: 'owners',
                width: 320,
                render: (_: unknown, record: EntityOwnerHistoryItemDTO) => {
                  const fromOwner = record.from_owner_id || '';
                  const toOwner = record.to_owner_id || '';
                  if (!fromOwner && !toOwner) return '-';
                  if (fromOwner && toOwner && fromOwner !== toOwner) return `${fromOwner} → ${toOwner}`;
                  return toOwner || fromOwner || '-';
                },
              },
              {
                title: 'Кто сделал',
                key: 'actor',
                render: (_: unknown, record: EntityOwnerHistoryItemDTO) => renderActor(record),
                width: 260,
              },
              { title: 'Комментарий', dataIndex: 'comment', key: 'comment' },
            ]}
          />
        </Card>
      </Space>

      <AgentObservationRawModal
        open={Boolean(activeObservationID)}
        observationID={activeObservationID}
        onClose={() => setActiveObservationID(undefined)}
      />
    </div>
  );
};

export default FiscalDetails;
