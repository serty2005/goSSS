import { useContext } from 'react';
import { SSEContext } from '@/features/realtime/SSEContext';

export const useSSE = () => {
  const value = useContext(SSEContext);
  if (!value) {
    throw new Error('useSSE должен использоваться внутри SSEProvider');
  }
  return value;
};
