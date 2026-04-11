import apiClient from './axios';
import { ApiResponse, CompanyBitrixMappingRowDTO, CompanyModel, InfrastructureItem } from '@/types/api';

export const companiesApi = {
  // Поиск/листинг компаний
  searchCompanies: async (term: string, limit = 20, offset = 0) => {
    const response = await apiClient.get<ApiResponse<CompanyModel[]>>('/companies', {
      params: { term, limit, offset },
    });
    return response.data;
  },

  // Получение профиля компании
  getCompany: async (id: string) => {
    const response = await apiClient.get<ApiResponse<CompanyModel>>(`/companies/${id}`);
    return response.data;
  },

  updateCompany: async (id: string, data: Record<string, unknown>) => {
    const response = await apiClient.put<ApiResponse<{ status: string }>>(`/companies/${id}`, data);
    return response.data;
  },

  // Получение инфраструктуры (CMDB)
  getInfrastructure: async (id: string) => {
    const response = await apiClient.get<ApiResponse<InfrastructureItem[]>>(`/companies/${id}/infrastructure`);
    return response.data;
  },

  // Получение дочерних компаний
  getChildren: async (companyId: string) => {
    const response = await apiClient.get<ApiResponse<CompanyModel[]>>(`/companies/${companyId}/children`);
    return response.data;
  },

  getBitrixMappings: async (term: string, limit = 50, offset = 0) => {
    const response = await apiClient.get<ApiResponse<CompanyBitrixMappingRowDTO[]>>('/companies/bitrix-service-point-mappings', {
      params: { term, limit, offset },
    });
    return response.data;
  },

  getBitrixMappingByCompanyID: async (companyId: string) => {
    const response = await apiClient.get<CompanyBitrixMappingRowDTO[] | ApiResponse<CompanyBitrixMappingRowDTO[]>>('/companies/bitrix-service-point-mappings', {
      params: { company_id: companyId },
    });
    const data = response.data as unknown;
    if (Array.isArray(data)) {
      return data[0] ?? null;
    }
    if (data && typeof data === 'object' && Array.isArray((data as { data?: CompanyBitrixMappingRowDTO[] }).data)) {
      return (data as { data: CompanyBitrixMappingRowDTO[] }).data[0] ?? null;
    }
    return null;
  },

  updateBitrixMapping: async (payload: { company_id?: string; bitrix_service_point_id?: number }) => {
    const response = await apiClient.put<ApiResponse<{ status: string }>>('/companies/bitrix-service-point-mappings', payload);
    return response.data;
  },

  syncBitrixContract: async (companyId: string) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>('/companies/bitrix-service-point-mappings/sync-contract', {
      company_id: companyId,
    });
    return response.data;
  },
};
