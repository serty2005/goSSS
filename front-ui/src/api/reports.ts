import apiClient from './axios';
import { ApiResponse, CompanyContractReportRowDTO } from '@/types/api';

export interface CompanyContractsReportFilters {
  statuses?: string[];
  contract_types?: string[];
  company_ids?: string[];
}

const joinMulti = (values?: string[]) => (values && values.length > 0 ? values.join(',') : undefined);

export const reportsApi = {
  getCompanyContractsReport: async (filters: CompanyContractsReportFilters) => {
    const response = await apiClient.get<ApiResponse<CompanyContractReportRowDTO[]>>('/reports/companies/contracts', {
      params: {
        statuses: joinMulti(filters.statuses),
        contract_types: joinMulti(filters.contract_types),
        company_ids: joinMulti(filters.company_ids),
      },
    });
    return response.data;
  },

  exportCompanyContractsReport: async (filters: CompanyContractsReportFilters) => {
    const response = await apiClient.get('/reports/companies/contracts/export', {
      params: {
        statuses: joinMulti(filters.statuses),
        contract_types: joinMulti(filters.contract_types),
        company_ids: joinMulti(filters.company_ids),
      },
      responseType: 'blob',
    });

    return {
      blob: response.data as Blob,
      contentDisposition: response.headers['content-disposition'] as string | undefined,
    };
  },
};

