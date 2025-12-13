import apiClient from './axios';
import { ApiResponse } from '@/types/api';

export const equipmentApi = {
  // Принудительный опрос сервера
  pollServer: async (uuid: string) => {
    // Предполагаем, что бэкенд поддерживает этот эндпоинт
    const response = await apiClient.post<ApiResponse<void>>(`/servers/${uuid}/poll`);
    return response.data;
  },
};