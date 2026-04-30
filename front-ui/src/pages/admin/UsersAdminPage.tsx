import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Col,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  CheckCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  StopOutlined,
} from '@ant-design/icons';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import { pyrusAdminApi } from '@/api/pyrusAdmin';
import { telephonyApi } from '@/api/telephony';
import { usersApi } from '@/api/users';
import {
  DeletedUserRestoreCandidateDTO,
  UserAdminDTO,
  UserIntegrationDTO,
  UserPosition,
  UserCreatePayload,
  UserSchedule,
  UserUpdatePayload,
} from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

type EditableIntegration = NonNullable<UserUpdatePayload['integrations']>[number];
type IntegrationOption = { label: string; value: string; color?: string };

const positionOptions: { label: string; value: UserPosition }[] = [
  { label: 'Администратор системы', value: 'admin' },
  { label: 'Специалист техподдержки', value: 'support_specialist' },
  { label: 'Стажёр', value: 'intern' },
];

const scheduleOptions: { label: string; value: UserSchedule }[] = [
  { label: '2/2', value: '2/2' },
  { label: '3/3', value: '3/3' },
  { label: '5/2', value: '5/2' },
];

const integrationCatalog: IntegrationOption[] = [
  { label: 'Telegram', value: 'telegram', color: 'cyan' },
  { label: 'Naumen', value: 'naumen', color: 'orange' },
  { label: 'Bitrix24', value: 'bitrix24', color: 'blue' },
  { label: 'Мегафон', value: 'megafon_vats', color: 'gold' },
  { label: 'Pyrus', value: 'pyrus', color: 'geekblue' },
];

const mapPositionLabel = (position: UserPosition) => {
  const found = positionOptions.find((item) => item.value === position);
  return found?.label ?? position;
};

const getIntegrationLabel = (integrationType?: string) => {
  const found = integrationCatalog.find((item) => item.value === integrationType);
  return found?.label ?? integrationType ?? 'Интеграция';
};

const getIntegrationColor = (integrationType?: string) => {
  const found = integrationCatalog.find((item) => item.value === integrationType);
  return found?.color ?? 'default';
};

const getExternalPlaceholder = (externalType?: string) => {
  switch (externalType) {
    case 'telegram':
      return '@login';
    case 'naumen':
      return '$uuid';
    case 'bitrix24':
    case 'pyrus':
      return '12345';
    case 'megafon_vats':
      return 'Логин сотрудника ВАТС';
    default:
      return 'ID внешней системы';
  }
};

const extractErrorText = (error: unknown, fallback: string) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || fallback;
};

const normalizeString = (value?: string | null) => {
  const normalized = String(value || '').trim();
  return normalized || undefined;
};

const normalizeIntegrationItems = (items?: EditableIntegration[] | null): EditableIntegration[] => {
  const result: EditableIntegration[] = [];
  const seen = new Set<string>();

  for (const item of items || []) {
    const integrationType = normalizeString(item.integration_type)?.toLowerCase();
    const externalID = normalizeString(item.external_id);
    if (!integrationType || !externalID) {
      continue;
    }

    const key = `${integrationType}::${externalID}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);

    result.push({
      integration_type: integrationType,
      external_id: externalID,
      is_enabled: item.is_enabled ?? true,
    });
  }

  return result;
};

const buildUserIntegrations = (user: UserAdminDTO): EditableIntegration[] => {
  const items: EditableIntegration[] = (user.integrations || []).map((item) => ({
    integration_type: item.integration_type,
    external_id: item.external_id,
    is_enabled: item.is_enabled,
  }));

  if (user.external_type && user.external_system_id) {
    items.push({
      integration_type: user.external_type,
      external_id: user.external_system_id,
      is_enabled: true,
    });
  }

  return normalizeIntegrationItems(items);
};

const buildDisplayIntegrations = (user: UserAdminDTO): UserIntegrationDTO[] => {
  const items = user.integrations?.length
    ? user.integrations
    : user.external_type && user.external_system_id
      ? [{
          id: 0,
          integration_type: user.external_type,
          external_id: user.external_system_id,
          is_enabled: true,
          is_verified: false,
          is_locked: false,
          verified_name: '',
        }]
      : [];

  const result: UserIntegrationDTO[] = [];
  const seen = new Set<string>();

  for (const item of items) {
    const key = `${item.integration_type}::${item.external_id}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(item);
  }

  return result;
};

const hasIntegrationType = (items: EditableIntegration[] | undefined, integrationType: string) => (
  (items || []).some((item) => String(item.integration_type || '').trim().toLowerCase() === integrationType)
);

const appendIntegration = (
  currentItems: EditableIntegration[] | undefined,
  integrationType: string,
  externalID: string,
): EditableIntegration[] => {
  const normalized = normalizeIntegrationItems(currentItems);
  const key = `${integrationType}::${externalID}`;
  const index = normalized.findIndex((item) => `${item.integration_type}::${item.external_id}` === key);

  if (index >= 0) {
    normalized[index] = { ...normalized[index], is_enabled: true };
    return normalized;
  }

  return [
    ...normalized,
    {
      integration_type: integrationType,
      external_id: externalID,
      is_enabled: true,
    },
  ];
};

const UsersAdminPage: React.FC = () => {
  const { message } = AntdApp.useApp();
  const queryClient = useQueryClient();
  const currentUser = useAuthStore((state) => state.user);
  const isBitrixEnabled = currentUser?.bitrix_enabled === true;
  const isPyrusEnabled = currentUser?.pyrus_enabled === true;

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserAdminDTO | null>(null);
  const [restoreCandidate, setRestoreCandidate] = useState<DeletedUserRestoreCandidateDTO | null>(null);
  const [createSuggestion, setCreateSuggestion] = useState<{ b24_user_id: number; name: string } | null>(null);
  const [editSuggestion, setEditSuggestion] = useState<{ b24_user_id: number; name: string } | null>(null);
  const [createMegafonSuggestion, setCreateMegafonSuggestion] = useState<{ login: string; name: string } | null>(null);
  const [editMegafonSuggestion, setEditMegafonSuggestion] = useState<{ login: string; name: string } | null>(null);
  const [createPyrusSuggestion, setCreatePyrusSuggestion] = useState<{ pyrus_user_id: number; name: string; email?: string } | null>(null);
  const [editPyrusSuggestion, setEditPyrusSuggestion] = useState<{ pyrus_user_id: number; name: string; email?: string } | null>(null);

  const [createForm] = Form.useForm<UserCreatePayload>();
  const [editForm] = Form.useForm<UserUpdatePayload>();

  const watchedCreateUsername = Form.useWatch('username', createForm);
  const watchedCreateExternalType = Form.useWatch('external_type', createForm);
  const watchedCreateFirstName = Form.useWatch('first_name', createForm);
  const watchedCreateLastName = Form.useWatch('last_name', createForm);
  const watchedCreateEmail = Form.useWatch('email', createForm);
  const watchedEditFirstName = Form.useWatch('first_name', editForm);
  const watchedEditLastName = Form.useWatch('last_name', editForm);
  const watchedEditEmail = Form.useWatch('email', editForm);
  const watchedEditIntegrations = Form.useWatch('integrations', editForm);

  const availableIntegrationOptions = useMemo(() => integrationCatalog.filter((item) => {
    if (item.value === 'bitrix24') {
      return isBitrixEnabled;
    }
    if (item.value === 'pyrus') {
      return isPyrusEnabled;
    }
    return true;
  }), [isBitrixEnabled, isPyrusEnabled]);

  const editIntegrationOptions = useMemo(() => {
    const result = [...availableIntegrationOptions];
    const existingTypes = new Set(
      (watchedEditIntegrations || [])
        .map((item) => String(item.integration_type || '').trim().toLowerCase())
        .filter(Boolean),
    );

    for (const integrationType of existingTypes) {
      if (!result.some((item) => item.value === integrationType)) {
        result.push({
          value: integrationType,
          label: getIntegrationLabel(integrationType),
        });
      }
    }

    return result;
  }, [availableIntegrationOptions, watchedEditIntegrations]);

  const { data, isLoading } = useQuery({
    queryKey: ['admin-users'],
    queryFn: () => usersApi.getUsers(),
  });

  const users = data?.data ?? [];

  const resetCreateModal = () => {
    setIsCreateOpen(false);
    setRestoreCandidate(null);
    setCreateSuggestion(null);
    setCreateMegafonSuggestion(null);
    setCreatePyrusSuggestion(null);
    createForm.resetFields();
  };

  const resetEditModal = () => {
    setIsEditOpen(false);
    setSelectedUser(null);
    setEditSuggestion(null);
    setEditMegafonSuggestion(null);
    setEditPyrusSuggestion(null);
    editForm.resetFields();
  };

  const invalidateUsers = async () => {
    await queryClient.invalidateQueries({ queryKey: ['admin-users'] });
  };

  const createMutation = useMutation({
    mutationFn: (payload: UserCreatePayload) => usersApi.createUser(payload),
    onSuccess: async () => {
      message.success('Пользователь создан');
      resetCreateModal();
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось создать пользователя'));
    },
  });

  const restoreMutation = useMutation({
    mutationFn: (payload: UserCreatePayload) => usersApi.restoreUser(payload),
    onSuccess: async () => {
      message.success('Пользователь восстановлен');
      resetCreateModal();
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось восстановить пользователя'));
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: number; payload: UserUpdatePayload }) => usersApi.updateUser(id, payload),
    onSuccess: async () => {
      message.success('Пользователь обновлён');
      resetEditModal();
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось обновить пользователя'));
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => usersApi.deleteUser(id),
    onSuccess: async () => {
      message.success('Пользователь удалён');
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось удалить пользователя'));
    },
  });

  const statusMutation = useMutation({
    mutationFn: ({ id, isActive }: { id: number; isActive: boolean }) => usersApi.updateUserStatus(id, isActive),
    onSuccess: async (_, variables) => {
      message.success(variables.isActive ? 'Пользователь разблокирован' : 'Пользователь заблокирован');
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось изменить статус пользователя'));
    },
  });

  const applySuggestionMutation = useMutation({
    mutationFn: (id: number) => usersApi.applyBitrixSuggestion(id),
    onSuccess: async () => {
      message.success('Интеграция Bitrix24 применена');
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось применить интеграцию Bitrix24'));
    },
  });

  const applyPyrusSuggestionMutation = useMutation({
    mutationFn: (id: number) => usersApi.applyPyrusSuggestion(id),
    onSuccess: async () => {
      message.success('Интеграция Pyrus применена');
      await invalidateUsers();
    },
    onError: (error) => {
      message.error(extractErrorText(error, 'Не удалось применить интеграцию Pyrus'));
    },
  });

  const normalizeCreatePayload = (values: UserCreatePayload): UserCreatePayload => ({
    username: normalizeString(values.username) || '',
    password: normalizeString(values.password) || '',
    first_name: normalizeString(values.first_name) || '',
    last_name: normalizeString(values.last_name) || '',
    email: normalizeString(values.email),
    position: values.position,
    schedule_type: values.schedule_type,
    external_type: normalizeString(values.external_type)?.toLowerCase(),
    external_system_id: normalizeString(values.external_system_id),
  });

  const normalizeUpdatePayload = (values: UserUpdatePayload): UserUpdatePayload => ({
    username: normalizeString(values.username),
    password: normalizeString(values.password),
    first_name: normalizeString(values.first_name),
    last_name: normalizeString(values.last_name),
    email: normalizeString(values.email),
    position: values.position,
    schedule_type: values.schedule_type,
    integrations: normalizeIntegrationItems(values.integrations),
  });

  const openEditModal = useCallback((user: UserAdminDTO) => {
    setSelectedUser(user);
    editForm.setFieldsValue({
      username: user.username,
      password: undefined,
      first_name: user.first_name,
      last_name: user.last_name,
      email: user.email,
      position: user.position,
      schedule_type: user.schedule_type,
      integrations: buildUserIntegrations(user),
    });
    setIsEditOpen(true);
  }, [editForm]);

  const fillCreateFormFromCandidate = (candidate: DeletedUserRestoreCandidateDTO) => {
    createForm.setFieldsValue({
      username: candidate.username,
      first_name: candidate.first_name,
      last_name: candidate.last_name,
      email: candidate.email,
      position: candidate.position,
      schedule_type: candidate.schedule_type,
    });
  };

  useEffect(() => {
    if (!isCreateOpen) {
      setRestoreCandidate(null);
      return;
    }

    const username = normalizeString(watchedCreateUsername);
    if (!username) {
      setRestoreCandidate(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await usersApi.getRestoreCandidate(username);
        setRestoreCandidate(response.data?.candidate || null);
      } catch {
        setRestoreCandidate(null);
      }
    }, 350);

    return () => clearTimeout(timer);
  }, [isCreateOpen, watchedCreateUsername]);

  useEffect(() => {
    if (!isBitrixEnabled || !isCreateOpen) {
      setCreateSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedCreateFirstName);
    const lastName = normalizeString(watchedCreateLastName);
    if (!firstName || !lastName) {
      setCreateSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await bitrixAdminApi.suggestUserByName({
          first_name: firstName,
          last_name: lastName,
          full_name: `${firstName} ${lastName}`,
        });
        setCreateSuggestion(response.data?.suggestion || null);
      } catch {
        setCreateSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isBitrixEnabled, isCreateOpen, watchedCreateFirstName, watchedCreateLastName]);

  useEffect(() => {
    if (!isCreateOpen) {
      setCreateMegafonSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedCreateFirstName);
    const lastName = normalizeString(watchedCreateLastName);
    if (!firstName || !lastName) {
      setCreateMegafonSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await telephonyApi.suggestMegafonUser({
          first_name: firstName,
          last_name: lastName,
          full_name: `${firstName} ${lastName}`,
        });
        setCreateMegafonSuggestion(response.data?.suggestion || null);
      } catch {
        setCreateMegafonSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isCreateOpen, watchedCreateFirstName, watchedCreateLastName]);

  useEffect(() => {
    if (!isPyrusEnabled || !isCreateOpen) {
      setCreatePyrusSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedCreateFirstName);
    const lastName = normalizeString(watchedCreateLastName);
    const email = normalizeString(watchedCreateEmail);
    if (!email && (!firstName || !lastName)) {
      setCreatePyrusSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await pyrusAdminApi.suggestUserByIdentity({
          first_name: firstName,
          last_name: lastName,
          full_name: firstName && lastName ? `${firstName} ${lastName}` : undefined,
          email,
        });
        setCreatePyrusSuggestion(response.data?.suggestion || null);
      } catch {
        setCreatePyrusSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isCreateOpen, isPyrusEnabled, watchedCreateEmail, watchedCreateFirstName, watchedCreateLastName]);

  useEffect(() => {
    if (!isBitrixEnabled || !isEditOpen || hasIntegrationType(watchedEditIntegrations, 'bitrix24')) {
      setEditSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedEditFirstName);
    const lastName = normalizeString(watchedEditLastName);
    if (!firstName || !lastName) {
      setEditSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await bitrixAdminApi.suggestUserByName({
          first_name: firstName,
          last_name: lastName,
          full_name: `${firstName} ${lastName}`,
        });
        setEditSuggestion(response.data?.suggestion || null);
      } catch {
        setEditSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isBitrixEnabled, isEditOpen, watchedEditFirstName, watchedEditIntegrations, watchedEditLastName]);

  useEffect(() => {
    if (!isEditOpen || hasIntegrationType(watchedEditIntegrations, 'megafon_vats')) {
      setEditMegafonSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedEditFirstName);
    const lastName = normalizeString(watchedEditLastName);
    if (!firstName || !lastName) {
      setEditMegafonSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await telephonyApi.suggestMegafonUser({
          first_name: firstName,
          last_name: lastName,
          full_name: `${firstName} ${lastName}`,
        });
        setEditMegafonSuggestion(response.data?.suggestion || null);
      } catch {
        setEditMegafonSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isEditOpen, watchedEditFirstName, watchedEditIntegrations, watchedEditLastName]);

  useEffect(() => {
    if (!isPyrusEnabled || !isEditOpen || hasIntegrationType(watchedEditIntegrations, 'pyrus')) {
      setEditPyrusSuggestion(null);
      return;
    }

    const firstName = normalizeString(watchedEditFirstName);
    const lastName = normalizeString(watchedEditLastName);
    const email = normalizeString(watchedEditEmail);
    if (!email && (!firstName || !lastName)) {
      setEditPyrusSuggestion(null);
      return;
    }

    const timer = setTimeout(async () => {
      try {
        const response = await pyrusAdminApi.suggestUserByIdentity({
          first_name: firstName,
          last_name: lastName,
          full_name: firstName && lastName ? `${firstName} ${lastName}` : undefined,
          email,
        });
        setEditPyrusSuggestion(response.data?.suggestion || null);
      } catch {
        setEditPyrusSuggestion(null);
      }
    }, 400);

    return () => clearTimeout(timer);
  }, [isEditOpen, isPyrusEnabled, watchedEditEmail, watchedEditFirstName, watchedEditIntegrations, watchedEditLastName]);

  const onCreate = (values: UserCreatePayload) => {
    const payload = normalizeCreatePayload(values);
    if (restoreCandidate) {
      restoreMutation.mutate(payload);
      return;
    }
    createMutation.mutate(payload);
  };

  const onEdit = (values: UserUpdatePayload) => {
    if (!selectedUser) {
      return;
    }

    const payload = normalizeUpdatePayload(values);
    if (selectedUser.has_logged_in) {
      delete payload.username;
      delete payload.password;
    }

    updateMutation.mutate({ id: selectedUser.id, payload });
  };

  const columns: ColumnsType<UserAdminDTO> = useMemo(() => [
    {
      title: 'Сотрудник',
      key: 'employee',
      render: (_, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{record.full_name}</Text>
          <Text type="secondary">@{record.username}</Text>
          {record.email ? <Text type="secondary">{record.email}</Text> : null}
        </Space>
      ),
    },
    {
      title: 'Должность',
      dataIndex: 'position',
      key: 'position',
      render: (value: UserPosition) => mapPositionLabel(value),
    },
    {
      title: 'График',
      dataIndex: 'schedule_type',
      key: 'schedule_type',
      width: 110,
    },
    {
      title: 'Внешние системы',
      key: 'integrations',
      render: (_, record) => {
        const integrations = buildDisplayIntegrations(record);
        if (!integrations.length) {
          return <Text type="secondary">Нет интеграций</Text>;
        }

        return (
          <Space wrap size={[4, 8]}>
            {integrations.map((item, index) => (
              <Tag key={`${record.id}-${item.integration_type}-${item.external_id}-${index}`} color={item.is_enabled ? getIntegrationColor(item.integration_type) : 'default'}>
                {getIntegrationLabel(item.integration_type)}{item.is_enabled ? '' : ' отключена'}
              </Tag>
            ))}
          </Space>
        );
      },
    },
    {
      title: 'Первый вход',
      dataIndex: 'has_logged_in',
      key: 'has_logged_in',
      width: 140,
      render: (value: boolean) => (value ? <Tag color="blue">Выполнен</Tag> : <Tag>Не было</Tag>),
    },
    {
      title: 'Статус',
      dataIndex: 'is_active',
      key: 'is_active',
      width: 130,
      render: (value: boolean) => (value ? <Tag color="success">Активен</Tag> : <Tag>Заблокирован</Tag>),
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 440,
      render: (_, record) => {
        const isCurrentUser = currentUser?.id === record.id;
        return (
          <Space wrap>
            <Button icon={<EditOutlined />} onClick={() => openEditModal(record)}>
              Редактировать
            </Button>
            {isBitrixEnabled && record.bitrix_suggestion ? (
              <Button
                type="primary"
                onClick={() => applySuggestionMutation.mutate(record.id)}
                loading={applySuggestionMutation.isPending}
              >
                Подключить Bitrix24
              </Button>
            ) : null}
            {isPyrusEnabled && record.pyrus_suggestion ? (
              <Button onClick={() => applyPyrusSuggestionMutation.mutate(record.id)} loading={applyPyrusSuggestionMutation.isPending}>
                Подключить Pyrus
              </Button>
            ) : null}
            {record.is_active ? (
              <Popconfirm
                title="Заблокировать пользователя?"
                description="Пользователь не сможет войти в систему."
                okText="Заблокировать"
                cancelText="Отмена"
                disabled={isCurrentUser}
                onConfirm={() => statusMutation.mutate({ id: record.id, isActive: false })}
              >
                <Button danger icon={<StopOutlined />} disabled={isCurrentUser} loading={statusMutation.isPending}>
                  Заблокировать
                </Button>
              </Popconfirm>
            ) : (
              <Button icon={<CheckCircleOutlined />} loading={statusMutation.isPending} onClick={() => statusMutation.mutate({ id: record.id, isActive: true })}>
                Разблокировать
              </Button>
            )}
            <Popconfirm
              title="Удалить пользователя?"
              description="Пользователь будет скрыт из системы. Восстановление доступно только при повторном создании с тем же логином."
              okText="Удалить"
              cancelText="Отмена"
              disabled={isCurrentUser}
              onConfirm={() => deleteMutation.mutate(record.id)}
            >
              <Button danger icon={<DeleteOutlined />} disabled={isCurrentUser} loading={deleteMutation.isPending}>
                Удалить
              </Button>
            </Popconfirm>
          </Space>
        );
      },
    },
  ], [
    applyPyrusSuggestionMutation,
    applySuggestionMutation,
    currentUser?.id,
    deleteMutation,
    isBitrixEnabled,
    isPyrusEnabled,
    openEditModal,
    statusMutation,
  ]);

  return (
    <div>
      <Card className="glass-panel" style={{ marginBottom: 16 }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <div>
            <Title level={4} style={{ marginBottom: 0 }}>Сотрудники</Title>
            <Text type="secondary">Управление доступом пользователей и их интеграциями</Text>
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setIsCreateOpen(true)}>
            Добавить сотрудника
          </Button>
        </Space>
      </Card>

      <Card className="glass-panel">
        <Table<UserAdminDTO>
          rowKey="id"
          loading={isLoading}
          columns={columns}
          dataSource={users}
          pagination={false}
          scroll={{ x: 'max-content' }}
        />
      </Card>

      <Modal
        title={restoreCandidate ? 'Восстановление сотрудника' : 'Новый сотрудник'}
        open={isCreateOpen}
        onCancel={resetCreateModal}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending || restoreMutation.isPending}
        okText={restoreCandidate ? 'Восстановить' : 'Создать'}
        cancelText="Отмена"
      >
        <Form<UserCreatePayload>
          form={createForm}
          layout="vertical"
          onFinish={onCreate}
          initialValues={{ position: 'intern', schedule_type: '5/2' }}
        >
          <Form.Item name="username" label="Логин" rules={[{ required: true, message: 'Введите логин' }]}>
            <Input placeholder="Логин" />
          </Form.Item>

          {restoreCandidate ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                <Text strong>Найден удалённый пользователь с таким логином</Text>
                <Text type="secondary">
                  {restoreCandidate.full_name} • {mapPositionLabel(restoreCandidate.position)} • {restoreCandidate.schedule_type}
                </Text>
                <Button onClick={() => fillCreateFormFromCandidate(restoreCandidate)}>
                  Подставить данные удалённого пользователя
                </Button>
              </Space>
            </Card>
          ) : null}

          <Form.Item name="password" label="Пароль" rules={[{ required: true, min: 6, message: 'Минимум 6 символов' }]}>
            <Input.Password placeholder="Пароль" />
          </Form.Item>

          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="first_name" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
                <Input placeholder="Имя" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="last_name" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
                <Input placeholder="Фамилия" />
              </Form.Item>
            </Col>
          </Row>

          <Form.Item name="email" label="Email">
            <Input placeholder="user@example.com" />
          </Form.Item>

          {isBitrixEnabled && createSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть пользователь в Bitrix24. Подключить?</Text>
                  <Text type="secondary">{createSuggestion.name} (ID: {createSuggestion.b24_user_id})</Text>
                </Space>
                <Button
                  type="primary"
                  onClick={() => {
                    createForm.setFieldsValue({
                      external_type: 'bitrix24',
                      external_system_id: String(createSuggestion.b24_user_id),
                    });
                  }}
                >
                  Подставить
                </Button>
              </Space>
            </Card>
          ) : null}

          {isPyrusEnabled && createPyrusSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть сотрудник в Pyrus. Подключить?</Text>
                  <Text type="secondary">
                    {createPyrusSuggestion.name} (ID: {createPyrusSuggestion.pyrus_user_id}
                    {createPyrusSuggestion.email ? `, ${createPyrusSuggestion.email}` : ''})
                  </Text>
                </Space>
                <Button
                  onClick={() => {
                    createForm.setFieldsValue({
                      external_type: 'pyrus',
                      external_system_id: String(createPyrusSuggestion.pyrus_user_id),
                      email: createPyrusSuggestion.email || createForm.getFieldValue('email'),
                    });
                  }}
                >
                  Подставить
                </Button>
              </Space>
            </Card>
          ) : null}

          {createMegafonSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть сотрудник в Мегафон. Подключить?</Text>
                  <Text type="secondary">{createMegafonSuggestion.name} (логин: {createMegafonSuggestion.login})</Text>
                </Space>
                <Button
                  onClick={() => {
                    createForm.setFieldsValue({
                      external_type: 'megafon_vats',
                      external_system_id: createMegafonSuggestion.login,
                    });
                  }}
                >
                  Подставить
                </Button>
              </Space>
            </Card>
          ) : null}

          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
                <Select options={positionOptions} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="schedule_type" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
                <Select options={scheduleOptions} />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={12}>
            <Col span={10}>
              <Form.Item name="external_type" label="Быстрое подключение интеграции">
                <Select allowClear options={availableIntegrationOptions} placeholder="Выберите" />
              </Form.Item>
            </Col>
            <Col span={14}>
              <Form.Item name="external_system_id" label="Внешний ID">
                <Input placeholder={getExternalPlaceholder(watchedCreateExternalType)} />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Modal>

      <Modal
        title="Редактирование сотрудника"
        open={isEditOpen}
        onCancel={resetEditModal}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        okText="Сохранить"
        cancelText="Отмена"
        width={760}
      >
        <Form<UserUpdatePayload> form={editForm} layout="vertical" onFinish={onEdit}>
          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="username" label="Логин">
                <Input disabled={selectedUser?.has_logged_in} placeholder="Логин" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="password" label="Новый пароль">
                <Input.Password
                  disabled={selectedUser?.has_logged_in}
                  placeholder={selectedUser?.has_logged_in ? 'После первого входа пароль меняет сотрудник' : 'Оставьте пустым без изменений'}
                />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="first_name" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
                <Input placeholder="Имя" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="last_name" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
                <Input placeholder="Фамилия" />
              </Form.Item>
            </Col>
          </Row>

          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="email" label="Email">
                <Input placeholder="user@example.com" />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
                <Select options={positionOptions} />
              </Form.Item>
            </Col>
            <Col span={6}>
              <Form.Item name="schedule_type" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
                <Select options={scheduleOptions} />
              </Form.Item>
            </Col>
          </Row>

          {isBitrixEnabled && editSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть пользователь в Bitrix24. Добавить интеграцию?</Text>
                  <Text type="secondary">{editSuggestion.name} (ID: {editSuggestion.b24_user_id})</Text>
                </Space>
                <Button
                  type="primary"
                  onClick={() => {
                    const nextItems = appendIntegration(editForm.getFieldValue('integrations'), 'bitrix24', String(editSuggestion.b24_user_id));
                    editForm.setFieldsValue({ integrations: nextItems });
                  }}
                >
                  Добавить Bitrix24
                </Button>
              </Space>
            </Card>
          ) : null}

          {isPyrusEnabled && editPyrusSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть сотрудник в Pyrus. Добавить интеграцию?</Text>
                  <Text type="secondary">
                    {editPyrusSuggestion.name} (ID: {editPyrusSuggestion.pyrus_user_id}
                    {editPyrusSuggestion.email ? `, ${editPyrusSuggestion.email}` : ''})
                  </Text>
                </Space>
                <Button
                  onClick={() => {
                    const nextItems = appendIntegration(editForm.getFieldValue('integrations'), 'pyrus', String(editPyrusSuggestion.pyrus_user_id));
                    editForm.setFieldsValue({
                      integrations: nextItems,
                      email: editPyrusSuggestion.email || editForm.getFieldValue('email'),
                    });
                  }}
                >
                  Добавить Pyrus
                </Button>
              </Space>
            </Card>
          ) : null}

          {editMegafonSuggestion ? (
            <Card size="small" style={{ marginBottom: 12 }}>
              <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
                <Space direction="vertical" size={0}>
                  <Text strong>Есть сотрудник в Мегафон. Добавить интеграцию?</Text>
                  <Text type="secondary">{editMegafonSuggestion.name} (логин: {editMegafonSuggestion.login})</Text>
                </Space>
                <Button
                  onClick={() => {
                    const nextItems = appendIntegration(editForm.getFieldValue('integrations'), 'megafon_vats', editMegafonSuggestion.login);
                    editForm.setFieldsValue({ integrations: nextItems });
                  }}
                >
                  Добавить Мегафон
                </Button>
              </Space>
            </Card>
          ) : null}

          <Card
            size="small"
            title="Интеграции"
            extra={(
              <Button
                type="link"
                icon={<PlusOutlined />}
                onClick={() => {
                  const nextItems = [...normalizeIntegrationItems(editForm.getFieldValue('integrations')), { integration_type: undefined, external_id: undefined, is_enabled: true }];
                  editForm.setFieldsValue({ integrations: nextItems });
                }}
              >
                Добавить интеграцию
              </Button>
            )}
          >
            <Form.List name="integrations">
              {(fields, { remove }) => (
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                  {fields.length === 0 ? <Text type="secondary">Интеграции ещё не подключены.</Text> : null}
                  {fields.map((field) => (
                    <Row key={field.key} gutter={12} align="middle">
                      <Col span={7}>
                        <Form.Item
                          {...field}
                          name={[field.name, 'integration_type']}
                          label={field.name === 0 ? 'Система' : ' '}
                          rules={[{ required: true, message: 'Выберите систему' }]}
                          style={{ marginBottom: 0 }}
                        >
                          <Select options={editIntegrationOptions} placeholder="Система" />
                        </Form.Item>
                      </Col>
                      <Col span={9}>
                        <Form.Item
                          {...field}
                          name={[field.name, 'external_id']}
                          label={field.name === 0 ? 'Внешний ID' : ' '}
                          rules={[{ required: true, message: 'Введите внешний ID' }]}
                          style={{ marginBottom: 0 }}
                        >
                          <Input placeholder="Внешний ID" />
                        </Form.Item>
                      </Col>
                      <Col span={4}>
                        <Form.Item
                          {...field}
                          name={[field.name, 'is_enabled']}
                          label={field.name === 0 ? 'Активна' : ' '}
                          valuePropName="checked"
                          initialValue={true}
                          style={{ marginBottom: 0 }}
                        >
                          <Switch checkedChildren="Да" unCheckedChildren="Нет" />
                        </Form.Item>
                      </Col>
                      <Col span={4}>
                        <Button danger icon={<DeleteOutlined />} onClick={() => remove(field.name)}>
                          Удалить
                        </Button>
                      </Col>
                    </Row>
                  ))}
                </Space>
              )}
            </Form.List>
          </Card>

          {selectedUser?.has_logged_in ? (
            <Text type="secondary" style={{ display: 'block', marginTop: 12 }}>
              После первого входа администратор больше не меняет логин и пароль сотрудника, но может управлять доступом и интеграциями.
            </Text>
          ) : null}
        </Form>
      </Modal>
    </div>
  );
};

export default UsersAdminPage;
