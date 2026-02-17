import React from 'react';
import { Select, Space, Typography } from 'antd';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';

const { Text } = Typography;

export interface CompanySearchOption {
  value: string;
  title: string;
  parentTitle?: string;
}

interface CompanySearchSelectProps {
  value?: string;
  options: CompanySearchOption[];
  loading?: boolean;
  placeholder?: string;
  allowClear?: boolean;
  onSearch?: (value: string) => void;
  onChange?: (value: string | undefined) => void;
}

const renderCompanyOptionLabel = (title: string, parentTitle?: string) => {
  const parts = getCompanyHierarchyParts(title, parentTitle);
  if (!parts.hasParent) {
    return parts.child;
  }

  return (
    <Space direction="vertical" size={0} style={{ lineHeight: 1.2 }}>
      <Text type="secondary" style={{ fontSize: 12 }}>
        {parts.parent}
      </Text>
      <Text style={{ paddingLeft: 14 }}>{parts.child}</Text>
    </Space>
  );
};

export const CompanySearchSelect: React.FC<CompanySearchSelectProps> = ({
  value,
  options,
  loading = false,
  placeholder,
  allowClear = false,
  onSearch,
  onChange,
}) => {
  const selectOptions = options.map((item) => ({
    value: item.value,
    label: renderCompanyOptionLabel(item.title, item.parentTitle),
  }));

  return (
    <Select
      showSearch
      value={value}
      allowClear={allowClear}
      filterOption={false}
      placeholder={placeholder}
      loading={loading}
      options={selectOptions}
      onSearch={onSearch}
      onChange={(nextValue) => onChange?.(nextValue as string | undefined)}
    />
  );
};
