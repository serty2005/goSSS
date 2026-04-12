import apiClient from './axios';
import type { ApiResponse, GlobalTranslationsDTO } from '@/types/api';

export const translationsApi = {
  getCatalog: async () => {
    const response = await apiClient.get<ApiResponse<GlobalTranslationsDTO>>('/translations');
    return response.data;
  },

  updateCatalog: async (payload: GlobalTranslationsDTO) => {
    const response = await apiClient.patch<ApiResponse<GlobalTranslationsDTO>>('/translations', payload);
    return response.data;
  },
};
