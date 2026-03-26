import apiClient from './axios';
import { ApiResponse, UserAdminDTO, UserCreatePayload, UserUpdatePayload } from '@/types/api';

export const usersApi = {
  getUsers: async () => {
    const response = await apiClient.get<ApiResponse<UserAdminDTO[]>>('/users');
    return response.data;
  },

  getAssignees: async () => {
    const response = await apiClient.get<ApiResponse<Array<{ id: number; full_name: string; username: string; is_active: boolean }>>>('/profile/assignees');
    return response.data;
  },

  createUser: async (payload: UserCreatePayload) => {
    const response = await apiClient.post<ApiResponse<UserAdminDTO>>('/users', payload);
    return response.data;
  },

  updateUser: async (id: number, payload: UserUpdatePayload) => {
    const response = await apiClient.put<ApiResponse<UserAdminDTO>>(`/users/${id}`, payload);
    return response.data;
  },

  updateUserStatus: async (id: number, is_active: boolean) => {
    const response = await apiClient.patch<ApiResponse<UserAdminDTO>>(`/users/${id}/status`, { is_active });
    return response.data;
  },

  applyBitrixSuggestion: async (id: number) => {
    const response = await apiClient.post<ApiResponse<UserAdminDTO>>(`/users/${id}/bitrix/sync-suggestion`);
    return response.data;
  },

  applyPyrusSuggestion: async (id: number) => {
    const response = await apiClient.post<ApiResponse<UserAdminDTO>>(`/users/${id}/pyrus/sync-suggestion`);
    return response.data;
  },
};

