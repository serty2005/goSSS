import apiClient from './axios';
import {
  ApiResponse,
  PyrusIncomingEventDTO,
  PyrusIncomingTaskDTO,
  PyrusReplayResultDTO,
  PyrusUserSuggestionDTO,
  PyrusUsersRefreshDTO,
} from '@/types/api';

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

  // Сводка входящих событий по задачам; scope=problem оставляет задачи с ошибками и ожиданием данных.
  listIncomingTasks: async (params: { scope: 'problem' | 'all'; limit: number; offset: number }) => {
    const response = await apiClient.get<ApiResponse<PyrusIncomingTaskDTO[]>>('/integrations/pyrus/sync/incoming-tasks', {
      params,
    });
    return response.data;
  },

  listTaskEvents: async (taskId: number) => {
    const response = await apiClient.get<ApiResponse<PyrusIncomingEventDTO[]>>('/integrations/pyrus/sync/incoming-events', {
      params: { task_id: taskId, limit: 200 },
    });
    return response.data;
  },

  replayTask: async (taskId: number) => {
    const response = await apiClient.post<ApiResponse<PyrusReplayResultDTO>>(`/integrations/pyrus/sync/incoming-tasks/${taskId}/replay`);
    return response.data;
  },

  replayEvent: async (eventId: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(`/integrations/pyrus/sync/incoming-events/${eventId}/replay`);
    return response.data;
  },

  replayProblemTasks: async () => {
    const response = await apiClient.post<ApiResponse<PyrusReplayResultDTO>>('/integrations/pyrus/sync/incoming-tasks/replay-problem');
    return response.data;
  },
};
