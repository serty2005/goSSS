import apiClient from './axios';
import { ApiResponse, MaterialDTO, MaterialPayload } from '@/types/api';

export const materialsApi = {
  listByEntity: async (
    entityType: 'Company' | 'Server' | 'Workstation' | 'FiscalRegister',
    entityID: string,
    limit = 50,
    offset = 0,
  ) => {
    const response = await apiClient.get<ApiResponse<MaterialDTO[]>>('/materials', {
      params: {
        entity_type: entityType,
        entity_id: entityID,
        limit,
        offset,
      },
    });
    return response.data;
  },

  create: async (payload: MaterialPayload) => {
    const response = await apiClient.post<ApiResponse<MaterialDTO>>('/materials', payload);
    return response.data;
  },

  update: async (id: string, payload: MaterialPayload) => {
    const response = await apiClient.put<ApiResponse<MaterialDTO>>(`/materials/${id}`, payload);
    return response.data;
  },

  delete: async (id: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/materials/${id}`);
    return response.data;
  },
};
