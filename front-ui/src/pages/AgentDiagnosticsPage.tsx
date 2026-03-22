import React, { useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeftOutlined } from '@ant-design/icons';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Row,
  Skeleton,
  Space,
  Statistic,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';

import { agentDiagnosticsApi } from '@/api/agentDiagnostics';
import AgentAdapterRunsCard from '@/components/agents/AgentAdapterRunsCard';
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
  AgentAdapterRunDTO,
  AgentAdapterStatusDTO,
  AgentDiagnosticsDetailsDTO,
  AgentInventoryCOMPortDTO,
  AgentInventoryHostInfoDTO,
  AgentInventorySnapshotDTO,
  AgentRegistrationAttemptDTO,
  EnqueueAgentAdapterRunPayload,
  SaveAgentAdapterSelectionPayload,
} from '@/types/api';

const { Title, Text } = Typography;

type OperatorFlowStep = {
  type: 'success' | 'info' | 'warning' | 'error';
  title: string;
  description: string;
};

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

const toRecentAdapterRuns = (value: AgentDiagnosticsDetailsDTO['recent_adapter_runs']) => {
  if (!Array.isArray(value)) {
    return [] as AgentAdapterRunDTO[];
  }
  return value as AgentAdapterRunDTO[];
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

const normalizeValue = (value?: string | null) => String(value || '').trim().toLowerCase();

const isReadyAdapterStatus = (status: AgentAdapterStatusDTO) => {
  const normalized = normalizeValue(status.status);
  return normalized === 'ready' || normalized === 'installed';
};

const isFailedAdapterStatus = (status: AgentAdapterStatusDTO) => {
  const normalizedStatus = normalizeValue(status.status);
  const normalizedRunStatus = normalizeValue(status.run_status);
  return normalizedStatus === 'error'
    || normalizedStatus === 'failed'
    || normalizedRunStatus === 'failed'
    || normalizedRunStatus === 'timeout'
    || Boolean(String(status.last_error || '').trim());
};

const AgentDiagnosticsPage: React.FC = () => {
  const { uuid = '' } = useParams<{ uuid: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [activeAttempt, setActiveAttempt] = useState<AgentRegistrationAttemptDTO | null>(null);
  const [activeTabKey, setActiveTabKey] = useState('overview');

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
  const recentAdapterRuns = useMemo(() => toRecentAdapterRuns(details?.recent_adapter_runs), [details?.recent_adapter_runs]);
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

  const runAdapterMutation = useMutation({
    mutationFn: async (payload: EnqueueAgentAdapterRunPayload) => {
      const response = await agentDiagnosticsApi.runAdapter(uuid, payload);
      return response.data;
    },
    onSuccess: async (nextDetails, payload) => {
      await updateDiagnosticsCache(nextDetails);
      message.success(`Команда запуска ${payload.adapter_id} поставлена в очередь`);
      setActiveTabKey('runs');
    },
    onError: (mutationError) => {
      message.error(getErrorMessage(mutationError));
    },
  });

  const inventoryNetworkInterfaces = useMemo(() => inventory?.network_interfaces || [], [inventory?.network_interfaces]);
  const inventoryHostInfo = (inventory?.host_info || null) as AgentInventoryHostInfoDTO | null;
  const inventoryComPorts = useMemo(() => inventory?.com_ports || [], [inventory?.com_ports]);
  const inventorySoftware = useMemo(() => inventory?.installed_software || [], [inventory?.installed_software]);
  const inventoryComponents = useMemo(() => inventory?.known_components || [], [inventory?.known_components]);
  const selectedAdapterIDs = diagnosticsOperatorFlow?.selected_adapter_ids || [];
  const recommendedAdapterIDs = diagnosticsOperatorFlow?.recommended_adapter_ids || [];
  const effectiveManifests = diagnosticsOperatorFlow?.effective_adapter_manifests || [];
  const readyAdaptersCount = useMemo(() => adapterStatuses.filter(isReadyAdapterStatus).length, [adapterStatuses]);
  const failedAdaptersCount = useMemo(() => adapterStatuses.filter(isFailedAdapterStatus).length, [adapterStatuses]);
  const completedRunsCount = useMemo(
    () => recentAdapterRuns.filter((item) => normalizeValue(item.status) === 'completed').length,
    [recentAdapterRuns],
  );
  const failedRunsCount = useMemo(
    () => recentAdapterRuns.filter((item) => normalizeValue(item.status) === 'failed').length,
    [recentAdapterRuns],
  );
  const latestAdapterErrors = useMemo(
    () => adapterStatuses
      .map((item) => ({
        adapterID: item.adapter_id || 'adapter',
        errorText: String(item.last_error || '').trim(),
      }))
      .filter((item) => item.errorText),
    [adapterStatuses],
  );
  const knownAdapterIDs = useMemo(() => (
    Array.from(new Set([
      ...selectedAdapterIDs,
      ...adapterStatuses.map((item) => String(item.adapter_id || '').trim()).filter(Boolean),
      ...recentAdapterRuns.map((item) => String(item.adapter_id || '').trim()).filter(Boolean),
    ])).sort((left, right) => left.localeCompare(right))
  ), [adapterStatuses, recentAdapterRuns, selectedAdapterIDs]);

  const operatorFlow = useMemo<OperatorFlowStep[]>(() => {
    const steps: OperatorFlowStep[] = [];

    if (registrationApprovalPending) {
      steps.push({
        type: 'warning',
        title: 'Регистрация ожидает подтверждения',
        description: 'Сервер уже увидел bootstrap-запрос, но токены ещё не выданы. После подтверждения агент повторит bootstrap и выйдет на heartbeat.',
      });
    } else if (registrationApprovalConfirmed) {
      steps.push({
        type: 'info',
        title: 'Ожидается повторный bootstrap',
        description: 'Регистрация уже подтверждена оператором. Следующее ожидаемое событие — повторный запрос агента за токенами и выход на heartbeat.',
      });
    } else if (agent?.last_registration_status === 'success') {
      steps.push({
        type: 'success',
        title: 'Регистрация завершена',
        description: 'Агент успешно получил токены и может стабильно работать через heartbeat.',
      });
    }

    if (!heartbeatAvailable) {
      steps.push({
        type: 'warning',
        title: 'Heartbeat snapshot ещё не пришёл',
        description: 'Пока нет inventory и статусов адаптеров. До первого heartbeat оператор видит только bootstrap-состояние.',
      });
    } else {
      steps.push({
        type: 'success',
        title: 'Heartbeat snapshot получен',
        description: 'Можно разбирать inventory, COM-устройства и фактическое состояние назначенных адаптеров.',
      });
    }

    if (heartbeatAvailable && recommendedAdapterIDs.length > 0 && selectedAdapterIDs.length === 0) {
      steps.push({
        type: 'info',
        title: 'Есть серверная рекомендация по адаптерам',
        description: `По текущему heartbeat сервер рекомендует: ${recommendedAdapterIDs.join(', ')}.`,
      });
    }

    if (heartbeatAvailable && selectedAdapterIDs.length === 0) {
      steps.push({
        type: 'warning',
        title: 'Адаптеры ещё не назначены',
        description: 'Откройте вкладку "Адаптеры", выберите published adapters и сохраните конфигурацию.',
      });
    } else if (heartbeatAvailable && adapterStatuses.length === 0 && effectiveManifests.length > 0) {
      steps.push({
        type: 'info',
        title: 'Выбор сохранён, ждём следующий heartbeat',
        description: 'Сервер уже собрал effective adapter_manifests. Следующий heartbeat должен забрать manifests и начать отчитываться по adapter_statuses.',
      });
    } else if (adapterStatuses.length > 0) {
      steps.push({
        type: 'success',
        title: 'Агент уже отчитывается по адаптерам',
        description: 'Назначение прошло полный цикл: выбор сохранён, manifests выданы, heartbeat уже несёт adapter_statuses.',
      });
    }

    if (failedRunsCount > 0 || latestAdapterErrors.length > 0) {
      steps.push({
        type: 'error',
        title: 'Есть ошибки последних запусков',
        description: 'Основное место диагностики сейчас — вкладка "Запуски адаптеров". Там видно очередь команд, result payload и stderr/stdout.',
      });
    }

    return steps;
  }, [
    adapterStatuses.length,
    agent?.last_registration_status,
    effectiveManifests.length,
    failedRunsCount,
    heartbeatAvailable,
    latestAdapterErrors.length,
    recommendedAdapterIDs,
    registrationApprovalConfirmed,
    registrationApprovalPending,
    selectedAdapterIDs.length,
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
      title: 'Адрес источника',
      dataIndex: 'remote_addr',
      key: 'remote_addr',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Fingerprint',
      dataIndex: 'machine_fingerprint',
      key: 'machine_fingerprint',
      width: 240,
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
      width: 180,
      render: (_value, record) => (
        <Button type="link" style={{ paddingInline: 0 }} onClick={() => setActiveAttempt(record)}>
          Открыть детали
        </Button>
      ),
    },
  ];

  const adapterColumns: ColumnsType<AgentAdapterStatusDTO> = [
    {
      title: 'Адаптер',
      dataIndex: 'adapter_id',
      key: 'adapter_id',
      width: 180,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Тип адаптера',
      dataIndex: 'adapter_type',
      key: 'adapter_type',
      width: 160,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Версия',
      dataIndex: 'version',
      key: 'version',
      width: 120,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Состояние',
      dataIndex: 'status',
      key: 'status',
      width: 150,
      render: (value?: string) => {
        const normalized = normalizeValue(value);
        const color = normalized === 'ready' || normalized === 'installed'
          ? 'success'
          : normalized === 'error' || normalized === 'failed'
            ? 'error'
            : normalized
              ? 'processing'
              : 'default';
        return (
          <Tag color={color} style={{ marginInlineEnd: 0 }}>
            {value || 'Не указано'}
          </Tag>
        );
      },
    },
    {
      title: 'Последний запуск',
      dataIndex: 'run_status',
      key: 'run_status',
      width: 160,
      render: (value?: string) => {
        const normalized = normalizeValue(value);
        const color = normalized === 'completed'
          ? 'success'
          : normalized === 'failed' || normalized === 'timeout'
            ? 'error'
            : normalized === 'running'
              ? 'processing'
              : normalized
                ? 'warning'
                : 'default';
        return (
          <Tag color={color} style={{ marginInlineEnd: 0 }}>
            {value || '-'}
          </Tag>
        );
      },
    },
    {
      title: 'ОС',
      dataIndex: 'target_os',
      key: 'target_os',
      width: 120,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Архитектура',
      dataIndex: 'target_arch',
      key: 'target_arch',
      width: 130,
      render: (value?: string) => value || '-',
    },
    {
      title: 'Exit code',
      dataIndex: 'last_exit_code',
      key: 'last_exit_code',
      width: 120,
      render: (value?: number | null) => (typeof value === 'number' ? value : '-'),
    },
    {
      title: 'Время запуска',
      dataIndex: 'last_run_at',
      key: 'last_run_at',
      width: 180,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: 'Последняя ошибка',
      dataIndex: 'last_error',
      key: 'last_error',
      render: (value?: string) => (
        value ? <Text ellipsis={{ tooltip: value }}>{value}</Text> : <Text type="secondary">-</Text>
      ),
    },
    {
      title: 'Обновлено',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 180,
      render: (value?: string) => formatDateTime(value),
    },
  ];

  const renderOverviewTab = () => (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      {registrationApprovalPending ? (
        <Alert
          type="warning"
          showIcon
          message="Требуется подтверждение bootstrap-регистрации"
          description="До подтверждения оператором сервер не выдаст токены. После подтверждения агент сам повторит bootstrap-запрос."
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

      {!heartbeatAvailable ? (
        <Alert
          type={agent?.last_registration_status === 'success' ? 'info' : 'warning'}
          showIcon
          message={agent?.last_registration_status === 'success'
            ? 'Агент зарегистрирован, но heartbeat snapshot ещё не пришёл'
            : 'Heartbeat snapshot пока отсутствует'}
          description={agent?.last_registration_status === 'success'
            ? 'Сервер ещё не получил inventory snapshot и статусы адаптеров после успешной регистрации.'
            : 'Проверьте, дошёл ли агент до отправки heartbeat после bootstrap-регистрации.'}
        />
      ) : null}

      {failedRunsCount > 0 || latestAdapterErrors.length > 0 ? (
        <Alert
          type="error"
          showIcon
          message="Есть ошибки последних запусков адаптеров"
          description={'Перейдите во вкладку "Запуски адаптеров". Там доступна очередь команд, result payload и stderr/stdout.'}
          action={(
            <Button onClick={() => setActiveTabKey('runs')}>
              Открыть запуски
            </Button>
          )}
        />
      ) : null}

      <Card className="glass-panel" title="Сводка по агенту" size="small">
        <Descriptions bordered column={2} size="small">
          <Descriptions.Item label="UUID">
            {agent?.uuid ? <Text copyable={{ text: agent.uuid }} code>{agent.uuid}</Text> : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Hostname">
            {agent?.hostname || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Тип агента">
            {agent?.type || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Статус агента">
            <Tag color={getAgentStatusColor(agent?.status)} style={{ marginInlineEnd: 0 }}>
              {agent?.status || 'Не указан'}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Статус регистрации">
            <Tag color={registrationStatus.color} style={{ marginInlineEnd: 0 }}>
              {registrationStatus.label}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Последний heartbeat">
            <Space direction="vertical" size={0}>
              <Text>{formatDateTime(agent?.last_heartbeat)}</Text>
              <Space size={8}>
                <Tag color={heartbeatFreshness.color} style={{ marginInlineEnd: 0 }}>
                  {heartbeatFreshness.label}
                </Tag>
                <Text type="secondary">{formatRelativeTime(agent?.last_heartbeat) || '-'}</Text>
              </Space>
            </Space>
          </Descriptions.Item>
          <Descriptions.Item label="Последнее наблюдение">
            <Space direction="vertical" size={0}>
              <Text>{formatDateTime(agent?.last_observed_at)}</Text>
              <Text type="secondary">{formatRelativeTime(agent?.last_observed_at) || '-'}</Text>
            </Space>
          </Descriptions.Item>
          <Descriptions.Item label="machine_fingerprint" span={2}>
            {agent?.machine_fingerprint ? (
              <Text copyable={{ text: agent.machine_fingerprint }} code>{agent.machine_fingerprint}</Text>
            ) : (
              '-'
            )}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card className="glass-panel" title="High-level состояние" size="small">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Descriptions bordered size="small" column={2}>
            <Descriptions.Item label="Регистрация">
              <Tag color={registrationStatus.color} style={{ marginInlineEnd: 0 }}>
                {registrationStatus.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Heartbeat snapshot">
              <Space wrap>
                <Tag color={heartbeatAvailable ? 'success' : 'warning'} style={{ marginInlineEnd: 0 }}>
                  {heartbeatAvailable ? 'Есть' : 'Пока нет'}
                </Tag>
                <Tag color={heartbeatFreshness.color} style={{ marginInlineEnd: 0 }}>
                  {heartbeatFreshness.label}
                </Tag>
              </Space>
            </Descriptions.Item>
            <Descriptions.Item label="Адаптеры назначены">
              {selectedAdapterIDs.length > 0 ? `${selectedAdapterIDs.length} шт.` : 'Нет'}
            </Descriptions.Item>
            <Descriptions.Item label="Последние adapter statuses">
              {adapterStatuses.length > 0 ? `${adapterStatuses.length} шт.` : 'Пока нет'}
            </Descriptions.Item>
            <Descriptions.Item label="Ошибки последних запусков" span={2}>
              {(failedRunsCount > 0 || latestAdapterErrors.length > 0)
                ? <Tag color="error" style={{ marginInlineEnd: 0 }}>Есть ошибки</Tag>
                : <Tag color="success" style={{ marginInlineEnd: 0 }}>Не обнаружены</Tag>}
            </Descriptions.Item>
          </Descriptions>

          {operatorFlow.length === 0 ? (
            <Text type="secondary">Высокоуровневое состояние пока не сформировано.</Text>
          ) : (
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
          )}
        </Space>
      </Card>

      <Card className="glass-panel" title="Сводка по адаптерам" size="small">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Row gutter={[16, 16]}>
            <Col xs={24} sm={12} lg={6}>
              <Card size="small" type="inner">
                <Statistic title="Назначено" value={selectedAdapterIDs.length} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card size="small" type="inner">
                <Statistic title="Ready / installed" value={readyAdaptersCount} valueStyle={{ color: '#3f8600' }} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card size="small" type="inner">
                <Statistic title="С ошибкой" value={failedAdaptersCount} valueStyle={{ color: failedAdaptersCount > 0 ? '#cf1322' : undefined }} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card size="small" type="inner">
                <Statistic title="Успешных запусков" value={completedRunsCount} />
              </Card>
            </Col>
            <Col xs={24} sm={12} lg={6}>
              <Card size="small" type="inner">
                <Statistic title="Запусков с ошибкой" value={failedRunsCount} valueStyle={{ color: failedRunsCount > 0 ? '#cf1322' : undefined }} />
              </Card>
            </Col>
          </Row>

          {knownAdapterIDs.length > 0 ? (
            <Text type="secondary">
              Известные адаптеры: {knownAdapterIDs.join(', ')}.
            </Text>
          ) : (
            <Text type="secondary">
              Сервер пока не видит ни назначенных, ни активных адаптеров у этого агента.
            </Text>
          )}

          {latestAdapterErrors.length > 0 ? (
            <Alert
              type="error"
              showIcon
              message="Последние ошибки по adapter_statuses"
              description={latestAdapterErrors.map((item) => `${item.adapterID}: ${item.errorText}`).join(' | ')}
            />
          ) : null}
        </Space>
      </Card>
    </Space>
  );

  const renderHeartbeatTab = () => (
    <Card className="glass-panel" title="Heartbeat snapshot и inventory" size="small">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {!heartbeatAvailable ? (
          <Alert
            type={agent?.last_registration_status === 'success' ? 'info' : 'warning'}
            showIcon
            message={agent?.last_registration_status === 'success'
              ? 'Агент зарегистрирован, но heartbeat snapshot ещё не пришёл'
              : 'Heartbeat snapshot пока отсутствует'}
            description={agent?.last_registration_status === 'success'
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
              {agent?.has_latest_inventory ? <Tag color="success">Есть snapshot</Tag> : <Tag>Нет snapshot</Tag>}
            </Space>
          )}
        >
          {inventory ? (
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Descriptions bordered size="small" column={2}>
                <Descriptions.Item label="Имя хоста">
                  {inventory.hostname || '-'}
                </Descriptions.Item>
                <Descriptions.Item label="ОС">
                  {inventory.os || '-'}
                </Descriptions.Item>
                <Descriptions.Item label="Архитектура">
                  {inventory.arch || '-'}
                </Descriptions.Item>
                <Descriptions.Item label="Собрано">
                  {formatDateTime(inventory.collected_at)}
                </Descriptions.Item>
                <Descriptions.Item label="Путь к агенту" span={2}>
                  {inventory.executable_path || '-'}
                </Descriptions.Item>
              </Descriptions>

              <div>
                <Text strong>Host info</Text>
                <div style={{ marginTop: 8 }}>
                  {inventoryHostInfo ? (
                    <Descriptions bordered size="small" column={2}>
                      <Descriptions.Item label="Продукт CashServer">
                        {inventoryHostInfo.cash_server_product || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="URL CashServer">
                        {inventoryHostInfo.cash_server_url || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="Конфиг CashServer" span={2}>
                        {inventoryHostInfo.cash_server_config || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="Roaming AppData" span={2}>
                        {inventoryHostInfo.roaming_app_data_path || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="TeamViewer ID">
                        {inventoryHostInfo.teamviewer_id || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="AnyDesk ID">
                        {inventoryHostInfo.anydesk_id || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="LiteManager ID">
                        {inventoryHostInfo.litemanager_id || '-'}
                      </Descriptions.Item>
                      <Descriptions.Item label="RustDesk ID">
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
                        { title: 'Имя', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                        { title: 'Индекс', dataIndex: 'index', key: 'index', width: 90, render: (value?: number) => value ?? '-' },
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
                        { title: 'Название', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                        { title: 'Версия', dataIndex: 'version', key: 'version', width: 120, render: (value?: string) => value || '-' },
                        { title: 'Издатель', dataIndex: 'publisher', key: 'publisher', render: (value?: string) => value || '-' },
                        { title: 'Источник', dataIndex: 'source', key: 'source', width: 120, render: (value?: string) => value || '-' },
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
                        { title: 'Ключ', dataIndex: 'key', key: 'key', render: (value?: string) => value || '-' },
                        { title: 'Название', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                        { title: 'Категория', dataIndex: 'category', key: 'category', render: (value?: string) => value || '-' },
                        {
                          title: 'Обнаружен',
                          dataIndex: 'detected',
                          key: 'detected',
                          width: 120,
                          render: (value?: boolean) => (
                            <Tag color={value ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>
                              {value ? 'Да' : 'Нет'}
                            </Tag>
                          ),
                        },
                        { title: 'Версия', dataIndex: 'version', key: 'version', width: 120, render: (value?: string) => value || '-' },
                        {
                          title: 'Доказательства',
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
                value={details?.latest_inventory}
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
              <span>Последние adapter_statuses</span>
              {agent?.has_adapter_statuses ? <Tag color="success">Есть snapshot</Tag> : <Tag>Нет snapshot</Tag>}
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
                scroll={{ x: 1150 }}
              />
              <JsonDataViewer
                title="Raw adapter_statuses JSON"
                value={details?.latest_adapter_statuses}
                emptyDescription="Raw adapter_statuses JSON отсутствует"
              />
            </Space>
          )}
        </Card>
      </Space>
    </Card>
  );

  const renderAdaptersTab = () => (
    <AgentOperatorFlowCard
      operatorFlow={diagnosticsOperatorFlow}
      inventoryCOMPorts={inventoryComPorts}
      saveSelectionPending={saveAdapterSelectionMutation.isPending}
      saveSelectionError={saveAdapterSelectionMutation.isError ? getErrorMessage(saveAdapterSelectionMutation.error) : undefined}
      runAdapterPending={runAdapterMutation.isPending}
      runningAdapterID={runAdapterMutation.variables?.adapter_id || null}
      onSaveSelection={(payload) => saveAdapterSelectionMutation.mutate(payload)}
      onRunAdapter={(payload) => runAdapterMutation.mutate(payload)}
    />
  );

  const renderRunsTab = () => (
    <AgentAdapterRunsCard
      runs={recentAdapterRuns}
      knownAdapterIDs={knownAdapterIDs}
    />
  );

  const renderRegistrationTab = () => (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel" title="Последняя регистрация" size="small">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Alert
            type={registrationStatus.alertType}
            showIcon
            message={registrationStatus.label}
            description={agent?.last_registration_error || registrationStatus.helper}
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
              {formatDateTime(agent?.last_registration_at)}
            </Descriptions.Item>
            <Descriptions.Item label="Статус">
              <Tag color={registrationStatus.color} style={{ marginInlineEnd: 0 }}>
                {registrationStatus.label}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="Fingerprint" span={2}>
              {agent?.machine_fingerprint ? (
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
              {agent?.last_registration_error || <Text type="secondary">Нет</Text>}
            </Descriptions.Item>
          </Descriptions>

          <JsonDataViewer
            title="System info"
            value={systemInfo}
            emptyDescription="System info для последней регистрации отсутствует"
          />

          <JsonDataViewer
            title="Registration payload"
            value={details?.registration_payload}
            emptyDescription="Сервер пока не сохранил registration payload"
          />
        </Space>
      </Card>

      <Card className="glass-panel" title="История попыток регистрации" size="small">
        {details?.recent_registrations.length ? (
          <Table
            size="small"
            rowKey="id"
            columns={registrationHistoryColumns}
            dataSource={details.recent_registrations}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            scroll={{ x: 1480 }}
          />
        ) : (
          <Empty description="История попыток регистрации пока пуста" />
        )}
      </Card>
    </Space>
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space wrap style={{ justifyContent: 'space-between', width: '100%' }}>
        <div>
          <Title level={4} style={{ margin: 0 }}>
            Диагностика агента
          </Title>
          <Text type="secondary">
            Страница разделена по сценариям работы оператора: обзор, heartbeat, адаптеры, история запусков и регистрация.
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
        <Tabs
          activeKey={activeTabKey}
          onChange={setActiveTabKey}
          items={[
            {
              key: 'overview',
              label: 'Обзор',
              children: renderOverviewTab(),
            },
            {
              key: 'heartbeat',
              label: 'Heartbeat / Inventory',
              children: renderHeartbeatTab(),
            },
            {
              key: 'adapters',
              label: 'Адаптеры',
              children: renderAdaptersTab(),
            },
            {
              key: 'runs',
              label: 'Запуски адаптеров',
              children: renderRunsTab(),
            },
            {
              key: 'registration',
              label: 'Регистрация',
              children: renderRegistrationTab(),
            },
          ]}
        />
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
