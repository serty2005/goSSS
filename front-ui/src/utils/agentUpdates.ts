export interface AgentUpdateMeta {
  updater: string;
  updatedAt?: string;
}

export const isAgentDataStale = (value?: string, now = new Date()) => {
  if (!value) return false;
  const timestamp = new Date(value);
  return !Number.isNaN(timestamp.valueOf()) && timestamp.valueOf() < now.valueOf() - 25 * 24 * 60 * 60 * 1000;
};

export const resolveAgentDataTimestamp = (input?: {
  v_time_parsed?: string;
  v_time?: string;
  current_time_parsed?: string;
  current_time?: string;
}) => input?.v_time_parsed || input?.v_time || input?.current_time_parsed || input?.current_time;

const UUID_LIKE_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const isAgentUpdater = (value?: string | null) => {
  const normalized = String(value || '').trim();
  if (!normalized) return false;
  if (UUID_LIKE_REGEX.test(normalized)) return true;
  return normalized.toLowerCase().startsWith('agent');
};

export const getAgentUpdateMeta = (input: {
  last_updated_by?: string | null;
  last_modified_date?: string | null;
  updated_at?: string | null;
}): AgentUpdateMeta | null => {
  if (!isAgentUpdater(input.last_updated_by)) {
    return null;
  }

  const updater = String(input.last_updated_by || '').trim() || 'agent';
  const updatedAt = String(input.last_modified_date || input.updated_at || '').trim();
  return {
    updater,
    updatedAt: updatedAt || undefined,
  };
};
