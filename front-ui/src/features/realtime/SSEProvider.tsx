import React, { createContext, useCallback, useEffect, useMemo, useRef } from 'react';
import { useAuthStore } from '@/store/authStore';

type SSECallback = (eventType: string, rawData: string) => void;

type SSEContextValue = {
  subscribe: (eventType: string, callback: SSECallback) => () => void;
};

export const SSEContext = createContext<SSEContextValue | null>(null);

export const SSEProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const authToken = useAuthStore((state) => state.token);
  const listenersRef = useRef<Map<string, Set<SSECallback>>>(new Map());

  const emit = useCallback((eventType: string, rawData: string) => {
    const exact = listenersRef.current.get(eventType);
    if (exact) {
      exact.forEach((listener) => listener(eventType, rawData));
    }
    const all = listenersRef.current.get('*');
    if (all) {
      all.forEach((listener) => listener(eventType, rawData));
    }
  }, []);

  const subscribe = useCallback((eventType: string, callback: SSECallback) => {
    const key = eventType.trim() || '*';
    const set = listenersRef.current.get(key) || new Set<SSECallback>();
    set.add(callback);
    listenersRef.current.set(key, set);

    return () => {
      const current = listenersRef.current.get(key);
      if (!current) return;
      current.delete(callback);
      if (current.size === 0) {
        listenersRef.current.delete(key);
      }
    };
  }, []);

  useEffect(() => {
    if (!authToken) {
      return undefined;
    }

    let stopped = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let controller: AbortController | null = null;
    const decoder = new TextDecoder();

    const connect = async () => {
      while (!stopped) {
        controller = new AbortController();
        try {
          const response = await fetch('/api/events', {
            method: 'GET',
            headers: {
              Accept: 'text/event-stream',
              Authorization: `Bearer ${authToken}`,
            },
            signal: controller.signal,
          });
          if (!response.ok || !response.body) {
            throw new Error(`sse status ${response.status}`);
          }

          const reader = response.body.getReader();
          let buffer = '';
          let eventType = '';
          let dataLines: string[] = [];

          for (;;) {
            const { done, value } = await reader.read();
            if (done) {
              break;
            }
            buffer += decoder.decode(value, { stream: true });

            for (;;) {
              const newLineIdx = buffer.indexOf('\n');
              if (newLineIdx < 0) {
                break;
              }
              const lineRaw = buffer.slice(0, newLineIdx);
              buffer = buffer.slice(newLineIdx + 1);
              const line = lineRaw.replace(/\r$/, '');

              if (line === '') {
                if (dataLines.length > 0) {
                  emit(eventType.trim(), dataLines.join('\n'));
                }
                eventType = '';
                dataLines = [];
                continue;
              }
              if (line.startsWith(':')) {
                continue;
              }
              if (line.startsWith('event:')) {
                eventType = line.slice('event:'.length).trim();
                continue;
              }
              if (line.startsWith('data:')) {
                dataLines.push(line.slice('data:'.length).trimStart());
              }
            }
          }
        } catch {
          // Разрыв соединения обрабатывается через переподключение.
        }

        if (stopped) {
          break;
        }
        await new Promise<void>((resolve) => {
          reconnectTimer = setTimeout(() => resolve(), 2000);
        });
      }
    };

    void connect();

    return () => {
      stopped = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
      }
      if (controller) {
        controller.abort();
      }
    };
  }, [authToken, emit]);

  const value = useMemo<SSEContextValue>(() => ({
    subscribe,
  }), [subscribe]);

  return (
    <SSEContext.Provider value={value}>
      {children}
    </SSEContext.Provider>
  );
};

