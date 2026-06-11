import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Card, Checkbox, DatePicker, Select, Space, Spin, Typography } from 'antd';
import dayjs from 'dayjs';
import type { Dayjs } from 'dayjs';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import DataTable, {
  DataTableColumn,
  DataTableSortState,
  DataTableTextCell,
} from '@/components/common/DataTable';
import { createDataTableColumnMinWidth, formatDataTableText } from '@/components/common/dataTableUtils';

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
  ofdName: string;
  licenses: unknown;
  licensesLabel: string;
  frFirmware: string;
  driverVersion: string;
};

type ListViewState = {
  companies: string[];
  models: string[];
  fnExpireFrom: string;
  fnExpireTo: string;
};

const STORAGE_KEY = 'fiscals_list_view_state_v1';
const FISCALS_TABLE_COLUMN_KEYS = [
  'model',
  'rnm',
  'serial',
  'fnExpireDate',
  'ofdName',
  'licensesLabel',
  'frFirmware',
  'driverVersion',
  'legalName',
  'address',
  'owner',
] as const;

const normalizeText = (value: unknown, fallback = '-') => {
  const clean = formatDataTableText(value);
  return clean || fallback;
};

const compareText = (a: string, b: string) => a.localeCompare(b, 'ru', {
  numeric: true,
  sensitivity: 'base',
});

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

const getDateSortValue = (value: string, sortState: DataTableSortState) => {
  const parsed = dayjs(value);
  if (parsed.isValid()) {
    return parsed.valueOf();
  }
  return sortState?.order === 'desc' ? Number.MIN_SAFE_INTEGER : Number.MAX_SAFE_INTEGER;
};

const formatLicenseDate = (value: unknown) => {
  const text = normalizeText(value, '');
  if (!text) return '';
  const parsed = dayjs(text);
  return parsed.isValid() ? parsed.format('DD.MM.YYYY') : text;
};

const formatLicenses = (value: unknown) => {
  if (!value) return '';
  if (typeof value === 'string') {
    return normalizeText(value, '');
  }
  if (typeof value !== 'object' || Array.isArray(value)) {
    return normalizeText(value, '');
  }

  return Object.entries(value as Record<string, unknown>)
    .map(([licenseID, rawLicense]) => {
      if (!rawLicense || typeof rawLicense !== 'object' || Array.isArray(rawLicense)) {
        return `${licenseID}: ${normalizeText(rawLicense, '')}`.trim();
      }

      const license = rawLicense as Record<string, unknown>;
      const name = normalizeText(license.name, '');
      const dateUntil = formatLicenseDate(license.date_until);
      const suffix = dateUntil ? `до ${dateUntil}` : '';
      return [licenseID, name, suffix].filter(Boolean).join(' ');
    })
    .filter(Boolean)
    .sort(compareText)
    .join('\n');
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
  const [tableSort, setTableSort] = useState<DataTableSortState>(null);

  const term = (searchParams.get('q') || '').trim();
  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  const companyFilterKey = useMemo(() => selectedCompanyIDs.slice().sort(), [selectedCompanyIDs]);
  const modelFilterKey = useMemo(() => selectedModels.slice().sort(), [selectedModels]);
  const sortBy = tableSort?.key === 'fnExpireDate' ? 'fn_expire_date' : '';
  const sortOrder = tableSort?.order === 'asc'
    ? 'ascend'
    : tableSort?.order === 'desc'
      ? 'descend'
      : undefined;

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: [
      'equipment',
      'fiscals',
      term,
      companyFilterKey,
      modelFilterKey,
      fnExpireRange,
      sortBy,
      sortOrder,
    ],
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
        sortOrder,
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

  const { data: filterOptionsData, isFetching: isFilterOptionsLoading } = useQuery({
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
          const licenses = row.licenses;
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
            ofdName: normalizeText(row.ofd_name),
            licenses,
            licensesLabel: formatLicenses(licenses),
            frFirmware: normalizeText(row.fr_firmware),
            driverVersion: normalizeText(row.driver_version),
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
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => compareText(a.label, b.label));
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

  const handleRangeChange = useCallback((dates: [Dayjs | null, Dayjs | null] | null) => {
    setFnExpireRange([
      dates?.[0]?.format('YYYY-MM-DD') || '',
      dates?.[1]?.format('YYYY-MM-DD') || '',
    ]);
  }, []);

  const handleSortChange = useCallback((columnKey: string) => {
    setTableSort((currentSort) => {
      if (currentSort?.key !== columnKey) {
        return { key: columnKey, order: 'asc' };
      }
      if (currentSort.order === 'asc') {
        return { key: columnKey, order: 'desc' };
      }
      return null;
    });
  }, []);

  const modelFilterContent = useMemo(() => (
    <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
      <Text strong>Модель ФР</Text>
      <Checkbox checked={selectedModels.length === 0} onChange={() => setSelectedModels([])}>
        Все
      </Checkbox>
      <Checkbox.Group
        value={selectedModels}
        onChange={(nextValues) => setSelectedModels(nextValues.map((value) => String(value)))}
      >
        <Space direction="vertical" size={4} style={{ maxHeight: 260, overflowY: 'auto' }}>
          {modelFilterOptions.map((option) => (
            <Checkbox key={option.value} value={option.value}>
              {option.label}
            </Checkbox>
          ))}
          {isFilterOptionsLoading && <Spin size="small" />}
        </Space>
      </Checkbox.Group>
    </Space>
  ), [isFilterOptionsLoading, modelFilterOptions, selectedModels]);

  const ownerFilterContent = useMemo(() => (
    <Space direction="vertical" size={8} className="company-ticket-table__filter-popover" style={{ width: 340 }}>
      <Text strong>Владелец</Text>
      <Select
        mode="multiple"
        showSearch
        allowClear
        filterOption={false}
        maxTagCount="responsive"
        placeholder="Начните вводить название"
        value={selectedCompanyIDs}
        options={companyFilterOptions}
        loading={isCompanySearchLoading}
        style={{ width: '100%' }}
        onSearch={setCompanyFilterSearch}
        onChange={(nextValue) => setSelectedCompanyIDs(nextValue.map(String))}
      />
      <Text type="secondary">Поиск владельцев выполняется отдельным запросом по компаниям.</Text>
    </Space>
  ), [companyFilterOptions, isCompanySearchLoading, selectedCompanyIDs]);

  const fnExpireFilterContent = useMemo(() => (
    <Space direction="vertical" size={8} className="company-ticket-table__filter-popover">
      <Text strong>Срок ФН</Text>
      <RangePicker
        allowClear
        format="DD.MM.YYYY"
        placeholder={['ФН от', 'ФН до']}
        value={[
          fnExpireRange[0] ? dayjs(fnExpireRange[0], 'YYYY-MM-DD') : null,
          fnExpireRange[1] ? dayjs(fnExpireRange[1], 'YYYY-MM-DD') : null,
        ]}
        onChange={(dates) => handleRangeChange(dates as [Dayjs | null, Dayjs | null] | null)}
      />
    </Space>
  ), [fnExpireRange, handleRangeChange]);

  const ownerColumnTitle = selectedCompanyIDs.length > 0 ? `Владелец (${selectedCompanyIDs.length})` : 'Владелец';

  const columns = useMemo<DataTableColumn<Row>[]>(() => [
    {
      title: 'Имя ФР',
      dataIndex: 'model',
      key: 'model',
      width: 240,
      minWidth: createDataTableColumnMinWidth('Имя ФР'),
      maxWidth: 420,
      isSortable: false,
      filterContent: modelFilterContent,
      isFilterActive: selectedModels.length > 0,
    },
    {
      title: 'РНМ',
      dataIndex: 'rnm',
      key: 'rnm',
      width: 170,
      minWidth: createDataTableColumnMinWidth('РНМ'),
      maxWidth: 260,
      isSortable: false,
    },
    {
      title: 'Серийный номер',
      dataIndex: 'serial',
      key: 'serial',
      width: 190,
      minWidth: createDataTableColumnMinWidth('Серийный номер'),
      maxWidth: 320,
      isSortable: false,
    },
    {
      title: 'Срок ФН',
      dataIndex: 'fnExpireDate',
      key: 'fnExpireDate',
      width: 150,
      minWidth: createDataTableColumnMinWidth('Срок ФН'),
      maxWidth: 220,
      filterContent: fnExpireFilterContent,
      isFilterActive: Boolean(fnExpireRange[0] || fnExpireRange[1]),
      sortValue: (row) => getDateSortValue(row.fnExpireDate, tableSort),
      render: (value?: string) => <DataTableTextCell value={formatDate(value || '')} />,
    },
    {
      title: 'Название ОФД',
      dataIndex: 'ofdName',
      key: 'ofdName',
      width: 220,
      minWidth: createDataTableColumnMinWidth('Название ОФД'),
      maxWidth: 420,
      isSortable: false,
    },
    {
      title: 'Лицензии ФР',
      dataIndex: 'licensesLabel',
      key: 'licensesLabel',
      width: 260,
      minWidth: createDataTableColumnMinWidth('Лицензии ФР'),
      maxWidth: 520,
      isSortable: false,
      render: (value?: string) => <DataTableTextCell value={value} multiline />,
    },
    {
      title: 'Версия прошивки',
      dataIndex: 'frFirmware',
      key: 'frFirmware',
      width: 180,
      minWidth: createDataTableColumnMinWidth('Версия прошивки'),
      maxWidth: 280,
      isSortable: false,
    },
    {
      title: 'Версия драйвера',
      dataIndex: 'driverVersion',
      key: 'driverVersion',
      width: 180,
      minWidth: createDataTableColumnMinWidth('Версия драйвера'),
      maxWidth: 280,
      isSortable: false,
    },
    {
      title: 'Юрлицо',
      dataIndex: 'legalName',
      key: 'legalName',
      width: 260,
      minWidth: createDataTableColumnMinWidth('Юрлицо'),
      maxWidth: 520,
      isSortable: false,
    },
    {
      title: 'Адрес установки',
      dataIndex: 'address',
      key: 'address',
      width: 320,
      minWidth: createDataTableColumnMinWidth('Адрес установки'),
      maxWidth: 640,
      isSortable: false,
    },
    {
      title: ownerColumnTitle,
      dataIndex: 'ownerName',
      key: 'owner',
      width: 280,
      minWidth: createDataTableColumnMinWidth('Владелец'),
      maxWidth: 460,
      isSortable: false,
      filterContent: ownerFilterContent,
      isFilterActive: selectedCompanyIDs.length > 0,
      render: (_ownerName: string, record) => renderCompanyLink(record.ownerId, record.ownerName),
    },
  ], [
    fnExpireFilterContent,
    fnExpireRange,
    modelFilterContent,
    ownerColumnTitle,
    ownerFilterContent,
    renderCompanyLink,
    selectedCompanyIDs.length,
    selectedModels.length,
    tableSort,
  ]);

  const tableFooter = (
    <Space direction="vertical" size={8} style={{ width: '100%', marginTop: 12 }}>
      {!isLoading && rows.length > 0 ? (
        <Space wrap size={12}>
          <Text type="secondary">Найдено по запросу: {total}</Text>
          <Text type="secondary">Загружено: {rows.length}</Text>
        </Space>
      ) : null}
      <div ref={loadMoreRef} style={{ display: 'flex', justifyContent: 'center', minHeight: 40 }}>
        {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
        {!hasNextPage && rows.length > 0 ? (
          <Text type="secondary">Показано: {rows.length} из {total}</Text>
        ) : null}
      </div>
    </Space>
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Фискальные регистраторы{term ? ` по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        <DataTable<Row>
          rowKey="id"
          dataSource={rows}
          columns={columns}
          loading={isLoading}
          pagination={false}
          size="small"
          layoutKey="fiscals_table_layout"
          layoutStorage="local"
          visibilityStorageKey="fiscals_table_visible_columns"
          availableColumnKeys={[...FISCALS_TABLE_COLUMN_KEYS]}
          defaultVisibleColumnKeys={[...FISCALS_TABLE_COLUMN_KEYS]}
          sortState={tableSort}
          onSortChange={handleSortChange}
          sortableColumnKeys={['fnExpireDate']}
          emptyText="Фискальные регистраторы не найдены"
          footer={tableFooter}
          onRow={(record) => ({
            onClick: () => record.id && navigate(`/fiscals/${record.id}`),
            style: { cursor: 'pointer' },
          })}
        />
      </Card>
    </Space>
  );
};

export default FiscalsPage;
