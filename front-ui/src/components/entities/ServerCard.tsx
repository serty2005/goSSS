import React from 'react';
import { Card, Badge, Button, Space, Typography, Tooltip, message, Tag, theme as antTheme } from 'antd';
import { CopyOutlined, LinkOutlined, SyncOutlined, GlobalOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ServerEntity } from '@/types/api';
import { equipmentApi } from '@/api/equipment';
import { getEntityIcon } from '@/utils/mappers';
import { cleanWebUrl, formatServerEdition, formatDate } from '@/utils/formatters';

interface Props {
  data: ServerEntity;
}

const { Text, Paragraph } = Typography;

const getPollBadge = (status?: string) => {
  const normalized = String(status || '').toLowerCase();
  if (normalized === 'active') {
    return { status: 'success' as const, text: 'Опрос: онлайн' };
  }
  if (normalized === 'offline') {
    return { status: 'error' as const, text: 'Опрос: офлайн' };
  }
  return { status: 'default' as const, text: 'Опрос: неизвестно' };
};

const ServerCard: React.FC<Props> = ({ data }) => {
  const { token } = antTheme.useToken();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const isCloud = (data.ip || '').toLowerCase().includes('iikoweb') ||
    (data.ip || '').toLowerCase().includes('syrve');

  const pollBadge = getPollBadge(data.operational_status);

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(data.uuid),
    onSuccess: () => {
      message.success('Запрос на опрос отправлен');
      queryClient.invalidateQueries({
        predicate: (query) => query.queryKey[0] === 'company' || query.queryKey[0] === 'search',
      });
    },
    onError: () => message.error('Не удалось выполнить опрос'),
  });

  const handlePoll = (e: React.MouseEvent) => {
    e.stopPropagation();
    pollMutation.mutate();
  };

  const handleCardClick = () => {
    navigate(`/servers/${data.uuid}`);
  };

  const handleCopyIp = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!data.ip) return;
    navigator.clipboard?.writeText(data.ip);
    message.success('IP скопирован');
  };

  if (isCloud) {
    const webUrl = data.ip ? cleanWebUrl(data.ip) : '';
    const fullUrl = `https://${webUrl}`;

    return (
      <Card
        size="small"
        className="glass-panel"
        hoverable
        onClick={handleCardClick}
        title={(
          <Space>
            <GlobalOutlined style={{ color: token.colorPrimary }} />
            <Text strong>{data.device_name || 'Cloud Server'}</Text>
          </Space>
        )}
        extra={<Badge status={pollBadge.status} text={pollBadge.text} />}
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <Paragraph copyable={{ text: webUrl }} style={{ margin: 0, maxWidth: 140 }} ellipsis>
              {webUrl}
            </Paragraph>
            <Space size={4}>
              <Button
                size="small"
                type="primary"
                ghost
                href={fullUrl}
                target="_blank"
                onClick={(e) => e.stopPropagation()}
                icon={<LinkOutlined />}
              >
                iikoWeb
              </Button>
              {data.partners_link && (
                <Button
                  size="small"
                  type="link"
                  href={data.partners_link}
                  target="_blank"
                  onClick={(e) => e.stopPropagation()}
                  icon={<LinkOutlined />}
                >
                  Партнёрский портал
                </Button>
              )}
            </Space>
          </div>

          {data.ip && (
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <Text type="secondary">IP:</Text>
              <Paragraph copyable={{ text: data.ip }} style={{ margin: 0 }}>{data.ip}</Paragraph>
            </div>
          )}

          <div style={{ display: 'flex', justifyContent: 'space-between' }}>
            <Text type="secondary">Версия:</Text>
            <Text strong>
              {data.server_version || '-'}
              <Tag style={{ marginLeft: 4, marginRight: 0 }}>{formatServerEdition(data.server_edition) || 'Web'}</Tag>
            </Text>
          </div>

          {data.unique_id && (
            <div style={{ display: 'flex', justifyContent: 'space-between' }}>
              <Text type="secondary">UID:</Text>
              <Paragraph copyable={{ text: data.unique_id }} style={{ margin: 0 }}>
                {data.unique_id.substring(0, 15)}...
              </Paragraph>
            </div>
          )}

          <div style={{ marginTop: 8, display: 'flex', justifyContent: 'flex-end' }}>
            <Button
              size="small"
              icon={<SyncOutlined spin={pollMutation.isPending} />}
              onClick={handlePoll}
              loading={pollMutation.isPending}
            >
              {pollMutation.isPending ? 'Опрос...' : formatDate(data.last_polled_at)}
            </Button>
          </div>
        </Space>
      </Card>
    );
  }

  const renderAccessLink = (label: string, value?: string) => {
    if (!value) return null;
    return (
      <Tooltip title={`Скопировать ID/Link для ${label}`}>
        <Paragraph copyable={{ text: value }} style={{ margin: 0 }}>
          <Text type="secondary">{label}:</Text> {value}
        </Paragraph>
      </Tooltip>
    );
  };

  return (
    <Card
      size="small"
      className="glass-panel"
      hoverable
      onClick={handleCardClick}
      title={(
        <Space>
          {getEntityIcon('Server')}
          <Text strong>{data.device_name || data.server_name || 'Сервер'}</Text>
        </Space>
      )}
      extra={<Badge status={pollBadge.status} text={pollBadge.text} />}
      actions={[
        <Button
          key="poll"
          type="text"
          size="small"
          icon={<SyncOutlined spin={pollMutation.isPending} />}
          onClick={handlePoll}
          style={{ width: '100%' }}
        >
          {pollMutation.isPending ? 'Опрос...' : 'Обновить статус'}
        </Button>,
      ]}
    >
      <div style={{ marginBottom: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
          <Text type="secondary">IP:</Text>
          {data.ip ? (
            <Space size={6}>
              <Paragraph copyable={{ text: data.ip }} style={{ margin: 0 }}>
                <Text strong>{data.ip}</Text>
              </Paragraph>
              <Button size="small" type="text" icon={<CopyOutlined />} onClick={handleCopyIp} />
            </Space>
          ) : (
            <Text type="secondary">Нет IP</Text>
          )}
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
          <Text type="secondary">Версия:</Text>
          <Text>{data.server_version} {data.server_edition ? `(${formatServerEdition(data.server_edition)})` : ''}</Text>
        </div>
        {data.partners_link && (
          <div style={{ marginTop: 4, textAlign: 'right' }}>
            <Button
              type="link"
              size="small"
              href={data.partners_link}
              target="_blank"
              onClick={(e) => e.stopPropagation()}
              icon={<LinkOutlined />}
              style={{ paddingRight: 0 }}
            >
              Партнёрский портал
            </Button>
          </div>
        )}
      </div>

      <div style={{ borderTop: '1px solid var(--app-color-divider)', paddingTop: 8 }}>
        <Space direction="vertical" size={0} style={{ width: '100%' }}>
          {renderAccessLink('AnyDesk', data.anydesk)}
          {renderAccessLink('TeamViewer', data.teamviewer)}
          {renderAccessLink('RDP', data.rdp)}
          {renderAccessLink('LM', data.litemanager)}

          {!data.anydesk && !data.teamviewer && !data.rdp && !data.litemanager && (
            <Text type="secondary" italic>Нет данных для доступа</Text>
          )}
        </Space>
      </div>
    </Card>
  );
};

export default ServerCard;
