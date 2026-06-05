import React, { useMemo, useState } from 'react';
import { Badge, Form, Input, Modal, Tag, Tooltip, message } from 'antd';
import { KeyOutlined } from '@ant-design/icons';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { equipmentApi } from '@/api/equipment';
import { InstallServerLicensePayload } from '@/types/api';
import { useAuthStore } from '@/store/authStore';
import { canManageServerActions } from '@/utils/permissions';

interface ServerLicenseStatusTagProps {
  serverID: string;
  status?: string;
  uniqueID?: string;
  displayStatus?: string;
  stopPropagation?: boolean;
  variant?: 'tag' | 'action';
  actionLabel?: string;
  onInstalled?: () => void;
}

const getTagColor = (status: string) => {
  if (status === 'active') return 'green';
  if (status === 'offline') return 'red';
  if (status === 'license') return 'gold';
  return 'default';
};

const getBadgeStatus = (status: string) => {
  if (status === 'active') return 'success' as const;
  if (status === 'offline') return 'error' as const;
  if (status === 'license') return 'warning' as const;
  return 'default' as const;
};

const getStatusLabel = (status: string, displayStatus?: string) => {
  const customLabel = String(displayStatus || '').trim();
  if (customLabel) return customLabel;
  if (status === 'active') return 'Онлайн';
  if (status === 'offline') return 'Офлайн';
  if (status === 'license') return 'Нужна лицензия';
  return String(status || 'unknown').trim().toUpperCase();
};

const ServerLicenseStatusTag: React.FC<ServerLicenseStatusTagProps> = ({
  serverID,
  status,
  uniqueID,
  displayStatus,
  stopPropagation = true,
  variant = 'tag',
  actionLabel = 'Лицензия',
  onInstalled,
}) => {
  const queryClient = useQueryClient();
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [licenseForm] = Form.useForm<InstallServerLicensePayload>();
  const user = useAuthStore((state) => state.user);
  const canManageActions = canManageServerActions(user?.roles);

  const normalizedStatus = useMemo(() => String(status || '').trim().toLowerCase() || 'unknown', [status]);
  const statusLabel = useMemo(
    () => getStatusLabel(normalizedStatus, displayStatus),
    [displayStatus, normalizedStatus],
  );
  const canInstallLicense = canManageActions && (normalizedStatus === 'active' || normalizedStatus === 'license');

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
    ? 'Установить лицензию'
    : 'Установка лицензии доступна для статусов active или license пользователям с должностью администратора или сотрудника техподдержки';

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

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (!canInstallLicense) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      if (stopPropagation) {
        event.stopPropagation();
      }
      licenseForm.setFieldsValue({
        login: '',
        password: '',
        fallback_password: '',
        unique_id: String(uniqueID || '').trim(),
      });
      setIsModalOpen(true);
    }
  };

  return (
    <>
      <Tooltip title={tooltipText}>
        {variant === 'action' ? (
          <button
            type="button"
            className="ticket-equipment-copy-row ticket-equipment-action-button"
            disabled={!canInstallLicense}
            onClick={openModal}
            onKeyDown={handleKeyDown}
          >
            <span className="ticket-equipment-copy-row__content">
              <span className="ticket-equipment-copy-row__label ticket-equipment-copy-row__label--single">{actionLabel}</span>
            </span>
            <span className="ticket-equipment-copy-row__indicator" aria-hidden="true">
              <KeyOutlined />
            </span>
          </button>
        ) : (
          <Tag
            color={getTagColor(normalizedStatus)}
            role={canInstallLicense ? 'button' : undefined}
            tabIndex={canInstallLicense ? 0 : undefined}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              marginInlineEnd: 0,
              cursor: canInstallLicense ? 'pointer' : undefined,
            }}
            onClick={openModal}
            onKeyDown={handleKeyDown}
          >
            <Badge status={getBadgeStatus(normalizedStatus)} text={<span>{statusLabel}</span>} />
            {canInstallLicense ? <KeyOutlined style={{ fontSize: 12 }} /> : null}
          </Tag>
        )}
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
