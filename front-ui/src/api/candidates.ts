import apiClient from './axios';
import { ApiResponse, CandidateApprovePayload, CandidateDTO, CandidateStatus } from '@/types/api';

interface CandidatesListParams {
  status?: CandidateStatus | 'ACTIVE' | 'ALL';
  limit?: number;
  offset?: number;
}

export const candidatesApi = {
  listCandidates: async (params: CandidatesListParams = {}) => {
    const response = await apiClient.get<ApiResponse<CandidateDTO[]>>('/candidates', {
      params,
    });
    return response.data;
  },

  getCandidate: async (id: number) => {
    const response = await apiClient.get<ApiResponse<CandidateDTO>>(`/candidates/${id}`);
    return response.data;
  },

  approveCandidate: async (id: number, payload: CandidateApprovePayload) => {
    const response = await apiClient.post<ApiResponse<CandidateDTO>>(`/candidates/${id}/approve`, payload);
    return response.data;
  },
};
