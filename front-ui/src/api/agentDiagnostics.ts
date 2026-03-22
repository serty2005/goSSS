import apiClient from './axios';
import {
  AgentDiagnosticsDetailsDTO,
  AgentDiagnosticsListItemDTO,
  ApiResponse,
  SaveAgentAdapterSelectionPayload,
  UpsertAgentCOMSignatureRulePayload,
} from '@/types/api';

export const agentDiagnosticsApi = {
  list: async (params?: {
    term?: string;
    registration_status?: string;
    limit?: number;
  }) => {
    const response = await apiClient.get<ApiResponse<AgentDiagnosticsListItemDTO[]>>('/agent-diagnostics', {
      params,
    });
    return response.data;
  },

  getByUUID: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<AgentDiagnosticsDetailsDTO>>(`/agent-diagnostics/${uuid}`);
    return response.data;
  },

  approveRegistration: async (uuid: string) => {
    const response = await apiClient.post<ApiResponse<AgentDiagnosticsDetailsDTO>>(`/agent-diagnostics/${uuid}/approve-registration`);
    return response.data;
  },

  saveAdapterSelection: async (uuid: string, payload: SaveAgentAdapterSelectionPayload) => {
    const response = await apiClient.post<ApiResponse<AgentDiagnosticsDetailsDTO>>(`/agent-diagnostics/${uuid}/adapter-selection`, payload);
    return response.data;
  },

  upsertCOMSignatureRule: async (uuid: string, payload: UpsertAgentCOMSignatureRulePayload) => {
    const response = await apiClient.post<ApiResponse<AgentDiagnosticsDetailsDTO>>(`/agent-diagnostics/${uuid}/signature-rules`, payload);
    return response.data;
  },
};
