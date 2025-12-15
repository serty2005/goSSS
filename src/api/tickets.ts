import apiClient from './axios';
import { ApiResponse, TicketDTO, TicketListParams } from '@/types/api';

export const ticketsApi = {
  getTickets: async (params: TicketListParams = {}) => {
    const response = await apiClient.get<ApiResponse<TicketDTO[]>>('/tickets', {
      params,
    });
    return response.data;
  },

  getTicket: async (id: number | string) => {
    const response = await apiClient.get<ApiResponse<TicketDTO>>(`/tickets/${id}`);
    return response.data;
  },
};