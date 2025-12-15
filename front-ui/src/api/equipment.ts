import apiClient from './axios';
import { 
  ApiResponse, 
  ServerDetailDTO, 
  WorkstationDetailDTO, 
  FiscalDetailDTO,
  UpdateServerPayload, 
  UpdateWorkstationPayload, 
  UpdateFiscalPayload 
} from '@/types/api';

export const equipmentApi = {
  // --- Servers ---
  getServer: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<ServerDetailDTO>>(`/servers/${uuid}`);
    return response.data;
  },
  
  updateServer: async (uuid: string, data: UpdateServerPayload) => {
    const response = await apiClient.put<ApiResponse<ServerDetailDTO>>(`/servers/${uuid}`, data);
    return response.data;
  },

  pollServer: async (uuid: string) => {
    const response = await apiClient.post<ApiResponse<void>>(`/servers/${uuid}/poll`);
    return response.data;
  },

  // --- Workstations ---
  getWorkstation: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<WorkstationDetailDTO>>(`/workstations/${uuid}`);
    return response.data;
  },

  updateWorkstation: async (uuid: string, data: UpdateWorkstationPayload) => {
    const response = await apiClient.put<ApiResponse<WorkstationDetailDTO>>(`/workstations/${uuid}`, data);
    return response.data;
  },

  // --- Fiscals ---
  getFiscal: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<FiscalDetailDTO>>(`/fiscals/${uuid}`);
    return response.data;
  },
  
  updateFiscal: async (uuid: string, data: UpdateFiscalPayload) => {
    const response = await apiClient.put<ApiResponse<FiscalDetailDTO>>(`/fiscals/${uuid}`, data);
    return response.data;
  },
};