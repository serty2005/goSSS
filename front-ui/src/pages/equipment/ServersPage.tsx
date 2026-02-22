import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, Space, Spin, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { companiesApi } from '@/api/companies';
import { equipmentApi } from '@/api/equipment';
import EntityListToolbar, {
  type EntityListColumnOption,
  type EntityListFilterConfig,
  type EntityListFilterOption,
} from '@/components/equipment/EntityListToolbar';
import { useLayoutHeader } from '@/components/layout/LayoutHeaderContext';

const { Title, Text } = Typography;

type Row = {
  id: string;
  name: string;
  ip: string;
  version: string;
  status: string;
  ownerId: string;
  ownerName: string;
  parentCompanyId: string;
  parentCompanyName: string;
  type: string;
};

type ListViewState = {
  companies: string[];
  statuses: string[];
  types: string[];
  cols: string[];
};

const STORAGE_KEY = 'servers_list_view_state_v1';
const DEFAULT_VISIBLE_COLUMNS = ['name', 'ip', 'owner', 'status'];

const TABLE_COLUMN_OPTIONS: EntityListColumnOption[] = [
  { key: 'name', label: 'Название' },
  { key: 'ip', label: 'IP' },
  { key: 'version', label: 'Версия' },
  { key: 'owner', label: 'Владелец' },
  { key: 'network', label: 'Сеть владельца' },
  { key: 'status', label: 'Статус' },
  { key: 'type', label: 'Тип' },
];

const normalizeText = (value: unknown, fallback = '-') => {
  const clean = String(value ?? '').trim();
  return clean || fallback;
};

const buildTypeValue = (row: Record<string, unknown>) => {
  const raw = row.server_type ?? row.server_edition;
  return normalizeText(raw);
};

const addUniqueValue = (values: string[], value: string) => {
  if (!value || values.includes(value)) {
    return values;
  }
  return [...values, value];
};

const compareText = (a: string, b: string) => a.localeCompare(b, 'ru', { sensitivity: 'base' });
const containsText = (value: string, search: string) =>
  value.toLocaleLowerCase('ru').includes(search.toLocaleLowerCase('ru'));

const sanitizeList = (value: unknown): string[] =>
  Array.isArray(value) ? value.map((item) => String(item).trim()).filter(Boolean) : [];

const readStoredViewState = (): ListViewState => {
  if (typeof window === 'undefined') {
    return { companies: [], statuses: [], types: [], cols: [] };
  }

  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return { companies: [], statuses: [], types: [], cols: [] };
    }

    const parsed = JSON.parse(raw) as Partial<ListViewState>;
    return {
      companies: sanitizeList(parsed.companies),
      statuses: sanitizeList(parsed.statuses),
      types: sanitizeList(parsed.types),
      cols: sanitizeList(parsed.cols),
    };
  } catch {
    return { companies: [], statuses: [], types: [], cols: [] };
  }
};

const ServersPage: React.FC = () => {
  const navigate = useNavigate();
  const { setHeaderAddon } = useLayoutHeader();
  const [searchParams, setSearchParams] = useSearchParams();

  const initialViewState = useMemo(readStoredViewState, []);
  const [selectedCompanyIDs, setSelectedCompanyIDs] = useState<string[]>(initialViewState.companies);
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>(initialViewState.statuses);
  const [selectedTypes, setSelectedTypes] = useState<string[]>(initialViewState.types);
  const [visibleColumnKeys, setVisibleColumnKeys] = useState<string[]>(initialViewState.cols);
  const [companyFilterSearch, setCompanyFilterSearch] = useState('');
  const [companyOptionCache, setCompanyOptionCache] = useState<Record<string, string>>({});

  const term = (searchParams.get('q') || '').trim();
  const activeVisibleColumnKeys = useMemo(() => {
    const allowedKeys = new Set(TABLE_COLUMN_OPTIONS.map((item) => item.key));
    const filtered = visibleColumnKeys.filter((key) => allowedKeys.has(key));
    return filtered.length > 0 ? filtered : DEFAULT_VISIBLE_COLUMNS;
  }, [visibleColumnKeys]);

  useEffect(() => {
    const hasLegacyFiltersInUrl =
      searchParams.has('companies') || searchParams.has('statuses') || searchParams.has('types') || searchParams.has('cols');
    if (!hasLegacyFiltersInUrl) return;

    const next = new URLSearchParams(searchParams);
    next.delete('companies');
    next.delete('statuses');
    next.delete('types');
    next.delete('cols');
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const payload: ListViewState = {
      companies: selectedCompanyIDs,
      statuses: selectedStatuses,
      types: selectedTypes,
      cols: visibleColumnKeys,
    };
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  }, [selectedCompanyIDs, selectedStatuses, selectedTypes, visibleColumnKeys]);

  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const companyFilterKey = useMemo(() => selectedCompanyIDs.slice().sort(), [selectedCompanyIDs]);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: ['equipment', 'servers', term, companyFilterKey],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => equipmentApi.listServers(term, limit, Number(pageParam) || 0, companyFilterKey),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) return undefined;
      return (meta.offset || 0) + (meta.limit || limit);
    },
    staleTime: 30_000,
  });

  const { data: companySearchData, isFetching: isCompanySearchLoading } = useQuery({
    queryKey: ['companies', 'search', 'servers-filter', companyFilterSearch],
    queryFn: () => companiesApi.searchCompanies(companyFilterSearch, 30, 0),
    enabled: companyFilterSearch.trim().length >= 2,
    staleTime: 30_000,
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
            name: normalizeText(row.device_name || row.server_name, id || 'Сервер'),
            ip: normalizeText(row.ip),
            version: normalizeText(row.server_version, '-'),
            status: normalizeText(row.status, 'unknown'),
            ownerId: normalizeText(row.owner_id, ''),
            ownerName: normalizeText(row.owner_title || row.owner_name || row.owner_id, normalizeText(row.owner_id, '-')),
            parentCompanyId: normalizeText(row.owner_parent_id, ''),
            parentCompanyName: normalizeText(row.owner_parent_title || row.parent_company_name, ''),
            type: buildTypeValue(row),
          };
        }),
    [data?.pages],
  );

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

  const companyFilterOptions = useMemo<EntityListFilterOption[]>(() => {
    const search = companyFilterSearch.trim();
    const optionMap = new Map<string, EntityListFilterOption>();

    rows.forEach((row) => {
      const rowOptions: EntityListFilterOption[] = [];
      if (row.ownerId) {
        rowOptions.push({ value: row.ownerId, label: row.ownerName || row.ownerId });
      }
      if (row.parentCompanyId) {
        rowOptions.push({ value: row.parentCompanyId, label: row.parentCompanyName || row.parentCompanyId });
      }

      rowOptions.forEach((option) => {
        if (search && !containsText(option.label, search)) {
          return;
        }
        optionMap.set(option.value, option);
      });
    });

    (companySearchData?.data || []).forEach((company) => {
      const id = String(company.id || '').trim();
      if (!id) return;
      const title = String(company.title || id).trim() || id;
      optionMap.set(id, { value: id, label: title });
    });

    selectedCompanyIDs.forEach((companyId) => {
      if (!companyId || optionMap.has(companyId)) return;
      optionMap.set(companyId, { value: companyId, label: companyOptionCache[companyId] || companyId });
    });

    return Array.from(optionMap.values()).sort((a, b) => compareText(a.label, b.label));
  }, [companyFilterSearch, companyOptionCache, companySearchData?.data, rows, selectedCompanyIDs]);

  const statusFilterOptions = useMemo<EntityListFilterOption[]>(
    () =>
      Array.from(new Set(rows.map((row) => row.status).filter(Boolean)))
        .sort(compareText)
        .map((value) => ({ value, label: value })),
    [rows],
  );

  const typeFilterOptions = useMemo<EntityListFilterOption[]>(
    () =>
      Array.from(new Set(rows.map((row) => row.type).filter(Boolean)))
        .sort(compareText)
        .map((value) => ({ value, label: value })),
    [rows],
  );

  const hasClientFilters = selectedStatuses.length > 0 || selectedTypes.length > 0;
  const filteredRows = useMemo(
    () =>
      rows.filter((row) => {
        if (selectedStatuses.length > 0 && !selectedStatuses.includes(row.status)) return false;
        if (selectedTypes.length > 0 && !selectedTypes.includes(row.type)) return false;
        return true;
      }),
    [rows, selectedStatuses, selectedTypes],
  );

  const total = data?.pages?.[0]?.meta?.total || 0;

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasNextPage || hasClientFilters) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextPage) return;
        void fetchNextPage();
      },
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasClientFilters, hasNextPage, isFetchingNextPage, rows.length]);

  const addCompanyFilter = (companyId: string) => {
    if (!companyId) return;
    setSelectedCompanyIDs((prev) => addUniqueValue(prev, companyId));
  };

  const renderCompanyLink = (companyId: string, companyName: string) => {
    if (!companyId) return '-';
    return (
      <Button
        type="link"
        size="small"
        style={{ paddingInline: 0 }}
        onClick={(event) => {
          event.stopPropagation();
          navigate(`/companies/${companyId}`);
        }}
      >
        {companyName || companyId}
      </Button>
    );
  };

  const allColumns: ColumnsType<Row> = useMemo(
    () => [
      { title: 'Название', dataIndex: 'name', key: 'name' },
      {
        title: 'IP',
        dataIndex: 'ip',
        key: 'ip',
        width: 180,
        sorter: (a, b) => compareText(a.ip, b.ip),
      },
      {
        title: 'Версия',
        dataIndex: 'version',
        key: 'version',
        width: 180,
        sorter: (a, b) => compareText(a.version, b.version),
      },
      {
        title: 'Владелец',
        dataIndex: 'ownerName',
        key: 'owner',
        width: 280,
        sorter: (a, b) => compareText(a.ownerName, b.ownerName),
        onCell: (record) => ({
          style: { cursor: record.ownerId ? 'pointer' : undefined },
          onClick: (event) => {
            if (!record.ownerId) return;
            event.stopPropagation();
            addCompanyFilter(record.ownerId);
          },
        }),
        render: (_value: string, record) => renderCompanyLink(record.ownerId, record.ownerName),
      },
      {
        title: 'Сеть владельца',
        dataIndex: 'parentCompanyName',
        key: 'network',
        width: 260,
        sorter: (a, b) => compareText(a.parentCompanyName, b.parentCompanyName),
        onCell: (record) => ({
          style: { cursor: record.parentCompanyId ? 'pointer' : undefined },
          onClick: (event) => {
            if (!record.parentCompanyId) return;
            event.stopPropagation();
            addCompanyFilter(record.parentCompanyId);
          },
        }),
        render: (_value: string, record) => renderCompanyLink(record.parentCompanyId, record.parentCompanyName),
      },
      {
        title: 'Статус',
        dataIndex: 'status',
        key: 'status',
        width: 140,
        sorter: (a, b) => compareText(a.status, b.status),
        render: (status: string) => <Tag color={status === 'active' ? 'success' : 'default'}>{status}</Tag>,
      },
      {
        title: 'Тип',
        dataIndex: 'type',
        key: 'type',
        width: 220,
        sorter: (a, b) => compareText(a.type, b.type),
        render: (value: string) => value || '-',
      },
    ],
    [navigate, selectedCompanyIDs],
  );

  const columns = useMemo(() => {
    const allowed = new Set(activeVisibleColumnKeys);
    return allColumns.filter((column) => allowed.has(String(column.key || '')));
  }, [activeVisibleColumnKeys, allColumns]);

  const filters = useMemo<EntityListFilterConfig[]>(
    () => [
      {
        key: 'companies',
        label: 'Компания',
        placeholder: 'Владельцы и сети',
        value: selectedCompanyIDs,
        options: companyFilterOptions,
        loading: isCompanySearchLoading,
        filterOption: false,
        onSearch: setCompanyFilterSearch,
        maxTagCount: undefined,
        style: { width: '100%', minWidth: 520, flex: '1 1 520px' },
        onChange: setSelectedCompanyIDs,
      },
      {
        key: 'statuses',
        label: 'Статус',
        placeholder: 'Статусы',
        value: selectedStatuses,
        options: statusFilterOptions,
        maxTagCount: 'responsive',
        style: { minWidth: 220, flex: '1 1 220px' },
        onChange: setSelectedStatuses,
      },
      {
        key: 'types',
        label: 'Тип',
        placeholder: 'Типы серверов',
        value: selectedTypes,
        options: typeFilterOptions,
        maxTagCount: 'responsive',
        style: { minWidth: 260, flex: '1 1 260px' },
        onChange: setSelectedTypes,
      },
    ],
    [companyFilterOptions, isCompanySearchLoading, selectedCompanyIDs, selectedStatuses, selectedTypes, statusFilterOptions, typeFilterOptions],
  );

  const headerAddon = useMemo(
    () => (
      <EntityListToolbar
        showSearch={false}
        searchValue=""
        onSearchValueChange={() => undefined}
        onSearchSubmit={() => undefined}
        filters={filters}
        columnOptions={TABLE_COLUMN_OPTIONS}
        selectedColumnKeys={activeVisibleColumnKeys}
        onSelectedColumnKeysChange={(keys) => setVisibleColumnKeys(keys.length > 0 ? keys : DEFAULT_VISIBLE_COLUMNS)}
        filterRows={[['companies'], ['statuses', 'types']]}
        columnsButtonPlacement="lastFilterRow"
      />
    ),
    [activeVisibleColumnKeys, filters],
  );

  useEffect(() => {
    setHeaderAddon(headerAddon);
    return () => setHeaderAddon(null);
  }, [headerAddon, setHeaderAddon]);

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Серверы{term ? ` по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        <div>
          {isLoading ? (
            <div style={{ textAlign: 'center', padding: 32 }}>
              <Spin />
            </div>
          ) : rows.length === 0 ? (
            <Empty description="Серверы не найдены" />
          ) : filteredRows.length === 0 ? (
            <Empty description="По выбранным фильтрам серверы не найдены" />
          ) : (
            <Table<Row>
              rowKey="id"
              dataSource={filteredRows}
              columns={columns}
              pagination={false}
              showSorterTooltip={false}
              onRow={(record) => ({
                onClick: () => record.id && navigate(`/servers/${record.id}`),
                style: { cursor: 'pointer' },
              })}
            />
          )}
        </div>

        {!isLoading && rows.length > 0 && (
          <Space direction="vertical" size={0} style={{ marginTop: 12 }}>
            <Text type="secondary">Найдено по запросу: {total}</Text>
            <Text type="secondary">Загружено: {rows.length}, после фильтров: {filteredRows.length}</Text>
            {hasClientFilters && hasNextPage ? (
              <Text type="warning">Автоподгрузка при активных фильтрах отключена</Text>
            ) : null}
          </Space>
        )}

        <div ref={loadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40, gap: 8 }}>
          {(isFetchingNextPage || (!hasClientFilters && hasNextPage && rows.length > 0)) && <Spin size="small" />}
          {hasClientFilters && hasNextPage && rows.length > 0 && !isFetchingNextPage ? (
            <Button size="small" onClick={() => void fetchNextPage()}>
              Загрузить ещё
            </Button>
          ) : null}
          {!hasNextPage && rows.length > 0 ? (
            <Text type="secondary">Показано: {rows.length} из {total}</Text>
          ) : null}
        </div>
      </Card>
    </Space>
  );
};

export default ServersPage;
