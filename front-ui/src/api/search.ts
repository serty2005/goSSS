import apiClient from './axios';
import { ApiResponse, SearchResponseData } from '@/types/api';

export const searchApi = {
  searchEntities: async (term: string, limit = 50, showInactive = false) => {
    const response = await apiClient.get<ApiResponse<SearchResponseData>>('/search', {
      params: { term, limit, show_inactive: showInactive ? '1' : undefined },
    });
    return response.data;
  },
};
