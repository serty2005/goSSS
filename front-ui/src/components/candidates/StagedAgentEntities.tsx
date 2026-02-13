import React, { useMemo, useState } from 'react';
import { Card, Empty, Input, Space, Typography } from 'antd';
import dayjs from 'dayjs';
import { CandidateFiscalStagingDTO } from '@/types/api';
import { CandidateWorkstationDraft } from '@/components/candidates/StagedWorkstations';

interface StagedAgentEntitiesProps {
  workstations: CandidateWorkstationDraft[];
  fiscals: CandidateFiscalStagingDTO[];
  observationAgents: Record<number, string[]>;
  onWorkstationNameChange: (mergeKey: string, nextName: string) => void;
}

interface AgentGroup {
  agentID: string;
  workstations: CandidateWorkstationDraft[];
  fiscals: CandidateFiscalStagingDTO[];
}

const { Text } = Typography;

const NO_AGENT_ID = 'нет данных';

export const StagedAgentEntities: React.FC<StagedAgentEntitiesProps> = ({
  workstations,
  fiscals,
  observationAgents,
  onWorkstationNameChange,
}) => {
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');

  const groups = useMemo(() => {
    const map = new Map<string, AgentGroup>();

    const ensure = (agentID: string) => {
      if (!map.has(agentID)) {
        map.set(agentID, { agentID, workstations: [], fiscals: [] });
      }
      return map.get(agentID)!;
    };

    workstations.forEach((ws) => {
      const agentIDs = ws.agent_uuids && ws.agent_uuids.length > 0 ? ws.agent_uuids : [NO_AGENT_ID];
      agentIDs.forEach((agentID) => {
        const group = ensure(agentID || NO_AGENT_ID);
        if (!group.workstations.some((item) => item.merge_key === ws.merge_key)) {
          group.workstations.push(ws);
        }
      });
    });

    fiscals.forEach((fr) => {
      const byObservation = observationAgents[fr.observation_id] || [];
      const agentIDs = byObservation.length > 0 ? byObservation : [NO_AGENT_ID];
      agentIDs.forEach((agentID) => {
        const group = ensure(agentID || NO_AGENT_ID);
        if (!group.fiscals.some((item) => item.id === fr.id)) {
          group.fiscals.push(fr);
        }
      });
    });

    return Array.from(map.values());
  }, [fiscals, observationAgents, workstations]);

  const saveName = () => {
    if (!editingKey) return;
    const value = draftName.trim();
    if (value) {
      onWorkstationNameChange(editingKey, value);
    }
    setEditingKey(null);
  };

  if (groups.length === 0) {
    return (
      <Card size="small" title="Сущности агентов">
        <Empty description="Данные не найдены" />
      </Card>
    );
  }

  return (
    <Card size="small" title="Сущности агентов">
      <Space direction="vertical" size={10} style={{ width: '100%' }}>
        {groups.map((group) => (
          <Card key={group.agentID} size="small" bodyStyle={{ padding: 10 }}>
            <Space direction="vertical" size={6} style={{ width: '100%' }}>
              <Text strong>{group.agentID}</Text>

              {group.workstations.map((ws) => {
                const isEditing = editingKey === ws.merge_key;
                return (
                  <Space key={ws.merge_key} direction="vertical" size={0} style={{ width: '100%' }}>
                    {isEditing ? (
                      <Input
                        autoFocus
                        size="small"
                        value={draftName}
                        onChange={(event) => setDraftName(event.target.value)}
                        onBlur={saveName}
                        onPressEnter={saveName}
                      />
                    ) : (
                      <Text strong style={{ cursor: 'text' }} onClick={() => {
                        setEditingKey(ws.merge_key);
                        setDraftName(ws.name || ws.hostname || '');
                      }}
                      >
                        {ws.name || ws.hostname || 'Станция без имени'}
                      </Text>
                    )}
                    {ws.teamviewer_id ? <Text type="secondary">TeamViewer: {ws.teamviewer_id}</Text> : null}
                    {ws.litemanager_id ? <Text type="secondary">LiteManager: {ws.litemanager_id}</Text> : null}
                    {ws.anydesk_id ? <Text type="secondary">AnyDesk: {ws.anydesk_id}</Text> : null}
                  </Space>
                );
              })}

              {group.fiscals.map((fr) => (
                <Space key={fr.id} direction="vertical" size={0}>
                  <Text strong>{fr.serial_number || fr.serial_normalized || `ФР #${fr.id}`}</Text>
                  <Text type="secondary">РН ККТ: {fr.rn_kkt || '-'}</Text>
                  <Text type="secondary">Модель: {fr.model_name || '-'}</Text>
                  <Text type="secondary">ИНН: {fr.inn || '-'}</Text>
                </Space>
              ))}

              <Text type="secondary">
                Последнее наблюдение:{' '}
                {dayjs(
                  [...group.workstations.map((item) => item.observed_at), ...group.fiscals.map((item) => item.observed_at)]
                    .filter(Boolean)
                    .sort((a, b) => dayjs(b).valueOf() - dayjs(a).valueOf())[0],
                ).isValid()
                  ? dayjs(
                    [...group.workstations.map((item) => item.observed_at), ...group.fiscals.map((item) => item.observed_at)]
                      .filter(Boolean)
                      .sort((a, b) => dayjs(b).valueOf() - dayjs(a).valueOf())[0],
                  ).format('DD.MM.YYYY HH:mm:ss')
                  : '-'}
              </Text>
            </Space>
          </Card>
        ))}
      </Space>
    </Card>
  );
};
