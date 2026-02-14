import React, { useEffect, useMemo, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Descriptions,
  Form,
  Row,
  Select,
  Space,
  Table,
  Tag,
  Typography,
  Upload,
  message,
} from 'antd';
import { ArrowLeftOutlined, InboxOutlined, PlayCircleOutlined, SyncOutlined } from '@ant-design/icons';
import type { UploadFile, UploadProps } from 'antd/es/upload/interface';
import { useNavigate } from 'react-router-dom';
import { bitrixAdminApi } from '@/api/bitrixAdmin';
import type {
  ServicePointImportColumnDTO,
  ServicePointImportPreviewDTO,
  ServicePointSyncApplyResultDTO,
  ServicePointSyncPlanItemDTO,
  ServicePointSyncPreviewDTO,
} from '@/types/api';

const { Title, Text } = Typography;
const { Dragger } = Upload;

const ROW_BATCH_SIZE = 100;

const detectColumn = (columns: ServicePointImportColumnDTO[], variants: string[]): string | undefined => {
  const normalized = variants.map((item) => item.toLowerCase());
  const found = columns.find((column) => {
    const lowerName = column.name.toLowerCase();
    return normalized.some((variant) => lowerName.includes(variant));
  });
  return found?.key;
};

const actionTag = (action: string) => {
  switch (action) {
    case 'create':
      return <Tag color="blue">Добавление</Tag>;
    case 'update':
      return <Tag color="orange">Обновление</Tag>;
    case 'unchanged':
      return <Tag color="green">Без изменений</Tag>;
    case 'ambiguous':
      return <Tag color="red">Неоднозначно</Tag>;
    case 'skipped':
      return <Tag>Пропуск</Tag>;
    default:
      return <Tag>{action}</Tag>;
  }
};

const isActionable = (item: ServicePointSyncPlanItemDTO) => item.action === 'create' || item.action === 'update';

const recomputeSyncPreview = (preview: ServicePointSyncPreviewDTO): ServicePointSyncPreviewDTO => {
  const counts = preview.items.reduce(
    (acc, item) => {
      if (item.action === 'create') acc.to_create += 1;
      if (item.action === 'update') acc.to_update += 1;
      if (item.action === 'unchanged') acc.unchanged += 1;
      if (item.action === 'skipped') acc.skipped += 1;
      if (item.action === 'ambiguous') acc.ambiguous += 1;
      return acc;
    },
    { to_create: 0, to_update: 0, unchanged: 0, skipped: 0, ambiguous: 0 },
  );

  return {
    ...preview,
    processed_rows: preview.items.length,
    ...counts,
  };
};

const ServicePointsImportPage: React.FC = () => {
  const [file, setFile] = useState<File | null>(null);
  const [fileList, setFileList] = useState<UploadFile[]>([]);
  const [preview, setPreview] = useState<ServicePointImportPreviewDTO | null>(null);
  const [syncPreview, setSyncPreview] = useState<ServicePointSyncPreviewDTO | null>(null);
  const [result, setResult] = useState<ServicePointSyncApplyResultDTO | null>(null);
  const [selectedRows, setSelectedRows] = useState<number[]>([]);
  const [visibleCount, setVisibleCount] = useState(ROW_BATCH_SIZE);
  const [rowApplyState, setRowApplyState] = useState<Record<number, 'error'>>({});
  const [mapping, setMapping] = useState<{ code_column?: string; name_column?: string; contract_column?: string }>({});
  const navigate = useNavigate();

  const previewMutation = useMutation({
    mutationFn: (targetFile: File) => bitrixAdminApi.previewServicePointsImport(targetFile),
    onSuccess: (response) => {
      const payload = response?.data;
      if (!payload) {
        message.error('Не удалось получить предпросмотр файла');
        return;
      }

      const nextMapping = {
        code_column: detectColumn(payload.columns, ['код', 'code']),
        name_column: detectColumn(payload.columns, ['точка обслуживания', 'название', 'name', 'точка']),
        contract_column: detectColumn(payload.columns, ['обслуживается', 'контракт', 'договор', 'contract']),
      };

      setPreview(payload);
      setSyncPreview(null);
      setResult(null);
      setRowApplyState({});
      setSelectedRows([]);
      setVisibleCount(ROW_BATCH_SIZE);
      setMapping(nextMapping);
      message.success('Файл прочитан. Проверьте колонки и рассчитайте изменения в Bitrix24.');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось прочитать файл');
      setPreview(null);
      setSyncPreview(null);
      setResult(null);
      setRowApplyState({});
      setSelectedRows([]);
      setVisibleCount(ROW_BATCH_SIZE);
    },
  });

  const syncPreviewMutation = useMutation({
    mutationFn: (params: { targetFile: File; payload: { code_column: string; name_column: string; contract_column: string } }) =>
      bitrixAdminApi.previewServicePointsSync(params.targetFile, params.payload),
    onSuccess: (response) => {
      const payload = response?.data;
      if (!payload) {
        message.error('Не удалось рассчитать план синхронизации');
        return;
      }

      setSyncPreview(payload);
      setResult(null);
      setRowApplyState({});
      setVisibleCount(ROW_BATCH_SIZE);
      const autoSelected = payload.items.filter(isActionable).map((item) => item.row);
      setSelectedRows(autoSelected);
      message.success('План синхронизации рассчитан. Выберите строки для применения.');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Не удалось рассчитать изменения в Bitrix24');
      setSyncPreview(null);
      setResult(null);
      setRowApplyState({});
      setSelectedRows([]);
      setVisibleCount(ROW_BATCH_SIZE);
    },
  });

  const applyMutation = useMutation({
    mutationFn: (params: {
      targetFile: File;
      payload: { code_column: string; name_column: string; contract_column: string; selected_rows: number[] };
    }) => bitrixAdminApi.applyServicePointsImport(params.targetFile, params.payload),
    onSuccess: (response) => {
      const payload = response?.data;
      if (!payload) {
        message.error('Синхронизация завершилась без результата');
        return;
      }

      setResult(payload);

      if (syncPreview) {
        const appliedRowsSet = new Set<number>(payload.applied_rows || []);
        const nextItems = syncPreview.items.filter((item) => !appliedRowsSet.has(item.row));
        setSyncPreview(recomputeSyncPreview({ ...syncPreview, items: nextItems }));
        setSelectedRows((prev) => prev.filter((row) => !appliedRowsSet.has(row)));
      }

      if (payload.errors?.length) {
        const nextStates = { ...rowApplyState };
        selectedRows.forEach((row) => {
          if (!(payload.applied_rows || []).includes(row)) {
            nextStates[row] = 'error';
          }
        });
        setRowApplyState(nextStates);
      }

      message.success('Синхронизация с Bitrix24 завершена');
    },
    onError: (error: any) => {
      message.error(error?.response?.data?.error?.error || 'Ошибка синхронизации с Bitrix24');
    },
  });

  const uploadProps: UploadProps = {
    multiple: false,
    maxCount: 1,
    accept: '.xls,.xlsx,.xlsm,.xltx,.xltm',
    beforeUpload: (incoming) => {
      setFile(incoming as unknown as File);
      setFileList([
        {
          uid: incoming.uid,
          name: incoming.name,
          status: 'done',
          size: incoming.size,
          type: incoming.type,
          originFileObj: incoming,
        },
      ]);
      setPreview(null);
      setSyncPreview(null);
      setResult(null);
      setRowApplyState({});
      setSelectedRows([]);
      setVisibleCount(ROW_BATCH_SIZE);
      previewMutation.mutate(incoming as unknown as File);
      return false;
    },
    onRemove: () => {
      setFile(null);
      setFileList([]);
      setPreview(null);
      setSyncPreview(null);
      setResult(null);
      setRowApplyState({});
      setSelectedRows([]);
      setVisibleCount(ROW_BATCH_SIZE);
    },
    fileList,
  };

  const mappingOptions = useMemo(() => {
    if (!preview) {
      return [];
    }
    return preview.columns.map((column) => ({
      value: column.key,
      label: `${column.key} - ${column.name}`,
    }));
  }, [preview]);

  const previewTableColumns = useMemo(() => {
    if (!preview) {
      return [];
    }

    const mappedColumns = new Set([mapping.code_column, mapping.name_column, mapping.contract_column].filter(Boolean));

    return preview.columns.map((column) => ({
      title: (
        <span style={mappedColumns.has(column.key) ? { color: '#1677ff', fontWeight: 600 } : undefined}>
          {column.key} · {column.name}
        </span>
      ),
      dataIndex: column.key,
      key: column.key,
      ellipsis: true,
      width: 220,
      onCell: () => (
        mappedColumns.has(column.key)
          ? { style: { backgroundColor: 'rgba(22, 119, 255, 0.08)' } }
          : {}
      ),
      render: (value: string) => value || <Text type="secondary">-</Text>,
    }));
  }, [preview, mapping.code_column, mapping.name_column, mapping.contract_column]);

  const planItems = useMemo(() => {
    if (!syncPreview) {
      return [];
    }
    return syncPreview.items.filter((item) => isActionable(item) || rowApplyState[item.row] === 'error');
  }, [syncPreview, rowApplyState]);

  const visiblePlanItems = useMemo(() => planItems.slice(0, visibleCount), [planItems, visibleCount]);

  const syncTableColumns = useMemo(
    () => [
      { title: 'Строка', dataIndex: 'row', key: 'row', width: 90 },
      { title: 'Точка обслуживания', dataIndex: 'name', key: 'name', width: 320 },
      { title: 'Код 1С', dataIndex: 'one_c_code', key: 'one_c_code', width: 160 },
      { title: 'Контракт', dataIndex: 'contract_label', key: 'contract_label', width: 120, render: (v: string) => v || '-' },
      {
        title: 'Действие',
        dataIndex: 'action',
        key: 'action',
        width: 140,
        render: (v: string) => actionTag(v),
      },
      {
        title: 'Результат',
        key: 'apply_result',
        width: 150,
        render: (_: unknown, record: ServicePointSyncPlanItemDTO) => {
          if (rowApplyState[record.row] === 'error') {
            return <Tag color="red">Ошибка</Tag>;
          }
          return <Tag>Ожидает</Tag>;
        },
      },
      {
        title: 'Текущее в Bitrix',
        key: 'current',
        width: 260,
        render: (_: unknown, record: ServicePointSyncPlanItemDTO) => (
          <Space direction="vertical" size={0}>
            <Text type="secondary">Код: {record.current_code || '-'}</Text>
            <Text type="secondary">Контракт: {record.current_contract || '-'}</Text>
          </Space>
        ),
      },
      { title: 'Причина', dataIndex: 'reason', key: 'reason', width: 280, render: (v: string) => v || '-' },
    ],
    [rowApplyState],
  );

  const canBuildSyncPreview = Boolean(file && mapping.code_column && mapping.name_column && mapping.contract_column);
  const canApply = Boolean(canBuildSyncPreview && planItems.length > 0 && selectedRows.length > 0);

  const selectableRowSet = useMemo(() => {
    return new Set(planItems.filter(isActionable).map((item) => item.row));
  }, [planItems]);

  const handleBuildSyncPreview = () => {
    if (!file || !mapping.code_column || !mapping.name_column || !mapping.contract_column) {
      message.warning('Выберите файл и заполните соответствие колонок');
      return;
    }

    syncPreviewMutation.mutate({
      targetFile: file,
      payload: {
        code_column: mapping.code_column,
        name_column: mapping.name_column,
        contract_column: mapping.contract_column,
      },
    });
  };

  const handleApply = () => {
    if (!file || !mapping.code_column || !mapping.name_column || !mapping.contract_column) {
      message.warning('Выберите файл и заполните соответствие колонок');
      return;
    }
    if (selectedRows.length === 0) {
      message.warning('Выберите минимум одну строку для применения');
      return;
    }

    applyMutation.mutate({
      targetFile: file,
      payload: {
        code_column: mapping.code_column,
        name_column: mapping.name_column,
        contract_column: mapping.contract_column,
        selected_rows: selectedRows,
      },
    });
  };

  const handleSelectAll = () => {
    setSelectedRows(Array.from(selectableRowSet));
  };

  const handleClearSelection = () => {
    setSelectedRows([]);
  };

  const handleTableScroll = (event: React.UIEvent<HTMLDivElement>) => {
    const target = event.currentTarget;
    const threshold = 120;
    if (target.scrollTop + target.clientHeight >= target.scrollHeight - threshold) {
      setVisibleCount((prev) => {
        if (prev >= planItems.length) {
          return prev;
        }
        return prev + ROW_BATCH_SIZE;
      });
    }
  };

  useEffect(() => {
    setVisibleCount(ROW_BATCH_SIZE);
  }, [planItems.length]);

  useEffect(() => {
    setSelectedRows((prev) => prev.filter((row) => selectableRowSet.has(row)));
  }, [selectableRowSet]);

  return (
    <div>
      <div style={{ marginBottom: 12 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/admin')}>Назад</Button>
      </div>
      <Row gutter={[16, 16]} align="stretch" style={{ marginBottom: 16 }}>
        <Col xs={24} lg={12}>
          <Card className="glass-panel" style={{ height: '100%' }}>
            <Space direction="vertical" size={10} style={{ width: '100%' }}>
              <div>
                <Title level={4} style={{ marginBottom: 0 }}>Импорт точек обслуживания</Title>
                <Text type="secondary">Загрузите XLS/XLSX из 1С для расчёта плана синхронизации с Bitrix24.</Text>
              </div>

              <Dragger {...uploadProps} style={{ marginTop: 6 }}>
                <p className="ant-upload-drag-icon"><InboxOutlined /></p>
                <p className="ant-upload-text">Перетащите файл сюда или нажмите для выбора</p>
                <p className="ant-upload-hint">Поддерживаются форматы .xls и .xlsx</p>
              </Dragger>

              {previewMutation.isPending && <Text type="secondary">Чтение файла...</Text>}

              {preview ? (
                <>
                  <Alert type="info" showIcon message={`Заголовки: строка ${preview.header_row}. Строк с данными: ${preview.total_rows}.`} />

                  <Form layout="vertical">
                    <Row gutter={12}>
                      <Col span={24}>
                        <Form.Item label="Колонка кода 1С" required style={{ marginBottom: 10 }}>
                          <Select options={mappingOptions} value={mapping.code_column} onChange={(value) => { setMapping((prev) => ({ ...prev, code_column: value })); setSyncPreview(null); setSelectedRows([]); }} />
                        </Form.Item>
                      </Col>
                      <Col span={24}>
                        <Form.Item label="Колонка названия точки" required style={{ marginBottom: 10 }}>
                          <Select options={mappingOptions} value={mapping.name_column} onChange={(value) => { setMapping((prev) => ({ ...prev, name_column: value })); setSyncPreview(null); setSelectedRows([]); }} />
                        </Form.Item>
                      </Col>
                      <Col span={24}>
                        <Form.Item label="Колонка статуса контракта" required style={{ marginBottom: 8 }}>
                          <Select options={mappingOptions} value={mapping.contract_column} onChange={(value) => { setMapping((prev) => ({ ...prev, contract_column: value })); setSyncPreview(null); setSelectedRows([]); }} />
                        </Form.Item>
                      </Col>
                    </Row>
                  </Form>

                  <Button icon={<SyncOutlined />} onClick={handleBuildSyncPreview} loading={syncPreviewMutation.isPending} disabled={!canBuildSyncPreview}>
                    Рассчитать изменения в Bitrix24
                  </Button>
                </>
              ) : (
                <Text type="secondary">После выбора файла здесь появится выбор колонок.</Text>
              )}
            </Space>
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card className="glass-panel" style={{ height: '100%' }}>
            {preview ? (
              <Table
                rowKey={(_, index) => String(index)}
                dataSource={preview.sample_rows}
                columns={previewTableColumns}
                pagination={false}
                scroll={{ x: 'max-content', y: 320 }}
                size="small"
              />
            ) : (
              <Text type="secondary">После чтения файла справа появится таблица предпросмотра.</Text>
            )}
          </Card>
        </Col>
      </Row>

      {syncPreview && (
        <Card className="glass-panel" style={{ marginBottom: 16 }}>
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Space style={{ width: '100%', justifyContent: 'space-between' }}>
              <Title level={5} style={{ marginBottom: 0 }}>План изменений для Bitrix24</Title>
              <Space>
                <Button onClick={handleSelectAll}>Выбрать все</Button>
                <Button onClick={handleClearSelection}>Снять выбор</Button>
                <Button type="primary" icon={<PlayCircleOutlined />} onClick={handleApply} loading={applyMutation.isPending} disabled={!canApply}>
                  Применить выбранные
                </Button>
              </Space>
            </Space>

            <Descriptions bordered size="small" column={4}>
              <Descriptions.Item label="Изменений к применению">{planItems.length}</Descriptions.Item>
              <Descriptions.Item label="Будет добавлено">{syncPreview.to_create}</Descriptions.Item>
              <Descriptions.Item label="Будет обновлено">{syncPreview.to_update}</Descriptions.Item>
              <Descriptions.Item label="Выбрано">{selectedRows.length}</Descriptions.Item>
            </Descriptions>

            <div style={{ maxHeight: 560, overflow: 'auto' }} onScroll={handleTableScroll}>
              <Table<ServicePointSyncPlanItemDTO>
                rowKey={(row) => row.row}
                dataSource={visiblePlanItems}
                columns={syncTableColumns as any}
                pagination={false}
                scroll={{ x: 'max-content' }}
                size="small"
                rowSelection={{
                  selectedRowKeys: selectedRows,
                  onChange: (keys) => setSelectedRows((keys as number[]).filter((k) => selectableRowSet.has(k))),
                  getCheckboxProps: (record) => ({ disabled: !isActionable(record) }),
                }}
              />
            </div>
            {visiblePlanItems.length < planItems.length && (
              <Text type="secondary">Прокрутите вниз для подгрузки следующих строк ({visiblePlanItems.length} из {planItems.length}).</Text>
            )}
          </Space>
        </Card>
      )}

      {result && (
        <Card className="glass-panel">
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Title level={5} style={{ marginBottom: 0 }}>Результат применения</Title>
            <Descriptions bordered size="small" column={3}>
              <Descriptions.Item label="Обработано строк">{result.processed_rows}</Descriptions.Item>
              <Descriptions.Item label="Добавлено">{result.created}</Descriptions.Item>
              <Descriptions.Item label="Обновлено">{result.updated}</Descriptions.Item>
              <Descriptions.Item label="Без изменений">{result.unchanged}</Descriptions.Item>
              <Descriptions.Item label="Пропущено">{result.skipped}</Descriptions.Item>
              <Descriptions.Item label="Неоднозначно">{result.ambiguous}</Descriptions.Item>
            </Descriptions>
            {Boolean(result.errors?.length) && <Alert type="error" showIcon message={result.errors?.join(' | ')} />}
          </Space>
        </Card>
      )}
    </div>
  );
};

export default ServicePointsImportPage;
