import apiClient from './axios';
import {
  ApiResponse,
  BitrixUserSuggestionDTO,
  ServicePointImportPreviewDTO,
  ServicePointSyncApplyResultDTO,
  ServicePointSyncPreviewDTO,
} from '@/types/api';

export const bitrixAdminApi = {
  refreshUsers: async () => {
    const response = await apiClient.post<ApiResponse<{ status: string; count: number }>>('/bitrix/users/refresh');
    return response.data;
  },

  suggestUserByName: async (params: { first_name: string; last_name: string; full_name?: string }) => {
    const response = await apiClient.get<ApiResponse<{ suggestion?: BitrixUserSuggestionDTO | null }>>('/bitrix/users/suggest', {
      params,
    });
    return response.data;
  },

  previewServicePointsImport: async (file: File) => {
    const formData = new FormData();
    formData.append('file', file);

    const response = await apiClient.post<ApiResponse<ServicePointImportPreviewDTO>>(
      '/bitrix/service-points/import/preview',
      formData,
      {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
      },
    );

    return response.data;
  },

  previewServicePointsSync: async (
    file: File,
    mapping: { code_column: string; name_column: string; contract_column: string },
  ) => {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('code_column', mapping.code_column);
    formData.append('name_column', mapping.name_column);
    formData.append('contract_column', mapping.contract_column);

    const response = await apiClient.post<ApiResponse<ServicePointSyncPreviewDTO>>(
      '/bitrix/service-points/import/sync-preview',
      formData,
      {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
      },
    );

    return response.data;
  },

  applyServicePointsImport: async (
    file: File,
    mapping: { code_column: string; name_column: string; contract_column: string; selected_rows?: number[] },
  ) => {
    const formData = new FormData();
    formData.append('file', file);
    formData.append('code_column', mapping.code_column);
    formData.append('name_column', mapping.name_column);
    formData.append('contract_column', mapping.contract_column);
    if (mapping.selected_rows && mapping.selected_rows.length > 0) {
      formData.append('selected_rows', JSON.stringify(mapping.selected_rows));
    }

    const response = await apiClient.post<ApiResponse<ServicePointSyncApplyResultDTO>>(
      '/bitrix/service-points/import/apply',
      formData,
      {
        headers: {
          'Content-Type': 'multipart/form-data',
        },
      },
    );

    return response.data;
  },
};
