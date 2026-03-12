import React, { useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Row,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeftOutlined, MailOutlined, ReloadOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import { useAuthStore } from '@/store/authStore';
import type { ContractMailImportDTO, ContractServicePointConflictDTO } from '@/types/api';

const { Title, Text } = Typography;

const formatDateTime = (value?: string) => {
  if (!value) {
    return '—';
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return date.toLocaleString('ru-RU');
};

const shortValue = (value?: string, size = 10) => {
  if (!value) {
    return '—';
  }
  if (value.length <= size * 2) {
    return value;
  }
  return `${value.slice(0, size)}…${value.slice(-size)}`;
};

const importStatusTag = (status: string) => {
  switch (status) {
    case 'processed':
      return <Tag color="green">Обработан</Tag>;
    case 'failed':
      return <Tag color="red">Ошибка</Tag>;
    default:
      return <Tag>{status || '—'}</Tag>;
  }
};

const conflictTypeTag = (type: string) => {
  switch (type) {
    case 'duplicate_name':
      return <Tag color="red">Дубль по имени</Tag>;
    default:
      return <Tag>{type || '—'}</Tag>;
  }
};

const getQueryErrorText = (error: unknown) => {
  const payload = error as { response?: { data?: { error?: { error?: string } } }; message?: string } | undefined;
  return payload?.response?.data?.error?.error || payload?.message || 'Не удалось загрузить состояние синхронизации';
};

const ServicePointsImportPage: React.FC = () => {
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const navigate = useNavigate();

  const contractSyncQuery = useQuery({
    queryKey: ['bitrix', 'contract-sync-state'],
    queryFn: () => bitrixAdminApi.getContractSyncState(),
    staleTime: 30_000,
  });

  const syncState = contractSyncQuery.data?.data;
  const latestImport = syncState?.latest_import;
  const recentImports = syncState?.recent_imports || [];
  const conflicts = syncState?.conflicts || [];
  const deletionCandidatesCount = conflicts.reduce((sum, item) => sum + (item.deletion_candidate_ids?.length || 0), 0);

  const importColumns = useMemo<ColumnsType<ContractMailImportDTO>>(
    () => [
      {
        title: 'Статус',
        dataIndex: 'status',
        key: 'status',
        width: 120,
        render: (value: string) => importStatusTag(value),
      },
      {
        title: 'Вложение',
        dataIndex: 'attachment_name',
        key: 'attachment_name',
        ellipsis: true,
        render: (value: string) => value || '—',
      },
      {
        title: 'Получено',
        dataIndex: 'received_at',
        key: 'received_at',
        width: 180,
        render: (value?: string) => formatDateTime(value),
      },
      {
        title: 'Обработано',
        dataIndex: 'processed_at',
        key: 'processed_at',
        width: 180,
        render: (value?: string) => formatDateTime(value),
      },
      {
        title: 'Хэш архива',
        dataIndex: 'attachment_hash',
        key: 'attachment_hash',
        width: 220,
        render: (value: string) => shortValue(value, 8),
      },
      {
        title: 'Ошибка',
        dataIndex: 'error_text',
        key: 'error_text',
        ellipsis: true,
        render: (value?: string) => value || '—',
      },
    ],
    [],
  );

  const conflictColumns = useMemo<ColumnsType<ContractServicePointConflictDTO>>(
    () => [
      {
        title: 'Конфликт',
        dataIndex: 'conflict_type',
        key: 'conflict_type',
        width: 160,
        render: (value: string) => conflictTypeTag(value),
      },
      {
        title: 'Точка обслуживания',
        dataIndex: 'service_point_name',
        key: 'service_point_name',
        ellipsis: true,
      },
      {
        title: 'Идентификатор контрагента',
        dataIndex: 'contractor_id',
        key: 'contractor_id',
        width: 220,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Дубли Bitrix24',
        dataIndex: 'matched_point_ids',
        key: 'matched_point_ids',
        width: 220,
        render: (value?: number[]) => (value && value.length > 0 ? value.join(', ') : '—'),
      },
      {
        title: 'Сопоставлены с компаниями',
        dataIndex: 'mapped_point_ids',
        key: 'mapped_point_ids',
        width: 220,
        render: (value?: number[]) => (value && value.length > 0 ? value.join(', ') : '—'),
      },
      {
        title: 'К удалению',
        dataIndex: 'deletion_candidate_ids',
        key: 'deletion_candidate_ids',
        width: 220,
        render: (value?: number[]) =>
          value && value.length > 0 ? <Tag color="volcano">{value.join(', ')}</Tag> : <Text type="secondary">—</Text>,
      },
      {
        title: 'Источник',
        dataIndex: 'attachment_hash',
        key: 'attachment_hash',
        width: 220,
        render: (value?: string) => shortValue(value, 8),
      },
      {
        title: 'Обновлено',
        dataIndex: 'updated_at',
        key: 'updated_at',
        width: 180,
        render: (value: string) => formatDateTime(value),
      },
    ],
    [],
  );

  if (!isBitrixEnabled) {
    return (
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/admin/users')}>
          Назад к сотрудникам
        </Button>
        <Alert
          type="warning"
          showIcon
          message="Интеграция Bitrix24 отключена"
          description="Экран контроля почтовой синхронизации недоступен, пока ENABLE_BITRIX_GATEWAY=false."
        />
      </Space>
    );
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/admin')}>
          Назад
        </Button>
        <Button
          icon={<ReloadOutlined />}
          onClick={() => void contractSyncQuery.refetch()}
          loading={contractSyncQuery.isFetching}
        >
          Обновить данные
        </Button>
      </Space>

      <Card className="glass-panel">
        <Space direction="vertical" size={10} style={{ width: '100%' }}>
          <Space size={10}>
            <MailOutlined style={{ fontSize: 18 }} />
            <Title level={4} style={{ margin: 0 }}>
              Почтовая синхронизация точек обслуживания
            </Title>
          </Space>
          <Text type="secondary">
            Загрузка XLSX больше не используется. Источник данных для точек обслуживания и контрактов теперь один:
            ежедневная рассылка на почтовый ящик, которую воркер забирает по IMAP.
          </Text>
          <Alert
            type="info"
            showIcon
            message="Как работает обновление"
            description="Если для точки уже есть сопоставление с компанией или найдено точное совпадение по имени без дублей, воркер обновляет Bitrix24 автоматически. Дубли по имени ниже выносятся на ручное решение оператором."
          />
          {latestImport?.status === 'failed' ? (
            <Alert
              type="error"
              showIcon
              message="Последний импорт завершился ошибкой"
              description={latestImport.error_text || 'Подробности ошибки отсутствуют в журнале импорта.'}
            />
          ) : null}
          {contractSyncQuery.isError ? (
            <Alert
              type="error"
              showIcon
              message="Не удалось загрузить состояние синхронизации"
              description={getQueryErrorText(contractSyncQuery.error)}
            />
          ) : null}
        </Space>
      </Card>

      <Row gutter={[16, 16]}>
        <Col xs={24} md={8}>
          <Card className="glass-panel">
            <Statistic title="Импортов в журнале" value={recentImports.length} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card className="glass-panel">
            <Statistic title="Актуальных конфликтов" value={conflicts.length} />
          </Card>
        </Col>
        <Col xs={24} md={8}>
          <Card className="glass-panel">
            <Statistic
              title="Точек в списке удаления"
              value={deletionCandidatesCount}
            />
          </Card>
        </Col>
      </Row>

      <Card className="glass-panel" title="Последний прогон воркера">
        {latestImport ? (
          <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
            <Descriptions.Item label="Статус">{importStatusTag(latestImport.status)}</Descriptions.Item>
            <Descriptions.Item label="Вложение">{latestImport.attachment_name || '—'}</Descriptions.Item>
            <Descriptions.Item label="Получено">{formatDateTime(latestImport.received_at)}</Descriptions.Item>
            <Descriptions.Item label="Обработано">{formatDateTime(latestImport.processed_at)}</Descriptions.Item>
            <Descriptions.Item label="Message-ID">{latestImport.message_id || '—'}</Descriptions.Item>
            <Descriptions.Item label="Хэш архива">{latestImport.attachment_hash || '—'}</Descriptions.Item>
            <Descriptions.Item label="Ошибка" span={2}>
              {latestImport.error_text || '—'}
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Empty description="Почтовые отчёты ещё не обрабатывались" />
        )}
      </Card>

      <Card
        className="glass-panel"
        title="История почтовых импортов"
        extra={<Text type="secondary">Показываются последние 20 прогонов</Text>}
      >
        <Table<ContractMailImportDTO>
          rowKey="id"
          dataSource={recentImports}
          columns={importColumns}
          loading={contractSyncQuery.isLoading}
          pagination={{ pageSize: 10, hideOnSinglePage: true }}
          scroll={{ x: 1000 }}
          locale={{ emptyText: 'История импортов пуста' }}
        />
      </Card>

      <Card
        className="glass-panel"
        title="Конфликты дублей точек Bitrix24"
        extra={<Text type="secondary">Показываются все группы дублей по имени и кандидаты на удаление без mapping</Text>}
      >
        {conflicts.length === 0 ? (
          <Alert
            type="success"
            showIcon
            message="Конфликтов нет"
            description="Сейчас среди точек Bitrix24 нет актуальных дублей по имени, требующих ручного решения."
          />
        ) : (
          <Table<ContractServicePointConflictDTO>
            rowKey="id"
            dataSource={conflicts}
            columns={conflictColumns}
            loading={contractSyncQuery.isLoading}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            scroll={{ x: 1100 }}
          />
        )}
      </Card>
    </Space>
  );
};

export default ServicePointsImportPage;
