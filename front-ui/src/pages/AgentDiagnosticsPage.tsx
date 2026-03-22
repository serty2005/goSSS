import React, { useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Descriptions, Empty, Skeleton, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { agentDiagnosticsApi } from '@/api/agentDiagnostics';
import AgentOperatorFlowCard from '@/components/agents/AgentOperatorFlowCard';
import AgentRegistrationAttemptModal from '@/components/agents/AgentRegistrationAttemptModal';
import {
  formatDateTime,
  formatRelativeTime,
  getAgentStatusColor,
  getHeartbeatFreshness,
  getRegistrationStatusMeta,
} from '@/components/agents/agentDiagnosticsUtils';
import JsonDataViewer from '@/components/common/JsonDataViewer';
import {
  AgentAdapterStatusDTO,
  AgentInventoryCOMPortDTO,
  AgentInventoryHostInfoDTO,
  AgentDiagnosticsDetailsDTO,
  AgentInventorySnapshotDTO,
  AgentRegistrationAttemptDTO,
  SaveAgentAdapterSelectionPayload,
} from '@/types/api';

const { Title, Text } = Typography;

const getErrorMessage = (error: unknown) => {
  if (typeof error === 'object' && error !== null && 'response' in error) {
    const response = (error as { response?: { status?: number; data?: { error?: { error?: string } } } }).response;
    const apiMessage = response?.data?.error?.error;
    if (apiMessage) {
      return apiMessage;
    }
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return 'Не удалось загрузить диагностику агента';
};

const isNotFoundError = (error: unknown) => {
  if (typeof error !== 'object' || error === null || !('response' in error)) {
    return false;
  }
  return (error as { response?: { status?: number } }).response?.status === 404;
};

const asRecord = (value: unknown) => (
  value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
);

const toInventorySnapshot = (value: AgentDiagnosticsDetailsDTO['latest_inventory']) => {
  if (!value || Array.isArray(value)) {
    return null;
  }
  return value as AgentInventorySnapshotDTO;
};

const toAdapterStatuses = (value: AgentDiagnosticsDetailsDTO['latest_adapter_statuses']) => {
  if (!Array.isArray(value)) {
    return [] as AgentAdapterStatusDTO[];
  }
  return value as AgentAdapterStatusDTO[];
};

const toComparableJSON = (value: unknown) => JSON.stringify(value ?? null);

const describeAttemptDiff = (current: AgentRegistrationAttemptDTO, previous?: AgentRegistrationAttemptDTO) => {
  if (!previous) {
    return {
      color: 'processing',
      label: 'Первая запись в текущей истории',
    };
  }

  const changedParts: string[] = [];
  if (toComparableJSON(current.payload) !== toComparableJSON(previous.payload)) {
    changedParts.push('payload');
  }
  if (toComparableJSON(current.system_info) !== toComparableJSON(previous.system_info)) {
    changedParts.push('system_info');
  }
  if ((current.machine_fingerprint || '') !== (previous.machine_fingerprint || '')) {
    changedParts.push('fingerprint');
  }

  if (changedParts.length === 0) {
    return {
      color: 'default',
      label: 'Без изменений относительно предыдущей',
    };
  }

  return {
    color: 'warning',
    label: `Изменились: ${changedParts.join(', ')}`,
  };
};

const renderInventoryTableEmpty = (description: string) => (
  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={description} />
);

type OperatorFlowStep = {
  type: 'success' | 'info' | 'warning' | 'error';
  title: string;
  description: string;
};

const classificationTagColor = (confidence?: string | null) => {
  const normalized = String(confidence || '').trim().toLowerCase();
  if (normalized === 'high') {
    return 'success';
  }
  if (normalized === 'medium') {
    return 'processing';
  }
  if (normalized === 'low') {
    return 'warning';
  }
  return 'default';
};

const describeCOMPort = (record: AgentInventoryCOMPortDTO) => {
  const title = record.friendly_name || record.description || record.name || '-';
  const meta = [record.manufacturer, record.class, record.location].filter(Boolean).join(' • ');
  return { title, meta };
};

const AgentDiagnosticsPage: React.FC = () => {
  const { uuid = '' } = useParams<{ uuid: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeAttempt, setActiveAttempt] = useState<AgentRegistrationAttemptDTO | null>(null);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ['agent-diagnostics', uuid],
    queryFn: async () => {
      const response = await agentDiagnosticsApi.getByUUID(uuid);
      return response.data;
    },
    enabled: Boolean(uuid),
    refetchOnWindowFocus: false,
    retry: false,
  });

  const details = data || null;
  const agent = details?.agent;
  const registrationStatus = getRegistrationStatusMeta(agent?.last_registration_status);
  const heartbeatFreshness = getHeartbeatFreshness(agent?.last_heartbeat);
  const inventory = useMemo(() => toInventorySnapshot(details?.latest_inventory), [details?.latest_inventory]);
  const adapterStatuses = useMemo(() => toAdapterStatuses(details?.latest_adapter_statuses), [details?.latest_adapter_statuses]);
  const systemInfo = asRecord(details?.registration_system_info);
  const diagnosticsOperatorFlow = details?.operator_flow || null;
  const heartbeatAvailable = Boolean(inventory || adapterStatuses.length > 0);
  const registrationApprovedAt = agent?.registration_approved_at;
  const registrationApprovedBy = (agent?.registration_approved_by || '').trim();
  const registrationApprovalPending = agent?.last_registration_status === 'pending_approval' && !registrationApprovedAt;
  const registrationApprovalConfirmed = agent?.last_registration_status === 'pending_approval' && Boolean(registrationApprovedAt);

  const updateDiagnosticsCache = async (nextDetails: AgentDiagnosticsDetailsDTO) => {
    queryClient.setQueryData(['agent-diagnostics', uuid], nextDetails);
    await queryClient.invalidateQueries({ queryKey: ['agent-diagnostics-list'] });
  };

  const approveRegistrationMutation = useMutation({
    mutationFn: async () => {
      const response = await agentDiagnosticsApi.approveRegistration(uuid);
      return response.data;
    },
    onSuccess: async (nextDetails) => {
      await updateDiagnosticsCache(nextDetails);
    },
  });

  const saveAdapterSelectionMutation = useMutation({
    mutationFn: async (payload: SaveAgentAdapterSelectionPayload) => {
      const response = await agentDiagnosticsApi.saveAdapterSelection(uuid, payload);
      return response.data;
    },
    onSuccess: async (nextDetails) => {
      await updateDiagnosticsCache(nextDetails);
    },
  });

  const inventoryNetworkInterfaces = useMemo(() => inventory?.network_interfaces || [], [inventory?.network_interfaces]);
  const inventoryHostInfo = (inventory?.host_info || null) as AgentInventoryHostInfoDTO | null;
  const inventoryComPorts = useMemo(() => inventory?.com_ports || [], [inventory?.com_ports]);
  const inventorySoftware = useMemo(() => inventory?.installed_software || [], [inventory?.installed_software]);
  const inventoryComponents = useMemo(() => inventory?.known_components || [], [inventory?.known_components]);
  const operatorFlow = useMemo<OperatorFlowStep[]>(() => {
    const steps: OperatorFlowStep[] = [];

    if (registrationApprovalPending) {
      steps.push({
        type: 'warning',
        title: 'Шаг 1. Подтвердить bootstrap-регистрацию',
        description: 'Сервер уже увидел новый агент, но токены ещё не выданы. После подтверждения агент сам повторит bootstrap и выйдет на heartbeat.',
      });
    } else if (registrationApprovalConfirmed) {
      steps.push({
        type: 'info',
        title: 'Шаг 1. Дождаться повторного bootstrap агента',
        description: 'Регистрация уже подтверждена оператором. Следующее ожидаемое событие — повторный запрос агента за токенами и переход к heartbeat.',
      });
    } else if (agent?.last_registration_status === 'success') {
      steps.push({
        type: 'success',
        title: 'Шаг 1. Регистрация завершена',
        description: 'Агент успешно получил токены и может стабильно работать через heartbeat.',
      });
    }

    if (!heartbeatAvailable) {
      steps.push({
        type: 'warning',
        title: 'Шаг 2. Дождаться первого heartbeat snapshot',
        description: 'Пока нет inventory и статусов адаптеров. До этого шага не стоит назначать профиль машины или планировать task-адаптеры.',
      });
    } else {
      steps.push({
        type: 'success',
        title: 'Шаг 2. Машина уже прислала inventory snapshot',
        description: 'Теперь можно анализировать ПО, remote ID, COM-устройства и решать, какие адаптеры действительно нужны этой машине.',
      });
    }

    const availableAdapters = diagnosticsOperatorFlow?.available_adapters || [];
    const selectedAdapterIDs = diagnosticsOperatorFlow?.selected_adapter_ids || [];
    const recommendedAdapterIDs = diagnosticsOperatorFlow?.recommended_adapter_ids || [];
    const effectiveManifests = diagnosticsOperatorFlow?.effective_adapter_manifests || [];

    if (heartbeatAvailable && availableAdapters.length === 0) {
      steps.push({
        type: 'warning',
        title: 'Шаг 3. Каталог published adapters ещё не подготовлен',
        description: 'Пока сервер не видит ни одного опубликованного адаптера, оператору нечего назначать этой машине.',
      });
    }

    if (heartbeatAvailable && recommendedAdapterIDs.length > 0 && selectedAdapterIDs.length === 0) {
      steps.push({
        type: 'info',
        title: 'Шаг 3. Сервер уже подготовил подсказку по адаптерам',
        description: `По текущему heartbeat сервер рекомендует: ${recommendedAdapterIDs.join(', ')}. Это только подсказка, оператору достаточно отметить нужные published adapters галками.`,
      });
    }

    if (heartbeatAvailable && selectedAdapterIDs.length === 0) {
      steps.push({
        type: 'warning',
        title: 'Шаг 4. Набор адаптеров ещё не выбран',
        description: 'Откройте блок "Доступные адаптеры", отметьте нужные published adapters и сохраните выбор.',
      });
    } else if (heartbeatAvailable && adapterStatuses.length === 0 && effectiveManifests.length > 0) {
      steps.push({
        type: 'info',
        title: 'Шаг 4. Выбор сохранён, ожидается следующий heartbeat',
        description: 'Сервер уже собрал полный adapter_manifests из published catalog. Следующий heartbeat агента должен забрать manifests и начать отчитываться по adapter_statuses.',
      });
    } else if (adapterStatuses.length > 0) {
      steps.push({
        type: 'success',
        title: 'Шаг 4. Агент уже отчитывается по локальным адаптерам',
        description: 'Назначение адаптеров прошло полный цикл: выбор сохранён, manifests выданы, агент уже показывает adapter_statuses.',
      });
    }

    return steps;
  }, [
    adapterStatuses.length,
    agent?.last_registration_status,
    heartbeatAvailable,
    diagnosticsOperatorFlow?.available_adapters,
    diagnosticsOperatorFlow?.effective_adapter_manifests,
    diagnosticsOperatorFlow?.recommended_adapter_ids,
    diagnosticsOperatorFlow?.selected_adapter_ids,
    registrationApprovalConfirmed,
    registrationApprovalPending,
  ]);

  const registrationHistoryColumns: ColumnsType<AgentRegistrationAttemptDTO> = [
    {
      title: 'Время',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 190,
      defaultSortOrder: 'descend',
      sorter: (left, right) => new Date(left.created_at || 0).getTime() - new Date(right.created_at || 0).getTime(),
      render: (value?: string) => (
        <Space direction="vertical" size={0}>
          <Text>{formatDateTime(value)}</Text>
          <Text type="secondary">{formatRelativeTime(value) || '-'}</Text>
        </Space>
      ),
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 180,
      render: (value?: string) => {
        const meta = getRegistrationStatusMeta(value);
        return (
          <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>
            {meta.label}
          </Tag>
        );
      },
    },
    {
      title: 'Ошибка',
      dataIndex: 'error_text',
      key: 'error_text',
      render: (value?: string) => (
        value ? <Text ellipsis={{ tooltip: value }}>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'Remote addr',
      dataIndex: 'remote_addr',
      key: 'remote_addr',
      width: 160,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Fingerprint',
      dataIndex: 'machine_fingerprint',
      key: 'machine_fingerprint',
      width: 220,
      render: (value?: string) => (
        value ? <Text copyable={{ text: value }} code>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'Сравнение',
      key: 'comparison',
      width: 280,
      render: (_value, record, index) => {
        const diff = describeAttemptDiff(record, details?.recent_registrations[index + 1]);
        return (
          <Tag color={diff.color} style={{ marginInlineEnd: 0, whiteSpace: 'normal' }}>
            {diff.label}
          </Tag>
        );
      },
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 160,
      render: (_value, record) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => setActiveAttempt(record)}>
          Открыть payload
        </Button>
      ),
    },
  ];

  const adapterColumns: ColumnsType<AgentAdapterStatusDTO> = [
    {
      title: 'adapter_id',
      dataIndex: 'adapter_id',
      key: 'adapter_id',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: 'adapter_type',
      dataIndex: 'adapter_type',
      key: 'adapter_type',
      width: 150,
      render: (value?: string) => value || '-',
    },
    {
      title: 'version',
      dataIndex: 'version',
      key: 'version',
      width: 120,
      render: (value?: string) => value || '-',
    },
    {
      title: 'status',
      dataIndex: 'status',
      key: 'status',
      width: 150,
      render: (value?: string) => {
        const normalized = String(value || '').trim().toLowerCase();
        const color = normalized === 'ready' || normalized === 'installed'
          ? 'success'
          : normalized === 'error' || normalized === 'failed'
            ? 'error'
            : normalized
              ? 'processing'
              : 'default';
        return (
          <Tag color={color} style={{ marginInlineEnd: 0 }}>
            {value || 'Не указан'}
          </Tag>
        );
      },
    },
    {
      title: 'target_os',
      dataIndex: 'target_os',
      key: 'target_os',
      width: 120,
      render: (value?: string) => value || '-',
    },
    {
      title: 'target_arch',
      dataIndex: 'target_arch',
      key: 'target_arch',
      width: 120,
      render: (value?: string) => value || '-',
    },
    {
      title: 'last_error',
      dataIndex: 'last_error',
      key: 'last_error',
      render: (value?: string) => (
        value ? <Text ellipsis={{ tooltip: value }}>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'updated_at',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 180,
      render: (value?: string) => formatDateTime(value),
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space wrap style={{ justifyContent: 'space-between', width: '100%' }}>
        <div>
          <Title level={4} style={{ margin: 0 }}>
            Диагностика агента
          </Title>
          <Text type="secondary">
            Подробный срез регистрации, heartbeat snapshot и истории попыток bootstrap-регистрации.
          </Text>
        </div>
        <Space wrap>
          <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/agents')}>
            К списку агентов
          </Button>
          {uuid ? (
            <Button onClick={() => navigate(`/agent-observations?agent_uuid=${encodeURIComponent(uuid)}`)}>
              Наблюдения агента
            </Button>
          ) : null}
          <Button onClick={() => void refetch()} loading={isFetching}>
            Обновить
          </Button>
        </Space>
      </Space>

      {isLoading ? (
        <Card className="glass-panel">
          <Skeleton active paragraph={{ rows: 10 }} />
        </Card>
      ) : isError ? (
        isNotFoundError(error) ? (
          <Card className="glass-panel">
            <Empty description={`Агент ${uuid || ''} не найден`} />
          </Card>
        ) : (
          <Alert
            type="error"
            showIcon
            message="Не удалось загрузить диагностику агента"
            description={getErrorMessage(error)}
            action={(
              <Button size="small" onClick={() => void refetch()}>
                Повторить
              </Button>
            )}
          />
        )
      ) : !agent ? (
        <Card className="glass-panel">
          <Empty description="Данные агента не найдены" />
        </Card>
      ) : (
        <>
          <Card className="glass-panel" title="Общая сводка" size="small">
            <Descriptions bordered column={2} size="small">
              <Descriptions.Item label="UUID">
                <Text copyable={{ text: agent.uuid }} code>{agent.uuid}</Text>
              </Descriptions.Item>
              <Descriptions.Item label="Hostname">
                {agent.hostname || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Тип">
                {agent.type || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Статус агента">
                <Tag color={getAgentStatusColor(agent.status)} style={{ marginInlineEnd: 0 }}>
                  {agent.status || 'Не указан'}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="owner_id">
                {agent.owner_id ? <Link to={`/companies/${agent.owner_id}`}>{agent.owner_id}</Link> : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="workstation_id">
                {agent.workstation_id ? <Link to={`/workstations/${agent.workstation_id}`}>{agent.workstation_id}</Link> : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="machine_fingerprint" span={2}>
                {agent.machine_fingerprint ? (
                  <Text copyable={{ text: agent.machine_fingerprint }} code>{agent.machine_fingerprint}</Text>
                ) : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Последняя регистрация">
                {formatDateTime(agent.last_registration_at)}
              </Descriptions.Item>
              <Descriptions.Item label="Статус регистрации">
                <Tag color={registrationStatus.color} style={{ marginInlineEnd: 0 }}>
                  {registrationStatus.label}
                </Tag>
              </Descriptions.Item>
              <Descriptions.Item label="Ошибка регистрации" span={2}>
                {agent.last_registration_error || <Text type="secondary">Нет</Text>}
              </Descriptions.Item>
              <Descriptions.Item label="Последний heartbeat">
                <Space direction="vertical" size={0}>
                  <Text>{formatDateTime(agent.last_heartbeat)}</Text>
                  <Space size={8}>
                    <Tag color={heartbeatFreshness.color} style={{ marginInlineEnd: 0 }}>
                      {heartbeatFreshness.label}
                    </Tag>
                    <Text type="secondary">{formatRelativeTime(agent.last_heartbeat) || '-'}</Text>
                  </Space>
                </Space>
              </Descriptions.Item>
              <Descriptions.Item label="Последнее наблюдение">
                <Space direction="vertical" size={0}>
                  <Text>{formatDateTime(agent.last_observed_at)}</Text>
                  <Text type="secondary">{formatRelativeTime(agent.last_observed_at) || '-'}</Text>
                </Space>
              </Descriptions.Item>
            </Descriptions>
          </Card>

          <Card className="glass-panel" title="Операторский флоу" size="small">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {operatorFlow.map((step) => (
                <Alert
                  key={step.title}
                  type={step.type}
                  showIcon
                  message={step.title}
                  description={step.description}
                />
              ))}
            </Space>
          </Card>

          <AgentOperatorFlowCard
            operatorFlow={diagnosticsOperatorFlow}
            saveSelectionPending={saveAdapterSelectionMutation.isPending}
            saveSelectionError={saveAdapterSelectionMutation.isError ? getErrorMessage(saveAdapterSelectionMutation.error) : undefined}
            onSaveSelection={(payload) => saveAdapterSelectionMutation.mutate(payload)}
          />

          <Card className="glass-panel" title="Последняя регистрация" size="small">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Alert
                type={registrationStatus.alertType}
                showIcon
                message={registrationStatus.label}
                description={agent.last_registration_error || registrationStatus.helper}
              />

              {registrationApprovalPending ? (
                <Alert
                  type="warning"
                  showIcon
                  message="Требуется подтверждение оператора"
                  description="Токены этому агенту пока не выданы. После подтверждения агент сам повторит bootstrap-регистрацию и перейдет к heartbeat."
                  action={(
                    <Button
                      type="primary"
                      loading={approveRegistrationMutation.isPending}
                      onClick={() => approveRegistrationMutation.mutate()}
                    >
                      Подтвердить регистрацию
                    </Button>
                  )}
                />
              ) : null}

              {registrationApprovalConfirmed ? (
                <Alert
                  type="info"
                  showIcon
                  message="Регистрация уже подтверждена"
                  description={registrationApprovedBy
                    ? `Подтвердил оператор ${registrationApprovedBy}. Ожидается повторный запрос агента для выдачи токенов.`
                    : 'Ожидается повторный запрос агента для выдачи токенов.'}
                />
              ) : null}

              {approveRegistrationMutation.isError ? (
                <Alert
                  type="error"
                  showIcon
                  message="Не удалось подтвердить регистрацию"
                  description={getErrorMessage(approveRegistrationMutation.error)}
                />
              ) : null}

              <Descriptions bordered column={2} size="small">
                <Descriptions.Item label="Время регистрации">
                  {formatDateTime(agent.last_registration_at)}
                </Descriptions.Item>
                <Descriptions.Item label="Статус">
                  <Tag color={registrationStatus.color} style={{ marginInlineEnd: 0 }}>
                    {registrationStatus.label}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="Fingerprint" span={2}>
                  {agent.machine_fingerprint ? (
                    <Text copyable={{ text: agent.machine_fingerprint }} code>{agent.machine_fingerprint}</Text>
                  ) : '-'}
                </Descriptions.Item>
                <Descriptions.Item label="Подтверждено оператором">
                  {registrationApprovedAt ? formatDateTime(registrationApprovedAt) : <Text type="secondary">Нет</Text>}
                </Descriptions.Item>
                <Descriptions.Item label="Кем подтверждено">
                  {registrationApprovedBy || <Text type="secondary">-</Text>}
                </Descriptions.Item>
                <Descriptions.Item label="Текст ошибки" span={2}>
                  {agent.last_registration_error || <Text type="secondary">Нет</Text>}
                </Descriptions.Item>
              </Descriptions>

              <JsonDataViewer
                title="System info"
                value={systemInfo}
                emptyDescription="System info для последней регистрации отсутствует"
              />

              <JsonDataViewer
                title="Registration payload"
                value={details.registration_payload}
                emptyDescription="Сервер пока не сохранил registration payload"
              />
            </Space>
          </Card>

          <Card className="glass-panel" title="Последний heartbeat snapshot" size="small">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {!heartbeatAvailable ? (
                <Alert
                  type={agent.last_registration_status === 'success' ? 'info' : 'warning'}
                  showIcon
                  message={agent.last_registration_status === 'success'
                    ? 'Агент зарегистрирован, но heartbeat snapshot ещё не пришёл'
                    : 'Heartbeat snapshot пока отсутствует'}
                  description={agent.last_registration_status === 'success'
                    ? 'Сервер ещё не получил inventory snapshot и статусы адаптеров после успешной регистрации.'
                    : 'Проверьте, дошёл ли агент до отправки heartbeat после bootstrap-регистрации.'}
                />
              ) : null}

              <Card
                size="small"
                type="inner"
                title={(
                  <Space>
                    <span>Inventory snapshot</span>
                    {agent.has_latest_inventory ? <Tag color="success">Есть snapshot</Tag> : <Tag>Нет snapshot</Tag>}
                  </Space>
                )}
              >
                {inventory ? (
                  <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                    <Descriptions bordered size="small" column={2}>
                      <Descriptions.Item label="hostname">
                        {inventory.hostname || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="os">
                        {inventory.os || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="arch">
                        {inventory.arch || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="collected_at">
                        {formatDateTime(inventory.collected_at)}
                      </Descriptions.Item>
                      <Descriptions.Item label="executable_path" span={2}>
                        {inventory.executable_path || '-'}
                      </Descriptions.Item>
                    </Descriptions>

                    <div>
                      <Text strong>Host info</Text>
                      <div style={{ marginTop: 8 }}>
                        {inventoryHostInfo ? (
                          <Descriptions bordered size="small" column={2}>
                            <Descriptions.Item label="cash_server_product">
                              {inventoryHostInfo.cash_server_product || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="cash_server_url">
                              {inventoryHostInfo.cash_server_url || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="cash_server_config" span={2}>
                              {inventoryHostInfo.cash_server_config || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="roaming_app_data_path" span={2}>
                              {inventoryHostInfo.roaming_app_data_path || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="teamviewer_id">
                              {inventoryHostInfo.teamviewer_id || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="anydesk_id">
                              {inventoryHostInfo.anydesk_id || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="litemanager_id">
                              {inventoryHostInfo.litemanager_id || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="rustdesk_id">
                              {inventoryHostInfo.rustdesk_id || '-'}
                            </Descriptions.Item>
                          </Descriptions>
                        ) : renderInventoryTableEmpty('host_info пока не пришёл')}
                      </div>
                    </div>

                    <div>
                      <Text strong>Сетевые интерфейсы</Text>
                      <div style={{ marginTop: 8 }}>
                        {inventoryNetworkInterfaces.length === 0 ? renderInventoryTableEmpty('Сетевые интерфейсы не пришли') : (
                          <Table
                            size="small"
                            rowKey={(record, index) => `${record.name || 'if'}-${index}`}
                            pagination={false}
                            columns={[
                              { title: 'name', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                              { title: 'index', dataIndex: 'index', key: 'index', width: 90, render: (value?: number) => value ?? '-' },
                              { title: 'MTU', dataIndex: 'mtu', key: 'mtu', width: 90, render: (value?: number) => value ?? '-' },
                              { title: 'MAC', dataIndex: 'hardware_addr', key: 'hardware_addr', width: 180, render: (value?: string) => value || '-' },
                              {
                                title: 'Адреса',
                                dataIndex: 'addresses',
                                key: 'addresses',
                                render: (value?: string[]) => value?.length ? value.join(', ') : '-',
                              },
                              {
                                title: 'Флаги',
                                dataIndex: 'flags',
                                key: 'flags',
                                render: (value?: string[]) => value?.length ? value.join(', ') : '-',
                              },
                            ]}
                            dataSource={inventoryNetworkInterfaces}
                            scroll={{ x: 900 }}
                          />
                        )}
                      </div>
                    </div>

                    <div>
                      <Text strong>COM-порты</Text>
                      <div style={{ marginTop: 8 }}>
                        {inventoryComPorts.length === 0 ? renderInventoryTableEmpty('COM-порты не пришли') : (
                          <Table<AgentInventoryCOMPortDTO>
                            size="small"
                            rowKey={(record) => `${record.name || 'com'}-${record.instance_id || record.device || 'na'}`}
                            pagination={false}
                            columns={[
                              {
                                title: 'Порт',
                                dataIndex: 'name',
                                key: 'name',
                                width: 120,
                                render: (value?: string, record?: AgentInventoryCOMPortDTO) => (
                                  <Space direction="vertical" size={0}>
                                    <Text code>{value || '-'}</Text>
                                    <Text type="secondary">{record?.device || record?.enumerator || '-'}</Text>
                                  </Space>
                                ),
                              },
                              {
                                title: 'Устройство',
                                key: 'device_details',
                                render: (_value?: unknown, record?: AgentInventoryCOMPortDTO) => {
                                  const details = describeCOMPort(record || {});
                                  return (
                                    <Space direction="vertical" size={0}>
                                      <Text>{details.title}</Text>
                                      <Text type="secondary">{details.meta || '-'}</Text>
                                    </Space>
                                  );
                                },
                              },
                              {
                                title: 'Сигнатура',
                                key: 'signature_key',
                                width: 260,
                                render: (_value?: unknown, record?: AgentInventoryCOMPortDTO) => (
                                  <Space direction="vertical" size={0}>
                                    <Text code>{record?.signature_key || '-'}</Text>
                                    <Text type="secondary">
                                      {[record?.vendor_id, record?.product_id].filter(Boolean).join(' / ') || '-'}
                                    </Text>
                                  </Space>
                                ),
                              },
                              {
                                title: 'Классификация',
                                key: 'classification',
                                width: 220,
                                render: (_value?: unknown, record?: AgentInventoryCOMPortDTO) => (
                                  record?.classification ? (
                                    <Space direction="vertical" size={0}>
                                      <Tag
                                        color={classificationTagColor(record.classification.confidence)}
                                        style={{ marginInlineEnd: 0, whiteSpace: 'normal' }}
                                      >
                                        {record.classification.label || record.classification.device_type || 'Есть правило'}
                                      </Tag>
                                      <Text type="secondary">
                                        {[
                                          record.classification.confidence,
                                          record.classification.source,
                                          record.classification.suggested_adapter,
                                        ].filter(Boolean).join(' • ') || '-'}
                                      </Text>
                                    </Space>
                                  ) : <Text type="secondary">Нет правила</Text>
                                ),
                              },
                              {
                                title: 'Instance / source',
                                key: 'instance_id',
                                render: (_value?: unknown, record?: AgentInventoryCOMPortDTO) => (
                                  <Space direction="vertical" size={0}>
                                    <Text ellipsis={{ tooltip: record?.instance_id || '-' }}>
                                      {record?.instance_id || '-'}
                                    </Text>
                                    <Text type="secondary">{record?.source || '-'}</Text>
                                  </Space>
                                ),
                              },
                            ]}
                            dataSource={inventoryComPorts}
                            scroll={{ x: 1400 }}
                          />
                        )}
                      </div>
                    </div>

                    <div>
                      <Text strong>Установленное ПО</Text>
                      <div style={{ marginTop: 8 }}>
                        {inventorySoftware.length === 0 ? renderInventoryTableEmpty('Список установленного ПО пока пуст') : (
                          <Table
                            size="small"
                            rowKey={(record, index) => `${record.name || 'soft'}-${index}`}
                            pagination={{ pageSize: 5, hideOnSinglePage: true }}
                            columns={[
                              { title: 'name', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                              { title: 'version', dataIndex: 'version', key: 'version', width: 120, render: (value?: string) => value || '-' },
                              { title: 'publisher', dataIndex: 'publisher', key: 'publisher', render: (value?: string) => value || '-' },
                              { title: 'source', dataIndex: 'source', key: 'source', width: 120, render: (value?: string) => value || '-' },
                            ]}
                            dataSource={inventorySoftware}
                            scroll={{ x: 720 }}
                          />
                        )}
                      </div>
                    </div>

                    <div>
                      <Text strong>Known components</Text>
                      <div style={{ marginTop: 8 }}>
                        {inventoryComponents.length === 0 ? renderInventoryTableEmpty('Known components пока не пришли') : (
                          <Table
                            size="small"
                            rowKey={(record, index) => `${record.key || 'component'}-${index}`}
                            pagination={false}
                            columns={[
                              { title: 'key', dataIndex: 'key', key: 'key', render: (value?: string) => value || '-' },
                              { title: 'name', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                              { title: 'category', dataIndex: 'category', key: 'category', render: (value?: string) => value || '-' },
                              {
                                title: 'detected',
                                dataIndex: 'detected',
                                key: 'detected',
                                width: 120,
                                render: (value?: boolean) => (
                                  <Tag color={value ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>
                                    {value ? 'Да' : 'Нет'}
                                  </Tag>
                                ),
                              },
                              { title: 'version', dataIndex: 'version', key: 'version', width: 120, render: (value?: string) => value || '-' },
                              {
                                title: 'evidence',
                                dataIndex: 'evidence',
                                key: 'evidence',
                                render: (value?: Array<{ type?: string; source?: string; value?: string }>) => (
                                  value?.length
                                    ? value.map((item) => [item.type, item.source, item.value].filter(Boolean).join(': ')).join('; ')
                                    : '-'
                                ),
                              },
                            ]}
                            dataSource={inventoryComponents}
                            scroll={{ x: 900 }}
                          />
                        )}
                      </div>
                    </div>

                    <JsonDataViewer
                      title="Raw inventory JSON"
                      value={details.latest_inventory}
                      emptyDescription="Raw inventory JSON отсутствует"
                    />
                  </Space>
                ) : (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Inventory snapshot для агента пока отсутствует" />
                )}
              </Card>

              <Card
                size="small"
                type="inner"
                title={(
                  <Space>
                    <span>Adapter statuses</span>
                    {agent.has_adapter_statuses ? <Tag color="success">Есть snapshot</Tag> : <Tag>Нет snapshot</Tag>}
                  </Space>
                )}
              >
                {adapterStatuses.length === 0 ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Статусы адаптеров для агента пока отсутствуют" />
                ) : (
                  <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                    <Table
                      size="small"
                      rowKey={(record, index) => `${record.adapter_id || 'adapter'}-${record.version || 'na'}-${index}`}
                      columns={adapterColumns}
                      dataSource={adapterStatuses}
                      pagination={{ pageSize: 5, hideOnSinglePage: true }}
                      scroll={{ x: 1100 }}
                    />
                    <JsonDataViewer
                      title="Raw adapter_statuses JSON"
                      value={details.latest_adapter_statuses}
                      emptyDescription="Raw adapter statuses JSON отсутствует"
                    />
                  </Space>
                )}
              </Card>
            </Space>
          </Card>

          <Card className="glass-panel" title="История попыток регистрации" size="small">
            {details.recent_registrations.length === 0 ? (
              <Empty description="История попыток регистрации пока пуста" />
            ) : (
              <Table
                size="small"
                rowKey="id"
                columns={registrationHistoryColumns}
                dataSource={details.recent_registrations}
                pagination={{ pageSize: 10, hideOnSinglePage: true }}
                scroll={{ x: 1400 }}
              />
            )}
          </Card>
        </>
      )}

      <AgentRegistrationAttemptModal
        open={Boolean(activeAttempt)}
        attempt={activeAttempt}
        onClose={() => setActiveAttempt(null)}
      />
    </Space>
  );
};

export default AgentDiagnosticsPage;
