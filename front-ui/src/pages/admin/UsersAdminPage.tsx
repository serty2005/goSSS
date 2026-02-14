import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
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

const getExternalPlaceholder = (external_type?: string): string => {
  switch (external_type) {
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
  const navigate = useNavigate();

  const watchedCreateExternalType = Form.useWatch('external_type', createForm);
  const watchedEditExternalType = Form.useWatch('external_type', editForm);

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
    mutationFn: ({ id, is_active }: { id: number; is_active: boolean }) => usersApi.updateUserStatus(id, is_active),
    onSuccess: (_, variables) => {
      message.success(variables.is_active ? 'Пользователь разблокирован' : 'Пользователь заблокирован');
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
      first_name: user.first_name,
      last_name: user.last_name,
      position: user.position,
      schedule_type: user.schedule_type,
      external_type: user.external_type,
      external_system_id: user.external_system_id,
      password: undefined,
    });
    setIsEditOpen(true);
  };

  const columns: ColumnsType<UserAdminDTO> = useMemo(
    () => [
      {
        title: 'Сотрудник',
        key: 'full_name',
        render: (_, record) => (
          <Space direction="vertical" size={0}>
            <Text strong>{record.full_name}</Text>
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
        dataIndex: 'schedule_type',
        key: 'schedule_type',
      },
      {
        title: 'Внешняя система',
        key: 'external',
        render: (_, record) => {
          if (!record.external_system_id && !record.external_type) {
            return <Text type="secondary">Не указано</Text>;
          }
          return <Text>{record.external_type}: {record.external_system_id}</Text>;
        },
      },
      {
        title: 'Первый вход',
        dataIndex: 'has_logged_in',
        key: 'has_logged_in',
        width: 140,
        render: (has_logged_in: boolean) => (has_logged_in ? <Tag color="blue">Выполнен</Tag> : <Tag>Не было</Tag>),
      },
      {
        title: 'Статус',
        dataIndex: 'is_active',
        key: 'is_active',
        width: 140,
        render: (is_active: boolean) =>
          is_active ? <Tag color="success">Активен</Tag> : <Tag color="default">Заблокирован</Tag>,
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
              {record.is_active ? (
                <Popconfirm
                  title="Заблокировать пользователя?"
                  description="Пользователь не сможет войти в систему."
                  okText="Заблокировать"
                  cancelText="Отмена"
                  onConfirm={() => statusMutation.mutate({ id: record.id, is_active: false })}
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
                  onClick={() => statusMutation.mutate({ id: record.id, is_active: true })}
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
    first_name: values.first_name?.trim(),
    last_name: values.last_name?.trim(),
    password: values.password?.trim() || undefined,
    external_type: values.external_type?.trim() || undefined,
    external_system_id: values.external_system_id?.trim() || undefined,
  });

  const onCreate = (values: UserCreatePayload) => {
    createMutation.mutate(normalizePayload(values) as UserCreatePayload);
  };

  const onEdit = (values: UserUpdatePayload) => {
    if (!selectedUser) {
      return;
    }

    const payload = normalizePayload(values) as UserUpdatePayload;
    if (selectedUser.has_logged_in) {
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

      <Card className="glass-panel" style={{ marginTop: 16 }}>
        <Space style={{ width: '100%', justifyContent: 'space-between' }}>
          <div>
            <Title level={5} style={{ marginBottom: 0 }}>Импорт точек обслуживания из 1С</Title>
            <Text type="secondary">Загрузка XLS/XLSX и привязка кодов 1С к существующим точкам Bitrix24</Text>
          </div>
          <Button type="primary" onClick={() => navigate('/admin/service-points-import')}>
            Открыть форму импорта
          </Button>
        </Space>
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
          initialValues={{ position: 'intern', schedule_type: '5/2' }}
        >
          <Form.Item name="username" label="Логин" rules={[{ required: true, message: 'Введите логин' }]}>
            <Input placeholder="Логин" />
          </Form.Item>

          <Form.Item name="password" label="Пароль" rules={[{ required: true, min: 6, message: 'Минимум 6 символов' }]}>
            <Input.Password placeholder="Пароль" />
          </Form.Item>

          <Form.Item name="first_name" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
            <Input placeholder="Имя" />
          </Form.Item>

          <Form.Item name="last_name" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
            <Input placeholder="Фамилия" />
          </Form.Item>

          <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
            <Select options={positionOptions} />
          </Form.Item>

          <Form.Item name="schedule_type" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
            <Select options={scheduleOptions} />
          </Form.Item>

          <Row gutter={12}>
            <Col span={10}>
              <Form.Item name="external_type" label="Внешняя система">
                <Select allowClear options={externalTypeOptions} placeholder="Выберите" />
              </Form.Item>
            </Col>
            <Col span={14}>
              <Form.Item name="external_system_id" label="ID">
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
            <Input disabled={selectedUser?.has_logged_in} placeholder="Логин" />
          </Form.Item>

          <Form.Item name="password" label="Новый пароль">
            <Input.Password
              disabled={selectedUser?.has_logged_in}
              placeholder={selectedUser?.has_logged_in ? 'После первого входа меняет только сотрудник' : 'Оставьте пустым, если без изменений'}
            />
          </Form.Item>

          <Form.Item name="first_name" label="Имя" rules={[{ required: true, message: 'Введите имя' }]}>
            <Input placeholder="Имя" />
          </Form.Item>

          <Form.Item name="last_name" label="Фамилия" rules={[{ required: true, message: 'Введите фамилию' }]}>
            <Input placeholder="Фамилия" />
          </Form.Item>

          <Form.Item name="position" label="Должность" rules={[{ required: true, message: 'Выберите должность' }]}>
            <Select options={positionOptions} />
          </Form.Item>

          <Form.Item name="schedule_type" label="График" rules={[{ required: true, message: 'Выберите график' }]}>
            <Select options={scheduleOptions} />
          </Form.Item>

          <Row gutter={12}>
            <Col span={10}>
              <Form.Item name="external_type" label="Внешняя система">
                <Select allowClear options={externalTypeOptions} placeholder="Выберите" />
              </Form.Item>
            </Col>
            <Col span={14}>
              <Form.Item name="external_system_id" label="ID">
                <Input placeholder={getExternalPlaceholder(watchedEditExternalType)} />
              </Form.Item>
            </Col>
          </Row>

          {selectedUser?.has_logged_in && (
            <Text type="secondary">Логин и пароль уже нельзя менять администратору после первого входа сотрудника.</Text>
          )}
        </Form>
      </Modal>
    </div>
  );
};

export default UsersAdminPage;


