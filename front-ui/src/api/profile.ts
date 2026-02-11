import apiClient from './axios';
import { ApiResponse } from '@/types/api';

export const profileApi = {
  updateCredentials: async (payload: { username?: string; password?: string }) => {
    const response = await apiClient.patch<ApiResponse<{ status: string }>>('/profile/credentials', payload);
    return response.data;
  },

  updateIntegrations: async (payload: { integrations: Array<{ integrationType: string; externalId: string }> }) => {
    const response = await apiClient.patch<ApiResponse<any>>('/profile/integrations', payload);
    return response.data;
  },
};
