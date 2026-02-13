import React, { useMemo } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button, Card, Form, Input, Space, Typography, message, Select } from 'antd';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

type CredentialsForm = {
  username: string;
  password?: string;
  confirmPassword?: string;
  integrations?: Array<{ integration_type?: string; external_id?: string; is_locked?: boolean; is_verified?: boolean; verified_name?: string }>;
};

const integrationOptions = [
  { value: 'bitrix24', label: 'Bitrix24' },
  { value: 'naumen', label: 'Naumen' },
  { value: 'telegram', label: 'Telegram' },
];

const ProfilePage: React.FC = () => {
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [form] = Form.useForm<CredentialsForm>();

  const initialIntegrations = useMemo(() => {
    return (user?.integrations || []).map((item) => ({
      integration_type: item.integration_type,
      external_id: item.external_id,
      is_locked: item.is_locked,
      is_verified: item.is_verified,
      verified_name: item.verified_name,
    }));
  }, [user?.integrations]);

  const updateCredentialsMutation = useMutation({
    mutationFn: (payload: { username?: string; password?: string }) => profileApi.updateCredentials(payload),
  });

  const updateIntegrationsMutation = useMutation({
    mutationFn: (payload: { integrations: Array<{ integration_type: string; external_id: string }> }) => profileApi.updateIntegrations(payload),
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

    if (!credentialsPayload.username && !credentialsPayload.password && !integrationsChanged) {
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
          updatedUser = {
            ...updatedUser,
            integrations: dtoUser.integrations || [],
            external_type: dtoUser.external_type,
            external_system_id: dtoUser.external_system_id,
            profile_config: dtoUser.profile_config || updatedUser.profile_config,
          };
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

      <Card className="glass-panel" title="Учётные данные и интеграции">
        <Form<CredentialsForm>
          form={form}
          layout="vertical"
          initialValues={{ username: user?.username || '', integrations: initialIntegrations }}
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
                      {...field}
                      name={[field.name, 'integration_type']}
                      label="Система"
                      rules={[{ required: true, message: 'Выберите систему' }]}
                      style={{ minWidth: 180, marginBottom: 0 }}
                    >
                      <Select options={integrationOptions} placeholder="Система" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))} />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, 'external_id']}
                      label="ID"
                      rules={[{ required: true, message: 'Введите ID' }]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="ID во внешней системе" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'is_locked']))} />
                    </Form.Item>
                    {!form.getFieldValue(['integrations', field.name, 'is_locked']) ? (
                      <Button onClick={() => remove(field.name)} danger style={{ marginTop: 30 }}>
                        Удалить
                      </Button>
                    ) : (
                      <Text type="secondary" style={{ marginTop: 34 }}>Автопривязка</Text>
                    )}
                  </Space>
                ))}
                <Button onClick={() => add()} type="dashed" block>
                  + Добавить интеграцию
                </Button>
              </Space>
            )}
          </Form.List>

          <Button
            type="primary"
            htmlType="submit"
            loading={updateCredentialsMutation.isPending || updateIntegrationsMutation.isPending}
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
