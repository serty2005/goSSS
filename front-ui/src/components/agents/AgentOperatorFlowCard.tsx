import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Empty,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from 'antd';
import {
  AgentAdapterRuntimeProfileDTO,
  AgentInventoryCOMPortDTO,
  AgentOperatorFlowDTO,
  EnqueueAgentAdapterRunPayload,
  PublishedAgentAdapterOptionDTO,
  SaveAgentAdapterSelectionPayload,
} from '@/types/api';

const { Paragraph, Text } = Typography;
const { TextArea } = Input;

type RuntimeDeviceDraft = {
  label: string;
  connectionType: 'tcp' | 'com';
  ip: string;
  port: number | null;
  comPort: string;
  baudrate: string;
  model: string;
  driverHintsText: string;
  extraParamsText: string;
};

type RuntimeProfileDraft = {
  adapterID: string;
  command: string;
  operation: string;
  timeoutSeconds: number | null;
  scheduleEnabled: boolean;
  scheduleIntervalSeconds: number | null;
  devices: RuntimeDeviceDraft[];
};

type AgentOperatorFlowCardProps = {
  operatorFlow?: AgentOperatorFlowDTO | null;
  inventoryCOMPorts?: AgentInventoryCOMPortDTO[];
  saveSelectionPending: boolean;
  saveSelectionError?: string;
  runAdapterPending: boolean;
  runningAdapterID?: string | null;
  onSaveSelection: (payload: SaveAgentAdapterSelectionPayload) => void;
  onRunAdapter: (payload: EnqueueAgentAdapterRunPayload) => void;
};

const defaultTimeoutSeconds = 45;
const defaultScheduleIntervalSeconds = 300;

const normalizeAdapterIDs = (values?: string[] | null) => (
  Array.from(new Set((values || []).map((value) => value.trim()).filter(Boolean))).sort()
);

const renderAdapterMeta = (adapter: PublishedAgentAdapterOptionDTO) => (
  [
    adapter.adapter_id,
    adapter.adapter_type,
    [adapter.target_os, adapter.target_arch].filter(Boolean).join('/'),
  ]
    .filter(Boolean)
    .join(' • ')
);

const prettyJSON = (value?: Record<string, unknown>) => (
  value && Object.keys(value).length > 0 ? JSON.stringify(value, null, 2) : ''
);

const buildEmptyDeviceDraft = (): RuntimeDeviceDraft => ({
  label: '',
  connectionType: 'tcp',
  ip: '',
  port: null,
  comPort: '',
  baudrate: '115200',
  model: '',
  driverHintsText: '',
  extraParamsText: '',
});

const buildDraftFromProfile = (adapterID: string, profile?: AgentAdapterRuntimeProfileDTO): RuntimeProfileDraft => {
  const normalizedDevices = (profile?.devices || []).map((device) => {
    const connectionType: 'tcp' | 'com' = String(device.connection_type || device.transport || 'tcp').trim().toLowerCase() === 'com' ? 'com' : 'tcp';
    return {
      label: String(device.label || '').trim(),
      connectionType,
      ip: String(device.ip || '').trim(),
      port: typeof device.port === 'number' && Number.isFinite(device.port) ? device.port : null,
      comPort: String(device.com_port || '').trim(),
      baudrate: String(device.baudrate || '').trim(),
      model: String(device.model || '').trim(),
      driverHintsText: prettyJSON(device.driver_hints),
      extraParamsText: prettyJSON(device.extra_params),
    };
  });

  return {
    adapterID,
    command: String(profile?.command || 'run').trim() || 'run',
    operation: String(profile?.operation || 'collect').trim() || 'collect',
    timeoutSeconds: typeof profile?.timeout_seconds === 'number' && profile.timeout_seconds > 0 ? profile.timeout_seconds : defaultTimeoutSeconds,
    scheduleEnabled: Boolean(profile?.schedule?.enabled),
    scheduleIntervalSeconds: typeof profile?.schedule?.interval_seconds === 'number' && profile.schedule.interval_seconds > 0
      ? profile.schedule.interval_seconds
      : defaultScheduleIntervalSeconds,
    devices: normalizedDevices.length > 0 ? normalizedDevices : [buildEmptyDeviceDraft()],
  };
};

const toJSONObject = (raw: string, fieldLabel: string, adapterID: string, deviceIndex: number) => {
  const text = raw.trim();
  if (!text) {
    return undefined;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new Error(`${adapterID}: устройство #${deviceIndex + 1}, поле «${fieldLabel}» должно содержать валидный JSON-объект`);
  }

  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error(`${adapterID}: устройство #${deviceIndex + 1}, поле «${fieldLabel}» должно быть JSON-объектом`);
  }

  return parsed as Record<string, unknown>;
};

const isDeviceDraftMeaningful = (device: RuntimeDeviceDraft) => (
  Boolean(
    device.label.trim()
    || device.ip.trim()
    || device.comPort.trim()
    || device.baudrate.trim()
    || device.model.trim()
    || device.driverHintsText.trim()
    || device.extraParamsText.trim()
    || device.port,
  )
);

const buildSavePayload = (
  selectedAdapterIDs: string[],
  runtimeProfiles: Record<string, RuntimeProfileDraft>,
): SaveAgentAdapterSelectionPayload => {
  const normalizedIDs = normalizeAdapterIDs(selectedAdapterIDs);
  const runtimeProfilesPayload = normalizedIDs.map((adapterID) => {
    const profile = runtimeProfiles[adapterID] || buildDraftFromProfile(adapterID);
    const devices = profile.devices
      .filter(isDeviceDraftMeaningful)
      .map((device, index) => {
        const driverHints = toJSONObject(device.driverHintsText, 'driver_hints', adapterID, index);
        const extraParams = toJSONObject(device.extraParamsText, 'extra_params', adapterID, index);

        return {
          connection_type: device.connectionType,
          ip: device.ip.trim() || undefined,
          port: device.port && device.port > 0 ? device.port : undefined,
          com_port: device.comPort.trim() || undefined,
          baudrate: device.baudrate.trim() || undefined,
          model: device.model.trim() || undefined,
          label: device.label.trim() || undefined,
          driver_hints: driverHints,
          extra_params: extraParams,
        };
      });

    return {
      adapter_id: adapterID,
      command: profile.command.trim() || 'run',
      operation: profile.operation.trim() || 'collect',
      timeout_seconds: profile.timeoutSeconds && profile.timeoutSeconds > 0 ? profile.timeoutSeconds : defaultTimeoutSeconds,
      devices,
      schedule: {
        enabled: profile.scheduleEnabled,
        interval_seconds: profile.scheduleEnabled
          ? (profile.scheduleIntervalSeconds && profile.scheduleIntervalSeconds > 0
            ? profile.scheduleIntervalSeconds
            : defaultScheduleIntervalSeconds)
          : undefined,
      },
    } satisfies AgentAdapterRuntimeProfileDTO;
  });

  return {
    selected_adapter_ids: normalizedIDs,
    runtime_profiles: runtimeProfilesPayload,
  };
};

const payloadSignature = (payload: SaveAgentAdapterSelectionPayload) => JSON.stringify(payload);

const buildInitialPayload = (operatorFlow?: AgentOperatorFlowDTO | null): SaveAgentAdapterSelectionPayload => ({
  selected_adapter_ids: normalizeAdapterIDs(operatorFlow?.selected_adapter_ids),
  runtime_profiles: normalizeAdapterIDs(operatorFlow?.selected_adapter_ids).map((adapterID) => {
    const saved = (operatorFlow?.saved_adapter_runtime_profiles || []).find((profile) => profile.adapter_id === adapterID);
    return {
      adapter_id: adapterID,
      command: saved?.command || 'run',
      operation: saved?.operation || 'collect',
      timeout_seconds: saved?.timeout_seconds || defaultTimeoutSeconds,
      devices: saved?.devices || [],
      schedule: {
        enabled: Boolean(saved?.schedule?.enabled),
        interval_seconds: saved?.schedule?.interval_seconds,
      },
    } satisfies AgentAdapterRuntimeProfileDTO;
  }),
});

const connectionTypeOptions = [
  { label: 'TCP/IP', value: 'tcp' },
  { label: 'COM', value: 'com' },
];

const commandOptions = [
  { label: 'run', value: 'run' },
  { label: 'health', value: 'health' },
  { label: 'describe', value: 'describe' },
];

const AgentOperatorFlowCard: React.FC<AgentOperatorFlowCardProps> = ({
  operatorFlow,
  inventoryCOMPorts = [],
  saveSelectionPending,
  saveSelectionError,
  runAdapterPending,
  runningAdapterID,
  onSaveSelection,
  onRunAdapter,
}) => {
  const availableAdapters = operatorFlow?.available_adapters || [];
  const warnings = operatorFlow?.warnings || [];
  const recommendedAdapterIDs = operatorFlow?.recommended_adapter_ids || [];
  const effectiveManifests = operatorFlow?.effective_adapter_manifests || [];
  const savedRuntimeProfiles = operatorFlow?.saved_adapter_runtime_profiles || [];

  const [selectedAdapterIDs, setSelectedAdapterIDs] = useState<string[]>([]);
  const [runtimeProfiles, setRuntimeProfiles] = useState<Record<string, RuntimeProfileDraft>>({});
  const [localValidationError, setLocalValidationError] = useState<string>();

  useEffect(() => {
    const nextSelectedIDs = normalizeAdapterIDs(operatorFlow?.selected_adapter_ids);
    const nextProfiles = nextSelectedIDs.reduce<Record<string, RuntimeProfileDraft>>((acc, adapterID) => {
      const savedProfile = savedRuntimeProfiles.find((item) => item.adapter_id === adapterID);
      acc[adapterID] = buildDraftFromProfile(adapterID, savedProfile);
      return acc;
    }, {});

    setSelectedAdapterIDs(nextSelectedIDs);
    setRuntimeProfiles(nextProfiles);
    setLocalValidationError(undefined);
  }, [operatorFlow, savedRuntimeProfiles]);

  const comPortOptions = useMemo(
    () => inventoryCOMPorts
      .map((port) => {
        const value = String(port.name || '').trim();
        if (!value) {
          return null;
        }
        return {
          label: `${value}${port.friendly_name ? ` • ${port.friendly_name}` : ''}`,
          value,
        };
      })
      .filter((item): item is { label: string; value: string } => Boolean(item)),
    [inventoryCOMPorts],
  );

  const availableAdaptersByID = useMemo(
    () => availableAdapters.reduce<Record<string, PublishedAgentAdapterOptionDTO>>((acc, adapter) => {
      acc[adapter.adapter_id] = adapter;
      return acc;
    }, {}),
    [availableAdapters],
  );

  const initialSignature = useMemo(
    () => payloadSignature(buildInitialPayload(operatorFlow)),
    [operatorFlow],
  );

  const currentSignature = useMemo(() => {
    try {
      return payloadSignature(buildSavePayload(selectedAdapterIDs, runtimeProfiles));
    } catch {
      return '__invalid_payload__';
    }
  }, [selectedAdapterIDs, runtimeProfiles]);

  const hasUnsavedChanges = currentSignature !== initialSignature;

  const toggleAdapter = (adapterID: string, checked: boolean) => {
    setLocalValidationError(undefined);
    setSelectedAdapterIDs((current) => {
      if (checked) {
        return normalizeAdapterIDs([...current, adapterID]);
      }
      return current.filter((value) => value !== adapterID);
    });
    setRuntimeProfiles((current) => {
      if (checked) {
        return {
          ...current,
          [adapterID]: current[adapterID] || buildDraftFromProfile(adapterID, savedRuntimeProfiles.find((item) => item.adapter_id === adapterID)),
        };
      }
      const next = { ...current };
      delete next[adapterID];
      return next;
    });
  };

  const updateProfile = (adapterID: string, updater: (current: RuntimeProfileDraft) => RuntimeProfileDraft) => {
    setRuntimeProfiles((current) => ({
      ...current,
      [adapterID]: updater(current[adapterID] || buildDraftFromProfile(adapterID)),
    }));
  };

  const updateDevice = (
    adapterID: string,
    deviceIndex: number,
    updater: (current: RuntimeDeviceDraft) => RuntimeDeviceDraft,
  ) => {
    updateProfile(adapterID, (profile) => ({
      ...profile,
      devices: profile.devices.map((device, index) => (index === deviceIndex ? updater(device) : device)),
    }));
  };

  const addDevice = (adapterID: string) => {
    updateProfile(adapterID, (profile) => ({
      ...profile,
      devices: [...profile.devices, buildEmptyDeviceDraft()],
    }));
  };

  const removeDevice = (adapterID: string, deviceIndex: number) => {
    updateProfile(adapterID, (profile) => ({
      ...profile,
      devices: profile.devices.length === 1
        ? [buildEmptyDeviceDraft()]
        : profile.devices.filter((_, index) => index !== deviceIndex),
    }));
  };

  const saveSelection = () => {
    try {
      const payload = buildSavePayload(selectedAdapterIDs, runtimeProfiles);
      setLocalValidationError(undefined);
      onSaveSelection(payload);
    } catch (error) {
      setLocalValidationError(error instanceof Error ? error.message : 'Не удалось подготовить payload сохранения');
    }
  };

  const runAdapter = (adapterID: string) => {
    setLocalValidationError(undefined);
    onRunAdapter({ adapter_id: adapterID });
  };

  return (
    <Card className="glass-panel" title="Управление адаптерами" size="small">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {warnings.map((warning, index) => (
          <Alert key={`${warning}-${index}`} type="warning" showIcon message={warning} />
        ))}

        {saveSelectionError ? (
          <Alert
            type="error"
            showIcon
            message="Не удалось сохранить конфигурацию адаптеров"
            description={saveSelectionError}
          />
        ) : null}

        {localValidationError ? (
          <Alert
            type="error"
            showIcon
            message="Конфигурация адаптера заполнена некорректно"
            description={localValidationError}
          />
        ) : null}

        {recommendedAdapterIDs.length > 0 ? (
          <Alert
            type="info"
            showIcon
            message="Подсказка сервера"
            description={`Сервер рекомендует обратить внимание на: ${recommendedAdapterIDs.join(', ')}.`}
          />
        ) : null}

        <Alert
          type="info"
          showIcon
          message="Как работает расписание"
          description="Сервер хранит параметры подключения и интервалы, а плановые run_adapter-команды ставит в очередь на heartbeat. Интервал отсчитывается от last_run_at адаптера."
        />

        {availableAdapters.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Каталог опубликованных адаптеров пока пуст" />
        ) : (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {availableAdapters.map((adapter) => {
              const checked = selectedAdapterIDs.includes(adapter.adapter_id);
              return (
                <Card
                  key={adapter.adapter_id}
                  size="small"
                  type="inner"
                  title={(
                    <Checkbox
                      checked={checked}
                      disabled={!adapter.selectable || saveSelectionPending}
                      onChange={(event) => toggleAdapter(adapter.adapter_id, event.target.checked)}
                    >
                      <Text strong>{adapter.title || adapter.adapter_id}</Text>
                    </Checkbox>
                  )}
                  extra={(
                    <Space size={8}>
                      <Tag color={adapter.selectable ? 'success' : adapter.published ? 'warning' : 'default'}>
                        {adapter.status_text}
                      </Tag>
                      {adapter.stable_version ? <Tag>stable {adapter.stable_version}</Tag> : null}
                      {adapter.latest_version && adapter.latest_version !== adapter.stable_version ? (
                        <Tag>latest {adapter.latest_version}</Tag>
                      ) : null}
                      {!adapter.stable_version && adapter.version ? <Tag>{adapter.version}</Tag> : null}
                    </Space>
                  )}
                >
                  <Space direction="vertical" size={4} style={{ width: '100%' }}>
                    {adapter.description ? (
                      <Paragraph style={{ marginBottom: 0 }}>
                        {adapter.description}
                      </Paragraph>
                    ) : null}
                    <Text type="secondary">
                      {renderAdapterMeta(adapter) || 'Метаданные публикации пока не заполнены'}
                    </Text>
                    {adapter.disabled_reason ? (
                      <Text type="secondary">{adapter.disabled_reason}</Text>
                    ) : null}
                  </Space>
                </Card>
              );
            })}
          </Space>
        )}

        <Card size="small" type="inner" title="Параметры запуска и расписание">
          {selectedAdapterIDs.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Сначала выберите хотя бы один адаптер" />
          ) : (
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {selectedAdapterIDs.map((adapterID) => {
                const draft = runtimeProfiles[adapterID] || buildDraftFromProfile(adapterID);
                const adapterMeta = availableAdaptersByID[adapterID];
                const schedule = draft.scheduleEnabled ? `${draft.scheduleIntervalSeconds || defaultScheduleIntervalSeconds} сек` : 'выключено';
                return (
                  <Card
                    key={adapterID}
                    size="small"
                    type="inner"
                    title={adapterMeta?.title || adapterID}
                    extra={(
                      <Space>
                        <Tag color="processing">{schedule}</Tag>
                        <Button
                          size="small"
                          type="primary"
                          ghost
                          loading={runAdapterPending && runningAdapterID === adapterID}
                          disabled={saveSelectionPending || hasUnsavedChanges}
                          onClick={() => runAdapter(adapterID)}
                        >
                          Запустить сейчас
                        </Button>
                      </Space>
                    )}
                  >
                    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                      <Space wrap size="middle" style={{ width: '100%' }}>
                        <div style={{ minWidth: 180 }}>
                          <Text type="secondary">Команда</Text>
                          <Select
                            style={{ width: '100%', marginTop: 6 }}
                            value={draft.command}
                            options={commandOptions}
                            onChange={(value) => updateProfile(adapterID, (current) => ({ ...current, command: value }))}
                          />
                        </div>
                        <div style={{ minWidth: 220 }}>
                          <Text type="secondary">Operation / task_type</Text>
                          <Input
                            style={{ marginTop: 6 }}
                            value={draft.operation}
                            onChange={(event) => updateProfile(adapterID, (current) => ({ ...current, operation: event.target.value }))}
                            placeholder="collect"
                          />
                        </div>
                        <div style={{ minWidth: 180 }}>
                          <Text type="secondary">Timeout, сек</Text>
                          <InputNumber
                            style={{ width: '100%', marginTop: 6 }}
                            min={1}
                            value={draft.timeoutSeconds}
                            onChange={(value) => updateProfile(adapterID, (current) => ({ ...current, timeoutSeconds: value }))}
                          />
                        </div>
                      </Space>

                      <Card size="small" type="inner" title="Расписание">
                        <Space wrap size="middle" style={{ width: '100%', alignItems: 'center' }}>
                          <Space direction="vertical" size={4}>
                            <Text type="secondary">Запускать автоматически</Text>
                            <Switch
                              checked={draft.scheduleEnabled}
                              onChange={(checked) => updateProfile(adapterID, (current) => ({ ...current, scheduleEnabled: checked }))}
                            />
                          </Space>
                          <div style={{ minWidth: 220 }}>
                            <Text type="secondary">Интервал, сек</Text>
                            <InputNumber
                              style={{ width: '100%', marginTop: 6 }}
                              min={30}
                              disabled={!draft.scheduleEnabled}
                              value={draft.scheduleIntervalSeconds}
                              onChange={(value) => updateProfile(adapterID, (current) => ({ ...current, scheduleIntervalSeconds: value }))}
                            />
                          </div>
                        </Space>
                      </Card>

                      <Card
                        size="small"
                        type="inner"
                        title="Устройства"
                        extra={(
                          <Button size="small" onClick={() => addDevice(adapterID)}>
                            Добавить устройство
                          </Button>
                        )}
                      >
                        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                          {draft.devices.map((device, deviceIndex) => (
                            <Card
                              key={`${adapterID}-device-${deviceIndex}`}
                              size="small"
                              type="inner"
                              title={`Устройство #${deviceIndex + 1}`}
                              extra={(
                                <Button
                                  size="small"
                                  danger
                                  disabled={draft.devices.length === 1}
                                  onClick={() => removeDevice(adapterID, deviceIndex)}
                                >
                                  Удалить
                                </Button>
                              )}
                            >
                              <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                                <Space wrap size="middle" style={{ width: '100%' }}>
                                  <div style={{ minWidth: 220 }}>
                                    <Text type="secondary">Тип подключения</Text>
                                    <Select
                                      style={{ width: '100%', marginTop: 6 }}
                                      value={device.connectionType}
                                      options={connectionTypeOptions}
                                      onChange={(value) => updateDevice(adapterID, deviceIndex, (current) => ({
                                        ...current,
                                        connectionType: value,
                                      }))}
                                    />
                                  </div>
                                  <div style={{ minWidth: 220 }}>
                                    <Text type="secondary">Метка</Text>
                                    <Input
                                      style={{ marginTop: 6 }}
                                      value={device.label}
                                      onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                        ...current,
                                        label: event.target.value,
                                      }))}
                                      placeholder="Например, основной ФР"
                                    />
                                  </div>
                                  <div style={{ minWidth: 220 }}>
                                    <Text type="secondary">Модель</Text>
                                    <Input
                                      style={{ marginTop: 6 }}
                                      value={device.model}
                                      onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                        ...current,
                                        model: event.target.value,
                                      }))}
                                      placeholder="АТОЛ 22Ф / Mitsu / Штрих"
                                    />
                                  </div>
                                </Space>

                                {device.connectionType === 'tcp' ? (
                                  <Space wrap size="middle" style={{ width: '100%' }}>
                                    <div style={{ minWidth: 220 }}>
                                      <Text type="secondary">IP / host</Text>
                                      <Input
                                        style={{ marginTop: 6 }}
                                        value={device.ip}
                                        onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                          ...current,
                                          ip: event.target.value,
                                        }))}
                                        placeholder="10.25.1.22"
                                      />
                                    </div>
                                    <div style={{ minWidth: 180 }}>
                                      <Text type="secondary">Port</Text>
                                      <InputNumber
                                        style={{ width: '100%', marginTop: 6 }}
                                        min={1}
                                        max={65535}
                                        value={device.port}
                                        onChange={(value) => updateDevice(adapterID, deviceIndex, (current) => ({
                                          ...current,
                                          port: value,
                                        }))}
                                      />
                                    </div>
                                  </Space>
                                ) : (
                                  <Space wrap size="middle" style={{ width: '100%' }}>
                                    <div style={{ minWidth: 220 }}>
                                      <Text type="secondary">COM-порт</Text>
                                      <Select
                                        allowClear
                                        showSearch
                                        style={{ width: '100%', marginTop: 6 }}
                                        value={device.comPort || undefined}
                                        options={comPortOptions}
                                        onChange={(value) => updateDevice(adapterID, deviceIndex, (current) => ({
                                          ...current,
                                          comPort: value || '',
                                        }))}
                                        placeholder={comPortOptions.length > 0 ? 'Выберите COM-порт из inventory' : 'Например, COM7'}
                                        optionFilterProp="label"
                                      />
                                    </div>
                                    <div style={{ minWidth: 180 }}>
                                      <Text type="secondary">Baudrate</Text>
                                      <Input
                                        style={{ marginTop: 6 }}
                                        value={device.baudrate}
                                        onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                          ...current,
                                          baudrate: event.target.value,
                                        }))}
                                        placeholder="115200"
                                      />
                                    </div>
                                  </Space>
                                )}

                                <div>
                                  <Text type="secondary">driver_hints JSON</Text>
                                  <TextArea
                                    style={{ marginTop: 6 }}
                                    autoSize={{ minRows: 3, maxRows: 8 }}
                                    value={device.driverHintsText}
                                    onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                      ...current,
                                      driverHintsText: event.target.value,
                                    }))}
                                    placeholder='{"branch":"10.9+"}'
                                  />
                                </div>

                                <div>
                                  <Text type="secondary">extra_params JSON</Text>
                                  <TextArea
                                    style={{ marginTop: 6 }}
                                    autoSize={{ minRows: 3, maxRows: 8 }}
                                    value={device.extraParamsText}
                                    onChange={(event) => updateDevice(adapterID, deviceIndex, (current) => ({
                                      ...current,
                                      extraParamsText: event.target.value,
                                    }))}
                                    placeholder='{"protocol":"mitsu"}'
                                  />
                                </div>
                              </Space>
                            </Card>
                          ))}
                        </Space>
                      </Card>
                    </Space>
                  </Card>
                );
              })}
            </Space>
          )}
        </Card>

        <Card size="small" type="inner" title="Что уйдёт агенту на следующем heartbeat">
          {effectiveManifests.length === 0 ? (
            <Text type="secondary">После сохранения пустого выбора сервер не будет отдавать adapter_manifests.</Text>
          ) : (
            <Space wrap>
              {effectiveManifests.map((manifest) => (
                <Tag key={`${manifest.adapter_id || 'adapter'}-${manifest.version || 'na'}`} color="processing">
                  {[manifest.adapter_id, manifest.version].filter(Boolean).join(' • ')}
                </Tag>
              ))}
            </Space>
          )}
        </Card>

        <Button
          type="primary"
          loading={saveSelectionPending}
          disabled={!hasUnsavedChanges}
          onClick={saveSelection}
        >
          Сохранить конфигурацию адаптеров
        </Button>
      </Space>
    </Card>
  );
};

export default AgentOperatorFlowCard;
