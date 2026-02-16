import { useEffect, useState } from 'react';
import { useAuthStore } from '@/store/authStore';

type UseAgentObservationStreamOptions = {
  onMessage: (eventType: string, rawData: string) => void;
};

export const useAgentObservationStream = ({ onMessage }: UseAgentObservationStreamOptions) => {
  const authToken = useAuthStore((state) => state.token);
  const [isConnecting, setIsConnecting] = useState(true);

  useEffect(() => {
    if (!authToken) {
      setIsConnecting(false);
      return undefined;
    }

    let stopped = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let controller: AbortController | null = null;
    const decoder = new TextDecoder();

    const connect = async () => {
      while (!stopped) {
        setIsConnecting(true);
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

          setIsConnecting(false);
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
                  onMessage(eventType.trim(), dataLines.join('\n'));
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
          if (stopped) {
            break;
          }
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
  }, [authToken, onMessage]);

  return { isConnecting };
};
