import React, { useEffect, useMemo } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Col,
  Divider,
  Form,
  Input,
  Row,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd';
import { useTranslation } from 'react-i18next';
import { profileApi } from '@/api/profile';
import type { AppLocaleCode } from '@/i18n/localeTypes';
import { useAppLocale } from '@/i18n/useAppLocale';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

type CredentialsForm = {
  username: string;
  password?: string;
  confirmPassword?: string;
  interface_locale?: AppLocaleCode;
  cards_columns?: number;
  notifications_personal_enabled?: boolean;
  notifications_common_enabled?: boolean;
  notifications_common_ticket_updates?: boolean;
  notifications_common_deferred_due?: boolean;
  comments_new_first?: boolean;
  integrations?: Array<{
    integration_type?: string;
    external_id?: string;
    is_locked?: boolean;
    is_verified?: boolean;
    verified_name?: string;
  }>;
};

const integrationOptions = [
  { value: 'bitrix24', label: 'Bitrix24' },
  { value: 'pyrus', label: 'Pyrus' },
  { value: 'naumen', label: 'Naumen' },
  { value: 'telegram', label: 'Telegram' },
];

const ProfilePage: React.FC = () => {
  const { t } = useTranslation(['layout']);
  const { message } = AntdApp.useApp();
  const { locale, setLocale, supportedLocales } = useAppLocale();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const isPyrusEnabled = user?.pyrus_enabled === true;
  const [form] = Form.useForm<CredentialsForm>();
  const availableIntegrationOptions = useMemo(() => integrationOptions.filter((item) => {
    if (item.value === 'bitrix24') {
      return isBitrixEnabled;
    }
    if (item.value === 'pyrus') {
      return isPyrusEnabled;
    }
    return true;
  }), [isBitrixEnabled, isPyrusEnabled]);
  const localeOptions = useMemo(
    () => supportedLocales
      .filter((item) => item.enabled)
      .map((item) => ({
        value: item.code,
        label: item.nativeLabel,
      })),
    [supportedLocales],
  );

  const profileQuery = useQuery({
    queryKey: ['profile-me'],
    queryFn: () => profileApi.getMyProfile(),
  });

  useEffect(() => {
    const dtoUser = profileQuery.data?.data;
    if (!dtoUser) {
      return;
    }
    setUser(dtoUser as any);
  }, [profileQuery.data?.data, setUser]);

  const initialIntegrations = useMemo(() => {
    return (user?.integrations || [])
      .filter((item) => {
        if (item.integration_type === 'bitrix24') {
          return isBitrixEnabled;
        }
        if (item.integration_type === 'pyrus') {
          return isPyrusEnabled;
        }
        return true;
      })
      .map((item) => ({
      integration_type: item.integration_type,
      external_id: item.external_id,
      is_locked: item.is_locked,
      is_verified: item.is_verified,
      verified_name: item.verified_name,
    }));
  }, [isBitrixEnabled, isPyrusEnabled, user?.integrations]);

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
        comments_new_first?: boolean;
      };
      interface?: {
        locale?: string;
        search?: {
          cards_columns?: number;
        };
      };
    };

    return {
      locale: String(cfg.interface?.locale || locale) as AppLocaleCode,
      cardsColumns: Number(cfg.interface?.search?.cards_columns ?? 5),
      personalEnabled: cfg.notifications?.personal_enabled !== false,
      commonEnabled: cfg.notifications?.common_enabled !== false,
      commonTicketUpdates:
        cfg.notifications?.common_ticket_updates !== false
        || cfg.notifications?.common_comments !== false
        || cfg.notifications?.ticket_subscriptions_only === true,
      commonDeferredDue: cfg.notifications?.common_deferred_due !== false,
      commentsNewFirst: cfg.tickets?.comments_new_first !== false,
    };
  }, [locale, user?.profile_config]);

  useEffect(() => {
    form.setFieldsValue({
      username: user?.username || '',
      integrations: initialIntegrations,
      interface_locale: notificationsConfig.locale,
      cards_columns: notificationsConfig.cardsColumns,
      notifications_personal_enabled: notificationsConfig.personalEnabled,
      notifications_common_enabled: notificationsConfig.commonEnabled,
      notifications_common_ticket_updates: notificationsConfig.commonTicketUpdates,
      notifications_common_deferred_due: notificationsConfig.commonDeferredDue,
      comments_new_first: notificationsConfig.commentsNewFirst,
    });
  }, [form, initialIntegrations, notificationsConfig, user?.username]);

  const updateCredentialsMutation = useMutation({
    mutationFn: (payload: { username?: string; password?: string }) => profileApi.updateCredentials(payload),
  });

  const updateIntegrationsMutation = useMutation({
    mutationFn: (payload: { integrations: Array<{ integration_type: string; external_id: string }> }) => profileApi.updateIntegrations(payload),
  });

  const updateConfigMutation = useMutation({
    mutationFn: (payload: { profile_config: Record<string, unknown> }) => profileApi.updateConfig(payload as any),
  });

  const applySuggestionMutation = useMutation({
    mutationFn: () => profileApi.applyBitrixSuggestion(),
    onSuccess: (response) => {
      const dtoUser = response?.data;
      if (!dtoUser) {
        message.error('Не удалось применить интеграцию Bitrix24');
        return;
      }
      setUser(dtoUser as any);
      message.success('Интеграция Bitrix24 добавлена');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось применить интеграцию Bitrix24');
    },
  });

  const applyPyrusSuggestionMutation = useMutation({
    mutationFn: () => profileApi.applyPyrusSuggestion(),
    onSuccess: (response) => {
      const dtoUser = response?.data;
      if (!dtoUser) {
        message.error('Не удалось применить интеграцию Pyrus');
        return;
      }
      setUser(dtoUser as any);
      message.success('Интеграция Pyrus добавлена');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось применить интеграцию Pyrus');
    },
  });

  const onFinish = async (values: CredentialsForm) => {
    if (!user) return;

    const credentialsPayload: { username?: string; password?: string } = {};
    const nextUsername = values.username?.trim();
    if (nextUsername && nextUsername !== user.username) {
      credentialsPayload.username = nextUsername;
    }
    const nextPassword = values.password?.trim();
    if (nextPassword) {
      credentialsPayload.password = nextPassword;
    }

    const normalizedIntegrations = (values.integrations || [])
      .map((item) => ({
        integration_type: String(item.integration_type || '').trim().toLowerCase(),
        external_id: String(item.external_id || '').trim(),
      }))
      .filter((item) => item.integration_type && item.external_id);
    const hiddenIntegrations = (user.integrations || [])
      .filter((item) => {
        if (item.integration_type === 'bitrix24') {
          return !isBitrixEnabled && String(item.external_id || '').trim();
        }
        if (item.integration_type === 'pyrus') {
          return !isPyrusEnabled && String(item.external_id || '').trim();
        }
        return false;
      })
      .map((item) => ({
        integration_type: item.integration_type,
        external_id: item.external_id,
      }));
    const nextIntegrationsPayload = [...normalizedIntegrations, ...hiddenIntegrations];

    const currentIntegrations = (user.integrations || [])
      .filter((item) => {
        if (item.integration_type === 'bitrix24') {
          return isBitrixEnabled;
        }
        if (item.integration_type === 'pyrus') {
          return isPyrusEnabled;
        }
        return true;
      })
      .map((item) => `${item.integration_type}:${item.external_id}`)
      .sort();
    const nextIntegrations = nextIntegrationsPayload
      .map((item) => `${item.integration_type}:${item.external_id}`)
      .sort();
    const integrationsChanged = currentIntegrations.join('|') !== nextIntegrations.join('|');

    const currentColumnsRaw = Number(user.profile_config?.interface?.search?.cards_columns ?? 5);
    const currentColumns = Number.isFinite(currentColumnsRaw)
      ? Math.max(1, Math.min(5, Math.round(currentColumnsRaw)))
      : 5;
    const nextColumnsRaw = Number(values.cards_columns ?? currentColumns);
    const nextColumns = Number.isFinite(nextColumnsRaw)
      ? Math.max(1, Math.min(5, Math.round(nextColumnsRaw)))
      : currentColumns;
    const currentLocale = String(user.profile_config?.interface?.locale || locale) as AppLocaleCode;
    const nextLocale = String(values.interface_locale || currentLocale) as AppLocaleCode;

    const currentNotifications = {
      personal_enabled: (user.profile_config as any)?.notifications?.personal_enabled !== false,
      common_enabled: (user.profile_config as any)?.notifications?.common_enabled !== false,
      common_ticket_updates: (user.profile_config as any)?.notifications?.common_ticket_updates !== false,
      common_deferred_due: (user.profile_config as any)?.notifications?.common_deferred_due !== false,
    };

    const nextNotifications = {
      personal_enabled: values.notifications_personal_enabled !== false,
      common_enabled: values.notifications_common_enabled !== false,
      common_ticket_updates: values.notifications_common_ticket_updates !== false,
      common_deferred_due: values.notifications_common_deferred_due !== false,
      common_comments: values.notifications_common_ticket_updates !== false,
      ticket_subscriptions_only: values.notifications_common_ticket_updates !== false,
      common_equipment_updates: false,
    };

    const currentTicketsConfig = {
      comments_new_first: (user.profile_config as any)?.tickets?.comments_new_first !== false,
    };

    const nextTicketsConfig = {
      comments_new_first: values.comments_new_first !== false,
    };

    const configChanged = (
      nextLocale !== currentLocale
      || nextColumns !== currentColumns
      || JSON.stringify(currentNotifications) !== JSON.stringify(nextNotifications)
      || JSON.stringify(currentTicketsConfig) !== JSON.stringify(nextTicketsConfig)
    );
    const configFieldsTouched = form.isFieldsTouched([
      'interface_locale',
      'cards_columns',
      'comments_new_first',
      'notifications_personal_enabled',
      'notifications_common_enabled',
      'notifications_common_ticket_updates',
      'notifications_common_deferred_due',
    ], false);
    const configNeedsSave = configChanged || configFieldsTouched;

    if (!credentialsPayload.username && !credentialsPayload.password && !integrationsChanged && !configNeedsSave) {
      message.info('Нет изменений для сохранения');
      return;
    }

    try {
      if (credentialsPayload.username || credentialsPayload.password) {
        await updateCredentialsMutation.mutateAsync(credentialsPayload);
      }

      let updatedUser = user;
      if (integrationsChanged) {
        const response = await updateIntegrationsMutation.mutateAsync({ integrations: nextIntegrationsPayload });
        const dtoUser = (response as any)?.data;
        if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
          updatedUser = { ...updatedUser, ...dtoUser };
        } else {
          updatedUser = {
            ...updatedUser,
            integrations: nextIntegrationsPayload.map((item, index) => ({
              id: index + 1,
              integration_type: item.integration_type,
              external_id: item.external_id,
              is_verified: false,
            })),
          };
        }
      }

      if (configNeedsSave) {
        const nextConfig = {
          ...(updatedUser.profile_config || {}),
          interface: {
            ...((updatedUser.profile_config || {}).interface || {}),
            locale: nextLocale,
            search: {
              ...((updatedUser.profile_config || {}).interface?.search || {}),
              cards_columns: nextColumns,
            },
          },
          notifications: {
            ...((updatedUser.profile_config || {}).notifications || {}),
            ...nextNotifications,
          },
          tickets: {
            ...((updatedUser.profile_config || {}).tickets || {}),
            ...nextTicketsConfig,
          },
        };
        const configResponse = await updateConfigMutation.mutateAsync({ profile_config: nextConfig });
        const dtoUser = (configResponse as any)?.data;
        if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
          updatedUser = { ...updatedUser, ...dtoUser };
        } else {
          updatedUser = { ...updatedUser, profile_config: nextConfig };
        }
      }

      if (credentialsPayload.username) {
        updatedUser = { ...updatedUser, username: credentialsPayload.username };
      }

      setUser(updatedUser);
      if (nextLocale !== locale) {
        await setLocale(nextLocale);
      }
      message.success('Профиль обновлён');
      form.setFieldValue('password', undefined);
      form.setFieldValue('confirmPassword', undefined);
    } catch (error: any) {
      message.error(error?.response?.data?.error?.error || 'Не удалось обновить профиль');
    }
  };

  const commonEnabled = Form.useWatch('notifications_common_enabled', form);

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Title level={4} style={{ marginBottom: 0 }}>Профиль</Title>
        <Text type="secondary">Логин, пароль, интеграции и персональные настройки интерфейса</Text>
      </Card>

      {isBitrixEnabled && user?.bitrix_suggestion && (
        <Card className="glass-panel">
          <Space style={{ width: '100%', justifyContent: 'space-between' }}>
            <Space direction="vertical" size={0}>
              <Text strong>Найден пользователь в Bitrix24. Синхронизировать?</Text>
              <Text type="secondary">{user.bitrix_suggestion.name} (ID: {user.bitrix_suggestion.b24_user_id})</Text>
            </Space>
            <Button type="primary" onClick={() => applySuggestionMutation.mutate()} loading={applySuggestionMutation.isPending}>
              Синхронизировать
            </Button>
          </Space>
        </Card>
      )}

      {isPyrusEnabled && user?.pyrus_suggestion && (
        <Card className="glass-panel">
          <Space style={{ width: '100%', justifyContent: 'space-between' }}>
            <Space direction="vertical" size={0}>
              <Text strong>Найден сотрудник в Pyrus. Синхронизировать?</Text>
              <Text type="secondary">
                {user.pyrus_suggestion.name} (ID: {user.pyrus_suggestion.pyrus_user_id}{user.pyrus_suggestion.email ? `, ${user.pyrus_suggestion.email}` : ''})
              </Text>
            </Space>
            <Button type="primary" onClick={() => applyPyrusSuggestionMutation.mutate()} loading={applyPyrusSuggestionMutation.isPending}>
              Синхронизировать
            </Button>
          </Space>
        </Card>
      )}

      <Card className="glass-panel" loading={profileQuery.isLoading}>
        <Form<CredentialsForm>
          form={form}
          layout="vertical"
          initialValues={{
            username: user?.username || '',
            integrations: initialIntegrations,
          }}
          onFinish={onFinish}
        >
          <Row gutter={[16, 16]} align="stretch">
            <Col xs={24} xl={16}>
              <Card size="small" title="Логин, пароль и интеграции" style={{ height: '100%' }}>
                <Form.Item name="username" label="Логин" rules={[{ required: true, message: 'Введите логин' }]}>
                  <Input placeholder="Логин" />
                </Form.Item>

                <Form.Item name="password" label="Новый пароль" rules={[{ min: 6, message: 'Минимум 6 символов' }]}>
                  <Input.Password placeholder="Оставьте пустым, если без изменений" />
                </Form.Item>

                <Form.Item
                  name="confirmPassword"
                  label="Подтверждение пароля"
                  dependencies={['password']}
                  rules={[
                    ({ getFieldValue }) => ({
                      validator(_, value) {
                        if (!getFieldValue('password') || !value || getFieldValue('password') === value) {
                          return Promise.resolve();
                        }
                        return Promise.reject(new Error('Пароли не совпадают'));
                      },
                    }),
                  ]}
                >
                  <Input.Password placeholder="Повторите пароль" />
                </Form.Item>

                <Form.List name="integrations">
                  {(fields, { add, remove }) => (
                    <Space direction="vertical" style={{ width: '100%' }}>
                      <Text strong>Интеграции</Text>
                      {fields.map((field) => (
                        <Space key={field.key} style={{ display: 'flex', width: '100%' }} align="start">
                          <Form.Item
                            name={[field.name, 'integration_type']}
                            label="Система"
                            rules={[{ required: true, message: 'Выберите систему' }]}
                            style={{ minWidth: 180, marginBottom: 0 }}
                          >
                            <Select
                              options={availableIntegrationOptions}
                              placeholder="Система"
                              disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))}
                            />
                          </Form.Item>
                          <Form.Item
                            name={[field.name, 'external_id']}
                            label="ID"
                            rules={[{ required: true, message: 'Введите ID' }]}
                            style={{ flex: 1, marginBottom: 0 }}
                          >
                            <Input
                              placeholder="Внешний ID"
                              disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))}
                            />
                          </Form.Item>
                          <Button
                            danger
                            onClick={() => remove(field.name)}
                            disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))}
                          >
                            Удалить
                          </Button>
                        </Space>
                      ))}
                      <Button onClick={() => add({ integration_type: undefined, external_id: '' })}>Добавить интеграцию</Button>
                    </Space>
                  )}
                </Form.List>

                <Divider />

                <Text strong>Интерфейс</Text>
                <Form.Item name="interface_locale" label={t('layout:locale.label')} style={{ marginTop: 12 }}>
                  <Select options={localeOptions} />
                </Form.Item>
                <Form.Item name="cards_columns" label="Колонок карточек в поиске" style={{ marginTop: 12 }}>
                  <Select
                    options={[
                      { value: 1, label: '1' },
                      { value: 2, label: '2' },
                      { value: 3, label: '3' },
                      { value: 4, label: '4' },
                      { value: 5, label: '5' },
                    ]}
                  />
                </Form.Item>

                <Form.Item
                  name="comments_new_first"
                  label="Комментарии в тикете: новые сверху"
                  valuePropName="checked"
                  style={{ marginBottom: 0 }}
                >
                  <Switch />
                </Form.Item>
              </Card>
            </Col>

            <Col xs={24} xl={8}>
              <Card size="small" title="Настройки оповещений" style={{ height: '100%' }}>
                <Form.Item
                  name="notifications_personal_enabled"
                  label="Личные уведомления в ленте снизу слева"
                  valuePropName="checked"
                  style={{ marginBottom: 12 }}
                >
                  <Switch />
                </Form.Item>

                <Form.Item name="notifications_common_enabled" label="Общие уведомления включены" valuePropName="checked" style={{ marginBottom: 12 }}>
                  <Switch />
                </Form.Item>

                <div className={`profile-notification-advanced ${commonEnabled ? 'is-open' : ''}`}>
                  <Form.Item
                    name="notifications_common_ticket_updates"
                    label="Уведомления по тикетам (описание, статус, комментарии, подписки)"
                    valuePropName="checked"
                    style={{ marginBottom: 12 }}
                  >
                    <Switch disabled={!commonEnabled} />
                  </Form.Item>

                  <Form.Item
                    name="notifications_common_deferred_due"
                    label="Срабатывание статуса «Отложено»"
                    valuePropName="checked"
                    style={{ marginBottom: 0 }}
                  >
                    <Switch disabled={!commonEnabled} />
                  </Form.Item>
                </div>
              </Card>
            </Col>
          </Row>

          <Button
            type="primary"
            htmlType="submit"
            loading={updateCredentialsMutation.isPending || updateIntegrationsMutation.isPending || updateConfigMutation.isPending}
          >
            Сохранить
          </Button>
        </Form>
      </Card>
    </Space>
  );
};

export default ProfilePage;
