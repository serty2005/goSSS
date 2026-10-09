import apiClient from './axios';
import { ApiResponse, CompanyScopedMaterialDTO, MaterialDTO, MaterialFilesConfigDTO, MaterialPayload } from '@/types/api';

type MaterialListParams = {
  term?: string;
  /** Только материалы, у которых есть файлы. */
  has_files?: boolean;
  limit?: number;
  offset?: number;
};

const MULTIPART_CONFIG = { headers: { 'Content-Type': 'multipart/form-data' } };

export const materialsApi = {
  list: async (params: MaterialListParams = {}) => {
    const response = await apiClient.get<ApiResponse<MaterialDTO[]>>('/materials', { params });
    return response.data;
  },

  listByEntity: async (
    entityType: 'Company' | 'Server' | 'Workstation' | 'FiscalRegister',
    entityID: string,
    limit = 50,
    offset = 0,
  ) => {
    const response = await apiClient.get<ApiResponse<MaterialDTO[]>>('/materials', {
      params: {
        entity_type: entityType,
        entity_id: entityID,
        limit,
        offset,
      },
    });
    return response.data;
  },

  listCompanyScope: async (companyID: string) => {
    const response = await apiClient.get<ApiResponse<CompanyScopedMaterialDTO[]>>(
      `/materials/company-scope/${encodeURIComponent(companyID)}`,
    );
    return response.data;
  },

  create: async (payload: MaterialPayload) => {
    const response = await apiClient.post<ApiResponse<MaterialDTO>>('/materials', payload);
    return response.data;
  },

  // createWithFiles создает материал и загружает файлы одним multipart-запросом.
  createWithFiles: async (payload: MaterialPayload, files: File[]) => {
    const form = new FormData();
    form.append('subject', payload.subject);
    form.append('content', payload.content);
    form.append('entity_refs', JSON.stringify(payload.entity_refs));
    files.forEach((file) => form.append('files', file, file.name));
    const response = await apiClient.post<ApiResponse<MaterialDTO>>('/materials', form, MULTIPART_CONFIG);
    return response.data;
  },

  filesConfig: async () => {
    const response = await apiClient.get<ApiResponse<MaterialFilesConfigDTO>>('/materials/files-config');
    return response.data;
  },

  uploadFiles: async (id: string, files: File[]) => {
    const form = new FormData();
    files.forEach((file) => form.append('files', file, file.name));
    const response = await apiClient.post<ApiResponse<MaterialDTO>>(`/materials/${id}/files`, form, MULTIPART_CONFIG);
    return response.data;
  },

  deleteFile: async (id: string, fileID: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/materials/${id}/files/${fileID}`);
    return response.data;
  },

  update: async (id: string, payload: MaterialPayload) => {
    const response = await apiClient.put<ApiResponse<MaterialDTO>>(`/materials/${id}`, payload);
    return response.data;
  },

  delete: async (id: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/materials/${id}`);
    return response.data;
  },
};
