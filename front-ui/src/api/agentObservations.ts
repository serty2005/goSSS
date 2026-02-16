import apiClient from './axios';
import { AgentObservationDetailsDTO, AgentListItemDTO, AgentObservationFeedRowDTO, ApiResponse } from '@/types/api';

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

  listAgents: async (params?: { term?: string; limit?: number }) => {
    const response = await apiClient.get<ApiResponse<AgentListItemDTO[]>>('/agents-list', { params });
    return response.data;
  },

  getByID: async (id: number) => {
    const response = await apiClient.get<ApiResponse<AgentObservationDetailsDTO>>(`/agent-observations/${id}`);
    return response.data;
  },
};
