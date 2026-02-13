import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Drawer,
  Empty,
  Form,
  Input,
  Modal,
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
import { ticketsApi } from '@/api/tickets';
import {
  CandidateApprovePayload,
  CandidateDTO,
  CandidateObservationDTO,
  CandidateStatus,
  CandidateWorkstationStagingDTO,
  BitrixServicePointDTO,
  CompanyMode,
  CompanyModel,
  ContractMode,
} from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { AcceptanceButton } from '@/components/candidates/AcceptanceButton';
import { AcceptanceForm } from '@/components/candidates/AcceptanceForm';
import { StagedAgentEntities } from '@/components/candidates/StagedAgentEntities';
import { CompanySearchOption } from '@/components/companies/CompanySearchSelect';
import { CandidateWorkstationDraft } from '@/components/candidates/StagedWorkstations';

const { Title } = Typography;
type CandidateFilter = 'ACTIVE' | CandidateStatus | 'ALL';

const CONTRACT_TYPE_OPTIONS = [
  'TS Cloud',
  'TS Standart (без выездов)',
  'TS Standart',
];

const STATUS_COLORS: Record<CandidateStatus, string> = {
  NEW: 'blue',
  IN_REVIEW: 'orange',
  APPROVED: 'green',
  REJECTED: 'red',
  CANCELLED: 'default',
};

// normalizeCandidate приводит ответ backend к формату фронта:
// все поля должны быть в snake_case.
const normalizeCandidate = (raw: Record<string, unknown>): CandidateDTO => {
  const asNumber = (v: unknown): number => Number(v || 0);
  const asString = (v: unknown): string => String(v || '');
  const pick = (...keys: string[]) => keys.map((k) => raw[k]).find((v) => v !== undefined);

  return {
    id: asNumber(pick('id', 'ID')),
    server_key: pick('server_key', 'ServerKey') as string | undefined,
    server_crm_id: pick('server_crm_id', 'ServerCRMID') as string | undefined,
    server_url: pick('server_url', 'ServerURL') as string | undefined,
    existing_server_id: pick('existing_server_id', 'ExistingServerID') as string | undefined,
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

const normalizeRemoteID = (value?: string) => String(value || '').trim().toLowerCase();

const buildMergeKey = (item: CandidateWorkstationStagingDTO, fallbackIndex: number) => {
  const tv = normalizeRemoteID(item.teamviewer_id);
  const lm = normalizeRemoteID(item.litemanager_id);
  const ad = normalizeRemoteID(item.anydesk_id);
  const parts = [
    tv ? `tv:${tv}` : '',
    lm ? `lm:${lm}` : '',
    ad ? `ad:${ad}` : '',
  ].filter(Boolean);

  if (parts.length > 0) {
    return parts.join('|');
  }

  return `fallback:${item.workstation_uuid || item.id || fallbackIndex}`;
};

const maxObservedAt = (left?: string, right?: string) => {
  if (!left) return right;
  if (!right) return left;
  return dayjs(left).isAfter(dayjs(right)) ? left : right;
};

const mergeCandidateWorkstations = (items: CandidateWorkstationStagingDTO[]): CandidateWorkstationDraft[] => {
  const merged = new Map<string, CandidateWorkstationDraft & { staging_ids: number[]; observation_ids: number[] }>();

  items.forEach((item, index) => {
    const key = buildMergeKey(item, index);
    const existing = merged.get(key);

    if (!existing) {
      merged.set(key, {
        merge_key: key,
        staging_id: item.id,
        staging_ids: item.id ? [item.id] : [],
        observation_id: item.observation_id,
        observation_ids: item.observation_id ? [item.observation_id] : [],
        workstation_uuid: item.workstation_uuid,
        hostname: item.hostname || '',
        name: item.hostname || '',
        teamviewer_id: item.teamviewer_id,
        litemanager_id: item.litemanager_id,
        anydesk_id: item.anydesk_id,
        agent_uuids: item.agent_uuid ? [item.agent_uuid] : [],
        observed_at: item.observed_at,
      });
      return;
    }

    if (item.id && !existing.staging_ids.includes(item.id)) {
      existing.staging_ids.push(item.id);
    }
    if (item.observation_id && !existing.observation_ids.includes(item.observation_id)) {
      existing.observation_ids.push(item.observation_id);
    }
    existing.hostname = existing.hostname || item.hostname || '';
    existing.name = existing.name || item.hostname || '';
    existing.workstation_uuid = existing.workstation_uuid || item.workstation_uuid;
    existing.teamviewer_id = existing.teamviewer_id || item.teamviewer_id;
    existing.litemanager_id = existing.litemanager_id || item.litemanager_id;
    existing.anydesk_id = existing.anydesk_id || item.anydesk_id;
    existing.observed_at = maxObservedAt(existing.observed_at, item.observed_at);
    if (item.agent_uuid && !existing.agent_uuids?.includes(item.agent_uuid)) {
      existing.agent_uuids = [...(existing.agent_uuids || []), item.agent_uuid];
    }
  });

  return Array.from(merged.values()).map((item) => ({
    merge_key: item.merge_key,
    staging_id: item.staging_ids[0] || item.staging_id,
    observation_id: item.observation_id,
    observation_ids: item.observation_ids,
    workstation_uuid: item.workstation_uuid,
    hostname: item.hostname,
    name: item.name,
    teamviewer_id: item.teamviewer_id,
    litemanager_id: item.litemanager_id,
    anydesk_id: item.anydesk_id,
    agent_uuids: item.agent_uuids || [],
    observed_at: item.observed_at,
  }));
};

const AcceptancePage: React.FC = () => {
  const queryClient = useQueryClient();

  const [status, setStatus] = useState<CandidateFilter>('ACTIVE');
  const [selectedCandidateID, setSelectedCandidateID] = useState<number | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [companySearch, setCompanySearch] = useState('');
  const [workstationDrafts, setWorkstationDrafts] = useState<CandidateWorkstationDraft[]>([]);
  const [agentDataOpen, setAgentDataOpen] = useState(false);
  const [agentDataTitle, setAgentDataTitle] = useState('');
  const [agentObservations, setAgentObservations] = useState<CandidateObservationDTO[]>([]);

  const [form] = Form.useForm();
  const formValues = Form.useWatch([], form);

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
    enabled: drawerOpen,
    staleTime: 15_000,
  });

  const { data: bitrixServicePoints = [], isLoading: isBitrixServicePointsLoading } = useQuery({
    queryKey: ['bitrix-service-points', 'acceptance'],
    queryFn: () => ticketsApi.getBitrixServicePoints(),
    enabled: drawerOpen,
    staleTime: 30_000,
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
      setWorkstationDrafts([]);
      setAgentDataOpen(false);
      setAgentObservations([]);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['candidates'] });
    },
    onError: () => {
      message.error('Не удалось подтвердить кандидата');
    },
  });

  const agentObservationsMutation = useMutation({
    mutationFn: async (observationIDs: number[]) => {
      if (!selectedCandidateID) {
        return [];
      }
      const response = await candidatesApi.getCandidateObservations(selectedCandidateID, observationIDs);
      return response.data || [];
    },
    onSuccess: (rows) => {
      setAgentObservations(rows);
    },
    onError: () => {
      setAgentObservations([]);
      message.error('Не удалось загрузить полные данные агента');
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

    const wsDefaults = mergeCandidateWorkstations(selectedCandidate.staged_workstations || []).map((item) => ({
      merge_key: item.merge_key,
      staging_id: item.staging_id,
      observation_id: item.observation_id,
      observation_ids: item.observation_ids || (item.observation_id ? [item.observation_id] : []),
      workstation_uuid: item.workstation_uuid,
      hostname: item.hostname || '',
      name: item.name || item.hostname || '',
      teamviewer_id: item.teamviewer_id,
      litemanager_id: item.litemanager_id,
      anydesk_id: item.anydesk_id,
      agent_uuids: item.agent_uuids || [],
      observed_at: item.observed_at,
    }));

    form.setFieldsValue({
      company_mode: 'new',
      contract_mode: 'inherit_parent',
      contract_type: CONTRACT_TYPE_OPTIONS[0],
      server_device_name: '',
      server_unique_id: '',
      server_cabinet_link: '',
      workstations: wsDefaults.length ? wsDefaults : [{ name: '' }],
    });
    setWorkstationDrafts(wsDefaults.length ? wsDefaults : []);
  }, [form, selectedCandidate]);

  const companyOptions = useMemo(() => {
    const list = companiesData?.data || [];
    return list
      .map((item) => {
        const company = item as CompanyModel;
        const id = resolveCompanyID(company);
        if (!id) return null;
        const title = resolveCompanyTitle(company) || id;
        const parentTitle = resolveCompanyParentTitle(company) || undefined;
        return {
          value: id,
          title,
          parentTitle,
          additionalName: company.additional_name || '',
          address: company.address || '',
          active_contract: Boolean(company.active_contract),
          contract_type: company.contract_type || '',
        };
      })
      .filter(Boolean) as Array<CompanySearchOption & {
        additionalName: string;
        address: string;
        active_contract: boolean;
        contract_type: string;
      }>;
  }, [companiesData?.data]);

  const companiesByID = useMemo(() => {
    return companyOptions.reduce<Record<string, {
      title: string;
      additionalName: string;
      address: string;
      active_contract: boolean;
      contract_type: string;
    }>>((acc, item) => {
      acc[item.value] = {
        title: item.title,
        additionalName: item.additionalName,
        address: item.address,
        active_contract: item.active_contract,
        contract_type: item.contract_type,
      };
      return acc;
    }, {});
  }, [companyOptions]);

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
      workstations: workstationDrafts
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
        contract_mode: values.contract_mode,
        contract_type: values.contract_mode === 'new' ? values.contract_type : undefined,
      };
    }

    payload.server = {
      mode: 'new',
      crm_id: selectedCandidate?.server_crm_id || '',
      url_rms: selectedCandidate?.server_url || '',
      unique_id: values.server_unique_id || '',
      cabinet_link: values.server_cabinet_link || '',
      device_name: values.server_device_name || '',
    };

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

  const stagedFiscals = selectedCandidate?.staged_fiscals || [];
  const observationAgents = useMemo(() => {
    const map: Record<number, string[]> = {};
    (selectedCandidate?.staged_workstations || []).forEach((item) => {
      if (!item.observation_id || !item.agent_uuid) return;
      if (!map[item.observation_id]) {
        map[item.observation_id] = [];
      }
      if (!map[item.observation_id].includes(item.agent_uuid)) {
        map[item.observation_id].push(item.agent_uuid);
      }
    });
    return map;
  }, [selectedCandidate?.staged_workstations]);
  const companyMode = (formValues?.company_mode || 'new') as CompanyMode;
  const selectedExistingCompanyID = String(formValues?.company_id || '').trim();
  const selectedParentCompanyID = String(formValues?.new_company_parent_id || '').trim();
  const selectedContractMode = (formValues?.contract_mode || 'inherit_parent') as ContractMode;
  const selectedExistingCompany = selectedExistingCompanyID ? companiesByID[selectedExistingCompanyID] : undefined;
  const selectedParentCompany = selectedParentCompanyID ? companiesByID[selectedParentCompanyID] : undefined;
  const bitrixServicePointOptions = useMemo(() => {
    if (!Array.isArray(bitrixServicePoints)) return [];
    return (bitrixServicePoints as BitrixServicePointDTO[]).map((point) => ({
      value: point.b24_element_id,
      label: point.name,
    }));
  }, [bitrixServicePoints]);
  const isBitrixEnabled = bitrixServicePointOptions.length > 0;

  useEffect(() => {
    if (!isBitrixEnabled) {
      form.setFieldValue('bitrix_service_point_id', undefined);
    }
  }, [form, isBitrixEnabled]);

  const handleWorkstationNameChange = (mergeKey: string, nextName: string) => {
    const nextRows = workstationDrafts.map((item) => (
      item.merge_key === mergeKey
        ? { ...item, name: nextName.trim() || item.name || item.hostname || '' }
        : item
    ));
    setWorkstationDrafts(nextRows);
    form.setFieldValue('workstations', nextRows);
  };

  const handleAgentGroupClick = (params: { agentID: string; observationIDs: number[]; unresolvedServer: boolean }) => {
    const uniqueObservationIDs = Array.from(
      new Set(params.observationIDs.filter((value) => Number.isFinite(value) && value > 0)),
    );
    setAgentDataTitle(
      params.unresolvedServer
        ? 'Нераспознанный сервер: полные данные агента'
        : `Полные данные агента: ${params.agentID}`,
    );
    setAgentDataOpen(true);
    setAgentObservations([]);
    if (uniqueObservationIDs.length === 0) {
      return;
    }
    agentObservationsMutation.mutate(uniqueObservationIDs);
  };

  const candidateLastObservedAt = useMemo(() => {
    const wsObserved = workstationDrafts.map((item) => item.observed_at).filter(Boolean) as string[];
    const frObserved = stagedFiscals.map((item) => item.observed_at).filter(Boolean) as string[];
    const allObserved = [...wsObserved, ...frObserved];
    if (allObserved.length === 0) return '';
    return allObserved.reduce((max, current) => (dayjs(current).isAfter(dayjs(max)) ? current : max));
  }, [stagedFiscals, workstationDrafts]);

  const approvalBlockReasons = useMemo(() => {
    const reasons: string[] = [];

    if (!selectedCandidate) {
      reasons.push('Кандидат не загружен');
      return reasons;
    }

    if (approveMutation.isPending) {
      reasons.push('Идёт подтверждение кандидата');
      return reasons;
    }

    if (companyMode === 'existing') {
      if (!String(formValues?.company_id || '').trim()) {
        reasons.push('Не выбрана компания');
      }
    } else {
      if (!String(formValues?.new_company_title || '').trim()) {
        reasons.push('Не указано название компании');
      }
      if (selectedContractMode === 'inherit_parent') {
        if (!selectedParentCompanyID) {
          reasons.push('Не выбрана родительская компания для наследования контракта');
        } else if (!selectedParentCompany) {
          reasons.push('Родительская компания не найдена в списке');
        } else if (!selectedParentCompany.active_contract) {
          reasons.push('У родительской компании нет активного контракта');
        }
      }
      if (selectedContractMode === 'new' && !String(formValues?.contract_type || '').trim()) {
        reasons.push('Не выбран тип обслуживания нового контракта');
      }
    }

    if (!String(formValues?.server_device_name || '').trim()) {
      reasons.push('Не указано имя сервера');
    }
    if (!String(formValues?.server_unique_id || '').trim()) {
      reasons.push('Не указан UniqueID');
    }
    if (!String(formValues?.server_cabinet_link || '').trim()) {
      reasons.push('Не указана ссылка на партнёрский кабинет');
    }
    if (isBitrixEnabled && !formValues?.bitrix_service_point_id) {
      reasons.push('Не выбрана точка обслуживания Bitrix24');
    }

    const workstationRows = workstationDrafts as Array<{ name?: string }>;
    workstationRows.forEach((item, index) => {
      if (!String(item?.name || '').trim()) {
        reasons.push(`Не указано имя станции #${index + 1}`);
      }
    });

    return reasons;
  }, [
    approveMutation.isPending,
    companyMode,
    formValues,
    selectedCandidate,
    selectedContractMode,
    selectedParentCompany,
    selectedParentCompanyID,
    isBitrixEnabled,
    workstationDrafts,
  ]);

  const approvalBlocked = approvalBlockReasons.length > 0;

  return (
    <Space direction="vertical" size="small" style={{ width: '100%' }}>
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
          setWorkstationDrafts([]);
          setAgentDataOpen(false);
          setAgentObservations([]);
        }}
        extra={(
          <Space>
            <Button onClick={() => {
              setDrawerOpen(false);
              setSelectedCandidateID(null);
              setWorkstationDrafts([]);
              setAgentDataOpen(false);
              setAgentObservations([]);
            }}
            >
              Отмена
            </Button>
            <AcceptanceButton
              isBlocked={approvalBlocked}
              blockReasons={approvalBlockReasons}
              onSubmit={onSubmit}
              isPending={approveMutation.isPending}
            />
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
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Card size="small" title="Обнаруженные данные">
              <Descriptions column={2} bordered size="small">
                <Descriptions.Item label="Статус">{selectedCandidate.status}</Descriptions.Item>
                <Descriptions.Item label="CRM ID">{selectedCandidate.server_crm_id || '-'}</Descriptions.Item>
                <Descriptions.Item label="Server Key">{selectedCandidate.server_key || '-'}</Descriptions.Item>
                <Descriptions.Item label="Адрес сервера">{selectedCandidate.server_url || '-'}</Descriptions.Item>
                <Descriptions.Item label="Распознанный сервер">{selectedCandidate.existing_server_id || 'нет'}</Descriptions.Item>
                <Descriptions.Item label="Последнее наблюдение">
                  {candidateLastObservedAt ? dayjs(candidateLastObservedAt).format('DD.MM.YYYY HH:mm:ss') : '-'}
                </Descriptions.Item>
              </Descriptions>
            </Card>

            <Row gutter={12}>
              <Col span={12}>
                <StagedAgentEntities
                  workstations={workstationDrafts}
                  fiscals={stagedFiscals}
                  observationAgents={observationAgents}
                  onWorkstationNameChange={handleWorkstationNameChange}
                  onGroupClick={handleAgentGroupClick}
                />
              </Col>
              <Col span={12}>
                <Card size="small" title="Параметры сервера">
                  <Form form={form} layout="vertical">
                    <Form.Item
                      name="server_device_name"
                      label="Имя сервера"
                      style={{ marginBottom: 10 }}
                      rules={[{ required: true, message: 'Укажите имя сервера' }]}
                    >
                      <Input />
                    </Form.Item>
                    <Form.Item
                      name="server_unique_id"
                      label="UniqueID"
                      style={{ marginBottom: 10 }}
                      rules={[{ required: true, message: 'Укажите UniqueID' }]}
                    >
                      <Input placeholder="например: 123-456-789" />
                    </Form.Item>
                    <Form.Item
                      name="server_cabinet_link"
                      label="Ссылка на партнёрский кабинет"
                      style={{ marginBottom: 0 }}
                      rules={[{ required: true, message: 'Укажите ссылку на кабинет' }]}
                    >
                      <Input placeholder="https://...clientid=12345" />
                    </Form.Item>
                  </Form>
                </Card>
              </Col>
            </Row>

            <AcceptanceForm
              form={form}
              companyMode={companyMode}
              selectedContractMode={selectedContractMode}
              selectedParentCompany={selectedParentCompany}
              selectedExistingCompany={selectedExistingCompany}
              companyOptions={companyOptions}
              isCompaniesLoading={isCompaniesLoading}
              isBitrixEnabled={isBitrixEnabled}
              bitrixServicePointOptions={bitrixServicePointOptions}
              isBitrixServicePointsLoading={isBitrixServicePointsLoading}
              onCompanySearch={setCompanySearch}
            />
          </Space>
        )}
      </Drawer>
      <Modal
        title={agentDataTitle}
        open={agentDataOpen}
        width={900}
        onCancel={() => setAgentDataOpen(false)}
        footer={null}
      >
        {agentObservationsMutation.isPending ? (
          <div style={{ textAlign: 'center', padding: 24 }}>
            <Spin />
          </div>
        ) : agentObservations.length === 0 ? (
          <Empty description="Полные данные наблюдений не найдены" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {agentObservations.map((item) => (
              <Card key={item.observation_id} size="small" title={`Наблюдение #${item.observation_id}`}>
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                  <Typography.Text type="secondary">
                    Время наблюдения: {dayjs(item.observed_at).isValid() ? dayjs(item.observed_at).format('DD.MM.YYYY HH:mm:ss') : '-'}
                  </Typography.Text>
                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                    {JSON.stringify(item.payload_json || {}, null, 2)}
                  </pre>
                </Space>
              </Card>
            ))}
          </Space>
        )}
      </Modal>
    </Space>
  );
};

export default AcceptancePage;
