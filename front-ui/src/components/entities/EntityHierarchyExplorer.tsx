import React, { useEffect, useMemo, useState } from 'react';
import { Card, Empty, Space, Tag, Typography } from 'antd';

const { Text } = Typography;

export type HierarchyFocusType = 'company' | 'server' | 'workstation' | 'fiscal';

export interface HierarchyCompanyNode {
  id: string;
  title: string;
}

export interface HierarchyServerNode {
  id: string;
  title: string;
}

export interface HierarchyWorkstationNode {
  id: string;
  title: string;
  serverID?: string;
}

export interface HierarchyFiscalNode {
  id: string;
  title: string;
  workstationID?: string;
}

interface HierarchyFocus {
  type: HierarchyFocusType;
  id: string;
}

interface EntityHierarchyExplorerProps {
  rootCompany?: HierarchyCompanyNode;
  parentCompany?: HierarchyCompanyNode;
  server: HierarchyServerNode;
  workstations: HierarchyWorkstationNode[];
  fiscals: HierarchyFiscalNode[];
  loading?: boolean;
  initialFocus?: HierarchyFocus;
}

const cardStyle: React.CSSProperties = {
  minWidth: 230,
  cursor: 'pointer',
};

const columnStyle: React.CSSProperties = {
  minWidth: 240,
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
};

const normalizeText = (value?: string) => String(value || '').trim();

const EntityHierarchyExplorer: React.FC<EntityHierarchyExplorerProps> = ({
  rootCompany,
  parentCompany,
  server,
  workstations,
  fiscals,
  loading,
  initialFocus,
}) => {
  const [focus, setFocus] = useState<HierarchyFocus>({
    type: initialFocus?.type || 'server',
    id: initialFocus?.id || server.id,
  });

  useEffect(() => {
    setFocus({
      type: initialFocus?.type || 'server',
      id: initialFocus?.id || server.id,
    });
  }, [initialFocus?.id, initialFocus?.type, server.id]);

  const workstationsByServer = useMemo(() => {
    const map = new Map<string, HierarchyWorkstationNode[]>();
    workstations.forEach((item) => {
      const key = normalizeText(item.serverID);
      if (!key) {
        return;
      }
      const list = map.get(key) || [];
      list.push(item);
      map.set(key, list);
    });
    return map;
  }, [workstations]);

  const fiscalsByWorkstation = useMemo(() => {
    const map = new Map<string, HierarchyFiscalNode[]>();
    fiscals.forEach((item) => {
      const key = normalizeText(item.workstationID);
      if (!key) {
        return;
      }
      const list = map.get(key) || [];
      list.push(item);
      map.set(key, list);
    });
    return map;
  }, [fiscals]);

  const selectedWorkstation = useMemo(() => {
    if (focus.type !== 'workstation' && focus.type !== 'fiscal') {
      return undefined;
    }
    if (focus.type === 'workstation') {
      return workstations.find((item) => item.id === focus.id);
    }
    const selectedFiscal = fiscals.find((item) => item.id === focus.id);
    if (!selectedFiscal?.workstationID) {
      return undefined;
    }
    return workstations.find((item) => item.id === selectedFiscal.workstationID);
  }, [fiscals, focus.id, focus.type, workstations]);

  const selectedFiscal = useMemo(() => {
    if (focus.type !== 'fiscal') {
      return undefined;
    }
    return fiscals.find((item) => item.id === focus.id);
  }, [fiscals, focus.id, focus.type]);

  const serverWorkstations = useMemo(() => {
    return workstationsByServer.get(server.id) || [];
  }, [server.id, workstationsByServer]);

  const focusedWorkstationFiscals = useMemo(() => {
    const wsID = normalizeText(selectedWorkstation?.id);
    if (!wsID) {
      return [];
    }
    return fiscalsByWorkstation.get(wsID) || [];
  }, [fiscalsByWorkstation, selectedWorkstation?.id]);

  const renderNodeCard = (
    nodeType: HierarchyFocusType,
    nodeID: string,
    title: string,
    subtitle: string,
    selected = false,
    subtle = false,
  ) => {
    return (
      <Card
        size="small"
        key={`${nodeType}-${nodeID}`}
        hoverable
        onClick={() => setFocus({ type: nodeType, id: nodeID })}
        style={{
          ...cardStyle,
          borderColor: selected ? '#1677ff' : undefined,
          boxShadow: selected ? '0 0 0 1px rgba(22, 119, 255, 0.25)' : undefined,
          opacity: subtle ? 0.8 : 1,
        }}
      >
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          <Space style={{ justifyContent: 'space-between', width: '100%' }}>
            <Text strong>{title}</Text>
            <Tag color={
              nodeType === 'company' ? 'blue' :
              nodeType === 'server' ? 'geekblue' :
              nodeType === 'workstation' ? 'cyan' :
              'gold'
            } style={{ marginRight: 0 }}>
              {nodeType === 'company' ? 'Компания' : nodeType === 'server' ? 'Сервер' : nodeType === 'workstation' ? 'РС' : 'ФР'}
            </Tag>
          </Space>
          <Text type="secondary" style={{ fontSize: 12 }}>{subtitle}</Text>
        </Space>
      </Card>
    );
  };

  if (loading) {
    return <Text type="secondary">Загрузка иерархии...</Text>;
  }

  const showParent = focus.type === 'company' && Boolean(parentCompany);
  const showServerColumn = focus.type !== 'fiscal';
  const showWorkstationColumn = focus.type === 'server' || focus.type === 'workstation' || focus.type === 'fiscal';
  const showFiscalColumn = focus.type === 'workstation' || focus.type === 'fiscal';

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Text type="secondary">
        Клик по карточке раскрывает следующий уровень связей.
      </Text>

      <div
        style={{
          display: 'flex',
          gap: 12,
          overflowX: 'auto',
          paddingBottom: 8,
          alignItems: 'flex-start',
        }}
      >
        <div style={columnStyle}>
          {showParent && parentCompany ? renderNodeCard('company', parentCompany.id, parentCompany.title, parentCompany.id, false, true) : null}
          {rootCompany ? renderNodeCard('company', rootCompany.id, rootCompany.title, rootCompany.id, focus.type === 'company' && focus.id === rootCompany.id) : null}
        </div>

        {showServerColumn ? (
          <div style={columnStyle}>
            {renderNodeCard('server', server.id, server.title, server.id, focus.type === 'server' && focus.id === server.id)}
          </div>
        ) : (
          <div style={columnStyle}>
            <Card
              size="small"
              style={{
                ...cardStyle,
                borderStyle: 'dashed',
                borderColor: '#d9d9d9',
              }}
            >
              <Text type="secondary">Связь с сервером скрыта на этом уровне</Text>
            </Card>
          </div>
        )}

        {showWorkstationColumn ? (
          <div style={columnStyle}>
            {focus.type === 'server' ? (
              serverWorkstations.length === 0 ? (
                <Card size="small" style={cardStyle}><Text type="secondary">К серверу не привязаны рабочие станции</Text></Card>
              ) : (
                serverWorkstations.map((item) => renderNodeCard(
                  'workstation',
                  item.id,
                  item.title,
                  item.id,
                  focus.type === 'workstation' && focus.id === item.id,
                ))
              )
            ) : selectedWorkstation ? (
              renderNodeCard('workstation', selectedWorkstation.id, selectedWorkstation.title, selectedWorkstation.id, focus.type === 'workstation' || focus.type === 'fiscal')
            ) : (
              <Card size="small" style={cardStyle}><Text type="secondary">Рабочая станция не выбрана</Text></Card>
            )}
          </div>
        ) : null}

        {showFiscalColumn ? (
          <div style={columnStyle}>
            {focus.type === 'fiscal' && selectedFiscal ? (
              renderNodeCard('fiscal', selectedFiscal.id, selectedFiscal.title, selectedFiscal.id, true)
            ) : focusedWorkstationFiscals.length === 0 ? (
              <Card size="small" style={cardStyle}><Text type="secondary">ФР для выбранной рабочей станции не найдены</Text></Card>
            ) : (
              focusedWorkstationFiscals.map((item) => renderNodeCard('fiscal', item.id, item.title, item.id, false))
            )}
          </div>
        ) : null}
      </div>

      {!rootCompany && !server.id ? <Empty description="Недостаточно данных для построения иерархии" /> : null}
    </Space>
  );
};

export default EntityHierarchyExplorer;
