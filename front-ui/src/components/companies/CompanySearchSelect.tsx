import React, { useEffect, useState } from 'react';
import { Select, Space, Typography } from 'antd';
import { getCompanyHierarchyParts } from '@/utils/companyHierarchy';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

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
  const [searchValue, setSearchValue] = useState('');
  const debouncedSearchValue = useDebouncedValue(searchValue, SELECT_SEARCH_DEBOUNCE_MS);
  const selectOptions = options.map((item) => ({
    value: item.value,
    label: renderCompanyOptionLabel(item.title, item.parentTitle),
  }));

  useEffect(() => {
    onSearch?.(debouncedSearchValue);
  }, [debouncedSearchValue, onSearch]);

  return (
    <Select
      showSearch
      searchValue={searchValue}
      value={value}
      allowClear={allowClear}
      filterOption={false}
      placeholder={placeholder}
      loading={loading}
      options={selectOptions}
      onSearch={setSearchValue}
      onInputKeyDown={(event) => {
        if (event.key === 'Enter') {
          onSearch?.(searchValue);
        }
      }}
      onChange={(nextValue) => {
        setSearchValue('');
        onChange?.(nextValue as string | undefined);
      }}
    />
  );
};
