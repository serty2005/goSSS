import React, { useMemo, useState } from 'react';
import { Card, Empty, Input, Space, Tag, Typography } from 'antd';
import dayjs from 'dayjs';
import { CandidateFiscalStagingDTO } from '@/types/api';
import { CandidateWorkstationDraft } from '@/components/candidates/StagedWorkstations';

interface StagedAgentEntitiesProps {
  workstations: CandidateWorkstationDraft[];
  fiscals: CandidateFiscalStagingDTO[];
  observationAgents: Record<number, string>;
  onWorkstationNameChange: (mergeKey: string, nextName: string) => void;
  onGroupClick: (params: { agentID: string; observationIDs: number[]; unresolvedServer: boolean }) => void;
}

interface AgentGroup {
  key: string;
  agentID: string;
  agentIDs: string[];
  workstations: CandidateWorkstationDraft[];
  fiscals: CandidateFiscalStagingDTO[];
  observationIDs: number[];
}

const { Text } = Typography;

const NO_AGENT_ID = 'нет данных';

const pickFiscalIdentity = (item: CandidateFiscalStagingDTO): string => {
  const normalized = String(item.serial_normalized || '').trim().toLowerCase();
  if (normalized) return `sn:${normalized}`;
  const serial = String(item.serial_number || '').trim().toLowerCase();
  if (serial) return `sn:${serial}`;
  return `id:${item.id}`;
};

const collectObservationIDs = (ws: CandidateWorkstationDraft): number[] => {
  return (ws.observation_ids || (ws.observation_id ? [ws.observation_id] : []))
    .filter((id): id is number => typeof id === 'number' && id > 0);
};

const pickLastObservedAt = (group: AgentGroup): string => {
  return (
    [...group.workstations.map((item) => item.observed_at), ...group.fiscals.map((item) => item.observed_at)]
      .filter(Boolean)
      .sort((a, b) => dayjs(b).valueOf() - dayjs(a).valueOf())[0] || ''
  );
};

export const StagedAgentEntities: React.FC<StagedAgentEntitiesProps> = ({
  workstations,
  fiscals,
  observationAgents,
  onWorkstationNameChange,
  onGroupClick,
}) => {
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');

  const groups = useMemo(() => {
    const map = new Map<string, AgentGroup>();
    const observationToWorkstation = new Map<number, string>();
    const fiscalIdentityByGroup = new Map<string, Set<string>>();
    const agentIDsByGroup = new Map<string, Set<string>>();

    const ensure = (groupKey: string): AgentGroup => {
      const existing = map.get(groupKey);
      if (existing) return existing;
      const created: AgentGroup = {
        key: groupKey,
        agentID: NO_AGENT_ID,
        agentIDs: [],
        workstations: [],
        fiscals: [],
        observationIDs: [],
      };
      map.set(groupKey, created);
      fiscalIdentityByGroup.set(groupKey, new Set<string>());
      agentIDsByGroup.set(groupKey, new Set<string>());
      return created;
    };

    const addObservationID = (group: AgentGroup, observationID?: number) => {
      if (!observationID || observationID <= 0) return;
      if (!group.observationIDs.includes(observationID)) {
        group.observationIDs.push(observationID);
      }
    };

    const addAgentFromObservation = (groupKey: string, observationID: number) => {
      const groupAgents = agentIDsByGroup.get(groupKey);
      if (!groupAgents) return;
      const agentID = String(observationAgents[observationID] || '').trim() || NO_AGENT_ID;
      groupAgents.add(agentID);
    };

    workstations.forEach((ws) => {
      const groupKey = `ws:${ws.merge_key}`;
      const group = ensure(groupKey);

      if (!group.workstations.some((item) => item.merge_key === ws.merge_key)) {
        group.workstations.push(ws);
      }

      collectObservationIDs(ws).forEach((observationID) => {
        observationToWorkstation.set(observationID, ws.merge_key);
        addObservationID(group, observationID);
        addAgentFromObservation(groupKey, observationID);
      });

      const wsAgentID = String(ws.agent_uuid || '').trim();
      if (wsAgentID) {
        const groupAgents = agentIDsByGroup.get(groupKey);
        if (groupAgents) {
          groupAgents.add(wsAgentID);
        }
      }
    });

    fiscals.forEach((fr) => {
      const fiscalIdentity = pickFiscalIdentity(fr);
      const workstationKey = fr.observation_id ? observationToWorkstation.get(fr.observation_id) : undefined;
      const groupKey = workstationKey ? `ws:${workstationKey}` : `fr:${fiscalIdentity}`;
      const group = ensure(groupKey);

      const existingFiscalKeys = fiscalIdentityByGroup.get(groupKey);
      if (existingFiscalKeys && !existingFiscalKeys.has(fiscalIdentity)) {
        existingFiscalKeys.add(fiscalIdentity);
        group.fiscals.push(fr);
      }

      addObservationID(group, fr.observation_id);
      if (fr.observation_id) {
        addAgentFromObservation(groupKey, fr.observation_id);
      }
    });

    return Array.from(map.values())
      .map((group) => {
        const agents = Array.from(agentIDsByGroup.get(group.key) || []);
        const realAgents = agents.filter((item) => item !== NO_AGENT_ID);
        const primaryAgent = realAgents[0] || NO_AGENT_ID;
        const normalizedAgents = realAgents.length > 0 ? realAgents : [NO_AGENT_ID];
        return {
          ...group,
          agentID: primaryAgent,
          agentIDs: normalizedAgents,
          observationIDs: [...group.observationIDs].sort((a, b) => a - b),
        };
      })
      .sort((left, right) => dayjs(pickLastObservedAt(right)).valueOf() - dayjs(pickLastObservedAt(left)).valueOf());
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
        {groups.map((group) => {
          const lastObservedAt = pickLastObservedAt(group);
          const hasUnknownAgent = group.agentIDs.includes(NO_AGENT_ID);
          return (
            <Card
              key={group.key}
              size="small"
              hoverable
              bodyStyle={{ padding: 10 }}
              onClick={() => onGroupClick({
                agentID: group.agentID,
                observationIDs: group.observationIDs,
                unresolvedServer: hasUnknownAgent,
              })}
            >
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                {group.agentIDs.length > 1 ? (
                  <Text strong>Агенты: {group.agentIDs.join(', ')}</Text>
                ) : (
                  <Text strong>{group.agentID === NO_AGENT_ID ? 'Агент без UUID' : group.agentID}</Text>
                )}
                {hasUnknownAgent ? <Tag color="orange">Есть сообщения без UUID</Tag> : null}

                {group.workstations.map((ws) => {
                  const isEditing = editingKey === ws.merge_key;
                  return (
                    <Space key={ws.merge_key} direction="vertical" size={0} style={{ width: '100%' }}>
                      {isEditing ? (
                        <Input
                          autoFocus
                          size="small"
                          value={draftName}
                          onClick={(event) => event.stopPropagation()}
                          onChange={(event) => setDraftName(event.target.value)}
                          onBlur={saveName}
                          onPressEnter={saveName}
                        />
                      ) : (
                        <Text strong style={{ cursor: 'text' }} onClick={(event) => {
                          event.stopPropagation();
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
                  <Space key={pickFiscalIdentity(fr)} direction="vertical" size={0}>
                    <Text strong>{fr.serial_number || fr.serial_normalized || `ФР #${fr.id}`}</Text>
                    <Text type="secondary">РН ККТ: {fr.rn_kkt || '-'}</Text>
                    <Text type="secondary">Модель: {fr.model_name || '-'}</Text>
                    <Text type="secondary">ИНН: {fr.inn || '-'}</Text>
                  </Space>
                ))}

                <Text type="secondary">
                  Последнее наблюдение: {dayjs(lastObservedAt).isValid() ? dayjs(lastObservedAt).format('DD.MM.YYYY HH:mm:ss') : '-'}
                </Text>
              </Space>
            </Card>
          );
        })}
      </Space>
    </Card>
  );
};
