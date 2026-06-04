import React, { useEffect, useMemo, useState } from 'react';
import { AutoComplete, Form, Input, Modal, Select, Space, Button, message, Row, Col, Card, Empty, Spin, Typography, Tag, Checkbox } from 'antd';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { companiesApi } from '@/api/companies';
import { telephonyApi } from '@/api/telephony';
import { ticketsApi } from '@/api/tickets';
import { usersApi } from '@/api/users';
import type { CompanyModel, InfrastructureItem, TelephonyCallDTO, TelephonyContactCompanyDTO } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { getIikoWebAppLinkMeta, normalizeServerAddress } from '@/utils/formatters';
import { getTelephonyContactPhoneDisplay, getTelephonyContactPhoneForCopy } from '@/utils/telephony';
import { normalizeTicketPreview } from '@/utils/ticketText';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import { getTicketStatusMeta } from '@/constants/ticketStatus';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

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

type CompanyMeta = {
  address?: string;
  additional?: string;
  title?: string;
  parent_title?: string;
  parent_id?: string;
  active_contract?: boolean;
  contract_type?: string;
};

const getContractTypeBadgeMeta = (value?: string) => {
  const raw = String(value || '').trim();
  const normalized = raw.toLowerCase();
  if (normalized.includes('cloud')) {
    return { label: 'TS Cloud', color: 'blue' };
  }
  if (normalized.includes('standart') || normalized.includes('standard')) {
    return { label: 'TS Standart', color: 'green' };
  }
  return { label: raw || 'Тип не указан', color: 'default' };
};

const renderCompanyContractTags = (activeContract?: boolean, contractType?: string) => {
  if (activeContract === false) {
    return <Tag color="default" style={{ marginInlineEnd: 0 }}>Контракт завершён</Tag>;
  }
  if (activeContract !== true && !String(contractType || '').trim()) {
    return <Tag color="default" style={{ marginInlineEnd: 0 }}>Контракт не задан</Tag>;
  }
  const meta = getContractTypeBadgeMeta(contractType);
  return <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>{meta.label}</Tag>;
};

const NewTicketModal: React.FC<Props> = ({ open, onClose, presetCompany, onCreated }) => {
  const { t } = useTranslation(['common', 'tickets']);
  const queryClient = useQueryClient();
  const [form] = Form.useForm();
  const [companySearch, setCompanySearch] = useState('');
  const [companyAppliedSearch, setCompanyAppliedSearch] = useState('');
  const debouncedCompanySearch = useDebouncedValue(companySearch, SELECT_SEARCH_DEBOUNCE_MS);
  const [companyOptions, setCompanyOptions] = useState<Array<{ value: string; label: React.ReactNode; title: string }>>([]);
  const [companyMeta, setCompanyMeta] = useState<Record<string, CompanyMeta>>({});
  const [selectedCompanyOption, setSelectedCompanyOption] = useState<{ value: string; label: React.ReactNode; title: string } | null>(null);
  const selectedCompanyId = Form.useWatch('company_id', form) as string | undefined;
  const selectedTelephonyCallID = Form.useWatch('telephony_call_id', form) as string | undefined;
  const contactPhone = Form.useWatch('contact_phone', form) as string | undefined;
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

  const selectCompany = (companyId: string, title?: string, parentTitle?: string, nextMeta?: CompanyMeta) => {
    const selectedLabel = String(title || companyId).trim();
    const label = renderCompanyOptionLabel(selectedLabel, parentTitle);
    const option = { value: companyId, label, title: selectedLabel };
    form.setFieldValue('company_id', companyId);
    setSelectedCompanyOption(option);
    if (nextMeta) {
      setCompanyMeta((prev) => ({
        ...prev,
        [companyId]: {
          ...prev[companyId],
          ...nextMeta,
          title: nextMeta.title ?? selectedLabel,
          parent_title: nextMeta.parent_title ?? parentTitle,
        },
      }));
    }
    setCompanyOptions((prev) => {
      const exists = prev.some((item) => item.value === companyId);
      return exists ? prev : [option, ...prev];
    });
  };

  const { data: companiesData, isLoading: isCompaniesLoading } = useQuery({
    queryKey: ['companies', companyAppliedSearch],
    queryFn: () => companiesApi.searchCompanies(companyAppliedSearch, 20, 0),
    enabled: open,
    staleTime: 30_000,
  });

  useEffect(() => {
    setCompanyAppliedSearch(debouncedCompanySearch);
  }, [debouncedCompanySearch]);

  useEffect(() => {
    if (!companiesData?.data) return;

    const nextMeta: Record<string, CompanyMeta> = {};
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
        const rawContractType = company.contract_type;
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
          contract_type: rawContractType ?? undefined,
        };
        return {
          value: id,
          label: labelNode,
          title,
        };
      })
      .filter(Boolean) as Array<{ value: string; label: React.ReactNode; title: string }>;

    if (selectedCompanyId && !nextOptions.some((opt) => opt.value === selectedCompanyId)) {
      const fallbackLabel = selectedCompanyOption?.title ?? selectedCompanyId;
      nextOptions.unshift({ value: selectedCompanyId, label: fallbackLabel, title: fallbackLabel });
    }

    setCompanyOptions(nextOptions);
    setCompanyMeta((prev) => ({ ...prev, ...nextMeta }));

  }, [companiesData, selectedCompanyId, selectedCompanyOption]);

  useEffect(() => {
    if (!selectedCompanyId) return;
    const match = companyOptions.find((opt) => opt.value === selectedCompanyId);
    if (match && match.title !== selectedCompanyOption?.title) {
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
      const option = { value: presetCompany.id, label, title: label };
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
      form.setFieldValue('contact_phone', getTelephonyContactPhoneForCopy(pendingContext.contact, pendingContext.call.client_phone));
      return;
    }
    if (selectableCalls.length === 1) {
      form.setFieldValue('telephony_call_id', selectableCalls[0].id);
      form.setFieldValue('contact_phone', getTelephonyContactPhoneForCopy(selectableCalls[0].contact, selectableCalls[0].client_phone));
    }
  }, [open, pendingContext?.call?.id, selectableCalls, form]);

  useEffect(() => {
    if (!open) return;
    const currentValue = String(form.getFieldValue('contact_name') || '').trim();
    if (currentValue) return;
    form.setFieldValue('contact_name', selectedTelephonyCall?.contact?.name || undefined);
  }, [open, selectedTelephonyCall?.contact?.name, form]);

  useEffect(() => {
    if (!open || !selectedTelephonyCall) return;
    const selectedPhone = getTelephonyContactPhoneForCopy(selectedTelephonyCall.contact, selectedTelephonyCall.client_phone);
    if (selectedPhone && !String(contactPhone || '').trim()) {
      form.setFieldValue('contact_phone', selectedPhone);
    }
  }, [open, selectedTelephonyCall, contactPhone, form]);

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
    const rawContractType = company.contract_type;

    setCompanyMeta((prev) => ({
      ...prev,
      [selectedCompanyId]: {
        address: rawAddress ?? undefined,
        additional: rawAdditional ?? undefined,
        title: rawTitle ?? undefined,
        parent_title: rawParentTitle ?? undefined,
        parent_id: rawParentID ?? undefined,
        active_contract: typeof rawActiveContract === 'boolean' ? rawActiveContract : undefined,
        contract_type: rawContractType ?? undefined,
      },
    }));

    if (rawTitle || rawAdditional) {
      const selectedLabel = rawTitle || rawAdditional || selectedCompanyId;
      const label = renderCompanyOptionLabel(selectedLabel, rawParentTitle);
      setCompanyOptions((prev) => {
        const exists = prev.some((opt) => opt.value === selectedCompanyId);
        return exists ? prev : [{ value: selectedCompanyId, label, title: selectedLabel }, ...prev];
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
      t('tickets:fallback.equipment')
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
          {
            label: t('tickets:newTicket.connections.partnerPortal'),
            value: data.partners_link as string | undefined,
            isLink: true,
          },
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
          title: `${resolveEquipmentTitle(item)} (${t('tickets:newTicket.connections.parentCompanySuffix')})`,
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
  }, [infrastructure, parentInfrastructure, t]);

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

  const telephonyPhoneOptions = useMemo(() => selectableCalls.map((call) => {
    const phone = getTelephonyContactPhoneDisplay(call.contact, call.client_phone) || t('tickets:newTicket.fallback.phoneUndefined');
    const phoneValue = getTelephonyContactPhoneForCopy(call.contact, call.client_phone) || phone;
    const contactName = String(call.contact?.name || '').trim();
    const timestamp = call.started_at || call.answered_at || call.completed_at;
    const secondaryParts = [
      timestamp ? new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '',
      call.employee_name || '',
    ].filter(Boolean);

    return {
      value: phoneValue,
      callID: call.id,
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
  }), [selectableCalls, t]);
  const showTelephonySidebar = Boolean(
    selectedCompanyId
    || selectedTelephonyContactID
    || contactCompanies.length > 0
    || isContactCompaniesLoading,
  );

  const createMutation = useMutation({
    mutationFn: async (values: { company_id: string; type: string; description: string; assignee_id: number; sync_with_bitrix?: boolean; bitrix_service_point_id?: number; contact_name?: string; contact_phone?: string; telephony_call_id?: string }) => {
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
      const manualPhone = String(values.contact_phone || '').trim();

      if (selectedCallID && createdTicketID) {
        try {
          await telephonyApi.bindCallToTicket(selectedCallID, createdTicketID, values.contact_name?.trim() || undefined);
        } catch {
          bindFailed = true;
        }
      } else if (manualPhone && createdTicketID) {
        try {
          await telephonyApi.setTicketContact(createdTicketID, {
            phone: manualPhone,
            contact_name: values.contact_name?.trim() || undefined,
          });
        } catch {
          bindFailed = true;
        }
      }

      if (selectedCallID && !bindFailed) {
        message.success(t('tickets:newTicket.messages.createdAndLinked'));
      } else if (manualPhone && !bindFailed) {
        message.success(t('tickets:newTicket.messages.createdAndContactSaved'));
      } else if (bindFailed) {
        message.warning(t('tickets:newTicket.messages.createdLinkWarning'));
      } else {
        message.success(t('tickets:newTicket.messages.created'));
      }

      form.resetFields();
      setCompanySearch('');
      setCompanyAppliedSearch('');
      setSelectedCompanyOption(null);
      onClose();
      onCreated?.();
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['telephony'] });
    },
    onError: () => {
      message.error(t('tickets:newTicket.messages.createError'));
    },
  });

  const handleCancel = () => {
    form.resetFields();
    setCompanySearch('');
    setCompanyAppliedSearch('');
    setSelectedCompanyOption(null);
    onClose();
  };

  return (
    <Modal
      open={open}
      onCancel={handleCancel}
      confirmLoading={createMutation.isPending}
      title={t('tickets:newTicket.modal.title')}
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
          <Button onClick={() => form.submit()} loading={createMutation.isPending}>
            {t('tickets:newTicket.actions.createAnother')}
          </Button>
          <Button type="primary" onClick={() => form.submit()} loading={createMutation.isPending}>
            {t('tickets:newTicket.actions.create')}
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
              title: t('tickets:newTicket.confirm.inactiveContractTitle'),
              content: t('tickets:newTicket.confirm.inactiveContractContent'),
              okText: t('tickets:newTicket.actions.confirmInactiveContract'),
              cancelText: t('common:actions.cancel'),
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
                  <Card
                    size="small"
                    title={t('tickets:newTicket.telephony.companiesByPhone')}
                    style={{ marginBottom: 12 }}
                  >
                    {isContactCompaniesLoading ? (
                      <div style={{ textAlign: 'center', padding: 16 }}>
                        <Spin />
                      </div>
                    ) : contactCompanies.length === 0 ? (
                      <Empty
                        description={t('tickets:newTicket.telephony.historyNotFound')}
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                      />
                    ) : (
                      <Space direction="vertical" size={8} style={{ width: '100%' }}>
                        {contactCompanies.map((item: TelephonyContactCompanyDTO) => (
                          <Button
                            key={item.company_id}
                            type={selectedCompanyId === item.company_id ? 'primary' : 'default'}
                            block
                            style={{ height: 'auto', textAlign: 'left', paddingBlock: 10 }}
                            onClick={() => selectCompany(item.company_id, item.title, item.parent_title, {
                              active_contract: item.active_contract,
                              contract_type: (item as TelephonyContactCompanyDTO & { contract_type?: string }).contract_type,
                            })}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%', alignItems: 'flex-start' }}>
                              <Text strong style={{ color: selectedCompanyId === item.company_id ? '#fff' : undefined }}>
                                {item.parent_title ? `${item.parent_title} / ${item.title}` : item.title}
                              </Text>
                              <Text
                                type={selectedCompanyId === item.company_id ? undefined : 'secondary'}
                                style={{ fontSize: 12, color: selectedCompanyId === item.company_id ? '#fff' : undefined }}
                              >
                                {t('tickets:newTicket.telephony.lastSeen', {
                                  value: new Date(item.last_seen_at).toLocaleString(),
                                })}
                              </Text>
                              {renderCompanyContractTags(
                                item.active_contract,
                                (item as TelephonyContactCompanyDTO & { contract_type?: string }).contract_type,
                              )}
                            </Space>
                          </Button>
                        ))}
                      </Space>
                    )}
                  </Card>
                )}

                {selectedCompanyId && (
                  <>
                    <Card
                      size="small"
                      title={t('tickets:newTicket.telephony.activeCompanyTickets')}
                      style={{ marginBottom: 12 }}
                    >
                      {isActiveTicketsLoading ? (
                        <div style={{ textAlign: 'center', padding: 16 }}>
                          <Spin />
                        </div>
                      ) : activeTickets.length === 0 ? (
                        <Empty description={t('tickets:newTicket.telephony.noActiveTickets')} />
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
                                    {normalizeTicketPreview(ticket.subject || ticket.description) || t('tickets:fallback.noDescription')}
                                  </Paragraph>
                                  <Text type="secondary">
                                    {t('tickets:newTicket.telephony.updatedAt', {
                                      value: ticket.last_activity ? new Date(ticket.last_activity).toLocaleString() : '-',
                                    })}
                                  </Text>
                                </Space>
                              </Card>
                            );
                          })}
                        </Space>
                      )}
                    </Card>

                    {!isResolvedOrClosedTicketsLoading && resolvedOrClosedTickets.length > 0 && (
                      <Card size="small" title={t('tickets:newTicket.telephony.recentTickets')}>
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
                                    {normalizeTicketPreview(ticket.subject || ticket.description) || t('tickets:fallback.noDescription')}
                                  </Paragraph>
                                  <Text type="secondary">
                                    {t('tickets:newTicket.telephony.updatedAt', {
                                      value: ticket.last_activity ? new Date(ticket.last_activity).toLocaleString() : '-',
                                    })}
                                  </Text>
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
            <Row gutter={12} align="top">
              <Col xs={24} lg={12}>
                <Form.Item name="telephony_call_id" hidden>
                  <Input />
                </Form.Item>
                <Form.Item
                  name="contact_phone"
                  label={t('tickets:newTicket.telephony.phoneField')}
                >
                  <AutoComplete
                    allowClear
                    placeholder={t('tickets:newTicket.telephony.phonePlaceholder')}
                    options={telephonyPhoneOptions}
                    filterOption={(inputValue, option) => String(option?.searchLabel || option?.value || '').toLowerCase().includes(inputValue.toLowerCase())}
                    onSelect={(value) => {
                      const option = telephonyPhoneOptions.find((item) => item.value === value);
                      form.setFieldValue('telephony_call_id', option?.callID);
                    }}
                    onChange={(value) => {
                      const option = telephonyPhoneOptions.find((item) => item.value === value);
                      form.setFieldValue('telephony_call_id', option?.callID);
                    }}
                    notFoundContent={(
                      <Empty
                        image={Empty.PRESENTED_IMAGE_SIMPLE}
                        description={t('tickets:newTicket.telephony.noFreeCalls')}
                      />
                    )}
                    disabled={isRecentCallsLoading || isPendingContextLoading}
                  />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12}>
                <Form.Item
                  name="contact_name"
                  label={t('tickets:newTicket.telephony.contactNameField')}
                >
                  <Input placeholder={t('tickets:newTicket.telephony.contactNamePlaceholder')} />
                </Form.Item>
              </Col>
            </Row>

            <Form.Item
              name="company_id"
              label={t('tickets:newTicket.form.company')}
              className="new-ticket-company-field"
              rules={[{ required: true, message: t('tickets:newTicket.form.companyRequired') }]}
            >
              <Select
                showSearch
                placeholder={t('tickets:newTicket.form.companyPlaceholder')}
                onSearch={(value) => {
                  setCompanySearch(value);
                }}
                onInputKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    setCompanyAppliedSearch(companySearch);
                  }
                }}
                loading={isCompaniesLoading}
                filterOption={false}
                autoClearSearchValue
                options={companyOptions}
                optionLabelProp="title"
                value={selectedCompanyId}
                onChange={(value, option) => {
                  const valueStr = String(value);
                  if (!valueStr || valueStr === 'undefined' || valueStr === 'null') {
                    console.warn('[NewTicketModal] onChange invalid value', { value, option });
                    return;
                  }
                  const selectedLabel = (option as { title?: string } | undefined)?.title ?? valueStr;
                  selectCompany(valueStr, selectedLabel, companyMeta[valueStr]?.parent_title);
                }}
              />
            </Form.Item>
            {selectedCompanyMeta && (
              <div style={{ marginTop: -6, marginBottom: 12 }}>
                {renderCompanyContractTags(selectedCompanyMeta.active_contract, selectedCompanyMeta.contract_type)}
                {selectedCompanyMeta.address && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    {t('tickets:newTicket.form.address', { value: selectedCompanyMeta.address })}
                  </Text>
                )}
                {selectedCompanyMeta.additional && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    {t('tickets:newTicket.form.additionalInfo', {
                      value: selectedCompanyMeta.additional,
                    })}
                  </Text>
                )}
                {selectedCompanyMeta.parent_title && (
                  <Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
                    {t('tickets:newTicket.form.companyNetwork', {
                      parent: selectedCompanyMeta.parent_title,
                      child: selectedCompanyMeta.title || selectedCompanyId,
                    })}
                  </Text>
                )}
              </div>
            )}

            <Form.Item
              name="type"
              label={t('tickets:newTicket.form.type')}
              className="new-ticket-type-field"
              rules={[{ required: true, message: t('tickets:newTicket.form.typeRequired') }]}
            >
              <Select
                options={[
                  { value: 'incident', label: t('tickets:newTicket.types.incident') },
                  { value: 'consultation', label: t('tickets:newTicket.types.consultation') },
                  { value: 'cto', label: t('tickets:newTicket.types.cto') },
                  { value: 'acceptance_ao', label: t('tickets:newTicket.types.acceptance_ao') },
                  { value: 'paid_works', label: t('tickets:newTicket.types.paid_works') },
                ]}
              />
            </Form.Item>

            <Form.Item
              name="assignee_id"
              label={t('tickets:newTicket.form.assignee')}
              rules={[{ required: true, message: t('tickets:newTicket.form.assigneeRequired') }]}
            >
              <Select
                showSearch
                placeholder={t('tickets:newTicket.form.assigneePlaceholder')}
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
                  tooltip={canDisableBitrixSync ? undefined : t('tickets:newTicket.bitrix.syncDisabledTooltip')}
                >
                  <Checkbox disabled={!canDisableBitrixSync}>
                    {t('tickets:newTicket.bitrix.syncWithB24')}
                  </Checkbox>
                </Form.Item>

                <Form.Item
                  name="bitrix_service_point_id"
                  label={t('tickets:newTicket.bitrix.servicePoint')}
                  className="new-ticket-bitrix-point-field"
                  rules={[
                    {
                      validator: (_, value) => {
                        if (syncWithBitrix === false) return Promise.resolve();
                        if (value === undefined || value === null || value === '') {
                          return Promise.reject(new Error(t('tickets:newTicket.bitrix.servicePointRequired')));
                        }
                        return Promise.resolve();
                      },
                    },
                  ]}
                >
                  <Select
                    showSearch
                    placeholder={t('tickets:newTicket.bitrix.servicePointPlaceholder')}
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
              label={t('tickets:newTicket.form.description')}
              rules={[{ required: true, message: t('tickets:newTicket.form.descriptionRequired') }]}
            >
              <Input.TextArea rows={4} placeholder={t('tickets:newTicket.form.descriptionPlaceholder')} />
            </Form.Item>
          </Col>

          {selectedCompanyId && (
            <Col xs={24} md={8} xl={7} style={{ maxHeight: MODAL_BODY_MAX_HEIGHT }}>
              <Card
                size="small"
                title={t('tickets:newTicket.connections.title')}
                bodyStyle={{ maxHeight: `calc(${MODAL_BODY_MAX_HEIGHT} - 56px)`, overflowY: 'auto' }}
              >
                {isInfrastructureLoading || isParentInfrastructureLoading ? (
                  <div style={{ textAlign: 'center', padding: 16 }}>
                    <Spin />
                  </div>
                ) : connectionsGroups.length === 0 ? (
                  <Empty description={t('tickets:newTicket.connections.empty')} />
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
