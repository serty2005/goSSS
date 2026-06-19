import React from 'react';
import { Card, Button, Space, Typography, Tooltip, message, theme as antTheme } from 'antd';
import { LinkOutlined, SyncOutlined, GlobalOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { ServerEntity } from '@/types/api';
import { equipmentApi } from '@/api/equipment';
import ServerLicenseStatusTag from '@/components/entities/ServerLicenseStatusTag';
import { getEntityIcon } from '@/utils/mappers';
import {
  formatServerEdition,
  getIikoWebAppLinkMeta,
  normalizeServerAddress,
} from '@/utils/formatters';
import { getAgentUpdateMeta } from '@/utils/agentUpdates';
import { useAuthStore } from '@/store/authStore';
import { canManageServerActions } from '@/utils/permissions';
import AgentBadge from '@/components/agents/AgentBadge';

interface Props {
  data: ServerEntity;
}

const { Text, Paragraph } = Typography;

const getPollBadge = (status?: string) => {
  const normalized = String(status || '').toLowerCase();
  if (normalized === 'active') {
    return { status: 'success' as const, text: 'Онлайн' };
  }
  if (normalized === 'offline') {
    return { status: 'error' as const, text: 'Офлайн' };
  }
  if (normalized === 'license') {
    return { status: 'warning' as const, text: 'Нужна лицензия' };
  }
  return { status: 'default' as const, text: 'неизвестно' };
};

const ServerCard: React.FC<Props> = ({ data }) => {
  const { token } = antTheme.useToken();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const canRunServerActions = canManageServerActions(user?.roles);

  const rawAddress = data.ip || '';
  const displayAddress = normalizeServerAddress(rawAddress, { dropPort443: true });
  const ipCloudMeta = getIikoWebAppLinkMeta(rawAddress);
  const iikoWebMeta = getIikoWebAppLinkMeta(data.iiko_web_link || rawAddress);
  const isCloud = Boolean(ipCloudMeta);
  const hasAccessData = Boolean(data.anydesk || data.teamviewer || data.rdp || data.litemanager);
  const agentUpdate = getAgentUpdateMeta(data);

  const operationalStatus = String(data.operational_status || '').trim().toLowerCase();
  const baseStatus = String(data.status || '').trim().toLowerCase();
  const licenseStatus = baseStatus === 'license' ? baseStatus : (operationalStatus || baseStatus);
  const pollBadge = getPollBadge(licenseStatus);

  const pollMutation = useMutation({
    mutationFn: () => equipmentApi.pollServer(data.uuid),
    onSuccess: () => {
      message.success('Опрос отправлен');
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

  if (isCloud) {
    const fullUrl = iikoWebMeta?.url || '';

    return (
      <Card
        size="small"
        className="glass-panel"
        hoverable
        style={{ minWidth: 300, minHeight: 200 }}
        onClick={handleCardClick}
        title={(
          <Space>
            <GlobalOutlined style={{ color: token.colorPrimary }} />
            <Text strong>{data.device_name || data.server_name || 'Cloud Server'}</Text>
          </Space>
        )}
      >
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button
            size="small"
            type="primary"
            ghost
            href={fullUrl}
            target="_blank"
            onClick={(e) => e.stopPropagation()}
            icon={<LinkOutlined />}
            disabled={!fullUrl}
          >
            {iikoWebMeta?.label || 'iikoWeb'}
          </Button>
        </div>
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
      style={{ minWidth: 300, minHeight: 200 }}
      onClick={handleCardClick}
      title={(
        <Space>
          {getEntityIcon('Server')}
          <Text strong>{data.device_name || data.server_name || 'Сервер'}</Text>
        </Space>
      )}
      extra={(
        <Space size={8}>
          {agentUpdate ? (
            <AgentBadge
              agentID={agentUpdate.updater}
              onClick={() => navigate(`/agent-diagnostics/${encodeURIComponent(agentUpdate.updater)}`)}
            />
          ) : null}
          <ServerLicenseStatusTag
            serverID={String(data.uuid || '')}
            status={licenseStatus}
            uniqueID={String(data.unique_id || '')}
            displayStatus={pollBadge.text}
          />
        </Space>
      )}
      actions={canRunServerActions ? [
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
      ] : []}
    >
      <div style={{ marginBottom: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4 }}>
          <Text type="secondary">IP:</Text>
          {displayAddress ? (
            <Paragraph copyable={{ text: displayAddress }} style={{ margin: 0 }}>
              <Text strong>{displayAddress}</Text>
            </Paragraph>
          ) : (
            <Text type="secondary">Нет IP</Text>
          )}
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
          <Text type="secondary">Версия:</Text>
          <Text>{data.server_version} {data.server_edition ? `(${formatServerEdition(data.server_edition)})` : ''}</Text>
        </div>
        {(data.partners_link || iikoWebMeta) && (
          <div style={{ marginTop: 4, textAlign: 'right' }}>
            <Space size={4} wrap>
              {iikoWebMeta ? (
                <Button
                  type="link"
                  size="small"
                  href={iikoWebMeta.url}
                  target="_blank"
                  onClick={(e) => e.stopPropagation()}
                  icon={<LinkOutlined />}
                  style={{ paddingRight: 0 }}
                >
                  {iikoWebMeta.label}
                </Button>
              ) : null}
              {data.partners_link ? (
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
              ) : null}
            </Space>
          </div>
        )}
      </div>

      {hasAccessData && (
        <div style={{ borderTop: '1px solid var(--app-color-divider)', paddingTop: 8 }}>
          <Space direction="vertical" size={0} style={{ width: '100%' }}>
            {renderAccessLink('AnyDesk', data.anydesk)}
            {renderAccessLink('TeamViewer', data.teamviewer)}
            {renderAccessLink('RDP', data.rdp)}
            {renderAccessLink('LM', data.litemanager)}
          </Space>
        </div>
      )}
    </Card>
  );
};

export default ServerCard;
