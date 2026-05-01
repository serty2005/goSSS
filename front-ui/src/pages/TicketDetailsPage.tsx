import React, { Suspense, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, DatePicker, Descriptions, Empty, Grid, Input, List, Modal, Popconfirm, Select, Space, Spin, Tabs, Tag, Tooltip, Typography, Upload, message } from 'antd';
import { CheckOutlined, CloseOutlined, CopyOutlined, EditOutlined, LinkOutlined, PaperClipOutlined, PlusOutlined } from '@ant-design/icons';
import type { UploadProps } from 'antd';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { telephonyApi } from '@/api/telephony';
import { profileApi } from '@/api/profile';
import { companiesApi } from '@/api/companies';
import { contractsApi } from '@/api/contracts';
import { usersApi } from '@/api/users';
import { agentObservationsApi } from '@/api/agentObservations';
import { AgentObservationFeedRowDTO, CompanyModel, FiscalEntity, InfrastructureItem, ServerEntity, TelephonyCallDTO, TicketDetailsDTO, TicketHistoryDTO, TicketListItemDTO, TicketStatus, WorkstationEntity } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import SmartTicketEditor from '@/features/tickets/editor/SmartTicketEditor';
import { hasEditorContent } from '@/features/tickets/editor/content';
import type { MentionOption } from '@/features/tickets/editor/mentions';
import { SafeHtmlContent } from '@/utils/safeHtml';
import { getTelephonyContactLabel, getTelephonyContactPhoneDisplay, getTelephonyContactPhoneForCopy } from '@/utils/telephony';
import { useBackNavigation } from '@/hooks/useBackNavigation';
import { useAuthStore } from '@/store/authStore';
import { isClosedLikeTicketStatus, TICKET_STATUS_OPTIONS } from '@/constants/ticketStatus';
import AgentObservationRawModal from '@/components/agents/AgentObservationRawModal';

const { Title, Text } = Typography;
const { useBreakpoint } = Grid;
const LazyNewTicketModal = React.lazy(() => import('@/components/tickets/NewTicketModal'));
type UploadRequestOption = Parameters<NonNullable<UploadProps['customRequest']>>[0];

const formatDeferredDateTime = (value?: string) => {
  if (!value) return '';
  const dt = dayjs(value);
  if (!dt.isValid()) return '';
  return dt.format('DD.MM.YYYY HH:mm');
};
const FIELD_HIGHLIGHT_MS = 2600;
const fieldHighlightStyle: React.CSSProperties = {
  background: 'rgba(250, 173, 20, 0.22)',
  transition: 'background-color 0.35s ease',
  borderRadius: 6,
  padding: '2px 6px',
};

const copyTicketPhone = async (phone: string) => {
  if (!phone) {
    message.warning('Телефон не найден');
    return;
  }
  await navigator.clipboard.writeText(phone);
  message.success('Телефон скопирован');
};

const historyLabel = (entry: TicketHistoryDTO) => {
  switch (entry.action) {
    case 'comment_added':
      return 'Комментарий добавлен';
    case 'comment_updated':
      return 'Комментарий изменён';
    case 'comment_deleted':
      return 'Комментарий удалён';
    case 'connection_copied':
      return 'Скопировано подключение';
    case 'field_changed':
    default:
      if (entry.field === 'status') return 'Изменён статус';
      if (entry.field === 'description') return 'Изменено описание';
      if (entry.field === 'assignee') return 'Изменён исполнитель';
      if (entry.field === 'company') return 'Изменена компания';
      if (entry.field === 'asset') return 'Изменено оборудование';
      if (entry.field === 'bitrix_link') return 'Изменена связь с Bitrix24';
      return 'Изменение заявки';
  }
};

const historySourceLabel = (source?: string) => {
  if (source === 'ui') return 'UI';
  if (source === 'bitrix') return 'Bitrix24';
  if (source === 'servicedesk') return 'ServiceDesk';
  return 'System';
};

const historyActorLabel = (entry: TicketHistoryDTO) => {
  if (entry.user_name) return entry.user_name;
  if (entry.source === 'bitrix') return 'Bitrix24';
  if (entry.source === 'servicedesk') return 'ServiceDesk';
  if (entry.source === 'ui') return 'Сотрудник';
  return 'Система';
};

const resolveTicketCreatedSource = (metadata?: TicketDetailsDTO['metadata']) => {
  if (!metadata) return 'system';
  const sdUUID = String(metadata.service_desk_uuid || '').trim();
  if (sdUUID.startsWith('b24:deal:')) return 'bitrix';
  if (metadata.reporter_id && metadata.reporter_id > 0) return 'ui';
  if (sdUUID) return 'servicedesk';
  return 'system';
};

const hasPyrusLink = (metadata?: TicketDetailsDTO['metadata']) => {
  if (!metadata) return false;
  if (metadata.pyrus_task_id && metadata.pyrus_task_id > 0) return true;
  if (String(metadata.pyrus_task_url || '').trim()) return true;
  return String(metadata.service_desk_uuid || '').trim().startsWith('pyrus:task:');
};

const resolveEntityTitle = (item: InfrastructureItem) => {
  const dataRow = item.data as Record<string, string | undefined>;
  return (
    dataRow.device_name ||
    dataRow.server_name ||
    dataRow.model_kkt ||
    dataRow.serial_number ||
    dataRow.rn_kkt ||
    dataRow.uuid ||
    'Оборудование'
  );
};

const ExternalLinkBadge: React.FC<{
  label: string;
  href?: string;
  title: string;
  color: string;
}> = ({ label, href, title, color }) => {
  if (!String(href || '').trim()) {
    return null;
  }
  return (
    <Tooltip title={title}>
      <a href={href} target="_blank" rel="noreferrer" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        <Tag color={color} style={{ marginInlineEnd: 0 }}>{label}</Tag>
        <LinkOutlined />
      </a>
    </Tooltip>
  );
};

type InfraDataRecord = Record<string, string | undefined>;

type AgentEquipmentGroup = {
  key: string;
  agentID: string;
  agentType: string;
  workstations: InfrastructureItem[];
  fiscals: InfrastructureItem[];
};

type CopyFieldEntityType = 'Server' | 'Workstation';

type CopyValueParams = {
  key: string;
  label: string;
  value?: string;
  displayValue?: string;
  entityType?: CopyFieldEntityType;
  entityID?: string;
  connectionField?: string;
  successText?: string;
};

const toInfraData = (item: InfrastructureItem) => item.data as InfraDataRecord;

const normalizeTextValue = (value: unknown) => String(value || '').trim();

const UUID_LIKE_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const getAgentTypeLabel = (value?: string | null) => {
  const normalized = normalizeTextValue(value);
  return normalized || 'agent';
};

const getAgentSourceMeta = (data: {
  last_updated_by?: string | null;
  last_modified_date?: string | null;
  updated_at?: string | null;
}) => {
  const raw = normalizeTextValue(data.last_updated_by);
  const lowerRaw = raw.toLowerCase();
  const hasAgentID = UUID_LIKE_REGEX.test(raw) || (lowerRaw.startsWith('agent-') && raw.length > 'agent-'.length);
  const explicitType = normalizeTextValue((data as Record<string, unknown>).agent_type);
  const agentType = explicitType || (hasAgentID || lowerRaw.startsWith('agent') ? 'agent' : '');
  const agentID = hasAgentID ? raw : '';
  return {
    raw,
    agentID,
    agentType,
    updatedAt: normalizeTextValue(data.last_modified_date || data.updated_at),
  };
};

const isDigitsOnly = (value: string) => /^\d+$/.test(value.trim());

const shortenAddress = (value: string) => {
  const trimmed = value.trim();
  if (trimmed.length <= 48) {
    return trimmed;
  }
  return `${trimmed.slice(0, 45)}...`;
};

const getLookupErrorMessage = (error: unknown) => {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return 'Не удалось найти последнее наблюдение агента';
};

const TicketDetailsPage: React.FC = () => {
  const screens = useBreakpoint();
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const [commentDraft, setCommentDraft] = useState('');
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [editingCommentUUID, setEditingCommentUUID] = useState('');
  const [editingCommentDraft, setEditingCommentDraft] = useState('');
  const [statusComment, setStatusComment] = useState('');
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [pendingDeferredAt, setPendingDeferredAt] = useState('');
  const [companySearch, setCompanySearch] = useState('');
  const [isCompanyEditMode, setIsCompanyEditMode] = useState(false);
  const [draftCompanyID, setDraftCompanyID] = useState<string | undefined>(undefined);
  const [isBitrixEditMode, setIsBitrixEditMode] = useState(false);
  const [isBitrixSyncModalOpen, setIsBitrixSyncModalOpen] = useState(false);
  const [draftBitrixPointID, setDraftBitrixPointID] = useState<number | undefined>(undefined);
  const [draftBitrixDealTitle, setDraftBitrixDealTitle] = useState('');
  const [isDescriptionEditMode, setIsDescriptionEditMode] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState('');
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isAttachCallModalOpen, setIsAttachCallModalOpen] = useState(false);
  const [isContactEditModalOpen, setIsContactEditModalOpen] = useState(false);
  const [contactPhoneDraft, setContactPhoneDraft] = useState('');
  const [contactNameDraft, setContactNameDraft] = useState('');
  const [attachCallPhoneSearch, setAttachCallPhoneSearch] = useState('');
  const [attachCallEmployeeID, setAttachCallEmployeeID] = useState<number | undefined>(undefined);
  const [highlightedFields, setHighlightedFields] = useState<Record<string, boolean>>({});
  const [highlightedComments, setHighlightedComments] = useState<Record<string, boolean>>({});
  const [copiedFieldKey, setCopiedFieldKey] = useState('');
  const [observationAgentID, setObservationAgentID] = useState('');
  const previousMetadataRef = useRef<TicketDetailsDTO['metadata'] | undefined>(undefined);
  const previousCommentsRef = useRef<Array<{ uuid: string; text: string; creation_date: string }>>([]);
  const clearFieldTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const clearCommentTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const createParam = searchParams.get('create') || '';
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const goBack = useBackNavigation('/tickets');
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const commentsNewFirst = ((user?.profile_config as any)?.tickets?.comments_new_first) !== false;
  const ticketSubscriptions = useMemo<string[]>(() => {
    const list = (user?.profile_config as any)?.tickets?.subscriptions;
    if (!Array.isArray(list)) return [];
    return list.map((item: unknown) => String(item).trim()).filter(Boolean);
  }, [user?.profile_config]);

  useEffect(() => {
    if (createParam === '1') {
      setIsCreateOpen(true);
    }
  }, [createParam]);

  const { data, isLoading } = useQuery({
    queryKey: ['ticket', id],
    queryFn: () => ticketsApi.getTicket(id),
    enabled: Boolean(id),
  });

  const details: TicketDetailsDTO | undefined = data?.data;
  const metadata = details?.metadata;
  const ticketCalls = useMemo(() => details?.calls || [], [details?.calls]);
  const isManagerFlowLocked = metadata?.status === 'to_manager';
  const isPyrusLinkedTicket = hasPyrusLink(metadata);
  const serviceInfoColumns = screens.lg ? 2 : 1;
  const userRoles = user?.roles || [];
  const isAdminRole = userRoles.includes('admin');
  const isDeleteBlockedRole = userRoles.includes('support_specialist') || userRoles.includes('intern');
  const isCommentAuthor = (authorName?: string) => String(authorName || '').trim() === String(user?.full_name || '').trim();
  const canManageComment = (authorName?: string) => isAdminRole || isCommentAuthor(authorName);
  const canDeleteComment = (authorName?: string) => isAdminRole || (!isDeleteBlockedRole && isCommentAuthor(authorName));

  const markFieldChanged = (field: string) => {
    setHighlightedFields((prev) => ({ ...prev, [field]: true }));
    const existingTimer = clearFieldTimersRef.current[field];
    if (existingTimer) {
      clearTimeout(existingTimer);
    }
    clearFieldTimersRef.current[field] = setTimeout(() => {
      setHighlightedFields((prev) => {
        if (!prev[field]) return prev;
        const next = { ...prev };
        delete next[field];
        return next;
      });
      delete clearFieldTimersRef.current[field];
    }, FIELD_HIGHLIGHT_MS);
  };

  const markCommentChanged = (commentID: string) => {
    if (!commentID) return;
    setHighlightedComments((prev) => ({ ...prev, [commentID]: true }));
    const existingTimer = clearCommentTimersRef.current[commentID];
    if (existingTimer) {
      clearTimeout(existingTimer);
    }
    clearCommentTimersRef.current[commentID] = setTimeout(() => {
      setHighlightedComments((prev) => {
        if (!prev[commentID]) return prev;
        const next = { ...prev };
        delete next[commentID];
        return next;
      });
      delete clearCommentTimersRef.current[commentID];
    }, FIELD_HIGHLIGHT_MS);
  };

  useEffect(() => {
    if (!metadata) {
      return;
    }
    const previous = previousMetadataRef.current;
    if (previous) {
      if (previous.status !== metadata.status) markFieldChanged('status');
      if ((previous.description || '') !== (metadata.description || '')) markFieldChanged('description');
      if ((previous.result || '') !== (metadata.result || '')) markFieldChanged('result');
      if ((previous.company_id || '') !== (metadata.company_id || '')) markFieldChanged('company');
      if ((previous.assignee?.id || 0) !== (metadata.assignee?.id || 0)) markFieldChanged('assignee');
      if ((previous.bitrix_deal_title || '') !== (metadata.bitrix_deal_title || '')) markFieldChanged('bitrix_deal_title');
      if ((previous.bitrix_service_point_id || 0) !== (metadata.bitrix_service_point_id || 0)) markFieldChanged('bitrix_service_point');
    }
    previousMetadataRef.current = metadata;
  }, [metadata]);

  useEffect(() => {
    const comments = (details?.comments || []).map((item) => ({
      uuid: item.uuid,
      text: item.text || '',
      creation_date: item.creation_date,
    }));
    const previous = previousCommentsRef.current;
    if (previous.length > 0 && comments.length > 0) {
      const previousMap = new Map(previous.map((item) => [item.uuid, item]));
      comments.forEach((item) => {
        const old = previousMap.get(item.uuid);
        if (!old || old.text !== item.text || old.creation_date !== item.creation_date) {
          markCommentChanged(item.uuid);
        }
      });
    }
    previousCommentsRef.current = comments;
  }, [details?.comments]);

  useEffect(() => {
    if (!editingCommentUUID) return;
    const current = (details?.comments || []).find((item) => item.uuid === editingCommentUUID);
    if (!current) {
      setEditingCommentUUID('');
      setEditingCommentDraft('');
    }
  }, [details?.comments, editingCommentUUID]);

  useEffect(() => () => {
    Object.values(clearFieldTimersRef.current).forEach((timer) => clearTimeout(timer));
    Object.values(clearCommentTimersRef.current).forEach((timer) => clearTimeout(timer));
  }, []);

  const { data: infraResponse, isLoading: isInfraLoading } = useQuery({
    queryKey: ['company-infra', metadata?.company_id],
    queryFn: () => companiesApi.getInfrastructure(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const infrastructure = useMemo(() => infraResponse?.data || [], [infraResponse?.data]);

  const { data: companyResponse } = useQuery({
    queryKey: ['company-profile', metadata?.company_id],
    queryFn: () => companiesApi.getCompany(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const parentCompanyID = useMemo(() => {
    const companyData = companyResponse?.data as { parent_id?: string } | undefined;
    const value = String(companyData?.parent_id || '').trim();
    if (!value || value === metadata?.company_id) {
      return '';
    }
    return value;
  }, [companyResponse?.data, metadata?.company_id]);

  const { data: parentInfraResponse, isLoading: isParentInfraLoading } = useQuery({
    queryKey: ['company-parent-infra', parentCompanyID],
    queryFn: () => companiesApi.getInfrastructure(parentCompanyID),
    enabled: Boolean(parentCompanyID),
    staleTime: 30_000,
  });

  const parentInfrastructure = useMemo(() => parentInfraResponse?.data || [], [parentInfraResponse?.data]);
  const { data: attachableCallsResponse, isFetching: isAttachableCallsLoading } = useQuery({
    queryKey: ['telephony', 'ticket-attachable-calls', id, isAttachCallModalOpen, attachCallPhoneSearch, attachCallEmployeeID],
    queryFn: () => {
      const now = new Date();
      return telephonyApi.getCalls({
        only_without_ticket: true,
        client_phone: attachCallPhoneSearch.trim() || undefined,
        employee_user_id: attachCallEmployeeID,
        started_from: new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString(),
        started_to: now.toISOString(),
        limit: 100,
      });
    },
    enabled: isAttachCallModalOpen && isAdminRole,
    staleTime: 15_000,
  });
  const attachableCalls = useMemo(() => attachableCallsResponse?.items || [], [attachableCallsResponse?.items]);

  const companyTitle = useMemo(() => {
    const companyData = companyResponse?.data as { title?: string; additional_name?: string } | undefined;
    return (
      companyData?.title ||
      companyData?.additional_name ||
      details?.company_name ||
      metadata?.company_name ||
      metadata?.company_id ||
      ''
    );
  }, [companyResponse?.data, details?.company_name, metadata?.company_id, metadata?.company_name]);

  const contractID = metadata?.contract_id
    || (companyResponse?.data as { contract_id?: string } | undefined)?.contract_id;
  const { data: contractResponse } = useQuery({
    queryKey: ['contract', contractID],
    queryFn: () => contractsApi.getContract(contractID || ''),
    enabled: Boolean(contractID),
    staleTime: 60_000,
  });
  const contractType = useMemo(() => {
    const companyContractType = (companyResponse?.data as { contract_type?: string } | undefined)?.contract_type;
    const rawServices = contractResponse?.data?.services;
    if (Array.isArray(rawServices) && rawServices.length > 0 && String(rawServices[0]).trim() !== '') {
      return String(rawServices[0]).trim();
    }
    return companyContractType || '-';
  }, [companyResponse?.data, contractResponse?.data?.services]);

  const { data: usersResponse } = useQuery({
    queryKey: ['users-assignees'],
    queryFn: () => usersApi.getAssignees(),
    retry: false,
    staleTime: 60_000,
  });
  const assigneeOptions = useMemo(
    () =>
      (usersResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({ value: item.id, label: item.full_name || item.username })),
    [usersResponse?.data],
  );
  const attachCallEmployeeOptions = useMemo(
    () => assigneeOptions.map((item) => ({ value: Number(item.value), label: String(item.label) })),
    [assigneeOptions],
  );
  const mentionOptions = useMemo<MentionOption[]>(
    () => assigneeOptions.map((item) => ({ id: Number(item.value), label: String(item.label) })),
    [assigneeOptions],
  );
  const { data: bitrixServicePoints = [] } = useQuery({
    queryKey: ['bitrix-service-points'],
    queryFn: () => ticketsApi.getBitrixServicePoints(),
    enabled: isBitrixEnabled,
    staleTime: 5 * 60_000,
  });

  const bitrixPointName = useMemo(() => {
    if (!metadata?.bitrix_service_point_id) return '-';
    const point = bitrixServicePoints.find((item) => item.b24_element_id === metadata.bitrix_service_point_id);
    return point?.name || String(metadata.bitrix_service_point_id);
  }, [bitrixServicePoints, metadata?.bitrix_service_point_id]);

  const commentsOrdered = useMemo(() => {
    const source = [...(details?.comments || [])];
    source.sort((a, b) => {
      const delta = dayjs(a.creation_date).valueOf() - dayjs(b.creation_date).valueOf();
      return commentsNewFirst ? -delta : delta;
    });
    return source;
  }, [commentsNewFirst, details?.comments]);

  const { data: latestCompanyTicketsResponse, isLoading: isLatestCompanyTicketsLoading } = useQuery({
    queryKey: ['tickets', 'company-latest', metadata?.company_id],
    queryFn: () => ticketsApi.getTickets({
      company_id: metadata?.company_id,
      limit: 8,
      offset: 0,
      archive_mode: 'all',
    }),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const latestCompanyTickets = useMemo(() => {
    return (latestCompanyTicketsResponse?.data || [])
      .filter((item) => item.id !== metadata?.id)
      .slice(0, 8);
  }, [latestCompanyTicketsResponse?.data, metadata?.id]);

  const {
    data: latestAgentObservationResponse,
    isFetching: isAgentObservationLookupLoading,
    isError: isAgentObservationLookupError,
    error: agentObservationLookupError,
  } = useQuery({
    queryKey: ['agent-observations', 'latest-by-agent', observationAgentID],
    queryFn: () => agentObservationsApi.listFeed({
      agent_uuid: observationAgentID,
      sort_by: 'latest',
      order: 'desc',
      limit: 1,
    }),
    enabled: Boolean(observationAgentID),
    staleTime: 15_000,
  });

  const latestAgentObservation = useMemo<AgentObservationFeedRowDTO | undefined>(
    () => latestAgentObservationResponse?.data?.[0],
    [latestAgentObservationResponse?.data],
  );

  const openBitrixSyncModal = () => {
    if (!metadata) {
      return;
    }
    setDraftBitrixPointID(metadata.bitrix_service_point_id ?? undefined);
    setDraftBitrixDealTitle(metadata.bitrix_deal_title || '');
    setIsBitrixSyncModalOpen(true);
  };

  useEffect(() => {
    if (!metadata || isBitrixEditMode) {
      return;
    }
    setDraftBitrixPointID(metadata.bitrix_service_point_id ?? undefined);
    setDraftBitrixDealTitle(metadata.bitrix_deal_title || '');
  }, [isBitrixEditMode, metadata]);

  useEffect(() => {
    if (!metadata || isDescriptionEditMode) {
      return;
    }
    setDescriptionDraft(metadata.description || '');
  }, [isDescriptionEditMode, metadata]);

  const { data: companiesData, isLoading: isCompaniesLoading } = useQuery({
    queryKey: ['ticket-companies', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 30_000,
  });

  const companySelectOptions = useMemo(() => {
    const list = companiesData?.data || [];
    const renderOptionLabel = (title: string, parentTitle?: string) => {
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
    const options = list
      .map((company) => {
        const item = company as CompanyModel;
        const companyID = resolveCompanyID(item);
        if (!companyID) return null;
        const title = resolveCompanyTitle(item) || companyID;
        const parentTitle = resolveCompanyParentTitle(item);
        return {
          value: companyID,
          label: renderOptionLabel(title, parentTitle),
          selectedLabel: title,
        };
      })
      .filter(Boolean) as Array<{ value: string; label: React.ReactNode; selectedLabel: string }>;

    if (metadata?.company_id && !options.some((item) => item.value === metadata.company_id)) {
      options.unshift({
        value: metadata.company_id,
        label: companyTitle || metadata.company_id,
        selectedLabel: companyTitle || metadata.company_id,
      });
    }
    return options;
  }, [companiesData?.data, metadata, companyTitle]);

  const serverItems = useMemo(() => infrastructure.filter((item) => item.entity_type === 'Server'), [infrastructure]);
  const parentServerItems = useMemo(() => parentInfrastructure.filter((item) => item.entity_type === 'Server'), [parentInfrastructure]);
  const workstationItems = useMemo(() => infrastructure.filter((item) => item.entity_type === 'Workstation'), [infrastructure]);
  const fiscalItems = useMemo(() => infrastructure.filter((item) => item.entity_type === 'FiscalRegister'), [infrastructure]);

  const agentEquipmentGroups = useMemo(() => {
    const groups = new Map<string, AgentEquipmentGroup>();
    const ensureGroup = (key: string, agentID: string, agentType: string) => {
      const existing = groups.get(key);
      if (existing) {
        return existing;
      }
      const next: AgentEquipmentGroup = {
        key,
        agentID,
        agentType,
        workstations: [],
        fiscals: [],
      };
      groups.set(key, next);
      return next;
    };

    workstationItems.forEach((item) => {
      const sourceMeta = getAgentSourceMeta(item.data as WorkstationEntity);
      if (!sourceMeta.agentID) {
        return;
      }
      const group = ensureGroup(`agent-${sourceMeta.agentID}`, sourceMeta.agentID, sourceMeta.agentType);
      group.workstations.push(item);
    });

    fiscalItems.forEach((item) => {
      const sourceMeta = getAgentSourceMeta(item.data as FiscalEntity);
      if (!sourceMeta.agentID) {
        return;
      }
      const group = ensureGroup(`agent-${sourceMeta.agentID}`, sourceMeta.agentID, sourceMeta.agentType);
      group.fiscals.push(item);
    });

    return Array.from(groups.values()).sort((left, right) => {
      const leftTitle = left.agentID || resolveEntityTitle(left.workstations[0] || left.fiscals[0]);
      const rightTitle = right.agentID || resolveEntityTitle(right.workstations[0] || right.fiscals[0]);
      return leftTitle.localeCompare(rightTitle, 'ru');
    });
  }, [fiscalItems, workstationItems]);

  const groupedWorkstationIDs = useMemo(() => {
    const ids = new Set<string>();
    agentEquipmentGroups.forEach((group) => {
      group.workstations.forEach((item) => {
        const entityID = normalizeTextValue(toInfraData(item).uuid);
        if (entityID) ids.add(entityID);
      });
    });
    return ids;
  }, [agentEquipmentGroups]);

  const groupedFiscalIDs = useMemo(() => {
    const ids = new Set<string>();
    agentEquipmentGroups.forEach((group) => {
      group.fiscals.forEach((item) => {
        const entityID = normalizeTextValue(toInfraData(item).uuid);
        if (entityID) ids.add(entityID);
      });
    });
    return ids;
  }, [agentEquipmentGroups]);

  const ungroupedWorkstationItems = useMemo(
    () => workstationItems.filter((item) => !groupedWorkstationIDs.has(normalizeTextValue(toInfraData(item).uuid))),
    [groupedWorkstationIDs, workstationItems],
  );

  const ungroupedFiscalItems = useMemo(
    () => fiscalItems.filter((item) => !groupedFiscalIDs.has(normalizeTextValue(toInfraData(item).uuid))),
    [fiscalItems, groupedFiscalIDs],
  );

  const attachments = useMemo(() => {
    return (details?.attachments || [])
      .map((raw) => {
        const item = raw as Record<string, unknown>;
        return {
          id: String(item.id || ''),
          fileName: String(item.file_name || item.FileName || 'Файл'),
          filePath: String(item.file_path || item.FilePath || '')
            .replace(/^\/static\//, '/api/static/')
            .replace(/^static\//, '/api/static/'),
          mimeType: String(item.mime_type || item.MimeType || ''),
        };
      })
      .filter((item) => item.filePath !== '');
  }, [details?.attachments]);

  const addCommentMutation = useMutation({
    mutationFn: async () => {
      if (!id || !hasEditorContent(commentDraft)) return;
      return ticketsApi.addComment(id, commentDraft, commentIsPrivate);
    },
    onSuccess: () => {
      message.success('Комментарий добавлен');
      setCommentDraft('');
      setCommentIsPrivate(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось добавить комментарий'),
  });

  const updateCommentMutation = useMutation({
    mutationFn: async () => {
      if (!id || !editingCommentUUID || !hasEditorContent(editingCommentDraft)) return;
      return ticketsApi.updateComment(id, editingCommentUUID, editingCommentDraft);
    },
    onSuccess: () => {
      message.success('Комментарий обновлён');
      setEditingCommentUUID('');
      setEditingCommentDraft('');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить комментарий'),
  });

  const deleteCommentMutation = useMutation({
    mutationFn: async (commentUUID: string) => {
      if (!id || !commentUUID) return;
      return ticketsApi.deleteComment(id, commentUUID);
    },
    onSuccess: () => {
      message.success('Комментарий удалён');
      setEditingCommentUUID('');
      setEditingCommentDraft('');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось удалить комментарий'),
  });

  const updateDescriptionMutation = useMutation({
    mutationFn: async () => {
      if (!id) return;
      return ticketsApi.updateDescription(id, descriptionDraft);
    },
    onSuccess: () => {
      message.success('Описание обновлено');
      setIsDescriptionEditMode(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить описание'),
  });

  const uploadAttachmentsMutation = useMutation({
    mutationFn: async (files: File[]) => {
      if (!id) {
        throw new Error('Отсутствует ID заявки');
      }
      return ticketsApi.uploadAttachments(id, files);
    },
    onSuccess: (response) => {
      const count = response.data?.items?.length || 0;
      message.success(count > 0 ? `Файлы загружены: ${count}` : 'Файлы загружены');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось загрузить файлы'),
  });

  const changeStatusMutation = useMutation({
    mutationFn: async (payload: { id: string; status: TicketStatus; comment?: string; deferredUntil?: string }) =>
      ticketsApi.changeStatus(payload.id, payload.status, payload.comment, payload.deferredUntil),
    onSuccess: () => {
      message.success('Статус обновлён');
      setPendingStatus(null);
      setStatusComment('');
      setPendingDeferredAt('');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить статус'),
  });

  const changeCompanyMutation = useMutation({
    mutationFn: async (nextCompanyID: string) => {
      if (!id) return;
      return ticketsApi.changeCompany(id, nextCompanyID);
    },
    onSuccess: () => {
      message.success('Компания обновлена');
      setIsCompanyEditMode(false);
      setCompanySearch('');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['company-infra'] });
      queryClient.invalidateQueries({ queryKey: ['company-parent-infra'] });
      queryClient.invalidateQueries({ queryKey: ['company-profile'] });
    },
    onError: () => message.error('Не удалось обновить компанию'),
  });

  const assignMutation = useMutation({
    mutationFn: async (nextAssigneeID?: number) => {
      if (!id) return;
      return ticketsApi.assign(id, nextAssigneeID);
    },
    onSuccess: () => {
      message.success('Исполнитель обновлён');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить исполнителя'),
  });

  const updateProfileConfigMutation = useMutation({
    mutationFn: (config: Record<string, unknown>) => profileApi.updateConfig({ profile_config: config as any }),
    onSuccess: (response) => {
      const dtoUser = (response as any)?.data;
      if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
        setUser(dtoUser as any);
      }
    },
  });

  const toggleTicketSubscription = async () => {
    if (!user || !id) {
      return;
    }
    const current = ticketSubscriptions;
    const exists = current.includes(id);
    const nextSubscriptions = exists
      ? current.filter((item) => item !== id)
      : [...current, id];
    const nextConfig = {
      ...(user.profile_config || {}),
      tickets: {
        ...((user.profile_config || {}).tickets || {}),
        subscriptions: nextSubscriptions,
      },
    };
    setUser({ ...user, profile_config: nextConfig as any });
    try {
      await updateProfileConfigMutation.mutateAsync(nextConfig as any);
      message.success(exists ? 'Подписка на тикет отключена' : 'Подписка на тикет включена');
    } catch {
      setUser(user);
      message.error('Не удалось изменить подписку на тикет');
    }
  };

  const updateBitrixMutation = useMutation({
    mutationFn: async () => {
      if (!id) return;
      return ticketsApi.updateBitrixFields(id, {
        bitrix_service_point_id: draftBitrixPointID,
        bitrix_deal_title: draftBitrixDealTitle.trim(),
      });
    },
    onSuccess: () => {
      message.success('Поля Bitrix24 обновлены');
      setIsBitrixEditMode(false);
      setIsBitrixSyncModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить поля Bitrix24'),
  });

  const unlinkBitrixMutation = useMutation({
    mutationFn: async () => {
      if (!id) return;
      return ticketsApi.unlinkFromBitrix(id);
    },
    onSuccess: () => {
      message.success('Связь с Bitrix24 разорвана');
      setIsBitrixEditMode(false);
      setIsBitrixSyncModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось разорвать связь с Bitrix24'),
  });

  const deleteTicketMutation = useMutation({
    mutationFn: async () => {
      if (!id) return;
      return ticketsApi.deleteTicket(id);
    },
    onSuccess: () => {
      message.success('Тикет удалён');
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      navigate('/tickets');
    },
    onError: () => message.error('Не удалось удалить тикет'),
  });

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: {
      label: string;
      value: string;
      entityType?: 'Server' | 'Workstation';
      entityID?: string;
      connectionField?: string;
    }) => {
      if (!id) return;
      return ticketsApi.recordConnectionCopy(
        id,
        payload.label,
        payload.value,
        payload.entityType,
        payload.entityID,
        payload.connectionField,
      );
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['ticket-connection-stats', id] });
    },
  });
  const bindTicketCallMutation = useMutation({
    mutationFn: async (call: TelephonyCallDTO) => {
      if (!id) return;
      return telephonyApi.bindCallToTicket(
        call.id,
        id,
        String(call.contact?.name || '').trim() || undefined,
      );
    },
    onSuccess: () => {
      message.success('Звонок привязан к тикету');
      setIsAttachCallModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['telephony'] });
    },
    onError: () => {
      message.error('Не удалось привязать звонок');
    },
  });

  const unbindTicketCallMutation = useMutation({
    mutationFn: async (call: TelephonyCallDTO) => {
      if (!id) return;
      return telephonyApi.unbindCallFromTicket(call.id, id);
    },
    onSuccess: () => {
      message.success('Звонок отвязан от тикета');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['telephony'] });
    },
    onError: () => {
      message.error('Не удалось отвязать звонок');
    },
  });

  const updateTicketContactMutation = useMutation({
    mutationFn: async (payload: { phone?: string; contactName?: string; clear?: boolean }) => {
      if (!id) return;
      return telephonyApi.setTicketContact(id, {
        phone: payload.phone,
        contact_name: payload.contactName,
        clear: payload.clear,
      });
    },
    onSuccess: (_, variables) => {
      message.success(variables.clear ? 'Контакт отвязан' : 'Контакт обновлён');
      setIsContactEditModalOpen(false);
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
      queryClient.invalidateQueries({ queryKey: ['telephony'] });
    },
    onError: () => {
      message.error('Не удалось обновить контакт');
    },
  });

  const handleDescriptionClick: React.MouseEventHandler<HTMLDivElement> = (event) => {
    const target = event.target as HTMLElement | null;
    if (!target) return;
    const link = target.closest('a.etalon-user-link[data-etalon-user-id]') as HTMLAnchorElement | null;
    if (!link) return;
    event.preventDefault();
    const userID = String(link.dataset.etalonUserId || '').trim();
    const userName = String(link.dataset.etalonUserName || '').trim();
    Modal.info({
      title: userName || `Пользователь #${userID}`,
      content: 'Тут будет шорт-инфа про сотрудника',
    });
  };

  if (isLoading || !details || !metadata) {
    return <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>;
  }

  const hasBitrixLink = Boolean(String(metadata.bitrix_deal_url || '').trim());
  const hasBitrixBinding = metadata.sync_with_bitrix
    || hasBitrixLink
    || Boolean(metadata.bitrix_service_point_id)
    || Boolean(String(metadata.bitrix_deal_title || '').trim())
    || String(metadata.service_desk_uuid || '').trim().startsWith('b24:deal:');
  const canPushToBitrix = Boolean(metadata.bitrix_service_point_id) && Boolean(String(metadata.bitrix_deal_title || '').trim());

  const uploadAttachmentsRequest = async (options: UploadRequestOption) => {
    const source = options.file as File;
    try {
      const response = await uploadAttachmentsMutation.mutateAsync([source]);
      const uploaded = response.data?.items?.[0];
      if (uploaded) {
        options.onSuccess?.(uploaded);
      } else {
        options.onSuccess?.({});
      }
    } catch (error) {
      options.onError?.(error as Error);
    }
  };

  const uploadInlineImage = async (source: File): Promise<string | null> => {
    const response = await uploadAttachmentsMutation.mutateAsync([source]);
    const uploaded = response.data?.items?.[0];
    if (!uploaded?.file_path) {
      return null;
    }
    return String(uploaded.file_path)
      .replace(/^\/static\//, '/api/static/')
      .replace(/^static\//, '/api/static/');
  };

  const uploadInlineFile = async (source: File): Promise<string | null> => {
    const response = await uploadAttachmentsMutation.mutateAsync([source]);
    const uploaded = response.data?.items?.[0];
    if (!uploaded?.file_path) {
      return null;
    }
    return String(uploaded.file_path)
      .replace(/^\/static\//, '/api/static/')
      .replace(/^static\//, '/api/static/');
  };

  const copyValue = async (params: CopyValueParams) => {
    const value = normalizeTextValue(params.value);
    if (!value) {
      message.warning('Нет значения для копирования');
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      setCopiedFieldKey(params.key);
      window.setTimeout(() => {
        setCopiedFieldKey((current) => (current === params.key ? '' : current));
      }, 1600);
      message.success(params.successText || 'Значение скопировано');
      if (params.entityType && params.entityID) {
        copyConnectionMutation.mutate({
          label: params.label,
          value,
          entityType: params.entityType,
          entityID: params.entityID,
          connectionField: params.connectionField,
        });
      }
    } catch {
      message.error('Не удалось скопировать значение');
    }
  };

  const renderCopyValueRow = (params: CopyValueParams) => {
    const value = normalizeTextValue(params.value);
    const displayValue = normalizeTextValue(params.displayValue) || value || '-';
    const copied = copiedFieldKey === params.key;
    return (
      <button
        key={params.key}
        type="button"
        className="ticket-equipment-copy-row"
        disabled={!value}
        onClick={() => copyValue(params)}
      >
        <span className="ticket-equipment-copy-row__content">
          <Text type="secondary" className="ticket-equipment-copy-row__label">{params.label}</Text>
          <Text className="ticket-equipment-copy-row__value">{displayValue}</Text>
        </span>
        <span className="ticket-equipment-copy-row__indicator" aria-hidden="true">
          {copied ? <CheckOutlined /> : <CopyOutlined />}
        </span>
      </button>
    );
  };

  const renderEditLink = (path: string, title: string) => (
    <Tooltip title={title}>
      <Button
        type="text"
        size="small"
        icon={<EditOutlined />}
        href={path}
        target="_blank"
        rel="noreferrer"
        aria-label={title}
      />
    </Tooltip>
  );

  const renderAgentBadge = (agentID: string, agentType?: string) => {
    if (!agentID) {
      return null;
    }
    const label = getAgentTypeLabel(agentType);
    return (
      <Tooltip title="Открыть последнее наблюдение агента">
        <button
          type="button"
          className="ticket-agent-badge"
          onClick={() => setObservationAgentID(agentID)}
        >
          {label}
        </button>
      </Tooltip>
    );
  };

  const renderServerCard = (item: InfrastructureItem, keyPrefix: string, sourceLabel?: string) => {
    const dataRow = toInfraData(item);
    const server = item.data as ServerEntity;
    const entityID = normalizeTextValue(server.uuid || dataRow.uuid);
    const path = entityID ? `/servers/${entityID}` : '';
    const title = resolveEntityTitle(item);
    const crmID = normalizeTextValue(dataRow.crm_id);
    return (
      <Card key={`${keyPrefix}-${entityID || title}`} size="small" className="glass-panel ticket-equipment-card">
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space className="ticket-equipment-card__header">
            <a href={path || undefined} target="_blank" rel="noreferrer">
              <Text strong>{title}</Text>
            </a>
            <Space size={4}>
              {sourceLabel ? <Tag color="purple" style={{ marginInlineEnd: 0 }}>{sourceLabel}</Tag> : null}
              <Tag color="geekblue" style={{ marginInlineEnd: 0 }}>Сервер</Tag>
              {path ? renderEditLink(path, 'Открыть карточку сервера') : null}
            </Space>
          </Space>
          {renderCopyValueRow({
            key: `${keyPrefix}-${entityID}-ip`,
            label: 'IP:порт',
            value: normalizeTextValue(dataRow.ip),
            entityType: 'Server',
            entityID,
            connectionField: 'ip',
          })}
          {isDigitsOnly(crmID) ? renderCopyValueRow({
              key: `${keyPrefix}-${entityID}-crm`,
              label: 'CRMid',
              value: crmID,
              entityType: 'Server',
              entityID,
              connectionField: 'crm_id',
            }) : null}
          {renderCopyValueRow({
            key: `${keyPrefix}-${entityID}-version`,
            label: 'Версия',
            value: normalizeTextValue(dataRow.server_version),
            entityType: 'Server',
            entityID,
            connectionField: 'server_version',
          })}
          {renderCopyValueRow({
            key: `${keyPrefix}-${entityID}-uid`,
            label: 'UID',
            value: normalizeTextValue(dataRow.unique_id),
            entityType: 'Server',
            entityID,
            connectionField: 'unique_id',
          })}
        </Space>
      </Card>
    );
  };

  const renderWorkstationCard = (item: InfrastructureItem) => {
    const dataRow = toInfraData(item);
    const entityID = normalizeTextValue(dataRow.uuid);
    const path = entityID ? `/workstations/${entityID}` : '';
    const title = normalizeTextValue(dataRow.device_name) || entityID || 'Рабочая станция';
    const remoteRows = [
      { label: 'AnyDesk', field: 'anydesk', value: normalizeTextValue(dataRow.anydesk) },
      { label: 'TeamViewer', field: 'teamviewer', value: normalizeTextValue(dataRow.teamviewer) },
      { label: 'LiteManager', field: 'litemanager', value: normalizeTextValue(dataRow.litemanager) },
      { label: 'RustDesk', field: 'rustdesk', value: normalizeTextValue(dataRow.rustdesk) },
      { label: 'RDP', field: 'rdp', value: normalizeTextValue(dataRow.rdp) },
    ].filter((row) => row.value);
    return (
      <Card key={`workstation-${entityID || title}`} size="small" className="glass-panel ticket-equipment-card">
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space className="ticket-equipment-card__header">
            <Text strong>{title}</Text>
            <Space size={4}>
              <Tag color="cyan" style={{ marginInlineEnd: 0 }}>РС</Tag>
              {path ? renderEditLink(path, 'Открыть карточку рабочей станции') : null}
            </Space>
          </Space>
          {remoteRows.length > 0 ? remoteRows.map((row) => renderCopyValueRow({
            key: `workstation-${entityID}-${row.field}`,
            label: row.label,
            value: row.value,
            entityType: 'Workstation',
            entityID,
            connectionField: row.field,
          })) : <Text type="secondary">ID удалённых доступов не указаны</Text>}
        </Space>
      </Card>
    );
  };

  const renderFiscalCard = (item: InfrastructureItem) => {
    const dataRow = toInfraData(item);
    const entityID = normalizeTextValue(dataRow.uuid);
    const path = entityID ? `/fiscals/${entityID}` : '';
    const title = normalizeTextValue(dataRow.model_kkt) || 'Фискальный регистратор';
    const organizationName = normalizeTextValue(dataRow.legal_name) || normalizeTextValue(dataRow.organization_name);
    const address = normalizeTextValue(dataRow.address);
    return (
      <Card key={`fiscal-${entityID || normalizeTextValue(dataRow.serial_number) || title}`} size="small" className="glass-panel ticket-equipment-card">
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Space className="ticket-equipment-card__header">
            <Text strong>{title}</Text>
            <Space size={4}>
              <Tag color="gold" style={{ marginInlineEnd: 0 }}>ФР</Tag>
              {path ? renderEditLink(path, 'Открыть карточку фискального регистратора') : null}
            </Space>
          </Space>
          <Text type="secondary">Серийный номер: {normalizeTextValue(dataRow.serial_number) || '-'}</Text>
          <Text type="secondary">РНМ: {normalizeTextValue(dataRow.rn_kkt) || '-'}</Text>
          <Text type="secondary">Юр. лицо: {organizationName || '-'}</Text>
          <Text type="secondary">ИНН: {normalizeTextValue(dataRow.inn) || '-'}</Text>
          {address ? renderCopyValueRow({
            key: `fiscal-${entityID}-address`,
            label: 'Адрес',
            value: address,
            displayValue: shortenAddress(address),
            successText: 'Адрес скопирован',
          }) : null}
        </Space>
      </Card>
    );
  };

  const renderAgentEquipmentGroupCard = (group: AgentEquipmentGroup) => (
    <Card key={group.key} size="small" className="glass-panel ticket-equipment-card ticket-agent-equipment-card">
      <Space direction="vertical" size={10} style={{ width: '100%' }}>
        <div className="ticket-agent-equipment-card__badge-row">
          {renderAgentBadge(group.agentID, group.agentType)}
        </div>
        {group.workstations.map((item) => {
          const dataRow = toInfraData(item);
          const entityID = normalizeTextValue(dataRow.uuid);
          const path = entityID ? `/workstations/${entityID}` : '';
          const title = normalizeTextValue(dataRow.device_name) || entityID || 'Рабочая станция';
          const remoteRows = [
            { label: 'AnyDesk', field: 'anydesk', value: normalizeTextValue(dataRow.anydesk) },
            { label: 'TeamViewer', field: 'teamviewer', value: normalizeTextValue(dataRow.teamviewer) },
            { label: 'LiteManager', field: 'litemanager', value: normalizeTextValue(dataRow.litemanager) },
            { label: 'RustDesk', field: 'rustdesk', value: normalizeTextValue(dataRow.rustdesk) },
            { label: 'RDP', field: 'rdp', value: normalizeTextValue(dataRow.rdp) },
          ].filter((row) => row.value);

          return (
            <div key={`agent-workstation-${entityID || title}`} className="ticket-agent-equipment-card__section">
              <Space className="ticket-equipment-card__header">
                <Text strong>{title}</Text>
                <Space size={4}>
                  <Tag color="cyan" style={{ marginInlineEnd: 0 }}>РС</Tag>
                  {path ? renderEditLink(path, 'Открыть карточку рабочей станции') : null}
                </Space>
              </Space>
              {remoteRows.length > 0 ? remoteRows.map((row) => renderCopyValueRow({
                key: `agent-workstation-${entityID}-${row.field}`,
                label: row.label,
                value: row.value,
                entityType: 'Workstation',
                entityID,
                connectionField: row.field,
              })) : <Text type="secondary">ID удалённых доступов не указаны</Text>}
            </div>
          );
        })}
        {group.fiscals.map((item) => {
          const dataRow = toInfraData(item);
          const entityID = normalizeTextValue(dataRow.uuid);
          const path = entityID ? `/fiscals/${entityID}` : '';
          const title = normalizeTextValue(dataRow.model_kkt) || 'Фискальный регистратор';
          const organizationName = normalizeTextValue(dataRow.legal_name) || normalizeTextValue(dataRow.organization_name);
          const address = normalizeTextValue(dataRow.address);

          return (
            <div key={`agent-fiscal-${entityID || normalizeTextValue(dataRow.serial_number) || title}`} className="ticket-agent-equipment-card__section">
              <Space className="ticket-equipment-card__header">
                <Text strong>{title}</Text>
                <Space size={4}>
                  <Tag color="gold" style={{ marginInlineEnd: 0 }}>ФР</Tag>
                  {path ? renderEditLink(path, 'Открыть карточку фискального регистратора') : null}
                </Space>
              </Space>
              <Text type="secondary">Серийный номер: {normalizeTextValue(dataRow.serial_number) || '-'}</Text>
              <Text type="secondary">РНМ: {normalizeTextValue(dataRow.rn_kkt) || '-'}</Text>
              <Text type="secondary">Юр. лицо: {organizationName || '-'}</Text>
              <Text type="secondary">ИНН: {normalizeTextValue(dataRow.inn) || '-'}</Text>
              {address ? renderCopyValueRow({
                key: `agent-fiscal-${entityID}-address`,
                label: 'Адрес',
                value: address,
                displayValue: shortenAddress(address),
                successText: 'Адрес скопирован',
              }) : null}
            </div>
          );
        })}
      </Space>
    </Card>
  );

  const equipmentTabContent = (
    (isInfraLoading || isParentInfraLoading) ? (
      <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
    ) : serverItems.length === 0
      && parentServerItems.length === 0
      && agentEquipmentGroups.length === 0
      && ungroupedWorkstationItems.length === 0
      && ungroupedFiscalItems.length === 0 ? (
      <Empty description="Оборудование не найдено" />
    ) : (
      <div className="ticket-overview-equipment-grid">
        {parentServerItems.map((item) => renderServerCard(item, 'parent-server', 'Родитель'))}
        {serverItems.map((item) => renderServerCard(item, 'server'))}
        {agentEquipmentGroups.map((group) => renderAgentEquipmentGroupCard(group))}
        {ungroupedWorkstationItems.map((item) => renderWorkstationCard(item))}
        {ungroupedFiscalItems.map((item) => renderFiscalCard(item))}
      </div>
    )
  );

  const latestTicketsTabContent = (
    isLatestCompanyTicketsLoading ? (
      <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
    ) : latestCompanyTickets.length === 0 ? (
      <Empty description="Других тикетов компании пока нет" />
    ) : (
      <List<TicketListItemDTO>
        dataSource={latestCompanyTickets}
        renderItem={(ticket) => (
          <List.Item key={ticket.id} className="ticket-latest-company-item">
            <Space direction="vertical" size={2} style={{ width: '100%' }}>
              <Link to={`/tickets/${ticket.id}`}>
                <Text strong>#{ticket.number} {ticket.subject}</Text>
              </Link>
              <Space size={6} wrap>
                <Tag style={{ marginInlineEnd: 0 }}>{ticket.status}</Tag>
                <Text type="secondary">
                  {dayjs(ticket.last_activity || ticket.created_at).isValid()
                    ? dayjs(ticket.last_activity || ticket.created_at).format('DD.MM.YYYY HH:mm')
                    : '-'}
                </Text>
              </Space>
              {ticket.last_comment ? <Text type="secondary" ellipsis>{ticket.last_comment}</Text> : null}
            </Space>
          </List.Item>
        )}
      />
    )
  );

  const callsTabContent = (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {ticketCalls.length === 0 ? (
        <Empty description="Звонки по тикету пока не привязаны" />
      ) : (
        <List
          dataSource={ticketCalls}
          renderItem={(call) => {
            const phoneDisplay = getTelephonyContactPhoneDisplay(call.contact, call.client_phone) || 'Номер не определён';
            const phoneForCopy = getTelephonyContactPhoneForCopy(call.contact, call.client_phone) || '';
            const contactName = normalizeTextValue(call.contact?.name);
            const employeeName = normalizeTextValue(call.employee_name || call.employee_login);
            const startedAt = call.started_at || call.answered_at || call.completed_at;
            return (
              <List.Item
                actions={[
                  <Button
                    key="recording"
                    type="link"
                    size="small"
                    href={call.recording_url}
                    target="_blank"
                    disabled={!call.recording_url}
                  >
                    Открыть запись
                  </Button>,
                  !isManagerFlowLocked ? (
                    <Popconfirm
                      key="unlink"
                      title="Отвязать звонок от тикета?"
                      okText="Отвязать"
                      cancelText="Отмена"
                      onConfirm={() => unbindTicketCallMutation.mutate(call)}
                    >
                      <Button
                        type="link"
                        size="small"
                        danger
                        loading={unbindTicketCallMutation.isPending && unbindTicketCallMutation.variables?.id === call.id}
                      >
                        Отвязать
                      </Button>
                    </Popconfirm>
                  ) : null,
                ]}
              >
                <Space direction="vertical" size={2} style={{ width: '100%' }}>
                  <Space size={8} wrap>
                    <Button
                      type="link"
                      size="small"
                      icon={<CopyOutlined />}
                      style={{ paddingInline: 0 }}
                      onClick={() => copyTicketPhone(phoneForCopy)}
                    >
                      {phoneDisplay}
                    </Button>
                    {contactName ? <Tag color="blue">{contactName}</Tag> : <Text type="secondary">Имя не указано</Text>}
                    {employeeName ? <Tag>{employeeName}</Tag> : <Tag>Сотрудник не определён</Tag>}
                  </Space>
                  <Text type="secondary">
                    {[
                      startedAt ? dayjs(startedAt).format('DD.MM.YYYY HH:mm') : '',
                      normalizeTextValue(call.direction),
                      normalizeTextValue(call.status),
                    ].filter(Boolean).join(' · ')}
                  </Text>
                </Space>
              </List.Item>
            );
          }}
        />
      )}
      {isAdminRole && !isManagerFlowLocked ? (
        <Button
          type="dashed"
          block
          icon={<PlusOutlined />}
          onClick={() => setIsAttachCallModalOpen(true)}
          style={{ height: 44 }}
        >
          Прикрепить звонок за последние 24 часа
        </Button>
      ) : null}
    </Space>
  );

  const commentComposer = (
    <Space direction="vertical" size="small" style={{ width: '100%', marginTop: commentsNewFirst ? 0 : 12, marginBottom: commentsNewFirst ? 12 : 0 }}>
      {isManagerFlowLocked && (
        <Alert
          type="info"
          showIcon
          message="Тикет передан менеджеру. Доступно только добавление комментариев, остальные действия обновляются из Bitrix24."
        />
      )}
      <SmartTicketEditor
        value={commentDraft}
        onChange={setCommentDraft}
        placeholder="Добавьте комментарий"
        mentions={mentionOptions}
        onImageUpload={uploadInlineImage}
        onFileUpload={uploadInlineFile}
        minHeight={100}
      />
      <Checkbox checked={commentIsPrivate} onChange={(event) => setCommentIsPrivate(event.target.checked)}>
        Приватный комментарий (не синхронизировать во внешние системы)
      </Checkbox>
      {isPyrusLinkedTicket && !commentIsPrivate && (
        <Alert
          type="warning"
          showIcon
          message="Публичный комментарий будет добавлен в Pyrus от имени бота интеграции."
        />
      )}
      <Space>
        <Button
          type="primary"
          loading={addCommentMutation.isPending}
          disabled={!hasEditorContent(commentDraft)}
          onClick={() => addCommentMutation.mutate()}
        >
          Отправить
        </Button>
      </Space>
    </Space>
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
        <Space direction="vertical" size={0}>
          <Space align="center" size={8}>
            <Title level={4} style={{ margin: 0 }}>Заявка #{metadata.number}</Title>
              <Space size={8} wrap>
                {isBitrixEnabled && (
                  <ExternalLinkBadge
                    label="B24"
                    href={metadata.bitrix_deal_url}
                    title="Открыть сделку в Bitrix24"
                    color="success"
                  />
                )}
                <ExternalLinkBadge
                  label="Pyrus"
                  href={metadata.pyrus_task_url}
                  title="Открыть задачу в Pyrus"
                  color="geekblue"
                />
              </Space>
          </Space>
          <Text type="secondary">
            Создана {dayjs(metadata.created_at).format('DD.MM.YYYY HH:mm')}
            {' • '}
            {metadata.reporter_name || 'Сотрудник'}
            {' • '}
            {historySourceLabel(resolveTicketCreatedSource(metadata))}
          </Text>
        </Space>

        <Space>
          {metadata.is_common_contract && <Tag color="gold">Платный</Tag>}
          <div style={highlightedFields.status ? fieldHighlightStyle : undefined}>
            {metadata.is_archived ? (
              <Button
                type="primary"
                loading={changeStatusMutation.isPending}
                onClick={() => {
                  if (!id) return;
                  changeStatusMutation.mutate({ id, status: 'in_progress' });
                }}
              >
                Вернуть в работу
              </Button>
            ) : (
              <Select
                value={metadata.status}
                options={TICKET_STATUS_OPTIONS.filter((item) => item.value !== 'closed').map((item) => ({ value: item.value, label: item.label }))}
                style={{ width: 180 }}
                disabled={isManagerFlowLocked}
                onChange={(nextStatus: TicketStatus) => {
                  if (!id || nextStatus === metadata.status) return;
                  if (nextStatus === 'deferred') {
                    setPendingStatus(nextStatus);
                    setPendingDeferredAt(dayjs().add(1, 'hour').toISOString());
                    return;
                  }
                  if (nextStatus === 'resolved') {
                    const hasComments = (details?.comments || []).length > 0;
                    if (!hasComments) {
                      setPendingStatus(nextStatus);
                      return;
                    }
                    changeStatusMutation.mutate({ id, status: nextStatus });
                    return;
                  }
                  changeStatusMutation.mutate({ id, status: nextStatus });
                }}
              />
            )}
          </div>
          {metadata.status === 'deferred' && (
            <Space size={4}>
              <Button
                type="link"
                size="small"
                style={{ paddingInline: 0 }}
                onClick={() => {
                  setPendingStatus('deferred');
                  setPendingDeferredAt(metadata.deferred_until || dayjs().add(1, 'hour').toISOString());
                }}
              >
                {metadata.deferred_until ? `до ${formatDeferredDateTime(metadata.deferred_until)}` : 'установить время'}
              </Button>
            </Space>
          )}
          {isBitrixEnabled && !hasBitrixLink && !isManagerFlowLocked && (
            <Button
              loading={updateBitrixMutation.isPending}
              onClick={() => {
                if (!metadata.sync_with_bitrix || !canPushToBitrix) {
                  openBitrixSyncModal();
                  return;
                }
                updateBitrixMutation.mutate();
              }}
            >
              {metadata.sync_with_bitrix ? 'Выгрузить в Битрикс24' : 'Включить синхронизацию с Битрикс24'}
            </Button>
          )}
          {isAdminRole && hasBitrixBinding && !isManagerFlowLocked && (
            <Popconfirm
              title="Разорвать связь с Bitrix24?"
              description="Тикет останется в ServiceDesk, но больше не будет синхронизироваться и не создастся заново из этой сделки."
              okText="Разорвать связь"
              cancelText="Отмена"
              onConfirm={() => unlinkBitrixMutation.mutate()}
            >
              <Button loading={unlinkBitrixMutation.isPending}>
                Разорвать связь B24
              </Button>
            </Popconfirm>
          )}
          {isAdminRole && !isManagerFlowLocked && (
            <Popconfirm
              title="Удалить тикет?"
              description={hasBitrixBinding ? 'Связь со сделкой Bitrix24 будет разорвана только локально. В Bitrix24 ничего не изменится.' : 'Тикет будет удалён из ServiceDesk без возможности восстановления.'}
              okText="Удалить"
              cancelText="Отмена"
              onConfirm={() => deleteTicketMutation.mutate()}
            >
              <Button danger loading={deleteTicketMutation.isPending}>
                Удалить тикет
              </Button>
            </Popconfirm>
          )}
          <Button onClick={() => void toggleTicketSubscription()}>
            {ticketSubscriptions.includes(id) ? 'Отписаться' : 'Подписаться на тикет'}
          </Button>
          <Button onClick={goBack}>Назад</Button>
        </Space>
      </Space>

      <div className="ticket-overview-layout">
        <div className="ticket-overview-main">
                  <Card size="small" className="ticket-overview-service-card" title="Служебная информация">
                    <Descriptions column={serviceInfoColumns} bordered size="small" className="ticket-service-descriptions">
                      <Descriptions.Item label="Компания">
                        <div style={highlightedFields.company ? fieldHighlightStyle : undefined}>
                          {!isCompanyEditMode ? (
                          <Space>
                            {metadata.company_id ? (
                              <Link to={`/companies/${metadata.company_id}`}>{companyTitle}</Link>
                            ) : (
                              companyTitle || '-'
                            )}
                            {!isManagerFlowLocked && <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDraftCompanyID(metadata.company_id);
                                setCompanySearch('');
                                setIsCompanyEditMode(true);
                              }}
                            />}
                          </Space>
                          ) : (
                          <Space>
                            <Select
                              showSearch
                              value={draftCompanyID || metadata.company_id}
                              placeholder="Выберите компанию"
                              style={{ width: 320, maxWidth: '100%' }}
                              options={companySelectOptions}
                              optionLabelProp="selectedLabel"
                              filterOption={false}
                              loading={isCompaniesLoading || changeCompanyMutation.isPending}
                              onSearch={(value) => setCompanySearch(value)}
                              onChange={(nextCompanyID) => setDraftCompanyID(String(nextCompanyID))}
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<CheckOutlined />}
                              loading={changeCompanyMutation.isPending}
                              onClick={() => {
                                const nextCompanyID = draftCompanyID || metadata.company_id;
                                if (!nextCompanyID || nextCompanyID === metadata.company_id) {
                                  setIsCompanyEditMode(false);
                                  return;
                                }
                                changeCompanyMutation.mutate(nextCompanyID);
                              }}
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<CloseOutlined />}
                              onClick={() => {
                                setDraftCompanyID(metadata.company_id);
                                setCompanySearch('');
                                setIsCompanyEditMode(false);
                              }}
                            />
                          </Space>
                          )}
                        </div>
                      </Descriptions.Item>
                      <Descriptions.Item label="Контракт">
                        <Space direction="vertical" size={0}>
                          <Text>{metadata.is_common_contract ? 'Общий контракт' : (metadata.contract_id || '-')}</Text>
                          <Text type="secondary">Тип: {contractType}</Text>
                        </Space>
                      </Descriptions.Item>
                      <Descriptions.Item label="Контакт" span={serviceInfoColumns}>
                        <div style={highlightedFields.contact ? fieldHighlightStyle : undefined}>
                          <Space size={8} wrap>
                            <Text>{getTelephonyContactLabel(details.contact, details.contact?.phone_display) || '-'}</Text>
                            <Button
                              type="text"
                              size="small"
                              icon={<CopyOutlined />}
                              disabled={!getTelephonyContactPhoneForCopy(details.contact)}
                              onClick={() => {
                                void copyTicketPhone(getTelephonyContactPhoneForCopy(details.contact));
                              }}
                            >
                              Копировать
                            </Button>
                            {!isManagerFlowLocked && (
                              <Button
                                type="text"
                                size="small"
                                icon={<EditOutlined />}
                                onClick={() => {
                                  setContactPhoneDraft(getTelephonyContactPhoneForCopy(details.contact));
                                  setContactNameDraft(String(details.contact?.name || ''));
                                  setIsContactEditModalOpen(true);
                                }}
                              >
                                Изменить
                              </Button>
                            )}
                            {!isManagerFlowLocked && details.contact && (
                              <Popconfirm
                                title="Отвязать контакт от тикета?"
                                okText="Отвязать"
                                cancelText="Отмена"
                                onConfirm={() => updateTicketContactMutation.mutate({ clear: true })}
                              >
                                <Button
                                  type="text"
                                  size="small"
                                  danger
                                  loading={updateTicketContactMutation.isPending && updateTicketContactMutation.variables?.clear}
                                >
                                  Отвязать
                                </Button>
                              </Popconfirm>
                            )}
                          </Space>
                        </div>
                      </Descriptions.Item>
                      <Descriptions.Item label="Исполнитель">
                        <div style={highlightedFields.assignee ? fieldHighlightStyle : undefined}>
                          <Select
                            allowClear
                            placeholder="Не назначен"
                            style={{ width: 260, maxWidth: '100%' }}
                            options={assigneeOptions}
                            value={metadata.assignee?.id}
                            loading={assignMutation.isPending}
                            disabled={isManagerFlowLocked}
                            onChange={(nextValue) => assignMutation.mutate(nextValue as number | undefined)}
                          />
                        </div>
                      </Descriptions.Item>
                      <Descriptions.Item label="Обновлена">
                        {dayjs(metadata.updated_at).format('DD.MM.YYYY HH:mm')}
                      </Descriptions.Item>
                      {isBitrixEnabled && metadata.sync_with_bitrix && (
                      <Descriptions.Item label="Заголовок сделки B24">
                        <div style={highlightedFields.bitrix_deal_title ? fieldHighlightStyle : undefined}>
                          {!isBitrixEditMode ? (
                          <Space>
                            <Text>{metadata.bitrix_deal_title || '-'}</Text>
                            {!isManagerFlowLocked && <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDraftBitrixPointID(metadata.bitrix_service_point_id ?? undefined);
                                setDraftBitrixDealTitle(metadata.bitrix_deal_title || '');
                                setIsBitrixEditMode(true);
                              }}
                            />}
                          </Space>
                          ) : (
                          <Input
                            value={draftBitrixDealTitle}
                            placeholder="Заголовок сделки в Bitrix24"
                            onChange={(event) => setDraftBitrixDealTitle(event.target.value)}
                          />
                          )}
                        </div>
                      </Descriptions.Item>
                      )}
                      {isBitrixEnabled && metadata.sync_with_bitrix && (
                      <Descriptions.Item label="Точка обслуживания B24">
                        <div style={highlightedFields.bitrix_service_point ? fieldHighlightStyle : undefined}>
                          {!isBitrixEditMode ? (
                          bitrixPointName
                          ) : (
                          <Space>
                            <Select
                              showSearch
                              value={draftBitrixPointID}
                              placeholder="Выберите точку обслуживания"
                              style={{ width: 320, maxWidth: '100%' }}
                              options={bitrixServicePoints.map((item) => ({
                                value: item.b24_element_id,
                                label: item.name,
                              }))}
                              optionFilterProp="label"
                              onChange={(value) => setDraftBitrixPointID(value)}
                              disabled={isManagerFlowLocked}
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<CheckOutlined />}
                              loading={updateBitrixMutation.isPending}
                              disabled={isManagerFlowLocked || !draftBitrixPointID || !draftBitrixDealTitle.trim()}
                              onClick={() => updateBitrixMutation.mutate()}
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<CloseOutlined />}
                              onClick={() => {
                                setDraftBitrixPointID(metadata.bitrix_service_point_id ?? undefined);
                                setDraftBitrixDealTitle(metadata.bitrix_deal_title || '');
                                setIsBitrixEditMode(false);
                              }}
                            />
                          </Space>
                          )}
                        </div>
                      </Descriptions.Item>
                      )}
                    </Descriptions>

                    <div style={{ marginTop: 16 }}>
                      <Text strong style={{ display: 'block', marginBottom: 8 }}>Описание</Text>
                      <div style={highlightedFields.description ? fieldHighlightStyle : undefined}>
                        {!isDescriptionEditMode ? (
                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
                            <SafeHtmlContent
                              html={metadata.description || '<span>Нет описания</span>'}
                              onClick={handleDescriptionClick}
                              style={{ whiteSpace: 'pre-wrap' }}
                            />
                            {!isManagerFlowLocked && <Button
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDescriptionDraft(metadata.description || '');
                                setIsDescriptionEditMode(true);
                              }}
                            >
                              Редактировать описание
                            </Button>}
                          </Space>
                        ) : (
                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
                            <SmartTicketEditor
                              value={descriptionDraft}
                              onChange={setDescriptionDraft}
                              placeholder="Введите описание тикета"
                              mentions={mentionOptions}
                              onImageUpload={uploadInlineImage}
                              onFileUpload={uploadInlineFile}
                            />
                            <Space>
                              <Button
                                type="primary"
                                loading={updateDescriptionMutation.isPending}
                                onClick={() => updateDescriptionMutation.mutate()}
                              >
                                Сохранить
                              </Button>
                              <Button
                                onClick={() => {
                                  setDescriptionDraft(metadata.description || '');
                                  setIsDescriptionEditMode(false);
                                }}
                              >
                                Отмена
                              </Button>
                            </Space>
                          </Space>
                        )}
                      </div>
                    </div>

                    {isClosedLikeTicketStatus(metadata.status) && Boolean((metadata.result || '').trim()) && (
                      <div style={{ marginTop: 16 }}>
                        <Text strong style={{ display: 'block', marginBottom: 8 }}>Результат</Text>
                        <div style={highlightedFields.result ? fieldHighlightStyle : undefined}>
                          <SafeHtmlContent html={metadata.result || ''} style={{ whiteSpace: 'pre-wrap' }} />
                        </div>
                      </div>
                    )}
                  </Card>

                  <Card size="small" className="ticket-overview-comments-card" title="Обзор">
                    <Tabs
                      defaultActiveKey="comments"
                      items={[
                        {
                          key: 'comments',
                          label: 'Комментарии',
                          children: (
                            <>
                              {commentsNewFirst && commentComposer}
                              {commentsOrdered.length ? (
                                <List
                                  dataSource={commentsOrdered}
                                  renderItem={(item) => (
                                    <List.Item key={item.uuid} style={highlightedComments[item.uuid] ? fieldHighlightStyle : undefined}>
                                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                                        <Space size={8} style={{ justifyContent: 'space-between', width: '100%' }} wrap>
                                          <Text type="secondary">{item.author_name || 'Сотрудник'} в {dayjs(item.creation_date).format('DD.MM.YYYY HH:mm')}</Text>
                                          <Space size={8}>
                                            {item.is_private && <Tag color="orange">Приватный</Tag>}
                                            {canManageComment(item.author_name) && !isManagerFlowLocked && (
                                              <Button
                                                type="link"
                                                size="small"
                                                onClick={() => {
                                                  setEditingCommentUUID(item.uuid);
                                                  setEditingCommentDraft(item.text || '');
                                                }}
                                              >
                                                Редактировать
                                              </Button>
                                            )}
                                            {canDeleteComment(item.author_name) && !isManagerFlowLocked && (
                                              <Popconfirm
                                                title="Удалить комментарий?"
                                                okText="Удалить"
                                                cancelText="Отмена"
                                                onConfirm={() => deleteCommentMutation.mutate(item.uuid)}
                                              >
                                                <Button type="link" size="small" danger loading={deleteCommentMutation.isPending}>
                                                  Удалить
                                                </Button>
                                              </Popconfirm>
                                            )}
                                          </Space>
                                        </Space>
                                        {editingCommentUUID === item.uuid ? (
                                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                            <SmartTicketEditor
                                              value={editingCommentDraft}
                                              onChange={setEditingCommentDraft}
                                              placeholder="Измените комментарий"
                                              mentions={mentionOptions}
                                              onImageUpload={uploadInlineImage}
                                              onFileUpload={uploadInlineFile}
                                              minHeight={100}
                                            />
                                            {isPyrusLinkedTicket && !item.is_private && (
                                              <Alert
                                                type="warning"
                                                showIcon
                                                message="Публичный комментарий будет синхронизирован в Pyrus от имени бота интеграции."
                                              />
                                            )}
                                            <Space>
                                              <Button
                                                type="primary"
                                                loading={updateCommentMutation.isPending}
                                                disabled={!hasEditorContent(editingCommentDraft)}
                                                onClick={() => updateCommentMutation.mutate()}
                                              >
                                                Сохранить
                                              </Button>
                                              <Button
                                                onClick={() => {
                                                  setEditingCommentUUID('');
                                                  setEditingCommentDraft('');
                                                }}
                                              >
                                                Отмена
                                              </Button>
                                            </Space>
                                          </Space>
                                        ) : (
                                          <SafeHtmlContent html={item.text} style={{ whiteSpace: 'pre-wrap' }} />
                                        )}
                                      </Space>
                                    </List.Item>
                                  )}
                                />
                              ) : null}
                              {!commentsNewFirst && commentComposer}
                            </>
                          ),
                        },
                        {
                          key: 'attachments',
                          label: `Вложения (${attachments.length})`,
                          children: (
                            <>
                              {!isManagerFlowLocked && <div style={{ marginBottom: 12 }}>
                                <Upload
                                  showUploadList={false}
                                  customRequest={uploadAttachmentsRequest}
                                  multiple
                                >
                                  <Button icon={<PaperClipOutlined />} loading={uploadAttachmentsMutation.isPending}>
                                    Прикрепить
                                  </Button>
                                </Upload>
                              </div>}
                              {!isManagerFlowLocked && <Upload.Dragger
                                name="files"
                                multiple
                                showUploadList={false}
                                customRequest={uploadAttachmentsRequest}
                                style={{ marginBottom: 12 }}
                              >
                                <p style={{ marginBottom: 4 }}>Перетащите файлы сюда или нажмите для выбора</p>
                                <Text type="secondary">Поддерживается множественная загрузка</Text>
                              </Upload.Dragger>}
                              {attachments.length === 0 ? (
                                null
                              ) : (
                                <List
                                  dataSource={attachments}
                                  renderItem={(item) => (
                                    <List.Item key={item.id || item.filePath}>
                                      <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                        <a href={item.filePath} target="_blank" rel="noreferrer">{item.fileName}</a>
                                        {item.mimeType.startsWith('image/') && (
                                          <a href={item.filePath} target="_blank" rel="noreferrer">
                                            <img
                                              src={item.filePath}
                                              alt={item.fileName}
                                              style={{ maxWidth: '100%', maxHeight: 280, objectFit: 'contain', border: '1px solid #f0f0f0', borderRadius: 8 }}
                                            />
                                          </a>
                                        )}
                                      </Space>
                                    </List.Item>
                                  )}
                                />
                              )}
                            </>
                          ),
                        },
                        {
                          key: 'history',
                          label: 'История',
                          children: (
                            <>
                              <Text type="secondary">
                                Создана {dayjs(metadata.created_at).format('DD.MM.YYYY HH:mm')} • {metadata.reporter_name || 'Сотрудник'} • {historySourceLabel(resolveTicketCreatedSource(metadata))}
                              </Text>
                              {(details.history || []).length === 0 ? (
                                <Empty description="История пока пуста" />
                              ) : (
                                <List
                                  style={{ marginTop: 12 }}
                                  dataSource={(details.history || []).slice().sort((a, b) => dayjs(b.created_at).valueOf() - dayjs(a.created_at).valueOf())}
                                  renderItem={(item) => (
                                    <List.Item key={`${item.id}-${item.created_at}`}>
                                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                                        <Text strong>{historyLabel(item)}</Text>
                                        <Text type="secondary">
                                          {dayjs(item.created_at).format('DD.MM.YYYY HH:mm')}
                                          {' • '}
                                          {historyActorLabel(item)}
                                          {' • '}
                                          {historySourceLabel(item.source)}
                                        </Text>
                                        {item.old_value && <Text type="secondary">Было: {item.old_value}</Text>}
                                        {item.new_value && (
                                          item.action === 'connection_copied' ?
                                            <Text>Скопировано: {item.new_value}</Text> :
                                            <Text>Стало: {item.new_value}</Text>
                                        )}
                                      </Space>
                                    </List.Item>
                                  )}
                                />
                              )}
                            </>
                          ),
                        },
                      ]}
                    />
                  </Card>
                </div>

                <Card size="small" className="ticket-overview-side-card" title="Контекст компании">
                  <Tabs
                    size="small"
                    defaultActiveKey="overview-equipment"
                    items={[
                      {
                        key: 'overview-equipment',
                        label: 'Оборудование',
                        children: equipmentTabContent,
                      },
                      {
                        key: 'overview-tickets',
                        label: 'Тикеты',
                        children: latestTicketsTabContent,
                      },
                      {
                        key: 'overview-calls',
                        label: 'Звонки',
                        children: callsTabContent,
                      },
                    ]}
                  />
                </Card>
      </div>

      <AgentObservationRawModal
        open={Boolean(observationAgentID)}
        observationID={latestAgentObservation?.observation_id}
        onClose={() => setObservationAgentID('')}
        title={observationAgentID ? 'Последнее наблюдение агента' : undefined}
        lookupLoading={isAgentObservationLookupLoading}
        lookupError={isAgentObservationLookupError ? getLookupErrorMessage(agentObservationLookupError) : undefined}
        emptyDescription="Для этого агента пока нет наблюдений"
        summary={{
          agentUUID: observationAgentID,
          serverURL: latestAgentObservation?.server_url,
          currentTime: latestAgentObservation?.current_time || latestAgentObservation?.current_time_parsed,
          vTime: latestAgentObservation?.v_time || latestAgentObservation?.v_time_parsed,
          workstation: latestAgentObservation?.workstation_name || latestAgentObservation?.workstation_id,
          fr: latestAgentObservation?.fr_name || latestAgentObservation?.fr_id,
        }}
      />

      {isBitrixEnabled && (
        <Modal
          open={isBitrixSyncModalOpen}
          title="Синхронизация с Битрикс24"
          okText="Сохранить и синхронизировать"
          cancelText="Отмена"
          onCancel={() => setIsBitrixSyncModalOpen(false)}
          confirmLoading={updateBitrixMutation.isPending}
          okButtonProps={{ disabled: !draftBitrixPointID || !draftBitrixDealTitle.trim() }}
          onOk={() => updateBitrixMutation.mutate()}
        >
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Select
              showSearch
              value={draftBitrixPointID}
              placeholder="Выберите точку обслуживания"
              style={{ width: '100%' }}
              options={bitrixServicePoints.map((item) => ({
                value: item.b24_element_id,
                label: item.name,
              }))}
              optionFilterProp="label"
              onChange={(value) => setDraftBitrixPointID(value)}
            />
            <Input
              value={draftBitrixDealTitle}
              placeholder="Заголовок сделки в Bitrix24"
              onChange={(event) => setDraftBitrixDealTitle(event.target.value)}
            />
          </Space>
        </Modal>
      )}

      <Modal
        open={Boolean(pendingStatus)}
        title={pendingStatus === 'deferred' ? 'Отложить заявку' : 'Отчёт по заявке'}
        okText={pendingStatus === 'deferred' ? 'Отложить' : 'Завершить заявку'}
        cancelText="Отмена"
        onCancel={() => {
          setPendingStatus(null);
          setStatusComment('');
          setPendingDeferredAt('');
        }}
        confirmLoading={changeStatusMutation.isPending}
        okButtonProps={{ disabled: pendingStatus === 'deferred' ? !pendingDeferredAt : !statusComment.trim() }}
        onOk={() => {
          if (!id || !pendingStatus) return;
          if (pendingStatus === 'deferred') {
            if (!pendingDeferredAt) return;
            changeStatusMutation.mutate({ id, status: pendingStatus, deferredUntil: pendingDeferredAt });
            return;
          }
          if (!statusComment.trim()) return;
          changeStatusMutation.mutate({ id, status: pendingStatus, comment: statusComment.trim() });
        }}
      >
        {pendingStatus === 'deferred' ? (
          <DatePicker
            showTime
            style={{ width: '100%' }}
            format="DD.MM.YYYY HH:mm"
            value={pendingDeferredAt ? dayjs(pendingDeferredAt) : null}
            onChange={(value) => setPendingDeferredAt(value ? value.toISOString() : '')}
            placeholder="Выберите дату и время"
          />
        ) : (
          <Input.TextArea
            rows={4}
            value={statusComment}
            onChange={(event) => setStatusComment(event.target.value)}
            placeholder="Добавьте отчёт по выполнению"
          />
        )}
      </Modal>
      <Modal
        open={isContactEditModalOpen}
        title="Контакт тикета"
        okText="Сохранить"
        cancelText="Отмена"
        confirmLoading={updateTicketContactMutation.isPending && !updateTicketContactMutation.variables?.clear}
        onCancel={() => setIsContactEditModalOpen(false)}
        onOk={() => {
          updateTicketContactMutation.mutate({
            phone: contactPhoneDraft,
            contactName: contactNameDraft,
          });
        }}
        destroyOnHidden
      >
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Input
            value={contactPhoneDraft}
            onChange={(event) => setContactPhoneDraft(event.target.value)}
            placeholder="Телефон"
          />
          <Input
            value={contactNameDraft}
            onChange={(event) => setContactNameDraft(event.target.value)}
            placeholder="Имя контакта"
          />
        </Space>
      </Modal>
      <Modal
        open={isAttachCallModalOpen}
        title="Непривязанные звонки за последние 24 часа"
        onCancel={() => setIsAttachCallModalOpen(false)}
        footer={null}
        destroyOnHidden
      >
        <Space direction="vertical" size={8} style={{ width: '100%', marginBottom: 12 }}>
          <Input.Search
            allowClear
            placeholder="Поиск по номеру"
            value={attachCallPhoneSearch}
            onChange={(event) => setAttachCallPhoneSearch(event.target.value)}
          />
          <Select
            allowClear
            showSearch
            placeholder="Сотрудник"
            value={attachCallEmployeeID}
            options={attachCallEmployeeOptions}
            optionFilterProp="label"
            onChange={(value) => setAttachCallEmployeeID(value)}
          />
        </Space>
        {isAttachableCallsLoading ? (
          <div style={{ textAlign: 'center', padding: 24 }}>
            <Spin />
          </div>
        ) : attachableCalls.length === 0 ? (
          <Empty description="Свободных звонков не найдено" />
        ) : (
          <List
            dataSource={attachableCalls}
            renderItem={(call) => {
              const phoneDisplay = getTelephonyContactPhoneDisplay(call.contact, call.client_phone) || 'Номер не определён';
              const phoneForCopy = getTelephonyContactPhoneForCopy(call.contact, call.client_phone) || '';
              const contactName = String(call.contact?.name || '').trim();
              const employeeName = String(call.employee_name || call.employee_login || '').trim();
              const startedAt = call.started_at || call.answered_at || call.completed_at;
              return (
                <List.Item
                  actions={[
                    <Button
                      key="attach"
                      type="primary"
                      size="small"
                      loading={bindTicketCallMutation.isPending && bindTicketCallMutation.variables?.id === call.id}
                      onClick={() => bindTicketCallMutation.mutate(call)}
                    >
                      Прикрепить
                    </Button>,
                  ]}
                >
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space size={8} wrap>
                      <Button
                        type="link"
                        size="small"
                        icon={<CopyOutlined />}
                        style={{ paddingInline: 0 }}
                        onClick={() => copyTicketPhone(phoneForCopy)}
                      >
                        {phoneDisplay}
                      </Button>
                      {contactName ? <Tag color="blue">{contactName}</Tag> : <Text type="secondary">Имя не указано</Text>}
                      {employeeName ? <Tag>{employeeName}</Tag> : <Tag>Сотрудник не определён</Tag>}
                    </Space>
                    <Text type="secondary">
                      {[
                        startedAt ? dayjs(startedAt).format('DD.MM.YYYY HH:mm') : '',
                        String(call.direction || '').trim(),
                        String(call.status || '').trim(),
                      ].filter(Boolean).join(' · ')}
                    </Text>
                  </Space>
                </List.Item>
              );
            }}
          />
        )}
      </Modal>
      {isCreateOpen && (
        <Suspense fallback={null}>
          <LazyNewTicketModal
            open={isCreateOpen}
            onClose={() => {
              setIsCreateOpen(false);
              const next = new URLSearchParams(searchParams);
              next.delete('create');
              setSearchParams(next);
            }}
            onCreated={() => {
              const next = new URLSearchParams(searchParams);
              next.delete('create');
              setSearchParams(next);
              queryClient.invalidateQueries({ queryKey: ['tickets'] });
            }}
          />
        </Suspense>
      )}
    </Space>
  );
};

export default TicketDetailsPage;
