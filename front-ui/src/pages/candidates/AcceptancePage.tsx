import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Divider,
  Drawer,
  Empty,
  Form,
  Input,
  List,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import dayjs from 'dayjs';
import { candidatesApi } from '@/api/candidates';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import {
  CandidateApprovePayload,
  CandidateDTO,
  CandidateStatus,
  CandidateWorkstationStagingDTO,
  CompanyModel,
} from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Title, Text } = Typography;
type CandidateFilter = 'ACTIVE' | CandidateStatus | 'ALL';
type CompanyMode = 'existing' | 'new';
type ServerMode = 'existing' | 'new';

const STATUS_COLORS: Record<CandidateStatus, string> = {
  NEW: 'blue',
  IN_REVIEW: 'orange',
  APPROVED: 'green',
  REJECTED: 'red',
  CANCELLED: 'default',
};

// normalizeCandidate приводит ответ backend к формату фронта:
// поддерживает как snake_case, так и поля в PascalCase.
const normalizeCandidate = (raw: Record<string, unknown>): CandidateDTO => {
  const asNumber = (v: unknown): number => Number(v || 0);
  const asString = (v: unknown): string => String(v || '');
  const pick = (...keys: string[]) => keys.map((k) => raw[k]).find((v) => v !== undefined);

  return {
    id: asNumber(pick('id', 'ID')),
    server_key: pick('server_key', 'ServerKey') as string | undefined,
    server_crm_id: pick('server_crm_id', 'ServerCRMID') as string | undefined,
    server_url: pick('server_url', 'ServerURL') as string | undefined,
    status: asString(pick('status', 'Status')) as CandidateStatus,
    ticket_id: pick('ticket_id', 'TicketID') as number | undefined,
    approved_company_id: pick('approved_company_id', 'ApprovedCompanyID') as string | undefined,
    approved_server_id: pick('approved_server_id', 'ApprovedServerID') as string | undefined,
    created_at: asString(pick('created_at', 'CreatedAt')),
    updated_at: asString(pick('updated_at', 'UpdatedAt')),
    staged_workstations: (pick('staged_workstations') as CandidateDTO['staged_workstations']) || [],
    staged_fiscals: (pick('staged_fiscals') as CandidateDTO['staged_fiscals']) || [],
  };
};

const AcceptancePage: React.FC = () => {
  const queryClient = useQueryClient();

  const [status, setStatus] = useState<CandidateFilter>('ACTIVE');
  const [selectedCandidateID, setSelectedCandidateID] = useState<number | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [companyMode, setCompanyMode] = useState<CompanyMode>('existing');
  const [serverMode, setServerMode] = useState<ServerMode>('new');
  const [companySearch, setCompanySearch] = useState('');
  const [serverSearch, setServerSearch] = useState('');

  const [form] = Form.useForm();

  const {
    data: candidatesData,
    isLoading: isCandidatesLoading,
    error: candidatesError,
    refetch: refetchCandidates,
  } = useQuery({
    queryKey: ['candidates', status],
    queryFn: () => candidatesApi.listCandidates({ status, limit: 200, offset: 0 }),
    staleTime: 15_000,
  });

  const {
    data: candidateDetails,
    isLoading: isCandidateLoading,
    error: candidateError,
  } = useQuery({
    queryKey: ['candidate', selectedCandidateID],
    queryFn: () => candidatesApi.getCandidate(selectedCandidateID as number),
    enabled: Boolean(selectedCandidateID),
    staleTime: 5_000,
  });

  const { data: companiesData, isLoading: isCompaniesLoading } = useQuery({
    queryKey: ['acceptance', 'companies', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 30, 0),
    staleTime: 15_000,
  });

  const { data: serversData, isLoading: isServersLoading } = useQuery({
    queryKey: ['acceptance', 'servers', serverSearch],
    queryFn: () => equipmentApi.listServers(serverSearch, 30, 0),
    staleTime: 15_000,
  });

  const approveMutation = useMutation({
    mutationFn: async (payload: CandidateApprovePayload) => {
      if (!selectedCandidateID) throw new Error('Кандидат не выбран');
      return candidatesApi.approveCandidate(selectedCandidateID, payload);
    },
    onSuccess: () => {
      message.success('Кандидат успешно принят на АО');
      setDrawerOpen(false);
      setSelectedCandidateID(null);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['candidates'] });
    },
    onError: () => {
      message.error('Не удалось подтвердить кандидата');
    },
  });

  const candidates = useMemo(() => {
    const rows = (candidatesData?.data || []) as unknown as Array<Record<string, unknown>>;
    return rows.map(normalizeCandidate);
  }, [candidatesData?.data]);

  const selectedCandidate = useMemo(() => {
    if (!candidateDetails?.data) return undefined;
    return normalizeCandidate(candidateDetails.data as unknown as Record<string, unknown>);
  }, [candidateDetails?.data]);

  useEffect(() => {
    if (!selectedCandidate) {
      return;
    }
    setCompanyMode('existing');
    setServerMode('new');

    const wsDefaults = (selectedCandidate.staged_workstations || []).map((item) => ({
      staging_id: item.id,
      workstation_uuid: item.workstation_uuid,
      name: item.hostname || '',
    }));

    form.setFieldsValue({
      company_mode: 'existing',
      server_mode: 'new',
      server_crm_id: selectedCandidate.server_crm_id || '',
      server_url_rms: selectedCandidate.server_url || '',
      server_device_name: '',
      server_description: '',
      workstations: wsDefaults.length ? wsDefaults : [{ name: '' }],
      comment: '',
    });
  }, [form, selectedCandidate]);

  const companyOptions = useMemo(() => {
    const list = companiesData?.data || [];
    return list
      .map((item) => {
        const company = item as CompanyModel;
        const id = resolveCompanyID(company);
        if (!id) return null;
        const title = resolveCompanyTitle(company) || id;
        const parentTitle = resolveCompanyParentTitle(company);
        return {
          value: id,
          label: parentTitle ? `${parentTitle} / ${title}` : title,
        };
      })
      .filter(Boolean) as Array<{ value: string; label: string }>;
  }, [companiesData?.data]);

  const serverOptions = useMemo(() => {
    const list = serversData?.data || [];
    return list.map((raw) => {
      const row = raw as Record<string, string | undefined>;
      const value = row.id || row.ID || '';
      const name = row.device_name || row.server_name || 'Сервер';
      const crm = row.crm_id || row.crm_id || '';
      const ip = row.ip || row.IP || '';
      return {
        value,
        label: `${name}${crm ? ` | CRM: ${crm}` : ''}${ip ? ` | ${ip}` : ''}`,
      };
    }).filter((item) => item.value);
  }, [serversData?.data]);

  const openCandidate = (candidateID: number) => {
    if (!candidateID) {
      message.error('Не удалось открыть кандидата: отсутствует идентификатор');
      return;
    }
    setSelectedCandidateID(candidateID);
    setDrawerOpen(true);
  };

  const onSubmit = async () => {
    const values = await form.validateFields();

    const payload: CandidateApprovePayload = {
      comment: values.comment?.trim() || '',
      workstations: (values.workstations || [])
        .map((item: { staging_id?: number; workstation_uuid?: string; name?: string }) => ({
          staging_id: item.staging_id,
          workstation_uuid: item.workstation_uuid,
          name: (item.name || '').trim(),
        }))
        .filter((item: { name: string }) => item.name !== ''),
    };

    if (values.company_mode === 'existing') {
      payload.company_id = values.company_id;
    } else {
      payload.company = {
        title: values.new_company_title,
        address: values.new_company_address || '',
        additional_name: values.new_company_additional_name || '',
        parent_id: values.new_company_parent_id || '',
      };
    }

    if (values.server_mode === 'existing') {
      payload.server = {
        mode: 'existing',
        server_id: values.server_id,
      };
    } else {
      payload.server = {
        mode: 'new',
        crm_id: values.server_crm_id || '',
        url_rms: values.server_url_rms || '',
        device_name: values.server_device_name || '',
        description: values.server_description || '',
      };
    }

    approveMutation.mutate(payload);
  };

  const columns = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      width: 140,
      render: (value: CandidateStatus) => <Tag color={STATUS_COLORS[value] || 'default'}>{value}</Tag>,
    },
    {
      title: 'CRM ID',
      dataIndex: 'server_crm_id',
      render: (value?: string) => value || '-',
    },
    {
      title: 'Адрес сервера',
      dataIndex: 'server_url',
      render: (value?: string) => value || '-',
    },
    {
      title: 'Создан',
      dataIndex: 'created_at',
      width: 180,
      render: (value: string) => dayjs(value).format('DD.MM.YYYY HH:mm'),
    },
    {
      title: 'Действие',
      key: 'action',
      width: 170,
      render: (_: unknown, row: CandidateDTO) => (
        <Button type="primary" onClick={() => openCandidate(row.id)}>
          Принять на АО
        </Button>
      ),
    },
  ];

  const stagedWorkstations = selectedCandidate?.staged_workstations || [];
  const stagedFiscals = selectedCandidate?.staged_fiscals || [];

  return (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
        <Title level={4} style={{ margin: 0 }}>Принятие на АО</Title>
        <Space>
          <Select<CandidateFilter>
            value={status}
            style={{ width: 220 }}
            onChange={(value) => setStatus(value)}
            options={[
              { value: 'ACTIVE', label: 'Активные (NEW, IN_REVIEW)' },
              { value: 'NEW', label: 'NEW' },
              { value: 'IN_REVIEW', label: 'IN_REVIEW' },
              { value: 'APPROVED', label: 'APPROVED' },
              { value: 'REJECTED', label: 'REJECTED' },
              { value: 'CANCELLED', label: 'CANCELLED' },
              { value: 'ALL', label: 'Все' },
            ]}
          />
          <Button onClick={() => void refetchCandidates()}>Обновить</Button>
        </Space>
      </Space>

      {candidatesError && (
        <Alert
          type="error"
          showIcon
          message="Не удалось загрузить список кандидатов"
          description="Проверьте, что backend ручки /api/candidates уже реализованы и доступны."
        />
      )}

      <Card className="glass-panel">
        {isCandidatesLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin size="large" /></div>
        ) : candidates.length === 0 ? (
          <Empty description="Кандидатов на принятие нет" />
        ) : (
          <Table<CandidateDTO>
            rowKey={(row) => String(row.id || `${row.server_key || 'candidate'}-${row.created_at || ''}`)}
            columns={columns}
            dataSource={candidates}
            pagination={{ pageSize: 20 }}
          />
        )}
      </Card>

      <Drawer
        title={selectedCandidate ? `Кандидат #${selectedCandidate.id}` : 'Кандидат'}
        size="large"
        open={drawerOpen}
        onClose={() => {
          setDrawerOpen(false);
          setSelectedCandidateID(null);
        }}
        extra={(
          <Space>
            <Button onClick={() => {
              setDrawerOpen(false);
              setSelectedCandidateID(null);
            }}
            >
              Отмена
            </Button>
            <Button type="primary" loading={approveMutation.isPending} onClick={() => void onSubmit()}>
              Подтвердить принятие
            </Button>
          </Space>
        )}
      >
        {candidateError && (
          <Alert
            type="error"
            showIcon
            message="Не удалось загрузить данные кандидата"
          />
        )}

        {isCandidateLoading || !selectedCandidate ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : (
          <Space orientation="vertical" size="large" style={{ width: '100%' }}>
            <Card size="small" title="Обнаруженные данные">
              <Descriptions column={2} bordered size="small">
                <Descriptions.Item label="Статус">{selectedCandidate.status}</Descriptions.Item>
                <Descriptions.Item label="CRM ID">{selectedCandidate.server_crm_id || '-'}</Descriptions.Item>
                <Descriptions.Item label="Server Key">{selectedCandidate.server_key || '-'}</Descriptions.Item>
                <Descriptions.Item label="Адрес сервера">{selectedCandidate.server_url || '-'}</Descriptions.Item>
                <Descriptions.Item label="Станций (staged)">
                  {stagedWorkstations.length}
                </Descriptions.Item>
                <Descriptions.Item label="ФР (staged)">
                  {stagedFiscals.length}
                </Descriptions.Item>
              </Descriptions>
            </Card>

            <Row gutter={16}>
              <Col span={12}>
                <Card size="small" title="Станции из наблюдений">
                  {stagedWorkstations.length === 0 ? (
                    <Empty description="Нет staged станций" />
                  ) : (
                    <List
                      dataSource={stagedWorkstations}
                      renderItem={(item: CandidateWorkstationStagingDTO) => (
                        <List.Item key={item.id}>
                            <Space orientation="vertical" size={0}>
                            <Text strong>{item.hostname || item.workstation_uuid || `Станция #${item.id}`}</Text>
                            <Text type="secondary">TV: {item.teamviewer_id || '-'}</Text>
                            <Text type="secondary">LM: {item.litemanager_id || '-'}</Text>
                            <Text type="secondary">AD: {item.anydesk_id || '-'}</Text>
                          </Space>
                        </List.Item>
                      )}
                    />
                  )}
                </Card>
              </Col>
              <Col span={12}>
                <Card size="small" title="ФР из наблюдений">
                  {stagedFiscals.length === 0 ? (
                    <Empty description="Нет staged ФР" />
                  ) : (
                    <List
                      dataSource={stagedFiscals}
                      renderItem={(item) => (
                        <List.Item key={item.id}>
                          <Space orientation="vertical" size={0}>
                            <Text strong>{item.serial_number || item.serial_normalized || `ФР #${item.id}`}</Text>
                            <Text type="secondary">РН ККТ: {item.rn_kkt || '-'}</Text>
                            <Text type="secondary">Модель: {item.model_name || '-'}</Text>
                          </Space>
                        </List.Item>
                      )}
                    />
                  )}
                </Card>
              </Col>
            </Row>

            <Card size="small" title="Подтверждение принятия на АО">
              <Form form={form} layout="vertical">
                <Form.Item name="company_mode" label="Компания">
                  <Select<CompanyMode>
                    value={companyMode}
                    onChange={(value) => {
                      setCompanyMode(value);
                    }}
                    options={[
                      { value: 'existing', label: 'Выбрать существующую' },
                      { value: 'new', label: 'Создать новую' },
                    ]}
                  />
                </Form.Item>

                {companyMode === 'existing' ? (
                  <Form.Item
                    name="company_id"
                    label="Компания"
                    rules={[{ required: true, message: 'Выберите компанию' }]}
                  >
                    <Select
                      showSearch
                      filterOption={false}
                      onSearch={setCompanySearch}
                      loading={isCompaniesLoading}
                      placeholder="Начните ввод названия компании"
                      options={companyOptions}
                    />
                  </Form.Item>
                ) : (
                  <Row gutter={12}>
                    <Col span={12}>
                      <Form.Item
                        name="new_company_title"
                        label="Название компании"
                        rules={[{ required: true, message: 'Введите название компании' }]}
                      >
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col span={12}>
                      <Form.Item name="new_company_additional_name" label="Юридическое название">
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col span={12}>
                      <Form.Item name="new_company_address" label="Адрес">
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col span={12}>
                      <Form.Item name="new_company_parent_id" label="Родительская компания (ID)">
                        <Input />
                      </Form.Item>
                    </Col>
                  </Row>
                )}

                <Divider />

                <Form.Item name="server_mode" label="Сервер">
                  <Select<ServerMode>
                    value={serverMode}
                    onChange={(value) => {
                      setServerMode(value);
                    }}
                    options={[
                      { value: 'existing', label: 'Выбрать существующий сервер' },
                      { value: 'new', label: 'Создать новый сервер' },
                    ]}
                  />
                </Form.Item>

                {serverMode === 'existing' ? (
                  <Form.Item
                    name="server_id"
                    label="Сервер"
                    rules={[{ required: true, message: 'Выберите сервер' }]}
                  >
                    <Select
                      showSearch
                      filterOption={false}
                      onSearch={setServerSearch}
                      loading={isServersLoading}
                      placeholder="Поиск по серверу / CRM / IP"
                      options={serverOptions}
                    />
                  </Form.Item>
                ) : (
                  <Row gutter={12}>
                    <Col span={8}>
                      <Form.Item name="server_crm_id" label="CRM ID сервера">
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col span={10}>
                      <Form.Item
                        name="server_url_rms"
                        label="Адрес RMS"
                        rules={[{ required: true, message: 'Укажите адрес RMS сервера' }]}
                      >
                        <Input placeholder="host:port" />
                      </Form.Item>
                    </Col>
                    <Col span={6}>
                      <Form.Item name="server_device_name" label="Имя сервера">
                        <Input />
                      </Form.Item>
                    </Col>
                    <Col span={24}>
                      <Form.Item name="server_description" label="Комментарий по серверу">
                        <Input.TextArea rows={2} />
                      </Form.Item>
                    </Col>
                  </Row>
                )}

                <Divider />

                <Form.List name="workstations">
                  {(fields, { add, remove }) => (
                    <Space orientation="vertical" size="small" style={{ width: '100%' }}>
                      <Text strong>Наименования обнаруженных станций</Text>
                      {fields.map((field) => (
                        <Row key={field.key} gutter={8} align="middle">
                          <Col span={10}>
                            <Form.Item name={[field.name, 'name']} rules={[{ required: true, message: 'Укажите имя станции' }]}>
                              <Input placeholder="Название станции" />
                            </Form.Item>
                          </Col>
                          <Col span={8}>
                            <Form.Item name={[field.name, 'workstation_uuid']}>
                              <Input placeholder="UUID станции (если есть)" />
                            </Form.Item>
                          </Col>
                          <Col span={4}>
                            <Form.Item name={[field.name, 'staging_id']}>
                              <Input placeholder="staging_id" />
                            </Form.Item>
                          </Col>
                          <Col span={2}>
                            <Button danger onClick={() => remove(field.name)}>Удалить</Button>
                          </Col>
                        </Row>
                      ))}
                      <Button onClick={() => add({ name: '' })}>Добавить станцию</Button>
                    </Space>
                  )}
                </Form.List>

                <Divider />

                <Form.Item name="comment" label="Комментарий к принятию">
                  <Input.TextArea rows={3} />
                </Form.Item>
              </Form>
            </Card>
          </Space>
        )}
      </Drawer>
    </Space>
  );
};

export default AcceptancePage;

