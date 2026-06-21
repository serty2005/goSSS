import apiClient from './axios';
import { ApiResponse, ContractDetailDTO } from '@/types/api';

export const contractsApi = {
  getContract: async (id: string) => {
    const response = await apiClient.get<ApiResponse<ContractDetailDTO>>(`/contracts/${id}`);
    return response.data;
  },

  listCompanyContracts: async (companyId: string) => {
    const response = await apiClient.get<ApiResponse<ContractDetailDTO[]>>('/contracts/', {
      params: { company_id: companyId },
    });
    return response.data;
  },

  createContract: async (data: Record<string, unknown>) => {
    const response = await apiClient.post<ApiResponse<ContractDetailDTO>>('/contracts', data);
    return response.data;
  },

  updateContract: async (id: string, data: Record<string, unknown>) => {
    const response = await apiClient.put<ApiResponse<{ message: string }>>(`/contracts/${id}`, data);
    return response.data;
  },
};
