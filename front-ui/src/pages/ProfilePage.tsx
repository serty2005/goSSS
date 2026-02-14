import React, { useEffect, useMemo } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { App as AntdApp, Button, Card, Form, Input, Select, Space, Typography } from 'antd';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

type CredentialsForm = {
  username: string;
  password?: string;
  confirmPassword?: string;
  cards_columns?: number;
  integrations?: Array<{ integration_type?: string; external_id?: string; is_locked?: boolean; is_verified?: boolean; verified_name?: string }>;
};

const integrationOptions = [
  { value: 'bitrix24', label: 'Bitrix24' },
  { value: 'naumen', label: 'Naumen' },
  { value: 'telegram', label: 'Telegram' },
];

const ProfilePage: React.FC = () => {
  const { message } = AntdApp.useApp();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [form] = Form.useForm<CredentialsForm>();

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
    return (user?.integrations || []).map((item) => ({
      integration_type: item.integration_type,
      external_id: item.external_id,
      is_locked: item.is_locked,
      is_verified: item.is_verified,
      verified_name: item.verified_name,
    }));
  }, [user?.integrations]);

  useEffect(() => {
    form.setFieldsValue({
      username: user?.username || '',
      integrations: initialIntegrations,
      cards_columns: Number(user?.profile_config?.interface?.search?.cards_columns ?? 5),
    });
  }, [form, initialIntegrations, user?.profile_config?.interface?.search?.cards_columns, user?.username]);

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

    const currentIntegrations = (user.integrations || [])
      .map((item) => `${item.integration_type}:${item.external_id}`)
      .sort();
    const nextIntegrations = normalizedIntegrations
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
    const columnsChanged = nextColumns !== currentColumns;

    if (!credentialsPayload.username && !credentialsPayload.password && !integrationsChanged && !columnsChanged) {
      message.info('Нет изменений для сохранения');
      return;
    }

    try {
      if (credentialsPayload.username || credentialsPayload.password) {
        await updateCredentialsMutation.mutateAsync(credentialsPayload);
      }

      let updatedUser = user;
      if (integrationsChanged) {
        const response = await updateIntegrationsMutation.mutateAsync({ integrations: normalizedIntegrations });
        const dtoUser = (response as any)?.data;
        if (dtoUser && typeof dtoUser === 'object' && 'id' in dtoUser) {
          updatedUser = { ...updatedUser, ...dtoUser };
        } else {
          updatedUser = {
            ...updatedUser,
            integrations: normalizedIntegrations.map((item, index) => ({
              id: index + 1,
              integration_type: item.integration_type,
              external_id: item.external_id,
              is_verified: false,
            })),
          };
        }
      }

      if (columnsChanged) {
        const nextConfig = {
          ...(updatedUser.profile_config || {}),
          interface: {
            ...((updatedUser.profile_config || {}).interface || {}),
            search: {
              ...((updatedUser.profile_config || {}).interface?.search || {}),
              cards_columns: nextColumns,
            },
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
      message.success('Профиль обновлён');
      form.setFieldValue('password', undefined);
      form.setFieldValue('confirmPassword', undefined);
    } catch (error: any) {
      message.error(error?.response?.data?.error?.error || 'Не удалось обновить профиль');
    }
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Title level={4} style={{ marginBottom: 0 }}>Профиль</Title>
        <Text type="secondary">Логин, пароль и внешние интеграции</Text>
      </Card>

      {user?.bitrix_suggestion && (
        <Card className="glass-panel">
          <Space style={{ width: '100%', justifyContent: 'space-between' }}>
            <Space direction="vertical" size={0}>
              <Text strong>Есть пользователь в Битрикс - Синхронизировать?</Text>
              <Text type="secondary">{user.bitrix_suggestion.name} (ID: {user.bitrix_suggestion.b24_user_id})</Text>
            </Space>
            <Button type="primary" onClick={() => applySuggestionMutation.mutate()} loading={applySuggestionMutation.isPending}>
              Синхронизировать
            </Button>
          </Space>
        </Card>
      )}

      <Card className="glass-panel" title="Учётные данные и интеграции" loading={profileQuery.isLoading}>
        <Form<CredentialsForm>
          form={form}
          layout="vertical"
          initialValues={{
            username: user?.username || '',
            integrations: initialIntegrations,
            cards_columns: Number(user?.profile_config?.interface?.search?.cards_columns ?? 5),
          }}
          onFinish={onFinish}
        >
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
                      <Select options={integrationOptions} placeholder="Система" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))} />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'external_id']}
                      label="ID"
                      rules={[{ required: true, message: 'Введите ID' }]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="ID во внешней системе" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))} />
                    </Form.Item>
                    <Button onClick={() => remove(field.name)} danger style={{ marginTop: 30 }}>
                      Удалить
                    </Button>
                  </Space>
                ))}
                <Button onClick={() => add()} type="dashed" block>
                  + Добавить интеграцию
                </Button>
              </Space>
            )}
          </Form.List>

          <Form.Item name="cards_columns" label="Количество колонок карточек в поиске">
            <Select
              options={[
                { value: 1, label: '1 колонка' },
                { value: 2, label: '2 колонки' },
                { value: 3, label: '3 колонки' },
                { value: 4, label: '4 колонки' },
                { value: 5, label: '5 колонок' },
              ]}
              style={{ maxWidth: 220 }}
            />
          </Form.Item>

          <Button
            type="primary"
            htmlType="submit"
            loading={updateCredentialsMutation.isPending || updateIntegrationsMutation.isPending || updateConfigMutation.isPending}
            style={{ marginTop: 16 }}
          >
            Сохранить
          </Button>
        </Form>
      </Card>
    </Space>
  );
};

export default ProfilePage;
