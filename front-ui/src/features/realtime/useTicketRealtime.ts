import { useEffect } from 'react';
import { useSSE } from '@/features/realtime/useSSE';

export type TicketRealtimePayload = {
  ticket_id?: string;
  action?: string;
  source?: string;
  message?: string;
  occurred_at?: string;
};

export const useTicketRealtime = (onEvent: (payload: TicketRealtimePayload) => void) => {
  const { subscribe } = useSSE();

  useEffect(() => {
    const unsubscribe = subscribe('ticket.updated', (_eventType, rawData) => {
      try {
        const payload = JSON.parse(rawData) as TicketRealtimePayload;
        onEvent(payload);
      } catch {
        // Некорректное событие пропускаем без прерывания потока.
      }
    });
    return unsubscribe;
  }, [onEvent, subscribe]);
};

