import React, { useMemo, useState } from 'react';
import { Form, Input, Modal, Tag, Tooltip, message } from 'antd';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { equipmentApi } from '@/api/equipment';
import { InstallServerLicensePayload } from '@/types/api';
import { useAuthStore } from '@/store/authStore';
import { canEditEquipment } from '@/utils/permissions';

interface ServerLicenseStatusTagProps {
  serverID: string;
  status?: string;
  uniqueID?: string;
  displayStatus?: string;
  stopPropagation?: boolean;
  onInstalled?: () => void;
}

const getTagColor = (status: string) => {
  if (status === 'active') return 'green';
  if (status === 'offline') return 'red';
  if (status === 'license') return 'gold';
  return 'default';
};

const ServerLicenseStatusTag: React.FC<ServerLicenseStatusTagProps> = ({
  serverID,
  status,
  uniqueID,
  displayStatus,
  stopPropagation = true,
  onInstalled,
}) => {
  const queryClient = useQueryClient();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [licenseForm] = Form.useForm<InstallServerLicensePayload>();
  const user = useAuthStore((state) => state.user);
  const canEdit = canEditEquipment(user?.roles);

  const normalizedStatus = useMemo(() => String(status || '').trim().toLowerCase() || 'unknown', [status]);
  const statusLabel = useMemo(
    () => String(displayStatus || normalizedStatus || 'unknown').trim().toUpperCase(),
    [displayStatus, normalizedStatus],
  );
  const canInstallLicense = canEdit && (normalizedStatus === 'active' || normalizedStatus === 'license');

  const installLicenseMutation = useMutation({
    mutationFn: (payload: InstallServerLicensePayload) => equipmentApi.installServerLicense(serverID, payload),
    onSuccess: (res) => {
      const result = res?.data?.result;
      const version = result?.server_version ? `, версия ${result.server_version}` : '';
      const newStatus = result?.status ? `, статус ${result.status}` : '';
      message.success(`Лицензия установлена успешно${newStatus}${version}`);
      setIsModalOpen(false);
      void queryClient.invalidateQueries({ queryKey: ['server', serverID] });
      void queryClient.invalidateQueries({ queryKey: ['equipment', 'servers'] });
      onInstalled?.();
    },
    onError: () => message.error('Не удалось установить лицензию'),
  });

  const tooltipText = canInstallLicense
    ? 'Нажмите для установки лицензии'
    : 'Установка лицензии доступна только для статусов active или license и прав редактирования';

  const openModal = (event: React.MouseEvent) => {
    if (stopPropagation) {
      event.stopPropagation();
    }
    if (!canInstallLicense) return;
    licenseForm.setFieldsValue({
      login: '',
      password: '',
      fallback_password: '',
      unique_id: String(uniqueID || '').trim(),
    });
    setIsModalOpen(true);
  };

  return (
    <>
      <Tooltip title={tooltipText}>
        <Tag
          color={getTagColor(normalizedStatus)}
          style={canInstallLicense ? { cursor: 'pointer' } : undefined}
          onClick={openModal}
        >
          {statusLabel}
        </Tag>
      </Tooltip>

      <Modal
        title="Установка лицензии сервера"
        open={isModalOpen}
        onCancel={() => setIsModalOpen(false)}
        onOk={() => {
          licenseForm
            .validateFields()
            .then((values) => installLicenseMutation.mutate(values))
            .catch(() => undefined);
        }}
        okText="Установить"
        cancelText="Отмена"
        confirmLoading={installLicenseMutation.isPending}
      >
        <Form<InstallServerLicensePayload> form={licenseForm} layout="vertical">
          <Form.Item name="login" label="Логин" rules={[{ required: true, message: 'Укажите логин' }]}>
            <Input autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="Пароль" rules={[{ required: true, message: 'Укажите пароль' }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item name="fallback_password" label="Резервный пароль (опционально)">
            <Input.Password autoComplete="off" />
          </Form.Item>
          <Form.Item name="unique_id" label="UID" rules={[{ required: true, message: 'Укажите UID' }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};

export default ServerLicenseStatusTag;
