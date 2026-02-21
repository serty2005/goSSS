import React, { useEffect, useMemo, useState } from 'react';
import { Form, Input, Modal, Select, Space, Button, message, Row, Col, Card, Empty, Spin, Typography, Tag, Checkbox } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import type { CompanyModel, InfrastructureItem } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { normalizeServerAddress } from '@/utils/formatters';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import { getTicketStatusMeta } from '@/constants/ticketStatus';

const { Text, Paragraph } = Typography;

interface Props {
  open: boolean;
  onClose: () => void;
  presetCompany?: { id: string; title?: string } | null;
  onCreated?: () => void;
}

const ACTIVE_TICKET_STATUSES = ['new', 'in_progress', 'pending', 'deferred', 'onsite', 'to_manager'];
const RESOLVED_OR_CLOSED_TICKET_STATUSES = ['resolved', 'closed'];
const MODAL_BODY_MAX_HEIGHT = 'calc(100vh - 240px)';

const NewTicketModal: React.FC<Props> = ({ open, onClose, presetCompany, onCreated }) => {
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [companySearch, setCompanySearch] = useState('');
  const [companyOptions, setCompanyOptions] = useState<Array<{ value: string; label: React.ReactNode; selectedLabel: string }>>([]);
  const [companyMeta, setCompanyMeta] = useState<Record<string, { address?: string; additional?: string; title?: string; parent_title?: string; parent_id?: string; active_contract?: boolean }>>({});
  const [selectedCompanyOption, setSelectedCompanyOption] = useState<{ value: string; label: React.ReactNode; selectedLabel: string } | null>(null);
  const selectedCompanyId = Form.useWatch('company_id', form) as string | undefined;
  const syncWithBitrix = Form.useWatch('sync_with_bitrix', form) as boolean | undefined;
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const canDisableBitrixSync = isBitrixEnabled && isAdmin(user?.roles);

  const renderCompanyOptionLabel = (title: string, parentTitle?: string) => {
    const parts = getCompanyHierarchyParts(title, parentTitle);
    if (!parts.hasParent) {
      return parts.child;
    }
    return (
      <Space orientation="vertical" size={0} style={{ lineHeight: 1.2 }}>
        <Text type="secondary" style={{ fontSize: 12 }}>{parts.parent}</Text>
        <Text style={{ paddingLeft: 14 }}>{parts.child}</Text>
      </Space>
    );
  };

  const { data: companiesData, isLoading: isCompaniesLoading } = useQuery({
    queryKey: ['companies', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    enabled: open,
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!companiesData?.data) return;

    const nextMeta: Record<string, { address?: string; additional?: string; title?: string; parent_title?: string; parent_id?: string; active_contract?: boolean }> = {};
    const nextOptions = companiesData.data
      .map((company) => {
        const item = company as CompanyModel;
        const rawId = resolveCompanyID(item);
        const rawTitle = resolveCompanyTitle(item);
        const rawParentTitle = resolveCompanyParentTitle(item);
        const rawParentID = company.parent_id;
        const rawAdditional = company.additional_name;
        const rawAddress = company.address;
        const rawActiveContract = company.active_contract;
        const id = rawId ? String(rawId) : '';
        const title = rawTitle || rawAdditional || id;
        const labelNode = renderCompanyOptionLabel(title, rawParentTitle);
        if (!id) {
          console.warn('[NewTicketModal] company without id', company);
          return null;
        }
        nextMeta[id] = {
          address: rawAddress ?? undefined,
          additional: rawAdditional ?? undefined,
          title: rawTitle ?? undefined,
          parent_title: rawParentTitle ?? undefined,
          parent_id: rawParentID ?? undefined,
          active_contract: typeof rawActiveContract === 'boolean' ? rawActiveContract : undefined,
        };
        return {
          value: id,
          label: labelNode,
          selectedLabel: title,
        };
      })
      .filter(Boolean) as Array<{ value: string; label: React.ReactNode; selectedLabel: string }>;

    if (selectedCompanyId && !nextOptions.some((opt) => opt.value === selectedCompanyId)) {
      const fallbackLabel = selectedCompanyOption?.selectedLabel ?? selectedCompanyId;
      nextOptions.unshift({ value: selectedCompanyId, label: fallbackLabel, selectedLabel: fallbackLabel });
    }

    setCompanyOptions(nextOptions);
    setCompanyMeta((prev) => ({ ...prev, ...nextMeta }));

  }, [companiesData, selectedCompanyId, selectedCompanyOption]);

  useEffect(() => {
    if (!selectedCompanyId) return;
    const match = companyOptions.find((opt) => opt.value === selectedCompanyId);
    if (match && match.selectedLabel !== selectedCompanyOption?.selectedLabel) {
      setSelectedCompanyOption(match);
    }

  }, [selectedCompanyId, companyOptions, selectedCompanyOption]);

  useEffect(() => {
    if (!open) return;
    form.setFieldsValue({
      sync_with_bitrix: isBitrixEnabled,
      assignee_id: user?.id,
    });
    if (presetCompany?.id) {
      const label = presetCompany.title || presetCompany.id;
      const option = { value: presetCompany.id, label, selectedLabel: label } as { value: string; label: React.ReactNode; selectedLabel: string };
      setSelectedCompanyOption(option);
      setCompanyOptions((prev) => {
        const exists = prev.some((opt) => opt.value === presetCompany.id);
        return exists ? prev : [option, ...prev];
      });
      form.setFieldsValue({ company_id: presetCompany.id });
    }

  }, [open, presetCompany, form, isBitrixEnabled, user?.id]);

  useEffect(() => {
    if (syncWithBitrix === false) {
      form.setFieldValue('bitrix_service_point_id', undefined);
      form.setFieldValue('bitrix_deal_title', undefined);
    }
  }, [syncWithBitrix, form]);

  const { data: infrastructureData, isLoading: isInfrastructureLoading } = useQuery({
    queryKey: ['company-infrastructure', selectedCompanyId],
    queryFn: () => companiesApi.getInfrastructure(selectedCompanyId ?? ''),
    enabled: open && Boolean(selectedCompanyId),
    staleTime: 30_000,
  });

  const parentCompanyID = selectedCompanyId ? companyMeta[selectedCompanyId]?.parent_id : undefined;
  const { data: parentInfrastructureData, isLoading: isParentInfrastructureLoading } = useQuery({
    queryKey: ['company-parent-infrastructure', parentCompanyID],
    queryFn: () => companiesApi.getInfrastructure(parentCompanyID ?? ''),
    enabled: open && Boolean(parentCompanyID) && parentCompanyID !== selectedCompanyId,
    staleTime: 30_000,
  });

  const { data: bitrixServicePoints = [], isLoading: isBitrixPointsLoading } = useQuery({
    queryKey: ['bitrix-service-points'],
    queryFn: () => ticketsApi.getBitrixServicePoints(),
    enabled: open && isBitrixEnabled,
    staleTime: 5 * 60_000,
  });
  const bitrixPointsOptions = useMemo(() => {
    if (!Array.isArray(bitrixServicePoints)) return [];
    return bitrixServicePoints.map((point) => ({
      value: point.b24_element_id,
      label: point.name,
    }));
  }, [bitrixServicePoints]);

  const shouldFetchCompanyDetail = open && Boolean(selectedCompanyId) && !companyMeta[selectedCompanyId ?? ''];
  const { data: companyDetailData } = useQuery({
    queryKey: ['company', selectedCompanyId],
    queryFn: () => companiesApi.getCompany(selectedCompanyId ?? ''),
    enabled: shouldFetchCompanyDetail,
    staleTime: 30_000,
  });

  useEffect(() => {
    const company = companyDetailData?.data;
    if (!company || !selectedCompanyId) return;

    const item = company as CompanyModel;
    const rawTitle = resolveCompanyTitle(item);
    const rawParentTitle = resolveCompanyParentTitle(item);
    const rawParentID = company.parent_id;
    const rawAdditional = company.additional_name;
    const rawAddress = company.address;
    const rawActiveContract = company.active_contract;

    setCompanyMeta((prev) => ({
      ...prev,
      [selectedCompanyId]: {
        address: rawAddress ?? undefined,
        additional: rawAdditional ?? undefined,
        title: rawTitle ?? undefined,
        parent_title: rawParentTitle ?? undefined,
        parent_id: rawParentID ?? undefined,
        active_contract: typeof rawActiveContract === 'boolean' ? rawActiveContract : undefined,
      },
    }));

    if (rawTitle || rawAdditional) {
      const selectedLabel = rawTitle || rawAdditional || selectedCompanyId;
      const label = renderCompanyOptionLabel(selectedLabel, rawParentTitle);
      setCompanyOptions((prev) => {
        const exists = prev.some((opt) => opt.value === selectedCompanyId);
        return exists ? prev : [{ value: selectedCompanyId, label, selectedLabel }, ...prev];
      });
    }
  }, [companyDetailData, selectedCompanyId]);

  const infrastructure = useMemo(() => infrastructureData?.data || [], [infrastructureData?.data]);
  const parentInfrastructure = useMemo(() => parentInfrastructureData?.data || [], [parentInfrastructureData?.data]);

  const selectedCompanyMeta = useMemo(() => {
    if (!selectedCompanyId) return null;
    return companyMeta[selectedCompanyId] || null;
  }, [companyMeta, selectedCompanyId]);

  const resolveEquipmentTitle = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    return (
      (data.device_name as string) ||
      (data.server_name as string) ||
      (data.model_kkt as string) ||
      (data.serial_number as string) ||
      (data.rn_kkt as string) ||
      (data.unique_id as string) ||
      (data.uuid as string) ||
      'Оборудование'
    );
  };

  const resolveConnectionItems = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    if (item.entity_type !== 'Server' && item.entity_type !== 'Workstation') {
      return null;
    }

    const formattedServerIp = item.entity_type === 'Server'
      ? normalizeServerAddress(data.ip as string | undefined, { dropPort443: true })
      : '';

    const items = [
      ...(item.entity_type === 'Server' ? [{ label: 'IP', value: formattedServerIp || undefined }] : []),
      { label: 'AnyDesk', value: data.anydesk as string | undefined },
      { label: 'TeamViewer', value: data.teamviewer as string | undefined },
      { label: 'rdp', value: data.rdp as string | undefined },
      { label: 'LM', value: data.litemanager as string | undefined },
      ...(item.entity_type === 'Server'
        ? [{ label: 'Partners', value: data.partners_link as string | undefined, isLink: true }]
        : []),
    ];

    return items.filter((entry) => entry.value) as Array<{ label: string; value?: string; isLink?: boolean }>;
  };

  const connectionsGroups = useMemo(() => {
    const parentServerGroups = parentInfrastructure
      .filter((item) => item.entity_type === 'Server')
      .map((item) => {
        const connections = resolveConnectionItems(item);
        if (!connections || connections.length === 0) return null;
        return {
          key: `parent-${(item.data as { uuid?: string })?.uuid || resolveEquipmentTitle(item)}`,
          title: `${resolveEquipmentTitle(item)} (родительская компания)`,
          connections,
        };
      })
      .filter(Boolean) as Array<{
      key?: string;
      title: string;
      connections: Array<{ label: string; value?: string; isLink?: boolean }>;
    }>;

    const ownGroups = infrastructure
      .map((item) => {
        const connections = resolveConnectionItems(item);
        if (!connections || connections.length === 0) return null;
        return {
          key: (item.data as { uuid?: string })?.uuid,
          title: resolveEquipmentTitle(item),
          connections,
        };
      })
      .filter(Boolean) as Array<{
      key?: string;
      title: string;
      connections: Array<{ label: string; value?: string; isLink?: boolean }>;
    }>;
    return [...parentServerGroups, ...ownGroups];
  }, [infrastructure, parentInfrastructure]);

  const { data: assigneesResponse, isLoading: isAssigneesLoading } = useQuery({
    queryKey: ['users-assignees'],
    queryFn: () => usersApi.getAssignees(),
    enabled: open,
    staleTime: 60_000,
  });

  const assigneeOptions = useMemo(
    () =>
      (assigneesResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({ value: item.id, label: item.full_name || item.username })),
    [assigneesResponse?.data],
  );

  const { data: activeTicketsResponse, isLoading: isActiveTicketsLoading } = useQuery({
    queryKey: ['company-active-tickets', selectedCompanyId],
    queryFn: () => ticketsApi.getTickets({
      company_id: selectedCompanyId,
      status: ACTIVE_TICKET_STATUSES.join(','),
      limit: 20,
      archive_mode: 'active',
    }),
    enabled: open && Boolean(selectedCompanyId),
    staleTime: 20_000,
  });
  const activeTickets = useMemo(() => activeTicketsResponse?.data || [], [activeTicketsResponse?.data]);
  const { data: resolvedOrClosedTicketsResponse, isLoading: isResolvedOrClosedTicketsLoading } = useQuery({
    queryKey: ['company-resolved-or-closed-tickets', selectedCompanyId],
    queryFn: () => ticketsApi.getTickets({
      company_id: selectedCompanyId,
      status: RESOLVED_OR_CLOSED_TICKET_STATUSES.join(','),
      limit: 10,
      archive_mode: 'all',
    }),
    enabled: open && Boolean(selectedCompanyId),
    staleTime: 20_000,
  });
  const resolvedOrClosedTickets = useMemo(
    () => resolvedOrClosedTicketsResponse?.data || [],
    [resolvedOrClosedTicketsResponse?.data],
  );

  const openTicketInNewTab = (ticketID: string) => {
    const targetURL = `${window.location.origin}/tickets/${ticketID}`;
    window.open(targetURL, '_blank', 'noopener,noreferrer');
  };

  const createMutation = useMutation({
    mutationFn: async (values: { company_id: string; type: string; description: string; assignee_id: number; sync_with_bitrix?: boolean; bitrix_service_point_id?: number; bitrix_deal_title?: string }) => {
      const description = values.description.trim();
      const effectiveSyncWithBitrix = isBitrixEnabled && (canDisableBitrixSync ? values.sync_with_bitrix !== false : true);
      return ticketsApi.createTicket({
        company_id: values.company_id,
        type: values.type,
        description,
        subject: description,
        assignee_id: values.assignee_id,
        sync_with_bitrix: effectiveSyncWithBitrix,
        bitrix_service_point_id: effectiveSyncWithBitrix ? values.bitrix_service_point_id : undefined,
        bitrix_deal_title: effectiveSyncWithBitrix ? values.bitrix_deal_title?.trim() : undefined,
      });
    },
    onSuccess: () => {
      message.success('Заявка создана');
      form.resetFields();
      setCompanySearch('');
      setSelectedCompanyOption(null);
      onClose();
      onCreated?.();
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => {
      message.error('Не удалось создать заявку');
    },
  });

  const handleCancel = () => {
    form.resetFields();
    setCompanySearch('');
    setSelectedCompanyOption(null);
    onClose();
  };

  return (
    <Modal
      open={open}
      onCancel={handleCancel}
      confirmLoading={createMutation.isPending}
      title="Новая заявка"
      destroyOnHidden
      width={selectedCompanyId ? 980 : 640}
      styles={{
        body: {
          maxHeight: MODAL_BODY_MAX_HEIGHT,
          overflow: 'hidden',
        },
      }}
      footer={(
        <Row justify="space-between">
          <Button onClick={() => form.submit()} loading={createMutation.isPending}>Тоже создать</Button>
          <Button type="primary" onClick={() => form.submit()} loading={createMutation.isPending}>
            Создать
          </Button>
        </Row>
      )}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={{ sync_with_bitrix: true }}
        onFinish={(values) => {
          const is_active = selectedCompanyMeta?.active_contract === true;
          if (!is_active) {
            Modal.confirm({
              title: 'Контракт неактивен',
              content: 'Данный тикет будет платным. Продолжить?',
              okText: 'Ок',
              cancelText: 'Отмена',
              onOk: () => createMutation.mutate(values),
            });
            return;
          }
          createMutation.mutate(values);
        }}
      >
        <Row gutter={24} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT }}>
          {selectedCompanyId && (
            <Col xs={24} md={8} xl={7} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT }}>
              <div style={{ maxHeight: '100%', overflowY: 'auto', paddingRight: 4 }}>
                <Card size="small" title="Активные тикеты компании" style={{ marginBottom: 12 }}>
                  {isActiveTicketsLoading ? (
                    <div style={{ textAlign: 'center', padding: 16 }}>
                      <Spin />
                    </div>
                  ) : activeTickets.length === 0 ? (
                    <Empty description="Активных тикетов нет" />
                  ) : (
                    <Space direction="vertical" size={8} style={{ width: '100%' }}>
                      {activeTickets.map((ticket) => {
                        const statusMeta = getTicketStatusMeta(ticket.status);
                        return (
                          <Card
                            key={ticket.id}
                            size="small"
                            className="glass-panel"
                            hoverable
                            style={{ cursor: 'pointer' }}
                            onClick={() => openTicketInNewTab(ticket.id)}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%' }}>
                              <Space size={6} wrap>
                                <Text strong>#{ticket.number}</Text>
                                <Tag color={statusMeta.color}>{statusMeta.label}</Tag>
                              </Space>
                              <Text>{ticket.subject || ticket.description || 'Без описания'}</Text>
                              <Text type="secondary">Обновлено: {ticket.last_activity ? new Date(ticket.last_activity).toLocaleString() : '-'}</Text>
                            </Space>
                          </Card>
                        );
                      })}
                    </Space>
                  )}
                </Card>

                {!isResolvedOrClosedTicketsLoading && resolvedOrClosedTickets.length > 0 && (
                  <Card size="small" title="Последние 10 тикетов">
                    <Space direction="vertical" size={8} style={{ width: '100%' }}>
                      {resolvedOrClosedTickets.map((ticket) => {
                        const statusMeta = getTicketStatusMeta(ticket.status);
                        return (
                          <Card
                            key={ticket.id}
                            size="small"
                            className="glass-panel"
                            hoverable
                            style={{ cursor: 'pointer' }}
                            onClick={() => openTicketInNewTab(ticket.id)}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%' }}>
                              <Space size={6} wrap>
                                <Text strong>#{ticket.number}</Text>
                                <Tag color={statusMeta.color}>{statusMeta.label}</Tag>
                              </Space>
                              <Text>{ticket.subject || ticket.description || 'Без описания'}</Text>
                              <Text type="secondary">Обновлено: {ticket.last_activity ? new Date(ticket.last_activity).toLocaleString() : '-'}</Text>
                            </Space>
                          </Card>
                        );
                      })}
                    </Space>
                  </Card>
                )}
              </div>
            </Col>
          )}

          <Col xs={24} md={selectedCompanyId ? 8 : 24} xl={selectedCompanyId ? 10 : 24} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT, overflowY: 'auto' }}>
            <Form.Item
              name="company_id"
              label="Компания"
              rules={[{ required: true, message: 'Выберите компанию' }]}
            >
              <Select
                showSearch
                placeholder="Введите название компании"
                onSearch={(value) => {
                  setCompanySearch(value);
                }}
                loading={isCompaniesLoading}
                filterOption={false}
                autoClearSearchValue
                options={companyOptions}
                optionLabelProp="selectedLabel"
                value={selectedCompanyId}
                onChange={(value, option) => {
                  const valueStr = String(value);
                  if (!valueStr || valueStr === 'undefined' || valueStr === 'null') {
                    console.warn('[NewTicketModal] onChange invalid value', { value, option });
                    return;
                  }
                  const selectedLabel = (option as { selectedLabel?: string } | undefined)?.selectedLabel ?? valueStr;
                  const label = (option as { label?: React.ReactNode } | undefined)?.label ?? selectedLabel;
                  form.setFieldValue('company_id', valueStr);
                  setSelectedCompanyOption({ value: valueStr, label, selectedLabel });
                  setCompanyOptions((prev) => {
                    const exists = prev.some((opt) => opt.value === valueStr);
                    return exists ? prev : [{ value: valueStr, label, selectedLabel }, ...prev];
                  });
                }}
              />
            </Form.Item>
            {selectedCompanyMeta && (
              <div style={{ marginTop: -6, marginBottom: 12 }}>
                <Tag color={selectedCompanyMeta.active_contract ? 'success' : 'default'}>
                  {selectedCompanyMeta.active_contract ? 'Активен' : 'Завершён'}
                </Tag>
                {selectedCompanyMeta.address && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    Адрес: {selectedCompanyMeta.address}
                  </Text>
                )}
                {selectedCompanyMeta.additional && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    Доп. информация: {selectedCompanyMeta.additional}
                  </Text>
                )}
                {selectedCompanyMeta.parent_title && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    Сеть компаний: {selectedCompanyMeta.parent_title} / {selectedCompanyMeta.title || selectedCompanyId}
                  </Text>
                )}
              </div>
            )}

            <Form.Item
              name="type"
              label="Тип заявки"
              rules={[{ required: true, message: 'Выберите тип заявки' }]}
            >
              <Select
                options={[
                  { value: 'incident', label: 'Инцидент' },
                  { value: 'consultation', label: 'Консультация' },
                  { value: 'cto', label: 'ЦТО' },
                  { value: 'acceptance_ao', label: 'Принятие на АО' },
                  { value: 'paid_works', label: 'Платные работы' },
                ]}
              />
            </Form.Item>

            <Form.Item
              name="assignee_id"
              label="Исполнитель"
              rules={[{ required: true, message: 'Выберите исполнителя' }]}
            >
              <Select
                showSearch
                placeholder="Выберите исполнителя"
                loading={isAssigneesLoading}
                optionFilterProp="label"
                options={assigneeOptions}
              />
            </Form.Item>

            {isBitrixEnabled && (
              <>
                <Form.Item
                  name="sync_with_bitrix"
                  valuePropName="checked"
                  tooltip={canDisableBitrixSync ? undefined : 'Только администратор может отключить синхронизацию'}
                >
                  <Checkbox disabled={!canDisableBitrixSync}>
                    Синхронизировать с B24
                  </Checkbox>
                </Form.Item>

                <Form.Item
                  name="bitrix_service_point_id"
                  label="Точка обслуживания (Bitrix24)"
                  rules={[
                    {
                      validator: (_, value) => {
                        if (syncWithBitrix === false) return Promise.resolve();
                        if (value === undefined || value === null || value === '') {
                          return Promise.reject(new Error('Выберите точку обслуживания Bitrix24'));
                        }
                        return Promise.resolve();
                      },
                    },
                  ]}
                >
                  <Select
                    showSearch
                    placeholder="Выберите точку обслуживания"
                    loading={isBitrixPointsLoading}
                    optionFilterProp="label"
                    options={bitrixPointsOptions}
                    disabled={syncWithBitrix === false}
                  />
                </Form.Item>

                <Form.Item
                  name="bitrix_deal_title"
                  label="Заголовок сделки (Bitrix24)"
                  rules={[
                    {
                      validator: (_, value) => {
                        if (syncWithBitrix === false) return Promise.resolve();
                        if (!String(value || '').trim()) {
                          return Promise.reject(new Error('Заполните заголовок сделки Bitrix24'));
                        }
                        return Promise.resolve();
                      },
                    },
                  ]}
                >
                  <Input placeholder="Введите заголовок сделки для Bitrix24" disabled={syncWithBitrix === false} />
                </Form.Item>
              </>
            )}

            <Form.Item
              name="description"
              label="Описание"
              rules={[{ required: true, message: 'Введите описание' }]}
            >
              <Input.TextArea rows={4} placeholder="Опишите проблему или запрос" />
            </Form.Item>

          </Col>

          {selectedCompanyId && (
            <Col xs={24} md={8} xl={7} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT }}>
              <Card size="small" title="Подключения" bodyStyle={{ maxHeight: `calc(${MODAL_BODY_MAX_HEIGHT} - 56px)`, overflowY: 'auto' }}>
                {isInfrastructureLoading || isParentInfrastructureLoading ? (
                  <div style={{ textAlign: 'center', padding: 16 }}>
                    <Spin />
                  </div>
                ) : connectionsGroups.length === 0 ? (
                  <Empty description="Подключения не найдены" />
                ) : (
                  <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                    {connectionsGroups.map((group) => (
                      <Card key={group.key || group.title} size="small" className="glass-panel">
                        <Space orientation="vertical" size={4} style={{ width: '100%' }}>
                          <Text strong>{group.title}</Text>
                          <Space orientation="vertical" size={0} style={{ width: '100%' }}>
                            {group.connections.map((entry) => (
                              entry.isLink ? (
                                <Paragraph key={`${group.title}-${entry.label}-${entry.value}`} style={{ margin: 0 }}>
                                  <a href={entry.value} target="_blank" rel="noopener noreferrer" onClick={(e) => e.stopPropagation()}>
                                    {entry.label}
                                  </a>
                                </Paragraph>
                              ) : (
                                <Paragraph key={`${group.title}-${entry.label}-${entry.value}`} copyable={{ text: entry.value }} style={{ margin: 0 }}>
                                  <Text type="secondary">{entry.label}:</Text> {entry.value}
                                </Paragraph>
                              )
                            ))}
                          </Space>
                        </Space>
                      </Card>
                    ))}
                  </Space>
                )}
              </Card>
            </Col>
          )}
        </Row>
      </Form>
    </Modal>
  );
};

export default NewTicketModal;

