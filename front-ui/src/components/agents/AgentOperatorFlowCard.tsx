import React, { useEffect, useMemo, useState } from 'react';
import { Alert, Button, Card, Checkbox, Empty, Space, Tag, Typography } from 'antd';
import {
  AgentOperatorFlowDTO,
  PublishedAgentAdapterOptionDTO,
  SaveAgentAdapterSelectionPayload,
} from '@/types/api';

const { Paragraph, Text } = Typography;

type AgentOperatorFlowCardProps = {
  operatorFlow?: AgentOperatorFlowDTO | null;
  saveSelectionPending: boolean;
  saveSelectionError?: string;
  onSaveSelection: (payload: SaveAgentAdapterSelectionPayload) => void;
};

const normalizeAdapterIDs = (values?: string[] | null) => (
  Array.from(new Set((values || []).map((value) => value.trim()).filter(Boolean))).sort()
);

const selectionSignature = (values?: string[] | null) => normalizeAdapterIDs(values).join('|');

const renderAdapterMeta = (adapter: PublishedAgentAdapterOptionDTO) => (
  [
    adapter.adapter_id,
    adapter.adapter_type,
    [adapter.target_os, adapter.target_arch].filter(Boolean).join('/'),
  ]
    .filter(Boolean)
    .join(' • ')
);

const AgentOperatorFlowCard: React.FC<AgentOperatorFlowCardProps> = ({
  operatorFlow,
  saveSelectionPending,
  saveSelectionError,
  onSaveSelection,
}) => {
  const availableAdapters = operatorFlow?.available_adapters || [];
  const warnings = operatorFlow?.warnings || [];
  const recommendedAdapterIDs = operatorFlow?.recommended_adapter_ids || [];
  const effectiveManifests = operatorFlow?.effective_adapter_manifests || [];

  const [selectedAdapterIDs, setSelectedAdapterIDs] = useState<string[]>([]);

  useEffect(() => {
    setSelectedAdapterIDs(normalizeAdapterIDs(operatorFlow?.selected_adapter_ids));
  }, [operatorFlow]);

  const selectionChanged = useMemo(
    () => selectionSignature(selectedAdapterIDs) !== selectionSignature(operatorFlow?.selected_adapter_ids),
    [operatorFlow?.selected_adapter_ids, selectedAdapterIDs],
  );

  const toggleAdapter = (adapterID: string, checked: boolean) => {
    setSelectedAdapterIDs((current) => {
      if (checked) {
        return normalizeAdapterIDs([...current, adapterID]);
      }
      return current.filter((value) => value !== adapterID);
    });
  };

  const saveSelection = () => {
    onSaveSelection({
      selected_adapter_ids: normalizeAdapterIDs(selectedAdapterIDs),
    });
  };

  return (
    <Card className="glass-panel" title="Доступные адаптеры" size="small">
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {warnings.map((warning, index) => (
          <Alert key={`${warning}-${index}`} type="warning" showIcon message={warning} />
        ))}

        {saveSelectionError ? (
          <Alert
            type="error"
            showIcon
            message="Не удалось сохранить набор адаптеров"
            description={saveSelectionError}
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
                      {adapter.version ? <Tag>{adapter.version}</Tag> : null}
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
          disabled={!selectionChanged}
          onClick={saveSelection}
        >
          Сохранить
        </Button>
      </Space>
    </Card>
  );
};

export default AgentOperatorFlowCard;
