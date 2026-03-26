import apiClient from './axios';
import { ApiResponse, PyrusUserSuggestionDTO, PyrusUsersRefreshDTO } from '@/types/api';

export const pyrusAdminApi = {
  suggestUserByIdentity: async (params: { first_name?: string; last_name?: string; full_name?: string; email?: string }) => {
    const response = await apiClient.get<ApiResponse<{ suggestion?: PyrusUserSuggestionDTO | null }>>('/pyrus/users/suggest', {
      params,
    });
    return response.data;
  },

  refreshUsers: async () => {
    const response = await apiClient.post<ApiResponse<PyrusUsersRefreshDTO>>('/pyrus/users/refresh');
    return response.data;
  },
};
