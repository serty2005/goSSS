import React, { useState } from 'react';
import { Card, Space, Typography, Tooltip, Tag, Button, Modal, Form, Input, message } from 'antd';
import { EditOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import dayjs from 'dayjs';
import { WorkstationEntity } from '@/types/api';
import { getEntityIcon } from '@/utils/mappers';
import { equipmentApi } from '@/api/equipment';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';

interface Props {
  data: WorkstationEntity;
}

const { Text, Paragraph } = Typography;

const WorkstationCard: React.FC<Props> = ({ data }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [isRenameOpen, setIsRenameOpen] = useState(false);
  const [name, setName] = useState((data.device_name || '').trim());

  const agentUpdate = getAgentUpdateMeta(data);

  const renameMutation = useMutation({
    mutationFn: (nextName: string) => equipmentApi.updateWorkstation(data.uuid, { device_name: nextName }),
    onSuccess: () => {
      message.success('Имя станции обновлено');
      setIsRenameOpen(false);
      queryClient.invalidateQueries({ queryKey: ['company'] });
      queryClient.invalidateQueries({ queryKey: ['search'] });
      queryClient.invalidateQueries({ queryKey: ['equipment', 'workstations'] });
      queryClient.invalidateQueries({ queryKey: ['workstation', data.uuid] });
    },
    onError: () => message.error('Не удалось обновить имя станции'),
  });

  const handleCardClick = () => {
    navigate(`/workstations/${data.uuid}`);
  };

  const openRename = (e: React.MouseEvent<HTMLElement>) => {
    e.stopPropagation();
    setName((data.device_name || '').trim());
    setIsRenameOpen(true);
  };

  const submitRename = () => {
    const trimmed = name.trim();
    if (!trimmed) {
      message.warning('Введите имя станции');
      return;
    }
    renameMutation.mutate(trimmed);
  };

  return (
    <>
      <Card
        size="small"
        className="glass-panel"
        hoverable
        onClick={handleCardClick}
        style={data.is_new ? { borderColor: '#91caff', boxShadow: '0 0 0 1px rgba(24, 144, 255, 0.25)' } : undefined}
        title={(
          <Space>
            {getEntityIcon('Workstation')}
            <Text strong>{data.device_name || 'Рабочая станция'}</Text>
            {data.is_new && <Tag color="blue">Новая</Tag>}
          </Space>
        )}
        extra={(
          <Space size={8}>
            {agentUpdate && (
              <Tooltip
                title={agentUpdate.updatedAt
                  ? `Агент ${agentUpdate.updater}, ${dayjs(agentUpdate.updatedAt).format('DD.MM.YYYY HH:mm:ss')}`
                  : `Агент ${agentUpdate.updater}`}
              >
                <Tag color="blue" style={{ fontSize: 12, lineHeight: '22px', paddingInline: 10, marginRight: 0 }}>
                  Агент
                </Tag>
              </Tooltip>
            )}
            {data.is_new && (
              <Tooltip title="Переименовать станцию">
                <Button type="text" size="small" icon={<EditOutlined />} onClick={openRename} />
              </Tooltip>
            )}
          </Space>
        )}
      >
        <Space direction="vertical" size={2} style={{ width: '100%' }}>
          {data.description && (
            <Paragraph ellipsis={{ rows: 2, expandable: false }} type="secondary" style={{ fontSize: 12, marginBottom: 8 }}>
              {data.description}
            </Paragraph>
          )}

          {data.anydesk && (
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <Text>AnyDesk:</Text>
              <Paragraph copyable={{ text: String(data.anydesk) }} style={{ margin: 0 }}>{String(data.anydesk)}</Paragraph>
            </div>
          )}

          {data.teamviewer && (
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <Text>TV:</Text>
              <Paragraph copyable={{ text: String(data.teamviewer) }} style={{ margin: 0 }}>{String(data.teamviewer)}</Paragraph>
            </div>
          )}

          {data.litemanager && (
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <Text>LM:</Text>
              <Paragraph copyable={{ text: String(data.litemanager) }} style={{ margin: 0 }}>{String(data.litemanager)}</Paragraph>
            </div>
          )}

          {!data.anydesk && !data.teamviewer && !data.litemanager && !data.description && (
            <Text type="secondary" italic>Нет дополнительной информации</Text>
          )}
        </Space>
      </Card>

      <Modal
        title="Переименование рабочей станции"
        open={isRenameOpen}
        onOk={submitRename}
        onCancel={() => setIsRenameOpen(false)}
        okText="Сохранить"
        cancelText="Отмена"
        confirmLoading={renameMutation.isPending}
      >
        <Form layout="vertical">
          <Form.Item label="Имя станции" required>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Введите имя станции" maxLength={200} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default WorkstationCard;
