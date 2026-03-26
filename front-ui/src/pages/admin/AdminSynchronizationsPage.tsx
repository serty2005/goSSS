import React, { useMemo, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import {
  App as AntdApp,
  Button,
  Card,
  Collapse,
  Empty,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined, RightOutlined } from '@ant-design/icons';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import { pyrusAdminApi } from '@/api/pyrusAdmin';
import {
  BitrixDirectoryUserDTO,
  BitrixUsersRefreshDTO,
  PyrusDirectoryUserDTO,
  PyrusUsersRefreshDTO,
} from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

const formatDateTime = (value?: string) => {
  if (!value) {
    return '—';
  }

  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('ru-RU');
};

const extractErrorText = (error: unknown, fallback: string) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || fallback;
};

const AdminSynchronizationsPage: React.FC = () => {
  const { message } = AntdApp.useApp();
  const navigate = useNavigate();
  const currentUser = useAuthStore((state) => state.user);
  const isBitrixEnabled = currentUser?.bitrix_enabled === true;
  const isPyrusEnabled = currentUser?.pyrus_enabled === true;

  const [bitrixUsers, setBitrixUsers] = useState<BitrixUsersRefreshDTO | null>(null);
  const [pyrusUsers, setPyrusUsers] = useState<PyrusUsersRefreshDTO | null>(null);
  const [activeKeys, setActiveKeys] = useState<string[]>([]);

  const bitrixColumns: ColumnsType<BitrixDirectoryUserDTO> = useMemo(() => [
    {
      title: 'Сотрудник',
      key: 'employee',
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{record.name}</Text>
          <Text type="secondary">{[record.last_name, record.first_name, record.second_name].filter(Boolean).join(' ') || '—'}</Text>
        </Space>
      ),
    },
    {
      title: 'Email',
      dataIndex: 'email',
      key: 'email',
      render: (value?: string) => value || '—',
    },
    {
      title: 'Телефон',
      dataIndex: 'phone',
      key: 'phone',
      render: (value?: string) => value || '—',
    },
    {
      title: 'Статус',
      dataIndex: 'active',
      key: 'active',
      width: 120,
      render: (value: boolean) => (value ? <Tag color="success">Активен</Tag> : <Tag>Неактивен</Tag>),
    },
    {
      title: 'Последняя активность',
      dataIndex: 'last_seen_at',
      key: 'last_seen_at',
      width: 190,
      render: (value?: string) => formatDateTime(value),
    },
  ], []);

  const pyrusColumns: ColumnsType<PyrusDirectoryUserDTO> = useMemo(() => [
    {
      title: 'Сотрудник',
      key: 'employee',
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{record.name}</Text>
          <Text type="secondary">{record.position || '—'}</Text>
        </Space>
      ),
    },
    {
      title: 'Email',
      dataIndex: 'email',
      key: 'email',
      render: (value?: string) => value || '—',
    },
    {
      title: 'Статус',
      key: 'status',
      width: 180,
      render: (_, record) => (
        <Space wrap size={[4, 4]}>
          {record.status ? <Tag color="blue">{record.status}</Tag> : null}
          {record.banned ? <Tag color="red">Заблокирован</Tag> : null}
          {record.fired ? <Tag color="volcano">Уволен</Tag> : null}
          {!record.status && !record.banned && !record.fired ? <Tag>—</Tag> : null}
        </Space>
      ),
    },
    {
      title: 'Телефон',
      key: 'phones',
      render: (_, record) => record.mobile_phone || record.phone || '—',
    },
    {
      title: 'Локация',
      dataIndex: 'location',
      key: 'location',
      render: (value?: string) => value || '—',
    },
  ], []);

  const openPanel = (key: string) => {
    setActiveKeys((prev) => (prev.includes(key) ? prev : [...prev, key]));
  };

  const refreshBitrixMutation = useMutation({
    mutationFn: () => bitrixAdminApi.refreshUsers(),
    onSuccess: (response) => {
      setBitrixUsers(response.data || null);
      openPanel('bitrix-users');
      message.success(`Сотрудники Bitrix24 обновлены: ${response.data?.count ?? 0}`);
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось обновить сотрудников Bitrix24'));
    },
  });

  const refreshPyrusMutation = useMutation({
    mutationFn: () => pyrusAdminApi.refreshUsers(),
    onSuccess: (response) => {
      setPyrusUsers(response.data || null);
      openPanel('pyrus-users');
      message.success(`Сотрудники Pyrus обновлены: ${response.data?.count ?? 0}`);
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось обновить сотрудников Pyrus'));
    },
  });

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Space direction="vertical" size={4}>
          <Title level={4} style={{ marginBottom: 0 }}>Синхронизации</Title>
          <Text type="secondary">Отдельный раздел для обновления справочников сотрудников и синхронизации точек обслуживания</Text>
        </Space>
      </Card>

      <Card
        className="glass-panel"
        title="Сотрудники Bitrix24"
        extra={(
          <Button
            type="primary"
            icon={<ReloadOutlined />}
            onClick={() => refreshBitrixMutation.mutate()}
            loading={refreshBitrixMutation.isPending}
            disabled={!isBitrixEnabled}
          >
            Обновить
          </Button>
        )}
      >
        {isBitrixEnabled ? (
          <Collapse
            activeKey={activeKeys}
            onChange={(keys) => setActiveKeys(Array.isArray(keys) ? keys.map(String) : [String(keys)])}
            items={[
              {
                key: 'bitrix-users',
                label: `Полученные сотрудники Bitrix24 (${bitrixUsers?.count ?? 0})`,
                children: bitrixUsers?.users?.length ? (
                  <Table<BitrixDirectoryUserDTO>
                    rowKey="b24_user_id"
                    columns={bitrixColumns}
                    dataSource={bitrixUsers.users}
                    pagination={{ pageSize: 10 }}
                  />
                ) : (
                  <Empty description="Список пока не загружен" />
                ),
              },
            ]}
          />
        ) : (
          <Text type="secondary">Интеграция Bitrix24 сейчас недоступна.</Text>
        )}
      </Card>

      <Card
        className="glass-panel"
        title="Сотрудники Pyrus"
        extra={(
          <Button
            icon={<ReloadOutlined />}
            onClick={() => refreshPyrusMutation.mutate()}
            loading={refreshPyrusMutation.isPending}
            disabled={!isPyrusEnabled}
          >
            Обновить
          </Button>
        )}
      >
        {isPyrusEnabled ? (
          <Collapse
            activeKey={activeKeys}
            onChange={(keys) => setActiveKeys(Array.isArray(keys) ? keys.map(String) : [String(keys)])}
            items={[
              {
                key: 'pyrus-users',
                label: `Полученные сотрудники Pyrus (${pyrusUsers?.count ?? 0})`,
                children: pyrusUsers?.users?.length ? (
                  <Table<PyrusDirectoryUserDTO>
                    rowKey="pyrus_user_id"
                    columns={pyrusColumns}
                    dataSource={pyrusUsers.users}
                    pagination={{ pageSize: 10 }}
                  />
                ) : (
                  <Empty description="Список пока не загружен" />
                ),
              },
            ]}
          />
        ) : (
          <Text type="secondary">Интеграция Pyrus сейчас недоступна.</Text>
        )}
      </Card>

      <Card
        className="glass-panel"
        title="Точки обслуживания из 1С"
        extra={(
          <Button
            type="primary"
            icon={<RightOutlined />}
            onClick={() => navigate('/admin/synchronizations/service-points-import')}
          >
            Открыть импорт
          </Button>
        )}
      >
        <Text type="secondary">
          Здесь находится синхронизация точек обслуживания и отчётов по контрактам для Bitrix24.
        </Text>
      </Card>
    </Space>
  );
};

export default AdminSynchronizationsPage;
