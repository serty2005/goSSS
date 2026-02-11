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
  integrations?: Array<{ integrationType?: string; externalId?: string; isLocked?: boolean; isVerified?: boolean; verifiedName?: string }>;
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
      integrationType: item.integrationType,
      externalId: item.externalId,
      isLocked: item.isLocked,
      isVerified: item.isVerified,
      verifiedName: item.verifiedName,
    }));
  }, [user?.integrations]);

  const updateCredentialsMutation = useMutation({
    mutationFn: (payload: { username?: string; password?: string }) => profileApi.updateCredentials(payload),
  });

  const updateIntegrationsMutation = useMutation({
    mutationFn: (payload: { integrations: Array<{ integrationType: string; externalId: string }> }) => profileApi.updateIntegrations(payload),
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
        integrationType: String(item.integrationType || '').trim().toLowerCase(),
        externalId: String(item.externalId || '').trim(),
      }))
      .filter((item) => item.integrationType && item.externalId);

    const currentIntegrations = (user.integrations || [])
      .map((item) => `${item.integrationType}:${item.externalId}`)
      .sort();
    const nextIntegrations = normalizedIntegrations
      .map((item) => `${item.integrationType}:${item.externalId}`)
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
            externalType: dtoUser.externalType,
            externalSystemId: dtoUser.externalSystemId,
          };
        } else {
          updatedUser = {
            ...updatedUser,
            integrations: normalizedIntegrations.map((item, index) => ({
              id: index + 1,
              integrationType: item.integrationType,
              externalId: item.externalId,
              isVerified: false,
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
                      name={[field.name, 'integrationType']}
                      label="Система"
                      rules={[{ required: true, message: 'Выберите систему' }]}
                      style={{ minWidth: 180, marginBottom: 0 }}
                    >
                      <Select options={integrationOptions} placeholder="Система" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'isLocked']))} />
                    </Form.Item>
                    <Form.Item
                      {...field}
                      name={[field.name, 'externalId']}
                      label="ID"
                      rules={[{ required: true, message: 'Введите ID' }]}
                      style={{ flex: 1, marginBottom: 0 }}
                    >
                      <Input placeholder="ID во внешней системе" disabled={Boolean(form.getFieldValue(['integrations', field.name, 'isLocked']))} />
                    </Form.Item>
                    {!form.getFieldValue(['integrations', field.name, 'isLocked']) ? (
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
