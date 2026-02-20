import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Layout, Menu, Button, Dropdown, Avatar, theme as antTheme, Typography, Space, Popover, Divider, message, Segmented, Grid, Tag } from 'antd';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import {
  SearchOutlined,
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
} from '@ant-design/icons';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import HeaderSearch from '@/components/common/HeaderSearch';
import { useUiStore } from '@/store/uiStore';
import { useAuthStore } from '@/store/authStore';
import { profileApi } from '@/api/profile';
import { LayoutHeaderContext, type ReportHeaderConfig } from '@/components/layout/LayoutHeaderContext';
import { buildProfileConfigWithPalettes, paletteFromProfileConfig } from '@/theme/profileConfig';
import { defaultThemePalettes, type ThemeMode, type ThemePalette } from '@/theme/themeConfig';
import { useTicketRealtime, type TicketRealtimePayload } from '@/features/realtime/useTicketRealtime';

const { Header, Sider, Content } = Layout;
const { Text } = Typography;
const { useBreakpoint } = Grid;

type EditableColorKey = 'primary' | 'bgLayout' | 'bgContainer' | 'borderColor';

type TicketNotificationItem = {
  id: string;
  ticketID: string;
  action: string;
  source: string;
  message: string;
  occurredAt: string;
};

const colorLabels: Record<EditableColorKey, string> = {
  primary: 'Акцент',
  bgLayout: 'Фон страницы',
  bgContainer: 'Фон форм',
  borderColor: 'Границы',
};

const palettePresets: Array<{
  key: string;
  label: string;
  light: Pick<ThemePalette, 'primary' | 'bgLayout' | 'bgContainer' | 'borderColor'>;
  dark: Pick<ThemePalette, 'primary' | 'bgLayout' | 'bgContainer' | 'borderColor'>;
}> = [
  {
    key: 'classic',
    label: 'Классика',
    light: { primary: '#1677ff', bgLayout: '#f0f2f5', bgContainer: '#ffffff', borderColor: '#d9d9d9' },
    dark: { primary: '#177ddc', bgLayout: '#000000', bgContainer: '#141414', borderColor: '#303030' },
  },
  {
    key: 'mint',
    label: 'Мята',
    light: { primary: '#13c2c2', bgLayout: '#eefaf9', bgContainer: '#ffffff', borderColor: '#a8d8d8' },
    dark: { primary: '#36cfc9', bgLayout: '#0b1516', bgContainer: '#111f20', borderColor: '#245054' },
  },
  {
    key: 'amber',
    label: 'Янтарь',
    light: { primary: '#faad14', bgLayout: '#fff8e6', bgContainer: '#fffdf7', borderColor: '#e8d3a3' },
    dark: { primary: '#d89614', bgLayout: '#1a1408', bgContainer: '#241b0c', borderColor: '#5e4a1d' },
  },
  {
    key: 'graphite',
    label: 'Графит',
    light: { primary: '#595959', bgLayout: '#f5f5f5', bgContainer: '#ffffff', borderColor: '#bfbfbf' },
    dark: { primary: '#8c8c8c', bgLayout: '#0f0f0f', bgContainer: '#1a1a1a', borderColor: '#3a3a3a' },
  },
];

const normalizeColor = (value: string | undefined, fallback: string) => {
  const candidate = String(value || '').trim().toLowerCase();
  return /^#[\da-f]{6}$/.test(candidate) ? candidate : fallback;
};

const MAX_TICKET_NOTIFICATIONS = 50;

const renderTicketNotificationTitle = (item: TicketNotificationItem) => {
  if (item.message.trim()) {
    return item.message.trim();
  }
  return 'Событие по тикету';
};

const MainLayout: React.FC = () => {
  const [themeMenuOpen, setThemeMenuOpen] = useState(false);
  const [headerConfig, setHeaderConfig] = useState<ReportHeaderConfig | null>(null);
  const [ticketNotifications, setTicketNotifications] = useState<TicketNotificationItem[]>([]);
  const colorInputRefs = useRef<Record<EditableColorKey, HTMLInputElement | null>>({
    primary: null,
    bgLayout: null,
    bgContainer: null,
    borderColor: null,
  });

  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const { token } = antTheme.useToken();
  const screens = useBreakpoint();

  const themeMode = useUiStore((state) => state.themeMode);
  const setTheme = useUiStore((state) => state.setTheme);
  const sidebarCollapsedPreference = useUiStore((state) => state.sidebarCollapsed);
  const setSidebarCollapsed = useUiStore((state) => state.setSidebarCollapsed);

  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const logout = useAuthStore((state) => state.logout);

  const isAdmin = Boolean(user?.roles?.includes('admin'));
  const canAccessAcceptance = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
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
      ? cfg.tickets?.subscriptions.map((item) => String(item).trim()).filter(Boolean)
      : [];
    return {
      personalEnabled: cfg.notifications?.personal_enabled !== false,
      commonEnabled: cfg.notifications?.common_enabled !== false,
      commonTicketUpdates:
        cfg.notifications?.common_ticket_updates !== false
        || cfg.notifications?.common_comments !== false
        || cfg.notifications?.ticket_subscriptions_only === true,
      commonDeferredDue: cfg.notifications?.common_deferred_due !== false,
      subscriptions,
    };
  }, [user?.profile_config]);

  const lightPalette = useMemo(() => {
    const palette = paletteFromProfileConfig(user?.profile_config, 'light');
    return {
      primary: normalizeColor(palette.primary, defaultThemePalettes.light.primary),
      bgLayout: normalizeColor(palette.bgLayout, defaultThemePalettes.light.bgLayout),
      bgContainer: normalizeColor(palette.bgContainer, defaultThemePalettes.light.bgContainer),
      borderColor: normalizeColor(palette.borderColor, defaultThemePalettes.light.borderColor),
    };
  }, [user?.profile_config]);

  const darkPalette = useMemo(() => {
    const palette = paletteFromProfileConfig(user?.profile_config, 'dark');
    return {
      primary: normalizeColor(palette.primary, defaultThemePalettes.dark.primary),
      bgLayout: normalizeColor(palette.bgLayout, defaultThemePalettes.dark.bgLayout),
      bgContainer: normalizeColor(palette.bgContainer, defaultThemePalettes.dark.bgContainer),
      borderColor: normalizeColor(palette.borderColor, defaultThemePalettes.dark.borderColor),
    };
  }, [user?.profile_config]);

  const activePalette = themeMode === 'light' ? lightPalette : darkPalette;
  const sidebarCollapsed = !screens.lg || sidebarCollapsedPreference;
  const isReportHeader = headerConfig?.mode === 'reports';

  useEffect(() => {
    if (!location.pathname.startsWith('/reports/')) {
      setHeaderConfig(null);
    }
  }, [location.pathname]);

  const pushNotification = useCallback((payload: TicketRealtimePayload) => {
    const ticketID = String(payload.ticket_id || '').trim();
    if (!ticketID) {
      return;
    }

    const action = String(payload.action || '').trim();
    const recipientUserID = Number(payload.recipient_user_id || 0);
    const actorUserID = Number(payload.actor_user_id || 0);
    const isPersonalRecipient = Boolean(user?.id && recipientUserID > 0 && Number(user.id) === recipientUserID);
    const isTicketUpdateAction =
      action.includes('status')
      || action.includes('description')
      || action.includes('comment');
    const isDeferredDue = action === 'ticket_deferred_due';
    const isSubscriptionMatch = notificationsConfig.subscriptions.includes(ticketID);
    const isActorSelf = Boolean(user?.id && actorUserID > 0 && Number(user.id) === actorUserID);

    const item: TicketNotificationItem = {
      id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
      ticketID,
      action,
      source: String(payload.source || '').trim() || 'system',
      message: String(payload.message || '').trim(),
      occurredAt: String(payload.occurred_at || new Date().toISOString()),
    };

    const allowCommonByType = isDeferredDue
      ? notificationsConfig.commonDeferredDue
      : notificationsConfig.commonTicketUpdates;
    const shouldShowPersonal = isPersonalRecipient && notificationsConfig.personalEnabled;
    const shouldShowCommon =
      notificationsConfig.commonEnabled
      && allowCommonByType
      && isSubscriptionMatch
      && !(isTicketUpdateAction && isActorSelf);

    if (shouldShowPersonal || shouldShowCommon) {
      setTicketNotifications((prev) => [item, ...prev].slice(0, MAX_TICKET_NOTIFICATIONS));
    }

    if (!shouldShowCommon) {
      void queryClient.invalidateQueries({ queryKey: ['tickets'] });
      void queryClient.invalidateQueries({ queryKey: ['ticket', ticketID] });
      return;
    }

    void queryClient.invalidateQueries({ queryKey: ['tickets'] });
    void queryClient.invalidateQueries({ queryKey: ['ticket', ticketID] });
  }, [notificationsConfig, queryClient, user?.id]);

  useTicketRealtime(pushNotification);

  const updateConfigMutation = useMutation({
    mutationFn: (payload: { profile_config: Record<string, unknown> }) => profileApi.updateConfig(payload),
  });

  const persistProfileConfig = async (
    nextMode: ThemeMode,
    nextPalettes?: Record<ThemeMode, Pick<ThemePalette, 'primary' | 'bgLayout' | 'bgContainer' | 'borderColor'>>,
  ) => {
    if (!user) {
      setTheme(nextMode);
      return;
    }

    const nextValues = nextPalettes || {
      light: lightPalette,
      dark: darkPalette,
    };

    const nextConfig = buildProfileConfigWithPalettes(user.profile_config, nextValues, nextMode);
    const prevUser = user;

    setTheme(nextMode);
    setUser({ ...user, profile_config: nextConfig });

    try {
      const response = await updateConfigMutation.mutateAsync({ profile_config: nextConfig });
      const dtoUser = (response as any)?.data;
      if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
        setUser({ ...prevUser, ...dtoUser });
      }
    } catch (error: any) {
      setUser(prevUser);
      setTheme(themeMode);
      message.error(error?.response?.data?.error?.error || 'Не удалось сохранить цветовые настройки');
    }
  };

  const handleToggleThemeMode = async () => {
    const nextMode: ThemeMode = themeMode === 'light' ? 'dark' : 'light';
    await persistProfileConfig(nextMode);
  };

  const handleSetColor = async (key: EditableColorKey, value: string) => {
    const nextLight = { ...lightPalette };
    const nextDark = { ...darkPalette };
    const target = themeMode === 'light' ? nextLight : nextDark;

    target[key] = value;

    await persistProfileConfig(themeMode, {
      light: nextLight,
      dark: nextDark,
    });
  };

  const handleMenuClick = (key: string) => {
    navigate(key);
  };

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const dismissNotification = useCallback((id: string) => {
    setTicketNotifications((prev) => prev.filter((item) => item.id !== id));
  }, []);

  const userMenu = {
    items: [
      {
        key: 'profile',
        label: 'Профиль',
        icon: <UserOutlined />,
        onClick: () => navigate('/profile'),
      },
      {
        key: 'logout',
        label: 'Выйти',
        icon: <LogoutOutlined />,
        onClick: handleLogout,
      },
    ],
  };

  const companiesChildren = [
    { key: '/companies', label: 'Компании' },
  ];

  if (canAccessAcceptance) {
    companiesChildren.push({ key: '/acceptance', label: 'Принятие на АО' });
    companiesChildren.push({ key: '/network-acceptance', label: 'Принятие в сеть' });
  }

  const equipmentChildren = [
    { key: '/servers', label: 'Серверы' },
    { key: '/workstations', label: 'Рабочие станции' },
    { key: '/fiscals', label: 'ФР' },
  ];

  if (canAccessAcceptance) {
    equipmentChildren.push({ key: '/agent-observations', label: 'Наблюдения агентов' });
  }

  const menuItems = [
    { key: '/', icon: <SearchOutlined />, label: 'Поиск' },
    { key: '/tickets', icon: <CustomerServiceOutlined />, label: 'Тикеты' },
    {
      key: 'companies',
      icon: <BankOutlined />,
      label: 'Компании',
      children: companiesChildren,
    },
    {
      key: 'equipment',
      icon: <DesktopOutlined />,
      label: 'Оборудование',
      children: equipmentChildren,
    },
  ];

  if (isAdmin) {
    menuItems.push({
      key: 'admin',
      icon: <SettingOutlined />,
      label: 'Администрирование',
      children: [
        { key: '/admin', label: 'Настройки' },
        { key: '/tasks', label: 'Проблемы' },
        { key: '/reports/companies-contracts', label: 'Отчеты: Компании и контракты' },
      ],
    });
  }

  const themeMenuContent = (
    <div style={{ width: 160 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
        <Text strong>Оформление</Text>
      </div>

      <Segmented
        block
        size="small"
        value={themeMode}
        options={[
          { label: 'День', value: 'light' },
          { label: 'Ночь', value: 'dark' },
        ]}
        onChange={(value) => {
          const nextMode = value as ThemeMode;
          if (nextMode !== themeMode) {
            void handleToggleThemeMode();
          }
        }}
      />

      <Divider style={{ margin: '8px 0' }} />

      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        {(Object.keys(colorLabels) as EditableColorKey[]).map((key) => (
          <div key={key} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>{colorLabels[key]}</Text>
            <div style={{ position: 'relative', width: 20, height: 20 }}>
              <input
                ref={(node) => {
                  colorInputRefs.current[key] = node;
                }}
                type="color"
                value={activePalette[key]}
                onChange={(event) => void handleSetColor(key, event.target.value)}
                style={{
                  position: 'absolute',
                  opacity: 0,
                  pointerEvents: 'none',
                  width: 1,
                  height: 1,
                }}
              />
              <button
                type="button"
                onClick={() => colorInputRefs.current[key]?.click()}
                title={`${colorLabels[key]}: ${activePalette[key]}`}
                style={{
                  width: 20,
                  height: 20,
                  borderRadius: 999,
                  border: `2px solid ${token.colorBorderSecondary}`,
                  background: activePalette[key],
                  cursor: 'pointer',
                  padding: 0,
                }}
              />
            </div>
          </div>
        ))}
      </Space>

      <Divider style={{ margin: '8px 0' }} />

      <div>
        <Text type="secondary" style={{ fontSize: 12 }}>Пресеты</Text>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6, marginTop: 6 }}>
          {palettePresets.map((preset) => (
            <Button
              key={preset.key}
              size="small"
              onClick={() => void persistProfileConfig(themeMode, { light: preset.light, dark: preset.dark })}
              loading={updateConfigMutation.isPending}
              style={{ paddingInline: 6 }}
            >
              <Space size={6}>
                <span
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: 999,
                    background: themeMode === 'light' ? preset.light.primary : preset.dark.primary,
                    border: '1px solid #00000022',
                  }}
                />
                <span
                  style={{
                    width: 10,
                    height: 10,
                    borderRadius: 999,
                    background: themeMode === 'light' ? preset.light.bgLayout : preset.dark.bgLayout,
                    border: '1px solid #00000022',
                  }}
                />
                <Text style={{ fontSize: 12 }}>{preset.label}</Text>
              </Space>
            </Button>
          ))}
        </div>
      </div>
    </div>
  );

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        trigger={null}
        collapsible
        collapsed={sidebarCollapsed}
        width={250}
        style={{
          background: token.colorBgContainer,
          borderRight: `1px solid ${token.colorBorderSecondary}`,
          backdropFilter: 'blur(10px)',
        }}
      >
        <div style={{ height: 64, margin: 16, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div
            style={{
              width: sidebarCollapsed ? 32 : '100%',
              height: 32,
              background: token.colorPrimary,
              borderRadius: 6,
              opacity: 0.8,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: token.colorTextLightSolid,
              fontWeight: 'bold',
            }}
          >
            {sidebarCollapsed ? 'XD' : 'MyHoreca XenionDesk'}
          </div>
        </div>
        <Menu
          mode="inline"
          defaultSelectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => handleMenuClick(key)}
          style={{ borderRight: 0, background: 'transparent' }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            padding: '0 24px',
            background: token.colorBgContainer,
            backdropFilter: 'blur(10px)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: `1px solid ${token.colorBorderSecondary}`,
            position: 'sticky',
            top: 0,
            zIndex: 10,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center' }}>
            <Button
              type="text"
              icon={sidebarCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => {
                if (!screens.lg) return;
                setSidebarCollapsed(!sidebarCollapsedPreference);
              }}
              style={{ fontSize: '16px', width: 64, height: 64 }}
            />
          </div>

          <div style={{ flex: 1, display: 'flex', justifyContent: 'center', minWidth: 0 }}>
            {isReportHeader ? (
              <div style={{ width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8, maxWidth: '100%', minWidth: 0, overflowX: 'auto' }}>
                  {headerConfig?.controls}
                </div>
              </div>
            ) : (
              <HeaderSearch />
            )}
          </div>

          <Space size="middle">
            <Popover
              trigger="click"
              placement="leftTop"
              align={{ offset: [0, 0] }}
              arrow={false}
              open={themeMenuOpen}
              onOpenChange={setThemeMenuOpen}
              content={themeMenuContent}
            >
              <Button shape="circle" icon={themeMode === 'light' ? <MoonOutlined /> : <SunOutlined />} />
            </Popover>

            <Dropdown menu={userMenu} placement="bottomRight" arrow>
              <Space style={{ cursor: 'pointer' }}>
                {user && (
                  <Text>{user.first_name} {user.last_name} • {user.schedule_type}</Text>
                )}
                <Avatar style={{ backgroundColor: token.colorPrimary }}>
                  {user?.full_name?.[0] || 'A'}
                </Avatar>
              </Space>
            </Dropdown>
          </Space>
        </Header>
        <Content
          style={{
            margin: '24px 16px',
            padding: 24,
            minHeight: 280,
            overflow: 'initial',
            position: 'relative',
          }}
        >
          <LayoutHeaderContext.Provider value={{ headerConfig, setHeaderConfig }}>
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
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      navigate(`/tickets/${item.ticketID}`);
                    }
                  }}
                >
                  <div className="inline-notification-card__head">
                    <Tag color="processing" style={{ marginInlineEnd: 0 }}>{`\u0422\u0438\u043a\u0435\u0442 ${item.ticketID}`}</Tag>
                    <Button
                      type="text"
                      size="small"
                      icon={<CloseOutlined />}
                      onClick={(event) => {
                        event.stopPropagation();
                        dismissNotification(item.id);
                      }}
                      aria-label={'\u0417\u0430\u043a\u0440\u044b\u0442\u044c \u0443\u0432\u0435\u0434\u043e\u043c\u043b\u0435\u043d\u0438\u0435'}
                    />
                  </div>
                  <Text className="inline-notification-card__text">{renderTicketNotificationTitle(item)}</Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {new Date(item.occurredAt).toLocaleString()} - {item.source || 'system'}
                  </Text>
                </article>
              ))}
            </div>
          )}
        </Content>
      </Layout>
    </Layout>
  );
};

export default MainLayout;
