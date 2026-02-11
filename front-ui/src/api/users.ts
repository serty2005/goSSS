import apiClient from './axios';
import { ApiResponse, UserAdminDTO, UserCreatePayload, UserUpdatePayload } from '@/types/api';

export const usersApi = {
  getUsers: async () => {
    const response = await apiClient.get<ApiResponse<UserAdminDTO[]>>('/users');
    return response.data;
  },

  getAssignees: async () => {
    const response = await apiClient.get<ApiResponse<Array<{ id: number; fullName: string; username: string; isActive: boolean }>>>('/profile/assignees');
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

  updateUserStatus: async (id: number, isActive: boolean) => {
    const response = await apiClient.patch<ApiResponse<UserAdminDTO>>(`/users/${id}/status`, { isActive });
    return response.data;
  },
};
