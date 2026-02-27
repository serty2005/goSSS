import React, { useMemo, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message, Table, Popconfirm, Tabs, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { deletionCandidatesApi } from '@/api/deletionCandidates';
import { companiesApi } from '@/api/companies';
import { getEntityIcon } from '@/utils/mappers';
import { EntityOwnerHistoryItemDTO, UpdateWorkstationPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';
import dayjs from 'dayjs';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';
import { CompanySearchSelect } from '@/components/companies/CompanySearchSelect';
import AgentObservationRawModal from '@/components/agents/AgentObservationRawModal';
import MaterialsPanel from '@/components/materials/MaterialsPanel';

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
  delete_marked: 'Кандидат на удаление',
  delete_confirmed: 'Подтверждение удаления',
  duplicate_merge: 'Склейка дублей',
};

const WorkstationDetails: React.FC = () => {
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

  const { data: wsRes, isLoading } = useQuery({
    queryKey: ['workstation', id],
    queryFn: () => equipmentApi.getWorkstation(id!),
    enabled: !!id,
  });

  const { data: ownerHistoryRes } = useQuery({
    queryKey: ['owner-history', 'Workstation', id],
    queryFn: () => equipmentApi.getOwnerHistory('Workstation', id!, 200),
    enabled: !!id,
  });

  const { data: deletionCandidateRes } = useQuery({
    queryKey: ['deletion-candidate', 'Workstation', id],
    queryFn: () => deletionCandidatesApi.getByEntity('Workstation', id!),
    enabled: !!id,
    staleTime: 5_000,
  });

  const { data: companiesRes } = useQuery({
    queryKey: ['companies-search', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 10_000,
  });
  const { data: ownerCompanyRes } = useQuery({
    queryKey: ['company', wsRes?.data?.owner_id],
    queryFn: () => companiesApi.getCompany(wsRes!.data.owner_id!),
    enabled: Boolean(wsRes?.data?.owner_id),
    staleTime: 60_000,
  });

  const updateMutation = useMutation({
    mutationFn: (values: UpdateWorkstationPayload) => equipmentApi.updateWorkstation(id!, values),
    onSuccess: () => {
      message.success('Данные обновлены');
      queryClient.invalidateQueries({ queryKey: ['workstation', id] });
      queryClient.invalidateQueries({ queryKey: ['owner-history', 'Workstation', id] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const deleteMutation = useMutation({
    mutationFn: () => equipmentApi.deleteWorkstation(id!),
    onSuccess: () => {
      message.success('Рабочая станция добавлена в кандидаты на удаление');
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidate', 'Workstation', id] });
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidates'] });
    },
    onError: () => message.error('Ошибка удаления'),
  });

  const ws = wsRes?.data;
  const pendingDeletion = deletionCandidateRes?.data || null;
  const companyOptions = useMemo(() => {
    const base = (companiesRes?.data || []).map((item) => ({
      value: String(item.id || ''),
      title: String(item.title || item.additional_name || item.id || ''),
      parentTitle: item.parent_title ? String(item.parent_title) : undefined,
    })).filter((item) => item.value && item.title);
    const ownerData = ownerCompanyRes?.data;
    if (ownerData?.id && ownerData?.title && !base.some((item) => item.value === ownerData.id)) {
      base.unshift({
        value: ownerData.id,
        title: ownerData.title,
        parentTitle: ownerData.parent_title ? String(ownerData.parent_title) : undefined,
      });
    }
    return base;
  }, [companiesRes?.data, ownerCompanyRes?.data]);
  const agentUpdate = useMemo(() => (ws ? getAgentUpdateMeta(ws) : null), [ws]);

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!ws) return <div>Рабочая станция не найдена</div>;

  const saveField = (field: keyof UpdateWorkstationPayload, value: string) => {
    if (!canEdit) return;
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
          {pendingDeletion ? (
            <Badge color="#faad14" text={`Ожидает подтверждения удаления • заявка #${pendingDeletion.id}`} />
          ) : null}
        </Space>

        {canEdit && (
          <Popconfirm
            title="Добавить рабочую станцию в кандидаты на удаление?"
            description="Подтверждение удаления выполнит другой администратор в разделе «Проблемы»."
            okText="Добавить"
            cancelText="Отмена"
            okButtonProps={{ danger: true, loading: deleteMutation.isPending }}
            onConfirm={() => deleteMutation.mutate()}
          >
            <Button danger icon={<DeleteOutlined />} disabled={Boolean(pendingDeletion)}>Удалить</Button>
          </Popconfirm>
        )}
      </div>

      <Card title="Детали рабочей станции" className="glass-panel" size="small">
        <Descriptions bordered column={1} className="compact-descriptions">
          <Descriptions.Item label="Владелец">
            <Space direction="vertical" style={{ width: '100%' }}>
              <CompanySearchSelect
                value={ws.owner_id}
                options={companyOptions}
                loading={updateMutation.isPending && activeField === 'owner_id'}
                placeholder="Выберите компанию-владельца"
                onSearch={setCompanySearch}
                onChange={(value) => {
                  if (!canEdit || !value) return;
                  saveField('owner_id', value);
                }}
              />
              <Space>
                <Text type="secondary">Режим привязки: {ws.owner_binding_mode || 'auto'}</Text>
                {ws.owner_id ? (
                  <Button type="link" onClick={() => navigate(`/companies/${ws.owner_id}`)}>К владельцу</Button>
                ) : null}
              </Space>
            </Space>
          </Descriptions.Item>
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
          <Descriptions.Item label="RustDesk">
            <InlineFieldEditor value={ws.rustdesk} editable={canEdit} onSave={(v) => saveField('rustdesk', v)} saving={updateMutation.isPending && activeField === 'rustdesk'} />
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Tabs
        style={{ marginTop: 16 }}
        defaultActiveKey="history"
        items={[
          {
            key: 'history',
            label: 'История изменений',
            children: (
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
            ),
          },
          {
            key: 'materials',
            label: 'Материалы',
            children: (
              <Card title="Материалы рабочей станции" className="glass-panel" size="small">
                <MaterialsPanel entityType="Workstation" entityID={String(ws.id)} />
              </Card>
            ),
          },
        ]}
      />

      <AgentObservationRawModal
        open={Boolean(activeObservationID)}
        observationID={activeObservationID}
        onClose={() => setActiveObservationID(undefined)}
      />
    </div>
  );
};

export default WorkstationDetails;
