import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Input, Modal, Radio, Select, Space } from 'antd';
import type { ManagerTransferContactType, ManagerTransferTarget } from '@/types/api';

export type ManagerTransferPayload = {
  managerTransferTarget: ManagerTransferTarget;
  clientContactType: ManagerTransferContactType;
  clientContactValue: string;
};

type ManagerTransferModalProps = {
  open: boolean;
  initialTarget?: ManagerTransferTarget | string;
  initialContactPhone?: string;
  confirmLoading?: boolean;
  onCancel: () => void;
  onSubmit: (payload: ManagerTransferPayload) => void;
};

const MANAGER_TRANSFER_OPTIONS: Array<{ value: ManagerTransferTarget; label: string }> = [
  { value: 'sales', label: 'Продажи клиентам (Ирина)' },
  { value: 'payment_review', label: 'Разбор оплат (Алина)' },
];

const normalizeInitialTarget = (value?: ManagerTransferTarget | string): ManagerTransferTarget =>
  value === 'payment_review' ? 'payment_review' : 'sales';

const ManagerTransferModal: React.FC<ManagerTransferModalProps> = ({
  open,
  initialTarget,
  initialContactPhone,
  confirmLoading,
  onCancel,
  onSubmit,
}) => {
  const [target, setTarget] = useState<ManagerTransferTarget>('sales');
  const [contactType, setContactType] = useState<ManagerTransferContactType>('phone');
  const [contactValue, setContactValue] = useState('');
  const wasOpenRef = useRef(false);

  useEffect(() => {
    if (open && !wasOpenRef.current) {
      setTarget(normalizeInitialTarget(initialTarget));
      setContactType('phone');
      setContactValue(initialContactPhone || '');
    }
    wasOpenRef.current = open;
  }, [initialContactPhone, initialTarget, open]);

  const isReady = useMemo(
    () => Boolean(target) && Boolean(contactType) && contactValue.trim().length > 0,
    [contactType, contactValue, target],
  );

  return (
    <Modal
      open={open}
      title="Передать менеджеру"
      okText="Передать"
      cancelText="Отмена"
      onCancel={onCancel}
      confirmLoading={confirmLoading}
      okButtonProps={{ disabled: !isReady }}
      destroyOnHidden
      onOk={() => {
        if (!isReady) return;
        onSubmit({
          managerTransferTarget: target,
          clientContactType: contactType,
          clientContactValue: contactValue.trim(),
        });
      }}
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Select
          value={target}
          options={MANAGER_TRANSFER_OPTIONS}
          onChange={(value) => setTarget(value)}
          style={{ width: '100%' }}
        />
        <Radio.Group
          value={contactType}
          onChange={(event) => {
            const nextType = event.target.value as ManagerTransferContactType;
            setContactType(nextType);
            setContactValue(nextType === 'phone' ? initialContactPhone || '' : '');
          }}
        >
          <Radio.Button value="phone">Телефон</Radio.Button>
          <Radio.Button value="telegram">Telegram</Radio.Button>
        </Radio.Group>
        <Input
          value={contactValue}
          onChange={(event) => setContactValue(event.target.value)}
          placeholder={contactType === 'phone' ? 'Телефон клиента' : '@telegram_login'}
        />
      </Space>
    </Modal>
  );
};

export default ManagerTransferModal;
