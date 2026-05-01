import { useEffect, useState } from 'react';

export const TEXT_SEARCH_DEBOUNCE_MS = 450;
export const SELECT_SEARCH_DEBOUNCE_MS = 350;
export const THEME_COLOR_SAVE_DEBOUNCE_MS = 700;

export const useDebouncedValue = <T,>(value: T, delayMs: number) => {
  const [debouncedValue, setDebouncedValue] = useState(value);

  useEffect(() => {
    const timerID = window.setTimeout(() => {
      setDebouncedValue(value);
    }, delayMs);

    return () => window.clearTimeout(timerID);
  }, [delayMs, value]);

  return debouncedValue;
};
