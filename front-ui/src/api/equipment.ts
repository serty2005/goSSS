import apiClient from './axios';
import { ApiResponse, ServerEntity, WorkstationEntity, FiscalEntity, UpdateServerDTO, UpdateWorkstationDTO, UpdateFiscalDTO } from '@/types/api';

export const equipmentApi = {
  // --- Servers ---
  getServer: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<ServerEntity>>(`/servers/${uuid}`);
    return response.data;
  },
  
  updateServer: async (uuid: string, data: UpdateServerDTO) => {
    const response = await apiClient.put<ApiResponse<ServerEntity>>(`/servers/${uuid}`, data);
    return response.data;
  },

  pollServer: async (uuid: string) => {
    const response = await apiClient.post<ApiResponse<void>>(`/servers/${uuid}/poll`);
    return response.data;
  },

  // --- Workstations ---
  getWorkstation: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<WorkstationEntity>>(`/workstations/${uuid}`);
    return response.data;
  },

  updateWorkstation: async (uuid: string, data: UpdateWorkstationDTO) => {
    const response = await apiClient.put<ApiResponse<WorkstationEntity>>(`/workstations/${uuid}`, data);
    return response.data;
  },

  // --- Fiscals ---
  getFiscal: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<FiscalEntity>>(`/fiscals/${uuid}`);
    return response.data;
  },
  
  updateFiscal: async (uuid: string, data: UpdateFiscalDTO) => {
    const response = await apiClient.put<ApiResponse<FiscalEntity>>(`/fiscals/${uuid}`, data);
    return response.data;
  },
};