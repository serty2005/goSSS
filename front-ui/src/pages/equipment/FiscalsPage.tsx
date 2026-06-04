import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Card, DatePicker, Empty, Select, Space, Spin, Table, Typography } from 'antd';
import type { TableProps } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { FilterValue, SorterResult, SortOrder } from 'antd/es/table/interface';
import dayjs from 'dayjs';
import type { Dayjs } from 'dayjs';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';

const { Title, Text } = Typography;
const { RangePicker } = DatePicker;

type Row = {
  id: string;
  model: string;
  rnm: string;
  serial: string;
  legalName: string;
  address: string;
  fnExpireDate: string;
  ownerId: string;
  ownerName: string;
  parentCompanyId: string;
  parentCompanyName: string;
};

type ListViewState = {
  companies: string[];
  models: string[];
  fnExpireFrom: string;
  fnExpireTo: string;
};

const STORAGE_KEY = 'fiscals_list_view_state_v1';

const normalizeText = (value: unknown, fallback = '-') => {
  const clean = String(value ?? '').trim();
  return clean || fallback;
};

const compareText = (a: string, b: string) => a.localeCompare(b, 'ru', { sensitivity: 'base' });
const containsText = (value: string, search: string) =>
  value.toLocaleLowerCase('ru').includes(search.toLocaleLowerCase('ru'));

const sanitizeList = (value: unknown): string[] =>
  Array.isArray(value) ? value.map((item) => String(item).trim()).filter(Boolean) : [];

const sanitizeDate = (value: unknown) => {
  const raw = String(value ?? '').trim();
  return /^\d{4}-\d{2}-\d{2}$/.test(raw) && dayjs(raw).isValid() ? raw : '';
};

const readStoredViewState = (): ListViewState => {
  if (typeof window === 'undefined') {
    return { companies: [], models: [], fnExpireFrom: '', fnExpireTo: '' };
  }

  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return { companies: [], models: [], fnExpireFrom: '', fnExpireTo: '' };
    }

    const parsed = JSON.parse(raw) as Partial<ListViewState>;
    return {
      companies: sanitizeList(parsed.companies),
      models: sanitizeList(parsed.models),
      fnExpireFrom: sanitizeDate(parsed.fnExpireFrom),
      fnExpireTo: sanitizeDate(parsed.fnExpireTo),
    };
  } catch {
    return { companies: [], models: [], fnExpireFrom: '', fnExpireTo: '' };
  }
};

const formatDate = (value: string) => {
  if (!value) return '-';
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.format('DD.MM.YYYY') : '-';
};

const FiscalsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const initialViewState = useMemo(() => readStoredViewState(), []);

  const [selectedCompanyIDs, setSelectedCompanyIDs] = useState<string[]>(initialViewState.companies);
  const [selectedModels, setSelectedModels] = useState<string[]>(initialViewState.models);
  const [fnExpireRange, setFnExpireRange] = useState<[string, string]>([
    initialViewState.fnExpireFrom,
    initialViewState.fnExpireTo,
  ]);
  const [companyFilterSearch, setCompanyFilterSearch] = useState('');
  const [companyOptionCache, setCompanyOptionCache] = useState<Record<string, string>>({});
  const [sortBy, setSortBy] = useState('');
  const [sortOrder, setSortOrder] = useState<SortOrder>(null);

  const term = (searchParams.get('q') || '').trim();
  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  const companyFilterKey = useMemo(() => selectedCompanyIDs.slice().sort(), [selectedCompanyIDs]);
  const modelFilterKey = useMemo(() => selectedModels.slice().sort(), [selectedModels]);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: ['equipment', 'fiscals', term, companyFilterKey, modelFilterKey, fnExpireRange, sortBy, sortOrder],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      equipmentApi.listFiscals({
        term,
        limit,
        offset: Number(pageParam) || 0,
        companyIDs: companyFilterKey,
        models: modelFilterKey,
        fnExpireFrom: fnExpireRange[0],
        fnExpireTo: fnExpireRange[1],
        sortBy,
        sortOrder: sortOrder || undefined,
      }),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || limit);
    },
    staleTime: 30_000,
  });

  const { data: companySearchData, isFetching: isCompanySearchLoading } = useQuery({
    queryKey: ['companies', 'search', 'fiscals-filter', companyFilterSearch],
    queryFn: () => companiesApi.searchCompanies(companyFilterSearch, 30, 0),
    enabled: companyFilterSearch.trim().length >= 2,
    staleTime: 30_000,
  });

  const { data: filterOptionsData } = useQuery({
    queryKey: ['equipment', 'fiscals', 'filter-options'],
    queryFn: () => equipmentApi.getFiscalFilterOptions(),
    staleTime: 5 * 60_000,
  });

  const rows = useMemo<Row[]>(
    () =>
      (data?.pages || [])
        .flatMap((pageData) => pageData.data || [])
        .map((item) => {
          const row = item as Record<string, unknown>;
          const id = normalizeText(row.id, '');
          return {
            id,
            model: normalizeText(row.model_kkt, 'ККТ'),
            rnm: normalizeText(row.rn_kkt),
            serial: normalizeText(row.fr_serial_number),
            legalName: normalizeText(row.legal_name),
            address: normalizeText(row.address),
            fnExpireDate: normalizeText(row.fn_expire_date, ''),
            ownerId: normalizeText(row.owner_id, ''),
            ownerName: normalizeText(row.owner_title || row.owner_id, normalizeText(row.owner_id, '-')),
            parentCompanyId: normalizeText(row.owner_parent_id, ''),
            parentCompanyName: normalizeText(row.owner_parent_title, ''),
          };
        }),
    [data?.pages],
  );

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const payload: ListViewState = {
      companies: selectedCompanyIDs,
      models: selectedModels,
      fnExpireFrom: fnExpireRange[0],
      fnExpireTo: fnExpireRange[1],
    };
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  }, [fnExpireRange, selectedCompanyIDs, selectedModels]);

  useEffect(() => {
    setCompanyOptionCache((prev) => {
      let changed = false;
      const next = { ...prev };

      rows.forEach((row) => {
        if (row.ownerId && row.ownerName && next[row.ownerId] !== row.ownerName) {
          next[row.ownerId] = row.ownerName;
          changed = true;
        }
        if (row.parentCompanyId && row.parentCompanyName && next[row.parentCompanyId] !== row.parentCompanyName) {
          next[row.parentCompanyId] = row.parentCompanyName;
          changed = true;
        }
      });

      (companySearchData?.data || []).forEach((company) => {
        const id = String(company.id || '').trim();
        const title = String(company.title || '').trim();
        if (!id || !title) return;
        if (next[id] !== title) {
          next[id] = title;
          changed = true;
        }
      });

      return changed ? next : prev;
    });
  }, [companySearchData?.data, rows]);

  const companyFilterOptions = useMemo(() => {
    const search = companyFilterSearch.trim();
    const optionMap = new Map<string, string>();

    rows.forEach((row) => {
      if (row.ownerId && (!search || containsText(row.ownerName, search))) {
        optionMap.set(row.ownerId, row.ownerName || row.ownerId);
      }
      if (row.parentCompanyId && (!search || containsText(row.parentCompanyName, search))) {
        optionMap.set(row.parentCompanyId, row.parentCompanyName || row.parentCompanyId);
      }
    });

    (companySearchData?.data || []).forEach((company) => {
      const id = String(company.id || '').trim();
      const title = String(company.title || id).trim() || id;
      if (id) {
        optionMap.set(id, title);
      }
    });

    selectedCompanyIDs.forEach((companyId) => {
      if (!companyId || optionMap.has(companyId)) return;
      optionMap.set(companyId, companyOptionCache[companyId] || companyId);
    });

    return Array.from(optionMap.entries())
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => compareText(a.label, b.label));
  }, [companyFilterSearch, companyOptionCache, companySearchData?.data, rows, selectedCompanyIDs]);

  const modelFilterOptions = useMemo(() => {
    const optionMap = new Map<string, string>();
    (filterOptionsData?.data?.models || []).forEach((model) => {
      const value = String(model || '').trim();
      if (value) {
        optionMap.set(value, value);
      }
    });
    selectedModels.forEach((model) => {
      const value = String(model || '').trim();
      if (value) {
        optionMap.set(value, value);
      }
    });
    optionMap.delete('-');
    return Array.from(optionMap.entries())
      .map(([value, label]) => ({ text: label, value }))
      .sort((a, b) => compareText(String(a.text), String(b.text)));
  }, [filterOptionsData?.data?.models, selectedModels]);

  const total = data?.pages?.[0]?.meta?.total || 0;

  useEffect(() => {
    const node = loadMoreRef.current;
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
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, rows.length]);

  const renderCompanyLink = useCallback((companyId: string, companyName: string) => {
    if (!companyId) return '-';
    return (
      <Link to={`/companies/${companyId}`} onClick={(event) => event.stopPropagation()}>
        {companyName || companyId}
      </Link>
    );
  }, []);

  const handleTableChange: TableProps<Row>['onChange'] = (_pagination, filters, sorter) => {
    const modelValues = Array.isArray(filters.model) ? filters.model.map(String) : [];
    setSelectedModels(modelValues);

    const activeSorter = (Array.isArray(sorter) ? sorter[0] : sorter) as SorterResult<Row>;
    if (activeSorter?.field === 'fnExpireDate' && activeSorter.order) {
      setSortBy('fn_expire_date');
      setSortOrder(activeSorter.order);
      return;
    }
    setSortBy('');
    setSortOrder(null);
  };

  const handleRangeChange = useCallback((dates: [Dayjs | null, Dayjs | null] | null) => {
    setFnExpireRange([
      dates?.[0]?.format('YYYY-MM-DD') || '',
      dates?.[1]?.format('YYYY-MM-DD') || '',
    ]);
  }, []);

  const ownerColumnTitle = selectedCompanyIDs.length > 0 ? `Владелец (${selectedCompanyIDs.length})` : 'Владелец';

  const columns: ColumnsType<Row> = [
    {
      title: 'Имя ФР',
      dataIndex: 'model',
      key: 'model',
      width: 240,
      filters: modelFilterOptions,
      filteredValue: selectedModels.length > 0 ? (selectedModels as FilterValue) : null,
    },
    { title: 'РНМ', dataIndex: 'rnm', key: 'rnm', width: 180 },
    { title: 'Серийный номер', dataIndex: 'serial', key: 'serial', width: 200 },
    {
      title: 'Срок ФН',
      dataIndex: 'fnExpireDate',
      key: 'fnExpireDate',
      width: 150,
      sorter: true,
      sortOrder: sortBy === 'fn_expire_date' ? sortOrder : null,
      render: (value: string) => formatDate(value),
    },
    { title: 'Юрлицо', dataIndex: 'legalName', key: 'legalName', width: 260 },
    { title: 'Адрес установки', dataIndex: 'address', key: 'address', width: 320 },
    {
      title: ownerColumnTitle,
      dataIndex: 'ownerName',
      key: 'owner',
      width: 280,
      render: (_ownerName: string, record) => renderCompanyLink(record.ownerId, record.ownerName),
    },
  ];

  const filtersPanel = useMemo(
    () => (
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        <Space wrap size={12} style={{ width: '100%', alignItems: 'flex-start' }}>
          <Select
            mode="multiple"
            showSearch
            allowClear
            filterOption={false}
            maxTagCount="responsive"
            placeholder="Владельцы"
            value={selectedCompanyIDs}
            options={companyFilterOptions}
            loading={isCompanySearchLoading}
            style={{ width: 'min(100%, 420px)', minWidth: 260 }}
            onSearch={setCompanyFilterSearch}
            onChange={(nextValue) => setSelectedCompanyIDs(nextValue.map(String))}
          />
          <RangePicker
            allowClear
            placeholder={['ФН от', 'ФН до']}
            value={[
              fnExpireRange[0] ? dayjs(fnExpireRange[0], 'YYYY-MM-DD') : null,
              fnExpireRange[1] ? dayjs(fnExpireRange[1], 'YYYY-MM-DD') : null,
            ]}
            style={{ width: 'min(100%, 280px)' }}
            onChange={(dates) => handleRangeChange(dates as [Dayjs | null, Dayjs | null] | null)}
          />
        </Space>
      </Space>
    ),
    [companyFilterOptions, fnExpireRange, handleRangeChange, isCompanySearchLoading, selectedCompanyIDs],
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Фискальные регистраторы{term ? ` по запросу "${term}"` : ''}
      </Title>
      {filtersPanel}
      <Card className="glass-panel">
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Фискальные регистраторы не найдены" />
        ) : (
          <Table<Row>
            rowKey="id"
            dataSource={rows}
            columns={columns}
            pagination={false}
            scroll={{ x: 'max-content' }}
            showSorterTooltip={false}
            onChange={handleTableChange}
            onRow={(record) => ({
              onClick: () => record.id && navigate(`/fiscals/${record.id}`),
              style: { cursor: 'pointer' },
            })}
          />
        )}

        {!isLoading && rows.length > 0 ? (
          <Space direction="vertical" size={0} style={{ marginTop: 12 }}>
            <Text type="secondary">Найдено по запросу: {total}</Text>
            <Text type="secondary">Загружено: {rows.length}</Text>
          </Space>
        ) : null}

        <div ref={loadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40 }}>
          {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
          {!hasNextPage && rows.length > 0 ? (
            <Text type="secondary">Показано: {rows.length} из {total}</Text>
          ) : null}
        </div>
      </Card>
    </Space>
  );
};

export default FiscalsPage;
