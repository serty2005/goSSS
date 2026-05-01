import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Empty, Form, Input, List, Modal, Popconfirm, Select, Space, Spin, Typography, message } from 'antd';
import Editor from '@monaco-editor/react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { materialsApi } from '@/api/materials';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import { MaterialDTO, MaterialEntityRefDTO, MaterialPayload } from '@/types/api';
import { useUiStore } from '@/store/uiStore';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

const { Text, Title } = Typography;

type MaterialEntityType = 'Company' | 'Server' | 'Workstation' | 'FiscalRegister';
type SelectOption = { value: string; label: string };

interface MaterialsPanelProps {
  entityType: MaterialEntityType;
  entityID: string;
  title?: string;
}

const toRefKey = (ref: MaterialEntityRefDTO) => `${ref.entity_type}:${ref.entity_id}`;

const dedupeRefs = (refs: MaterialEntityRefDTO[]) => {
  const map = new Map<string, MaterialEntityRefDTO>();
  refs.forEach((ref) => {
    if (!ref.entity_id) return;
    map.set(toRefKey(ref), ref);
  });
  return Array.from(map.values());
};

const mergeOptions = (...groups: SelectOption[][]): SelectOption[] => {
  const map = new Map<string, SelectOption>();
  groups.forEach((group) => {
    group.forEach((item) => {
      if (!item.value) return;
      map.set(item.value, item);
    });
  });
  return Array.from(map.values());
};

const MonacoMarkdownEditor: React.FC<{ value?: string; onChange?: (value: string) => void; themeMode: 'light' | 'dark' }> = ({ value, onChange, themeMode }) => (
  <Editor
    height="340px"
    defaultLanguage="markdown"
    theme={themeMode === 'dark' ? 'vs-dark' : 'vs'}
    value={value || ''}
    onChange={(nextValue) => onChange?.(nextValue || '')}
    options={{
      minimap: { enabled: false },
      fontSize: 13,
      wordWrap: 'on',
      automaticLayout: true,
    }}
  />
);

const MaterialsPanel: React.FC<MaterialsPanelProps> = ({ entityType, entityID, title }) => {
  const themeMode = useUiStore((state) => state.themeMode);
  const queryClient = useQueryClient();
  const [selectedID, setSelectedID] = useState<string>('');
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingItem, setEditingItem] = useState<MaterialDTO | null>(null);
  const [companySearch, setCompanySearch] = useState('');
  const [serverSearch, setServerSearch] = useState('');
  const [workstationSearch, setWorkstationSearch] = useState('');
  const [fiscalSearch, setFiscalSearch] = useState('');
  const [companyAppliedSearch, setCompanyAppliedSearch] = useState('');
  const [serverAppliedSearch, setServerAppliedSearch] = useState('');
  const [workstationAppliedSearch, setWorkstationAppliedSearch] = useState('');
  const [fiscalAppliedSearch, setFiscalAppliedSearch] = useState('');
  const debouncedCompanySearch = useDebouncedValue(companySearch, SELECT_SEARCH_DEBOUNCE_MS);
  const debouncedServerSearch = useDebouncedValue(serverSearch, SELECT_SEARCH_DEBOUNCE_MS);
  const debouncedWorkstationSearch = useDebouncedValue(workstationSearch, SELECT_SEARCH_DEBOUNCE_MS);
  const debouncedFiscalSearch = useDebouncedValue(fiscalSearch, SELECT_SEARCH_DEBOUNCE_MS);
  const [selectedCompanyOptions, setSelectedCompanyOptions] = useState<SelectOption[]>([]);
  const [selectedServerOptions, setSelectedServerOptions] = useState<SelectOption[]>([]);
  const [selectedWorkstationOptions, setSelectedWorkstationOptions] = useState<SelectOption[]>([]);
  const [selectedFiscalOptions, setSelectedFiscalOptions] = useState<SelectOption[]>([]);
  const [fiscalOwnerNames, setFiscalOwnerNames] = useState<Record<string, string>>({});
  const [form] = Form.useForm<{
    subject: string;
    content: string;
    company_ids: string[];
    server_ids: string[];
    workstation_ids: string[];
    fiscal_ids: string[];
  }>();

  const { data: materialsRes, isLoading } = useQuery({
    queryKey: ['materials', entityType, entityID],
    queryFn: () => materialsApi.listByEntity(entityType, entityID, 100, 0),
    enabled: Boolean(entityID),
    staleTime: 30_000,
  });

  const materials = useMemo(() => materialsRes?.data || [], [materialsRes?.data]);

  useEffect(() => {
    if (!materials.length) {
      setSelectedID('');
      return;
    }
    if (!selectedID || !materials.some((item) => item.id === selectedID)) {
      setSelectedID(materials[0].id);
    }
  }, [materials, selectedID]);

  const selectedMaterial = useMemo(
    () => materials.find((item) => item.id === selectedID) || null,
    [materials, selectedID],
  );

  const { data: companiesRes } = useQuery({
    queryKey: ['materials-company-search', companyAppliedSearch],
    queryFn: () => companiesApi.searchCompanies(companyAppliedSearch, 20, 0),
    staleTime: 20_000,
  });
  const { data: serversRes } = useQuery({
    queryKey: ['materials-server-search', serverAppliedSearch],
    queryFn: () => equipmentApi.listServers(serverAppliedSearch, 20, 0),
    staleTime: 20_000,
  });
  const { data: workstationsRes } = useQuery({
    queryKey: ['materials-workstation-search', workstationAppliedSearch],
    queryFn: () => equipmentApi.listWorkstations(workstationAppliedSearch, 20, 0),
    staleTime: 20_000,
  });
  const { data: fiscalsRes } = useQuery({
    queryKey: ['materials-fiscal-search', fiscalAppliedSearch],
    queryFn: () => equipmentApi.listFiscals(fiscalAppliedSearch, 20, 0),
    staleTime: 20_000,
  });

  useEffect(() => {
    setCompanyAppliedSearch(debouncedCompanySearch);
  }, [debouncedCompanySearch]);

  useEffect(() => {
    setServerAppliedSearch(debouncedServerSearch);
  }, [debouncedServerSearch]);

  useEffect(() => {
    setWorkstationAppliedSearch(debouncedWorkstationSearch);
  }, [debouncedWorkstationSearch]);

  useEffect(() => {
    setFiscalAppliedSearch(debouncedFiscalSearch);
  }, [debouncedFiscalSearch]);

  useEffect(() => {
    const ownerIDs = Array.from(
      new Set(
        (fiscalsRes?.data || [])
          .map((item) => String((item as Record<string, unknown>).owner_id || ''))
          .filter(Boolean),
      ),
    );
    if (ownerIDs.length === 0) {
      return;
    }

    let cancelled = false;
    void (async () => {
      const entries = await Promise.all(ownerIDs.map(async (ownerID) => {
        try {
          const company = await companiesApi.getCompany(ownerID);
          const companyData = company.data || {};
          const ownerTitle = String(companyData.title || companyData.additional_name || ownerID);
          return [ownerID, ownerTitle] as const;
        } catch {
          return [ownerID, ownerID] as const;
        }
      }));

      if (cancelled) return;

      setFiscalOwnerNames((prev) => {
        const next = { ...prev };
        entries.forEach(([ownerID, ownerTitle]) => {
          next[ownerID] = ownerTitle;
        });
        return next;
      });
    })();

    return () => {
      cancelled = true;
    };
  }, [fiscalsRes?.data]);

  const searchCompanyOptions = useMemo(
    () => (companiesRes?.data || [])
      .map((item) => ({
        value: String(item.id || ''),
        label: String(item.title || item.additional_name || item.id || ''),
      }))
      .filter((item) => item.value && item.label),
    [companiesRes?.data],
  );

  const searchServerOptions = useMemo(
    () => (serversRes?.data || [])
      .map((item) => ({
        value: String(item.id || ''),
        label: String(item.device_name || item.server_name || item.id || ''),
      }))
      .filter((item) => item.value && item.label),
    [serversRes?.data],
  );

  const searchWorkstationOptions = useMemo(
    () => (workstationsRes?.data || [])
      .map((item) => ({
        value: String(item.id || ''),
        label: String(item.device_name || item.id || ''),
      }))
      .filter((item) => item.value && item.label),
    [workstationsRes?.data],
  );

  const searchFiscalOptions = useMemo(
    () => (fiscalsRes?.data || [])
      .map((item) => {
        const row = item as Record<string, unknown>;
        const fiscalID = String(row.id || '');
        const ownerID = String(row.owner_id || '');
        const ownerTitle = fiscalOwnerNames[ownerID] || ownerID || 'Без владельца';
        const serial = String(row.fr_serial_number || row.serial_number || row.rn_kkt || fiscalID || '-');
        return {
          value: fiscalID,
          label: `${ownerTitle} • ${serial}`,
        };
      })
      .filter((item) => item.value && item.label),
    [fiscalOwnerNames, fiscalsRes?.data],
  );

  const companyOptions = useMemo(
    () => mergeOptions(selectedCompanyOptions, searchCompanyOptions),
    [selectedCompanyOptions, searchCompanyOptions],
  );
  const serverOptions = useMemo(
    () => mergeOptions(selectedServerOptions, searchServerOptions),
    [searchServerOptions, selectedServerOptions],
  );
  const workstationOptions = useMemo(
    () => mergeOptions(selectedWorkstationOptions, searchWorkstationOptions),
    [searchWorkstationOptions, selectedWorkstationOptions],
  );
  const fiscalOptions = useMemo(
    () => mergeOptions(selectedFiscalOptions, searchFiscalOptions),
    [searchFiscalOptions, selectedFiscalOptions],
  );

  const preloadReferenceLabels = async (refs: MaterialEntityRefDTO[]) => {
    const uniqueByType = {
      companies: Array.from(new Set(refs.filter((ref) => ref.entity_type === 'Company').map((ref) => ref.entity_id))),
      servers: Array.from(new Set(refs.filter((ref) => ref.entity_type === 'Server').map((ref) => ref.entity_id))),
      workstations: Array.from(new Set(refs.filter((ref) => ref.entity_type === 'Workstation').map((ref) => ref.entity_id))),
      fiscals: Array.from(new Set(refs.filter((ref) => ref.entity_type === 'FiscalRegister').map((ref) => ref.entity_id))),
    };

    const [companies, servers, workstations] = await Promise.all([
      Promise.all(uniqueByType.companies.map(async (companyID) => {
        try {
          const company = await companiesApi.getCompany(companyID);
          const companyData = company.data || {};
          return {
            value: companyID,
            label: String(companyData.title || companyData.additional_name || companyID),
          };
        } catch {
          return { value: companyID, label: companyID };
        }
      })),
      Promise.all(uniqueByType.servers.map(async (serverID) => {
        try {
          const server = await equipmentApi.getServer(serverID);
          const serverData = server.data || {};
          return {
            value: serverID,
            label: String(serverData.device_name || serverData.server_name || serverID),
          };
        } catch {
          return { value: serverID, label: serverID };
        }
      })),
      Promise.all(uniqueByType.workstations.map(async (workstationID) => {
        try {
          const workstation = await equipmentApi.getWorkstation(workstationID);
          const workstationData = workstation.data || {};
          return {
            value: workstationID,
            label: String(workstationData.device_name || workstationID),
          };
        } catch {
          return { value: workstationID, label: workstationID };
        }
      })),
    ]);

    const fiscalOwnerCache: Record<string, string> = { ...fiscalOwnerNames };
    const fiscals = await Promise.all(uniqueByType.fiscals.map(async (fiscalID) => {
      try {
        const fiscal = await equipmentApi.getFiscal(fiscalID);
        const fiscalData = fiscal.data || {};
        const ownerID = String(fiscalData.owner_id || '');
        if (ownerID && !fiscalOwnerCache[ownerID]) {
          try {
            const owner = await companiesApi.getCompany(ownerID);
            const ownerData = owner.data || {};
            fiscalOwnerCache[ownerID] = String(ownerData.title || ownerData.additional_name || ownerID);
          } catch {
            fiscalOwnerCache[ownerID] = ownerID;
          }
        }

        const ownerTitle = ownerID ? (fiscalOwnerCache[ownerID] || ownerID) : 'Без владельца';
        const serial = String(fiscalData.fr_serial_number || fiscalData.rn_kkt || fiscalID || '-');
        return {
          value: fiscalID,
          label: `${ownerTitle} • ${serial}`,
        };
      } catch {
        return { value: fiscalID, label: fiscalID };
      }
    }));

    setFiscalOwnerNames((prev) => ({ ...prev, ...fiscalOwnerCache }));
    setSelectedCompanyOptions((prev) => mergeOptions(prev, companies));
    setSelectedServerOptions((prev) => mergeOptions(prev, servers));
    setSelectedWorkstationOptions((prev) => mergeOptions(prev, workstations));
    setSelectedFiscalOptions((prev) => mergeOptions(prev, fiscals));
  };

  const saveMutation = useMutation({
    mutationFn: async (values: {
      subject: string;
      content: string;
      company_ids: string[];
      server_ids: string[];
      workstation_ids: string[];
      fiscal_ids: string[];
    }) => {
      const refs = dedupeRefs([
        ...values.company_ids.map((id) => ({ entity_type: 'Company' as const, entity_id: id })),
        ...values.server_ids.map((id) => ({ entity_type: 'Server' as const, entity_id: id })),
        ...values.workstation_ids.map((id) => ({ entity_type: 'Workstation' as const, entity_id: id })),
        ...values.fiscal_ids.map((id) => ({ entity_type: 'FiscalRegister' as const, entity_id: id })),
        { entity_type: entityType, entity_id: entityID },
      ]);

      const payload: MaterialPayload = {
        subject: values.subject.trim(),
        content: values.content,
        entity_refs: refs,
      };

      if (editingItem) {
        return materialsApi.update(editingItem.id, payload);
      }
      return materialsApi.create(payload);
    },
    onSuccess: () => {
      message.success(editingItem ? 'Материал обновлён' : 'Материал создан');
      setIsModalOpen(false);
      setEditingItem(null);
      form.resetFields();
      queryClient.invalidateQueries({ queryKey: ['materials', entityType, entityID] });
    },
    onError: () => {
      message.error('Не удалось сохранить материал');
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => materialsApi.delete(id),
    onSuccess: () => {
      message.success('Материал удалён');
      queryClient.invalidateQueries({ queryKey: ['materials', entityType, entityID] });
    },
    onError: () => {
      message.error('Не удалось удалить материал');
    },
  });

  const openCreate = () => {
    setEditingItem(null);
    form.setFieldsValue({
      subject: '',
      content: '',
      company_ids: entityType === 'Company' ? [entityID] : [],
      server_ids: entityType === 'Server' ? [entityID] : [],
      workstation_ids: entityType === 'Workstation' ? [entityID] : [],
      fiscal_ids: entityType === 'FiscalRegister' ? [entityID] : [],
    });
    void preloadReferenceLabels([{ entity_type: entityType, entity_id: entityID }]);
    setIsModalOpen(true);
  };

  const openEdit = (item: MaterialDTO) => {
    setEditingItem(item);
    form.setFieldsValue({
      subject: item.subject,
      content: item.content,
      company_ids: item.entity_refs.filter((ref) => ref.entity_type === 'Company').map((ref) => ref.entity_id),
      server_ids: item.entity_refs.filter((ref) => ref.entity_type === 'Server').map((ref) => ref.entity_id),
      workstation_ids: item.entity_refs.filter((ref) => ref.entity_type === 'Workstation').map((ref) => ref.entity_id),
      fiscal_ids: item.entity_refs.filter((ref) => ref.entity_type === 'FiscalRegister').map((ref) => ref.entity_id),
    });
    void preloadReferenceLabels(item.entity_refs);
    setIsModalOpen(true);
  };

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '320px 1fr', gap: 12 }}>
      <Card
        size="small"
        title={title || 'Материалы'}
        extra={<Button size="small" type="primary" onClick={openCreate}>Новый</Button>}
      >
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 16 }}><Spin /></div>
        ) : materials.length === 0 ? (
          <Empty description="Материалов пока нет" />
        ) : (
          <List
            size="small"
            dataSource={materials}
            renderItem={(item) => (
              <List.Item
                style={{
                  cursor: 'pointer',
                  background: selectedID === item.id ? 'rgba(24, 144, 255, 0.08)' : undefined,
                  borderRadius: 8,
                  paddingInline: 8,
                }}
                onClick={() => setSelectedID(item.id)}
              >
                <Space direction="vertical" size={0} style={{ width: '100%' }}>
                  <Text strong>{item.subject}</Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {item.author_name || 'Сотрудник'} • {new Date(item.updated_at).toLocaleString()}
                  </Text>
                </Space>
              </List.Item>
            )}
          />
        )}
      </Card>

      <Card
        size="small"
        title={selectedMaterial?.subject || 'Просмотр материала'}
        extra={selectedMaterial ? (
          <Space>
            <Button size="small" onClick={() => openEdit(selectedMaterial)}>Редактировать</Button>
            <Popconfirm
              title="Удалить материал?"
              okText="Удалить"
              cancelText="Отмена"
              onConfirm={() => deleteMutation.mutate(selectedMaterial.id)}
            >
              <Button size="small" danger loading={deleteMutation.isPending}>Удалить</Button>
            </Popconfirm>
          </Space>
        ) : null}
      >
        {!selectedMaterial ? (
          <Empty description="Выберите материал из списка" />
        ) : (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Text type="secondary">
              Автор: {selectedMaterial.author_name || 'Сотрудник'} • Создан: {new Date(selectedMaterial.created_at).toLocaleString()} • Обновлён: {new Date(selectedMaterial.updated_at).toLocaleString()}
            </Text>
            <div className="markdown-body" style={{ whiteSpace: 'normal' }}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {selectedMaterial.content}
              </ReactMarkdown>
            </div>
          </Space>
        )}
      </Card>

      <Modal
        open={isModalOpen}
        onCancel={() => {
          setIsModalOpen(false);
          setEditingItem(null);
        }}
        title={editingItem ? 'Редактирование материала' : 'Новый материал'}
        width={980}
        okText="Сохранить"
        cancelText="Отмена"
        onOk={() => form.submit()}
        confirmLoading={saveMutation.isPending}
        destroyOnHidden
      >
        <Form
          layout="vertical"
          form={form}
          onFinish={(values) => saveMutation.mutate(values)}
          initialValues={{
            subject: '',
            content: '',
            company_ids: [],
            server_ids: [],
            workstation_ids: [],
            fiscal_ids: [],
          }}
        >
          <Form.Item
            name="subject"
            label="Тема"
            rules={[{ required: true, message: 'Введите тему материала' }]}
          >
            <Input placeholder="Краткая тема" />
          </Form.Item>

          <Form.Item
            name="content"
            label="Содержание (Markdown)"
            rules={[{ required: true, message: 'Введите содержание' }]}
          >
            <MonacoMarkdownEditor themeMode={themeMode} />
          </Form.Item>

          <Title level={5} style={{ marginTop: 0 }}>Привязки</Title>

          <Form.Item name="company_ids" label="Компании">
            <Select
              mode="multiple"
              allowClear
              showSearch
              filterOption={false}
              options={companyOptions}
              onSearch={setCompanySearch}
              onInputKeyDown={(event) => {
                if (event.key === 'Enter') {
                  setCompanyAppliedSearch(companySearch);
                }
              }}
              placeholder="Найдите и выберите компании"
            />
          </Form.Item>

          <Form.Item name="server_ids" label="Серверы">
            <Select
              mode="multiple"
              allowClear
              showSearch
              filterOption={false}
              options={serverOptions}
              onSearch={setServerSearch}
              onInputKeyDown={(event) => {
                if (event.key === 'Enter') {
                  setServerAppliedSearch(serverSearch);
                }
              }}
              placeholder="Найдите и выберите серверы"
            />
          </Form.Item>

          <Form.Item name="workstation_ids" label="Рабочие станции">
            <Select
              mode="multiple"
              allowClear
              showSearch
              filterOption={false}
              options={workstationOptions}
              onSearch={setWorkstationSearch}
              onInputKeyDown={(event) => {
                if (event.key === 'Enter') {
                  setWorkstationAppliedSearch(workstationSearch);
                }
              }}
              placeholder="Найдите и выберите рабочие станции"
            />
          </Form.Item>

          <Form.Item name="fiscal_ids" label="Фискальные регистраторы">
            <Select
              mode="multiple"
              allowClear
              showSearch
              filterOption={false}
              options={fiscalOptions}
              onSearch={setFiscalSearch}
              onInputKeyDown={(event) => {
                if (event.key === 'Enter') {
                  setFiscalAppliedSearch(fiscalSearch);
                }
              }}
              placeholder="Найдите и выберите ФР (владелец • серийный номер)"
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};

export default MaterialsPanel;
