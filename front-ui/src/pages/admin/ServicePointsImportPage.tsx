import React, { useEffect, useMemo, useRef, useState } from 'react';
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
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ArrowLeftOutlined, MailOutlined, PlayCircleOutlined, ReloadOutlined, StopOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import { useAuthStore } from '@/store/authStore';
import type { ContractMailImportDTO, ContractSyncQueueItemDTO } from '@/types/api';

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

const actionTag = (action: ContractSyncQueueItemDTO['action']) => {
  switch (action) {
    case 'create':
      return <Tag color="green">Создать</Tag>;
    case 'update':
      return <Tag color="blue">Обновить</Tag>;
    case 'delete':
      return <Tag color="volcano">Удалить</Tag>;
    default:
      return <Tag>{action}</Tag>;
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
  const abortRef = useRef<AbortController | null>(null);

  const [selectedUpsertKeys, setSelectedUpsertKeys] = useState<React.Key[]>([]);
  const [selectedDeleteKeys, setSelectedDeleteKeys] = useState<React.Key[]>([]);
  const [selectedQueueKeys, setSelectedQueueKeys] = useState<React.Key[]>([]);
  const [queueItems, setQueueItems] = useState<ContractSyncQueueItemDTO[]>([]);
  const [isExecuting, setIsExecuting] = useState(false);

  const contractSyncQuery = useQuery({
    queryKey: ['bitrix', 'contract-sync-state'],
    queryFn: () => bitrixAdminApi.getContractSyncState(),
    staleTime: 30_000,
  });

  const syncState = contractSyncQuery.data?.data;
  const latestImport = syncState?.latest_import;
  const activeReportImport = syncState?.active_report_import;
  const recentImports = syncState?.recent_imports || [];
  const baseUpsertItems = syncState?.upsert_items || [];
  const baseDeleteItems = syncState?.delete_items || [];

  useEffect(() => {
    setQueueItems([]);
    setSelectedUpsertKeys([]);
    setSelectedDeleteKeys([]);
    setSelectedQueueKeys([]);
  }, [activeReportImport?.attachment_hash]);

  const queueKeySet = useMemo(() => new Set(queueItems.map((item) => item.key)), [queueItems]);

  const upsertItems = useMemo(
    () => baseUpsertItems.filter((item) => !queueKeySet.has(item.key)),
    [baseUpsertItems, queueKeySet],
  );
  const deleteItems = useMemo(
    () => baseDeleteItems.filter((item) => !queueKeySet.has(item.key)),
    [baseDeleteItems, queueKeySet],
  );

  const addItemsToQueue = (items: ContractSyncQueueItemDTO[], selectedKeys: React.Key[], clearSelection: () => void) => {
    const selected = items.filter((item) => selectedKeys.includes(item.key));
    if (selected.length === 0) {
      return;
    }

    setQueueItems((current) => {
      const existing = new Set(current.map((item) => item.key));
      const next = [...current];
      selected.forEach((item) => {
        if (!existing.has(item.key)) {
          next.push(item);
        }
      });
      return next;
    });
    clearSelection();
  };

  const removeItemsFromQueue = () => {
    const selected = new Set(selectedQueueKeys.map(String));
    if (selected.size === 0) {
      return;
    }
    setQueueItems((current) => current.filter((item) => !selected.has(item.key)));
    setSelectedQueueKeys([]);
  };

  const executeQueue = async () => {
    if (isExecuting) {
      abortRef.current?.abort();
      return;
    }
    if (queueItems.length === 0) {
      return;
    }

    const controller = new AbortController();
    abortRef.current = controller;
    setIsExecuting(true);

    try {
      const result = await bitrixAdminApi.executeContractSync(
        { selected_keys: queueItems.map((item) => item.key) },
        controller.signal,
      );
      const payload = result.data;
      const parts = [
        payload.created ? `создано ${payload.created}` : '',
        payload.updated ? `обновлено ${payload.updated}` : '',
        payload.deleted ? `удалено ${payload.deleted}` : '',
      ].filter(Boolean);

      if (payload.errors && payload.errors.length > 0) {
        message.warning(`Очередь выполнена с ошибками: ${payload.errors.join('; ')}`);
      } else {
        message.success(parts.length > 0 ? parts.join(', ') : 'Очередь выполнена');
      }

      setQueueItems([]);
      setSelectedQueueKeys([]);
      await contractSyncQuery.refetch();
    } catch (error) {
      const axiosError = error as { code?: string; name?: string; message?: string };
      if (axiosError.code === 'ERR_CANCELED' || axiosError.name === 'CanceledError') {
        message.info('Выполнение очереди остановлено');
      } else {
        message.error(getQueryErrorText(error));
      }
    } finally {
      abortRef.current = null;
      setIsExecuting(false);
    }
  };

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
      },
      {
        title: 'Строк',
        dataIndex: 'rows_count',
        key: 'rows_count',
        width: 90,
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
        title: 'Хэш',
        dataIndex: 'attachment_hash',
        key: 'attachment_hash',
        width: 180,
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

  const upsertColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Действие',
        dataIndex: 'action',
        key: 'action',
        width: 120,
        render: (value: ContractSyncQueueItemDTO['action']) => actionTag(value),
      },
      {
        title: 'Точка обслуживания',
        dataIndex: 'service_point_name',
        key: 'service_point_name',
        ellipsis: true,
      },
      {
        title: 'Код точки',
        dataIndex: 'service_point_code',
        key: 'service_point_code',
        width: 150,
      },
      {
        title: 'Контракт',
        dataIndex: 'contract_type',
        key: 'contract_type',
        width: 140,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Bitrix ID',
        dataIndex: 'b24_element_id',
        key: 'b24_element_id',
        width: 110,
        render: (value?: number) => value || '—',
      },
      {
        title: 'Текущий код',
        dataIndex: 'current_code',
        key: 'current_code',
        width: 150,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Текущий контракт',
        dataIndex: 'current_contract_type',
        key: 'current_contract_type',
        width: 150,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Причина',
        dataIndex: 'reason',
        key: 'reason',
        ellipsis: true,
      },
    ],
    [],
  );

  const deleteColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Bitrix ID',
        dataIndex: 'b24_element_id',
        key: 'b24_element_id',
        width: 110,
        render: (value?: number) => value || '—',
      },
      {
        title: 'Точка обслуживания',
        dataIndex: 'service_point_name',
        key: 'service_point_name',
        ellipsis: true,
      },
      {
        title: 'Код точки',
        dataIndex: 'service_point_code',
        key: 'service_point_code',
        width: 150,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Заполнено полей',
        dataIndex: 'filled_fields',
        key: 'filled_fields',
        width: 130,
        render: (value?: number) => value ?? '—',
      },
      {
        title: 'Группа дублей',
        dataIndex: 'matched_point_ids',
        key: 'matched_point_ids',
        width: 220,
        render: (value?: number[]) => (value && value.length > 0 ? value.join(', ') : '—'),
      },
      {
        title: 'Причина удаления',
        dataIndex: 'reason',
        key: 'reason',
        ellipsis: true,
      },
    ],
    [],
  );

  const queueColumns = useMemo<ColumnsType<ContractSyncQueueItemDTO>>(
    () => [
      {
        title: 'Действие',
        dataIndex: 'action',
        key: 'action',
        width: 120,
        render: (value: ContractSyncQueueItemDTO['action']) => actionTag(value),
      },
      {
        title: 'Точка обслуживания',
        dataIndex: 'service_point_name',
        key: 'service_point_name',
        ellipsis: true,
      },
      {
        title: 'Код точки',
        dataIndex: 'service_point_code',
        key: 'service_point_code',
        width: 150,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Контракт',
        dataIndex: 'contract_type',
        key: 'contract_type',
        width: 140,
        render: (value?: string) => value || '—',
      },
      {
        title: 'Bitrix ID',
        dataIndex: 'b24_element_id',
        key: 'b24_element_id',
        width: 110,
        render: (value?: number) => value || '—',
      },
      {
        title: 'Причина',
        dataIndex: 'reason',
        key: 'reason',
        ellipsis: true,
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
          description="Экран ручной синхронизации недоступен, пока ENABLE_BITRIX_GATEWAY=false."
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

      {contractSyncQuery.isError ? (
        <Alert
          type="error"
          showIcon
          message="Не удалось загрузить состояние синхронизации"
          description={getQueryErrorText(contractSyncQuery.error)}
        />
      ) : null}

      <Row gutter={[16, 16]}>
        <Col xs={24} md={6}>
          <Card className="glass-panel">
            <Statistic title="Строк в активном отчёте" value={syncState?.report_rows || 0} />
          </Card>
        </Col>
        <Col xs={24} md={6}>
          <Card className="glass-panel">
            <Statistic title="К созданию / обновлению" value={(syncState?.to_create || 0) + (syncState?.to_update || 0)} />
          </Card>
        </Col>
        <Col xs={24} md={6}>
          <Card className="glass-panel">
            <Statistic title="К удалению дублей" value={syncState?.to_delete || 0} />
          </Card>
        </Col>
        <Col xs={24} md={6}>
          <Card className="glass-panel">
            <Statistic title="Заблокировано дублями" value={syncState?.blocked_rows || 0} />
          </Card>
        </Col>
      </Row>

      <Card
        className="glass-panel"
        title="Удаление неактуальных дублей"
        extra={
          <Button
            type="primary"
            disabled={selectedDeleteKeys.length === 0}
            onClick={() => addItemsToQueue(deleteItems, selectedDeleteKeys, () => setSelectedDeleteKeys([]))}
          >
            Добавить в очередь выполнения
          </Button>
        }
      >
        {deleteItems.length === 0 ? (
          <Alert
            type="success"
            showIcon
            message="Неактуальных дублей нет"
            description="Сейчас в Bitrix24 нет дублей точек обслуживания, подпадающих под правила ручного удаления."
          />
        ) : (
          <Table<ContractSyncQueueItemDTO>
            rowKey="key"
            dataSource={deleteItems}
            columns={deleteColumns}
            rowSelection={{
              selectedRowKeys: selectedDeleteKeys,
              onChange: setSelectedDeleteKeys,
            }}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            scroll={{ x: 1100 }}
          />
        )}
      </Card>

      <Card
        className="glass-panel"
        title="Обновления точек обслуживания"
        extra={
          <Button
            type="primary"
            disabled={selectedUpsertKeys.length === 0}
            onClick={() => addItemsToQueue(upsertItems, selectedUpsertKeys, () => setSelectedUpsertKeys([]))}
          >
            Добавить в очередь выполнения
          </Button>
        }
      >
        {upsertItems.length === 0 ? (
          <Alert
            type="info"
            showIcon
            message="Новых изменений нет"
            description="Последний отчёт не требует создания или обновления точек в Bitrix24."
          />
        ) : (
          <Table<ContractSyncQueueItemDTO>
            rowKey="key"
            dataSource={upsertItems}
            columns={upsertColumns}
            rowSelection={{
              selectedRowKeys: selectedUpsertKeys,
              onChange: setSelectedUpsertKeys,
            }}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            scroll={{ x: 1200 }}
          />
        )}
      </Card>

      <Card
        className="glass-panel"
        title="Очередь выполнения"
        extra={
          <Space>
            <Button
              icon={isExecuting ? <StopOutlined /> : <PlayCircleOutlined />}
              type="primary"
              disabled={!isExecuting && queueItems.length === 0}
              loading={isExecuting}
              onClick={() => void executeQueue()}
            >
              {isExecuting ? 'Стоп' : 'Пуск'}
            </Button>
            <Button disabled={selectedQueueKeys.length === 0} onClick={removeItemsFromQueue}>
              Отменить
            </Button>
          </Space>
        }
      >
        {queueItems.length === 0 ? (
          <Empty description="Очередь выполнения пуста" />
        ) : (
          <Table<ContractSyncQueueItemDTO>
            rowKey="key"
            dataSource={queueItems}
            columns={queueColumns}
            rowSelection={{
              selectedRowKeys: selectedQueueKeys,
              onChange: setSelectedQueueKeys,
            }}
            pagination={{ pageSize: 10, hideOnSinglePage: true }}
            scroll={{ x: 1100 }}
          />
        )}
      </Card>

      <Card className="glass-panel">
        <Space direction="vertical" size={10} style={{ width: '100%' }}>
          <Space size={10}>
            <MailOutlined style={{ fontSize: 18 }} />
            <Title level={4} style={{ margin: 0 }}>
              Отчёты и журнал обработки
            </Title>
          </Space>
          <Text type="secondary">
            Для расчёта очереди используется последний успешно разобранный отчёт из почты. Контракты компаний в
            ServiceDesk обновляются автоматически по нему, а изменения Bitrix24 выполняются только вручную через очередь.
          </Text>
          {latestImport?.status === 'failed' ? (
            <Alert
              type="error"
              showIcon
              message="Последний прогон завершился ошибкой"
              description={latestImport.error_text || 'Подробности ошибки отсутствуют в журнале импорта.'}
            />
          ) : null}
        </Space>
      </Card>

      <Card className="glass-panel" title="Используемый отчёт">
        {activeReportImport ? (
          <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
            <Descriptions.Item label="Статус">{importStatusTag(activeReportImport.status)}</Descriptions.Item>
            <Descriptions.Item label="Вложение">{activeReportImport.attachment_name || '—'}</Descriptions.Item>
            <Descriptions.Item label="Получено">{formatDateTime(activeReportImport.received_at)}</Descriptions.Item>
            <Descriptions.Item label="Обработано">{formatDateTime(activeReportImport.processed_at)}</Descriptions.Item>
            <Descriptions.Item label="Строк">{activeReportImport.rows_count}</Descriptions.Item>
            <Descriptions.Item label="Хэш">{shortValue(activeReportImport.attachment_hash, 8)}</Descriptions.Item>
            <Descriptions.Item label="Message-ID" span={2}>
              {activeReportImport.message_id || '—'}
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Empty description="Нет успешно обработанного отчёта" />
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
    </Space>
  );
};

export default ServicePointsImportPage;
