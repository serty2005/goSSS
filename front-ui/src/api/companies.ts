import apiClient from './axios';
import { ApiResponse, CompanyModel, InfrastructureItem } from '@/types/api';

export const companiesApi = {
  // Получение профиля компании
  getCompany: async (id: string) => {
    // В URL передаем ID компании
    const response = await apiClient.get<ApiResponse<CompanyModel>>(`/companies/${id}`);
    return response.data;
  },

  // Получение инфраструктуры (CMDB)
  getInfrastructure: async (id: string) => {
    const response = await apiClient.get<ApiResponse<InfrastructureItem[]>>(`/companies/${id}/infrastructure`);
    return response.data;
  },
};