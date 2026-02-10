import React, { useEffect, useState } from 'react';
import { Button, Input, Space, Typography } from 'antd';
import { CheckOutlined, CloseOutlined, EditOutlined } from '@ant-design/icons';

type Props = {
  value?: string | null;
  placeholder?: string;
  multiline?: boolean;
  saving?: boolean;
  onSave: (nextValue: string) => void;
};

const { Text } = Typography;

const InlineFieldEditor: React.FC<Props> = ({
  value,
  placeholder = '-',
  multiline = false,
  saving = false,
  onSave,
}) => {
  const [isEditing, setIsEditing] = useState(false);
  const [draft, setDraft] = useState(value || '');

  useEffect(() => {
    if (!isEditing) {
      setDraft(value || '');
    }
  }, [value, isEditing]);

  if (!isEditing) {
    return (
      <Space size={4} align="start">
        <Text>{(value || '').trim() || placeholder}</Text>
        <Button
          type="text"
          size="small"
          icon={<EditOutlined />}
          onClick={() => setIsEditing(true)}
        />
      </Space>
    );
  }

  return (
    <Space size={4} align="start">
      {multiline ? (
        <Input.TextArea
          rows={3}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          style={{ minWidth: 260 }}
        />
      ) : (
        <Input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          style={{ minWidth: 220 }}
        />
      )}
      <Button
        type="text"
        size="small"
        icon={<CheckOutlined />}
        loading={saving}
        onClick={() => {
          onSave(draft);
          setIsEditing(false);
        }}
      />
      <Button
        type="text"
        size="small"
        icon={<CloseOutlined />}
        onClick={() => {
          setDraft(value || '');
          setIsEditing(false);
        }}
      />
    </Space>
  );
};

export default InlineFieldEditor;
