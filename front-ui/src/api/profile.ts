import apiClient from './axios';
import { ApiResponse } from '@/types/api';
import type { UserProfileConfigDTO } from '@/types/api';

export const profileApi = {
  updateCredentials: async (payload: { username?: string; password?: string }) => {
    const response = await apiClient.patch<ApiResponse<{ status: string }>>('/profile/credentials', payload);
    return response.data;
  },

  updateIntegrations: async (payload: { integrations: Array<{ integration_type: string; external_id: string }> }) => {
    const response = await apiClient.patch<ApiResponse<any>>('/profile/integrations', payload);
    return response.data;
  },

  getConfig: async () => {
    const response = await apiClient.get<ApiResponse<{ profile_config: UserProfileConfigDTO }>>('/profile/config');
    return response.data;
  },

  updateConfig: async (payload: { profile_config: UserProfileConfigDTO }) => {
    const response = await apiClient.patch<ApiResponse<any>>('/profile/config', payload);
    return response.data;
  },
};
