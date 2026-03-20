import React, { useMemo, useState } from 'react';
import { Button, Empty, Space, Typography, message } from 'antd';

const { Text } = Typography;

type Props = {
  title?: React.ReactNode;
  value?: unknown;
  emptyDescription?: string;
  defaultExpanded?: boolean;
  collapsedLines?: number;
  maxHeight?: number;
};

const toPrettyJSON = (value: unknown) => {
  if (value === undefined || value === null) {
    return '';
  }

  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) {
      return '';
    }
    try {
      return JSON.stringify(JSON.parse(trimmed), null, 2);
    } catch {
      return trimmed;
    }
  }

  try {
    return JSON.stringify(value, null, 2) || '';
  } catch {
    return String(value);
  }
};

const JsonDataViewer: React.FC<Props> = ({
  title,
  value,
  emptyDescription = 'JSON отсутствует',
  defaultExpanded = false,
  collapsedLines = 16,
  maxHeight = 520,
}) => {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const payload = useMemo(() => toPrettyJSON(value), [value]);
  const lines = useMemo(() => (payload ? payload.split('\n') : []), [payload]);
  const canCollapse = lines.length > collapsedLines;
  const visiblePayload = expanded || !canCollapse ? payload : `${lines.slice(0, collapsedLines).join('\n')}\n...`;

  if (!payload) {
    return <Empty description={emptyDescription} image={Empty.PRESENTED_IMAGE_SIMPLE} />;
  }

  const copyPayload = async () => {
    try {
      await navigator.clipboard.writeText(payload);
      message.success('JSON скопирован');
    } catch {
      message.error('Не удалось скопировать JSON');
    }
  };

  return (
    <div style={{ display: 'grid', gap: 10 }}>
      <Space wrap style={{ justifyContent: 'space-between', width: '100%' }}>
        <Text strong>{title}</Text>
        <Space size={4}>
          <Button size="small" onClick={() => void copyPayload()}>
            Копировать JSON
          </Button>
          {canCollapse ? (
            <Button size="small" type="text" onClick={() => setExpanded((prev) => !prev)}>
              {expanded ? 'Свернуть' : 'Развернуть'}
            </Button>
          ) : null}
        </Space>
      </Space>

      <pre
        style={{
          margin: 0,
          maxHeight,
          overflow: 'auto',
          padding: 12,
          borderRadius: 8,
          background: 'rgba(0, 0, 0, 0.04)',
          fontSize: 12,
          lineHeight: 1.45,
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {visiblePayload}
      </pre>
    </div>
  );
};

export default JsonDataViewer;
