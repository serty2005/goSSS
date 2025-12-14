import apiClient from './axios';
import { ApiResponse, TaskDTO, TaskResolutionPayload } from '@/types/api';

export const tasksApi = {
  getTasks: async (status?: string, page = 1, limit = 50) => {
    const offset = (page - 1) * limit;
    const response = await apiClient.get<ApiResponse<TaskDTO[]>>('/tasks', {
      params: { status, limit, offset },
    });
    return response.data;
  },

  resolveTask: async (id: number, payload: TaskResolutionPayload) => {
    const response = await apiClient.post<ApiResponse<void>>(`/tasks/${id}/resolve`, payload);
    return response.data;
  },

  createEntityInSd: async (id: number, entityType: string) => {
    const response = await apiClient.post<ApiResponse<void>>(`/tasks/${id}/create-entity-in-sd`, {
      entity_type: entityType,
    });
    return response.data;
  },
};