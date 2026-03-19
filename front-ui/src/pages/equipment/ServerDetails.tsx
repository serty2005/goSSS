import React, { useMemo, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient, useQueries } from '@tanstack/react-query';
import { Card, Descriptions, Button, Tag, Space, Typography, Spin, message, Table, Tabs, Empty, Popconfirm, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined, SyncOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { deletionCandidatesApi } from '@/api/deletionCandidates';
import { companiesApi } from '@/api/companies';
import { getEntityIcon, getStatusColor } from '@/utils/mappers';
import { formatDate } from '@/utils/formatters';
import { EntityOwnerHistoryItemDTO, UpdateServerPayload } from '@/types/api';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';
import { CompanySearchSelect } from '@/components/companies/CompanySearchSelect';
import EntityHierarchyExplorer from '@/components/entities/EntityHierarchyExplorer';
import ServerLicenseStatusTag from '@/components/entities/ServerLicenseStatusTag';
import MaterialsPanel from '@/components/materials/MaterialsPanel';
import { useBackNavigation } from '@/hooks/useBackNavigation';
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
  delete_marked: 'Кандидат на удаление',
  delete_confirmed: 'Подтверждение удаления',
  duplicate_merge: 'Склейка дублей',
};

const fieldLabelMap: Record<string, string> = {
  id: 'ID',
  created_at: 'Создано',
  updated_at: 'Обновлено',
  last_updated_by: 'Последний источник обновления',
  deleted_at: 'Удалено',
  last_modified_date: 'Дата изменения в SD',
  unique_id: 'Unique ID',
  ip: 'URL/IP',
  cabinet_link: 'Партнёрский портал',
  device_name: 'Название устройства',
  litemanager: 'LiteManager',
  server_version: 'Версия сервера',
  description: 'Описание',
  owner_id: 'Владелец',
  owner_binding_mode: 'Режим назначения владельца',
  additional_owners: 'Дополнительные владельцы',
  server_name: 'Имя сервера',
  server_edition: 'Редакция сервера',
  last_polled_at: 'Последний опрос',
  status: 'Статус',
  health_status: 'Health Status',
  status_details: 'Детали статуса',
  crm_id: 'CRM ID',
  rdp: 'RDP',
  teamviewer: 'TeamViewer',
  anydesk: 'AnyDesk',
  partners_link: 'Ссылка партнёрского портала',
};

const isPresent = (value: unknown) => {
  if (value === null || value === undefined) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (Array.isArray(value)) return value.length > 0;
  return true;
};

const formatDynamicValue = (key: string, value: unknown) => {
  if (!isPresent(value)) return '-';
  if (typeof value === 'boolean') return value ? 'Да' : 'Нет';
  if (Array.isArray(value)) return value.join(', ');
  if (value && typeof value === 'object') {
    return JSON.stringify(value, null, 2);
  }
  if (typeof value === 'string' && (key.endsWith('_at') || key.endsWith('_date'))) {
    return formatDate(value);
  }
  return String(value);
};

const toTitle = (value: string | undefined, fallback: string) => {
  const cleaned = String(value || '').trim();
  return cleaned || fallback;
};

const ServerDetails: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const goBack = useBackNavigation('/servers');
  const [activeField, setActiveField] = useState<string | null>(null);
  const [companySearch, setCompanySearch] = useState<string>('');
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

  const { data: serverRes, isLoading } = useQuery({
    queryKey: ['server', id],
    queryFn: () => equipmentApi.getServer(id!),
    enabled: !!id,
    refetchInterval: 5000,
  });

  const { data: ownerHistoryRes } = useQuery({
    queryKey: ['owner-history', 'Server', id],
    queryFn: () => equipmentApi.getOwnerHistory('Server', id!, 200),
    enabled: !!id,
  });
  const { data: deletionCandidateRes } = useQuery({
    queryKey: ['deletion-candidate', 'Server', id],
    queryFn: () => deletionCandidatesApi.getByEntity('Server', id!),
    enabled: !!id,
    staleTime: 5_000,
  });

  const { data: companiesRes } = useQuery({
    queryKey: ['companies-search', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 10_000,
  });

  const server = serverRes?.data;
  const pendingDeletion = deletionCandidateRes?.data || null;

  const { data: ownerCompanyRes } = useQuery({
    queryKey: ['company', server?.owner_id],
    queryFn: () => companiesApi.getCompany(server!.owner_id!),
    enabled: Boolean(server?.owner_id),
    staleTime: 60_000,
  });

  const parentCompanyID = String(ownerCompanyRes?.data?.parent_id || '').trim();
  const { data: parentCompanyRes } = useQuery({
    queryKey: ['company', 'parent', parentCompanyID],
    queryFn: () => companiesApi.getCompany(parentCompanyID),
    enabled: Boolean(parentCompanyID),
    staleTime: 60_000,
  });

  const { data: ownerInfraRes, isLoading: isOwnerInfraLoading } = useQuery({
    queryKey: ['company', server?.owner_id, 'infra', 'server-details-hierarchy'],
    queryFn: () => companiesApi.getInfrastructure(server!.owner_id!),
    enabled: Boolean(server?.owner_id),
    staleTime: 30_000,
  });

  const workstationIDs = useMemo(() => {
    return (ownerInfraRes?.data || [])
      .filter((item) => item.entity_type === 'Workstation')
      .map((item) => String((item.data as Record<string, unknown>).uuid || '').trim())
      .filter(Boolean);
  }, [ownerInfraRes?.data]);

  const fiscalIDs = useMemo(() => {
    return (ownerInfraRes?.data || [])
      .filter((item) => item.entity_type === 'FiscalRegister')
      .map((item) => String((item.data as Record<string, unknown>).uuid || '').trim())
      .filter(Boolean);
  }, [ownerInfraRes?.data]);

  const workstationDetailQueries = useQueries({
    queries: workstationIDs.map((workstationID) => ({
      queryKey: ['workstation', workstationID, 'hierarchy'],
      queryFn: () => equipmentApi.getWorkstation(workstationID),
      staleTime: 30_000,
    })),
  });

  const fiscalDetailQueries = useQueries({
    queries: fiscalIDs.map((fiscalID) => ({
      queryKey: ['fiscal', fiscalID, 'hierarchy'],
      queryFn: () => equipmentApi.getFiscal(fiscalID),
      staleTime: 30_000,
    })),
  });

  const hierarchyLoading = isOwnerInfraLoading
    || workstationDetailQueries.some((query) => query.isLoading)
    || fiscalDetailQueries.some((query) => query.isLoading);

  const hierarchyWorkstations = useMemo(() => {
    if (!server) return [];
    return workstationDetailQueries
      .map((query) => query.data?.data)
      .filter((item): item is NonNullable<typeof item> => Boolean(item))
      .filter((item) => String(item.server_id || '') === String(server.id))
      .map((item) => ({
        id: String(item.id),
        title: toTitle(item.device_name, 'Рабочая станция'),
        serverID: String(item.server_id || ''),
      }));
  }, [server, workstationDetailQueries]);

  const hierarchyWorkstationIDs = useMemo(() => new Set(hierarchyWorkstations.map((item) => item.id)), [hierarchyWorkstations]);

  const hierarchyFiscals = useMemo(() => {
    return fiscalDetailQueries
      .map((query) => query.data?.data)
      .filter((item): item is NonNullable<typeof item> => Boolean(item))
      .filter((item) => hierarchyWorkstationIDs.has(String(item.workstation_id || '')))
      .map((item) => ({
        id: String(item.id),
        title: toTitle(item.model_kkt || item.rn_kkt, 'Фискальный регистратор'),
        workstationID: String(item.workstation_id || ''),
      }));
  }, [fiscalDetailQueries, hierarchyWorkstationIDs]);

  const updateMutation = useMutation({
    mutationFn: (values: UpdateServerPayload) => equipmentApi.updateServer(id!, values),
    onSuccess: () => {
      message.success('Данные сервера обновлены');
      queryClient.invalidateQueries({ queryKey: ['server', id] });
      queryClient.invalidateQueries({ queryKey: ['owner-history', 'Server', id] });
      queryClient.invalidateQueries({ queryKey: ['company', server?.owner_id, 'infra', 'server-details-hierarchy'] });
      setActiveField(null);
    },
    onError: () => message.error('Ошибка обновления'),
  });

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(id!),
    onSuccess: () => {
      message.success('Запрос на опрос отправлен');
      void queryClient.invalidateQueries({ queryKey: ['server', id] });
    },
    onError: () => message.error('Не удалось отправить запрос на опрос'),
  });
  const deleteMutation = useMutation({
    mutationFn: () => equipmentApi.deleteServer(id!),
    onSuccess: () => {
      message.success('Сервер добавлен в кандидаты на удаление');
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidate', 'Server', id] });
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidates'] });
      void queryClient.invalidateQueries({ queryKey: ['owner-history', 'Server', id] });
    },
    onError: () => message.error('Ошибка удаления'),
  });

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

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!server) return <div>Сервер не найден</div>;

  const normalizedStatus = String(server.status || '').toLowerCase();

  const saveField = (field: keyof UpdateServerPayload, value: string) => {
    if (!canEdit) return;
    setActiveField(field);
    updateMutation.mutate({ [field]: value } as UpdateServerPayload);
  };

  const serverRecord = server as unknown as Record<string, unknown>;
  const fixedRenderedKeys = new Set([
    'id',
    'owner_id',
    'owner_binding_mode',
    'device_name',
    'server_name',
    'description',
    'ip',
    'health_status',
    'status',
    'unique_id',
    'crm_id',
    'partners_link',
    'cabinet_link',
    'server_version',
    'server_edition',
    'teamviewer',
    'anydesk',
    'rdp',
    'litemanager',
    'last_polled_at',
  ]);

  const dynamicFields = Object.entries(serverRecord)
    .filter(([key, value]) => !fixedRenderedKeys.has(key) && isPresent(value))
    .sort(([a], [b]) => a.localeCompare(b, 'ru'));

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={goBack} />
          <Space>
            <div style={{ fontSize: 24, color: token.colorPrimary }}>{getEntityIcon('Server')}</div>
            <div>
              <Title level={4} style={{ margin: 0 }}>{server.device_name || server.server_name || 'Сервер'}</Title>
              <Text type="secondary">{server.id}</Text>
            </div>
          </Space>
          <ServerLicenseStatusTag
            serverID={String(id || '')}
            status={normalizedStatus}
            uniqueID={String(server.unique_id || '')}
            onInstalled={() => {
              void queryClient.invalidateQueries({ queryKey: ['owner-history', 'Server', id] });
            }}
          />
          {pendingDeletion ? <Tag color="orange">Ожидает удаления #{pendingDeletion.id}</Tag> : null}
        </Space>

        {canEdit && (
          <Space>
            <Button icon={<SyncOutlined spin={pollMutation.isPending} />} onClick={() => pollMutation.mutate()}>
              Опросить
            </Button>
            <Popconfirm
              title="Добавить сервер в кандидаты на удаление?"
              description="Фактическое удаление подтверждает другой администратор в разделе «Проблемы»."
              okText="Добавить"
              cancelText="Отмена"
              okButtonProps={{ danger: true, loading: deleteMutation.isPending }}
              onConfirm={() => deleteMutation.mutate()}
            >
              <Button danger icon={<DeleteOutlined />} disabled={Boolean(pendingDeletion)}>Удалить</Button>
            </Popconfirm>
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
                <Space>
                  <Text type="secondary">Режим назначения: {server.owner_binding_mode || 'auto'}</Text>
                  {server.owner_id ? (
                    <Button type="link" onClick={() => navigate(`/companies/${server.owner_id}`)}>К владельцу</Button>
                  ) : null}
                </Space>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="Название устройства">
              <InlineFieldEditor value={server.device_name} editable={canEdit} onSave={(v) => saveField('device_name', v)} saving={updateMutation.isPending && activeField === 'device_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="Имя сервера">
              <InlineFieldEditor value={server.server_name} editable={canEdit} onSave={(v) => saveField('server_name', v)} saving={updateMutation.isPending && activeField === 'server_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="Описание" span={2}>
              <InlineFieldEditor value={server.description} editable={canEdit} multiline onSave={(v) => saveField('description', v)} saving={updateMutation.isPending && activeField === 'description'} />
            </Descriptions.Item>
            <Descriptions.Item label="URL/IP">
              <InlineFieldEditor value={server.ip} editable={canEdit} onSave={(v) => saveField('ip', v)} saving={updateMutation.isPending && activeField === 'ip'} />
            </Descriptions.Item>
            <Descriptions.Item label="Health Status">
              <Tag color={getStatusColor(server.health_status) === 'success' ? 'green' : 'default'}>{server.health_status || '-'}</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Unique ID">
              <InlineFieldEditor value={server.unique_id} editable={canEdit} onSave={(v) => saveField('unique_id', v)} saving={updateMutation.isPending && activeField === 'unique_id'} />
            </Descriptions.Item>
            <Descriptions.Item label="CRM ID">
              <Text>{(server.crm_id || '').trim() || '-'}</Text>
            </Descriptions.Item>
            <Descriptions.Item label="Партнёрский портал" span={2}>
              <InlineFieldEditor
                value={server.partners_link || server.cabinet_link}
                placeholder="Вставьте ссылку партнёрского портала или ID клиента"
                editable={canEdit}
                onSave={(v) => saveField('cabinet_link', v)}
                saving={updateMutation.isPending && activeField === 'cabinet_link'}
              />
            </Descriptions.Item>
            <Descriptions.Item label="Версия сервера">
              <InlineFieldEditor value={server.server_version} editable={canEdit} onSave={(v) => saveField('server_version', v)} saving={updateMutation.isPending && activeField === 'server_version'} />
            </Descriptions.Item>
            <Descriptions.Item label="Редакция сервера">
              <InlineFieldEditor value={server.server_edition} editable={canEdit} onSave={(v) => saveField('server_edition', v)} saving={updateMutation.isPending && activeField === 'server_edition'} />
            </Descriptions.Item>
            <Descriptions.Item label="AnyDesk">
              <InlineFieldEditor value={server.anydesk} editable={canEdit} onSave={(v) => saveField('anydesk', v)} saving={updateMutation.isPending && activeField === 'anydesk'} />
            </Descriptions.Item>
            <Descriptions.Item label="TeamViewer">
              <InlineFieldEditor value={server.teamviewer} editable={canEdit} onSave={(v) => saveField('teamviewer', v)} saving={updateMutation.isPending && activeField === 'teamviewer'} />
            </Descriptions.Item>
            <Descriptions.Item label="RDP">
              <InlineFieldEditor value={server.rdp} editable={canEdit} onSave={(v) => saveField('rdp', v)} saving={updateMutation.isPending && activeField === 'rdp'} />
            </Descriptions.Item>
            <Descriptions.Item label="LiteManager">
              <InlineFieldEditor value={server.litemanager} editable={canEdit} onSave={(v) => saveField('litemanager', v)} saving={updateMutation.isPending && activeField === 'litemanager'} />
            </Descriptions.Item>
            <Descriptions.Item label="Посл. опрос">{formatDate(server.last_polled_at)}</Descriptions.Item>
            <Descriptions.Item label="Статус">{server.status || '-'}</Descriptions.Item>

            {dynamicFields.map(([key, value]) => (
              <Descriptions.Item key={key} label={fieldLabelMap[key] || key} span={key === 'status_details' ? 2 : 1}>
                {typeof value === 'object' && value !== null ? (
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{formatDynamicValue(key, value)}</pre>
                ) : (
                  formatDynamicValue(key, value)
                )}
              </Descriptions.Item>
            ))}
          </Descriptions>
        </Card>

        <Tabs
          defaultActiveKey="history"
          items={[
            {
              key: 'history',
              label: 'История',
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
              ),
            },
            {
              key: 'hierarchy',
              label: 'Иерархия',
              children: (
                <Card title="Иерархия связей" className="glass-panel" size="small">
                  {!server.owner_id ? (
                    <Empty description="Для сервера не назначен владелец, иерархия недоступна" />
                  ) : (
                    <EntityHierarchyExplorer
                      loading={hierarchyLoading}
                      rootCompany={ownerCompanyRes?.data ? { id: String(ownerCompanyRes.data.id), title: toTitle(ownerCompanyRes.data.title, 'Компания') } : undefined}
                      parentCompany={parentCompanyRes?.data ? { id: String(parentCompanyRes.data.id), title: toTitle(parentCompanyRes.data.title, 'Родительская компания') } : undefined}
                      server={{
                        id: String(server.id),
                        title: toTitle(server.device_name || server.server_name, 'Сервер'),
                      }}
                      workstations={hierarchyWorkstations}
                      fiscals={hierarchyFiscals}
                      initialFocus={{ type: 'server', id: String(server.id) }}
                    />
                  )}
                </Card>
              ),
            },
            {
              key: 'materials',
              label: 'Материалы',
              children: (
                <Card title="Материалы сервера" className="glass-panel" size="small">
                  <MaterialsPanel entityType="Server" entityID={String(server.id)} />
                </Card>
              ),
            },
          ]}
        />
      </Space>

    </div>
  );
};

export default ServerDetails;
