import React, { useCallback, useEffect, useState } from 'react';
import { Input, Space, Typography } from 'antd';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import SearchResultsContent from '@/components/search/SearchResultsContent';
import { TEXT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

const { Title, Text } = Typography;

const SearchPage: React.FC = () => {
  const { t } = useTranslation(['layout']);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const currentTerm = searchParams.get('term') || '';
  const [term, setTerm] = useState(currentTerm);
  const debouncedTerm = useDebouncedValue(term, TEXT_SEARCH_DEBOUNCE_MS);

  useEffect(() => {
    setTerm(currentTerm);
  }, [currentTerm]);

  const updateSearchParams = useCallback((nextTerm: string) => {
    const params = new URLSearchParams();
    if (nextTerm.trim()) {
      params.set('term', nextTerm.trim());
    }
    const query = params.toString();
    navigate(query ? `/search?${query}` : '/search');
  }, [navigate]);

  useEffect(() => {
    if (debouncedTerm.trim() === currentTerm.trim()) {
      return;
    }
    updateSearchParams(debouncedTerm);
  }, [currentTerm, debouncedTerm, updateSearchParams]);

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <div>
        <Title level={2} style={{ marginBottom: 8 }}>
          {t('layout:headerSearch.global.overlayTitle')}
        </Title>
        <Text type="secondary">
          {t('layout:headerSearch.global.pageHint')}
        </Text>
      </div>

      <Space size="middle" wrap style={{ width: '100%' }}>
        <Input.Search
          allowClear
          size="large"
          placeholder={t('layout:headerSearch.global.placeholder')}
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          onSearch={(value) => updateSearchParams(value)}
          style={{ width: 420, maxWidth: '100%' }}
        />
      </Space>

      <SearchResultsContent
        term={currentTerm}
        variant="page"
      />
    </Space>
  );
};

export default SearchPage;
