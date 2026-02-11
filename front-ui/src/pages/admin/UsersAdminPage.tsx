import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
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
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { PlusOutlined, StopOutlined, CheckCircleOutlined, EditOutlined } from '@ant-design/icons';
import { usersApi } from '@/api/users';
import { UserAdminDTO, UserCreatePayload, UserPosition, UserSchedule, UserUpdatePayload } from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

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

const externalTypeOptions: { label: string; value: string }[] = [
  { label: 'Telegram', value: 'telegram' },
  { label: 'Naumen', value: 'naumen' },
  { label: 'Bitrix24', value: 'bitrix24' },
];

const mapPositionLabel = (position: UserPosition): string => {
  const found = positionOptions.find((item) => item.value === position);
  return found?.label ?? position;
};

const getExternalPlaceholder = (externalType?: string): string => {
  switch (externalType) {
    case 'telegram':
      return '@login';
    case 'naumen':
      return '$uuid';
    case 'bitrix24':
      return '12345';
    default:
      return 'ID внешней системы';
  }
};

const UsersAdminPage: React.FC = () => {
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserAdminDTO | null>(null);
  const [createForm] = Form.useForm<UserCreatePayload>();
  const [editForm] = Form.useForm<UserUpdatePayload>();
  const queryClient = useQueryClient();
  const currentUser = useAuthStore((state) => state.user);

  const watchedCreateExternalType = Form.useWatch('externalType', createForm);
  const watchedEditExternalType = Form.useWatch('externalType', editForm);

  const { data, isLoading } = useQuery({
    queryKey: ['admin-users'],
    queryFn: () => usersApi.getUsers(),
  });

  const users = data?.data ?? [];

  const createMutation = useMutation({
    mutationFn: (payload: UserCreatePayload) => usersApi.createUser(payload),
    onSuccess: () => {
      message.success('Пользователь создан');
      setIsCreateOpen(false);
      createForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось создать пользователя');
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, payload }: { id: number; payload: UserUpdatePayload }) => usersApi.updateUser(id, payload),
    onSuccess: () => {
      message.success('Пользователь обновлён');
      setIsEditOpen(false);
      setSelectedUser(null);
      editForm.resetFields();
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось обновить пользователя');
    },
  });

  const statusMutation = useMutation({
    mutationFn: ({ id, isActive }: { id: number; isActive: boolean }) => usersApi.updateUserStatus(id, isActive),
    onSuccess: (_, variables) => {
      message.success(variables.isActive ? 'Пользователь разблокирован' : 'Пользователь заблокирован');
      queryClient.invalidateQueries({ queryKey: ['admin-users'] });
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось изменить статус пользователя');
    },
  });

  const openEditModal = (user: UserAdminDTO) => {
    setSelectedUser(user);
    editForm.setFieldsValue({
      username: user.username,
      firstName: user.firstName,
      lastName: user.lastName,
      position: user.position,
      scheduleType: user.scheduleType,
      externalType: user.externalType,
      externalSystemId: user.externalSystemId,
      password: undefined,
    });
    setIsEditOpen(true);
  };

  const columns: ColumnsType<UserAdminDTO> = useMemo(
    () => [
      {
        title: 'Сотрудник',
        key: 'fullName',
        render: (_, record) => (
          <Space direction="vertical" size={0}>
            <Text strong>{record.fullName}</Text>
            <Text type="secondary">@{record.username}</Text>
          </Space>
        ),
      },
      {
        title: 'Должность',
        dataIndex: 'position',
        key: 'position',
        render: (position: UserPosition) => mapPositionLabel(position),
      },
      {
        title: 'График',
        dataIndex: 'scheduleType',
        key: 'scheduleType',
      },
      {
        title: 'Внешняя система',
        key: 'external',
        render: (_, record) => {
          if (!record.externalSystemId && !record.externalType) {
            return <Text type="secondary">Не указано</Text>;
          }
          return <Text>{record.externalType}: {record.externalSystemId}</Text>;
        },
      },
      {
        title: 'Первый вход',
        dataIndex: 'hasLoggedIn',
        key: 'hasLoggedIn',
        width: 140,
        render: (hasLoggedIn: boolean) => (hasLoggedIn ? <Tag color="blue">Выполнен</Tag> : <Tag>Не было</Tag>),
      },
      {
        title: 'Статус',
        dataIndex: 'isActive',
        key: 'isActive',
        width: 140,
        render: (isActive: boolean) =>
          isActive ? <Tag color="success">Активен</Tag> : <Tag color="default">Заблокирован</Tag>,
      },
      {
        title: 'Действия',
        key: 'actions',
        width: 320,
        render: (_, record) => {
          const isCurrentUser = currentUser?.id === record.id;
          return (
            <Space>
              <Button icon={<EditOutlined />} onClick={() => openEditModal(record)}>
                Редактировать
              </Button>
              {record.isActive ? (
                <Popconfirm
                  title="Заблокировать пользователя?"
                  description="Пользователь не сможет войти в систему."
                  okText="Заблокировать"
                  cancelText="Отмена"
                  onConfirm={() => statusMutation.mutate({ id: record.id, isActive: false })}
                  disabled={isCurrentUser}
                >
                  <Button danger icon={<StopOutlined />} disabled={isCurrentUser} loading={statusMutation.isPending}>
                    Заблокировать
                  </Button>
                </Popconfirm>
              ) : (
                <Button
                  type="default"
                  icon={<CheckCircleOutlined />}
                  loading={statusMutation.isPending}
                  onClick={() => statusMutation.mutate({ id: record.id, isActive: true })}
                >
                  Разблокировать
                </Button>
              )}
            </Space>
          );
        },
      },
    ],
    [currentUser?.id, statusMutation]
  );

  const normalizePayload = (values: UserCreatePayload | UserUpdatePayload) => ({
    ...values,
    username: values.username?.trim(),
    firstName: values.firstName?.trim(),
    lastName: values.lastName?.trim(),
    password: values.password?.trim() || undefined,
    externalType: values.externalType?.trim() || undefined,
    externalSystemId: values.externalSystemId?.trim() || undefined,
  });

  const onCreate = (values: UserCreatePayload) => {
    createMutation.mutate(normalizePayload(values) as UserCreatePayload);
  };

  const onEdit = (values: UserUpdatePayload) => {
    if (!selectedUser) {
      return;
    }

    const payload = normalizePayload(values) as UserUpdatePayload;
    if (selectedUser.hasLoggedIn) {
      delete payload.username;
      delete payload.password;
    }

    updateMutation.mutate({ id: selectedUser.id, payload });
  };

  return (
    <div>
      <Card className="glass-panel" style={{ marginBottom: 16 }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }}>
          <div>
            <Title level={4} style={{ marginBottom: 0 }}>Сотрудники</Title>
            <Text type="secondary">Создание, редактирование и блокировка учетных записей сотрудников</Text>
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
          pagination={{ pageSize: 10 }}
        />
      </Card>

      <Modal
        title="Новый сотрудник"
        open={isCreateOpen}
        onCancel={() => setIsCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        okText="Создать"
        cancelText="Отмена"
      >
        <Form<UserCreatePayload>
          form={createForm}
          layout="vertical"
          onFinish={onCreate}
          initialValues={{ position: 'intern', scheduleType: '5/2' }}
        >
          <Form.Item name="username" label="Логин" rules={[{ required: true, message: 'Введите логин' }]}>
            <Input placeholder="Логин" />
          </Form.Item>

          <Form.Item name="password" label="Пароль" rules={[{ required: true, min: 6, message: 'Минимум 6 символов' }]}>
            <Input.Password placeholder="Пароль" />
          </Form.Item>

          <Form.Item name="firstName" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
            <Input placeholder="Имя" />
          </Form.Item>

          <Form.Item name="lastName" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
            <Input placeholder="Фамилия" />
          </Form.Item>

          <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
            <Select options={positionOptions} />
          </Form.Item>

          <Form.Item name="scheduleType" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
            <Select options={scheduleOptions} />
          </Form.Item>

          <Row gutter={12}>
            <Col span={10}>
              <Form.Item name="externalType" label="Внешняя система">
                <Select allowClear options={externalTypeOptions} placeholder="Выберите" />
              </Form.Item>
            </Col>
            <Col span={14}>
              <Form.Item name="externalSystemId" label="ID">
                <Input placeholder={getExternalPlaceholder(watchedCreateExternalType)} />
              </Form.Item>
            </Col>
          </Row>
        </Form>
      </Modal>

      <Modal
        title="Редактирование сотрудника"
        open={isEditOpen}
        onCancel={() => {
          setIsEditOpen(false);
          setSelectedUser(null);
          editForm.resetFields();
        }}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        okText="Сохранить"
        cancelText="Отмена"
      >
        <Form<UserUpdatePayload>
          form={editForm}
          layout="vertical"
          onFinish={onEdit}
        >
          <Form.Item name="username" label="Логин">
            <Input disabled={selectedUser?.hasLoggedIn} placeholder="Логин" />
          </Form.Item>

          <Form.Item name="password" label="Новый пароль">
            <Input.Password
              disabled={selectedUser?.hasLoggedIn}
              placeholder={selectedUser?.hasLoggedIn ? 'После первого входа меняет только сотрудник' : 'Оставьте пустым, если без изменений'}
            />
          </Form.Item>

          <Form.Item name="firstName" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
            <Input placeholder="Имя" />
          </Form.Item>

          <Form.Item name="lastName" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
            <Input placeholder="Фамилия" />
          </Form.Item>

          <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
            <Select options={positionOptions} />
          </Form.Item>

          <Form.Item name="scheduleType" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
            <Select options={scheduleOptions} />
          </Form.Item>

          <Row gutter={12}>
            <Col span={10}>
              <Form.Item name="externalType" label="Внешняя система">
                <Select allowClear options={externalTypeOptions} placeholder="Выберите" />
              </Form.Item>
            </Col>
            <Col span={14}>
              <Form.Item name="externalSystemId" label="ID">
                <Input placeholder={getExternalPlaceholder(watchedEditExternalType)} />
              </Form.Item>
            </Col>
          </Row>

          {selectedUser?.hasLoggedIn && (
            <Text type="secondary">Логин и пароль уже нельзя менять администратору после первого входа сотрудника.</Text>
          )}
        </Form>
      </Modal>
    </div>
  );
};

export default UsersAdminPage;
