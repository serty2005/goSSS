import React from 'react';
import { Button, Checkbox, Dropdown, Input, Select, Space, Typography, theme as antTheme } from 'antd';
import { SettingOutlined } from '@ant-design/icons';
import { SELECT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

const { Text } = Typography;

export interface EntityListFilterOption {
  value: string;
  label: string;
}

export interface EntityListFilterConfig {
  key: string;
  label: string;
  placeholder: string;
  value: string[];
  options: EntityListFilterOption[];
  loading?: boolean;
  showSearch?: boolean;
  filterOption?: boolean;
  onSearch?: (value: string) => void;
  maxTagCount?: number | 'responsive';
  style?: React.CSSProperties;
  onChange: (nextValue: string[]) => void;
}

export interface EntityListColumnOption {
  key: string;
  label: string;
}

interface EntityListToolbarProps {
  showSearch?: boolean;
  searchValue: string;
  searchPlaceholder?: string;
  onSearchValueChange: (value: string) => void;
  onSearchSubmit: (value: string) => void;
  filters: EntityListFilterConfig[];
  columnOptions: EntityListColumnOption[];
  selectedColumnKeys: string[];
  onSelectedColumnKeysChange: (keys: string[]) => void;
  filterRows?: string[][];
  columnsButtonPlacement?: 'controls' | 'lastFilterRow';
}

const EntityListFilterSelect: React.FC<{ filter: EntityListFilterConfig }> = ({ filter }) => {
  const [searchValue, setSearchValue] = React.useState('');
  const debouncedSearchValue = useDebouncedValue(searchValue, SELECT_SEARCH_DEBOUNCE_MS);
  const { onSearch } = filter;

  React.useEffect(() => {
    if (!onSearch) {
      return;
    }
    onSearch(debouncedSearchValue);
  }, [debouncedSearchValue, onSearch]);

  return (
    <Select
      mode="multiple"
      showSearch={filter.showSearch ?? true}
      allowClear
      filterOption={filter.filterOption ?? true}
      maxTagCount={filter.maxTagCount}
      placeholder={filter.placeholder}
      value={filter.value}
      options={filter.options}
      loading={filter.loading}
      optionFilterProp="label"
      style={{ width: '100%' }}
      onSearch={setSearchValue}
      onInputKeyDown={(event) => {
        if (event.key === 'Enter') {
          filter.onSearch?.(searchValue);
        }
      }}
      onChange={(nextValue) => filter.onChange(nextValue.map(String))}
    />
  );
};

const EntityListToolbar: React.FC<EntityListToolbarProps> = ({
  showSearch = true,
  searchValue,
  searchPlaceholder,
  onSearchValueChange,
  onSearchSubmit,
  filters,
  columnOptions,
  selectedColumnKeys,
  onSelectedColumnKeysChange,
  filterRows,
  columnsButtonPlacement = 'controls',
}) => {
  const { token } = antTheme.useToken();
  const orderedSelectedKeys = columnOptions
    .map((item) => item.key)
    .filter((key) => selectedColumnKeys.includes(key));

  const columnMenu = (
    <div
      style={{
        minWidth: 260,
        padding: 8,
        background: token.colorBgElevated,
        border: `1px solid ${token.colorBorderSecondary}`,
        borderRadius: 10,
        boxShadow: token.boxShadowSecondary,
      }}
    >
      <Space direction="vertical" size={8} style={{ width: '100%' }}>
        <Text strong>Столбцы таблицы</Text>
        <Checkbox.Group
          style={{ display: 'flex', flexDirection: 'column', gap: 8 }}
          value={orderedSelectedKeys}
          onChange={(values) => onSelectedColumnKeysChange(values.map(String))}
        >
          {columnOptions.map((item) => (
            <Checkbox key={item.key} value={item.key}>
              {item.label}
            </Checkbox>
          ))}
        </Checkbox.Group>
      </Space>
    </div>
  );

  const renderColumnsButton = () => (
    <Dropdown popupRender={() => columnMenu} trigger={['click']}>
      <Button icon={<SettingOutlined />}>Столбцы</Button>
    </Dropdown>
  );

  const filterRowsResolved = React.useMemo(() => {
    if (!filterRows || filterRows.length === 0) {
      return [filters.map((filter) => filter.key)];
    }

    const declaredKeys = new Set(filterRows.flat());
    const extraKeys = filters
      .filter((filter) => !declaredKeys.has(filter.key))
      .map((filter) => filter.key);

    return [...filterRows, ...(extraKeys.length > 0 ? [extraKeys] : [])];
  }, [filterRows, filters]);

  const renderFilter = (filter: EntityListFilterConfig) => (
    <div
      key={filter.key}
      style={{
        minWidth: 240,
        flex: '1 1 240px',
        ...filter.style,
      }}
    >
      <EntityListFilterSelect filter={filter} />
    </div>
  );

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space wrap size={12} style={{ width: '100%' }}>
        {showSearch ? (
          <Input.Search
            value={searchValue}
            allowClear
            placeholder={searchPlaceholder}
            style={{ width: 360, maxWidth: '100%' }}
            onChange={(event) => onSearchValueChange(event.target.value)}
            onSearch={onSearchSubmit}
          />
        ) : null}
        {columnsButtonPlacement === 'controls' ? renderColumnsButton() : null}
      </Space>

      {filterRowsResolved.map((rowKeys, rowIndex) => {
        const rowFilters = rowKeys
          .map((key) => filters.find((filter) => filter.key === key))
          .filter((filter): filter is EntityListFilterConfig => Boolean(filter));
        const showColumnsHere = columnsButtonPlacement === 'lastFilterRow' && rowIndex === filterRowsResolved.length - 1;

        if (rowFilters.length === 0 && !showColumnsHere) {
          return null;
        }

        return (
          <Space
            key={`filters-row-${rowIndex}`}
            wrap
            size={12}
            style={{ width: '100%', alignItems: 'flex-start' }}
          >
            {rowFilters.map(renderFilter)}
            {showColumnsHere ? renderColumnsButton() : null}
          </Space>
        );
      })}
    </Space>
  );
};

export default EntityListToolbar;
