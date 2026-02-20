import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface TicketParamsState {
  ticketParams: string;
  createTicketRequestID: number;
  selectedTicketIDs: string[];
  setTicketParams: (params: string) => void;
  requestCreateTicket: () => void;
  clearCreateTicketRequest: () => void;
  setSelectedTicketIDs: (ids: string[]) => void;
  clearSelectedTicketIDs: () => void;
}

export const useTicketParamsStore = create<TicketParamsState>()(
  persist(
    (set) => ({
      ticketParams: '',
      createTicketRequestID: 0,
      selectedTicketIDs: [],
      setTicketParams: (params) => set({ ticketParams: params }),
      requestCreateTicket: () =>
        set((state) => ({
          createTicketRequestID: state.createTicketRequestID + 1,
        })),
      clearCreateTicketRequest: () => set({ createTicketRequestID: 0 }),
      setSelectedTicketIDs: (ids) => set({ selectedTicketIDs: ids }),
      clearSelectedTicketIDs: () => set({ selectedTicketIDs: [] }),
    }),
    {
      name: 'tickets-params-storage',
      partialize: (state) => ({ ticketParams: state.ticketParams }),
    },
  ),
);
