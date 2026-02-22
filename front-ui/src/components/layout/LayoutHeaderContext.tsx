import React, { createContext, useContext } from 'react';

export type ReportHeaderConfig = {
  mode: 'reports';
  title: React.ReactNode;
  controls?: React.ReactNode;
};

type LayoutHeaderContextValue = {
  headerConfig: ReportHeaderConfig | null;
  setHeaderConfig: (config: ReportHeaderConfig | null) => void;
  headerAddon: React.ReactNode | null;
  setHeaderAddon: (node: React.ReactNode | null) => void;
};

export const LayoutHeaderContext = createContext<LayoutHeaderContextValue | null>(null);

export const useLayoutHeader = () => {
  const context = useContext(LayoutHeaderContext);
  if (!context) {
    throw new Error('useLayoutHeader должен использоваться внутри MainLayout');
  }
  return context;
};
