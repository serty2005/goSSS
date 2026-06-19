import React from 'react';
import { Tag, Tooltip } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { agentObservationsApi } from '@/api/agentObservations';
import { isAgentDataStale, resolveAgentDataTimestamp } from '@/utils/agentUpdates';

type Props = {
  agentID: string;
  label?: string;
  onClick?: () => void;
  variant?: 'tag' | 'button';
};

const AgentBadge: React.FC<Props> = ({ agentID, label = 'Агент', onClick, variant = 'tag' }) => {
  const { data } = useQuery({
    queryKey: ['agent-observation', 'latest', agentID],
    queryFn: () => agentObservationsApi.listFeed({ agent_uuid: agentID, sort_by: 'latest', order: 'desc', limit: 1 }),
    enabled: Boolean(agentID),
    staleTime: 5 * 60_000,
  });
  const observation = data?.data?.[0];
  const timestamp = resolveAgentDataTimestamp(observation);
  const stale = isAgentDataStale(timestamp);
  const title = stale
    ? `Данные агента старше 25 дней${timestamp ? `: ${new Date(timestamp).toLocaleString('ru-RU')}` : ''}`
    : 'Открыть последнее наблюдение агента';

  if (variant === 'button') {
    return (
      <Tooltip title={title}>
        <button
          type="button"
          className={`ticket-agent-badge${stale ? ' ticket-agent-badge--stale' : ''}`}
          onClick={onClick}
        >
          {label}
        </button>
      </Tooltip>
    );
  }

  return (
    <Tooltip title={title}>
      <Tag
        color={stale ? 'gold' : 'blue'}
        style={{ fontSize: 12, lineHeight: '22px', paddingInline: 10, marginRight: 0, cursor: 'pointer' }}
        onClick={(event) => {
          event.stopPropagation();
          onClick?.();
        }}
      >
        {label}
      </Tag>
    </Tooltip>
  );
};

export default AgentBadge;
