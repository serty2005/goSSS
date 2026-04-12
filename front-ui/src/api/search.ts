import apiClient from './axios';
import { ApiResponse, SearchResponseData } from '@/types/api';

export const searchApi = {
  searchEntities: async (term: string, limit = 50) => {
    const response = await apiClient.get<ApiResponse<SearchResponseData>>('/search', {
      params: { term, limit, show_inactive: '1' },
    });
    return response.data;
  },
};
