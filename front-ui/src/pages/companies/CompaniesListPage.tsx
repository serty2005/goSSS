import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Checkbox, DatePicker, Select, Space, Spin, Tag, Tabs, Tooltip, Typography, message } from 'antd';
import type { Dayjs } from 'dayjs';
import { BankOutlined, CheckOutlined, CloseOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import { BitrixServicePointDTO, CompanyBitrixMappingRowDTO, CompanyModel } from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';
import DataTable, {
  DataTableColumn,
  DataTableTextCell,
} from '@/components/common/DataTable';
import {
  createDataTableColumnMinWidth,
  estimateDataTableHeaderMinWidth,
  formatDataTableText,
} from '@/components/common/dataTableUtils';
import { formatMappedServicePointLabel } from './companyBitrixMappingState';

const { Title, Text } = Typography;
const { RangePicker } = DatePicker;

type CompanyOption = { value: string; label: string };
type PointOption = { value: number; label: string };
type CompanyMappingDraft = Record<string, number | undefined>;
type PointMappingDraft = Record<number, string | undefined>;
type DateRangeValue = [Dayjs | null, Dayjs | null] | null;

const COMPANIES_LIMIT = 50;
const MAPPINGS_PAGE_LIMIT = 50;
const POINTS_PAGE_LIMIT = 50;
const EMPTY_SERVICE_POINTS: BitrixServicePointDTO[] = [];
const COMPANY_TABLE_COLUMN_KEYS = [
  'company_title',
  'contract_status',
  'company_parent_title',
  'company_address',
  'company_additional_name',
  'bitrix_mapping',
] as const;
const SERVICE_POINT_TABLE_COLUMN_KEYS = [
  'one_c_code',
  'name',
  'created_at',
  'updated_at',
  'contract_type',
  'company_mapping',
] as const;

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
    {contractType && <Text type="secondary">{formatDataTableText(contractType)}</Text>}
  </Space>
);

const ellipsisText = (value?: string | null) => <DataTableTextCell value={value} />;

const getDateValue = (value?: string) => {
  if (!isKnownDate(value)) {
    return 0;
  }
  return dayjs(value).valueOf();
};

const isDateInRange = (value: string | undefined, range: DateRangeValue) => {
  if (!range?.[0] && !range?.[1]) {
    return true;
  }
  if (!isKnownDate(value)) {
    return false;
  }
  const parsed = dayjs(value);
  const from = range?.[0]?.startOf('day');
  const to = range?.[1]?.endOf('day');
  return (!from || !parsed.isBefore(from)) && (!to || !parsed.isAfter(to));
};

const renderCheckboxFilter = (
  title: string,
  values: string[],
  options: Array<{ value: string; label: React.ReactNode; count?: number }>,
  onChange: (values: string[]) => void,
) => (
  <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
    <Text strong>{title}</Text>
    <Checkbox
      checked={values.length === 0}
      onChange={() => onChange([])}
    >
      Все
    </Checkbox>
    <Checkbox.Group
      value={values}
      onChange={(nextValues) => onChange(nextValues.map((value) => String(value)))}
    >
      <Space direction="vertical" size={4}>
        {options.map((option) => (
          <Checkbox key={option.value} value={option.value}>
            <Space size={6}>
              <span>{option.label}</span>
              {typeof option.count === 'number' && <Tag>{option.count}</Tag>}
            </Space>
          </Checkbox>
        ))}
      </Space>
    </Checkbox.Group>
  </Space>
);

const renderDateRangeFilter = (
  title: string,
  value: DateRangeValue,
  onChange: (value: DateRangeValue) => void,
) => (
  <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
    <Text strong>{title}</Text>
    <RangePicker
      value={value}
      format="DD.MM.YYYY"
      allowClear
      onChange={(nextValue) => onChange((nextValue as DateRangeValue) || null)}
    />
  </Space>
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
  const [companyContractFilter, setCompanyContractFilter] = useState<string[]>([]);
  const [companyParentFilter, setCompanyParentFilter] = useState<string[]>([]);
  const [companyMappingFilter, setCompanyMappingFilter] = useState<string[]>([]);
  const [servicePointContractFilter, setServicePointContractFilter] = useState<string[]>([]);
  const [servicePointCreatedRange, setServicePointCreatedRange] = useState<DateRangeValue>(null);
  const [servicePointUpdatedRange, setServicePointUpdatedRange] = useState<DateRangeValue>(null);
  const [activeTabKey, setActiveTabKey] = useState('companies');
  const companiesLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const mappingsLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const servicePointsLoadMoreRef = useRef<HTMLDivElement | null>(null);
  const debouncedCompanyLookupTerm = useDebouncedValue(companyLookupTerm, SELECT_SEARCH_DEBOUNCE_MS);
  const debouncedServicePointLookupTerm = useDebouncedValue(servicePointLookupTerm, SELECT_SEARCH_DEBOUNCE_MS);

  const {
    data: companiesData,
    isLoading: isCompaniesLoading,
    isFetchingNextPage,
    hasNextPage,
    fetchNextPage,
  } = useInfiniteQuery({
    queryKey: ['companies', 'list', term, companyParentFilter],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => companiesApi.searchCompanies(
      term,
      COMPANIES_LIMIT,
      Number(pageParam) || 0,
      companyParentFilter,
    ),
    enabled: !canMapBitrix,
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || COMPANIES_LIMIT);
    },
    staleTime: 30_000,
  });

  const {
    data: mappingsData,
    isLoading: isMappingsLoading,
    isFetchingNextPage: isFetchingNextMappingsPage,
    hasNextPage: hasNextMappingsPage,
    fetchNextPage: fetchNextMappingsPage,
  } = useInfiniteQuery({
    queryKey: ['companies', 'bitrix-mappings', term, companyParentFilter],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => companiesApi.getCompaniesWithBitrixMappings(
      term,
      MAPPINGS_PAGE_LIMIT,
      Number(pageParam) || 0,
      companyParentFilter,
    ),
    enabled: canMapBitrix,
    getNextPageParam: (lastPage, _pages, lastPageParam) => {
      const rows = lastPage.data || [];
      if (rows.length < MAPPINGS_PAGE_LIMIT) {
        return undefined;
      }
      return Number(lastPageParam || 0) + MAPPINGS_PAGE_LIMIT;
    },
    staleTime: 15_000,
  });

  const { data: parentCompaniesData } = useQuery({
    queryKey: ['companies', 'parents'],
    queryFn: () => companiesApi.getParentCompanies('', 300),
    staleTime: 60_000,
  });

  const {
    data: servicePointsData,
    isLoading: isServicePointsLoading,
    isFetchingNextPage: isFetchingNextServicePointsPage,
    hasNextPage: hasNextServicePointsPage,
    fetchNextPage: fetchNextServicePointsPage,
  } = useInfiniteQuery({
    queryKey: ['bitrix-service-points', 'companies-page', term],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => ticketsApi.getBitrixServicePoints({
      term,
      limit: POINTS_PAGE_LIMIT,
      offset: Number(pageParam) || 0,
    }),
    enabled: canMapBitrix && activeTabKey === 'service-points',
    getNextPageParam: (lastPage, _pages, lastPageParam) => {
      if ((lastPage || []).length < POINTS_PAGE_LIMIT) {
        return undefined;
      }
      return Number(lastPageParam || 0) + POINTS_PAGE_LIMIT;
    },
    staleTime: 15_000,
  });

  const { data: pointsLookupData } = useQuery({
    queryKey: ['bitrix-service-points', 'company-mapping-lookup', servicePointLookupAppliedTerm],
    queryFn: () => ticketsApi.getBitrixServicePoints({
      term: servicePointLookupAppliedTerm,
      limit: 40,
      offset: 0,
      random_if_empty: true,
    }),
    enabled: canMapBitrix && editingCompanyID !== null,
    staleTime: 15_000,
  });

  const { data: companiesLookupData } = useQuery({
    queryKey: ['companies', 'mapping-lookup', companyLookupAppliedTerm],
    queryFn: () => companiesApi.searchCompanies(companyLookupAppliedTerm, 40, 0),
    enabled: canMapBitrix && editingPointID !== null,
    staleTime: 30_000,
  });

  useEffect(() => {
    setCompanyLookupAppliedTerm(debouncedCompanyLookupTerm);
  }, [debouncedCompanyLookupTerm]);

  useEffect(() => {
    setServicePointLookupAppliedTerm(debouncedServicePointLookupTerm);
  }, [debouncedServicePointLookupTerm]);

  const mappings = useMemo(
    () => (mappingsData?.pages || []).flatMap((pageData) => pageData.data || []),
    [mappingsData?.pages],
  );
  const servicePoints = useMemo(
    () => (servicePointsData?.pages || []).flatMap((pageData) => pageData || []),
    [servicePointsData?.pages],
  );
  const pointsLookup = useMemo(() => pointsLookupData || EMPTY_SERVICE_POINTS, [pointsLookupData]);
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
    if (canMapBitrix || !node || !hasNextPage) {
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
  }, [canMapBitrix, fetchNextPage, hasNextPage, isFetchingNextPage, companies.length]);

  useEffect(() => {
    const node = mappingsLoadMoreRef.current;
    if (!canMapBitrix || !node || !hasNextMappingsPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextMappingsPage) {
          return;
        }
        void fetchNextMappingsPage();
      },
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [
    canMapBitrix,
    fetchNextMappingsPage,
    hasNextMappingsPage,
    isFetchingNextMappingsPage,
    mappings.length,
  ]);

  useEffect(() => {
    const node = servicePointsLoadMoreRef.current;
    if (!canMapBitrix || activeTabKey !== 'service-points' || !node || !hasNextServicePointsPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextServicePointsPage) {
          return;
        }
        void fetchNextServicePointsPage();
      },
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [
    activeTabKey,
    canMapBitrix,
    fetchNextServicePointsPage,
    hasNextServicePointsPage,
    isFetchingNextServicePointsPage,
    servicePoints.length,
  ]);

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

  const applyCompanyMapping = useCallback(async (row: CompanyBitrixMappingRowDTO) => {
    const pointID = companyDrafts[row.company_id];
    await updateMutation.mutateAsync({
      company_id: row.company_id,
      bitrix_service_point_id: pointID,
    });
    setEditingCompanyID(null);
  }, [companyDrafts, updateMutation]);

  const applyPointMapping = useCallback(async (point: BitrixServicePointDTO) => {
    const companyID = pointDrafts[point.b24_element_id];
    await updateMutation.mutateAsync({
      company_id: companyID,
      bitrix_service_point_id: point.b24_element_id,
    });
    setEditingPointID(null);
  }, [pointDrafts, updateMutation]);

  const renderPointMappingCell = useCallback((row: CompanyBitrixMappingRowDTO) => {
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
  }, [
    applyCompanyMapping,
    canMapBitrix,
    companyDrafts,
    editingCompanyID,
    servicePointOptions,
    updateMutation.isPending,
  ]);

  const renderCompanyMappingCell = useCallback((point: BitrixServicePointDTO) => {
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
  }, [
    applyPointMapping,
    canMapBitrix,
    companyLookupOptions,
    editingPointID,
    mappingByPointID,
    pointDrafts,
    updateMutation.isPending,
  ]);

  const companyRows = useMemo<CompanyBitrixMappingRowDTO[]>(() => {
    if (canMapBitrix) {
      return mappings;
    }
    return companies.map((item) => ({
      company_id: resolveCompanyID(item) || '',
      company_title: resolveCompanyTitle(item) || '',
      company_parent_id: item.parent_id,
      company_parent_title: resolveCompanyParentTitle(item) || undefined,
      company_additional_name: item.additional_name,
      company_address: item.address,
      company_active_contract: item.active_contract,
      company_contract_type: item.contract_type,
    })).filter((row) => row.company_id);
  }, [canMapBitrix, companies, mappings]);

  const companyFilterCounters = useMemo(() => {
    const result = {
      active: 0,
      inactive: 0,
      mapped: 0,
      unmapped: 0,
    };
    companyRows.forEach((row) => {
      if (row.company_active_contract) {
        result.active += 1;
      } else {
        result.inactive += 1;
      }
      if (row.bitrix_service_point_id) {
        result.mapped += 1;
      } else {
        result.unmapped += 1;
      }
    });
    return result;
  }, [companyRows]);

  const companyParentFilterOptions = useMemo(() => (
    (parentCompaniesData?.data || [])
      .map((item) => ({
        value: item.id,
        label: formatDataTableText(item.title) || item.id,
        count: item.children_count,
      }))
      .sort((left, right) => left.label.localeCompare(right.label, 'ru', {
        numeric: true,
        sensitivity: 'base',
      }))
  ), [parentCompaniesData?.data]);

  const servicePointFilterCounters = useMemo(() => {
    const result = {
      active: 0,
      inactive: 0,
    };
    servicePoints.forEach((point) => {
      if (point.contract_on) {
        result.active += 1;
      } else {
        result.inactive += 1;
      }
    });
    return result;
  }, [servicePoints]);

  const filteredCompanyRows = useMemo(() => (
    companyRows.filter((row) => {
      if (companyContractFilter.length > 0) {
        const status = row.company_active_contract ? 'active' : 'inactive';
        if (!companyContractFilter.includes(status)) {
          return false;
        }
      }
      if (companyParentFilter.length > 0) {
        const parentID = String(row.company_parent_id || '').trim();
        const parentTitle = formatDataTableText(row.company_parent_title);
        const parentKey = parentID || (parentTitle ? `title:${parentTitle}` : '');
        if (!parentKey || !companyParentFilter.includes(parentKey)) {
          return false;
        }
      }
      if (companyMappingFilter.length > 0) {
        const mappingStatus = row.bitrix_service_point_id ? 'mapped' : 'unmapped';
        if (!companyMappingFilter.includes(mappingStatus)) {
          return false;
        }
      }
      return true;
    })
  ), [companyContractFilter, companyMappingFilter, companyParentFilter, companyRows]);

  const filteredServicePoints = useMemo(() => (
    servicePoints.filter((point) => {
      if (servicePointContractFilter.length > 0) {
        const status = point.contract_on ? 'active' : 'inactive';
        if (!servicePointContractFilter.includes(status)) {
          return false;
        }
      }
      return isDateInRange(point.created_at, servicePointCreatedRange) &&
        isDateInRange(point.updated_at, servicePointUpdatedRange);
    })
  ), [servicePointContractFilter, servicePointCreatedRange, servicePointUpdatedRange, servicePoints]);

  const companyColumns = useMemo<DataTableColumn<CompanyBitrixMappingRowDTO>[]>(() => [
    {
      title: 'Имя',
      dataIndex: 'company_title',
      key: 'company_title',
      width: 260,
      minWidth: createDataTableColumnMinWidth('Имя'),
      maxWidth: 460,
      sortValue: (row) => row.company_title || row.company_id,
      render: (_value, row) => (
        <Space size={8} className="companies-name-cell">
          <BankOutlined />
          <Link to={`/companies/${row.company_id}`} className="company-ticket-table__company-link">
            {formatDataTableText(row.company_title || row.company_id)}
          </Link>
        </Space>
      ),
    },
    {
      title: 'Статус контракта',
      key: 'contract_status',
      width: 190,
      minWidth: createDataTableColumnMinWidth('Статус контракта'),
      maxWidth: 260,
      sortValue: (row) => (row.company_active_contract ? 1 : 0),
      filterContent: renderCheckboxFilter(
        'Статус контракта',
        companyContractFilter,
        [
          { value: 'active', label: 'Активен', count: companyFilterCounters.active },
          { value: 'inactive', label: 'Нет активного', count: companyFilterCounters.inactive },
        ],
        setCompanyContractFilter,
      ),
      isFilterActive: companyContractFilter.length > 0,
      render: (_value, row) => contractStatusTag(row.company_active_contract, row.company_contract_type),
    },
    {
      title: 'Родитель',
      dataIndex: 'company_parent_title',
      key: 'company_parent_title',
      width: 220,
      minWidth: createDataTableColumnMinWidth('Родитель'),
      maxWidth: 380,
      sortValue: (row) => row.company_parent_title || '',
      filterContent: renderCheckboxFilter(
        'Родительская компания',
        companyParentFilter,
        companyParentFilterOptions,
        setCompanyParentFilter,
      ),
      isFilterActive: companyParentFilter.length > 0,
      render: ellipsisText,
    },
    {
      title: 'Адрес',
      dataIndex: 'company_address',
      key: 'company_address',
      width: 280,
      minWidth: createDataTableColumnMinWidth('Адрес'),
      maxWidth: 560,
      sortValue: (row) => row.company_address || '',
      render: ellipsisText,
    },
    {
      title: 'Юр. название',
      dataIndex: 'company_additional_name',
      key: 'company_additional_name',
      width: 260,
      minWidth: createDataTableColumnMinWidth('Юр. название'),
      maxWidth: 520,
      sortValue: (row) => row.company_additional_name || '',
      render: ellipsisText,
    },
    {
      title: 'Сопоставленная точка Bitrix24',
      key: 'bitrix_mapping',
      width: 380,
      minWidth: createDataTableColumnMinWidth('Сопоставленная точка Bitrix24'),
      maxWidth: 620,
      sortValue: (row) => formatMappedServicePointLabel({
        bitrix_service_point_id: row.bitrix_service_point_id,
        bitrix_service_point_name: row.bitrix_service_point_name,
        bitrix_service_point_code: row.bitrix_service_point_code,
        bitrix_service_point_enabled: row.bitrix_service_point_enabled,
      }),
      filterContent: renderCheckboxFilter(
        'Сопоставление',
        companyMappingFilter,
        [
          { value: 'mapped', label: 'Сопоставлены', count: companyFilterCounters.mapped },
          { value: 'unmapped', label: 'Без точки', count: companyFilterCounters.unmapped },
        ],
        setCompanyMappingFilter,
      ),
      isFilterActive: companyMappingFilter.length > 0,
      render: (_value, row) => renderPointMappingCell(row),
    },
  ], [
    companyContractFilter,
    companyFilterCounters.active,
    companyFilterCounters.inactive,
    companyFilterCounters.mapped,
    companyFilterCounters.unmapped,
    companyMappingFilter,
    companyParentFilter,
    companyParentFilterOptions,
    renderPointMappingCell,
  ]);

  const servicePointColumns = useMemo<DataTableColumn<BitrixServicePointDTO>[]>(() => [
    {
      title: 'ID в 1C',
      dataIndex: 'one_c_code',
      key: 'one_c_code',
      width: 150,
      minWidth: createDataTableColumnMinWidth('ID в 1C'),
      maxWidth: 240,
      sortValue: (point) => point.one_c_code || '',
      render: ellipsisText,
    },
    {
      title: 'Название',
      dataIndex: 'name',
      key: 'name',
      width: 280,
      minWidth: createDataTableColumnMinWidth('Название'),
      maxWidth: 560,
      sortValue: (point) => point.name || '',
      render: ellipsisText,
    },
    {
      title: 'Дата создания',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 160,
      minWidth: estimateDataTableHeaderMinWidth('Дата создания'),
      maxWidth: 230,
      sortValue: (point) => getDateValue(point.created_at),
      filterContent: renderDateRangeFilter('Дата создания', servicePointCreatedRange, setServicePointCreatedRange),
      isFilterActive: Boolean(servicePointCreatedRange?.[0] || servicePointCreatedRange?.[1]),
      render: formatDateTime,
    },
    {
      title: 'Дата обновления',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 160,
      minWidth: estimateDataTableHeaderMinWidth('Дата обновления'),
      maxWidth: 230,
      sortValue: (point) => getDateValue(point.updated_at),
      filterContent: renderDateRangeFilter('Дата обновления', servicePointUpdatedRange, setServicePointUpdatedRange),
      isFilterActive: Boolean(servicePointUpdatedRange?.[0] || servicePointUpdatedRange?.[1]),
      render: formatDateTime,
    },
    {
      title: 'Тип контракта',
      dataIndex: 'contract_type',
      key: 'contract_type',
      width: 200,
      minWidth: createDataTableColumnMinWidth('Тип контракта'),
      maxWidth: 320,
      sortValue: (point) => point.contract_type || '',
      filterContent: renderCheckboxFilter(
        'Статус контракта',
        servicePointContractFilter,
        [
          { value: 'active', label: 'Активен', count: servicePointFilterCounters.active },
          { value: 'inactive', label: 'Не активен', count: servicePointFilterCounters.inactive },
        ],
        setServicePointContractFilter,
      ),
      isFilterActive: servicePointContractFilter.length > 0,
      render: (_value, row) => contractStatusTag(row.contract_on ?? undefined, row.contract_type),
    },
    {
      title: 'Сопоставленная компания',
      key: 'company_mapping',
      width: 380,
      minWidth: createDataTableColumnMinWidth('Сопоставленная компания'),
      maxWidth: 620,
      sortValue: (point) => pointDrafts[point.b24_element_id] || '',
      render: (_value, row) => renderCompanyMappingCell(row),
    },
  ], [
    pointDrafts,
    renderCompanyMappingCell,
    servicePointContractFilter,
    servicePointCreatedRange,
    servicePointFilterCounters.active,
    servicePointFilterCounters.inactive,
    servicePointUpdatedRange,
  ]);

  const companiesContent = (
    <div className="companies-table-surface">
      {isCompaniesLoading || (canMapBitrix && isMappingsLoading) ? (
        <div className="companies-table-loader"><Spin size="large" /></div>
      ) : (
        <>
          <DataTable<CompanyBitrixMappingRowDTO>
            rowKey="company_id"
            columns={companyColumns}
            dataSource={filteredCompanyRows}
            pagination={false}
            size="small"
            className="companies-work-table"
            layoutKey="companies_list_table_layout"
            layoutStorage="local"
            visibilityStorageKey="companies_list_table_visible_columns"
            availableColumnKeys={[...COMPANY_TABLE_COLUMN_KEYS]}
            defaultVisibleColumnKeys={[...COMPANY_TABLE_COLUMN_KEYS]}
            emptyText="Компании не найдены"
          />
          {!canMapBitrix && (
            <div ref={companiesLoadMoreRef} className="companies-load-more">
              {(isFetchingNextPage || (hasNextPage && companies.length > 0)) && <Spin size="small" />}
              {!hasNextPage && companies.length > 0 && (
                <Text type="secondary">Показано: {companies.length} из {companiesTotal}</Text>
              )}
            </div>
          )}
          {canMapBitrix && (
            <div ref={mappingsLoadMoreRef} className="companies-load-more">
              {(isFetchingNextMappingsPage || (hasNextMappingsPage && mappings.length > 0)) && <Spin size="small" />}
              {!hasNextMappingsPage && mappings.length > 0 && (
                <Text type="secondary">Загружено сопоставлений: {mappings.length}</Text>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );

  const servicePointsContent = (
    <div className="companies-table-surface">
      {isServicePointsLoading ? (
        <div className="companies-table-loader"><Spin size="large" /></div>
      ) : (
        <>
          <DataTable<BitrixServicePointDTO>
            rowKey="b24_element_id"
            columns={servicePointColumns}
            dataSource={filteredServicePoints}
            pagination={false}
            size="small"
            className="companies-work-table"
            layoutKey="companies_service_points_table_layout"
            layoutStorage="local"
            visibilityStorageKey="companies_service_points_table_visible_columns"
            availableColumnKeys={[...SERVICE_POINT_TABLE_COLUMN_KEYS]}
            defaultVisibleColumnKeys={[...SERVICE_POINT_TABLE_COLUMN_KEYS]}
            emptyText="Точки обслуживания Bitrix24 не найдены"
          />
          <div ref={servicePointsLoadMoreRef} className="companies-load-more">
            {(isFetchingNextServicePointsPage || (hasNextServicePointsPage && servicePoints.length > 0)) && <Spin size="small" />}
            {!hasNextServicePointsPage && servicePoints.length > 0 && (
              <Text type="secondary">Загружено точек Bitrix24: {servicePoints.length}</Text>
            )}
          </div>
        </>
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
      <Tabs activeKey={activeTabKey} onChange={setActiveTabKey} items={tabItems} />
    </Space>
  );
};

export default CompaniesListPage;
