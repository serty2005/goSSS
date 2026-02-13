import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Drawer, Empty, Form, Input, List, Select, Space, Spin, Table, Tag, Typography, message, Divider } from 'antd';
import dayjs from 'dayjs';
import { networkCandidatesApi } from '@/api/networkCandidates';
import { companiesApi } from '@/api/companies';
import { NetworkCandidateApprovePayload, NetworkCandidateDetailsDTO, NetworkCandidateDTO, NetworkCandidateGroupDTO } from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Title, Text } = Typography;

const STATUS_COLORS: Record<string, string> = {
  NEW: 'blue',
  IN_REVIEW: 'orange',
  APPROVED: 'green',
  REJECTED: 'red',
  CANCELLED: 'default',
};

const NetworkAcceptancePage: React.FC = () => {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<string>('ACTIVE');
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [companyMode, setCompanyMode] = useState<'existing' | 'new'>('existing');
  const [form] = Form.useForm();

  const listQuery = useQuery({
    queryKey: ['network-candidates', status],
    queryFn: () => networkCandidatesApi.list({ status, limit: 200, offset: 0 }),
    staleTime: 10_000,
  });

  const detailsQuery = useQuery({
    queryKey: ['network-candidate', selectedID],
    queryFn: () => networkCandidatesApi.get(selectedID as number),
    enabled: Boolean(selectedID),
  });

  const removeGroupMutation = useMutation({
    mutationFn: async (groupID: number) => networkCandidatesApi.removeGroup(selectedID as number, groupID),
    onSuccess: () => {
      message.success('Группа перенесена в новый кандидат');
      void queryClient.invalidateQueries({ queryKey: ['network-candidates'] });
      void queryClient.invalidateQueries({ queryKey: ['network-candidate', selectedID] });
    },
    onError: () => message.error('Не удалось перенести группу'),
  });

  const approveMutation = useMutation({
    mutationFn: async (payload: NetworkCandidateApprovePayload) => networkCandidatesApi.approve(selectedID as number, payload),
    onSuccess: () => {
      message.success('Network-кандидат подтвержден');
      setDrawerOpen(false);
      setSelectedID(null);
      form.resetFields();
      void queryClient.invalidateQueries({ queryKey: ['network-candidates'] });
    },
    onError: () => message.error('Не удалось подтвердить network-кандидата'),
  });

  const rows = useMemo(() => ((listQuery.data?.data || []) as NetworkCandidateDTO[]), [listQuery.data?.data]);
  const details = useMemo(() => (detailsQuery.data?.data as NetworkCandidateDetailsDTO | undefined), [detailsQuery.data?.data]);

  // Загрузка дочерних компаний hub-компании
  const hubCompanyId = details?.candidate?.hub_company_id;
  const companiesQuery = useQuery({
    queryKey: ['network-candidate-children', hubCompanyId],
    queryFn: () => companiesApi.getChildren(hubCompanyId as string),
    enabled: Boolean(hubCompanyId),
  });

  const companyOptions = useMemo(() => {
    const list = companiesQuery.data?.data || [];
    return list.map((item) => {
      const id = resolveCompanyID(item);
      const title = resolveCompanyTitle(item) || id || '';
      const parentTitle = resolveCompanyParentTitle(item);
      return { value: id, label: parentTitle ? `${parentTitle} / ${title}` : title };
    }).filter((item) => item.value) as Array<{ value: string; label: string }>;
  }, [companiesQuery.data?.data]);

  // Предзаполнение формы при наличии конфликта
  React.useEffect(() => {
    if (details?.candidate && drawerOpen) {
      // При конфликте предзаполняем кандидата по РС (приоритет)
      const preselectedCompany = details.candidate.ws_owner_candidate || details.candidate.fr_owner_candidate;
      if (preselectedCompany) {
        form.setFieldsValue({
          company_mode: 'existing',
          child_company_id: preselectedCompany,
        });
        setCompanyMode('existing');
      }
    }
  }, [details?.candidate, drawerOpen, form]);

  const onApprove = async () => {
    const values = await form.validateFields();
    const payload: NetworkCandidateApprovePayload = { comment: values.comment || '' };
    if (values.company_mode === 'existing') {
      payload.child_company_id = values.child_company_id;
    } else {
      payload.child_company = { title: values.child_company_title, address: values.child_company_address || '' };
    }
    approveMutation.mutate(payload);
  };

  const open = (id: number) => {
    setSelectedID(id);
    setDrawerOpen(true);
    setCompanyMode('existing');
    form.setFieldsValue({ company_mode: 'existing' });
  };

  // Проверка на конфликт
  const hasConflict = details?.candidate?.conflict_info && details.candidate.conflict_info.length > 0;

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Title level={4} style={{ margin: 0 }}>Принятие в сеть</Title>
        <Select
          style={{ width: 220 }}
          value={status}
          onChange={setStatus}
          options={[
            { value: 'ACTIVE', label: 'Активные' },
            { value: 'NEW', label: 'NEW' },
            { value: 'IN_REVIEW', label: 'IN_REVIEW' },
            { value: 'APPROVED', label: 'APPROVED' },
            { value: 'ALL', label: 'Все' },
          ]}
        />
      </Space>

      {listQuery.error && <Alert type="error" message="Не удалось загрузить network-кандидатов" showIcon />}

      <Card className="glass-panel">
        {listQuery.isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Кандидатов нет" />
        ) : (
          <Table<NetworkCandidateDTO>
            rowKey={(row) => String(row.id)}
            dataSource={rows}
            pagination={{ pageSize: 20 }}
            columns={[
              { title: 'ID', dataIndex: 'id', width: 80 },
              { title: 'Статус', dataIndex: 'status', width: 130, render: (v: string) => <Tag color={STATUS_COLORS[v] || 'default'}>{v}</Tag> },
              { title: 'Hub', dataIndex: 'hub_company_id', width: 220 },
              { title: 'Сервер', dataIndex: 'server_id', width: 220 },
              { title: 'CRM', dataIndex: 'server_crm_id', render: (v?: string) => v || '-' },
              { 
                title: 'Конфликт', 
                dataIndex: 'conflict_info', 
                width: 200,
                render: (v?: string) => v ? <Tag color="warning">Конфликт владельцев</Tag> : '-'
              },
              { title: 'Создан', dataIndex: 'created_at', width: 180, render: (v: string) => dayjs(v).format('DD.MM.YYYY HH:mm') },
              { title: 'Действие', key: 'action', width: 160, render: (_: unknown, row: NetworkCandidateDTO) => <Button type="primary" onClick={() => open(row.id)}>Открыть</Button> },
            ]}
          />
        )}
      </Card>

      <Drawer
        title={details?.candidate ? `Network-кандидат #${details.candidate.id}` : 'Network-кандидат'}
        open={drawerOpen}
        size="large"
        onClose={() => { setDrawerOpen(false); setSelectedID(null); }}
        extra={(
          <Space>
            <Button onClick={() => { setDrawerOpen(false); setSelectedID(null); }}>Отмена</Button>
            <Button type="primary" loading={approveMutation.isPending} onClick={() => void onApprove()}>Подтвердить</Button>
          </Space>
        )}
      >
        {!details || detailsQuery.isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : (
          <Space direction="vertical" size="large" style={{ width: '100%' }}>
            <Card size="small" title="Данные кандидата">
              <Space direction="vertical" style={{ width: '100%' }}>
                <Text>Родительская компания (hub): <Text code>{details.candidate.hub_company_id}</Text></Text>
                <Text>Сервер: <Text code>{details.candidate.server_id}</Text></Text>
                <Text>CRM: {details.candidate.server_crm_id || '-'}</Text>
              </Space>
            </Card>

            {/* Отображение информации о конфликте */}
            {hasConflict && (
              <Card size="small" title="Информация о конфликте" style={{ borderColor: '#faad14' }}>
                <Space direction="vertical" style={{ width: '100%' }}>
                  <Alert
                    type="warning"
                    message="Обнаружен конфликт владельцев"
                    description={details.candidate.conflict_info}
                    showIcon
                  />
                  <Divider style={{ margin: '8px 0' }} />
                  <Space>
                    {details.candidate.ws_owner_candidate && (
                      <Text>Кандидат по РС: <Text code>{details.candidate.ws_owner_candidate}</Text></Text>
                    )}
                    {details.candidate.fr_owner_candidate && (
                      <Text>Кандидат по ФР: <Text code>{details.candidate.fr_owner_candidate}</Text></Text>
                    )}
                  </Space>
                </Space>
              </Card>
            )}

            <Card size="small" title="Группы данных">
              {details.groups.length === 0 ? (
                <Empty description="Нет групп" />
              ) : (
                <List
                  dataSource={details.groups}
                  renderItem={(item: NetworkCandidateGroupDTO) => (
                    <List.Item
                      key={item.group.id}
                      actions={[
                        <Button key="remove" danger loading={removeGroupMutation.isPending} onClick={() => removeGroupMutation.mutate(item.group.id)}>
                          Удалить группу
                        </Button>,
                      ]}
                    >
                      <Space direction="vertical">
                        <Text strong>Группа #{item.group.id} (observation: {item.group.observation_id})</Text>
                        <Text>WS: {item.ws?.hostname || item.ws?.workstation_uuid || '-'}</Text>
                        <Text type="secondary">TV: {item.ws?.teamviewer_id || '-'} | LM: {item.ws?.litemanager_id || '-'} | AD: {item.ws?.anydesk_id || '-'}</Text>
                        <Text>ФР в группе: {item.frs.length}</Text>
                      </Space>
                    </List.Item>
                  )}
                />
              )}
            </Card>

            <Card size="small" title="Сопоставление дочерней компании">
              <Form form={form} layout="vertical">
                <Form.Item name="company_mode" label="Режим">
                  <Select
                    value={companyMode}
                    onChange={(v: 'existing' | 'new') => setCompanyMode(v)}
                    options={[
                      { value: 'existing', label: 'Выбрать существующую дочернюю' },
                      { value: 'new', label: 'Создать новую дочернюю' },
                    ]}
                  />
                </Form.Item>
                {companyMode === 'existing' ? (
                  <Form.Item name="child_company_id" label="Дочерняя компания" rules={[{ required: true, message: 'Выберите компанию' }]}>
                    <Select
                      showSearch
                      options={companyOptions}
                      loading={companiesQuery.isLoading}
                      placeholder="Выберите дочернюю компанию"
                    />
                  </Form.Item>
                ) : (
                  <>
                    <Form.Item name="child_company_title" label="Название" rules={[{ required: true, message: 'Введите название' }]}>
                      <Input />
                    </Form.Item>
                    <Form.Item name="child_company_address" label="Адрес">
                      <Input />
                    </Form.Item>
                  </>
                )}
                <Form.Item name="comment" label="Комментарий">
                  <Input.TextArea rows={3} />
                </Form.Item>
              </Form>
            </Card>
          </Space>
        )}
      </Drawer>
    </Space>
  );
};

export default NetworkAcceptancePage;
