import React, { useEffect, useRef, useState } from 'react';
import { CloseOutlined, SearchOutlined } from '@ant-design/icons';
import { Button, Input, Popover, Typography } from 'antd';
import type { InputRef } from 'antd';
import { useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import SearchResultsContent from '@/components/search/SearchResultsContent';
import { TEXT_SEARCH_DEBOUNCE_MS, useDebouncedValue } from '@/hooks/useDebouncedValue';

const { Text } = Typography;

type Props = {
  collapsed: boolean;
  sidebarWidth: number;
};

const GlobalSearchLauncher: React.FC<Props> = ({ collapsed, sidebarWidth }) => {
  const { t } = useTranslation(['layout']);
  const location = useLocation();
  const [open, setOpen] = useState(false);
  const [term, setTerm] = useState('');
  const [appliedTerm, setAppliedTerm] = useState('');
  const debouncedTerm = useDebouncedValue(term.trim(), TEXT_SEARCH_DEBOUNCE_MS);
  const popoverOffset = collapsed ? 12 : Math.max(12, Math.round(sidebarWidth * 0.06));
  const triggerRef = useRef<HTMLDivElement | null>(null);
  const popupRef = useRef<HTMLDivElement | null>(null);
  const collapsedInputRef = useRef<InputRef | null>(null);

  useEffect(() => {
    setOpen(false);
  }, [location.pathname, location.search]);

  useEffect(() => {
    setAppliedTerm(debouncedTerm);
  }, [debouncedTerm]);

  useEffect(() => {
    if (!open) {
      return undefined;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) {
        return;
      }

      if (triggerRef.current?.contains(target) || popupRef.current?.contains(target)) {
        return;
      }

      setOpen(false);
    };

    window.addEventListener('keydown', handleKeyDown);
    document.addEventListener('mousedown', handlePointerDown);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('mousedown', handlePointerDown);
    };
  }, [open]);

  useEffect(() => {
    if (!open || !collapsed) {
      return;
    }

    const timerID = window.setTimeout(() => {
      collapsedInputRef.current?.focus({ cursor: 'end' });
    }, 0);

    return () => window.clearTimeout(timerID);
  }, [collapsed, open]);

  const panelTitle = term.trim()
    ? t('layout:headerSearch.global.resultsTitle', { term: term.trim() })
    : t('layout:headerSearch.global.overlayTitle');

  const content = (
    <div
      ref={popupRef}
      className={`global-search-popover global-search-popover--${collapsed ? 'collapsed' : 'expanded'}`}
    >
      <div className="global-search-popover__header">
        <Text strong>{panelTitle}</Text>
        <Button
          type="text"
          shape="circle"
          icon={<CloseOutlined />}
          aria-label={t('layout:headerSearch.global.closeButton')}
          onClick={() => setOpen(false)}
        />
      </div>

      {collapsed ? (
        <Input
          ref={collapsedInputRef}
          allowClear
          size="large"
          prefix={<SearchOutlined />}
          placeholder={t('layout:headerSearch.global.placeholder')}
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          onPressEnter={() => setAppliedTerm(term.trim())}
        />
      ) : null}

      <div className="global-search-popover__results">
        <SearchResultsContent term={appliedTerm} variant="popover" />
      </div>
    </div>
  );

  return (
    <Popover
      open={open}
      content={content}
      trigger={[]}
      placement="rightTop"
      overlayClassName={`global-search-popover-root global-search-popover-root--${collapsed ? 'collapsed' : 'expanded'}`}
      align={{ offset: [popoverOffset, 0] }}
    >
      <div ref={triggerRef} className="global-search-launcher">
        {collapsed ? (
          <Button
            type="text"
            shape="circle"
            icon={<SearchOutlined />}
            aria-label={t('layout:headerSearch.global.openButton')}
            className="global-search-launcher global-search-launcher--collapsed"
            onClick={() => setOpen((current) => !current)}
          />
        ) : (
          <div className="global-search-launcher global-search-launcher--expanded">
            <Input
              allowClear
              size="large"
              prefix={<SearchOutlined />}
              placeholder={t('layout:headerSearch.global.placeholder')}
              value={term}
              onFocus={() => setOpen(true)}
              onClick={() => setOpen(true)}
              onChange={(event) => {
                setTerm(event.target.value);
                setOpen(true);
              }}
              onPressEnter={() => setAppliedTerm(term.trim())}
              className="global-search-launcher__input"
            />
          </div>
        )}
      </div>
    </Popover>
  );
};

export default GlobalSearchLauncher;
