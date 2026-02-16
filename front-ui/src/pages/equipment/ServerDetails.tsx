import React, { useMemo, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Tag, Space, Typography, Spin, message, Table, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined, SyncOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { companiesApi } from '@/api/companies';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatDate } from '@/utils/formatters';
import { EntityOwnerHistoryItemDTO, UpdateServerPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';
import { CompanySearchSelect } from '@/components/companies/CompanySearchSelect';
import dayjs from 'dayjs';

const { Title, Text } = Typography;

const sourceLabelMap: Record<string, string> = {
  created: 'Создание',
  manual_update: 'Ручное изменение',
  agent_data_update: 'Обновление из агента',
  candidate_approve: 'Подтверждение кандидата',
  network_auto: 'Автоопределение сети',
  network_auto_ws: 'Автоопределение сети (РС)',
  network_auto_fr: 'Автоопределение сети (ФР)',
  network_auto_both: 'Автоопределение сети (РС+ФР)',
  network_conflict: 'Конфликт сети',
  manual_resolution: 'Ручное разрешение',
};

const ServerDetails: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [activeField, setActiveField] = useState<string | null>(null);
  const [companySearch, setCompanySearch] = useState<string>('');
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

  const { data: serverRes, isLoading } = useQuery({
    queryKey: ['server', id],
    queryFn: () => equipmentApi.getServer(id!),
    enabled: !!id,
  });

  const { data: ownerHistoryRes } = useQuery({
    queryKey: ['owner-history', 'Server', id],
    queryFn: () => equipmentApi.getOwnerHistory('Server', id!, 200),
    enabled: !!id,
  });

  const { data: companiesRes } = useQuery({
    queryKey: ['companies-search', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 10_000,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateServerPayload) => equipmentApi.updateServer(id!, values),
    onSuccess: () => {
      message.success('Данные сервера обновлены');
      queryClient.invalidateQueries({ queryKey: ['server', id] });
      queryClient.invalidateQueries({ queryKey: ['owner-history', 'Server', id] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(id!),
    onSuccess: () => message.success('Запрос на опрос отправлен'),
    onError: () => message.error('Не удалось отправить запрос на опрос'),
  });

  const server = serverRes?.data;
  const companyOptions = useMemo(() => (companiesRes?.data || []).map((item) => ({
    value: String(item.id || ''),
    title: String(item.title || item.additional_name || item.id || ''),
    parentTitle: item.parent_title ? String(item.parent_title) : undefined,
  })).filter((item) => item.value && item.title), [companiesRes?.data]);

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!server) return <div>Сервер РЅРµ найден</div>;

  const saveField = (field: keyof UpdateServerPayload, value: string) => {
    if (!canEdit) return;
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
            <div style={{ fontSize: 24, color: token.colorPrimary }}>{getEntityIcon('Server')}</div>
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

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Card title="Основная информация" className="glass-panel" size="small">
          <Descriptions bordered column={2} className="compact-descriptions">
            <Descriptions.Item label="Владелец" span={2}>
              <Space direction="vertical" style={{ width: '100%' }}>
                <CompanySearchSelect
                  value={server.owner_id}
                  options={companyOptions}
                  loading={updateMutation.isPending && activeField === 'owner_id'}
                  placeholder="Выберите компанию-владельца"
                  onSearch={setCompanySearch}
                  onChange={(value) => {
                    if (!canEdit || !value) return;
                    saveField('owner_id', value);
                  }}
                />
                <Text type="secondary">Режим назначения: {server.owner_binding_mode || 'auto'}</Text>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="Название устройства">
              <InlineFieldEditor value={server.device_name} editable={canEdit} onSave={(v) => saveField('device_name', v)} saving={updateMutation.isPending && activeField === 'device_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="Имя сервера">
              <InlineFieldEditor value={server.server_name} editable={canEdit} onSave={(v) => saveField('server_name', v)} saving={updateMutation.isPending && activeField === 'server_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="URL">
              <InlineFieldEditor value={server.ip} editable={canEdit} onSave={(v) => saveField('ip', v)} saving={updateMutation.isPending && activeField === 'ip'} />
            </Descriptions.Item>
            <Descriptions.Item label="Health Status">
              <Tag color={getStatusColor(server.health_status) === 'success' ? 'green' : 'default'}>{server.health_status || '-'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Unique ID">
              <InlineFieldEditor value={server.unique_id} editable={canEdit} onSave={(v) => saveField('unique_id', v)} saving={updateMutation.isPending && activeField === 'unique_id'} />
            </Descriptions.Item>
            <Descriptions.Item label="CRM ID">
              <InlineFieldEditor value={server.crm_id} editable={canEdit} onSave={(v) => saveField('crm_id', v)} saving={updateMutation.isPending && activeField === 'crm_id'} />
            </Descriptions.Item>
            <Descriptions.Item label="Версия сервера">
              <InlineFieldEditor value={server.server_version} editable={canEdit} onSave={(v) => saveField('server_version', v)} saving={updateMutation.isPending && activeField === 'server_version'} />
            </Descriptions.Item>
            <Descriptions.Item label="Посл. опрос">{formatDate(server.last_polled_at)}</Descriptions.Item>
          </Descriptions>
        </Card>

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
                render: (_: unknown, record: EntityOwnerHistoryItemDTO) => {
                  const fromOwner = record.from_owner_id || '';
                  const toOwner = record.to_owner_id || '';
                  if (!fromOwner && !toOwner) {
                    return '-';
                  }
                  if (fromOwner && toOwner && fromOwner !== toOwner) {
                    return `${fromOwner} → ${toOwner}`;
                  }
                  return toOwner || fromOwner || '-';
                },
                width: 340,
              },
              {
                title: 'Кто сделал',
                key: 'actor',
                render: (_: unknown, record: EntityOwnerHistoryItemDTO) => {
                  if (record.changed_by_user_id) {
                    return `Пользователь ${record.changed_by_user_id}`;
                  }
                  if (record.agent_uuid) {
                    return record.agent_uuid;
                  }
                  return '-';
                },
              },
              { title: 'Комментарий', dataIndex: 'comment', key: 'comment' },
            ]}
          />
        </Card>
      </Space>
    </div>
  );
};

export default ServerDetails;

