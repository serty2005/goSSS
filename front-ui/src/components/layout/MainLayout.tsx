import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Layout,
  Menu,
  Button,
  Dropdown,
  Avatar,
  theme as antTheme,
  Typography,
  Space,
  Popover,
  Divider,
  message,
  Segmented,
  Grid,
  Tag,
  Drawer,
} from "antd";
import { Outlet, useNavigate, useLocation } from "react-router-dom";
import {
  CustomerServiceOutlined,
  BankOutlined,
  DesktopOutlined,
  SettingOutlined,
  UserOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  SunOutlined,
  MoonOutlined,
  CloseOutlined,
  HomeOutlined,
} from "@ant-design/icons";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import HeaderSearch from "@/components/common/HeaderSearch";
import GlobalSearchLauncher from "@/components/search/GlobalSearchLauncher";
import { formatLocaleDateTime } from "@/i18n/formatters";
import { useAppLocale } from "@/i18n/useAppLocale";
import { useUiStore } from "@/store/uiStore";
import { useAuthStore } from "@/store/authStore";
import { profileApi } from "@/api/profile";
import {
  LayoutHeaderContext,
  type LayoutHeaderAddonPlacement,
  type LayoutHeaderConfig,
} from "@/components/layout/LayoutHeaderContext";
import {
  buildProfileConfigWithPalettes,
  paletteFromProfileConfig,
} from "@/theme/profileConfig";
import {
  defaultThemePalettes,
  type ThemeMode,
  type ThemePalette,
} from "@/theme/themeConfig";
import {
  useTicketRealtime,
  type TicketRealtimePayload,
} from "@/features/realtime/useTicketRealtime";
import { THEME_COLOR_SAVE_DEBOUNCE_MS } from "@/hooks/useDebouncedValue";

const { Header, Sider, Content } = Layout;
const { Text } = Typography;
const { useBreakpoint } = Grid;

type EditableColorKey = "primary" | "bgLayout" | "bgContainer" | "borderColor";

type TicketNotificationItem = {
  id: string;
  ticketID: string;
  action: string;
  source: string;
  message: string;
  occurredAt: string;
};

const colorLabelKeys: Record<EditableColorKey, string> = {
  primary: "theme.colorPrimary",
  bgLayout: "theme.colorBgLayout",
  bgContainer: "theme.colorBgContainer",
  borderColor: "theme.colorBorder",
};

const palettePresets: Array<{
  key: string;
  labelKey: string;
  light: Pick<
    ThemePalette,
    "primary" | "bgLayout" | "bgContainer" | "borderColor"
  >;
  dark: Pick<
    ThemePalette,
    "primary" | "bgLayout" | "bgContainer" | "borderColor"
  >;
}> = [
  {
    key: "classic",
    labelKey: "theme.presetClassic",
    light: {
      primary: "#1677ff",
      bgLayout: "#f0f2f5",
      bgContainer: "#ffffff",
      borderColor: "#d9d9d9",
    },
    dark: {
      primary: "#177ddc",
      bgLayout: "#000000",
      bgContainer: "#141414",
      borderColor: "#303030",
    },
  },
  {
    key: "mint",
    labelKey: "theme.presetMint",
    light: {
      primary: "#13c2c2",
      bgLayout: "#eefaf9",
      bgContainer: "#ffffff",
      borderColor: "#a8d8d8",
    },
    dark: {
      primary: "#36cfc9",
      bgLayout: "#0b1516",
      bgContainer: "#111f20",
      borderColor: "#245054",
    },
  },
  {
    key: "amber",
    labelKey: "theme.presetAmber",
    light: {
      primary: "#faad14",
      bgLayout: "#fff8e6",
      bgContainer: "#fffdf7",
      borderColor: "#e8d3a3",
    },
    dark: {
      primary: "#d89614",
      bgLayout: "#1a1408",
      bgContainer: "#241b0c",
      borderColor: "#5e4a1d",
    },
  },
  {
    key: "graphite",
    labelKey: "theme.presetGraphite",
    light: {
      primary: "#595959",
      bgLayout: "#f5f5f5",
      bgContainer: "#ffffff",
      borderColor: "#bfbfbf",
    },
    dark: {
      primary: "#8c8c8c",
      bgLayout: "#0f0f0f",
      bgContainer: "#1a1a1a",
      borderColor: "#3a3a3a",
    },
  },
];

const normalizeColor = (value: string | undefined, fallback: string) => {
  const candidate = String(value || "")
    .trim()
    .toLowerCase();
  return /^#[\da-f]{6}$/.test(candidate) ? candidate : fallback;
};

const MAX_TICKET_NOTIFICATIONS = 50;

const renderTicketNotificationTitle = (
  item: TicketNotificationItem,
  fallbackTitle: string,
) => {
  if (item.message.trim()) {
    return item.message.trim();
  }
  return fallbackTitle;
};

const MainLayout: React.FC = () => {
  const { t } = useTranslation(["layout", "common"]);
  const { locale } = useAppLocale();
  const [themeMenuOpen, setThemeMenuOpen] = useState(false);
  const [mobileNavGroupKey, setMobileNavGroupKey] = useState<string | null>(
    null,
  );
  const [headerConfig, setHeaderConfig] = useState<LayoutHeaderConfig | null>(
    null,
  );
  const [headerAddon, setHeaderAddon] = useState<React.ReactNode | null>(null);
  const [headerAddonPlacement, setHeaderAddonPlacement] =
    useState<LayoutHeaderAddonPlacement>("below");
  const [ticketNotifications, setTicketNotifications] = useState<
    TicketNotificationItem[]
  >([]);
  const colorInputRefs = useRef<
    Record<EditableColorKey, HTMLInputElement | null>
  >({
    primary: null,
    bgLayout: null,
    bgContainer: null,
    borderColor: null,
  });
  const themeColorSaveTimerRef = useRef<number | null>(null);
  const themeConfigSaveIDRef = useRef(0);

  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const { token } = antTheme.useToken();
  const screens = useBreakpoint();

  const themeMode = useUiStore((state) => state.themeMode);
  const setTheme = useUiStore((state) => state.setTheme);
  const sidebarCollapsedPreference = useUiStore(
    (state) => state.sidebarCollapsed,
  );
  const setSidebarCollapsed = useUiStore((state) => state.setSidebarCollapsed);

  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const logout = useAuthStore((state) => state.logout);

  const isAdmin = Boolean(user?.roles?.includes("admin"));
  const canAccessAcceptance = Boolean(
    user?.roles?.includes("admin") ||
    user?.roles?.includes("support_specialist"),
  );
  const notificationsConfig = useMemo(() => {
    const cfg = (user?.profile_config || {}) as {
      notifications?: {
        personal_enabled?: boolean;
        common_enabled?: boolean;
        common_ticket_updates?: boolean;
        common_comments?: boolean;
        common_deferred_due?: boolean;
        ticket_subscriptions_only?: boolean;
      };
      tickets?: {
        subscriptions?: string[];
      };
    };
    const subscriptions = Array.isArray(cfg.tickets?.subscriptions)
      ? cfg.tickets?.subscriptions
          .map((item) => String(item).trim())
          .filter(Boolean)
      : [];
    return {
      personalEnabled: cfg.notifications?.personal_enabled !== false,
      commonEnabled: cfg.notifications?.common_enabled !== false,
      commonTicketUpdates:
        cfg.notifications?.common_ticket_updates !== false ||
        cfg.notifications?.common_comments !== false ||
        cfg.notifications?.ticket_subscriptions_only === true,
      commonDeferredDue: cfg.notifications?.common_deferred_due !== false,
      subscriptions,
    };
  }, [user?.profile_config]);

  const lightPalette = useMemo(() => {
    const palette = paletteFromProfileConfig(user?.profile_config, "light");
    return {
      primary: normalizeColor(
        palette.primary,
        defaultThemePalettes.light.primary,
      ),
      bgLayout: normalizeColor(
        palette.bgLayout,
        defaultThemePalettes.light.bgLayout,
      ),
      bgContainer: normalizeColor(
        palette.bgContainer,
        defaultThemePalettes.light.bgContainer,
      ),
      borderColor: normalizeColor(
        palette.borderColor,
        defaultThemePalettes.light.borderColor,
      ),
    };
  }, [user?.profile_config]);

  const darkPalette = useMemo(() => {
    const palette = paletteFromProfileConfig(user?.profile_config, "dark");
    return {
      primary: normalizeColor(
        palette.primary,
        defaultThemePalettes.dark.primary,
      ),
      bgLayout: normalizeColor(
        palette.bgLayout,
        defaultThemePalettes.dark.bgLayout,
      ),
      bgContainer: normalizeColor(
        palette.bgContainer,
        defaultThemePalettes.dark.bgContainer,
      ),
      borderColor: normalizeColor(
        palette.borderColor,
        defaultThemePalettes.dark.borderColor,
      ),
    };
  }, [user?.profile_config]);

  const activePalette = themeMode === "light" ? lightPalette : darkPalette;
  const sidebarCollapsed = !screens.lg || sidebarCollapsedPreference;
  const isMobileLayout = !screens.md;
  const hasCustomHeaderControls = Boolean(headerConfig?.controls);
  const shouldShowHeaderActions = !isMobileLayout;
  const fallbackNotificationTitle = t("layout:notifications.ticketEvent");
  const systemSourceLabel = t("common:states.system");

  const resolveSettingsErrorMessage = useCallback(
    (fallbackKey: string, error: unknown) => {
      const errorDetails = String(
        (error as any)?.response?.data?.error?.error || "",
      ).trim();
      const fallbackMessage = t(fallbackKey);
      return errorDetails
        ? t("common:errors.withDetails", {
            message: fallbackMessage,
            details: errorDetails,
          })
        : fallbackMessage;
    },
    [t],
  );

  const pushNotification = useCallback(
    (payload: TicketRealtimePayload) => {
      const ticketID = String(payload.ticket_id || "").trim();
      if (!ticketID) {
        return;
      }

      const action = String(payload.action || "").trim();
      const recipientUserID = Number(payload.recipient_user_id || 0);
      const actorUserID = Number(payload.actor_user_id || 0);
      const isPersonalRecipient = Boolean(
        user?.id && recipientUserID > 0 && Number(user.id) === recipientUserID,
      );
      const isTicketUpdateAction =
        action.includes("status") ||
        action.includes("description") ||
        action.includes("comment");
      const isDeferredDue = action === "ticket_deferred_due";
      const isSubscriptionMatch =
        notificationsConfig.subscriptions.includes(ticketID);
      const isActorSelf = Boolean(
        user?.id && actorUserID > 0 && Number(user.id) === actorUserID,
      );

      const item: TicketNotificationItem = {
        id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
        ticketID,
        action,
        source: String(payload.source || "").trim() || "system",
        message: String(payload.message || "").trim(),
        occurredAt: String(payload.occurred_at || new Date().toISOString()),
      };

      const allowCommonByType = isDeferredDue
        ? notificationsConfig.commonDeferredDue
        : notificationsConfig.commonTicketUpdates;
      const shouldShowPersonal =
        isPersonalRecipient && notificationsConfig.personalEnabled;
      const shouldShowCommon =
        notificationsConfig.commonEnabled &&
        allowCommonByType &&
        isSubscriptionMatch &&
        !(isTicketUpdateAction && isActorSelf);

      if (shouldShowPersonal || shouldShowCommon) {
        setTicketNotifications((prev) =>
          [item, ...prev].slice(0, MAX_TICKET_NOTIFICATIONS),
        );
      }

      if (!shouldShowCommon) {
        void queryClient.invalidateQueries({ queryKey: ["tickets"] });
        void queryClient.invalidateQueries({ queryKey: ["ticket", ticketID] });
        return;
      }

      void queryClient.invalidateQueries({ queryKey: ["tickets"] });
      void queryClient.invalidateQueries({ queryKey: ["ticket", ticketID] });
    },
    [notificationsConfig, queryClient, user?.id],
  );

  useTicketRealtime(pushNotification);

  const updateConfigMutation = useMutation({
    mutationFn: (payload: { profile_config: Record<string, unknown> }) =>
      profileApi.updateConfig(payload),
  });

  const clearThemeColorSaveTimer = useCallback(() => {
    if (themeColorSaveTimerRef.current === null) {
      return;
    }
    window.clearTimeout(themeColorSaveTimerRef.current);
    themeColorSaveTimerRef.current = null;
  }, []);

  const persistProfileConfig = async (
    nextMode: ThemeMode,
    nextPalettes?: Record<
      ThemeMode,
      Pick<ThemePalette, "primary" | "bgLayout" | "bgContainer" | "borderColor">
    >,
  ) => {
    clearThemeColorSaveTimer();
    themeConfigSaveIDRef.current += 1;

    if (!user) {
      setTheme(nextMode);
      return;
    }

    const nextValues = nextPalettes || {
      light: lightPalette,
      dark: darkPalette,
    };

    const nextConfig = buildProfileConfigWithPalettes(
      user.profile_config,
      nextValues,
      nextMode,
    );
    const prevUser = user;

    setTheme(nextMode);
    setUser({ ...user, profile_config: nextConfig });

    try {
      const response = await updateConfigMutation.mutateAsync({
        profile_config: nextConfig,
      });
      const dtoUser = (response as any)?.data;
      if (dtoUser && typeof dtoUser === "object" && "id" in dtoUser) {
        setUser({ ...prevUser, ...dtoUser });
      }
    } catch (error: any) {
      setUser(prevUser);
      setTheme(themeMode);
      message.error(
        resolveSettingsErrorMessage(
          "layout:notifications.themeSaveError",
          error,
        ),
      );
    }
  };

  const handleToggleThemeMode = async () => {
    const nextMode: ThemeMode = themeMode === "light" ? "dark" : "light";
    await persistProfileConfig(nextMode);
  };

  const handleSetColor = (key: EditableColorKey, value: string) => {
    const nextLight = { ...lightPalette };
    const nextDark = { ...darkPalette };
    const target = themeMode === "light" ? nextLight : nextDark;

    target[key] = value;

    if (!user) {
      setTheme(themeMode);
      return;
    }

    const nextConfig = buildProfileConfigWithPalettes(
      user.profile_config,
      {
        light: nextLight,
        dark: nextDark,
      },
      themeMode,
    );
    const prevUser = user;
    const saveID = themeConfigSaveIDRef.current + 1;
    themeConfigSaveIDRef.current = saveID;

    setUser({ ...user, profile_config: nextConfig });
    clearThemeColorSaveTimer();
    themeColorSaveTimerRef.current = window.setTimeout(() => {
      themeColorSaveTimerRef.current = null;
      void updateConfigMutation
        .mutateAsync({
          profile_config: nextConfig,
        })
        .then((response) => {
          if (themeConfigSaveIDRef.current !== saveID) {
            return;
          }
          const dtoUser = (response as any)?.data;
          if (dtoUser && typeof dtoUser === "object" && "id" in dtoUser) {
            setUser({ ...prevUser, ...dtoUser });
          }
        })
        .catch((error: unknown) => {
          if (themeConfigSaveIDRef.current !== saveID) {
            return;
          }
          setUser(prevUser);
          setTheme(themeMode);
          message.error(
            resolveSettingsErrorMessage(
              "layout:notifications.themeSaveError",
              error,
            ),
          );
        });
    }, THEME_COLOR_SAVE_DEBOUNCE_MS);
  };

  useEffect(() => () => {
    clearThemeColorSaveTimer();
  }, [clearThemeColorSaveTimer]);

  useEffect(() => {
    if (screens.lg && mobileNavGroupKey) {
      setMobileNavGroupKey(null);
    }
  }, [mobileNavGroupKey, screens.lg]);

  const handleMenuClick = (key: string, closeMobileNav = false) => {
    navigate(key);
    if (closeMobileNav) {
      setMobileNavGroupKey(null);
    }
  };

  const selectedMenuKey = (() => {
    if (location.pathname.startsWith("/admin/telephony")) {
      return "/admin/telephony";
    }
    if (location.pathname.startsWith("/admin/translations")) {
      return "/admin/translations";
    }
    if (location.pathname.startsWith("/admin/synchronizations")) {
      return "/admin/synchronizations";
    }
    if (location.pathname.startsWith("/admin/users")) {
      return "/admin/users";
    }
    if (location.pathname.startsWith("/telephony/users/")) {
      return "/tickets";
    }
    if (location.pathname.startsWith("/info")) {
      return "/info";
    }
    return location.pathname;
  })();

  const handleLogout = () => {
    logout();
    navigate("/login");
  };

  const dismissNotification = useCallback((id: string) => {
    setTicketNotifications((prev) => prev.filter((item) => item.id !== id));
  }, []);

  const userMenu = {
    items: [
      {
        key: "profile",
        label: t("layout:userMenu.profile"),
        icon: <UserOutlined />,
        onClick: () => navigate("/profile"),
      },
      {
        key: "logout",
        label: t("layout:userMenu.logout"),
        icon: <LogoutOutlined />,
        onClick: handleLogout,
      },
    ],
  };

  const companiesChildren = [
    { key: "/companies", label: t("layout:menu.companies") },
    { key: "/info", label: t("layout:menu.knowledgeBase") },
  ];

  if (canAccessAcceptance) {
    companiesChildren.push({
      key: "/acceptance",
      label: t("layout:menu.acceptance"),
    });
    companiesChildren.push({
      key: "/network-acceptance",
      label: t("layout:menu.networkAcceptance"),
    });
  }

  const equipmentChildren = [
    { key: "/servers", label: t("layout:menu.servers") },
    { key: "/workstations", label: t("layout:menu.workstations") },
    { key: "/fiscals", label: t("layout:menu.fiscals") },
    { key: "/agents", label: t("layout:menu.agents") },
  ];

  if (canAccessAcceptance) {
    equipmentChildren.push({
      key: "/agent-observations",
      label: t("layout:menu.agentObservations"),
    });
  }

  const menuItems = [
    {
      key: "/",
      icon: <HomeOutlined />,
      label: t("layout:menu.home"),
    },
    {
      key: "/tickets",
      icon: <CustomerServiceOutlined />,
      label: t("layout:menu.tickets"),
    },
    {
      key: "companies",
      icon: <BankOutlined />,
      label: t("layout:menu.companies"),
      children: companiesChildren,
    },
    {
      key: "equipment",
      icon: <DesktopOutlined />,
      label: t("layout:menu.equipment"),
      children: equipmentChildren,
    },
  ];

  if (isAdmin) {
    menuItems.push({
      key: "admin",
      icon: <SettingOutlined />,
      label: t("layout:menu.admin"),
      children: [
        { key: "/admin/users", label: t("layout:menu.users") },
        {
          key: "/admin/translations",
          label: t("layout:menu.translations"),
        },
        {
          key: "/admin/telephony",
          label: t("layout:menu.telephony"),
        },
        {
          key: "/admin/synchronizations",
          label: t("layout:menu.synchronizations"),
        },
        { key: "/tasks", label: t("layout:menu.issues") },
        {
          key: "/reports/companies-contracts",
          label: t("layout:menu.companyContractsReport"),
        },
      ],
    });
  }

  const mobileNavGroup = menuItems.find(
    (item) => String(item.key) === mobileNavGroupKey,
  );
  const getMenuLabelText = (label: React.ReactNode) =>
    typeof label === "string" ? label : "";
  const handleMobileRootClick = (item: (typeof menuItems)[number]) => {
    if (item.children?.length) {
      setMobileNavGroupKey(String(item.key));
      return;
    }
    handleMenuClick(String(item.key));
  };
  const isMobileRootActive = (item: (typeof menuItems)[number]) =>
    selectedMenuKey === item.key ||
    Boolean(item.children?.some((child) => child.key === selectedMenuKey));

  const themeMenuContent = (
    <div style={{ width: 160 }}>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 8,
        }}
      >
        <Text strong>{t("layout:theme.title")}</Text>
      </div>

      <Segmented
        block
        size="small"
        value={themeMode}
        options={[
          { label: t("layout:theme.light"), value: "light" },
          { label: t("layout:theme.dark"), value: "dark" },
        ]}
        onChange={(value) => {
          const nextMode = value as ThemeMode;
          if (nextMode !== themeMode) {
            void handleToggleThemeMode();
          }
        }}
      />

      <Divider style={{ margin: "8px 0" }} />

      <Space direction="vertical" size={8} style={{ width: "100%" }}>
        {(Object.keys(colorLabelKeys) as EditableColorKey[]).map((key) => {
          const colorLabel = t(colorLabelKeys[key]);
          return (
            <div
              key={key}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
              }}
            >
              <Text type="secondary" style={{ fontSize: 12 }}>
                {colorLabel}
              </Text>
              <div style={{ position: "relative", width: 20, height: 20 }}>
                <input
                  ref={(node) => {
                    colorInputRefs.current[key] = node;
                  }}
                  type="color"
                  value={activePalette[key]}
                  onChange={(event) =>
                    void handleSetColor(key, event.target.value)
                  }
                  style={{
                    position: "absolute",
                    opacity: 0,
                    pointerEvents: "none",
                    width: 1,
                    height: 1,
                  }}
                />
                <button
                  type="button"
                  onClick={() => colorInputRefs.current[key]?.click()}
                  title={t("layout:theme.colorSwatchTitle", {
                    label: colorLabel,
                    value: activePalette[key],
                  })}
                  style={{
                    width: 20,
                    height: 20,
                    borderRadius: 999,
                    border: `2px solid ${token.colorBorderSecondary}`,
                    background: activePalette[key],
                    cursor: "pointer",
                    padding: 0,
                  }}
                />
              </div>
            </div>
          );
        })}
      </Space>

      <Divider style={{ margin: "8px 0" }} />

      <div>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {t("layout:theme.presets")}
        </Text>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "1fr 1fr",
            gap: 6,
            marginTop: 6,
          }}
        >
          {palettePresets.map((preset) => (
            <Button
              key={preset.key}
              size="small"
              onClick={() =>
                void persistProfileConfig(themeMode, {
                  light: preset.light,
                  dark: preset.dark,
                })
              }
              loading={updateConfigMutation.isPending}
              style={{ paddingInline: 6 }}
            >
              <Space size={6}>
                <span
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: 999,
                    background:
                      themeMode === "light"
                        ? preset.light.primary
                        : preset.dark.primary,
                    border: "1px solid #00000022",
                  }}
                />
                <span
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: 999,
                    background:
                      themeMode === "light"
                        ? preset.light.bgLayout
                        : preset.dark.bgLayout,
                    border: "1px solid #00000022",
                  }}
                />
                <Text style={{ fontSize: 12 }}>{t(preset.labelKey)}</Text>
              </Space>
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
  const inlineHeaderAddon =
    shouldShowHeaderActions && headerAddonPlacement === "inline"
      ? headerAddon
      : null;
  const belowHeaderAddon =
    shouldShowHeaderActions && headerAddonPlacement === "below"
      ? headerAddon
      : null;

  return (
    <Layout style={{ minHeight: "100dvh" }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={sidebarCollapsed}
        width={250}
        style={{
          display: screens.lg ? undefined : "none",
          position: "sticky",
          top: 0,
          alignSelf: "flex-start",
          height: "100dvh",
          overflowY: "auto",
          background: token.colorBgContainer,
          borderRight: `1px solid ${token.colorBorderSecondary}`,
          backdropFilter: "blur(10px)",
        }}
      >
        <div
          style={{
            minHeight: 64,
            margin: 16,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
          }}
        >
          <GlobalSearchLauncher
            collapsed={sidebarCollapsed}
            sidebarWidth={sidebarCollapsed ? 80 : 250}
          />
        </div>
        <Menu
          mode="inline"
          selectedKeys={[selectedMenuKey]}
          items={menuItems}
          onClick={({ key }) => handleMenuClick(key)}
          style={{ borderRight: 0, background: "transparent" }}
        />
      </Sider>
      <Drawer
        className="mobile-navigation-drawer"
        title={
          mobileNavGroup
            ? getMenuLabelText(mobileNavGroup.label)
            : t("layout:navigation.mobileTitle")
        }
        placement="bottom"
        height={isMobileLayout ? "min(68dvh, 520px)" : undefined}
        open={!screens.lg && Boolean(mobileNavGroup)}
        onClose={() => setMobileNavGroupKey(null)}
        destroyOnHidden
        aria-label={t("layout:navigation.mobileTitle")}
      >
        <Menu
          mode="inline"
          selectedKeys={[selectedMenuKey]}
          items={mobileNavGroup?.children || []}
          onClick={({ key }) => handleMenuClick(key, true)}
          style={{ borderRight: 0 }}
        />
      </Drawer>
      <Layout>
        <div
          style={{
            position: "sticky",
            top: 0,
            zIndex: 40,
            background: token.colorBgContainer,
            backdropFilter: "blur(10px)",
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
            overflow: "visible",
          }}
        >
          <Header
            className="app-main-header"
            style={{
              padding: screens.md ? "0 24px" : "0 12px",
              background: "transparent",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              overflow: "visible",
            }}
          >
            <div
              className="app-header-left"
              style={{
                display: shouldShowHeaderActions ? "flex" : "none",
                alignItems: "center",
              }}
            >
              <Button
                type="text"
                icon={
                  sidebarCollapsed ? (
                    <MenuUnfoldOutlined />
                  ) : (
                    <MenuFoldOutlined />
                  )
                }
                onClick={() => {
                  if (!screens.lg) {
                    return;
                  }
                  setSidebarCollapsed(!sidebarCollapsedPreference);
                }}
                aria-label={t("layout:accessibility.toggleSidebar")}
                style={{
                  fontSize: "16px",
                  width: screens.md ? 64 : 40,
                  height: 64,
                }}
              />
            </div>

            <div
              className="app-header-center"
              style={{
                flex: 1,
                display: "flex",
                justifyContent: "center",
                minWidth: 0,
                overflow: "visible",
                paddingInlineEnd: shouldShowHeaderActions ? (screens.md ? 16 : 8) : 0,
              }}
            >
              <div
                style={{
                  width: "100%",
                  display: "grid",
                  gridTemplateColumns:
                    shouldShowHeaderActions
                      ? "minmax(0, 1fr) minmax(0, 920px) minmax(0, 1fr)"
                      : "minmax(0, 1fr)",
                  alignItems: "center",
                  columnGap: screens.md ? 12 : 8,
                  minWidth: 0,
                  overflow: "visible",
                }}
              >
                <div
                  style={{
                    display: shouldShowHeaderActions ? "flex" : "none",
                    alignItems: "center",
                    minWidth: 0,
                    overflow: "visible",
                  }}
                >
                  {inlineHeaderAddon ? (
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "flex-start",
                        minWidth: 0,
                      }}
                    >
                      {inlineHeaderAddon}
                    </div>
                  ) : null}
                </div>
                <div
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    minWidth: 0,
                    overflow: "visible",
                  }}
                >
                  {hasCustomHeaderControls ? (
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "center",
                        gap: 8,
                        maxWidth: "100%",
                        width: "100%",
                        minWidth: 0,
                      }}
                    >
                      {headerConfig?.controls}
                    </div>
                  ) : (
                    <div
                      style={{
                        minWidth: 0,
                        width: "100%",
                        display: "flex",
                        justifyContent: "center",
                      }}
                    >
                      <HeaderSearch />
                    </div>
                  )}
                </div>
                <div
                  aria-hidden
                  style={{
                    display: shouldShowHeaderActions ? undefined : "none",
                    minWidth: 0,
                  }}
                />
              </div>
            </div>

            <Space
              size={screens.md ? "middle" : "small"}
              className="app-header-right"
              style={{
                display: shouldShowHeaderActions ? undefined : "none",
                minWidth: 0,
              }}
            >
              <Button
                shape="circle"
                icon={<HomeOutlined />}
                onClick={() => navigate("/")}
                aria-label={t("layout:accessibility.openHome")}
              />
              <Popover
                trigger="click"
                placement="leftTop"
                align={{ offset: [0, 0] }}
                arrow={false}
                open={themeMenuOpen}
                onOpenChange={setThemeMenuOpen}
                content={themeMenuContent}
              >
                <Button
                  shape="circle"
                  icon={
                    themeMode === "light" ? <MoonOutlined /> : <SunOutlined />
                  }
                  aria-label={t("layout:accessibility.openThemeSettings")}
                />
              </Popover>

              <Dropdown menu={userMenu} placement="bottomRight" arrow>
                <Space
                  className="app-header-user-trigger"
                  style={{ cursor: "pointer", minWidth: 0 }}
                >
                  {user && (
                    <Text className="app-header-user-text">
                      {user.first_name} {user.last_name} • {user.schedule_type}
                    </Text>
                  )}
                  <Avatar style={{ backgroundColor: token.colorPrimary }}>
                    {user?.full_name?.[0] || "A"}
                  </Avatar>
                </Space>
              </Dropdown>
            </Space>
          </Header>
          {belowHeaderAddon ? (
            <div
              style={{
                padding: screens.md ? "8px 24px 12px" : "8px 12px 12px",
                borderTop: `1px solid ${token.colorBorderSecondary}`,
              }}
            >
              <div style={{ maxWidth: "100%" }}>{belowHeaderAddon}</div>
            </div>
          ) : null}
        </div>
        <Content
          style={{
            margin: screens.md ? "24px 16px" : "12px 8px",
            padding: screens.md ? 24 : "8px 8px calc(76px + env(safe-area-inset-bottom))",
            minHeight: 280,
            overflow: "initial",
            position: "relative",
          }}
        >
          <LayoutHeaderContext.Provider
            value={{
              headerConfig,
              setHeaderConfig,
              headerAddon,
              setHeaderAddon,
              headerAddonPlacement,
              setHeaderAddonPlacement,
            }}
          >
            <Outlet />
          </LayoutHeaderContext.Provider>
          <div id="inline-message-host" aria-live="polite" />
          {ticketNotifications.length > 0 && (
            <div className="inline-notification-stack" aria-live="polite">
              {ticketNotifications.slice(0, 6).map((item) => (
                <article
                  key={item.id}
                  className="inline-notification-card"
                  onClick={() => navigate(`/tickets/${item.ticketID}`)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      navigate(`/tickets/${item.ticketID}`);
                    }
                  }}
                >
                  <div className="inline-notification-card__head">
                    <Tag color="processing" style={{ marginInlineEnd: 0 }}>
                      {t("layout:notifications.ticketTag", {
                        ticketID: item.ticketID,
                      })}
                    </Tag>
                    <Button
                      type="text"
                      size="small"
                      icon={<CloseOutlined />}
                      onClick={(event) => {
                        event.stopPropagation();
                        dismissNotification(item.id);
                      }}
                      aria-label={t("layout:notifications.dismiss")}
                    />
                  </div>
                  <Text className="inline-notification-card__text">
                    {renderTicketNotificationTitle(
                      item,
                      fallbackNotificationTitle,
                    )}
                  </Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {formatLocaleDateTime(item.occurredAt, locale)} -{" "}
                    {item.source || systemSourceLabel}
                  </Text>
                </article>
              ))}
            </div>
          )}
        </Content>
        {!screens.lg && (
          <div
            className="app-mobile-action-bar"
            role="navigation"
            aria-label={t("layout:navigation.mobileActions")}
          >
            {menuItems.map((item) => {
              const label = getMenuLabelText(item.label);
              return (
                <Button
                  key={String(item.key)}
                  type={isMobileRootActive(item) ? "primary" : "text"}
                  icon={item.icon}
                  onClick={() => handleMobileRootClick(item)}
                  aria-label={
                    item.children?.length
                      ? t("layout:accessibility.openMobileNavGroup", { name: label })
                      : label
                  }
                >
                  {label}
                </Button>
              );
            })}
          </div>
        )}
      </Layout>
    </Layout>
  );
};

export default MainLayout;
