import apiClient from './axios';
import {
  ApiResponse,
  ArticleDTO,
  ArticleListParams,
  ArticlePayload,
} from '@/types/api';

export const articlesApi = {
  list: async (params: ArticleListParams = {}) => {
    const response = await apiClient.get<ApiResponse<ArticleDTO[]>>('/articles', { params });
    return response.data;
  },

  featured: async (limit = 6) => {
    const response = await apiClient.get<ApiResponse<ArticleDTO[]>>('/articles/featured', {
      params: { limit },
    });
    return response.data;
  },

  get: async (id: string) => {
    const response = await apiClient.get<ApiResponse<ArticleDTO>>(`/articles/${id}`);
    return response.data;
  },

  create: async (payload: ArticlePayload) => {
    const response = await apiClient.post<ApiResponse<ArticleDTO>>('/articles', payload);
    return response.data;
  },

  update: async (id: string, payload: ArticlePayload) => {
    const response = await apiClient.put<ApiResponse<ArticleDTO>>(`/articles/${id}`, payload);
    return response.data;
  },

  publish: async (id: string) => {
    const response = await apiClient.patch<ApiResponse<ArticleDTO>>(`/articles/${id}/publish`);
    return response.data;
  },

  archive: async (id: string) => {
    const response = await apiClient.patch<ApiResponse<ArticleDTO>>(`/articles/${id}/archive`);
    return response.data;
  },
};
