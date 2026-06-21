import React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Descriptions, Empty, Modal, Skeleton, Tag } from 'antd';
import dayjs from 'dayjs';
import { Link } from 'react-router-dom';
import { contractsApi } from '@/api/contracts';

type Props = {
  open: boolean;
  contractId?: string;
  companyTitle?: string;
  isCommonContract?: boolean;
  onClose: () => void;
};

const formatDate = (value?: string) => value ? dayjs(value).format('DD.MM.YYYY HH:mm') : '-';

const ContractInfoModal: React.FC<Props> = ({ open, contractId, companyTitle, isCommonContract, onClose }) => {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['contract', contractId],
    queryFn: () => contractsApi.getContract(contractId!),
    enabled: open && Boolean(contractId),
  });
  const contract = data?.data;
  const services = Array.isArray(contract?.services) ? contract.services : Object.values(contract?.services || {});

  return (
    <Modal
      open={open}
      title="Информация о контракте"
      onCancel={onClose}
      footer={<Button onClick={onClose}>Закрыть</Button>}
    >
      {isLoading ? <Skeleton active paragraph={{ rows: 5 }} /> : isError || !contract ? (
        <Empty description={contractId ? 'Не удалось загрузить контракт' : 'Контракт не указан'} />
      ) : (
        <Descriptions bordered size="small" column={1}>
          {companyTitle && <Descriptions.Item label="Компания">{companyTitle}</Descriptions.Item>}
          <Descriptions.Item label="Контракт">
            {contract.id || contractId}
            {isCommonContract && <Tag color="gold" style={{ marginInlineStart: 8 }}>Платный</Tag>}
          </Descriptions.Item>
          <Descriptions.Item label="Тип">{String(services[0] || '-')}</Descriptions.Item>
          <Descriptions.Item label="Статус">
            <Tag color={contract.state === 'active' ? 'success' : 'default'}>
              {contract.state === 'active' ? 'Активен' : 'Неактивен'}
            </Tag>
          </Descriptions.Item>
          <Descriptions.Item label="Создан">{formatDate(contract.created_at)}</Descriptions.Item>
          <Descriptions.Item label="Обновлен">{formatDate(contract.updated_at)}</Descriptions.Item>
          <Descriptions.Item label="Компании">
            {(contract.companies || []).length > 0
              ? (contract.companies || []).map((company, index) => (
                <React.Fragment key={company.id}>
                  {index > 0 && ', '}
                  <Link to={`/companies/${company.id}`} onClick={onClose}>{company.title || company.id}</Link>
                </React.Fragment>
              ))
              : '-'}
          </Descriptions.Item>
        </Descriptions>
      )}
    </Modal>
  );
};

export default ContractInfoModal;
