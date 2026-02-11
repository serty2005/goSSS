import React, { useEffect, useMemo, useState } from 'react';
import { Button, Dropdown, Grid, Input, Select, Space, Switch, Typography } from 'antd';
import { FilterOutlined, PlusOutlined } from '@ant-design/icons';
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ticketsApi } from '@/api/tickets';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';

const { useBreakpoint } = Grid;
const { Text } = Typography;

const HeaderSearch: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const currentTerm = searchParams.get('term') || '';
  const showInactive = ['1', 'true', 'yes', 'on'].includes((searchParams.get('show_inactive') || '').toLowerCase());
  const [searchTerm, setSearchTerm] = useState(currentTerm);

  useEffect(() => {
    setSearchTerm(currentTerm);
  }, [currentTerm]);

  const onGlobalSearch = (value: string) => {
    const trimmed = value.trim();
    if (!trimmed) return;
    const params = new URLSearchParams();
    params.set('term', trimmed);
    if (showInactive) {
      params.set('show_inactive', '1');
    }
    navigate(`/search?${params.toString()}`);
  };

  const onToggleShowInactive = (nextValue: boolean) => {
    const params = new URLSearchParams(searchParams);
    if (nextValue) {
      params.set('show_inactive', '1');
    } else {
      params.delete('show_inactive');
    }
    if (currentTerm || searchTerm) {
      params.set('term', (searchTerm || currentTerm).trim());
      navigate(`/search?${params.toString()}`);
    } else if (location.pathname.startsWith('/search')) {
      navigate(`/search?${params.toString()}`);
    }
  };

  const isTicketsPage = location.pathname.startsWith('/tickets');
  const isCompaniesPage = location.pathname.startsWith('/companies');
  const isServersPage = location.pathname === '/servers';
  const isWorkstationsPage = location.pathname === '/workstations';
  const isFiscalsPage = location.pathname === '/fiscals';
  const isSectionSearchPage = isCompaniesPage || isServersPage || isWorkstationsPage || isFiscalsPage;
  const screens = useBreakpoint();
  const isCompact = !screens.xl;

  const [ticketParams, setTicketParams] = useSearchParams();
  const [ticketTerm, setTicketTerm] = useState(ticketParams.get('q') || '');
  const appliedSearch = ticketParams.get('q') || '';
  const ticketStatus = ticketParams.get('status') || '';
  const ticketCompany = ticketParams.get('company') || undefined;
  const ticketView = ticketParams.get('view') || 'list';

  useEffect(() => {
    setTicketTerm(ticketParams.get('q') || '');
  }, [ticketParams]);

  const statusValues = useMemo(() => (ticketStatus ? ticketStatus.split(',').filter(Boolean) : []), [ticketStatus]);

  const { data: filterRes, isFetching: isFiltersLoading } = useQuery({
    queryKey: ['ticket-filters', appliedSearch, statusValues],
    queryFn: () =>
      ticketsApi.getTicketFilters({
        search: appliedSearch || undefined,
        status: statusValues.length ? statusValues : undefined,
      }),
    staleTime: 30_000,
  });

  const companyOptions = useMemo(() => {
    const list = filterRes?.data?.companies || [];
    const renderLabel = (title: string, parentTitle?: string) => {
      const parts = getCompanyHierarchyParts(title, parentTitle);
      if (!parts.hasParent) {
        return parts.child;
      }
      return (
        <Space direction="vertical" size={0} style={{ lineHeight: 1.2 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>{parts.parent}</Text>
          <Text style={{ paddingLeft: 14 }}>{parts.child}</Text>
        </Space>
      );
    };
    return list.map((company) => ({
      value: company.id,
      selectedLabel: company.name || company.id,
      label: (
        <Space style={{ width: '100%', justifyContent: 'space-between' }}>
          {renderLabel(company.name || company.id, company.parent_name)}
          <Text type="secondary">({company.count})</Text>
        </Space>
      ),
      searchText: `${company.parent_name || ''} ${company.name || company.id}`.trim().toLowerCase(),
    }));
  }, [filterRes]);

  const updateTicketParams = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(ticketParams);
    Object.entries(next).forEach(([key, value]) => {
      if (!value) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    params.set('page', '1');
    setTicketParams(params);
  };

  const [sectionParams] = useSearchParams();
  const sectionTerm = sectionParams.get('q') || '';
  const [sectionSearchTerm, setSectionSearchTerm] = useState(sectionTerm);

  useEffect(() => {
    if (!isSectionSearchPage) return;
    setSectionSearchTerm(sectionTerm);
  }, [isSectionSearchPage, sectionTerm]);

  const sectionPlaceholder = (() => {
    if (isCompaniesPage) return 'Поиск компаний: название, адрес, юр. название';
    if (isServersPage) return 'Поиск серверов: id, ip, название';
    if (isWorkstationsPage) return 'Поиск РС: id, название';
    if (isFiscalsPage) return 'Поиск ФР: id, модель, РНМ';
    return 'Поиск...';
  })();

  const onSectionSearch = (value: string) => {
    const trimmed = value.trim();
    const params = new URLSearchParams(sectionParams);
    if (!trimmed) {
      params.delete('q');
    } else {
      params.set('q', trimmed);
    }
    params.delete('page');
    const query = params.toString();
    navigate(query ? `${location.pathname}?${query}` : location.pathname);
  };

  if (isTicketsPage) {
    const controls = (
      <Space size="small" wrap style={{ justifyContent: 'center' }}>
        <Select
          value={ticketView}
          onChange={(value) => updateTicketParams({ view: value })}
          style={{ width: 130 }}
          options={[
            { value: 'list', label: 'Список' },
            { value: 'cards', label: 'Карточки' },
            { value: 'table', label: 'Таблица' },
          ]}
        />
        <Input.Search
          placeholder="Поиск по заявкам..."
          allowClear
          value={ticketTerm}
          onChange={(event) => setTicketTerm(event.target.value)}
          onSearch={(value) => updateTicketParams({ q: value.trim() || undefined })}
          style={{ width: 260 }}
        />
        <Select
          mode="multiple"
          placeholder="Статусы"
          value={statusValues}
          onChange={(values) => updateTicketParams({ status: values.length ? values.join(',') : undefined })}
          style={{ width: 220 }}
          options={[
            { value: 'new', label: 'Новая' },
            { value: 'in_progress', label: 'В работе' },
            { value: 'pending', label: 'Ожидание' },
            { value: 'resolved', label: 'Решена' },
            { value: 'closed', label: 'Закрыта' },
          ]}
        />
        <Select
          showSearch
          allowClear
          placeholder="Компания-владелец"
          value={ticketCompany}
          onChange={(value) => updateTicketParams({ company: value || undefined })}
          filterOption={(input, option) =>
            String((option as { searchText?: string } | undefined)?.searchText || '').includes(input.toLowerCase())
          }
          options={companyOptions}
          loading={isFiltersLoading}
          style={{ width: 260 }}
          optionLabelProp="selectedLabel"
        />
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => updateTicketParams({ create: '1' })}
        >
          Новая заявка
        </Button>
      </Space>
    );

    if (isCompact) {
      return (
        <Dropdown
          trigger={['click']}
          placement="bottom"
          popupRender={() => (
            <div style={{ padding: 12, width: 320 }}>
              <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                {controls}
              </Space>
            </div>
          )}
        >
          <Button icon={<FilterOutlined />}>Поиск и фильтры</Button>
        </Dropdown>
      );
    }

    return controls;
  }

  if (isSectionSearchPage) {
    return (
      <Input.Search
        placeholder={sectionPlaceholder}
        allowClear
        value={sectionSearchTerm}
        onChange={(event) => setSectionSearchTerm(event.target.value)}
        onSearch={onSectionSearch}
        style={{ width: 440, maxWidth: '100%' }}
      />
    );
  }

  return (
    <Space size="small">
      <Input.Search
        placeholder="Поиск по IP, Serial, Name..."
        allowClear
        value={searchTerm}
        onChange={(event) => setSearchTerm(event.target.value)}
        onSearch={onGlobalSearch}
        style={{ width: 360 }}
        className="header-search-input"
      />
      <Space size={6}>
        <Switch size="small" checked={showInactive} onChange={onToggleShowInactive} />
        <span style={{ fontSize: 12, color: '#8c8c8c' }}>Без контракта</span>
      </Space>
    </Space>
  );
};

export default HeaderSearch;
