import React, { createContext, useContext } from "react";

export type LayoutHeaderConfig = {
  mode: "reports" | "page-controls";
  title?: React.ReactNode;
  controls?: React.ReactNode;
};

export type LayoutHeaderAddonPlacement = "below" | "inline";

type LayoutHeaderContextValue = {
  headerConfig: LayoutHeaderConfig | null;
  setHeaderConfig: (config: LayoutHeaderConfig | null) => void;
  headerAddon: React.ReactNode | null;
  setHeaderAddon: (node: React.ReactNode | null) => void;
  headerAddonPlacement: LayoutHeaderAddonPlacement;
  setHeaderAddonPlacement: (placement: LayoutHeaderAddonPlacement) => void;
};

export const LayoutHeaderContext =
  createContext<LayoutHeaderContextValue | null>(null);

export const useLayoutHeader = () => {
  const context = useContext(LayoutHeaderContext);
  if (!context) {
    throw new Error("useLayoutHeader должен использоваться внутри MainLayout");
  }
  return context;
};
