import React from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button, Card, Form, Input, Space, Typography, message } from 'antd';
import { profileApi } from '@/api/profile';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

type CredentialsForm = {
  username: string;
  password?: string;
  confirmPassword?: string;
};

const ProfilePage: React.FC = () => {
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const [form] = Form.useForm<CredentialsForm>();

  const updateMutation = useMutation({
    mutationFn: (payload: { username?: string; password?: string }) => profileApi.updateCredentials(payload),
    onSuccess: (_, variables) => {
      if (!user) return;
      if (variables.username) {
        setUser({ ...user, username: variables.username });
      }
      message.success('Профиль обновлён');
      form.setFieldValue('password', undefined);
      form.setFieldValue('confirmPassword', undefined);
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось обновить профиль');
    },
  });

  const onFinish = (values: CredentialsForm) => {
    const payload: { username?: string; password?: string } = {};

    const nextUsername = values.username?.trim();
    if (nextUsername && nextUsername !== user?.username) {
      payload.username = nextUsername;
    }

    const nextPassword = values.password?.trim();
    if (nextPassword) {
      payload.password = nextPassword;
    }

    if (!payload.username && !payload.password) {
      message.info('Нет изменений для сохранения');
      return;
    }

    updateMutation.mutate(payload);
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Title level={4} style={{ marginBottom: 0 }}>Профиль</Title>
        <Text type="secondary">Изменение логина и пароля</Text>
      </Card>

      <Card className="glass-panel" title="Учётные данные">
        <Form<CredentialsForm>
          form={form}
          layout="vertical"
          initialValues={{ username: user?.username || '' }}
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

          <Button type="primary" htmlType="submit" loading={updateMutation.isPending}>
            Сохранить
          </Button>
        </Form>
      </Card>

      <Card className="glass-panel" title="Оформление (скоро)">
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Text type="secondary">Палитра акцентного цвета</Text>
          <Button disabled block>Выбор акцентного цвета (скоро)</Button>
          <Text type="secondary">Тема интерфейса</Text>
          <Button disabled block>Выбор темы интерфейса (скоро)</Button>
        </Space>
      </Card>
    </Space>
  );
};

export default ProfilePage;
