import apiClient from './axios';
import { ApiResponse, CandidateApprovePayload, CandidateDTO, CandidateObservationDTO, CandidateRecalculationResultDTO, CandidateStatus } from '@/types/api';

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

  getCandidateObservations: async (id: number, observationIDs: number[]) => {
    const ids = observationIDs
      .map((value) => Number(value))
      .filter((value) => Number.isFinite(value) && value > 0);
    const params = ids.length > 0 ? { ids: ids.join(',') } : undefined;
    const response = await apiClient.get<ApiResponse<CandidateObservationDTO[]>>(`/candidates/${id}/observations`, {
      params,
    });
    return response.data;
  },

  recalculateCandidates: async () => {
    const response = await apiClient.post<ApiResponse<CandidateRecalculationResultDTO>>('/candidates/recalculate');
    return response.data;
  },

  approveCandidate: async (id: number, payload: CandidateApprovePayload) => {
    const response = await apiClient.post<ApiResponse<CandidateDTO>>(`/candidates/${id}/approve`, payload);
    return response.data;
  },

  approveManualCandidate: async (payload: CandidateApprovePayload) => {
    const response = await apiClient.post<ApiResponse<CandidateDTO>>('/candidates/approve-manual', payload);
    return response.data;
  },
};
