import { createContext } from 'react';

export type SSECallback = (eventType: string, rawData: string) => void;

export type SSEContextValue = {
  subscribe: (eventType: string, callback: SSECallback) => () => void;
};

export const SSEContext = createContext<SSEContextValue | null>(null);
