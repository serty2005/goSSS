import apiClient from './axios';
import { ApiResponse, PyrusUserSuggestionDTO } from '@/types/api';

export const pyrusAdminApi = {
  suggestUserByIdentity: async (params: { first_name?: string; last_name?: string; full_name?: string; email?: string }) => {
    const response = await apiClient.get<ApiResponse<{ suggestion?: PyrusUserSuggestionDTO | null }>>('/pyrus/users/suggest', {
      params,
    });
    return response.data;
  },
};
