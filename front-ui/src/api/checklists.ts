import apiClient from './axios';
import {
  ApiResponse,
  ChecklistTemplateDTO,
  ChecklistTemplatePayload,
  TicketChecklistItemDTO,
} from '@/types/api';

export type ChecklistItemCreatePayload = {
  title: string;
  parent_id?: string | null;
  assignee_ids?: number[];
};

export type ChecklistItemUpdatePayload = {
  title?: string;
  is_done?: boolean;
  assignee_ids?: number[];
};

// ticketChecklistQueryKey вложен в ключ тикета, чтобы realtime-инвалидация ['ticket', id] обновляла и чеклист.
export const ticketChecklistQueryKey = (ticketID: string) => ['ticket', ticketID, 'checklist'] as const;

export const checklistsApi = {
  list: async (ticketID: string) => {
    const response = await apiClient.get<ApiResponse<TicketChecklistItemDTO[]>>(`/tickets/${ticketID}/checklist`);
    return response.data;
  },

  createItems: async (ticketID: string, payload: ChecklistItemCreatePayload) => {
    const response = await apiClient.post<ApiResponse<TicketChecklistItemDTO[]>>(
      `/tickets/${ticketID}/checklist/items`,
      payload,
    );
    return response.data;
  },

  updateItem: async (ticketID: string, itemID: string, payload: ChecklistItemUpdatePayload) => {
    const response = await apiClient.patch<ApiResponse<TicketChecklistItemDTO>>(
      `/tickets/${ticketID}/checklist/items/${itemID}`,
      payload,
    );
    return response.data;
  },

  deleteItem: async (ticketID: string, itemID: string) => {
    const response = await apiClient.delete<ApiResponse<{ status: string }>>(
      `/tickets/${ticketID}/checklist/items/${itemID}`,
    );
    return response.data;
  },

  reorder: async (ticketID: string, parentID: string | null, orderedIDs: string[]) => {
    const response = await apiClient.post<ApiResponse<{ status: string }>>(
      `/tickets/${ticketID}/checklist/reorder`,
      { parent_id: parentID, ordered_ids: orderedIDs },
    );
    return response.data;
  },

  applyTemplate: async (ticketID: string, templateID: string) => {
    const response = await apiClient.post<ApiResponse<TicketChecklistItemDTO[]>>(
      `/tickets/${ticketID}/checklist/apply-template`,
      { template_id: templateID },
    );
    return response.data;
  },

  listTemplates: async (includeInactive = false) => {
    const response = await apiClient.get<ApiResponse<ChecklistTemplateDTO[]>>('/checklist-templates', {
      params: includeInactive ? { all: true } : undefined,
    });
    return response.data;
  },

  createTemplate: async (payload: ChecklistTemplatePayload) => {
    const response = await apiClient.post<ApiResponse<ChecklistTemplateDTO>>('/checklist-templates', payload);
    return response.data;
  },

  updateTemplate: async (id: string, payload: ChecklistTemplatePayload) => {
    const response = await apiClient.put<ApiResponse<ChecklistTemplateDTO>>(`/checklist-templates/${id}`, payload);
    return response.data;
  },

  deleteTemplate: async (id: string) => {
    const response = await apiClient.delete<ApiResponse<{ status: string }>>(`/checklist-templates/${id}`);
    return response.data;
  },
};
