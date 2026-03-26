import React, { useState } from 'react';
import { Form, Input, Button, Card, Typography, message, Layout, Space } from 'antd';
import { UserOutlined, LockOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/store/authStore';
import apiClient from '@/api/axios';
import { useAppLocale } from '@/i18n/useAppLocale';
import type { AppLocaleCode } from '@/i18n/localeTypes';

const { Title } = Typography;

const LOGIN_LOCALE_FLAGS: Record<AppLocaleCode, string> = {
  en: '🇬🇧',
  ru: '🇷🇺',
};

type LoginFormValues = {
  username: string;
  password: string;
};

const resolveLoginErrorMessage = (
  error: unknown,
  t: (key: string, options?: Record<string, unknown>) => string,
) => {
  const responseStatus = Number((error as any)?.response?.status || 0);
  const serverMessage = String((error as any)?.response?.data?.error?.error || '').trim();

  if (!responseStatus) {
    return t('auth:login.messages.networkError');
  }

  if (responseStatus === 401) {
    return t('auth:login.messages.invalidCredentials');
  }

  const fallbackMessage = t('auth:login.messages.unknownError');
  return serverMessage
    ? t('common:errors.withDetails', { message: fallbackMessage, details: serverMessage })
    : fallbackMessage;
};

const LoginPage: React.FC = () => {
  const { t } = useTranslation(['auth', 'common']);
  const [loading, setLoading] = useState(false);
  const navigate = useNavigate();
  const login = useAuthStore((state) => state.login);
  const { locale, supportedLocales } = useAppLocale();

  const onFinish = async (values: LoginFormValues) => {
    setLoading(true);
    try {
      // Прямой запрос к API вместо отдельного сервиса для простоты на старте
      const response = await apiClient.post('/auth/login', {
        username: values.username,
        password: values.password,
      });

      if (response.data.status === 'success') {
        const { access_token, user } = response.data.data;
        login(access_token, user);
        message.success(t('auth:login.messages.success'));
        navigate('/');
      } else {
        message.error(t('auth:login.messages.invalidResponse'));
      }
    } catch (error: unknown) {
      console.error(error);
      message.error(resolveLoginErrorMessage(error, t));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Layout style={{ minHeight: '100vh', justifyContent: 'center', alignItems: 'center' }}>
      <Card
        style={{ width: 400, boxShadow: '0 8px 24px rgba(0,0,0,0.1)' }}
        className="glass-panel"
        bordered={false}
      >
        <div style={{ textAlign: 'center', marginBottom: 24 }}>
          <Title level={3} style={{ margin: 0 }}>MyHoreca XenionDesk</Title>
          <Typography.Text type="secondary">{t('auth:login.subtitle')}</Typography.Text>
        </div>

        <Form
          name="login_form"
          initialValues={{ remember: true }}
          onFinish={onFinish}
          size="large"
        >
          <Form.Item
            name="username"
            rules={[{ required: true, message: t('auth:login.usernameRequired') }]}
          >
            <Input prefix={<UserOutlined />} placeholder={t('auth:login.usernamePlaceholder')} />
          </Form.Item>
          <Form.Item
            name="password"
            rules={[{ required: true, message: t('auth:login.passwordRequired') }]}
          >
            <Input.Password prefix={<LockOutlined />} placeholder={t('auth:login.passwordPlaceholder')} />
          </Form.Item>

          <Form.Item>
            <Button type="primary" htmlType="submit" style={{ width: '100%' }} loading={loading}>
              {t('auth:login.submit')}
            </Button>
          </Form.Item>
        </Form>

        <Space direction="vertical" size={10} style={{ width: '100%', alignItems: 'center' }}>
          <Typography.Text type="secondary">{t('auth:login.locale.label')}</Typography.Text>
          <Space size={12}>
            {supportedLocales.map((supportedLocale) => {
              const isActive = supportedLocale.code === locale;
              return (
                <form
                  key={supportedLocale.code}
                  action="/login"
                  method="get"
                  style={{ margin: 0 }}
                >
                  <button
                    type="submit"
                    name="locale"
                    value={supportedLocale.code}
                    aria-label={t(`auth:login.locale.switchTo.${supportedLocale.code}`)}
                    title={t(`auth:login.locale.switchTo.${supportedLocale.code}`)}
                    style={{
                      width: 44,
                      height: 44,
                      borderRadius: 999,
                      border: isActive ? '1px solid #1677ff' : '1px solid #d9d9d9',
                      background: isActive ? '#e6f4ff' : '#ffffff',
                      boxShadow: isActive ? '0 0 0 2px rgba(22,119,255,0.15)' : 'none',
                      cursor: 'pointer',
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      fontSize: 22,
                      padding: 0,
                    }}
                  >
                    {LOGIN_LOCALE_FLAGS[supportedLocale.code]}
                  </button>
                </form>
              );
            })}
          </Space>
        </Space>
      </Card>
    </Layout>
  );
};

export default LoginPage;
