import apiClient from './axios';
import { ApiResponse } from '@/types/api';

export const profileApi = {
  updateCredentials: async (payload: { username?: string; password?: string }) => {
    const response = await apiClient.patch<ApiResponse<{ status: string }>>('/profile/credentials', payload);
    return response.data;
  },
};
