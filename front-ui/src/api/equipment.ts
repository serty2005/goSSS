import apiClient from './axios';
import { 
  ApiResponse, 
  EntityOwnerHistoryItemDTO,
  ServerDetailDTO, 
  WorkstationDetailDTO, 
  FiscalDetailDTO,
  UpdateServerPayload, 
  UpdateWorkstationPayload, 
  UpdateFiscalPayload 
} from '@/types/api';

export const equipmentApi = {
  // --- Servers ---
  listServers: async (term = '', limit = 50, offset = 0, companyIDs: string[] = []) => {
    const response = await apiClient.get<ApiResponse<Record<string, unknown>[]>>('/servers', {
      params: {
        term,
        limit,
        offset,
        ...(companyIDs.length > 0 ? { company_ids: companyIDs.join(',') } : {}),
      },
    });
    return response.data;
  },

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

  deleteServer: async (uuid: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/servers/${uuid}`);
    return response.data;
  },

  // --- Workstations ---
  listWorkstations: async (term = '', limit = 50, offset = 0) => {
    const response = await apiClient.get<ApiResponse<Record<string, unknown>[]>>('/workstations', {
      params: { term, limit, offset },
    });
    return response.data;
  },

  getWorkstation: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<WorkstationDetailDTO>>(`/workstations/${uuid}`);
    return response.data;
  },

  updateWorkstation: async (uuid: string, data: UpdateWorkstationPayload) => {
    const response = await apiClient.put<ApiResponse<WorkstationDetailDTO>>(`/workstations/${uuid}`, data);
    return response.data;
  },

  deleteWorkstation: async (uuid: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/workstations/${uuid}`);
    return response.data;
  },

  // --- Fiscals ---
  listFiscals: async (term = '', limit = 50, offset = 0) => {
    const response = await apiClient.get<ApiResponse<Record<string, unknown>[]>>('/fiscals', {
      params: { term, limit, offset },
    });
    return response.data;
  },

  getFiscal: async (uuid: string) => {
    const response = await apiClient.get<ApiResponse<FiscalDetailDTO>>(`/fiscals/${uuid}`);
    return response.data;
  },
  
  updateFiscal: async (uuid: string, data: UpdateFiscalPayload) => {
    const response = await apiClient.put<ApiResponse<FiscalDetailDTO>>(`/fiscals/${uuid}`, data);
    return response.data;
  },

  deleteFiscal: async (uuid: string) => {
    const response = await apiClient.delete<ApiResponse<unknown>>(`/fiscals/${uuid}`);
    return response.data;
  },

  getOwnerHistory: async (entityType: 'Server' | 'Workstation' | 'FiscalRegister', entityID: string, limit = 100) => {
    const response = await apiClient.get<ApiResponse<EntityOwnerHistoryItemDTO[]>>('/owner-history', {
      params: { entity_type: entityType, entity_id: entityID, limit },
    });
    return response.data;
  },
};
