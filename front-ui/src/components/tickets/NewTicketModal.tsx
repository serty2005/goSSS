import React, { useEffect, useMemo, useState } from 'react';
import { Form, Input, Modal, Select, Space, Button, message, Row, Col, Card, Empty, Spin, Typography, Tag } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import type { CompanyModel, InfrastructureItem } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Text, Paragraph } = Typography;

interface Props {
  open: boolean;
  onClose: () => void;
  presetCompany?: { id: string; title?: string } | null;
  onCreated?: () => void;
}

const NewTicketModal: React.FC<Props> = ({ open, onClose, presetCompany, onCreated }) => {
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [companySearch, setCompanySearch] = useState('');
  const [companyOptions, setCompanyOptions] = useState<Array<{ value: string; label: React.ReactNode }>>([]);
  const [companyMeta, setCompanyMeta] = useState<Record<string, { address?: string; additional?: string; title?: string; parentTitle?: string; activeContract?: boolean }>>({});
  const [selectedCompanyOption, setSelectedCompanyOption] = useState<{ value: string; label: React.ReactNode } | null>(null);
  const selectedCompanyId = Form.useWatch('company_id', form) as string | undefined;

  const renderCompanyOptionLabel = (title: string, parentTitle?: string) => {
    const parts = getCompanyHierarchyParts(title, parentTitle);
    if (!parts.hasParent) {
      return parts.child;
    }
    return (
      <Space direction="vertical" size={0} style={{ lineHeight: 1.2 }}>
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

    const nextMeta: Record<string, { address?: string; additional?: string; title?: string; parentTitle?: string; activeContract?: boolean }> = {};
    const nextOptions = companiesData.data
      .map((company) => {
        const item = company as CompanyModel;
        const rawId = resolveCompanyID(item);
        const rawTitle = resolveCompanyTitle(item);
        const rawParentTitle = resolveCompanyParentTitle(item);
        const rawAdditional = (company as { AdditionalName?: string; additional_name?: string }).AdditionalName ?? (company as { additional_name?: string }).additional_name;
        const rawAddress = (company as { Address?: string; address?: string }).Address ?? (company as { address?: string }).address;
        const rawActiveContract = (company as { ActiveContract?: boolean; active_contract?: boolean }).ActiveContract ?? (company as { active_contract?: boolean }).active_contract;
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
          parentTitle: rawParentTitle ?? undefined,
          activeContract: typeof rawActiveContract === 'boolean' ? rawActiveContract : undefined,
        };
        return {
          value: id,
          label: labelNode,
        };
      })
      .filter(Boolean) as Array<{ value: string; label: React.ReactNode }>;

    if (selectedCompanyId && !nextOptions.some((opt) => opt.value === selectedCompanyId)) {
      const fallbackLabel = selectedCompanyOption?.label ?? selectedCompanyId;
      nextOptions.unshift({ value: selectedCompanyId, label: fallbackLabel });
    }

    setCompanyOptions(nextOptions);
    setCompanyMeta((prev) => ({ ...prev, ...nextMeta }));

  }, [companiesData, selectedCompanyId, selectedCompanyOption]);

  useEffect(() => {
    if (!selectedCompanyId) return;
    const match = companyOptions.find((opt) => opt.value === selectedCompanyId);
    if (match && match.label !== selectedCompanyOption?.label) {
      setSelectedCompanyOption(match);
    }

  }, [selectedCompanyId, companyOptions, selectedCompanyOption]);

  useEffect(() => {
    if (!open) return;
    if (presetCompany?.id) {
      const label = presetCompany.title || presetCompany.id;
      const option = { value: presetCompany.id, label } as { value: string; label: React.ReactNode };
      setSelectedCompanyOption(option);
      setCompanyOptions((prev) => {
        const exists = prev.some((opt) => opt.value === presetCompany.id);
        return exists ? prev : [option, ...prev];
      });
      form.setFieldsValue({ company_id: presetCompany.id });
    }

  }, [open, presetCompany, form]);

  const { data: infrastructureData, isLoading: isInfrastructureLoading } = useQuery({
    queryKey: ['company-infrastructure', selectedCompanyId],
    queryFn: () => companiesApi.getInfrastructure(selectedCompanyId ?? ''),
    enabled: open && Boolean(selectedCompanyId),
    staleTime: 30_000,
  });

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
    const rawAdditional = (company as { AdditionalName?: string; additional_name?: string }).AdditionalName ?? (company as { additional_name?: string }).additional_name;
    const rawAddress = (company as { Address?: string; address?: string }).Address ?? (company as { address?: string }).address;
    const rawActiveContract = (company as { ActiveContract?: boolean; active_contract?: boolean }).ActiveContract ?? (company as { active_contract?: boolean }).active_contract;

    setCompanyMeta((prev) => ({
      ...prev,
      [selectedCompanyId]: {
        address: rawAddress ?? undefined,
        additional: rawAdditional ?? undefined,
        title: rawTitle ?? undefined,
        parentTitle: rawParentTitle ?? undefined,
        activeContract: typeof rawActiveContract === 'boolean' ? rawActiveContract : undefined,
      },
    }));

    if (rawTitle || rawAdditional) {
      const label = renderCompanyOptionLabel(rawTitle || rawAdditional || selectedCompanyId, rawParentTitle);
      setCompanyOptions((prev) => {
        const exists = prev.some((opt) => opt.value === selectedCompanyId);
        return exists ? prev : [{ value: selectedCompanyId, label }, ...prev];
      });
    }
  }, [companyDetailData, selectedCompanyId]);

  const infrastructure = infrastructureData?.data || [];

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

    const items = [
      ...(item.entity_type === 'Server' ? [{ label: 'IP', value: data.ip as string | undefined }] : []),
      { label: 'AnyDesk', value: data.anydesk as string | undefined },
      { label: 'TeamViewer', value: data.teamviewer as string | undefined },
      { label: 'RDP', value: data.rdp as string | undefined },
      { label: 'LM', value: data.litemanager as string | undefined },
    ];

    return items.filter((entry) => entry.value);
  };

  const connectionsGroups = useMemo(() => {
    return infrastructure
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
      connections: Array<{ label: string; value?: string }>;
    }>;
  }, [infrastructure]);

  const createMutation = useMutation({
    mutationFn: async (values: { company_id: string; type: string; description: string }) => {
      const description = values.description.trim();
      return ticketsApi.createTicket({
        company_id: values.company_id,
        type: values.type,
        description,
        subject: description,
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
      footer={(
        <Row justify="space-between">
          <Button onClick={handleCancel}>Отмена</Button>
          <Button type="primary" onClick={() => form.submit()} loading={createMutation.isPending}>
            Создать
          </Button>
        </Row>
      )}
    >
      <Form
        form={form}
        layout="vertical"
        onFinish={(values) => {
          const isActive = selectedCompanyMeta?.activeContract === true;
          if (!isActive) {
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
        <Row gutter={24}>
          <Col xs={24} md={selectedCompanyId ? 12 : 24}>
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
                value={selectedCompanyId}
                onChange={(value, option) => {
                  const valueStr = String(value);
                  if (!valueStr || valueStr === 'undefined' || valueStr === 'null') {
                    console.warn('[NewTicketModal] onChange invalid value', { value, option });
                    return;
                  }
                  const label = (option as { label?: React.ReactNode })?.label ?? valueStr;
                  form.setFieldValue('company_id', valueStr);
                  setSelectedCompanyOption({ value: valueStr, label });
                  setCompanyOptions((prev) => {
                    const exists = prev.some((opt) => opt.value === valueStr);
                    return exists ? prev : [{ value: valueStr, label }, ...prev];
                  });
                }}
              />
            </Form.Item>
            {selectedCompanyMeta && (
              <div style={{ marginTop: -6, marginBottom: 12 }}>
                <Tag color={selectedCompanyMeta.activeContract ? 'success' : 'default'}>
                  {selectedCompanyMeta.activeContract ? 'Активен' : 'Завершён'}
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
                {selectedCompanyMeta.parentTitle && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    Сеть компаний: {selectedCompanyMeta.parentTitle} / {selectedCompanyMeta.title || selectedCompanyId}
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
                  { value: 'service_request', label: 'Запрос на обслуживание' },
                ]}
              />
            </Form.Item>

            <Form.Item
              name="description"
              label="Описание"
              rules={[{ required: true, message: 'Введите описание' }]}
            >
              <Input.TextArea rows={4} placeholder="Опишите проблему или запрос" />
            </Form.Item>

          </Col>

          {selectedCompanyId && (
            <Col xs={24} md={12}>
              <Card size="small" title="Подключения">
                {isInfrastructureLoading ? (
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
                              <Paragraph key={`${group.title}-${entry.label}-${entry.value}`} copyable={{ text: entry.value }} style={{ margin: 0 }}>
                                <Text type="secondary">{entry.label}:</Text> {entry.value}
                              </Paragraph>
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
