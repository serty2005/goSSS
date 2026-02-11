import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Col, Descriptions, Empty, Input, List, Modal, Row, Select, Space, Spin, Tabs, Tag, Typography, Upload, message } from 'antd';
import { CheckOutlined, CloseOutlined, EditOutlined, PaperClipOutlined } from '@ant-design/icons';
import type { UploadProps } from 'antd';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import dayjs from 'dayjs';
import { ticketsApi } from '@/api/tickets';
import { companiesApi } from '@/api/companies';
import { CompanyModel, InfrastructureItem, TicketDetailsDTO, TicketHistoryDTO, TicketStatus } from '@/types/api';
import { getCompanyHierarchyParts, resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Title, Text, Paragraph } = Typography;
type UploadRequestOption = Parameters<NonNullable<UploadProps['customRequest']>>[0];

const STATUS_OPTIONS: Array<{ value: TicketStatus; label: string; color: string }> = [
  { value: 'new', label: 'Новая', color: 'blue' },
  { value: 'in_progress', label: 'В работе', color: 'processing' },
  { value: 'pending', label: 'Ожидание', color: 'orange' },
  { value: 'resolved', label: 'Решена', color: 'green' },
  { value: 'closed', label: 'Закрыта', color: 'default' },
];

const sanitizeRichHtml = (value?: string) => {
  if (!value) return '';
  return value
    .replace(/<script[\s\S]*?>[\s\S]*?<\/script>/gi, '')
    .replace(/\son\w+="[^"]*"/gi, '')
    .replace(/\son\w+='[^']*'/gi, '')
    .replace(/javascript:/gi, '')
    .replace(/(src|href)=["']\/static\//gi, '$1="/api/static/')
    .replace(/(src|href)=["']static\//gi, '$1="/api/static/');
};
const isClosedLikeStatus = (status?: string) => status === 'resolved' || status === 'closed';

const historyLabel = (entry: TicketHistoryDTO) => {
  switch (entry.action) {
    case 'comment_added':
      return 'Комментарий добавлен';
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

const TicketDetailsPage: React.FC = () => {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();

  const [commentDraft, setCommentDraft] = useState('');
  const [statusComment, setStatusComment] = useState('');
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [companySearch, setCompanySearch] = useState('');
  const [isCompanyEditMode, setIsCompanyEditMode] = useState(false);
  const [draftCompanyID, setDraftCompanyID] = useState<string | undefined>(undefined);

  const { data, isLoading } = useQuery({
    queryKey: ['ticket', id],
    queryFn: () => ticketsApi.getTicket(id),
    enabled: Boolean(id),
  });

  const details: TicketDetailsDTO | undefined = data?.data;
  const metadata = details?.metadata;

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
    const companyData = companyResponse?.data as { Title?: string; title?: string; AdditionalName?: string; additional_name?: string } | undefined;
    return (
      companyData?.Title ||
      companyData?.title ||
      companyData?.AdditionalName ||
      companyData?.additional_name ||
      details?.company_name ||
      metadata?.company_name ||
      metadata?.company_id ||
      ''
    );
  }, [companyResponse?.data, details?.company_name, metadata?.company_id, metadata?.company_name]);

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
          { label: 'RDP', value: dataRow.rdp },
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
          id: String(item.id || item.ID || ''),
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
      return ticketsApi.addComment(id, commentDraft.trim());
    },
    onSuccess: () => {
      message.success('Комментарий добавлен');
      setCommentDraft('');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось добавить комментарий'),
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

  const refreshCommentsMutation = useMutation({
    mutationFn: async () => {
      if (!id) return;
      return ticketsApi.refreshCommentsFromServiceDesk(id);
    },
    onSuccess: (response) => {
      const added = response?.data?.added ?? 0;
      message.success(added > 0 ? `Комментарии обновлены: +${added}` : 'Новых комментариев нет');
      queryClient.invalidateQueries({ queryKey: ['ticket', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => message.error('Не удалось обновить комментарии из Naumen-SD'),
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

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: { label: string; value: string }) => {
      if (!id) return;
      return ticketsApi.recordConnectionCopy(id, payload.label, payload.value);
    },
  });

  if (isLoading || !details || !metadata) {
    return <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>;
  }

  const statusMeta = STATUS_OPTIONS.find((item) => item.value === metadata.status) || STATUS_OPTIONS[0];

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

  const insertImageRequest = async (options: UploadRequestOption) => {
    const source = options.file as File;
    try {
      const response = await uploadAttachmentsMutation.mutateAsync([source]);
      const uploaded = response.data?.items?.[0];
      if (uploaded?.file_path) {
        const imageTag = `<p><img src="${uploaded.file_path}" alt="${uploaded.file_name}" /></p>`;
        setCommentDraft((prev) => `${prev.trim()}\n${imageTag}`.trim());
      }
      options.onSuccess?.(uploaded || {});
    } catch (error) {
      options.onError?.(error as Error);
    }
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space direction="vertical" size={0}>
            <Title level={4} style={{ margin: 0 }}>Заявка #{metadata.number}</Title>
            <Text type="secondary">Создана {dayjs(metadata.created_at).format('DD.MM.YYYY HH:mm')}</Text>
          </Space>

          <Space>
            <Tag color={statusMeta.color}>{statusMeta.label}</Tag>
            {metadata.is_common_contract && <Tag color="gold">Платный</Tag>}
            <Select
              value={metadata.status}
              options={STATUS_OPTIONS.map((item) => ({ value: item.value, label: item.label }))}
              style={{ width: 180 }}
              onChange={(nextStatus: TicketStatus) => {
                if (!id || nextStatus === metadata.status) return;
                if (nextStatus === 'resolved' || nextStatus === 'closed') {
                  setPendingStatus(nextStatus);
                  return;
                }
                changeStatusMutation.mutate({ id, status: nextStatus });
              }}
            />
            {metadata.status !== 'resolved' && (
              <Button onClick={() => setPendingStatus('resolved')}>
                Закрыть заявку
              </Button>
            )}
            <Button onClick={() => navigate('/tickets')}>К списку</Button>
          </Space>
        </Space>

        <Descriptions style={{ marginTop: 16 }} column={2} bordered size="small">
          <Descriptions.Item label="Компания">
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
          </Descriptions.Item>
          <Descriptions.Item label="Контракт">
            {metadata.is_common_contract ? 'Общий контракт' : metadata.contract_id || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Исполнитель">
            {metadata.assignee?.fullName || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Обновлена">
            {dayjs(metadata.updated_at).format('DD.MM.YYYY HH:mm')}
          </Descriptions.Item>
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
                  <div dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(metadata.description || '<span>Нет описания</span>') }} />
                </Card>

                {isClosedLikeStatus(metadata.status) && (
                  <Card size="small" title="Результат">
                    <div dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(metadata.result || '<span>Результат не заполнен</span>') }} />
                  </Card>
                )}

                <Card
                  size="small"
                  title="Комментарии"
                  extra={(
                    <Button
                      size="small"
                      loading={refreshCommentsMutation.isPending}
                      onClick={() => refreshCommentsMutation.mutate()}
                    >
                      Обновить из SD
                    </Button>
                  )}
                >
                  {details.comments?.length ? (
                    <List
                      dataSource={details.comments}
                      renderItem={(item) => (
                        <List.Item key={item.uuid}>
                          <Space direction="vertical" size={2} style={{ width: '100%' }}>
                            <Text type="secondary">{item.author_name || 'Сотрудник'} • {dayjs(item.creation_date).format('DD.MM.YYYY HH:mm')}</Text>
                            <div dangerouslySetInnerHTML={{ __html: sanitizeRichHtml(item.text) }} />
                          </Space>
                        </List.Item>
                      )}
                    />
                  ) : (
                    <Empty description="Комментариев нет" />
                  )}

                  <Space direction="vertical" size="small" style={{ width: '100%', marginTop: 12 }}>
                    <Input.TextArea
                      rows={3}
                      value={commentDraft}
                      onChange={(event) => setCommentDraft(event.target.value)}
                      placeholder="Добавьте комментарий"
                    />
                    <Space>
                      <Upload
                        showUploadList={false}
                        customRequest={insertImageRequest}
                        accept="image/*"
                        multiple
                      >
                        <Button icon={<PaperClipOutlined />}>
                          Вставить изображение
                        </Button>
                      </Upload>
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
                          <Text type="secondary">{dayjs(item.created_at).format('DD.MM.YYYY HH:mm')}</Text>
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
                              <Text type="secondary">РНМ: {dataRow.rn_kkt || '-'}</Text>
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
    </Space>
  );
};

export default TicketDetailsPage;

