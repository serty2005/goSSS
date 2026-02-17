import React, { useMemo, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Descriptions, Button, Space, Typography, Spin, Badge, message, Popconfirm, Table, theme as antTheme } from 'antd';
import { ArrowLeftOutlined, DeleteOutlined } from '@ant-design/icons';
import { equipmentApi } from '@/api/equipment';
import { companiesApi } from '@/api/companies';
import { getEntityIcon } from '@/utils/mappers';
import { formatRnm } from '@/utils/formatters';
import { EntityOwnerHistoryItemDTO, UpdateFiscalPayload } from '@/types/api';
import dayjs from 'dayjs';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment, isAdmin } from '@/utils/permissions';
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

type LicenseRow = {
  licenseID: string;
  dateLabel: string;
  expiresAt?: dayjs.Dayjs;
};

const renderTermIndicator = (date?: dayjs.Dayjs) => {
  if (!date || !date.isValid()) return '-';
  const monthsLeft = date.endOf('day').diff(dayjs(), 'month', true);
  const color = monthsLeft > 6 ? '#52c41a' : monthsLeft > 2 ? '#faad14' : '#ff4d4f';
  return <Badge color={color} text={date.format('DD.MM.YYYY')} />;
};

const formatFFD = (value?: string) => {
  if (!value) return '-';
  if (value === '105') return '1.05';
  if (value === '120') return '1.2';
  if (value === '110') return '1.1';
  return value;
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
  const canDelete = isAdmin(user?.roles);

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

  const { data: ownerCompanyRes } = useQuery({
    queryKey: ['company', fiscalRes?.data?.owner_id],
    queryFn: () => companiesApi.getCompany(fiscalRes!.data.owner_id!),
    enabled: Boolean(fiscalRes?.data?.owner_id),
    staleTime: 60_000,
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

  const deleteMutation = useMutation({
    mutationFn: () => equipmentApi.deleteFiscal(id!),
    onSuccess: () => {
      message.success('ФР удалён');
      navigate('/fiscals');
    },
    onError: () => message.error('Ошибка удаления'),
  });

  const fiscal = fiscalRes?.data;
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

  const agentUpdate = useMemo(() => (fiscal ? getAgentUpdateMeta(fiscal) : null), [fiscal]);

  const licensesData = useMemo<LicenseRow[]>(() => {
    if (!fiscal?.licenses) return [];

    if (typeof fiscal.licenses === 'string') {
      const raw = fiscal.licenses.trim();
      if (!raw) return [];

      return raw
        .split(';')
        .map((part) => part.trim())
        .filter(Boolean)
        .map((part) => {
          const splitIndex = part.indexOf(':');
          const licenseID = splitIndex >= 0 ? part.slice(0, splitIndex).trim() : part;
          const dateRaw = splitIndex >= 0 ? part.slice(splitIndex + 1).trim() : '';
          const parsed = dayjs(dateRaw);
          return {
            licenseID: licenseID || 'license',
            dateLabel: parsed.isValid() ? parsed.format('DD.MM.YYYY') : (dateRaw || '-'),
            expiresAt: parsed.isValid() ? parsed : undefined,
          };
        })
        .filter((item) => item.licenseID === '17' || item.licenseID === '19');
    }

    return Object.entries(fiscal.licenses)
      .map(([licenseID, data]) => {
      const parsed = dayjs(data.date_until);
      return {
        licenseID,
        dateLabel: parsed.isValid() ? parsed.format('DD.MM.YYYY') : '-',
        expiresAt: parsed.isValid() ? parsed : undefined,
      };
      })
      .filter((item) => item.licenseID === '17' || item.licenseID === '19');
  }, [fiscal?.licenses]);

  if (isLoading) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!fiscal) return <div>Фискальный регистратор не найден</div>;

  const saveOwner = (value: string) => {
    if (!canEdit) return;
    setActiveField('owner_id');
    updateMutation.mutate({ owner_id: value } as UpdateFiscalPayload);
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

  const renderFlag = (value?: boolean | null) => (
    <Badge color={value ? '#52c41a' : '#ff4d4f'} text={value ? 'да' : 'нет'} />
  );

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

        {canDelete && (
          <Popconfirm
            title="Удалить фискальный регистратор?"
            description="Действие необратимо."
            okText="Удалить"
            cancelText="Отмена"
            okButtonProps={{ danger: true, loading: deleteMutation.isPending }}
            onConfirm={() => deleteMutation.mutate()}
          >
            <Button danger icon={<DeleteOutlined />}>Удалить</Button>
          </Popconfirm>
        )}
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
                    saveOwner(value);
                  }}
                />
                <Space>
                  <Text type="secondary">Режим привязки: {fiscal.owner_binding_mode || 'auto'}</Text>
                  {fiscal.owner_id ? <Button type="link" onClick={() => navigate(`/companies/${fiscal.owner_id}`)}>К владельцу</Button> : null}
                </Space>
              </Space>
            </Descriptions.Item>

            <Descriptions.Item label="РНМ">
              {fiscal.rn_kkt || '-'}
              <div><Text type="secondary">Формат: {formatRnm(fiscal.rn_kkt)}</Text></div>
            </Descriptions.Item>
            <Descriptions.Item label="Юр. лицо">{fiscal.legal_name || '-'}</Descriptions.Item>
            <Descriptions.Item label="Модель">{fiscal.model_kkt || '-'}</Descriptions.Item>
            <Descriptions.Item label="Адрес" span={2}>{fiscal.address || '-'}</Descriptions.Item>
            <Descriptions.Item label="Заводской номер">{fiscal.fr_serial_number || '-'}</Descriptions.Item>
            <Descriptions.Item label="ИНН">{fiscal.inn || '-'}</Descriptions.Item>
            <Descriptions.Item label="ФН">{fiscal.fn_number || '-'}</Descriptions.Item>
            <Descriptions.Item label="Исполнение ФН">{fiscal.fn_execution || '-'}</Descriptions.Item>
            <Descriptions.Item label="Дата регистрации ККТ">{fiscal.kkt_reg_date ? dayjs(fiscal.kkt_reg_date).format('DD.MM.YYYY HH:mm:ss') : '-'}</Descriptions.Item>
            <Descriptions.Item label="Срок ФН">{renderTermIndicator(fiscal.fn_expire_date ? dayjs(fiscal.fn_expire_date) : undefined)}</Descriptions.Item>
            <Descriptions.Item label="ФФД">{formatFFD(fiscal.ffd)}</Descriptions.Item>
            <Descriptions.Item label="Версия драйвера">{fiscal.driver_version || '-'}</Descriptions.Item>
            <Descriptions.Item label="Прошивка">{fiscal.fr_firmware || '-'}</Descriptions.Item>
            <Descriptions.Item label="Акцизные товары">{renderFlag(fiscal.attribute_excise)}</Descriptions.Item>
            <Descriptions.Item label="ОФД">{fiscal.ofd_name || '-'}</Descriptions.Item>
            <Descriptions.Item label="Маркированные товары">{renderFlag(fiscal.attribute_marked)}</Descriptions.Item>
            <Descriptions.Item label="Лицензии ККТ" span={2}>
              {licensesData.length === 0 ? '-' : (
                <Space size={12} wrap>
                  {licensesData.map((item) => (
                    <Space key={`${item.licenseID}:${item.dateLabel}`} size={4}>
                      <Text>{item.licenseID}</Text>
                      {item.expiresAt ? renderTermIndicator(item.expiresAt) : <Text>{item.dateLabel}</Text>}
                    </Space>
                  ))}
                </Space>
              )}
            </Descriptions.Item>
            <Descriptions.Item label="ID РС">{fiscal.workstation_id || '-'}</Descriptions.Item>
            <Descriptions.Item label="Статус">{fiscal.health_status || '-'}</Descriptions.Item>
            <Descriptions.Item label="Обновлено">{fiscal.updated_at ? dayjs(fiscal.updated_at).format('DD.MM.YYYY HH:mm:ss') : '-'}</Descriptions.Item>
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
