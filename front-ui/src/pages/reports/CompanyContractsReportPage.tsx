import React, { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, Select, Space, Table, Typography, message } from 'antd';
import { DownloadOutlined } from '@ant-design/icons';
import { reportsApi } from '@/api/reports';
import { companiesApi } from '@/api/companies';
import { CompanyContractReportRowDTO } from '@/types/api';
import { downloadBlob, extractFileNameFromContentDisposition } from '@/utils/reportExport';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';
import { useLayoutHeader } from '@/components/layout/LayoutHeaderContext';

const { Text } = Typography;

type CompanyOption = { value: string; label: string };

const CompanyContractsReportPage: React.FC = () => {
  const { setHeaderConfig } = useLayoutHeader();
  const [statuses, setStatuses] = useState<string[]>([]);
  const [contractTypes, setContractTypes] = useState<string[]>([]);
  const [companyIDs, setCompanyIDs] = useState<string[]>([]);
  const [companySearch, setCompanySearch] = useState('');

  const reportQuery = useQuery({
    queryKey: ['report', 'companies-contracts', statuses, contractTypes, companyIDs],
    queryFn: () => reportsApi.getCompanyContractsReport({
      statuses,
      contract_types: contractTypes,
      company_ids: companyIDs,
    }),
    staleTime: 30_000,
  });

  const filterOptionsQuery = useQuery({
    queryKey: ['report', 'companies-contracts', 'filter-options'],
    queryFn: () => reportsApi.getCompanyContractsReport({}),
    staleTime: 60_000,
  });

  const companiesLookupQuery = useQuery({
    queryKey: ['companies', 'reports-company-filter', companySearch],
    queryFn: () => companiesApi.searchCompanies(companySearch, 30, 0),
    staleTime: 30_000,
  });

  const exportMutation = useMutation({
    mutationFn: () => reportsApi.exportCompanyContractsReport({
      statuses,
      contract_types: contractTypes,
      company_ids: companyIDs,
    }),
    onSuccess: (payload) => {
      const fromHeader = extractFileNameFromContentDisposition(payload.contentDisposition);
      const fileName = fromHeader || `report-companies-contracts-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.xlsx`;
      downloadBlob(payload.blob, fileName);
      message.success('Отчет выгружен');
    },
    onError: () => {
      message.error('Не удалось выгрузить отчет');
    },
  });

  const rows = useMemo(() => reportQuery.data?.data || [], [reportQuery.data?.data]);
  const optionRows = useMemo(() => filterOptionsQuery.data?.data || rows, [filterOptionsQuery.data?.data, rows]);

  const statusOptions = useMemo(() => {
    const unique = new Set<string>();
    for (const row of optionRows) {
      if (row.contract_state) {
        unique.add(row.contract_state);
      } else {
        unique.add('without_contract');
      }
    }
    return Array.from(unique).map((value) => ({
      value,
      label: value === 'without_contract' ? 'Без контракта' : value,
    }));
  }, [optionRows]);

  const contractTypeOptions = useMemo(() => {
    const unique = new Set<string>();
    for (const row of optionRows) {
      const type = String(row.contract_type || '').trim();
      if (type) {
        unique.add(type);
      }
    }
    return Array.from(unique)
      .sort((a, b) => a.localeCompare(b, 'ru'))
      .map((value) => ({ value, label: value }));
  }, [optionRows]);

  const companyOptions = useMemo<CompanyOption[]>(() => {
    const lookup = companiesLookupQuery.data?.data || [];
    const base = lookup
      .map((item) => {
        const id = resolveCompanyID(item);
        if (!id) return null;
        const title = resolveCompanyTitle(item) || id;
        const parentTitle = resolveCompanyParentTitle(item);
        const label = parentTitle ? `${title} (${parentTitle})` : title;
        return { value: id, label };
      })
      .filter(Boolean) as CompanyOption[];

    const selectedFromReport = rows
      .filter((row) => companyIDs.includes(row.company_id))
      .map((row) => ({
        value: row.company_id,
        label: row.company_parent_title ? `${row.company_title} (${row.company_parent_title})` : row.company_title,
      }));

    const map = new Map<string, CompanyOption>();
    for (const option of [...selectedFromReport, ...base]) {
      map.set(option.value, option);
    }
    return Array.from(map.values());
  }, [companiesLookupQuery.data?.data, rows, companyIDs]);

  const columns = [
    {
      title: 'Компания',
      dataIndex: 'company_title',
      key: 'company_title',
      render: (_: unknown, row: CompanyContractReportRowDTO) => (
        <Space direction="vertical" size={0}>
          <Text strong>{row.company_title}</Text>
          <Text type="secondary">ID: {row.company_id}</Text>
        </Space>
      ),
    },
    {
      title: 'Родительская компания',
      dataIndex: 'company_parent_title',
      key: 'company_parent_title',
      render: (value: string | undefined) => value || '-',
    },
    {
      title: 'Статус компании',
      dataIndex: 'company_contract_status',
      key: 'company_contract_status',
      render: (value: string) => value || '-',
    },
    {
      title: 'Тип контракта',
      dataIndex: 'contract_type',
      key: 'contract_type',
      render: (value: string | undefined) => value || '-',
    },
    {
      title: 'Статус контракта',
      dataIndex: 'contract_state',
      key: 'contract_state',
      render: (value: string | undefined) => value || 'without_contract',
    },
    {
      title: 'ID контракта',
      dataIndex: 'contract_id',
      key: 'contract_id',
      render: (value: string | undefined) => value || '-',
    },
  ];

  const headerControls = useMemo(() => (
    <Space wrap size={8}>
      <Select
        mode="multiple"
        allowClear
        placeholder="Статусы контрактов"
        value={statuses}
        options={statusOptions}
        style={{ minWidth: 220 }}
        onChange={(values) => setStatuses(values)}
      />
      <Select
        mode="multiple"
        allowClear
        placeholder="Типы контрактов"
        value={contractTypes}
        options={contractTypeOptions}
        style={{ minWidth: 260 }}
        onChange={(values) => setContractTypes(values)}
      />
      <Select
        mode="multiple"
        allowClear
        showSearch
        filterOption={false}
        placeholder="Компании"
        value={companyIDs}
        options={companyOptions}
        style={{ minWidth: 320 }}
        onSearch={setCompanySearch}
        onChange={(values) => setCompanyIDs(values)}
      />
      <Button
        type="primary"
        icon={<DownloadOutlined />}
        loading={exportMutation.isPending}
        onClick={() => exportMutation.mutate()}
      >
        Скачать XLSX
      </Button>
    </Space>
  ), [
    statuses,
    statusOptions,
    contractTypes,
    contractTypeOptions,
    companyIDs,
    companyOptions,
    exportMutation.isPending,
  ]);

  useEffect(() => {
    setHeaderConfig({
      mode: 'reports',
      title: 'Отчет: Компании и контракты',
      controls: headerControls,
    });
    return () => setHeaderConfig(null);
  }, [headerControls, setHeaderConfig]);

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel">
        {reportQuery.isLoading ? (
          <Text type="secondary">Загрузка отчета...</Text>
        ) : rows.length === 0 ? (
          <Empty description="По выбранным фильтрам данных нет" />
        ) : (
          <Table
            rowKey={(row) => `${row.company_id}-${row.contract_id || 'none'}`}
            columns={columns}
            dataSource={rows}
            pagination={{ pageSize: 50, showSizeChanger: true }}
            size="small"
          />
        )}
      </Card>
    </Space>
  );
};

export default CompanyContractsReportPage;
