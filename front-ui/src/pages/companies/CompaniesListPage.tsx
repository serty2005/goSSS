import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Empty, Select, Space, Spin, Table, Tag, Tabs, Tooltip, Typography, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { BankOutlined, CheckOutlined, CloseOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import { BitrixServicePointDTO, CompanyBitrixMappingRowDTO, CompanyModel } from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';
import { formatMappedServicePointLabel } from './companyBitrixMappingState';

const { Title, Text } = Typography;

type CompanyOption = { value: string; label: string };
type PointOption = { value: number; label: string };
type CompanyMappingDraft = Record<string, number | undefined>;
type PointMappingDraft = Record<number, string | undefined>;

const COMPANIES_LIMIT = 50;
const MAPPINGS_PAGE_LIMIT = 200;
const POINTS_PAGE_LIMIT = 200;

const isKnownDate = (value?: string) => {
  if (!value) return false;
  const parsed = dayjs(value);
  return parsed.isValid() && parsed.year() > 1900;
};

const formatDateTime = (value?: string) => {
  if (!isKnownDate(value)) {
    return '-';
  }
  return dayjs(value).format('DD.MM.YYYY HH:mm');
};

const contractStatusTag = (active?: boolean, contractType?: string) => (
  <Space size={6} wrap>
    <Tag color={active ? 'success' : 'default'}>{active ? 'Активен' : 'Нет активного'}</Tag>
    {contractType && <Text type="secondary">{contractType}</Text>}
  </Space>
);

const ellipsisText = (value?: string | null) => (
  <div className="company-ticket-table__cell-ellipsis" title={value || '-'}>
    {value || '-'}
  </div>
);

const buildCompanyOptionLabel = (item: CompanyModel | CompanyBitrixMappingRowDTO) => {
  const isMappingRow = 'company_id' in item;
  const id = isMappingRow ? item.company_id : resolveCompanyID(item);
  const title = (isMappingRow ? item.company_title : resolveCompanyTitle(item)) || id || 'Компания';
  const parentTitle = isMappingRow ? item.company_parent_title : resolveCompanyParentTitle(item);
  return parentTitle ? `${title} (${parentTitle})` : title;
};

const buildPointOptionLabel = (point: BitrixServicePointDTO) => {
  const parts = [point.name || `ID ${point.b24_element_id}`];
  if (point.one_c_code) {
    parts.push(`1C: ${point.one_c_code}`);
  }
  if (point.contract_type) {
    parts.push(point.contract_type);
  } else if (typeof point.contract_on === 'boolean') {
    parts.push(point.contract_on ? 'контракт активен' : 'контракт не активен');
  }
  return parts.join(' · ');
};

const fetchAllBitrixMappings = async (term: string) => {
  const result: CompanyBitrixMappingRowDTO[] = [];
  let offset = 0;
  for (let page = 0; page < 50; page += 1) {
    const response = await companiesApi.getBitrixMappings(term, MAPPINGS_PAGE_LIMIT, offset);
    const rows = response.data || [];
    result.push(...rows);
    if (rows.length < MAPPINGS_PAGE_LIMIT) {
      break;
    }
    offset += MAPPINGS_PAGE_LIMIT;
  }
  return result;
};

const fetchAllServicePoints = async (term: string) => {
  const result: BitrixServicePointDTO[] = [];
  let offset = 0;
  for (let page = 0; page < 50; page += 1) {
    const rows = await ticketsApi.getBitrixServicePoints({
      term,
      limit: POINTS_PAGE_LIMIT,
      offset,
    });
    result.push(...rows);
    if (rows.length < POINTS_PAGE_LIMIT) {
      break;
    }
    offset += POINTS_PAGE_LIMIT;
  }
  return result;
};

const CompaniesListPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const isAdminUser = isAdmin(user?.roles);
  const canMapBitrix = isAdminUser && isBitrixEnabled;

  const [companyLookupTerm, setCompanyLookupTerm] = useState('');
  const [servicePointLookupTerm, setServicePointLookupTerm] = useState('');
  const [companyLookupAppliedTerm, setCompanyLookupAppliedTerm] = useState('');
  const [servicePointLookupAppliedTerm, setServicePointLookupAppliedTerm] = useState('');
  const [editingCompanyID, setEditingCompanyID] = useState<string | null>(null);
  const [editingPointID, setEditingPointID] = useState<number | null>(null);
  const [companyDrafts, setCompanyDrafts] = useState<CompanyMappingDraft>({});
  const [pointDrafts, setPointDrafts] = useState<PointMappingDraft>({});
  const companiesLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const debouncedCompanyLookupTerm = useDebouncedValue(companyLookupTerm, SELECT_SEARCH_DEBOUNCE_MS);
  const debouncedServicePointLookupTerm = useDebouncedValue(servicePointLookupTerm, SELECT_SEARCH_DEBOUNCE_MS);

  const {
    data: companiesData,
    isLoading: isCompaniesLoading,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: ['companies', 'list', term],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => companiesApi.searchCompanies(term, COMPANIES_LIMIT, Number(pageParam) || 0),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || COMPANIES_LIMIT);
    },
    staleTime: 30_000,
  });

  const { data: mappingsData, isLoading: isMappingsLoading } = useQuery({
    queryKey: ['companies', 'bitrix-mappings', term],
    queryFn: () => fetchAllBitrixMappings(term),
    enabled: canMapBitrix,
    staleTime: 15_000,
  });

  const { data: servicePoints = [], isLoading: isServicePointsLoading } = useQuery({
    queryKey: ['bitrix-service-points', 'companies-page', term],
    queryFn: () => fetchAllServicePoints(term),
    enabled: canMapBitrix,
    staleTime: 15_000,
  });

  const { data: pointsLookup = [] } = useQuery({
    queryKey: ['bitrix-service-points', 'company-mapping-lookup', servicePointLookupAppliedTerm],
    queryFn: () => ticketsApi.getBitrixServicePoints({
      term: servicePointLookupAppliedTerm,
      limit: 40,
      offset: 0,
      random_if_empty: true,
    }),
    enabled: canMapBitrix,
    staleTime: 15_000,
  });

  const { data: companiesLookupData } = useQuery({
    queryKey: ['companies', 'mapping-lookup', companyLookupAppliedTerm],
    queryFn: () => companiesApi.searchCompanies(companyLookupAppliedTerm, 40, 0),
    enabled: canMapBitrix,
    staleTime: 30_000,
  });

  useEffect(() => {
    setCompanyLookupAppliedTerm(debouncedCompanyLookupTerm);
  }, [debouncedCompanyLookupTerm]);

  useEffect(() => {
    setServicePointLookupAppliedTerm(debouncedServicePointLookupTerm);
  }, [debouncedServicePointLookupTerm]);

  const mappings = useMemo(() => mappingsData || [], [mappingsData]);
  const companies = useMemo(() => (companiesData?.pages || []).flatMap((pageData) => pageData.data || []), [companiesData?.pages]);
  const companiesTotal = companiesData?.pages?.[0]?.meta?.total || 0;

  const mappingByPointID = useMemo(() => {
    const result = new Map<number, CompanyBitrixMappingRowDTO>();
    mappings.forEach((row) => {
      if (row.bitrix_service_point_id) {
        result.set(row.bitrix_service_point_id, row);
      }
    });
    return result;
  }, [mappings]);

  useEffect(() => {
    const nextCompanyDrafts: CompanyMappingDraft = {};
    const nextPointDrafts: PointMappingDraft = {};
    mappings.forEach((row) => {
      nextCompanyDrafts[row.company_id] = row.bitrix_service_point_id;
      if (row.bitrix_service_point_id) {
        nextPointDrafts[row.bitrix_service_point_id] = row.company_id;
      }
    });
    servicePoints.forEach((point) => {
      if (nextPointDrafts[point.b24_element_id] === undefined) {
        nextPointDrafts[point.b24_element_id] = undefined;
      }
    });
    setCompanyDrafts(nextCompanyDrafts);
    setPointDrafts(nextPointDrafts);
  }, [mappings, servicePoints]);

  useEffect(() => {
    const node = companiesLoadMoreRef.current;
    if (!node || !hasNextPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextPage) {
          return;
        }
        void fetchNextPage();
      },
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, companies.length]);

  const companyLookupOptions = useMemo<CompanyOption[]>(() => {
    const source = [...mappings, ...(companiesLookupData?.data || [])];
    const seen = new Set<string>();
    return source
      .map((item) => {
        const id = 'company_id' in item ? item.company_id : resolveCompanyID(item);
        if (!id || seen.has(id)) return null;
        seen.add(id);
        return { value: id, label: buildCompanyOptionLabel(item) };
      })
      .filter(Boolean) as CompanyOption[];
  }, [companiesLookupData?.data, mappings]);

  const servicePointOptions = useMemo<PointOption[]>(() => {
    const source = [...servicePoints, ...pointsLookup];
    const seen = new Set<number>();
    return source
      .map((point) => {
        if (!point.b24_element_id || seen.has(point.b24_element_id)) return null;
        seen.add(point.b24_element_id);
        return { value: point.b24_element_id, label: buildPointOptionLabel(point) };
      })
      .filter(Boolean) as PointOption[];
  }, [pointsLookup, servicePoints]);

  const updateMutation = useMutation({
    mutationFn: (payload: { company_id?: string; bitrix_service_point_id?: number }) => companiesApi.updateBitrixMapping(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['companies', 'bitrix-mappings'] });
      queryClient.invalidateQueries({ queryKey: ['companies', 'list'] });
      queryClient.invalidateQueries({ queryKey: ['bitrix-service-points'] });
      message.success('Сопоставление сохранено, контракт синхронизирован');
    },
    onError: (error: any) => {
      const apiMessage = error?.response?.data?.error?.error;
      message.error(apiMessage || 'Не удалось сохранить сопоставление');
    },
  });

  const applyCompanyMapping = async (row: CompanyBitrixMappingRowDTO) => {
    const pointID = companyDrafts[row.company_id];
    await updateMutation.mutateAsync({
      company_id: row.company_id,
      bitrix_service_point_id: pointID,
    });
    setEditingCompanyID(null);
  };

  const applyPointMapping = async (point: BitrixServicePointDTO) => {
    const companyID = pointDrafts[point.b24_element_id];
    await updateMutation.mutateAsync({
      company_id: companyID,
      bitrix_service_point_id: point.b24_element_id,
    });
    setEditingPointID(null);
  };

  const renderPointMappingCell = (row: CompanyBitrixMappingRowDTO) => {
    const isEditing = editingCompanyID === row.company_id;
    const originalPointID = row.bitrix_service_point_id;
    const draftPointID = companyDrafts[row.company_id];
    const dirty = (draftPointID ?? undefined) !== (originalPointID ?? undefined);
    const mappedLabel = formatMappedServicePointLabel({
      bitrix_service_point_id: draftPointID,
      bitrix_service_point_name: row.bitrix_service_point_name,
      bitrix_service_point_code: row.bitrix_service_point_code,
      bitrix_service_point_enabled: row.bitrix_service_point_enabled,
    });

    if (!canMapBitrix) {
      return ellipsisText(mappedLabel);
    }

    if (!isEditing) {
      return (
        <button
          type="button"
          className="companies-mapping-cell"
          onClick={() => setEditingCompanyID(row.company_id)}
          title={mappedLabel}
        >
          {mappedLabel}
        </button>
      );
    }

    return (
      <Space.Compact block className="companies-mapping-editor">
        <Select
          allowClear
          showSearch
          value={draftPointID}
          options={servicePointOptions}
          onSearch={setServicePointLookupTerm}
          onChange={(value) => {
            setCompanyDrafts((prev) => ({ ...prev, [row.company_id]: value }));
          }}
          placeholder="Выберите точку B24"
          filterOption={false}
        />
        <Tooltip title={dirty ? 'Подтвердить сопоставление' : 'Нет изменений'}>
          <Button
            type="primary"
            icon={<CheckOutlined />}
            disabled={!dirty}
            loading={updateMutation.isPending}
            onClick={() => void applyCompanyMapping(row)}
          />
        </Tooltip>
        <Tooltip title="Отмена">
          <Button
            icon={<CloseOutlined />}
            onClick={() => {
              setCompanyDrafts((prev) => ({ ...prev, [row.company_id]: originalPointID }));
              setEditingCompanyID(null);
            }}
          />
        </Tooltip>
      </Space.Compact>
    );
  };

  const renderCompanyMappingCell = (point: BitrixServicePointDTO) => {
    const pointID = point.b24_element_id;
    const mapping = mappingByPointID.get(pointID);
    const isEditing = editingPointID === pointID;
    const originalCompanyID = mapping?.company_id;
    const draftCompanyID = pointDrafts[pointID];
    const dirty = (draftCompanyID ?? undefined) !== (originalCompanyID ?? undefined);
    const currentLabel = draftCompanyID
      ? companyLookupOptions.find((option) => option.value === draftCompanyID)?.label || mapping?.company_title || draftCompanyID
      : 'Компания не выбрана';

    if (!canMapBitrix) {
      return ellipsisText(currentLabel);
    }

    if (!isEditing) {
      return (
        <button
          type="button"
          className="companies-mapping-cell"
          onClick={() => setEditingPointID(pointID)}
          title={currentLabel}
        >
          {currentLabel}
        </button>
      );
    }

    return (
      <Space.Compact block className="companies-mapping-editor">
        <Select
          allowClear
          showSearch
          value={draftCompanyID}
          options={companyLookupOptions}
          onSearch={setCompanyLookupTerm}
          onChange={(value) => {
            setPointDrafts((prev) => ({ ...prev, [pointID]: value }));
          }}
          placeholder="Выберите компанию"
          filterOption={false}
        />
        <Tooltip title={dirty ? 'Подтвердить сопоставление' : 'Нет изменений'}>
          <Button
            type="primary"
            icon={<CheckOutlined />}
            disabled={!dirty}
            loading={updateMutation.isPending}
            onClick={() => void applyPointMapping(point)}
          />
        </Tooltip>
        <Tooltip title="Отмена">
          <Button
            icon={<CloseOutlined />}
            onClick={() => {
              setPointDrafts((prev) => ({ ...prev, [pointID]: originalCompanyID }));
              setEditingPointID(null);
            }}
          />
        </Tooltip>
      </Space.Compact>
    );
  };

  const companyRows = useMemo<CompanyBitrixMappingRowDTO[]>(() => {
    if (canMapBitrix) {
      return mappings;
    }
    return companies.map((item) => ({
      company_id: resolveCompanyID(item) || '',
      company_title: resolveCompanyTitle(item) || '',
      company_parent_title: resolveCompanyParentTitle(item) || undefined,
      company_additional_name: item.additional_name,
      company_address: item.address,
      company_active_contract: item.active_contract,
      company_contract_type: item.contract_type,
    })).filter((row) => row.company_id);
  }, [canMapBitrix, companies, mappings]);

  const companyColumns: ColumnsType<CompanyBitrixMappingRowDTO> = [
    {
      title: 'Имя',
      dataIndex: 'company_title',
      key: 'company_title',
      width: 260,
      render: (_value, row) => (
        <Space size={8} className="companies-name-cell">
          <BankOutlined />
          <Link to={`/companies/${row.company_id}`} className="company-ticket-table__company-link">
            {row.company_title || row.company_id}
          </Link>
        </Space>
      ),
    },
    {
      title: 'Статус контракта',
      key: 'contract_status',
      width: 190,
      render: (_value, row) => contractStatusTag(row.company_active_contract, row.company_contract_type),
    },
    {
      title: 'Родитель',
      dataIndex: 'company_parent_title',
      key: 'company_parent_title',
      width: 220,
      render: ellipsisText,
    },
    {
      title: 'Адрес',
      dataIndex: 'company_address',
      key: 'company_address',
      width: 280,
      render: ellipsisText,
    },
    {
      title: 'Юр. название',
      dataIndex: 'company_additional_name',
      key: 'company_additional_name',
      width: 260,
      render: ellipsisText,
    },
    {
      title: 'Сопоставленная точка Bitrix24',
      key: 'bitrix_mapping',
      width: 380,
      render: (_value, row) => renderPointMappingCell(row),
    },
  ];

  const servicePointColumns: ColumnsType<BitrixServicePointDTO> = [
    {
      title: 'ID в 1C',
      dataIndex: 'one_c_code',
      key: 'one_c_code',
      width: 150,
      render: ellipsisText,
    },
    {
      title: 'Название',
      dataIndex: 'name',
      key: 'name',
      width: 280,
      render: ellipsisText,
    },
    {
      title: 'Дата создания',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
      render: formatDateTime,
    },
    {
      title: 'Дата обновления',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      render: formatDateTime,
    },
    {
      title: 'Тип контракта',
      dataIndex: 'contract_type',
      key: 'contract_type',
      width: 200,
      render: (_value, row) => contractStatusTag(row.contract_on ?? undefined, row.contract_type),
    },
    {
      title: 'Сопоставленная компания',
      key: 'company_mapping',
      width: 380,
      render: (_value, row) => renderCompanyMappingCell(row),
    },
  ];

  const companiesContent = (
    <div className="companies-table-surface">
      {isCompaniesLoading || (canMapBitrix && isMappingsLoading) ? (
        <div className="companies-table-loader"><Spin size="large" /></div>
      ) : companyRows.length === 0 ? (
        <Empty description="Компании не найдены" />
      ) : (
        <>
          <Table<CompanyBitrixMappingRowDTO>
            rowKey="company_id"
            columns={companyColumns}
            dataSource={companyRows}
            pagination={false}
            size="small"
            className="tickets-table company-ticket-table companies-work-table"
            scroll={{ x: 1550 }}
          />
          {!canMapBitrix && (
            <div ref={companiesLoadMoreRef} className="companies-load-more">
              {(isFetchingNextPage || (hasNextPage && companies.length > 0)) && <Spin size="small" />}
              {!hasNextPage && companies.length > 0 && (
                <Text type="secondary">Показано: {companies.length} из {companiesTotal}</Text>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );

  const servicePointsContent = (
    <div className="companies-table-surface">
      {isServicePointsLoading || isMappingsLoading ? (
        <div className="companies-table-loader"><Spin size="large" /></div>
      ) : servicePoints.length === 0 ? (
        <Empty description="Точки обслуживания Bitrix24 не найдены" />
      ) : (
        <Table<BitrixServicePointDTO>
          rowKey="b24_element_id"
          columns={servicePointColumns}
          dataSource={servicePoints}
          pagination={{ pageSize: 50, hideOnSinglePage: true }}
          size="small"
          className="tickets-table company-ticket-table companies-work-table"
          scroll={{ x: 1350 }}
        />
      )}
    </div>
  );

  const tabItems = [
    { key: 'companies', label: 'Компании', children: companiesContent },
  ];

  if (canMapBitrix) {
    tabItems.push({ key: 'service-points', label: 'Точки обслуживания Bitrix24', children: servicePointsContent });
  }

  return (
    <Space direction="vertical" size="middle" className="companies-page">
      <Title level={4} style={{ margin: 0 }}>
        Компании {term ? `по запросу "${term}"` : ''}
      </Title>
      <Tabs defaultActiveKey="companies" items={tabItems} />
    </Space>
  );
};

export default CompaniesListPage;
