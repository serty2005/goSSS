import React, { useEffect, useMemo, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { Typography, Tabs, Tag, Descriptions, Spin, Empty, Card, Button, Space, Modal, Form, Input, message, Select, Segmented, theme as antTheme, Popconfirm } from 'antd';
import { BankOutlined, CheckCircleOutlined, CloseCircleOutlined, ArrowLeftOutlined, PlusOutlined, EditOutlined, CopyOutlined } from '@ant-design/icons';
import { companiesApi } from '@/api/companies';
import { contractsApi } from '@/api/contracts';
import { deletionCandidatesApi } from '@/api/deletionCandidates';
import { ServerEntity, WorkstationEntity, FiscalEntity, ContractDetailDTO, CompanyModel } from '@/types/api';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import TicketTable from '@/components/tickets/TicketTable';
import { CompanySearchSelect } from '@/components/companies/CompanySearchSelect';
import { useBackNavigation } from '@/hooks/useBackNavigation';
import MaterialsPanel from '@/components/materials/MaterialsPanel';
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

const resolveServerTitle = (server: ServerEntity) => (
  server.device_name
  || server.server_name
  || server.ip
  || server.unique_id
  || server.uuid
);

type NetworkCompanyNode = {
  id: string;
  parentID: string;
  depth: number;
  company: CompanyModel;
};

const CompanyPage: React.FC = () => {
  const { token } = antTheme.useToken();
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const goBack = useBackNavigation('/companies');
  const queryClient = useQueryClient();
  const [isCompanyEditOpen, setIsCompanyEditOpen] = useState(false);
  const [isContractEditOpen, setIsContractEditOpen] = useState(false);
  const [ticketScope, setTicketScope] = useState<'own' | 'with_children'>('own');
  const [companyForm] = Form.useForm<{ title: string; address: string; parent_id?: string }>();
  const [contractForm] = Form.useForm<{ contract_type: string; contract_state: 'active' | 'inactive' }>();
  const [companySearch, setCompanySearch] = useState('');
  const [spreadCompanySearch, setSpreadCompanySearch] = useState('');
  const [spreadCompanyID, setSpreadCompanyID] = useState<string | undefined>(undefined);
  const [isNetworkContractModalOpen, setIsNetworkContractModalOpen] = useState(false);
  const [selectedNetworkContractCompanyID, setSelectedNetworkContractCompanyID] = useState('');
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
    placeholderData: (previous) => previous,
  });

  const { data: infraRes, isLoading: loadingInfra } = useQuery({
    queryKey: ['company', id, 'infra'],
    queryFn: () => companiesApi.getInfrastructure(id!),
    enabled: !!id,
    placeholderData: (previous) => previous,
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
  const companyID = resolveCompanyID(company || {}) || company?.id || '';
  const parentCompanyID = String(company?.parent_id || '').trim();

  const { data: bitrixMapping } = useQuery({
    queryKey: ['company-bitrix-mapping', companyID, 'company-card'],
    queryFn: () => companiesApi.getBitrixMappingByCompanyID(companyID),
    enabled: Boolean(companyID),
    staleTime: 30_000,
  });

  const { data: contractRes } = useQuery({
    queryKey: ['contract', contractID, 'company-modal'],
    queryFn: () => contractsApi.getContract(contractID!),
    enabled: isContractEditOpen && !!contractID,
  });

  const { data: companySearchRes } = useQuery({
    queryKey: ['companies-search-company-page', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 20, 0),
    staleTime: 30_000,
  });

  const { data: networkRootCompanyRes, isLoading: loadingNetworkRootCompany } = useQuery({
    queryKey: ['company', parentCompanyID, 'network-root'],
    queryFn: () => companiesApi.getCompany(parentCompanyID),
    enabled: Boolean(parentCompanyID),
    staleTime: 30_000,
  });

  const networkRootCompany = useMemo(() => {
    if (parentCompanyID) {
      return networkRootCompanyRes?.data;
    }
    return company;
  }, [company, networkRootCompanyRes?.data, parentCompanyID]);

  const networkRootID = resolveCompanyID(networkRootCompany || {}) || networkRootCompany?.id || '';

  const { data: networkGraphNodes = [], isLoading: loadingNetworkGraph } = useQuery({
    queryKey: ['company', networkRootID, 'network-graph'],
    enabled: Boolean(networkRootID),
    staleTime: 30_000,
    queryFn: async () => {
      const rootID = String(networkRootID || '').trim();
      if (!rootID) {
        return [] as NetworkCompanyNode[];
      }

      const visited = new Set<string>([rootID]);
      const queue: Array<{ id: string; depth: number }> = [{ id: rootID, depth: 0 }];
      const nodes: NetworkCompanyNode[] = [];

      while (queue.length > 0) {
        const current = queue.shift()!;
        const childrenRes = await companiesApi.getChildren(current.id);
        const children = childrenRes?.data || [];
        children.forEach((child) => {
          const childID = String(resolveCompanyID(child || {}) || child.id || '').trim();
          if (!childID || visited.has(childID)) {
            return;
          }
          visited.add(childID);
          nodes.push({
            id: childID,
            parentID: current.id,
            depth: current.depth + 1,
            company: child,
          });
          queue.push({ id: childID, depth: current.depth + 1 });
        });
      }

      return nodes;
    },
  });

  const hasNetwork = Boolean(parentCompanyID) || networkGraphNodes.length > 0;
  const networkCompanyIDs = useMemo(() => {
    if (!networkRootID) {
      return [] as string[];
    }
    const ids = new Set<string>([networkRootID]);
    networkGraphNodes.forEach((node) => ids.add(node.id));
    return Array.from(ids);
  }, [networkGraphNodes, networkRootID]);

  const networkCompanyProfileQueries = useQueries({
    queries: hasNetwork
      ? networkCompanyIDs.map((networkCompanyID) => ({
        queryKey: ['company', networkCompanyID, 'profile', 'network'],
        queryFn: () => companiesApi.getCompany(networkCompanyID),
        staleTime: 30_000,
      }))
      : [],
  });

  const loadingNetworkProfiles = networkCompanyProfileQueries.some((query) => query.isLoading);
  const networkCompanyByID = useMemo(() => {
    const result = new Map<string, CompanyModel>();
    if (networkRootID && networkRootCompany) {
      result.set(networkRootID, networkRootCompany);
    }
    networkGraphNodes.forEach((node) => {
      result.set(node.id, node.company);
    });
    networkCompanyIDs.forEach((networkCompanyID, index) => {
      const profile = networkCompanyProfileQueries[index]?.data?.data;
      if (profile) {
        result.set(networkCompanyID, profile);
      }
    });
    return result;
  }, [networkCompanyIDs, networkCompanyProfileQueries, networkGraphNodes, networkRootCompany, networkRootID]);

  const networkNodes = useMemo(() => {
    if (!networkRootID) {
      return [] as NetworkCompanyNode[];
    }
    const rootCompanyData = networkCompanyByID.get(networkRootID) || networkRootCompany || {};
    const nodes: NetworkCompanyNode[] = [{
      id: networkRootID,
      parentID: '',
      depth: 0,
      company: rootCompanyData,
    }];
    networkGraphNodes.forEach((node) => {
      nodes.push({
        ...node,
        company: networkCompanyByID.get(node.id) || node.company,
      });
    });
    return nodes;
  }, [networkCompanyByID, networkGraphNodes, networkRootCompany, networkRootID]);

  const networkChildNodes = useMemo(() => {
    return networkNodes
      .filter((node) => node.depth > 0)
      .sort((left, right) => {
        const depthDelta = left.depth - right.depth;
        if (depthDelta !== 0) {
          return depthDelta;
        }
        const leftTitle = String(left.company?.title || left.company?.additional_name || left.id);
        const rightTitle = String(right.company?.title || right.company?.additional_name || right.id);
        return leftTitle.localeCompare(rightTitle, 'ru');
      });
  }, [networkNodes]);

  const networkInfrastructureQueries = useQueries({
    queries: hasNetwork
      ? networkCompanyIDs.map((networkCompanyID) => ({
        queryKey: ['company', networkCompanyID, 'infra', 'network'],
        queryFn: () => companiesApi.getInfrastructure(networkCompanyID),
        staleTime: 30_000,
      }))
      : [],
  });

  const loadingNetworkInfrastructure = networkInfrastructureQueries.some((query) => query.isLoading);
  const networkServersByCompanyID = useMemo(() => {
    const result = new Map<string, ServerEntity[]>();
    networkCompanyIDs.forEach((networkCompanyID, index) => {
      const queryData = networkInfrastructureQueries[index]?.data?.data || [];
      const servers = queryData
        .filter((item) => item.entity_type === 'Server')
        .map((item) => item.data as ServerEntity);
      result.set(networkCompanyID, servers);
    });
    return result;
  }, [networkCompanyIDs, networkInfrastructureQueries]);

  const selectedNetworkContractCompany = useMemo(
    () => networkCompanyByID.get(selectedNetworkContractCompanyID),
    [networkCompanyByID, selectedNetworkContractCompanyID],
  );
  const selectedNetworkContractID = String(selectedNetworkContractCompany?.contract_id || '').trim();
  const { data: selectedNetworkContractRes, isLoading: loadingSelectedNetworkContract } = useQuery({
    queryKey: ['contract', selectedNetworkContractID, 'network-card-view'],
    queryFn: () => contractsApi.getContract(selectedNetworkContractID),
    enabled: isNetworkContractModalOpen && Boolean(selectedNetworkContractID),
    staleTime: 30_000,
  });

  const { data: spreadCompanySearchRes } = useQuery({
    queryKey: ['companies-search-company-page-spread', spreadCompanySearch],
    queryFn: () => companiesApi.searchCompanies(spreadCompanySearch, 20, 0),
    enabled: isContractEditOpen,
    staleTime: 30_000,
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
    mutationFn: async (values: { title: string; address: string; parent_id?: string }) => {
      return companiesApi.updateCompany(id!, {
        title: values.title.trim(),
        address: values.address.trim(),
        parent_id: values.parent_id || null,
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

  const createContractMutation = useMutation({
    mutationFn: async (values: { contract_type: string; contract_state: 'active' | 'inactive' }) => {
      const currentCompanyID = resolveCompanyID(companyRes?.data || {}) || companyRes?.data?.id || '';
      if (!currentCompanyID) {
        throw new Error('Отсутствует компания');
      }

      return contractsApi.createContract({
        state: values.contract_state,
        services: [values.contract_type],
        recipients: [currentCompanyID],
        service_level: 0,
        company_ids: [currentCompanyID],
      });
    },
    onSuccess: () => {
      message.success('Контракт создан');
      setIsContractEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['company', id] });
      queryClient.invalidateQueries({ queryKey: ['company'] });
    },
    onError: () => {
      message.error('Не удалось создать контракт');
    },
  });

  const leaveContractMutation = useMutation({
    mutationFn: async () => {
      const currentCompanyID = resolveCompanyID(companyRes?.data || {}) || companyRes?.data?.id || '';
      if (!contractID || !currentCompanyID) {
        throw new Error('Недостаточно данных для операции');
      }
      const current = await contractsApi.getContract(contractID);
      const companies = current.data.companies || [];
      const remainingCompanyIDs = companies.map((item) => item.id).filter((item) => item !== currentCompanyID);
      const services = normalizeServices(current.data.services);

      await contractsApi.updateContract(contractID, {
        company_ids: remainingCompanyIDs,
        recipients: remainingCompanyIDs,
      });

      await contractsApi.createContract({
        state: current.data.state || 'active',
        state_start_time: current.data.state_start_time,
        services,
        recipients: [currentCompanyID],
        service_level: current.data.service_level || 0,
        company_ids: [currentCompanyID],
      });
    },
    onSuccess: () => {
      message.success('Для компании создан новый отдельный контракт');
      setSpreadCompanyID(undefined);
      setIsContractEditOpen(false);
      queryClient.invalidateQueries({ queryKey: ['company', id] });
      queryClient.invalidateQueries({ queryKey: ['contract', contractID, 'company-modal'] });
    },
    onError: () => {
      message.error('Не удалось вывести компанию в отдельный контракт');
    },
  });

  const spreadContractMutation = useMutation({
    mutationFn: async () => {
      if (!contractID || !spreadCompanyID) {
        throw new Error('Выберите компанию для распространения');
      }
      const current = await contractsApi.getContract(contractID);
      const companyIDs = Array.from(new Set([...(current.data.companies || []).map((item) => item.id), spreadCompanyID]));
      return contractsApi.updateContract(contractID, {
        company_ids: companyIDs,
        recipients: companyIDs,
      });
    },
    onSuccess: () => {
      message.success('Компания добавлена в контракт');
      setSpreadCompanyID(undefined);
      queryClient.invalidateQueries({ queryKey: ['contract', contractID, 'company-modal'] });
      queryClient.invalidateQueries({ queryKey: ['company'] });
    },
    onError: () => {
      message.error('Не удалось распространить контракт');
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

  const hasChildCompanies = companyChildrenIDs.length > 0;
  const ticketCompanyIDs = ticketScope === 'with_children' && hasChildCompanies
    ? [companyID, ...companyChildrenIDs]
    : [companyID];
  const parentTitle = company?.parent_title;
  const contractCompanies = contractRes?.data?.companies || [];

  const parentOptions = useMemo(() => {
    const options = (companySearchRes?.data || [])
      .map((item) => ({
        value: String(item.id || ''),
        title: String(item.title || item.additional_name || item.id || ''),
        parentTitle: item.parent_title ? String(item.parent_title) : undefined,
      }))
      .filter((item) => item.value && item.title && item.value !== companyID);
    if (company?.parent_id && parentTitle && !options.some((item) => item.value === company.parent_id)) {
      options.unshift({
        value: company.parent_id,
        title: parentTitle,
        parentTitle: undefined,
      });
    }
    return options;
  }, [company?.parent_id, companyID, companySearchRes?.data, parentTitle]);

  const spreadCompanyOptions = useMemo(() => {
    const existing = new Set(contractCompanies.map((item) => item.id));
    return (spreadCompanySearchRes?.data || [])
      .map((item) => ({
        value: String(item.id || ''),
        title: String(item.title || item.additional_name || item.id || ''),
        parentTitle: item.parent_title ? String(item.parent_title) : undefined,
      }))
      .filter((item) => item.value && item.title && !existing.has(item.value));
  }, [contractCompanies, spreadCompanySearchRes?.data]);

  if (loadingCompany) {
    return (
      <div style={{ padding: 50, textAlign: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }

  if (!company) return <Empty description="Компания не найдена" />;

  const openCompanyEdit = () => {
    companyForm.setFieldsValue({
      title: company.title || '',
      address: company.address || '',
      parent_id: company.parent_id || undefined,
    });
    setIsCompanyEditOpen(true);
  };

  const openContractEdit = () => {
    const resolvedType = normalizeServices(contractRes?.data?.services)[0] || contractType || contractTypeOptions[0];
    const resolvedState = (contractRes?.data?.state) === 'active' ? 'active' : 'inactive';
    contractForm.setFieldsValue({
      contract_type: resolvedType,
      contract_state: resolvedState,
    });
    if (contractID && (contractRes?.data?.companies || []).length > 1) {
      message.warning('Контракт общий. Изменения типа и статуса применятся ко всем компаниям-участникам.');
    }
    setIsContractEditOpen(true);
  };

  const handleContractFinish = (values: { contract_type: string; contract_state: 'active' | 'inactive' }) => {
    if (contractID) {
      updateContractMutation.mutate(values);
      return;
    }
    createContractMutation.mutate(values);
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

  const renderEquipmentTab = (
    <div style={{ marginTop: 10 }}>
      {loadingInfra ? <Spin /> : (
        <>
          {renderSection(
            'Серверы',
            groupedInfra.servers.length,
            <div className="company-equipment-grid">
              {groupedInfra.servers.map((srv) => (
                <div key={srv.uuid} className="company-equipment-grid__item">
                  <ServerCard data={srv} />
                </div>
              ))}
            </div>,
          )}

          {renderSection(
            'Фискальные регистраторы',
            groupedInfra.fiscals.length,
            <div className="company-equipment-grid">
              {groupedInfra.fiscals.map((fr) => (
                <div key={fr.uuid} className="company-equipment-grid__item">
                  <FiscalCard data={fr} />
                </div>
              ))}
            </div>,
          )}

          {renderSection(
            'Рабочие станции',
            groupedInfra.workstations.length,
            <div className="company-equipment-grid company-equipment-grid--compact">
              {groupedInfra.workstations.map((ws) => (
                <div key={ws.uuid} className="company-equipment-grid__item">
                  <WorkstationCard data={ws} />
                </div>
              ))}
            </div>,
          )}

          {(infraRes?.data || []).length === 0 && <Empty description="Оборудование не найдено" />}
        </>
      )}
    </div>
  );

  const resolveContractBadge = (item?: CompanyModel) => {
    if (!item) {
      return { color: 'default' as const, label: 'Контракт не задан' };
    }
    const hasContract = Boolean(String(item.contract_id || '').trim());
    if (!hasContract) {
      return { color: 'default' as const, label: 'Контракт не задан' };
    }
    if (item.active_contract) {
      return { color: 'success' as const, label: 'Активный контракт' };
    }
    return { color: 'default' as const, label: 'Неактивный контракт' };
  };

  const openNetworkContractModal = (networkCompanyID: string) => {
    const row = networkCompanyByID.get(networkCompanyID);
    const rowContractID = String(row?.contract_id || '').trim();
    if (!rowContractID) {
      message.info('У компании отсутствует контракт');
      return;
    }
    setSelectedNetworkContractCompanyID(networkCompanyID);
    setIsNetworkContractModalOpen(true);
  };

  const renderNetworkCompanyCard = (node: NetworkCompanyNode, isRoot = false) => {
    const cardCompanyID = node.id;
    const item = node.company || {};
    const cardTitle = String(item.title || item.additional_name || cardCompanyID || 'Компания');
    const cardAdditionalName = String(item.additional_name || '').trim();
    const cardAddress = String(item.address || '').trim();
    const cardServers = networkServersByCompanyID.get(cardCompanyID) || [];
    const isCurrent = cardCompanyID === companyID;
    const contractBadge = resolveContractBadge(item);

    return (
      <Card
        key={cardCompanyID || cardTitle}
        size="small"
        className="company-network-card"
        style={{
          borderColor: isCurrent ? token.colorPrimary : token.colorBorder,
          boxShadow: isCurrent ? `0 0 0 1px ${token.colorPrimary}` : undefined,
        }}
      >
          <Space direction="vertical" size={8} style={{ width: '100%' }}>
            <Space size={8} wrap>
              {isRoot && <Tag color="geekblue" style={{ marginRight: 0 }}>Родитель</Tag>}
              <Tag
              color={contractBadge.color}
              style={{ marginRight: 0, cursor: 'pointer' }}
              onClick={() => openNetworkContractModal(cardCompanyID)}
            >
              {contractBadge.label}
            </Tag>
          </Space>
          <div>
            <Link to={`/companies/${cardCompanyID}`} style={{ fontWeight: 600, display: 'block' }}>
              {cardTitle}
            </Link>
            <Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
              Юр. название: {cardAdditionalName || '-'}
            </Text>
            <Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
              Адрес: {cardAddress || '-'}
            </Text>
          </div>
          <div>
            {cardServers.length > 0 ? (
              <Space direction="vertical" size={6} style={{ width: '100%' }}>
                {cardServers.map((server) => {
                  const serverIP = String(server.ip || '').trim();
                  return (
                    <div key={server.uuid} style={{ border: `1px solid ${token.colorBorderSecondary}`, borderRadius: 8, padding: 8 }}>
                      <Link to={`/servers/${server.uuid}`} style={{ fontWeight: 600 }}>
                        {resolveServerTitle(server)}
                      </Link>
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, marginTop: 4 }}>
                        <Text type="secondary" style={{ fontSize: 12, wordBreak: 'break-all' }}>
                          {serverIP || '-'}
                        </Text>
                        <Button
                          type="text"
                          size="small"
                          icon={<CopyOutlined />}
                          disabled={!serverIP}
                          onClick={async () => {
                            if (!serverIP) {
                              return;
                            }
                            try {
                              await navigator.clipboard.writeText(serverIP);
                              message.success('IP скопирован');
                            } catch {
                              message.error('Не удалось скопировать IP');
                            }
                          }}
                        >
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </Space>
            ) : null }
          </div>
        </Space>
      </Card>
    );
  };

  const rootNode = networkNodes.find((node) => node.depth === 0 && node.id === networkRootID);

  const renderNetworkTab = (
    <div style={{ marginTop: 10 }}>
      {(loadingNetworkRootCompany || loadingNetworkGraph || loadingNetworkProfiles || loadingNetworkInfrastructure) ? (
        <Spin />
      ) : !rootNode ? (
        <Empty description="Структура сети не найдена" />
      ) : (
        <div className="company-network-tree">
          {renderNetworkCompanyCard(rootNode, true)}
          <div className="company-network-children-grid">
            {networkChildNodes.length > 0 ? networkChildNodes.map((node) => renderNetworkCompanyCard(node)) : (
              <Card size="small">
                <Text type="secondary">Дочерние компании отсутствуют</Text>
              </Card>
            )}
          </div>
        </div>
      )}
    </div>
  );

  const items = [
    {
      key: 'equipment',
      label: 'Оборудование',
      children: renderEquipmentTab,
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
          <TicketTable
            companyIds={ticketCompanyIDs}
            limit={10}
            showCompanyColumn={ticketScope === 'with_children'}
          />
        </div>
      ),
    },
    {
      key: 'materials',
      label: 'Материалы',
      children: (
        <div style={{ marginTop: 10 }}>
          <MaterialsPanel entityType="Company" entityID={companyID} title="Материалы компании" />
        </div>
      ),
    },
  ];

  const renderNetworkBlock = hasNetwork ? (
    <Card
      size="small"
      className="glass-panel company-network-aside-card"
      title="Инфраструктура сети"
    >
      {renderNetworkTab}
    </Card>
  ) : null;

  const renderCompanyTabs = (
    <Tabs defaultActiveKey="equipment" items={items} />
  );

  const companySummaryCard = (
    <Card className="glass-panel company-summary-card" style={{ marginBottom: 12 }} size="small" bodyStyle={{ padding: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <Button type="link" onClick={goBack} style={{ padding: 0, color: token.colorTextSecondary }}>
          <ArrowLeftOutlined style={{ marginRight: 8 }} /> Назад
        </Button>
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
          ) : canEditContract ? (
            <Button type="link" size="small" style={{ padding: 0 }} onClick={openContractEdit}>
              Создать контракт
            </Button>
          ) : '-'}
        </Descriptions.Item>
        <Descriptions.Item label="Точка обслуживания B24" span={2}>
          {bitrixMapping?.bitrix_service_point_id ? (
            <Space size={8} wrap>
              <Text>{bitrixMapping.bitrix_service_point_name || `ID ${bitrixMapping.bitrix_service_point_id}`}</Text>
              {bitrixMapping.bitrix_service_point_code && <Tag>{bitrixMapping.bitrix_service_point_code}</Tag>}
              {typeof bitrixMapping.bitrix_service_point_enabled === 'boolean' && (
                <Tag color={bitrixMapping.bitrix_service_point_enabled ? 'success' : 'default'}>
                  {bitrixMapping.bitrix_service_point_enabled ? 'контракт активен' : 'контракт не активен'}
                </Tag>
              )}
            </Space>
          ) : (
            <Text type="secondary">Не сопоставлена</Text>
          )}
        </Descriptions.Item>
      </Descriptions>
    </Card>
  );

  return (
    <div className="company-details-page">
      {hasNetwork ? (
        <div className="company-network-layout company-details-content">
          <div className="company-network-main">
            {companySummaryCard}
            {renderCompanyTabs}
          </div>
          <aside className="company-network-aside">
            {renderNetworkBlock}
          </aside>
        </div>
      ) : (
        <>
          {companySummaryCard}
          <div className="company-details-content company-details-content--single">
          {renderCompanyTabs}
          </div>
        </>
      )}

      <Modal
        title="Параметры контракта компании"
        open={isNetworkContractModalOpen}
        onCancel={() => setIsNetworkContractModalOpen(false)}
        footer={<Button onClick={() => setIsNetworkContractModalOpen(false)}>Закрыть</Button>}
      >
        {loadingSelectedNetworkContract ? (
          <Spin />
        ) : (
          <Space direction="vertical" size={10} style={{ width: '100%' }}>
            <div>
              <Text type="secondary">Компания</Text>
              <Text strong style={{ display: 'block' }}>
                {selectedNetworkContractCompany?.title || selectedNetworkContractCompany?.additional_name || selectedNetworkContractCompanyID || '-'}
              </Text>
            </div>
            <div>
              <Text type="secondary">Статус контракта</Text>
              <Text strong style={{ display: 'block' }}>
                {selectedNetworkContractRes?.data?.state === 'active'
                  ? 'Активен'
                  : selectedNetworkContractRes?.data?.state
                    ? String(selectedNetworkContractRes.data.state)
                    : (selectedNetworkContractCompany?.active_contract ? 'Активен' : 'Неактивен')}
              </Text>
            </div>
            <div>
              <Text type="secondary">Тип контракта</Text>
              <Text strong style={{ display: 'block' }}>
                {normalizeServices(selectedNetworkContractRes?.data?.services || [])[0]
                  || selectedNetworkContractCompany?.contract_type
                  || '-'}
              </Text>
            </div>
            <div>
              <Text type="secondary">В этом же контракте</Text>
              {(selectedNetworkContractRes?.data?.companies || []).length > 0 ? (
                <Space direction="vertical" size={4} style={{ display: 'flex', marginTop: 4 }}>
                  {(selectedNetworkContractRes?.data?.companies || []).map((recipient) => (
                    <Link key={recipient.id} to={`/companies/${recipient.id}`}>
                      {recipient.title || recipient.id}
                    </Link>
                  ))}
                </Space>
              ) : (
                <Text strong style={{ display: 'block' }}>-</Text>
              )}
            </div>
          </Space>
        )}
      </Modal>

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
          <Form.Item label="Родительская компания" name="parent_id">
            <CompanySearchSelect
              allowClear
              options={parentOptions}
              placeholder="Выберите родительскую компанию"
              onSearch={setCompanySearch}
              onChange={(value) => companyForm.setFieldValue('parent_id', value)}
            />
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
        title={contractID ? 'Параметры контракта' : 'Создание контракта'}
        open={isContractEditOpen}
        onCancel={() => setIsContractEditOpen(false)}
        onOk={() => contractForm.submit()}
        confirmLoading={updateContractMutation.isPending || createContractMutation.isPending}
        okText="Сохранить"
        cancelText="Отмена"
        width={560}
      >
        <Form form={contractForm} layout="vertical" onFinish={handleContractFinish}>
          {contractID && contractCompanies.length > 0 && (
            <Form.Item label="Участники контракта">
              <Space direction="vertical" size={4} style={{ width: '100%' }}>
                {contractCompanies.map((item) => (
                  <Link key={item.id} to={`/companies/${item.id}`} target="_blank" rel="noreferrer">
                    {item.title}
                  </Link>
                ))}
              </Space>
            </Form.Item>
          )}
          {contractCompanies.length > 1 && (
            <Form.Item>
              <Text type="warning">
                Контракт общий: изменение статуса и типа применится ко всем участникам.
              </Text>
            </Form.Item>
          )}
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
          {contractID && (
            <Form.Item label="Распространить контракт на компанию">
              <Space style={{ width: '100%' }} align="start">
                <div style={{ flex: 1 }}>
                  <CompanySearchSelect
                    allowClear
                    value={spreadCompanyID}
                    options={spreadCompanyOptions}
                    placeholder="Выберите компанию"
                    onSearch={setSpreadCompanySearch}
                    onChange={setSpreadCompanyID}
                  />
                </div>
                <Button
                  onClick={() => spreadContractMutation.mutate()}
                  loading={spreadContractMutation.isPending}
                  disabled={!spreadCompanyID}
                >
                  Добавить
                </Button>
              </Space>
            </Form.Item>
          )}
          {contractID && (
            <Form.Item>
              <Button
                danger
                onClick={() => leaveContractMutation.mutate()}
                loading={leaveContractMutation.isPending}
              >
                Выйти из контракта и создать новый
              </Button>
            </Form.Item>
          )}
        </Form>
      </Modal>
    </div>
  );
};

export default CompanyPage;
