import apiClient from './axios';
import type {
  ApiResponse,
  EntityDeletionCandidateDTO,
  EntityDeletionCandidateDetailsDTO,
  EntityDeletionReplayRequestDTO,
} from '@/types/api';

export const deletionCandidatesApi = {
  list: async (params?: { status?: string; limit?: number; offset?: number }) => {
    const response = await apiClient.get<ApiResponse<EntityDeletionCandidateDTO[]>>('/deletion-candidates', { params });
    return response.data;
  },

  getByEntity: async (entityType: string, entityID: string) => {
    const response = await apiClient.get<ApiResponse<EntityDeletionCandidateDTO | null>>('/deletion-candidates/by-entity', {
      params: { entity_type: entityType, entity_id: entityID },
    });
    return response.data;
  },

  request: async (payload: { entity_type: string; entity_id: string; reason?: string; comment?: string }) => {
    const response = await apiClient.post<ApiResponse<EntityDeletionCandidateDTO>>('/deletion-candidates', payload);
    return response.data;
  },

  confirm: async (id: number) => {
    const response = await apiClient.post<ApiResponse<EntityDeletionCandidateDTO>>(`/deletion-candidates/${id}/confirm`);
    return response.data;
  },

  getDetails: async (id: number) => {
    const response = await apiClient.get<ApiResponse<EntityDeletionCandidateDetailsDTO>>(`/deletion-candidates/${id}`);
    return response.data;
  },

  replay: async (id: number, payload: EntityDeletionReplayRequestDTO) => {
    const response = await apiClient.post<ApiResponse<EntityDeletionCandidateDTO>>(`/deletion-candidates/${id}/replay`, payload);
    return response.data;
  },
};
