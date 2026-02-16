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
  manual_update: 'Р СѓС‡РЅРѕРµ РёР·РјРµРЅРµРЅРёРµ',
  agent_data_update: 'РћР±РЅРѕРІР»РµРЅРёРµ РѕС‚ Р°РіРµРЅС‚Р°',
  candidate_approve: 'РџРѕРґС‚РІРµСЂР¶РґРµРЅРёРµ РєР°РЅРґРёРґР°С‚Р°',
  network_auto: 'РђРІС‚РѕРѕРїСЂРµРґРµР»РµРЅРёРµ СЃРµС‚Рё',
  network_auto_ws: 'РђРІС‚РѕРѕРїСЂРµРґРµР»РµРЅРёРµ СЃРµС‚Рё (Р РЎ)',
  network_auto_fr: 'РђРІС‚РѕРѕРїСЂРµРґРµР»РµРЅРёРµ СЃРµС‚Рё (Р¤Р )',
  network_auto_both: 'РђРІС‚РѕРѕРїСЂРµРґРµР»РµРЅРёРµ СЃРµС‚Рё (Р РЎ+Р¤Р )',
  network_conflict: 'РљРѕРЅС„Р»РёРєС‚ СЃРµС‚Рё',
  manual_resolution: 'Р СѓС‡РЅРѕРµ СЂР°Р·СЂРµС€РµРЅРёРµ',
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
      message.success('Р”Р°РЅРЅС‹Рµ СЃРµСЂРІРµСЂР° РѕР±РЅРѕРІР»РµРЅС‹');
      queryClient.invalidateQueries({ queryKey: ['server', id] });
      queryClient.invalidateQueries({ queryKey: ['owner-history', 'Server', id] });
      setActiveField(null);
    },
    onError: () => message.error('РћС€РёР±РєР° РѕР±РЅРѕРІР»РµРЅРёСЏ'),
  });

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(id!),
    onSuccess: () => message.success('Р—Р°РїСЂРѕСЃ РЅР° РѕРїСЂРѕСЃ РѕС‚РїСЂР°РІР»РµРЅ'),
    onError: () => message.error('РќРµ СѓРґР°Р»РѕСЃСЊ РѕС‚РїСЂР°РІРёС‚СЊ Р·Р°РїСЂРѕСЃ РЅР° РѕРїСЂРѕСЃ'),
  });

  const server = serverRes?.data;
  const companyOptions = useMemo(() => (companiesRes?.data || []).map((item) => ({
    value: String(item.id || ''),
    title: String(item.title || item.additional_name || item.id || ''),
    parentTitle: item.parent_title ? String(item.parent_title) : undefined,
  })).filter((item) => item.value && item.title), [companiesRes?.data]);

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!server) return <div>РЎРµСЂРІРµСЂ РЅРµ РЅР°Р№РґРµРЅ</div>;

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
              <Title level={4} style={{ margin: 0 }}>{server.device_name || server.server_name || 'РЎРµСЂРІРµСЂ'}</Title>
              <Text type="secondary">{server.id}</Text>
            </div>
          </Space>
          <Tag color={getStatusColor(server.status) === 'success' ? 'green' : 'red'}>{(server.status || 'unknown').toUpperCase()}</Tag>
        </Space>

        {canEdit && (
          <Space>
            <Button icon={<SyncOutlined spin={pollMutation.isPending} />} onClick={() => pollMutation.mutate()}>
              РћРїСЂРѕСЃРёС‚СЊ
            </Button>
            <Button danger icon={<DeleteOutlined />}>РЈРґР°Р»РёС‚СЊ</Button>
          </Space>
        )}
      </div>

      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Card title="РћСЃРЅРѕРІРЅР°СЏ РёРЅС„РѕСЂРјР°С†РёСЏ" className="glass-panel" size="small">
          <Descriptions bordered column={2} className="compact-descriptions">
            <Descriptions.Item label="Р’Р»Р°РґРµР»РµС†" span={2}>
              <Space direction="vertical" style={{ width: '100%' }}>
                <CompanySearchSelect
                  value={server.owner_id}
                  options={companyOptions}
                  loading={updateMutation.isPending && activeField === 'owner_id'}
                  placeholder="Р’С‹Р±РµСЂРёС‚Рµ РєРѕРјРїР°РЅРёСЋ-РІР»Р°РґРµР»СЊС†Р°"
                  onSearch={setCompanySearch}
                  onChange={(value) => {
                    if (!canEdit || !value) return;
                    saveField('owner_id', value);
                  }}
                />
                <Text type="secondary">Р РµР¶РёРј РїСЂРёРІСЏР·РєРё: {server.owner_binding_mode || 'auto'}</Text>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="РќР°Р·РІР°РЅРёРµ СѓСЃС‚СЂРѕР№СЃС‚РІР°">
              <InlineFieldEditor value={server.device_name} editable={canEdit} onSave={(v) => saveField('device_name', v)} saving={updateMutation.isPending && activeField === 'device_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="РРјСЏ СЃРµСЂРІРµСЂР°">
              <InlineFieldEditor value={server.server_name} editable={canEdit} onSave={(v) => saveField('server_name', v)} saving={updateMutation.isPending && activeField === 'server_name'} />
            </Descriptions.Item>
            <Descriptions.Item label="IP Р°РґСЂРµСЃ">
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
            <Descriptions.Item label="Р’РµСЂСЃРёСЏ СЃРµСЂРІРµСЂР°">
              <InlineFieldEditor value={server.server_version} editable={canEdit} onSave={(v) => saveField('server_version', v)} saving={updateMutation.isPending && activeField === 'server_version'} />
            </Descriptions.Item>
            <Descriptions.Item label="РџРѕСЃР». РѕРїСЂРѕСЃ">{formatDate(server.last_polled_at)}</Descriptions.Item>
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

