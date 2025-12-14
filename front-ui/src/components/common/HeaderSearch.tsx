import React from 'react';
import { Input } from 'antd';
import { useNavigate, useSearchParams } from 'react-router-dom';

const HeaderSearch: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const currentTerm = searchParams.get('term') || '';

  const onSearch = (value: string) => {
    if (value.trim()) {
      navigate(`/search?term=${encodeURIComponent(value.trim())}`);
    }
  };

  return (
    <Input.Search
      placeholder="Поиск по IP, Serial, Name..."
      allowClear
      defaultValue={currentTerm}
      onSearch={onSearch}
      style={{ width: 400 }}
      className="header-search-input"
    />
  );
};

export default HeaderSearch;