export interface AgentUpdateMeta {
  updater: string;
  updatedAt?: string;
}

const UUID_REGEX = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export const isAgentUpdater = (value?: string | null) => {
  const normalized = String(value || '').trim();
  if (!normalized) return false;
  if (UUID_REGEX.test(normalized)) return true;
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
