import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Form, Input, Modal, Select, Space, Typography, Upload, message } from 'antd';
import type { UploadFile } from 'antd';
import { UploadOutlined } from '@ant-design/icons';
import Editor from '@monaco-editor/react';
import { materialsApi } from '@/api/materials';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import { MaterialDTO, MaterialEntityRefDTO, MaterialPayload } from '@/types/api';
import { useUiStore } from '@/store/uiStore';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';
import { withApiError } from '@/utils/apiError';
import MaterialFilesList from '@/components/materials/MaterialFilesList';
import { useDeleteMaterialFile } from '@/components/materials/useMaterialMutations';
import { formatFileSize } from '@/utils/formatters';

const { Title } = Typography;

type SelectOption = { value: string; label: string };

type MaterialFormValues = {
  subject: string;
  content: string;
  company_ids: string[];
  server_ids: string[];
  workstation_ids: string[];
  fiscal_ids: string[];
};

interface MaterialEditorModalProps {
  open: boolean;
  /** Редактируемый материал (актуальная версия из списка); null - создание нового. */
  material: MaterialDTO | null;
  /** Привязки нового материала. Пусто - материал без привязки к сущностям. */
  defaultRefs?: MaterialEntityRefDTO[];
  /** Привязка, которая всегда сохраняется у материала (сущность, из карточки которой он открыт). */
  pinnedRef?: MaterialEntityRefDTO;
  onClose: () => void;
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


// MaterialEditorModal создает и редактирует материал: текст, файлы в S3 и привязки к сущностям (привязки необязательны).
const MaterialEditorModal: React.FC<MaterialEditorModalProps> = ({ open, material, defaultRefs, pinnedRef, onClose }) => {
  const themeMode = useUiStore((state) => state.themeMode);
  const queryClient = useQueryClient();
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
  const [pendingFiles, setPendingFiles] = useState<UploadFile[]>([]);
  const [form] = Form.useForm<MaterialFormValues>();
  const { deleteFileMutation, deletingFileID } = useDeleteMaterialFile();

  const { data: filesConfigRes } = useQuery({
    queryKey: ['materials-files-config'],
    queryFn: () => materialsApi.filesConfig(),
    staleTime: 5 * 60_000,
  });
  const filesConfig = filesConfigRes?.data;

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
    queryFn: () => equipmentApi.listFiscals({ term: fiscalAppliedSearch, limit: 20, offset: 0 }),
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
    mutationFn: async (values: MaterialFormValues) => {
      const refs = dedupeRefs([
        ...values.company_ids.map((id) => ({ entity_type: 'Company' as const, entity_id: id })),
        ...values.server_ids.map((id) => ({ entity_type: 'Server' as const, entity_id: id })),
        ...values.workstation_ids.map((id) => ({ entity_type: 'Workstation' as const, entity_id: id })),
        ...values.fiscal_ids.map((id) => ({ entity_type: 'FiscalRegister' as const, entity_id: id })),
        ...(pinnedRef ? [pinnedRef] : []),
      ]);

      const payload: MaterialPayload = {
        subject: values.subject.trim(),
        content: values.content,
        entity_refs: refs,
      };

      const newFiles = pendingFiles.map((item) => item.originFileObj as File).filter(Boolean);
      if (material) {
        const updated = await materialsApi.update(material.id, payload);
        if (newFiles.length > 0) {
          await materialsApi.uploadFiles(material.id, newFiles);
        }
        return updated;
      }
      if (newFiles.length > 0) {
        return materialsApi.createWithFiles(payload, newFiles);
      }
      return materialsApi.create(payload);
    },
    onSuccess: () => {
      message.success(material ? 'Материал обновлён' : 'Материал создан');
      queryClient.invalidateQueries({ queryKey: ['materials'] });
      onClose();
    },
    onError: (error) => {
      message.error(withApiError('Не удалось сохранить материал', error));
    },
  });

  const beforeFileSelect = (file: File) => {
    if (filesConfig && file.size > filesConfig.max_file_bytes) {
      message.error(`Файл «${file.name}» больше допустимого размера ${formatFileSize(filesConfig.max_file_bytes)}`);
      return Upload.LIST_IGNORE;
    }
    return false;
  };

  // Форма заполняется при каждом открытии: данными материала или привязками по умолчанию.
  useEffect(() => {
    if (!open) {
      return;
    }
    setPendingFiles([]);
    const refs = material ? material.entity_refs : (defaultRefs || []);
    form.setFieldsValue({
      subject: material?.subject || '',
      content: material?.content || '',
      company_ids: refs.filter((ref) => ref.entity_type === 'Company').map((ref) => ref.entity_id),
      server_ids: refs.filter((ref) => ref.entity_type === 'Server').map((ref) => ref.entity_id),
      workstation_ids: refs.filter((ref) => ref.entity_type === 'Workstation').map((ref) => ref.entity_id),
      fiscal_ids: refs.filter((ref) => ref.entity_type === 'FiscalRegister').map((ref) => ref.entity_id),
    });
    if (refs.length > 0) {
      void preloadReferenceLabels(refs);
    }
    // Форму нужно переинициализировать только при открытии или смене материала, а не при обновлении его файлов.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, material?.id]);

  return (
  <Modal
    open={open}
    onCancel={onClose}
    title={material ? 'Редактирование материала' : 'Новый материал'}
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
        rules={[{
          validator: async (_, value?: string) => {
            if (String(value || '').trim() || pendingFiles.length > 0 || (material?.files.length || 0) > 0) {
              return;
            }
            throw new Error('Введите содержание или приложите файл');
          },
        }]}
      >
        <MonacoMarkdownEditor themeMode={themeMode} />
      </Form.Item>

      <Form.Item
        label="Файлы"
        extra={filesConfig?.enabled
          ? `Файлы хранятся в S3 и доступны по прямым ссылкам. До ${filesConfig.max_files} файлов за раз, не более ${formatFileSize(filesConfig.max_file_bytes)} каждый.`
          : 'Загрузка файлов недоступна: хранилище S3 для материалов не настроено.'}
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          {(material?.files.length || 0) > 0 ? (
            <MaterialFilesList
              files={material?.files || []}
              onDelete={(file) => material && deleteFileMutation.mutate({ materialID: material.id, fileID: file.id })}
              deletingFileID={deletingFileID}
            />
          ) : null}
          <Upload
            multiple
            fileList={pendingFiles}
            beforeUpload={beforeFileSelect}
            onChange={({ fileList }) => setPendingFiles(fileList)}
            maxCount={filesConfig?.max_files}
            disabled={!filesConfig?.enabled}
          >
            <Button icon={<UploadOutlined />} disabled={!filesConfig?.enabled}>Прикрепить файлы</Button>
          </Upload>
        </Space>
      </Form.Item>

      <Title level={5} style={{ marginTop: 0 }}>Привязки (необязательно)</Title>

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
  );
};

export default MaterialEditorModal;
