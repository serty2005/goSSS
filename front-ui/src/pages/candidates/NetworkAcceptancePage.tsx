import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Divider,
  Drawer,
  Empty,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import dayjs from 'dayjs';
import { networkCandidatesApi } from '@/api/networkCandidates';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import {
  CompanyModel,
  NetworkCandidateApprovePayload,
  NetworkCandidateDetailsDTO,
  NetworkCandidateDTO,
  NetworkCandidateFRStagingDTO,
  NetworkCandidateGroupDTO,
  NetworkCandidateWSStagingDTO,
} from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Title, Text } = Typography;

const STATUS_COLORS: Record<string, string> = {
  NEW: 'blue',
  IN_REVIEW: 'orange',
  APPROVED: 'green',
  REJECTED: 'red',
  CANCELLED: 'default',
};

const NO_AGENT_ID = 'Агент без UUID';

type CompanyMode = 'existing' | 'new';

interface AgentCluster {
  key: string;
  agentID: string;
  ws: NetworkCandidateWSStagingDTO[];
  frs: NetworkCandidateFRStagingDTO[];
  groups: NetworkCandidateGroupDTO[];
  observationIDs: number[];
  lastObservedAt?: string;
  hasUnresolvedAgent: boolean;
}

const normalizeID = (value?: string) => String(value || '').trim().toLowerCase();

const pickFiscalIdentity = (item: NetworkCandidateFRStagingDTO) => (
  String(item.serial_normalized || item.serial_number || item.rn_kkt || item.fn_number || item.id || '')
    .trim()
    .toLowerCase()
);

const maxObservedAt = (left?: string, right?: string) => {
  if (!left) return right;
  if (!right) return left;
  return dayjs(left).isAfter(dayjs(right)) ? left : right;
};

const buildAgentClusters = (groups: NetworkCandidateGroupDTO[]): AgentCluster[] => {
  if (groups.length === 0) {
    return [];
  }

  const parent = Array.from({ length: groups.length }, (_, index) => index);

  const find = (index: number): number => {
    if (parent[index] === index) return index;
    parent[index] = find(parent[index]);
    return parent[index];
  };

  const unite = (left: number, right: number) => {
    const rootLeft = find(left);
    const rootRight = find(right);
    if (rootLeft !== rootRight) {
      parent[rootRight] = rootLeft;
    }
  };

  const indexByAgent = new Map<string, number[]>();
  const indexByConnection = new Map<string, number[]>();
  const indexByFiscal = new Map<string, number[]>();

  groups.forEach((group, index) => {
    const agentID = String(group.ws?.agent_uuid || '').trim();
    if (agentID) {
      if (!indexByAgent.has(agentID)) {
        indexByAgent.set(agentID, []);
      }
      indexByAgent.get(agentID)!.push(index);
    }

    const connections = [
      normalizeID(group.ws?.teamviewer_id) ? `tv:${normalizeID(group.ws?.teamviewer_id)}` : '',
      normalizeID(group.ws?.litemanager_id) ? `lm:${normalizeID(group.ws?.litemanager_id)}` : '',
      normalizeID(group.ws?.rustdesk_id) ? `rd:${normalizeID(group.ws?.rustdesk_id)}` : '',
      normalizeID(group.ws?.anydesk_id) ? `ad:${normalizeID(group.ws?.anydesk_id)}` : '',
    ].filter(Boolean);

    connections.forEach((token) => {
      if (!indexByConnection.has(token)) {
        indexByConnection.set(token, []);
      }
      indexByConnection.get(token)!.push(index);
    });

    group.frs
      .map((fr) => pickFiscalIdentity(fr))
      .filter(Boolean)
      .forEach((token) => {
        if (!indexByFiscal.has(token)) {
          indexByFiscal.set(token, []);
        }
        indexByFiscal.get(token)!.push(index);
      });
  });

  [indexByAgent, indexByConnection, indexByFiscal].forEach((map) => {
    map.forEach((indexes) => {
      if (indexes.length < 2) {
        return;
      }
      const [first, ...rest] = indexes;
      rest.forEach((index) => unite(first, index));
    });
  });

  const bucket = new Map<number, NetworkCandidateGroupDTO[]>();
  groups.forEach((group, index) => {
    const root = find(index);
    if (!bucket.has(root)) {
      bucket.set(root, []);
    }
    bucket.get(root)!.push(group);
  });

  const clusters = Array.from(bucket.values()).map((clusterGroups): AgentCluster => {
    const wsRows = clusterGroups
      .map((group) => group.ws)
      .filter((row): row is NetworkCandidateWSStagingDTO => Boolean(row));

    const uniqueWs = wsRows.reduce<NetworkCandidateWSStagingDTO[]>((acc, item) => {
      if (!acc.some((candidate) => candidate.id === item.id)) {
        acc.push(item);
      }
      return acc;
    }, []);

    const frRows = clusterGroups.flatMap((group) => group.frs || []);
    const uniqueFrs = frRows.reduce<NetworkCandidateFRStagingDTO[]>((acc, item) => {
      if (!acc.some((candidate) => candidate.id === item.id)) {
        acc.push(item);
      }
      return acc;
    }, []);

    const preferredAgent = uniqueWs.find((item) => String(item.agent_uuid || '').trim())?.agent_uuid;
    const observationIDs = Array.from(
      new Set(clusterGroups.map((item) => item.group.observation_id).filter((value) => Number.isFinite(value) && value > 0)),
    );

    const lastObservedAt = [...uniqueWs.map((item) => item.observed_at), ...uniqueFrs.map((item) => item.observed_at)]
      .filter(Boolean)
      .reduce<string | undefined>((acc, value) => maxObservedAt(acc, value), undefined);

    return {
      key: `${preferredAgent || NO_AGENT_ID}:${observationIDs.join(',') || 'none'}`,
      agentID: String(preferredAgent || NO_AGENT_ID),
      ws: uniqueWs,
      frs: uniqueFrs,
      groups: clusterGroups,
      observationIDs,
      lastObservedAt,
      hasUnresolvedAgent: !preferredAgent,
    };
  });

  return clusters.sort((left, right) => {
    const leftValue = left.lastObservedAt ? dayjs(left.lastObservedAt).valueOf() : 0;
    const rightValue = right.lastObservedAt ? dayjs(right.lastObservedAt).valueOf() : 0;
    return rightValue - leftValue;
  });
};

const NetworkAcceptancePage: React.FC = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const [status, setStatus] = useState<string>('ACTIVE');
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [companyMode, setCompanyMode] = useState<CompanyMode>('existing');
  const [selectedCluster, setSelectedCluster] = useState<AgentCluster | null>(null);
  const [clusterModalOpen, setClusterModalOpen] = useState(false);
  const [form] = Form.useForm();

  const setRouteState = (params: { candidateID?: number | null; clusterKey?: string | null; clusterModal?: boolean }, replace = false) => {
    const next = new URLSearchParams();
    if (params.candidateID && params.candidateID > 0) {
      next.set('candidate', String(params.candidateID));
    }
    if (params.clusterKey) {
      next.set('cluster', params.clusterKey);
    }
    if (params.clusterModal) {
      next.set('clusterModal', '1');
    }

    navigate(
      {
        pathname: '/network-acceptance',
        search: next.toString() ? `?${next.toString()}` : '',
      },
      { replace },
    );
  };

  const buildBackToPath = (candidateID?: number | null, clusterKey?: string | null, clusterModal = false) => {
    const next = new URLSearchParams();
    if (candidateID && candidateID > 0) {
      next.set('candidate', String(candidateID));
    }
    if (clusterKey) {
      next.set('cluster', clusterKey);
    }
    if (clusterModal) {
      next.set('clusterModal', '1');
    }
    const query = next.toString();
    return query ? `/network-acceptance?${query}` : '/network-acceptance';
  };

  const listQuery = useQuery({
    queryKey: ['network-candidates', status],
    queryFn: () => networkCandidatesApi.list({ status, limit: 200, offset: 0 }),
    staleTime: 10_000,
  });

  const detailsQuery = useQuery({
    queryKey: ['network-candidate', selectedID],
    queryFn: () => networkCandidatesApi.get(selectedID as number),
    enabled: Boolean(selectedID),
  });

  const removeGroupMutation = useMutation({
    mutationFn: async (groupID: number) => networkCandidatesApi.removeGroup(selectedID as number, groupID),
    onSuccess: () => {
      message.success('Группа перенесена в новый network-кандидат');
      void queryClient.invalidateQueries({ queryKey: ['network-candidates'] });
      void queryClient.invalidateQueries({ queryKey: ['network-candidate', selectedID] });
    },
    onError: () => message.error('Не удалось перенести группу'),
  });

  const approveMutation = useMutation({
    mutationFn: async (payload: NetworkCandidateApprovePayload) => networkCandidatesApi.approve(selectedID as number, payload),
    onSuccess: () => {
      message.success('Network-кандидат подтверждён');
      setDrawerOpen(false);
      setSelectedID(null);
      setSelectedCluster(null);
      setClusterModalOpen(false);
      form.resetFields();
      setRouteState({}, true);
      void queryClient.invalidateQueries({ queryKey: ['network-candidates'] });
    },
    onError: () => message.error('Не удалось подтвердить network-кандидата'),
  });

  const rows = useMemo(() => ((listQuery.data?.data || []) as NetworkCandidateDTO[]), [listQuery.data?.data]);
  const details = useMemo(() => (detailsQuery.data?.data as NetworkCandidateDetailsDTO | undefined), [detailsQuery.data?.data]);
  const clusters = useMemo(() => buildAgentClusters(details?.groups || []), [details?.groups]);
  const selectedCandidateParam = Number(searchParams.get('candidate') || '0');
  const selectedClusterParam = String(searchParams.get('cluster') || '').trim();
  const clusterModalParam = searchParams.get('clusterModal') === '1';

  const listHubCompanyIDs = useMemo(
    () => Array.from(new Set(rows.map((row) => String(row.hub_company_id || '').trim()).filter(Boolean))),
    [rows],
  );

  const listCompanyNamesQuery = useQuery({
    queryKey: ['network-candidates', 'hub-company-names', listHubCompanyIDs],
    queryFn: async () => {
      const pairs = await Promise.all(listHubCompanyIDs.map(async (companyID) => {
        try {
          const response = await companiesApi.getCompany(companyID);
          const title = resolveCompanyTitle(response.data) || companyID;
          return [companyID, title] as const;
        } catch {
          return [companyID, companyID] as const;
        }
      }));

      return pairs.reduce<Record<string, string>>((acc, [companyID, title]) => {
        acc[companyID] = title;
        return acc;
      }, {});
    },
    enabled: listHubCompanyIDs.length > 0,
    staleTime: 60_000,
  });

  const hubCompanyID = details?.candidate?.hub_company_id;
  const companiesQuery = useQuery({
    queryKey: ['network-candidate-children', hubCompanyID],
    queryFn: () => companiesApi.getChildren(hubCompanyID as string),
    enabled: Boolean(hubCompanyID),
  });

  const companyOptions = useMemo(() => {
    const raw = companiesQuery.data?.data;
    const list = Array.isArray(raw)
      ? raw
      : (raw && Array.isArray((raw as { data?: unknown[] }).data) ? (raw as { data: unknown[] }).data : []);
    return list
      .map((item) => {
        const model = item as CompanyModel & { name?: string };
        const id = resolveCompanyID(model);
        const title = String(model.name || resolveCompanyTitle(model) || id || '').trim();
        const parentTitle = resolveCompanyParentTitle(model);
        return { value: id, label: parentTitle ? `${parentTitle} / ${title}` : title };
      })
      .filter((item) => item.value) as Array<{ value: string; label: string }>;
  }, [companiesQuery.data?.data]);

  const workstationUUIDs = useMemo(
    () => Array.from(new Set((details?.groups || []).map((group) => String(group.ws?.workstation_uuid || '').trim()).filter(Boolean))),
    [details?.groups],
  );

  const workstationNamesQuery = useQuery({
    queryKey: ['network-candidate', selectedID, 'workstation-names', workstationUUIDs],
    queryFn: async () => {
      const pairs = await Promise.all(workstationUUIDs.map(async (workstationUUID) => {
        try {
          const response = await equipmentApi.getWorkstation(workstationUUID);
          const name = String(response.data.device_name || response.data.id || workstationUUID);
          return [workstationUUID, name] as const;
        } catch {
          return [workstationUUID, workstationUUID] as const;
        }
      }));

      return pairs.reduce<Record<string, string>>((acc, [workstationUUID, name]) => {
        acc[workstationUUID] = name;
        return acc;
      }, {});
    },
    enabled: drawerOpen && workstationUUIDs.length > 0,
    staleTime: 60_000,
  });

  useEffect(() => {
    if (selectedCandidateParam > 0) {
      setSelectedID((prev) => (prev === selectedCandidateParam ? prev : selectedCandidateParam));
      setDrawerOpen(true);
      return;
    }
    setDrawerOpen(false);
    setSelectedID(null);
    setSelectedCluster(null);
    setClusterModalOpen(false);
  }, [selectedCandidateParam]);

  useEffect(() => {
    if (!clusterModalParam || !selectedClusterParam) {
      setSelectedCluster(null);
      setClusterModalOpen(false);
      return;
    }
    if (detailsQuery.isLoading || clusters.length === 0) {
      return;
    }
    const found = clusters.find((item) => item.key === selectedClusterParam) || null;
    setSelectedCluster(found);
    setClusterModalOpen(Boolean(found));
  }, [clusterModalParam, selectedClusterParam, clusters, detailsQuery.isLoading]);

  useEffect(() => {
    if (details?.candidate && drawerOpen) {
      const preselectedCompany = details.candidate.ws_owner_candidate || details.candidate.fr_owner_candidate;
      if (preselectedCompany) {
        form.setFieldsValue({
          company_mode: 'existing',
          child_company_id: preselectedCompany,
        });
        setCompanyMode('existing');
      }
    }
  }, [details?.candidate, drawerOpen, form]);

  const onApprove = async () => {
    const values = await form.validateFields();
    const payload: NetworkCandidateApprovePayload = { comment: values.comment || '' };
    if (values.company_mode === 'existing') {
      payload.child_company_id = values.child_company_id;
    } else {
      payload.child_company = {
        title: values.child_company_title,
        address: values.child_company_address || '',
      };
    }
    approveMutation.mutate(payload);
  };

  const open = (id: number) => {
    setSelectedID(id);
    setDrawerOpen(true);
    setCompanyMode('existing');
    setSelectedCluster(null);
    setClusterModalOpen(false);
    form.setFieldsValue({ company_mode: 'existing' });
    setRouteState({ candidateID: id });
  };

  const hasConflict = Boolean(details?.candidate?.conflict_info && details.candidate.conflict_info.length > 0);
  const hubCompanyName = String(listCompanyNamesQuery.data?.[String(hubCompanyID || '')] || hubCompanyID || '-');
  const workstationNames = workstationNamesQuery.data || {};

  const candidateLastObservedAt = useMemo(() => {
    const all = (details?.groups || [])
      .flatMap((group) => [group.ws?.observed_at || '', ...group.frs.map((fr) => fr.observed_at || '')])
      .filter(Boolean);

    if (all.length === 0) return '';
    return all.reduce((max, current) => (dayjs(current).isAfter(dayjs(max)) ? current : max));
  }, [details?.groups]);

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
        <Title level={4} style={{ margin: 0 }}>Принятие в сеть</Title>
        <Space>
          <Select
            style={{ width: 220 }}
            value={status}
            onChange={setStatus}
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
          <Button onClick={() => void listQuery.refetch()}>Обновить</Button>
        </Space>
      </Space>

      {listQuery.error && <Alert type="error" message="Не удалось загрузить network-кандидатов" showIcon />}

      <Card className="glass-panel">
        {listQuery.isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin size="large" /></div>
        ) : rows.length === 0 ? (
          <Empty description="Кандидатов на принятие нет" />
        ) : (
          <Table<NetworkCandidateDTO>
            rowKey={(row) => String(row.id)}
            dataSource={rows}
            pagination={{ pageSize: 20 }}
            columns={[
              { title: 'ID', dataIndex: 'id', width: 80 },
              {
                title: 'Статус',
                dataIndex: 'status',
                width: 130,
                render: (value: string) => <Tag color={STATUS_COLORS[value] || 'default'}>{value}</Tag>,
              },
              {
                title: 'Hub-компания',
                dataIndex: 'hub_company_id',
                width: 260,
                render: (value?: string) => {
                  const companyID = String(value || '').trim();
                  if (!companyID) return '-';
                  const title = listCompanyNamesQuery.data?.[companyID] || companyID;
                  return <Link to={`/companies/${companyID}`}>{title}</Link>;
                },
              },
              { title: 'Сервер', dataIndex: 'server_id', width: 220 },
              { title: 'CRM', dataIndex: 'server_crm_id', render: (value?: string) => value || '-' },
              {
                title: 'Конфликт',
                dataIndex: 'conflict_info',
                width: 180,
                render: (value?: string) => (value ? <Tag color="warning">Есть конфликт владельцев</Tag> : '-'),
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
                width: 150,
                render: (_: unknown, row: NetworkCandidateDTO) => (
                  <Button type="primary" onClick={() => open(row.id)}>Открыть</Button>
                ),
              },
            ]}
          />
        )}
      </Card>

      <Drawer
        title={details?.candidate ? `Network-кандидат #${details.candidate.id}` : 'Network-кандидат'}
        open={drawerOpen}
        size="large"
        onClose={() => {
          setDrawerOpen(false);
          setSelectedID(null);
          setSelectedCluster(null);
          setClusterModalOpen(false);
          setRouteState({});
        }}
        extra={(
          <Space>
            <Button
              onClick={() => {
                setDrawerOpen(false);
                setSelectedID(null);
                setSelectedCluster(null);
                setClusterModalOpen(false);
                setRouteState({});
              }}
            >
              Отмена
            </Button>
            <Button type="primary" loading={approveMutation.isPending} onClick={() => void onApprove()}>
              Подтвердить
            </Button>
          </Space>
        )}
      >
        {!details || detailsQuery.isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            <Card size="small" title="Обнаруженные данные">
              <Descriptions column={2} bordered size="small">
                <Descriptions.Item label="Статус">{details.candidate.status}</Descriptions.Item>
                <Descriptions.Item label="Hub-компания">
                  {hubCompanyID ? <Link to={`/companies/${hubCompanyID}`}>{hubCompanyName}</Link> : '-'}
                </Descriptions.Item>
                <Descriptions.Item label="Сервер">{details.candidate.server_id || '-'}</Descriptions.Item>
                <Descriptions.Item label="CRM ID">{details.candidate.server_crm_id || '-'}</Descriptions.Item>
                <Descriptions.Item label="Server Key">{details.candidate.server_key || '-'}</Descriptions.Item>
                <Descriptions.Item label="Последнее наблюдение">
                  {candidateLastObservedAt ? dayjs(candidateLastObservedAt).format('DD.MM.YYYY HH:mm:ss') : '-'}
                </Descriptions.Item>
              </Descriptions>
            </Card>

            {hasConflict && (
              <Card size="small" title="Информация о конфликте" style={{ borderColor: '#faad14' }}>
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Alert
                    type="warning"
                    message="Обнаружен конфликт владельцев"
                    description={details.candidate.conflict_info}
                    showIcon
                  />
                  <Divider style={{ margin: '8px 0' }} />
                  <Space wrap>
                    {details.candidate.ws_owner_candidate && (
                      <Text>Кандидат по РС: <Text code>{details.candidate.ws_owner_candidate}</Text></Text>
                    )}
                    {details.candidate.fr_owner_candidate && (
                      <Text>Кандидат по ФР: <Text code>{details.candidate.fr_owner_candidate}</Text></Text>
                    )}
                  </Space>
                </Space>
              </Card>
            )}

            <Card size="small" title={`Сущности агентов (${clusters.length})`}>
              {clusters.length === 0 ? (
                <Empty description="Данные агентов не найдены" />
              ) : (
                <Space direction="vertical" size={10} style={{ width: '100%' }}>
                  {clusters.map((cluster) => (
                    <Card
                      key={cluster.key}
                      size="small"
                      hoverable
                      bodyStyle={{ padding: 10 }}
                      onClick={() => {
                        setRouteState({ candidateID: selectedID, clusterKey: cluster.key, clusterModal: true });
                      }}
                    >
                      <Space direction="vertical" size={6} style={{ width: '100%' }}>
                        <Space wrap>
                          <Text strong>{cluster.agentID}</Text>
                          {cluster.hasUnresolvedAgent ? <Tag color="orange">UUID не определён</Tag> : null}
                          <Tag color="blue">Наблюдений: {cluster.observationIDs.length}</Tag>
                          <Tag color="geekblue">Групп: {cluster.groups.length}</Tag>
                          <Tag color="gold">ФР: {cluster.frs.length}</Tag>
                        </Space>

                        {cluster.ws.map((item) => (
                          <Space key={item.id} direction="vertical" size={0} style={{ width: '100%' }}>
                            <Text strong>
                              {item.workstation_uuid ? (
                                <Link
                                  to={`/workstations/${item.workstation_uuid}`}
                                  state={{
                                    backTo: buildBackToPath(selectedID, cluster.key, false),
                                  }}
                                >
                                  {workstationNames[item.workstation_uuid] || item.hostname || item.workstation_uuid}
                                </Link>
                              ) : (
                                item.hostname || `Рабочая станция #${item.id}`
                              )}
                            </Text>
                            <Text type="secondary">TeamViewer: {item.teamviewer_id || '-'}</Text>
                            <Text type="secondary">LiteManager: {item.litemanager_id || '-'}</Text>
                            <Text type="secondary">RustDesk: {item.rustdesk_id || '-'}</Text>
                            <Text type="secondary">AnyDesk: {item.anydesk_id || '-'}</Text>
                          </Space>
                        ))}

                        {cluster.frs.length > 0 ? (
                          <Space wrap>
                            {cluster.frs.map((fr) => (
                              <Tag key={fr.id}>
                                {fr.serial_number || fr.serial_normalized || fr.rn_kkt || `ФР #${fr.id}`}
                              </Tag>
                            ))}
                          </Space>
                        ) : (
                          <Text type="secondary">ФР в этой группе не найдены</Text>
                        )}

                        <Text type="secondary">
                          Последнее наблюдение: {cluster.lastObservedAt ? dayjs(cluster.lastObservedAt).format('DD.MM.YYYY HH:mm:ss') : '-'}
                        </Text>
                      </Space>
                    </Card>
                  ))}
                </Space>
              )}
            </Card>

            <Card size="small" title="Сопоставление дочерней компании">
              <Form form={form} layout="vertical">
                <Form.Item name="company_mode" label="Режим">
                  <Select
                    value={companyMode}
                    onChange={(value: CompanyMode) => setCompanyMode(value)}
                    options={[
                      { value: 'existing', label: 'Выбрать существующую дочернюю' },
                      { value: 'new', label: 'Создать новую дочернюю' },
                    ]}
                  />
                </Form.Item>
                {companyMode === 'existing' ? (
                  <Form.Item
                    name="child_company_id"
                    label="Дочерняя компания"
                    rules={[{ required: true, message: 'Выберите компанию' }]}
                  >
                    <Select
                      showSearch
                      options={companyOptions}
                      loading={companiesQuery.isLoading}
                      placeholder="Выберите дочернюю компанию"
                    />
                  </Form.Item>
                ) : (
                  <>
                    <Form.Item
                      name="child_company_title"
                      label="Название"
                      rules={[{ required: true, message: 'Введите название' }]}
                    >
                      <Input />
                    </Form.Item>
                    <Form.Item name="child_company_address" label="Адрес">
                      <Input />
                    </Form.Item>
                  </>
                )}
              </Form>
            </Card>
          </Space>
        )}
      </Drawer>

      <Modal
        title={selectedCluster ? `Полная информация агента: ${selectedCluster.agentID}` : 'Полная информация агента'}
        open={clusterModalOpen}
        width={980}
        onCancel={() => {
          setClusterModalOpen(false);
          setSelectedCluster(null);
          setRouteState({ candidateID: selectedID, clusterKey: null, clusterModal: false });
        }}
        footer={null}
      >
        {!selectedCluster ? (
          <Empty description="Агент не выбран" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {selectedCluster.groups.map((group) => (
              <Card
                key={group.group.id}
                size="small"
                title={`Группа #${group.group.id}`}
                extra={(
                  <Button
                    danger
                    loading={removeGroupMutation.isPending}
                    onClick={() => removeGroupMutation.mutate(group.group.id)}
                  >
                    Перенести группу
                  </Button>
                )}
              >
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                  <Text type="secondary">ID наблюдения: {group.group.observation_id}</Text>
                  <Text type="secondary">
                    Время: {dayjs(group.ws?.observed_at || group.group.updated_at).isValid()
                      ? dayjs(group.ws?.observed_at || group.group.updated_at).format('DD.MM.YYYY HH:mm:ss')
                      : '-'}
                  </Text>

                  <Divider style={{ margin: '6px 0' }} />

                  <Text strong>Рабочая станция</Text>
                  {group.ws ? (
                    <Space direction="vertical" size={0}>
                      <Text>Хост: {group.ws.hostname || '-'}</Text>
                      <Text>
                        Станция:{' '}
                        {group.ws.workstation_uuid ? (
                          <Link
                            to={`/workstations/${group.ws.workstation_uuid}`}
                            state={{
                              backTo: buildBackToPath(selectedID, selectedCluster.key, true),
                              from: location.pathname + location.search,
                            }}
                          >
                            {workstationNames[group.ws.workstation_uuid] || group.ws.workstation_uuid}
                          </Link>
                        ) : (
                          '-'
                        )}
                      </Text>
                      <Text>UUID станции: {group.ws.workstation_uuid || '-'}</Text>
                      <Text>UUID агента: {group.ws.agent_uuid || '-'}</Text>
                      <Text>TeamViewer: {group.ws.teamviewer_id || '-'}</Text>
                      <Text>LiteManager: {group.ws.litemanager_id || '-'}</Text>
                      <Text>RustDesk: {group.ws.rustdesk_id || '-'}</Text>
                      <Text>AnyDesk: {group.ws.anydesk_id || '-'}</Text>
                    </Space>
                  ) : (
                    <Text type="secondary">Данные рабочей станции отсутствуют</Text>
                  )}

                  <Divider style={{ margin: '6px 0' }} />

                  <Text strong>Фискальные регистраторы</Text>
                  {group.frs.length === 0 ? (
                    <Text type="secondary">ФР отсутствуют</Text>
                  ) : (
                    <Space direction="vertical" size={6} style={{ width: '100%' }}>
                      {group.frs.map((fr) => (
                        <Card key={fr.id} size="small" bodyStyle={{ padding: 10 }}>
                          <Space direction="vertical" size={0}>
                            <Text strong>{fr.serial_number || fr.serial_normalized || `ФР #${fr.id}`}</Text>
                            <Text type="secondary">РН ККТ: {fr.rn_kkt || '-'}</Text>
                            <Text type="secondary">Модель: {fr.model_name || '-'}</Text>
                            <Text type="secondary">ИНН: {fr.inn || '-'}</Text>
                            <Text type="secondary">ФН: {fr.fn_number || '-'}</Text>
                            <Text type="secondary">Организация: {fr.organization_name || '-'}</Text>
                            <Text type="secondary">Адрес: {fr.address || '-'}</Text>
                          </Space>
                        </Card>
                      ))}
                    </Space>
                  )}
                </Space>
              </Card>
            ))}
          </Space>
        )}
      </Modal>
    </Space>
  );
};

export default NetworkAcceptancePage;
