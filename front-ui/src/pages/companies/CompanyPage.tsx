import React, { useEffect, useMemo, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Typography, Tabs, Tag, Descriptions, Spin, Empty, Row, Col, Card, Button, Space, Modal, Form, Input, message, Select, Segmented, theme as antTheme, Popconfirm } from 'antd';
import { BankOutlined, CheckCircleOutlined, CloseCircleOutlined, ArrowLeftOutlined, PlusOutlined, EditOutlined } from '@ant-design/icons';
import { companiesApi } from '@/api/companies';
import { contractsApi } from '@/api/contracts';
import { deletionCandidatesApi } from '@/api/deletionCandidates';
import { ServerEntity, WorkstationEntity, FiscalEntity, ContractDetailDTO } from '@/types/api';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import TicketTable from '@/components/tickets/TicketTable';
import { useAuthStore } from '@/store/authStore';
import { canEditCompanyBase, canEditCompanyContract, isAdmin } from '@/utils/permissions';
import { resolveCompanyID } from '@/utils/companyHierarchy';

const { Title, Text } = Typography;

const contractTypeOptions = [
  'TS Cloud',
  'TS Standart (без выездов)',
  'TS Standart',
];

const normalizeServices = (raw: ContractDetailDTO['services']): string[] => {
  if (Array.isArray(raw)) {
    return raw.map((item) => String(item));
  }
  if (raw && typeof raw === 'object') {
    return Object.values(raw as Record<string, unknown>).map((item) => String(item));
  }
  return [];
};

const CompanyPage: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [isCompanyEditOpen, setIsCompanyEditOpen] = useState(false);
  const [isContractEditOpen, setIsContractEditOpen] = useState(false);
  const [ticketScope, setTicketScope] = useState<'own' | 'with_children'>('own');
  const [companyForm] = Form.useForm<{ title: string; address: string }>();
  const [contractForm] = Form.useForm<{ contract_type: string; contract_state: 'active' | 'inactive' }>();
  const user = useAuthStore((state) => state.user);
  const currentUserID = String(user?.id || '');
  const canEditBase = canEditCompanyBase(user?.roles);
  const canEditContract = canEditCompanyContract(user?.roles);
  const canDeleteCompany = isAdmin(user?.roles);
  const canViewDeletionCandidate = canEditBase || canDeleteCompany;

  const { data: companyRes, isLoading: loadingCompany } = useQuery({
    queryKey: ['company', id],
    queryFn: () => companiesApi.getCompany(id!),
    enabled: !!id,
  });

  const { data: infraRes, isLoading: loadingInfra } = useQuery({
    queryKey: ['company', id, 'infra'],
    queryFn: () => companiesApi.getInfrastructure(id!),
    enabled: !!id,
  });

  const { data: companyDeletionCandidateRes } = useQuery({
    queryKey: ['deletion-candidate', 'Company', id],
    queryFn: () => deletionCandidatesApi.getByEntity('Company', id!),
    enabled: Boolean(id) && canViewDeletionCandidate,
  });

  const { data: companyChildrenIDs = [], isLoading: loadingCompanyChildren } = useQuery({
    queryKey: ['company', id, 'children-tree'],
    enabled: !!id,
    queryFn: async () => {
      const rootID = String(id || '').trim();
      if (!rootID) {
        return [] as string[];
      }

      const visited = new Set<string>([rootID]);
      const childIDs: string[] = [];
      const queue: string[] = [rootID];

      while (queue.length > 0) {
        const parentID = queue.shift()!;
        const response = await companiesApi.getChildren(parentID);
        const items = response?.data || [];
        items.forEach((item) => {
          const childID = String(item.id || '').trim();
          if (!childID || visited.has(childID)) {
            return;
          }
          visited.add(childID);
          childIDs.push(childID);
          queue.push(childID);
        });
      }

      return childIDs;
    },
    staleTime: 60_000,
  });

  const company = companyRes?.data;
  const companyDeletionCandidate = companyDeletionCandidateRes?.data || null;
  const canOpenDeletionCandidateInTasks = Boolean(
    companyDeletionCandidate
      && canDeleteCompany
      && String(companyDeletionCandidate.requested_by_user_id || '') !== currentUserID,
  );
  const contractID = company?.contract_id;
  const contractType = company?.contract_type;

  const { data: contractRes } = useQuery({
    queryKey: ['contract', contractID, 'company-modal'],
    queryFn: () => contractsApi.getContract(contractID!),
    enabled: isContractEditOpen && !!contractID,
  });

  useEffect(() => {
    if (!isContractEditOpen) {
      return;
    }
    const liveContractType = normalizeServices(contractRes?.data?.services)[0];
    contractForm.setFieldsValue({
      contract_type: liveContractType || contractType || contractTypeOptions[0],
      contract_state: ((contractRes?.data?.state) === 'active' ? 'active' : 'inactive'),
    });
  }, [contractRes?.data?.services, contractRes?.data?.state, contractForm, contractType, isContractEditOpen]);

  const updateCompanyMutation = useMutation({
    mutationFn: async (values: { title: string; address: string }) => {
      return companiesApi.updateCompany(id!, {
        title: values.title.trim(),
        address: values.address.trim(),
      });
    },
    onSuccess: () => {
      message.success('Компания обновлена');
      setIsCompanyEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['company', id] });
      queryClient.invalidateQueries({ queryKey: ['tickets'] });
    },
    onError: () => {
      message.error('Не удалось обновить компанию');
    },
  });

  const requestCompanyDeletionMutation = useMutation({
    mutationFn: async () => {
      return deletionCandidatesApi.request({
        entity_type: 'Company',
        entity_id: id!,
        reason: 'Ручная постановка компании в кандидаты на удаление',
      });
    },
    onSuccess: () => {
      message.success('Компания добавлена в кандидаты на удаление');
      queryClient.invalidateQueries({ queryKey: ['deletion-candidate', 'Company', id] });
      queryClient.invalidateQueries({ queryKey: ['deletion-candidates'] });
      setIsCompanyEditOpen(false);
    },
    onError: () => {
      message.error('Не удалось добавить компанию в кандидаты на удаление');
    },
  });

  const updateContractMutation = useMutation({
    mutationFn: async (values: { contract_type: string; contract_state: 'active' | 'inactive' }) => {
      if (!contractID) {
        throw new Error('Отсутствует контракт');
      }

      const currentContract = await contractsApi.getContract(contractID);
      const services = normalizeServices(currentContract.data.services);
      const nextServices = services.length > 0 ? [...services] : [''];
      nextServices[0] = values.contract_type;

      return contractsApi.updateContract(contractID, {
        state: values.contract_state,
        services: nextServices,
      });
    },
    onSuccess: () => {
      message.success('Тип контракта обновлён');
      setIsContractEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['company', id] });
      queryClient.invalidateQueries({ queryKey: ['contract', contractID, 'company-modal'] });
    },
    onError: () => {
      message.error('Не удалось обновить тип контракта');
    },
  });

  const groupedInfra = useMemo(() => {
    const rawInfrastructure = infraRes?.data ?? [];
    const servers: ServerEntity[] = [];
    const workstations: WorkstationEntity[] = [];
    const fiscals: FiscalEntity[] = [];

    rawInfrastructure.forEach((item) => {
      if (item.entity_type === 'Server') servers.push(item.data as ServerEntity);
      else if (item.entity_type === 'Workstation') workstations.push(item.data as WorkstationEntity);
      else if (item.entity_type === 'FiscalRegister') fiscals.push(item.data as FiscalEntity);
    });

    return { servers, workstations, fiscals };
  }, [infraRes?.data]);

  useEffect(() => {
    if (companyChildrenIDs.length === 0 && ticketScope !== 'own') {
      setTicketScope('own');
    }
  }, [companyChildrenIDs.length, ticketScope]);

  if (loadingCompany) {
    return (
      <div style={{ padding: 50, textAlign: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }

  if (!company) return <Empty description="Компания не найдена" />;
  const companyID = resolveCompanyID(company) || company.id || '';
  const hasChildCompanies = companyChildrenIDs.length > 0;
  const ticketCompanyIDs = ticketScope === 'with_children' && hasChildCompanies
    ? [companyID, ...companyChildrenIDs]
    : [companyID];

  const parentTitle = company.parent_title;

  const openCompanyEdit = () => {
    companyForm.setFieldsValue({
      title: company.title || '',
      address: company.address || '',
    });
    setIsCompanyEditOpen(true);
  };

  const openContractEdit = () => {
    if (!contractID) return;

    const resolvedType = normalizeServices(contractRes?.data?.services)[0] || contractType || contractTypeOptions[0];
    const resolvedState = (contractRes?.data?.state) === 'active' ? 'active' : 'inactive';
    contractForm.setFieldsValue({
      contract_type: resolvedType,
      contract_state: resolvedState,
    });
    setIsContractEditOpen(true);
  };

  const renderSection = (title: string, count: number, content: React.ReactNode) => {
    if (count === 0) return null;

    return (
      <section style={{ marginBottom: 16 }}>
        <Space size={8} align="baseline" style={{ marginBottom: 10 }}>
          <Title level={5} style={{ margin: 0 }}>{title}</Title>
          <Text type="secondary">{count}</Text>
        </Space>
        {content}
      </section>
    );
  };

  const items = [
    {
      key: 'infrastructure',
      label: 'Инфраструктура',
      children: (
        <div style={{ marginTop: 10 }}>
          {loadingInfra ? <Spin /> : (
            <>
              {renderSection(
                'Серверы',
                groupedInfra.servers.length,
                <Row gutter={[12, 12]}>
                  {groupedInfra.servers.map((srv) => (
                    <Col key={srv.uuid} xs={24} sm={12} lg={8} xl={6}>
                      <ServerCard data={srv} />
                    </Col>
                  ))}
                </Row>,
              )}

              {renderSection(
                'Фискальные регистраторы',
                groupedInfra.fiscals.length,
                <Row gutter={[12, 12]}>
                  {groupedInfra.fiscals.map((fr) => (
                    <Col key={fr.uuid} xs={24} sm={12} lg={8} xl={6}>
                      <FiscalCard data={fr} />
                    </Col>
                  ))}
                </Row>,
              )}

              {renderSection(
                'Рабочие станции',
                groupedInfra.workstations.length,
                <Row gutter={[12, 12]}>
                  {groupedInfra.workstations.map((ws) => (
                    <Col key={ws.uuid} xs={24} sm={12} lg={8} xl={6}>
                      <WorkstationCard data={ws} />
                    </Col>
                  ))}
                </Row>,
              )}

              {(infraRes?.data || []).length === 0 && <Empty description="Оборудование не найдено" />}
            </>
          )}
        </div>
      ),
    },
    {
      key: 'tickets',
      label: 'Тикеты',
      children: (
        <div style={{ marginTop: 10 }}>
          <div style={{ marginBottom: 10, display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
            <Space size={8} wrap>
              {hasChildCompanies && (
                <Segmented
                  size="small"
                  value={ticketScope}
                  onChange={(value) => setTicketScope(value as 'own' | 'with_children')}
                  options={[
                    { label: 'Только текущая', value: 'own' },
                    { label: 'Текущая + дочерние', value: 'with_children' },
                  ]}
                />
              )}
              {loadingCompanyChildren && (
                <Text type="secondary">Загрузка дочерних компаний...</Text>
              )}
            </Space>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => console.log('Create Ticket')}>
              Создать тикет
            </Button>
          </div>
          <TicketTable companyIds={ticketCompanyIDs} limit={10} />
        </div>
      ),
    },
    {
      key: 'contracts',
      label: 'Контракты',
      children: <Empty description="Раздел в разработке" style={{ marginTop: 12 }} />,
    },
  ];

  return (
    <div>
      <Card className="glass-panel company-summary-card" style={{ marginBottom: 12 }} size="small" bodyStyle={{ padding: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
          <Link to="/companies" style={{ display: 'inline-flex', alignItems: 'center', color: token.colorTextSecondary }}>
            <ArrowLeftOutlined style={{ marginRight: 8 }} /> Назад к списку
          </Link>
          {canEditBase && (
            <Button icon={<EditOutlined />} size="small" onClick={openCompanyEdit}>Редактировать</Button>
          )}
        </div>

        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 10 }}>
          <div style={{ display: 'flex', alignItems: 'center', minWidth: 0 }}>
            <div
              style={{
                width: 38,
                height: 38,
                background: 'var(--app-color-primary-soft)',
                borderRadius: 8,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                marginRight: 10,
                fontSize: 18,
                color: token.colorPrimary,
                flexShrink: 0,
              }}
            >
              <BankOutlined />
            </div>
            <div style={{ minWidth: 0 }}>
              <Title level={5} style={{ margin: 0 }}>{company.title}</Title>
              <Text type="secondary" ellipsis style={{ display: 'block', fontSize: 12 }}>{company.address || '-'}</Text>
            </div>
          </div>

          <div style={{ textAlign: 'right', flexShrink: 0 }}>
            <Space size={8} wrap style={{ justifyContent: 'flex-end' }}>
              {companyDeletionCandidate && (
                <Tag
                  color="orange"
                  style={{ marginRight: 0, cursor: canOpenDeletionCandidateInTasks ? 'pointer' : 'default' }}
                  onClick={() => {
                    if (!canOpenDeletionCandidateInTasks) return;
                    navigate(`/tasks?deletion_candidate_id=${companyDeletionCandidate.id}`);
                  }}
                >
                  Кандидат на удаление
                </Tag>
              )}
              {company.active_contract ? (
                <Tag icon={<CheckCircleOutlined />} color="success" style={{ marginRight: 0 }}>Активен</Tag>
              ) : (
                <Tag icon={<CloseCircleOutlined />} color="default" style={{ marginRight: 0 }}>Завершён</Tag>
              )}
            </Space>
          </div>
        </div>

        <Descriptions bordered size="small" column={2} className="compact-descriptions" style={{ marginTop: 10 }}>
          <Descriptions.Item label="Юр. название">{company.additional_name || '-'}</Descriptions.Item>
          <Descriptions.Item label="Родительская компания">
            {company.parent_id && parentTitle ? <Link to={`/companies/${company.parent_id}`}>{parentTitle}</Link> : parentTitle || '-'}
          </Descriptions.Item>
          <Descriptions.Item label="Контракт" span={2}>
            {contractID ? (
              <Space size={8}>
                {canEditContract ? (
                  <Button type="link" size="small" style={{ padding: 0 }} onClick={openContractEdit}>
                    {contractType || 'Не указан'}
                  </Button>
                ) : (
                  <Text>{contractType || 'Не указан'}</Text>
                )}
              </Space>
            ) : '-'}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Tabs defaultActiveKey="infrastructure" items={items} />

      <Modal
        title="Редактирование компании"
        open={isCompanyEditOpen}
        onCancel={() => setIsCompanyEditOpen(false)}
        footer={
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
            <div>
              {canDeleteCompany && (
                <Popconfirm
                  title="Подтверждаете удаление?"
                  description="Окончательное удаление сущности будет подтверждено другим администратором"
                  okText="Да"
                  cancelText="Нет"
                  onConfirm={() => requestCompanyDeletionMutation.mutate()}
                  disabled={Boolean(companyDeletionCandidate)}
                >
                  <Button
                    danger
                    disabled={Boolean(companyDeletionCandidate)}
                    loading={requestCompanyDeletionMutation.isPending}
                  >
                    {companyDeletionCandidate ? 'Уже в кандидатах' : 'Удалить'}
                  </Button>
                </Popconfirm>
              )}
            </div>
            <Space>
              <Button onClick={() => setIsCompanyEditOpen(false)}>Отмена</Button>
              <Button
                type="primary"
                loading={updateCompanyMutation.isPending}
                onClick={() => companyForm.submit()}
              >
                Сохранить
              </Button>
            </Space>
          </div>
        }
      >
        <Form form={companyForm} layout="vertical" onFinish={(values) => updateCompanyMutation.mutate(values)}>
          <Form.Item label="Название" name="title" rules={[{ required: true, message: 'Введите название компании' }]}>
            <Input placeholder="Название компании" />
          </Form.Item>
          <Form.Item label="Адрес" name="address" rules={[{ required: true, message: 'Введите адрес' }]}>
            <Input.TextArea rows={3} placeholder="Адрес компании" />
          </Form.Item>
          <Form.Item label="Адресный классификатор">
            <Button disabled block>Подключение классификатора (скоро)</Button>
          </Form.Item>
          {canDeleteCompany && companyDeletionCandidate && (
            <Form.Item style={{ marginBottom: 0 }}>
              <Tag color="orange" style={{ marginRight: 0 }}>
                Компания уже в кандидатах на удаление (ID #{companyDeletionCandidate.id})
              </Tag>
            </Form.Item>
          )}
        </Form>
      </Modal>

      <Modal
        title="Параметры контракта"
        open={isContractEditOpen}
        onCancel={() => setIsContractEditOpen(false)}
        onOk={() => contractForm.submit()}
        confirmLoading={updateContractMutation.isPending}
        okText="Сохранить"
        cancelText="Отмена"
        width={420}
      >
        <Form form={contractForm} layout="vertical" onFinish={(values) => updateContractMutation.mutate(values)}>
          <Form.Item
            label="Статус"
            name="contract_state"
            rules={[{ required: true, message: 'Выберите статус контракта' }]}
          >
            <Select
              options={[
                { label: 'Активен', value: 'active' },
                { label: 'Неактивен', value: 'inactive' },
              ]}
              placeholder="Выберите статус"
            />
          </Form.Item>
          <Form.Item
            label="Тип контракта"
            name="contract_type"
            rules={[{ required: true, message: 'Выберите тип контракта' }]}
          >
            <Select
              options={contractTypeOptions.map((item) => ({ label: item, value: item }))}
              placeholder="Выберите тип контракта"
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default CompanyPage;
