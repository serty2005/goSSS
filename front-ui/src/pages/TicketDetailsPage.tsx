import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Checkbox, Descriptions, Empty, Input, List, Modal, Popconfirm, Select, Space, Spin, Tabs, Tag, Tooltip, Typography, Upload, message } from 'antd';
import { CheckOutlined, CloseOutlined, EditOutlined, LinkOutlined, PaperClipOutlined } from '@ant-design/icons';
import type { UploadProps } from 'antd';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { companiesApi } from '@/api/companies';
import { contractsApi } from '@/api/contracts';
import { usersApi } from '@/api/users';
import { equipmentApi } from '@/api/equipment';
import { CompanyModel, ConnectionCopyStatDTO, InfrastructureItem, TicketDetailsDTO, TicketHistoryDTO, TicketStatus } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import SmartTicketEditor from '@/features/tickets/editor/SmartTicketEditor';
import { hasEditorContent } from '@/features/tickets/editor/content';
import type { MentionOption } from '@/features/tickets/editor/mentions';
import { SafeHtmlContent } from '@/utils/safeHtml';
import InlineFieldEditor from '@/components/common/InlineFieldEditor';
import { useAuthStore } from '@/store/authStore';

const { Title, Text, Paragraph } = Typography;
type UploadRequestOption = Parameters<NonNullable<UploadProps['customRequest']>>[0];

const STATUS_OPTIONS: Array<{ value: TicketStatus; label: string; color: string }> = [
  { value: 'new', label: 'Новая', color: 'blue' },
  { value: 'in_progress', label: 'В работе', color: 'processing' },
  { value: 'pending', label: 'Ожидание', color: 'orange' },
  { value: 'deferred', label: 'Отложено', color: 'orange' },
  { value: 'onsite', label: 'На выезд', color: 'cyan' },
  { value: 'to_manager', label: 'Передать менеджеру', color: 'purple' },
  { value: 'resolved', label: 'Решена', color: 'green' },
  { value: 'spam', label: 'Спам', color: 'red' },
  { value: 'execution', label: 'Реализация', color: 'magenta' },
  { value: 'closed', label: 'Закрыта', color: 'default' },
];

const isClosedLikeStatus = (status?: string) => status === 'resolved' || status === 'closed' || status === 'spam' || status === 'execution';
const FIELD_HIGHLIGHT_MS = 2600;
const fieldHighlightStyle: React.CSSProperties = {
  background: 'rgba(250, 173, 20, 0.22)',
  transition: 'background-color 0.35s ease',
  borderRadius: 6,
  padding: '2px 6px',
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
      return 'Изменение заявки';
  }
};

const historySourceLabel = (source?: string) => {
  if (source === 'ui') return 'UI';
  if (source === 'bitrix') return 'Bitrix24';
  if (source === 'servicedesk') return 'ServiceDesk';
  return 'System';
};

const resolveTicketCreatedSource = (metadata?: TicketDetailsDTO['metadata']) => {
  if (!metadata) return 'system';
  const sdUUID = String(metadata.service_desk_uuid || '').trim();
  if (sdUUID.startsWith('b24:deal:')) return 'bitrix';
  if (metadata.reporter_id && metadata.reporter_id > 0) return 'ui';
  if (sdUUID) return 'servicedesk';
  return 'system';
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

const resolveEntityPath = (item: InfrastructureItem) => {
  const dataRow = item.data as Record<string, string | undefined>;
  const uuid = dataRow.uuid;
  if (!uuid) return '';
  if (item.entity_type === 'Server') return `/servers/${uuid}`;
  if (item.entity_type === 'Workstation') return `/workstations/${uuid}`;
  if (item.entity_type === 'FiscalRegister') return `/fiscals/${uuid}`;
  return '';
};

const BitrixSyncIndicator: React.FC<{ sync?: boolean; dealURL?: string }> = ({ sync, dealURL }) => {
  if (!sync) {
    return null;
  }
  if (!dealURL) {
    return <Tag color="processing">B24</Tag>;
  }
  return (
    <Tooltip title="Открыть сделку в Bitrix24">
      <a href={dealURL} target="_blank" rel="noreferrer" style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
        <Tag color="success" style={{ marginInlineEnd: 0 }}>Синхронизировано B24</Tag>
        <LinkOutlined />
      </a>
    </Tooltip>
  );
};

const TicketDetailsPage: React.FC = () => {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const [commentDraft, setCommentDraft] = useState('');
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [editingCommentUUID, setEditingCommentUUID] = useState('');
  const [editingCommentDraft, setEditingCommentDraft] = useState('');
  const [statusComment, setStatusComment] = useState('');
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
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
  const [highlightedFields, setHighlightedFields] = useState<Record<string, boolean>>({});
  const [highlightedComments, setHighlightedComments] = useState<Record<string, boolean>>({});
  const previousMetadataRef = useRef<TicketDetailsDTO['metadata'] | undefined>(undefined);
  const previousCommentsRef = useRef<Array<{ uuid: string; text: string; creation_date: string }>>([]);
  const clearFieldTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const clearCommentTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const createParam = searchParams.get('create') || '';
  const user = useAuthStore((state) => state.user);

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
  const mentionOptions = useMemo<MentionOption[]>(
    () => assigneeOptions.map((item) => ({ id: Number(item.value), label: String(item.label) })),
    [assigneeOptions],
  );
  const { data: bitrixServicePoints = [] } = useQuery({
    queryKey: ['bitrix-service-points'],
    queryFn: () => ticketsApi.getBitrixServicePoints(),
    staleTime: 5 * 60_000,
  });

  const bitrixPointName = useMemo(() => {
    if (!metadata?.bitrix_service_point_id) return '-';
    const point = bitrixServicePoints.find((item) => item.b24_element_id === metadata.bitrix_service_point_id);
    return point?.name || String(metadata.bitrix_service_point_id);
  }, [bitrixServicePoints, metadata?.bitrix_service_point_id]);

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

  const { data: connectionStatsResponse } = useQuery({
    queryKey: ['ticket-connection-stats', id],
    queryFn: () => ticketsApi.getConnectionCopyStats(id),
    enabled: Boolean(id) && Boolean(metadata?.company_id),
    staleTime: 15_000,
  });

  const connectionStatsMap = useMemo(() => {
    const map = new Map<string, { count: number; lastCopiedAt: number }>();
    (connectionStatsResponse?.data || []).forEach((item: ConnectionCopyStatDTO) => {
      const entityType = String(item.entity_type || '').trim();
      const entityID = String(item.entity_id || '').trim();
      if (!entityType || !entityID) return;
      const key = `${entityType}:${entityID}`;
      map.set(key, {
        count: Number(item.copy_count || 0),
        lastCopiedAt: item.last_copied_at ? dayjs(item.last_copied_at).valueOf() : 0,
      });
    });
    return map;
  }, [connectionStatsResponse?.data]);

  const buildServerConnectionCards = (items: InfrastructureItem[], keyPrefix: string) => {
    return items
      .map((item) => {
        const dataRow = item.data as Record<string, string | undefined>;
        const entityID = String(dataRow.uuid || '').trim();
        const address = String(dataRow.ip || '').trim();
        if (!entityID || !address) return null;
        const stats = connectionStatsMap.get(`Server:${entityID}`);
        return {
          key: `${keyPrefix}-Server-${entityID}`,
          entityType: 'Server' as const,
          entityID,
          title: resolveEntityTitle(item),
          statsCount: stats?.count || 0,
          rows: [{ label: 'Адрес сервера', field: 'ip', value: address }],
        };
      })
      .filter(Boolean) as Array<{
      key: string;
      entityType: 'Server';
      entityID: string;
      title: string;
      statsCount: number;
      rows: Array<{ label: string; field: string; value: string }>;
    }>;
  };

  const ownConnectionCards = useMemo(() => {
    const serverCards = buildServerConnectionCards(serverItems, 'own');

    const workstationCards = workstationItems
      .map((item) => {
        const dataRow = item.data as Record<string, string | undefined>;
        const entityID = String(dataRow.uuid || '').trim();
        if (!entityID) return null;
        const rows = [
          { label: 'AnyDesk', field: 'anydesk', value: String(dataRow.anydesk || '').trim() },
          { label: 'TeamViewer', field: 'teamviewer', value: String(dataRow.teamviewer || '').trim() },
          { label: 'LiteManager', field: 'litemanager', value: String(dataRow.litemanager || '').trim() },
          { label: 'RDP', field: 'rdp', value: String(dataRow.rdp || '').trim() },
        ].filter((row) => row.value);
        if (rows.length === 0) return null;
        const stats = connectionStatsMap.get(`Workstation:${entityID}`);
        return {
          key: `Workstation-${entityID}`,
          entityType: 'Workstation' as const,
          entityID,
          title: resolveEntityTitle(item),
          statsCount: stats?.count || 0,
          statsLastCopiedAt: stats?.lastCopiedAt || 0,
          rows,
        };
      })
      .filter(Boolean) as Array<{
      key: string;
      entityType: 'Workstation';
      entityID: string;
      title: string;
      statsCount: number;
      statsLastCopiedAt: number;
      rows: Array<{ label: string; field: string; value: string }>;
    }>;

    workstationCards.sort((a, b) => {
      if (b.statsCount !== a.statsCount) return b.statsCount - a.statsCount;
      if (b.statsLastCopiedAt !== a.statsLastCopiedAt) return b.statsLastCopiedAt - a.statsLastCopiedAt;
      return a.title.localeCompare(b.title, 'ru');
    });

    return [...serverCards, ...workstationCards];
  }, [serverItems, workstationItems, connectionStatsMap]);

  const parentConnectionCards = useMemo(
    () => buildServerConnectionCards(parentServerItems, 'parent'),
    [parentServerItems, connectionStatsMap],
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
    mutationFn: async (payload: { id: string; status: TicketStatus; comment?: string }) =>
      ticketsApi.changeStatus(payload.id, payload.status, payload.comment),
    onSuccess: () => {
      message.success('Статус обновлён');
      setPendingStatus(null);
      setStatusComment('');
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

  const updateWorkstationNameMutation = useMutation({
    mutationFn: async (payload: { workstationID: string; deviceName: string }) => {
      return equipmentApi.updateWorkstation(payload.workstationID, { device_name: payload.deviceName });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['company-infra', metadata?.company_id] });
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
    },
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

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
        <Space direction="vertical" size={0}>
          <Space align="center" size={8}>
            <Title level={4} style={{ margin: 0 }}>Заявка #{metadata.number}</Title>
            <BitrixSyncIndicator sync={metadata.sync_with_bitrix} dealURL={metadata.bitrix_deal_url} />
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
                options={STATUS_OPTIONS.filter((item) => item.value !== 'closed').map((item) => ({ value: item.value, label: item.label }))}
                style={{ width: 180 }}
                onChange={(nextStatus: TicketStatus) => {
                  if (!id || nextStatus === metadata.status) return;
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
          {!metadata.sync_with_bitrix && (
            <Button onClick={openBitrixSyncModal}>
              Синхронизировать с Битрикс24
            </Button>
          )}
          <Button onClick={() => navigate('/tickets')}>К списку</Button>
        </Space>
      </Space>

      <div className="ticket-overview-layout">
        <div className="ticket-overview-main">
                  <Card size="small" className="ticket-overview-service-card" title="Служебная информация">
                    <Descriptions column={2} bordered size="small">
                      <Descriptions.Item label="Компания">
                        <div style={highlightedFields.company ? fieldHighlightStyle : undefined}>
                          {!isCompanyEditMode ? (
                          <Space>
                            {metadata.company_id ? (
                              <Link to={`/companies/${metadata.company_id}`}>{companyTitle}</Link>
                            ) : (
                              companyTitle || '-'
                            )}
                            <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDraftCompanyID(metadata.company_id);
                                setCompanySearch('');
                                setIsCompanyEditMode(true);
                              }}
                            />
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
                      <Descriptions.Item label="Исполнитель">
                        <div style={highlightedFields.assignee ? fieldHighlightStyle : undefined}>
                          <Select
                            allowClear
                            placeholder="Не назначен"
                            style={{ width: 260, maxWidth: '100%' }}
                            options={assigneeOptions}
                            value={metadata.assignee?.id}
                            loading={assignMutation.isPending}
                            onChange={(nextValue) => assignMutation.mutate(nextValue as number | undefined)}
                          />
                        </div>
                      </Descriptions.Item>
                      <Descriptions.Item label="Обновлена">
                        {dayjs(metadata.updated_at).format('DD.MM.YYYY HH:mm')}
                      </Descriptions.Item>
                      {metadata.sync_with_bitrix && (
                      <Descriptions.Item label="Заголовок сделки B24">
                        <div style={highlightedFields.bitrix_deal_title ? fieldHighlightStyle : undefined}>
                          {!isBitrixEditMode ? (
                          <Space>
                            <Text>{metadata.bitrix_deal_title || '-'}</Text>
                            <Button
                              type="text"
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDraftBitrixPointID(metadata.bitrix_service_point_id ?? undefined);
                                setDraftBitrixDealTitle(metadata.bitrix_deal_title || '');
                                setIsBitrixEditMode(true);
                              }}
                            />
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
                      {metadata.sync_with_bitrix && (
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
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<CheckOutlined />}
                              loading={updateBitrixMutation.isPending}
                              disabled={!draftBitrixPointID || !draftBitrixDealTitle.trim()}
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
                            <Button
                              size="small"
                              icon={<EditOutlined />}
                              onClick={() => {
                                setDescriptionDraft(metadata.description || '');
                                setIsDescriptionEditMode(true);
                              }}
                            >
                              Редактировать описание
                            </Button>
                          </Space>
                        ) : (
                          <Space direction="vertical" size={8} style={{ width: '100%' }}>
                            <SmartTicketEditor
                              value={descriptionDraft}
                              onChange={setDescriptionDraft}
                              placeholder="Введите описание тикета"
                              mentions={mentionOptions}
                              onImageUpload={uploadInlineImage}
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

                    {isClosedLikeStatus(metadata.status) && Boolean((metadata.result || '').trim()) && (
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
                              {details.comments?.length ? (
                                <List
                                  dataSource={details.comments}
                                  renderItem={(item) => (
                                    <List.Item key={item.uuid} style={highlightedComments[item.uuid] ? fieldHighlightStyle : undefined}>
                                      <Space direction="vertical" size={2} style={{ width: '100%' }}>
                                        <Space size={8} style={{ justifyContent: 'space-between', width: '100%' }} wrap>
                                          <Text type="secondary">{item.author_name || 'Сотрудник'} в {dayjs(item.creation_date).format('DD.MM.YYYY HH:mm')}</Text>
                                          <Space size={8}>
                                            {item.is_private && <Tag color="orange">Приватный</Tag>}
                                            {canManageComment(item.author_name) && (
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
                                            {canDeleteComment(item.author_name) && (
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
                                              minHeight={100}
                                            />
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

                              <Space direction="vertical" size="small" style={{ width: '100%', marginTop: 12 }}>
                                <SmartTicketEditor
                                  value={commentDraft}
                                  onChange={setCommentDraft}
                                  placeholder="Добавьте комментарий"
                                  mentions={mentionOptions}
                                  onImageUpload={uploadInlineImage}
                                  minHeight={100}
                                />
                                <Checkbox checked={commentIsPrivate} onChange={(event) => setCommentIsPrivate(event.target.checked)}>
                                  Приватный комментарий (не синхронизировать во внешние системы)
                                </Checkbox>
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
                            </>
                          ),
                        },
                        {
                          key: 'attachments',
                          label: `Вложения (${attachments.length})`,
                          children: (
                            <>
                              <div style={{ marginBottom: 12 }}>
                                <Upload
                                  showUploadList={false}
                                  customRequest={uploadAttachmentsRequest}
                                  multiple
                                >
                                  <Button icon={<PaperClipOutlined />} loading={uploadAttachmentsMutation.isPending}>
                                    Прикрепить
                                  </Button>
                                </Upload>
                              </div>
                              <Upload.Dragger
                                name="files"
                                multiple
                                showUploadList={false}
                                customRequest={uploadAttachmentsRequest}
                                style={{ marginBottom: 12 }}
                              >
                                <p style={{ marginBottom: 4 }}>Перетащите файлы сюда или нажмите для выбора</p>
                                <Text type="secondary">Поддерживается множественная загрузка</Text>
                              </Upload.Dragger>
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
                                        <Text type="secondary">{dayjs(item.created_at).format('DD.MM.YYYY HH:mm')} в {historySourceLabel(item.source)}</Text>
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

                <Card size="small" className="ticket-overview-side-card" title="Подключения и оборудование">
                  <Tabs
                    size="small"
                    items={[
                      {
                        key: 'overview-connections',
                        label: 'Подключения',
                        children: (
                          (isInfraLoading || isParentInfraLoading) ? (
                            <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
                          ) : ownConnectionCards.length === 0 && parentConnectionCards.length === 0 ? (
                            <Empty description="Подключения не найдены" />
                          ) : (
                            <Space direction="vertical" size={12} style={{ width: '100%' }}>
                              {parentConnectionCards.length > 0 && (
                                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                  <Text strong>Chain</Text>
                                  <div className="ticket-overview-connection-grid">
                                    {parentConnectionCards.map((group) => (
                                      <Card key={group.key} size="small" className="glass-panel">
                                        <Space direction="vertical" size={6} style={{ width: '100%' }}>
                                          <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                                            <Text strong>{group.title}</Text>
                                            <Tag color="geekblue">Сервер</Tag>
                                          </Space>
                                          {group.rows.map((row) => (
                                            <Paragraph
                                              key={`${group.key}-${row.field}-${row.value}`}
                                              style={{ margin: 0 }}
                                              copyable={{
                                                text: row.value,
                                                onCopy: () => {
                                                  copyConnectionMutation.mutate({
                                                    label: row.label,
                                                    value: row.value,
                                                    entityType: group.entityType,
                                                    entityID: group.entityID,
                                                    connectionField: row.field,
                                                  });
                                                },
                                              }}
                                            >
                                              <Text type="secondary">{row.label}:</Text> {row.value}
                                            </Paragraph>
                                          ))}
                                        </Space>
                                      </Card>
                                    ))}
                                  </div>
                                </Space>
                              )}

                              {ownConnectionCards.length > 0 && (
                                <div className="ticket-overview-connection-grid">
                                  {ownConnectionCards.map((group) => (
                                    <Card key={group.key} size="small" className="glass-panel">
                                      <Space direction="vertical" size={6} style={{ width: '100%' }}>
                                        <Space style={{ justifyContent: 'space-between', width: '100%' }}>
                                          <Text strong>{group.title}</Text>
                                          <Tag color={group.entityType === 'Server' ? 'geekblue' : 'cyan'}>
                                            {group.entityType === 'Server' ? 'Сервер' : 'Станция'}
                                          </Tag>
                                        </Space>
                                        {group.rows.map((row) => (
                                          <Paragraph
                                            key={`${group.key}-${row.field}-${row.value}`}
                                            style={{ margin: 0 }}
                                            copyable={{
                                              text: row.value,
                                              onCopy: () => {
                                                copyConnectionMutation.mutate({
                                                  label: row.label,
                                                  value: row.value,
                                                  entityType: group.entityType,
                                                  entityID: group.entityID,
                                                  connectionField: row.field,
                                                });
                                              },
                                            }}
                                          >
                                            <Text type="secondary">{row.label}:</Text> {row.value}
                                          </Paragraph>
                                        ))}
                                      </Space>
                                    </Card>
                                  ))}
                                </div>
                              )}
                            </Space>
                          )
                        ),
                      },
                      {
                        key: 'overview-equipment',
                        label: 'Оборудование',
                        children: (
                          isInfraLoading ? (
                            <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
                          ) : (
                            <Space direction="vertical" size={12} style={{ width: '100%' }}>
                              {serverItems.length > 0 && (
                                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                  <Text strong>Серверы</Text>
                                  <div className="ticket-overview-equipment-grid">
                                    {serverItems.map((item) => {
                                      const dataRow = item.data as Record<string, string | undefined>;
                                      const path = resolveEntityPath(item);
                                      return (
                                        <Card
                                          key={`equip-server-${dataRow.uuid || resolveEntityTitle(item)}`}
                                          size="small"
                                          hoverable
                                          className="glass-panel"
                                          onClick={() => {
                                            if (!path) return;
                                            navigate(path, { state: { backTo: `${location.pathname}${location.search}` } });
                                          }}
                                        >
                                          <Space direction="vertical" size={6} style={{ width: '100%' }}>
                                            <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                                              <Text strong>{resolveEntityTitle(item)}</Text>
                                              <Tag color="geekblue">Сервер</Tag>
                                            </Space>
                                            {dataRow.partners_link ? (
                                              <a href={dataRow.partners_link} target="_blank" rel="noreferrer" onClick={(event) => event.stopPropagation()}>
                                                Партнёрский портал
                                              </a>
                                            ) : (
                                              <Text type="secondary">Партнёрский портал: -</Text>
                                            )}
                                            <Paragraph copyable={dataRow.unique_id ? { text: dataRow.unique_id } : false} style={{ margin: 0 }}>
                                              <Text type="secondary">UniqueID:</Text> {dataRow.unique_id || '-'}
                                            </Paragraph>
                                            <Paragraph copyable={dataRow.server_version ? { text: dataRow.server_version } : false} style={{ margin: 0 }}>
                                              <Text type="secondary">Версия:</Text> {dataRow.server_version || '-'}
                                            </Paragraph>
                                            <Paragraph copyable={dataRow.ip ? { text: dataRow.ip } : false} style={{ margin: 0 }}>
                                              <Text type="secondary">Адрес:</Text> {dataRow.ip || '-'}
                                            </Paragraph>
                                          </Space>
                                        </Card>
                                      );
                                    })}
                                  </div>
                                </Space>
                              )}

                              {workstationItems.length > 0 && (
                                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                  <Text strong>Рабочие станции</Text>
                                  <div className="ticket-overview-equipment-grid">
                                    {workstationItems.map((item) => {
                                      const dataRow = item.data as Record<string, string | undefined>;
                                      const path = resolveEntityPath(item);
                                      const workstationID = String(dataRow.uuid || '').trim();
                                      return (
                                        <Card
                                          key={`equip-workstation-${workstationID || resolveEntityTitle(item)}`}
                                          size="small"
                                          hoverable
                                          className="glass-panel"
                                          onClick={() => {
                                            if (!path) return;
                                            navigate(path, { state: { backTo: `${location.pathname}${location.search}` } });
                                          }}
                                        >
                                          <Space direction="vertical" size={6} style={{ width: '100%' }}>
                                            <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                                              <Text strong>Рабочая станция</Text>
                                              <Tag color="cyan">РС</Tag>
                                            </Space>
                                            <div onClick={(event) => event.stopPropagation()}>
                                              <InlineFieldEditor
                                                value={dataRow.device_name || workstationID || 'Рабочая станция'}
                                                onSave={(value) => {
                                                  if (!workstationID) return;
                                                  updateWorkstationNameMutation.mutate({ workstationID, deviceName: value });
                                                }}
                                                saving={updateWorkstationNameMutation.isPending}
                                              />
                                            </div>
                                            <Text type="secondary">AnyDesk: {dataRow.anydesk || '-'}</Text>
                                            <Text type="secondary">TeamViewer: {dataRow.teamviewer || '-'}</Text>
                                            <Text type="secondary">LiteManager: {dataRow.litemanager || '-'}</Text>
                                          </Space>
                                        </Card>
                                      );
                                    })}
                                  </div>
                                </Space>
                              )}

                              {fiscalItems.length > 0 && (
                                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                                  <Text strong>Фискальные регистраторы</Text>
                                  <div className="ticket-overview-equipment-grid">
                                    {fiscalItems.map((item) => {
                                      const dataRow = item.data as Record<string, string | undefined>;
                                      const path = resolveEntityPath(item);
                                      return (
                                        <Card
                                          key={`equip-fiscal-${dataRow.uuid || dataRow.serial_number || dataRow.rn_kkt}`}
                                          size="small"
                                          hoverable
                                          className="glass-panel"
                                          onClick={() => {
                                            if (!path) return;
                                            navigate(path, { state: { backTo: `${location.pathname}${location.search}` } });
                                          }}
                                        >
                                          <Space direction="vertical" size={4} style={{ width: '100%' }}>
                                            <Space style={{ width: '100%', justifyContent: 'space-between' }}>
                                              <Text strong>{dataRow.model_kkt || 'ККТ'}</Text>
                                              <Tag color="gold">ФР</Tag>
                                            </Space>
                                            <Text type="secondary">РНМ: {dataRow.rn_kkt || '-'}</Text>
                                            <Text type="secondary">SN: {dataRow.serial_number || '-'}</Text>
                                            <Text type="secondary">
                                              ФН до: {dataRow.fn_expire_date ? dayjs(dataRow.fn_expire_date).format('DD.MM.YYYY') : '-'}
                                            </Text>
                                          </Space>
                                        </Card>
                                      );
                                    })}
                                  </div>
                                </Space>
                              )}

                              {serverItems.length === 0 && workstationItems.length === 0 && fiscalItems.length === 0 && (
                                <Empty description="Оборудование не найдено" />
                              )}
                            </Space>
                          )
                        ),
                      },
                    ]}
                  />
                </Card>
      </div>

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

      <Modal
        open={Boolean(pendingStatus)}
        title="Отчёт по заявке"
        okText="Завершить заявку"
        cancelText="Отмена"
        onCancel={() => {
          setPendingStatus(null);
          setStatusComment('');
        }}
        confirmLoading={changeStatusMutation.isPending}
        okButtonProps={{ disabled: !statusComment.trim() }}
        onOk={() => {
          if (!id || !pendingStatus || !statusComment.trim()) return;
          changeStatusMutation.mutate({ id, status: pendingStatus, comment: statusComment.trim() });
        }}
      >
        <Input.TextArea
          rows={4}
          value={statusComment}
          onChange={(event) => setStatusComment(event.target.value)}
          placeholder="Добавьте отчёт по выполнению"
        />
      </Modal>

      <NewTicketModal
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
    </Space>
  );
};

export default TicketDetailsPage;
