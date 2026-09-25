import apiClient from './axios';
import { AgentObservationDetailsDTO, AgentListItemDTO, AgentObservationFeedRowDTO, ApiResponse } from '@/types/api';
import { createBatchLoader } from '@/utils/batchLoader';

export const agentObservationsApi = {
  listFeed: async (params?: {
    sort_by?: 'latest' | 'v_time' | 'current_time';
    order?: 'asc' | 'desc';
    agent_uuid?: string;
    workstation_id?: string;
    fr_id?: string;
    limit?: number;
  }) => {
    const response = await apiClient.get<ApiResponse<AgentObservationFeedRowDTO[]>>('/agent-observations', {
      params,
    });
    return response.data;
  },

  // Последние наблюдения для набора агентов одним запросом
  listLatestForAgents: async (agentUUIDs: string[]) => {
    const response = await apiClient.get<ApiResponse<AgentObservationFeedRowDTO[]>>('/agent-observations/latest', {
      params: { agent_uuids: agentUUIDs.join(',') },
    });
    return response.data;
  },

  listAgents: async (params?: { term?: string; limit?: number }) => {
    const response = await apiClient.get<ApiResponse<AgentListItemDTO[]>>('/agents-list', { params });
    return response.data;
  },

  getByID: async (id: number) => {
    const response = await apiClient.get<ApiResponse<AgentObservationDetailsDTO>>(`/agent-observations/${id}`);
    return response.data;
  },
};

// Загрузчик последнего наблюдения агента: запросы от всех бейджей на странице
// объединяются в пакетные запросы к /agent-observations/latest.
const latestObservationLoader = createBatchLoader<AgentObservationFeedRowDTO>(
  async (agentUUIDs) => {
    const response = await agentObservationsApi.listLatestForAgents(agentUUIDs);
    const result = new Map<string, AgentObservationFeedRowDTO>();
    (response.data || []).forEach((row) => {
      const agentUUID = String(row.agent_uuid || '').trim();
      if (agentUUID) {
        result.set(agentUUID, row);
      }
    });
    return result;
  },
  { maxBatchSize: 100, delayMs: 20 },
);

export const loadLatestAgentObservation = async (agentUUID: string) => (
  (await latestObservationLoader.load(agentUUID)) ?? null
);
