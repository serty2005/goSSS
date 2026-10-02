import React, { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Modal, Select, Space, Typography } from 'antd';
import { ticketsApi } from '@/api/tickets';

const { Text } = Typography;

type BitrixServicePointModalProps = {
  open: boolean;
  confirmLoading?: boolean;
  okText?: string;
  onCancel: () => void;
  onSubmit: (servicePointId: number) => void;
};

const SEARCH_LIMIT = 50;
const SEARCH_DEBOUNCE_MS = 300;

// Выбор точки обслуживания Bitrix24 для тикета, у компании которого нет сопоставления с точкой.
const BitrixServicePointModal: React.FC<BitrixServicePointModalProps> = ({
  open,
  confirmLoading,
  okText = 'Выбрать и продолжить',
  onCancel,
  onSubmit,
}) => {
  const [pointId, setPointId] = useState<number | undefined>(undefined);
  const [searchInput, setSearchInput] = useState('');
  const [term, setTerm] = useState('');

  useEffect(() => {
    if (!open) {
      setPointId(undefined);
      setSearchInput('');
      setTerm('');
    }
  }, [open]);

  useEffect(() => {
    const timer = window.setTimeout(() => setTerm(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [searchInput]);

  const { data: points = [], isFetching } = useQuery({
    queryKey: ['bitrix-service-points', 'picker', term],
    queryFn: () => ticketsApi.getBitrixServicePoints({ term, limit: SEARCH_LIMIT }),
    enabled: open,
    staleTime: 60_000,
    meta: { globalLoading: false },
  });

  return (
    <Modal
      open={open}
      title="Точка обслуживания Bitrix24"
      okText={okText}
      cancelText="Отмена"
      onCancel={onCancel}
      confirmLoading={confirmLoading}
      okButtonProps={{ disabled: !pointId }}
      destroyOnHidden
      onOk={() => {
        if (pointId) onSubmit(pointId);
      }}
    >
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Text>
          У компании тикета нет сопоставления с точкой обслуживания Bitrix24. Выберите точку: она нужна для связи со сделкой
          и будет запомнена для этой компании.
        </Text>
        <Select
          showSearch
          filterOption={false}
          value={pointId}
          loading={isFetching}
          placeholder="Начните вводить название точки"
          style={{ width: '100%' }}
          options={points.map((item) => ({ value: item.b24_element_id, label: item.name }))}
          onSearch={setSearchInput}
          onChange={(value) => setPointId(value)}
        />
      </Space>
    </Modal>
  );
};

export default BitrixServicePointModal;
