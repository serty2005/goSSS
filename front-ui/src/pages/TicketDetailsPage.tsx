import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Checkbox, Col, Descriptions, Empty, Input, List, Modal, Row, Select, Space, Spin, Tabs, Tag, Tooltip, Typography, Upload, message } from 'antd';
import { CheckOutlined, CloseOutlined, EditOutlined, LinkOutlined, PaperClipOutlined } from '@ant-design/icons';
import type { UploadProps } from 'antd';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { companiesApi } from '@/api/companies';
import { contractsApi } from '@/api/contracts';
import { usersApi } from '@/api/users';
import { CompanyModel, InfrastructureItem, TicketDetailsDTO, TicketHistoryDTO, TicketStatus } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import SmartTicketEditor from '@/features/tickets/editor/SmartTicketEditor';
import type { MentionOption } from '@/features/tickets/editor/mentions';
import { SafeHtmlContent } from '@/utils/safeHtml';

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
      if (entry.field === 'status') return 'РР·РјРµРЅС‘РЅ статус';
      if (entry.field === 'description') return 'РР·РјРµРЅРµРЅРѕ описание';
      if (entry.field === 'assignee') return 'РР·РјРµРЅС‘РЅ исполнитель';
      if (entry.field === 'company') return 'РР·РјРµРЅРµРЅР° компания';
      if (entry.field === 'asset') return 'РР·РјРµРЅРµРЅРѕ оборудование';
      return 'РР·РјРµРЅРµРЅРёРµ заявки';
  }
};

const historySourceLabel = (source?: string) => {
  if (source === 'ui') return 'UI';
  if (source === 'bitrix') return 'Bitrix24';
  if (source === 'servicedesk') return 'ServiceDesk';
  return 'System';
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

  const infrastructure = infraResponse?.data || [];

  const { data: companyResponse } = useQuery({
    queryKey: ['company-profile', metadata?.company_id],
    queryFn: () => companiesApi.getCompany(metadata?.company_id || ''),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

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

  const connectionCards = useMemo(() => {
    return infrastructure
      .map((item) => {
        if (item.entity_type !== 'Server' && item.entity_type !== 'Workstation') {
          return null;
        }
        const dataRow = item.data as Record<string, string | undefined>;
        const rows = [
          ...(item.entity_type === 'Server' ? [{ label: 'IP', value: dataRow.ip }] : []),
          { label: 'AnyDesk', value: dataRow.anydesk },
          { label: 'TeamViewer', value: dataRow.teamviewer },
          { label: 'rdp', value: dataRow.rdp },
          { label: 'LM', value: dataRow.litemanager },
        ].filter((entry) => entry.value);
        if (rows.length === 0) return null;

        return {
          key: `${item.entity_type}-${dataRow.uuid || resolveEntityTitle(item)}`,
          title: resolveEntityTitle(item),
          path: resolveEntityPath(item),
          rows,
        };
      })
      .filter(Boolean) as Array<{
      key: string;
      title: string;
      path: string;
      rows: Array<{ label: string; value?: string }>;
    }>;
  }, [infrastructure]);

  const serverItems = useMemo(() => infrastructure.filter((item) => item.entity_type === 'Server'), [infrastructure]);
  const fiscalItems = useMemo(() => infrastructure.filter((item) => item.entity_type === 'FiscalRegister'), [infrastructure]);

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
      if (!id || !commentDraft.trim()) return;
      return ticketsApi.addComment(id, commentDraft.trim(), commentIsPrivate);
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

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: { label: string; value: string }) => {
      if (!id) return;
      return ticketsApi.recordConnectionCopy(id, payload.label, payload.value);
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
      content: 'РРЅС„РѕСЂРјР°С†РёСЏ о пользователе будет доступна в следующем обновлении.',
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
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space direction="vertical" size={0}>
            <Space align="center" size={8}>
              <Title level={4} style={{ margin: 0 }}>Заявка #{metadata.number}</Title>
              <BitrixSyncIndicator sync={metadata.sync_with_bitrix} dealURL={metadata.bitrix_deal_url} />
            </Space>
            <Text type="secondary">Создана {dayjs(metadata.created_at).format('DD.MM.YYYY HH:mm')}</Text>
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
                      setPendingStatus(nextStatus);
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

        <Descriptions style={{ marginTop: 16 }} column={2} bordered size="small">
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
      </Card>

      <Tabs
        items={[
          {
            key: 'overview',
            label: 'Обзор',
            children: (
              <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                <Card size="small" title="Описание">
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
                          Р едактировать описание
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
                </Card>

                {isClosedLikeStatus(metadata.status) && (
                  <Card size="small" title="Результат">
                    <div style={highlightedFields.result ? fieldHighlightStyle : undefined}>
                      <SafeHtmlContent html={metadata.result || '<span>Результат не заполнен</span>'} style={{ whiteSpace: 'pre-wrap' }} />
                    </div>
                  </Card>
                )}

                <Card
                  size="small"
                  title="Комментарии"
                >
                  {details.comments?.length ? (
                    <List
                      dataSource={details.comments}
                      renderItem={(item) => (
                        <List.Item key={item.uuid} style={highlightedComments[item.uuid] ? fieldHighlightStyle : undefined}>
                          <Space direction="vertical" size={2} style={{ width: '100%' }}>
                            <Space size={8}>
                              <Text type="secondary">{item.author_name || 'Сотрудник'} в {dayjs(item.creation_date).format('DD.MM.YYYY HH:mm')}</Text>
                              {item.is_private && <Tag color="orange">Приватный</Tag>}
                            </Space>
                            <SafeHtmlContent html={item.text} style={{ whiteSpace: 'pre-wrap' }} />
                          </Space>
                        </List.Item>
                      )}
                    />
                  ) : (
                    <Empty description="Комментариев нет" />
                  )}

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
                        disabled={!commentDraft.trim()}
                        onClick={() => addCommentMutation.mutate()}
                      >
                        Отправить
                      </Button>
                    </Space>
                  </Space>
                </Card>
              </Space>
            ),
          },
          {
            key: 'history',
            label: 'История',
            children: (
              <Card size="small" title="История изменений">
                {(details.history || []).length === 0 ? (
                  <Empty description="История пока пуста" />
                ) : (
                  <List
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
              </Card>
            ),
          },
          {
            key: 'attachments',
            label: 'Вложения',
            children: (
              <Card
                size="small"
                title={`Вложения (${attachments.length})`}
                extra={(
                  <Upload
                    showUploadList={false}
                    customRequest={uploadAttachmentsRequest}
                    multiple
                  >
                    <Button icon={<PaperClipOutlined />} loading={uploadAttachmentsMutation.isPending}>
                      Прикрепить
                    </Button>
                  </Upload>
                )}
              >
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
                  <Empty description="Вложений нет" />
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
              </Card>
            ),
          },
          {
            key: 'connections',
            label: 'Подключения',
            children: (
              <Card size="small" title="Подключения">
                {isInfraLoading ? (
                  <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
                ) : connectionCards.length === 0 ? (
                  <Empty description="Подключения не найдены" />
                ) : (
                  <Row gutter={[12, 12]}>
                    {connectionCards.map((group) => (
                      <Col key={group.key} xs={24} md={12} xl={8}>
                        <Card
                          hoverable
                          className="glass-panel"
                          onClick={() => {
                            if (!group.path) return;
                            navigate(group.path, { state: { backTo: `${location.pathname}${location.search}` } });
                          }}
                        >
                          <Space direction="vertical" size={2} style={{ width: '100%' }}>
                            <Text strong>{group.title}</Text>
                            {group.rows.map((row) => (
                              <Paragraph
                                key={`${group.key}-${row.label}-${row.value}`}
                                style={{ margin: 0 }}
                                copyable={row.value ? {
                                  text: row.value,
                                  onCopy: () => {
                                    if (!row.value) return;
                                    copyConnectionMutation.mutate({ label: row.label, value: row.value });
                                  },
                                } : false}
                              >
                                <Text type="secondary">{row.label}:</Text> {row.value}
                              </Paragraph>
                            ))}
                          </Space>
                        </Card>
                      </Col>
                    ))}
                  </Row>
                )}
              </Card>
            ),
          },
          {
            key: 'servers',
            label: 'Серверы',
            children: (
              <Card size="small" title="Серверы компании">
                {isInfraLoading ? (
                  <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
                ) : serverItems.length === 0 ? (
                  <Empty description="Серверы не найдены" />
                ) : (
                  <Row gutter={[12, 12]}>
                    {serverItems.map((item) => {
                      const dataRow = item.data as Record<string, string | undefined>;
                      const path = resolveEntityPath(item);
                      return (
                        <Col key={dataRow.uuid || dataRow.server_name || dataRow.device_name} xs={24} md={12} xl={8}>
                          <Card
                            hoverable
                            className="glass-panel"
                            onClick={() => {
                              if (!path) return;
                              navigate(path, { state: { backTo: `${location.pathname}${location.search}` } });
                            }}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%' }}>
                              <Text strong>{resolveEntityTitle(item)}</Text>
                              <Text type="secondary">IP: {dataRow.ip || '-'}</Text>
                              <Text type="secondary">AnyDesk: {dataRow.anydesk || '-'}</Text>
                              <Text type="secondary">TeamViewer: {dataRow.teamviewer || '-'}</Text>
                            </Space>
                          </Card>
                        </Col>
                      );
                    })}
                  </Row>
                )}
              </Card>
            ),
          },
          {
            key: 'fiscals',
            label: 'Фискальники',
            children: (
              <Card size="small" title="Фискальные регистраторы">
                {isInfraLoading ? (
                  <div style={{ textAlign: 'center', padding: 12 }}><Spin /></div>
                ) : fiscalItems.length === 0 ? (
                  <Empty description="Фискальные регистраторы не найдены" />
                ) : (
                  <Row gutter={[12, 12]}>
                    {fiscalItems.map((item) => {
                      const dataRow = item.data as Record<string, string | undefined>;
                      const path = resolveEntityPath(item);
                      return (
                        <Col key={dataRow.uuid || dataRow.serial_number || dataRow.rn_kkt} xs={24} md={12} xl={8}>
                          <Card
                            hoverable
                            className="glass-panel"
                            onClick={() => {
                              if (!path) return;
                              navigate(path, { state: { backTo: `${location.pathname}${location.search}` } });
                            }}
                          >
                            <Space direction="vertical" size={2} style={{ width: '100%' }}>
                              <Text strong>{dataRow.model_kkt || 'ККТ'}</Text>
                              <Text type="secondary">Р НМ: {dataRow.rn_kkt || '-'}</Text>
                              <Text type="secondary">SN: {dataRow.serial_number || '-'}</Text>
                              <Text type="secondary">
                                ФН до: {dataRow.fn_expire_date ? dayjs(dataRow.fn_expire_date).format('DD.MM.YYYY') : '-'}
                              </Text>
                            </Space>
                          </Card>
                        </Col>
                      );
                    })}
                  </Row>
                )}
              </Card>
            ),
          },
        ]}
      />

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
        title="Завершение заявки"
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
          placeholder="Опишите итог выполнения заявки"
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
