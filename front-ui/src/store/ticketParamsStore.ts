import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface TicketParamsState {
  ticketParams: string;
  createTicketRequestID: number;
  setTicketParams: (params: string) => void;
  requestCreateTicket: () => void;
  clearCreateTicketRequest: () => void;
}

export const useTicketParamsStore = create<TicketParamsState>()(
  persist(
    (set) => ({
      ticketParams: '',
      createTicketRequestID: 0,
      setTicketParams: (params) => set({ ticketParams: params }),
      requestCreateTicket: () =>
        set((state) => ({
          createTicketRequestID: state.createTicketRequestID + 1,
        })),
      clearCreateTicketRequest: () => set({ createTicketRequestID: 0 }),
    }),
    {
      name: 'tickets-params-storage',
      partialize: (state) => ({ ticketParams: state.ticketParams }),
    },
  ),
);

