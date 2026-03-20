import React, { useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Alert, Button, Card, Descriptions, Empty, Skeleton, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { agentDiagnosticsApi } from '@/api/agentDiagnostics';
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
  AgentDiagnosticsDetailsDTO,
  AgentInventorySnapshotDTO,
  AgentRegistrationAttemptDTO,
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

const AgentDiagnosticsPage: React.FC = () => {
  const { uuid = '' } = useParams<{ uuid: string }>();
  const navigate = useNavigate();
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
  const heartbeatAvailable = Boolean(inventory || adapterStatuses.length > 0);

  const inventoryNetworkInterfaces = inventory?.network_interfaces || [];
  const inventoryComPorts = inventory?.com_ports || [];
  const inventorySoftware = inventory?.installed_software || [];
  const inventoryComponents = inventory?.known_components || [];

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

          <Card className="glass-panel" title="Последняя регистрация" size="small">
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Alert
                type={registrationStatus.alertType}
                showIcon
                message={registrationStatus.label}
                description={agent.last_registration_error || registrationStatus.helper}
              />

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
                          <Table
                            size="small"
                            rowKey={(record, index) => `${record.name || 'com'}-${index}`}
                            pagination={false}
                            columns={[
                              { title: 'name', dataIndex: 'name', key: 'name', render: (value?: string) => value || '-' },
                              { title: 'device', dataIndex: 'device', key: 'device', render: (value?: string) => value || '-' },
                              { title: 'source', dataIndex: 'source', key: 'source', render: (value?: string) => value || '-' },
                            ]}
                            dataSource={inventoryComPorts}
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
