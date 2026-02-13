import React, { useState } from 'react';
import { Card, Empty, Input, List, Space, Typography } from 'antd';
import dayjs from 'dayjs';

export interface CandidateWorkstationDraft {
  merge_key: string;
  staging_id?: number;
  observation_id?: number;
  workstation_uuid?: string;
  hostname?: string;
  name?: string;
  teamviewer_id?: string;
  litemanager_id?: string;
  anydesk_id?: string;
  agent_uuids?: string[];
  observed_at?: string;
}

interface StagedWorkstationsProps {
  workstations: CandidateWorkstationDraft[];
  onNameChange: (mergeKey: string, nextName: string) => void;
}

const { Text } = Typography;

export const StagedWorkstations: React.FC<StagedWorkstationsProps> = ({ workstations, onNameChange }) => {
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');

  const saveEdit = () => {
    if (!editingKey) return;
    const next = draftName.trim();
    if (next) {
      onNameChange(editingKey, next);
    }
    setEditingKey(null);
  };

  return (
    <Card title={`Станции из наблюдений (${workstations.length})`} size="small">
      {workstations.length === 0 ? (
        <Empty description="Нет staged станций" />
      ) : (
        <List
          dataSource={workstations}
          renderItem={(ws) => {
            const isEditing = editingKey === ws.merge_key;
            return (
              <List.Item key={ws.merge_key}>
                <Space direction="vertical" size={2} style={{ width: '100%' }}>
                  {isEditing ? (
                    <Input
                      autoFocus
                      value={draftName}
                      onChange={(event) => setDraftName(event.target.value)}
                      onBlur={saveEdit}
                      onPressEnter={saveEdit}
                      size="small"
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
                  <Text type="secondary">
                    Последнее наблюдение: {ws.observed_at ? dayjs(ws.observed_at).format('DD.MM.YYYY HH:mm:ss') : '-'}
                  </Text>
                </Space>
              </List.Item>
            );
          }}
        />
      )}
    </Card>
  );
};
