import React, { useEffect, useMemo, useState } from 'react';
import { Form, Input, Modal, Select, Space, Button, message, Row, Col, Card, Empty, Spin, Typography, Tag, Checkbox } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { companiesApi } from '@/api/companies';
import { telephonyApi } from '@/api/telephony';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import type { CompanyModel, InfrastructureItem, TelephonyCallDTO, TelephonyContactCompanyDTO } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { getIikoWebAppLinkMeta, normalizeServerAddress } from '@/utils/formatters';
import { getTelephonyContactLabel, getTelephonyContactPhoneDisplay } from '@/utils/telephony';
import { normalizeTicketPreview } from '@/utils/ticketText';
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
  const selectedTelephonyCallID = Form.useWatch('telephony_call_id', form) as string | undefined;
  const syncWithBitrix = Form.useWatch('sync_with_bitrix', form) as boolean | undefined;
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const canDisableBitrixSync = isBitrixEnabled && isAdmin(user?.roles);
  const { data: pendingContext, isFetching: isPendingContextLoading } = useQuery({
    queryKey: ['telephony', 'pending-context', 'me'],
    queryFn: () => telephonyApi.getMyPendingContext(),
    enabled: open,
    staleTime: 15_000,
  });
  const { data: recentCallsResponse, isFetching: isRecentCallsLoading } = useQuery({
    queryKey: ['telephony', 'recent-calls-for-ticket', user?.id],
    queryFn: () => {
      const now = new Date();
      return telephonyApi.getUserCalls(user?.id ?? 0, {
        started_from: new Date(now.getTime() - 60 * 60 * 1000).toISOString(),
        started_to: now.toISOString(),
        only_without_ticket: true,
        limit: 50,
      });
    },
    enabled: open && Boolean(user?.id),
    staleTime: 15_000,
  });
  const recentCalls = useMemo(() => recentCallsResponse?.items || [], [recentCallsResponse?.items]);
  const selectableCalls = useMemo(() => {
    const latestByPhone = new Map<string, TelephonyCallDTO>();
    const upsertCall = (call?: TelephonyCallDTO | null) => {
      if (!call) return;
      const phoneKey = String(call.contact?.phone_normalized || call.client_phone || '').trim();
      if (!phoneKey) return;
      const existing = latestByPhone.get(phoneKey);
      const currentRank = new Date(call.started_at || call.answered_at || call.completed_at || 0).getTime();
      const existingRank = existing ? new Date(existing.started_at || existing.answered_at || existing.completed_at || 0).getTime() : -1;
      if (!existing || currentRank >= existingRank) {
        latestByPhone.set(phoneKey, call);
      }
    };

    recentCalls.forEach((call) => upsertCall(call));
    if (pendingContext?.call?.id) {
      upsertCall({
        ...pendingContext.call,
        contact: pendingContext.contact,
      });
    }

    return Array.from(latestByPhone.values()).sort((left, right) => {
      const leftRank = new Date(left.started_at || left.answered_at || left.completed_at || 0).getTime();
      const rightRank = new Date(right.started_at || right.answered_at || right.completed_at || 0).getTime();
      return rightRank - leftRank;
    });
  }, [pendingContext?.call, pendingContext?.contact, recentCalls]);
  const selectedTelephonyCall = useMemo(
    () => selectableCalls.find((item) => item.id === selectedTelephonyCallID),
    [selectableCalls, selectedTelephonyCallID],
  );
  const selectedTelephonyContactID = selectedTelephonyCall?.contact?.id;
  const { data: contactCompanies = [], isLoading: isContactCompaniesLoading } = useQuery({
    queryKey: ['telephony', 'contact-companies', selectedTelephonyContactID],
    queryFn: () => telephonyApi.getContactCompanies(selectedTelephonyContactID ?? 0),
    enabled: open && Boolean(selectedTelephonyContactID),
    staleTime: 30_000,
  });

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

  const selectCompany = (companyId: string, title?: string, parentTitle?: string) => {
    const selectedLabel = String(title || companyId).trim();
    const label = renderCompanyOptionLabel(selectedLabel, parentTitle);
    const option = { value: companyId, label, selectedLabel };
    form.setFieldValue('company_id', companyId);
    setSelectedCompanyOption(option);
    setCompanyOptions((prev) => {
      const exists = prev.some((item) => item.value === companyId);
      return exists ? prev : [option, ...prev];
    });
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
    if (!open) return;
    const currentCallID = String(form.getFieldValue('telephony_call_id') || '').trim();
    if (currentCallID) return;
    if (pendingContext?.call?.id && selectableCalls.some((item) => item.id === pendingContext.call?.id)) {
      form.setFieldValue('telephony_call_id', pendingContext.call.id);
      return;
    }
    if (selectableCalls.length === 1) {
      form.setFieldValue('telephony_call_id', selectableCalls[0].id);
    }
  }, [open, pendingContext?.call?.id, selectableCalls, form]);

  useEffect(() => {
    if (!open) return;
    form.setFieldValue('contact_display', getTelephonyContactLabel(selectedTelephonyCall?.contact, selectedTelephonyCall?.client_phone) || undefined);
  }, [open, selectedTelephonyCall, form]);

  useEffect(() => {
    if (!open) return;
    const currentValue = String(form.getFieldValue('contact_name') || '').trim();
    if (currentValue) return;
    form.setFieldValue('contact_name', selectedTelephonyCall?.contact?.name || undefined);
  }, [open, selectedTelephonyCall?.contact?.name, form]);

  useEffect(() => {
    if (syncWithBitrix === false) {
      form.setFieldValue('bitrix_service_point_id', undefined);
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

  const { data: companyBitrixMapping, isFetching: isCompanyBitrixMappingLoading } = useQuery({
    queryKey: ['company-bitrix-mapping', selectedCompanyId],
    queryFn: () => companiesApi.getBitrixMappingByCompanyID(selectedCompanyId ?? ''),
    enabled: open && isBitrixEnabled && Boolean(selectedCompanyId),
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!open || !isBitrixEnabled) return;
    if (!selectedCompanyId) {
      form.setFieldValue('bitrix_service_point_id', undefined);
      return;
    }
    if (syncWithBitrix === false) return;
    const mappedPointID = companyBitrixMapping?.bitrix_service_point_id;
    if (mappedPointID && mappedPointID > 0) {
      form.setFieldValue('bitrix_service_point_id', mappedPointID);
    }
  }, [open, isBitrixEnabled, selectedCompanyId, syncWithBitrix, companyBitrixMapping, form]);

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
    const iikoWebMeta = item.entity_type === 'Server'
      ? getIikoWebAppLinkMeta((data.iiko_web_link as string | undefined) || (data.ip as string | undefined))
      : null;

    const items = [
      ...(item.entity_type === 'Server' ? [{ label: 'IP', value: formattedServerIp || undefined }] : []),
      { label: 'AnyDesk', value: data.anydesk as string | undefined },
      { label: 'TeamViewer', value: data.teamviewer as string | undefined },
      { label: 'rdp', value: data.rdp as string | undefined },
      { label: 'LM', value: data.litemanager as string | undefined },
      { label: 'RustDesk', value: data.rustdesk as string | undefined },
      ...(item.entity_type === 'Server'
        ? [
          ...(iikoWebMeta ? [{ label: iikoWebMeta.label, value: iikoWebMeta.url, isLink: true }] : []),
          { label: 'Партнёрский портал', value: data.partners_link as string | undefined, isLink: true },
        ]
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
          entityPath: `/servers/${(item.data as { uuid?: string })?.uuid || ''}`,
          connections,
        };
      })
      .filter(Boolean) as Array<{
      key?: string;
      title: string;
      connections: Array<{ label: string; value?: string; isLink?: boolean }>;
      entityPath: string;
    }>;

    const ownGroups = infrastructure
      .map((item) => {
        const connections = resolveConnectionItems(item);
        if (!connections || connections.length === 0) return null;
        return {
          key: (item.data as { uuid?: string })?.uuid,
          title: resolveEquipmentTitle(item),
          entityPath: item.entity_type === 'Server'
            ? `/servers/${(item.data as { uuid?: string })?.uuid || ''}`
            : `/workstations/${(item.data as { uuid?: string })?.uuid || ''}`,
          connections,
        };
      })
      .filter(Boolean) as Array<{
      key?: string;
      title: string;
      connections: Array<{ label: string; value?: string; isLink?: boolean }>;
      entityPath: string;
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

  const telephonyCallOptions = useMemo(() => selectableCalls.map((call) => {
    const phone = getTelephonyContactPhoneDisplay(call.contact, call.client_phone) || 'Номер не определён';
    const contactName = String(call.contact?.name || '').trim();
    const timestamp = call.started_at || call.answered_at || call.completed_at;
    const secondaryParts = [
      timestamp ? new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '',
      call.employee_name || '',
    ].filter(Boolean);

    return {
      value: call.id,
      searchLabel: `${phone} ${contactName}`.trim(),
      label: (
        <Space direction="vertical" size={2} style={{ lineHeight: 1.2 }}>
          <Space size={8} wrap>
            <Text strong>{phone}</Text>
            {contactName ? <Tag color="blue">{contactName}</Tag> : null}
          </Space>
          {secondaryParts.length > 0 ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {secondaryParts.join(' · ')}
            </Text>
          ) : null}
        </Space>
      ),
    };
  }), [selectableCalls]);
  const showTelephonySidebar = Boolean(
    selectedCompanyId
    || selectedTelephonyContactID
    || contactCompanies.length > 0
    || isContactCompaniesLoading,
  );

  const createMutation = useMutation({
    mutationFn: async (values: { company_id: string; type: string; description: string; assignee_id: number; sync_with_bitrix?: boolean; bitrix_service_point_id?: number; contact_name?: string; telephony_call_id?: string }) => {
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
      });
    },
    onSuccess: async (response, values) => {
      const createdTicketID = String(response?.data?.id || '').trim();
      let bindFailed = false;
      const selectedCallID = String(values.telephony_call_id || '').trim();

      if (selectedCallID && createdTicketID) {
        try {
          await telephonyApi.bindCallToTicket(selectedCallID, createdTicketID, values.contact_name?.trim() || undefined);
        } catch {
          bindFailed = true;
        }
      }

      if (selectedCallID && !bindFailed) {
        message.success('Заявка создана и привязана к звонку');
      } else if (bindFailed) {
        message.warning('Заявка создана, но привязка к звонку не выполнилась');
      } else {
        message.success('Заявка создана');
      }

      form.resetFields();
      setCompanySearch('');
      setSelectedCompanyOption(null);
      onClose();
      onCreated?.();
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['telephony'] });
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
      width={showTelephonySidebar ? 980 : 640}
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
          {showTelephonySidebar && (
            <Col xs={24} md={8} xl={7} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT }}>
              <div style={{ maxHeight: '100%', overflowY: 'auto', paddingRight: 4 }}>
                {(selectedTelephonyContactID || isContactCompaniesLoading) && (
                  <Card size="small" title="Компании по номеру" style={{ marginBottom: 12 }}>
                    {isContactCompaniesLoading ? (
                      <div style={{ textAlign: 'center', padding: 16 }}>
                        <Spin />
                      </div>
                    ) : contactCompanies.length === 0 ? (
                      <Empty description="История по номеру пока не найдена" image={Empty.PRESENTED_IMAGE_SIMPLE} />
                    ) : (
                      <Space direction="vertical" size={8} style={{ width: '100%' }}>
                        {contactCompanies.map((item: TelephonyContactCompanyDTO) => (
                          <Button
                            key={item.company_id}
                            type={selectedCompanyId === item.company_id ? 'primary' : 'default'}
                            block
                            style={{ height: 'auto', textAlign: 'left', paddingBlock: 10 }}
                            onClick={() => selectCompany(item.company_id, item.title, item.parent_title)}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%', alignItems: 'flex-start' }}>
                              <Text strong style={{ color: selectedCompanyId === item.company_id ? '#fff' : undefined }}>
                                {item.parent_title ? `${item.parent_title} / ${item.title}` : item.title}
                              </Text>
                              <Text
                                type={selectedCompanyId === item.company_id ? undefined : 'secondary'}
                                style={{ fontSize: 12, color: selectedCompanyId === item.company_id ? '#fff' : undefined }}
                              >
                                Последняя связь: {new Date(item.last_seen_at).toLocaleString()}
                              </Text>
                              <Tag color={item.active_contract === false ? 'default' : 'success'} style={{ marginInlineEnd: 0 }}>
                                {item.active_contract === false ? 'Контракт завершён' : 'Контракт активен'}
                              </Tag>
                            </Space>
                          </Button>
                        ))}
                      </Space>
                    )}
                  </Card>
                )}

                {selectedCompanyId && (
                  <>
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
                                  <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }} ellipsis={{ rows: 4 }}>
                                    {normalizeTicketPreview(ticket.subject || ticket.description) || 'Без описания'}
                                  </Paragraph>
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
                                  <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }} ellipsis={{ rows: 4 }}>
                                    {normalizeTicketPreview(ticket.subject || ticket.description) || 'Без описания'}
                                  </Paragraph>
                                  <Text type="secondary">Обновлено: {ticket.last_activity ? new Date(ticket.last_activity).toLocaleString() : '-'}</Text>
                                </Space>
                              </Card>
                            );
                          })}
                        </Space>
                      </Card>
                    )}
                  </>
                )}
              </div>
            </Col>
          )}

          <Col
            xs={24}
            md={showTelephonySidebar ? (selectedCompanyId ? 8 : 16) : 24}
            xl={showTelephonySidebar ? (selectedCompanyId ? 10 : 17) : 24}
            style={{ maxHeight: MODAL_BODY_MAX_HEIGHT, overflowY: 'auto' }}
          >
            <Row gutter={12}>
              <Col xs={24} lg={8}>
                <Form.Item name="telephony_call_id" label="Номер телефона">
                  <Select
                    allowClear
                    showSearch
                    placeholder="Выберите звонок за последний час"
                    loading={isRecentCallsLoading || isPendingContextLoading}
                    options={telephonyCallOptions}
                    optionFilterProp="searchLabel"
                    notFoundContent={<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Свободных звонков пока нет" />}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} lg={8}>
                <Form.Item name="contact_display" label="Контакт">
                  <Input readOnly placeholder="Контакт будет определён по выбранному номеру" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={8}>
                <Form.Item name="contact_name" label="Имя контакта">
                  <Input placeholder="Уточните имя звонящего" />
                </Form.Item>
              </Col>
            </Row>

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
                  selectCompany(valueStr, selectedLabel, companyMeta[valueStr]?.parent_title);
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
                    loading={isBitrixPointsLoading || isCompanyBitrixMappingLoading}
                    optionFilterProp="label"
                    options={bitrixPointsOptions}
                    disabled={syncWithBitrix === false}
                  />
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
                  <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                    {connectionsGroups.map((group) => (
                      <Card key={group.key || group.title} size="small" className="glass-panel">
                        <Space direction="vertical" size={4} style={{ width: '100%' }}>
                          <a href={group.entityPath} target="_blank" rel="noreferrer">
                            <Text strong>{group.title}</Text>
                          </a>
                          <Space direction="vertical" size={0} style={{ width: '100%' }}>
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

