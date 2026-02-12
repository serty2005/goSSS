import apiClient from './axios';
import { ApiResponse, NetworkCandidateApprovePayload, NetworkCandidateDetailsDTO, NetworkCandidateDTO } from '@/types/api';

interface NetworkCandidatesListParams {
  status?: string;
  limit?: number;
  offset?: number;
}

export const networkCandidatesApi = {
  list: async (params: NetworkCandidatesListParams = {}) => {
    const response = await apiClient.get<ApiResponse<NetworkCandidateDTO[]>>('/network-candidates', { params });
    return response.data;
  },
  get: async (id: number) => {
    const response = await apiClient.get<ApiResponse<NetworkCandidateDetailsDTO>>(`/network-candidates/${id}`);
    return response.data;
  },
  approve: async (id: number, payload: NetworkCandidateApprovePayload) => {
    const response = await apiClient.post<ApiResponse<NetworkCandidateDTO>>(`/network-candidates/${id}/approve`, payload);
    return response.data;
  },
  removeGroup: async (id: number, groupID: number) => {
    const response = await apiClient.post<ApiResponse<NetworkCandidateDTO>>(`/network-candidates/${id}/groups/${groupID}/remove`);
    return response.data;
  },
};
