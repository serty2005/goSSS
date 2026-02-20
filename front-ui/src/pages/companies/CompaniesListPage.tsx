import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Card, Empty, List, message, Select, Space, Spin, Table, Tag, Tabs, Tooltip, Typography } from 'antd';
import { BankOutlined, CheckOutlined, CloseOutlined, SwapOutlined } from '@ant-design/icons';
import { companiesApi } from '@/api/companies';
import { ticketsApi } from '@/api/tickets';
import { CompanyBitrixMappingRowDTO, CompanyModel } from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import { cancelDraft, createInitialDraft, isDraftDirty, MappingDraft, toggleDirection } from './companyBitrixMappingState';

const { Title, Text } = Typography;

type CompanyOption = { value: string; label: string };

const CompaniesListPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const isAdminUser = isAdmin(user?.roles);

  const [companyLookupTerm, setCompanyLookupTerm] = useState('');
  const [servicePointLookupTerm, setServicePointLookupTerm] = useState('');
  const [drafts, setDrafts] = useState<Record<string, MappingDraft>>({});
  const companiesLimit = 20;
  const companiesLoadMoreRef = useRef<HTMLDivElement | null>(null);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: ['companies', 'list', term],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => companiesApi.searchCompanies(term, companiesLimit, Number(pageParam) || 0),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || companiesLimit);
    },
    staleTime: 30_000,
  });

  const { data: mappingsData, isLoading: isMappingsLoading } = useQuery({
    queryKey: ['companies', 'bitrix-mappings', term],
    queryFn: () => companiesApi.getBitrixMappings(term, 200, 0),
    enabled: isAdminUser && isBitrixEnabled,
    staleTime: 15_000,
  });

  const { data: pointsData = [] } = useQuery({
    queryKey: ['bitrix-service-points', 'for-company-mappings', servicePointLookupTerm],
    queryFn: () => ticketsApi.getBitrixServicePoints({
      term: servicePointLookupTerm,
      limit: 20,
      offset: 0,
      random_if_empty: true,
    }),
    enabled: isAdminUser && isBitrixEnabled,
    staleTime: 15_000,
  });

  const { data: companiesLookupData } = useQuery({
    queryKey: ['companies', 'lookup', companyLookupTerm],
    queryFn: () => companiesApi.searchCompanies(companyLookupTerm, 30, 0),
    enabled: isAdminUser,
    staleTime: 30_000,
  });

  useEffect(() => {
    if (!mappingsData?.data) {
      return;
    }
    const next: Record<string, MappingDraft> = {};
    for (const row of mappingsData.data) {
      next[row.company_id] = createInitialDraft(row);
    }
    setDrafts(next);
  }, [mappingsData?.data]);

  const companyLookupOptions = useMemo<CompanyOption[]>(() => {
    const items = companiesLookupData?.data || [];
    return items
      .map((item) => {
        const id = resolveCompanyID(item as CompanyModel);
        if (!id) return null;
        const title = resolveCompanyTitle(item as CompanyModel) || id;
        const parentTitle = resolveCompanyParentTitle(item as CompanyModel);
        const label = parentTitle ? `${title} (${parentTitle})` : title;
        return { value: id, label };
      })
      .filter(Boolean) as CompanyOption[];
  }, [companiesLookupData?.data]);

  const servicePointOptions = useMemo(() => {
    return (pointsData || []).map((point) => {
      const code = point.one_c_code ? ` · код 1С: ${point.one_c_code}` : '';
      const suffix = point.address ? ` · ${point.address}` : '';
      const status = point.contract_on == null ? '' : point.contract_on ? ' · контракт: активен' : ' · контракт: нет';
      return {
        value: point.b24_element_id,
        label: `${point.name}${code}${suffix}${status}`,
      };
    });
  }, [pointsData]);

  const updateMutation = useMutation({
    mutationFn: (payload: { company_id?: string; bitrix_service_point_id?: number }) => companiesApi.updateBitrixMapping(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['companies', 'bitrix-mappings'] });
      message.success('Сопоставление обновлено');
    },
    onError: (error: any) => {
      const apiMessage = error?.response?.data?.error?.error;
      message.error(apiMessage || 'Не удалось обновить сопоставление');
    },
  });

  const applyDraft = async (row: CompanyBitrixMappingRowDTO) => {
    const draft = drafts[row.company_id];
    if (!draft) return;

    if (draft.direction === 'company_to_point') {
      await updateMutation.mutateAsync({
        company_id: row.company_id,
        bitrix_service_point_id: draft.pointId,
      });
      return;
    }

    await updateMutation.mutateAsync({
      company_id: draft.companyId,
      bitrix_service_point_id: draft.pointId,
    });
  };

  const mappings = mappingsData?.data || [];
  const companies = useMemo(() => (data?.pages || []).flatMap((pageData) => pageData.data || []), [data?.pages]);
  const companiesTotal = data?.pages?.[0]?.meta?.total || 0;

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

  const columns = [
    {
      title: 'Наша компания',
      dataIndex: 'company_title',
      key: 'company',
      width: '42%',
      render: (_: unknown, row: CompanyBitrixMappingRowDTO) => {
        const draft = drafts[row.company_id];
        const isPointToCompany = draft?.direction === 'point_to_company';
        if (isPointToCompany) {
          const currentValue = draft.companyId;
          const fallbackLabel = row.company_parent_title ? `${row.company_title} (${row.company_parent_title})` : row.company_title;
          const options = currentValue && !companyLookupOptions.some((item) => item.value === currentValue)
            ? [{ value: currentValue, label: fallbackLabel }, ...companyLookupOptions]
            : companyLookupOptions;
          return (
            <Select
              allowClear
              showSearch
              value={currentValue}
              options={options}
              onSearch={setCompanyLookupTerm}
              onChange={(value) => {
                setDrafts((prev) => ({
                  ...prev,
                  [row.company_id]: {
                    ...prev[row.company_id],
                    companyId: value || undefined,
                  },
                }));
              }}
              style={{ width: '100%' }}
              placeholder="Выберите компанию"
              filterOption={(input, option) => String(option?.label || '').toLowerCase().includes(input.toLowerCase())}
            />
          );
        }

        return (
          <Space direction="vertical" size={0}>
            <Text strong>{row.company_title}</Text>
            {row.company_parent_title && <Text type="secondary">Сеть: {row.company_parent_title}</Text>}
          </Space>
        );
      },
    },
    {
      title: '',
      key: 'direction',
      width: 90,
      render: (_: unknown, row: CompanyBitrixMappingRowDTO) => {
        const draft = drafts[row.company_id];
        const disabled = !draft?.pointId;
        return (
          <Tooltip title={disabled ? 'Переключение доступно только для сопоставленной точки B24' : 'Сменить направление сопоставления'}>
            <Button
              type={draft?.direction === 'point_to_company' ? 'primary' : 'default'}
              icon={<SwapOutlined />}
              disabled={disabled}
              onClick={() => {
                setDrafts((prev) => ({
                  ...prev,
                  [row.company_id]: toggleDirection(prev[row.company_id]),
                }));
              }}
            />
          </Tooltip>
        );
      },
    },
    {
      title: 'Точка обслуживания Bitrix24',
      dataIndex: 'bitrix_service_point_name',
      key: 'point',
      width: '46%',
      render: (_: unknown, row: CompanyBitrixMappingRowDTO) => {
        const draft = drafts[row.company_id];
        const isCompanyToPoint = draft?.direction !== 'point_to_company';
        if (isCompanyToPoint) {
          return (
            <Select
              allowClear
              showSearch
              value={draft?.pointId}
              options={servicePointOptions}
              onSearch={setServicePointLookupTerm}
              onChange={(value) => {
                setDrafts((prev) => ({
                  ...prev,
                  [row.company_id]: {
                    ...prev[row.company_id],
                    pointId: value,
                  },
                }));
              }}
              style={{ width: '50%' }}
              placeholder="Выберите точку B24"
              filterOption={false}
            />
          );
        }

        if (!draft?.pointId) {
          return <Text type="secondary">Точка B24 не выбрана</Text>;
        }

        const item = servicePointOptions.find((option) => option.value === draft.pointId);
        return <Text>{item?.label || `ID: ${draft.pointId}`}</Text>;
      },
    },
    {
      title: 'Действия',
      key: 'actions',
      width: 110,
      render: (_: unknown, row: CompanyBitrixMappingRowDTO) => {
        const draft = drafts[row.company_id];
        if (!draft || !isDraftDirty(draft)) {
          return null;
        }
        return (
          <Space>
            <Tooltip title="Применить">
              <Button
                type="primary"
                icon={<CheckOutlined />}
                loading={updateMutation.isPending}
                onClick={() => void applyDraft(row)}
              />
            </Tooltip>
            <Tooltip title="Отмена">
              <Button
                icon={<CloseOutlined />}
                onClick={() => {
                  setDrafts((prev) => ({
                    ...prev,
                    [row.company_id]: cancelDraft(prev[row.company_id]),
                  }));
                }}
              />
            </Tooltip>
          </Space>
        );
      },
    },
  ];

  if (isLoading) {
    return <div style={{ textAlign: 'center', padding: 40 }}><Spin size="large" /></div>;
  }

  const listContent = (
    <Card className="glass-panel">
      {companies.length === 0 ? (
        <Empty description="Компании не найдены" />
      ) : (
        <>
          <List
            dataSource={companies}
            renderItem={(item) => {
              const company = item as CompanyModel;
              const id = resolveCompanyID(company);
              const title = resolveCompanyTitle(company) || id;
              const parentTitle = resolveCompanyParentTitle(company);
              const address = company.address;
              const additional = company.additional_name;
              const is_active = company.active_contract === true;

              return (
                <List.Item key={id || title}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space size={8}>
                      <BankOutlined />
                      {id ? <Link to={`/companies/${id}`}>{title}</Link> : <Text strong>{title}</Text>}
                      <Tag color={is_active ? 'success' : 'default'}>{is_active ? 'Активен' : 'Завершён'}</Tag>
                    </Space>
                    {parentTitle && <Text type="secondary">Группа: {parentTitle}</Text>}
                    {additional && <Text type="secondary">Юр. название: {additional}</Text>}
                    {address && <Text type="secondary">{address}</Text>}
                  </Space>
                </List.Item>
              );
            }}
          />
          <div ref={companiesLoadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40 }}>
            {(isFetchingNextPage || (hasNextPage && companies.length > 0)) && <Spin size="small" />}
            {!hasNextPage && companies.length > 0 && (
              <Text type="secondary">Показано: {companies.length} из {companiesTotal}</Text>
            )}
          </div>
        </>
      )}
    </Card>
  );

  const mappingContent = (
    <Card className="glass-panel">
      {isMappingsLoading ? (
        <div style={{ textAlign: 'center', padding: 24 }}><Spin /></div>
      ) : mappings.length === 0 ? (
        <Empty description="Сопоставления не найдены" />
      ) : (
        <Table
          rowKey="company_id"
          columns={columns}
          dataSource={mappings}
          pagination={{ pageSize: 20, hideOnSinglePage: true }}
          size="small"
        />
      )}
    </Card>
  );

  const tabItems = [
    { key: 'companies', label: 'Компании', children: listContent },
  ];

  if (isAdminUser && isBitrixEnabled) {
    tabItems.push({ key: 'mappings', label: 'Сопоставление с B24', children: mappingContent });
  }

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Компании {term ? `по запросу "${term}"` : ''}
      </Title>

      <Tabs defaultActiveKey="companies" items={tabItems} />
    </Space>
  );
};

export default CompaniesListPage;
