import { useCallback, useLayoutEffect, useState } from 'react';

// useElementWidth отслеживает ширину элемента, чтобы раскладка зависела от размера колонки, а не окна.
export const useElementWidth = <T extends HTMLElement>() => {
  const [node, setNode] = useState<T | null>(null);
  const [width, setWidth] = useState(0);

  const ref = useCallback((element: T | null) => {
    setNode(element);
  }, []);

  useLayoutEffect(() => {
    if (!node) {
      return;
    }
    const update = () => setWidth(Math.floor(node.getBoundingClientRect().width));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);

  return [ref, width] as const;
};
